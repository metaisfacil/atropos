package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"
	"time"

	"atropos/internal/raster"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

const (
	viewportOverscanFraction = 0.25
	viewportSourceQuantum    = 64
	viewportMaxDim           = 2048
	viewportMaxPixels        = 4 * 1024 * 1024
	viewportCacheCap         = 12
	viewportCheckerSize      = 16
)

type rectI struct {
	X1, Y1, X2, Y2 int
}

type lineI struct {
	X1, Y1, X2, Y2 int
}

type optimisticCropState struct {
	Source     string
	SourceDims image.Point
	TargetDims image.Point
	Direction  string
	Amount     int
}

type discLiveTransform struct {
	Dx    float32
	Dy    float32
	Angle float32
}

type touchupCursorState struct {
	X        float64
	Y        float64
	Resizing bool
}

type viewportVisualState struct {
	Mode                      string
	RealImageDims             image.Point
	Dragging                  bool
	DragStart                 *f32.Point
	DragCurrent               *f32.Point
	UseTouchupTool            bool
	TouchupStrokes            []TouchUpPoint
	BrushSize                 float64
	TouchupCursor             *touchupCursorState
	UseStraightEdgeTool       bool
	DiscActive                bool
	DiscLiveActive            bool
	DiscLiveTransform         discLiveTransform
	DiscCenter                image.Point
	DiscRadius                int
	DiscCenterCutout          bool
	DiscCutoutPercent         int
	DetectedCornerPts         []image.Point
	SelectedCornerPts         []image.Point
	DotRadius                 int
	ShowCornerSequence        bool
	CornerLabels              []string
	NormalRect                *rectI
	AdjustmentSelectionActive bool
	AdjustmentRect            *rectI
	Lines                     []lineI
	LineDragKind              string
	LineStartImg              *image.Point
	CtrlDragActive            bool
	ShiftDragActive           bool
	OverlayStroke             []f32.Point // normalized [0..1] points for Compositor overlay
	OverlayBrushPct           float64
}

type viewportLayoutInfo struct {
	ViewportW     int
	ViewportH     int
	StageLeft     float64
	StageTop      float64
	StageX        float64
	StageY        float64
	StageWidth    float64
	StageHeight   float64
	ContentWidth  float64
	ContentHeight float64
	StageDims     image.Point
}

type viewportRasterEntry struct {
	key    string
	source string
	dims   image.Point
	rect   image.Rectangle
	width  int
	height int
	imgOp  paint.ImageOp
}

type rasterBuildSpec struct {
	key      string
	source   string
	dims     image.Point
	rect     image.Rectangle
	dstW     int
	dstH     int
	srcImage *image.NRGBA
}

type scrollThumbDrag struct {
	startPos    float32
	startScroll float64
}

type viewportCallbacks struct {
	FixedDisplayWidth float64
	HideScrollbars    bool
	DisableWheelZoom  bool
	Cursor            pointer.Cursor
	GetCursor         func(gtx layout.Context) pointer.Cursor
	GetVisual         func() viewportVisualState
	OnPointer         func(gtx layout.Context, info viewportLayoutInfo, pe pointer.Event)
	OnWheelFeather    func(deltaY float32)
	OnPresented       func(source string, dims image.Point)
	Invalidate        func()
	DebugLog          func(format string, args ...any)
}

type cornerOverlayCache struct {
	valid       bool
	ptsLen      int
	ptsHash     uint64
	dims        image.Point
	dotRadius   int
	stageWidth  float64
	stageHeight float64
	coverRect   image.Rectangle

	bufs  [2]*image.RGBA
	slot  int
	imgOp paint.ImageOp

	mask       []uint8
	maskSide   int
	maskHalf   int
	maskRadius float64

	rebuilds int
}

type viewportState struct {
	Zoom              float64
	FitWidth          float64
	ScrollX           float64
	ScrollY           float64
	PresentedSource   string
	PresentedDims     image.Point
	LastViewportSize  image.Point
	LastLayout        viewportLayoutInfo
	LastHoverWindowPt *f32.Point

	cache         []viewportRasterEntry
	activeEntry   *viewportRasterEntry
	cornerOverlay *cornerOverlayCache

	mu          sync.Mutex
	building    bool
	pendingSpec *rasterBuildSpec
	sessionGen  uint64

	canvasTag        uint8
	hScrollTag       uint8
	vScrollTag       uint8
	hScrollDrag      *scrollThumbDrag
	vScrollDrag      *scrollThumbDrag
	hoverHBar        bool
	hoverVBar        bool
	lastDiscGuideLog time.Time
	lastDiscGuideRad float32
}

func newViewportState() *viewportState {
	return &viewportState{Zoom: 1}
}

func fitWidthFor(vpW, vpH, imgW, imgH int) float64 {
	if vpW <= 0 || vpH <= 0 || imgW <= 0 || imgH <= 0 {
		return 0
	}
	aw := math.Max(1, float64(vpW))
	ah := math.Max(1, float64(vpH))
	return math.Min(aw, ah*(float64(imgW)/float64(imgH)))
}

func maxUsefulZoom(naturalWidth int, fitWidth float64) float64 {
	if naturalWidth <= 0 || fitWidth <= 0 {
		return 5
	}
	return math.Min(40, math.Max(5, (float64(naturalWidth)/fitWidth)*2))
}

func clampF64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (v *viewportState) ResetCache() {
	v.mu.Lock()
	v.sessionGen++
	v.pendingSpec = nil
	v.mu.Unlock()
	v.cache = nil
	v.activeEntry = nil
	v.cornerOverlay = nil
	v.PresentedSource = ""
	v.PresentedDims = image.Point{}
}

func (v *viewportState) SetZoomKeepCenter(nextZoom float64) {
	if v.LastLayout.ViewportW <= 0 || v.LastLayout.ViewportH <= 0 || v.FitWidth <= 0 {
		v.Zoom = nextZoom
		return
	}
	cx := float64(v.LastLayout.ViewportW) / 2
	cy := float64(v.LastLayout.ViewportH) / 2
	v.applyZoomAnchored(nextZoom, cx, cy)
}

func (v *viewportState) applyZoomAnchored(nextZoom, vpCursorX, vpCursorY float64) {
	info := v.LastLayout
	if info.StageWidth <= 0 || info.StageHeight <= 0 || v.FitWidth <= 0 {
		v.Zoom = nextZoom
		return
	}
	limit := maxUsefulZoom(info.StageDims.X, v.FitWidth)
	clamped := clampF64(nextZoom, 0.1, limit)
	if math.Abs(clamped-v.Zoom) < 1e-6 {
		return
	}
	u := clampF64((vpCursorX-info.StageX)/info.StageWidth, 0, 1)
	vFrac := clampF64((vpCursorY-info.StageY)/info.StageHeight, 0, 1)

	v.Zoom = clamped
	nextStageW := math.Max(1, v.FitWidth*clamped)
	nextStageH := nextStageW * float64(info.StageDims.Y) / float64(info.StageDims.X)
	nextContentW := math.Max(float64(info.ViewportW), nextStageW)
	nextContentH := math.Max(float64(info.ViewportH), nextStageH)
	nextStageLeft := (nextContentW - nextStageW) / 2
	nextStageTop := (nextContentH - nextStageH) / 2

	maxSX := math.Max(0, nextContentW-float64(info.ViewportW))
	maxSY := math.Max(0, nextContentH-float64(info.ViewportH))
	v.ScrollX = clampF64(nextStageLeft+u*nextStageW-vpCursorX, 0, maxSX)
	v.ScrollY = clampF64(nextStageTop+vFrac*nextStageH-vpCursorY, 0, maxSY)
}

func (v *viewportState) ApplyWheelZoom(deltaY float32, vpCursorX, vpCursorY float64) {
	if deltaY == 0 {
		return
	}
	factor := 1.1
	if deltaY > 0 {
		factor = 0.9
	}
	limit := maxUsefulZoom(v.LastLayout.StageDims.X, v.FitWidth)
	next := clampF64(v.Zoom*factor, 0.1, limit)
	v.applyZoomAnchored(next, vpCursorX, vpCursorY)
}

