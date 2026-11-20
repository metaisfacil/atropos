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

	"atropos/internal/compositor"
	"atropos/internal/imageops"
	"atropos/internal/raster"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// ---- Methods -----------------------------------------------------------------

// CompositorOpenFilesDialog opens a multi-file picker restricted to image
// types and returns the selected paths (empty slice if cancelled).
func (a *App) CompositorOpenFilesDialog() ([]string, error) {
	a.logf("CompositorOpenFilesDialog: showing dialog")
	paths, err := a.openMultipleFilesDialog("Select Images to Composite", "", []FileFilter{
		{DisplayName: "Image Files (*.png;*.jpg;*.jpeg;*.bmp;*.tiff;*.tif)", Patterns: []string{"*.png", "*.jpg", "*.jpeg", "*.bmp", "*.tiff", "*.tif", "*.gif", "*.webp"}},
		{DisplayName: "All Files (*.*)", Patterns: []string{"*.*"}},
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

	rotSteps := make([]int, len(req.ImagePaths))
	for i := range rotSteps {
		if i < len(req.RotationSteps) {
			rotSteps[i] = ((req.RotationSteps[i] % 4) + 4) % 4
		}
	}

	// For reversed orientations the user listed images in the opposite direction
	// to the reference frame.  Reverse the path and rotation lists together so
	// image[0] is always the "first" segment (anchor) and each subsequent image
	// keeps its matching per-image rotation.
	paths := req.ImagePaths
	if req.Orientation == "rtl" || req.Orientation == "btt" {
		rev := make([]string, len(paths))
		revRot := make([]int, len(paths))
		for i, p := range paths {
			rev[len(paths)-1-i] = p
			revRot[len(paths)-1-i] = rotSteps[i]
		}
		paths = rev
		rotSteps = revRot
	}

	// Decode all images in parallel — decoding is I/O + CPU bound and dominates
	// total stitch time when images are large, so concurrent decoding is a
	// significant win even for just 2 files.
	type decodeResult struct {
		idx int
		img *image.NRGBA
		err error
	}
	ch := make(chan decodeResult, len(paths))
	for i, path := range paths {
		rot := rotSteps[i]
		go func(i int, path string, rot int) {
			raw, err := a.decodeImageFile(path)
			if err != nil {
				ch <- decodeResult{idx: i, err: fmt.Errorf("failed to decode image %d (%q): %w", i, filepath.Base(path), err)}
				return
			}
			img := imageops.RotateQuarterTurns(raster.ToNRGBA(raw), rot)
			a.logf("CompositorStitch: image %d decoded: %v (rot=%d)", i, img.Bounds(), rot)
			ch <- decodeResult{idx: i, img: img}
		}(i, path, rot)
	}
	imgs := make([]*image.NRGBA, len(paths))
	for range paths {
		r := <-ch
		if r.err != nil {
			return nil, r.err
		}
		imgs[r.idx] = r.img
	}

	// Run the stitching algorithm.
	result, err := compositor.Stitch(imgs)
	if err != nil {
		return nil, err
	}

	// Cache the result.
	a.compositorMu.Lock()
	a.compositorResult = result
	a.compositorOverlay = nil
	a.compositorMu.Unlock()

	preview, err := a.imagePreviewURL(result)
	if err != nil {
		return nil, fmt.Errorf("preview registration failed: %w", err)
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

// CompositorAlignOverlay aligns repeated scans to the first scan's canvas.
// The listed order is the stacking order: the final scan is on top.
func (a *App) CompositorAlignOverlay(req CompositorStitchRequest) (*CompositorResult, error) {
	if len(req.ImagePaths) < 2 {
		return nil, fmt.Errorf("provide at least 2 image paths (got %d)", len(req.ImagePaths))
	}
	imgs := make([]*image.NRGBA, len(req.ImagePaths))
	type decoded struct {
		index int
		image *image.NRGBA
		err   error
	}
	results := make(chan decoded, len(imgs))
	for i, path := range req.ImagePaths {
		rot := 0
		if i < len(req.RotationSteps) {
			rot = ((req.RotationSteps[i] % 4) + 4) % 4
		}
		go func(i int, path string, rot int) {
			raw, err := a.decodeImageFile(path)
			if err != nil {
				results <- decoded{index: i, err: fmt.Errorf("failed to decode image %d (%q): %w", i+1, filepath.Base(path), err)}
				return
			}
			img := imageops.RotateQuarterTurns(raster.ToNRGBA(raw), rot)
			results <- decoded{index: i, image: img}
		}(i, path, rot)
	}
	var decodeErr error
	for range imgs {
		r := <-results
		if r.err != nil {
			decodeErr = r.err
		} else {
			imgs[r.index] = r.image
		}
	}
	if decodeErr != nil {
		return nil, decodeErr
	}
	session, err := compositor.AlignOverlay(imgs)
	if err != nil {
		return nil, err
	}
	a.compositorMu.Lock()
	a.compositorOverlay = session
	a.compositorResult = nil
	response, err := a.compositorOverlayPreviewLocked()
	a.compositorMu.Unlock()
	return response, err
}

func (a *App) CompositorAddMask(req CompositorMaskRequest) (*CompositorResult, error) {
	a.compositorMu.Lock()
	defer a.compositorMu.Unlock()
	if a.compositorOverlay == nil {
		return nil, fmt.Errorf("align repeated scans first")
	}
	if err := a.compositorOverlay.AddStroke(req.Stroke); err != nil {
		return nil, err
	}
	a.compositorResult = nil
	return a.compositorOverlayPreviewLocked()
}

func (a *App) CompositorUndoMask() (*CompositorResult, error) {
	a.compositorMu.Lock()
	defer a.compositorMu.Unlock()
	if a.compositorOverlay == nil {
		return nil, fmt.Errorf("align repeated scans first")
	}
	a.compositorOverlay.UndoStroke()
	a.compositorResult = nil
	return a.compositorOverlayPreviewLocked()
}

func (a *App) CompositorClearMasks() (*CompositorResult, error) {
	a.compositorMu.Lock()
	defer a.compositorMu.Unlock()
	if a.compositorOverlay == nil {
		return nil, fmt.Errorf("align repeated scans first")
	}
	a.compositorOverlay.ClearStrokes()
	a.compositorResult = nil
	return a.compositorOverlayPreviewLocked()
}

// Caller holds compositorMu so mask changes and their preview stay in order.
func (a *App) compositorOverlayPreviewLocked() (*CompositorResult, error) {
	previewImage := a.compositorOverlay.Render(1400)
	preview, err := a.imagePreviewURL(previewImage)
	if err != nil {
		return nil, fmt.Errorf("preview registration failed: %w", err)
	}
	w, h := a.compositorOverlay.Size()
	return &CompositorResult{Preview: preview, Width: w, Height: h, Message: fmt.Sprintf("Aligned %d scans on the first scan (%d×%d)", a.compositorOverlay.LayerCount(), w, h)}, nil
}

// CompositorRefreshPreview republishes the cached compositor output after an
// image load has reset the shared preview store. Alignment and masks are kept.
func (a *App) CompositorRefreshPreview() (*CompositorResult, error) {
	a.compositorMu.Lock()
	defer a.compositorMu.Unlock()
	if a.compositorOverlay != nil {
		return a.compositorOverlayPreviewLocked()
	}
	if a.compositorResult == nil {
		return nil, fmt.Errorf("no compositor result available")
	}
	preview, err := a.imagePreviewURL(a.compositorResult)
	if err != nil {
		return nil, fmt.Errorf("preview registration failed: %w", err)
	}
	b := a.compositorResult.Bounds()
	return &CompositorResult{
		Preview: preview,
		Width:   b.Dx(),
		Height:  b.Dy(),
		Message: fmt.Sprintf("Stitched result (%d×%d)", b.Dx(), b.Dy()),
	}, nil
}

// CompositorLoadResult promotes the cached stitched image into the main editing
// pipeline exactly as if it had been opened from disk with LoadImage.  After
// this call the compositor result becomes the active image and the user can
// apply any of the normal crop/adjust modes to it.
func (a *App) CompositorLoadResult(req CompositorLoadResultRequest) (*ImageInfo, error) {
	a.compositorMu.Lock()
	result := a.compositorResult
	if a.compositorOverlay != nil && result == nil {
		result = a.compositorOverlay.Render(0)
		a.compositorResult = result
	}
	a.compositorMu.Unlock()

	if result == nil {
		return nil, fmt.Errorf("no stitched result available — run Stitch first")
	}

	if !a.loadMu.TryLock() {
		return nil, fmt.Errorf("another load is already in progress")
	}
	defer a.loadMu.Unlock()
	a.cancelTouchup()
	a.CancelCornerDetect()

	img := raster.CloneNRGBA(result)
	steps := ((req.RotationSteps % 4) + 4) % 4
	for i := 0; i < steps; i++ {
		img = imageops.Rotate90(img, 1) // 1 = CW
	}

	a.originalImage = img
	a.currentImage = raster.CloneNRGBA(img)
	a.imageLoaded = true
	a.loadedFilePath = ""
	a.resetPipelineState()

	preview, err := a.imagePreviewURL(a.currentImage)
	if err != nil {
		return nil, fmt.Errorf("preview registration failed: %w", err)
	}

	b := img.Bounds()
	a.setWindowTitle(AppBaseTitle() + " — [Compositor Result]")
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
	if a.compositorOverlay != nil && result == nil {
		result = a.compositorOverlay.Render(0)
		a.compositorResult = result
	}
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
	path, err := a.saveFileDialog("Save Stitched Image", "", "stitched.png", []FileFilter{
		{DisplayName: "PNG (*.png)", Patterns: []string{"*.png"}},
		{DisplayName: "JPEG (*.jpg)", Patterns: []string{"*.jpg", "*.jpeg"}},
		{DisplayName: "TIFF (*.tiff)", Patterns: []string{"*.tiff", "*.tif"}},
		{DisplayName: "BMP (*.bmp)", Patterns: []string{"*.bmp"}},
	})
	a.logf("CompositorOpenSaveDialog: path=%q err=%v", path, err)
	return path, err
}
