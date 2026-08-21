package main

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// newLoadedTestApp is like newTestApp but also sets imageLoaded=true so that
// methods that guard on that field (DetectCorners, ClickCorner) work correctly.
func newLoadedTestApp(w, h int) *App {
	a := newTestApp(w, h)
	a.imageLoaded = true
	return a
}

// clickFourCorners is a helper that sends 4 custom corner clicks forming a
// rectangle inside the image. It returns the final ClickCornerResult.
func clickFourCorners(t *testing.T, a *App) *ClickCornerResult {
	t.Helper()
	coords := []ClickCornerRequest{
		{X: 10, Y: 10, Custom: true},
		{X: 110, Y: 10, Custom: true},
		{X: 10, Y: 90, Custom: true},
		{X: 110, Y: 90, Custom: true},
	}
	var res *ClickCornerResult
	for i, c := range coords {
		var err error
		res, err = a.ClickCorner(c)
		if err != nil {
			t.Fatalf("click %d: unexpected error: %v", i+1, err)
		}
	}
	return res
}

// ---- ClickCorner ----

func TestClickCorner_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.ClickCorner(ClickCornerRequest{X: 10, Y: 10, Custom: true})
	if err == nil {
		t.Fatal("expected error when no image loaded")
	}
}

func TestCornerDetectBlockSizeWidensCoarsestScale(t *testing.T) {
	if got := cornerDetectBlockSize(1); got != 7 {
		t.Fatalf("scale 1 block size: got %d, want 7", got)
	}
	if got := cornerDetectBlockSize(2); got != 7 {
		t.Fatalf("scale 2 block size: got %d, want 7", got)
	}
	if got := cornerDetectBlockSize(4); got != 11 {
		t.Fatalf("scale 4 block size: got %d, want 11", got)
	}
	if got := cornerDetectBlockSize(16); got != 11 {
		t.Fatalf("scale 16 block size: got %d, want 11", got)
	}
}

func TestCornerDetectPassesPreserveRequestedBudget(t *testing.T) {
	passes := cornerDetectPasses(500)
	want := []cornerDetectPass{
		{scale: 1, maxCorners: 32, highlightBlackPoint: 240},
		{scale: 1, maxCorners: 54, highlightBlackPoint: 230},
		{scale: 1, maxCorners: 125},
		{scale: 2, maxCorners: 78},
		{scale: 4, maxCorners: 133},
		{scale: 16, maxCorners: 78},
	}
	if len(passes) != len(want) {
		t.Fatalf("got %v, want %v", passes, want)
	}
	for i := range want {
		if passes[i] != want[i] {
			t.Fatalf("got %v, want %v", passes, want)
		}
	}
}

func TestStretchGrayRangeExpandsHighlights(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 4, 1))
	src.Pix = []byte{239, 240, 248, 255}
	got := stretchGrayRange(src, 240, 255)
	want := []byte{0, 0, 136, 255}
	for i := range want {
		if got.Pix[i] != want[i] {
			t.Fatalf("pixel %d: got %d, want %d", i, got.Pix[i], want[i])
		}
	}
}

func TestAdaptiveHighlightStretchUsesBrightPerimeter(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 100, 100))
	for i := range src.Pix {
		src.Pix[i] = 248
	}
	for y := 20; y < 80; y++ {
		for x := 20; x < 80; x++ {
			src.SetGray(x, y, color.Gray{Y: 220})
		}
	}
	got, blackPoint, whitePoint := adaptiveHighlightStretch(src)
	if blackPoint <= 220 || blackPoint >= 248 || whitePoint < 248 {
		t.Fatalf("unexpected adaptive range %d-%d", blackPoint, whitePoint)
	}
	if got.GrayAt(50, 50).Y != 0 || got.GrayAt(0, 0).Y < 200 {
		t.Fatalf("adaptive stretch did not separate object and perimeter: object=%d perimeter=%d", got.GrayAt(50, 50).Y, got.GrayAt(0, 0).Y)
	}
}

func TestCornerDetectPassesHandleSmallBudget(t *testing.T) {
	passes := cornerDetectPasses(1)
	total := 0
	for _, pass := range passes {
		total += pass.maxCorners
	}
	if total != 1 {
		t.Fatalf("allocated %d corners, want 1: %v", total, passes)
	}
}

func TestHighlightRecoveryBudgetFavorsBrightPerimeterScans(t *testing.T) {
	bright := highlightRecoveryBudget(500, perimeterBackground{dark: false})
	if bright != 375 {
		t.Fatalf("bright perimeter budget: got %d, want 375", bright)
	}
	dark := highlightRecoveryBudget(500, perimeterBackground{dark: true})
	if dark != 500 {
		t.Fatalf("dark perimeter budget: got %d, want 500", dark)
	}
	if got := highlightRecoveryBudget(1, perimeterBackground{dark: false}); got != 1 {
		t.Fatalf("small bright budget: got %d, want 1", got)
	}
}

