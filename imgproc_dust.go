package main

import (
	"fmt"
	"image"
	"math"
)

// dustProfile contains the calibration parameters. The three limit rows are
// Low, Medium, and High; each row contains the dense, sparse, and elongated
// connected-component area caps.
type dustProfile struct {
	edgeFloor                           float64
	compactMin, compactSplit, fillSplit float64
	limits                              [3][3]float64
	baselineDPI                         float64
}

var reflectiveDustProfile = dustProfile{
	edgeFloor:    0.09,
	compactMin:   0.150,
	compactSplit: 0.270,
	fillSplit:    0.520,
	limits: [3][3]float64{
		{160, 280, 400},
		{240, 420, 600},
		{320, 600, 900},
	},
	baselineDPI: 400,
}

type dustSpan struct {
	y, x0, x1 int
}

func dustLevelIndex(level string) (int, error) {
	switch level {
	case "low":
		return 0, nil
	case "medium":
		return 1, nil
	case "high":
		return 2, nil
	default:
		return 0, fmt.Errorf("invalid dust removal level %q", level)
	}
}

// applyDustRemoval reproduces the software dust-removal path: luminance Sobel
// seeds, square dilation, component shape and area filtering, enclosed-hole
// filling, and a local median repair.
func applyDustRemoval(src *image.NRGBA, level string, dpi float64) (*image.NRGBA, int, float64, error) {
	levelIndex, err := dustLevelIndex(level)
	if err != nil {
		return nil, 0, 0, err
	}
	if src == nil || src.Bounds().Empty() {
		return nil, 0, 0, fmt.Errorf("no image loaded")
	}

	// The dust removal process receives resolution from the scanner descriptor.
	// Files without DPI metadata use a conservative document-scan default.
	if dpi <= 0 || math.IsNaN(dpi) || math.IsInf(dpi, 0) {
		dpi = 300
	}

	profile := reflectiveDustProfile
	detectImage := src
	scale := 1.0
	if dpi > profile.baselineDPI {
		scale = profile.baselineDPI / dpi
		b := src.Bounds()
		newW := maxInt(1, int(math.Round(float64(b.Dx())*scale)))
		newH := maxInt(1, int(math.Round(float64(b.Dy())*scale)))
		detectImage = resizeNRGBA(src, newW, newH)
	}

	db := detectImage.Bounds()
	w, h := db.Dx(), db.Dy()
	seed := sobelSeedMask(detectImage, profile.edgeFloor)
	if countBinaryMask(seed) == 0 {
		return src, 0, dpi, nil
	}

	window := 5
	if dpi <= 100 {
		window = 3
	}
	dilated, scratch := dilateSquareBinary(seed, w, h, window)

	limits := profile.limits[levelIndex]
	if dpi < profile.baselineDPI {
		areaScale := dpi / profile.baselineDPI
		for i := range limits {
			limits[i] *= areaScale
		}
	}

	// dilated is consumed as the connected components are visited. Reuse the
	// horizontal dilation scratch as the accepted-component mask.
	clear(scratch)
	filterDustComponents(dilated, scratch, w, h, profile, limits)
	for i := range scratch {
		scratch[i] &= seed[i]
	}
	if countBinaryMask(scratch) == 0 {
		return src, 0, dpi, nil
	}

	fillBinaryMaskHoles(scratch, dilated, w, h)
	mask := scratch
	if scale != 1 {
		sb := src.Bounds()
		mask = resizeBinaryMaskNearest(mask, w, h, sb.Dx(), sb.Dy())
	}

	out, repaired := medianRepair(src, mask)
	if repaired == 0 {
		return src, 0, dpi, nil
	}
	return out, repaired, dpi, nil
}

func sobelSeedMask(src *image.NRGBA, edgeFloor float64) []uint8 {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	luma := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		srcRow := y * src.Stride
		row := y * w
		for x := 0; x < w; x++ {
			i := srcRow + x*4
			luma[row+x] = uint8((299*int(src.Pix[i]) + 587*int(src.Pix[i+1]) + 114*int(src.Pix[i+2])) / 1000)
		}
	}

	if w < 3 || h < 3 {
		return make([]uint8, w*h)
	}

	var maxMagnitudeSquared int64
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			gx, gy := sobelAt(luma, w, x, y)
			magnitudeSquared := int64(gx*gx + gy*gy)
			if magnitudeSquared > maxMagnitudeSquared {
				maxMagnitudeSquared = magnitudeSquared
			}
		}
	}
	mask := make([]uint8, w*h)
	if maxMagnitudeSquared == 0 {
		return mask
	}

	thresholdSquared := edgeFloor * edgeFloor * float64(maxMagnitudeSquared)
	for y := 1; y < h-1; y++ {
		row := y * w
		for x := 1; x < w-1; x++ {
			gx, gy := sobelAt(luma, w, x, y)
			if float64(int64(gx*gx+gy*gy)) >= thresholdSquared {
				mask[row+x] = 1
			}
		}
	}
	return mask
}

