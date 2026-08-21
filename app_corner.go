package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
)

// suggestCornerParams returns sensible detection defaults derived from image
// dimensions. minDistance scales with the shorter side so that corner density
// remains proportional across images of very different sizes.
func suggestCornerParams(w, h int) SuggestedCornerParams {
	shortSide := w
	if h < shortSide {
		shortSide = h
	}
	minDist := shortSide / 30
	if minDist < 10 {
		minDist = 10
	}
	if minDist > 200 {
		minDist = 200
	}
	return SuggestedCornerParams{
		MinDistance: minDist,
		MaxCorners:  500,
	}
}

// CornerDetectRequest contains parameters for the Shi-Tomasi corner detector.
type CornerDetectRequest struct {
	MaxCorners   int     `json:"maxCorners"`
	QualityLevel float64 `json:"qualityLevel"`
	MinDistance  int     `json:"minDistance"`
	AccentValue  int     `json:"accentValue"`
	UseStretch   bool    `json:"useStretch"`
	StretchLow   float64 `json:"stretchLow"`
	StretchHigh  float64 `json:"stretchHigh"`
}

// cornerDetectBlockSize uses a wider structure-tensor window at the coarsest
// scale. Broad, low-contrast object corners are otherwise easily outranked by
// small high-contrast details after the image has been reduced to that scale.
func cornerDetectBlockSize(scale int) int {
	if scale >= 4 {
		return 11
	}
	return 7
}

type cornerDetectPass struct {
	scale               int
	maxCorners          int
	highlightBlackPoint int
}

// cornerDetectPasses divides the requested result budget between detail,
// mid-scale, broad-corner, silhouette, and highlight-boundary passes. The two
// highlight passes emulate aggressive levels curves and need only small
// candidate shares because they suppress nearly all non-highlight detail.
func cornerDetectPasses(maxCorners int) []cornerDetectPass {
	if maxCorners < 1 {
		maxCorners = 1
	}
	weights := [...]int{4, 7, 16, 10, 17, 10}
	passes := []cornerDetectPass{
		{scale: 1, highlightBlackPoint: 240},
		{scale: 1, highlightBlackPoint: 230},
		{scale: 1},
		{scale: 2},
		{scale: 4},
		{scale: 16},
	}
	const totalWeight = 64

	allocated := 0
	for i := range passes {
		passes[i].maxCorners = maxCorners * weights[i] / totalWeight
		allocated += passes[i].maxCorners
	}
	// Assign rounding leftovers to broad object corners first, then to the
	// highlight boundary and silhouette passes.
	for _, i := range [...]int{4, 0, 1, 5, 2, 3} {
		if allocated == maxCorners {
			break
		}
		passes[i].maxCorners++
		allocated++
	}
	return passes
}

// stretchGrayRange applies a deliberately severe linear curve to a grayscale
// image. A narrow highlight range isolates subtle differences between white
// media and a white scanner background while mapping coloured text and artwork
// to black, making the media outline competitive with interior feature corners.
func stretchGrayRange(src *image.Gray, blackPoint, whitePoint int) *image.Gray {
	if blackPoint >= whitePoint {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	scale := 255.0 / float64(whitePoint-blackPoint)
	for y := 0; y < h; y++ {
		srcRow := src.Pix[y*src.Stride : y*src.Stride+w]
		dstRow := dst.Pix[y*dst.Stride : y*dst.Stride+w]
		for x, value := range srcRow {
			dstRow[x] = clampByte(int(float64(int(value)-blackPoint) * scale))
		}
	}
	return dst
}

// adaptiveHighlightStretch estimates scanner background brightness from the
// outer perimeter. It adds an image-specific silhouette view alongside the
// two established fixed highlight curves; it never replaces them.
func adaptiveHighlightStretch(src *image.Gray) (*image.Gray, int, int) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return src, 0, 255
	}
	if min(w, h) < 4 {
		return src, 0, 255
	}
	thickness := min(w, h) / 50
	if thickness < 2 {
		thickness = 2
	}
	if thickness*2 > min(w, h) {
		thickness = min(w, h) / 2
	}
	var hist [256]int
	for y := 0; y < h; y++ {
		row := src.Pix[y*src.Stride : y*src.Stride+w]
		for x, value := range row {
			if x < thickness || x >= w-thickness || y < thickness || y >= h-thickness {
				hist[value]++
			}
		}
	}
	percentile := func(fraction float64) int {
		total := 0
		for _, count := range hist {
			total += count
		}
		target, seen := int(float64(total)*fraction), 0
		for value, count := range hist {
			seen += count
			if seen >= target {
				return value
			}
		}
		return 255
	}
	p10, background, p90 := percentile(0.10), percentile(0.50), percentile(0.90)
	spread := max(2, p90-p10)
	blackPoint, whitePoint := 0, 255
	if background >= 128 {
		blackPoint = background - max(12, 2*spread)
		whitePoint = background + max(3, spread/2)
	} else {
		blackPoint = background - max(3, spread/2)
		whitePoint = background + max(12, 2*spread)
	}
	blackPoint = clamp(blackPoint, 0, 254)
	whitePoint = clamp(whitePoint, blackPoint+1, 255)
	return stretchGrayRange(src, blackPoint, whitePoint), blackPoint, whitePoint
}