func (v *viewportState) computeLayout(vpW, vpH int, imageDims image.Point, optimisticCrop *optimisticCropState, fixedDisplayWidth float64) viewportLayoutInfo {
	stageDims := imageDims
	if optimisticCrop != nil && optimisticCrop.TargetDims.X > 0 && optimisticCrop.TargetDims.Y > 0 {
		stageDims = optimisticCrop.TargetDims
	} else if v.PresentedDims.X > 0 && v.PresentedDims.Y > 0 && imageDims.X <= 0 {
		stageDims = v.PresentedDims
	}
	if stageDims.X <= 0 || stageDims.Y <= 0 {
		stageDims = image.Pt(1, 1)
	}

	if fitted := fitWidthFor(vpW, vpH, stageDims.X, stageDims.Y); fitted > 0 {
		v.FitWidth = fitted
	}
	stageW := fixedDisplayWidth
	if stageW <= 0 {
		if v.FitWidth > 0 {
			stageW = math.Max(1, v.FitWidth*v.Zoom)
		} else {
			stageW = math.Max(1, fitWidthFor(vpW, vpH, stageDims.X, stageDims.Y))
		}
	}
	stageH := math.Max(1, stageW*float64(stageDims.Y)/float64(stageDims.X))

	contentW := math.Max(float64(vpW), stageW)
	contentH := math.Max(float64(vpH), stageH)
	maxSX := math.Max(0, contentW-float64(vpW))
	maxSY := math.Max(0, contentH-float64(vpH))
	v.ScrollX = clampF64(v.ScrollX, 0, maxSX)
	v.ScrollY = clampF64(v.ScrollY, 0, maxSY)

	stageLeft := (contentW - stageW) / 2
	stageTop := (contentH - stageH) / 2

	info := viewportLayoutInfo{
		ViewportW:     vpW,
		ViewportH:     vpH,
		StageLeft:     stageLeft,
		StageTop:      stageTop,
		StageX:        stageLeft - v.ScrollX,
		StageY:        stageTop - v.ScrollY,
		StageWidth:    stageW,
		StageHeight:   stageH,
		ContentWidth:  contentW,
		ContentHeight: contentH,
		StageDims:     stageDims,
	}
	v.LastLayout = info
	v.LastViewportSize = image.Pt(vpW, vpH)
	return info
}

func (v *viewportState) Layout(
	gtx layout.Context,
	th *material.Theme,
	resolveImg func(string) (*image.NRGBA, bool),
	source string,
	imageDims image.Point,
	showPlaceholder bool,
	placeholderText string,
	optimisticCrop *optimisticCropState,
	visual viewportVisualState,
	cb viewportCallbacks,
) layout.Dimensions {
	vpW := max(1, gtx.Constraints.Max.X)
	vpH := max(1, gtx.Constraints.Max.Y)
	size := image.Pt(vpW, vpH)
	vpRect := image.Rectangle{Max: size}

	// Background
	paint.FillShape(gtx.Ops, colCanvasBg, clip.Rect(vpRect).Op())

	if source == "" {
		v.LastLayout = viewportLayoutInfo{ViewportW: vpW, ViewportH: vpH, StageDims: image.Pt(1, 1)}
		v.LastViewportSize = size
		if showPlaceholder {
			layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(th, unit.Sp(14), placeholderText)
				lbl.Color = colTextMuted
				return lbl.Layout(gtx)
			})
		}
		return layout.Dimensions{Size: size}
	}

	info := v.computeLayout(vpW, vpH, imageDims, optimisticCrop, cb.FixedDisplayWidth)

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &v.canvasTag,
			Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Move | pointer.Enter | pointer.Leave | pointer.Scroll,
			ScrollX: pointer.ScrollRange{
				Min: -1200,
				Max: 1200,
			},
			ScrollY: pointer.ScrollRange{
				Min: -1200,
				Max: 1200,
			},
		})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		layoutDirty := false
		if pe.Kind == pointer.Scroll {
			if pe.Modifiers.Contain(key.ModShift) && cb.OnWheelFeather != nil {
				cb.OnWheelFeather(pe.Scroll.Y)
				continue
			}
			if !cb.DisableWheelZoom && pe.Scroll.Y != 0 {
				v.ApplyWheelZoom(pe.Scroll.Y, float64(pe.Position.X), float64(pe.Position.Y))
				info = v.computeLayout(vpW, vpH, imageDims, optimisticCrop, cb.FixedDisplayWidth)
				continue
			}
			if cb.DisableWheelZoom && (pe.Scroll.X != 0 || pe.Scroll.Y != 0) {
				maxSX := math.Max(0, info.ContentWidth-float64(vpW))
				maxSY := math.Max(0, info.ContentHeight-float64(vpH))
				nextSX := clampF64(v.ScrollX+float64(pe.Scroll.X), 0, maxSX)
				nextSY := clampF64(v.ScrollY+float64(pe.Scroll.Y), 0, maxSY)
				if nextSX != v.ScrollX || nextSY != v.ScrollY {
					v.ScrollX = nextSX
					v.ScrollY = nextSY
					info = v.computeLayout(vpW, vpH, imageDims, optimisticCrop, cb.FixedDisplayWidth)
				}
				continue
			}
		}
		if cb.OnPointer != nil {
			prevSX, prevSY, prevZoom := v.ScrollX, v.ScrollY, v.Zoom
			cb.OnPointer(gtx, info, pe)
			if v.ScrollX != prevSX || v.ScrollY != prevSY || v.Zoom != prevZoom {
				layoutDirty = true
			}
		}
		if layoutDirty {
			info = v.computeLayout(vpW, vpH, imageDims, optimisticCrop, cb.FixedDisplayWidth)
		}
	}

	if cb.GetVisual != nil {
		visual = cb.GetVisual()
	}

	// Pick up any completed background raster build before drawing.
	v.consumeCompletedAsync(source, cb)

	// Ensure raster is cached or requested for current source + visible rect.
	if resolveImg != nil && imageDims.X > 0 && imageDims.Y > 0 && (optimisticCrop == nil || source != optimisticCrop.Source) {
		v.ensureRaster(resolveImg, source, imageDims, info, cb)
	}

	// Draw checkerboard skeleton anchored in stage space.
	stageClip := image.Rect(
		int(math.Floor(info.StageX)),
		int(math.Floor(info.StageY)),
		int(math.Ceil(info.StageX+info.StageWidth)),
		int(math.Ceil(info.StageY+info.StageHeight)),
	).Intersect(vpRect)
	if !stageClip.Empty() {
		drawCheckerboard(gtx.Ops, stageClip, float32(info.StageX), float32(info.StageY), viewportCheckerSize)
	}

	// Draw cached/active raster layers clipped to viewport.
	vpStack := clip.Rect(vpRect).Push(gtx.Ops)
	var liveStack op.TransformStack
	hasLive := false
	if visual.DiscLiveActive {
		cx := float32(info.StageX + info.StageWidth/2)
		cy := float32(info.StageY + info.StageHeight/2)
		liveAff := f32.AffineId().
			Rotate(f32.Pt(cx, cy), visual.DiscLiveTransform.Angle*math.Pi/180).
			Offset(f32.Pt(visual.DiscLiveTransform.Dx, visual.DiscLiveTransform.Dy))
		liveStack = op.Affine(liveAff).Push(gtx.Ops)
		hasLive = true
	}

	if v.activeEntry != nil {
		// Draw neighboring cached rasters for same source/dims underneath active entry.
		for i := range v.cache {
			c := &v.cache[i]
			if c == v.activeEntry || c.source != v.activeEntry.source || c.dims != v.activeEntry.dims {
				continue
			}
			v.drawRasterEntry(gtx.Ops, c, info, optimisticCrop)
		}
		v.drawRasterEntry(gtx.Ops, v.activeEntry, info, optimisticCrop)
	}
	if hasLive {
		liveStack.Pop()
	}

	if !visual.Dragging {
		v.lastDiscGuideLog = time.Time{}
		v.lastDiscGuideRad = 0
	}

	// Draw vector guides in viewport space.
	drawCanvasGuides(gtx, th, visual, info, v, cb.DebugLog)
	vpStack.Pop()

	// Canvas pointer input area (registered before scrollbars so scrollbar thumbs win hit testing).
	cursor := cb.Cursor
	if cb.GetCursor != nil {
		cursor = cb.GetCursor(gtx)
	}
	area := clip.Rect(vpRect).Push(gtx.Ops)
	event.Op(gtx.Ops, &v.canvasTag)
	if cursor != pointer.CursorDefault {
		cursor.Add(gtx.Ops)
	}
	area.Pop()

	// Scrollbars (drawn above guides and registered after canvasTag so thumbs capture clicks).
	if !cb.HideScrollbars {
		v.layoutScrollbars(gtx, info)
	}

	return layout.Dimensions{Size: size}
}

