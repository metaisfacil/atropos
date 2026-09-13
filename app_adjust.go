package main

import (
	"errors"
	"fmt"
	"image"

	"atropos/internal/descreen"
	"atropos/internal/imageops"
	"atropos/internal/raster"
)

// undoEntry stores lossless tiled pixels and disc adjustment metadata.
// All new entries capture rotationAngle so subsequent disc re-renders use
// the restored angle, including after undoing non-rotation adjustments.
// preWarp is true when the entry was saved before any warp/disc/crop operation
// had produced a warpedImage; restoring such an entry returns the app to the
// initial cropping phase rather than a post-warp editing state.
type undoEntry struct {
	image           *undoImage
	disc            *historyDisc
	rotationAngle   *float64
	preWarp         bool
	postDiscBlack   int
	postDiscWhite   int
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
	Black     int                  `json:"black"`
	White     int                  `json:"white"`
	Selection *AdjustmentSelection `json:"selection,omitempty"`
}

// AutoContrastRequest optionally limits auto contrast to an image-space selection.
type AutoContrastRequest struct {
	Selection *AdjustmentSelection `json:"selection,omitempty"`
}

// DescreenRequest carries parameters for the FFT-based descreen filter.
type DescreenRequest struct {
	Thresh    int                  `json:"thresh"`
	Radius    int                  `json:"radius"`
	Middle    int                  `json:"middle"`
	Highlight int                  `json:"highlight"`
	Fast      bool                 `json:"fast"`
	Selection *AdjustmentSelection `json:"selection,omitempty"`
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
	a.redoStack = nil
	var previous undoEntry
	if n := len(a.undoStack); n > 0 {
		previous = a.undoStack[n-1]
	}
	a.undoStack = append(a.undoStack, a.captureHistory(previous))
	a.trimUndo()
	// Any committing operation invalidates both adjustment baselines so that
	// the next SetLevels / Descreen call re-snapshots from the new working image.
	a.levelsBaseImage = nil
	a.levelsSelection = adjustmentSelectionKey{}
	a.descreenBaseImage = nil
	a.descreenResultImage = nil
	a.descreenSelection = adjustmentSelectionKey{}
}

// saveDiscRotationUndo preserves the rotation-operation call sites. All undo
// entries now capture disc rotation and invalidate both adjustment sessions.
func (a *App) saveDiscRotationUndo() {
	a.saveUndo()
}

// Undo reverts the last operation on the image.
//
// When the popped entry carries preWarp=true (saved before any warp/crop had
// produced a warpedImage), Undo restores the app to the pre-crop phase:
// currentImage is set from the entry and warpedImage is cleared to nil.  Any
// disc state accumulated since then is also cleared.  The response carries
// Uncropped=true so the frontend can return to the initial cropping UI.
func (a *App) Undo() (*ProcessResult, error) { return a.stepHistory(false) }

// Redo restores the last undone result without re-running image processing.
func (a *App) Redo() (*ProcessResult, error) { return a.stepHistory(true) }

