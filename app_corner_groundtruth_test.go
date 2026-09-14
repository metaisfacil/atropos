package main

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"atropos/internal/raster"
)

// Set ATROPOS_CORNER_GROUND_TRUTH to an exported Corner Calibration dataset
// to run the production detector against real scans. The corpus is intentionally
// opt-in because it lives outside the repository and is several gigabytes.
func TestCornerDetectorGroundTruth(t *testing.T) {
	datasetPath := os.Getenv("ATROPOS_CORNER_GROUND_TRUTH")
	if datasetPath == "" {
		t.Skip("set ATROPOS_CORNER_GROUND_TRUTH to run the real-scan evaluator")
	}
	dataset := loadCornerGroundTruth(t, datasetPath)
	if len(dataset.Images) < 5 {
		t.Fatalf("need at least 5 ground-truth images, got %d", len(dataset.Images))
	}

	var train, holdout cornerEvalMetrics
	requestedIndex := 0
	if value := os.Getenv("ATROPOS_CORNER_GROUND_TRUTH_INDEX"); value != "" {
		var err error
		requestedIndex, err = strconv.Atoi(value)
		if err != nil || requestedIndex < 1 || requestedIndex > len(dataset.Images) {
			t.Fatalf("ATROPOS_CORNER_GROUND_TRUTH_INDEX must be between 1 and %d", len(dataset.Images))
		}
	}
	requestedSplit := os.Getenv("ATROPOS_CORNER_GROUND_TRUTH_SPLIT")
	if requestedSplit != "" && requestedSplit != "train" && requestedSplit != "holdout" {
		t.Fatal("ATROPOS_CORNER_GROUND_TRUTH_SPLIT must be train or holdout")
	}
	started := time.Now()
	for index, sample := range dataset.Images {
		if requestedIndex != 0 && requestedIndex != index+1 {
			continue
		}
		isHoldout := index%5 == 4
		if (requestedSplit == "train" && isHoldout) || (requestedSplit == "holdout" && !isHoldout) {
			continue
		}
		imgPath := sample.Path
		if !isAbsoluteWindowsPath(imgPath) {
			imgPath = resolveCalibrationImagePath(datasetPath, imgPath)
		}
		app := NewApp()
		src, err := app.decodeImageFile(imgPath)
		if err != nil {
			t.Fatalf("decode sample %d %q: %v", index+1, imgPath, err)
		}
		app.currentImage = raster.ToNRGBA(src)
		app.imageLoaded = true
		bounds := app.currentImage.Bounds()
		if bounds.Dx() != sample.Width || bounds.Dy() != sample.Height {
			t.Fatalf("sample %d dimensions changed: dataset=%dx%d actual=%dx%d", index+1, sample.Width, sample.Height, bounds.Dx(), bounds.Dy())
		}

		suggested := suggestCornerParams(sample.Width, sample.Height)
		result, err := app.DetectCorners(CornerDetectRequest{
			MaxCorners:   suggested.MaxCorners,
			QualityLevel: 1,
			MinDistance:  suggested.MinDistance,
			AccentValue:  20,
			UseStretch:   true,
			StretchLow:   0.01,
			StretchHigh:  0.99,
		})
		if err != nil {
			t.Fatalf("detect sample %d %q: %v", index+1, imgPath, err)
		}
		distances := nearestCornerDistances(sample.Corners, result.Corners)
		target := &train
		split := "train"
		if isHoldout {
			target = &holdout
			split = "holdout"
		}
		target.add(distances, len(result.Corners))
		t.Logf("%s %02d %-36s proposals=%3d nearest=%s", split, index+1, filepath.Base(imgPath), len(result.Corners), formatCornerDistances(distances))
		if os.Getenv("ATROPOS_CORNER_GROUND_TRUTH_VERBOSE") != "" {
			t.Logf("%s", formatNearestCornerMatches(sample.Corners, result.Corners))
		}
	}

	t.Logf("TRAIN   %s", train.summary())
	t.Logf("HOLDOUT %s", holdout.summary())
	t.Logf("ALL     %s elapsed=%s", mergedCornerEvalMetrics(train, holdout).summary(), time.Since(started).Round(time.Millisecond))
}

type cornerEvalMetrics struct {
	distances     []float64
	scans         int
	proposalTotal int
	allWithin20   int
	allWithin40   int
	allWithin80   int
	allWithin160  int
}

