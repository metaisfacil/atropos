package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// resetPipelineState clears all derived/processing state after a new image is
// loaded. It does NOT touch originalImage, currentImage, imageLoaded, or
// loadedFilePath — the caller is responsible for those.
func (a *App) resetPipelineState() {
	a.warpedImage = nil
	a.levelsBaseImage = nil
	a.selectedCorners = nil
	a.detectedCorners = nil
	a.lines = nil
	a.undoStack = nil
	a.resetDiscFields()
}

// LoadImageRequest contains the file path for loading an image.
type LoadImageRequest struct {
	FilePath string `json:"filePath"`
}

// LoadImageBytesRequest contains image bytes for loading an image from clipboard/drag drop.
type LoadImageBytesRequest struct {
	Data []byte `json:"data"`
	Name string `json:"name,omitempty"`
}

// SuggestedCornerParams holds auto-computed corner detection defaults derived
// from objective image properties (dimensions, luminance statistics).
type SuggestedCornerParams struct {
	MinDistance int `json:"minDistance"`
	MaxCorners  int `json:"maxCorners"`
}

// ImageInfo contains image metadata and preview data.
type ImageInfo struct {
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	Preview string  `json:"preview"`
	Format  string  `json:"format"` // e.g. "JPEG", "PNG", "TIFF", "BMP"
	DPIX    float64 `json:"dpiX"`   // horizontal DPI; 0 if unknown
	DPIY    float64 `json:"dpiY"`   // vertical DPI; 0 if unknown

	SuggestedCornerParams SuggestedCornerParams `json:"suggestedCornerParams"`
}

// LoadImage loads an image from disk and returns its metadata.
func (a *App) LoadImage(req LoadImageRequest) (*ImageInfo, error) {
	if !a.loadMu.TryLock() {
		const msg = "LoadImage: rejected, another load is already in progress"
		a.logf(msg)
		return nil, fmt.Errorf(msg)
	}
	defer a.loadMu.Unlock()
	a.cancelTouchup()

	a.logf("LoadImage: filePath=%q", req.FilePath)

	t0 := time.Now()
	src, err := a.decodeImageFile(req.FilePath)
	if err != nil {
		msg := fmt.Sprintf("LoadImage: decode error: %v", err)
		a.logf(msg)
		return nil, fmt.Errorf(msg)
	}
	a.logf("LoadImage: decode took %v", time.Since(t0))

	t1 := time.Now()
	nrgba := toNRGBA(src)
	src = nil // allow GC to reclaim decoded image
	a.logf("LoadImage: toNRGBA took %v", time.Since(t1))

	t2 := time.Now()
	a.originalImage = nrgba            // reuse — toNRGBA already made a fresh copy
	a.currentImage = cloneImage(nrgba) // one clone instead of two

	// Apply automatic border trim immediately after load, but keep originalImage
	trimRect := trimBordersRect(a.currentImage)
	if !trimRect.Eq(a.currentImage.Bounds()) {
		a.currentImage = subImage(a.currentImage, trimRect)
		a.logf("LoadImage: auto-trimmed borders to %dx%d", a.currentImage.Bounds().Dx(), a.currentImage.Bounds().Dy())
	}

	a.imageLoaded = true
	a.loadedFilePath = req.FilePath
	a.resetPipelineState()
	a.logf("LoadImage: clone took %v", time.Since(t2))

	t3 := time.Now()
	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		msg := fmt.Sprintf("LoadImage: base64 error: %v", err)
		a.logf(msg)
		return nil, fmt.Errorf(msg)
	}
	a.logf("LoadImage: preview took %v", time.Since(t3))

	b := nrgba.Bounds()
	a.logf("LoadImage: total %v, returning %dx%d, preview len=%d", time.Since(t0), b.Dx(), b.Dy(), len(preview))

	// Update window title with the filename
	name := filepath.Base(req.FilePath)
	runtime.WindowSetTitle(a.ctx, AppBaseTitle()+" — "+name)

	format, dpiX, dpiY := extractFileMeta(req.FilePath)
	return &ImageInfo{
		Width:                 b.Dx(),
		Height:                b.Dy(),
		Preview:               preview,
		Format:                format,
		DPIX:                  dpiX,
		DPIY:                  dpiY,
		SuggestedCornerParams: suggestCornerParams(b.Dx(), b.Dy()),
	}, nil
}

