package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// App struct holds all application state.
type App struct {
	ctx    context.Context
	logger *log.Logger
	loadMu sync.Mutex

	// Image state
	originalImage *image.NRGBA
	currentImage  *image.NRGBA
	warpedImage   *image.NRGBA
	imageLoaded   bool
	undoStack     []*image.NRGBA

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

// ============================================================
// IMAGE I/O
// ============================================================

// LoadImageRequest contains the file path for loading an image.
type LoadImageRequest struct {
	FilePath string `json:"filePath"`
}

// ImageInfo contains image metadata and preview data.
type ImageInfo struct {
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Preview string `json:"preview"`
}

// LoadImage loads an image from disk and returns its metadata.
func (a *App) LoadImage(req LoadImageRequest) (*ImageInfo, error) {
	if !a.loadMu.TryLock() {
		a.logf("LoadImage: rejected, another load is already in progress")
		return nil, fmt.Errorf("another image is still loading — please wait")
	}
	defer a.loadMu.Unlock()

	a.logf("LoadImage: filePath=%q", req.FilePath)

	t0 := time.Now()
	src, err := a.decodeImageFile(req.FilePath)
	if err != nil {
		a.logf("LoadImage: decode error: %v", err)
		return nil, fmt.Errorf("failed to decode %s: %w", req.FilePath, err)
	}
	a.logf("LoadImage: decode took %v", time.Since(t0))

	t1 := time.Now()
	nrgba := toNRGBA(src)
	src = nil // allow GC to reclaim decoded image
	a.logf("LoadImage: toNRGBA took %v", time.Since(t1))

	t2 := time.Now()
	a.originalImage = nrgba            // reuse — toNRGBA already made a fresh copy
	a.currentImage = cloneImage(nrgba) // one clone instead of two
	a.warpedImage = nil
	a.imageLoaded = true
	a.loadedFilePath = req.FilePath
	a.selectedCorners = nil
	a.detectedCorners = nil
	a.lines = nil
	a.undoStack = nil
	a.logf("LoadImage: clone took %v", time.Since(t2))

	t3 := time.Now()
	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		a.logf("LoadImage: base64 error: %v", err)
		return nil, err
	}
	a.logf("LoadImage: preview took %v", time.Since(t3))

	b := nrgba.Bounds()
	a.logf("LoadImage: total %v, returning %dx%d, preview len=%d", time.Since(t0), b.Dx(), b.Dy(), len(preview))

	// Update window title with the filename
	name := filepath.Base(req.FilePath)
	runtime.WindowSetTitle(a.ctx, "Atropos — "+name)
	return &ImageInfo{
		Width:   b.Dx(),
		Height:  b.Dy(),
		Preview: preview,
	}, nil
}

// decodeImageFile decodes an image file. For TIFF files it prefers ImageMagick
// (which links the fast C libtiff library) and pipes BMP for minimal overhead.
// Falls back to Go's standard decoders, then ImageMagick for exotic formats.
func (a *App) decodeImageFile(path string) (image.Image, error) {
	ext := strings.ToLower(filepath.Ext(path))

	// ── Fast path for TIFFs: use ImageMagick if available ──────────
	if ext == ".tif" || ext == ".tiff" {
		img, err := a.decodeViaMagick(path, "bmp3")
		if err == nil {
			return img, nil
		}
		a.logf("decodeImageFile: magick fast-path failed (%v), trying Go decoder", err)
	}

	// ── Standard Go decoders ───────────────────────────────────────
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	src, format, err := image.Decode(f)
	if err == nil {
		a.logf("decodeImageFile: standard decoder OK, format=%q bounds=%v", format, src.Bounds())
		return src, nil
	}

	stdErr := err
	a.logf("decodeImageFile: standard decoder failed (%v), trying ImageMagick fallback", stdErr)

	// ── ImageMagick fallback for exotic formats ────────────────────
	img, magickErr := a.decodeViaMagick(path, "bmp3")
	if magickErr != nil {
		return nil, fmt.Errorf("standard decode failed: %w; ImageMagick also failed: %v", stdErr, magickErr)
	}
	return img, nil
}

