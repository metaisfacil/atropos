package main

import (
	"fmt"
	"image"
)

// undoEntry stores a single undo snapshot. rotationAngle is non-nil for
// operations that also need to restore the accumulated disc rotation angle
// (e.g. StraightEdgeRotate) so that subsequent disc re-renders stay correct.
type undoEntry struct {
	image         *image.NRGBA
	rotationAngle *float64
}

// CropRequest specifies which edge to crop.
type CropRequest struct {
	Direction string `json:"direction"`
}

// RotateRequest specifies the rotation/flip operation to apply.
type RotateRequest struct {
	FlipCode int `json:"flipCode"`
}

// SetLevelsRequest carries explicit black- and white-point values.
type SetLevelsRequest struct {
	Black int `json:"black"`
	White int `json:"white"`
}

// workingImage returns the image that adjustment operations should act on.
// Once a warp/crop/disc operation has produced a warpedImage that is what
// the user is editing; before any such operation it is currentImage.
func (a *App) workingImage() *image.NRGBA {
	if a.warpedImage != nil {
		return a.warpedImage
	}
	return a.currentImage
}

// setWorkingImage stores the result of an adjustment. It always writes to
// warpedImage so that SaveImage (which only reads warpedImage) always has
// something to save, even when the user adjusts before cropping.
func (a *App) setWorkingImage(img *image.NRGBA) {
	a.warpedImage = img
}

// saveUndo pushes the current working image onto the undo stack and clears
// the levels baseline so the next SetLevels session gets a fresh snapshot.
func (a *App) saveUndo() {
	if len(a.undoStack) >= a.undoLimit {
		a.undoStack = a.undoStack[1:]
	}
	// warpedImage may be nil when a committing op runs before any warp
	// (e.g. AutoContrast in corner mode). Store currentImage in that case.
	var img *image.NRGBA
	if a.warpedImage != nil {
		img = cloneImage(a.warpedImage)
	} else if a.currentImage != nil {
		img = cloneImage(a.currentImage)
	}
	a.undoStack = append(a.undoStack, undoEntry{image: img})
	// Any committing operation invalidates the levels baseline so that the
	// next SetLevels call re-snapshots from the new working image.
	a.levelsBaseImage = nil
}

// saveDiscRotationUndo is like saveUndo but also snapshots the current
// rotationAngle so that Undo() can restore disc re-renders to the correct angle.
func (a *App) saveDiscRotationUndo() {
	if len(a.undoStack) >= a.undoLimit {
		a.undoStack = a.undoStack[1:]
	}
	var img *image.NRGBA
	if a.warpedImage != nil {
		img = cloneImage(a.warpedImage)
	} else if a.currentImage != nil {
		img = cloneImage(a.currentImage)
	}
	angle := a.rotationAngle
	a.undoStack = append(a.undoStack, undoEntry{image: img, rotationAngle: &angle})
	a.levelsBaseImage = nil
}

// Undo reverts the last operation on the image.
func (a *App) Undo() (*ProcessResult, error) {
	a.logf("Undo: stack depth=%d", len(a.undoStack))
	if len(a.undoStack) == 0 {
		a.logf("Undo: nothing to undo")
		return &ProcessResult{Message: "Nothing to undo"}, nil
	}
	entry := a.undoStack[len(a.undoStack)-1]
	a.undoStack = a.undoStack[:len(a.undoStack)-1]
	a.warpedImage = entry.image
	if entry.rotationAngle != nil {
		a.rotationAngle = *entry.rotationAngle
		a.logf("Undo: restored rotationAngle=%.3f°", a.rotationAngle)
	}

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	b := a.warpedImage.Bounds()
	return &ProcessResult{Preview: preview, Width: b.Dx(), Height: b.Dy()}, nil
}

