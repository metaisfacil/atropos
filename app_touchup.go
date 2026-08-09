package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TouchUpPoint is an image-space point in a touch-up brush stroke.
type TouchUpPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// TouchUpStrokeRequest is the compact brush representation sent by the
// frontend. Keeping the stroke as points avoids constructing, PNG-encoding,
// base64-encoding, transferring, decoding, and rescanning a full-size mask.
type TouchUpStrokeRequest struct {
	Points     []TouchUpPoint `json:"points"`
	BrushSize  float64        `json:"brushSize"`
	PatchSize  int            `json:"patchSize"`
	Iterations int            `json:"iterations"`
}

// buildStrokeMask rasterizes round brush segments into an Alpha image whose
// bounds are limited to the painted area but remain in source-image coordinates.
func buildStrokeMask(bounds image.Rectangle, points []TouchUpPoint, brushSize float64) (*image.Alpha, error) {
	if bounds.Empty() {
		return nil, fmt.Errorf("no image loaded")
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("touch-up stroke is empty")
	}
	if len(points) > 1_000_000 {
		return nil, fmt.Errorf("touch-up stroke has too many points")
	}
	if brushSize <= 0 || math.IsNaN(brushSize) || math.IsInf(brushSize, 0) {
		return nil, fmt.Errorf("invalid touch-up brush size")
	}

	minX, minY := points[0].X, points[0].Y
	maxX, maxY := minX, minY
	for _, p := range points {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
			return nil, fmt.Errorf("invalid touch-up stroke point")
		}
		minX, minY = math.Min(minX, p.X), math.Min(minY, p.Y)
		maxX, maxY = math.Max(maxX, p.X), math.Max(maxY, p.Y)
	}

	radius := brushSize / 2
	pad := radius + 1
	maskBounds := image.Rect(
		int(math.Floor(minX-pad)),
		int(math.Floor(minY-pad)),
		int(math.Ceil(maxX+pad)),
		int(math.Ceil(maxY+pad)),
	).Intersect(bounds)
	if maskBounds.Empty() {
		return nil, fmt.Errorf("touch-up stroke is outside the image")
	}

	mask := image.NewAlpha(maskBounds)
	if len(points) == 1 {
		paintStrokeSegment(mask, points[0], points[0], radius)
	} else {
		for i := 1; i < len(points); i++ {
			paintStrokeSegment(mask, points[i-1], points[i], radius)
		}
	}
	return mask, nil
}

// paintStrokeSegment paints one antialiased round-ended segment, taking the
// maximum coverage so overlapping segments behave like a canvas brush stroke.
func paintStrokeSegment(mask *image.Alpha, a, b TouchUpPoint, radius float64) {
	outer := radius + 0.5
	inner := math.Max(0, radius-0.5)
	segmentBounds := image.Rect(
		int(math.Floor(math.Min(a.X, b.X)-outer)),
		int(math.Floor(math.Min(a.Y, b.Y)-outer)),
		int(math.Ceil(math.Max(a.X, b.X)+outer)),
		int(math.Ceil(math.Max(a.Y, b.Y)+outer)),
	).Intersect(mask.Bounds())
	if segmentBounds.Empty() {
		return
	}

	dx, dy := b.X-a.X, b.Y-a.Y
	lengthSquared := dx*dx + dy*dy
	innerSquared := inner * inner
	outerSquared := outer * outer
	for y := segmentBounds.Min.Y; y < segmentBounds.Max.Y; y++ {
		py := float64(y) + 0.5
		row := (y - mask.Rect.Min.Y) * mask.Stride
		for x := segmentBounds.Min.X; x < segmentBounds.Max.X; x++ {
			px := float64(x) + 0.5
			t := 0.0
			if lengthSquared > 0 {
				t = ((px-a.X)*dx + (py-a.Y)*dy) / lengthSquared
				t = math.Max(0, math.Min(1, t))
			}
			qx, qy := a.X+t*dx, a.Y+t*dy
			distX, distY := px-qx, py-qy
			distanceSquared := distX*distX + distY*distY
			if distanceSquared >= outerSquared {
				continue
			}

			alpha := uint8(255)
			if distanceSquared > innerSquared {
				coverage := outer - math.Sqrt(distanceSquared)
				alpha = uint8(math.Round(math.Max(0, math.Min(1, coverage)) * 255))
			}
			index := row + x - mask.Rect.Min.X
			if alpha > mask.Pix[index] {
				mask.Pix[index] = alpha
			}
		}
	}
}

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

// TouchUpApply accepts the legacy full-size PNG mask. New brush callers should
// use TouchUpApplyStrokes to avoid the full-image encode/decode path.
func (a *App) TouchUpApply(maskB64 string, patchSize int, iterations int) (*ProcessResult, error) {
	a.logf("TouchUpApply: backend=%q patchSize=%d iterations=%d patchKernel=%s", a.touchupBackend, patchSize, iterations, pmActivePatchKernel())
	if a.currentImage == nil && a.warpedImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	a.cancelTouchup()
	mask, err := a.buildMask(maskB64)
	if err != nil {
		return nil, err
	}

	srcImg := a.workingImage()
	if srcImg == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	return a.startTouchup(srcImg, mask, patchSize, iterations)
}

// TouchUpApplyStrokes rasterizes a compact image-space brush stroke directly
// into a bounded mask and starts the asynchronous fill.
func (a *App) TouchUpApplyStrokes(request TouchUpStrokeRequest) (*ProcessResult, error) {
	started := time.Now()
	a.logf("TouchUpApplyStrokes: backend=%q points=%d brushSize=%.1f patchSize=%d iterations=%d patchKernel=%s",
		a.touchupBackend, len(request.Points), request.BrushSize, request.PatchSize, request.Iterations, pmActivePatchKernel())
	if a.currentImage == nil && a.warpedImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}

	// Cancel first so a prior fill cannot commit while this stroke is being
	// rasterized. The bounded rasterizer normally completes in a few milliseconds.
	a.cancelTouchup()
	srcImg := a.workingImage()
	if srcImg == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	mask, err := buildStrokeMask(srcImg.Bounds(), request.Points, request.BrushSize)
	if err != nil {
		return nil, err
	}
	a.logf("TouchUpApplyStrokes: mask=%v bytes=%d rasterize=%s", mask.Bounds(), len(mask.Pix), time.Since(started))
	return a.startTouchup(srcImg, mask, request.PatchSize, request.Iterations)
}

// startTouchup registers cancellation, launches the fill, and returns without
// holding up Wails' IPC queue. Completion is delivered via "touchup-done".
func (a *App) startTouchup(srcImg *image.NRGBA, mask *image.Alpha, patchSize, iterations int) (*ProcessResult, error) {
	a.cancelTouchup()
	ctx, cancel := context.WithCancel(context.Background())
	a.touchupMu.Lock()
	a.touchupCancel = cancel
	a.touchupMu.Unlock()

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
