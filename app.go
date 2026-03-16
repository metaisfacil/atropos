package main

import (
	"bytes"
	"context"
	"encoding/base64"
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
	if a.currentImage == nil && a.warpedImage == nil {
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

// TouchUpFill accepts a base64-encoded PNG mask (white where the user painted)
// and returns a preview produced by the PatchMatch-based filler. The mask may
// be at any resolution; it will be resized to match the current image.
func (a *App) TouchUpFill(maskB64 string, patchSize int, iterations int) (*ProcessResult, error) {
	a.logf("TouchUpFill: patchSize=%d iterations=%d", patchSize, iterations)
	if a.currentImage == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}

	data, err := base64.StdEncoding.DecodeString(maskB64)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	// Convert decoded image to *image.Alpha
	b := img.Bounds()
	mask := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			aVal := c.A
			if aVal == 0 {
				// if no alpha channel, use luminance threshold
				lum := (299*uint32(c.R) + 587*uint32(c.G) + 114*uint32(c.B)) / 1000
				if lum > 10 {
					aVal = 255
				}
			}
			mask.Pix[(y-b.Min.Y)*mask.Stride+(x-b.Min.X)] = aVal
		}
	}

	// Resize mask to the working image size if needed
	srcImg := a.workingImage()
	if srcImg == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}
	tgtBounds := srcImg.Bounds()
	if !mask.Bounds().Eq(tgtBounds) {
		// convert Alpha -> Gray -> resizeGray -> Alpha
		gray := image.NewGray(mask.Bounds())
		for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
			for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
				v := mask.Pix[(y-mask.Bounds().Min.Y)*mask.Stride+(x-mask.Bounds().Min.X)]
				gray.Pix[(y-mask.Bounds().Min.Y)*gray.Stride+(x-mask.Bounds().Min.X)] = v
			}
		}
		resized := resizeGray(gray, tgtBounds.Dx(), tgtBounds.Dy())
		newMask := image.NewAlpha(tgtBounds)
		for y := 0; y < tgtBounds.Dy(); y++ {
			for x := 0; x < tgtBounds.Dx(); x++ {
				v := resized.Pix[y*resized.Stride+x]
				newMask.Pix[y*newMask.Stride+x] = v
			}
		}
		mask = newMask
	}

	// Run PatchMatchFill (non-destructive preview) on the working image
	out := PatchMatchFill(srcImg, mask, patchSize, iterations)

	preview, err := imageToBase64(out)
	if err != nil {
		return nil, err
	}
	b2 := out.Bounds()
	return &ProcessResult{Preview: preview, Message: "Touch-up preview", Width: b2.Dx(), Height: b2.Dy()}, nil
}

// TouchUpApply applies a touch-up fill to the working image, saving an undo
// snapshot so the change can be reverted. Returns the new preview.
func (a *App) TouchUpApply(maskB64 string, patchSize int, iterations int) (*ProcessResult, error) {
	a.logf("TouchUpApply: patchSize=%d iterations=%d", patchSize, iterations)
	if a.currentImage == nil && a.warpedImage == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}

	data, err := base64.StdEncoding.DecodeString(maskB64)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	// Convert decoded image to *image.Alpha
	b := img.Bounds()
	mask := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			aVal := c.A
			if aVal == 0 {
				lum := (299*uint32(c.R) + 587*uint32(c.G) + 114*uint32(c.B)) / 1000
				if lum > 10 {
					aVal = 255
				}
			}
			mask.Pix[(y-b.Min.Y)*mask.Stride+(x-b.Min.X)] = aVal
		}
	}

	// Resize mask to the working image size if needed
	srcImg := a.workingImage()
	if srcImg == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}
	tgtBounds := srcImg.Bounds()
	if !mask.Bounds().Eq(tgtBounds) {
		gray := image.NewGray(mask.Bounds())
		for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
			for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
				v := mask.Pix[(y-mask.Bounds().Min.Y)*mask.Stride+(x-mask.Bounds().Min.X)]
				gray.Pix[(y-mask.Bounds().Min.Y)*gray.Stride+(x-mask.Bounds().Min.X)] = v
			}
		}
		resized := resizeGray(gray, tgtBounds.Dx(), tgtBounds.Dy())
		newMask := image.NewAlpha(tgtBounds)
		for y := 0; y < tgtBounds.Dy(); y++ {
			for x := 0; x < tgtBounds.Dx(); x++ {
				v := resized.Pix[y*resized.Stride+x]
				newMask.Pix[y*newMask.Stride+x] = v
			}
		}
		mask = newMask
	}

	// Run PatchMatchFill and apply result
	out := PatchMatchFill(srcImg, mask, patchSize, iterations)

	// save undo snapshot and apply
	a.saveUndo()
	a.setWorkingImage(out)

	preview, err := imageToBase64(out)
	if err != nil {
		return nil, err
	}
	b2 := out.Bounds()
	return &ProcessResult{Preview: preview, Message: "Touch-up applied.", Width: b2.Dx(), Height: b2.Dy()}, nil
}