func (v *viewportState) drawRasterEntry(ops *op.Ops, entry *viewportRasterEntry, info viewportLayoutInfo, optimistic *optimisticCropState) {
	if entry == nil || entry.width <= 0 || entry.height <= 0 || entry.dims.X <= 0 || entry.dims.Y <= 0 {
		return
	}
	if optimistic != nil && entry.dims == optimistic.SourceDims && optimistic.TargetDims.X > 0 && optimistic.TargetDims.Y > 0 {
		cropRect, ok := optimisticCropSourceRect(optimistic)
		if !ok {
			return
		}
		inter := entry.rect.Intersect(cropRect)
		if inter.Empty() {
			return
		}
		// Full entry rectangle transformed such that cropRect maps to [StageX..StageX+StageWidth].
		scaleX := float32(info.StageWidth / float64(cropRect.Dx()))
		scaleY := float32(info.StageHeight / float64(cropRect.Dy()))
		dstX := float32(info.StageX) + float32(entry.rect.Min.X-cropRect.Min.X)*scaleX
		dstY := float32(info.StageY) + float32(entry.rect.Min.Y-cropRect.Min.Y)*scaleY
		dstW := float32(entry.rect.Dx()) * scaleX
		dstH := float32(entry.rect.Dy()) * scaleY

		clipX0 := int(math.Floor(info.StageX + float64(inter.Min.X-cropRect.Min.X)*float64(scaleX)))
		clipY0 := int(math.Floor(info.StageY + float64(inter.Min.Y-cropRect.Min.Y)*float64(scaleY)))
		clipX1 := int(math.Ceil(info.StageX + float64(inter.Max.X-cropRect.Min.X)*float64(scaleX)))
		clipY1 := int(math.Ceil(info.StageY + float64(inter.Max.Y-cropRect.Min.Y)*float64(scaleY)))
		cRect := image.Rect(clipX0, clipY0, clipX1, clipY1)
		if cRect.Empty() {
			return
		}
		cl := clip.Rect(cRect).Push(ops)
		aff := f32.AffineId().
			Scale(f32.Point{}, f32.Pt(dstW/float32(entry.width), dstH/float32(entry.height))).
			Offset(f32.Pt(dstX, dstY))
		st := op.Affine(aff).Push(ops)
		entry.imgOp.Add(ops)
		paint.PaintOp{}.Add(ops)
		st.Pop()
		cl.Pop()
		return
	}

	if entry.dims != info.StageDims && optimistic != nil {
		return
	}
	dstX := float32(info.StageX + (float64(entry.rect.Min.X)/float64(entry.dims.X))*info.StageWidth)
	dstY := float32(info.StageY + (float64(entry.rect.Min.Y)/float64(entry.dims.Y))*info.StageHeight)
	dstW := float32((float64(entry.rect.Dx()) / float64(entry.dims.X)) * info.StageWidth)
	dstH := float32((float64(entry.rect.Dy()) / float64(entry.dims.Y)) * info.StageHeight)
	if dstW <= 0 || dstH <= 0 {
		return
	}

	clipRect := image.Rect(
		int(math.Floor(float64(dstX))),
		int(math.Floor(float64(dstY))),
		int(math.Ceil(float64(dstX+dstW))),
		int(math.Ceil(float64(dstY+dstH))),
	)
	cl := clip.Rect(clipRect).Push(ops)
	aff := f32.AffineId().
		Scale(f32.Point{}, f32.Pt(dstW/float32(entry.width), dstH/float32(entry.height))).
		Offset(f32.Pt(dstX, dstY))
	st := op.Affine(aff).Push(ops)
	entry.imgOp.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
	cl.Pop()
}

func optimisticCropSourceRect(crop *optimisticCropState) (image.Rectangle, bool) {
	if crop == nil || crop.SourceDims.X <= 0 || crop.SourceDims.Y <= 0 || crop.TargetDims.X <= 0 || crop.TargetDims.Y <= 0 {
		return image.Rectangle{}, false
	}
	w, h := crop.SourceDims.X, crop.SourceDims.Y
	tw, th := crop.TargetDims.X, crop.TargetDims.Y
	amt := crop.Amount
	switch crop.Direction {
	case "top":
		return image.Rect(0, amt, w, amt+th), true
	case "bottom":
		return image.Rect(0, 0, w, th), true
	case "left":
		return image.Rect(amt, 0, amt+tw, h), true
	case "right":
		return image.Rect(0, 0, tw, h), true
	default:
		return image.Rectangle{}, false
	}
}

func visibleImageRect(info viewportLayoutInfo, dims image.Point) image.Rectangle {
	if dims.X <= 0 || dims.Y <= 0 || info.StageWidth <= 0 || info.StageHeight <= 0 {
		return image.Rect(0, 0, max(1, dims.X), max(1, dims.Y))
	}
	x0 := clampF64((0-info.StageX)/info.StageWidth*float64(dims.X), 0, float64(dims.X))
	y0 := clampF64((0-info.StageY)/info.StageHeight*float64(dims.Y), 0, float64(dims.Y))
	x1 := clampF64((float64(info.ViewportW)-info.StageX)/info.StageWidth*float64(dims.X), 0, float64(dims.X))
	y1 := clampF64((float64(info.ViewportH)-info.StageY)/info.StageHeight*float64(dims.Y), 0, float64(dims.Y))
	ix0 := int(math.Floor(x0))
	iy0 := int(math.Floor(y0))
	ix1 := int(math.Ceil(x1))
	iy1 := int(math.Ceil(y1))
	if ix1 <= ix0 || iy1 <= iy0 {
		return image.Rect(0, 0, dims.X, dims.Y)
	}
	return image.Rect(ix0, iy0, ix1, iy1)
}

func quantizeSourceRect(visible image.Rectangle, dims image.Point) image.Rectangle {
	padX := int(math.Max(32, float64(visible.Dx())*viewportOverscanFraction))
	padY := int(math.Max(32, float64(visible.Dy())*viewportOverscanFraction))
	x1 := clamp(visible.Min.X-padX, 0, dims.X)
	y1 := clamp(visible.Min.Y-padY, 0, dims.Y)
	x2 := clamp(visible.Max.X+padX, 0, dims.X)
	y2 := clamp(visible.Max.Y+padY, 0, dims.Y)

	qx1 := clamp((x1/viewportSourceQuantum)*viewportSourceQuantum, 0, dims.X)
	qy1 := clamp((y1/viewportSourceQuantum)*viewportSourceQuantum, 0, dims.Y)
	qx2 := clamp(((x2+viewportSourceQuantum-1)/viewportSourceQuantum)*viewportSourceQuantum, 0, dims.X)
	qy2 := clamp(((y2+viewportSourceQuantum-1)/viewportSourceQuantum)*viewportSourceQuantum, 0, dims.Y)
	if qx2 <= qx1 {
		qx2 = min(dims.X, qx1+1)
	}
	if qy2 <= qy1 {
		qy2 = min(dims.Y, qy1+1)
	}
	return image.Rect(qx1, qy1, qx2, qy2)
}

func boundedDestSize(srcRect image.Rectangle, stageScale float64) (int, int) {
	w := max(1, min(srcRect.Dx(), int(math.Ceil(float64(srcRect.Dx())*stageScale))))
	h := max(1, min(srcRect.Dy(), int(math.Ceil(float64(srcRect.Dy())*stageScale))))
	factor := math.Min(1, math.Min(float64(viewportMaxDim)/float64(w), float64(viewportMaxDim)/float64(h)))
	if float64(w)*float64(h)*factor*factor > float64(viewportMaxPixels) {
		factor = math.Min(factor, math.Sqrt(float64(viewportMaxPixels)/(float64(w)*float64(h))))
	}
	if factor < 1 {
		w = max(1, int(math.Floor(float64(w)*factor)))
		h = max(1, int(math.Floor(float64(h)*factor)))
	}
	return w, h
}

func (v *viewportState) findExactCached(key string) *viewportRasterEntry {
	for i := len(v.cache) - 1; i >= 0; i-- {
		if v.cache[i].key == key {
			entry := v.cache[i]
			copy(v.cache[i:], v.cache[i+1:])
			v.cache[len(v.cache)-1] = entry
			return &v.cache[len(v.cache)-1]
		}
	}
	return nil
}

