package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"atropos/internal/raster"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
)

type stubBackend struct {
	nextID         int
	previews       map[string]*image.NRGBA
	calibPreviews  map[string]*image.NRGBA
	current        *image.NRGBA
	corners        []image.Point
	settings       AllSettings
	lastStitchReq  CompositorStitchRequest
	lastOverlayReq CompositorStitchRequest
}

func newStubBackend() *stubBackend {
	return &stubBackend{
		previews:      make(map[string]*image.NRGBA),
		calibPreviews: make(map[string]*image.NRGBA),
		settings: AllSettings{
			TouchupBackend:            "patchmatch",
			IOPaintURL:                "http://127.0.0.1:8086/",
			WarpFillMode:              "clamp",
			WarpFillColor:             "#ffffff",
			CropEdgePixels:            3,
			DiscCenterCutout:          true,
			DiscCutoutPercent:         11,
			CornerMaxCorners:          500,
			CornerMinDistance:         100,
			CornerSettingsVersion:     1,
			TouchupRemainsActive:      true,
			StraightEdgeRemainsActive: true,
			AutoDetectOnModeSwitch:    true,
		},
	}
}

func (s *stubBackend) registerPreview(img *image.NRGBA) string {
	s.nextID++
	url := fmt.Sprintf("/preview/stub-%d.jpg", s.nextID)
	s.previews[url] = img
	return url
}

func (s *stubBackend) cleanResult(msg string) *ProcessResult {
	if s.current == nil {
		return &ProcessResult{Message: msg}
	}
	b := s.current.Bounds()
	return &ProcessResult{
		Preview: s.registerPreview(s.current),
		Width:   b.Dx(),
		Height:  b.Dy(),
		Message: msg,
	}
}

func decodeTestPNG(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	return raster.ToNRGBA(img), nil
}

func (s *stubBackend) SetHost(AppHost)                 {}
func (s *stubBackend) Logf(string, ...interface{})     {}
func (s *stubBackend) OpenURL(string) error            { return nil }
func (s *stubBackend) GetAllSettings() AllSettings     { return s.settings }
func (s *stubBackend) SaveAllSettings(c AllSettings) error { s.settings = c; return nil }
func (s *stubBackend) GetLaunchArgs() LaunchArgs       { return LaunchArgs{} }
func (s *stubBackend) OpenImageDialog() (string, error) { return "", nil }
func (s *stubBackend) OpenSaveDialog() (string, error)  { return "", nil }
func (s *stubBackend) LoadImage(req LoadImageRequest) (*ImageInfo, error) {
	img, err := decodeTestPNG(req.FilePath)
	if err != nil {
		return nil, err
	}
	s.current = img
	s.corners = []image.Point{{X: 24, Y: 20}, {X: img.Bounds().Dx() - 24, Y: 20}}
	b := img.Bounds()
	return &ImageInfo{
		Width:   b.Dx(),
		Height:  b.Dy(),
		Preview: s.registerPreview(img),
		Format:  "PNG",
		SuggestedCornerParams: SuggestedCornerParams{
			MinDistance: 100,
			MaxCorners:  500,
		},
	}, nil
}
func (s *stubBackend) LoadImageFromClipboard() (*ImageInfo, error) { return nil, fmt.Errorf("empty") }
func (s *stubBackend) SaveImage(SaveRequest) (*ProcessResult, error) { return &ProcessResult{}, nil }
func (s *stubBackend) RecropImage() (*ImageInfo, error)              { return nil, fmt.Errorf("none") }
func (s *stubBackend) RunPostSaveCommand(string, string) error       { return nil }
func (s *stubBackend) GetCleanPreview() (*ProcessResult, error)      { return s.cleanResult("Ready"), nil }
func (s *stubBackend) DetectCorners(CornerDetectRequest) (*ProcessResult, error) {
	res := s.cleanResult("Detected corners")
	res.Corners = append([]image.Point(nil), s.corners...)
	return res, nil
}
func (s *stubBackend) CancelCornerDetect() {}
func (s *stubBackend) ClickCorner(req ClickCornerRequest) (*ClickCornerResult, error) {
	return &ClickCornerResult{SnappedX: req.X, SnappedY: req.Y, Count: 1}, nil
}
func (s *stubBackend) RestoreCornerOverlay(RestoreCornerOverlayRequest) (*ProcessResult, error) {
	res := s.cleanResult("Restored corners")
	res.Corners = append([]image.Point(nil), s.corners...)
	return res, nil
}
func (s *stubBackend) UndoLastCorner() int                          { return 0 }
func (s *stubBackend) ResetCorners() (*ProcessResult, error) {
	res := s.cleanResult("Reset")
	res.Corners = append([]image.Point(nil), s.corners...)
	return res, nil
}
func (s *stubBackend) SkipCrop() (*ProcessResult, error)            { return s.cleanResult("Skipped"), nil }
func (s *stubBackend) DrawDisc(req DiscDrawRequest) (*ProcessResult, error) {
	d := req.Radius * 2
	if d < 2 {
		d = 2
	}
	img := image.NewNRGBA(image.Rect(0, 0, d, d))
	s.current = img
	res := s.cleanResult("Disc drawn")
	res.DiscCenterX = req.CenterX
	res.DiscCenterY = req.CenterY
	res.DiscRadius = req.Radius
	return res, nil
}
func (s *stubBackend) StraightEdgeRotate(StraightEdgeRotateRequest) (*ProcessResult, error) { return s.cleanResult(""), nil }
func (s *stubBackend) RotateDisc(DiscRotateRequest) (*ProcessResult, error)                 { return s.cleanResult(""), nil }
func (s *stubBackend) ShiftDisc(ShiftDiscRequest) (*ProcessResult, error)                   { return s.cleanResult(""), nil }
func (s *stubBackend) DiscShiftPreview() (*DiscShiftPreview, error) {
	return nil, fmt.Errorf("stub: no disc shift preview")
}
func (s *stubBackend) GetPixelColor(PixelColorRequest) (*ProcessResult, error)              { return s.cleanResult(""), nil }
func (s *stubBackend) SetFeatherRadius(FeatherRadiusRequest) (*ProcessResult, error)        { return s.cleanResult(""), nil }
func (s *stubBackend) SetFeatherSize(FeatherSizeRequest) (*ProcessResult, error)            { return s.cleanResult(""), nil }
func (s *stubBackend) SetDiscSettings(DiscSettings) (*ProcessResult, error)                 { return s.cleanResult(""), nil }
func (s *stubBackend) ResetDisc() (*ProcessResult, error)                                   { return s.cleanResult("Reset"), nil }
func (s *stubBackend) AddLine(LineAddRequest) (*ProcessResult, error)                       { return &ProcessResult{}, nil }
func (s *stubBackend) ProcessLines() (*ProcessResult, error)                                { return s.cleanResult(""), nil }
func (s *stubBackend) ClearLines() (*ProcessResult, error)                                  { return s.cleanResult("Reset"), nil }
func (s *stubBackend) NormalCrop(NormalCropRequest) (*ProcessResult, error)                 { return s.cleanResult(""), nil }
func (s *stubBackend) ResetNormal() (*ProcessResult, error)                                 { return s.cleanResult("Reset"), nil }
func (s *stubBackend) Undo() (*ProcessResult, error)                                        { return &ProcessResult{}, nil }
func (s *stubBackend) Redo() (*ProcessResult, error)                                        { return &ProcessResult{}, nil }
func (s *stubBackend) Crop(CropRequest) (*ProcessResult, error)                             { return s.cleanResult(""), nil }
func (s *stubBackend) Rotate(RotateRequest) (*ProcessResult, error)                         { return s.cleanResult(""), nil }
func (s *stubBackend) ResizeImage(ResizeRequest) (*ProcessResult, error)                    { return s.cleanResult(""), nil }
func (s *stubBackend) TrimBorders() (*ProcessResult, error)                                 { return s.cleanResult(""), nil }
func (s *stubBackend) AutoContrast(AutoContrastRequest) (*ProcessResult, error)             { return s.cleanResult(""), nil }
func (s *stubBackend) Descreen(DescreenRequest) (*ProcessResult, error)                     { return s.cleanResult(""), nil }
func (s *stubBackend) DustRemoval(DustRemovalRequest) (*ProcessResult, error)               { return s.cleanResult(""), nil }
func (s *stubBackend) SetLevels(SetLevelsRequest) (*ProcessResult, error)                   { return s.cleanResult(""), nil }
func (s *stubBackend) CopySelectionToClipboard(AdjustmentSelection) (string, error)         { return "", nil }
func (s *stubBackend) TouchUpApplyStrokes(TouchUpStrokeRequest) (*ProcessResult, error)   { return s.cleanResult(""), nil }
func (s *stubBackend) CancelTouchup()                                                       {}
func (s *stubBackend) ResolvePreviewImage(url string) (*image.NRGBA, bool) {
	img, ok := s.previews[url]
	return img, ok
}
func (s *stubBackend) ResolveCalibrationImage(url string) (*image.NRGBA, bool) {
	img, ok := s.calibPreviews[url]
	return img, ok
}
func (s *stubBackend) CalibrationOpenFilesDialog() ([]string, error) { return nil, nil }
func (s *stubBackend) CalibrationLoadImage(req CalibrationLoadRequest) (*CalibrationImageInfo, error) {
	img, err := decodeTestPNG(req.FilePath)
	if err != nil {
		return nil, err
	}
	s.nextID++
	url := fmt.Sprintf("/calib/stub-%d.jpg", s.nextID)
	s.calibPreviews[url] = img
	return &CalibrationImageInfo{
		ImagePath: req.FilePath,
		Preview:   url,
		Width:     img.Bounds().Dx(),
		Height:    img.Bounds().Dy(),
	}, nil
}
func (s *stubBackend) CalibrationClearPreview() {}
func (s *stubBackend) CalibrationSaveDataset(CalibrationSaveRequest) (*CalibrationSaveResult, error) {
	return &CalibrationSaveResult{Cancelled: true}, nil
}
func (s *stubBackend) CompositorOpenFilesDialog() ([]string, error)                { return nil, nil }
func (s *stubBackend) CompositorStitch(req CompositorStitchRequest) (*CompositorResult, error) {
	s.lastStitchReq = req
	return &CompositorResult{
		Preview: s.registerPreview(image.NewNRGBA(image.Rect(0, 0, 120, 80))),
		Width:   120,
		Height:  80,
		Message: "Stitched",
	}, nil
}
func (s *stubBackend) CompositorAlignOverlay(req CompositorStitchRequest) (*CompositorResult, error) {
	s.lastOverlayReq = req
	return &CompositorResult{
		Preview: s.registerPreview(image.NewNRGBA(image.Rect(0, 0, 120, 80))),
		Width:   120,
		Height:  80,
		Message: "Aligned",
	}, nil
}
func (s *stubBackend) CompositorAddMask(CompositorMaskRequest) (*CompositorResult, error) {
	return &CompositorResult{}, nil
}
func (s *stubBackend) CompositorUndoMask() (*CompositorResult, error)        { return &CompositorResult{}, nil }
func (s *stubBackend) CompositorClearMasks() (*CompositorResult, error)      { return &CompositorResult{}, nil }
func (s *stubBackend) CompositorRefreshPreview() (*CompositorResult, error)  { return &CompositorResult{}, fmt.Errorf("none") }
func (s *stubBackend) CompositorLoadResult(CompositorLoadResultRequest) (*ImageInfo, error) {
	return nil, fmt.Errorf("none")
}