// dedupeCornerPoints removes only near-identical localizations produced by
// different scales. The detector's full minDistance is intentionally not used
// here: scale-space localization can shift a broad corner without making the
// two candidates redundant for snapping purposes.
func dedupeCornerPoints(corners []image.Point, minDistance int) []image.Point {
	var unique []image.Point
	dedupeDistance := float64(minDistance) / 3.0
	minDistSq := dedupeDistance * dedupeDistance
	for _, c := range corners {
		duplicate := false
		for _, u := range unique {
			dx := float64(c.X - u.X)
			dy := float64(c.Y - u.Y)
			if dx*dx+dy*dy < minDistSq {
				duplicate = true
				break
			}
		}
		if !duplicate {
			unique = append(unique, c)
		}
	}
	return unique
}

// refineCoarseCorner replaces a coarse-scale localization with the nearest
// fine highlight candidate within one coarse cell. The coarse pass is good at
// discovering broad silhouettes, but mapping its integer grid coordinate
// directly to the source can be off by tens of pixels on large scans.
func refineCoarseCorner(coarse image.Point, fine []image.Point, radius int) image.Point {
	if radius < 1 || len(fine) == 0 {
		return coarse
	}
	best := coarse
	bestDistSq := int64(radius)*int64(radius) + 1
	for _, candidate := range fine {
		dx := int64(candidate.X - coarse.X)
		dy := int64(candidate.Y - coarse.Y)
		distanceSq := dx*dx + dy*dy
		if distanceSq <= int64(radius)*int64(radius) && distanceSq < bestDistSq {
			best = candidate
			bestDistSq = distanceSq
		}
	}
	return best
}

const (
	rawBroadRefinementRadius = 16
	rawFineRefinementRadius  = 8
)

// refineDetectedCorner applies localization sources in order of authority.
// The severe 240 highlight-pass points already describe the intended subtle
// white-media boundary, so raw texture must not move them (notably on the
// test-pilot regression). The broader 230 pass still benefits from raw edge
// localization on dark media.
func refineDetectedCorner(point image.Point, pass cornerDetectPass, highlight, rawBroad, rawFine []image.Point) image.Point {
	if pass.highlightBlackPoint == 240 {
		return point
	}
	if pass.scale >= 16 {
		point = refineCoarseCorner(point, highlight, pass.scale)
	}
	point = refineCoarseCorner(point, rawBroad, rawBroadRefinementRadius)
	return refineCoarseCorner(point, rawFine, rawFineRefinementRadius)
}

// highlightRecoveryBudget keeps a useful secondary set of candidates from
// the severe highlight curve when the measured scanner perimeter is bright.
// In that situation a clipped white document can have a real outer corner
// that ranks well below ordinary artwork corners, even though the curve has
// isolated it correctly. Dark-background scans keep the established small
// highlight share so this recovery path does not drown out their detail.
func highlightRecoveryBudget(maxCorners int, perimeter perimeterBackground) int {
	if maxCorners < 1 {
		return 1
	}
	if perimeter.dark {
		return maxCorners
	}
	budget := maxCorners * 3 / 4
	if budget < 32 {
		budget = 32
	}
	if budget > maxCorners {
		budget = maxCorners
	}
	return budget
}

