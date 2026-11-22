package ui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"atropos/internal/raster"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const (
	defaultSidebarWidthDp = float32(320)
	minSidebarWidthDp     = float32(200)
	maxSidebarWidthDp     = float32(600)
)

type confirmDialogState struct {
	Title       string
	Message     string
	YesLabel    string
	NoLabel     string
	CancelLabel string
	YesVariant  btnVariant
	NoVariant   btnVariant
	OnYes       func()
	OnNo        func()
	OnCancel    func()
}

type cornerEntryCache struct {
	Preview      string
	Corners      []image.Point
	Width        int
	Height       int
	MaxCorners   int
	QualityLevel int
	MinDistance  int
	AccentValue  int
	UseStretch   bool
}

type imageMetaState struct {
	Format    string
	InputDims image.Point
	DPIX      float64
	DPIY      float64
}

type sidebarSashDragState struct {
	startX     float32
	startWidth float32
}

type EditorUI struct {
	app        Backend
	win        *app.Window
	host       *gioHost
	appVersion string
	appIconPNG []byte

	th       *material.Theme
	overlays *overlayManager
	tooltips *tooltipManager

	appIconOp     paint.ImageOp
	appIconLoaded bool

	syncJobs    bool
	backendJobs chan func()
	uiQueue     chan func()
	closeOnce   sync.Once
	allowClose  bool

	transitionGen uint64
	detectGen     uint64

	// Image & mode state
	mode              string
	backendModeMu     sync.Mutex
	backendMode       string
	imageLoaded       bool
	preview           string
	realImageDims     image.Point
	discSourceDims    image.Point
	imageMeta         imageMetaState
	cropSkipped       bool
	canRecrop         bool
	unsavedChanges    bool
	optimisticCrop    *optimisticCropState
	postSaveAfterSave func()

	// Loading & status
	loading            bool
	loadingFull        bool
	loadingMessage     string
	detectingCorners   bool
	touchupPending     bool
	descreenPending    bool
	dustRemovalPending bool
	autoContrastPend   bool
	savingImage        bool
	statusText         string
	statusUpdatedAt    time.Time
	statusOpacity      float32

	// Settings & spatial shortcuts
	settings           AllSettings
	cliPostSaveCmd     string
	cliPostSaveEnabled bool
	cliPostSaveExit    bool
	spatialLabels      map[string]string
	spatialKeyMap      map[string]string

	// Corner state
	maxCorners        int
	qualityLevel      int
	minDistance       int
	accentValue       int
	dotRadius         int
	customCorner      bool
	cornersDetected   bool
	detectedCornerPts []image.Point
	selectedCornerPts []image.Point
	cornerCount       int
	cornerEntry       *cornerEntryCache
	cornerMouseDown   bool

	// Disc state
	discActive        bool
	discInitialRadius int
	discCenter        image.Point
	discRadius        int
	discRotation      float64
	featherSize       int
	discLiveActive    bool
	discLiveTransform discLiveTransform
	discShiftLive     *discShiftLiveLayer // live Ctrl+drag layer (see disc_shift_live.go)
	discShiftLiveGen  uint64
	discShiftWarm     *discShiftLiveLayer // prebuilt layer for discShiftWarmKey
	discShiftWarmKey  discShiftLiveKey
	discShiftWarmGen  uint64
	discShiftWarmWant discShiftWarmWant
	ctrlDragShift     image.Point // pending Ctrl+drag shift in source px
	ctrlDrag          *discCtrlDragState
	shiftDrag         *discShiftDragState

	// Line state
	lines          []lineI
	linesProcessed bool
	lineDragKind   string
	lineStartImg   *image.Point
	lineHandleDrag *lineHandleDragState

	// Normal crop state
	normalRect        *rectI
	normalDragKind    string
	normalDragPending bool
	normalHandleDrag  string
	normalMoveDrag    *rectMoveDragState

	// Adjustments & selection state
	useStretch                bool
	blackPoint                int
	whitePoint                int
	adjustmentSelectionActive bool
	adjustmentRect            *rectI
	adjustmentDragActive      bool
	adjustmentDragKind        string
	adjustmentDragPending     bool
	adjustmentHandleDrag      string
	adjustmentMoveDrag        *rectMoveDragState

	useDescreenTool    bool
	descreenThresh     int
	descreenRadius     int
	descreenMiddle     int
	descreenHighlight  int
	descreenFast       bool
	useDustRemovalTool bool
	dustLevel          string
	useTouchupTool     bool
	brushSize          float64
	touchupStrokes     []TouchUpPoint
	touchupDragging    bool
	touchupResize      *touchupResizeDragState
	touchupCursor      *touchupCursorState
	useStraightEdgeTool bool

	// Main viewport & shared pointer state
	mainViewport      *viewportState
	dragging          bool
	dragStart         *f32.Point
	dragCurrent       *f32.Point
	lastDiscDragLogAt time.Time
	mousePos          f32.Point
	hoverDispPos *f32.Point
	spaceDown    bool
	spacePanMode bool
	panDrag      *panDragState
	lastWindowSz image.Point
	lastResizeAt time.Time

	// Modals & dialogs
	errorMessage    string
	confirmDialog   *confirmDialogState
	aboutOpen       bool
	optionsOpen     bool
	resizeOpen      bool
	compositorOpen  bool
	calibrationOpen bool

	resizeState     resizeModalState
	optionsState    optionsModalState
	compositorState compositorModalState
	calibState      calibrationModalState

	// UI widgets & sidebar layout
	anyEditorFocused bool
	sidebarWidthDp   float32
	sidebarSashTag   uint8
	sidebarSashDrag  *sidebarSashDragState
	lastSashClickAt  time.Time
	sidebarList      widget.List
	modePanelH       int

	btnModeCorner widget.Clickable
	btnModeDisc   widget.Clickable
	btnModeLine   widget.Clickable
	btnModeNormal widget.Clickable

	sldMaxCorners   uiSlider
	sldQualityLevel uiSlider
	sldMinDistance  uiSlider
	sldAccent       uiSlider
	sldDotRadius    uiSlider
	chkCustomCorner widget.Clickable

	sldDiscRadius  uiSlider
	sldDiscFeather uiSlider
	sldDiscCutout  uiSlider

	btnPrimaryAction  widget.Clickable
	btnSkipCrop       widget.Clickable
	btnRecrop         widget.Clickable
	btnResetAction    widget.Clickable
	btnLoadImage      widget.Clickable
	btnSaveImage      widget.Clickable
	btnOptions        widget.Clickable
	btnAbout          widget.Clickable
	btnStatusZoom     widget.Clickable
	btnOpenCompositor widget.Clickable

	accAdjustments     uiAccordion
	accTools           uiAccordion
	accShortcuts       uiAccordion
	accAdjustmentsOpen bool
	accToolsOpen       bool
	accShortcutsOpen   bool

	btnSelToggle          widget.Clickable
	btnResize             widget.Clickable
	btnTrim               widget.Clickable
	btnAutoContrast       widget.Clickable
	btnDescreenToggle     widget.Clickable
	btnDustToggle         widget.Clickable
	btnTouchupToggle      widget.Clickable
	btnStraightEdgeToggle widget.Clickable
	ddDustLevel           uiDropdown
	btnApplyDust          widget.Clickable
	sldDescreenThresh     uiSlider
	sldDescreenRadius     uiSlider
	sldDescreenMiddle     uiSlider
	sldDescreenHighlight  uiSlider
	chkDescreenFast       widget.Clickable
	btnApplyDescreen      widget.Clickable
	sldBrushSize          uiSlider
	chkUseStretch         widget.Clickable
	sldBlackPoint         uiSlider
	sldWhitePoint         uiSlider

	btnCloseError    widget.Clickable
	btnConfirmYes    widget.Clickable
	btnConfirmNo     widget.Clickable
	btnConfirmCancel widget.Clickable
	btnAboutOK       widget.Clickable
	btnAboutLink     widget.Clickable
	modalBackdropBtn widget.Clickable
}