func (v *viewportState) findCoveringCached(source string, dims image.Point, visible image.Rectangle, reqScaleX, reqScaleY float64) *viewportRasterEntry {
	bestIdx := -1
	bestArea := math.MaxInt
	for i := range v.cache {
		c := &v.cache[i]
		if c.source != source || c.dims != dims {
			continue
		}
		if !visible.In(c.rect) {
			continue
		}
		sx := float64(c.width) / float64(c.rect.Dx())
		sy := float64(c.height) / float64(c.rect.Dy())
		if sx+1e-6 < reqScaleX || sy+1e-6 < reqScaleY {
			continue
		}
		area := c.rect.Dx() * c.rect.Dy()
		if area < bestArea {
			bestArea = area
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return nil
	}
	entry := v.cache[bestIdx]
	copy(v.cache[bestIdx:], v.cache[bestIdx+1:])
	v.cache[len(v.cache)-1] = entry
	return &v.cache[len(v.cache)-1]
}

func (v *viewportState) insertCache(entry viewportRasterEntry) *viewportRasterEntry {
	for i := range v.cache {
		if v.cache[i].key == entry.key {
			v.cache[i] = entry
			return &v.cache[i]
		}
	}
	if len(v.cache) >= viewportCacheCap {
		copy(v.cache[0:], v.cache[1:])
		v.cache = v.cache[:viewportCacheCap-1]
	}
	v.cache = append(v.cache, entry)
	return &v.cache[len(v.cache)-1]
}

func (v *viewportState) ensureRaster(
	resolveImg func(string) (*image.NRGBA, bool),
	source string,
	dims image.Point,
	info viewportLayoutInfo,
	cb viewportCallbacks,
) {
	visible := visibleImageRect(info, dims)
	srcRect := quantizeSourceRect(visible, dims)
	scale := info.StageWidth / float64(dims.X)
	dstW, dstH := boundedDestSize(srcRect, scale)
	reqSX := float64(dstW) / float64(srcRect.Dx())
	reqSY := float64(dstH) / float64(srcRect.Dy())

	if cov := v.findCoveringCached(source, dims, visible, reqSX, reqSY); cov != nil {
		v.activateEntry(cov, cb)
		return
	}
	key := fmt.Sprintf("%s:%d,%d,%d,%d:%dx%d", source, srcRect.Min.X, srcRect.Min.Y, srcRect.Dx(), srcRect.Dy(), dstW, dstH)
	if exact := v.findExactCached(key); exact != nil {
		v.activateEntry(exact, cb)
		return
	}

	srcImg, ok := resolveImg(source)
	if !ok || srcImg == nil {
		return
	}

	// Build small/medium rasters synchronously for zero flicker.
	if srcRect.Dx()*srcRect.Dy() <= 1_500_000 {
		entry := buildViewportRaster(rasterBuildSpec{
			key:      key,
			source:   source,
			dims:     dims,
			rect:     srcRect,
			dstW:     dstW,
			dstH:     dstH,
			srcImage: srcImg,
		})
		stored := v.insertCache(entry)
		v.activateEntry(stored, cb)
		return
	}

	spec := &rasterBuildSpec{
		key:      key,
		source:   source,
		dims:     dims,
		rect:     srcRect,
		dstW:     dstW,
		dstH:     dstH,
		srcImage: srcImg,
	}
	v.mu.Lock()
	v.pendingSpec = spec
	if v.building {
		v.mu.Unlock()
		return
	}
	v.building = true
	gen := v.sessionGen
	v.mu.Unlock()

	go func() {
		for {
			v.mu.Lock()
			if v.sessionGen != gen || v.pendingSpec == nil {
				v.building = false
				v.mu.Unlock()
				return
			}
			job := v.pendingSpec
			v.pendingSpec = nil
			v.mu.Unlock()

			entry := buildViewportRaster(*job)

			v.mu.Lock()
			stillValid := v.sessionGen == gen
			hasMore := v.pendingSpec != nil
			if !hasMore {
				v.building = false
			}
			v.mu.Unlock()

			if stillValid {
				// Store asynchronously? Wait — cache slice is read on UI thread, so
				// let's dispatch via Invalidator or keep cache insertion on UI thread!
				// Actually, let's keep a mutex-protected completed entry slot so the UI thread
				// promotes it on the next frame!
				v.storeCompletedAsync(gen, entry, cb.Invalidate)
			}
			if !hasMore {
				return
			}
		}
	}()
}

// Completed async rasters sit behind v.mu until the UI frame picks them up,
// keeping v.cache / v.activeEntry strictly UI-thread owned.
var completedAsyncRasters sync.Map // *viewportState -> viewportRasterEntry

func (v *viewportState) storeCompletedAsync(gen uint64, entry viewportRasterEntry, invalidate func()) {
	v.mu.Lock()
	ok := v.sessionGen == gen
	v.mu.Unlock()
	if !ok {
		return
	}
	completedAsyncRasters.Store(v, entry)
	if invalidate != nil {
		invalidate()
	}
}

func (v *viewportState) consumeCompletedAsync(source string, cb viewportCallbacks) {
	raw, ok := completedAsyncRasters.LoadAndDelete(v)
	if !ok {
		return
	}
	entry := raw.(viewportRasterEntry)
	if entry.source == source {
		stored := v.insertCache(entry)
		v.activateEntry(stored, cb)
	}
}

func (v *viewportState) activateEntry(entry *viewportRasterEntry, cb viewportCallbacks) {
	v.activeEntry = entry
	changed := v.PresentedSource != entry.source || v.PresentedDims != entry.dims
	v.PresentedSource = entry.source
	v.PresentedDims = entry.dims
	if changed && cb.OnPresented != nil {
		cb.OnPresented(entry.source, entry.dims)
	}
}

func buildViewportRaster(spec rasterBuildSpec) viewportRasterEntry {
	srcRect := spec.rect.Intersect(spec.srcImage.Bounds())
	var bmp *image.NRGBA
	if srcRect.Dx() == spec.dstW && srcRect.Dy() == spec.dstH {
		bmp = raster.CropNRGBA(spec.srcImage, srcRect)
	} else {
		bmp = downsampleViewportRegion(spec.srcImage, srcRect, spec.dstW, spec.dstH)
	}
	opImg := paint.NewImageOp(bmp)
	opImg.Filter = paint.FilterLinear
	return viewportRasterEntry{
		key:    spec.key,
		source: spec.source,
		dims:   spec.dims,
		rect:   srcRect,
		width:  bmp.Bounds().Dx(),
		height: bmp.Bounds().Dy(),
		imgOp:  opImg,
	}
}

func downsampleViewportRegion(src *image.NRGBA, rect image.Rectangle, newW, newH int) *image.NRGBA {
	rect = rect.Intersect(src.Bounds())
	if rect.Empty() || newW < 1 || newH < 1 {
		return image.NewNRGBA(image.Rect(0, 0, max(1, newW), max(1, newH)))
	}
	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	srcW, srcH := rect.Dx(), rect.Dy()

	// Bilinear sampling when near 1:1 or slightly scaled; box-average 2x2/area when shrinking significantly.
	workers := min(runtime.NumCPU(), newH)
	if workers < 1 {
		workers = 1
	}
	rowsPerWorker := (newH + workers - 1) / workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		y0 := w * rowsPerWorker
		y1 := min(newH, (w+1)*rowsPerWorker)
		if y0 >= y1 {
			break
		}
		wg.Add(1)
		go func(startY, endY int) {
			defer wg.Done()
			scaleX := float64(srcW) / float64(newW)
			scaleY := float64(srcH) / float64(newH)
			areaFilter := scaleX > 1.5 || scaleY > 1.5
			for y := startY; y < endY; y++ {
				dstRow := y * dst.Stride
				if !areaFilter {
					fy := (float64(y)+0.5)*scaleY - 0.5
					iy := int(math.Floor(fy))
					wy := fy - float64(iy)
					iy0 := clamp(rect.Min.Y+iy, rect.Min.Y, rect.Max.Y-1)
					iy1 := clamp(rect.Min.Y+iy+1, rect.Min.Y, rect.Max.Y-1)
					row0 := src.PixOffset(rect.Min.X, iy0)
					row1 := src.PixOffset(rect.Min.X, iy1)
					for x := 0; x < newW; x++ {
						fx := (float64(x)+0.5)*scaleX - 0.5
						ix := int(math.Floor(fx))
						wx := fx - float64(ix)
						ix0 := clamp(ix, 0, srcW-1) * 4
						ix1 := clamp(ix+1, 0, srcW-1) * 4

						p00 := src.Pix[row0+ix0 : row0+ix0+4]
						p10 := src.Pix[row0+ix1 : row0+ix1+4]
						p01 := src.Pix[row1+ix0 : row1+ix0+4]
						p11 := src.Pix[row1+ix1 : row1+ix1+4]

						w00 := (1 - wx) * (1 - wy)
						w10 := wx * (1 - wy)
						w01 := (1 - wx) * wy
						w11 := wx * wy
						di := dstRow + x*4
						dst.Pix[di+0] = uint8(float64(p00[0])*w00 + float64(p10[0])*w10 + float64(p01[0])*w01 + float64(p11[0])*w11 + 0.5)
						dst.Pix[di+1] = uint8(float64(p00[1])*w00 + float64(p10[1])*w10 + float64(p01[1])*w01 + float64(p11[1])*w11 + 0.5)
						dst.Pix[di+2] = uint8(float64(p00[2])*w00 + float64(p10[2])*w10 + float64(p01[2])*w01 + float64(p11[2])*w11 + 0.5)
						dst.Pix[di+3] = uint8(float64(p00[3])*w00 + float64(p10[3])*w10 + float64(p01[3])*w01 + float64(p11[3])*w11 + 0.5)
					}
					continue
				}

				sy0 := rect.Min.Y + (y*srcH)/newH
				sy1 := rect.Min.Y + ((y+1)*srcH+newH-1)/newH
				if sy1 <= sy0 {
					sy1 = sy0 + 1
				}
				sy1 = min(sy1, rect.Max.Y)
				stepY := max(1, (sy1-sy0)/4)
				for x := 0; x < newW; x++ {
					sx0 := rect.Min.X + (x*srcW)/newW
					sx1 := rect.Min.X + ((x+1)*srcW+newW-1)/newW
					if sx1 <= sx0 {
						sx1 = sx0 + 1
					}
					sx1 = min(sx1, rect.Max.X)
					stepX := max(1, (sx1-sx0)/4)
					var sr, sg, sb, sa, cnt int
					for sy := sy0; sy < sy1; sy += stepY {
						off := src.PixOffset(sx0, sy)
						for sx := sx0; sx < sx1; sx += stepX {
							sr += int(src.Pix[off])
							sg += int(src.Pix[off+1])
							sb += int(src.Pix[off+2])
							sa += int(src.Pix[off+3])
							cnt++
							off += stepX * 4
						}
					}
					di := dstRow + x*4
					if cnt > 0 {
						dst.Pix[di+0] = uint8(sr / cnt)
						dst.Pix[di+1] = uint8(sg / cnt)
						dst.Pix[di+2] = uint8(sb / cnt)
						dst.Pix[di+3] = uint8(sa / cnt)
					}
				}
			}
		}(y0, y1)
	}
	wg.Wait()
	return dst
}

