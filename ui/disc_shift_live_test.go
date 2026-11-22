package ui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/f32"
	"gioui.org/op"
)

func TestComputeDiscShiftHonoursMaxShift(t *testing.T) {
	// Scale 2x: a 100px screen drag wants a 200px source shift; cap it at 50.
	res, ok := computeDiscShift(100, 0, image.Pt(2000, 2000), 300, 0, image.Pt(1000, 1000), image.Pt(1000, 1000), 1000, 1000, image.Pt(2000, 2000), 50)
	if !ok {
		t.Fatal("computeDiscShift failed")
	}
	if res.dx != -50 || res.dy != 0 {
		t.Fatalf("clamped shift = (%d, %d), want (-50, 0)", res.dx, res.dy)
	}
	// Diagonal drags are clamped by magnitude, preserving direction.
	res, _ = computeDiscShift(100, 100, image.Pt(2000, 2000), 300, 0, image.Pt(1000, 1000), image.Pt(1000, 1000), 1000, 1000, image.Pt(2000, 2000), 50)
	if got := math.Hypot(float64(res.dx), float64(res.dy)); math.Abs(got-50) > 1 || res.dx != res.dy {
		t.Fatalf("diagonal clamped shift = (%d, %d) |%.2f|, want ~(-35,-35)", res.dx, res.dy, got)
	}
}

func testDiscShiftGeometry() DiscShiftPreview {
	// 200x200 result for a radius-100 disc centred at (300,250) in a 600x500
	// source, no rotation or insets.
	return DiscShiftPreview{
		CenterX: 300, CenterY: 250,
		SourcePivotX: 300.5, SourcePivotY: 250.5,
		PivotX: 100.5, PivotY: 100.5,
		MaskCenterX: 100.5, MaskCenterY: 100.5,
		Radius: 100, FeatherSize: 10, CutoutRadius: 12,
		BgR: 10, BgG: 20, BgB: 30,
		LevelsWhite: 255,
		Width:       200, Height: 200,
	}
}

func TestRenderDiscVignette(t *testing.T) {
	geo := testDiscShiftGeometry()
	// Stage drawn at 2x (400px) starting at viewport (50,40).
	ov := renderDiscVignette(geo, image.Rect(50, 40, 450, 440), 50, 40, 2, 2, nil, false)
	at := func(rx, ry float64) color.RGBA {
		return ov.RGBAAt(int(rx*2), int(ry*2))
	}
	if c := at(100, 50); c.A != 0 {
		t.Fatalf("inside disc should be clear, alpha=%d", c.A)
	}
	if c := at(100.5, 100.5); c.A != 255 || c.R != 10 || c.G != 20 || c.B != 30 {
		t.Fatalf("centre cutout should be opaque bg, got %+v", c)
	}
	if c := at(2, 2); c.A != 255 {
		t.Fatalf("corner outside disc should be opaque bg, alpha=%d", c.A)
	}
	// Mid-feather (5px in from the edge) is half covered.
	if c := at(100.5, 5.5); c.A < 100 || c.A > 155 {
		t.Fatalf("mid-feather alpha=%d, want ~128", c.A)
	}
}

func TestBuildAndDrawDiscShiftLiveLayer(t *testing.T) {
	geo := testDiscShiftGeometry()
	src := image.NewNRGBA(image.Rect(0, 0, 600, 500))
	for i := range src.Pix {
		src.Pix[i] = 200
	}
	info := viewportLayoutInfo{
		ViewportW: 800, ViewportH: 600,
		StageX: 200, StageY: 100, StageWidth: 400, StageHeight: 400,
		StageDims: image.Pt(200, 200),
	}
	maxShift := discShiftMaxShift(info)
	if maxShift != 0.5*800/2 {
		t.Fatalf("maxShift = %v, want 200", maxShift)
	}
	l := buildDiscShiftLiveLayer(geo, src, info, maxShift)
	if l == nil {
		t.Fatal("layer not built")
	}
	// Content must cover the disc at any permitted shift (clipped to source).
	if want := image.Rect(0, 0, 600, 500); l.contentRect != want {
		t.Fatalf("contentRect = %v, want %v", l.contentRect, want)
	}
	if l.overlayRect != image.Rect(200, 100, 600, 500) {
		t.Fatalf("overlayRect = %v", l.overlayRect)
	}
	if l.contentSize.X > discShiftLiveMaxDim || l.contentSize.Y > discShiftLiveMaxDim {
		t.Fatalf("content raster too large: %v", l.contentSize)
	}
	l.gesture = discLivePose{shift: image.Pt(40, -30), angle: 12}
	ops := new(op.Ops)
	drawDiscShiftLive(ops, l, info, image.Rect(0, 0, 800, 600))

	// Levels are applied to the content raster.
	geo.LevelsBlack, geo.LevelsWhite = 0, 100
	l = buildDiscShiftLiveLayer(geo, src, info, maxShift)
	if l == nil {
		t.Fatal("layer with levels not built")
	}
}