func TestDedupeCornerPointsPreservesDistinctScaleLocalizations(t *testing.T) {
	points := []image.Point{
		{X: 100, Y: 100},
		{X: 110, Y: 100}, // duplicate: less than 90/3 pixels away
		{X: 140, Y: 100}, // distinct: formerly removed by the 90/2 radius
	}
	got := dedupeCornerPoints(points, 90)
	want := []image.Point{{X: 100, Y: 100}, {X: 140, Y: 100}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestRefineCoarseCornerUsesNearestFineCandidate(t *testing.T) {
	coarse := image.Pt(256, 192)
	fine := []image.Point{{280, 192}, {257, 196}, {250, 198}}
	got := refineCoarseCorner(coarse, fine, 16)
	want := image.Pt(257, 196)
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRefineCoarseCornerKeepsPointWithoutNearbyCandidate(t *testing.T) {
	coarse := image.Pt(256, 192)
	fine := []image.Point{{280, 192}, {256, 209}}
	if got := refineCoarseCorner(coarse, fine, 16); got != coarse {
		t.Fatalf("got %v, want unchanged %v", got, coarse)
	}
}

func TestRefineDetectedCornerUsesRawCandidateForRegularPass(t *testing.T) {
	point := image.Pt(100, 100)
	rawBroad := []image.Point{{90, 100}}
	rawFine := []image.Point{{84, 100}}
	got := refineDetectedCorner(point, cornerDetectPass{scale: 1}, nil, rawBroad, rawFine)
	if got != rawFine[0] {
		t.Fatalf("got %v, want %v", got, rawFine[0])
	}
}

func TestRefineDetectedCornerDoesNotMoveHighlightPass(t *testing.T) {
	point := image.Pt(100, 100)
	rawBroad := []image.Point{{90, 100}}
	rawFine := []image.Point{{84, 100}}
	got := refineDetectedCorner(point, cornerDetectPass{scale: 1, highlightBlackPoint: 240}, nil, rawBroad, rawFine)
	if got != point {
		t.Fatalf("got %v, want unchanged %v", got, point)
	}
}

func TestRefineDetectedCornerLocalizesBroaderHighlightPass(t *testing.T) {
	point := image.Pt(100, 100)
	rawBroad := []image.Point{{90, 100}}
	rawFine := []image.Point{{84, 100}}
	got := refineDetectedCorner(point, cornerDetectPass{scale: 1, highlightBlackPoint: 230}, nil, rawBroad, rawFine)
	if got != rawFine[0] {
		t.Fatalf("got %v, want %v", got, rawFine[0])
	}
}

func TestClickCorner_FirstThreeReturnNoPreview(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	for i := 1; i <= 3; i++ {
		res, err := a.ClickCorner(ClickCornerRequest{X: i * 20, Y: i * 20, Custom: true})
		if err != nil {
			t.Fatalf("click %d: unexpected error: %v", i, err)
		}
		if res.Preview != "" {
			t.Fatalf("click %d: expected no preview, got non-empty preview", i)
		}
		if res.Done {
			t.Fatalf("click %d: expected Done=false", i)
		}
	}
}

func TestClickCorner_CountIncrements(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	for i := 1; i <= 3; i++ {
		res, _ := a.ClickCorner(ClickCornerRequest{X: i * 20, Y: i * 20, Custom: true})
		if res.Count != i {
			t.Fatalf("expected Count=%d, got %d", i, res.Count)
		}
	}
}

func TestClickCorner_FirstThreeReturnSnappedCoords(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	res, err := a.ClickCorner(ClickCornerRequest{X: 42, Y: 17, Custom: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SnappedX != 42 || res.SnappedY != 17 {
		t.Fatalf("expected snapped (42,17), got (%d,%d)", res.SnappedX, res.SnappedY)
	}
}

func TestClickCorner_SnapsToNearestDetectedCorner(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.detectedCorners = []image.Point{{50, 50}, {150, 150}}
	// Click near (52,48) — closest detected corner is (50,50).
	res, err := a.ClickCorner(ClickCornerRequest{X: 52, Y: 48, Custom: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SnappedX != 50 || res.SnappedY != 50 {
		t.Fatalf("expected snap to (50,50), got (%d,%d)", res.SnappedX, res.SnappedY)
	}
}

func TestClickCorner_DoesNotSnapBeyondRadius(t *testing.T) {
	a := newLoadedTestApp(1000, 1000)
	a.detectedCorners = []image.Point{{500, 500}}
	res, err := a.ClickCorner(ClickCornerRequest{X: 100, Y: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SnappedX != 100 || res.SnappedY != 100 {
		t.Fatalf("expected raw click (100,100), got (%d,%d)", res.SnappedX, res.SnappedY)
	}
}

func TestCornerSnapRadiusIsBounded(t *testing.T) {
	if got := cornerSnapRadius(200, 300); got != 24 {
		t.Fatalf("small image radius: got %.1f, want 24", got)
	}
	if got := cornerSnapRadius(5100, 7020); got != 153 {
		t.Fatalf("scan radius: got %.1f, want 153", got)
	}
	if got := cornerSnapRadius(10000, 12000); got != 160 {
		t.Fatalf("large image radius: got %.1f, want 160", got)
	}
}

func TestClickCorner_CustomIgnoresDetectedCorners(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.detectedCorners = []image.Point{{50, 50}}
	// Custom=true: raw coordinate must be used even though (50,50) is nearby.
	res, err := a.ClickCorner(ClickCornerRequest{X: 10, Y: 10, Custom: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SnappedX != 10 || res.SnappedY != 10 {
		t.Fatalf("expected (10,10) with custom mode, got (%d,%d)", res.SnappedX, res.SnappedY)
	}
}

func TestClickCorner_FourthClickDoneIsTrue(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	res := clickFourCorners(t, a)
	if !res.Done {
		t.Fatal("expected Done=true after 4th click")
	}
}

func TestClickCorner_FourthClickReturnsPreview(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	res := clickFourCorners(t, a)
	if !strings.HasPrefix(res.Preview, previewAssetPathPrefix) {
		t.Fatalf("expected asset preview after 4th click, got: %q", res.Preview[:min(len(res.Preview), 50)])
	}
}

func TestClickCorner_FourthClickSetsWarpedImage(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	clickFourCorners(t, a)
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set after 4th click")
	}
}

func TestClickCorner_FourthClickSavesUndo(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	before := len(a.undoStack)
	clickFourCorners(t, a)
	if len(a.undoStack) != before+1 {
		t.Fatalf("expected undo stack +1 after warp, got %d→%d", before, len(a.undoStack))
	}
}

func TestClickCorner_FourthClickClearsSelectedCorners(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	clickFourCorners(t, a)
	if len(a.selectedCorners) != 0 {
		t.Fatalf("selectedCorners should be cleared after warp, got %d", len(a.selectedCorners))
	}
}

func TestClickCorner_FourthClickReturnsNonZeroDims(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	res := clickFourCorners(t, a)
	if res.Width <= 0 || res.Height <= 0 {
		t.Fatalf("expected positive Width/Height after warp, got %d×%d", res.Width, res.Height)
	}
}

func TestClickCorner_FourthClickSetsDescreenReset(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.descreenResultImage = cloneImage(a.currentImage)
	res := clickFourCorners(t, a)
	if !res.DescreenReset {
		t.Fatal("expected DescreenReset=true after perspective warp when descreenResultImage was present")
	}
}

// ---- ResetCorners ----

func TestResetCorners_ClearsSelectedCorners(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.selectedCorners = []image.Point{{10, 10}, {50, 50}}
	_, err := a.ResetCorners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.selectedCorners) != 0 {
		t.Fatal("selectedCorners should be nil after ResetCorners")
	}
}

func TestResetCorners_ClearsWarpedImage(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.warpedImage = cloneImage(a.currentImage)
	_, err := a.ResetCorners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage != nil {
		t.Fatal("warpedImage should be nil after ResetCorners")
	}
}

func TestResetCorners_PreservesDetectedCorners(t *testing.T) {
	a := newLoadedTestApp(200, 200)
	a.detectedCorners = []image.Point{{10, 10}, {50, 50}, {80, 80}}
	_, err := a.ResetCorners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.detectedCorners) != 3 {
		t.Fatalf("detectedCorners should be preserved, got %d", len(a.detectedCorners))
	}
}

func TestResetCorners_ReturnsCurrentImageDims(t *testing.T) {
	a := newLoadedTestApp(200, 150)
	res, err := a.ResetCorners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 200 || res.Height != 150 {
		t.Fatalf("expected 200×150, got %d×%d", res.Width, res.Height)
	}
}

func TestResetCorners_PreviewIsDataURI(t *testing.T) {
	a := newLoadedTestApp(100, 80)
	res, err := a.ResetCorners()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, previewAssetPathPrefix) {
		t.Fatal("preview is not an asset URL")
	}
}

func TestResetCorners_ReturnsDetectedCornersInResult(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	a.detectedCorners = []image.Point{{20, 20}, {80, 80}}
	res, _ := a.ResetCorners()
	if len(res.Corners) != 2 {
		t.Fatalf("expected 2 corners in result, got %d", len(res.Corners))
	}
}

func TestResizeImage_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.ResizeImage(ResizeRequest{Width: 100, Height: 100})
	if err == nil {
		t.Fatal("expected error when no image loaded")
	}
}

func TestResizeImage_ResizesCurrentImage(t *testing.T) {
	a := newLoadedTestApp(120, 80)
	res, err := a.ResizeImage(ResizeRequest{Width: 60, Height: 40})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 60 || res.Height != 40 {
		t.Fatalf("unexpected dimensions %dx%d", res.Width, res.Height)
	}
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set after ResizeImage")
	}
	if a.warpedImage.Bounds().Dx() != 60 || a.warpedImage.Bounds().Dy() != 40 {
		t.Fatalf("warpedImage dimensions mismatch %dx%d", a.warpedImage.Bounds().Dx(), a.warpedImage.Bounds().Dy())
	}
}

// ---- RestoreCornerOverlay ----

func TestRestoreCornerOverlay_NoCachedCorners(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	_, err := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{DotRadius: 5})
	if err == nil {
		t.Fatal("expected error when no cached corners")
	}
}

func TestRestoreCornerOverlay_ReturnsCachedCorners(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	a.detectedCorners = []image.Point{{10, 10}, {90, 10}, {90, 90}, {10, 90}}
	res, err := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{DotRadius: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Corners) != 4 {
		t.Fatalf("expected 4 corners, got %d", len(res.Corners))
	}
}

func TestRestoreCornerOverlay_PreviewIsDataURI(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	a.detectedCorners = []image.Point{{10, 10}}
	res, err := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.Preview, previewAssetPathPrefix) {
		t.Fatal("preview is not an asset URL")
	}
}

func TestRestoreCornerOverlay_MessageMentionsCount(t *testing.T) {
	a := newLoadedTestApp(100, 100)
	a.detectedCorners = []image.Point{{10, 10}, {90, 90}}
	res, _ := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{})
	if !strings.Contains(res.Message, "2") {
		t.Fatalf("message should mention corner count 2, got: %q", res.Message)
	}
}

func TestRestoreCornerOverlay_ReturnsDims(t *testing.T) {
	a := newLoadedTestApp(120, 90)
	a.detectedCorners = []image.Point{{10, 10}}
	res, _ := a.RestoreCornerOverlay(RestoreCornerOverlayRequest{})
	if res.Width != 120 || res.Height != 90 {
		t.Fatalf("expected 120×90, got %d×%d", res.Width, res.Height)
	}
}

// ---- SkipCrop ----

func TestSkipCrop_NoImage(t *testing.T) {
	a := NewApp()
	_, err := a.SkipCrop()
	if err == nil {
		t.Fatal("expected error when no image loaded")
	}
}

func TestSkipCrop_SetsWarpedImage(t *testing.T) {
	a := newTestApp(100, 80)
	_, err := a.SkipCrop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage == nil {
		t.Fatal("warpedImage should be set after SkipCrop")
	}
}

func TestSkipCrop_WarpedImageIsCloneNotSamePointer(t *testing.T) {
	a := newTestApp(100, 80)
	_, err := a.SkipCrop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.warpedImage == a.currentImage {
		t.Fatal("warpedImage should be a clone, not the same pointer as currentImage")
	}
}

func TestSkipCrop_ReturnsDims(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.SkipCrop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Width != 100 || res.Height != 80 {
		t.Fatalf("expected 100×80, got %d×%d", res.Width, res.Height)
	}
}

func TestSkipCrop_DoesNotPublishRedundantPreview(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.SkipCrop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Preview != "" {
		t.Fatalf("SkipCrop published an unchanged preview revision: %q", res.Preview)
	}
}

func TestSkipCrop_ClearsSelectedCorners(t *testing.T) {
	a := newTestApp(100, 100)
	a.selectedCorners = []image.Point{{10, 10}, {50, 50}}
	_, err := a.SkipCrop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a.selectedCorners) != 0 {
		t.Fatal("selectedCorners should be cleared after SkipCrop")
	}
}

func TestSkipCrop_Message(t *testing.T) {
	a := newTestApp(100, 80)
	res, err := a.SkipCrop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Message != "Crop skipped — image ready to save" {
		t.Fatalf("unexpected message: %q", res.Message)
	}
}
