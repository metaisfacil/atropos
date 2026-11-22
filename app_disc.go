package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"atropos/internal/imageops"
	"atropos/internal/raster"
)

// discWorkingCropShiftPadding is the extra margin (in pixels) added on each
// side of the disc bounding box when pre-cropping discBaseImage into
// discWorkingCrop. It defines how far the user can shift the disc before a
// re-crop from the full discBaseImage is needed.
const discWorkingCropShiftPadding = 500

type discCropInsets struct{ top, bottom, left, right int }

func (insets discCropInsets) crop(img *image.NRGBA) *image.NRGBA {
	if img == nil || insets == (discCropInsets{}) {
		return img
	}
	return raster.CropNRGBA(img, insets.rect(img.Bounds()))
}

// rect returns the sub-rectangle of b that crop keeps.
func (insets discCropInsets) rect(b image.Rectangle) image.Rectangle {
	if insets == (discCropInsets{}) {
		return b
	}
	r := image.Rect(b.Min.X+insets.left, b.Min.Y+insets.top,
		b.Max.X-insets.right, b.Max.Y-insets.bottom)
	// A changed radius or center can make previous trims too large. Keep
	// the remaining crop valid rather than letting CropNRGBA see an empty rect.
	r.Min.X = clamp(r.Min.X, b.Min.X, b.Max.X-1)
	r.Min.Y = clamp(r.Min.Y, b.Min.Y, b.Max.Y-1)
	r.Max.X = clamp(r.Max.X, r.Min.X+1, b.Max.X)
	r.Max.Y = clamp(r.Max.Y, r.Min.Y+1, b.Max.Y)
	return r
}

// discCropRect returns the region of base that redrawDisc crops for the
// current disc centre and radius.
func (a *App) discCropRect(base *image.NRGBA) image.Rectangle {
	ob := base.Bounds()
	return image.Rect(
		clamp(a.discCenter.X-a.discRadius, ob.Min.X, ob.Max.X),
		clamp(a.discCenter.Y-a.discRadius, ob.Min.Y, ob.Max.Y),
		clamp(a.discCenter.X+a.discRadius, ob.Min.X, ob.Max.X),
		clamp(a.discCenter.Y+a.discRadius, ob.Min.Y, ob.Max.Y),
	)
}

// discCutoutRadius returns the centre-cutout radius in source pixels (0 when
// the cutout is disabled).
func (a *App) discCutoutRadius() int {
	if !a.discCenterCutout || a.discCutoutPercent <= 0 {
		return 0
	}
	// Cutout radius = half the cutout diameter, which is discCutoutPercent% of the disc diameter.
	return int(math.Round(float64(a.discRadius) * float64(a.discCutoutPercent) / 100.0))
}

// refreshDiscWorkingCrop pre-crops discBaseImage around the current disc
// centre with a generous extra margin and stores the result in
// discWorkingCrop. Call this whenever discBaseImage or discCenter changes
// significantly (DrawDisc, large shift). After a successful refresh,
// redrawDisc can work entirely from the small working crop instead of the
// potentially huge discBaseImage.
func (a *App) refreshDiscWorkingCrop() {
	if a.discBaseImage == nil {
		a.discWorkingCrop = nil
		return
	}
	pad := a.discRadius + discWorkingCropShiftPadding
	ob := a.discBaseImage.Bounds()
	r := image.Rect(
		clamp(a.discCenter.X-pad, ob.Min.X, ob.Max.X),
		clamp(a.discCenter.Y-pad, ob.Min.Y, ob.Max.Y),
		clamp(a.discCenter.X+pad, ob.Min.X, ob.Max.X),
		clamp(a.discCenter.Y+pad, ob.Min.Y, ob.Max.Y),
	)
	a.discWorkingCrop = raster.CropNRGBA(a.discBaseImage, r)
	a.discWorkingCropRect = r
}

