package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"sort"
)

// boundaryLine is a finite, fitted image boundary in normal form. The finite
// endpoints matter: intersections of unrelated infinite artwork lines are not
// useful corner proposals.
type boundaryLine struct {
	theta   float64
	rho     float64
	startX  float64
	startY  float64
	endX    float64
	endY    float64
	length  float64
	support float64
	score   float64
}

type houghPeak struct {
	thetaBin int
	rhoBin   int
	votes    float64
}

type scoredPoint struct {
	point image.Point
	score float64
}

const boundaryThetaBins = 180

type perimeterBackground struct {
	r          uint8
	g          uint8
	b          uint8
	noise      int
	dark       bool
	blackPoint int
	whitePoint int
}

// backgroundDistanceSilhouette models the scanner surface from a narrow image
// perimeter, then maps RGB distance from that surface into a near-binary
// silhouette. Unlike luminance stretching, this retains strong separation
// between similarly bright colours and suppresses texture within a grey or
// coloured scanner background.
func backgroundDistanceSilhouette(src *image.NRGBA) (*image.Gray, perimeterBackground) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewGray(image.Rect(0, 0, w, h))
	if w == 0 || h == 0 {
		return dst, perimeterBackground{}
	}
	shortSide := min(w, h)
	thickness := shortSide / 50
	if thickness < 2 {
		thickness = min(2, shortSide)
	}
	var redHist, greenHist, blueHist [256]int
	samples := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= thickness && x < w-thickness && y >= thickness && y < h-thickness {
				continue
			}
			pixel := src.NRGBAAt(x+b.Min.X, y+b.Min.Y)
			redHist[pixel.R]++
			greenHist[pixel.G]++
			blueHist[pixel.B]++
			samples++
		}
	}
	medianChannel := func(hist [256]int) uint8 {
		target, seen := (samples+1)/2, 0
		for value, count := range hist {
			seen += count
			if seen >= target {
				return uint8(value)
			}
		}
		return 0
	}
	background := perimeterBackground{
		r: medianChannel(redHist),
		g: medianChannel(greenHist),
		b: medianChannel(blueHist),
	}
	backgroundLuma := (299*int(background.r) + 587*int(background.g) + 114*int(background.b)) / 1000
	background.dark = backgroundLuma < 160

	// Measure the upper tail of normal perimeter variation so woven scanner
	// mats and other textured surfaces map to black rather than becoming edges.
	var distanceHist [443]int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= thickness && x < w-thickness && y >= thickness && y < h-thickness {
				continue
			}
			pixel := src.NRGBAAt(x+b.Min.X, y+b.Min.Y)
			distanceHist[rgbDistance(pixel, background)]++
		}
	}
	noiseTarget, seen := samples*95/100, 0
	for value, count := range distanceHist {
		seen += count
		if seen >= noiseTarget {
			background.noise = value
			break
		}
	}
	background.blackPoint = min(441, background.noise+3)
	background.whitePoint = background.blackPoint + max(24, background.noise*2)
	if background.whitePoint > 442 {
		background.whitePoint = 442
	}
	rangeWidth := max(1, background.whitePoint-background.blackPoint)
	for y := 0; y < h; y++ {
		dstRow := dst.Pix[y*dst.Stride : y*dst.Stride+w]
		for x := 0; x < w; x++ {
			pixel := src.NRGBAAt(x+b.Min.X, y+b.Min.Y)
			distance := rgbDistance(pixel, background)
			dstRow[x] = clampByte((distance - background.blackPoint) * 255 / rangeWidth)
		}
	}
	return dst, background
}

func rgbDistance(pixel color.NRGBA, background perimeterBackground) int {
	dr := int(pixel.R) - int(background.r)
	dg := int(pixel.G) - int(background.g)
	db := int(pixel.B) - int(background.b)
	return int(math.Round(math.Sqrt(float64(dr*dr + dg*dg + db*db))))
}

