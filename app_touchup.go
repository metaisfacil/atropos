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

// patchMatchChunkedFill splits distant mask regions into independent jobs,
// crops each to its bounding box (+ margin), then composites only filled mask
// pixels back into a full-size clone of src.
// A larger margin than IOPaint (256 px vs 128 px) is used so PatchMatch has
// sufficient unmasked context from which to draw source patches.
func patchMatchChunkedFill(ctx context.Context, src *image.NRGBA, mask *image.Alpha,
	patchSize, iterations int) (*image.NRGBA, error) {

	const cropMargin = 256
	crops, err := patchMatchRegions(ctx, mask, cropMargin, src.Bounds())
	if err != nil {
		return nil, err
	}
	if len(crops) == 0 {
		return toNRGBA(src), nil
	}

	result := toNRGBA(src)
	for _, crop := range crops {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Re-origin both inputs for PatchMatchFill. The crop mask includes every
		// marked pixel in the context window, not just the seed component, so a
		// second damaged region can never be selected as valid source material.
		cropSrc := toNRGBA(src.SubImage(crop))
		cropMask := image.NewAlpha(image.Rect(0, 0, crop.Dx(), crop.Dy()))
		for y := crop.Min.Y; y < crop.Max.Y; y++ {
			for x := crop.Min.X; x < crop.Max.X; x++ {
				cropMask.SetAlpha(x-crop.Min.X, y-crop.Min.Y, mask.AlphaAt(x, y))
			}
		}

		filled, fillErr := PatchMatchFill(ctx, cropSrc, cropMask, patchSize, iterations)
		if fillErr != nil {
			return nil, fillErr
		}

		for y := crop.Min.Y; y < crop.Max.Y; y++ {
			for x := crop.Min.X; x < crop.Max.X; x++ {
				if mask.AlphaAt(x, y).A > 0 {
					result.SetNRGBA(x, y, filled.NRGBAAt(x-crop.Min.X, y-crop.Min.Y))
				}
			}
		}
	}
	return result, nil
}

// patchMatchRegions finds connected groups on a small occupancy grid. A
// brush-width tile is precise enough for job scheduling while avoiding a
// full-image component-label allocation. Expanded regions are merged whenever
// their source-context windows overlap.
func patchMatchRegions(ctx context.Context, mask *image.Alpha, margin int, bounds image.Rectangle) ([]image.Rectangle, error) {
	if mask == nil || bounds.Empty() {
		return nil, nil
	}
	const tileSize = 32
	tilesX := (bounds.Dx() + tileSize - 1) / tileSize
	tilesY := (bounds.Dy() + tileSize - 1) / tileSize
	occupied := make([]bool, tilesX*tilesY)

	scan := mask.Bounds().Intersect(bounds)
	for y := scan.Min.Y; y < scan.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := scan.Min.X; x < scan.Max.X; x++ {
			if mask.AlphaAt(x, y).A != 0 {
				tx := (x - bounds.Min.X) / tileSize
				ty := (y - bounds.Min.Y) / tileSize
				occupied[ty*tilesX+tx] = true
			}
		}
	}

	visited := make([]bool, len(occupied))
	queue := make([]int, 0, 64)
	var regions []image.Rectangle
	for start, present := range occupied {
		if !present || visited[start] {
			continue
		}
		visited[start] = true
		queue = append(queue[:0], start)
		minTX, minTY := start%tilesX, start/tilesX
		maxTX, maxTY := minTX, minTY

		for len(queue) > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			id := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			tx, ty := id%tilesX, id/tilesX
			minTX, minTY = minInt(minTX, tx), minInt(minTY, ty)
			maxTX, maxTY = maxInt(maxTX, tx), maxInt(maxTY, ty)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := tx+dx, ty+dy
					if nx < 0 || ny < 0 || nx >= tilesX || ny >= tilesY {
						continue
					}
					next := ny*tilesX + nx
					if occupied[next] && !visited[next] {
						visited[next] = true
						queue = append(queue, next)
					}
				}
			}
		}

		region := image.Rect(
			bounds.Min.X+minTX*tileSize-margin,
			bounds.Min.Y+minTY*tileSize-margin,
			bounds.Min.X+(maxTX+1)*tileSize+margin,
			bounds.Min.Y+(maxTY+1)*tileSize+margin,
		).Intersect(bounds)

		// Merge transitively so no masked pixels in an overlapping source
		// window are accidentally solved as separate jobs.
		for i := 0; i < len(regions); {
			if rectanglesOverlap(region, regions[i]) {
				region = unionRectangle(region, regions[i])
				regions = append(regions[:i], regions[i+1:]...)
				i = 0
				continue
			}
			i++
		}
		regions = append(regions, region)
	}
	return regions, nil
}

func rectanglesOverlap(a, b image.Rectangle) bool {
	return a.Min.X < b.Max.X && b.Min.X < a.Max.X &&
		a.Min.Y < b.Max.Y && b.Min.Y < a.Max.Y
}

func unionRectangle(a, b image.Rectangle) image.Rectangle {
	return image.Rect(
		minInt(a.Min.X, b.Min.X),
		minInt(a.Min.Y, b.Min.Y),
		maxInt(a.Max.X, b.Max.X),
		maxInt(a.Max.Y, b.Max.Y),
	)
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
	Cancelled     bool   `json:"cancelled,omitempty"`
	Error         string `json:"error,omitempty"`
	Preview       string `json:"preview,omitempty"`
	Message       string `json:"message,omitempty"`
	Width         int    `json:"width,omitempty"`
	Height        int    `json:"height,omitempty"`
	DescreenReset bool   `json:"descreenReset,omitempty"`
}

// TouchUpApply builds the mask synchronously (fast), then launches a goroutine
// for the slow fill and returns immediately. This keeps the Wails IPC queue free
// so that CancelTouchup() can interrupt the in-flight operation at any time.
// The fill result is delivered via the "touchup-done" event.
func (a *App) TouchUpApply(maskB64 string, patchSize int, iterations int) (*ProcessResult, error) {
	a.logf("TouchUpApply: backend=%q patchSize=%d iterations=%d patchKernel=%s", a.touchupBackend, patchSize, iterations, pmActivePatchKernel())
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

		descreenReset := a.descreenResultImage != nil
		a.saveUndo()
		a.setWorkingImage(out)

		preview, encErr := imageToBase64(out)
		if encErr != nil {
			emit(touchUpDoneEvent{Error: encErr.Error()})
			return
		}
		b := out.Bounds()
		emit(touchUpDoneEvent{Preview: preview, Message: "Touch-up applied.", Width: b.Dx(), Height: b.Dy(), DescreenReset: descreenReset})
	}()

	return &ProcessResult{Message: "running"}, nil
}