// bakeDiscWorkingImage records img as the new base for subsequent disc
// redraws whenever a post-disc raster edit modifies warpedImage directly.
// Prior rotation, levels, and edge crops are already baked into img, so
// delta rotation starts from the current rotationAngle and post-disc levels
// and crop insets reset to neutral.
func (a *App) bakeDiscWorkingImage(img *image.NRGBA) {
	if a.discRadius <= 0 || img == nil {
		return
	}
	a.discBakedBase = raster.CloneNRGBA(img)
	a.discBakedAngle = a.rotationAngle
	a.postDiscBlack = 0
	a.postDiscWhite = 255
	a.discCropInsets = discCropInsets{}
}

// DrawDisc extracts and applies a circular mask with feathering.
//
// A snapshot of currentImage is captured into discBaseImage so that any
// pre-disc tonal adjustments (levels, auto-contrast) are carried through every
// subsequent redrawDisc call. postDiscBlack/White are reset to their neutral
// values because this is a fresh disc with no post-commit adjustments yet.
func (a *App) DrawDisc(req DiscDrawRequest) (*ProcessResult, error) {
	a.logf("DrawDisc: center=(%d,%d) radius=%d", req.CenterX, req.CenterY, req.Radius)
	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	descreenReset := a.descreenResultImage != nil
	a.saveUndo()
	a.cropSkipped = false
	a.discCenter = image.Pt(req.CenterX, req.CenterY)
	a.discRadius = req.Radius
	a.rotationAngle = 0
	a.discCropInsets = discCropInsets{}
	a.discBakedBase = nil
	a.discBakedAngle = 0

	// Snapshot the adjusted working image as the immutable source for all
	// disc renders. Using currentImage (not originalImage) means any levels
	// or auto-contrast applied before the disc is drawn are preserved.
	a.discBaseImage = raster.CloneNRGBA(a.currentImage)

	// Reset post-disc levels to neutral — nothing has been applied on top yet.
	a.postDiscBlack = 0
	a.postDiscWhite = 255
	a.levelsBaseImage = nil

	// Pre-crop a compact working region so subsequent shifts and re-renders
	// read from a small, cache-friendly image rather than the huge original.
	a.refreshDiscWorkingCrop()

	// Keep a no-mask preview for live drag mode (disc overlay is decoupled from
	// content during momentum interactions).
	if a.discBaseImage != nil {
		if p, err := a.imagePreviewURL(a.discBaseImage); err == nil {
			a.discNoMaskPreview = p
		}
	}

	result, err := a.redrawDisc()
	if err != nil {
		return nil, err
	}
	result.DescreenReset = descreenReset
	return result, nil
}

