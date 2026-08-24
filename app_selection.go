package main

import (
	"fmt"
	"image"
	"image/draw"
	"math"
)

// AdjustmentSelection is an optional image-space rectangle used to limit a
// pixel adjustment. Coordinates may arrive in either drag direction.
type AdjustmentSelection struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

type adjustmentSelectionKey struct {
	Rect   image.Rectangle
	Active bool
}

func resolveAdjustmentSelection(selection *AdjustmentSelection, bounds image.Rectangle) (image.Rectangle, adjustmentSelectionKey, error) {
	if selection == nil {
		return bounds, adjustmentSelectionKey{}, nil
	}
	values := []float64{selection.X1, selection.Y1, selection.X2, selection.Y2}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return image.Rectangle{}, adjustmentSelectionKey{}, fmt.Errorf("invalid adjustment selection")
		}
	}

	left := int(math.Floor(math.Min(selection.X1, selection.X2)))
	top := int(math.Floor(math.Min(selection.Y1, selection.Y2)))
	right := int(math.Ceil(math.Max(selection.X1, selection.X2)))
	bottom := int(math.Ceil(math.Max(selection.Y1, selection.Y2)))
	rect := image.Rect(left, top, right, bottom).Intersect(bounds)
	if rect.Empty() {
		return image.Rectangle{}, adjustmentSelectionKey{}, fmt.Errorf("adjustment selection is outside the image")
	}
	return rect, adjustmentSelectionKey{Rect: rect, Active: true}, nil
}

// CopySelectionToClipboard copies a full-resolution image-space selection
// directly to the native clipboard without modifying working image or undo
// state. No image bytes cross the Wails bridge.
func (a *App) CopySelectionToClipboard(selection AdjustmentSelection) (string, error) {
	src := a.workingImage()
	if src == nil {
		return "", fmt.Errorf("no image loaded")
	}
	rect, _, err := resolveAdjustmentSelection(&selection, src.Bounds())
	if err != nil {
		return "", err
	}

	a.clipboardMu.Lock()
	defer a.clipboardMu.Unlock()
	writer := a.clipboardWriter
	if writer == nil {
		writer = copyImageRegionToClipboard
	}
	if err := writer(src, rect); err != nil {
		return "", fmt.Errorf("copy selection: %w", err)
	}
	return fmt.Sprintf("Copied %d×%d selection to clipboard", rect.Dx(), rect.Dy()), nil
}

func applyLevelsInSelection(src *image.NRGBA, black, white int, key adjustmentSelectionKey) *image.NRGBA {
	if !key.Active {
		return applyLevels(src, black, white)
	}
	return compositeAdjustmentSelection(src, applyLevels(subImage(src, key.Rect), black, white), key.Rect)
}

func compositeAdjustmentSelection(base, adjusted *image.NRGBA, rect image.Rectangle) *image.NRGBA {
	out := cloneImage(base)
	draw.Draw(out, rect, adjusted, adjusted.Bounds().Min, draw.Src)
	return out
}

func clipAlphaToAdjustmentSelection(mask *image.Alpha, key adjustmentSelectionKey) (*image.Alpha, error) {
	if !key.Active {
		return mask, nil
	}
	rect := mask.Bounds().Intersect(key.Rect)
	if rect.Empty() {
		return nil, fmt.Errorf("touch-up stroke is outside the adjustment selection")
	}
	clipped := image.NewAlpha(rect)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			clipped.SetAlpha(x, y, mask.AlphaAt(x, y))
		}
	}
	return clipped, nil
}