func (v *viewportState) layoutScrollbars(gtx layout.Context, info viewportLayoutInfo) {
	barThickness := gtx.Dp(8)
	margin := gtx.Dp(2)
	minThumb := float32(gtx.Dp(24))

	if info.ContentWidth > float64(info.ViewportW)+0.5 {
		trackLen := float32(info.ViewportW - margin*2)
		if info.ContentHeight > float64(info.ViewportH)+0.5 {
			trackLen -= float32(barThickness + margin)
		}
		if trackLen > minThumb {
			frac := float32(float64(info.ViewportW) / info.ContentWidth)
			thumbLen := float32(math.Max(float64(minThumb), float64(trackLen*frac)))
			maxScroll := info.ContentWidth - float64(info.ViewportW)
			scrollFrac := float32(v.ScrollX / maxScroll)
			thumbX := float32(margin) + scrollFrac*(trackLen-thumbLen)
			barY := info.ViewportH - barThickness - margin
			barRect := image.Rect(margin, barY, margin+int(trackLen), barY+barThickness)

			area := clip.Rect(barRect).Push(gtx.Ops)
			event.Op(gtx.Ops, &v.hScrollTag)
			for {
				ev, ok := gtx.Event(pointer.Filter{
					Target: &v.hScrollTag,
					Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave,
				})
				if !ok {
					break
				}
				if pe, ok := ev.(pointer.Event); ok {
					switch pe.Kind {
					case pointer.Enter:
						v.hoverHBar = true
					case pointer.Leave:
						v.hoverHBar = false
					case pointer.Press:
						if pe.Buttons.Contain(pointer.ButtonPrimary) {
							gtx.Execute(pointer.GrabCmd{Tag: &v.hScrollTag, ID: pe.PointerID})
							v.hScrollDrag = &scrollThumbDrag{startPos: pe.Position.X, startScroll: v.ScrollX}
						}
					case pointer.Drag:
						if v.hScrollDrag != nil && trackLen > thumbLen {
							dx := float64(pe.Position.X - v.hScrollDrag.startPos)
							v.ScrollX = clampF64(v.hScrollDrag.startScroll+dx*(maxScroll/float64(trackLen-thumbLen)), 0, maxScroll)
						}
					case pointer.Release, pointer.Cancel:
						v.hScrollDrag = nil
					}
				}
			}
			area.Pop()

			col := colBorder
			if v.hoverHBar || v.hScrollDrag != nil {
				col = colBorderLight
			}
			thumbRect := image.Rect(int(thumbX), barY, int(thumbX+thumbLen), barY+barThickness)
			fillRRect(gtx.Ops, thumbRect, barThickness/2, col)
		}
	}

	if info.ContentHeight > float64(info.ViewportH)+0.5 {
		trackLen := float32(info.ViewportH - margin*2)
		if info.ContentWidth > float64(info.ViewportW)+0.5 {
			trackLen -= float32(barThickness + margin)
		}
		if trackLen > minThumb {
			frac := float32(float64(info.ViewportH) / info.ContentHeight)
			thumbLen := float32(math.Max(float64(minThumb), float64(trackLen*frac)))
			maxScroll := info.ContentHeight - float64(info.ViewportH)
			scrollFrac := float32(v.ScrollY / maxScroll)
			thumbY := float32(margin) + scrollFrac*(trackLen-thumbLen)
			barX := info.ViewportW - barThickness - margin
			barRect := image.Rect(barX, margin, barX+barThickness, margin+int(trackLen))

			area := clip.Rect(barRect).Push(gtx.Ops)
			event.Op(gtx.Ops, &v.vScrollTag)
			for {
				ev, ok := gtx.Event(pointer.Filter{
					Target: &v.vScrollTag,
					Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave,
				})
				if !ok {
					break
				}
				if pe, ok := ev.(pointer.Event); ok {
					switch pe.Kind {
					case pointer.Enter:
						v.hoverVBar = true
					case pointer.Leave:
						v.hoverVBar = false
					case pointer.Press:
						if pe.Buttons.Contain(pointer.ButtonPrimary) {
							gtx.Execute(pointer.GrabCmd{Tag: &v.vScrollTag, ID: pe.PointerID})
							v.vScrollDrag = &scrollThumbDrag{startPos: pe.Position.Y, startScroll: v.ScrollY}
						}
					case pointer.Drag:
						if v.vScrollDrag != nil && trackLen > thumbLen {
							dy := float64(pe.Position.Y - v.vScrollDrag.startPos)
							v.ScrollY = clampF64(v.vScrollDrag.startScroll+dy*(maxScroll/float64(trackLen-thumbLen)), 0, maxScroll)
						}
					case pointer.Release, pointer.Cancel:
						v.vScrollDrag = nil
					}
				}
			}
			area.Pop()

			col := colBorder
			if v.hoverVBar || v.vScrollDrag != nil {
				col = colBorderLight
			}
			thumbRect := image.Rect(barX, int(thumbY), barX+barThickness, int(thumbY+thumbLen))
			fillRRect(gtx.Ops, thumbRect, barThickness/2, col)
		}
	}
}

func stagePointFromImage(pt f32.Point, info viewportLayoutInfo, dims image.Point) f32.Point {
	return f32.Pt(
		float32(info.StageX)+(pt.X/float32(dims.X))*float32(info.StageWidth),
		float32(info.StageY)+(pt.Y/float32(dims.Y))*float32(info.StageHeight),
	)
}