// ResetImage restores the app image state back to the original loaded image
// and clears all intermediate crop/warp/adjustment state.
func (a *App) ResetImage() (*ProcessResult, error) {
	a.logf("ResetImage")
	a.cancelTouchup()
	if a.originalImage == nil {
		return nil, fmt.Errorf("ResetImage: no image loaded")
	}

	// Restore the pre-load image and clear derived state.
	a.currentImage = cloneImage(a.originalImage)
	a.warpedImage = nil
	a.levelsBaseImage = nil
	a.undoStack = nil
	a.resetPipelineState()
	a.imageLoaded = true

	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview: preview,
		Message: "Reset to original image",
		Width:   b.Dx(),
		Height:  b.Dy(),
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

func (a *App) decodeImageData(data []byte) (image.Image, string, error) {
	src, format, err := image.Decode(bytes.NewReader(data))
	if err == nil {
		return src, format, nil
	}

	// Try ImageMagick fallback when the standard decoder fails.
	tmp, tmpErr := os.CreateTemp("", "atropos-image-*")
	if tmpErr != nil {
		return nil, "", fmt.Errorf("standard decode failed: %w; failed to create temp file: %v", err, tmpErr)
	}
	defer os.Remove(tmp.Name())

	if _, tmpErr = tmp.Write(data); tmpErr != nil {
		tmp.Close()
		return nil, "", fmt.Errorf("failed to write temp image file for decode: %v", tmpErr)
	}
	tmp.Close()

	img, magickErr := a.decodeViaMagick(tmp.Name(), "bmp3")
	if magickErr != nil {
		return nil, "", fmt.Errorf("standard decode failed: %w; ImageMagick also failed: %v", err, magickErr)
	}
	return img, "BMP", nil
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
		msg := fmt.Sprintf("decodeViaMagick: magick failed: %v stderr=%s", runErr, stderr.String())
		a.logf(msg)
		return nil, fmt.Errorf(msg)
	}

	a.logf("decodeViaMagick: magick produced %d bytes of %s", stdout.Len(), outFmt)
	img, decErr := bmp.Decode(bytes.NewReader(stdout.Bytes()))
	if decErr != nil {
		msg := fmt.Sprintf("decodeViaMagick: bmp decode failed: %v", decErr)
		a.logf(msg)
		return nil, fmt.Errorf(msg)
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
	img := a.workingImage()
	if img == nil {
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
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: 95})
	case ".bmp":
		err = bmp.Encode(f, img)
	case ".tiff", ".tif":
		err = tiff.Encode(f, img, nil)
	default:
		bw := bufio.NewWriterSize(f, 1<<20) // 1 MiB write buffer
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		if err = enc.Encode(bw, img); err == nil {
			err = bw.Flush()
		}
	}
	if err != nil {
		msg := fmt.Sprintf("SaveImage: encode error: %v", err)
		a.logf(msg)
		return nil, fmt.Errorf(msg)
	}

	a.logf("SaveImage: saved successfully to %s", req.OutputPath)
	// If a CLI-supplied post-save command was provided, run it and exit.
	if a.postSaveCmd != "" {
		a.logf("SaveImage: running CLI post-save command: %q", a.postSaveCmd)
		if err := a.RunPostSaveCommand(a.postSaveCmd, req.OutputPath); err != nil {
			a.logf("SaveImage: RunPostSaveCommand failed: %v", err)
			// Still return success to the frontend; do not treat post-save failure as save failure.
			return &ProcessResult{Message: fmt.Sprintf("Saved to %s (post-save failed)", req.OutputPath)}, nil
		}
		// If the CLI requested exit after launching the command, quit now.
		if a.postSaveExit {
			a.logf("SaveImage: started CLI post-save command, quitting as requested")
			runtime.Quit(a.ctx)
		}
		a.logf("SaveImage: started CLI post-save command")
	}

	return &ProcessResult{
		Message: fmt.Sprintf("Saved to %s", req.OutputPath),
	}, nil
}