// redrawDisc re-renders the disc crop using the current discCenter, discRadius,
// featherSize, bgColor, rotationAngle, and post-disc levels.
//
// Invariant: every disc re-render (shift, rotate, feather, eyedropper) must
// produce a result that is identical to what the user saw immediately after the
// last committing operation. To guarantee this, redrawDisc:
//
//  1. Crops from discBaseImage (the state of currentImage at DrawDisc time),
//     so pre-disc adjustments are never lost.
//  2. Re-applies accumulated rotationAngle, so rotating then shifting never
//     discards the rotation.
//  3. Re-applies postDiscBlack/White, so any levels the user set after the disc
//     was committed survive every subsequent disc re-render.
func (a *App) redrawDisc() (*ProcessResult, error) {
	a.redoStack = nil
	descreenReset := a.descreenResultImage != nil
	if a.discBakedBase != nil {
		return a.redrawBakedDisc(descreenReset)
	}
	base := a.discBaseImage
	if base == nil {
		// Safety fallback for callers that pre-date discBaseImage.
		base = a.originalImage
	}
	if base == nil || a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}

	// Determine the required crop rect in discBaseImage coords.
	// discRadius is the fixed outer edge of the selection. Feathering is drawn
	// inward from that edge, so changing featherSize must not resize the crop.
	reqRect := a.discCropRect(base)
	reqX1, reqY1, reqX2, reqY2 := reqRect.Min.X, reqRect.Min.Y, reqRect.Max.X, reqRect.Max.Y

	// Use the pre-cropped working region when it covers the required rect.
	// This avoids reading from the huge discBaseImage on every shift, which
	// would cause cache thrashing due to large image strides.
	// If the disc has shifted outside the working crop, refresh it first.
	if a.discWorkingCrop != nil {
		if !reqRect.In(a.discWorkingCropRect) {
			a.refreshDiscWorkingCrop()
		}
	}

	var cropped *image.NRGBA
	var localCenter image.Point
	if a.discWorkingCrop != nil && reqRect.In(a.discWorkingCropRect) {
		// Translate required rect into working-crop coords (which are 0-based).
		off := a.discWorkingCropRect.Min
		wcX1 := reqX1 - off.X
		wcY1 := reqY1 - off.Y
		wcX2 := reqX2 - off.X
		wcY2 := reqY2 - off.Y
		cropped = raster.CropNRGBA(a.discWorkingCrop, image.Rect(wcX1, wcY1, wcX2, wcY2))
		localCenter = image.Pt(a.discCenter.X-reqX1, a.discCenter.Y-reqY1)
	} else {
		// Fall back to the full base image (e.g. discWorkingCrop not yet built).
		cropped = raster.CropNRGBA(base, reqRect)
		localCenter = image.Pt(a.discCenter.X-reqX1, a.discCenter.Y-reqY1)
	}

	centerCutoutRadius := a.discCutoutRadius()

	// Prepare the unmasked disc preview (used during live drag operations).
	unmasked := cropped
	if a.rotationAngle != 0 {
		unmasked = imageops.Rotate(unmasked, a.rotationAngle, a.bgColor)
	}
	if a.postDiscBlack != 0 || a.postDiscWhite != 255 {
		unmasked = imageops.ApplyLevels(unmasked, a.postDiscBlack, a.postDiscWhite)
	}
	unmasked = a.discCropInsets.crop(unmasked)
	if p, err := a.imagePreviewURL(unmasked); err == nil {
		a.discNoMaskPreview = p
	}

	feathered := imageops.ApplyCircularMask(cropped, localCenter, a.discRadius, a.featherSize, centerCutoutRadius, a.bgColor)

	// Re-apply accumulated rotation so that ShiftDisc / SetFeatherSize / etc.
	// don't discard a rotation the user already applied.
	if a.rotationAngle != 0 {
		feathered = imageops.Rotate(feathered, a.rotationAngle, a.bgColor)
	}

	// Re-apply any post-disc levels so that shift / rotate / feather operations
	// never silently strip a tonal adjustment the user already committed.
	if a.postDiscBlack != 0 || a.postDiscWhite != 255 {
		feathered = imageops.ApplyLevels(feathered, a.postDiscBlack, a.postDiscWhite)
	}
	feathered = a.discCropInsets.crop(feathered)

	a.descreenBaseImage = nil
	a.descreenResultImage = nil
	a.descreenSelection = adjustmentSelectionKey{}
	a.warpedImage = feathered

	// Invalidate the levels baseline so the next SetLevels drag re-snapshots
	// from the freshly rendered disc image.
	a.levelsBaseImage = nil

	preview, err := a.imagePreviewURL(a.warpedImage)
	if err != nil {
		return nil, err
	}
	resultBounds := a.warpedImage.Bounds()

	return &ProcessResult{
		Preview:         preview,
		Width:           resultBounds.Dx(),
		Height:          resultBounds.Dy(),
		UnmaskedPreview: a.discNoMaskPreview,
		DiscCenterX:     a.discCenter.X,
		DiscCenterY:     a.discCenter.Y,
		DiscRadius:      a.discRadius,
		DiscRotation:    a.rotationAngle,
		DiscBgR:         int(a.bgColor.R),
		DiscBgG:         int(a.bgColor.G),
		DiscBgB:         int(a.bgColor.B),
		DescreenReset:   descreenReset,
	}, nil
}