// Crop removes pixels from the specified edge of the warped image.
func (a *App) Crop(req CropRequest) (*ProcessResult, error) {
	a.logf("Crop: direction=%q", req.Direction)
	if a.warpedImage == nil {
		a.logf("Crop: no warped image")
		return nil, fmt.Errorf("no warped image")
	}
	a.saveUndo()

	b := a.warpedImage.Bounds()
	r := b

	switch req.Direction {
	case "top":
		if a.cropTop < b.Dy()-1 {
			a.cropTop += a.cropAmount
			r.Min.Y += a.cropAmount
		}
	case "bottom":
		if a.cropBottom < b.Dy()-1 {
			a.cropBottom += a.cropAmount
			r.Max.Y -= a.cropAmount
		}
	case "left":
		if a.cropLeft < b.Dx()-1 {
			a.cropLeft += a.cropAmount
			r.Min.X += a.cropAmount
		}
	case "right":
		if a.cropRight < b.Dx()-1 {
			a.cropRight += a.cropAmount
			r.Max.X -= a.cropAmount
		}
	}

	a.warpedImage = subImage(a.warpedImage, r)

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
}

// Rotate applies a 90-degree rotation to the warped image.
func (a *App) Rotate(req RotateRequest) (*ProcessResult, error) {
	a.logf("Rotate: flipCode=%d", req.FlipCode)
	if a.warpedImage == nil {
		a.logf("Rotate: no warped image")
		return nil, fmt.Errorf("no warped image")
	}
	a.saveUndo()

	a.warpedImage = rotate90(a.warpedImage, req.FlipCode)

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
}

// AutoContrast computes the luminance min/max of the working image, stretches
// all channels so that min->0 and max->255, and returns the updated preview.
// Matches the behaviour of Photoshop Image > Auto Contrast.
//
// Pre-warp path: applies to currentImage and re-renders the corner overlay so
// dots remain visible.  Post-warp path: applies to warpedImage as normal.
// If the sliders have been partially dragged (levelsBaseImage != nil), the
// stretch runs against that base so it does not stack on a partial drag.
//
// Disc mode (post-warp with discRadius > 0): the computed black/white points
// are stored in postDiscBlack/postDiscWhite and the disc is re-rendered via
// redrawDisc so that subsequent shift/rotate/feather operations continue to
// apply the same tonal adjustment and never silently revert it.
func (a *App) AutoContrast() (*ProcessResult, error) {
	a.logf("AutoContrast")

	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	preWarp := a.warpedImage == nil

	// Prefer the pre-adjustment base when the sliders have been touched.
	var img *image.NRGBA
	if a.levelsBaseImage != nil {
		img = a.levelsBaseImage
	} else if preWarp {
		img = a.currentImage
	} else {
		img = a.warpedImage
	}

	// Capture a snapshot of the image before we commit the AutoContrast so
	// that slider sessions can still reference the pre-adjustment base. We
	// call saveUndo() to push the previous state onto the undo stack (which
	// clears levelsBaseImage), and then restore our captured snapshot into
	// levelsBaseImage so the sliders can revert back to the original image.
	preLevelsBase := cloneImage(img)
	a.saveUndo()

	bp, wp := computeAutoContrastPoints(img)
	a.logf("AutoContrast: blackPt=%d whitePt=%d", bp, wp)
	adjusted := applyLevels(img, bp, wp)

	if preWarp {
		a.currentImage = adjusted
		// Restore the pre-adjustment base so SetLevels sessions operate
		// against the original image (allowing sliders to revert the effect).
		a.levelsBaseImage = preLevelsBase
		preview, err := imageToBase64(adjusted)
		if err != nil {
			return nil, err
		}
		b := adjusted.Bounds()
		return &ProcessResult{
			Preview: preview,
			Message: fmt.Sprintf("Auto Contrast applied (black=%d, white=%d)", bp, wp),
			Black:   bp,
			White:   wp,
			Width:   b.Dx(),
			Height:  b.Dy(),
		}, nil
	}

	// Post-warp, disc mode: store the computed points as postDisc levels and
	// re-render through redrawDisc so the adjustment survives future disc
	// operations (shift, rotate, feather). The disc re-render applies
	// postDiscBlack/White at the end of every render, so this is persistent.
	if a.discRadius > 0 {
		a.postDiscBlack = bp
		a.postDiscWhite = wp
		a.levelsBaseImage = preLevelsBase
		result, err := a.redrawDisc()
		if err != nil {
			return nil, err
		}
		result.Message = fmt.Sprintf("Auto Contrast applied (black=%d, white=%d)", bp, wp)
		result.Black = bp
		result.White = wp
		return result, nil
	}

	// Post-warp, non-disc path (corner / line mode after warp).
	a.warpedImage = adjusted
	// Restore the pre-adjustment base for slider sessions as above.
	a.levelsBaseImage = preLevelsBase
	preview, err := imageToBase64(adjusted)
	if err != nil {
		return nil, err
	}
	b := adjusted.Bounds()
	return &ProcessResult{
		Preview: preview,
		Message: fmt.Sprintf("Auto Contrast applied (black=%d, white=%d)", bp, wp),
		Black:   bp,
		White:   wp,
		Width:   b.Dx(),
		Height:  b.Dy(),
	}, nil
}

