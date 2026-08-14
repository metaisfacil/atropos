package main

import (
	"fmt"
	"strings"
)

// DustRemovalRequest selects one of three dust-removal strength banks.
// DPI controls the detector's morphology and area scaling; a non-positive
// value falls back to 300 DPI.
type DustRemovalRequest struct {
	Level string  `json:"level"`
	DPI   float64 `json:"dpi"`
}

// DustRemoval applies the dust-removal process to the current working image
// as one undoable operation.
func (a *App) DustRemoval(req DustRemovalRequest) (*ProcessResult, error) {
	level := strings.ToLower(strings.TrimSpace(req.Level))
	a.logf("DustRemoval: level=%q dpi=%.2f", level, req.DPI)

	src := a.workingImage()
	if src == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	processed, repaired, usedDPI, err := applyDustRemoval(src, level, req.DPI)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	if repaired == 0 {
		preview, previewErr := a.imagePreviewURL(src)
		if previewErr != nil {
			return nil, previewErr
		}
		return &ProcessResult{
			Preview: preview,
			Message: fmt.Sprintf("No dust detected at %s strength", level),
			Width:   b.Dx(),
			Height:  b.Dy(),
		}, nil
	}

	descreenReset := a.descreenResultImage != nil
	preWarp := a.warpedImage == nil
	a.saveUndo()
	if preWarp {
		a.currentImage = processed
	} else {
		a.setWorkingImage(processed)
	}

	preview, err := a.imagePreviewURL(processed)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{
		Preview:       preview,
		Message:       fmt.Sprintf("Dust removal applied (%s, %.0f DPI, %d pixel repairs)", level, usedDPI, repaired),
		Width:         b.Dx(),
		Height:        b.Dy(),
		DescreenReset: descreenReset,
		Changed:       true,
	}, nil
}