func drawCanvasGuides(gtx layout.Context, th *material.Theme, visual viewportVisualState, info viewportLayoutInfo, v *viewportState, debugLog func(format string, args ...any)) {
	dims := visual.RealImageDims
	if dims.X <= 0 || dims.Y <= 0 {
		return
	}
	ops := gtx.Ops
	imageScale := float32(info.StageWidth) / float32(dims.X)
	toCanvas := func(x, y float32) f32.Point {
		return stagePointFromImage(f32.Pt(x, y), info, dims)
	}

	// 1. Adjustment marquee
	if visual.AdjustmentSelectionActive {
		fillCol := color.NRGBA{R: 145, G: 145, B: 145, A: 20}
		strokeCol := color.NRGBA{R: 169, G: 169, B: 169, A: 255}
		if visual.Dragging && visual.DragStart != nil && visual.DragCurrent != nil && visual.AdjustmentRect == nil && !visual.UseTouchupTool {
			x := float32(info.StageX) + float32(math.Min(float64(visual.DragStart.X), float64(visual.DragCurrent.X)))
			y := float32(info.StageY) + float32(math.Min(float64(visual.DragStart.Y), float64(visual.DragCurrent.Y)))
			w := float32(math.Abs(float64(visual.DragCurrent.X - visual.DragStart.X)))
			h := float32(math.Abs(float64(visual.DragCurrent.Y - visual.DragStart.Y)))
			drawDashedRect(ops, x, y, w, h, 2, 6, 3, fillCol, strokeCol)
		} else if visual.AdjustmentRect != nil {
			r := normalizedRectI(*visual.AdjustmentRect)
			a := toCanvas(float32(r.X1), float32(r.Y1))
			b := toCanvas(float32(r.X2), float32(r.Y2))
			drawDashedRect(ops, a.X, a.Y, b.X-a.X, b.Y-a.Y, 2, 6, 3, fillCol, strokeCol)
			if !visual.UseTouchupTool {
				for _, pt := range []f32.Point{a, {X: b.X, Y: a.Y}, b, {X: a.X, Y: b.Y}} {
					drawSolidCircle(ops, pt, 7*imageScale, strokeCol)
					drawStrokeCircle(ops, pt, 7*imageScale, 1.5, color.NRGBA{R: 52, G: 52, B: 52, A: 255})
				}
			}
		}
	}

	// 2. Touch-up brush strokes + double-ring cursor
	if visual.UseTouchupTool && len(visual.TouchupStrokes) > 0 {
		rad := float32(math.Max(1, visual.BrushSize)) * imageScale / 2
		for _, pt := range visual.TouchupStrokes {
			p := toCanvas(float32(pt.X), float32(pt.Y))
			drawSolidCircle(ops, p, rad, color.NRGBA{R: 255, G: 0, B: 0, A: 90})
			drawStrokeCircle(ops, p, rad, 1, color.NRGBA{R: 255, G: 0, B: 0, A: 204})
		}
	}
	if visual.UseTouchupTool && visual.TouchupCursor != nil {
		p := toCanvas(float32(visual.TouchupCursor.X), float32(visual.TouchupCursor.Y))
		rad := float32(math.Max(1, visual.BrushSize)) * imageScale / 2
		if visual.TouchupCursor.Resizing {
			drawSolidCircle(ops, p, rad, color.NRGBA{R: 255, G: 0, B: 0, A: 90})
		}
		drawStrokeCircle(ops, p, rad, 3, color.NRGBA{R: 0, G: 0, B: 0, A: 199})
		drawStrokeCircle(ops, p, rad, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 242})
		if visual.TouchupCursor.Resizing {
			label := fmt.Sprintf("%d px", int(math.Round(math.Max(1, visual.BrushSize))))
			drawBrushSizeReadout(gtx, th, label, p, rad, info)
		}
	}

	// 3. Straight edge yellow drag line
	if visual.UseStraightEdgeTool && visual.Dragging && visual.DragStart != nil && visual.DragCurrent != nil {
		a := f32.Pt(float32(info.StageX)+visual.DragStart.X, float32(info.StageY)+visual.DragStart.Y)
		b := f32.Pt(float32(info.StageX)+visual.DragCurrent.X, float32(info.StageY)+visual.DragCurrent.Y)
		drawStrokeLine(ops, a, b, 2, colYellow)
	}

	// 4. Disc mode initial circle draw guide
	if visual.Mode == "disc" && !visual.AdjustmentSelectionActive && !visual.UseStraightEdgeTool && !visual.UseTouchupTool &&
		visual.Dragging && visual.DragStart != nil && visual.DragCurrent != nil && !visual.CtrlDragActive && !visual.ShiftDragActive {
		dx := visual.DragStart.X - visual.DragCurrent.X
		dy := visual.DragStart.Y - visual.DragCurrent.Y
		rad := float32(math.Hypot(float64(dx), float64(dy)))
		center := f32.Pt(float32(info.StageX)+visual.DragCurrent.X, float32(info.StageY)+visual.DragCurrent.Y)
		var cutoutRad float32
		if visual.DiscCenterCutout && visual.DiscCutoutPercent > 0 {
			cutoutRad = rad * float32(visual.DiscCutoutPercent) / 100
		}
		if debugLog != nil && (v == nil || v.lastDiscGuideLog.IsZero() || time.Since(v.lastDiscGuideLog) >= 80*time.Millisecond || math.Abs(float64(rad-v.lastDiscGuideRad)) >= 10) {
			if v != nil {
				v.lastDiscGuideLog = time.Now()
				v.lastDiscGuideRad = rad
			}
			debugLog(
				"disc Guide: dragStart=(%.1f,%.1f) dragCurrent=(%.1f,%.1f) stage=(%.1f,%.1f %.1fx%.1f) center=(%.1f,%.1f) rad=%.1f cutoutOn=%v cutoutPct=%d cutoutRad=%.1f vpMax=%dx%d pxPerDp=%.3f",
				visual.DragStart.X, visual.DragStart.Y, visual.DragCurrent.X, visual.DragCurrent.Y,
				info.StageX, info.StageY, info.StageWidth, info.StageHeight,
				center.X, center.Y, rad, visual.DiscCenterCutout, visual.DiscCutoutPercent, cutoutRad,
				gtx.Constraints.Max.X, gtx.Constraints.Max.Y, gtx.Metric.PxPerDp,
			)
		}
		drawStrokeCircle(ops, center, rad, 2, colGreen)
		if cutoutRad > 0 {
			drawDashedCircle(ops, center, cutoutRad, 2, 4, 3, colGreen)
		}
	}

	// 5. Corner mode dots + calibration sequence
	if visual.Mode != "corner" || len(visual.DetectedCornerPts) == 0 {
		if v != nil {
			v.cornerOverlay = nil
		}
	}
	if visual.Mode == "corner" {
		const refSize = 1600.0
		minDim := math.Min(float64(dims.X), float64(dims.Y))
		dotBase := float64(visual.DotRadius)
		if dotBase <= 0 {
			dotBase = 5
		}
		dotImageR := float32(math.Max(2, math.Round(dotBase*(minDim/refSize))))
		if minDim <= 0 {
			dotImageR = float32(math.Max(2, dotBase))
		}
		selImageR := float32(math.Max(float64(dotImageR)*1.5, float64(dotImageR)+4))
		if len(visual.DetectedCornerPts) > 0 {
			screenR := float64(dotImageR * imageScale)
			if v != nil && screenR > 0 && screenR <= 128 && info.ViewportW > 0 && info.ViewportH > 0 {
				v.drawDetectedCornersOverlay(ops, visual.DetectedCornerPts, dims, visual.DotRadius, screenR, info)
			} else {
				frustumR := float32(screenR) + 2
				vpW := float32(info.ViewportW)
				vpH := float32(info.ViewportH)
				hasVP := info.ViewportW > 0 && info.ViewportH > 0
				for _, pt := range visual.DetectedCornerPts {
					p := toCanvas(float32(pt.X), float32(pt.Y))
					if hasVP && (p.X < -frustumR || p.Y < -frustumR || p.X > vpW+frustumR || p.Y > vpH+frustumR) {
						continue
					}
					drawSolidCircle(ops, p, dotImageR*imageScale, color.NRGBA{R: 255, G: 0, B: 0, A: 153})
					drawStrokeCircle(ops, p, dotImageR*imageScale, 1, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
				}
			}
		}
		if visual.ShowCornerSequence && len(visual.SelectedCornerPts) > 1 {
			poly := make([]f32.Point, len(visual.SelectedCornerPts))
			for i, pt := range visual.SelectedCornerPts {
				poly[i] = toCanvas(float32(pt.X), float32(pt.Y))
			}
			drawDashedPolyline(ops, poly, len(poly) == 4, 2, 6, 4, color.NRGBA{R: 0, G: 255, B: 0, A: 230})
		}
		for i, pt := range visual.SelectedCornerPts {
			p := toCanvas(float32(pt.X), float32(pt.Y))
			drawSolidCircle(ops, p, selImageR*imageScale, color.NRGBA{R: 0, G: 255, B: 0, A: 153})
			drawStrokeCircle(ops, p, selImageR*imageScale, 2, colGreen)
			if i < len(visual.CornerLabels) && visual.CornerLabels[i] != "" {
				drawCornerLabelPill(gtx, th, visual.CornerLabels[i], p)
			}
		}
	}

	// 6. Normal crop rectangle
	if visual.Mode == "normal" {
		if visual.Dragging && visual.DragStart != nil && visual.DragCurrent != nil && !visual.UseTouchupTool && visual.NormalRect == nil && !visual.AdjustmentSelectionActive {
			x := float32(info.StageX) + float32(math.Min(float64(visual.DragStart.X), float64(visual.DragCurrent.X)))
			y := float32(info.StageY) + float32(math.Min(float64(visual.DragStart.Y), float64(visual.DragCurrent.Y)))
			w := float32(math.Abs(float64(visual.DragCurrent.X - visual.DragStart.X)))
			h := float32(math.Abs(float64(visual.DragCurrent.Y - visual.DragStart.Y)))
			drawDashedRect(ops, x, y, w, h, 2, 6, 3, colGreenFill, colGreen)
		} else if visual.NormalRect != nil {
			r := normalizedRectI(*visual.NormalRect)
			a := toCanvas(float32(r.X1), float32(r.Y1))
			b := toCanvas(float32(r.X2), float32(r.Y2))
			drawDashedRect(ops, a.X, a.Y, b.X-a.X, b.Y-a.Y, 2, 6, 3, colGreenFill, colGreen)
			for _, pt := range []f32.Point{a, {X: b.X, Y: a.Y}, b, {X: a.X, Y: b.Y}} {
				drawSolidCircle(ops, pt, 8*imageScale, colGreen)
				drawStrokeCircle(ops, pt, 8*imageScale, 1.5, colGreenDark)
			}
		}
	}

	// 7. Line mode lines + endpoints + live drag line
	if visual.Mode == "line" {
		for _, ln := range visual.Lines {
			a := toCanvas(float32(ln.X1), float32(ln.Y1))
			b := toCanvas(float32(ln.X2), float32(ln.Y2))
			drawStrokeLine(ops, a, b, 2, colGreen)
			drawSolidCircle(ops, a, 6*imageScale, colGreen)
			drawStrokeCircle(ops, a, 6*imageScale, 1.5, colGreenDark)
			drawSolidCircle(ops, b, 6*imageScale, colGreen)
			drawStrokeCircle(ops, b, 6*imageScale, 1.5, colGreenDark)
		}
		if !visual.UseTouchupTool && visual.LineDragKind == "draw" && visual.Dragging && visual.DragStart != nil && visual.DragCurrent != nil {
			var a f32.Point
			if visual.LineStartImg != nil {
				a = toCanvas(float32(visual.LineStartImg.X), float32(visual.LineStartImg.Y))
			} else {
				a = f32.Pt(float32(info.StageX)+visual.DragStart.X, float32(info.StageY)+visual.DragStart.Y)
			}
			b := f32.Pt(float32(info.StageX)+visual.DragCurrent.X, float32(info.StageY)+visual.DragCurrent.Y)
			drawStrokeLine(ops, a, b, 2, colGreen)
		}
	}

	// 8. Compositor overlay live orange stroke
	if len(visual.OverlayStroke) > 0 {
		strokeCol := color.NRGBA{R: 255, G: 195, B: 107, A: 192}
		sw := float32(visual.OverlayBrushPct * math.Min(info.StageWidth, info.StageHeight) / 50.0)
		if sw < 2 {
			sw = 2
		}
		if len(visual.OverlayStroke) == 1 {
			p := f32.Pt(float32(info.StageX)+visual.OverlayStroke[0].X*float32(info.StageWidth), float32(info.StageY)+visual.OverlayStroke[0].Y*float32(info.StageHeight))
			drawSolidCircle(ops, p, sw/2, color.NRGBA{R: 255, G: 195, B: 107, A: 140})
		} else {
			var path clip.Path
			path.Begin(ops)
			for i, pt := range visual.OverlayStroke {
				p := f32.Pt(float32(info.StageX)+pt.X*float32(info.StageWidth), float32(info.StageY)+pt.Y*float32(info.StageHeight))
				if i == 0 {
					path.MoveTo(p)
				} else {
					path.LineTo(p)
				}
			}
			paint.FillShape(ops, strokeCol, clip.Stroke{Path: path.End(), Width: sw}.Op())
		}
	}
}

