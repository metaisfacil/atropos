package main

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestPatchMatchPhotoshopReference is an opt-in, local regression harness for
// comparing PatchMatch against a controlled Photoshop Content-Aware Brush
// result. It is skipped in normal/CI runs because the reference images are not
// repository assets. Set ATROPOS_PM_RAW and ATROPOS_PM_REFERENCE to enable it.
func TestPatchMatchPhotoshopReference(t *testing.T) {
	raw := loadPMReferenceImage(t, "ATROPOS_PM_RAW")
	reference := loadPMReferenceImage(t, "ATROPOS_PM_REFERENCE")
	if raw.Bounds() != reference.Bounds() {
		t.Fatalf("reference bounds %v differ from raw bounds %v", reference.Bounds(), raw.Bounds())
	}

	const (
		brushX    = 516
		brushY    = 239
		brushSize = 50
	)
	mask, err := buildStrokeMask(raw.Bounds(), []TouchUpPoint{{X: brushX, Y: brushY}}, brushSize)
	if err != nil {
		t.Fatal(err)
	}
	changedBounds, changedPixels := pmChangedBounds(raw, reference)
	t.Logf("Photoshop changed %d pixels in %v; rasterized brush has %d covered pixels",
		changedPixels, changedBounds, pmMaskPixelCount(mask))

	translations := pmBestReferenceTranslations(raw, reference, mask, 12)
	for i, candidate := range translations {
		t.Logf("reference source %2d: offset=(%d,%d) MAE=%.3f RMSE=%.3f", i+1,
			candidate.dx, candidate.dy, candidate.mae, candidate.rmse)
	}
	if attemptPath := os.Getenv("ATROPOS_PM_ATTEMPT"); attemptPath != "" {
		attempt := loadPMReferenceImagePath(t, attemptPath)
		score := pmReferenceScore(attempt, reference, mask)
		t.Logf("saved Atropos attempt: MAE=%.3f RMSE=%.3f gradient=%.3f/%.3f meanRGB=(%.1f,%.1f,%.1f)/(%0.1f,%0.1f,%0.1f)",
			score.mae, score.rmse, score.gradient, score.referenceGradient,
			score.mean[0], score.mean[1], score.mean[2],
			score.referenceMean[0], score.referenceMean[1], score.referenceMean[2])
	}
	pmLogReferenceRegions(t, raw, reference, mask, translations[0])
	fullMask := image.NewAlpha(raw.Bounds())
	for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
		for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
			fullMask.SetAlpha(x, y, mask.AlphaAt(x, y))
		}
	}
	healed := pmHealedWorking(raw, fullMask)
	guideTranslations := pmBestReferenceTranslations(raw, healed, mask, 5)
	for i, candidate := range guideTranslations {
		t.Logf("directional-guide source %d: offset=(%d,%d) MAE=%.3f RMSE=%.3f", i+1,
			candidate.dx, candidate.dy, candidate.mae, candidate.rmse)
	}
	for _, candidate := range []pmReferenceTranslation{translations[0], guideTranslations[0]} {
		coherent := pmCoherentGuideFill(raw, healed, mask, candidate.dx, candidate.dy)
		score := pmReferenceScore(coherent, reference, mask)
		t.Logf("coherent guide+detail offset=(%d,%d): MAE=%.3f RMSE=%.3f gradient=%.3f/%.3f meanRGB=(%.1f,%.1f,%.1f)",
			candidate.dx, candidate.dy, score.mae, score.rmse, score.gradient, score.referenceGradient,
			score.mean[0], score.mean[1], score.mean[2])
		if outputPath := os.Getenv("ATROPOS_PM_COHERENT_OUTPUT"); outputPath != "" && candidate.dx == guideTranslations[0].dx && candidate.dy == guideTranslations[0].dy {
			writePMReferenceImage(t, outputPath, coherent)
		}
	}
	pmLogFinalNNF(t, raw, mask, 17, 5, translations[0])

	patchSizes := []int{7, 9, 13, 17, 21, 25, 29, 35, 41, 49, 51, 61}
	iterations := []int{5, 8, 12}
	if value := os.Getenv("ATROPOS_PM_PATCH"); value != "" {
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		patchSizes = []int{parsed}
	}
	if value := os.Getenv("ATROPOS_PM_ITERATIONS"); value != "" {
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		iterations = []int{parsed}
	}
	for _, patchSize := range patchSizes {
		for _, iterationCount := range iterations {
			out, fillErr := patchMatchChunkedFill(context.Background(), raw, mask, patchSize, iterationCount)
			if fillErr != nil {
				t.Fatalf("PatchMatchFill(%d,%d): %v", patchSize, iterationCount, fillErr)
			}
			score := pmReferenceScore(out, reference, mask)
			t.Logf("patch=%d iterations=%d: MAE=%.3f RMSE=%.3f gradient=%.3f/%.3f (%.1f%%) meanRGB=(%.1f,%.1f,%.1f)/(%0.1f,%0.1f,%0.1f)",
				patchSize, iterationCount, score.mae, score.rmse,
				score.gradient, score.referenceGradient, 100*score.gradient/maxFloat64(score.referenceGradient, 0.0001),
				score.mean[0], score.mean[1], score.mean[2],
				score.referenceMean[0], score.referenceMean[1], score.referenceMean[2])
			if outputPath := os.Getenv("ATROPOS_PM_OUTPUT"); outputPath != "" && patchSize == patchSizes[len(patchSizes)-1] && iterationCount == iterations[len(iterations)-1] {
				writePMReferenceImage(t, outputPath, out)
			}
			if patchSize == 17 && iterationCount == 5 {
				if score.mae > 7 {
					t.Errorf("production 50 px brush approximation regressed: MAE %.3f, want <= 7", score.mae)
				}
				gradientRatio := score.gradient / maxFloat64(score.referenceGradient, 0.0001)
				if gradientRatio < 0.85 || gradientRatio > 1.15 {
					t.Errorf("production texture energy regressed: ratio %.3f, want 0.85..1.15", gradientRatio)
				}
			}
		}
	}
}

