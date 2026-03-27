package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// patchMatchChunkedFill is like PatchMatchFill but crops to the bounding box
// of the mask (+ margin) before calling PatchMatchFill, then composites only
// the filled masked pixels back into a full-size clone of src.
// A larger margin than IOPaint (256 px vs 128 px) is used so PatchMatch has
// sufficient unmasked context from which to draw source patches.
func patchMatchChunkedFill(ctx context.Context, src *image.NRGBA, mask *image.Alpha,
	patchSize, iterations int) (*image.NRGBA, error) {

	const cropMargin = 256
	crop, hasMask := maskBoundingBox(mask, cropMargin, src.Bounds())
	if !hasMask {
		return toNRGBA(src), nil
	}

	// Re-origin the sub-image so PatchMatchFill's (y*w+x)*4 indexing works.
	cropSrc := toNRGBA(src.SubImage(crop))

	// Translate mask to the same (0,0)-origin coordinate space.
	cropMask := image.NewAlpha(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for y := crop.Min.Y; y < crop.Max.Y; y++ {
		for x := crop.Min.X; x < crop.Max.X; x++ {
			cropMask.SetAlpha(x-crop.Min.X, y-crop.Min.Y, mask.AlphaAt(x, y))
		}
	}

	filled, err := PatchMatchFill(ctx, cropSrc, cropMask, patchSize, iterations)
	if err != nil {
		return nil, err
	}

	// Composite filled pixels back into a full-size clone of src.
	result := toNRGBA(src)
	for y := crop.Min.Y; y < crop.Max.Y; y++ {
		for x := crop.Min.X; x < crop.Max.X; x++ {
			if mask.AlphaAt(x, y).A > 0 {
				result.SetNRGBA(x, y, filled.NRGBAAt(x-crop.Min.X, y-crop.Min.Y))
			}
		}
	}
	return result, nil
}

// buildMask decodes a base64-encoded PNG mask (white/opaque = fill region) and
// returns an *image.Alpha sized to match the current working image.
func (a *App) buildMask(maskB64 string) (*image.Alpha, error) {
	data, err := base64.StdEncoding.DecodeString(maskB64)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	b := img.Bounds()
	mask := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			aVal := c.A
			if aVal == 0 {
				// No alpha channel: use luminance threshold.
				lum := (299*uint32(c.R) + 587*uint32(c.G) + 114*uint32(c.B)) / 1000
				if lum > 10 {
					aVal = 255
				}
			}
			mask.Pix[(y-b.Min.Y)*mask.Stride+(x-b.Min.X)] = aVal
		}
	}

	srcImg := a.workingImage()
	if srcImg == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	tgtBounds := srcImg.Bounds()
	if mask.Bounds().Eq(tgtBounds) {
		return mask, nil
	}

	// Resize mask to working image dimensions.
	gray := image.NewGray(mask.Bounds())
	for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
		for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
			v := mask.Pix[(y-mask.Bounds().Min.Y)*mask.Stride+(x-mask.Bounds().Min.X)]
			gray.Pix[(y-mask.Bounds().Min.Y)*gray.Stride+(x-mask.Bounds().Min.X)] = v
		}
	}
	resized := resizeGray(gray, tgtBounds.Dx(), tgtBounds.Dy())
	newMask := image.NewAlpha(tgtBounds)
	for y := 0; y < tgtBounds.Dy(); y++ {
		for x := 0; x < tgtBounds.Dx(); x++ {
			newMask.Pix[y*newMask.Stride+x] = resized.Pix[y*resized.Stride+x]
		}
	}
	return newMask, nil
}

// CancelTouchup is the Wails-bound counterpart of cancelTouchup. The frontend
// calls this before issuing any reset/load IPC call so the cancellation signal
// is processed by Wails as a separate, near-instantaneous call that arrives
// before the queue drains into the (now-cancelled) TouchUpApply.
func (a *App) CancelTouchup() {
	a.touchupMu.Lock()
	hasCancel := a.touchupCancel != nil
	a.touchupMu.Unlock()
	a.logf("CancelTouchup: called, hasCancel=%v", hasCancel)
	a.cancelTouchup()
	a.logf("CancelTouchup: done")
}