// NewEditorUI constructs the Gio UI controller bound to the provided backend.
func NewEditorUI(a Backend, win *app.Window, cfg Config) *EditorUI {
	overlays := &overlayManager{}
	tooltips := newTooltipManager(overlays)
	spatialLabels := querySpatialKeyLabels()
	spatialKeyMap := make(map[string]string, 6)
	canonMap := map[string]string{
		"KeyW": "w",
		"KeyA": "a",
		"KeyS": "s",
		"KeyD": "d",
		"KeyQ": "q",
		"KeyE": "e",
	}
	for code, canon := range canonMap {
		if label := strings.TrimSpace(spatialLabels[code]); label != "" {
			spatialKeyMap[strings.ToUpper(label)] = canon
		}
	}

	version := cfg.AppVersion
	if version == "" {
		version = "dev"
	}

	ui := &EditorUI{
		app:               a,
		win:               win,
		appVersion:        version,
		appIconPNG:        cfg.AppIconPNG,
		th:                newAtroposTheme(),
		overlays:          overlays,
		tooltips:          tooltips,
		backendJobs:       make(chan func(), 128),
		uiQueue:           make(chan func(), 256),
		mode:              "corner",
		backendMode:       "corner",
		spatialLabels:     spatialLabels,
		spatialKeyMap:     spatialKeyMap,
		sidebarWidthDp:    defaultSidebarWidthDp,
		maxCorners:        500,
		qualityLevel:      1,
		minDistance:       100,
		accentValue:       20,
		dotRadius:         5,
		featherSize:       20,
		lineDragKind:      "none",
		normalDragKind:    "none",
		adjustmentDragKind: "none",
		blackPoint:        0,
		whitePoint:        255,
		descreenThresh:    93,
		descreenRadius:    18,
		descreenMiddle:    6,
		descreenHighlight: 50,
		dustLevel:         "medium",
		brushSize:         40,
		mainViewport:      newViewportState(),
	}
	ui.sidebarList.Axis = layout.Vertical
	ui.initAppIcon()

	ui.host = newGioHost(
		win,
		ui.postUI,
		ui.onTouchupDone,
		ui.handleCloseRequest,
	)
	if a != nil {
		a.SetHost(ui.host)
	}

	go ui.backendWorker()
	return ui
}

// RunWindowLoop drives the Gio window event loop until the window is destroyed.
func (e *EditorUI) RunWindowLoop() error {
	if e.win == nil {
		return nil
	}
	var ops op.Ops
	for {
		switch ev := e.win.Event().(type) {
		case app.DestroyEvent:
			return ev.Err
		case *app.ClosingEvent:
			if !e.allowClose && e.unsavedChanges {
				ev.Abort()
				e.handleCloseRequest()
			}
		case app.ViewEvent:
			e.handlePlatformViewEvent(ev)
		case app.FrameEvent:
			gtx := app.NewContext(&ops, ev)
			e.layoutRoot(gtx)
			ev.Frame(gtx.Ops)
		}
	}
}

// SetSyncJobs configures synchronous backend execution for tests.
func (e *EditorUI) SetSyncJobs(sync bool) {
	e.syncJobs = sync
}

// State returns a snapshot of UI state for integration tests.
func (e *EditorUI) State() StateSnapshot {
	snap := StateSnapshot{
		Mode:               e.mode,
		ImageLoaded:        e.imageLoaded,
		Preview:            e.preview,
		RealImageDims:      e.realImageDims,
		CornerCount:        e.cornerCount,
		CornersDetected:    e.cornersDetected,
		HasCornerCache:     e.cornerEntry != nil,
		DiscActive:         e.discActive,
		LinesProcessed:     e.linesProcessed,
		CropSkipped:        e.cropSkipped,
		CanRecrop:          e.canRecrop,
		CanSave:            e.canSave(),
		UnsavedChanges:     e.unsavedChanges,
		UseDescreenTool:    e.useDescreenTool,
		ErrorMessage:       e.errorMessage,
		CalibrationOpen:    e.calibrationOpen,
		CalibrationPreview: e.calibState.previewURL,
		CalibrationDims:    e.calibState.dims,
	}
	if e.cornerEntry != nil {
		snap.CornerCacheWidth = e.cornerEntry.Width
	}
	return snap
}

// LoadFile loads an image from disk into the UI session.
func (e *EditorUI) LoadFile(path string) {
	e.loadFile(path, false)
}

// SwitchMode switches the active editor mode.
func (e *EditorUI) SwitchMode(mode string) {
	e.handleModeSwitch(mode)
}

// SetDescreenTool toggles the descreen tool state.
func (e *EditorUI) SetDescreenTool(active bool) {
	e.useDescreenTool = active
}

// ClickCorner selects a corner at the given image-space coordinates.
func (e *EditorUI) ClickCorner(pt image.Point, custom bool) {
	e.handleCornerClick(pt, custom)
}

// SkipCrop skips geometric cropping and enters post-crop editing.
func (e *EditorUI) SkipCrop() {
	e.handleSkipCrop()
}

// ResetAction resets the active mode's selection or crop and returns to the pre-crop phase.
func (e *EditorUI) ResetAction() {
	e.handleResetAction()
}

// ApplyNormalCrop commits a normal-mode rectangular crop.
func (e *EditorUI) ApplyNormalCrop(x1, y1, x2, y2 int) {
	e.normalRect = &rectI{X1: x1, Y1: y1, X2: x2, Y2: y2}
	e.handleNormalCrop()
}

// ApplyDrawDisc creates or redraws a disc crop at the specified circle.
func (e *EditorUI) ApplyDrawDisc(cx, cy, radius int) {
	e.applyDrawDisc(cx, cy, radius)
}

// ApplyAddLine appends a line segment in line mode and processes once 4 are present.
func (e *EditorUI) ApplyAddLine(p1, p2 image.Point) {
	e.applyAddLine(p1, p2)
}

// Undo triggers one undo step.
func (e *EditorUI) Undo() {
	e.handleHistory(false)
}

// Redo triggers one redo step.
func (e *EditorUI) Redo() {
	e.handleHistory(true)
}

// OpenCalibration opens the isolated Corner Calibration modal.
func (e *EditorUI) OpenCalibration() {
	e.calibState.open(e)
}

// CalibrationAddPaths adds calibration scan paths without modifying the main document.
func (e *EditorUI) CalibrationAddPaths(paths []string) {
	e.calibState.addPaths(paths, e)
}

// CloseCalibration immediately closes the Corner Calibration modal.
func (e *EditorUI) CloseCalibration() {
	e.calibState.closeNow(e)
}

func (e *EditorUI) initAppIcon() {
	if e.appIconLoaded || len(e.appIconPNG) == 0 {
		return
	}
	img, _, err := image.Decode(bytes.NewReader(e.appIconPNG))
	if err != nil {
		return
	}
	nrgba := raster.ToNRGBA(img)
	e.appIconOp = paint.NewImageOp(nrgba)
	e.appIconLoaded = true
}

func (e *EditorUI) backendWorker() {
	for job := range e.backendJobs {
		if job != nil {
			job()
		}
	}
}

func (e *EditorUI) runBackend(job func()) {
	if job == nil {
		return
	}
	if e.syncJobs {
		job()
		return
	}
	e.backendJobs <- job
}

func (e *EditorUI) postUI(fn func()) {
	if fn == nil {
		return
	}
	if e.syncJobs {
		fn()
		return
	}
	select {
	case e.uiQueue <- fn:
	default:
		go func() { e.uiQueue <- fn }()
	}
	e.invalidate()
}

func (e *EditorUI) drainUIQueue() {
	for {
		select {
		case fn := <-e.uiQueue:
			if fn != nil {
				fn()
			}
		default:
			return
		}
	}
}

func (e *EditorUI) invalidate() {
	if e.win != nil {
		e.win.Invalidate()
	}
}

func (e *EditorUI) InitOnStartup() {
	if e.app == nil {
		return
	}
	s := e.app.GetAllSettings()
	e.settings = s
	if s.CornerMaxCorners > 0 {
		e.maxCorners = s.CornerMaxCorners
	}
	if s.CornerMinDistance > 0 {
		e.minDistance = s.CornerMinDistance
	}
	args := e.app.GetLaunchArgs()
	e.cliPostSaveCmd = args.PostSaveCommand
	e.cliPostSaveEnabled = args.PostSaveEnabled
	e.cliPostSaveExit = args.PostSaveExit
	if args.Mode != "" {
		switch strings.ToLower(args.Mode) {
		case "corner", "disc", "line", "normal":
			e.mode = strings.ToLower(args.Mode)
			e.setBackendMode(e.mode)
		}
	}
	if args.FilePath != "" {
		e.loadFile(args.FilePath, false)
	}
}

func (e *EditorUI) saveSettings(next AllSettings) {
	e.settings = next
	cfg := next
	e.runBackend(func() {
		if err := e.app.SaveAllSettings(cfg); err != nil {
			e.postUI(func() { e.showError(err) })
		}
	})
}

