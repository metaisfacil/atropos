package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validCalibrationEntry(path string) CalibrationEntryRequest {
	return CalibrationEntryRequest{
		ImagePath: path,
		Width:     1000,
		Height:    800,
		Corners: []CalibrationPoint{
			{X: 10, Y: 20},
			{X: 980, Y: 15},
			{X: 990, Y: 780},
			{X: 8, Y: 790},
		},
	}
}

func TestWriteCalibrationDataset(t *testing.T) {
	dir := t.TempDir()
	scanDir := filepath.Join(dir, "scans")
	if err := os.Mkdir(scanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "corner-ground-truth.json")
	created := time.Date(2026, 9, 14, 18, 30, 0, 0, time.FixedZone("EDT", -4*60*60))

	if err := writeCalibrationDataset(output, []CalibrationEntryRequest{
		validCalibrationEntry(filepath.Join(scanDir, "scan 01.tif")),
	}, created); err != nil {
		t.Fatalf("writeCalibrationDataset: %v", err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var dataset calibrationDataset
	if err := json.Unmarshal(data, &dataset); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if dataset.SchemaVersion != 1 || dataset.CreatedAt != "2026-09-14T22:30:00Z" {
		t.Fatalf("unexpected dataset header: %+v", dataset)
	}
	if len(dataset.Images) != 1 {
		t.Fatalf("got %d images, want 1", len(dataset.Images))
	}
	image := dataset.Images[0]
	if image.Path != "scans/scan 01.tif" {
		t.Fatalf("stored path = %q", image.Path)
	}
	if len(image.Corners) != 4 {
		t.Fatalf("got %d corners", len(image.Corners))
	}
	for i, want := range calibrationCornerNames {
		if image.Corners[i].Name != want {
			t.Fatalf("corner %d name = %q, want %q", i, image.Corners[i].Name, want)
		}
	}
}

func TestValidateCalibrationEntry(t *testing.T) {
	entry := validCalibrationEntry("scan.tif")
	if err := validateCalibrationEntry(entry); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}

	t.Run("out of bounds", func(t *testing.T) {
		bad := entry
		bad.Corners = append([]CalibrationPoint(nil), entry.Corners...)
		bad.Corners[2].X = bad.Width
		if err := validateCalibrationEntry(bad); err == nil || !strings.Contains(err.Error(), "outside") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("reversed order", func(t *testing.T) {
		bad := entry
		bad.Corners = []CalibrationPoint{entry.Corners[0], entry.Corners[3], entry.Corners[2], entry.Corners[1]}
		if err := validateCalibrationEntry(bad); err == nil || !strings.Contains(err.Error(), "clockwise") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("wrong count", func(t *testing.T) {
		bad := entry
		bad.Corners = entry.Corners[:3]
		if err := validateCalibrationEntry(bad); err == nil || !strings.Contains(err.Error(), "expected 4") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestCalibrationPreviewStoreIsIndependent(t *testing.T) {
	app := NewApp()
	if app.previewAssets == app.calibrationPreviewAssets {
		t.Fatal("calibration and document previews must use separate stores")
	}
}