func writeTestScanPNG(t *testing.T, name string, w, h int) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	bg := color.NRGBA{R: 20, G: 20, B: 20, A: 255}
	fg := color.NRGBA{R: 235, G: 230, B: 220, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= 24 && x < w-24 && y >= 20 && y < h-20 {
				img.SetNRGBA(x, y, fg)
			} else {
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	p := filepath.Join(t.TempDir(), name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create test png: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return p
}

func newTestEditorUI(t *testing.T) *EditorUI {
	t.Helper()
	ui := NewEditorUI(newStubBackend(), nil, Config{AppVersion: "test"})
	ui.syncJobs = true
	ui.InitOnStartup()
	return ui
}

func TestResizeModalAspectLockAndUpscaleConfirm(t *testing.T) {
	ui := newTestEditorUI(t)
	scan := writeTestScanPNG(t, "main.png", 240, 180)
	ui.handleModeSwitch("normal")
	ui.loadFile(scan, false)
	ui.handleSkipCrop()

	ui.resizeState.open(ui)
	ui.resizeState.setWidth(480)
	if ui.resizeState.height != 360 || ui.resizeState.scalePct != 200 {
		t.Fatalf("aspect-locked resize = (%d,%d @ %d%%), want (480,360 @ 200%%)",
			ui.resizeState.width, ui.resizeState.height, ui.resizeState.scalePct)
	}
	ui.resizeState.setWidth(2000)
	ui.resizeState.tryApply(ui)
	if ui.confirmDialog == nil {
		t.Fatal("expected 2M-pixel upscale confirmation dialog")
	}
}

func TestHeadlessLayoutAllModesAccordionsAndModals(t *testing.T) {
	ui := newTestEditorUI(t)
	scan := writeTestScanPNG(t, "layout.png", 240, 180)

	renderFrame := func(name string) {
		t.Helper()
		var ops op.Ops
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1280, 900)),
			Now:         time.Now(),
		}
		dims := ui.layoutRoot(gtx)
		if dims.Size != image.Pt(1280, 900) {
			t.Fatalf("%s: root dims = %v, want (1280,900)", name, dims.Size)
		}
	}

	// 1. Unloaded initial view
	renderFrame("unloaded")

	// 2. Loaded across all 4 modes with open accordions & sub-tools
	ui.loadFile(scan, false)
	ui.accAdjustmentsOpen = true
	ui.accToolsOpen = true
	ui.accShortcutsOpen = true
	ui.useDescreenTool = true
	ui.useDustRemovalTool = true
	ui.useTouchupTool = true
	ui.adjustmentSelectionActive = true
	ui.adjustmentRect = &rectI{X1: 10, Y1: 10, X2: 100, Y2: 80}
	ui.touchupCursor = &touchupCursorState{X: 50, Y: 50}
	renderFrame("corner_accordions")

	ui.handleModeSwitch("disc")
	ui.discActive = true
	ui.discCenter = image.Pt(120, 90)
	ui.discRadius = 60
	ui.discInitialRadius = 60
	ui.settings.DiscCenterCutout = true
	ui.accAdjustmentsOpen = true
	ui.accShortcutsOpen = true
	renderFrame("disc_active")

	ui.handleModeSwitch("line")
	ui.lines = []lineI{{X1: 20, Y1: 20, X2: 220, Y2: 25}, {X1: 220, Y1: 25, X2: 215, Y2: 160}}
	renderFrame("line_mode")

	ui.handleModeSwitch("normal")
	ui.normalRect = &rectI{X1: 15, Y1: 15, X2: 180, Y2: 140}
	renderFrame("normal_mode")

	// 3. Every open modal
	ui.errorMessage = "Sample error message"
	renderFrame("error_modal")
	ui.errorMessage = ""

	ui.confirmDialog = &confirmDialogState{
		Title: "Confirm", Message: "Save changes?", YesLabel: "Save", NoLabel: "Don't save", CancelLabel: "Cancel",
		YesVariant: btnPrimary, NoVariant: btnDanger,
	}
	renderFrame("confirm_modal")
	ui.confirmDialog = nil

	ui.aboutOpen = true
	renderFrame("about_modal")
	ui.aboutOpen = false

	ui.resizeState.open(ui)
	renderFrame("resize_modal")
	ui.resizeOpen = false

	ui.optionsState.open(ui)
	for _, tab := range optionsTabs {
		ui.optionsState.activeTab = tab.id
		renderFrame("options_" + tab.id)
	}
	ui.optionsOpen = false

	ui.compositorState.open(ui)
	ui.compositorState.paths = []string{scan, scan}
	ui.compositorState.orientation = "ltr"
	renderFrame("compositor_stitch")
	ui.compositorState.orientation = "overlay"
	ui.compositorState.alignedLayers = 2
	ui.compositorState.previewURL = ui.preview
	ui.compositorState.previewDims = ui.realImageDims
	ui.compositorState.maskStroke = []f32.Point{f32.Pt(0.2, 0.2), f32.Pt(0.5, 0.5)}
	renderFrame("compositor_overlay")
	ui.compositorOpen = false

	ui.calibState.open(ui)
	ui.calibState.addPaths([]string{scan}, ui)
	ui.calibState.corners = []CalibrationPoint{{X: 24, Y: 20}, {X: 215, Y: 20}}
	renderFrame("calibration_modal")
	ui.calibState.closeNow(ui)
}