func (e *EditorUI) hasOpenModal() bool {
	return e.errorMessage != "" ||
		e.confirmDialog != nil ||
		e.calibrationOpen ||
		e.resizeOpen ||
		e.compositorOpen ||
		e.optionsOpen ||
		e.aboutOpen
}

func (e *EditorUI) isBusy() bool {
	return e.loading ||
		e.loadingFull ||
		e.detectingCorners ||
		e.touchupPending ||
		e.descreenPending ||
		e.dustRemovalPending ||
		e.autoContrastPend ||
		e.savingImage
}

func (e *EditorUI) postCropAvailable() bool {
	if !e.imageLoaded {
		return false
	}
	if e.cropSkipped || e.canRecrop {
		return true
	}
	switch e.mode {
	case "corner":
		return e.cornerCount >= 4
	case "disc":
		return e.discActive && e.discRadius > 0
	case "line":
		return e.linesProcessed
	case "normal":
		return false
	}
	return false
}

func (e *EditorUI) canSave() bool {
	if !e.imageLoaded {
		return false
	}
	return e.postCropAvailable()
}

func (e *EditorUI) showStatus(msg string) {
	e.statusText = msg
	e.statusUpdatedAt = time.Now()
	if msg != "" {
		e.statusOpacity = 1
	} else {
		e.statusOpacity = 0
	}
	e.invalidate()
}

func (e *EditorUI) showError(err error) {
	if err == nil {
		return
	}
	e.showErrorMsg(err.Error())
}

func (e *EditorUI) showErrorMsg(msg string) {
	if msg == "" {
		return
	}
	e.errorMessage = msg
	e.loading = false
	e.loadingFull = false
	e.loadingMessage = ""
	e.invalidate()
}

func (e *EditorUI) setPreview(url string) {
	if url == "" {
		return
	}
	e.preview = url
	e.invalidate()
}

func (e *EditorUI) beginTransition() uint64 {
	e.transitionGen++
	e.detectGen++
	e.cancelActiveCanvasDrag()
	e.clearDiscShiftLive()
	e.loading = false
	e.loadingFull = false
	e.loadingMessage = ""
	e.detectingCorners = false
	e.touchupPending = false
	e.descreenPending = false
	e.dustRemovalPending = false
	e.autoContrastPend = false
	e.savingImage = false
	if e.app != nil {
		e.app.CancelTouchup()
		e.app.CancelCornerDetect()
	}
	return e.transitionGen
}

func (e *EditorUI) resetImageState(clearCornerCache bool) {
	e.cropSkipped = false
	e.canRecrop = false
	e.unsavedChanges = false
	e.optimisticCrop = nil
	e.postSaveAfterSave = nil

	e.cornersDetected = false
	e.detectedCornerPts = nil
	e.selectedCornerPts = nil
	e.cornerCount = 0
	e.customCorner = false
	if clearCornerCache {
		e.cornerEntry = nil
	}

	e.discActive = false
	e.discInitialRadius = 0
	e.discCenter = image.Point{}
	e.discRadius = 0
	e.discRotation = 0
	e.featherSize = 20
	e.discLiveActive = false
	e.discLiveTransform = discLiveTransform{}
	e.clearDiscShiftLive()

	e.lines = nil
	e.linesProcessed = false
	e.lineDragKind = "none"
	e.lineStartImg = nil

	e.normalRect = nil
	e.normalDragKind = "none"
	e.normalDragPending = false

	e.clearAdjustmentSelection()

	e.useDescreenTool = false
	e.useDustRemovalTool = false
	e.useStraightEdgeTool = false
	e.clearTouchupState()

	e.blackPoint = 0
	e.whitePoint = 255
}

func (e *EditorUI) clearAdjustmentSelection() {
	e.adjustmentSelectionActive = false
	e.adjustmentRect = nil
	e.adjustmentDragActive = false
	e.adjustmentDragKind = "none"
	e.adjustmentDragPending = false
	e.adjustmentHandleDrag = ""
	e.adjustmentMoveDrag = nil
}

func (e *EditorUI) clearTouchupState() {
	e.useTouchupTool = false
	e.touchupStrokes = nil
	e.touchupDragging = false
	e.touchupCursor = nil
	e.touchupResize = nil
}

func (e *EditorUI) handleLoadImage() {
	if e.app == nil {
		return
	}
	e.runBackend(func() {
		path, err := e.app.OpenImageDialog()
		e.postUI(func() {
			if err != nil {
				e.showError(err)
				return
			}
			if path == "" {
				return
			}
			e.loadFile(path, false)
		})
	})
}

func (e *EditorUI) handlePasteImage() {
	if e.app == nil {
		return
	}
	doPaste := func() {
		gen := e.beginTransition()
		e.resetImageState(true)
		e.loading = true
		e.loadingFull = true
		e.loadingMessage = "Loading image from clipboard…"
		e.showStatus("Loading image from clipboard…")
		e.runBackend(func() {
			info, err := e.app.LoadImageFromClipboard()
			e.postUI(func() {
				if e.transitionGen != gen {
					return
				}
				if err != nil {
					e.loading = false
					e.loadingFull = false
					e.loadingMessage = ""
					e.showError(err)
					return
				}
				e.applyLoadedImageInfo(info, gen)
			})
		})
	}
	if e.unsavedChanges {
		e.confirmDialog = &confirmDialogState{
			Title:       "Unsaved Changes",
			Message:     "Discard unsaved changes and load clipboard image?",
			YesLabel:    "Discard",
			CancelLabel: "Cancel",
			YesVariant:  btnDanger,
			OnYes: func() {
				e.confirmDialog = nil
				doPaste()
			},
			OnCancel: func() { e.confirmDialog = nil },
		}
		return
	}
	doPaste()
}

func filterDroppedImagePaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		switch strings.ToLower(filepath.Ext(trimmed)) {
		case ".png", ".jpg", ".jpeg", ".tif", ".tiff", ".bmp", ".gif", ".webp":
			out = append(out, trimmed)
		}
	}
	return out
}

func (e *EditorUI) handleDroppedFiles(paths []string) {
	if len(paths) == 0 {
		return
	}
	e.postUI(func() {
		if e.errorMessage != "" || e.confirmDialog != nil {
			return
		}
		if e.calibrationOpen {
			if imgPaths := filterDroppedImagePaths(paths); len(imgPaths) > 0 {
				e.calibState.addPaths(imgPaths, e)
			}
			return
		}
		if e.compositorOpen {
			if imgPaths := filterDroppedImagePaths(paths); len(imgPaths) > 0 {
				e.compositorState.addPaths(imgPaths, e)
			}
			return
		}
		if e.hasOpenModal() {
			return
		}
		for _, p := range paths {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				e.loadFile(trimmed, false)
				return
			}
		}
	})
}

func (e *EditorUI) loadFile(filePath string, fromCompositor bool) {
	if e.app == nil || strings.TrimSpace(filePath) == "" {
		return
	}
	doLoad := func() {
		gen := e.beginTransition()
		e.resetImageState(true)
		e.loading = true
		e.loadingFull = true
		e.loadingMessage = "Loading image…"
		e.showStatus("Loading image…")
		e.runBackend(func() {
			info, err := e.app.LoadImage(LoadImageRequest{FilePath: filePath})
			e.postUI(func() {
				if e.transitionGen != gen {
					return
				}
				if err != nil {
					e.loading = false
					e.loadingFull = false
					e.loadingMessage = ""
					e.showError(err)
					return
				}
				e.applyLoadedImageInfo(info, gen)
			})
		})
	}
	if !fromCompositor && e.unsavedChanges {
		e.confirmDialog = &confirmDialogState{
			Title:       "Unsaved Changes",
			Message:     fmt.Sprintf("Discard unsaved changes and load %s?", filepath.Base(filePath)),
			YesLabel:    "Discard",
			CancelLabel: "Cancel",
			YesVariant:  btnDanger,
			OnYes: func() {
				e.confirmDialog = nil
				doLoad()
			},
			OnCancel: func() { e.confirmDialog = nil },
		}
		return
	}
	doLoad()
}

