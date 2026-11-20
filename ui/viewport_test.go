package ui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

func TestFitWidthForAndMaxUsefulZoom(t *testing.T) {
	// Wide image constrained by container width
	fw := fitWidthFor(800, 600, 2000, 1000)
	if math.Abs(fw-800) > 1e-6 {
		t.Fatalf("fitWidthFor wide = %v, want 800", fw)
	}
	// Tall image constrained by height * aspect ratio
	fwTall := fitWidthFor(800, 600, 1000, 2000)
	if math.Abs(fwTall-300) > 1e-6 {
		t.Fatalf("fitWidthFor tall = %v, want 300", fwTall)
	}

	if z := maxUsefulZoom(1000, 800); math.Abs(z-5.0) > 1e-6 {
		t.Fatalf("maxUsefulZoom small = %v, want 5", z)
	}
	if z := maxUsefulZoom(8000, 800); math.Abs(z-20.0) > 1e-6 {
		t.Fatalf("maxUsefulZoom large = %v, want 20", z)
	}
	if z := maxUsefulZoom(40000, 800); math.Abs(z-40.0) > 1e-6 {
		t.Fatalf("maxUsefulZoom clamped = %v, want 40", z)
	}
}

func TestOptimisticCropDimensionsAndSourceRect(t *testing.T) {
	out, ok := optimisticCropDimensions(image.Pt(100, 80), "left", 10)
	if !ok || out != image.Pt(90, 80) {
		t.Fatalf("left crop dims = (%v, %v), want (90,80)", out, ok)
	}
	out, ok = optimisticCropDimensions(image.Pt(100, 80), "bottom", 95)
	if !ok || out != image.Pt(100, 1) {
		t.Fatalf("clamped bottom crop dims = (%v, %v), want (100,1)", out, ok)
	}
	if _, ok := optimisticCropDimensions(image.Pt(100, 80), "diag", 5); ok {
		t.Fatal("expected invalid direction to return ok=false")
	}

	srcRect, ok := optimisticCropSourceRect(&optimisticCropState{
		SourceDims: image.Pt(100, 80),
		TargetDims: image.Pt(90, 80),
		Direction:  "left",
		Amount:     10,
	})
	if !ok || srcRect != image.Rect(10, 0, 100, 80) {
		t.Fatalf("left crop source rect = %v, want (10,0)-(100,80)", srcRect)
	}

	srcRect, ok = optimisticCropSourceRect(&optimisticCropState{
		SourceDims: image.Pt(100, 80),
		TargetDims: image.Pt(100, 72),
		Direction:  "bottom",
		Amount:     8,
	})
	if !ok || srcRect != image.Rect(0, 0, 100, 72) {
		t.Fatalf("bottom crop source rect = %v, want (0,0)-(100,72)", srcRect)
	}
}

func TestComputeDiscShift(t *testing.T) {
	// No rotation: dragging +20px screen when scale is 2x shifts disc center by -40
	res, ok := computeDiscShift(20, 10, image.Pt(1000, 1000), 300, 0, image.Pt(500, 500), image.Pt(500, 500), 500, 500, image.Pt(1000, 1000))
	if !ok {
		t.Fatal("computeDiscShift unrotated failed")
	}
	if res.dx != -40 || res.dy != -20 {
		t.Fatalf("unrotated shift = (%d, %d), want (-40, -20)", res.dx, res.dy)
	}

	// 90° clockwise disc rotation: +screenX corresponds to +sourceY after inverse rotation
	res90, ok := computeDiscShift(20, 0, image.Pt(1000, 1000), 300, 90, image.Pt(500, 500), image.Pt(500, 500), 500, 500, image.Pt(1000, 1000))
	if !ok {
		t.Fatal("computeDiscShift 90° failed")
	}
	if res90.dx != 0 || res90.dy != 40 {
		t.Fatalf("90deg shift = (%d, %d), want (0, 40)", res90.dx, res90.dy)
	}
}