func sobelAt(luma []uint8, width, x, y int) (int, int) {
	top := (y-1)*width + x
	middle := y*width + x
	bottom := (y+1)*width + x
	gx := -int(luma[top-1]) + int(luma[top+1]) - 2*int(luma[middle-1]) + 2*int(luma[middle+1]) - int(luma[bottom-1]) + int(luma[bottom+1])
	gy := -int(luma[top-1]) - 2*int(luma[top]) - int(luma[top+1]) + int(luma[bottom-1]) + 2*int(luma[bottom]) + int(luma[bottom+1])
	return gx, gy
}

func dilateSquareBinary(src []uint8, width, height, window int) ([]uint8, []uint8) {
	radius := window / 2
	horizontal := make([]uint8, len(src))
	for y := 0; y < height; y++ {
		row := y * width
		count := 0
		for x := 0; x <= radius && x < width; x++ {
			count += int(src[row+x])
		}
		for x := 0; x < width; x++ {
			if count > 0 {
				horizontal[row+x] = 1
			}
			removeX := x - radius
			addX := x + radius + 1
			if removeX >= 0 {
				count -= int(src[row+removeX])
			}
			if addX < width {
				count += int(src[row+addX])
			}
		}
	}

	dst := make([]uint8, len(src))
	for x := 0; x < width; x++ {
		count := 0
		for y := 0; y <= radius && y < height; y++ {
			count += int(horizontal[y*width+x])
		}
		for y := 0; y < height; y++ {
			if count > 0 {
				dst[y*width+x] = 1
			}
			removeY := y - radius
			addY := y + radius + 1
			if removeY >= 0 {
				count -= int(horizontal[removeY*width+x])
			}
			if addY < height {
				count += int(horizontal[addY*width+x])
			}
		}
	}
	return dst, horizontal
}

func filterDustComponents(mask, accepted []uint8, width, height int, profile dustProfile, limits [3]float64) {
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if mask[y*width+x] == 0 {
				continue
			}
			first := takeDustRun(mask, width, x, y)
			stack := []dustSpan{first}
			component := []dustSpan{first}
			area := first.x1 - first.x0 + 1
			minX, maxX, minY, maxY := first.x0, first.x1, y, y

			for len(stack) > 0 {
				span := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				for _, adjacentY := range [...]int{span.y - 1, span.y + 1} {
					if adjacentY < 0 || adjacentY >= height {
						continue
					}
					searchX := maxInt(0, span.x0-1)
					searchEnd := minInt(width-1, span.x1+1)
					for searchX <= searchEnd {
						if mask[adjacentY*width+searchX] == 0 {
							searchX++
							continue
						}
						run := takeDustRun(mask, width, searchX, adjacentY)
						stack = append(stack, run)
						component = append(component, run)
						area += run.x1 - run.x0 + 1
						minX = minInt(minX, run.x0)
						maxX = maxInt(maxX, run.x1)
						minY = minInt(minY, run.y)
						maxY = maxInt(maxY, run.y)
						searchX = run.x1 + 1
					}
				}
			}

			boxW, boxH := maxX-minX+1, maxY-minY+1
			maxDimension := maxInt(boxW, boxH)
			compactness := float64(area) / float64(maxDimension*maxDimension)
			if compactness <= profile.compactMin {
				continue
			}
			limit := limits[0]
			if compactness <= profile.compactSplit {
				limit = limits[2]
			} else if float64(area)/float64(boxW*boxH) < profile.fillSplit {
				limit = limits[1]
			}
			if float64(area) > limit {
				continue
			}
			for _, span := range component {
				row := span.y * width
				for markX := span.x0; markX <= span.x1; markX++ {
					accepted[row+markX] = 1
				}
			}
		}
	}
}

func takeDustRun(mask []uint8, width, x, y int) dustSpan {
	row := y * width
	x0, x1 := x, x
	for x0 > 0 && mask[row+x0-1] != 0 {
		x0--
	}
	for x1+1 < width && mask[row+x1+1] != 0 {
		x1++
	}
	clear(mask[row+x0 : row+x1+1])
	return dustSpan{y: y, x0: x0, x1: x1}
}