// LoadImageBytes loads an image directly from raw bytes (clipboard or browser drop).
func (a *App) LoadImageBytes(req LoadImageBytesRequest) (*ImageInfo, error) {
	if !a.loadMu.TryLock() {
		const msg = "LoadImageBytes: rejected, another load is already in progress"
		a.logf(msg)
		return nil, fmt.Errorf(msg)
	}
	defer a.loadMu.Unlock()
	a.cancelTouchup()

	a.logf("LoadImageBytes: name=%q size=%d", req.Name, len(req.Data))

	t0 := time.Now()
	src, format, err := a.decodeImageData(req.Data)
	if err != nil {
		msg := fmt.Sprintf("LoadImageBytes: decode error: %v", err)
		a.logf(msg)
		return nil, fmt.Errorf(msg)
	}
	a.logf("LoadImageBytes: decode took %v", time.Since(t0))

	t1 := time.Now()
	nrgba := toNRGBA(src)
	src = nil
	a.logf("LoadImageBytes: toNRGBA took %v", time.Since(t1))

	t2 := time.Now()
	a.originalImage = nrgba
	a.currentImage = cloneImage(nrgba)
	a.imageLoaded = true
	a.loadedFilePath = ""
	a.resetPipelineState()
	a.logf("LoadImageBytes: clone took %v", time.Since(t2))

	t3 := time.Now()
	preview, err := imageToBase64(a.currentImage)
	if err != nil {
		msg := fmt.Sprintf("LoadImageBytes: base64 error: %v", err)
		a.logf(msg)
		return nil, fmt.Errorf(msg)
	}
	a.logf("LoadImageBytes: preview took %v", time.Since(t3))

	b := nrgba.Bounds()
	a.logf("LoadImageBytes: total %v, returning %dx%d, preview len=%d", time.Since(t0), b.Dx(), b.Dy(), len(preview))

	runtime.WindowSetTitle(a.ctx, AppBaseTitle()+" — "+func() string {
		if req.Name != "" {
			return req.Name
		}
		return "Clipboard"
	}())

	return &ImageInfo{
		Width:                 b.Dx(),
		Height:                b.Dy(),
		Preview:               preview,
		Format:                strings.ToUpper(format),
		DPIX:                  0,
		DPIY:                  0,
		SuggestedCornerParams: suggestCornerParams(b.Dx(), b.Dy()),
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
	a.resetPipelineState()

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
		dir := filepath.Dir(a.loadedFilePath)
		if _, statErr := os.Stat(dir); statErr == nil {
			defaultDir = dir
		}
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

// extractFileMeta returns the format name and DPI (X, Y) for the given file.
// DPI values are 0 if unavailable or unknown.
func extractFileMeta(path string) (format string, dpiX, dpiY float64) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		format = "JPEG"
		dpiX, dpiY = extractJPEGDPI(path)
	case ".png":
		format = "PNG"
		dpiX, dpiY = extractPNGDPI(path)
	case ".tif", ".tiff":
		format = "TIFF"
	case ".bmp":
		format = "BMP"
		dpiX, dpiY = extractBMPDPI(path)
	case ".gif":
		format = "GIF"
	case ".webp":
		format = "WebP"
	default:
		format = strings.ToUpper(strings.TrimPrefix(ext, "."))
	}
	return
}

// extractJPEGDPI reads DPI from a JFIF APP0 header.
func extractJPEGDPI(path string) (dpiX, dpiY float64) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	// Need 18 bytes: SOI(2) + APP0 marker(2) + length(2) + "JFIF\0"(5) + ver(2) + units(1) + density(4)
	buf := make([]byte, 18)
	if _, err := io.ReadFull(f, buf); err != nil {
		return 0, 0
	}
	if buf[0] != 0xFF || buf[1] != 0xD8 || buf[2] != 0xFF || buf[3] != 0xE0 {
		return 0, 0
	}
	if string(buf[6:11]) != "JFIF\x00" {
		return 0, 0
	}
	units := buf[13]
	xDens := binary.BigEndian.Uint16(buf[14:16])
	yDens := binary.BigEndian.Uint16(buf[16:18])
	if xDens == 0 || yDens == 0 {
		return 0, 0
	}
	switch units {
	case 1: // dots per inch
		return float64(xDens), float64(yDens)
	case 2: // dots per cm → DPI
		return math.Round(float64(xDens)*2.54*10) / 10, math.Round(float64(yDens)*2.54*10) / 10
	}
	return 0, 0
}

