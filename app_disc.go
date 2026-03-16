package main

import (
	"fmt"
	"image"
	"image/color"
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

// DrawDisc extracts and applies a circular mask with feathering.
func (a *App) DrawDisc(req DiscDrawRequest) (*ProcessResult, error) {
	a.logf("DrawDisc: center=(%d,%d) radius=%d", req.CenterX, req.CenterY, req.Radius)
	if a.originalImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	a.discCenter = image.Pt(req.CenterX, req.CenterY)
	a.discRadius = req.Radius
	a.rotationAngle = 0
	return a.redrawDisc()
}

// redrawDisc re-renders the disc crop using the current discCenter, discRadius,
// featherSize and bgColor. Called by DrawDisc, ShiftDisc, GetPixelColor, etc.
func (a *App) redrawDisc() (*ProcessResult, error) {
	if a.originalImage == nil || a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}
	margin := a.featherSize
	ob := a.originalImage.Bounds()
	x1 := clamp(a.discCenter.X-a.discRadius-margin, ob.Min.X, ob.Max.X)
	y1 := clamp(a.discCenter.Y-a.discRadius-margin, ob.Min.Y, ob.Max.Y)
	x2 := clamp(a.discCenter.X+a.discRadius+margin, ob.Min.X, ob.Max.X)
	y2 := clamp(a.discCenter.Y+a.discRadius+margin, ob.Min.Y, ob.Max.Y)

	cropped := subImage(a.originalImage, image.Rect(x1, y1, x2, y2))
	localCenter := image.Pt(a.discCenter.X-x1, a.discCenter.Y-y1)

	feathered := applyCircularMaskWithFeather(cropped, localCenter, a.discRadius, a.featherSize, a.bgColor)

	// Re-apply accumulated rotation so that ShiftDisc / SetFeatherSize / etc.
	// don't discard a rotation the user already applied.
	if a.rotationAngle != 0 {
		feathered = rotateArbitrary(feathered, a.rotationAngle, a.bgColor)
	}
	a.warpedImage = feathered

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
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

// GetPixelColor samples the original image at (x,y), sets bgColor, and
// re-renders the disc crop so the feathered edge matches the sampled colour.
func (a *App) GetPixelColor(req PixelColorRequest) (*ProcessResult, error) {
	a.logf("GetPixelColor: x=%d y=%d", req.X, req.Y)
	if a.originalImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	b := a.originalImage.Bounds()
	px := clamp(req.X, b.Min.X, b.Max.X-1)
	py := clamp(req.Y, b.Min.Y, b.Max.Y-1)
	c := a.originalImage.NRGBAAt(px, py)
	a.bgColor = color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255}
	a.logf("GetPixelColor: sampled (%d,%d,%d) at (%d,%d)", c.R, c.G, c.B, px, py)

	if a.discRadius > 0 {
		return a.redrawDisc()
	}
	// No disc yet — just acknowledge
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
	a.discCenter = image.Point{}
	a.discRadius = 0
	a.rotationAngle = 0
	a.warpedImage = nil

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
