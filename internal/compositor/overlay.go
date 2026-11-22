package compositor

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"

	"atropos/internal/imageops"
	"atropos/internal/raster"
)

// MaskPoint uses coordinates relative to the reference image. This keeps brush
// strokes independent of preview size and lets the final render use full resolution.
type MaskPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type MaskStroke struct {
	Layer   int         `json:"layer"`
	Radius  float64     `json:"radius"`  // fraction of the shorter canvas side
	Feather float64     `json:"feather"` // fraction of the brush radius, 0 = hard edge
	Points  []MaskPoint `json:"points"`
}

// OverlaySession aligns every scan to the first scan and retains the source
// layers so masks can be edited without repeating registration or decoding.
type OverlaySession struct {
	images  []*image.NRGBA
	inverse []stitchH
	strokes []MaskStroke
	width   int
	height  int
}

func (s *OverlaySession) Size() (int, int) { return s.width, s.height }
func (s *OverlaySession) LayerCount() int  { return len(s.images) }
func (s *OverlaySession) StrokeCount() int { return len(s.strokes) }

// AlignOverlay tests all quarter-turns for each additional scan. Matching is
// done on small copies; only the winning orientation is applied at full size.
func AlignOverlay(images []*image.NRGBA) (*OverlaySession, error) {
	if len(images) < 2 {
		return nil, errors.New("compositor: need at least 2 images")
	}
	for i, img := range images {
		if img == nil || img.Bounds().Empty() {
			return nil, fmt.Errorf("compositor: image %d is empty", i+1)
		}
	}
	anchor := images[0]
	aw, ah := anchor.Bounds().Dx(), anchor.Bounds().Dy()
	if aw > compositorMaxOutDim || ah > compositorMaxOutDim {
		return nil, fmt.Errorf("compositor: reference image exceeds %d pixels per side", compositorMaxOutDim)
	}
	anchorFeatures, anchorScale := stitchDetectAndDescribe(anchor)
	if len(anchorFeatures) < compositorMinMatches {
		return nil, errors.New("compositor: too few features in the reference scan")
	}
	s := &OverlaySession{images: make([]*image.NRGBA, len(images)), inverse: make([]stitchH, len(images)), width: aw, height: ah}
	s.images[0] = anchor
	s.inverse[0] = stitchIdentity()
	for i := 1; i < len(images); i++ {
		work, scale := overlayWorkImage(images[i])
		var best stitchH
		bestCount, bestTurn := 0, -1
		for turn := 0; turn < 4; turn++ {
			features, _ := stitchDetectAndDescribe(work)
			matches := stitchMatchFeatures(features, anchorFeatures)
			if len(matches) >= compositorMinMatches {
				src, dst := make([]stitchPt, len(matches)), make([]stitchPt, len(matches))
				for k, m := range matches {
					src[k] = stitchPt{features[m.A].Pt.X / scale, features[m.A].Pt.Y / scale}
					dst[k] = stitchPt{anchorFeatures[m.B].Pt.X / anchorScale, anchorFeatures[m.B].Pt.Y / anchorScale}
				}
				H, inliers, ok := stitchRANSAC(src, dst)
				if ok && len(inliers) > bestCount && overlayPlausible(H, work.Bounds().Dx(), work.Bounds().Dy(), scale, aw, ah) {
					best, bestCount, bestTurn = H, len(inliers), turn
				}
			}
			work = imageops.Rotate90(work, 1)
		}
		if bestTurn < 0 {
			return nil, fmt.Errorf("compositor: could not align scan %d to the first scan", i+1)
		}
		oriented := images[i]
		switch bestTurn {
		case 1:
			oriented = imageops.Rotate90(oriented, 1)
		case 2:
			oriented = imageops.Rotate90(imageops.Rotate90(oriented, 1), 1)
		case 3:
			oriented = imageops.Rotate90(oriented, 0)
		}
		s.images[i] = oriented
		s.inverse[i] = stitchInv(best)
	}
	return s, nil
}

func overlayWorkImage(img *image.NRGBA) (*image.NRGBA, float64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if max(w, h) <= compositorWorkDim {
		return img, 1
	}
	scale := float64(compositorWorkDim) / float64(max(w, h))
	return raster.ResizeNRGBA(img, max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))), scale
}

// Reject registrations which barely touch the reference or have implausible
// scale. This is especially useful on repeated lines of booklet text.
func overlayPlausible(H stitchH, workW, workH int, scale float64, anchorW, anchorH int) bool {
	w, h := float64(workW)/scale, float64(workH)/scale
	p := [4]stitchPt{stitchApply(H, stitchPt{0, 0}), stitchApply(H, stitchPt{w, 0}), stitchApply(H, stitchPt{0, h}), stitchApply(H, stitchPt{w, h})}
	for _, v := range p {
		if math.IsNaN(v.X) || math.IsNaN(v.Y) || math.IsInf(v.X, 0) || math.IsInf(v.Y, 0) {
			return false
		}
	}
	top := math.Hypot(p[1].X-p[0].X, p[1].Y-p[0].Y) / w
	left := math.Hypot(p[2].X-p[0].X, p[2].Y-p[0].Y) / h
	if top < .65 || top > 1.5 || left < .65 || left > 1.5 {
		return false
	}
	minX, maxX := math.Min(math.Min(p[0].X, p[1].X), math.Min(p[2].X, p[3].X)), math.Max(math.Max(p[0].X, p[1].X), math.Max(p[2].X, p[3].X))
	minY, maxY := math.Min(math.Min(p[0].Y, p[1].Y), math.Min(p[2].Y, p[3].Y)), math.Max(math.Max(p[0].Y, p[1].Y), math.Max(p[2].Y, p[3].Y))
	overlapW := math.Max(0, math.Min(maxX, float64(anchorW))-math.Max(minX, 0))
	overlapH := math.Max(0, math.Min(maxY, float64(anchorH))-math.Max(minY, 0))
	return overlapW*overlapH >= .3*float64(anchorW*anchorH)
}