// lineDerivedCornerProposals implements a complementary edges-first proposal
// path. It deliberately does not replace Shi-Tomasi: long edge fragments are
// assembled into finite lines, and intersections near the ends of two strong
// lines become extra candidates for snapping.
func lineDerivedCornerProposals(ctx context.Context, grayImages []*image.Gray, maxCorners, minDistance int) ([]image.Point, error) {
	if maxCorners < 1 || len(grayImages) == 0 {
		return nil, nil
	}

	var lines []boundaryLine
	for _, gray := range grayImages {
		if gray == nil {
			continue
		}
		found, err := detectBoundaryLines(ctx, gray)
		if err != nil {
			return nil, err
		}
		for _, candidate := range found {
			lines = mergeBoundaryLine(lines, candidate)
		}
	}

	if len(lines) < 2 {
		return nil, nil
	}
	w, h := grayImages[0].Bounds().Dx(), grayImages[0].Bounds().Dy()
	shortSide := float64(min(w, h))
	endpointPad := math.Max(12, shortSide*0.035)
	margin := shortSide * 0.025
	var candidates []scoredPoint

	for i := 0; i < len(lines); i++ {
		for j := i + 1; j < len(lines); j++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			a, b := lines[i], lines[j]
			// Adjacent document sides should not be close to parallel. A 35 degree
			// floor still permits strong perspective distortion.
			cross := math.Abs(math.Sin(a.theta - b.theta))
			if cross < math.Sin(35*math.Pi/180) {
				continue
			}
			x, y, ok := intersectNormalLines(a, b)
			if !ok || x < -margin || y < -margin || x > float64(w-1)+margin || y > float64(h-1)+margin {
				continue
			}
			if !pointNearFiniteLine(x, y, a, endpointPad) || !pointNearFiniteLine(x, y, b, endpointPad) {
				continue
			}
			candidates = append(candidates, scoredPoint{
				point: clampedImagePoint(x, y, w, h),
				score: math.Sqrt(a.score*b.score) * cross,
			})
		}
	}
	// Intersections that participate in a geometrically valid quadrilateral
	// receive stronger proposals. Unpaired intersections remain available for
	// partial/occluded documents, which is important in manual corner mode.
	candidates = append(candidates, quadrilateralCornerProposals(lines, w, h, endpointPad)...)

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	proposalLimit := maxCorners / 8
	if proposalLimit < 16 {
		proposalLimit = 16
	}
	if proposalLimit > 64 {
		proposalLimit = 64
	}
	dedupeRadius := math.Max(5, float64(minDistance)/3)
	dedupeRadiusSq := dedupeRadius * dedupeRadius
	result := make([]image.Point, 0, min(proposalLimit, len(candidates)))
	for _, candidate := range candidates {
		duplicate := false
		for _, existing := range result {
			dx := float64(candidate.point.X - existing.X)
			dy := float64(candidate.point.Y - existing.Y)
			if dx*dx+dy*dy < dedupeRadiusSq {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, candidate.point)
			if len(result) == proposalLimit {
				break
			}
		}
	}
	return result, nil
}

type boundaryLinePair struct {
	first  int
	second int
}

type floatPoint struct {
	x float64
	y float64
}