// decodeViaMagick shells out to ImageMagick to decode an image file.
// outFmt should be a fast-to-decode format like "bmp3" (raw pixels, tiny header).
func (a *App) decodeViaMagick(path, outFmt string) (image.Image, error) {
	magickPath, lookErr := exec.LookPath("magick")
	if lookErr != nil {
		return nil, fmt.Errorf("ImageMagick not found")
	}

	cmd := exec.Command(magickPath, "convert", path, outFmt+":-")
	hideCommandWindow(cmd)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		a.logf("decodeViaMagick: magick failed: %v stderr=%s", runErr, stderr.String())
		return nil, fmt.Errorf("magick: %v", runErr)
	}

	a.logf("decodeViaMagick: magick produced %d bytes of %s", stdout.Len(), outFmt)
	img, decErr := bmp.Decode(bytes.NewReader(stdout.Bytes()))
	if decErr != nil {
		a.logf("decodeViaMagick: bmp decode failed: %v", decErr)
		return nil, fmt.Errorf("bmp decode: %v", decErr)
	}

	a.logf("decodeViaMagick: OK, bounds=%v", img.Bounds())
	return img, nil
}

// SaveRequest specifies the output file path for saving the processed image.
type SaveRequest struct {
	OutputPath string `json:"outputPath"`
}

// SaveImage writes the processed image to disk.
func (a *App) SaveImage(req SaveRequest) (*ProcessResult, error) {
	a.logf("SaveImage: outputPath=%q", req.OutputPath)
	if a.warpedImage == nil {
		a.logf("SaveImage: no image to save")
		return nil, fmt.Errorf("no image to save")
	}

	f, err := os.Create(req.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", req.OutputPath, err)
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(req.OutputPath))
	switch ext {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(f, a.warpedImage, &jpeg.Options{Quality: 95})
	case ".bmp":
		err = bmp.Encode(f, a.warpedImage)
	case ".tiff", ".tif":
		err = tiff.Encode(f, a.warpedImage, nil)
	default:
		err = png.Encode(f, a.warpedImage)
	}
	if err != nil {
		a.logf("SaveImage: encode error: %v", err)
		return nil, fmt.Errorf("failed to encode image: %w", err)
	}

	a.logf("SaveImage: saved successfully to %s", req.OutputPath)
	return &ProcessResult{
		Message: fmt.Sprintf("Saved to %s", req.OutputPath),
	}, nil
}

