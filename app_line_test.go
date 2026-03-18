package main

import (
	"image/color"
	"strings"
	"testing"
)

// linesForRect returns 4 LineAddRequests that trace the edges of a rectangle
// defined by its TL, TR, BR, BL corners. When passed to ProcessLines, the
// 4 pairwise edge-intersections yield exactly those 4 corners.
func linesForRect(tl, tr, br, bl [2]int) []LineAddRequest {
	return []LineAddRequest{
		{X1: tl[0], Y1: tl[1], X2: tr[0], Y2: tr[1]}, // top edge
		{X1: tr[0], Y1: tr[1], X2: br[0], Y2: br[1]}, // right edge
		{X1: br[0], Y1: br[1], X2: bl[0], Y2: bl[1]}, // bottom edge
		{X1: bl[0], Y1: bl[1], X2: tl[0], Y2: tl[1]}, // left edge
	}
}

// addRectLines is a helper that adds 4 edge lines for a fixed test rectangle.
func addRectLines(a *App) {
	for _, l := range linesForRect([2]int{10, 10}, [2]int{90, 10}, [2]int{90, 70}, [2]int{10, 70}) {
		a.AddLine(l)
	}
}

// ---- AddLine ----

func TestAddLine_AppendsLine(t *testing.T) {
	a := newTestApp(200, 200)
	if len(a.lines) != 0 {
		t.Fatal("lines should start empty")
	}
	a.AddLine(LineAddRequest{X1: 0, Y1: 0, X2: 100, Y2: 100})
	if len(a.lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(a.lines))
	}
}

func TestAddLine_MessageFormat(t *testing.T) {
	a := newTestApp(200, 200)
	a.AddLine(LineAddRequest{X1: 0, Y1: 0, X2: 100, Y2: 0})
	res, _ := a.AddLine(LineAddRequest{X1: 0, Y1: 100, X2: 100, Y2: 100})
	if res.Message != "Lines: 2/4" {
		t.Fatalf("expected 'Lines: 2/4', got %q", res.Message)
	}
}

func TestAddLine_StoresEndpoints(t *testing.T) {
	a := newTestApp(200, 200)
	a.AddLine(LineAddRequest{X1: 10, Y1: 20, X2: 30, Y2: 40})
	if len(a.lines[0]) != 2 {
		t.Fatal("line should have 2 endpoints")
	}
	if a.lines[0][0].X != 10 || a.lines[0][0].Y != 20 {
		t.Fatalf("start point incorrect: %v", a.lines[0][0])
	}
	if a.lines[0][1].X != 30 || a.lines[0][1].Y != 40 {
		t.Fatalf("end point incorrect: %v", a.lines[0][1])
	}
}

func TestAddLine_CountsUpToFour(t *testing.T) {
	a := newTestApp(200, 200)
	for i := 1; i <= 4; i++ {
		res, _ := a.AddLine(LineAddRequest{X1: 0, Y1: i * 10, X2: 100, Y2: i * 10})
		want := "Lines: " + string(rune('0'+i)) + "/4"
		if res.Message != want {
			t.Fatalf("line %d: expected %q, got %q", i, want, res.Message)
		}
	}
}

// ---- ProcessLines ----

func TestProcessLines_TooFewLines(t *testing.T) {
	a := newTestApp(200, 200)
	_, err := a.ProcessLines()
	if err == nil {
		t.Fatal("expected error with 0 lines")
	}
}

func TestProcessLines_ThreeLinesError(t *testing.T) {
	a := newTestApp(200, 200)
	for i := 0; i < 3; i++ {
		a.AddLine(LineAddRequest{X1: 0, Y1: i * 30, X2: 100, Y2: i * 30})
	}
	_, err := a.ProcessLines()
	if err == nil {
		t.Fatal("expected error with only 3 lines")
	}
}

func TestProcessLines_ProducesWarpedImage(t *testing.T) {
	a := newTestApp(200, 200)
	addRectLines(a)
	_, err := a.ProcessLines()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set after ProcessLines")
	}
}