// extractPNGDPI reads DPI from a PNG pHYs chunk (searches first 20 chunks).
func extractPNGDPI(path string) (dpiX, dpiY float64) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	sig := make([]byte, 8)
	if _, err := io.ReadFull(f, sig); err != nil {
		return 0, 0
	}
	if string(sig) != "\x89PNG\r\n\x1a\n" {
		return 0, 0
	}

	for i := 0; i < 20; i++ {
		var hdr [8]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			return 0, 0
		}
		length := binary.BigEndian.Uint32(hdr[0:4])
		chunkType := string(hdr[4:8])

		if chunkType == "pHYs" && length >= 9 {
			data := make([]byte, 9)
			if _, err := io.ReadFull(f, data); err != nil {
				return 0, 0
			}
			xPPU := binary.BigEndian.Uint32(data[0:4])
			yPPU := binary.BigEndian.Uint32(data[4:8])
			if data[8] == 1 && xPPU > 0 && yPPU > 0 {
				// pixels per metre → DPI
				return math.Round(float64(xPPU)*0.0254*10) / 10, math.Round(float64(yPPU)*0.0254*10) / 10
			}
			return 0, 0
		}
		if chunkType == "IDAT" || chunkType == "IEND" {
			return 0, 0
		}
		// Skip chunk data + CRC (4 bytes)
		if _, err := f.Seek(int64(length)+4, io.SeekCurrent); err != nil {
			return 0, 0
		}
	}
	return 0, 0
}

// extractBMPDPI reads DPI from a BMP BITMAPINFOHEADER (XPelsPerMeter / YPelsPerMeter).
func extractBMPDPI(path string) (dpiX, dpiY float64) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	// File header (14 bytes) + first 32 bytes of DIB header covers through YPelsPerMeter
	buf := make([]byte, 46)
	if _, err := io.ReadFull(f, buf); err != nil {
		return 0, 0
	}
	if buf[0] != 'B' || buf[1] != 'M' {
		return 0, 0
	}
	dibSize := binary.LittleEndian.Uint32(buf[14:18])
	if dibSize < 40 {
		return 0, 0
	}
	xPPM := int32(binary.LittleEndian.Uint32(buf[38:42]))
	yPPM := int32(binary.LittleEndian.Uint32(buf[42:46]))
	if xPPM <= 0 || yPPM <= 0 {
		return 0, 0
	}
	return math.Round(float64(xPPM)*0.0254*10) / 10, math.Round(float64(yPPM)*0.0254*10) / 10
}

// RunPostSaveCommand executes a user-specified command after a successful save.
// commandLine may contain {path} as a placeholder for the saved file path.
// The first token (respecting double-quoted strings) is the executable; the
// remaining tokens are passed as individual arguments.  The process is started
// and detached — Atropos does not wait for it to finish.
func (a *App) RunPostSaveCommand(commandLine, savedPath string) error {
	if commandLine == "" {
		return nil
	}
	tokens := tokenizeCommandLine(commandLine)
	if len(tokens) == 0 {
		return nil
	}
	exe := strings.ReplaceAll(tokens[0], "{path}", savedPath)
	args := make([]string, len(tokens)-1)
	for i, t := range tokens[1:] {
		args[i] = strings.ReplaceAll(t, "{path}", savedPath)
	}
	cmd := exec.Command(exe, args...)
	hideCommandWindow(cmd)
	a.logf("RunPostSaveCommand: exe=%q args=%v", exe, args)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("RunPostSaveCommand: failed to start %q: %w", exe, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// tokenizeCommandLine splits a command-line string into tokens, respecting
// double-quoted sub-strings (which may contain spaces).
func tokenizeCommandLine(s string) []string {
	var tokens []string
	var cur strings.Builder
	inQuote := false
	for _, ch := range s {
		switch {
		case ch == '"':
			inQuote = !inQuote
		case ch == ' ' && !inQuote:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// OpenImageDialog shows a file picker for loading images.
func (a *App) OpenImageDialog() (string, error) {
	a.logf("OpenImageDialog: showing dialog")
	defaultDir := ""
	defaultName := ""
	if a.loadedFilePath != "" {
		dir := filepath.Dir(a.loadedFilePath)
		if _, statErr := os.Stat(dir); statErr == nil {
			defaultDir = dir
		}
		defaultName = filepath.Base(a.loadedFilePath)
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Open Image",
		DefaultDirectory: defaultDir,
		DefaultFilename:  defaultName,
		Filters: []runtime.FileFilter{
			{DisplayName: "Image Files (*.png;*.jpg;*.jpeg;*.bmp;*.tiff;*.tif)", Pattern: "*.png;*.jpg;*.jpeg;*.bmp;*.tiff;*.tif"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	a.logf("OpenImageDialog: path=%q err=%v", path, err)
	return path, err
}