func TestModalCardClickDoesNotCloseOptions(t *testing.T) {
	ui := newTestEditorUI(t)

	if &modalScrimTag == &modalCardStopTag {
		t.Fatal("modalScrimTag and modalCardStopTag alias to the same address")
	}
	vp := ui.mainViewport
	if &vp.canvasTag == &vp.hScrollTag || &vp.canvasTag == &vp.vScrollTag || &vp.hScrollTag == &vp.vScrollTag {
		t.Fatal("viewport pointer tags alias to the same address")
	}

	var router input.Router
	var ops op.Ops
	frame := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Source:      router.Source(),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1280, 900)),
			Now:         time.Now(),
		}
		ui.layoutRoot(gtx)
		router.Frame(&ops)
	}

	ui.optionsState.open(ui)
	if !ui.optionsOpen {
		t.Fatal("expected optionsOpen=true after open")
	}
	frame()

	// Click inside the Options card background (header area around centered 540x480 card)
	router.Queue(
		pointer.Event{
			Kind:     pointer.Press,
			Source:   pointer.Mouse,
			Buttons:  pointer.ButtonPrimary,
			Position: f32.Pt(640, 225),
		},
		pointer.Event{
			Kind:     pointer.Release,
			Source:   pointer.Mouse,
			Position: f32.Pt(640, 225),
		},
	)
	frame()
	if !ui.optionsOpen {
		t.Fatal("clicking inside Options card background unexpectedly closed the modal")
	}

	// Click outside the Options card on the dark backdrop scrim
	router.Queue(
		pointer.Event{
			Kind:     pointer.Press,
			Source:   pointer.Mouse,
			Buttons:  pointer.ButtonPrimary,
			Position: f32.Pt(20, 20),
		},
		pointer.Event{
			Kind:     pointer.Release,
			Source:   pointer.Mouse,
			Position: f32.Pt(20, 20),
		},
	)
	frame()
	if ui.optionsOpen {
		t.Fatal("clicking backdrop scrim did not close Options modal")
	}
}

func TestDustRemovalDropdownOpensAndSelectsOption(t *testing.T) {
	ui := newTestEditorUI(t)
	ui.dustLevel = "medium"
	opts := []uiSelectOption{
		{Value: "low", Label: "Low"},
		{Value: "medium", Label: "Medium"},
		{Value: "high", Label: "High"},
	}

	var router input.Router
	var ops op.Ops
	frame := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Source:      router.Source(),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(300, 400)),
			Now:         time.Now(),
		}
		ui.overlays.beginFrame(gtx)
		layout.Inset{Top: unit.Dp(10), Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			cg := gtx
			cg.Constraints.Min = image.Pt(160, 0)
			cg.Constraints.Max.X = 160
			return ui.ddDustLevel.Layout(cg, ui.th, ui.overlays, opts, ui.dustLevel, true, func(val string) {
				ui.dustLevel = val
			})
		})
		ui.overlays.layoutOverlayPass(gtx)
		router.Frame(&ops)
	}

	frame()

	// Click trigger button to open dropdown.
	router.Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(60, 22)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(60, 22)},
	)
	frame()
	frame()
	if !ui.ddDustLevel.open {
		t.Fatal("expected Dust removal dropdown to open when clicked")
	}

	// Click first popup option ("Low") below anchorPos.
	optPt := f32.Pt(ui.ddDustLevel.anchorPos.X+40, ui.ddDustLevel.anchorPos.Y+14)
	router.Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: optPt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: optPt},
	)
	frame()
	if ui.ddDustLevel.open {
		t.Fatal("expected dropdown to close after selecting an option")
	}
	if ui.dustLevel != "low" {
		t.Fatalf("dustLevel = %q, want %q", ui.dustLevel, "low")
	}

	// Re-open and click outside to dismiss without changing selection.
	router.Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(60, 22)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(60, 22)},
	)
	frame()
	frame()
	if !ui.ddDustLevel.open {
		t.Fatal("expected Dust removal dropdown to re-open on second click")
	}
	router.Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(250, 300)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(250, 300)},
	)
	frame()
	if ui.ddDustLevel.open {
		t.Fatal("expected outside click to dismiss dropdown")
	}
	if ui.dustLevel != "low" {
		t.Fatalf("dustLevel changed on dismiss to %q, want %q", ui.dustLevel, "low")
	}
}

func TestAdjustmentsSelectionHeaderIconDoesNotCollapseAccordion(t *testing.T) {
	ui := newTestEditorUI(t)
	scan := writeTestScanPNG(t, "acc_select.png", 200, 160)
	ui.loadFile(scan, false)
	ui.accAdjustmentsOpen = true

	var router input.Router
	var ops op.Ops
	frame := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Source:      router.Source(),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(320, 400)),
			Now:         time.Now(),
		}
		ui.overlays.beginFrame(gtx)
		cg := gtx
		cg.Constraints.Min = image.Pt(320, 0)
		ui.layoutAdjustmentsAccordion(cg, 300)
		ui.overlays.layoutOverlayPass(gtx)
		router.Frame(&ops)
	}

	clickAt := func(pt f32.Point) {
		router.Queue(
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
		)
		frame()
		frame()
	}

	frame()

	// Pre-crop: selection icon is disabled; clicking it must not collapse the accordion.
	iconPt := f32.Pt(277, 21)
	clickAt(iconPt)
	if !ui.accAdjustmentsOpen || ui.adjustmentSelectionActive {
		t.Fatalf("pre-crop click on selection icon changed state unexpectedly: open=%v sel=%v",
			ui.accAdjustmentsOpen, ui.adjustmentSelectionActive)
	}

	// Post-crop: selection icon toggles selection mode without closing Adjustments.
	ui.handleSkipCrop()
	frame()

	clickAt(iconPt)
	if !ui.adjustmentSelectionActive || !ui.accAdjustmentsOpen {
		t.Fatalf("expected selection active and accordion open after first icon click, got sel=%v open=%v",
			ui.adjustmentSelectionActive, ui.accAdjustmentsOpen)
	}

	clickAt(iconPt)
	if ui.adjustmentSelectionActive || !ui.accAdjustmentsOpen {
		t.Fatalf("expected selection inactive and accordion open after second icon click, got sel=%v open=%v",
			ui.adjustmentSelectionActive, ui.accAdjustmentsOpen)
	}

	// Clicking the title closes the accordion.
	clickAt(f32.Pt(40, 21))
	if ui.accAdjustmentsOpen {
		t.Fatal("expected clicking accordion title to close Adjustments")
	}

	// Clicking the arrow re-opens the accordion.
	clickAt(f32.Pt(304, 21))
	if !ui.accAdjustmentsOpen {
		t.Fatal("expected clicking accordion arrow to reopen Adjustments")
	}
}