func (e *EditorUI) applyLoadedImageInfo(info *ImageInfo, gen uint64) {
	e.loadingFull = false
	if info == nil {
		e.loading = false
		e.loadingMessage = ""
		return
	}
	e.mainViewport.ResetCache()
	e.mainViewport.Zoom = 1
	e.mainViewport.FitWidth = 0
	e.mainViewport.ScrollX = 0
	e.mainViewport.ScrollY = 0

	e.imageLoaded = true
	e.setBackendMode(e.mode)
	e.unsavedChanges = false
	e.imageMeta = imageMetaState{
		Format:    info.Format,
		InputDims: image.Pt(info.Width, info.Height),
		DPIX:      info.DPIX,
		DPIY:      info.DPIY,
	}
	e.realImageDims = image.Pt(info.Width, info.Height)
	e.discSourceDims = e.realImageDims
	e.setPreview(info.Preview)

	if e.settings.AutoCornerParams && info.SuggestedCornerParams.MinDistance > 0 {
		e.minDistance = info.SuggestedCornerParams.MinDistance
		e.maxCorners = info.SuggestedCornerParams.MaxCorners
	}

	if e.mode == "corner" {
		e.detectCornersWithGen(gen)
		return
	}
	e.loading = false
	e.loadingMessage = ""
	e.showStatus("Loaded image")
}

func (e *EditorUI) getBackendMode() string {
	e.backendModeMu.Lock()
	defer e.backendModeMu.Unlock()
	if e.backendMode == "" {
		return "corner"
	}
	return e.backendMode
}

func (e *EditorUI) setBackendMode(m string) {
	e.backendModeMu.Lock()
	e.backendMode = m
	e.backendModeMu.Unlock()
}

func (e *EditorUI) handleModeSwitch(nextMode string) {
	if nextMode == "" || nextMode == e.mode {
		return
	}
	// Set mode synchronously at top so button highlight never lags async calls.
	e.mode = nextMode
	e.sidebarList.Position = layout.Position{}
	if !e.imageLoaded || e.app == nil {
		e.setBackendMode(nextMode)
		return
	}

	hadPostCrop := e.canRecrop || e.cropSkipped || e.discActive || e.linesProcessed || e.cornerCount >= 4
	gen := e.beginTransition()
	e.resetImageState(false)
	if !hadPostCrop {
		e.mainViewport.Zoom = 1
		e.mainViewport.ScrollX = 0
		e.mainViewport.ScrollY = 0
	}

	dots := e.dotRadius
	autoDetect := e.settings.AutoDetectOnModeSwitch
	cacheValid := nextMode == "corner" &&
		e.cornerEntry != nil &&
		len(e.cornerEntry.Corners) > 0 &&
		e.cornerEntry.MaxCorners == e.maxCorners &&
		e.cornerEntry.QualityLevel == e.qualityLevel &&
		e.cornerEntry.MinDistance == e.minDistance &&
		e.cornerEntry.AccentValue == e.accentValue &&
		e.cornerEntry.UseStretch == e.useStretch

	if nextMode == "corner" && cacheValid && !hadPostCrop {
		e.detectedCornerPts = append([]image.Point(nil), e.cornerEntry.Corners...)
		e.cornersDetected = true
	}

	e.runBackend(func() {
		departing := e.getBackendMode()
		var clean *ProcessResult
		var err error
		switch departing {
		case "disc":
			clean, err = e.app.ResetDisc()
		case "line":
			clean, err = e.app.ClearLines()
		case "normal":
			clean, err = e.app.ResetNormal()
		default:
			clean, err = e.app.ResetCorners()
		}
		if err != nil {
			e.postUI(func() {
				if e.transitionGen != gen {
					return
				}
				e.mode = departing
				e.showError(err)
			})
			return
		}
		e.setBackendMode(nextMode)

		res := clean
		restoredCorners := false
		shouldDetect := false
		if nextMode == "corner" {
			if cacheValid {
				if ov, ovErr := e.app.RestoreCornerOverlay(RestoreCornerOverlayRequest{DotRadius: dots}); ovErr == nil && ov != nil {
					res = ov
					restoredCorners = true
				} else if autoDetect {
					shouldDetect = true
				}
			} else if autoDetect {
				shouldDetect = true
			}
		}
		if !shouldDetect && (res == nil || res.Preview == "") {
			res, _ = e.app.GetCleanPreview()
		}

		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			if hadPostCrop {
				e.mainViewport.Zoom = 1
				e.mainViewport.FitWidth = 0
				e.mainViewport.ScrollX = 0
				e.mainViewport.ScrollY = 0
				if clean != nil {
					if clean.Preview != "" {
						e.setPreview(clean.Preview)
					}
					if clean.Width > 0 && clean.Height > 0 {
						e.realImageDims = image.Pt(clean.Width, clean.Height)
						e.discSourceDims = e.realImageDims
					}
				}
			}
			if shouldDetect {
				e.detectCornersWithGen(gen)
				return
			}
			if restoredCorners {
				e.detectedCornerPts = append([]image.Point(nil), res.Corners...)
				e.cornersDetected = len(e.detectedCornerPts) > 0
			}
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
					e.discSourceDims = e.realImageDims
				}
				if res.Message != "" {
					e.showStatus(res.Message)
				} else {
					e.showStatus("")
				}
			}
		})
	})
}

func (e *EditorUI) refreshCleanPreview(gen uint64) {
	if e.app == nil || !e.imageLoaded {
		return
	}
	e.loading = true
	e.loadingMessage = "Preparing preview…"
	e.runBackend(func() {
		res, err := e.app.GetCleanPreview()
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.loading = false
			e.loadingMessage = ""
			if err != nil {
				e.showError(err)
				return
			}
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
					e.discSourceDims = e.realImageDims
				}
				if res.Message != "" {
					e.showStatus(res.Message)
				} else {
					e.showStatus("")
				}
			}
		})
	})
}

func (e *EditorUI) handleDetectCorners() {
	if !e.imageLoaded || e.cropSkipped || e.isBusy() {
		return
	}
	gen := e.transitionGen
	e.detectCornersWithGen(gen)
}

func (e *EditorUI) detectCornersWithGen(transGen uint64) {
	if e.app == nil || !e.imageLoaded {
		return
	}
	e.detectGen++
	dGen := e.detectGen
	e.detectingCorners = true
	e.cornersDetected = false
	e.detectedCornerPts = nil
	e.selectedCornerPts = nil
	e.cornerCount = 0
	e.loadingMessage = "Detecting corners…"
	e.showStatus("Detecting corners…")

	qual := e.qualityLevel
	req := CornerDetectRequest{
		MaxCorners:   e.maxCorners,
		QualityLevel: float64(qual) / 100.0,
		MinDistance:  e.minDistance,
		AccentValue:  e.accentValue,
		UseStretch:   e.useStretch,
		StretchLow:   float64(e.blackPoint),
		StretchHigh:  float64(e.whitePoint),
	}
	e.runBackend(func() {
		res, err := e.app.DetectCorners(req)
		e.postUI(func() {
			if e.transitionGen != transGen || e.detectGen != dGen {
				return
			}
			e.loading = false
			e.loadingMessage = ""
			e.detectingCorners = false
			if err != nil {
				e.showError(err)
				return
			}
			if res == nil {
				return
			}
			if res.Preview != "" {
				e.setPreview(res.Preview)
			}
			if res.Width > 0 && res.Height > 0 {
				e.realImageDims = image.Pt(res.Width, res.Height)
			}
			e.detectedCornerPts = append([]image.Point(nil), res.Corners...)
			e.cornersDetected = len(e.detectedCornerPts) > 0
			e.cornerEntry = &cornerEntryCache{
				Preview:      res.Preview,
				Corners:      append([]image.Point(nil), res.Corners...),
				Width:        res.Width,
				Height:       res.Height,
				MaxCorners:   req.MaxCorners,
				QualityLevel: qual,
				MinDistance:  req.MinDistance,
				AccentValue:  req.AccentValue,
				UseStretch:   req.UseStretch,
			}
			e.showStatus(res.Message)
		})
	})
}

func (e *EditorUI) handleCornerClick(pt image.Point, custom bool) {
	if e.app == nil || !e.imageLoaded || e.isBusy() {
		return
	}
	gen := e.transitionGen
	e.runBackend(func() {
		res, err := e.app.ClickCorner(ClickCornerRequest{
			X:      pt.X,
			Y:      pt.Y,
			Custom: custom,
		})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			if err != nil {
				e.showError(err)
				return
			}
			if res == nil {
				return
			}
			e.cornerCount = res.Count
			if res.Done {
				e.selectedCornerPts = append(e.selectedCornerPts, image.Pt(res.SnappedX, res.SnappedY))
				e.canRecrop = true
				e.detectedCornerPts = nil
				e.cornersDetected = false
				if res.Preview != "" {
					e.setPreview(res.Preview)
				}
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				if res.DescreenReset {
					e.useDescreenTool = false
				}
				e.unsavedChanges = true
				e.showStatus(res.Message)
				e.flushPendingSave()
				return
			}
			e.selectedCornerPts = append(e.selectedCornerPts, image.Pt(res.SnappedX, res.SnappedY))
			e.showStatus(res.Message)
		})
	})
}

