package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"atropos/internal/raster"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// LoadImageFromClipboard reads and decodes the clipboard entirely in Go, then
// replaces the current document using the same state invariants as other load
// paths. No encoded image bytes cross the Wails bridge.
func (a *App) LoadImageFromClipboard() (*ImageInfo, error) {
	if !a.loadMu.TryLock() {
		const msg = "LoadImageFromClipboard: rejected, another load is already in progress"
		a.logf(msg)
		return nil, errors.New(msg)
	}
	defer a.loadMu.Unlock()
	a.cancelTouchup()

	t0 := time.Now()
	a.clipboardMu.Lock()
	reader := a.clipboardReader
	if reader == nil {
		reader = readImageFromClipboard
	}
	nrgba, format, err := reader()
	a.clipboardMu.Unlock()
	if err != nil {
		msg := fmt.Sprintf("LoadImageFromClipboard: read error: %v", err)
		a.logf(msg)
		return nil, errors.New(msg)
	}
	if nrgba == nil || nrgba.Bounds().Empty() {
		const msg = "LoadImageFromClipboard: clipboard image is empty"
		a.logf(msg)
		return nil, errors.New(msg)
	}
	a.logf("LoadImageFromClipboard: read took %v", time.Since(t0))

	t1 := time.Now()
	a.originalImage = nrgba
	a.currentImage = raster.CloneNRGBA(nrgba)
	a.imageLoaded = true
	a.loadedFilePath = ""
	a.resetPipelineState()
	a.logf("LoadImageFromClipboard: clone took %v", time.Since(t1))

	t2 := time.Now()
	preview, err := a.imagePreviewURL(a.currentImage)
	if err != nil {
		msg := fmt.Sprintf("LoadImageFromClipboard: preview registration error: %v", err)
		a.logf(msg)
		return nil, errors.New(msg)
	}
	a.logf("LoadImageFromClipboard: preview registration took %v", time.Since(t2))

	if a.ctx != nil {
		runtime.WindowSetTitle(a.ctx, AppBaseTitle()+" — [Clipboard Data]")
	}
	b := nrgba.Bounds()
	a.logf("LoadImageFromClipboard: total %v, returning %dx%d, preview=%q", time.Since(t0), b.Dx(), b.Dy(), preview)
	return &ImageInfo{
		Width:                 b.Dx(),
		Height:                b.Dy(),
		Preview:               preview,
		Format:                strings.ToUpper(format),
		DPIX:                  0,
		DPIY:                  0,
		SuggestedCornerParams: suggestCornerParams(b.Dx(), b.Dy()),
	}, nil
}