// Rapid successive drags must stay on one live layer: a press while earlier
// commits are still in flight continues from the accumulated shift instead of
// falling back to the stale raster, and the layer only hides once the last
// commit's render is presented.
func TestDiscShiftRapidDragChain(t *testing.T) {
	e := newTestEditorUI(t)
	e.mode, e.discActive, e.imageLoaded = "disc", true, true
	geo := testDiscShiftGeometry()
	src := image.NewNRGBA(image.Rect(0, 0, 600, 500))
	info := viewportLayoutInfo{
		ViewportW: 800, ViewportH: 600,
		StageX: 200, StageY: 100, StageWidth: 400, StageHeight: 400,
		StageDims: image.Pt(200, 200),
	}
	l := buildDiscShiftLiveLayer(geo, src, info, discShiftMaxShift(info))
	if l == nil {
		t.Fatal("layer not built")
	}
	l.key.source, l.validSource, l.own = "/p/0", "/p/0", map[string]bool{"/p/0": true}
	e.mainViewport.PresentedSource = "/p/0"
	e.discShiftLive = l
	center := func(dx, dy int) *ProcessResult {
		return &ProcessResult{DiscCenterX: geo.CenterX + dx, DiscCenterY: geo.CenterY + dy}
	}
	press := func() {
		e.ctrlDrag = &discCtrlDragState{}
		e.startDiscLive(info)
		if e.discShiftLive != l || !l.dragging || !l.showing {
			t.Fatalf("press did not continue on the chain layer (live=%p dragging=%v showing=%v)", e.discShiftLive, l.dragging, l.showing)
		}
		if want := image.Pt(geo.CenterX+l.base.shift.X, geo.CenterY+l.base.shift.Y); e.ctrlDrag.startCenter != want {
			t.Fatalf("startCenter = %v, want %v", e.ctrlDrag.startCenter, want)
		}
	}

	press()
	e.releaseDiscLiveGesture(discLivePose{shift: image.Pt(10, 5)})
	if l.base.shift != image.Pt(10, 5) || l.pending != 1 || !l.showing || !l.overlayExact || l.next == nil {
		t.Fatalf("after release 1: base=%v pending=%d showing=%v exact=%v next=%v", l.base, l.pending, l.showing, l.overlayExact, l.next != nil)
	}

	// Second drag (a Shift+drag rotation) before commit 1 lands.
	press()
	if l.overlayExact || l.next != nil {
		t.Fatal("second gesture did not switch to the translucent overlay")
	}
	e.releaseDiscLiveGesture(discLivePose{angle: 2.5})
	// Third drag, another shift, still before anything lands.
	press()
	e.releaseDiscLiveGesture(discLivePose{shift: image.Pt(4, 0)})
	if l.base != (discLivePose{shift: image.Pt(14, 5), angle: 2.5}) || l.pending != 3 {
		t.Fatalf("after three releases: base=%+v pending=%d", l.base, l.pending)
	}

	r1 := center(10, 5)
	r1.Preview = "/p/1"
	e.discLiveCommitted(l, discLivePose{shift: image.Pt(10, 5)}, r1)
	e.mainViewport.PresentedSource = "/p/1"
	e.onDiscShiftPresented("/p/1")
	if !l.showing || l.pending != 2 {
		t.Fatalf("layer hidden while commits pending (showing=%v pending=%d)", l.showing, l.pending)
	}
	rr := center(10, 5)
	rr.Preview, rr.DiscRotation = "/p/1r", 2.5
	e.discLiveCommitted(l, discLivePose{angle: 2.5}, rr)
	r2 := center(14, 5)
	r2.Preview, r2.DiscRotation = "/p/2", 2.5
	e.discLiveCommitted(l, discLivePose{shift: image.Pt(4, 0)}, r2)
	if !l.showing {
		t.Fatal("layer hidden before the final render was presented")
	}
	e.mainViewport.PresentedSource = "/p/2"
	e.onDiscShiftPresented("/p/2")
	if l.showing || e.discShiftLive != l || l.stale {
		t.Fatalf("chain did not settle into an idle reusable layer (showing=%v live=%p stale=%v)", l.showing, e.discShiftLive, l.stale)
	}

	// A later drag reuses the idle layer from the accumulated shift; a
	// cancelled gesture just settles again.
	press()
	if l.base != (discLivePose{shift: image.Pt(14, 5), angle: 2.5}) {
		t.Fatalf("reused layer base = %+v", l.base)
	}
	if got := e.liveDiscRotation(); got != 2.5 {
		t.Fatalf("live rotation for shift mapping = %v, want 2.5", got)
	}
	e.cancelActiveCanvasDrag()
	if l.showing || l.dragging || e.discShiftLive != l {
		t.Fatal("cancelled gesture did not settle the layer")
	}

	// A render the chain did not produce (feather change, undo...) drops it.
	e.onDiscShiftPresented("/p/other")
	if e.discShiftLive != nil {
		t.Fatal("foreign render did not drop the idle layer")
	}
}