func (e *EditorUI) applyDrawDisc(cx, cy, radius int) {
	if e.app == nil || !e.imageLoaded || e.isBusy() {
		return
	}
	if e.app != nil {
		e.app.Logf("disc applyDrawDisc: center=(%d,%d) radius=%d realDims=%dx%d", cx, cy, radius, e.realImageDims.X, e.realImageDims.Y)
	}
	gen := e.transitionGen
	e.loading = true
	e.loadingMessage = "Extracting disc…"
	e.showStatus("Extracting disc…")
	e.runBackend(func() {
		res, err := e.app.DrawDisc(DiscDrawRequest{
			CenterX: cx,
			CenterY: cy,
			Radius:  radius,
		})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.loading = false
			e.loadingMessage = ""
			if err != nil {
				e.showError(err)
				return
			}
			if e.discInitialRadius == 0 && res != nil && res.DiscRadius > 0 {
				e.discInitialRadius = res.DiscRadius
			}
			e.syncDiscResponse(res)
			e.canRecrop = true
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) syncDiscResponse(res *ProcessResult) {
	if res == nil {
		return
	}
	e.discLiveActive = false
	e.discLiveTransform = discLiveTransform{}
	e.discActive = res.DiscRadius > 0
	if res.DiscRadius > 0 {
		e.discCenter = image.Pt(res.DiscCenterX, res.DiscCenterY)
		e.discRadius = res.DiscRadius
		e.discRotation = res.DiscRotation
		if e.discInitialRadius == 0 {
			e.discInitialRadius = res.DiscRadius
		}
	}
	if res.HistoryFeatherSize > 0 {
		e.featherSize = res.HistoryFeatherSize
	}
	if res.Preview != "" {
		e.setPreview(res.Preview)
	}
	if res.Width > 0 && res.Height > 0 {
		e.realImageDims = image.Pt(res.Width, res.Height)
	}
	if res.DescreenReset {
		e.useDescreenTool = false
	}
	if res.Message != "" {
		e.showStatus(res.Message)
	}
	e.flushPendingSave()
}

func (e *EditorUI) applyStraightEdgeRotate(angleDeg float64) {
	if e.app == nil || !e.discActive || e.isBusy() {
		return
	}
	gen := e.transitionGen
	e.loading = true
	e.loadingMessage = "Rotating disc…"
	e.runBackend(func() {
		res, err := e.app.StraightEdgeRotate(StraightEdgeRotateRequest{AngleDeg: angleDeg})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.loading = false
			e.loadingMessage = ""
			if err != nil {
				e.showError(err)
				return
			}
			e.syncDiscResponse(res)
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) commitDiscRotateDrag(angle float64) {
	if e.app == nil || !e.discActive {
		e.discLiveActive = false
		e.discLiveTransform = discLiveTransform{}
		return
	}
	if math.Abs(angle) < 0.01 {
		e.discLiveActive = false
		e.discLiveTransform = discLiveTransform{}
		return
	}
	gen := e.transitionGen
	e.runBackend(func() {
		res, err := e.app.RotateDisc(DiscRotateRequest{Angle: angle})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			if err != nil {
				e.discLiveActive = false
				e.discLiveTransform = discLiveTransform{}
				e.showError(err)
				return
			}
			e.syncDiscResponse(res)
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) commitDiscShiftDrag(dx, dy int) {
	if e.app == nil || !e.discActive || (dx == 0 && dy == 0) {
		e.discLiveActive = false
		e.discLiveTransform = discLiveTransform{}
		e.clearDiscShiftLive()
		return
	}
	gen := e.transitionGen
	live := e.discShiftLive // tracks this commit (see releaseDiscShiftGesture)
	e.runBackend(func() {
		res, err := e.app.ShiftDisc(ShiftDiscRequest{DX: dx, DY: dy})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			if err != nil {
				e.discLiveActive = false
				e.discLiveTransform = discLiveTransform{}
				e.clearDiscShiftLive()
				e.showError(err)
				return
			}
			e.syncDiscResponse(res)
			e.discShiftCommitted(live, dx, dy, res)
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) handleDiscRadiusCommit(radius int) {
	if e.app == nil || !e.discActive || radius < 1 {
		return
	}
	e.discRadius = radius
	gen := e.transitionGen
	e.runBackend(func() {
		res, err := e.app.SetFeatherRadius(FeatherRadiusRequest{Radius: radius})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			if err != nil {
				e.showError(err)
				return
			}
			e.syncDiscResponse(res)
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) handleDiscFeatherCommit(size int) {
	e.setFeatherSizeAndApply(size)
}

func (e *EditorUI) handleDiscCutoutCommit(pct int) {
	if e.app == nil {
		return
	}
	next := e.settings
	next.DiscCutoutPercent = clamp(pct, 0, 50)
	e.settings = next
	cfg := next
	gen := e.transitionGen
	e.runBackend(func() {
		if err := e.app.SaveAllSettings(cfg); err != nil {
			e.postUI(func() { e.showError(err) })
			return
		}
		if e.discActive {
			res, err := e.app.SetDiscSettings(DiscSettings{
				CenterCutout:  cfg.DiscCenterCutout,
				CutoutPercent: cfg.DiscCutoutPercent,
			})
			e.postUI(func() {
				if e.transitionGen != gen {
					return
				}
				if err != nil {
					e.showError(err)
					return
				}
				e.syncDiscResponse(res)
				e.unsavedChanges = true
			})
		}
	})
}

func (e *EditorUI) applyAddLine(start, end image.Point) {
	if e.app == nil || !e.imageLoaded || e.linesProcessed || e.isBusy() {
		return
	}
	gen := e.transitionGen
	e.lines = append(e.lines, lineI{X1: start.X, Y1: start.Y, X2: end.X, Y2: end.Y})
	shouldProcess := len(e.lines) == 4
	e.runBackend(func() {
		res, err := e.app.AddLine(LineAddRequest{X1: start.X, Y1: start.Y, X2: end.X, Y2: end.Y})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			if err != nil {
				if len(e.lines) > 0 {
					e.lines = e.lines[:len(e.lines)-1]
				}
				e.showError(err)
				return
			}
			if res != nil && res.Message != "" {
				e.showStatus(res.Message)
			}
			if shouldProcess {
				e.loading = true
				e.loadingMessage = "Applying line perspective…"
				e.runBackend(func() {
					pRes, pErr := e.app.ProcessLines()
					e.postUI(func() {
						if e.transitionGen != gen {
							return
						}
						e.loading = false
						e.loadingMessage = ""
						if pErr != nil {
							e.showError(pErr)
							return
						}
						e.linesProcessed = true
						e.canRecrop = true
						e.unsavedChanges = true
						if pRes != nil {
							e.setPreview(pRes.Preview)
							if pRes.Width > 0 && pRes.Height > 0 {
								e.realImageDims = image.Pt(pRes.Width, pRes.Height)
							}
							if pRes.DescreenReset {
								e.useDescreenTool = false
							}
							e.showStatus(pRes.Message)
						}
						e.flushPendingSave()
					})
				})
			}
		})
	})
}

func (e *EditorUI) syncBackendLines() {
	if e.app == nil || !e.imageLoaded {
		return
	}
	linesCopy := append([]lineI(nil), e.lines...)
	gen := e.transitionGen
	e.runBackend(func() {
		if _, err := e.app.ClearLines(); err != nil {
			e.postUI(func() {
				if e.transitionGen == gen {
					e.showError(err)
				}
			})
			return
		}
		for _, ln := range linesCopy {
			if _, err := e.app.AddLine(LineAddRequest{X1: ln.X1, Y1: ln.Y1, X2: ln.X2, Y2: ln.Y2}); err != nil {
				e.postUI(func() {
					if e.transitionGen == gen {
						e.showError(err)
					}
				})
				return
			}
		}
	})
}

func (e *EditorUI) handleNormalCrop() {
	if e.app == nil || !e.imageLoaded || e.normalRect == nil || e.isBusy() {
		return
	}
	r := normalizedRectI(*e.normalRect)
	gen := e.transitionGen
	e.loading = true
	e.loadingMessage = "Cropping image…"
	e.runBackend(func() {
		res, err := e.app.NormalCrop(NormalCropRequest{X1: r.X1, Y1: r.Y1, X2: r.X2, Y2: r.Y2})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.loading = false
			e.loadingMessage = ""
			if err != nil {
				e.showError(err)
				return
			}
			e.normalRect = nil
			e.canRecrop = true
			e.unsavedChanges = true
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				if res.DescreenReset {
					e.useDescreenTool = false
				}
				e.showStatus(res.Message)
			}
			e.flushPendingSave()
		})
	})
}

func (e *EditorUI) handleSkipCrop() {
	if e.app == nil || !e.imageLoaded || e.isBusy() {
		return
	}
	gen := e.beginTransition()
	e.normalRect = nil
	e.selectedCornerPts = nil
	e.cornerCount = 0
	e.lines = nil
	e.linesProcessed = false
	e.clearAdjustmentSelection()
	e.loading = true
	e.loadingMessage = "Skipping crop…"
	e.runBackend(func() {
		res, err := e.app.SkipCrop()
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.loading = false
			e.loadingMessage = ""
			if err != nil {
				e.showError(err)
				return
			}
			e.cropSkipped = true
			e.canRecrop = true
			e.detectedCornerPts = nil
			e.cornersDetected = false
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				if res.DescreenReset {
					e.useDescreenTool = false
				}
				e.showStatus(res.Message)
			}
		})
	})
}

func (e *EditorUI) handleRecrop() {
	if e.app == nil || !e.imageLoaded || e.isBusy() || (!e.canRecrop && !e.cropSkipped) {
		return
	}
	e.confirmDialog = &confirmDialogState{
		Title:       "Re-crop Image",
		Message:     "Re-crop will use the current output as a new source image, resetting all crop and adjustment state. Continue?",
		YesLabel:    "Re-crop",
		CancelLabel: "Cancel",
		YesVariant:  btnPrimary,
		OnYes: func() {
			e.confirmDialog = nil
			e.executeRecrop()
		},
		OnCancel: func() { e.confirmDialog = nil },
	}
	e.invalidate()
}

func (e *EditorUI) executeRecrop() {
	if e.app == nil || !e.imageLoaded || e.isBusy() || (!e.canRecrop && !e.cropSkipped) {
		return
	}
	gen := e.beginTransition()
	e.clearTouchupState()
	e.clearAdjustmentSelection()
	e.optimisticCrop = nil
	e.loading = true
	e.loadingMessage = "Restoring original image…"
	e.runBackend(func() {
		info, err := e.app.RecropImage()
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			if err != nil {
				e.loading = false
				e.loadingMessage = ""
				e.showError(err)
				return
			}
			e.resetImageState(true)
			if info != nil {
				e.imageMeta = imageMetaState{InputDims: image.Pt(info.Width, info.Height)}
				e.realImageDims = image.Pt(info.Width, info.Height)
				e.discSourceDims = e.realImageDims
				e.setPreview(info.Preview)
			}
			e.unsavedChanges = true
			e.mainViewport.Zoom = 1
			e.mainViewport.FitWidth = 0
			e.mainViewport.ScrollX = 0
			e.mainViewport.ScrollY = 0
			if e.mode == "corner" {
				e.detectCornersWithGen(gen)
				return
			}
			e.loading = false
			e.loadingMessage = ""
			e.showStatus("Ready to crop")
		})
	})
}