// TestPatchMatchStrokeReplay is an opt-in visual harness for replaying one or
// more hard brush dabs against a local source image. Set ATROPOS_PM_STROKES to
// semicolon-separated x,y coordinates and ATROPOS_PM_OUTPUT to the result path.
func TestPatchMatchStrokeReplay(t *testing.T) {
	raw := loadPMReferenceImage(t, "ATROPOS_PM_RAW")
	strokeText := os.Getenv("ATROPOS_PM_STROKES")
	if strokeText == "" {
		t.Skip("set ATROPOS_PM_STROKES to run a local stroke replay")
	}
	var points []TouchUpPoint
	for _, stroke := range strings.Split(strokeText, ";") {
		coordinates := strings.Split(strings.TrimSpace(stroke), ",")
		if len(coordinates) != 2 {
			t.Fatalf("invalid stroke coordinate %q", stroke)
		}
		x, err := strconv.Atoi(strings.TrimSpace(coordinates[0]))
		if err != nil {
			t.Fatal(err)
		}
		y, err := strconv.Atoi(strings.TrimSpace(coordinates[1]))
		if err != nil {
			t.Fatal(err)
		}
		points = append(points, TouchUpPoint{X: float64(x), Y: float64(y)})
	}
	brushSize := 40
	if value := os.Getenv("ATROPOS_PM_BRUSH"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		brushSize = parsed
	}
	patchSize := maxInt(7, brushSize/3)
	if patchSize%2 == 0 {
		patchSize++
	}
	if value := os.Getenv("ATROPOS_PM_PATCH"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		patchSize = parsed
	}
	iterations := 5
	if value := os.Getenv("ATROPOS_PM_ITERATIONS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		iterations = parsed
	}
	mask, err := buildStrokeMask(raw.Bounds(), points, float64(brushSize))
	if err != nil {
		t.Fatal(err)
	}
	if guidePath := os.Getenv("ATROPOS_PM_GUIDE_OUTPUT"); guidePath != "" {
		fullMask := image.NewAlpha(raw.Bounds())
		draw.Draw(fullMask, mask.Rect, mask, mask.Rect.Min, draw.Src)
		guide := pmHealedWorking(raw, fullMask)
		if rawGuidePath := os.Getenv("ATROPOS_PM_RAW_GUIDE_OUTPUT"); rawGuidePath != "" {
			writePMReferenceImage(t, rawGuidePath, guide)
		}
		writePMReferenceImage(t, guidePath, pmSmoothedDirectionalGuide(guide, fullMask))
	}
	out, err := patchMatchChunkedFill(context.Background(), raw, mask, patchSize, iterations)
	if err != nil {
		t.Fatal(err)
	}
	outputPath := os.Getenv("ATROPOS_PM_OUTPUT")
	if outputPath == "" {
		t.Skip("set ATROPOS_PM_OUTPUT to write the stroke replay")
	}
	writePMReferenceImage(t, outputPath, out)
}