// ClickCornerRequest holds the image-space coordinates of a user click.
type ClickCornerRequest struct {
	X      int  `json:"x"`
	Y      int  `json:"y"`
	Custom bool `json:"custom"`
}

// ClickCornerResult is returned after each corner click.
// For clicks 1–3 only SnappedX/SnappedY/Count/Message are set; Preview is empty.
// On the 4th click Done is true, Preview contains the warped image, and
// Width/Height reflect the new image dimensions.
type ClickCornerResult struct {
	Preview       string `json:"preview"`
	Message       string `json:"message"`
	Count         int    `json:"count"`
	Done          bool   `json:"done"`
	SnappedX      int    `json:"snappedX"`
	SnappedY      int    `json:"snappedY"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	DescreenReset bool   `json:"descreenReset,omitempty"`
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
	srcPts := [4]image.Point{sorted[0], sorted[1], sorted[2], sorted[3]}

	var warped *image.NRGBA
	if a.warpFillMode == "clamp" {
		warped = perspectiveTransform(a.currentImage, srcPts, dst, width, height)
	} else {
		var oobMask *image.Alpha
		warped, oobMask = perspectiveTransformWithMask(a.currentImage, srcPts, dst, width, height)
		warped = a.applyWarpFill(warped, oobMask)
	}

	a.warpedImage = warped
	a.cropTop, a.cropBottom, a.cropLeft, a.cropRight = 0, 0, 0, 0
	return warped, width, height, nil
}

// applyWarpFill fills out-of-bounds pixels (marked in oobMask) according to
// the configured warpFillMode.
func (a *App) applyWarpFill(img *image.NRGBA, oobMask *image.Alpha) *image.NRGBA {
	// Fast path: nothing is OOB.
	hasOOB := false
	for _, v := range oobMask.Pix {
		if v > 0 {
			hasOOB = true
			break
		}
	}
	if !hasOOB {
		return img
	}

	if a.warpFillMode == "outpaint" {
		out, _ := PatchMatchFill(context.Background(), img, oobMask, 9, 5)
		if out == nil {
			out = img
		}
		a.logf("applyWarpFill: outpaint OK")
		return out
	}

	// Solid fill: paint OOB pixels with warpFillColor.
	b := img.Bounds()
	fc := a.warpFillColor
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if oobMask.AlphaAt(x, y).A > 0 {
				img.SetNRGBA(x, y, fc)
			}
		}
	}
	return img
}

// CancelCornerDetect cancels any in-flight DetectCorners call. Safe to call
// from any goroutine; no-op when no detection is running.
func (a *App) CancelCornerDetect() {
	a.cornerDetectMu.Lock()
	fn := a.cornerDetectCancel
	a.cornerDetectCancel = nil
	a.cornerDetectMu.Unlock()
	if fn != nil {
		a.logf("CancelCornerDetect: cancelling in-flight detection")
		fn()
	}
}

// DetectCorners detects corners with complementary point and boundary-line
// proposal paths. It returns the clean (unmodified) preview together with the
// detected coordinates so the frontend can render the overlay dots via SVG.
func (a *App) DetectCorners(req CornerDetectRequest) (*ProcessResult, error) {
	a.logf("DetectCorners: maxCorners=%d qualityLevel=%.2f minDistance=%d accentValue=%d",
		req.MaxCorners, req.QualityLevel, req.MinDistance, req.AccentValue)
	if !a.imageLoaded {
		const msg = "DetectCorners: no image loaded"
		a.logf(msg)
		return nil, errors.New(msg)
	}

	// Register a cancellable context so CancelCornerDetect() can abort this call.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.cornerDetectMu.Lock()
	a.cornerDetectCancel = cancel
	a.cornerDetectMu.Unlock()
	defer func() {
		a.cornerDetectMu.Lock()
		a.cornerDetectCancel = nil
		a.cornerDetectMu.Unlock()
	}()

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

	// When downsampling (the common case for large scans), collapse
	// applyAccentAdjustment + toGrayscale + resizeGray into a single
	// parallelized pass over the source pixels to avoid the large
	// intermediate NRGBA clone and full-resolution gray buffer.
	var workGray, rawGray *image.Gray
	if scaleFactor < 1.0 {
		workGray, rawGray = resizeNRGBAToGrayPair(a.currentImage, workW, workH, req.AccentValue)
	} else {
		workGray = toGrayscaleAccent(a.currentImage, req.AccentValue)
		rawGray = toGrayscale(a.currentImage)
	}
	highlightGray240 := stretchGrayRange(rawGray, 240, 255)
	highlightGray230 := stretchGrayRange(rawGray, 230, 255)
	adaptiveHighlightGray, adaptiveBlack, adaptiveWhite := adaptiveHighlightStretch(rawGray)
	a.logf("DetectCorners: adaptive perimeter highlight range %d-%d", adaptiveBlack, adaptiveWhite)
	workColor := a.currentImage
	if workW != imgW || workH != imgH {
		workColor = resizeNRGBA(a.currentImage, workW, workH)
	}
	backgroundSilhouette, perimeter := backgroundDistanceSilhouette(workColor)
	a.logf("DetectCorners: perimeter RGB=(%d,%d,%d) noise=%d silhouette=%d-%d dark=%v",
		perimeter.r, perimeter.g, perimeter.b, perimeter.noise, perimeter.blackPoint, perimeter.whitePoint, perimeter.dark)
	// Keep each source map's line proposals independent. A strong but
	// distracting line in one map can otherwise consume the shared proposal
	// budget before a partial document edge from another map is considered.
	var lineCorners []image.Point
	for _, lineSource := range []*image.Gray{rawGray, highlightGray240, highlightGray230, adaptiveHighlightGray, backgroundSilhouette} {
		proposals, lineErr := lineDerivedCornerProposals(ctx, []*image.Gray{lineSource}, req.MaxCorners, int(float64(req.MinDistance)*scaleFactor))
		if lineErr != nil {
			return nil, lineErr
		}
		lineCorners = append(lineCorners, proposals...)
	}
	lineCorners = dedupeCornerPoints(lineCorners, int(float64(req.MinDistance)*scaleFactor))
	a.logf("DetectCorners: line boundary path proposed %d corners", len(lineCorners))

	// Optionally pre-stretch contrast using percentiles to handle non-white backgrounds,
	// then apply CLAHE to boost local contrast before detection.
	if req.UseStretch {
		low := req.StretchLow
		high := req.StretchHigh
		if low <= 0 || low >= 1 {
			low = 0.01
		}
		if high <= 0 || high > 1 {
			high = 0.99
		}
		stretched := stretchGrayPercentiles(workGray, low, high)
		workGray = applyCLAHE(stretched, 2.0, 8)
	} else {
		// Auto-detect dark images and apply contrast stretch automatically.
		// Dark scans produce low-magnitude gradients that cause the Shi-Tomasi
		// detector to miss corners; CLAHE normalises local contrast without
		// requiring the user to enable the stretch toggle manually.
		const autoStretchThreshold = 100
		wb := workGray.Bounds()
		ww, wh := wb.Dx(), wb.Dy()
		if ww*wh > 0 {
			var lumaSum int64
			for y := 0; y < wh; y++ {
				for x := 0; x < ww; x++ {
					lumaSum += int64(workGray.GrayAt(x, y).Y)
				}
			}
			meanLuma := lumaSum / int64(ww*wh)
			if meanLuma < autoStretchThreshold {
				a.logf("DetectCorners: auto-applying contrast stretch (mean luma %d < %d)", meanLuma, autoStretchThreshold)
				stretched := stretchGrayPercentiles(workGray, 0.01, 0.99)
				workGray = applyCLAHE(stretched, 2.0, 8)
			}
		}
	}

	quality := req.QualityLevel / 100.0
	if quality <= 0 {
		quality = 0.01
	}

	workMinDist := int(float64(req.MinDistance) * scaleFactor)
	if workMinDist < 1 {
		workMinDist = 1
	}

	a.logf("DetectCorners: running multi-scale goodFeaturesToTrack on %dx%d", workW, workH)

	// Build a private set of localizers from the unmodified grayscale. Accent,
	// CLAHE, and large tensor windows improve discovery but can pull the peak
	// away from the physical edge intersection. These points do not consume the
	// user's result budget; they only refine nearby discovered candidates.
	refinementMax := req.MaxCorners
	if refinementMax < 1 {
		refinementMax = 1
	}
	rawBroadRefinementCorners, err := goodFeaturesToTrack(ctx, rawGray, refinementMax, quality, workMinDist, 11)
	if err != nil {
		return nil, err
	}
	rawFineRefinementCorners, err := goodFeaturesToTrack(ctx, rawGray, refinementMax, quality, workMinDist, 5)
	if err != nil {
		return nil, err
	}
	silhouetteMax := req.MaxCorners / 8
	if silhouetteMax < 16 {
		silhouetteMax = 16
	}
	if silhouetteMax > 64 {
		silhouetteMax = 64
	}
	backgroundCorners, err := goodFeaturesToTrack(ctx, backgroundSilhouette, silhouetteMax, quality, workMinDist, 7)
	if err != nil {
		return nil, err
	}
	a.logf("DetectCorners: perimeter-colour silhouette got %d pts", len(backgroundCorners))

	// Multi-scale detection: run detector at several integer scales and
	// accumulate results, then remove duplicates.
	var allCorners []image.Point
	var highlightRefinementCorners []image.Point
	for _, pass := range cornerDetectPasses(req.MaxCorners) {
		if pass.maxCorners == 0 {
			continue
		}
		s := pass.scale
		passGray := workGray
		switch pass.highlightBlackPoint {
		case 240:
			passGray = highlightGray240
		case 230:
			passGray = highlightGray230
		}
		var srcGray *image.Gray
		if s > 1 {
			sw := workW / s
			sh := workH / s
			if sw < 1 {
				sw = 1
			}
			if sh < 1 {
				sh = 1
			}
			srcGray = resizeGray(passGray, sw, sh)
		} else {
			srcGray = passGray
		}

		thisMinDist := workMinDist / s
		if thisMinDist < 1 {
			thisMinDist = 1
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a.logf("DetectCorners: scale=%d highlightBlack=%d src=%dx%d max=%d minDist=%d", s, pass.highlightBlackPoint, srcGray.Bounds().Dx(), srcGray.Bounds().Dy(), pass.maxCorners, thisMinDist)
		blockSize := cornerDetectBlockSize(s)
		detectMax := pass.maxCorners
		if pass.highlightBlackPoint == 240 && detectMax < req.MaxCorners {
			// Keep the full requested set privately for localizing silhouette
			// candidates, while exposing only this pass's allotted share.
			detectMax = req.MaxCorners
		}
		if detectMax < 1 {
			detectMax = 1
		}
		pts, err := goodFeaturesToTrack(ctx, srcGray, detectMax, quality, thisMinDist, blockSize)
		if err != nil {
			return nil, err
		}
		a.logf("DetectCorners: scale=%d got %d pts", s, len(pts))
		if pass.highlightBlackPoint > 0 {
			highlightRefinementCorners = append(highlightRefinementCorners, pts...)
			keep := pass.maxCorners
			if pass.highlightBlackPoint == 240 {
				recoveryBudget := highlightRecoveryBudget(req.MaxCorners, perimeter)
				if recoveryBudget > keep {
					keep = recoveryBudget
				}
			}
			if len(pts) > keep {
				pts = pts[:keep]
			}
		}

		// Scale pts back to working resolution
		for _, p := range pts {
			workPoint := image.Pt(p.X*s, p.Y*s)
			workPoint = refineDetectedCorner(workPoint, pass, highlightRefinementCorners, rawBroadRefinementCorners, rawFineRefinementCorners)
			allCorners = append(allCorners, workPoint)
		}
	}
	if !perimeter.dark {
		allCorners = append(allCorners, backgroundCorners...)
	}
	// Preserve the established point detector's localization when it already
	// found a nearby corner. Line intersections complement missing candidates;
	// they do not displace known-good Shi-Tomasi results.
	allCorners = append(allCorners, lineCorners...)

	a.logf("DetectCorners: %d raw corners from all scales", len(allCorners))

	// Remove only near-identical cross-scale results. A broader radius can
	// discard the better localization of a large, low-contrast corner.
	uniq := dedupeCornerPoints(allCorners, workMinDist)
	if perimeter.dark {
		// Retain both established point localizations and colour-silhouette
		// localizations on dark backgrounds. The two sources may differ by only
		// a few working pixels, but either can be the more accurate side of a
		// blurred physical edge. Suppress exact duplicates only.
		for _, candidate := range backgroundCorners {
			exact := false
			for _, existing := range uniq {
				if candidate == existing {
					exact = true
					break
				}
			}
			if !exact {
				uniq = append(uniq, candidate)
			}
		}
	}

	a.logf("DetectCorners: %d unique corners after dedupe", len(uniq))
	// The highlight recovery pass can use most of the requested budget on a
	// bright, low-contrast scan. Keep the public result bounded by the same
	// MaxCorners contract as the regular detector; the recovery candidates are
	// first in allCorners, so they retain priority over later duplicate-scale
	// detail proposals.
	resultMax := req.MaxCorners
	if resultMax < 1 {
		resultMax = 1
	}
	if len(uniq) > resultMax {
		uniq = uniq[:resultMax]
		a.logf("DetectCorners: capped unique corners to requested maximum %d", resultMax)
	}

	// Map working-space corners to full-resolution image coordinates
	var fullCorners []image.Point
	for _, c := range uniq {
		fullCorners = append(fullCorners, image.Pt(
			int(float64(c.X)/scaleFactor),
			int(float64(c.Y)/scaleFactor),
		))
	}
	a.detectedCorners = fullCorners
	a.logf("DetectCorners: %d corners mapped to full resolution", len(a.detectedCorners))

	// Return the clean (unmodified) image; the frontend renders dots via SVG.
	preview, err := a.imagePreviewURL(a.currentImage)
	if err != nil {
		return nil, err
	}
	return &ProcessResult{
		Preview: preview,
		Width:   imgW,
		Height:  imgH,
		Message: fmt.Sprintf("Detected %d corners", len(a.detectedCorners)),
		Corners: a.detectedCorners,
	}, nil
}

// cornerSnapRadius bounds automatic snapping so a click cannot jump to an
// unrelated artwork feature when no useful proposal exists nearby.
func cornerSnapRadius(w, h int) float64 {
	shortSide := min(w, h)
	radius := float64(shortSide) * 0.03
	if radius < 24 {
		radius = 24
	}
	if radius > 160 {
		radius = 160
	}
	return radius
}

// ClickCorner registers a corner selection click. If a detected corner is
// within the bounded snap radius, the click is snapped to the nearest one;
// otherwise the raw coordinate is used.
// After 4 corners the perspective warp is performed automatically.
// For clicks 1–3 no preview is returned — the frontend renders dots via SVG.
func (a *App) ClickCorner(req ClickCornerRequest) (*ClickCornerResult, error) {
	a.logf("ClickCorner: x=%d y=%d custom=%v", req.X, req.Y, req.Custom)
	if !a.imageLoaded {
		return nil, fmt.Errorf("no image loaded")
	}

	descreenReset := a.descreenResultImage != nil
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
		b := a.currentImage.Bounds()
		snapRadius := cornerSnapRadius(b.Dx(), b.Dy())
		if bestDist <= snapRadius {
			pt = bestPt
			a.logf("ClickCorner: snapped to (%d,%d) dist=%.1f radius=%.1f", pt.X, pt.Y, bestDist, snapRadius)
		} else {
			a.logf("ClickCorner: no detected corner within snap radius (nearest=%.1f radius=%.1f); using raw click", bestDist, snapRadius)
		}
	} else {
		a.logf("ClickCorner: custom placement at (%d,%d)", pt.X, pt.Y)
	}

	a.selectedCorners = append(a.selectedCorners, pt)
	count := len(a.selectedCorners)

	if count < 4 {
		return &ClickCornerResult{
			SnappedX: pt.X,
			SnappedY: pt.Y,
			Message:  fmt.Sprintf("Corner %d of 4 selected", count),
			Count:    count,
			Done:     false,
		}, nil
	}

	// 4 corners selected → perform perspective warp
	a.saveUndo()
	// Patch the freshly-pushed undo entry to remember the 3 in-progress corner
	// clicks so that Undo() can restore them instead of starting from scratch.
	if n := len(a.undoStack); n > 0 && len(a.selectedCorners) >= 4 {
		prev := make([]image.Point, 3)
		copy(prev, a.selectedCorners[:3])
		a.undoStack[n-1].selectedCorners = prev
	}
	_, width, height, warpErr := a.warpFromCorners(a.selectedCorners[:4])
	if warpErr != nil {
		return nil, warpErr
	}
	a.selectedCorners = nil

	preview, err := a.imagePreviewURL(a.warpedImage)
	if err != nil {
		return nil, err
	}

	a.logf("ClickCorner: warp complete %dx%d", width, height)
	return &ClickCornerResult{
		Preview:       preview,
		Message:       fmt.Sprintf("Perspective corrected to %d×%d", width, height),
		Count:         4,
		Done:          true,
		Width:         width,
		Height:        height,
		DescreenReset: descreenReset,
	}, nil
}

// RestoreCornerOverlayRequest is the argument for RestoreCornerOverlay.
// It contains the dot radius for rendering the SVG overlay when switching back to corner mode.
type RestoreCornerOverlayRequest struct {
	DotRadius int `json:"dotRadius"`
}

// RestoreCornerOverlay returns the clean preview and cached detected corners for SVG overlay restoration when switching back to corner mode.
func (a *App) RestoreCornerOverlay(req RestoreCornerOverlayRequest) (*ProcessResult, error) {
	a.logf("RestoreCornerOverlay: %d cached corners", len(a.detectedCorners))
	if len(a.detectedCorners) == 0 {
		return nil, fmt.Errorf("no cached corners")
	}
	preview, err := a.imagePreviewURL(a.currentImage)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview: preview,
		Width:   b.Dx(),
		Height:  b.Dy(),
		Message: fmt.Sprintf("Detected %d corners — click 4 corners", len(a.detectedCorners)),
		Corners: a.detectedCorners,
	}, nil
}

// UndoLastCorner removes the most recently selected corner from the
// in-progress selection without resetting the full state. It is a no-op if
// no corners are currently selected. Returns the number of corners remaining.
func (a *App) UndoLastCorner() int {
	if len(a.selectedCorners) > 0 {
		a.selectedCorners = a.selectedCorners[:len(a.selectedCorners)-1]
	}
	a.logf("UndoLastCorner: %d corners remaining", len(a.selectedCorners))
	return len(a.selectedCorners)
}

// ResetCorners clears any in-progress corner selection. The detected corners
// are preserved and returned so the frontend can restore its SVG overlay.
func (a *App) ResetCorners() (*ProcessResult, error) {
	a.logf("ResetCorners")
	descreenReset := a.descreenResultImage != nil
	a.cancelTouchup()
	a.selectedCorners = nil
	a.warpedImage = nil

	preview, err := a.imagePreviewURL(a.currentImage)
	if err != nil {
		return nil, err
	}
	b := a.currentImage.Bounds()
	return &ProcessResult{
		Preview:       preview,
		Width:         b.Dx(),
		Height:        b.Dy(),
		Message:       fmt.Sprintf("Reset — %d corners detected, click to select", len(a.detectedCorners)),
		Corners:       a.detectedCorners,
		DescreenReset: descreenReset,
	}, nil
}

// SkipCrop sets warpedImage to the current image so that adjustments can be
// saved without performing a perspective crop.
func (a *App) SkipCrop() (*ProcessResult, error) {
	a.logf("SkipCrop")
	if a.currentImage == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	descreenReset := a.descreenResultImage != nil
	a.warpedImage = cloneImage(a.currentImage)
	a.selectedCorners = nil

	b := a.warpedImage.Bounds()
	return &ProcessResult{
		Width:         b.Dx(),
		Height:        b.Dy(),
		Message:       "Crop skipped — image ready to save",
		DescreenReset: descreenReset,
	}, nil
}