// SetLevels applies an explicit levels stretch to the working image.
// Called while the user drags the Black Point / White Point sliders.
//
// Each call always applies against levelsBaseImage — a snapshot taken the
// first time the sliders are touched after a committing operation. This
// prevents the stacking bug where each drag re-stretches the already-stretched
// result. saveUndo (called by Crop, Rotate, AutoContrast, etc.) clears
// levelsBaseImage so the next slider session gets a fresh base.
//
// Pre-warp path: writes to currentImage so drawCornerOverlay keeps rendering
// dots on top of the adjusted pixels.
//
// Disc mode (post-warp with discRadius > 0): the requested black/white values
// are stored in postDiscBlack/postDiscWhite and the disc is re-rendered via
// redrawDisc. This guarantees the stretch survives any subsequent disc
// operation (shift, rotate, feather) because redrawDisc always re-applies
// postDiscBlack/White at the end of its pipeline.
func (a *App) SetLevels(req SetLevelsRequest) (*ProcessResult, error) {
	a.logf("SetLevels: black=%d white=%d", req.Black, req.White)

	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	preWarp := a.warpedImage == nil

	// Snapshot the base on first touch; reuse on every subsequent drag.
	if a.levelsBaseImage == nil {
		if preWarp {
			a.levelsBaseImage = cloneImage(a.currentImage)
		} else {
			a.levelsBaseImage = cloneImage(a.warpedImage)
		}
		a.logf("SetLevels: captured levelsBaseImage (preWarp=%v)", preWarp)
	}

	// Do NOT call saveUndo — slider ticks must not flood the undo stack.

	if preWarp {
		adjusted := applyLevels(a.levelsBaseImage, req.Black, req.White)
		a.currentImage = adjusted
		// Frontend renders corner dots via SVG; return plain preview.
		preview, err := imageToBase64(adjusted)
		if err != nil {
			return nil, err
		}
		b := adjusted.Bounds()
		return &ProcessResult{Preview: preview, Width: b.Dx(), Height: b.Dy()}, nil
	}

	// Post-warp, disc mode: record the new levels and re-render the full disc
	// pipeline. redrawDisc will apply postDiscBlack/White at the end, keeping
	// the stretch alive across shift / rotate / feather operations.
	if a.discRadius > 0 {
		a.postDiscBlack = req.Black
		a.postDiscWhite = req.White
		return a.redrawDisc()
	}

	// Post-warp, non-disc path (corner / line mode after warp).
	adjusted := applyLevels(a.levelsBaseImage, req.Black, req.White)
	a.warpedImage = adjusted
	preview, err := imageToBase64(adjusted)
	if err != nil {
		return nil, err
	}
	b := adjusted.Bounds()
	return &ProcessResult{Preview: preview, Width: b.Dx(), Height: b.Dy()}, nil
}