func TestLockSquareDragAndRectHandles(t *testing.T) {
	sq := lockSquareDrag(f32.Pt(10, 10), f32.Pt(70, 35), 200, 200)
	if math.Abs(float64(sq.X-sq.Y)) > 1e-4 {
		t.Fatalf("lockSquareDrag produced non-square endpoint %v", sq)
	}

	r := rectI{X1: 20, Y1: 30, X2: 120, Y2: 150}
	info := viewportLayoutInfo{StageWidth: 200, StageHeight: 200}
	dims := image.Pt(200, 200)
	if h := hitTestRectHandle(&r, f32.Pt(120, 150), info, dims, 10, 6); h != "se" {
		t.Fatalf("hitTestRectHandle(se) = %q, want se", h)
	}
	if h := hitTestRectHandle(&r, f32.Pt(70, 30), info, dims, 10, 6); h != "n" {
		t.Fatalf("hitTestRectHandle(n) = %q, want n", h)
	}

	updated := updateRectFromHandle(&r, "nw", image.Pt(5, 10), dims)
	if updated.X1 != 5 || updated.Y1 != 10 || updated.X2 != 120 || updated.Y2 != 150 {
		t.Fatalf("updateRectFromHandle(nw) = %+v", updated)
	}

	lines := []lineI{{X1: 10, Y1: 10, X2: 100, Y2: 20}}
	lh := hitTestLineHandle(lines, f32.Pt(100, 20), info, dims, 10)
	if lh == nil || lh.index != 0 || lh.end != "end" {
		t.Fatalf("hitTestLineHandle = %+v, want index=0 end=end", lh)
	}
}

func TestDownsampleViewportRegion(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 100, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 100; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 2), G: uint8(y * 3), B: 120, A: 255})
		}
	}
	out := downsampleViewportRegion(src, image.Rect(10, 10, 90, 70), 40, 30)
	if out == nil || out.Bounds().Dx() != 40 || out.Bounds().Dy() != 30 {
		t.Fatalf("unexpected downsampled dims: %v", out.Bounds())
	}
	c := out.NRGBAAt(20, 15)
	if c.A != 255 || c.B != 120 {
		t.Fatalf("unexpected sampled pixel color: %+v", c)
	}
}

func TestOverlayLabelPillsStayCompactUnderTightConstraints(t *testing.T) {
	th := newAtroposTheme()
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(1000, 800)),
		Metric:      unit.Metric{PxPerDp: 1.25, PxPerSp: 1.25},
	}
	info := viewportLayoutInfo{
		ViewportW:   1000,
		ViewportH:   800,
		StageX:      100,
		StageY:      50,
		StageWidth:  800,
		StageHeight: 700,
	}

	readout := drawBrushSizeReadout(gtx, th, "38 px", f32.Pt(400, 300), 24, info)
	if readout.Dx() <= 0 || readout.Dx() >= 120 || readout.Dy() <= 0 || readout.Dy() >= 40 {
		t.Fatalf("drawBrushSizeReadout size = %dx%d (rect %v), want compact pill (<120x40)", readout.Dx(), readout.Dy(), readout)
	}

	// Zoomed in with stage extending past all viewport edges and brush near the right edge:
	// the pill must flip to the left of the brush and stay inside [0, 1000] x [0, 800].
	zoomedInfo := viewportLayoutInfo{
		ViewportW:   1000,
		ViewportH:   800,
		StageX:      -1200,
		StageY:      -900,
		StageWidth:  3500,
		StageHeight: 3500,
	}
	flipped := drawBrushSizeReadout(gtx, th, "38 px", f32.Pt(960, 400), 24, zoomedInfo)
	if flipped.Dx() <= 0 || flipped.Dx() >= 120 || flipped.Dy() <= 0 || flipped.Dy() >= 40 {
		t.Fatalf("zoomed drawBrushSizeReadout size = %dx%d (rect %v), want compact pill", flipped.Dx(), flipped.Dy(), flipped)
	}
	if !flipped.In(image.Rect(0, 0, 1000, 800)) {
		t.Fatalf("zoomed drawBrushSizeReadout rect = %v escaped viewport bounds", flipped)
	}
	if flipped.Max.X > 960 {
		t.Fatalf("expected readout near right edge to flip left of cursor x=960, got rect=%v", flipped)
	}

	cornerPill := drawCornerLabelPill(gtx, th, "TL", f32.Pt(300, 250))
	if cornerPill.Dx() <= 0 || cornerPill.Dx() >= 80 || cornerPill.Dy() <= 0 || cornerPill.Dy() >= 40 {
		t.Fatalf("drawCornerLabelPill size = %dx%d (rect %v), want compact pill (<80x40)", cornerPill.Dx(), cornerPill.Dy(), cornerPill)
	}
}

func makeTestCornerPoints(n int, w, h int) []image.Point {
	pts := make([]image.Point, 0, n)
	cols := 40
	rows := (n + cols - 1) / cols
	for i := 0; i < n; i++ {
		c := i % cols
		r := i / cols
		x := 80 + ((w-160)*c)/max(1, cols-1)
		y := 80 + ((h-160)*r)/max(1, rows-1)
		pts = append(pts, image.Pt(x, y))
	}
	return pts
}

