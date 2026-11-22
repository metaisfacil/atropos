package main

import (
	"fmt"
	"image"
	"math"

	"atropos/internal/descreen"
	"atropos/internal/dust"
	"atropos/internal/imageops"
	"atropos/internal/raster"
)

// Post-disc pixel edits (Dust removal, Descreen, Touch-up, selection-scoped
// Levels / Auto contrast) are applied to discBaseImage, the unmasked source
// that redrawDisc crops from, rather than to the rendered result. The disc
// therefore stays a live crop over an edited scan: shift, rotation, radius
// and feather keep working after edits, and the edits move with the disc art.
//
// discBaseImage is treated as immutable (preview assets and undo history
// reference it), so every edit installs an edited copy.

// discScale is the output scale applied as the last step of every disc
// redraw (from Resize). The zero value means unscaled.
type discScale struct{ x, y float64 }

func (s discScale) factors() (float64, float64) {
	if s.x <= 0 || s.y <= 0 {
		return 1, 1
	}
	return s.x, s.y
}

func (s discScale) identity() bool {
	x, y := s.factors()
	return x == 1 && y == 1
}

// discEditing reports that a disc crop is live, so pixel edits belong in
// discBaseImage.
func (a *App) discEditing() bool {
	return a.discRadius > 0 && a.discBaseImage != nil && a.warpedImage != nil
}

// discSourceMap converts between result-image coordinates (after the output
// scale) and discBaseImage coordinates, mirroring redrawDisc: crop → rotate
// about the crop centre → inset crop → output scale. Coordinates are
// continuous (pixel i spans [i, i+1)).
type discSourceMap struct {
	srcPivotX, srcPivotY float64 // rotation pivot in discBaseImage coords
	resPivotX, resPivotY float64 // same pivot in unscaled result coords
	cos, sin             float64
	scaleX, scaleY       float64
}

func (a *App) discSourceMapping() discSourceMap {
	req := a.discCropRect(a.discBaseImage)
	// imageops.Rotate pivots on pixel index (w/2, h/2); in continuous
	// pixel-edge coordinates that is half a pixel further on.
	cpx, cpy := float64(req.Dx())/2+0.5, float64(req.Dy())/2+0.5
	keep := a.discCropInsets.rect(image.Rect(0, 0, req.Dx(), req.Dy()))
	rad := a.rotationAngle * math.Pi / 180
	sx, sy := a.discScale.factors()
	return discSourceMap{
		srcPivotX: float64(req.Min.X) + cpx, srcPivotY: float64(req.Min.Y) + cpy,
		resPivotX: cpx - float64(keep.Min.X), resPivotY: cpy - float64(keep.Min.Y),
		cos: math.Cos(rad), sin: math.Sin(rad),
		scaleX: sx, scaleY: sy,
	}
}

// toSource maps a result-image point to discBaseImage coords.
func (m discSourceMap) toSource(px, py float64) (float64, float64) {
	rx, ry := px/m.scaleX-m.resPivotX, py/m.scaleY-m.resPivotY
	return m.cos*rx + m.sin*ry + m.srcPivotX, -m.sin*rx + m.cos*ry + m.srcPivotY
}

// toResult maps a discBaseImage point to result-image coords.
func (m discSourceMap) toResult(sx, sy float64) (float64, float64) {
	dx, dy := sx-m.srcPivotX, sy-m.srcPivotY
	return (m.cos*dx - m.sin*dy + m.resPivotX) * m.scaleX, (m.sin*dx + m.cos*dy + m.resPivotY) * m.scaleY
}

// meanScale is the average output scale, for converting lengths (brush
// sizes) between result and source.
func (m discSourceMap) meanScale() float64 { return (m.scaleX + m.scaleY) / 2 }

// discEditRegion is the part of discBaseImage a whole-image edit processes:
// the disc's crop square plus a margin of one disc radius on every side, so
// later shifts within that margin still show edited pixels.
func (a *App) discEditRegion() image.Rectangle {
	pad := 2 * a.discRadius
	c := a.discCenter
	return image.Rect(c.X-pad, c.Y-pad, c.X+pad, c.Y+pad).Intersect(a.discBaseImage.Bounds())
}

