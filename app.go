package main

import (
	"context"
	"image"
	"image/color"
	"log"
	"sync"
)

// App struct holds all application state.
type App struct {
	ctx    context.Context
	logger *log.Logger
	loadMu sync.Mutex

	// Image state
	originalImage   *image.NRGBA
	currentImage    *image.NRGBA
	warpedImage     *image.NRGBA
	levelsBaseImage *image.NRGBA // snapshot taken before slider dragging begins; always the source for SetLevels
	imageLoaded     bool
	undoStack       []undoEntry

	// Processing state
	detectedCorners []image.Point
	selectedCorners []image.Point
	cornerDotRadius int
	lines           [][]image.Point
	discCenter      image.Point
	discRadius      int
	rotationAngle   float64

	// discBaseImage is a snapshot of currentImage taken the moment DrawDisc is
	// first called. All redrawDisc calls read from this so that pre-disc
	// adjustments (levels, auto-contrast) are preserved across every subsequent
	// shift / rotate / feather operation.
	discBaseImage *image.NRGBA

	// discWorkingCrop is a pre-cropped sub-region of discBaseImage centred on
	// the disc with a generous extra margin. redrawDisc crops from this small
	// image instead of the full discBaseImage, avoiding the cache thrashing
	// caused by large image strides. discWorkingCropRect records which rect of
	// discBaseImage was captured (in discBaseImage coords) so we can detect
	// when a shift has moved the disc outside the cached region and refresh.
	discWorkingCrop     *image.NRGBA
	discWorkingCropRect image.Rectangle

	// postDiscBlack / postDiscWhite record any levels stretch applied after the
	// disc was committed. redrawDisc re-applies them at the end of every render
	// so that disc re-renders (shift, rotate, feather) never silently discard a
	// tonal adjustment the user already made.
	postDiscBlack int // default 0   (no stretch)
	postDiscWhite int // default 255 (no stretch)

	// Background color for disc mode
	bgColor color.NRGBA

	// Last loaded file path (for save dialog defaults)
	loadedFilePath string

	// Configuration
	cropTop, cropBottom, cropLeft, cropRight int
	featherSize                              int
	undoLimit                                int
	cropAmount                               int

	// Launch arguments (set before startup)
	launchFilePath string
	launchMode     string // "corner", "disc", or "line"

	// Touch-up backend settings
	touchupBackend string // "patchmatch" or "iopaint"
	iopaintURL     string

	// touchupCancel cancels an in-flight TouchUpApply (PatchMatch or IOPaint).
	// Protected by touchupMu; nil when no operation is running.
	touchupMu     sync.Mutex
	touchupCancel context.CancelFunc

	// Warp out-of-bounds fill settings
	warpFillMode  string      // "clamp", "fill", or "outpaint"
	warpFillColor color.NRGBA // used when warpFillMode == "fill"

	// Disc mode settings
	discCenterCutout  bool // if true, a centered hole is cut out to expose the bg colour
	discCutoutPercent int  // diameter of the cutout as a percentage of the disc diameter (1–50)
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		undoLimit:      10,
		featherSize:    15,
		cropAmount:     3,
		undoStack:      []undoEntry{},
		bgColor:        color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		postDiscWhite:  255,
		touchupBackend:   "patchmatch",
		iopaintURL:       "http://127.0.0.1:8086/",
		warpFillMode:     "clamp",
		warpFillColor:    color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		discCenterCutout:  true,
		discCutoutPercent: 11,
	}
}

// logf writes a formatted message to the debug log if enabled.
func (a *App) logf(format string, args ...interface{}) {
	if a.logger != nil {
		a.logger.Printf(format, args...)
	}
}

// LogFrontend writes a message from the frontend into the debug log file.
func (a *App) LogFrontend(msg string) {
	a.logf("[FE] %s", msg)
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.logf("startup: context initialized")
}

// shutdown is called when the app is being destroyed.
func (a *App) shutdown(ctx context.Context) {
	a.logf("shutdown: application closing")
}

// ProcessResult is the standard response for image processing operations,
// carrying an optional preview, status message, and image dimensions.
type ProcessResult struct {
	Preview string        `json:"preview"`
	Message string        `json:"message"`
	Width   int           `json:"width"`
	Height  int           `json:"height"`
	// Optional numeric results (e.g. from AutoContrast)
	Black   int           `json:"black,omitempty"`
	White   int           `json:"white,omitempty"`
	// Corners is populated by DetectCorners and ResetCorners so the frontend
	// can render the overlay dots via SVG instead of baking them into the image.
	Corners []image.Point `json:"corners,omitempty"`
}

// LaunchArgs contains the initial file path and mode from CLI arguments.
type LaunchArgs struct {
	FilePath string `json:"filePath"`
	Mode     string `json:"mode"`
}

// GetLaunchArgs returns any CLI-provided file path and mode.
func (a *App) GetLaunchArgs() LaunchArgs {
	return LaunchArgs{FilePath: a.launchFilePath, Mode: a.launchMode}
}