func TestProcessLines_ClearsLines(t *testing.T) {
	a := newTestApp(200, 200)
	addRectLines(a)
	a.ProcessLines()
	if len(a.lines) != 0 {
		t.Fatalf("lines should be cleared after ProcessLines, got %d", len(a.lines))
	}
}

func TestProcessLines_SavesUndo(t *testing.T) {
	a := newTestApp(200, 200)
	before := len(a.undoStack)
	addRectLines(a)
	a.ProcessLines()
	if len(a.undoStack) != before+1 {
		t.Fatalf("expected undo stack +1, got %d→%d", before, len(a.undoStack))
	}
}

func TestProcessLines_ReturnsPreview(t *testing.T) {
	a := newTestApp(200, 200)
	addRectLines(a)
	res, err := a.ProcessLines()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatal("preview is not a data URI")
	}
}

func TestProcessLines_OutputDimensions(t *testing.T) {
	// Rectangle: TL(10,10) TR(90,10) BR(90,70) BL(10,70)
	// Expected output: width=80 (dist TL-TR), height=60 (dist TL-BL).
	a := newTestApp(200, 200)
	addRectLines(a)
	res, err := a.ProcessLines()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 80 || res.Height != 60 {
		t.Fatalf("expected 80×60, got %d×%d", res.Width, res.Height)
	}
}

func TestProcessLines_UsesOriginalImageNotCurrentImage(t *testing.T) {
	// ProcessLines warps from originalImage, not currentImage.
	// Override currentImage with a different colour so the distinction is visible.
	a := newTestApp(200, 200)
	// originalImage is (128,64,32). Paint currentImage solid red.
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			a.currentImage.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	addRectLines(a)
	if _, err := a.ProcessLines(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Sample a pixel from inside the warped region. If originalImage was used,
	// the pixel should be near (128,64,32), not (255,0,0).
	c := a.warpedImage.NRGBAAt(40, 30)
	if c.R == 255 && c.G == 0 && c.B == 0 {
		t.Fatal("ProcessLines should warp from originalImage, not currentImage")
	}
}

func TestProcessLines_UndoRestoresPreviousState(t *testing.T) {
	a := newTestApp(200, 200)
	addRectLines(a)
	a.ProcessLines()

	// Undo should restore the state captured before ProcessLines ran.
	res, err := a.Undo()
	if err != nil {
		t.Fatalf("Undo failed: %v", err)
	}
	if res.Width <= 0 || res.Height <= 0 {
		t.Fatalf("Undo returned invalid dims: %d×%d", res.Width, res.Height)
	}
}

// ---- ClearLines ----

func TestClearLines_ClearsLinesSlice(t *testing.T) {
	a := newTestApp(200, 200)
	a.AddLine(LineAddRequest{X1: 0, Y1: 0, X2: 100, Y2: 0})
	a.AddLine(LineAddRequest{X1: 0, Y1: 100, X2: 100, Y2: 100})
	a.ClearLines()
	if len(a.lines) != 0 {
		t.Fatalf("lines should be empty after ClearLines, got %d", len(a.lines))
	}
}

func TestClearLines_ClearsWarpedImage(t *testing.T) {
	a := newTestApp(200, 200)
	a.warpedImage = cloneImage(a.currentImage)
	a.ClearLines()
	if a.warpedImage != nil {
		t.Fatal("warpedImage should be nil after ClearLines")
	}
}

func TestClearLines_ReturnsCurrentImagePreview(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.ClearLines()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "data:image/") {
		t.Fatal("preview is not a data URI")
	}
}

func TestClearLines_ReturnsDimsOfCurrentImage(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.ClearLines()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 100 || res.Height != 80 {
		t.Fatalf("expected 100×80, got %d×%d", res.Width, res.Height)
	}
}

func TestClearLines_NoImageStillReturnsResult(t *testing.T) {
	// ClearLines is more permissive than other resets: it returns a result
	// even when no image is loaded (lines are still cleared).
	a := NewApp()
	res, err := a.ClearLines()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result even with no image")
	}
}

func TestClearLines_MessageAfterClear(t *testing.T) {
	a := newTestApp(100, 80)
	res, _ := a.ClearLines()
	if res.Message == "" {
		t.Fatal("expected non-empty message after ClearLines")
	}
}