func TestCornerOverlayCacheHitPanAndInvalidation(t *testing.T) {
	th := newAtroposTheme()
	v := newViewportState()
	dims := image.Pt(5100, 7020)
	pts := makeTestCornerPoints(1000, dims.X, dims.Y)
	// Place one isolated point far from the grid so we can verify its exact premultiplied pixel values.
	pts[0] = image.Pt(2550, 3510)

	info := viewportLayoutInfo{
		ViewportW:   1101,
		ViewportH:   1031,
		StageX:      176,
		StageY:      0,
		StageWidth:  749,
		StageHeight: 1031,
	}
	visual := viewportVisualState{
		Mode:              "corner",
		RealImageDims:     dims,
		DetectedCornerPts: pts,
		DotRadius:         18,
	}

	var ops op.Ops
	drawFrame := func(inf viewportLayoutInfo, vis viewportVisualState) {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Constraints: layout.Exact(image.Pt(inf.ViewportW, inf.ViewportH)),
			Metric:      unit.Metric{PxPerDp: 1.25, PxPerSp: 1.25},
		}
		drawCanvasGuides(gtx, th, vis, inf, v, nil)
	}

	// 1. Initial build.
	drawFrame(info, visual)
	if v.cornerOverlay == nil || !v.cornerOverlay.valid || v.cornerOverlay.rebuilds != 1 {
		t.Fatalf("expected 1 overlay rebuild after first frame, got %+v", v.cornerOverlay)
	}

	// Verify pixel colors for isolated dot at (2550, 3510): red premultiplied fill/stroke and transparent background.
	oc := v.cornerOverlay
	buf := oc.bufs[oc.slot]
	cx := int(math.Round(float64(pts[0].X)*(info.StageWidth/float64(dims.X)))) - oc.coverRect.Min.X
	cy := int(math.Round(float64(pts[0].Y)*(info.StageHeight/float64(dims.Y)))) - oc.coverRect.Min.Y
	centerPx := buf.RGBAAt(cx, cy)
	if centerPx.A == 0 || centerPx.R != centerPx.A || centerPx.G != 0 || centerPx.B != 0 {
		t.Fatalf("unexpected dot center RGBA = %+v, want premultiplied red", centerPx)
	}
	farPx := buf.RGBAAt(0, 0)
	if farPx != (color.RGBA{}) {
		t.Fatalf("expected transparent background corner pixel, got %+v", farPx)
	}

	// 2. Identical frame hits cache.
	drawFrame(info, visual)
	if v.cornerOverlay.rebuilds != 1 {
		t.Fatalf("expected cache hit on identical frame, got rebuilds=%d", v.cornerOverlay.rebuilds)
	}

	// 3. Panning fit-to-screen stage does not rebuild.
	pannedFit := info
	pannedFit.StageX = 120
	drawFrame(pannedFit, visual)
	if v.cornerOverlay.rebuilds != 1 {
		t.Fatalf("expected 0 rebuilds on fit stage pan, got rebuilds=%d", v.cornerOverlay.rebuilds)
	}

	// 4. Changing DotRadius invalidates cache.
	visualBig := visual
	visualBig.DotRadius = 24
	drawFrame(info, visualBig)
	if v.cornerOverlay.rebuilds != 2 {
		t.Fatalf("expected rebuild on DotRadius change, got rebuilds=%d", v.cornerOverlay.rebuilds)
	}

	// 5. Zoomed-in stage: small pan within overscan hits cache; large pan rebuilds.
	zoomedInfo := viewportLayoutInfo{
		ViewportW:   1000,
		ViewportH:   800,
		StageX:      -1000,
		StageY:      -1200,
		StageWidth:  3500,
		StageHeight: 4800,
	}
	drawFrame(zoomedInfo, visualBig)
	rebuildsAfterZoom := v.cornerOverlay.rebuilds
	if rebuildsAfterZoom != 3 {
		t.Fatalf("expected rebuild on zoom, got rebuilds=%d", rebuildsAfterZoom)
	}

	smallPan := zoomedInfo
	smallPan.StageX -= 50
	smallPan.StageY -= 50
	drawFrame(smallPan, visualBig)
	if v.cornerOverlay.rebuilds != rebuildsAfterZoom {
		t.Fatalf("expected overscan cache hit on 50px zoomed pan, got rebuilds=%d (want %d)",
			v.cornerOverlay.rebuilds, rebuildsAfterZoom)
	}

	largePan := zoomedInfo
	largePan.StageX -= 500
	largePan.StageY -= 500
	drawFrame(largePan, visualBig)
	if v.cornerOverlay.rebuilds != rebuildsAfterZoom+1 {
		t.Fatalf("expected cache rebuild on 500px zoomed pan, got rebuilds=%d (want %d)",
			v.cornerOverlay.rebuilds, rebuildsAfterZoom+1)
	}

	// 6. Leaving Corner mode releases cache.
	discVisual := visualBig
	discVisual.Mode = "disc"
	drawFrame(largePan, discVisual)
	if v.cornerOverlay != nil {
		t.Fatal("expected cornerOverlay to be released when leaving Corner mode")
	}
}