func pmCoherentGuideFill(raw, guide *image.NRGBA, mask *image.Alpha, dx, dy int) *image.NRGBA {
	out := cloneNRGBA(raw)
	base := pmLowPassNRGBA(raw)
	for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
		for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
			alpha := float32(mask.AlphaAt(x, y).A) / 255
			if alpha == 0 || x+dx < 0 || x+dx >= raw.Bounds().Dx() || y+dy < 0 || y+dy >= raw.Bounds().Dy() {
				continue
			}
			oi := y*out.Stride + x*4
			si := (y+dy)*raw.Stride + (x+dx)*4
			for channel := 0; channel < 3; channel++ {
				fill := float32(guide.Pix[oi+channel]) + float32(raw.Pix[si+channel]) - float32(base.Pix[si+channel])
				out.Pix[oi+channel] = byte(clampFloat32((1-alpha)*float32(raw.Pix[oi+channel]) + alpha*fill))
			}
		}
	}
	return out
}

func pmLogFinalNNF(t *testing.T, raw *image.NRGBA, mask *image.Alpha, patchSize, iterations int, reference pmReferenceTranslation) {
	t.Helper()
	fullMask := image.NewAlpha(raw.Bounds())
	for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
		for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
			fullMask.SetAlpha(x, y, mask.AlphaAt(x, y))
		}
	}
	srcPyramid, maskPyramid := buildPatchPyramid(raw, fullMask, patchSize)
	var parent *pmSolution
	for levelIndex := len(srcPyramid) - 1; levelIndex >= 0; levelIndex-- {
		level := preparePMLevel(srcPyramid[levelIndex], maskPyramid[levelIndex], patchSize)
		if len(level.sources) == 0 {
			continue
		}
		working := seedPMWorking(level, parent)
		var nnf []pmPoint
		var costs []float32
		var stats []pmPatchStats
		for round := 0; round < 2; round++ {
			var err error
			nnf, costs, stats, err = solvePMLevel(context.Background(), level, working, parent, iterations, round)
			if err != nil {
				t.Fatal(err)
			}
			if levelIndex == 0 {
				bounds := maskBounds(level.mask)
				cx, cy := bounds.Min.X+bounds.Dx()/2, bounds.Min.Y+bounds.Dy()/2
				match := nnf[cy*level.w+cx]
				referencePoint := pmPoint{int32(cx + reference.dx), int32(cy + reference.dy)}
				referenceCost := pmPatchCost(level, &level.targetPlanes, stats, cx, cy, referencePoint, float32(math.Inf(1)))
				t.Logf("final level round %d center NNF=(%d,%d), cost=%.3f; Photoshop-like cost=%.3f",
					round+1, int(match.x)-cx, int(match.y)-cy, costs[cy*level.w+cx], referenceCost)
			}
			working, err = reconstructPMLevel(context.Background(), level, working, nnf, costs, stats)
			if err != nil {
				t.Fatal(err)
			}
			parent = nil
		}
		parent = &pmSolution{level: level, working: working, nnf: nnf}
	}
	if parent == nil {
		t.Fatal("no final PatchMatch solution")
	}

	type offsetCount struct {
		dx, dy int
		count  int
		cost   float64
	}
	histogram := make(map[[2]int]*offsetCount)
	level := parent.level
	finalMaskBounds := maskBounds(level.mask)
	centers := finalMaskBounds.Inset(-level.half).Intersect(level.active)
	for y := centers.Min.Y; y < centers.Max.Y; y++ {
		for x := centers.Min.X; x < centers.Max.X; x++ {
			id := y*level.w + x
			match := parent.nnf[id]
			dx, dy := int(match.x)-x, int(match.y)-y
			key := [2]int{int(pmVoteClusterKey(int32(dx))), int(pmVoteClusterKey(int32(dy)))}
			entry := histogram[key]
			if entry == nil {
				entry = &offsetCount{dx: dx, dy: dy}
				histogram[key] = entry
			}
			entry.count++
		}
	}
	counts := make([]offsetCount, 0, len(histogram))
	for _, entry := range histogram {
		counts = append(counts, *entry)
	}
	sort.Slice(counts, func(i, j int) bool { return counts[i].count > counts[j].count })
	for i := 0; i < minInt(12, len(counts)); i++ {
		t.Logf("NNF cluster %2d: offset=(%d,%d) centers=%d", i+1, counts[i].dx, counts[i].dy, counts[i].count)
	}

	centerX, centerY := finalMaskBounds.Min.X+finalMaskBounds.Dx()/2, finalMaskBounds.Min.Y+finalMaskBounds.Dy()/2
	id := centerY*level.w + centerX
	current := parent.nnf[id]
	referencePoint := pmPoint{int32(centerX + reference.dx), int32(centerY + reference.dy)}
	currentCost := pmPatchCost(level, &level.targetPlanes, level.targetStats, centerX, centerY, current, float32(math.Inf(1)))
	referenceCost := pmPatchCost(level, &level.targetPlanes, level.targetStats, centerX, centerY, referencePoint, float32(math.Inf(1)))
	t.Logf("center NNF offset=(%d,%d), cost=%.3f; Photoshop-like offset=(%d,%d), cost=%.3f",
		int(current.x)-centerX, int(current.y)-centerY, currentCost, reference.dx, reference.dy, referenceCost)
}

