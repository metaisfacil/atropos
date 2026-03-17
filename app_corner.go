package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// CornerDetectRequest contains parameters for the Shi-Tomasi corner detector.
type CornerDetectRequest struct {
	MaxCorners   int     `json:"maxCorners"`
	QualityLevel float64 `json:"qualityLevel"`
	MinDistance  int     `json:"minDistance"`
	AccentValue  int     `json:"accentValue"`
	DotRadius    int     `json:"dotRadius"`
	UseStretch   bool    `json:"useStretch"`
	StretchLow   float64 `json:"stretchLow"`
	StretchHigh  float64 `json:"stretchHigh"`
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
		out := PatchMatchFill(img, oobMask, 9, 5)
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

	// Optionally pre-stretch contrast using percentiles to handle non-white backgrounds
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
		enhanced := applyCLAHE(stretched, 2.0, 8)

		// replace enhanced variable in outer scope
		_ = enhanced
		// Now set enhanced variable that the rest of the function expects by shadowing
		workGray = stretched
	}

	// If not using stretch, or after stretch we continue with CLAHE on workGray

	quality := req.QualityLevel / 100.0
	if quality <= 0 {
		quality = 0.01
	}

	workMinDist := int(float64(req.MinDistance) * scaleFactor)
	if workMinDist < 1 {
		workMinDist = 1
	}

	a.logf("DetectCorners: running multi-scale goodFeaturesToTrack on %dx%d", workW, workH)

	// Multi-scale detection: run detector at several integer scales and
	// accumulate results, then remove duplicates.
	scales := []int{1, 2, 4}
	var allCorners []image.Point
	perScale := req.MaxCorners / len(scales)
	if perScale < 1 {
		perScale = 1
	}

	for _, s := range scales {
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
			srcGray = resizeGray(workGray, sw, sh)
		} else {
			srcGray = workGray
		}

		thisMinDist := workMinDist / s
		if thisMinDist < 1 {
			thisMinDist = 1
		}

		a.logf("DetectCorners: scale=%d src=%dx%d max=%d minDist=%d", s, srcGray.Bounds().Dx(), srcGray.Bounds().Dy(), perScale, thisMinDist)
		pts := goodFeaturesToTrack(srcGray, perScale, quality, thisMinDist, 7)
		a.logf("DetectCorners: scale=%d got %d pts", s, len(pts))

		// Scale pts back to working resolution
		for _, p := range pts {
			allCorners = append(allCorners, image.Pt(p.X*s, p.Y*s))
		}
	}

	a.logf("DetectCorners: %d raw corners from all scales", len(allCorners))

	// Remove duplicates by enforcing a minimum squared distance
	var uniq []image.Point
	minDistSq := float64(workMinDist*workMinDist) / 4.0
	for _, c := range allCorners {
		dup := false
		for _, u := range uniq {
			dx := float64(c.X - u.X)
			dy := float64(c.Y - u.Y)
			if dx*dx+dy*dy < minDistSq {
				dup = true
				break
			}
		}
		if !dup {
			uniq = append(uniq, c)
		}
	}

	a.logf("DetectCorners: %d unique corners after dedupe", len(uniq))

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
	a.warpedImage = nil

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

// RestoreCornerOverlay redraws the cached corner detection overlay without
// re-running detection. Returns an error if no corners are cached.
func (a *App) RestoreCornerOverlay(req struct {
	DotRadius int `json:"dotRadius"`
}) (*ProcessResult, error) {
	if len(a.detectedCorners) == 0 {
		return nil, fmt.Errorf("no cached corners")
	}
	dr := req.DotRadius
	if dr < 2 {
		dr = a.cornerDotRadius
	}
	if dr < 2 {
		dr = 2
	}
	a.cornerDotRadius = dr
	result, err := a.drawCornerOverlay(dr)
	if err != nil {
		return nil, err
	}
	result.Message = fmt.Sprintf("Detected %d corners — click 4 corners", len(a.detectedCorners))
	return result, nil
}