func TestAccordionCloseNeverPopsHeightWithMultipleOpenPanels(t *testing.T) {
	ui := newTestEditorUI(t)
	scan := writeTestScanPNG(t, "acc_close_pop.png", 200, 160)
	ui.loadFile(scan, false)
	ui.handleSkipCrop()
	ui.handleModeSwitch("disc")
	ui.applyDrawDisc(100, 80, 40)
	ui.lastWindowSz = image.Pt(1400, 900)
	ui.accAdjustmentsOpen = true
	ui.accToolsOpen = false
	ui.accShortcutsOpen = true

	var router input.Router
	var ops op.Ops
	now := time.Now()
	maxAccHForTest := func() int {
		openCount := 0
		if ui.accAdjustmentsOpen {
			openCount++
		}
		if ui.accToolsOpen {
			openCount++
		}
		if ui.accShortcutsOpen {
			openCount++
		}
		h := int(0.30 * float64(ui.lastWindowSz.Y))
		switch openCount {
		case 2:
			h = int(0.18 * float64(ui.lastWindowSz.Y))
		case 3:
			h = int(0.12 * float64(ui.lastWindowSz.Y))
		}
		return max(h, 110)
	}
	stepFrame := func() (adjH, shH int) {
		now = now.Add(16 * time.Millisecond)
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Source:      router.Source(),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(360, 860)),
			Now:         now,
		}
		ui.overlays.beginFrame(gtx)
		cg := gtx
		cg.Constraints.Min = image.Pt(320, 0)
		cg.Constraints.Max = image.Pt(320, 860)
		maxH := maxAccHForTest()
		adjH = ui.layoutAdjustmentsAccordion(cg, maxH).Size.Y
		shH = ui.layoutShortcutsAccordion(cg, maxH).Size.Y
		ui.overlays.layoutOverlayPass(gtx)
		router.Frame(&ops)
		return adjH, shH
	}

	// Render initial settled open state (2 panels open -> maxAccH = 162px).
	prevAdjH, prevShH := stepFrame()
	if prevAdjH <= 50 || prevShH <= 50 {
		t.Fatalf("expected both panels open on first frame, got adjH=%d shH=%d", prevAdjH, prevShH)
	}

	// Close Adjustments while Shortcuts remains open (openCount drops 2 -> 1, maxAccH jumps 162 -> 270).
	ui.accAdjustmentsOpen = false
	var sawShSmoothStep bool
	for i := 0; i < 40; i++ {
		adjH, shH := stepFrame()
		if adjH > prevAdjH {
			t.Fatalf("frame %d: Adjustments popped taller on close: prev=%d cur=%d", i, prevAdjH, adjH)
		}
		if shH < prevShH {
			t.Fatalf("frame %d: Shortcuts shrank while expanding: prev=%d cur=%d", i, prevShH, shH)
		}
		if shH > prevShH && ui.accShortcuts.animating {
			sawShSmoothStep = true
		}
		prevAdjH, prevShH = adjH, shH
	}
	if ui.accAdjustments.displayH != 0 || ui.accAdjustments.animating {
		t.Fatalf("expected Adjustments settled closed, got displayH=%v animating=%v",
			ui.accAdjustments.displayH, ui.accAdjustments.animating)
	}
	if !sawShSmoothStep {
		t.Fatal("expected Shortcuts to expand smoothly across frames after Adjustments closed")
	}

	// Close Shortcuts from settled expanded state and ensure strictly monotonic shrink.
	ui.accShortcutsOpen = false
	for i := 0; i < 40; i++ {
		_, shH := stepFrame()
		if shH > prevShH {
			t.Fatalf("frame %d: Shortcuts popped taller on close: prev=%d cur=%d", i, prevShH, shH)
		}
		prevShH = shH
	}
	if ui.accShortcuts.displayH != 0 || ui.accShortcuts.animating {
		t.Fatalf("expected Shortcuts settled closed, got displayH=%v animating=%v",
			ui.accShortcuts.displayH, ui.accShortcuts.animating)
	}
}

func TestSidebarAndAccordionContentHorizontallyCentered(t *testing.T) {
	ui := newTestEditorUI(t)
	var ops op.Ops

	// 1. Slideout accordion content gets equal 16px left inset and 16px right gutter
	// (6px inner inset + 10px Occupy scrollbar), so 320px card gives 288px body width.
	var acc uiAccordion
	var bodyMaxX int
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(320, 400)),
		Now:         time.Now(),
	}
	acc.Layout(gtx, ui.th, "Shortcuts", true, 240, nil, nil, func(gtx layout.Context) layout.Dimensions {
		bodyMaxX = gtx.Constraints.Max.X
		return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 120)}
	})
	if bodyMaxX != 288 {
		t.Fatalf("accordion body max width = %d, want 288 (16px left + 16px right gutter)", bodyMaxX)
	}

	// 2. Main sidebar middle mode area gives controls the same centered 320px width
	// (360px sidebar - 20px left - 20px right) both with and without scrolling.
	ui.handleModeSwitch("normal")
	measureMiddle := func(availH int) int {
		ops.Reset()
		mg := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(360, availH)),
			Now:         time.Now(),
		}
		var modeW int
		// Mirror layoutMiddleModeArea calling normal/line/corner panel constraints.
		if ui.modePanelH > 0 && ui.modePanelH <= availH {
			cg := mg
			cg.Constraints.Min.Y = 0
			cg.Constraints.Max.Y = availH
			dims := layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(cg, func(gtx layout.Context) layout.Dimensions {
				modeW = gtx.Constraints.Max.X
				return layout.Dimensions{Size: image.Pt(modeW, 80)}
			})
			ui.modePanelH = dims.Size.Y
		} else {
			layout.Inset{Right: unit.Dp(4)}.Layout(mg, func(gtx layout.Context) layout.Dimensions {
				return styledList(ui.th, &ui.sidebarList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						modeW = gtx.Constraints.Max.X
						return layout.Dimensions{Size: image.Pt(modeW, 200)}
					})
				})
			})
		}
		return modeW
	}

	scrollW := measureMiddle(120)
	if scrollW != 320 {
		t.Fatalf("scrolling middle area content width = %d, want 320", scrollW)
	}
	ui.modePanelH = 80
	noScrollW := measureMiddle(300)
	if noScrollW != 320 {
		t.Fatalf("non-scrolling middle area content width = %d, want 320", noScrollW)
	}

	// Exercise the actual EditorUI.layoutMiddleModeArea in both overflowing and fitted heights.
	ui.modePanelH = 0
	overflowGtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(360, 120)),
		Now:         time.Now(),
	}
	ui.layoutMiddleModeArea(overflowGtx)
	if ui.modePanelH <= 0 {
		t.Fatal("expected layoutMiddleModeArea to measure modePanelH")
	}
	fitGtx := overflowGtx
	fitGtx.Constraints = layout.Exact(image.Pt(360, ui.modePanelH+100))
	ui.layoutMiddleModeArea(fitGtx)
}

func TestCompositorModalLayoutCompactHeaderRowsAndSpacer(t *testing.T) {
	ui := newTestEditorUI(t)
	var ops op.Ops

	spacerGtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Min: image.Pt(250, 0), Max: image.Pt(250, 40)},
	}
	if d := flexSpacer(spacerGtx); d.Size.X != 250 {
		t.Fatalf("flexSpacer width = %d, want 250", d.Size.X)
	}

	c := &ui.compositorState
	c.ensureInit()

	renderModal := func(orientation string, paths []string) {
		c.orientation = orientation
		c.paths = paths
		ui.compositorOpen = true
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1200, 900)),
			Now:         time.Now(),
		}
		ui.overlays.beginFrame(gtx)
		c.layout(gtx, ui)
		ui.overlays.layoutOverlayPass(gtx)
	}

	// Render empty Stitch mode, populated Stitch mode, and Overlay mode cleanly.
	renderModal("ltr", nil)
	renderModal("ltr", []string{filepath.Join("scans", "scan_01.png"), filepath.Join("scans", "scan_02.png")})
	renderModal("overlay", []string{filepath.Join("scans", "scan_01.png"), filepath.Join("scans", "scan_02.png")})

	// Measure preview pane with tight Min.X=584 (matching layoutCenteredModal card inner width):
	// rotate buttons must remain compact beside a single-line Preview label (never wrapping 1 char/line).
	ops.Reset()
	tightPaneGtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(584, 300)),
		Now:         time.Now(),
	}
	paneDims := c.layoutStitchPreviewPane(tightPaneGtx, ui)
	if paneDims.Size != image.Pt(584, 300) {
		t.Fatalf("stitch preview pane dims = %v, want 584x300", paneDims.Size)
	}
	// If rotBtn had captured tight Min.X=584, LastLayout.ViewportH would collapse to <= 160px.
	if c.viewport.LastLayout.ViewportH < 240 || c.viewport.LastLayout.ViewportW > 584 {
		t.Fatalf("expected compact rotate header leaving >=240px for viewport (<=584w), got %dx%d",
			c.viewport.LastLayout.ViewportW, c.viewport.LastLayout.ViewportH)
	}

	// Measure file list section with tight Min.X=584 to verify compact item rows.
	ops.Reset()
	c.orientation = "ltr"
	c.paths = []string{filepath.Join("scans", "scan_01.png"), filepath.Join("scans", "scan_02.png")}
	tightFilesGtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(584, 185)),
		Now:         time.Now(),
	}
	filesDims := c.layoutFilesSection(tightFilesGtx, ui)
	if filesDims.Size != image.Pt(584, 185) {
		t.Fatalf("files section dims = %v, want 584x185", filesDims.Size)
	}
}

