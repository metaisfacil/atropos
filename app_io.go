package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

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
	a.cancelTouchup()

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
	a.levelsBaseImage = nil
	a.imageLoaded = true
	a.loadedFilePath = req.FilePath
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
	runtime.WindowSetTitle(a.ctx, AppBaseTitle()+" — "+name)
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
	// Try "magick" first (ImageMagick 7+), then fall back to "convert" (IM 6 / Linux distros)
	magickPath, lookErr := exec.LookPath("magick")
	if lookErr != nil {
		magickPath, lookErr = exec.LookPath("convert")
		if lookErr != nil {
			return nil, fmt.Errorf("ImageMagick not found")
		}
	}

	var cmd *exec.Cmd
	if strings.HasSuffix(magickPath, "magick") || strings.HasSuffix(magickPath, "magick.exe") {
		cmd = exec.Command(magickPath, "convert", path, outFmt+":-")
	} else {
		// "convert" is the binary itself — no sub-command needed
		cmd = exec.Command(magickPath, path, outFmt+":-")
	}
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
		const msg = "SaveImage: no image to save"
		a.logf(msg)
		return nil, fmt.Errorf(msg)
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
		bw := bufio.NewWriterSize(f, 1<<20) // 1 MiB write buffer
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		if err = enc.Encode(bw, a.warpedImage); err == nil {
			err = bw.Flush()
		}
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

// RecropImage promotes the current warpedImage to be the new source image,
// resetting all processing state so the user can apply a second crop mode on
// the result of the first one without having to save and reload the file.
func (a *App) RecropImage() (*ImageInfo, error) {
	a.logf("RecropImage: called")
	a.cancelTouchup()
	if a.warpedImage == nil {
		return nil, fmt.Errorf("no processed image to re-crop from")
	}

	src := a.warpedImage
	a.originalImage = src
	a.currentImage = cloneImage(src)
	a.warpedImage = nil
	a.levelsBaseImage = nil
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
		return nil, err
	}

	b := a.currentImage.Bounds()
	a.logf("RecropImage: new source is %dx%d", b.Dx(), b.Dy())
	return &ImageInfo{
		Width:   b.Dx(),
		Height:  b.Dy(),
		Preview: preview,
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