// discSelectionInSource maps a result-space selection rectangle to the
// discBaseImage pixels it covers: their bounding box, and a test for whether
// a pixel's centre lies inside the (rotated) selection.
func (a *App) discSelectionInSource(sel image.Rectangle) (image.Rectangle, func(x, y int) bool) {
	m := a.discSourceMapping()
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range [4][2]float64{
		{float64(sel.Min.X), float64(sel.Min.Y)}, {float64(sel.Max.X), float64(sel.Min.Y)},
		{float64(sel.Min.X), float64(sel.Max.Y)}, {float64(sel.Max.X), float64(sel.Max.Y)},
	} {
		sx, sy := m.toSource(p[0], p[1])
		minX, maxX = math.Min(minX, sx), math.Max(maxX, sx)
		minY, maxY = math.Min(minY, sy), math.Max(maxY, sy)
	}
	bbox := image.Rect(int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY))).
		Intersect(a.discBaseImage.Bounds())
	x0, y0, x1, y1 := float64(sel.Min.X), float64(sel.Min.Y), float64(sel.Max.X), float64(sel.Max.Y)
	inside := func(x, y int) bool {
		px, py := m.toResult(float64(x)+0.5, float64(y)+0.5)
		return px >= x0 && px < x1 && py >= y0 && py < y1
	}
	return bbox, inside
}

// withDiscSourceRegion returns a copy of base whose region is replaced by
// processed (a zero-origin image the size of region), restricted to pixels
// for which inside reports true when inside is non-nil.
func withDiscSourceRegion(base, processed *image.NRGBA, region image.Rectangle, inside func(x, y int) bool) *image.NRGBA {
	out := raster.CloneNRGBA(base)
	region = region.Intersect(out.Bounds())
	w := region.Dx()
	for y := region.Min.Y; y < region.Max.Y; y++ {
		so := processed.PixOffset(processed.Rect.Min.X, processed.Rect.Min.Y+y-region.Min.Y)
		do := out.PixOffset(region.Min.X, y)
		if inside == nil {
			copy(out.Pix[do:do+4*w], processed.Pix[so:so+4*w])
			continue
		}
		for x := 0; x < w; x++ {
			if inside(region.Min.X+x, y) {
				copy(out.Pix[do+4*x:do+4*x+4], processed.Pix[so+4*x:so+4*x+4])
			}
		}
	}
	return out
}

// editDiscSource processes region of the current disc source with process,
// optionally restricted to a result-space selection, and returns the edited
// copy of discBaseImage (not yet installed).
func (a *App) editDiscSource(base *image.NRGBA, selection *image.Rectangle, process func(*image.NRGBA) *image.NRGBA) *image.NRGBA {
	region, inside := a.discEditRegion(), (func(x, y int) bool)(nil)
	if selection != nil {
		region, inside = a.discSelectionInSource(*selection)
	}
	if region.Empty() {
		return base
	}
	return withDiscSourceRegion(base, process(raster.CropNRGBA(base, region)), region, inside)
}

// installDiscSource makes img the disc source and re-renders the disc.
func (a *App) installDiscSource(img *image.NRGBA) (*ProcessResult, error) {
	a.discBaseImage = img
	a.refreshDiscWorkingCrop()
	return a.redrawDisc()
}

// discSelectionPixels gathers the source pixels inside a result-space
// selection into a single-row image (for statistics such as auto contrast).
func (a *App) discSelectionPixels(base *image.NRGBA, sel image.Rectangle) *image.NRGBA {
	region, inside := a.discSelectionInSource(sel)
	pix := make([]uint8, 0, 4*region.Dx()*region.Dy())
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			if inside(x, y) {
				o := base.PixOffset(x, y)
				pix = append(pix, base.Pix[o:o+4]...)
			}
		}
	}
	if len(pix) == 0 {
		return raster.CropNRGBA(base, region)
	}
	n := len(pix) / 4
	return &image.NRGBA{Pix: pix, Stride: 4 * n, Rect: image.Rect(0, 0, n, 1)}
}

// discPreScaleSize is the size of the disc render before the output scale.
func (a *App) discPreScaleSize() image.Point {
	req := a.discCropRect(a.discBaseImage)
	return a.discCropInsets.rect(image.Rect(0, 0, req.Dx(), req.Dy())).Size()
}