func TestSkipCropThenResetButtonFlow(t *testing.T) {
	ui := newTestEditorUI(t)
	scan := writeTestScanPNG(t, "skip_reset.png", 200, 160)
	ui.loadFile(scan, false)
	if !ui.cornersDetected || ui.cropSkipped || ui.canRecrop {
		t.Fatalf("unexpected initial state: cornersDetected=%v cropSkipped=%v canRecrop=%v",
			ui.cornersDetected, ui.cropSkipped, ui.canRecrop)
	}

	var router input.Router
	var ops op.Ops
	frame := func() layout.Dimensions {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Source:      router.Source(),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Min: image.Pt(280, 0), Max: image.Pt(280, 300)},
			Now:         time.Now(),
		}
		ui.overlays.beginFrame(gtx)
		dims := ui.layoutActionButtons(gtx)
		ui.overlays.layoutOverlayPass(gtx)
		router.Frame(&ops)
		return dims
	}
	clickAt := func(pt f32.Point) {
		router.Queue(
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
		)
		frame()
		frame()
	}

	preDims := frame()
	if preDims.Size.Y < 55 {
		t.Fatalf("expected Detect + Skip crop rows >= 55px tall, got %v", preDims.Size)
	}

	// Click "Skip crop" (second row centered around x=140, y=50).
	clickAt(f32.Pt(140, 50))
	if !ui.cropSkipped || !ui.canRecrop || ui.cornersDetected {
		t.Fatalf("after Skip crop click: cropSkipped=%v canRecrop=%v cornersDetected=%v, want true/true/false",
			ui.cropSkipped, ui.canRecrop, ui.cornersDetected)
	}
	if ui.errorMessage != "" {
		t.Fatalf("unexpected error after Skip crop click: %q", ui.errorMessage)
	}

	// Click "Reset" (right half of second row around x=212, y=50) and verify immediate return to pre-crop.
	clickAt(f32.Pt(212, 50))
	if ui.cropSkipped || ui.canRecrop || !ui.cornersDetected {
		t.Fatalf("after Reset click: cropSkipped=%v canRecrop=%v cornersDetected=%v, want false/false/true",
			ui.cropSkipped, ui.canRecrop, ui.cornersDetected)
	}
	if ui.errorMessage != "" || ui.confirmDialog != nil {
		t.Fatalf("unexpected error/confirm dialog after Reset click: err=%q confirm=%v", ui.errorMessage, ui.confirmDialog != nil)
	}
}

type gatedStubBackend struct {
	*stubBackend
	gate chan struct{}
}

func (g *gatedStubBackend) waitGate() {
	if g.gate != nil {
		<-g.gate
	}
}

func (g *gatedStubBackend) LoadImage(req LoadImageRequest) (*ImageInfo, error) {
	g.waitGate()
	return g.stubBackend.LoadImage(req)
}

func (g *gatedStubBackend) ResetCorners() (*ProcessResult, error) {
	g.waitGate()
	return g.stubBackend.ResetCorners()
}

func (g *gatedStubBackend) ResetDisc() (*ProcessResult, error) {
	g.waitGate()
	return g.stubBackend.ResetDisc()
}

func (g *gatedStubBackend) ClearLines() (*ProcessResult, error) {
	g.waitGate()
	return g.stubBackend.ClearLines()
}

func (g *gatedStubBackend) ResetNormal() (*ProcessResult, error) {
	g.waitGate()
	return g.stubBackend.ResetNormal()
}

func TestModeSwitchHasNoLoadingFlickerOrSidebarLayoutJump(t *testing.T) {
	scan := writeTestScanPNG(t, "modeswitch_flicker.png", 240, 180)
	gb := &gatedStubBackend{stubBackend: newStubBackend()}
	ui := NewEditorUI(gb, nil, Config{AppVersion: "test"})
	ui.InitOnStartup()

	flushBackend := func() {
		t.Helper()
		done := make(chan struct{})
		ui.backendJobs <- func() { close(done) }
		<-done
		ui.drainUIQueue()
	}

	// 1. Full document load sets loadingFull=true while in flight and clears it once applied.
	loadGate := make(chan struct{})
	gb.gate = loadGate
	ui.loadFile(scan, false)
	if !ui.loadingFull || !ui.loading || !ui.isBusy() {
		t.Fatalf("expected loadingFull=true while loadFile is in flight: loadingFull=%v loading=%v busy=%v",
			ui.loadingFull, ui.loading, ui.isBusy())
	}
	close(loadGate)
	flushBackend()
	flushBackend()
	if ui.loadingFull || ui.loading || ui.isBusy() || !ui.imageLoaded || !ui.cornersDetected {
		t.Fatalf("expected loadFile to clear loadingFull and populate corners: loadingFull=%v loading=%v busy=%v loaded=%v corners=%v",
			ui.loadingFull, ui.loading, ui.isBusy(), ui.imageLoaded, ui.cornersDetected)
	}

	// Render one full root frame so FitWidth is initialized and measure Corner panel height.
	var ops op.Ops
	renderRoot := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1280, 900)),
			Now:         time.Now(),
		}
		ui.layoutRoot(gtx)
	}
	renderMiddleAt := func(h int) int {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(360, h)),
			Now:         time.Now(),
		}
		ui.overlays.beginFrame(gtx)
		ui.layoutMiddleModeArea(gtx)
		ui.overlays.layoutOverlayPass(gtx)
		return ui.modePanelH
	}

	renderRoot()
	savedFitW := ui.mainViewport.FitWidth
	if savedFitW <= 0 {
		t.Fatalf("expected FitWidth > 0 after initial render, got %v", savedFitW)
	}
	cornerMiddleH := renderMiddleAt(220)
	if cornerMiddleH <= 220 {
		t.Fatalf("expected corner mode panel height > 220 at 360px width, got %d", cornerMiddleH)
	}

	// 2. Switching across all modes asynchronously never flashes loadingFull, loading, isBusy, or zero FitWidth,
	// updates sidebar modePanelH on the very first frame, and restores cached corners immediately on return.
	for _, nextMode := range []string{"disc", "line", "normal", "corner"} {
		gate := make(chan struct{})
		gb.gate = gate
		ui.handleModeSwitch(nextMode)

		if ui.loadingFull || ui.loading || ui.isBusy() {
			close(gate)
			flushBackend()
			t.Fatalf("switch to %s unexpectedly set busy/loading overlay: loadingFull=%v loading=%v busy=%v",
				nextMode, ui.loadingFull, ui.loading, ui.isBusy())
		}
		if ui.mainViewport.FitWidth <= 0 {
			close(gate)
			flushBackend()
			t.Fatalf("switch to %s zeroed FitWidth synchronously", nextMode)
		}
		if nextMode == "corner" && (!ui.cornersDetected || len(ui.detectedCornerPts) == 0) {
			close(gate)
			flushBackend()
			t.Fatalf("switch back to corner did not synchronously restore cached corner dots")
		}

		// Sidebar middle area must reflect the new mode's height on the very first frame (no 1-frame jump).
		immediateH := renderMiddleAt(220)
		if nextMode == "disc" && (immediateH <= 0 || immediateH > 220 || immediateH == cornerMiddleH) {
			close(gate)
			flushBackend()
			t.Fatalf("switch to disc kept stale sidebar height: got %d (corner was %d)", immediateH, cornerMiddleH)
		}
		if nextMode == "corner" && immediateH != cornerMiddleH {
			close(gate)
			flushBackend()
			t.Fatalf("switch back to corner had wrong first-frame height %d, want %d", immediateH, cornerMiddleH)
		}

		renderRoot()
		close(gate)
		flushBackend()
	}
}