func loadPMReferenceImage(t *testing.T, environmentName string) *image.NRGBA {
	t.Helper()
	path := os.Getenv(environmentName)
	if path == "" {
		t.Skipf("set %s to run the Photoshop reference comparison", environmentName)
	}
	return loadPMReferenceImagePath(t, path)
}

func loadPMReferenceImagePath(t *testing.T, path string) *image.NRGBA {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	return normalizeNRGBA(toNRGBA(decoded))
}

func writePMReferenceImage(t *testing.T, path string, src *image.NRGBA) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, src); err != nil {
		t.Fatal(err)
	}
}

func pmLogReferenceRegions(t *testing.T, raw, reference *image.NRGBA, mask *image.Alpha, source pmReferenceTranslation) {
	t.Helper()
	fullMask := image.NewAlpha(raw.Bounds())
	for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
		for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
			fullMask.SetAlpha(x, y, mask.AlphaAt(x, y))
		}
	}
	healed := pmHealedWorking(raw, fullMask)
	var rawMean, referenceMean, sourceMean, healedMean [3]float64
	var samples float64
	for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
		for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
			if mask.AlphaAt(x, y).A < 250 {
				continue
			}
			ri := y*raw.Stride + x*4
			si := (y+source.dy)*raw.Stride + (x+source.dx)*4
			pi := y*reference.Stride + x*4
			for channel := 0; channel < 3; channel++ {
				rawMean[channel] += float64(raw.Pix[ri+channel])
				sourceMean[channel] += float64(raw.Pix[si+channel])
				referenceMean[channel] += float64(reference.Pix[pi+channel])
				healedMean[channel] += float64(healed.Pix[ri+channel])
			}
			samples++
		}
	}
	for channel := 0; channel < 3; channel++ {
		rawMean[channel] /= samples
		sourceMean[channel] /= samples
		referenceMean[channel] /= samples
		healedMean[channel] /= samples
	}
	t.Logf("hard-circle means raw=(%.1f,%.1f,%.1f) healed=(%.1f,%.1f,%.1f) Photoshop=(%.1f,%.1f,%.1f) source%s=(%.1f,%.1f,%.1f)",
		rawMean[0], rawMean[1], rawMean[2], healedMean[0], healedMean[1], healedMean[2],
		referenceMean[0], referenceMean[1], referenceMean[2], source,
		sourceMean[0], sourceMean[1], sourceMean[2])
}

func pmChangedBounds(a, b *image.NRGBA) (image.Rectangle, int) {
	bounds := a.Bounds().Intersect(b.Bounds())
	changed := image.Rectangle{}
	count := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			ai := y*a.Stride + x*4
			bi := y*b.Stride + x*4
			if a.Pix[ai] == b.Pix[bi] && a.Pix[ai+1] == b.Pix[bi+1] && a.Pix[ai+2] == b.Pix[bi+2] {
				continue
			}
			pixel := image.Rect(x, y, x+1, y+1)
			if count == 0 {
				changed = pixel
			} else {
				changed = changed.Union(pixel)
			}
			count++
		}
	}
	return changed, count
}

func pmMaskPixelCount(mask *image.Alpha) int {
	count := 0
	for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
		for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
			if mask.AlphaAt(x, y).A != 0 {
				count++
			}
		}
	}
	return count
}

type pmReferenceTranslation struct {
	dx, dy int
	mae    float64
	rmse   float64
}

