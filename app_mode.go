package main

import "fmt"

// GetCleanPreview returns the current working image with no mode-specific
// overlay (no corner dots, no line guides). Called by the frontend whenever
// the user switches between modes so that residual decorations from the
// departing mode do not bleed into the new one.
//
// It also clears detectedCorners and selectedCorners on the backend so that
// corner dots cannot reappear if the user returns to Corner mode without
// running detection again.
func (a *App) GetCleanPreview() (*ProcessResult, error) {
	a.logf("GetCleanPreview")

	// Clear in-progress selections; detected corners are preserved so the
	// frontend can restore the overlay when the user returns to corner mode.
	a.selectedCorners = nil

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
		Message: "Ready",
	}, nil
}