func (a *App) redrawBakedDisc(descreenReset bool) (*ProcessResult, error) {
	if a.discBakedBase == nil || a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}
	deltaAngle := a.rotationAngle - a.discBakedAngle
	var img *image.NRGBA
	if deltaAngle != 0 {
		img = imageops.Rotate(a.discBakedBase, deltaAngle, a.bgColor)
	} else {
		img = raster.CloneNRGBA(a.discBakedBase)
	}
	if a.postDiscBlack != 0 || a.postDiscWhite != 255 {
		img = imageops.ApplyLevels(img, a.postDiscBlack, a.postDiscWhite)
	}
	img = a.discCropInsets.crop(img)

	a.descreenBaseImage = nil
	a.descreenResultImage = nil
	a.descreenSelection = adjustmentSelectionKey{}
	a.levelsBaseImage = nil
	a.warpedImage = img

	preview, err := a.imagePreviewURL(a.warpedImage)
	if err != nil {
		return nil, err
	}
	a.discNoMaskPreview = preview
	resultBounds := a.warpedImage.Bounds()

	return &ProcessResult{
		Preview:         preview,
		Width:           resultBounds.Dx(),
		Height:          resultBounds.Dy(),
		UnmaskedPreview: a.discNoMaskPreview,
		DiscCenterX:     a.discCenter.X,
		DiscCenterY:     a.discCenter.Y,
		DiscRadius:      a.discRadius,
		DiscRotation:    a.rotationAngle,
		DiscBgR:         int(a.bgColor.R),
		DiscBgG:         int(a.bgColor.G),
		DiscBgB:         int(a.bgColor.B),
		DescreenReset:   descreenReset,
	}, nil
}

// StraightEdgeRotate rotates the disc so that the reference edge drawn by the
// user becomes perfectly horizontal. Unlike RotateDisc (used for Q/E keys and
// Shift+drag), this operation pushes a full undo snapshot that includes the
// current rotationAngle, so that Ctrl+Z restores both the image and the angle.
func (a *App) StraightEdgeRotate(req StraightEdgeRotateRequest) (*ProcessResult, error) {
	a.logf("StraightEdgeRotate: angleDeg=%.3f (cumulative before: %.3f)", req.AngleDeg, a.rotationAngle)
	if a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}
	a.saveDiscRotationUndo()
	a.rotationAngle -= req.AngleDeg
	a.logf("StraightEdgeRotate: new rotationAngle=%.3f", a.rotationAngle)
	return a.redrawDisc()
}

// RotateDisc rotates the disc image by the specified angle.
func (a *App) RotateDisc(req DiscRotateRequest) (*ProcessResult, error) {
	a.logf("RotateDisc: angle=%.2f (cumulative before: %.2f)", req.Angle, a.rotationAngle)
	if a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}
	a.rotationAngle += req.Angle
	return a.redrawDisc()
}

// GetPixelColor samples the disc base image at (x,y), sets bgColor, and
// re-renders the disc crop so the feathered edge matches the sampled colour.
//
// Sampling from discBaseImage (rather than originalImage) means the colour
// reflects any pre-disc tonal adjustments the user applied.
func (a *App) GetPixelColor(req PixelColorRequest) (*ProcessResult, error) {
	a.redoStack = nil
	a.logf("GetPixelColor: x=%d y=%d", req.X, req.Y)

	// Prefer the disc base (which includes pre-disc adjustments); fall back to
	// currentImage for the pre-disc eyedropper case.
	src := a.discBaseImage
	if src == nil {
		src = a.currentImage
	}
	if src == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	b := src.Bounds()
	px := clamp(req.X, b.Min.X, b.Max.X-1)
	py := clamp(req.Y, b.Min.Y, b.Max.Y-1)
	c := src.NRGBAAt(px, py)
	a.bgColor = color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255}
	a.logf("GetPixelColor: sampled (%d,%d,%d) at (%d,%d)", c.R, c.G, c.B, px, py)

	if a.discRadius > 0 {
		return a.redrawDisc()
	}
	// No disc yet — just acknowledge.
	return &ProcessResult{
		Message: fmt.Sprintf("Background colour set to (%d,%d,%d)", c.R, c.G, c.B),
	}, nil
}

