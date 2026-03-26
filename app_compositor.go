package main

// app_compositor.go — Wails-facing compositor methods.
//
// Keeps the stitching result in app state so the user can stitch once and
// save without re-running the algorithm.

import (
	"bufio"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// ---- Types -------------------------------------------------------------------

// CompositorStitchRequest carries the ordered list of source file paths and
// the orientation that describes how consecutive images are arranged.
// Orientation is one of: "ltr" (left-to-right, default), "rtl" (right-to-left),
// "ttb" (top-to-bottom), "btt" (bottom-to-top).
// For reversed orientations ("rtl", "btt") the image array is reversed before
// stitching so that image 0 is always the anchor in the reference frame.
type CompositorStitchRequest struct {
	ImagePaths  []string `json:"imagePaths"`
	Orientation string   `json:"orientation"`
}

// CompositorResult is returned by CompositorStitch.
type CompositorResult struct {
	Preview string `json:"preview"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Message string `json:"message"`
}

// CompositorSaveRequest carries the output path for saving the stitched image.
type CompositorSaveRequest struct {
	OutputPath string `json:"outputPath"`
}

// ---- Methods -----------------------------------------------------------------

// CompositorOpenFilesDialog opens a multi-file picker restricted to image
// types and returns the selected paths (empty slice if cancelled).
func (a *App) CompositorOpenFilesDialog() ([]string, error) {
	a.logf("CompositorOpenFilesDialog: showing dialog")
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Images to Composite",
		Filters: []runtime.FileFilter{
			{DisplayName: "Image Files (*.png;*.jpg;*.jpeg;*.bmp;*.tiff;*.tif)", Pattern: "*.png;*.jpg;*.jpeg;*.bmp;*.tiff;*.tif"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	a.logf("CompositorOpenFilesDialog: %d paths, err=%v", len(paths), err)
	return paths, err
}

// CompositorStitch decodes all supplied images, runs the stitching pipeline,
// and returns a preview of the assembled result.  The full-resolution result
// is cached in app state for a subsequent CompositorSave call.
func (a *App) CompositorStitch(req CompositorStitchRequest) (*CompositorResult, error) {
	if len(req.ImagePaths) < 2 {
		return nil, fmt.Errorf("provide at least 2 image paths (got %d)", len(req.ImagePaths))
	}

	a.logf("CompositorStitch: %d images, orientation=%q", len(req.ImagePaths), req.Orientation)

	// For reversed orientations the user listed images in the opposite direction
	// to the reference frame.  Reverse the path list so image[0] is always the
	// "first" segment (anchor) and each subsequent image extends in the forward
	// direction.
	paths := req.ImagePaths
	if req.Orientation == "rtl" || req.Orientation == "btt" {
		rev := make([]string, len(paths))
		for i, p := range paths {
			rev[len(paths)-1-i] = p
		}
		paths = rev
	}

	// Decode all images.
	imgs := make([]*image.NRGBA, len(paths))
	for i, path := range paths {
		raw, err := a.decodeImageFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to decode image %d (%q): %w", i, filepath.Base(path), err)
		}
		imgs[i] = toNRGBA(raw)
		a.logf("CompositorStitch: image %d decoded: %v", i, imgs[i].Bounds())
	}

	// Run the stitching algorithm.
	result, err := stitchImages(imgs)
	if err != nil {
		return nil, err
	}

	// Cache the result.
	a.compositorMu.Lock()
	a.compositorResult = result
	a.compositorMu.Unlock()

	preview, err := imageToBase64(result)
	if err != nil {
		return nil, fmt.Errorf("preview encoding failed: %w", err)
	}

	b := result.Bounds()
	msg := fmt.Sprintf("Stitched %d images → %d×%d", len(imgs), b.Dx(), b.Dy())
	a.logf("CompositorStitch: %s", msg)
	return &CompositorResult{
		Preview: preview,
		Width:   b.Dx(),
		Height:  b.Dy(),
		Message: msg,
	}, nil
}

// CompositorLoadResultRequest carries optional post-processing for the
// compositor output before it enters the editing pipeline.
type CompositorLoadResultRequest struct {
	// RotationSteps is the number of 90-degree clockwise rotations to apply
	// to the stitched image before loading it.  0 = no rotation, 1 = 90° CW,
	// 2 = 180°, 3 = 270° CW.
	RotationSteps int `json:"rotationSteps"`
}

// CompositorLoadResult promotes the cached stitched image into the main editing
// pipeline exactly as if it had been opened from disk with LoadImage.  After
// this call the compositor result becomes the active image and the user can
// apply any of the normal crop/adjust modes to it.
func (a *App) CompositorLoadResult(req CompositorLoadResultRequest) (*ImageInfo, error) {
	a.compositorMu.Lock()
	result := a.compositorResult
	a.compositorMu.Unlock()

	if result == nil {
		return nil, fmt.Errorf("no stitched result available — run Stitch first")
	}

	if !a.loadMu.TryLock() {
		return nil, fmt.Errorf("another load is already in progress")
	}
	defer a.loadMu.Unlock()
	a.cancelTouchup()

	img := cloneImage(result)
	steps := ((req.RotationSteps % 4) + 4) % 4
	for i := 0; i < steps; i++ {
		img = rotate90(img, 1) // 1 = CW
	}

	a.originalImage = img
	a.currentImage = cloneImage(img)
	a.warpedImage = nil
	a.levelsBaseImage = nil
	a.imageLoaded = true
	a.loadedFilePath = ""
	a.selectedCorners = nil
	a.detectedCorners = nil
	a.lines = nil
	a.undoStack = nil
	a.discCenter = image.Point{}
	a.discRadius = 0
	a.rotationAngle = 0
	a.discBaseImage = nil
	a.discWorkingCrop = nil
	a.discWorkingCropRect = image.Rectangle{}
	a.postDiscBlack = 0
	a.postDiscWhite = 255

	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		return nil, fmt.Errorf("preview encoding failed: %w", err)
	}

	b := img.Bounds()
	runtime.WindowSetTitle(a.ctx, AppBaseTitle()+" — [Compositor Result]")
	a.logf("CompositorLoadResult: loaded %dx%d stitched image into editing pipeline (rotationSteps=%d)", b.Dx(), b.Dy(), steps)
	return &ImageInfo{
		Width:                 b.Dx(),
		Height:                b.Dy(),
		Preview:               preview,
		SuggestedCornerParams: suggestCornerParams(b.Dx(), b.Dy()),
	}, nil
}

// CompositorSave writes the cached stitched image to disk.
// The output format is inferred from the file extension.
func (a *App) CompositorSave(req CompositorSaveRequest) (string, error) {
	a.compositorMu.Lock()
	result := a.compositorResult
	a.compositorMu.Unlock()

	if result == nil {
		return "", fmt.Errorf("no stitched result available — run Stitch first")
	}
	if req.OutputPath == "" {
		return "", fmt.Errorf("output path is empty")
	}

	f, err := os.Create(req.OutputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create %s: %w", req.OutputPath, err)
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(req.OutputPath))
	switch ext {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(f, result, &jpeg.Options{Quality: 95})
	case ".bmp":
		err = bmp.Encode(f, result)
	case ".tiff", ".tif":
		err = tiff.Encode(f, result, nil)
	default: // .png and everything else
		bw := bufio.NewWriterSize(f, 1<<20)
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		if err = enc.Encode(bw, result); err == nil {
			err = bw.Flush()
		}
	}
	if err != nil {
		return "", fmt.Errorf("encode error: %w", err)
	}

	msg := fmt.Sprintf("Saved to %s", req.OutputPath)
	a.logf("CompositorSave: %s", msg)
	return msg, nil
}

// CompositorOpenSaveDialog shows a save-file dialog pre-filtered to image
// formats and returns the chosen path (empty string if cancelled).
func (a *App) CompositorOpenSaveDialog() (string, error) {
	a.logf("CompositorOpenSaveDialog: showing dialog")
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save Stitched Image",
		DefaultFilename: "stitched.png",
		Filters: []runtime.FileFilter{
			{DisplayName: "PNG (*.png)", Pattern: "*.png"},
			{DisplayName: "JPEG (*.jpg)", Pattern: "*.jpg"},
			{DisplayName: "TIFF (*.tiff)", Pattern: "*.tiff"},
			{DisplayName: "BMP (*.bmp)", Pattern: "*.bmp"},
		},
	})
	a.logf("CompositorOpenSaveDialog: path=%q err=%v", path, err)
	return path, err
}
