package main

import (
	"fmt"
	"strings"

	"atropos/internal/dust"
	"atropos/internal/raster"
)

// DustRemoval applies the dust-removal process to the current working image
// as one undoable operation.
func (a *App) DustRemoval(req DustRemovalRequest) (*ProcessResult, error) {
	level := strings.ToLower(strings.TrimSpace(req.Level))
	a.logf("DustRemoval: level=%q dpi=%.2f", level, req.DPI)

	src := a.workingImage()
	if src == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	selectionRect, selectionKey, err := resolveAdjustmentSelection(req.Selection, src.Bounds())
	if err != nil {
		return nil, err
	}
	if a.warpedImage != nil && a.discEditing() {
		return a.discDustRemoval(level, req.DPI, selectionRect, selectionKey)
	}
	processSource := src
	if selectionKey.Active {
		processSource = raster.CropNRGBA(src, selectionRect)
	}
	processedRegion, repaired, usedDPI, err := dust.Apply(processSource, level, req.DPI)
	if err != nil {
		return nil, err
	}
	processed := processedRegion
	if selectionKey.Active {
		processed = compositeAdjustmentSelection(src, processedRegion, selectionRect)
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