func (a *App) stepHistory(redo bool) (*ProcessResult, error) {
	// An in-flight touch-up has not entered history yet. Cancel it and leave
	// the existing stack alone; otherwise Undo could pop the preceding edit and
	// the worker could later push a snapshot of that wrong state.
	if a.cancelTouchup() {
		img := a.workingImage()
		if img == nil {
			return &ProcessResult{Message: "Touch-up cancelled"}, nil
		}
		preview, err := a.imagePreviewURL(img)
		if err != nil {
			return nil, err
		}
		b := img.Bounds()
		a.logf("Undo: cancelled in-flight touch-up; stack unchanged at depth=%d", len(a.undoStack))
		return &ProcessResult{
			Preview: preview,
			Message: "Touch-up cancelled",
			Width:   b.Dx(),
			Height:  b.Dy(),
		}, nil
	}

	from, to := &a.undoStack, &a.redoStack
	action, completed := "undo", "Undone"
	if redo {
		from, to = &a.redoStack, &a.undoStack
		action, completed = "redo", "Redone"
	}
	if len(*from) == 0 {
		return &ProcessResult{Message: "Nothing to " + action}, nil
	}
	entry := (*from)[len(*from)-1]
	restored := entry.image.restore()
	preview, err := a.imagePreviewURL(restored)
	if err != nil {
		return nil, err
	}
	// Prepare both directions before mutating the stacks or document.
	var discBase *image.NRGBA
	var unmasked string
	if entry.disc != nil {
		discBase = entry.disc.base.restore()
		if discBase != nil {
			unmasked, err = a.imagePreviewURL(discBase)
			if err != nil {
				return nil, err
			}
		}
	}
	inverse := a.captureHistory(entry)
	(*from)[len(*from)-1] = undoEntry{}
	*from = (*from)[:len(*from)-1]
	*to = append(*to, inverse)
	a.trimHistory(!redo)

	a.levelsBaseImage = nil
	a.levelsSelection = adjustmentSelectionKey{}
	a.descreenSelection = adjustmentSelectionKey{}
	a.postDiscBlack = entry.postDiscBlack
	a.postDiscWhite = entry.postDiscWhite

	if entry.preWarp {
		// Restore the pre-warp image state: the saved image goes back into
		// currentImage, and warpedImage is cleared so that workingImage()
		// returns currentImage again (pre-crop state).
		a.currentImage = restored
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
		a.discNoMaskPreview = ""
		a.discWorkingCropRect = image.Rectangle{}
		a.postDiscBlack = 0
		a.postDiscWhite = 255
		a.levelsBaseImage = nil
		a.descreenBaseImage = nil
		a.descreenResultImage = nil
		a.logf("Undo: restored pre-warp state (selectedCorners=%d)", len(a.selectedCorners))
	} else {
		a.warpedImage = restored
		if entry.rotationAngle != nil {
			a.rotationAngle = *entry.rotationAngle
			a.logf("Undo: restored rotationAngle=%.3f°", a.rotationAngle)
		}
		a.descreenBaseImage = nil
		a.descreenResultImage = nil
	}

	a.selectedCorners = append([]image.Point(nil), entry.selectedCorners...)
	a.restoreHistoryDisc(entry.disc, discBase, unmasked)
	b := restored.Bounds()
	res := &ProcessResult{
		Preview:       preview,
		Width:         b.Dx(),
		Height:        b.Dy(),
		Uncropped:     entry.preWarp,
		DescreenReset: true,
		Changed:       true,
		White:         255,
		Message:       fmt.Sprintf("%s (%d %s steps remaining)", completed, len(*from), action),
	}
	if a.discRadius > 0 {
		res.Black, res.White = a.postDiscBlack, a.postDiscWhite
		res.DiscRotation = a.rotationAngle
		res.HistoryDiscSettings = &DiscSettings{CenterCutout: a.discCenterCutout, CutoutPercent: a.discCutoutPercent}
		res.HistoryFeatherSize = a.featherSize
		res.DiscCenterX, res.DiscCenterY, res.DiscRadius = a.discCenter.X, a.discCenter.Y, a.discRadius
		res.UnmaskedPreview = a.discNoMaskPreview
		res.DiscBgR, res.DiscBgG, res.DiscBgB = int(a.bgColor.R), int(a.bgColor.G), int(a.bgColor.B)
	}
	if entry.preWarp {
		if len(a.detectedCorners) > 0 {
			res.Corners = a.detectedCorners
		}
		if len(a.selectedCorners) > 0 {
			res.SelectedCorners = a.selectedCorners
			res.Message = fmt.Sprintf("Corner %d of 4 selected", len(a.selectedCorners))
		} else {
			res.Message = completed + " - ready to crop"
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

	a.warpedImage = raster.CropNRGBA(a.warpedImage, r)

	preview, err := a.imagePreviewURL(a.warpedImage)
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

	a.warpedImage = imageops.Rotate90(a.warpedImage, req.FlipCode)

	preview, err := a.imagePreviewURL(a.warpedImage)
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
	resized := raster.ResizeNRGBA(src, req.Width, req.Height)
	a.setWorkingImage(resized)

	preview, err := a.imagePreviewURL(resized)
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
func (a *App) AutoContrast(req AutoContrastRequest) (*ProcessResult, error) {
	a.logf("AutoContrast")

	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	descreenReset := a.descreenResultImage != nil
	preWarp := a.warpedImage == nil
	working := a.workingImage()
	selectionRect, selectionKey, err := resolveAdjustmentSelection(req.Selection, working.Bounds())
	if err != nil {
		return nil, err
	}

	// Prefer the pre-adjustment base when the sliders have been touched.
	var img *image.NRGBA
	if a.levelsBaseImage != nil && a.levelsSelection == selectionKey {
		img = a.levelsBaseImage
	} else {
		img = working
	}

	// Capture a snapshot of the image before we commit the AutoContrast so
	// that slider sessions can still reference the pre-adjustment base. We
	// call saveUndo() to push the previous state onto the undo stack (which
	// clears levelsBaseImage), and then restore our captured snapshot into
	// levelsBaseImage so the sliders can revert back to the original image.
	preLevelsBase := raster.CloneNRGBA(img)
	a.saveUndo()

	contrastSource := img
	if selectionKey.Active {
		contrastSource = raster.CropNRGBA(img, selectionRect)
	}
	bp, wp := imageops.AutoContrastPoints(contrastSource)
	a.logf("AutoContrast: blackPt=%d whitePt=%d", bp, wp)
	adjusted := applyLevelsInSelection(img, bp, wp, selectionKey)

	if preWarp {
		a.currentImage = adjusted
		// Restore the pre-adjustment base so SetLevels sessions operate
		// against the original image (allowing sliders to revert the effect).
		a.levelsBaseImage = preLevelsBase
		a.levelsSelection = selectionKey
		preview, err := a.imagePreviewURL(adjusted)
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
	if a.discRadius > 0 && !selectionKey.Active {
		a.postDiscBlack = bp
		a.postDiscWhite = wp
		a.levelsBaseImage = preLevelsBase
		a.levelsSelection = selectionKey
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
	a.levelsSelection = selectionKey
	preview, err := a.imagePreviewURL(adjusted)
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
		preview, err := a.imagePreviewURL(img)
		if err != nil {
			return nil, err
		}
		return &ProcessResult{Preview: preview, Message: "No border strips detected", Width: w, Height: h}, nil
	}

	descreenReset := a.descreenResultImage != nil
	a.saveUndo()
	a.warpedImage = raster.CropNRGBA(img, image.Rect(left, top, right, bottom))

	preview, err := a.imagePreviewURL(a.warpedImage)
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
//	thresh — distance-weighted log-magnitude threshold (0–200; default 93)
//	radius — dilation/blur radius for the suppression mask (1–20; default 18)
//	middle — DC neighbourhood preservation ratio (1–10; default 6)
//	highlight — highlight restoration strength (0–100; default 50)
//	fast — filter one luminance plane and preserve source chroma
func (a *App) Descreen(req DescreenRequest) (*ProcessResult, error) {
	a.logf("Descreen: thresh=%d radius=%d middle=%d highlight=%d fast=%t", req.Thresh, req.Radius, req.Middle, req.Highlight, req.Fast)

	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	src := a.workingImage()
	selectionRect, selectionKey, err := resolveAdjustmentSelection(req.Selection, src.Bounds())
	if err != nil {
		return nil, err
	}

	// Start a new descreen session when:
	//   (a) no base has been captured yet, OR
	//   (b) warpedImage was changed by a non-descreen operation since the last
	//       Descreen call (pointer differs from descreenResultImage).
	if a.descreenBaseImage == nil || src != a.descreenResultImage || a.descreenSelection != selectionKey {
		a.saveUndo() // pushes undo entry and clears both baselines
		a.descreenBaseImage = raster.CloneNRGBA(src)
		a.descreenSelection = selectionKey
		a.logf("Descreen: captured descreenBaseImage")
	}

	filterSource := a.descreenBaseImage
	if selectionKey.Active {
		filterSource = raster.CropNRGBA(a.descreenBaseImage, selectionRect)
	}
	var filteredRegion *image.NRGBA
	if req.Fast {
		filteredRegion = descreen.ApplyLuminance(filterSource, req.Thresh, req.Radius, req.Middle, req.Highlight, a.logf)
	} else {
		filteredRegion = descreen.Apply(filterSource, req.Thresh, req.Radius, req.Middle, req.Highlight, a.logf)
	}
	filtered := filteredRegion
	if selectionKey.Active {
		filtered = compositeAdjustmentSelection(a.descreenBaseImage, filteredRegion, selectionRect)
	}
	a.setWorkingImage(filtered)
	a.descreenResultImage = filtered // track pointer so next call can detect external changes

	preview, err := a.imagePreviewURL(filtered)
	if err != nil {
		return nil, err
	}
	rb := filtered.Bounds()
	return &ProcessResult{
		Preview: preview,
		Message: fmt.Sprintf("Descreen applied (thresh=%d, radius=%d, middle=%d, highlight=%d, fast=%t)", req.Thresh, req.Radius, req.Middle, req.Highlight, req.Fast),
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
	working := a.workingImage()
	_, selectionKey, err := resolveAdjustmentSelection(req.Selection, working.Bounds())
	if err != nil {
		return nil, err
	}

	a.redoStack = nil

	// Snapshot the base on first touch; reuse on every subsequent drag.
	if a.levelsBaseImage == nil || a.levelsSelection != selectionKey {
		a.levelsBaseImage = raster.CloneNRGBA(working)
		a.levelsSelection = selectionKey
		a.logf("SetLevels: captured levelsBaseImage (preWarp=%v)", preWarp)
	}

	// Do NOT call saveUndo — slider ticks must not flood the undo stack.

	if preWarp {
		adjusted := applyLevelsInSelection(a.levelsBaseImage, req.Black, req.White, selectionKey)
		a.currentImage = adjusted
		// Frontend renders corner dots via SVG; return plain preview.
		preview, err := a.imagePreviewURL(adjusted)
		if err != nil {
			return nil, err
		}
		b := adjusted.Bounds()
		return &ProcessResult{Preview: preview, Width: b.Dx(), Height: b.Dy(), DescreenReset: descreenReset}, nil
	}

	// Post-warp, disc mode: record the new levels and re-render the full disc
	// pipeline. redrawDisc will apply postDiscBlack/White at the end, keeping
	// the stretch alive across shift / rotate / feather operations.
	if a.discRadius > 0 && !selectionKey.Active {
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
	adjusted := applyLevelsInSelection(a.levelsBaseImage, req.Black, req.White, selectionKey)
	a.warpedImage = adjusted
	preview, err := a.imagePreviewURL(adjusted)
	if err != nil {
		return nil, err
	}
	b := adjusted.Bounds()
	return &ProcessResult{Preview: preview, Width: b.Dx(), Height: b.Dy(), DescreenReset: descreenReset}, nil
}