// ShiftDisc moves the disc center by (dx,dy) pixels and re-renders.
func (a *App) ShiftDisc(req ShiftDiscRequest) (*ProcessResult, error) {
	a.logf("ShiftDisc: dx=%d dy=%d", req.DX, req.DY)
	if a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}
	if a.discBaseImage == nil && a.currentImage != nil {
		a.discBaseImage = raster.CloneNRGBA(a.currentImage)
	}
	imgBounds := image.Rect(0, 0, 0, 0)
	if a.discBaseImage != nil {
		imgBounds = a.discBaseImage.Bounds()
	} else if a.currentImage != nil {
		imgBounds = a.currentImage.Bounds()
	}
	a.discCenter.X += req.DX
	a.discCenter.Y += req.DY

	minX := imgBounds.Min.X + a.discRadius
	maxX := imgBounds.Max.X - a.discRadius
	minY := imgBounds.Min.Y + a.discRadius
	maxY := imgBounds.Max.Y - a.discRadius
	if minX > maxX {
		minX = imgBounds.Min.X
		maxX = imgBounds.Max.X
	}
	if minY > maxY {
		minY = imgBounds.Min.Y
		maxY = imgBounds.Max.Y
	}
	a.discCenter.X = clamp(a.discCenter.X, minX, maxX)
	a.discCenter.Y = clamp(a.discCenter.Y, minY, maxY)

	a.logf("ShiftDisc: new center=(%d,%d)", a.discCenter.X, a.discCenter.Y)
	return a.redrawDisc()
}

// DiscShiftPreview reports the geometry of the current disc render so the UI
// can preview a Ctrl+drag shift: unmasked source content moves under a
// vignette that stays fixed in result space. It mirrors redrawDisc exactly
// (crop → mask → rotate about the crop centre → levels → inset crop) and does
// not mutate any state. Baked discs (post-disc raster edits) ignore the disc
// centre when redrawing, so no live shift preview is offered for them.
func (a *App) DiscShiftPreview() (*DiscShiftPreview, error) {
	if a.discRadius <= 0 || a.discBaseImage == nil || a.warpedImage == nil {
		return nil, fmt.Errorf("no disc defined")
	}
	if a.discBakedBase != nil {
		return nil, fmt.Errorf("disc shift preview unavailable after raster edits")
	}
	base := a.discBaseImage
	source, err := a.imagePreviewURL(base)
	if err != nil {
		return nil, err
	}

	req := a.discCropRect(base)
	// imageops.Rotate pivots on pixel index (w/2, h/2); in continuous
	// pixel-edge coordinates that is half a pixel further on.
	cropPivotX := float64(req.Dx())/2 + 0.5
	cropPivotY := float64(req.Dy())/2 + 0.5
	keep := a.discCropInsets.rect(image.Rect(0, 0, req.Dx(), req.Dy()))
	pivotX := cropPivotX - float64(keep.Min.X)
	pivotY := cropPivotY - float64(keep.Min.Y)

	// ApplyCircularMask measures from the centre pixel index.
	localX := float64(a.discCenter.X-req.Min.X) + 0.5 - cropPivotX
	localY := float64(a.discCenter.Y-req.Min.Y) + 0.5 - cropPivotY
	rad := a.rotationAngle * math.Pi / 180
	cos, sin := math.Cos(rad), math.Sin(rad)

	bg := a.bgColor
	if a.postDiscBlack != 0 || a.postDiscWhite != 255 {
		px := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		px.SetNRGBA(0, 0, color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: 255})
		bg = imageops.ApplyLevels(px, a.postDiscBlack, a.postDiscWhite).NRGBAAt(0, 0)
	}
	out := a.warpedImage.Bounds()
	return &DiscShiftPreview{
		Source:       source,
		CenterX:      a.discCenter.X,
		CenterY:      a.discCenter.Y,
		SourcePivotX: float64(req.Min.X) + cropPivotX,
		SourcePivotY: float64(req.Min.Y) + cropPivotY,
		PivotX:       pivotX,
		PivotY:       pivotY,
		MaskCenterX:  pivotX + cos*localX - sin*localY,
		MaskCenterY:  pivotY + sin*localX + cos*localY,
		Rotation:     a.rotationAngle,
		Radius:       a.discRadius,
		FeatherSize:  a.featherSize,
		CutoutRadius: a.discCutoutRadius(),
		BgR:          bg.R,
		BgG:          bg.G,
		BgB:          bg.B,
		LevelsBlack:  a.postDiscBlack,
		LevelsWhite:  a.postDiscWhite,
		Width:        out.Dx(),
		Height:       out.Dy(),
	}, nil
}