// cancelTouchup cancels any in-flight TouchUpApply operation. Safe to call
// from any goroutine. No-op when no operation is running.
func (a *App) cancelTouchup() {
	a.touchupMu.Lock()
	fn := a.touchupCancel
	a.touchupCancel = nil
	a.touchupMu.Unlock()
	if fn != nil {
		a.logf("cancelTouchup: calling cancel()")
		fn()
	}
}

// touchUpDoneEvent is the payload sent on the "touchup-done" Wails event.
type touchUpDoneEvent struct {
	Cancelled bool   `json:"cancelled,omitempty"`
	Error     string `json:"error,omitempty"`
	Preview   string `json:"preview,omitempty"`
	Message   string `json:"message,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

// TouchUpApply builds the mask synchronously (fast), then launches a goroutine
// for the slow fill and returns immediately. This keeps the Wails IPC queue free
// so that CancelTouchup() can interrupt the in-flight operation at any time.
// The fill result is delivered via the "touchup-done" event.
func (a *App) TouchUpApply(maskB64 string, patchSize int, iterations int) (*ProcessResult, error) {
	a.logf("TouchUpApply: backend=%q patchSize=%d iterations=%d", a.touchupBackend, patchSize, iterations)
	if a.currentImage == nil && a.warpedImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	// Cancel any previous in-flight operation, then register this one.
	a.cancelTouchup()
	ctx, cancel := context.WithCancel(context.Background())
	a.touchupMu.Lock()
	a.touchupCancel = cancel
	a.touchupMu.Unlock()

	mask, err := a.buildMask(maskB64)
	if err != nil {
		cancel()
		a.touchupMu.Lock()
		a.touchupCancel = nil
		a.touchupMu.Unlock()
		return nil, err
	}

	srcImg := a.workingImage()
	if srcImg == nil {
		cancel()
		a.touchupMu.Lock()
		a.touchupCancel = nil
		a.touchupMu.Unlock()
		return nil, fmt.Errorf("no image loaded")
	}

	go func() {
		a.logf("TouchUpApply goroutine: starting fill, backend=%s", a.touchupBackend)
		defer func() {
			cancel()
			a.touchupMu.Lock()
			a.touchupCancel = nil
			a.touchupMu.Unlock()
			a.logf("TouchUpApply goroutine: exited")
		}()

		emit := func(ev touchUpDoneEvent) { runtime.EventsEmit(a.ctx, "touchup-done", ev) }

		var out *image.NRGBA
		var fillErr error
		if a.touchupBackend == "iopaint" {
			out, fillErr = a.iopaintFill(ctx, srcImg, mask)
		} else {
			out, fillErr = patchMatchChunkedFill(ctx, srcImg, mask, patchSize, iterations)
		}
		a.logf("TouchUpApply goroutine: fill returned, err=%v", fillErr)

		if fillErr != nil {
			if errors.Is(fillErr, context.Canceled) {
				a.logf("TouchUpApply: cancelled (%s)", a.touchupBackend)
				emit(touchUpDoneEvent{Cancelled: true})
				return
			}
			emit(touchUpDoneEvent{Error: fillErr.Error()})
			return
		}

		// Guard against a reset that arrived while the fill was in flight.
		if ctx.Err() != nil {
			emit(touchUpDoneEvent{Cancelled: true})
			return
		}

		a.saveUndo()
		a.setWorkingImage(out)

		preview, encErr := imageToBase64(out)
		if encErr != nil {
			emit(touchUpDoneEvent{Error: encErr.Error()})
			return
		}
		b := out.Bounds()
		emit(touchUpDoneEvent{Preview: preview, Message: "Touch-up applied.", Width: b.Dx(), Height: b.Dy()})
	}()

	return &ProcessResult{Message: "running"}, nil
}
