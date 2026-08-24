package main

import (
	"image"
	"net/http"

	"atropos/internal/preview"
)

const previewAssetPathPrefix = preview.PathPrefix

// PreviewViewportRequest asks the backend for only the source rectangle needed
// by the current frontend viewport. These Wails-facing types remain in package
// main so extracting preview internals does not change the generated bridge.
type PreviewViewportRequest struct {
	Preview    string `json:"preview"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DestWidth  int    `json:"destWidth"`
	DestHeight int    `json:"destHeight"`
	Quality    int    `json:"quality"`
}

// PreviewViewportResponse carries a viewport JPEG as a data URL.
type PreviewViewportResponse struct {
	DataURL      string `json:"dataURL"`
	X            int    `json:"x"`
	Y            int    `json:"y"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	RasterWidth  int    `json:"rasterWidth"`
	RasterHeight int    `json:"rasterHeight"`
}

func (a *App) imagePreviewURL(img *image.NRGBA) (string, error) {
	return a.previewAssets.Register(img)
}

// RenderPreviewViewport is the Wails adapter for the internal preview store.
func (a *App) RenderPreviewViewport(request PreviewViewportRequest) (PreviewViewportResponse, error) {
	result, err := a.previewAssets.Render(a.ctx, preview.RenderRequest{
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