func (m *cornerEvalMetrics) add(distances []float64, proposals int) {
	m.scans++
	m.proposalTotal += proposals
	m.distances = append(m.distances, distances...)
	if allDistancesWithin(distances, 20) {
		m.allWithin20++
	}
	if allDistancesWithin(distances, 40) {
		m.allWithin40++
	}
	if allDistancesWithin(distances, 80) {
		m.allWithin80++
	}
	if allDistancesWithin(distances, 160) {
		m.allWithin160++
	}
}

func (m cornerEvalMetrics) summary() string {
	if len(m.distances) == 0 || m.scans == 0 {
		return "no samples"
	}
	distances := append([]float64(nil), m.distances...)
	sort.Float64s(distances)
	sum := 0.0
	for _, distance := range distances {
		sum += distance
	}
	return fmt.Sprintf(
		"corners=%d mean=%.1f p50=%.1f p90=%.1f max=%.1f recall[20/40/80/160]=%.1f/%.1f/%.1f/%.1f%% scans-all[20/40/80/160]=%d/%d/%d/%d of %d proposals=%.1f",
		len(distances), sum/float64(len(distances)), percentileDistance(distances, 0.50), percentileDistance(distances, 0.90), distances[len(distances)-1],
		cornerRecall(distances, 20), cornerRecall(distances, 40), cornerRecall(distances, 80), cornerRecall(distances, 160),
		m.allWithin20, m.allWithin40, m.allWithin80, m.allWithin160, m.scans, float64(m.proposalTotal)/float64(m.scans),
	)
}

func mergedCornerEvalMetrics(a, b cornerEvalMetrics) cornerEvalMetrics {
	return cornerEvalMetrics{
		distances:     append(append([]float64(nil), a.distances...), b.distances...),
		scans:         a.scans + b.scans,
		proposalTotal: a.proposalTotal + b.proposalTotal,
		allWithin20:   a.allWithin20 + b.allWithin20,
		allWithin40:   a.allWithin40 + b.allWithin40,
		allWithin80:   a.allWithin80 + b.allWithin80,
		allWithin160:  a.allWithin160 + b.allWithin160,
	}
}

func nearestCornerDistances(truth []calibrationNamedPoint, proposals []image.Point) []float64 {
	result := make([]float64, len(truth))
	for index, corner := range truth {
		best := math.Inf(1)
		for _, proposal := range proposals {
			distance := math.Hypot(float64(corner.X-proposal.X), float64(corner.Y-proposal.Y))
			if distance < best {
				best = distance
			}
		}
		result[index] = best
	}
	return result
}

func percentileDistance(sorted []float64, fraction float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	index := int(math.Ceil(fraction*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func cornerRecall(distances []float64, threshold float64) float64 {
	within := 0
	for _, distance := range distances {
		if distance <= threshold {
			within++
		}
	}
	return 100 * float64(within) / float64(len(distances))
}

func allDistancesWithin(distances []float64, threshold float64) bool {
	if len(distances) != 4 {
		return false
	}
	for _, distance := range distances {
		if distance > threshold {
			return false
		}
	}
	return true
}

func formatCornerDistances(distances []float64) string {
	return fmt.Sprintf("TL %.1f TR %.1f BR %.1f BL %.1f", distances[0], distances[1], distances[2], distances[3])
}

func formatNearestCornerMatches(truth []calibrationNamedPoint, proposals []image.Point) string {
	result := ""
	for index, corner := range truth {
		best := image.Point{}
		bestDistance := math.Inf(1)
		for _, proposal := range proposals {
			distance := math.Hypot(float64(corner.X-proposal.X), float64(corner.Y-proposal.Y))
			if distance < bestDistance {
				best, bestDistance = proposal, distance
			}
		}
		if index > 0 {
			result += " "
		}
		result += fmt.Sprintf("%s truth=(%d,%d) nearest=(%d,%d) d=%.1f", corner.Name, corner.X, corner.Y, best.X, best.Y, bestDistance)
	}
	return result
}

func loadCornerGroundTruth(t *testing.T, path string) calibrationDataset {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dataset calibrationDataset
	if err := json.Unmarshal(data, &dataset); err != nil {
		t.Fatal(err)
	}
	return dataset
}

func resolveCalibrationImagePath(datasetPath, imagePath string) string {
	return filepath.Join(filepath.Dir(datasetPath), filepath.FromSlash(imagePath))
}

func isAbsoluteWindowsPath(path string) bool {
	return len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '/' || path[2] == '\\')
}