func (s *OverlaySession) AddStroke(stroke MaskStroke) error {
	if stroke.Layer <= 0 || stroke.Layer >= len(s.images) {
		return errors.New("select a layer above the reference scan")
	}
	if math.IsNaN(stroke.Radius) || math.IsInf(stroke.Radius, 0) || stroke.Radius <= 0 || stroke.Radius > .25 || len(stroke.Points) == 0 || len(stroke.Points) > 10000 {
		return errors.New("invalid mask brush stroke")
	}
	if math.IsNaN(stroke.Feather) || math.IsInf(stroke.Feather, 0) || stroke.Feather < 0 || stroke.Feather > 1 {
		return errors.New("mask feather must be between 0 and 1")
	}
	for _, p := range stroke.Points {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) || p.X < 0 || p.X > 1 || p.Y < 0 || p.Y > 1 {
			return errors.New("mask point outside the output")
		}
	}
	s.strokes = append(s.strokes, stroke)
	return nil
}

func (s *OverlaySession) UndoStroke() bool {
	if len(s.strokes) == 0 {
		return false
	}
	s.strokes = s.strokes[:len(s.strokes)-1]
	return true
}

func (s *OverlaySession) ClearStrokes() { s.strokes = nil }

// Render composes layers in list order, last on top. maxDim=0 renders at the
// reference scan's original resolution. Upper layers are visible only where
// their masks have been painted; untouched areas remain from the first scan.
func (s *OverlaySession) Render(maxDim int) *image.NRGBA {
	w, h := s.width, s.height
	if maxDim > 0 && max(w, h) > maxDim {
		f := float64(maxDim) / float64(max(w, h))
		w, h = max(1, int(math.Round(float64(w)*f))), max(1, int(math.Round(float64(h)*f)))
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	masks := s.rasterMasks(w, h)
	var wg sync.WaitGroup
	rows := make(chan int, h)
	for y := 0; y < h; y++ {
		rows <- y
	}
	close(rows)
	workers := min(runtime.NumCPU(), h)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for y := range rows {
				refY := (float64(y)+.5)*float64(s.height)/float64(h) - .5
				for x := 0; x < w; x++ {
					refX := (float64(x)+.5)*float64(s.width)/float64(w) - .5
					var r, g, b float64
					for layer, img := range s.images {
						alpha := 1.0
						if layer > 0 {
							alpha = 0
							if mask := masks[layer]; mask != nil {
								alpha = float64(mask[y*w+x]) / 255
							}
						}
						if alpha == 0 {
							continue
						}
						sp := stitchApply(s.inverse[layer], stitchPt{refX, refY})
						if sp.X < 0 || sp.Y < 0 || sp.X >= float64(img.Bounds().Dx()) || sp.Y >= float64(img.Bounds().Dy()) {
							continue
						}
						cr, cg, cb := stitchBilinear(img, sp.X, sp.Y)
						r = cr*alpha + r*(1-alpha)
						g = cg*alpha + g*(1-alpha)
						b = cb*alpha + b*(1-alpha)
					}
					out.SetNRGBA(x, y, color.NRGBA{R: raster.ClampByte(int(r + .5)), G: raster.ClampByte(int(g + .5)), B: raster.ClampByte(int(b + .5)), A: 255})
				}
			}
		}()
	}
	wg.Wait()
	return out
}

func (s *OverlaySession) rasterMasks(w, h int) [][]uint8 {
	masks := make([][]uint8, len(s.images))
	for _, stroke := range s.strokes {
		if masks[stroke.Layer] == nil {
			masks[stroke.Layer] = make([]uint8, w*h)
		}
		mask := masks[stroke.Layer]
		radius := stroke.Radius * float64(min(w, h))
		paint := func(a, b MaskPoint) {
			ax, ay := a.X*float64(w), a.Y*float64(h)
			bx, by := b.X*float64(w), b.Y*float64(h)
			x0 := max(0, int(math.Floor(math.Min(ax, bx)-radius-1)))
			x1 := min(w-1, int(math.Ceil(math.Max(ax, bx)+radius+1)))
			y0 := max(0, int(math.Floor(math.Min(ay, by)-radius-1)))
			y1 := min(h-1, int(math.Ceil(math.Max(ay, by)+radius+1)))
			dx, dy := bx-ax, by-ay
			den := dx*dx + dy*dy
			for y := y0; y <= y1; y++ {
				for x := x0; x <= x1; x++ {
					t := 0.0
					if den > 0 {
						t = math.Max(0, math.Min(1, ((float64(x)+.5-ax)*dx+(float64(y)+.5-ay)*dy)/den))
					}
					d := math.Hypot(float64(x)+.5-(ax+t*dx), float64(y)+.5-(ay+t*dy))
					var v uint8
					if stroke.Feather == 0 {
						if d <= radius {
							v = 255
						}
					} else {
						v = uint8(math.Round(255 * math.Max(0, math.Min(1, (radius-d)/(radius*stroke.Feather)))))
					}
					if v > mask[y*w+x] {
						mask[y*w+x] = v
					}
				}
			}
		}
		paint(stroke.Points[0], stroke.Points[0])
		for i := 1; i < len(stroke.Points); i++ {
			paint(stroke.Points[i-1], stroke.Points[i])
		}
	}
	return masks
}
