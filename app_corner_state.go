package main

import (
	"context"
	"fmt"
	"image"
)

// Only the newest detector owns the cancellation handle and may publish.
func (a *App) beginCornerDetection() (context.Context, uint64, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	a.cornerDetectMu.Lock()
	previous := a.cornerDetectCancel
	a.cornerDetectGen++
	generation := a.cornerDetectGen
	a.cornerDetectCancel = cancel
	a.cornerDetectMu.Unlock()
	if previous != nil {
		previous()
	}
	return ctx, generation, func() {
		cancel()
		a.cornerDetectMu.Lock()
		if a.cornerDetectGen == generation {
			a.cornerDetectCancel = nil
		}
		a.cornerDetectMu.Unlock()
	}
}

func (a *App) finishCornerDetection(ctx context.Context, generation uint64, source *image.NRGBA, corners []image.Point) (*ProcessResult, error) {
	a.cornerDetectMu.Lock()
	defer a.cornerDetectMu.Unlock()
	if ctx.Err() != nil || generation != a.cornerDetectGen || source != a.currentImage {
		return nil, context.Canceled
	}
	preview, err := a.imagePreviewURL(source)
	if err != nil {
		return nil, err
	}
	a.detectedCorners = append([]image.Point(nil), corners...)
	b := source.Bounds()
	return &ProcessResult{Preview: preview, Width: b.Dx(), Height: b.Dy(),
		Message: fmt.Sprintf("Detected %d corners", len(corners)), Corners: a.detectedCorners}, nil
}
