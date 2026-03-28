package main

import (
	"errors"
	"fmt"
	"image"
)

// undoEntry stores a single undo snapshot. rotationAngle is non-nil for
// operations that also need to restore the accumulated disc rotation angle
// (e.g. StraightEdgeRotate) so that subsequent disc re-renders stay correct.
// preWarp is true when the entry was saved before any warp/disc/crop operation
// had produced a warpedImage; restoring such an entry returns the app to the
// initial cropping phase rather than a post-warp editing state.
type undoEntry struct {
	image           *image.NRGBA
	rotationAngle   *float64
	preWarp         bool
	selectedCorners []image.Point // in-progress corner clicks at save time (corner mode only)
}

// CropRequest specifies which edge to crop.
type CropRequest struct {
	Direction string `json:"direction"`
}

// RotateRequest specifies the rotation/flip operation to apply.
type RotateRequest struct {
	FlipCode int `json:"flipCode"`
}

// ResizeRequest specifies the target width and height for resizing an image.
type ResizeRequest struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// SetLevelsRequest carries explicit black- and white-point values.
type SetLevelsRequest struct {
	Black int `json:"black"`
	White int `json:"white"`
}

// DescreenRequest carries parameters for the FFT-based descreen filter.
type DescreenRequest struct {
	Thresh    int `json:"thresh"`
	Radius    int `json:"radius"`
	Middle    int `json:"middle"`
	Highlight int `json:"highlight"`
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
// The entry is tagged preWarp=true when warpedImage is nil at save time so
// that Undo() can restore the pre-crop state correctly.
func (a *App) saveUndo() {
	if len(a.undoStack) >= a.undoLimit {
		a.undoStack = a.undoStack[1:]
	}
	// warpedImage may be nil when a committing op runs before any warp
	// (e.g. AutoContrast in corner mode). Store currentImage in that case.
	preWarp := a.warpedImage == nil
	var img *image.NRGBA
	if a.warpedImage != nil {
		img = cloneImage(a.warpedImage)
	} else if a.currentImage != nil {
		img = cloneImage(a.currentImage)
	}
	a.undoStack = append(a.undoStack, undoEntry{image: img, preWarp: preWarp})
	// Any committing operation invalidates both adjustment baselines so that
	// the next SetLevels / Descreen call re-snapshots from the new working image.
	a.levelsBaseImage = nil
	a.descreenBaseImage = nil
	a.descreenResultImage = nil
}

// saveDiscRotationUndo is like saveUndo but also snapshots the current
// rotationAngle so that Undo() can restore disc re-renders to the correct angle.
func (a *App) saveDiscRotationUndo() {
	if len(a.undoStack) >= a.undoLimit {
		a.undoStack = a.undoStack[1:]
	}
	preWarp := a.warpedImage == nil
	var img *image.NRGBA
	if a.warpedImage != nil {
		img = cloneImage(a.warpedImage)
	} else if a.currentImage != nil {
		img = cloneImage(a.currentImage)
	}
	angle := a.rotationAngle
	a.undoStack = append(a.undoStack, undoEntry{image: img, rotationAngle: &angle, preWarp: preWarp})
	a.levelsBaseImage = nil
}

// Undo reverts the last operation on the image.
//
// When the popped entry carries preWarp=true (saved before any warp/crop had
// produced a warpedImage), Undo restores the app to the pre-crop phase:
// currentImage is set from the entry and warpedImage is cleared to nil.  Any
// disc state accumulated since then is also cleared.  The response carries
// Uncropped=true so the frontend can return to the initial cropping UI.
func (a *App) Undo() (*ProcessResult, error) {
	a.logf("Undo: stack depth=%d", len(a.undoStack))
	if len(a.undoStack) == 0 {
		a.logf("Undo: nothing to undo")
		return &ProcessResult{Message: "Nothing to undo"}, nil
	}
	entry := a.undoStack[len(a.undoStack)-1]
	a.undoStack = a.undoStack[:len(a.undoStack)-1]

	if entry.preWarp {
		// Restore the pre-warp image state: the saved image goes back into
		// currentImage, and warpedImage is cleared so that workingImage()
		// returns currentImage again (pre-crop state).
		a.currentImage = entry.image
		a.warpedImage = nil
		// Restore any in-progress corner selection that was captured at save time.
		if len(entry.selectedCorners) > 0 {
			sc := make([]image.Point, len(entry.selectedCorners))
			copy(sc, entry.selectedCorners)
			a.selectedCorners = sc
		} else {
			a.selectedCorners = nil
		}
		// Clear disc state that may have been built up since the save.
		a.discCenter = image.Point{}
		a.discRadius = 0
		a.rotationAngle = 0
		a.discBaseImage = nil
		a.discWorkingCrop = nil
		a.discWorkingCropRect = image.Rectangle{}
		a.postDiscBlack = 0
		a.postDiscWhite = 255
		a.levelsBaseImage = nil
		a.descreenBaseImage = nil
		a.descreenResultImage = nil
		a.logf("Undo: restored pre-warp state (selectedCorners=%d)", len(a.selectedCorners))
	} else {
		a.warpedImage = entry.image
		if entry.rotationAngle != nil {
			a.rotationAngle = *entry.rotationAngle
			a.logf("Undo: restored rotationAngle=%.3f°", a.rotationAngle)
		}
		a.descreenBaseImage = nil
		a.descreenResultImage = nil
	}

	img := a.workingImage()
	preview, err := imageToBase64(img)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	res := &ProcessResult{
		Preview:   preview,
		Width:     b.Dx(),
		Height:    b.Dy(),
		Uncropped: entry.preWarp,
	}
	if entry.preWarp {
		if len(a.detectedCorners) > 0 {
			res.Corners = a.detectedCorners
		}
		if len(a.selectedCorners) > 0 {
			res.SelectedCorners = a.selectedCorners
			res.Message = fmt.Sprintf("Corner %d of 4 selected", len(a.selectedCorners))
		} else {
			res.Message = "Crop undone — click 4 corners"
		}
	}
	return res, nil
}

// Crop removes pixels from the specified edge of the warped image.
func (a *App) Crop(req CropRequest) (*ProcessResult, error) {
	a.logf("Crop: direction=%q", req.Direction)
	if a.warpedImage == nil {
		const msg = "Crop: no warped image"
		a.logf(msg)
		return nil, errors.New(msg)
	}
	descreenReset := a.descreenResultImage != nil
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
	nb := a.warpedImage.Bounds()
	return &ProcessResult{Preview: preview, Width: nb.Dx(), Height: nb.Dy(), DescreenReset: descreenReset}, nil
}

// Rotate applies a 90-degree rotation to the warped image.
func (a *App) Rotate(req RotateRequest) (*ProcessResult, error) {
	a.logf("Rotate: flipCode=%d", req.FlipCode)
	if a.warpedImage == nil {
		const msg = "Rotate: no warped image"
		a.logf(msg)
		return nil, errors.New(msg)
	}
	descreenReset := a.descreenResultImage != nil
	a.saveUndo()

	a.warpedImage = rotate90(a.warpedImage, req.FlipCode)

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	rb := a.warpedImage.Bounds()
	return &ProcessResult{Preview: preview, Width: rb.Dx(), Height: rb.Dy(), DescreenReset: descreenReset}, nil
}

// ResizeImage applies an explicit width/height resize against the working image.
func (a *App) ResizeImage(req ResizeRequest) (*ProcessResult, error) {
	a.logf("ResizeImage: %dx%d", req.Width, req.Height)
	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	if req.Width <= 0 || req.Height <= 0 {
		return nil, fmt.Errorf("invalid dimensions")
	}

	src := a.workingImage()
	if src == nil {
		return nil, fmt.Errorf("no working image")
	}

	descreenReset := a.descreenResultImage != nil
	a.saveUndo()
	resized := resizeNRGBA(src, req.Width, req.Height)
	a.setWorkingImage(resized)

	preview, err := imageToBase64(resized)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{
		Preview:       preview,
		Message:       fmt.Sprintf("Resized to %dx%d", req.Width, req.Height),
		Width:         req.Width,
		Height:        req.Height,
		DescreenReset: descreenReset,
	}, nil
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

	descreenReset := a.descreenResultImage != nil
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
			Preview:       preview,
			Message:       fmt.Sprintf("Auto Contrast applied (black=%d, white=%d)", bp, wp),
			Black:         bp,
			White:         wp,
			Width:         b.Dx(),
			Height:        b.Dy(),
			DescreenReset: descreenReset,
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
		result.DescreenReset = descreenReset
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
		Preview:       preview,
		Message:       fmt.Sprintf("Auto Contrast applied (black=%d, white=%d)", bp, wp),
		Black:         bp,
		White:         wp,
		Width:         b.Dx(),
		Height:        b.Dy(),
		DescreenReset: descreenReset,
	}, nil
}

