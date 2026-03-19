package main

import (
	"fmt"
	"image"
)

// NormalCropRequest holds the image-space bounding box for a rectangle crop.
type NormalCropRequest struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// NormalCrop crops the working image to the given rectangle.
// Coordinates are normalised (x1/y1 need not be the top-left corner) and
// clamped to the image bounds before the crop is applied.
func (a *App) NormalCrop(req NormalCropRequest) (*ProcessResult, error) {
	a.logf("NormalCrop: (%d,%d)–(%d,%d)", req.X1, req.Y1, req.X2, req.Y2)
	img := a.workingImage()
	if img == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	x1, x2 := req.X1, req.X2
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	y1, y2 := req.Y1, req.Y2
	if y1 > y2 {
		y1, y2 = y2, y1
	}

	b := img.Bounds()
	if x1 < b.Min.X {
		x1 = b.Min.X
	}
	if y1 < b.Min.Y {
		y1 = b.Min.Y
	}
	if x2 > b.Max.X {
		x2 = b.Max.X
	}
	if y2 > b.Max.Y {
		y2 = b.Max.Y
	}

	if x2 <= x1 || y2 <= y1 {
		return nil, fmt.Errorf("crop region is empty")
	}

	a.saveUndo()
	r := image.Rect(x1, y1, x2, y2)
	a.setWorkingImage(subImage(img, r))

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	b2 := a.warpedImage.Bounds()
	return &ProcessResult{
		Preview: preview,
		Width:   b2.Dx(),
		Height:  b2.Dy(),
		Message: fmt.Sprintf("Cropped to %d×%d", b2.Dx(), b2.Dy()),
	}, nil
}

// ResetNormal clears warpedImage so that GetCleanPreview returns the original
// currentImage — consistent with ResetCorners / ClearLines / ResetDisc.
func (a *App) ResetNormal() (*ProcessResult, error) {
	a.logf("ResetNormal")
	a.cancelTouchup()
	a.warpedImage = nil
	img := a.workingImage()
	if img == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	preview, err := imageToBase64(img)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	return &ProcessResult{
		Preview: preview,
		Width:   b.Dx(),
		Height:  b.Dy(),
		Message: "Normal crop reset",
	}, nil
}
