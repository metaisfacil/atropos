package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// DiscDrawRequest specifies the centre point and radius for a circular disc crop.
type DiscDrawRequest struct {
	CenterX int `json:"centerX"`
	CenterY int `json:"centerY"`
	Radius  int `json:"radius"`
}

// DiscRotateRequest specifies the rotation angle for disc mode.
type DiscRotateRequest struct {
	Angle float64 `json:"angle"`
}

// PixelColorRequest holds image-space coordinates for colour sampling.
type PixelColorRequest struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// ShiftDiscRequest holds the pixel offset to shift the disc centre by.
type ShiftDiscRequest struct {
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// FeatherSizeRequest holds the new feather radius.
type FeatherSizeRequest struct {
	Size int `json:"size"`
}

// discWorkingCropShiftPadding is the extra margin (in pixels) added on each
// side of the disc+feather bounding box when pre-cropping discBaseImage into
// discWorkingCrop. It defines how far the user can shift the disc before a
// re-crop from the full discBaseImage is needed.
const discWorkingCropShiftPadding = 500

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
	pad := a.discRadius + a.featherSize + discWorkingCropShiftPadding
	ob := a.discBaseImage.Bounds()
	r := image.Rect(
		clamp(a.discCenter.X-pad, ob.Min.X, ob.Max.X),
		clamp(a.discCenter.Y-pad, ob.Min.Y, ob.Max.Y),
		clamp(a.discCenter.X+pad, ob.Min.X, ob.Max.X),
		clamp(a.discCenter.Y+pad, ob.Min.Y, ob.Max.Y),
	)
	a.discWorkingCrop = subImage(a.discBaseImage, r)
	a.discWorkingCropRect = r
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
	a.saveUndo()
	a.discCenter = image.Pt(req.CenterX, req.CenterY)
	a.discRadius = req.Radius
	a.rotationAngle = 0

	// Snapshot the adjusted working image as the immutable source for all
	// disc renders. Using currentImage (not originalImage) means any levels
	// or auto-contrast applied before the disc is drawn are preserved.
	a.discBaseImage = cloneImage(a.currentImage)

	// Reset post-disc levels to neutral — nothing has been applied on top yet.
	a.postDiscBlack = 0
	a.postDiscWhite = 255
	a.levelsBaseImage = nil

	// Pre-crop a compact working region so subsequent shifts and re-renders
	// read from a small, cache-friendly image rather than the huge original.
	a.refreshDiscWorkingCrop()

	return a.redrawDisc()
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
	base := a.discBaseImage
	if base == nil {
		// Safety fallback for callers that pre-date discBaseImage.
		base = a.originalImage
	}
	if base == nil || a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}

	margin := a.featherSize

	// Determine the required crop rect in discBaseImage coords.
	ob := base.Bounds()
	reqX1 := clamp(a.discCenter.X-a.discRadius-margin, ob.Min.X, ob.Max.X)
	reqY1 := clamp(a.discCenter.Y-a.discRadius-margin, ob.Min.Y, ob.Max.Y)
	reqX2 := clamp(a.discCenter.X+a.discRadius+margin, ob.Min.X, ob.Max.X)
	reqY2 := clamp(a.discCenter.Y+a.discRadius+margin, ob.Min.Y, ob.Max.Y)
	reqRect := image.Rect(reqX1, reqY1, reqX2, reqY2)

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
		cropped = subImage(a.discWorkingCrop, image.Rect(wcX1, wcY1, wcX2, wcY2))
		localCenter = image.Pt(a.discCenter.X-reqX1, a.discCenter.Y-reqY1)
	} else {
		// Fall back to the full base image (e.g. discWorkingCrop not yet built).
		cropped = subImage(base, reqRect)
		localCenter = image.Pt(a.discCenter.X-reqX1, a.discCenter.Y-reqY1)
	}

	centerCutoutRadius := 0
	if a.discCenterCutout && a.discCutoutPercent > 0 {
		// Cutout radius = half the cutout diameter, which is discCutoutPercent% of the disc diameter.
		centerCutoutRadius = int(math.Round(float64(a.discRadius) * float64(a.discCutoutPercent) / 100.0))
	}
	feathered := applyCircularMaskWithFeather(cropped, localCenter, a.discRadius, a.featherSize, centerCutoutRadius, a.bgColor)

	// Re-apply accumulated rotation so that ShiftDisc / SetFeatherSize / etc.
	// don't discard a rotation the user already applied.
	if a.rotationAngle != 0 {
		feathered = rotateArbitrary(feathered, a.rotationAngle, a.bgColor)
	}

	// Re-apply any post-disc levels so that shift / rotate / feather operations
	// never silently strip a tonal adjustment the user already committed.
	if a.postDiscBlack != 0 || a.postDiscWhite != 255 {
		feathered = applyLevels(feathered, a.postDiscBlack, a.postDiscWhite)
	}

	a.warpedImage = feathered

	// Invalidate the levels baseline so the next SetLevels drag re-snapshots
	// from the freshly rendered disc image.
	a.levelsBaseImage = nil

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
}

// StraightEdgeRotateRequest carries the angle (in degrees) of the reference
// line drawn by the user in display space. The backend subtracts it from
// rotationAngle so the edge becomes perfectly horizontal.
type StraightEdgeRotateRequest struct {
	AngleDeg float64 `json:"angleDeg"`
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
	a.discCenter.X += req.DX
	a.discCenter.Y += req.DY
	a.logf("ShiftDisc: new center=(%d,%d)", a.discCenter.X, a.discCenter.Y)
	return a.redrawDisc()
}

// SetFeatherSize updates the feather radius and re-renders the disc.
func (a *App) SetFeatherSize(req FeatherSizeRequest) (*ProcessResult, error) {
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
	a.logf("SetBackgroundColor: r=%d g=%d b=%d", r, g, b)
	a.bgColor = color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
}

// ResetDisc clears the disc selection and restores the pre-disc image.
func (a *App) ResetDisc() (*ProcessResult, error) {
	a.logf("ResetDisc")
	a.cancelTouchup()
	a.discCenter = image.Point{}
	a.discRadius = 0
	a.rotationAngle = 0
	a.discBaseImage = nil
	a.discWorkingCrop = nil
	a.discWorkingCropRect = image.Rectangle{}
	a.postDiscBlack = 0
	a.postDiscWhite = 255
	a.warpedImage = nil
	a.levelsBaseImage = nil

	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview: preview,
		Message: "Disc selection reset — draw a new circle",
		Width:   b.Dx(),
		Height:  b.Dy(),
	}, nil
}