// fillBinaryMaskHoles uses 4-connected background against the detector's
// 8-connected foreground. visited is caller-provided scratch storage.
func fillBinaryMaskHoles(mask, visited []uint8, width, height int) {
	clear(visited)
	if width == 0 || height == 0 {
		return
	}
	stack := make([]dustSpan, 0, height*2)
	seed := func(x, y int) {
		index := y*width + x
		if mask[index] != 0 || visited[index] != 0 {
			return
		}
		run := takeBackgroundRun(mask, visited, width, x, y)
		stack = append(stack, run)
		for len(stack) > 0 {
			span := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, adjacentY := range [...]int{span.y - 1, span.y + 1} {
				if adjacentY < 0 || adjacentY >= height {
					continue
				}
				searchX := span.x0
				for searchX <= span.x1 {
					index := adjacentY*width + searchX
					if mask[index] != 0 || visited[index] != 0 {
						searchX++
						continue
					}
					next := takeBackgroundRun(mask, visited, width, searchX, adjacentY)
					stack = append(stack, next)
					searchX = next.x1 + 1
				}
			}
		}
	}

	for x := 0; x < width; x++ {
		seed(x, 0)
		if height > 1 {
			seed(x, height-1)
		}
	}
	for y := 1; y < height-1; y++ {
		seed(0, y)
		if width > 1 {
			seed(width-1, y)
		}
	}
	for i := range mask {
		if mask[i] == 0 && visited[i] == 0 {
			mask[i] = 1
		}
	}
}

func takeBackgroundRun(mask, visited []uint8, width, x, y int) dustSpan {
	row := y * width
	x0, x1 := x, x
	for x0 > 0 && mask[row+x0-1] == 0 && visited[row+x0-1] == 0 {
		x0--
	}
	for x1+1 < width && mask[row+x1+1] == 0 && visited[row+x1+1] == 0 {
		x1++
	}
	for markX := x0; markX <= x1; markX++ {
		visited[row+markX] = 1
	}
	return dustSpan{y: y, x0: x0, x1: x1}
}

func resizeBinaryMaskNearest(src []uint8, srcW, srcH, dstW, dstH int) []uint8 {
	dst := make([]uint8, dstW*dstH)
	for y := 0; y < dstH; y++ {
		srcY := y * srcH / dstH
		for x := 0; x < dstW; x++ {
			dst[y*dstW+x] = src[srcY*srcW+x*srcW/dstW]
		}
	}
	return dst
}

var repairOffsets = [...]image.Point{
	{-1, -1}, {0, -1}, {-1, 0}, {1, -1}, {0, 0},
	{-1, 1}, {1, 0}, {0, 1}, {1, 1},
}

func medianRepair(src *image.NRGBA, mask []uint8) (*image.NRGBA, int) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := cloneImage(src)
	repaired := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if mask[y*w+x] == 0 {
				continue
			}
			for _, offset := range repairOffsets {
				targetX, targetY := x+offset.X, y+offset.Y
				if targetX < 0 || targetX >= w || targetY < 0 || targetY >= h {
					continue
				}
				var red, green, blue [25]uint8
				n := 0
				for sampleY := maxInt(0, targetY-2); sampleY <= minInt(h-1, targetY+2); sampleY++ {
					for sampleX := maxInt(0, targetX-2); sampleX <= minInt(w-1, targetX+2); sampleX++ {
						if mask[sampleY*w+sampleX] != 0 {
							continue
						}
						i := sampleY*out.Stride + sampleX*4
						red[n], green[n], blue[n] = out.Pix[i], out.Pix[i+1], out.Pix[i+2]
						n++
					}
				}
				if n == 0 {
					continue
				}
				r := medianByte(red[:n])
				g := medianByte(green[:n])
				bl := medianByte(blue[:n])
				i := targetY*out.Stride + targetX*4
				if out.Pix[i] == r && out.Pix[i+1] == g && out.Pix[i+2] == bl {
					continue
				}
				out.Pix[i], out.Pix[i+1], out.Pix[i+2] = r, g, bl
				if mask[targetY*w+targetX] != 0 {
					mask[targetY*w+targetX] = 0
				}
				repaired++
			}
		}
	}
	return out, repaired
}

func medianByte(values []uint8) uint8 {
	for i := 1; i < len(values); i++ {
		value := values[i]
		j := i - 1
		for ; j >= 0 && values[j] > value; j-- {
			values[j+1] = values[j]
		}
		values[j+1] = value
	}
	middle := len(values) / 2
	if len(values)%2 == 0 {
		return uint8((int(values[middle-1]) + int(values[middle])) / 2)
	}
	return values[middle]
}

func countBinaryMask(mask []uint8) int {
	count := 0
	for _, value := range mask {
		if value != 0 {
			count++
		}
	}
	return count
}