func TestDiscShiftClampedCommitDropsLayer(t *testing.T) {
	e := newTestEditorUI(t)
	geo := testDiscShiftGeometry()
	l := &discShiftLiveLayer{geo: geo, key: discShiftLiveKey{maxShift: 100}, validSource: "/p/0", own: map[string]bool{"/p/0": true}}
	e.mainViewport.PresentedSource = "/p/0"
	e.discShiftLive = l
	l.dragging, l.showing = true, true
	e.releaseDiscLiveGesture(discLivePose{shift: image.Pt(30, 0)})
	// Backend clamped the centre to +20 instead of +30.
	e.discLiveCommitted(l, discLivePose{shift: image.Pt(30, 0)}, &ProcessResult{Preview: "/p/1", DiscCenterX: geo.CenterX + 20, DiscCenterY: geo.CenterY})
	e.mainViewport.PresentedSource = "/p/1"
	e.onDiscShiftPresented("/p/1")
	if e.discShiftLive != nil {
		t.Fatal("layer kept after the backend clamped the shift differently")
	}
}

func TestDiscLiveLimitGestureKeepsCoverage(t *testing.T) {
	geo := testDiscShiftGeometry()
	src := image.NewNRGBA(image.Rect(0, 0, 600, 500))
	// Fit view: the whole disc is visible and covered, so any rotation works
	// and shifts are bounded by magnitude.
	fit := viewportLayoutInfo{ViewportW: 800, ViewportH: 600, StageX: 200, StageY: 100, StageWidth: 400, StageHeight: 400, StageDims: image.Pt(200, 200)}
	l := buildDiscShiftLiveLayer(geo, src, fit, 50)
	l.base = discLivePose{shift: image.Pt(40, 0)}
	if g := l.limitGesture(discLivePose{shift: image.Pt(30, 0)}); l.base.add(g).shift != image.Pt(50, 0) {
		t.Fatalf("clamped total = %+v, want shift (50,0)", l.base.add(g))
	}
	if g := l.limitGesture(discLivePose{shift: image.Pt(-30, 0)}); g.shift != image.Pt(-30, 0) {
		t.Fatalf("shift back toward centre limited to %+v", g)
	}
	l.base = discLivePose{}
	if g := l.limitGesture(discLivePose{angle: 170}); g.angle != 170 {
		t.Fatalf("rotation in a fit view limited to %v", g.angle)
	}

	// Zoomed 8x onto the rim: only a sliver of source around the visible
	// area is held, so a large rotation is stopped short instead of
	// revealing missing content, and a small one passes.
	zoom := viewportLayoutInfo{ViewportW: 400, ViewportH: 300, StageX: -1400, StageY: -100, StageWidth: 1600, StageHeight: 1600, StageDims: image.Pt(200, 200)}
	z := buildDiscShiftLiveLayer(geo, src, zoom, discShiftMaxShift(zoom))
	if z == nil {
		t.Fatal("zoomed layer not built")
	}
	if g := z.limitGesture(discLivePose{angle: 2}); g.angle != 2 {
		t.Fatalf("small rotation limited to %v", g.angle)
	}
	g := z.limitGesture(discLivePose{angle: 90})
	if g.angle <= 2 || g.angle >= 90 || !z.covers(g) {
		t.Fatalf("large rotation limited to %v (covered=%v), want a covered angle between 2 and 90", g.angle, z.covers(g))
	}
	if want := !z.covers(discLivePose{angle: 10}) || !z.covers(discLivePose{angle: -10}); z.coverageLow() != want {
		t.Fatalf("coverageLow = %v, want %v", z.coverageLow(), want)
	}
}

func TestDiscRotateDragTracksPointerAngle(t *testing.T) {
	pivot := f32.Pt(100, 100)
	r := &discRotateDragState{rebase: true}
	r.update(f32.Pt(200, 100), pivot, 10) // anchors at 0°
	r.update(f32.Pt(100, 200), pivot, 10) // quarter turn clockwise (y down)
	if math.Abs(r.total-90) > 1e-9 {
		t.Fatalf("quarter turn = %v, want 90", r.total)
	}
	// Keep going round past ±180 without a jump, for more than a full turn.
	for _, p := range []f32.Point{{X: 0, Y: 100}, {X: 100, Y: 0}, {X: 200, Y: 100}, {X: 100, Y: 200}} {
		r.update(p, pivot, 10)
	}
	if math.Abs(r.total-450) > 1e-9 {
		t.Fatalf("one and a quarter turns = %v, want 450", r.total)
	}
	// Passing through the dead zone pauses and re-anchors instead of flipping.
	r.update(f32.Pt(102, 101), pivot, 10)
	r.update(f32.Pt(100, 0), pivot, 10)
	if math.Abs(r.total-450) > 1e-9 {
		t.Fatalf("dead zone changed the angle to %v", r.total)
	}
	r.update(f32.Pt(0, 100), pivot, 10) // quarter turn counter-clockwise
	if math.Abs(r.total-360) > 1e-9 {
		t.Fatalf("after counter-clockwise quarter = %v, want 360", r.total)
	}
	// A moved pivot (layer arrived) re-anchors without a jump.
	r.update(f32.Pt(0, 105), f32.Pt(100, 105), 10)
	if math.Abs(r.total-360) > 1e-9 {
		t.Fatalf("pivot change changed the angle to %v", r.total)
	}
}
