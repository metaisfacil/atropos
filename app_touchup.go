package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"time"

	"atropos/internal/raster"

	"atropos/internal/cornerdetect"
	"atropos/internal/patchmatch"

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
	Points     []TouchUpPoint       `json:"points"`
	BrushSize  float64              `json:"brushSize"`
	PatchSize  int                  `json:"patchSize"`
	Iterations int                  `json:"iterations"`
	Selection  *AdjustmentSelection `json:"selection,omitempty"`
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
	return patchMatchChunkedFillLogged(ctx, src, mask, patchSize, iterations, nil)
}

// patchMatchChunkedFillLogged is the instrumented implementation used by the
// application. Keeping the logger optional leaves the state-free helper useful
// to benchmarks and tests without requiring an App.
func patchMatchChunkedFillLogged(ctx context.Context, src *image.NRGBA, mask *image.Alpha,
	patchSize, iterations int, logf func(string, ...interface{})) (*image.NRGBA, error) {

	const cropMargin = 256
	phaseStarted := time.Now()
	crops, err := patchMatchRegions(ctx, mask, cropMargin, src.Bounds())
	if err != nil {
		return nil, err
	}
	if logf != nil {
		logf("TouchUp PatchMatch: region scan found %d crop(s) in %s", len(crops), time.Since(phaseStarted))
	}

	phaseStarted = time.Now()
	if len(crops) == 0 {
		result := raster.ToNRGBA(src)
		if logf != nil {
			logf("TouchUp PatchMatch: cloned source (empty mask) in %s", time.Since(phaseStarted))
		}
		return result, nil
	}

	result := raster.ToNRGBA(src)
	if logf != nil {
		logf("TouchUp PatchMatch: cloned source in %s", time.Since(phaseStarted))
	}
	for cropIndex, crop := range crops {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Re-origin both inputs for patchmatch.Fill. The crop mask includes every
		// marked pixel in the context window, not just the seed component, so a
		// second damaged region can never be selected as valid source material.
		phaseStarted = time.Now()
		cropSrc := raster.ToNRGBA(src.SubImage(crop))
		cropMask := image.NewAlpha(image.Rect(0, 0, crop.Dx(), crop.Dy()))
		for y := crop.Min.Y; y < crop.Max.Y; y++ {
			for x := crop.Min.X; x < crop.Max.X; x++ {
				cropMask.SetAlpha(x-crop.Min.X, y-crop.Min.Y, mask.AlphaAt(x, y))
			}
		}
		if logf != nil {
			logf("TouchUp PatchMatch: crop %d/%d prepared rect=%v in %s", cropIndex+1, len(crops), crop, time.Since(phaseStarted))
		}

		phaseStarted = time.Now()
		filled, fillErr := patchmatch.Fill(ctx, cropSrc, cropMask, patchSize, iterations)
		if fillErr != nil {
			return nil, fillErr
		}
		if logf != nil {
			logf("TouchUp PatchMatch: crop %d/%d filled in %s", cropIndex+1, len(crops), time.Since(phaseStarted))
		}

		phaseStarted = time.Now()
		for y := crop.Min.Y; y < crop.Max.Y; y++ {
			for x := crop.Min.X; x < crop.Max.X; x++ {
				if mask.AlphaAt(x, y).A > 0 {
					result.SetNRGBA(x, y, filled.NRGBAAt(x-crop.Min.X, y-crop.Min.Y))
				}
			}
		}
		if logf != nil {
			logf("TouchUp PatchMatch: crop %d/%d composited in %s", cropIndex+1, len(crops), time.Since(phaseStarted))
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
			minTX, minTY = min(minTX, tx), min(minTY, ty)
			maxTX, maxTY = max(maxTX, tx), max(maxTY, ty)
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
		min(a.Min.X, b.Min.X),
		min(a.Min.Y, b.Min.Y),
		max(a.Max.X, b.Max.X),
		max(a.Max.Y, b.Max.Y),
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
	resized := cornerdetect.ResizeGray(gray, tgtBounds.Dx(), tgtBounds.Dy())
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
	started := time.Now()
	a.touchupMu.Lock()
	hasCancel := a.touchupCancel != nil
	a.touchupMu.Unlock()
	a.logf("CancelTouchup: called, hasCancel=%v", hasCancel)
	a.cancelTouchup()
	a.logf("CancelTouchup: done in %s", time.Since(started))
}

// cancelTouchup cancels any in-flight TouchUpApply operation. Safe to call
// from any goroutine. It also advances the generation so a worker that has
// already returned from its fill cannot commit stale pixels afterward.
// The return value reports whether an operation was active.
func (a *App) cancelTouchup() bool {
	a.touchupMu.Lock()
	fn := a.touchupCancel
	a.touchupCancel = nil
	a.touchupGen++
	a.touchupMu.Unlock()
	if fn != nil {
		a.logf("cancelTouchup: calling cancel()")
		fn()
		return true
	}
	return false
}

// touchUpDoneEvent is the payload sent on the "touchup-done" Wails event.
// A successful touch-up publishes a normal immutable preview revision and, for
// a reasonably small brush rectangle, a lossless patch that lets the canvas
// promote its current viewport without re-rendering all of the unchanged pixels.
type touchUpDoneEvent struct {
	Cancelled     bool                 `json:"cancelled,omitempty"`
	Error         string               `json:"error,omitempty"`
	Preview       string               `json:"preview,omitempty"`
	Patch         *touchUpPreviewPatch `json:"patch,omitempty"`
	Message       string               `json:"message,omitempty"`
	Width         int                  `json:"width,omitempty"`
	Height        int                  `json:"height,omitempty"`
	DescreenReset bool                 `json:"descreenReset,omitempty"`
}

type touchUpPreviewPatch struct {
	DataURL string `json:"dataURL"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

const touchUpPreviewPatchMaxPixels = 512 * 512

// encodeTouchUpPreviewPatch returns the exact output rectangle covering the
// non-zero mask. Oversized rectangles deliberately fall back to the normal
// immutable viewport render so one unusual stroke cannot create a huge event.
func encodeTouchUpPreviewPatch(out *image.NRGBA, mask *image.Alpha) (*touchUpPreviewPatch, error) {
	if out == nil || mask == nil {
		return nil, nil
	}
	scan := mask.Bounds().Intersect(out.Bounds())
	bounds := image.Rectangle{}
	for y := scan.Min.Y; y < scan.Max.Y; y++ {
		row := (y - mask.Rect.Min.Y) * mask.Stride
		for x := scan.Min.X; x < scan.Max.X; x++ {
			if mask.Pix[row+x-mask.Rect.Min.X] == 0 {
				continue
			}
			pixel := image.Rect(x, y, x+1, y+1)
			if bounds.Empty() {
				bounds = pixel
			} else {
				bounds = bounds.Union(pixel)
			}
		}
	}
	if bounds.Empty() {
		return nil, nil
	}
	// Keep transparent padding around the changed pixels. Without it, browser
	// filtering can clamp an edge pixel across the PNG's rectangular boundary.
	bounds = bounds.Inset(-1).Intersect(out.Bounds())
	if bounds.Dx() > touchUpPreviewPatchMaxPixels/bounds.Dy() {
		return nil, nil
	}

	patch := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			coverage := mask.AlphaAt(x, y).A
			if coverage == 0 {
				continue
			}
			pixel := out.NRGBAAt(x, y)
			// Fully covered pixels replace the preview exactly. At the brush's
			// antialiased edge, retain its fractional coverage: making a 1/255
			// edge pixel opaque exposes the quality difference between this
			// lossless patch and the JPEG viewport as a bright contour.
			pixel.A = uint8((uint16(pixel.A)*uint16(coverage) + 127) / 255)
			patch.SetNRGBA(x-bounds.Min.X, y-bounds.Min.Y, pixel)
		}
	}
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&encoded, patch); err != nil {
		return nil, err
	}
	return &touchUpPreviewPatch{
		DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes()),
		X:       bounds.Min.X,
		Y:       bounds.Min.Y,
		Width:   bounds.Dx(),
		Height:  bounds.Dy(),
	}, nil
}

// commitTouchupResult atomically verifies and records a completed touch-up.
// A result is valid only while it is still the newest registered operation and
// the working image is the exact source from which the fill was calculated.
func (a *App) commitTouchupResult(ctx context.Context, generation uint64, srcImg, out *image.NRGBA) (bool, bool) {
	a.touchupMu.Lock()
	defer a.touchupMu.Unlock()
	if ctx.Err() != nil || a.touchupGen != generation || a.workingImage() != srcImg {
		return false, false
	}
	descreenReset := a.descreenResultImage != nil
	a.saveUndo()
	a.setWorkingImage(out)
	a.touchupCancel = nil
	return true, descreenReset
}

// TouchUpApply accepts the legacy full-size PNG mask. New brush callers should
// use TouchUpApplyStrokes to avoid the full-image encode/decode path.
func (a *App) TouchUpApply(maskB64 string, patchSize int, iterations int) (*ProcessResult, error) {
	operationStarted := time.Now()
	a.logf("TouchUpApply: backend=%q patchSize=%d iterations=%d patchKernel=%s", a.touchupBackend, patchSize, iterations, patchmatch.ActiveKernel())
	if a.currentImage == nil && a.warpedImage == nil {
		a.logf("TouchUpApply: failed before launch in %s: no image loaded", time.Since(operationStarted))
		return nil, fmt.Errorf("no image loaded")
	}

	a.cancelTouchup()
	phaseStarted := time.Now()
	mask, err := a.buildMask(maskB64)
	if err != nil {
		a.logf("TouchUpApply: mask preparation failed in %s (total %s): %v", time.Since(phaseStarted), time.Since(operationStarted), err)
		return nil, err
	}
	a.logf("TouchUpApply: mask=%v bytes=%d prepared in %s (total %s)", mask.Bounds(), len(mask.Pix), time.Since(phaseStarted), time.Since(operationStarted))

	srcImg := a.workingImage()
	if srcImg == nil {
		a.logf("TouchUpApply: failed before launch in %s: no image loaded", time.Since(operationStarted))
		return nil, fmt.Errorf("no image loaded")
	}
	return a.startTouchup(srcImg, mask, patchSize, iterations, operationStarted)
}

// TouchUpApplyStrokes rasterizes a compact image-space brush stroke directly
// into a bounded mask and starts the asynchronous fill.
func (a *App) TouchUpApplyStrokes(request TouchUpStrokeRequest) (*ProcessResult, error) {
	started := time.Now()
	a.logf("TouchUpApplyStrokes: backend=%q points=%d brushSize=%.1f patchSize=%d iterations=%d patchKernel=%s",
		a.touchupBackend, len(request.Points), request.BrushSize, request.PatchSize, request.Iterations, patchmatch.ActiveKernel())
	if a.currentImage == nil && a.warpedImage == nil {
		a.logf("TouchUpApplyStrokes: failed before launch in %s: no image loaded", time.Since(started))
		return nil, fmt.Errorf("no image loaded")
	}

	// Cancel first so a prior fill cannot commit while this stroke is being
	// rasterized. The bounded rasterizer normally completes in a few milliseconds.
	a.cancelTouchup()
	srcImg := a.workingImage()
	if srcImg == nil {
		a.logf("TouchUpApplyStrokes: failed before launch in %s: no image loaded", time.Since(started))
		return nil, fmt.Errorf("no image loaded")
	}
	phaseStarted := time.Now()
	mask, err := buildStrokeMask(srcImg.Bounds(), request.Points, request.BrushSize)
	if err != nil {
		a.logf("TouchUpApplyStrokes: mask rasterization failed in %s (total %s): %v", time.Since(phaseStarted), time.Since(started), err)
		return nil, err
	}
	_, selectionKey, err := resolveAdjustmentSelection(request.Selection, srcImg.Bounds())
	if err != nil {
		a.logf("TouchUpApplyStrokes: selection resolution failed in %s (total %s): %v", time.Since(phaseStarted), time.Since(started), err)
		return nil, err
	}
	mask, err = clipAlphaToAdjustmentSelection(mask, selectionKey)
	if err != nil {
		a.logf("TouchUpApplyStrokes: mask clipping failed in %s (total %s): %v", time.Since(phaseStarted), time.Since(started), err)
		return nil, err
	}
	a.logf("TouchUpApplyStrokes: mask=%v bytes=%d prepared in %s (total %s)", mask.Bounds(), len(mask.Pix), time.Since(phaseStarted), time.Since(started))
	return a.startTouchup(srcImg, mask, request.PatchSize, request.Iterations, started)
}

// startTouchup registers cancellation, launches the fill, and returns without
// holding up Wails' IPC queue. Completion is delivered via "touchup-done".
func (a *App) startTouchup(srcImg *image.NRGBA, mask *image.Alpha, patchSize, iterations int, operationStarted time.Time) (*ProcessResult, error) {
	a.cancelTouchup()
	ctx, cancel := context.WithCancel(context.Background())
	a.touchupMu.Lock()
	a.touchupGen++
	generation := a.touchupGen
	a.touchupCancel = cancel
	a.touchupMu.Unlock()
	backend := a.touchupBackend

	go func() {
		outcome := "cancelled"
		a.logf("TouchUpApply %d: starting fill, backend=%s (setup %s)", generation, backend, time.Since(operationStarted))
		defer func() {
			cancel()
			a.touchupMu.Lock()
			if a.touchupGen == generation {
				a.touchupCancel = nil
			}
			a.touchupMu.Unlock()
			a.logf("TouchUpApply %d: finished outcome=%s backend=%s in %s", generation, outcome, backend, time.Since(operationStarted))
		}()

		emit := func(ev touchUpDoneEvent) { runtime.EventsEmit(a.ctx, "touchup-done", ev) }

		var out *image.NRGBA
		var fillErr error
		phaseStarted := time.Now()
		if backend == "iopaint" {
			out, fillErr = a.iopaintFill(ctx, srcImg, mask)
		} else {
			out, fillErr = patchMatchChunkedFillLogged(ctx, srcImg, mask, patchSize, iterations, a.logf)
		}
		a.logf("TouchUpApply %d: fill finished in %s (total %s), err=%v", generation, time.Since(phaseStarted), time.Since(operationStarted), fillErr)

		if fillErr != nil {
			if errors.Is(fillErr, context.Canceled) {
				a.logf("TouchUpApply %d: cancelled during fill (%s)", generation, backend)
				emit(touchUpDoneEvent{Cancelled: true})
				return
			}
			outcome = "error"
			emit(touchUpDoneEvent{Error: fillErr.Error()})
			return
		}

		// Guard against a reset/undo that arrived while the fill was in flight.
		if ctx.Err() != nil {
			emit(touchUpDoneEvent{Cancelled: true})
			return
		}

		// Register the completed image as a new immutable preview revision before
		// committing it. Registration retains the NRGBA pointer but performs no
		// full-frame encoding; viewport JPEGs are produced lazily by the renderer.
		// Publishing first preserves the invariant that a preview-publication
		// failure cannot leave behind an invisible committed edit. A cancelled
		// operation may leave one unreachable cache entry, which is harmless and
		// bounded by the preview revision LRU.
		phaseStarted = time.Now()
		preview, previewErr := a.imagePreviewURL(out)
		if previewErr != nil {
			outcome = "error"
			a.logf("TouchUpApply %d: preview publication failed in %s (total %s): %v", generation, time.Since(phaseStarted), time.Since(operationStarted), previewErr)
			emit(touchUpDoneEvent{Error: previewErr.Error()})
			return
		}
		a.logf("TouchUpApply %d: preview published in %s (total %s)", generation, time.Since(phaseStarted), time.Since(operationStarted))

		// Serialize the small state commit with cancellation. The generation and
		// source-pointer checks prevent a superseded worker from snapshotting a
		// newer image into undo and then overwriting it with stale output.
		phaseStarted = time.Now()
		committed, descreenReset := a.commitTouchupResult(ctx, generation, srcImg, out)
		if !committed {
			a.logf("TouchUpApply %d: commit rejected in %s (total %s)", generation, time.Since(phaseStarted), time.Since(operationStarted))
			emit(touchUpDoneEvent{Cancelled: true})
			return
		}
		a.logf("TouchUpApply %d: committed in %s (total %s)", generation, time.Since(phaseStarted), time.Since(operationStarted))

		phaseStarted = time.Now()
		patch, patchErr := encodeTouchUpPreviewPatch(out, mask)
		if patchErr != nil {
			// The immutable preview remains a complete, authoritative fallback.
			a.logf("TouchUpApply %d: preview patch failed in %s: %v", generation, time.Since(phaseStarted), patchErr)
		} else if patch != nil {
			a.logf("TouchUpApply %d: preview patch %dx%d encoded in %s, payload=%d bytes (total %s)",
				generation, patch.Width, patch.Height, time.Since(phaseStarted), len(patch.DataURL), time.Since(operationStarted))
		} else {
			a.logf("TouchUpApply %d: preview patch skipped in %s (total %s)", generation, time.Since(phaseStarted), time.Since(operationStarted))
		}

		b := out.Bounds()
		emit(touchUpDoneEvent{Preview: preview, Patch: patch, Message: "Touch-up applied.", Width: b.Dx(), Height: b.Dy(), DescreenReset: descreenReset})
		outcome = "success"
	}()

	return &ProcessResult{Message: "running"}, nil
}
