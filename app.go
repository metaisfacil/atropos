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
	undoStack       []*image.NRGBA

	// Processing state
	detectedCorners []image.Point
	selectedCorners []image.Point
	cornerDotRadius int
	lines           [][]image.Point
	discCenter      image.Point
	discRadius      int
	rotationAngle   float64

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
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		undoLimit:   10,
		featherSize: 15,
		cropAmount:  3,
		undoStack:   []*image.NRGBA{},
		bgColor:     color.NRGBA{R: 255, G: 255, B: 255, A: 255},
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
	Preview string `json:"preview"`
	Message string `json:"message"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// LaunchArgs contains the initial file path and mode from CLI arguments.
type LaunchArgs struct {
	FilePath string `json:"filePath"`
	Mode     string `json:"mode"`
}

// GetLaunchArgs returns any CLI-provided file path and mode.
func (a *App) GetLaunchArgs() *LaunchArgs {
	a.logf("GetLaunchArgs: filePath=%q mode=%q", a.launchFilePath, a.launchMode)
	return &LaunchArgs{
		FilePath: a.launchFilePath,
		Mode:     a.launchMode,
	}
}

// GetCleanPreview returns the current image as a base64 preview without
// any corner/disc/line overlay annotations.
func (a *App) GetCleanPreview() (*ProcessResult, error) {
	a.logf("GetCleanPreview")
	if a.currentImage == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}
	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview: preview,
		Message: "",
		Width:   b.Dx(),
		Height:  b.Dy(),
	}, nil
}
