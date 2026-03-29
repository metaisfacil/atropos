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
	// ----------------
	// `originalImage`:
	//   The full-resolution decoded source image loaded from disk. This image
	//   is treated as immutable after `LoadImage()` — it is never modified in
	//   place. Use this when you need the unmodified source pixels.
	originalImage *image.NRGBA

	// `currentImage`:
	//   A working copy of the source image that holds pre-warp adjustments
	//   (levels, auto-contrast, etc.). This is the image used for most
	//   transient, non-committing edits. When no crop/warp/disc has been
	//   committed, `workingImage()` will return `currentImage`.
	currentImage *image.NRGBA

	// `warpedImage`:
	//   The committed output image after a warp/crop/disc operation. Once a
	//   cropping/warp operation completes this field is set and becomes the
	//   authoritative image for subsequent adjustments and for `SaveImage()`.
	//   If `warpedImage` is nil, the app falls back to `currentImage`.
	warpedImage *image.NRGBA

	// `levelsBaseImage`:
	//   A snapshot captured the first time the user starts dragging the
	//   levels sliders after a committing operation. Slider ticks apply levels
	//   relative to this base so continuous slider motion doesn't stack
	//   repeatedly. This snapshot is cleared by `saveUndo()` so the next
	//   adjustment session starts from the current pixels.
	levelsBaseImage *image.NRGBA

	// `descreenBaseImage`:
	//   A snapshot captured the first time Descreen is applied after a
	//   committing operation (or after any non-descreen change to warpedImage).
	//   Subsequent descreen calls in the same uninterrupted session apply to
	//   this base so that changing parameters always operates on the same
	//   source image, not the already-processed result. Cleared by saveUndo()
	//   so the next session starts from the then-current pixels.
	descreenBaseImage *image.NRGBA

	// `descreenResultImage`:
	//   Pointer to the *image.NRGBA last written by applyDescreen. Used to
	//   detect when warpedImage has been modified by a non-descreen operation
	//   (e.g. SetLevels, which does not call saveUndo) so that a re-snapshot
	//   is taken before the next descreen call rather than silently discarding
	//   the intervening adjustment.
	descreenResultImage *image.NRGBA

	imageLoaded bool
	undoStack   []undoEntry

	// Processing state
	detectedCorners []image.Point
	selectedCorners []image.Point
	lines           [][]image.Point
	discCenter      image.Point
	discRadius      int
	rotationAngle   float64

	// discBaseImage is a snapshot of currentImage taken the moment DrawDisc is
	// first called. All redrawDisc calls read from this so that pre-disc
	// adjustments (levels, auto-contrast) are preserved across every subsequent
	// shift / rotate / feather operation.
	discBaseImage *image.NRGBA

	// discNoMaskPreview keeps a base preview image for disc mode that does not
	// include the masking/fill overlay; used by real-time translate/rotate drags.
	discNoMaskPreview string

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
	// postSaveCmd, when non-empty, overrides persisted frontend settings
	// and will be executed after a successful save (placeholder {path} allowed).
	postSaveCmd string
	// If true, quit the application after launching the CLI post-save command.
	postSaveExit bool

	// Touch-up backend settings
	touchupBackend string // "patchmatch" or "iopaint"
	iopaintURL     string

	// close flow state
	closeConfirmed bool

	// touchupCancel cancels an in-flight TouchUpApply (PatchMatch or IOPaint).
	// Protected by touchupMu; nil when no operation is running.
	touchupMu     sync.Mutex
	touchupCancel context.CancelFunc

	// cornerDetectCancel cancels an in-flight DetectCorners call.
	// Protected by cornerDetectMu; nil when no operation is running.
	cornerDetectMu     sync.Mutex
	cornerDetectCancel context.CancelFunc

	// Warp out-of-bounds fill settings
	warpFillMode  string      // "clamp", "fill", or "outpaint"
	warpFillColor color.NRGBA // used when warpFillMode == "fill"

	// Disc mode settings
	discCenterCutout  bool // if true, a centered hole is cut out to expose the bg colour
	discCutoutPercent int  // diameter of the cutout as a percentage of the disc diameter (1–50)

	// Compositor state
	compositorMu     sync.Mutex
	compositorResult *image.NRGBA
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		undoLimit:         10,
		featherSize:       15,
		cropAmount:        3,
		undoStack:         []undoEntry{},
		bgColor:           color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		postDiscWhite:     255,
		touchupBackend:    "patchmatch",
		iopaintURL:        "http://127.0.0.1:8086/",
		warpFillMode:      "clamp",
		warpFillColor:     color.NRGBA{R: 255, G: 255, B: 255, A: 255},
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

// ConfirmClose allows the JS close handler to permit the next OnBeforeClose.
func (a *App) ConfirmClose() {
	a.closeConfirmed = true
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
	Preview string `json:"preview"`
	Message string `json:"message"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	// Optional numeric results (e.g. from AutoContrast)
	Black int `json:"black,omitempty"`
	White int `json:"white,omitempty"`
	// Corners is populated by DetectCorners and ResetCorners so the frontend
	// can render the overlay dots via SVG instead of baking them into the image.
	Corners []image.Point `json:"corners,omitempty"`
	// SelectedCorners is populated by Undo when restoring a pre-warp corner
	// selection, so the frontend can restore the in-progress corner dots.
	SelectedCorners []image.Point `json:"selectedCorners,omitempty"`
	// Uncropped is set by Undo when the popped entry was saved before any warp
	// had produced a warpedImage — i.e. undoing the initial crop itself.  The
	// frontend uses this to return the UI to the cropping phase.
	Uncropped bool `json:"uncropped,omitempty"`
	// DescreenReset is true when the operation invalidated an active descreen
	// session, meaning the next Descreen call will re-snapshot from scratch.
	// The frontend uses this to untoggle the Descreen button as a subtle hint.
	DescreenReset bool `json:"descreenReset,omitempty"`
	// UnmaskedPreview is the preview of disc source image without the disc mask
	// above it, used during live preview drag operations.
	UnmaskedPreview string `json:"unmaskedPreview,omitempty"`
	// Disc preview metadata (returned for mode sync)
	DiscCenterX  int     `json:"discCenterX,omitempty"`
	DiscCenterY  int     `json:"discCenterY,omitempty"`
	DiscRadius   int     `json:"discRadius,omitempty"`
	DiscRotation float64 `json:"discRotation,omitempty"`
	DiscBgR      int     `json:"discBgR,omitempty"`
	DiscBgG      int     `json:"discBgG,omitempty"`
	DiscBgB      int     `json:"discBgB,omitempty"`
}

// LaunchArgs contains the initial file path and mode from CLI arguments.
type LaunchArgs struct {
	FilePath        string `json:"filePath"`
	Mode            string `json:"mode"`
	PostSaveCommand string `json:"postSaveCommand,omitempty"`
	PostSaveEnabled bool   `json:"postSaveEnabled,omitempty"`
	PostSaveExit    bool   `json:"postSaveExit,omitempty"`
}

// GetLaunchArgs returns any CLI-provided file path and mode.
func (a *App) GetLaunchArgs() LaunchArgs {
	return LaunchArgs{
		FilePath:        a.launchFilePath,
		Mode:            a.launchMode,
		PostSaveCommand: a.postSaveCmd,
		PostSaveEnabled: a.postSaveCmd != "",
		PostSaveExit:    a.postSaveExit,
	}
}