// applyDiscScale resizes a finished disc render by the output scale.
func (a *App) applyDiscScale(img *image.NRGBA) *image.NRGBA {
	if img == nil || a.discScale.identity() {
		return img
	}
	sx, sy := a.discScale.factors()
	b := img.Bounds()
	w := max(1, int(math.Round(float64(b.Dx())*sx)))
	h := max(1, int(math.Round(float64(b.Dy())*sy)))
	return raster.ResizeNRGBA(img, w, h)
}

// discScaledLength converts an output-space length (e.g. a crop amount) to
// unscaled render pixels along one axis.
func (a *App) discScaledLength(n int, vertical bool) int {
	sx, sy := a.discScale.factors()
	s := sx
	if vertical {
		s = sy
	}
	return int(math.Round(float64(n) / s))
}

// discDustRemoval runs dust removal on the disc source (the edit region, or
// the source pixels under a result-space selection).
func (a *App) discDustRemoval(level string, dpi float64, selRect image.Rectangle, selKey adjustmentSelectionKey) (*ProcessResult, error) {
	base := a.discBaseImage
	var sel *image.Rectangle
	if selKey.Active {
		sel = &selRect
	}
	var repaired int
	var usedDPI float64
	var procErr error
	edited := a.editDiscSource(base, sel, func(region *image.NRGBA) *image.NRGBA {
		out, n, d, err := dust.Apply(region, level, dpi)
		if err != nil {
			procErr = err
			return region
		}
		repaired, usedDPI = n, d
		return out
	})
	if procErr != nil {
		return nil, procErr
	}
	if repaired == 0 {
		preview, err := a.imagePreviewURL(a.warpedImage)
		if err != nil {
			return nil, err
		}
		b := a.warpedImage.Bounds()
		return &ProcessResult{Preview: preview, Message: fmt.Sprintf("No dust detected at %s strength", level), Width: b.Dx(), Height: b.Dy()}, nil
	}
	descreenReset := a.descreenResultImage != nil
	a.saveUndo()
	result, err := a.installDiscSource(edited)
	if err != nil {
		return nil, err
	}
	result.Message = fmt.Sprintf("Dust removal applied (%s, %.0f DPI, %d pixel repairs)", level, usedDPI, repaired)
	result.DescreenReset = result.DescreenReset || descreenReset
	result.Changed = true
	return result, nil
}

// discDescreen filters the disc source. Like the result-space Descreen,
// repeated calls with new parameters re-filter the same session base instead
// of stacking.
func (a *App) discDescreen(req DescreenRequest, selRect image.Rectangle, selKey adjustmentSelectionKey) (*ProcessResult, error) {
	src := a.discBaseImage
	if a.descreenBaseImage == nil || src != a.descreenResultImage || a.descreenSelection != selKey {
		a.saveUndo()
		a.descreenBaseImage = src // the disc source is immutable; no copy needed
		a.descreenSelection = selKey
		a.logf("Descreen: captured disc source as descreenBaseImage")
	}
	base, session := a.descreenBaseImage, a.descreenSelection
	var sel *image.Rectangle
	if selKey.Active {
		sel = &selRect
	}
	edited := a.editDiscSource(base, sel, func(region *image.NRGBA) *image.NRGBA {
		if req.Fast {
			return descreen.ApplyLuminance(region, req.Thresh, req.Radius, req.Middle, req.Highlight, a.logf)
		}
		return descreen.Apply(region, req.Thresh, req.Radius, req.Middle, req.Highlight, a.logf)
	})
	result, err := a.installDiscSource(edited)
	if err != nil {
		return nil, err
	}
	// redrawDisc ends descreen sessions for every other operation; this one
	// continues with the same base.
	a.descreenBaseImage, a.descreenResultImage, a.descreenSelection = base, edited, session
	result.DescreenReset = false
	result.Message = fmt.Sprintf("Descreen applied (thresh=%d, radius=%d, middle=%d, highlight=%d, fast=%t)", req.Thresh, req.Radius, req.Middle, req.Highlight, req.Fast)
	return result, nil
}