// TrimBorders scans each edge of the working image and removes runs of
// near-white (≥240 per channel) or near-black (≤15 per channel) rows/columns.
// A row or column is treated as a border strip when at least 99% of its pixels
// qualify as near-white or near-black.
func (a *App) TrimBorders() (*ProcessResult, error) {
	a.logf("TrimBorders")
	if a.warpedImage == nil {
		const msg = "TrimBorders: no processed image"
		a.logf(msg)
		return nil, errors.New(msg)
	}

	img := a.warpedImage
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	// isBorderRow returns true when ≥99% of the row's pixels are near-white or near-black.
	isBorderRow := func(y int) bool {
		count := 0
		for x := 0; x < w; x++ {
			off := y*img.Stride + x*4
			r, g, bv := img.Pix[off], img.Pix[off+1], img.Pix[off+2]
			if (r >= 240 && g >= 240 && bv >= 240) || (r <= 15 && g <= 15 && bv <= 15) {
				count++
			}
		}
		return count*100 >= w*99
	}

	// isBorderCol returns true when ≥99% of the column's pixels are near-white or near-black.
	isBorderCol := func(x int) bool {
		count := 0
		for y := 0; y < h; y++ {
			off := y*img.Stride + x*4
			r, g, bv := img.Pix[off], img.Pix[off+1], img.Pix[off+2]
			if (r >= 240 && g >= 240 && bv >= 240) || (r <= 15 && g <= 15 && bv <= 15) {
				count++
			}
		}
		return count*100 >= h*99
	}

	top := 0
	for top < h && isBorderRow(top) {
		top++
	}
	bottom := h
	for bottom > top && isBorderRow(bottom-1) {
		bottom--
	}
	left := 0
	for left < w && isBorderCol(left) {
		left++
	}
	right := w
	for right > left && isBorderCol(right-1) {
		right--
	}

	if top == 0 && bottom == h && left == 0 && right == w {
		a.logf("TrimBorders: no border strips detected")
		preview, err := imageToBase64(img)
		if err != nil {
			return nil, err
		}
		return &ProcessResult{Preview: preview, Message: "No border strips detected", Width: w, Height: h}, nil
	}

	descreenReset := a.descreenResultImage != nil
	a.saveUndo()
	a.warpedImage = subImage(img, image.Rect(left, top, right, bottom))

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	nb := a.warpedImage.Bounds()
	a.logf("TrimBorders: trimmed top=%d bottom=%d left=%d right=%d → %dx%d", top, h-bottom, left, w-right, nb.Dx(), nb.Dy())
	return &ProcessResult{
		Preview:       preview,
		Message:       fmt.Sprintf("Trimmed borders (top=%d, bottom=%d, left=%d, right=%d)", top, h-bottom, left, w-right),
		Width:         nb.Dx(),
		Height:        nb.Dy(),
		DescreenReset: descreenReset,
	}, nil
}