func measureOverlayLabel(gtx layout.Context, th *material.Theme, sp unit.Sp, textStr string, weight font.Weight, col color.NRGBA) (op.CallOp, image.Point) {
	cg := gtx
	cg.Constraints.Min = image.Point{}
	macro := op.Record(cg.Ops)
	lbl := material.Label(th, sp, textStr)
	lbl.MaxLines = 1
	if weight != 0 {
		lbl.Font.Weight = weight
	}
	lbl.Color = col
	td := lbl.Layout(cg)
	return macro.Stop(), td.Size
}

func paintOverlayPill(ops *op.Ops, call op.CallOp, textSz image.Point, origin image.Point, boxW, boxH, padX, radius int) image.Rectangle {
	off := op.Offset(origin).Push(ops)
	fillRRect(ops, image.Rectangle{Max: image.Pt(boxW, boxH)}, radius, color.NRGBA{R: 0, G: 0, B: 0, A: 204})
	to := op.Offset(image.Pt(padX, (boxH-textSz.Y)/2)).Push(ops)
	call.Add(ops)
	to.Pop()
	off.Pop()
	return image.Rect(origin.X, origin.Y, origin.X+boxW, origin.Y+boxH)
}

func drawBrushSizeReadout(gtx layout.Context, th *material.Theme, label string, p f32.Point, rad float32, info viewportLayoutInfo) image.Rectangle {
	padX := gtx.Dp(6)
	call, textSz := measureOverlayLabel(gtx, th, unit.Sp(12), label, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 245})
	boxW := textSz.X + padX*2
	boxH := max(gtx.Dp(22), textSz.Y+gtx.Dp(4))
	gap := float32(math.Max(12, float64(rad)+8))

	margin := float64(gtx.Dp(6))
	visL, visR := info.StageX, info.StageX+info.StageWidth
	if info.ViewportW > 0 {
		visL = math.Max(margin, visL)
		visR = math.Min(float64(info.ViewportW)-margin, visR)
		if visR-visL < float64(boxW) {
			visL = margin
			visR = math.Max(margin, float64(info.ViewportW)-margin)
		}
	}
	visT, visB := info.StageY, info.StageY+info.StageHeight
	if info.ViewportH > 0 {
		visT = math.Max(margin, visT)
		visB = math.Min(float64(info.ViewportH)-margin, visB)
		if visB-visT < float64(boxH) {
			visT = margin
			visB = math.Max(margin, float64(info.ViewportH)-margin)
		}
	}

	lx := float64(p.X + gap)
	if lx+float64(boxW) > visR {
		lx = float64(p.X-gap) - float64(boxW)
	}
	lx = clampF64(lx, visL, math.Max(visL, visR-float64(boxW)))
	ly := clampF64(float64(p.Y)-float64(boxH)/2, visT, math.Max(visT, visB-float64(boxH)))
	rx := int(math.Round(lx))
	ry := int(math.Round(ly))
	return paintOverlayPill(gtx.Ops, call, textSz, image.Pt(rx, ry), boxW, boxH, padX, gtx.Dp(4))
}