// discSelectionLevels applies a slider levels stretch to the source pixels
// under a result-space selection. Slider ticks re-apply against the session
// snapshot (levelsBaseImage, here a disc source) and do not record undo.
func (a *App) discSelectionLevels(req SetLevelsRequest, selRect image.Rectangle, selKey adjustmentSelectionKey, descreenReset bool) (*ProcessResult, error) {
	a.redoStack = nil
	a.descreenBaseImage, a.descreenResultImage, a.descreenSelection = nil, nil, adjustmentSelectionKey{}
	if a.levelsBaseImage == nil || a.levelsSelection != selKey {
		a.levelsBaseImage, a.levelsSelection = a.discBaseImage, selKey
	}
	base := a.levelsBaseImage
	edited := a.editDiscSource(base, &selRect, func(region *image.NRGBA) *image.NRGBA {
		return imageops.ApplyLevels(region, req.Black, req.White)
	})
	result, err := a.installDiscSource(edited)
	if err != nil {
		return nil, err
	}
	a.levelsBaseImage, a.levelsSelection = base, selKey
	result.DescreenReset = descreenReset
	return result, nil
}

// discSelectionAutoContrast stretches the source pixels under a result-space
// selection to their own luminance range.
func (a *App) discSelectionAutoContrast(selRect image.Rectangle, selKey adjustmentSelectionKey, descreenReset bool) (*ProcessResult, error) {
	base := a.discBaseImage
	if a.levelsBaseImage != nil && a.levelsSelection == selKey {
		base = a.levelsBaseImage // don't stack on a partial slider drag
	}
	bp, wp := imageops.AutoContrastPoints(a.discSelectionPixels(base, selRect))
	a.logf("AutoContrast (disc selection): blackPt=%d whitePt=%d", bp, wp)
	a.saveUndo()
	edited := a.editDiscSource(base, &selRect, func(region *image.NRGBA) *image.NRGBA {
		return imageops.ApplyLevels(region, bp, wp)
	})
	result, err := a.installDiscSource(edited)
	if err != nil {
		return nil, err
	}
	// Slider sessions can still revert to the pre-adjustment source.
	a.levelsBaseImage, a.levelsSelection = base, selKey
	result.Message = fmt.Sprintf("Auto Contrast applied (black=%d, white=%d)", bp, wp)
	result.Black, result.White = bp, wp
	result.DescreenReset = descreenReset
	return result, nil
}

// discStrokesToSource maps result-space touch-up brush points (pixel-index
// coordinates, as buildStrokeMask expects) and brush size into the disc
// source. Rotation preserves lengths; the output scale does not.
func (a *App) discStrokesToSource(points []TouchUpPoint, brushSize float64) ([]TouchUpPoint, float64) {
	m := a.discSourceMapping()
	out := make([]TouchUpPoint, len(points))
	for i, p := range points {
		sx, sy := m.toSource(p.X+0.5, p.Y+0.5)
		out[i] = TouchUpPoint{X: sx - 0.5, Y: sy - 0.5}
	}
	return out, brushSize / m.meanScale()
}

// clipAlphaToDiscSelection keeps only the source-space mask pixels that lie
// inside a result-space selection.
func (a *App) clipAlphaToDiscSelection(mask *image.Alpha, sel image.Rectangle) (*image.Alpha, error) {
	_, inside := a.discSelectionInSource(sel)
	b := mask.Bounds()
	clipped := image.NewAlpha(b)
	any := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if v := mask.AlphaAt(x, y); v.A != 0 && inside(x, y) {
				clipped.SetAlpha(x, y, v)
				any = true
			}
		}
	}
	if !any {
		return nil, fmt.Errorf("touch-up stroke is outside the adjustment selection")
	}
	return clipped, nil
}

// discMaskToSource resamples a result-space touch-up mask (the legacy
// full-image mask path) into disc-source coordinates.
func (a *App) discMaskToSource(mask *image.Alpha) (*image.Alpha, error) {
	b := mask.Bounds()
	used := image.Rectangle{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if mask.AlphaAt(x, y).A != 0 {
				used = used.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if used.Empty() {
		return nil, fmt.Errorf("touch-up mask is empty")
	}
	region, _ := a.discSelectionInSource(used)
	m := a.discSourceMapping()
	out := image.NewAlpha(region)
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			px, py := m.toResult(float64(x)+0.5, float64(y)+0.5)
			if p := image.Pt(int(math.Floor(px)), int(math.Floor(py))); p.In(b) {
				out.SetAlpha(x, y, mask.AlphaAt(p.X, p.Y))
			}
		}
	}
	return out, nil
}