func (e *EditorUI) handleResetAction() {
	if e.app == nil || !e.imageLoaded || e.isBusy() {
		return
	}
	hadPostCrop := e.canRecrop || e.cropSkipped || e.discActive || e.linesProcessed || e.cornerCount >= 4
	gen := e.beginTransition()

	// Synchronously clear shared post-crop, selection, and tool state across all modes.
	e.cropSkipped = false
	e.canRecrop = false
	e.unsavedChanges = false
	e.optimisticCrop = nil
	e.postSaveAfterSave = nil
	e.clearTouchupState()
	e.useDescreenTool = false
	e.useDustRemovalTool = false
	e.useStraightEdgeTool = false
	e.clearAdjustmentSelection()
	e.blackPoint = 0
	e.whitePoint = 255

	applyResetResponse := func(res *ProcessResult, err error, restoreCorners bool) {
		if e.transitionGen != gen {
			return
		}
		if err != nil {
			e.showError(err)
			return
		}
		if hadPostCrop {
			e.mainViewport.Zoom = 1
			e.mainViewport.FitWidth = 0
			e.mainViewport.ScrollX = 0
			e.mainViewport.ScrollY = 0
		}
		if res != nil {
			if restoreCorners {
				e.detectedCornerPts = append([]image.Point(nil), res.Corners...)
				e.cornersDetected = len(e.detectedCornerPts) > 0
			}
			if res.Preview != "" {
				e.setPreview(res.Preview)
			}
			if res.Width > 0 && res.Height > 0 {
				e.realImageDims = image.Pt(res.Width, res.Height)
				e.discSourceDims = e.realImageDims
			}
			if res.Message != "" {
				e.showStatus(res.Message)
			}
		}
	}

	switch e.mode {
	case "corner":
		e.selectedCornerPts = nil
		e.cornerCount = 0
		e.runBackend(func() {
			res, err := e.app.ResetCorners()
			e.postUI(func() { applyResetResponse(res, err, true) })
		})
	case "disc":
		e.discActive = false
		e.discInitialRadius = 0
		e.discCenter = image.Point{}
		e.discRadius = 0
		e.discRotation = 0
		e.featherSize = 20
		e.discLiveActive = false
		e.discLiveTransform = discLiveTransform{}
		e.clearDiscShiftLive()
		e.runBackend(func() {
			res, err := e.app.ResetDisc()
			e.postUI(func() { applyResetResponse(res, err, false) })
		})
	case "line":
		e.lines = nil
		e.linesProcessed = false
		e.lineDragKind = "none"
		e.lineStartImg = nil
		e.lineHandleDrag = nil
		e.runBackend(func() {
			res, err := e.app.ClearLines()
			e.postUI(func() { applyResetResponse(res, err, false) })
		})
	case "normal":
		e.normalRect = nil
		e.normalDragKind = "none"
		e.normalDragPending = false
		e.normalHandleDrag = ""
		e.normalMoveDrag = nil
		e.runBackend(func() {
			res, err := e.app.ResetNormal()
			e.postUI(func() { applyResetResponse(res, err, false) })
		})
	}
}

func (e *EditorUI) handleHistory(redo bool) {
	if e.app == nil || !e.imageLoaded {
		return
	}
	gen := e.beginTransition()
	e.optimisticCrop = nil
	e.adjustmentRect = nil
	e.touchupStrokes = nil
	e.runBackend(func() {
		var res *ProcessResult
		var err error
		if redo {
			res, err = e.app.Redo()
		} else {
			res, err = e.app.Undo()
		}
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			if err != nil {
				e.showError(err)
				return
			}
			if res == nil || !res.Changed {
				if res != nil && res.Message != "" {
					e.showStatus(res.Message)
				}
				return
			}
			e.cropSkipped = res.CropSkipped
			if res.Uncropped {
				e.canRecrop = false
				e.discActive = false
				e.discInitialRadius = 0
				e.discCenter = image.Point{}
				e.discRadius = 0
				e.discRotation = 0
				e.linesProcessed = false
				if e.mode == "corner" {
					e.detectedCornerPts = append([]image.Point(nil), res.Corners...)
					e.cornersDetected = len(e.detectedCornerPts) > 0
					e.selectedCornerPts = append([]image.Point(nil), res.SelectedCorners...)
					e.cornerCount = len(e.selectedCornerPts)
				}
			} else {
				e.canRecrop = true
			}
			if res.HistoryDiscSettings != nil {
				e.settings.DiscCenterCutout = res.HistoryDiscSettings.CenterCutout
				e.settings.DiscCutoutPercent = res.HistoryDiscSettings.CutoutPercent
			}
			if res.HistoryFeatherSize > 0 {
				e.featherSize = res.HistoryFeatherSize
			}
			if res.DiscRadius > 0 {
				e.discActive = true
				e.discCenter = image.Pt(res.DiscCenterX, res.DiscCenterY)
				e.discRadius = res.DiscRadius
				e.discRotation = res.DiscRotation
				if e.discInitialRadius == 0 {
					e.discInitialRadius = res.DiscRadius
				}
			}
			if res.Black != 0 || res.White != 0 || res.Black < res.White {
				if res.White > 0 {
					e.blackPoint = clamp(res.Black, 0, 254)
					e.whitePoint = clamp(res.White, e.blackPoint+1, 255)
				}
			}
			if res.DescreenReset {
				e.useDescreenTool = false
			}
			if res.Preview != "" {
				e.setPreview(res.Preview)
			}
			if res.Width > 0 && res.Height > 0 {
				e.realImageDims = image.Pt(res.Width, res.Height)
			}
			e.unsavedChanges = !res.Uncropped
			if res.Message != "" {
				e.showStatus(res.Message)
			}
		})
	})
}