func quadrilateralCornerProposals(lines []boundaryLine, w, h int, endpointPad float64) []scoredPoint {
	if len(lines) < 4 {
		return nil
	}
	// Quad enumeration grows quickly, so use the strongest refined lines. This
	// is proposal prioritization only; every line still participates in the
	// ordinary intersection path above.
	strong := append([]boundaryLine(nil), lines...)
	sort.Slice(strong, func(i, j int) bool { return strong[i].score > strong[j].score })
	if len(strong) > 28 {
		strong = strong[:28]
	}
	shortSide := float64(min(w, h))
	var pairs []boundaryLinePair
	for i := 0; i < len(strong); i++ {
		for j := i + 1; j < len(strong); j++ {
			if angleDistance(strong[i].theta, strong[j].theta) > 12*math.Pi/180 {
				continue
			}
			mx := (strong[i].startX + strong[i].endX) / 2
			my := (strong[i].startY + strong[i].endY) / 2
			separation := math.Abs(mx*math.Cos(strong[j].theta) + my*math.Sin(strong[j].theta) - strong[j].rho)
			if separation >= shortSide*0.12 {
				pairs = append(pairs, boundaryLinePair{i, j})
			}
		}
	}

	margin := shortSide * 0.025
	imageArea := float64(w * h)
	var result []scoredPoint
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			pa, pb := pairs[i], pairs[j]
			if pa.first == pb.first || pa.first == pb.second || pa.second == pb.first || pa.second == pb.second {
				continue
			}
			if math.Abs(math.Sin(strong[pa.first].theta-strong[pb.first].theta)) < math.Sin(45*math.Pi/180) {
				continue
			}
			combinations := [4][2]int{
				{pa.first, pb.first}, {pa.first, pb.second},
				{pa.second, pb.first}, {pa.second, pb.second},
			}
			var quad [4]floatPoint
			valid := true
			for k, combination := range combinations {
				a, b := strong[combination[0]], strong[combination[1]]
				x, y, ok := intersectNormalLines(a, b)
				if !ok || x < -margin || y < -margin || x > float64(w-1)+margin || y > float64(h-1)+margin ||
					!pointNearFiniteLine(x, y, a, endpointPad) || !pointNearFiniteLine(x, y, b, endpointPad) {
					valid = false
					break
				}
				quad[k] = floatPoint{x, y}
			}
			if !valid {
				continue
			}
			ordered := orderFloatQuad(quad)
			area, convex, anglesOK := validateFloatQuad(ordered)
			if !convex || !anglesOK || area < imageArea*0.025 {
				continue
			}
			lineScore := (strong[pa.first].score + strong[pa.second].score + strong[pb.first].score + strong[pb.second].score) / 4
			quadScore := lineScore * (2 + math.Min(1, area/imageArea))
			for _, point := range ordered {
				result = append(result, scoredPoint{clampedImagePoint(point.x, point.y, w, h), quadScore})
			}
		}
	}
	return result
}

func clampedImagePoint(x, y float64, w, h int) image.Point {
	return image.Pt(
		clamp(int(math.Round(x)), 0, w-1),
		clamp(int(math.Round(y)), 0, h-1),
	)
}

func orderFloatQuad(points [4]floatPoint) [4]floatPoint {
	cx, cy := 0.0, 0.0
	for _, point := range points {
		cx += point.x
		cy += point.y
	}
	cx, cy = cx/4, cy/4
	ordered := points
	sort.Slice(ordered[:], func(i, j int) bool {
		return math.Atan2(ordered[i].y-cy, ordered[i].x-cx) < math.Atan2(ordered[j].y-cy, ordered[j].x-cx)
	})
	return ordered
}

func validateFloatQuad(points [4]floatPoint) (area float64, convex, anglesOK bool) {
	signedArea := 0.0
	crossSign := 0.0
	anglesOK = true
	for i := 0; i < 4; i++ {
		current := points[i]
		next := points[(i+1)%4]
		prev := points[(i+3)%4]
		signedArea += current.x*next.y - next.x*current.y
		cross := (current.x-prev.x)*(next.y-current.y) - (current.y-prev.y)*(next.x-current.x)
		if math.Abs(cross) < 1e-6 || (crossSign != 0 && cross*crossSign < 0) {
			return 0, false, false
		}
		crossSign = cross
		ax, ay := prev.x-current.x, prev.y-current.y
		bx, by := next.x-current.x, next.y-current.y
		denominator := math.Hypot(ax, ay) * math.Hypot(bx, by)
		if denominator == 0 {
			return 0, false, false
		}
		cosine := clampFloat((ax*bx+ay*by)/denominator, -1, 1)
		angle := math.Acos(cosine) * 180 / math.Pi
		if angle < 40 || angle > 140 {
			anglesOK = false
		}
	}
	return math.Abs(signedArea) / 2, true, anglesOK
}

func clampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func detectBoundaryLines(ctx context.Context, src *image.Gray) ([]boundaryLine, error) {
	gray := gaussianBlurGray(src)
	b := gray.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 32 || h < 32 {
		return nil, nil
	}

	gx := make([]int16, w*h)
	gy := make([]int16, w*h)
	mag := make([]uint16, w*h)
	var histogram [1444]int
	for y := 1; y < h-1; y++ {
		if y&63 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		rowX := gx[y*w : (y+1)*w]
		rowY := gy[y*w : (y+1)*w]
		cornerSobelRow(
			gray.Pix[(y-1)*gray.Stride:],
			gray.Pix[y*gray.Stride:],
			gray.Pix[(y+1)*gray.Stride:],
			rowX[1:w-1], rowY[1:w-1],
		)
		for x := 1; x < w-1; x++ {
			m := int(math.Hypot(float64(rowX[x]), float64(rowY[x])))
			if m > 1443 {
				m = 1443
			}
			mag[y*w+x] = uint16(m)
			if m > 0 {
				histogram[m]++
			}
		}
	}

	threshold := gradientPercentile(histogram[:], 0.62)
	if threshold < 8 {
		threshold = 8
	}
	// A highly textured insert must not raise the global threshold enough to
	// erase its quieter outer boundary.
	if threshold > 56 {
		threshold = 56
	}

	diag := int(math.Ceil(math.Hypot(float64(w), float64(h))))
	rhoBins := diag*2 + 1
	acc := make([]float32, boundaryThetaBins*rhoBins)
	cosTable := make([]float64, boundaryThetaBins)
	sinTable := make([]float64, boundaryThetaBins)
	for t := 0; t < boundaryThetaBins; t++ {
		theta := float64(t) * math.Pi / boundaryThetaBins
		cosTable[t], sinTable[t] = math.Cos(theta), math.Sin(theta)
	}

	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			idx := y*w + x
			m := int(mag[idx])
			if m < threshold {
				continue
			}
			theta := math.Atan2(float64(gy[idx]), float64(gx[idx]))
			if theta < 0 {
				theta += math.Pi
			}
			if theta >= math.Pi {
				theta -= math.Pi
			}
			center := int(math.Round(theta * boundaryThetaBins / math.Pi))
			weight := math.Min(3, math.Sqrt(float64(m)/float64(threshold)))
			for dt := -1; dt <= 1; dt++ {
				t := (center + dt + boundaryThetaBins) % boundaryThetaBins
				rho := int(math.Round(float64(x)*cosTable[t]+float64(y)*sinTable[t])) + diag
				if rho >= 0 && rho < rhoBins {
					acc[t*rhoBins+rho] += float32(weight)
				}
			}
		}
	}

	peakFloor := math.Max(20, float64(min(w, h))*0.025)
	peaks := make([]houghPeak, 0, 256)
	for t := 0; t < boundaryThetaBins; t++ {
		for r := 1; r < rhoBins-1; r++ {
			v := float64(acc[t*rhoBins+r])
			if v < peakFloor || v < float64(acc[t*rhoBins+r-1]) || v < float64(acc[t*rhoBins+r+1]) {
				continue
			}
			peaks = append(peaks, houghPeak{thetaBin: t, rhoBin: r - diag, votes: v})
		}
	}
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].votes > peaks[j].votes })
	if len(peaks) > 160 {
		peaks = peaks[:160]
	}

	var lines []boundaryLine
	for _, peak := range peaks {
		theta := float64(peak.thetaBin) * math.Pi / boundaryThetaBins
		duplicate := false
		for _, accepted := range lines {
			if angleDistance(theta, accepted.theta) < 3*math.Pi/180 && math.Abs(float64(peak.rhoBin)-accepted.rho) < 9 {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		line, ok := fitFiniteBoundaryLine(theta, float64(peak.rhoBin), peak.votes, gx, gy, mag, w, h, threshold)
		if ok && !isImageBorderLine(line, w, h) {
			lines = append(lines, line)
			if len(lines) == 48 {
				break
			}
		}
	}
	return lines, nil
}

func gradientPercentile(hist []int, fraction float64) int {
	total := 0
	for _, count := range hist {
		total += count
	}
	if total == 0 {
		return 0
	}
	target := int(float64(total) * fraction)
	seen := 0
	for value, count := range hist {
		seen += count
		if seen >= target {
			return value
		}
	}
	return len(hist) - 1
}

func angleDistance(a, b float64) float64 {
	d := math.Abs(a - b)
	if d > math.Pi/2 {
		d = math.Pi - d
	}
	return math.Abs(d)
}

func fitFiniteBoundaryLine(theta, rho, votes float64, gx, gy []int16, mag []uint16, w, h, threshold int) (boundaryLine, bool) {
	nx, ny := math.Cos(theta), math.Sin(theta)
	dx, dy := -ny, nx
	baseX, baseY := rho*nx, rho*ny
	tMin, tMax := math.MaxFloat64, -math.MaxFloat64
	for _, corner := range [4][2]float64{{0, 0}, {float64(w - 1), 0}, {0, float64(h - 1)}, {float64(w - 1), float64(h - 1)}} {
		t := (corner[0]-baseX)*dx + (corner[1]-baseY)*dy
		if t < tMin {
			tMin = t
		}
		if t > tMax {
			tMax = t
		}
	}

	const step = 2.0
	// Printed matter and artwork can interrupt an otherwise coherent boundary
	// for dozens of pixels. Joining those collinear fragments is the useful
	// part of line assembly; the support-ratio check below still rejects a line
	// made mostly of unrelated fragments.
	maxGap := math.Max(16, float64(min(w, h))*0.055)
	lowThreshold := math.Max(6, float64(threshold)*0.55)
	bestStart, bestEnd, bestCount := 0.0, 0.0, 0
	runStart, lastSupport, runCount := 0.0, 0.0, 0
	inRun := false
	for t := tMin; t <= tMax; t += step {
		supported := false
		for offset := -3; offset <= 3 && !supported; offset++ {
			x := int(math.Round(baseX + t*dx + float64(offset)*nx))
			y := int(math.Round(baseY + t*dy + float64(offset)*ny))
			if x < 1 || y < 1 || x >= w-1 || y >= h-1 {
				continue
			}
			idx := y*w + x
			m := float64(mag[idx])
			if m < lowThreshold {
				continue
			}
			alignment := math.Abs(float64(gx[idx])*nx+float64(gy[idx])*ny) / m
			supported = alignment >= math.Cos(28*math.Pi/180)
		}
		if supported {
			if !inRun || t-lastSupport > maxGap {
				runStart, runCount, inRun = t, 0, true
			}
			lastSupport = t
			runCount++
			if lastSupport-runStart > bestEnd-bestStart {
				bestStart, bestEnd, bestCount = runStart, lastSupport, runCount
			}
		}
	}
	length := bestEnd - bestStart
	if length < float64(min(w, h))*0.18 || bestCount < 8 {
		return boundaryLine{}, false
	}
	support := float64(bestCount) / (length/step + 1)
	if support < 0.12 {
		return boundaryLine{}, false
	}
	return boundaryLine{
		theta: theta, rho: rho,
		startX: baseX + bestStart*dx, startY: baseY + bestStart*dy,
		endX: baseX + bestEnd*dx, endY: baseY + bestEnd*dy,
		length: length, support: support,
		score: length * (0.35 + support) * math.Log1p(votes),
	}, true
}

func isImageBorderLine(line boundaryLine, w, h int) bool {
	const border = 7.0
	return (line.startX <= border && line.endX <= border) ||
		(line.startX >= float64(w-1)-border && line.endX >= float64(w-1)-border) ||
		(line.startY <= border && line.endY <= border) ||
		(line.startY >= float64(h-1)-border && line.endY >= float64(h-1)-border)
}

func mergeBoundaryLine(lines []boundaryLine, candidate boundaryLine) []boundaryLine {
	for i, existing := range lines {
		if angleDistance(candidate.theta, existing.theta) < 2.5*math.Pi/180 && math.Abs(candidate.rho-existing.rho) < 8 {
			if candidate.score > existing.score {
				lines[i] = candidate
			}
			return lines
		}
	}
	return append(lines, candidate)
}

func intersectNormalLines(a, b boundaryLine) (float64, float64, bool) {
	a1, b1 := math.Cos(a.theta), math.Sin(a.theta)
	a2, b2 := math.Cos(b.theta), math.Sin(b.theta)
	det := a1*b2 - a2*b1
	if math.Abs(det) < 1e-9 {
		return 0, 0, false
	}
	x := (a.rho*b2 - b.rho*b1) / det
	y := (a1*b.rho - a2*a.rho) / det
	return x, y, true
}

func pointNearFiniteLine(x, y float64, line boundaryLine, pad float64) bool {
	dx, dy := line.endX-line.startX, line.endY-line.startY
	lengthSq := dx*dx + dy*dy
	if lengthSq == 0 {
		return false
	}
	t := ((x-line.startX)*dx + (y-line.startY)*dy) / lengthSq
	return t >= -pad/line.length && t <= 1+pad/line.length
}