// OpenSaveDialog shows a file picker for saving images.
func (a *App) OpenSaveDialog() (string, error) {
	a.logf("OpenSaveDialog: showing dialog")

	defaultDir := ""
	defaultName := "output"
	if a.loadedFilePath != "" {
		defaultDir = filepath.Dir(a.loadedFilePath)
		base := filepath.Base(a.loadedFilePath)
		defaultName = strings.TrimSuffix(base, filepath.Ext(base))
	}

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "Save Image",
		DefaultDirectory: defaultDir,
		DefaultFilename:  defaultName,
		Filters: []runtime.FileFilter{
			{DisplayName: "PNG Files (*.png)", Pattern: "*.png"},
			{DisplayName: "JPEG Files (*.jpg;*.jpeg)", Pattern: "*.jpg;*.jpeg"},
			{DisplayName: "BMP Files (*.bmp)", Pattern: "*.bmp"},
			{DisplayName: "TIFF Files (*.tiff;*.tif)", Pattern: "*.tiff;*.tif"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	a.logf("OpenSaveDialog: path=%q err=%v", path, err)
	return path, err
}

// OpenImageDialog shows a file picker for loading images.
func (a *App) OpenImageDialog() (string, error) {
	a.logf("OpenImageDialog: showing dialog")
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Open Image",
		Filters: []runtime.FileFilter{
			{DisplayName: "Image Files (*.png;*.jpg;*.jpeg;*.bmp;*.tiff;*.tif)", Pattern: "*.png;*.jpg;*.jpeg;*.bmp;*.tiff;*.tif"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	a.logf("OpenImageDialog: path=%q err=%v", path, err)
	return path, err
}

// ============================================================
// CORNER DETECTION MODE
// ============================================================

// CornerDetectRequest contains parameters for the Shi-Tomasi corner detector.
type CornerDetectRequest struct {
	MaxCorners   int     `json:"maxCorners"`
	QualityLevel float64 `json:"qualityLevel"`
	MinDistance  int     `json:"minDistance"`
	AccentValue  int     `json:"accentValue"`
	DotRadius    int     `json:"dotRadius"`
}

// ProcessResult is the standard response for image processing operations,
// carrying an optional preview, status message, and image dimensions.
type ProcessResult struct {
	Preview string `json:"preview"`
	Message string `json:"message"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// drawCornerOverlay renders detected (red) and selected (green) corner dots
// onto a clone of currentImage and returns the preview with dimensions.
func (a *App) drawCornerOverlay(dr int) (*ProcessResult, error) {
	if dr < 2 {
		dr = 2
	}
	vis := cloneImage(a.currentImage)
	for _, c := range a.detectedCorners {
		drawFilledCircle(vis, c, dr+2, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		drawFilledCircle(vis, c, dr, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	}
	selDR := dr + dr/2
	if selDR < dr+4 {
		selDR = dr + 4
	}
	for _, c := range a.selectedCorners {
		drawFilledCircle(vis, c, selDR+2, color.NRGBA{R: 0, G: 200, B: 0, A: 255})
		drawFilledCircle(vis, c, selDR, color.NRGBA{R: 0, G: 255, B: 0, A: 255})
	}
	preview, err := imageToBase64(vis)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview: preview,
		Width:   b.Dx(),
		Height:  b.Dy(),
	}, nil
}

// warpFromCorners sorts 4 corner points and applies a perspective transform,
// storing the result in warpedImage and resetting crop offsets.
func (a *App) warpFromCorners(corners []image.Point) (*image.NRGBA, int, int, error) {
	sorted := sortVertices(corners[:4])

	w1 := dist(sorted[0], sorted[1])
	h1 := dist(sorted[0], sorted[2])
	w2 := dist(sorted[2], sorted[3])
	h2 := dist(sorted[1], sorted[3])
	width := int(math.Max(w1, w2))
	height := int(math.Max(h1, h2))
	if width < 10 || height < 10 {
		return nil, 0, 0, fmt.Errorf("selected area too small (%dx%d)", width, height)
	}

	dst := [4]image.Point{
		{0, 0}, {width, 0}, {0, height}, {width, height},
	}
	warped := perspectiveTransform(a.currentImage,
		[4]image.Point{sorted[0], sorted[1], sorted[2], sorted[3]},
		dst, width, height)

	a.warpedImage = warped
	a.cropTop, a.cropBottom, a.cropLeft, a.cropRight = 0, 0, 0, 0
	return warped, width, height, nil
}

// DetectCorners detects corners in the current image using Shi-Tomasi algorithm.
func (a *App) DetectCorners(req CornerDetectRequest) (*ProcessResult, error) {
	a.logf("DetectCorners: maxCorners=%d qualityLevel=%.2f minDistance=%d accentValue=%d",
		req.MaxCorners, req.QualityLevel, req.MinDistance, req.AccentValue)
	if !a.imageLoaded {
		a.logf("DetectCorners: no image loaded")
		return nil, fmt.Errorf("no image loaded")
	}

	b := a.currentImage.Bounds()
	imgW, imgH := b.Dx(), b.Dy()

	// Downsample to max ~1500px on longest side for fast detection
	const maxDetectDim = 1500
	scaleFactor := 1.0
	workW, workH := imgW, imgH
	if imgW > maxDetectDim || imgH > maxDetectDim {
		if imgW > imgH {
			scaleFactor = float64(maxDetectDim) / float64(imgW)
		} else {
			scaleFactor = float64(maxDetectDim) / float64(imgH)
		}
		workW = int(float64(imgW) * scaleFactor)
		workH = int(float64(imgH) * scaleFactor)
	}
	a.logf("DetectCorners: image %dx%d, working at %dx%d (scale=%.3f)", imgW, imgH, workW, workH, scaleFactor)

	adjusted := applyAccentAdjustment(a.currentImage, req.AccentValue)
	gray := toGrayscale(adjusted)

	var workGray *image.Gray
	if scaleFactor < 1.0 {
		workGray = resizeGray(gray, workW, workH)
	} else {
		workGray = gray
	}

	enhanced := applyCLAHE(workGray, 2.0, 8)

	quality := req.QualityLevel / 100.0
	if quality <= 0 {
		quality = 0.01
	}

	workMinDist := int(float64(req.MinDistance) * scaleFactor)
	if workMinDist < 1 {
		workMinDist = 1
	}

	a.logf("DetectCorners: running goodFeaturesToTrack on %dx%d", workW, workH)
	corners := goodFeaturesToTrack(enhanced, req.MaxCorners, quality, workMinDist, 7)
	a.logf("DetectCorners: got %d raw corners", len(corners))

	// Scale corner coordinates back to original image space
	var fullCorners []image.Point
	for _, c := range corners {
		fullCorners = append(fullCorners, image.Pt(
			int(float64(c.X)/scaleFactor),
			int(float64(c.Y)/scaleFactor),
		))
	}
	a.detectedCorners = fullCorners
	a.logf("DetectCorners: %d corners mapped to full resolution", len(a.detectedCorners))

	dr := req.DotRadius
	if dr < 2 {
		dr = 2
	}
	a.cornerDotRadius = dr

	result, err := a.drawCornerOverlay(dr)
	if err != nil {
		return nil, err
	}
	result.Message = fmt.Sprintf("Detected %d corners", len(a.detectedCorners))
	a.logf("DetectCorners: preview generated, dotRadius=%d", dr)
	return result, nil
}

// ClickCornerRequest holds the image-space coordinates of a user click.
type ClickCornerRequest struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Custom    bool `json:"custom"`
	DotRadius int  `json:"dotRadius"`
}

// ClickCornerResult is returned after each corner click.
type ClickCornerResult struct {
	Preview string `json:"preview"`
	Message string `json:"message"`
	Count   int    `json:"count"`
	Done    bool   `json:"done"`
}

// ClickCorner registers a corner selection click. If detected corners exist
// the click is snapped to the nearest one; otherwise the raw coordinate is used.
// After 4 corners the perspective warp is performed automatically.
func (a *App) ClickCorner(req ClickCornerRequest) (*ClickCornerResult, error) {
	a.logf("ClickCorner: x=%d y=%d custom=%v dotRadius=%d", req.X, req.Y, req.Custom, req.DotRadius)
	if !a.imageLoaded {
		return nil, fmt.Errorf("no image loaded")
	}

	dr := req.DotRadius
	if dr < 2 {
		dr = a.cornerDotRadius
	}
	if dr < 2 {
		dr = 2
	}

	// Snap to nearest detected corner unless custom mode
	pt := image.Pt(req.X, req.Y)
	if !req.Custom && len(a.detectedCorners) > 0 {
		bestDist := math.MaxFloat64
		bestPt := pt
		for _, c := range a.detectedCorners {
			d := dist(pt, c)
			if d < bestDist {
				bestDist = d
				bestPt = c
			}
		}
		pt = bestPt
		a.logf("ClickCorner: snapped to (%d,%d) dist=%.1f", pt.X, pt.Y, bestDist)
	} else {
		a.logf("ClickCorner: custom placement at (%d,%d)", pt.X, pt.Y)
	}

	a.selectedCorners = append(a.selectedCorners, pt)
	count := len(a.selectedCorners)

	if count < 4 {
		result, err := a.drawCornerOverlay(dr)
		if err != nil {
			return nil, err
		}
		return &ClickCornerResult{
			Preview: result.Preview,
			Message: fmt.Sprintf("Corner %d of 4 selected", count),
			Count:   count,
			Done:    false,
		}, nil
	}

	// 4 corners selected → perform perspective warp
	_, width, height, warpErr := a.warpFromCorners(a.selectedCorners[:4])
	if warpErr != nil {
		return nil, warpErr
	}
	a.selectedCorners = nil

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}

	a.logf("ClickCorner: warp complete %dx%d", width, height)
	return &ClickCornerResult{
		Preview: preview,
		Message: fmt.Sprintf("Perspective corrected to %d×%d", width, height),
		Count:   4,
		Done:    true,
	}, nil
}

// ResetCorners clears any in-progress corner selection and redraws the
// detection overlay.
func (a *App) ResetCorners() (*ProcessResult, error) {
	a.logf("ResetCorners")
	a.selectedCorners = nil

	result, err := a.drawCornerOverlay(a.cornerDotRadius)
	if err != nil {
		return nil, err
	}
	result.Message = fmt.Sprintf("Reset — %d corners detected, click to select", len(a.detectedCorners))
	return result, nil
}

// SetCornerDotRadius updates the dot radius and redraws the corner overlay
// without re-running detection.
func (a *App) SetCornerDotRadius(req struct {
	DotRadius int `json:"dotRadius"`
}) (*ProcessResult, error) {
	dr := req.DotRadius
	if dr < 2 {
		dr = 2
	}
	a.cornerDotRadius = dr
	a.logf("SetCornerDotRadius: dr=%d, detectedCorners=%d, selectedCorners=%d", dr, len(a.detectedCorners), len(a.selectedCorners))

	result, err := a.drawCornerOverlay(dr)
	if err != nil {
		return nil, err
	}
	result.Message = fmt.Sprintf("Dot size: %d", dr)
	return result, nil
}

// ============================================================
// CROP & ROTATE
// ============================================================

// CropRequest specifies which edge to crop.
type CropRequest struct {
	Direction string `json:"direction"`
}

// Crop removes pixels from the specified edge of the warped image.
func (a *App) Crop(req CropRequest) (*ProcessResult, error) {
	a.logf("Crop: direction=%q", req.Direction)
	if a.warpedImage == nil {
		a.logf("Crop: no warped image")
		return nil, fmt.Errorf("no warped image")
	}
	a.saveUndo()

	b := a.warpedImage.Bounds()
	r := b

	switch req.Direction {
	case "top":
		if a.cropTop < b.Dy()-1 {
			a.cropTop += a.cropAmount
			r.Min.Y += a.cropAmount
		}
	case "bottom":
		if a.cropBottom < b.Dy()-1 {
			a.cropBottom += a.cropAmount
			r.Max.Y -= a.cropAmount
		}
	case "left":
		if a.cropLeft < b.Dx()-1 {
			a.cropLeft += a.cropAmount
			r.Min.X += a.cropAmount
		}
	case "right":
		if a.cropRight < b.Dx()-1 {
			a.cropRight += a.cropAmount
			r.Max.X -= a.cropAmount
		}
	}

	a.warpedImage = subImage(a.warpedImage, r)

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
}

// RotateRequest specifies the rotation/flip operation to apply.
type RotateRequest struct {
	FlipCode int `json:"flipCode"`
}

// Rotate applies a 90-degree rotation to the warped image.
func (a *App) Rotate(req RotateRequest) (*ProcessResult, error) {
	a.logf("Rotate: flipCode=%d", req.FlipCode)
	if a.warpedImage == nil {
		a.logf("Rotate: no warped image")
		return nil, fmt.Errorf("no warped image")
	}
	a.saveUndo()

	a.warpedImage = rotate90(a.warpedImage, req.FlipCode)

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
}

// Undo reverts the last operation on the image.
func (a *App) Undo() (*ProcessResult, error) {
	a.logf("Undo: stack depth=%d", len(a.undoStack))
	if len(a.undoStack) == 0 {
		a.logf("Undo: nothing to undo")
		return &ProcessResult{Message: "Nothing to undo"}, nil
	}
	a.warpedImage = a.undoStack[len(a.undoStack)-1]
	a.undoStack = a.undoStack[:len(a.undoStack)-1]

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
}

// ============================================================
// DISC MODE
// ============================================================

// DiscDrawRequest specifies the centre point and radius for a circular disc crop.
type DiscDrawRequest struct {
	CenterX int `json:"centerX"`
	CenterY int `json:"centerY"`
	Radius  int `json:"radius"`
}

// DrawDisc extracts and applies a circular mask with feathering.
func (a *App) DrawDisc(req DiscDrawRequest) (*ProcessResult, error) {
	a.logf("DrawDisc: center=(%d,%d) radius=%d", req.CenterX, req.CenterY, req.Radius)
	if a.originalImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	a.discCenter = image.Pt(req.CenterX, req.CenterY)
	a.discRadius = req.Radius
	a.rotationAngle = 0
	return a.redrawDisc()
}

// redrawDisc re-renders the disc crop using the current discCenter, discRadius,
// featherSize and bgColor. Called by DrawDisc, ShiftDisc, GetPixelColor, etc.
func (a *App) redrawDisc() (*ProcessResult, error) {
	if a.originalImage == nil || a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}
	margin := a.featherSize
	ob := a.originalImage.Bounds()
	x1 := clamp(a.discCenter.X-a.discRadius-margin, ob.Min.X, ob.Max.X)
	y1 := clamp(a.discCenter.Y-a.discRadius-margin, ob.Min.Y, ob.Max.Y)
	x2 := clamp(a.discCenter.X+a.discRadius+margin, ob.Min.X, ob.Max.X)
	y2 := clamp(a.discCenter.Y+a.discRadius+margin, ob.Min.Y, ob.Max.Y)

	cropped := subImage(a.originalImage, image.Rect(x1, y1, x2, y2))
	localCenter := image.Pt(a.discCenter.X-x1, a.discCenter.Y-y1)

	feathered := applyCircularMaskWithFeather(cropped, localCenter, a.discRadius, a.featherSize, a.bgColor)

	// Re-apply accumulated rotation so that ShiftDisc / SetFeatherSize / etc.
	// don't discard a rotation the user already applied.
	if a.rotationAngle != 0 {
		feathered = rotateArbitrary(feathered, a.rotationAngle, a.bgColor)
	}
	a.warpedImage = feathered

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
}

// DiscRotateRequest specifies the rotation angle for disc mode.
type DiscRotateRequest struct {
	Angle float64 `json:"angle"`
}

// RotateDisc rotates the disc image by the specified angle.
func (a *App) RotateDisc(req DiscRotateRequest) (*ProcessResult, error) {
	a.logf("RotateDisc: angle=%.2f (cumulative before: %.2f)", req.Angle, a.rotationAngle)
	if a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}
	a.rotationAngle += req.Angle
	return a.redrawDisc()
}

// PixelColorRequest holds image-space coordinates for colour sampling.
type PixelColorRequest struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// GetPixelColor samples the original image at (x,y), sets bgColor, and
// re-renders the disc crop so the feathered edge matches the sampled colour.
func (a *App) GetPixelColor(req PixelColorRequest) (*ProcessResult, error) {
	a.logf("GetPixelColor: x=%d y=%d", req.X, req.Y)
	if a.originalImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	b := a.originalImage.Bounds()
	px := clamp(req.X, b.Min.X, b.Max.X-1)
	py := clamp(req.Y, b.Min.Y, b.Max.Y-1)
	c := a.originalImage.NRGBAAt(px, py)
	a.bgColor = color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255}
	a.logf("GetPixelColor: sampled (%d,%d,%d) at (%d,%d)", c.R, c.G, c.B, px, py)

	if a.discRadius > 0 {
		return a.redrawDisc()
	}
	// No disc yet — just acknowledge
	return &ProcessResult{
		Message: fmt.Sprintf("Background colour set to (%d,%d,%d)", c.R, c.G, c.B),
	}, nil
}

// ShiftDiscRequest holds the pixel offset to shift the disc centre by.
type ShiftDiscRequest struct {
	DX int `json:"dx"`
	DY int `json:"dy"`
}

// ShiftDisc moves the disc center by (dx,dy) pixels and re-renders.
func (a *App) ShiftDisc(req ShiftDiscRequest) (*ProcessResult, error) {
	a.logf("ShiftDisc: dx=%d dy=%d", req.DX, req.DY)
	if a.discRadius <= 0 {
		return nil, fmt.Errorf("no disc defined")
	}
	a.discCenter.X += req.DX
	a.discCenter.Y += req.DY
	a.logf("ShiftDisc: new center=(%d,%d)", a.discCenter.X, a.discCenter.Y)
	return a.redrawDisc()
}

// FeatherSizeRequest holds the new feather radius.
type FeatherSizeRequest struct {
	Size int `json:"size"`
}

// SetFeatherSize updates the feather radius and re-renders the disc.
func (a *App) SetFeatherSize(req FeatherSizeRequest) (*ProcessResult, error) {
	a.logf("SetFeatherSize: %d", req.Size)
	if req.Size < 0 {
		req.Size = 0
	}
	a.featherSize = req.Size
	if a.discRadius > 0 {
		return a.redrawDisc()
	}
	return &ProcessResult{
		Message: fmt.Sprintf("Feather size set to %d", a.featherSize),
	}, nil
}

// SetBackgroundColor sets the background colour for disc mode.
func (a *App) SetBackgroundColor(r, g, b int) {
	a.logf("SetBackgroundColor: r=%d g=%d b=%d", r, g, b)
	a.bgColor = color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}
}

// ResetDisc clears the disc selection and restores the pre-disc image.
func (a *App) ResetDisc() (*ProcessResult, error) {
	a.logf("ResetDisc")
	a.discCenter = image.Point{}
	a.discRadius = 0
	a.rotationAngle = 0
	a.warpedImage = nil

	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview: preview,
		Message: "Disc selection reset — draw a new circle",
		Width:   b.Dx(),
		Height:  b.Dy(),
	}, nil
}

// ============================================================
// LINE MODE
// ============================================================

// LineAddRequest defines start and end coordinates for a user-drawn line
// used in 4-line perspective correction mode.
type LineAddRequest struct {
	X1 int `json:"x1"`
	Y1 int `json:"y1"`
	X2 int `json:"x2"`
	Y2 int `json:"y2"`
}

// AddLine records a line for line-based perspective correction.
func (a *App) AddLine(req LineAddRequest) (*ProcessResult, error) {
	a.logf("AddLine: (%d,%d)-(%d,%d)", req.X1, req.Y1, req.X2, req.Y2)
	a.lines = append(a.lines, []image.Point{
		{req.X1, req.Y1},
		{req.X2, req.Y2},
	})
	return &ProcessResult{
		Message: fmt.Sprintf("Lines: %d/4", len(a.lines)),
	}, nil
}

// ProcessLines calculates corner intersections and applies perspective correction.
func (a *App) ProcessLines() (*ProcessResult, error) {
	a.logf("ProcessLines: lines=%d", len(a.lines))
	if len(a.lines) != 4 {
		return nil, fmt.Errorf("need exactly 4 lines")
	}

	var intersections []image.Point
	for i := 0; i < 4; i++ {
		for j := i + 1; j < 4; j++ {
			if pt := lineIntersection(a.lines[i], a.lines[j]); pt != nil {
				intersections = append(intersections, *pt)
			}
		}
	}

	if len(intersections) < 4 {
		return nil, fmt.Errorf("could not find 4 corner intersections")
	}

	// Filter out intersections too far outside image bounds
	ob := a.originalImage.Bounds()
	marginX := float64(ob.Dx()) * 0.5
	marginY := float64(ob.Dy()) * 0.5
	var valid []image.Point
	for _, p := range intersections {
		px, py := float64(p.X), float64(p.Y)
		if px >= -marginX && px <= float64(ob.Max.X)+marginX &&
			py >= -marginY && py <= float64(ob.Max.Y)+marginY {
			valid = append(valid, p)
		}
	}

	if len(valid) < 4 {
		return nil, fmt.Errorf("not enough valid intersections (%d found)", len(valid))
	}

	// If more than 4, pick the 4 farthest from centroid
	if len(valid) > 4 {
		cx, cy := 0.0, 0.0
		for _, p := range valid {
			cx += float64(p.X)
			cy += float64(p.Y)
		}
		cx /= float64(len(valid))
		cy /= float64(len(valid))

		type scoredPt struct {
			pt   image.Point
			dist float64
		}
		scored := make([]scoredPt, len(valid))
		for i, p := range valid {
			dx := float64(p.X) - cx
			dy := float64(p.Y) - cy
			scored[i] = scoredPt{p, dx*dx + dy*dy}
		}
		// Sort descending by distance
		for i := 0; i < len(scored)-1; i++ {
			for j := i + 1; j < len(scored); j++ {
				if scored[j].dist > scored[i].dist {
					scored[i], scored[j] = scored[j], scored[i]
				}
			}
		}
		valid = []image.Point{scored[0].pt, scored[1].pt, scored[2].pt, scored[3].pt}
	}

	sorted := orderPoints(valid[:4])
	tl, tr, br, bl := sorted[0], sorted[1], sorted[2], sorted[3]

	// Compute output dimensions from the quadrilateral edge lengths
	widthTop := dist(tl, tr)
	widthBot := dist(bl, br)
	heightLeft := dist(tl, bl)
	heightRight := dist(tr, br)
	outW := int(math.Max(widthTop, widthBot))
	outH := int(math.Max(heightLeft, heightRight))
	if outW < 10 {
		outW = 10
	}
	if outH < 10 {
		outH = 10
	}
	a.logf("ProcessLines: output %dx%d", outW, outH)

	dst := [4]image.Point{
		{0, 0}, {outW, 0}, {outW, outH}, {0, outH},
	}
	src := [4]image.Point{tl, tr, br, bl}

	warped := perspectiveTransform(a.originalImage, src, dst, outW, outH)
	a.warpedImage = warped
	a.lines = nil

	preview, err := imageToBase64(a.warpedImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{Preview: preview}, nil
}

// ClearLines removes all drawn lines and restores the pre-line image.
func (a *App) ClearLines() (*ProcessResult, error) {
	a.logf("ClearLines")
	a.lines = nil
	a.warpedImage = nil

	if a.currentImage == nil {
		return &ProcessResult{Message: "Lines cleared"}, nil
	}
	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview: preview,
		Message: "Lines cleared — draw 4 new lines",
		Width:   b.Dx(),
		Height:  b.Dy(),
	}, nil
}

// ============================================================
// UNDO
// ============================================================

func (a *App) saveUndo() {
	if len(a.undoStack) >= a.undoLimit {
		a.undoStack = a.undoStack[1:]
	}
	a.undoStack = append(a.undoStack, cloneImage(a.warpedImage))
}
