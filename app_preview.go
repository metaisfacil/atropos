package main

import (
	"image"
	"net/http"

	"atropos/internal/preview"
)

const previewAssetPathPrefix = preview.PathPrefix

func (a *App) imagePreviewURL(img *image.NRGBA) (string, error) {
	return a.previewAssets.Register(img)
}

// ResolvePreviewImage returns the immutable source image registered under previewURL.
func (a *App) ResolvePreviewImage(previewURL string) (*image.NRGBA, bool) {
	if a.previewAssets == nil {
		return nil, false
	}
	return a.previewAssets.Resolve(previewURL)
}

// ResolveCalibrationImage returns the immutable calibration scan registered under previewURL.
func (a *App) ResolveCalibrationImage(previewURL string) (*image.NRGBA, bool) {
	if a.calibrationPreviewAssets == nil {
		return nil, false
	}
	return a.calibrationPreviewAssets.Resolve(previewURL)
}

// RenderPreviewViewport is the Wails adapter for the internal preview store.
func (a *App) RenderPreviewViewport(request PreviewViewportRequest) (PreviewViewportResponse, error) {
	return a.renderPreviewViewport(a.previewAssets, request)
}

func (a *App) renderPreviewViewport(store *preview.Store, request PreviewViewportRequest) (PreviewViewportResponse, error) {
	result, err := store.Render(a.ctx, preview.RenderRequest{
		Preview:    request.Preview,
		X:          request.X,
		Y:          request.Y,
		Width:      request.Width,
		Height:     request.Height,
		DestWidth:  request.DestWidth,
		DestHeight: request.DestHeight,
		Quality:    request.Quality,
	})
	if err != nil {
		return PreviewViewportResponse{}, err
	}
	return PreviewViewportResponse{
		DataURL:      result.DataURL,
		X:            result.X,
		Y:            result.Y,
		Width:        result.Width,
		Height:       result.Height,
		RasterWidth:  result.RasterWidth,
		RasterHeight: result.RasterHeight,
	}, nil
}

func (a *App) servePreviewAsset(w http.ResponseWriter, r *http.Request) {
	a.previewAssets.ServeHTTP(w, r)
}