func TestCompositorAcceptsDroppedImageFiles(t *testing.T) {
	ui := newTestEditorUI(t)
	scan1 := writeTestScanPNG(t, "drop1.png", 120, 90)
	scan2 := writeTestScanPNG(t, "drop2.jpg", 120, 90)
	scan3 := writeTestScanPNG(t, "drop3.png", 120, 90)

	ui.compositorState.open(ui)
	ui.handleDroppedFiles([]string{scan1, "notes.txt", scan2})

	got := ui.compositorState.paths
	if len(got) != 2 || got[0] != scan1 || got[1] != scan2 {
		t.Fatalf("compositor paths = %v, want [%q %q]", got, scan1, scan2)
	}
	if ui.compositorState.statusMsg != "2 image(s) queued" {
		t.Fatalf("compositor statusMsg = %q, want %q", ui.compositorState.statusMsg, "2 image(s) queued")
	}
	if ui.imageLoaded {
		t.Fatal("dropping files into open compositor unexpectedly loaded main editor image")
	}

	// Drops while compositor is busy must be ignored.
	ui.compositorState.busy = true
	ui.handleDroppedFiles([]string{scan3})
	if len(ui.compositorState.paths) != 2 {
		t.Fatalf("expected busy compositor to ignore drop, got %d paths", len(ui.compositorState.paths))
	}
	ui.compositorState.busy = false

	// Closing compositor resumes routing drops to the main editor.
	ui.compositorOpen = false
	ui.handleDroppedFiles([]string{scan1})
	if !ui.imageLoaded {
		t.Fatal("expected dropping file with compositor closed to load main editor image")
	}
}

func TestCompositorOverlayReducedPreviewFillsStage(t *testing.T) {
	ui := newTestEditorUI(t)
	sb := ui.app.(*stubBackend)
	reducedDims := image.Pt(102, 140)
	reducedImg := image.NewNRGBA(image.Rectangle{Max: reducedDims})
	url := sb.registerPreview(reducedImg)

	c := &ui.compositorState
	c.ensureInit()
	c.orientation = "overlay"
	c.alignedLayers = 2
	c.paths = []string{"scan1.png", "scan2.png"}
	c.previewURL = url
	c.previewDims = image.Pt(510, 700)

	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(760, 620)),
		Now:         time.Now(),
	}
	ui.overlays.beginFrame(gtx)
	c.layoutOverlayPreviewPane(gtx, ui)
	ui.overlays.layoutOverlayPass(gtx)

	if c.viewport.activeEntry == nil {
		t.Fatal("expected compositor viewport activeEntry after layoutOverlayPreviewPane")
	}
	if c.viewport.activeEntry.dims != reducedDims {
		t.Fatalf("compositor viewport activeEntry.dims = %v, want %v", c.viewport.activeEntry.dims, reducedDims)
	}
	wantRect := image.Rectangle{Max: reducedDims}
	if c.viewport.activeEntry.rect != wantRect {
		t.Fatalf("compositor viewport activeEntry.rect = %v, want %v", c.viewport.activeEntry.rect, wantRect)
	}
}

func TestCompositorReorderAndRemovePointerClicks(t *testing.T) {
	runMode := func(orientation string) {
		ui := newTestEditorUI(t)
		sb := ui.app.(*stubBackend)
		previewURL := sb.registerPreview(image.NewNRGBA(image.Rect(0, 0, 120, 90)))
		scanA := filepath.Join("scans", "aeternam moongod 2026-11-19-0001.tif")
		scanB := filepath.Join("scans", "aeternam moongod 2026-11-19-0002.tif")
		scanC := filepath.Join("scans", "aeternam moongod 2026-11-19-0003.tif")
		c := &ui.compositorState
		c.open(ui)
		c.orientation = orientation
		c.paths = []string{scanA, scanB, scanC}
		c.previewURL = previewURL
		c.previewDims = image.Pt(120, 90)
		if orientation == "overlay" {
			c.alignedLayers = 3
			c.activeLayer = 2
		}

		var router input.Router
		var ops op.Ops
		frame := func() {
			ops.Reset()
			gtx := layout.Context{
				Ops:         &ops,
				Source:      router.Source(),
				Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Exact(image.Pt(1280, 900)),
				Now:         time.Now(),
			}
			ui.layoutRoot(gtx)
			router.Frame(&ops)
		}

		findButtons := func(label string) []image.Rectangle {
			nodes := router.AppendSemantics(nil)
			seen := make(map[image.Rectangle]bool)
			var rects []image.Rectangle
			var walk func([]input.SemanticNode)
			walk = func(ns []input.SemanticNode) {
				for _, n := range ns {
					if n.Desc.Gestures == input.ClickGesture && !n.Desc.Bounds.Empty() && !seen[n.Desc.Bounds] {
						matched := n.Desc.Label == label
						if !matched {
							for _, child := range n.Children {
								if child.Desc.Label == label {
									matched = true
									break
								}
							}
						}
						if matched {
							seen[n.Desc.Bounds] = true
							rects = append(rects, n.Desc.Bounds)
						}
					}
					if len(n.Children) > 0 {
						walk(n.Children)
					}
				}
			}
			walk(nodes)
			sort.Slice(rects, func(i, j int) bool {
				return rects[i].Min.Y < rects[j].Min.Y
			})
			return rects
		}

		clickRect := func(r image.Rectangle) {
			pt := f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2))
			router.Queue(
				pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
				pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
				pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
			)
			frame()
			frame()
		}

		frame()

		downRects := findButtons("▼")
		if len(downRects) != 2 {
			t.Fatalf("%s: expected 2 enabled ▼ semantic rects for 3 rows, got %v", orientation, downRects)
		}
		upRects := findButtons("▲")
		if len(upRects) != 2 {
			t.Fatalf("%s: expected 2 enabled ▲ semantic rects for 3 rows, got %v", orientation, upRects)
		}
		delRects := findButtons("✕")
		if len(delRects) != 3 {
			t.Fatalf("%s: expected 3 ✕ semantic rects, got %v", orientation, delRects)
		}

		// Clicking disabled ▲ on row 0 (just left of row 0 ▼) and disabled ▼ on last row (between last ▲ and last ✕) must do nothing.
		row0DisabledUp := downRects[0].Sub(image.Pt(downRects[0].Dx()+3, 0))
		clickRect(row0DisabledUp)
		if c.paths[0] != scanA || c.paths[1] != scanB || c.paths[2] != scanC || c.previewURL == "" {
			t.Fatalf("%s: clicking disabled row 0 ▲ altered state: paths=%v preview=%q", orientation, c.paths, c.previewURL)
		}
		lastRowDisabledDown := image.Rect(upRects[1].Max.X+3, delRects[2].Min.Y, delRects[2].Min.X-3, delRects[2].Max.Y)
		clickRect(lastRowDisabledDown)
		if c.paths[0] != scanA || c.paths[1] != scanB || c.paths[2] != scanC || c.previewURL == "" {
			t.Fatalf("%s: clicking disabled last-row ▼ altered state: paths=%v preview=%q", orientation, c.paths, c.previewURL)
		}

		// Click Row 0 ▼ -> swaps scanA/scanB and invalidates stale preview/alignment.
		clickRect(downRects[0])
		if len(c.paths) != 3 || c.paths[0] != scanB || c.paths[1] != scanA || c.paths[2] != scanC {
			t.Fatalf("%s: after clicking row 0 ▼ (%v), paths = %v, want [%q %q %q]", orientation, downRects[0], c.paths, scanB, scanA, scanC)
		}
		if c.previewURL != "" || c.alignedLayers != 0 {
			t.Fatalf("%s: expected reorder to clear stale preview/alignment, got preview=%q alignedLayers=%d", orientation, c.previewURL, c.alignedLayers)
		}
		wantMsg := `Order updated — click "Stitch images" to update`
		if orientation == "overlay" {
			wantMsg = `Order updated — click "Align scans" to update`
		}
		if c.statusMsg != wantMsg {
			t.Fatalf("%s: reorder statusMsg = %q, want %q", orientation, c.statusMsg, wantMsg)
		}

		// Click Row 1 ▲ -> swaps back.
		upRects = findButtons("▲")
		if len(upRects) != 2 {
			t.Fatalf("%s: expected 2 enabled ▲ semantic rects after reorder, got %v", orientation, upRects)
		}
		clickRect(upRects[0])
		if len(c.paths) != 3 || c.paths[0] != scanA || c.paths[1] != scanB || c.paths[2] != scanC {
			t.Fatalf("%s: after clicking row 1 ▲ (%v), paths = %v, want [%q %q %q]", orientation, upRects[0], c.paths, scanA, scanB, scanC)
		}

		// Re-populate stale preview and remove last row (✕) -> clears preview/alignment and clamps activeLayer.
		c.previewURL = previewURL
		if orientation == "overlay" {
			c.alignedLayers = 3
			c.activeLayer = 2
		}
		frame()
		delRects = findButtons("✕")
		if len(delRects) != 3 {
			t.Fatalf("%s: expected 3 ✕ semantic rects before remove, got %v", orientation, delRects)
		}
		clickRect(delRects[2])
		if len(c.paths) != 2 || c.paths[0] != scanA || c.paths[1] != scanB {
			t.Fatalf("%s: after clicking row 2 ✕, paths = %v, want [%q %q]", orientation, c.paths, scanA, scanB)
		}
		if c.previewURL != "" || c.alignedLayers != 0 {
			t.Fatalf("%s: expected remove to clear preview/alignment, got preview=%q alignedLayers=%d", orientation, c.previewURL, c.alignedLayers)
		}
		if orientation == "overlay" && c.activeLayer != 1 {
			t.Fatalf("overlay: expected activeLayer clamped to 1 after removing row 2, got %d", c.activeLayer)
		}
		if c.statusMsg != "2 image(s) queued" {
			t.Fatalf("%s: remove statusMsg = %q, want %q", orientation, c.statusMsg, "2 image(s) queued")
		}
	}

	runMode("ltr")
	runMode("overlay")
}

