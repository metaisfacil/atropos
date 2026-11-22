package ui

import (
	"image"
	"image/color"
	"math"
	"testing"

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
	l.dx, l.dy = 40, -30
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
		e.startDiscShiftLive(info)
		if e.discShiftLive != l || !l.dragging || !l.showing {
			t.Fatalf("press did not continue on the chain layer (live=%p dragging=%v showing=%v)", e.discShiftLive, l.dragging, l.showing)
		}
		if want := image.Pt(geo.CenterX+l.base.X, geo.CenterY+l.base.Y); e.ctrlDrag.startCenter != want {
			t.Fatalf("startCenter = %v, want %v", e.ctrlDrag.startCenter, want)
		}
	}

	press()
	e.releaseDiscShiftGesture(10, 5)
	if l.base != image.Pt(10, 5) || l.pending != 1 || !l.showing || !l.overlayExact || l.next == nil {
		t.Fatalf("after release 1: base=%v pending=%d showing=%v exact=%v next=%v", l.base, l.pending, l.showing, l.overlayExact, l.next != nil)
	}

	// Second drag before commit 1 lands.
	press()
	if l.overlayExact || l.next != nil {
		t.Fatal("second gesture did not switch to the translucent overlay")
	}
	e.releaseDiscShiftGesture(4, 0)

	r1 := center(10, 5)
	r1.Preview = "/p/1"
	e.discShiftCommitted(l, 10, 5, r1)
	e.mainViewport.PresentedSource = "/p/1"
	e.onDiscShiftPresented("/p/1")
	if !l.showing || l.pending != 1 {
		t.Fatalf("layer hidden while commit 2 pending (showing=%v pending=%d)", l.showing, l.pending)
	}
	r2 := center(14, 5)
	r2.Preview = "/p/2"
	e.discShiftCommitted(l, 4, 0, r2)
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
	if l.base != image.Pt(14, 5) {
		t.Fatalf("reused layer base = %v, want (14,5)", l.base)
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
	e.releaseDiscShiftGesture(30, 0)
	// Backend clamped the centre to +20 instead of +30.
	e.discShiftCommitted(l, 30, 0, &ProcessResult{Preview: "/p/1", DiscCenterX: geo.CenterX + 20, DiscCenterY: geo.CenterY})
	e.mainViewport.PresentedSource = "/p/1"
	e.onDiscShiftPresented("/p/1")
	if e.discShiftLive != nil {
		t.Fatal("layer kept after the backend clamped the shift differently")
	}
}

func TestDiscShiftClampGestureKeepsCoverage(t *testing.T) {
	l := &discShiftLiveLayer{key: discShiftLiveKey{maxShift: 50}, base: image.Pt(40, 0)}
	dx, dy := l.clampGesture(30, 0)
	if got := l.base.Add(image.Pt(dx, dy)); got != image.Pt(50, 0) {
		t.Fatalf("clamped total = %v, want (50,0)", got)
	}
	if dx, dy := l.clampGesture(-30, 0); dx != -30 || dy != 0 {
		t.Fatalf("shift back toward centre clamped to (%d,%d)", dx, dy)
	}
}