func drawCornerLabelPill(gtx layout.Context, th *material.Theme, textStr string, p f32.Point) image.Rectangle {
	padX := gtx.Dp(4)
	call, textSz := measureOverlayLabel(gtx, th, unit.Sp(11.5), textStr, font.Bold, colGreenLight)
	boxW := textSz.X + padX*2
	boxH := max(gtx.Dp(20), textSz.Y+gtx.Dp(4))
	x := int(math.Round(float64(p.X))) + gtx.Dp(10)
	y := int(math.Round(float64(p.Y))) - boxH/2
	return paintOverlayPill(gtx.Ops, call, textSz, image.Pt(x, y), boxW, boxH, padX, gtx.Dp(4))
}

func normalizedRectI(r rectI) rectI {
	if r.X1 > r.X2 {
		r.X1, r.X2 = r.X2, r.X1
	}
	if r.Y1 > r.Y2 {
		r.Y1, r.Y2 = r.Y2, r.Y1
	}
	return r
}

func hashCornerPoints(pts []image.Point) uint64 {
	const offset64 = uint64(14695981039346656037)
	const prime64 = uint64(1099511628211)
	h := offset64
	for _, pt := range pts {
		h ^= uint64(int64(pt.X))
		h *= prime64
		h ^= uint64(int64(pt.Y))
		h *= prime64
	}
	return h
}

func buildCornerDotMask(radius float64) (alpha []uint8, side, half int) {
	half = int(math.Ceil(radius + 1.5))
	side = half*2 + 1
	alpha = make([]uint8, side*side)
	idx := 0
	for dy := -half; dy <= half; dy++ {
		for dx := -half; dx <= half; dx++ {
			d := math.Hypot(float64(dx), float64(dy))
			fillCov := clampF64(radius+0.5-d, 0, 1)
			aFill := fillCov * (153.0 / 255.0)
			inEdge := clampF64(d-(radius-1.0), 0, 1)
			outEdge := clampF64((radius+1.0)-d, 0, 1)
			aStroke := math.Min(inEdge, outEdge)
			aTotal := aStroke + aFill*(1-aStroke)
			alpha[idx] = uint8(math.Round(clampF64(aTotal, 0, 1) * 255.0))
			idx++
		}
	}
	return alpha, side, half
}

func stampCornerDotMask(dst *image.RGBA, mask []uint8, side, half, cx, cy int) {
	dstW := dst.Rect.Dx()
	dstH := dst.Rect.Dy()
	x0 := cx - half
	y0 := cy - half
	x1 := x0 + side
	y1 := y0 + side
	if x1 <= 0 || x0 >= dstW || y1 <= 0 || y0 >= dstH {
		return
	}
	mx0 := 0
	if x0 < 0 {
		mx0 = -x0
		x0 = 0
	}
	my0 := 0
	if y0 < 0 {
		my0 = -y0
		y0 = 0
	}
	if x1 > dstW {
		x1 = dstW
	}
	if y1 > dstH {
		y1 = dstH
	}
	cols := x1 - x0
	if cols <= 0 || y1 <= y0 {
		return
	}
	for py := y0; py < y1; py++ {
		mOff := (my0+(py-y0))*side + mx0
		mRow := mask[mOff : mOff+cols]
		pOff := py*dst.Stride + x0*4
		for _, sa := range mRow {
			if sa == 0 {
				pOff += 4
				continue
			}
			da := dst.Pix[pOff+3]
			outA := sa
			if da != 0 {
				outA = uint8(int(sa) + (int(da)*(255-int(sa))+127)/255)
			}
			dst.Pix[pOff] = outA
			dst.Pix[pOff+3] = outA
			pOff += 4
		}
	}
}

func clampOverlaySpan(reqMin, reqMax, expMin, expMax, stageMin, stageMax, maxDim int) (int, int) {
	if expMax-expMin <= maxDim {
		return expMin, expMax
	}
	cx := (reqMin + reqMax) / 2
	start := clamp(cx-maxDim/2, stageMin, stageMax-maxDim)
	if reqMin < start {
		start = reqMin
	}
	if reqMax > start+maxDim {
		start = reqMax - maxDim
	}
	start = clamp(start, stageMin, stageMax-maxDim)
	return start, start + maxDim
}

func (v *viewportState) drawDetectedCornersOverlay(ops *op.Ops, pts []image.Point, dims image.Point, dotRadius int, screenR float64, info viewportLayoutInfo) {
	half := int(math.Ceil(screenR + 1.5))
	pad := half + 2
	stageBounds := image.Rect(-pad, -pad, int(math.Ceil(info.StageWidth))+pad, int(math.Ceil(info.StageHeight))+pad)
	stageOriginX := int(math.Round(info.StageX))
	stageOriginY := int(math.Round(info.StageY))
	vpRel := image.Rect(-stageOriginX, -stageOriginY, info.ViewportW-stageOriginX, info.ViewportH-stageOriginY)
	reqRect := vpRel.Intersect(stageBounds)
	if reqRect.Empty() {
		return
	}

	oc := v.cornerOverlay
	if oc == nil {
		oc = &cornerOverlayCache{}
		v.cornerOverlay = oc
	}

	ptsHash := hashCornerPoints(pts)
	hit := oc.valid &&
		oc.ptsLen == len(pts) &&
		oc.ptsHash == ptsHash &&
		oc.dims == dims &&
		oc.dotRadius == dotRadius &&
		math.Abs(oc.stageWidth-info.StageWidth) < 0.25 &&
		math.Abs(oc.stageHeight-info.StageHeight) < 0.25 &&
		reqRect.In(oc.coverRect)

	if !hit {
		maxDim := max(2048, max(info.ViewportW, info.ViewportH)+64)
		var coverRect image.Rectangle
		if stageBounds.Dx() <= maxDim && stageBounds.Dy() <= maxDim {
			coverRect = stageBounds
		} else {
			const overscan = 192
			expanded := image.Rect(reqRect.Min.X-overscan, reqRect.Min.Y-overscan, reqRect.Max.X+overscan, reqRect.Max.Y+overscan).Intersect(stageBounds)
			expanded.Min.X, expanded.Max.X = clampOverlaySpan(reqRect.Min.X, reqRect.Max.X, expanded.Min.X, expanded.Max.X, stageBounds.Min.X, stageBounds.Max.X, maxDim)
			expanded.Min.Y, expanded.Max.Y = clampOverlaySpan(reqRect.Min.Y, reqRect.Max.Y, expanded.Min.Y, expanded.Max.Y, stageBounds.Min.Y, stageBounds.Max.Y, maxDim)
			coverRect = expanded
		}

		if oc.mask == nil || math.Abs(oc.maskRadius-screenR) >= 0.05 {
			oc.mask, oc.maskSide, oc.maskHalf = buildCornerDotMask(screenR)
			oc.maskRadius = screenR
		}

		oc.slot = 1 - oc.slot
		sz := coverRect.Size()
		buf := oc.bufs[oc.slot]
		if buf == nil || buf.Rect.Size() != sz {
			buf = image.NewRGBA(image.Rectangle{Max: sz})
			oc.bufs[oc.slot] = buf
		} else {
			clear(buf.Pix)
		}

		sxScale := info.StageWidth / float64(dims.X)
		syScale := info.StageHeight / float64(dims.Y)
		ovW, ovH := sz.X, sz.Y
		for _, pt := range pts {
			cx := int(math.Round(float64(pt.X)*sxScale)) - coverRect.Min.X
			cy := int(math.Round(float64(pt.Y)*syScale)) - coverRect.Min.Y
			if cx < -oc.maskHalf || cx >= ovW+oc.maskHalf || cy < -oc.maskHalf || cy >= ovH+oc.maskHalf {
				continue
			}
			stampCornerDotMask(buf, oc.mask, oc.maskSide, oc.maskHalf, cx, cy)
		}

		imgOp := paint.NewImageOp(buf)
		imgOp.Filter = paint.FilterNearest
		oc.imgOp = imgOp
		oc.valid = true
		oc.ptsLen = len(pts)
		oc.ptsHash = ptsHash
		oc.dims = dims
		oc.dotRadius = dotRadius
		oc.stageWidth = info.StageWidth
		oc.stageHeight = info.StageHeight
		oc.coverRect = coverRect
		oc.rebuilds++
	}

	drawX := stageOriginX + oc.coverRect.Min.X
	drawY := stageOriginY + oc.coverRect.Min.Y
	off := op.Offset(image.Pt(drawX, drawY)).Push(ops)
	cl := clip.Rect{Max: oc.coverRect.Size()}.Push(ops)
	oc.imgOp.Add(ops)
	paint.PaintOp{}.Add(ops)
	cl.Pop()
	off.Pop()
}