func TestCompositorMiddleTruncatePreservesDistinguishingSuffix(t *testing.T) {
	ui := newTestEditorUI(t)
	var ops op.Ops
	gtx := layout.Context{
		Ops:    &ops,
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:    time.Now(),
	}

	paths := []string{
		filepath.Join("scans", "aeternam moongod 2026-11-19-0001.tif"),
		filepath.Join("scans", "aeternam moongod 2026-11-19-0002.tif"),
	}
	const maxWidth = 170
	out1 := middleTruncateLabel(gtx, ui.th, unit.Sp(12), maxWidth, "Ref. ", compositorDisplayName(paths, 0))
	out2 := middleTruncateLabel(gtx, ui.th, unit.Sp(12), maxWidth, "2. ", compositorDisplayName(paths, 1))

	if out1 == out2 {
		t.Fatalf("expected middle-truncated labels to differ, both were %q", out1)
	}
	if !strings.Contains(out1, "0001.tif") {
		t.Fatalf("expected first label %q to preserve suffix 0001.tif", out1)
	}
	if !strings.Contains(out2, "0002.tif") {
		t.Fatalf("expected second label %q to preserve suffix 0002.tif", out2)
	}
	if w1 := measureLabelWidth(gtx, ui.th, unit.Sp(12), out1); w1 > maxWidth {
		t.Fatalf("first label %q width %d exceeds budget %d", out1, w1, maxWidth)
	}
	if w2 := measureLabelWidth(gtx, ui.th, unit.Sp(12), out2); w2 > maxWidth {
		t.Fatalf("second label %q width %d exceeds budget %d", out2, w2, maxWidth)
	}

	// Duplicate basenames from different directories include their parent folder name.
	dupPaths := []string{
		filepath.Join("rollA", "scan_01.tif"),
		filepath.Join("rollB", "scan_01.tif"),
	}
	if got := compositorDisplayName(dupPaths, 0); got != "rollA/scan_01.tif" {
		t.Fatalf("compositorDisplayName(dup, 0) = %q, want %q", got, "rollA/scan_01.tif")
	}
	if got := compositorDisplayName(dupPaths, 1); got != "rollB/scan_01.tif" {
		t.Fatalf("compositorDisplayName(dup, 1) = %q, want %q", got, "rollB/scan_01.tif")
	}
}

func TestCompositorFileListNoUnusedRightScrollbarGutter(t *testing.T) {
	ui := newTestEditorUI(t)
	c := &ui.compositorState
	c.open(ui)
	c.orientation = "ltr"

	var router input.Router
	var ops op.Ops
	const boxW, boxH = 584, 185
	frame := func() {
		for len(c.upBtns) < len(c.paths) {
			c.upBtns = append(c.upBtns, widget.Clickable{})
			c.downBtns = append(c.downBtns, widget.Clickable{})
			c.delBtns = append(c.delBtns, widget.Clickable{})
		}
		for len(c.rotBtns) < len(c.paths) {
			c.rotBtns = append(c.rotBtns, widget.Clickable{})
		}
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Source:      router.Source(),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(boxW, boxH)),
			Now:         time.Now(),
		}
		ui.overlays.beginFrame(gtx)
		c.layoutFilesSection(gtx, ui)
		ui.overlays.layoutOverlayPass(gtx)
		router.Frame(&ops)
	}
	firstDeleteRight := func() int {
		nodes := router.AppendSemantics(nil)
		var delRight int
		var walk func([]input.SemanticNode)
		walk = func(ns []input.SemanticNode) {
			for _, n := range ns {
				if n.Desc.Gestures == input.ClickGesture && !n.Desc.Bounds.Empty() {
					for _, child := range n.Children {
						if child.Desc.Label == "✕" {
							if n.Desc.Bounds.Max.X > delRight {
								delRight = n.Desc.Bounds.Max.X
							}
							break
						}
					}
				}
				if len(n.Children) > 0 {
					walk(n.Children)
				}
			}
		}
		walk(nodes)
		return delRight
	}

	// Non-scrollable (2 items): no 10px scrollbar gutter should be reserved on the right.
	c.paths = []string{"scan_01.png", "scan_02.png"}
	frame()
	frame()
	nonScrollDelRight := firstDeleteRight()
	if gap := boxW - nonScrollDelRight; gap > 10 {
		t.Fatalf("non-scrollable right gap = %dpx (delRight=%d, boxW=%d), want <= 10px (no unused scrollbar gutter)", gap, nonScrollDelRight, boxW)
	}

	// Scrollable (10 items): Occupy reserves scrollbar width so ✕ shifts left cleanly.
	c.paths = make([]string, 10)
	for i := range c.paths {
		c.paths[i] = fmt.Sprintf("scan_%02d.png", i+1)
	}
	frame()
	frame()
	scrollDelRight := firstDeleteRight()
	if scrollDelRight >= nonScrollDelRight-4 {
		t.Fatalf("scrollable delRight=%d did not reserve scrollbar room relative to non-scrollable delRight=%d", scrollDelRight, nonScrollDelRight)
	}
}