func (e *EditorUI) activeSelection() *AdjustmentSelection {
	if !e.adjustmentSelectionActive || e.adjustmentRect == nil {
		return nil
	}
	r := normalizedRectI(*e.adjustmentRect)
	if r.X2 <= r.X1 || r.Y2 <= r.Y1 {
		return nil
	}
	return &AdjustmentSelection{
		X1: float64(r.X1),
		Y1: float64(r.Y1),
		X2: float64(r.X2),
		Y2: float64(r.Y2),
	}
}

func (e *EditorUI) handleTrimBorders() {
	if e.app == nil || !e.canSave() || e.isBusy() {
		return
	}
	gen := e.transitionGen
	e.loading = true
	e.loadingMessage = "Trimming borders…"
	e.runBackend(func() {
		res, err := e.app.TrimBorders()
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.loading = false
			e.loadingMessage = ""
			if err != nil {
				e.showError(err)
				return
			}
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				if res.DescreenReset {
					e.useDescreenTool = false
				}
				e.showStatus(res.Message)
			}
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) handleAutoContrast() {
	if e.app == nil || !e.canSave() || e.isBusy() {
		return
	}
	gen := e.transitionGen
	sel := e.activeSelection()
	e.autoContrastPend = true
	e.showStatus("Applying auto-contrast…")
	e.runBackend(func() {
		res, err := e.app.AutoContrast(AutoContrastRequest{Selection: sel})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.autoContrastPend = false
			if err != nil {
				e.showError(err)
				return
			}
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				if res.DescreenReset {
					e.useDescreenTool = false
				}
				e.showStatus(res.Message)
			}
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) handleDescreen() {
	if e.app == nil || !e.canSave() || e.isBusy() {
		return
	}
	gen := e.transitionGen
	sel := e.activeSelection()
	req := DescreenRequest{
		Thresh:    e.descreenThresh,
		Radius:    e.descreenRadius,
		Middle:    e.descreenMiddle,
		Highlight: e.descreenHighlight,
		Fast:      e.descreenFast,
		Selection: sel,
	}
	e.descreenPending = true
	e.showStatus("Applying descreen…")
	e.runBackend(func() {
		res, err := e.app.Descreen(req)
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.descreenPending = false
			if err != nil {
				e.showError(err)
				return
			}
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				e.showStatus(res.Message)
			}
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) handleDustRemoval() {
	if e.app == nil || !e.canSave() || e.isBusy() {
		return
	}
	gen := e.transitionGen
	dpi := 0.0
	if e.imageMeta.DPIX > 0 && e.imageMeta.DPIY > 0 {
		dpi = max(e.imageMeta.DPIX, e.imageMeta.DPIY)
	} else if e.imageMeta.DPIX > 0 {
		dpi = e.imageMeta.DPIX
	}
	req := DustRemovalRequest{
		Level:     e.dustLevel,
		DPI:       dpi,
		Selection: e.activeSelection(),
	}
	e.dustRemovalPending = true
	e.showStatus("Removing dust…")
	e.runBackend(func() {
		res, err := e.app.DustRemoval(req)
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.dustRemovalPending = false
			if err != nil {
				e.showError(err)
				return
			}
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				if res.DescreenReset {
					e.useDescreenTool = false
				}
				e.showStatus(res.Message)
			}
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) handleLevelsCommit(black, white int) {
	if e.app == nil || !e.imageLoaded {
		return
	}
	e.blackPoint = clamp(black, 0, 254)
	e.whitePoint = clamp(white, e.blackPoint+1, 255)
	gen := e.transitionGen
	req := SetLevelsRequest{
		Black:     e.blackPoint,
		White:     e.whitePoint,
		Selection: e.activeSelection(),
	}
	e.runBackend(func() {
		res, err := e.app.SetLevels(req)
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			if err != nil {
				e.showError(err)
				return
			}
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				if res.DescreenReset {
					e.useDescreenTool = false
				}
			}
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) handleResizeApply(w, h int) {
	if e.app == nil || !e.canSave() || w < 1 || h < 1 {
		return
	}
	gen := e.transitionGen
	e.resizeOpen = false
	e.loading = true
	e.loadingMessage = "Resizing image…"
	e.runBackend(func() {
		res, err := e.app.ResizeImage(ResizeRequest{Width: w, Height: h})
		e.postUI(func() {
			if e.transitionGen != gen {
				return
			}
			e.loading = false
			e.loadingMessage = ""
			if err != nil {
				e.showError(err)
				return
			}
			if res != nil {
				e.setPreview(res.Preview)
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				if res.DescreenReset {
					e.useDescreenTool = false
				}
				e.showStatus(res.Message)
			}
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) commitTouchup() {
	if e.app == nil || len(e.touchupStrokes) == 0 {
		return
	}
	gen := e.transitionGen
	pts := append([]TouchUpPoint(nil), e.touchupStrokes...)
	req := TouchUpStrokeRequest{
		Points:     pts,
		BrushSize:  e.brushSize,
		PatchSize:  9,
		Iterations: 5,
		Selection:  e.activeSelection(),
	}
	e.touchupPending = true
	e.showStatus("Applying touch-up…")
	e.runBackend(func() {
		res, err := e.app.TouchUpApplyStrokes(req)
		if err != nil {
			e.postUI(func() {
				if e.transitionGen != gen {
					return
				}
				e.touchupPending = false
				e.touchupStrokes = nil
				e.showError(err)
			})
			return
		}
		// Synchronous fallback for tests/headless paths where worker didn't return "running"
		if res != nil && res.Message != "running" && res.Preview != "" {
			e.postUI(func() {
				if e.transitionGen != gen {
					return
				}
				e.onTouchupDone(touchUpDoneEvent{
					Preview:       res.Preview,
					Message:       res.Message,
					Width:         res.Width,
					Height:        res.Height,
					DescreenReset: res.DescreenReset,
				})
			})
		}
	})
}

func (e *EditorUI) onTouchupDone(ev touchUpDoneEvent) {
	e.touchupPending = false
	e.touchupStrokes = nil
	if ev.Cancelled {
		e.showStatus("")
		return
	}
	if ev.Error != "" {
		e.showErrorMsg(ev.Error)
		return
	}
	if ev.Preview != "" {
		e.setPreview(ev.Preview)
	}
	if ev.Width > 0 && ev.Height > 0 {
		e.realImageDims = image.Pt(ev.Width, ev.Height)
	}
	if ev.DescreenReset {
		e.useDescreenTool = false
	}
	if !e.settings.TouchupRemainsActive {
		e.useTouchupTool = false
		e.touchupCursor = nil
	}
	e.unsavedChanges = true
	if ev.Message != "" {
		e.showStatus(ev.Message)
	}
	e.flushPendingSave()
}

func (e *EditorUI) handleSaveImage(afterSave func()) {
	if e.app == nil || !e.canSave() {
		return
	}
	if afterSave != nil {
		e.postSaveAfterSave = afterSave
	}
	if e.savingImage {
		return
	}
	e.runBackend(func() {
		outPath, err := e.app.OpenSaveDialog()
		e.postUI(func() {
			if err != nil {
				e.postSaveAfterSave = nil
				e.showError(err)
				return
			}
			if outPath == "" {
				e.postSaveAfterSave = nil
				return
			}
			e.savingImage = true
			e.loading = true
			e.loadingMessage = "Saving image…"
			e.runBackend(func() {
				res, err := e.app.SaveImage(SaveRequest{OutputPath: outPath})
				postCmd := e.settings.PostSaveCommand
				runPost := e.settings.PostSaveEnabled && strings.TrimSpace(postCmd) != ""
				if e.cliPostSaveEnabled && strings.TrimSpace(e.cliPostSaveCmd) != "" {
					runPost = true
					postCmd = e.cliPostSaveCmd
				}
				if err == nil && runPost {
					if pErr := e.app.RunPostSaveCommand(postCmd, outPath); pErr != nil {
						err = pErr
					}
				}
				e.postUI(func() {
					e.savingImage = false
					e.loading = false
					e.loadingMessage = ""
					if err != nil {
						e.postSaveAfterSave = nil
						e.showError(err)
						return
					}
					e.unsavedChanges = false
					if res != nil && res.Message != "" {
						e.showStatus(res.Message)
					} else {
						e.showStatus("Saved image")
					}
					cb := e.postSaveAfterSave
					e.postSaveAfterSave = nil
					if cb != nil {
						cb()
						return
					}
					if e.cliPostSaveExit || e.settings.CloseAfterSave {
						e.allowClose = true
						if e.win != nil {
							e.win.Perform(system.ActionClose)
						}
					}
				})
			})
		})
	})
}

func (e *EditorUI) flushPendingSave() {
	if e.postSaveAfterSave != nil && !e.isBusy() && e.canSave() {
		cb := e.postSaveAfterSave
		e.postSaveAfterSave = nil
		e.handleSaveImage(cb)
	}
}

func (e *EditorUI) handleCloseRequest() {
	if !e.unsavedChanges {
		e.allowClose = true
		if e.win != nil {
			e.win.Perform(system.ActionClose)
		}
		return
	}
	e.confirmDialog = &confirmDialogState{
		Title:       "Unsaved Changes",
		Message:     "You have unsaved changes. Save before closing?",
		YesLabel:    "Save",
		NoLabel:     "Don't save",
		CancelLabel: "Cancel",
		YesVariant:  btnPrimary,
		NoVariant:   btnDanger,
		OnYes: func() {
			e.confirmDialog = nil
			e.handleSaveImage(func() {
				e.allowClose = true
				if e.win != nil {
					e.win.Perform(system.ActionClose)
				}
			})
		},
		OnNo: func() {
			e.confirmDialog = nil
			e.allowClose = true
			if e.win != nil {
				e.win.Perform(system.ActionClose)
			}
		},
		OnCancel: func() {
			e.confirmDialog = nil
		},
	}
	e.invalidate()
}

func (e *EditorUI) mainVisualState() viewportVisualState {
	var cornerPts []image.Point
	var selectedPts []image.Point
	if e.mode == "corner" && !e.useTouchupTool && !e.cropSkipped && e.cornerCount < 4 {
		cornerPts = e.detectedCornerPts
		selectedPts = e.selectedCornerPts
	}
	var normRect *rectI
	if e.mode == "normal" && !e.useTouchupTool && !e.cropSkipped {
		normRect = e.normalRect
	}
	var adjRect *rectI
	if e.adjustmentSelectionActive {
		adjRect = e.adjustmentRect
	}
	var lines []lineI
	if e.mode == "line" && !e.useTouchupTool && !e.linesProcessed && !e.cropSkipped {
		lines = e.lines
	}

	return viewportVisualState{
		Mode:                      e.mode,
		RealImageDims:             e.realImageDims,
		Dragging:                  e.dragging,
		DragStart:                 e.dragStart,
		DragCurrent:               e.dragCurrent,
		UseTouchupTool:            e.useTouchupTool,
		TouchupStrokes:            e.touchupStrokes,
		BrushSize:                 e.brushSize,
		TouchupCursor:             e.touchupCursor,
		UseStraightEdgeTool:       e.useStraightEdgeTool,
		DiscActive:                e.discActive,
		DiscLiveActive:            e.discLiveActive,
		DiscLiveTransform:         e.discLiveTransform,
		DiscShiftLive:             e.visibleDiscShiftLive(),
		DiscCenter:                e.discCenter,
		DiscRadius:                e.discRadius,
		DiscCenterCutout:          e.settings.DiscCenterCutout,
		DiscCutoutPercent:         e.settings.DiscCutoutPercent,
		DetectedCornerPts:         cornerPts,
		SelectedCornerPts:         selectedPts,
		DotRadius:                 e.dotRadius,
		NormalRect:                normRect,
		AdjustmentSelectionActive: e.adjustmentSelectionActive,
		AdjustmentRect:            adjRect,
		Lines:                     lines,
		LineDragKind:              e.lineDragKind,
		LineStartImg:              e.lineStartImg,
		CtrlDragActive:            e.ctrlDrag != nil,
		ShiftDragActive:           e.shiftDrag != nil,
	}
}

func (e *EditorUI) updateStatusFade(gtx layout.Context) {
	if e.statusText == "" {
		e.statusOpacity = 0
		return
	}
	if e.isBusy() {
		e.statusOpacity = 1
		e.statusUpdatedAt = gtx.Now
		return
	}
	elapsed := gtx.Now.Sub(e.statusUpdatedAt)
	const hold = 3500 * time.Millisecond
	const fade = 600 * time.Millisecond
	if elapsed < hold {
		e.statusOpacity = 1
		gtx.Execute(op.InvalidateCmd{At: e.statusUpdatedAt.Add(hold)})
		return
	}
	if elapsed < hold+fade {
		t := float32(elapsed-hold) / float32(fade)
		e.statusOpacity = 1 - t
		gtx.Execute(op.InvalidateCmd{})
		return
	}
	e.statusOpacity = 0
	e.statusText = ""
}

func (e *EditorUI) resolveMainPreview(src string) (*image.NRGBA, bool) {
	if e.app == nil || src == "" {
		return nil, false
	}
	return e.app.ResolvePreviewImage(src)
}

func (e *EditorUI) layoutRoot(gtx layout.Context) layout.Dimensions {
	e.drainUIQueue()
	e.overlays.beginFrame(gtx)
	e.tooltips.beginFrame()
	e.anyEditorFocused = false

	if gtx.Constraints.Max != e.lastWindowSz {
		if e.lastWindowSz != (image.Point{}) {
			e.lastResizeAt = time.Now()
		}
		e.lastWindowSz = gtx.Constraints.Max
	}
	e.updateStatusFade(gtx)

	paint.FillShape(gtx.Ops, colCanvasBg, clip.Rect(image.Rectangle{Max: gtx.Constraints.Max}).Op())

	sidebarW := gtx.Dp(unit.Dp(e.sidebarWidthDp))
	maxSide := gtx.Constraints.Max.X - gtx.Dp(400)
	if maxSide < gtx.Dp(unit.Dp(minSidebarWidthDp)) {
		maxSide = gtx.Dp(unit.Dp(minSidebarWidthDp))
	}
	if sidebarW > maxSide {
		sidebarW = maxSide
	}

	layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			cg := gtx
			cg.Constraints = layout.Exact(image.Pt(sidebarW, gtx.Constraints.Max.Y))
			return e.layoutSidebar(cg)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(e.layoutToolbar),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return e.mainViewport.Layout(
						gtx,
						e.th,
						e.resolveMainPreview,
						e.preview,
						e.realImageDims,
						!e.imageLoaded,
						"Load or drop an image to begin",
						e.optimisticCrop,
						e.mainVisualState(),
						viewportCallbacks{
							Cursor:     e.resolveCanvasCursor(gtx),
							GetCursor:  e.resolveCanvasCursor,
							GetVisual:  e.mainVisualState,
							OnPointer: func(gtx layout.Context, info viewportLayoutInfo, pe pointer.Event) {
								e.handleCanvasPointer(gtx, info, pe)
							},
							OnWheelFeather: func(deltaY float32) {
								if deltaY > 0 {
									e.setFeatherSizeAndApply(e.featherSize - 2)
								} else if deltaY < 0 {
									e.setFeatherSizeAndApply(e.featherSize + 2)
								}
							},
							OnPresented: func(source string, dims image.Point) {
								if e.optimisticCrop != nil && source != e.optimisticCrop.Source {
									e.optimisticCrop = nil
									e.invalidate()
								}
								e.onDiscShiftPresented(source)
							},
							Invalidate: e.invalidate,
							DebugLog: func(format string, args ...any) {
								if e.app != nil {
									e.app.Logf(format, args...)
								}
							},
							// The live Ctrl+drag layer covers the raster; keep its
							// replacement build off the UI thread.
							PreferAsyncRaster: e.visibleDiscShiftLive() != nil,
						},
					)
				}),
				layout.Rigid(e.layoutStatusBar),
			)
		}),
	)

	e.maybeWarmDiscShift()

	// Sidebar resize sash positioned right over the sidebar/canvas border
	e.layoutSidebarSash(gtx, sidebarW)

	if e.loadingFull && e.loadingMessage != "" {
		e.layoutLoadingOverlay(gtx)
	}
	if e.hasOpenModal() {
		e.layoutModals(gtx)
	}

	// Process global shortcuts AFTER widgets/modals so focused editors capture text keys first.
	e.processGlobalShortcuts(gtx)

	e.overlays.layoutOverlayPass(gtx)
	e.tooltips.endFrame(gtx, e.th)

	return layout.Dimensions{Size: gtx.Constraints.Max}
}