// pmBestReferenceTranslations tests whether Photoshop's result is primarily a
// translated source patch. A sparse first pass keeps this diagnostic cheap;
// the best candidates are then scored over the complete hard brush interior.
func pmBestReferenceTranslations(raw, reference *image.NRGBA, mask *image.Alpha, keep int) []pmReferenceTranslation {
	type sampledPixel struct{ x, y int }
	all := make([]sampledPixel, 0, 2048)
	sparse := make([]sampledPixel, 0, 128)
	for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
		for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
			if mask.AlphaAt(x, y).A < 250 {
				continue
			}
			pixel := sampledPixel{x, y}
			all = append(all, pixel)
			if (x*17+y*31)%19 == 0 {
				sparse = append(sparse, pixel)
			}
		}
	}

	candidates := make([]pmReferenceTranslation, 0, raw.Bounds().Dx()*raw.Bounds().Dy())
	for dy := raw.Bounds().Min.Y - mask.Rect.Min.Y; dy < raw.Bounds().Max.Y-mask.Rect.Max.Y; dy++ {
		for dx := raw.Bounds().Min.X - mask.Rect.Min.X; dx < raw.Bounds().Max.X-mask.Rect.Max.X; dx++ {
			if absInt(dx) <= mask.Rect.Dx() && absInt(dy) <= mask.Rect.Dy() {
				continue
			}
			var absolute float64
			for _, pixel := range sparse {
				rawIndex := (pixel.y+dy)*raw.Stride + (pixel.x+dx)*4
				refIndex := pixel.y*reference.Stride + pixel.x*4
				for channel := 0; channel < 3; channel++ {
					absolute += math.Abs(float64(raw.Pix[rawIndex+channel]) - float64(reference.Pix[refIndex+channel]))
				}
			}
			candidates = append(candidates, pmReferenceTranslation{dx: dx, dy: dy, mae: absolute / float64(len(sparse)*3)})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mae < candidates[j].mae })
	refine := minInt(len(candidates), maxInt(keep*32, 256))
	for i := 0; i < refine; i++ {
		candidate := &candidates[i]
		var absolute, squared float64
		for _, pixel := range all {
			rawIndex := (pixel.y+candidate.dy)*raw.Stride + (pixel.x+candidate.dx)*4
			refIndex := pixel.y*reference.Stride + pixel.x*4
			for channel := 0; channel < 3; channel++ {
				difference := float64(raw.Pix[rawIndex+channel]) - float64(reference.Pix[refIndex+channel])
				absolute += math.Abs(difference)
				squared += difference * difference
			}
		}
		denominator := float64(len(all) * 3)
		candidate.mae = absolute / denominator
		candidate.rmse = math.Sqrt(squared / denominator)
	}
	candidates = candidates[:refine]
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mae < candidates[j].mae })
	return candidates[:minInt(keep, len(candidates))]
}

type pmReferenceMetrics struct {
	mae, rmse                   float64
	gradient, referenceGradient float64
	mean, referenceMean         [3]float64
}

func pmReferenceScore(actual, reference *image.NRGBA, mask *image.Alpha) pmReferenceMetrics {
	var result pmReferenceMetrics
	var samples, gradientSamples int
	for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
		for x := mask.Rect.Min.X; x < mask.Rect.Max.X; x++ {
			if mask.AlphaAt(x, y).A < 250 {
				continue
			}
			actualIndex := y*actual.Stride + x*4
			referenceIndex := y*reference.Stride + x*4
			for channel := 0; channel < 3; channel++ {
				difference := float64(actual.Pix[actualIndex+channel]) - float64(reference.Pix[referenceIndex+channel])
				result.mae += math.Abs(difference)
				result.rmse += difference * difference
				result.mean[channel] += float64(actual.Pix[actualIndex+channel])
				result.referenceMean[channel] += float64(reference.Pix[referenceIndex+channel])
			}
			samples++
			if x > mask.Rect.Min.X && y > mask.Rect.Min.Y && mask.AlphaAt(x-1, y).A >= 250 && mask.AlphaAt(x, y-1).A >= 250 {
				result.gradient += pmPixelGradient(actual, x, y)
				result.referenceGradient += pmPixelGradient(reference, x, y)
				gradientSamples++
			}
		}
	}
	channels := float64(samples * 3)
	result.mae /= channels
	result.rmse = math.Sqrt(result.rmse / channels)
	for channel := range result.mean {
		result.mean[channel] /= float64(samples)
		result.referenceMean[channel] /= float64(samples)
	}
	result.gradient /= float64(gradientSamples)
	result.referenceGradient /= float64(gradientSamples)
	return result
}

func pmPixelGradient(src *image.NRGBA, x, y int) float64 {
	center := y*src.Stride + x*4
	left := y*src.Stride + (x-1)*4
	up := (y-1)*src.Stride + x*4
	var energy float64
	for channel := 0; channel < 3; channel++ {
		dx := float64(src.Pix[center+channel]) - float64(src.Pix[left+channel])
		dy := float64(src.Pix[center+channel]) - float64(src.Pix[up+channel])
		energy += dx*dx + dy*dy
	}
	return math.Sqrt(energy / 3)
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func (candidate pmReferenceTranslation) String() string {
	return fmt.Sprintf("(%d,%d): %.3f", candidate.dx, candidate.dy, candidate.mae)
}
