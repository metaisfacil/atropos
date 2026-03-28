package main

import (
	"fmt"
	"image"
)

// Normal mode notes:
//
// Drag interaction rules (implemented in frontend/hooks/useMouseHandlers.js):
// - A drag may begin on the canvas area outside image bounds; the selection
//   starts when the cursor first enters the image (entry point becomes
//   `dragStart`).
// - While dragging, `dragCurrent` is clamped to the image boundary — the
//   rectangle tracks the cursor and extends to the edge, but never beyond.
// - A click (drag smaller than 5 px in either dimension) clears any existing
//   `normalRect` instead of creating a tiny crop.
// - `normalDragPendingRef` tracks the outside-image mousedown state;
//   `e.preventDefault()` is called on that mousedown to suppress the
//   browser's native drag gesture. When the cursor enters the image the
//   pending drag transitions to active and `normalDragActiveRef` is set true
//   synchronously so the very first `mousemove` inside the image updates the
//   selection immediately.
//
// NormalCrop (server-side pseudocode):
//   img = workingImage()
//   normalise coordinates (swap if x1>x2 or y1>y2)
//   clamp to img.Bounds()
//   if region is empty → error
//   saveUndo()
//   warpedImage = subImage(img, rect)
//   return preview + width + height + "Cropped to W×H"
//
// ResetNormal clears `warpedImage` so that GetCleanPreview returns `currentImage`.

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

	descreenReset := a.descreenResultImage != nil
	a.saveUndo()
	r := image.Rect(x1, y1, x2, y2)
	a.setWorkingImage(subImage(img, r))

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	b2 := a.warpedImage.Bounds()
	return &ProcessResult{
		Preview:       preview,
		Width:         b2.Dx(),
		Height:        b2.Dy(),
		Message:       fmt.Sprintf("Cropped to %d×%d", b2.Dx(), b2.Dy()),
		DescreenReset: descreenReset,
	}, nil
}

// ResetNormal clears warpedImage so that GetCleanPreview returns the original
// currentImage — consistent with ResetCorners / ClearLines / ResetDisc.
func (a *App) ResetNormal() (*ProcessResult, error) {
	a.logf("ResetNormal")
	descreenReset := a.descreenResultImage != nil
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
		Preview:       preview,
		Width:         b.Dx(),
		Height:        b.Dy(),
		Message:       "Normal crop reset",
		DescreenReset: descreenReset,
	}, nil
}