func TestCompositorPerImageRotationPointerFlow(t *testing.T) {
	ui := newTestEditorUI(t)
	sb := ui.app.(*stubBackend)
	previewURL := sb.registerPreview(image.NewNRGBA(image.Rect(0, 0, 120, 90)))
	scanA := filepath.Join("scans", "scan_a.png")
	scanB := filepath.Join("scans", "scan_b.png")

	c := &ui.compositorState
	c.open(ui)
	c.orientation = "ltr"
	c.paths = []string{scanA, scanB}
	c.previewURL = previewURL
	c.previewDims = image.Pt(120, 90)

	var router input.Router
	var ops op.Ops
	frame := func() {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Source:      router.Source(),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1280, 900)),
			Now:         time.Now(),
		}
		ui.layoutRoot(gtx)
		router.Frame(&ops)
	}

	findButtons := func(label string) []image.Rectangle {
		nodes := router.AppendSemantics(nil)
		seen := make(map[image.Rectangle]bool)
		var rects []image.Rectangle
		var walk func([]input.SemanticNode)
		walk = func(ns []input.SemanticNode) {
			for _, n := range ns {
				if n.Desc.Gestures == input.ClickGesture && !n.Desc.Bounds.Empty() && !seen[n.Desc.Bounds] {
					matched := n.Desc.Label == label
					if !matched {
						for _, child := range n.Children {
							if child.Desc.Label == label {
								matched = true
								break
							}
						}
					}
					if matched {
						seen[n.Desc.Bounds] = true
						rects = append(rects, n.Desc.Bounds)
					}
				}
				if len(n.Children) > 0 {
					walk(n.Children)
				}
			}
		}
		walk(nodes)
		sort.Slice(rects, func(i, j int) bool {
			return rects[i].Min.Y < rects[j].Min.Y
		})
		return rects
	}

	clickRect := func(r image.Rectangle) {
		pt := f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2))
		router.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
		)
		frame()
		frame()
	}

	frame()

	// Click Row 1 ↻ -> 90°, clears stale preview, prompts to re-stitch.
	rotRects := findButtons("↻")
	if len(rotRects) != 2 {
		t.Fatalf("expected 2 ↻ buttons, got %v", rotRects)
	}
	clickRect(rotRects[1])
	if len(c.rotations) != 2 || c.rotations[0] != 0 || c.rotations[1] != 1 {
		t.Fatalf("rotations after first click = %v, want [0 1]", c.rotations)
	}
	if c.previewURL != "" {
		t.Fatalf("expected rotation click to clear stale preview, got %q", c.previewURL)
	}
	wantStatus := `Rotation updated — click "Stitch images" to update`
	if c.statusMsg != wantStatus {
		t.Fatalf("statusMsg = %q, want %q", c.statusMsg, wantStatus)
	}

	// Click "↻ 90°" -> 180° (restrict to file list rows y < 330 so preview "↻ 90°" is excluded).
	rot90All := findButtons("↻ 90°")
	var rot90 []image.Rectangle
	for _, r := range rot90All {
		if r.Max.Y < 330 {
			rot90 = append(rot90, r)
		}
	}
	if len(rot90) != 1 {
		t.Fatalf("expected 1 row '↻ 90°' button, got %v (all=%v)", rot90, rot90All)
	}
	clickRect(rot90[0])
	if c.rotations[0] != 0 || c.rotations[1] != 2 {
		t.Fatalf("rotations after second click = %v, want [0 2]", c.rotations)
	}
	if len(findButtons("↻ 180°")) != 1 {
		t.Fatal("expected '↻ 180°' button after second click")
	}

	// Move Row 1 up (▲) -> rotations swap alongside paths.
	upRects := findButtons("▲")
	if len(upRects) != 1 {
		t.Fatalf("expected 1 enabled ▲ button for 2 rows, got %v", upRects)
	}
	clickRect(upRects[0])
	if c.paths[0] != scanB || c.paths[1] != scanA || c.rotations[0] != 2 || c.rotations[1] != 0 {
		t.Fatalf("after reorder: paths=%v rots=%v, want [%q %q] [2 0]", c.paths, c.rotations, scanB, scanA)
	}

	// Click "Stitch images" -> sends updated order and RotationSteps [2, 0].
	stitchRects := findButtons("Stitch images")
	if len(stitchRects) != 1 {
		t.Fatalf("expected 1 Stitch images button, got %v", stitchRects)
	}
	clickRect(stitchRects[0])
	if len(sb.lastStitchReq.ImagePaths) != 2 || sb.lastStitchReq.ImagePaths[0] != scanB || sb.lastStitchReq.ImagePaths[1] != scanA {
		t.Fatalf("lastStitchReq.ImagePaths = %v, want [%q %q]", sb.lastStitchReq.ImagePaths, scanB, scanA)
	}
	if len(sb.lastStitchReq.RotationSteps) != 2 || sb.lastStitchReq.RotationSteps[0] != 2 || sb.lastStitchReq.RotationSteps[1] != 0 {
		t.Fatalf("lastStitchReq.RotationSteps = %v, want [2 0]", sb.lastStitchReq.RotationSteps)
	}

	// Cycle Row 0 from 180° -> 270° -> 0°.
	clickRect(findButtons("↻ 180°")[0])
	if c.rotations[0] != 3 || len(findButtons("↻ 270°")) != 1 {
		t.Fatalf("expected rotation 3 ('↻ 270°'), got rots=%v", c.rotations)
	}
	clickRect(findButtons("↻ 270°")[0])
	if c.rotations[0] != 0 || len(findButtons("↻")) != 2 {
		t.Fatalf("expected rotation wrapped back to 0 ('↻'), got rots=%v", c.rotations)
	}

	// Delete Row 0 -> both paths and rotations shrink in sync.
	clickRect(findButtons("✕")[0])
	if len(c.paths) != 1 || c.paths[0] != scanA || len(c.rotations) != 1 || c.rotations[0] != 0 {
		t.Fatalf("after remove row 0: paths=%v rots=%v", c.paths, c.rotations)
	}
}

func TestCompositorRotatedPreviewResolvesFromCache(t *testing.T) {
	ui := newTestEditorUI(t)
	sb := ui.app.(*stubBackend)
	orig := image.NewNRGBA(image.Rect(0, 0, 200, 120))
	orig.SetNRGBA(10, 5, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	url := sb.registerPreview(orig)

	c := &ui.compositorState
	c.ensureInit()
	c.orientation = "ltr"
	c.previewURL = url
	c.previewDims = image.Pt(200, 120)

	// Step 1 (90° CW): rotated 120x200 image cached and reused across calls.
	c.rotationSteps = 1
	rot1a, ok := c.resolveCompositorImage(ui, c.effectivePreviewKey())
	if !ok || rot1a == nil || rot1a.Bounds().Size() != image.Pt(120, 200) {
		t.Fatalf("step 1 resolve failed: ok=%v img=%v", ok, rot1a)
	}
	rot1b, ok := c.resolveCompositorImage(ui, c.effectivePreviewKey())
	if !ok || rot1b != rot1a {
		t.Fatalf("expected second resolve at step 1 to hit rotatedCacheImg pointer: got %p != %p", rot1b, rot1a)
	}

	// Step 3 (270° CW): rebuilds cache for 270°, then reuses pointer.
	c.rotationSteps = 3
	rot3a, ok := c.resolveCompositorImage(ui, c.effectivePreviewKey())
	if !ok || rot3a == nil || rot3a.Bounds().Size() != image.Pt(120, 200) || rot3a == rot1a {
		t.Fatalf("step 3 resolve unexpected: ok=%v bounds=%v samePtr=%v", ok, rot3a.Bounds().Size(), rot3a == rot1a)
	}
	rot3b, ok := c.resolveCompositorImage(ui, c.effectivePreviewKey())
	if !ok || rot3b != rot3a {
		t.Fatal("expected second resolve at step 3 to hit cache")
	}

	// Step 2 (180°): rebuilds cache for 200x120, then reuses pointer.
	c.rotationSteps = 2
	rot2a, ok := c.resolveCompositorImage(ui, c.effectivePreviewKey())
	if !ok || rot2a == nil || rot2a.Bounds().Size() != image.Pt(200, 120) {
		t.Fatalf("step 2 resolve unexpected: ok=%v img=%v", ok, rot2a)
	}
	rot2b, ok := c.resolveCompositorImage(ui, c.effectivePreviewKey())
	if !ok || rot2b != rot2a {
		t.Fatal("expected second resolve at step 2 to hit cache")
	}

	// Step 0: returns original pointer and clears rotated cache.
	c.rotationSteps = 0
	got0, ok := c.resolveCompositorImage(ui, c.effectivePreviewKey())
	if !ok || got0 != orig || c.rotatedCacheImg != nil || c.rotatedCacheKey != "" {
		t.Fatalf("step 0 should return orig and clear cache: ok=%v same=%v cacheImg=%v cacheKey=%q", ok, got0 == orig, c.rotatedCacheImg != nil, c.rotatedCacheKey)
	}

	// Re-populate cache then clearResult() must release it.
	c.rotationSteps = 1
	_, _ = c.resolveCompositorImage(ui, c.effectivePreviewKey())
	c.clearResult()
	if c.rotatedCacheImg != nil || c.rotatedCacheKey != "" {
		t.Fatalf("clearResult left rotated cache behind: key=%q img=%v", c.rotatedCacheKey, c.rotatedCacheImg != nil)
	}
}