// Descreen applies the FFT-based halftone descreen filter to the working
// image.
//
// Consecutive Descreen calls apply to the same base image (descreenBaseImage)
// so that changing parameters shows the effect on the original without
// needing to undo between attempts. saveUndo() is called only at the start of
// each new session.
//
// A new session begins when warpedImage differs from descreenResultImage — the
// pointer last written by Descreen itself. This detects changes made by any
// operation including SetLevels (which does not call saveUndo), so a
// re-application of Descreen will never silently discard an intervening
// adjustment.
//
//	thresh — distance-weighted log-magnitude threshold (0–200; default 92)
//	radius — dilation/blur radius for the suppression mask (1–20; default 6)
//	middle — DC neighbourhood preservation ratio (1–10; default 4)
func (a *App) Descreen(req DescreenRequest) (*ProcessResult, error) {
	a.logf("Descreen: thresh=%d radius=%d middle=%d highlight=%d", req.Thresh, req.Radius, req.Middle, req.Highlight)

	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	// Start a new descreen session when:
	//   (a) no base has been captured yet, OR
	//   (b) warpedImage was changed by a non-descreen operation since the last
	//       Descreen call (pointer differs from descreenResultImage).
	if a.descreenBaseImage == nil || a.workingImage() != a.descreenResultImage {
		src := a.workingImage()
		if src == nil {
			return nil, fmt.Errorf("no image loaded")
		}
		a.saveUndo() // pushes undo entry and clears both baselines
		a.descreenBaseImage = cloneImage(src)
		a.logf("Descreen: captured descreenBaseImage")
	}

	filtered := applyDescreen(a.descreenBaseImage, req.Thresh, req.Radius, req.Middle, req.Highlight, a.logf)
	a.setWorkingImage(filtered)
	a.descreenResultImage = filtered // track pointer so next call can detect external changes

	preview, err := imageToBase64(filtered)
	if err != nil {
		return nil, err
	}
	rb := filtered.Bounds()
	return &ProcessResult{
		Preview: preview,
		Message: fmt.Sprintf("Descreen applied (thresh=%d, radius=%d, middle=%d, highlight=%d)", req.Thresh, req.Radius, req.Middle, req.Highlight),
		Width:   rb.Dx(),
		Height:  rb.Dy(),
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
	descreenReset := a.descreenResultImage != nil

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
		return &ProcessResult{Preview: preview, Width: b.Dx(), Height: b.Dy(), DescreenReset: descreenReset}, nil
	}

	// Post-warp, disc mode: record the new levels and re-render the full disc
	// pipeline. redrawDisc will apply postDiscBlack/White at the end, keeping
	// the stretch alive across shift / rotate / feather operations.
	if a.discRadius > 0 {
		a.postDiscBlack = req.Black
		a.postDiscWhite = req.White
		result, err := a.redrawDisc()
		if err != nil {
			return nil, err
		}
		result.DescreenReset = descreenReset
		return result, nil
	}

	// Post-warp, non-disc path (corner / line mode after warp).
	adjusted := applyLevels(a.levelsBaseImage, req.Black, req.White)
	a.warpedImage = adjusted
	preview, err := imageToBase64(adjusted)
	if err != nil {
		return nil, err
	}
	b := adjusted.Bounds()
	return &ProcessResult{Preview: preview, Width: b.Dx(), Height: b.Dy(), DescreenReset: descreenReset}, nil
}