// SetFeatherRadius updates the fixed outer radius and re-renders the disc.
// Unlike SetFeatherSize, this intentionally changes the crop dimensions.
func (a *App) SetFeatherRadius(req FeatherRadiusRequest) (*ProcessResult, error) {
	a.logf("SetFeatherRadius: %d", req.Radius)
	if a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}
	if req.Radius < 1 {
		req.Radius = 1
	}
	a.discRadius = req.Radius
	return a.redrawDisc()
}

// SetFeatherSize updates the inward feather width and re-renders the disc.
func (a *App) SetFeatherSize(req FeatherSizeRequest) (*ProcessResult, error) {
	a.redoStack = nil
	a.logf("SetFeatherSize: %d", req.Size)
	if req.Size < 0 {
		req.Size = 0
	}
	a.featherSize = req.Size
	if a.discRadius > 0 {
		return a.redrawDisc()
	}
	return &ProcessResult{
		Message: fmt.Sprintf("Feather size set to %d", a.featherSize),
	}, nil
}

// SetBackgroundColor sets the background colour for disc mode.
func (a *App) SetBackgroundColor(r, g, b int) {
	a.redoStack = nil
	a.logf("SetBackgroundColor: r=%d g=%d b=%d", r, g, b)
	a.bgColor = color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
}

// resetDiscFields zeros all disc-related state. It is safe to call at any time
// (no image required) and is used both by ResetDisc and by the load helpers.
func (a *App) resetDiscFields() {
	a.discCenter = image.Point{}
	a.discRadius = 0
	a.rotationAngle = 0
	a.discBaseImage = nil
	a.discWorkingCrop = nil
	a.discWorkingCropRect = image.Rectangle{}
	a.discCropInsets = discCropInsets{}
	a.discBakedBase = nil
	a.discBakedAngle = 0
	a.discNoMaskPreview = ""
	a.postDiscBlack = 0
	a.postDiscWhite = 255
}

// ResetDisc clears the disc selection and restores the pre-disc image.
func (a *App) ResetDisc() (*ProcessResult, error) {
	a.cancelTouchup()
	a.redoStack = nil
	a.undoStack = nil
	a.logf("ResetDisc")
	descreenReset := a.descreenResultImage != nil
	a.levelsBaseImage = nil
	a.levelsSelection = adjustmentSelectionKey{}
	a.descreenBaseImage = nil
	a.descreenResultImage = nil
	a.descreenSelection = adjustmentSelectionKey{}
	a.resetDiscFields()
	a.warpedImage = nil
	a.cropSkipped = false

	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	preview, err := a.imagePreviewURL(a.currentImage)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview:       preview,
		Message:       "Disc selection reset — draw a new circle",
		Width:         b.Dx(),
		Height:        b.Dy(),
		DescreenReset: descreenReset,
	}, nil
}
