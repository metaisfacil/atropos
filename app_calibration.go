package main

// app_calibration.go provides an isolated ground-truth annotation workflow for
// corner-detector calibration. It never changes the active editing document.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"atropos/internal/raster"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type CalibrationLoadRequest struct {
	FilePath string `json:"filePath"`
}

type CalibrationImageInfo struct {
	ImagePath string `json:"imagePath"`
	Preview   string `json:"preview"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type CalibrationPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type CalibrationEntryRequest struct {
	ImagePath string             `json:"imagePath"`
	Width     int                `json:"width"`
	Height    int                `json:"height"`
	Corners   []CalibrationPoint `json:"corners"`
}

type CalibrationSaveRequest struct {
	Entries []CalibrationEntryRequest `json:"entries"`
}

type CalibrationSaveResult struct {
	OutputPath string `json:"outputPath"`
	Count      int    `json:"count"`
	Cancelled  bool   `json:"cancelled,omitempty"`
}

type calibrationNamedPoint struct {
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
}

type calibrationDatasetImage struct {
	Path    string                  `json:"path"`
	Width   int                     `json:"width"`
	Height  int                     `json:"height"`
	Corners []calibrationNamedPoint `json:"corners"`
}

type calibrationCoordinateSystem struct {
	Origin      string   `json:"origin"`
	Units       string   `json:"units"`
	CornerOrder []string `json:"cornerOrder"`
}

type calibrationDataset struct {
	SchemaVersion    int                         `json:"schemaVersion"`
	CreatedAt        string                      `json:"createdAt"`
	CoordinateSystem calibrationCoordinateSystem `json:"coordinateSystem"`
	Images           []calibrationDatasetImage   `json:"images"`
}

var calibrationCornerNames = []string{"topLeft", "topRight", "bottomRight", "bottomLeft"}

// CalibrationOpenFilesDialog selects a batch of scans without loading them
// into the main editing pipeline.
func (a *App) CalibrationOpenFilesDialog() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Scans for Corner Calibration",
		Filters: []runtime.FileFilter{
			{DisplayName: "Image Files (*.png;*.jpg;*.jpeg;*.bmp;*.tiff;*.tif;*.gif;*.webp)", Pattern: "*.png;*.jpg;*.jpeg;*.bmp;*.tiff;*.tif;*.gif;*.webp"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
}

// CalibrationLoadImage decodes one scan into a dedicated one-entry preview
// store. The active document, undo history, and crop state are untouched.
func (a *App) CalibrationLoadImage(req CalibrationLoadRequest) (*CalibrationImageInfo, error) {
	if strings.TrimSpace(req.FilePath) == "" {
		return nil, fmt.Errorf("image path is empty")
	}
	a.calibrationMu.Lock()
	defer a.calibrationMu.Unlock()

	src, err := a.decodeImageFile(req.FilePath)
	if err != nil {
		return nil, fmt.Errorf("load calibration image %q: %w", filepath.Base(req.FilePath), err)
	}
	img := raster.ToNRGBA(src)
	a.calibrationPreviewAssets.Reset()
	previewURL, err := a.calibrationPreviewAssets.Register(img)
	if err != nil {
		return nil, fmt.Errorf("register calibration preview: %w", err)
	}
	b := img.Bounds()
	return &CalibrationImageInfo{
		ImagePath: req.FilePath,
		Preview:   previewURL,
		Width:     b.Dx(),
		Height:    b.Dy(),
	}, nil
}

// RenderCalibrationPreviewViewport renders against the isolated calibration
// preview store while retaining the same request/response schema as the main
// viewport renderer.
func (a *App) RenderCalibrationPreviewViewport(req PreviewViewportRequest) (PreviewViewportResponse, error) {
	return a.renderPreviewViewport(a.calibrationPreviewAssets, req)
}

// CalibrationClearPreview releases the full-resolution calibration scan.
func (a *App) CalibrationClearPreview() {
	a.calibrationMu.Lock()
	defer a.calibrationMu.Unlock()
	a.calibrationPreviewAssets.Reset()
}

// CalibrationSaveDataset validates the completed samples, asks for a JSON
// destination, and writes portable paths relative to the dataset file.
func (a *App) CalibrationSaveDataset(req CalibrationSaveRequest) (*CalibrationSaveResult, error) {
	if len(req.Entries) == 0 {
		return nil, fmt.Errorf("annotate at least one image before exporting")
	}
	defaultDir := ""
	if dir := commonCalibrationDirectory(req.Entries); dir != "" {
		defaultDir = dir
	}
	outputPath, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:            "Export Corner Ground Truth",
		DefaultDirectory: defaultDir,
		DefaultFilename:  "corner-ground-truth.json",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return nil, err
	}
	if outputPath == "" {
		return &CalibrationSaveResult{Cancelled: true}, nil
	}
	if filepath.Ext(outputPath) == "" {
		outputPath += ".json"
	}
	if err := writeCalibrationDataset(outputPath, req.Entries, time.Now()); err != nil {
		return nil, err
	}
	return &CalibrationSaveResult{OutputPath: outputPath, Count: len(req.Entries)}, nil
}

func writeCalibrationDataset(outputPath string, entries []CalibrationEntryRequest, createdAt time.Time) error {
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("output path is empty")
	}
	baseDir := filepath.Dir(outputPath)
	dataset := calibrationDataset{
		SchemaVersion: 1,
		CreatedAt:     createdAt.UTC().Format(time.RFC3339),
		CoordinateSystem: calibrationCoordinateSystem{
			Origin:      "top-left",
			Units:       "source-image pixels",
			CornerOrder: append([]string(nil), calibrationCornerNames...),
		},
		Images: make([]calibrationDatasetImage, 0, len(entries)),
	}

	seen := make(map[string]struct{}, len(entries))
	for i, entry := range entries {
		if err := validateCalibrationEntry(entry); err != nil {
			return fmt.Errorf("sample %d: %w", i+1, err)
		}
		absolute, err := filepath.Abs(entry.ImagePath)
		if err != nil {
			return fmt.Errorf("sample %d: resolve image path: %w", i+1, err)
		}
		key := strings.ToLower(filepath.Clean(absolute))
		if _, ok := seen[key]; ok {
			return fmt.Errorf("sample %d: duplicate image path %q", i+1, entry.ImagePath)
		}
		seen[key] = struct{}{}

		storedPath := absolute
		if relative, relErr := filepath.Rel(baseDir, absolute); relErr == nil {
			storedPath = relative
		}
		imageEntry := calibrationDatasetImage{
			Path:    filepath.ToSlash(storedPath),
			Width:   entry.Width,
			Height:  entry.Height,
			Corners: make([]calibrationNamedPoint, 4),
		}
		for cornerIndex, point := range entry.Corners {
			imageEntry.Corners[cornerIndex] = calibrationNamedPoint{
				Name: calibrationCornerNames[cornerIndex],
				X:    point.X,
				Y:    point.Y,
			}
		}
		dataset.Images = append(dataset.Images, imageEntry)
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create calibration dataset: %w", err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encodeErr := encoder.Encode(dataset)
	closeErr := file.Close()
	if encodeErr != nil {
		return fmt.Errorf("encode calibration dataset: %w", encodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close calibration dataset: %w", closeErr)
	}
	return nil
}

func validateCalibrationEntry(entry CalibrationEntryRequest) error {
	if strings.TrimSpace(entry.ImagePath) == "" {
		return fmt.Errorf("image path is empty")
	}
	if entry.Width <= 0 || entry.Height <= 0 {
		return fmt.Errorf("invalid source dimensions %dx%d", entry.Width, entry.Height)
	}
	if len(entry.Corners) != 4 {
		return fmt.Errorf("expected 4 corners, got %d", len(entry.Corners))
	}
	for i, point := range entry.Corners {
		if point.X < 0 || point.X >= entry.Width || point.Y < 0 || point.Y >= entry.Height {
			return fmt.Errorf("%s corner (%d,%d) is outside %dx%d", calibrationCornerNames[i], point.X, point.Y, entry.Width, entry.Height)
		}
	}
	for i := range entry.Corners {
		a := entry.Corners[i]
		b := entry.Corners[(i+1)%4]
		c := entry.Corners[(i+2)%4]
		cross := int64(b.X-a.X)*int64(c.Y-b.Y) - int64(b.Y-a.Y)*int64(c.X-b.X)
		if cross == 0 {
			return fmt.Errorf("corners form a degenerate quadrilateral")
		}
		if cross < 0 {
			return fmt.Errorf("corners must form a convex quadrilateral in clockwise screen order: top-left, top-right, bottom-right, bottom-left")
		}
	}
	return nil
}

func commonCalibrationDirectory(entries []CalibrationEntryRequest) string {
	if len(entries) == 0 {
		return ""
	}
	dir := filepath.Dir(entries[0].ImagePath)
	for _, entry := range entries[1:] {
		candidate := filepath.Dir(entry.ImagePath)
		for !strings.EqualFold(dir, candidate) {
			parent := filepath.Dir(dir)
			if parent == dir {
				return ""
			}
			dir = parent
			if relative, err := filepath.Rel(dir, candidate); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				break
			}
		}
	}
	return dir
}