func TestCheckerboardTilePhaseAndAlignment(t *testing.T) {
	img := buildCheckerboardTileImage(16)
	if img == nil || img.Rect.Size() != image.Pt(512, 512) {
		t.Fatalf("buildCheckerboardTileImage(16) size = %v, want 512x512", img.Bounds())
	}
	wantLight := color.RGBA{R: colCheckerLight.R, G: colCheckerLight.G, B: colCheckerLight.B, A: colCheckerLight.A}
	wantDark := color.RGBA{R: colCheckerDark.R, G: colCheckerDark.G, B: colCheckerDark.B, A: colCheckerDark.A}
	if got := img.RGBAAt(0, 0); got != wantLight {
		t.Fatalf("tile(0,0) = %+v, want %+v", got, wantLight)
	}
	if got := img.RGBAAt(16, 0); got != wantDark {
		t.Fatalf("tile(16,0) = %+v, want %+v", got, wantDark)
	}
	if got := img.RGBAAt(0, 16); got != wantDark {
		t.Fatalf("tile(0,16) = %+v, want %+v", got, wantDark)
	}
	if got := img.RGBAAt(16, 16); got != wantLight {
		t.Fatalf("tile(16,16) = %+v, want %+v", got, wantLight)
	}

	var ops op.Ops
	drawCheckerboard(&ops, image.Rect(50, 20, 900, 820), 176.4, -310.2, 16)
	drawCheckerboard(&ops, image.Rect(0, 0, 1200, 900), 0, 0, 16)
}

func BenchmarkCornerOverlayRebuild1000Corners(b *testing.B) {
	v := newViewportState()
	dims := image.Pt(5100, 7020)
	pts := makeTestCornerPoints(1000, dims.X, dims.Y)
	info := viewportLayoutInfo{
		ViewportW:   1101,
		ViewportH:   1031,
		StageX:      176,
		StageY:      0,
		StageWidth:  749,
		StageHeight: 1031,
	}
	var ops op.Ops
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ops.Reset()
		v.drawDetectedCornersOverlay(&ops, pts, dims, 5+(i%2), 2.5, info)
	}
}

func TestViewportReducedResolutionPreviewFillsFullStage(t *testing.T) {
	v := newViewportState()
	th := newAtroposTheme()
	logicalDims := image.Pt(5100, 7020)
	reducedDims := image.Pt(1017, 1400)
	reducedImg := image.NewNRGBA(image.Rectangle{Max: reducedDims})
	resolveCalls := 0
	resolveImg := func(src string) (*image.NRGBA, bool) {
		resolveCalls++
		return reducedImg, true
	}

	var ops op.Ops
	renderFrame := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(800, 700)),
		}
		v.Layout(
			gtx,
			th,
			resolveImg,
			"reduced-preview",
			logicalDims,
			false,
			"",
			nil,
			viewportVisualState{RealImageDims: logicalDims},
			viewportCallbacks{},
		)
	}

	renderFrame()
	if v.activeEntry == nil {
		t.Fatal("expected activeEntry after rendering reduced preview")
	}
	if v.activeEntry.dims != reducedDims {
		t.Fatalf("activeEntry.dims = %v, want %v", v.activeEntry.dims, reducedDims)
	}
	wantFullRect := image.Rectangle{Max: reducedDims}
	if v.activeEntry.rect != wantFullRect {
		t.Fatalf("activeEntry.rect = %v, want %v (full reduced source covering stage)", v.activeEntry.rect, wantFullRect)
	}

	prevCalls := resolveCalls
	renderFrame()
	if resolveCalls != prevCalls {
		t.Fatalf("expected second identical frame to hit cache without resolving image: calls %d -> %d", prevCalls, resolveCalls)
	}

	// Zoom in and pan to bottom-right; raster coordinates must remain in reducedDims space.
	v.Zoom = 3.0
	v.ScrollX = 1e6
	v.ScrollY = 1e6
	renderFrame()
	if v.activeEntry == nil || v.activeEntry.rect.Empty() {
		t.Fatal("expected non-empty activeEntry after zoomed pan")
	}
	if v.activeEntry.dims != reducedDims {
		t.Fatalf("zoomed activeEntry.dims = %v, want %v", v.activeEntry.dims, reducedDims)
	}
	if v.activeEntry.rect.Max != reducedDims {
		t.Fatalf("expected panned bottom-right rect to reach %v, got %v", reducedDims, v.activeEntry.rect.Max)
	}
}
