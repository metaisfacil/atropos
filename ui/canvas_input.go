package ui

import (
	"fmt"
	"image"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"
)

const (
	touchupBrushMin = 4
	touchupBrushMax = 200
)

type panDragState struct {
	startX     float32
	startY     float32
	startLeft  float64
	startTop   float64
}

type rectMoveDragState struct {
	startImg  image.Point
	startRect rectI
}

type lineHandleDragState struct {
	index int
	end   string // "start" or "end"
}

type touchupResizeDragState struct {
	startX    float32
	startSize float64
}

type discCtrlDragState struct {
	startClientX float32
	startClientY float32
	startCenter  image.Point
	stageW       float64
	stageH       float64
	natDims      image.Point
	// maxShift bounds the gesture (source px) so the live content layer
	// built at press time always covers the disc; 0 means unbounded.
	maxShift float64
}

type discShiftResult struct {
	dx     int
	dy     int
	liveDx float32
	liveDy float32
}

func displayToImage(dispX, dispY float64, dims image.Point, stageW, stageH float64) image.Point {
	if dims.X <= 0 || dims.Y <= 0 || stageW <= 0 || stageH <= 0 {
		return image.Point{}
	}
	return image.Pt(
		int(math.Round(dispX*(float64(dims.X)/stageW))),
		int(math.Round(dispY*(float64(dims.Y)/stageH))),
	)
}

func displayToImageFloat(dispX, dispY float64, dims image.Point, stageW, stageH float64) (float64, float64) {
	if dims.X <= 0 || dims.Y <= 0 || stageW <= 0 || stageH <= 0 {
		return 0, 0
	}
	return dispX * (float64(dims.X) / stageW), dispY * (float64(dims.Y) / stageH)
}

func computeDiscShift(
	screenDx, screenDy float64,
	sourceDims image.Point,
	discRadius int,
	discRotation float64,
	discCenter image.Point,
	ctrlStartCenter image.Point,
	stageW, stageH float64,
	naturalDims image.Point,
	maxShift float64,
) (discShiftResult, bool) {
	if sourceDims.X <= 0 || sourceDims.Y <= 0 || discRadius <= 0 || stageW <= 0 || stageH <= 0 {
		return discShiftResult{}, false
	}
	src := naturalDims
	if src.X <= 0 || src.Y <= 0 {
		src = sourceDims
	}
	scaleX := float64(src.X) / stageW
	scaleY := float64(src.Y) / stageH

	rotatedX := screenDx * scaleX
	rotatedY := screenDy * scaleY
	rad := discRotation * math.Pi / 180
	cos := math.Cos(rad)
	sin := math.Sin(rad)

	desiredImgDx := -(rotatedX*cos + rotatedY*sin)
	desiredImgDy := -(-rotatedX*sin + rotatedY*cos)
	if mag := math.Hypot(desiredImgDx, desiredImgDy); maxShift > 0 && mag > maxShift {
		desiredImgDx *= maxShift / mag
		desiredImgDy *= maxShift / mag
	}

	startCenter := ctrlStartCenter
	if startCenter == (image.Point{}) {
		startCenter = discCenter
	}
	minCenterX := float64(discRadius)
	maxCenterX := math.Max(minCenterX, float64(sourceDims.X-discRadius))
	minCenterY := float64(discRadius)
	maxCenterY := math.Max(minCenterY, float64(sourceDims.Y-discRadius))

	clampedX := clampF64(float64(startCenter.X)+desiredImgDx, minCenterX, maxCenterX)
	clampedY := clampF64(float64(startCenter.Y)+desiredImgDy, minCenterY, maxCenterY)
	appliedDx := clampedX - float64(startCenter.X)
	appliedDy := clampedY - float64(startCenter.Y)

	roundedDx := int(math.Round(appliedDx))
	roundedDy := int(math.Round(appliedDy))

	previewX := -(cos*float64(roundedDx) - sin*float64(roundedDy))
	previewY := -(sin*float64(roundedDx) + cos*float64(roundedDy))

	return discShiftResult{
		dx:     roundedDx,
		dy:     roundedDy,
		liveDx: float32(math.Round(previewX / scaleX)),
		liveDy: float32(math.Round(previewY / scaleY)),
	}, true
}

func lockSquareDrag(start, current f32.Point, width, height float32) f32.Point {
	dx := current.X - start.X
	dy := current.Y - start.Y
	side := float32(math.Max(math.Abs(float64(dx)), math.Abs(float64(dy))))
	sx := float32(1)
	if dx < 0 {
		sx = -1
	}
	sy := float32(1)
	if dy < 0 || (dy == 0 && sx < 0) {
		sy = -1
	}
	return f32.Pt(
		float32(clampF64(float64(start.X+side*sx), 0, float64(width))),
		float32(clampF64(float64(start.Y+side*sy), 0, float64(height))),
	)
}

func isPointInRectI(r rectI, pt image.Point) bool {
	nr := normalizedRectI(r)
	return pt.X >= nr.X1 && pt.X <= nr.X2 && pt.Y >= nr.Y1 && pt.Y <= nr.Y2
}

func hitTestRectHandle(r *rectI, dispPos f32.Point, info viewportLayoutInfo, dims image.Point, cornerRadPx, edgeHalfPx float32) string {
	if r == nil || dims.X <= 0 || dims.Y <= 0 || info.StageWidth <= 0 || info.StageHeight <= 0 {
		return ""
	}
	// Handles sit clipped to image bounds.
	if dispPos.X < 0 || dispPos.Y < 0 || float64(dispPos.X) > info.StageWidth || float64(dispPos.Y) > info.StageHeight {
		return ""
	}
	nr := normalizedRectI(*r)
	sx := float32(info.StageWidth) / float32(dims.X)
	sy := float32(info.StageHeight) / float32(dims.Y)
	l := float32(nr.X1) * sx
	t := float32(nr.Y1) * sy
	ri := float32(nr.X2) * sx
	b := float32(nr.Y2) * sy

	corners := []struct {
		name string
		x, y float32
	}{
		{"nw", l, t},
		{"ne", ri, t},
		{"se", ri, b},
		{"sw", l, b},
	}
	for _, c := range corners {
		if float32(math.Hypot(float64(dispPos.X-c.x), float64(dispPos.Y-c.y))) <= cornerRadPx {
			return c.name
		}
	}
	if dispPos.X >= l && dispPos.X <= ri {
		if float32(math.Abs(float64(dispPos.Y-t))) <= edgeHalfPx {
			return "n"
		}
		if float32(math.Abs(float64(dispPos.Y-b))) <= edgeHalfPx {
			return "s"
		}
	}
	if dispPos.Y >= t && dispPos.Y <= b {
		if float32(math.Abs(float64(dispPos.X-l))) <= edgeHalfPx {
			return "w"
		}
		if float32(math.Abs(float64(dispPos.X-ri))) <= edgeHalfPx {
			return "e"
		}
	}
	return ""
}

func cursorForRectHandle(h string) pointer.Cursor {
	switch h {
	case "nw", "se":
		return pointer.CursorNorthWestSouthEastResize
	case "ne", "sw":
		return pointer.CursorNorthEastSouthWestResize
	case "n", "s":
		return pointer.CursorNorthSouthResize
	case "e", "w":
		return pointer.CursorEastWestResize
	default:
		return pointer.CursorCrosshair
	}
}

func hitTestLineHandle(lines []lineI, dispPos f32.Point, info viewportLayoutInfo, dims image.Point, radPx float32) *lineHandleDragState {
	if len(lines) == 0 || dims.X <= 0 || dims.Y <= 0 || info.StageWidth <= 0 || info.StageHeight <= 0 {
		return nil
	}
	if dispPos.X < 0 || dispPos.Y < 0 || float64(dispPos.X) > info.StageWidth || float64(dispPos.Y) > info.StageHeight {
		return nil
	}
	sx := float32(info.StageWidth) / float32(dims.X)
	sy := float32(info.StageHeight) / float32(dims.Y)
	for i := len(lines) - 1; i >= 0; i-- {
		ln := lines[i]
		if float32(math.Hypot(float64(dispPos.X-float32(ln.X1)*sx), float64(dispPos.Y-float32(ln.Y1)*sy))) <= radPx {
			return &lineHandleDragState{index: i, end: "start"}
		}
		if float32(math.Hypot(float64(dispPos.X-float32(ln.X2)*sx), float64(dispPos.Y-float32(ln.Y2)*sy))) <= radPx {
			return &lineHandleDragState{index: i, end: "end"}
		}
	}
	return nil
}

func updateRectFromHandle(r *rectI, handle string, imgPt image.Point, dims image.Point) *rectI {
	if r == nil || handle == "" {
		return r
	}
	nr := normalizedRectI(*r)
	x := clamp(imgPt.X, 0, max(0, dims.X))
	y := clamp(imgPt.Y, 0, max(0, dims.Y))
	switch handle {
	case "nw":
		nr.X1, nr.Y1 = x, y
	case "ne":
		nr.X2, nr.Y1 = x, y
	case "se":
		nr.X2, nr.Y2 = x, y
	case "sw":
		nr.X1, nr.Y2 = x, y
	case "n":
		nr.Y1 = y
	case "e":
		nr.X2 = x
	case "s":
		nr.Y2 = y
	case "w":
		nr.X1 = x
	}
	return &nr
}

func (e *EditorUI) adjustmentToolReady() bool {
	return e.adjustmentSelectionActive && !e.useTouchupTool && !e.useStraightEdgeTool
}

func (e *EditorUI) resolveCanvasCursor(gtx layout.Context) pointer.Cursor {
	if e.spacePanMode || e.panDrag != nil {
		if e.panDrag != nil {
			return pointer.CursorGrabbing
		}
		return pointer.CursorGrab
	}
	if !e.imageLoaded {
		return pointer.CursorDefault
	}
	info := e.mainViewport.LastLayout
	if e.normalDragKind == "move" || e.adjustmentDragKind == "move" || e.lineHandleDrag != nil {
		return pointer.CursorAllScroll
	}
	if e.adjustmentHandleDrag != "" {
		return cursorForRectHandle(e.adjustmentHandleDrag)
	}
	if e.normalHandleDrag != "" {
		return cursorForRectHandle(e.normalHandleDrag)
	}
	if e.hoverDispPos != nil {
		dp := *e.hoverDispPos
		cornerRad := float32(gtx.Dp(10))
		edgeHalf := float32(gtx.Dp(8))
		if e.adjustmentToolReady() && e.adjustmentRect != nil {
			if h := hitTestRectHandle(e.adjustmentRect, dp, info, e.realImageDims, cornerRad, edgeHalf); h != "" {
				return cursorForRectHandle(h)
			}
			imgPt := displayToImage(float64(dp.X), float64(dp.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			if isPointInRectI(*e.adjustmentRect, imgPt) {
				return pointer.CursorAllScroll
			}
		}
		if e.mode == "normal" && !e.useTouchupTool && !e.adjustmentToolReady() && e.normalRect != nil {
			if h := hitTestRectHandle(e.normalRect, dp, info, e.realImageDims, cornerRad, edgeHalf); h != "" {
				return cursorForRectHandle(h)
			}
			imgPt := displayToImage(float64(dp.X), float64(dp.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			if isPointInRectI(*e.normalRect, imgPt) {
				return pointer.CursorAllScroll
			}
		}
		if e.mode == "line" && !e.useTouchupTool && !e.linesProcessed {
			if lh := hitTestLineHandle(e.lines, dp, info, e.realImageDims, cornerRad); lh != nil {
				return pointer.CursorAllScroll
			}
		}
	}
	return pointer.CursorCrosshair
}

func (e *EditorUI) handleCanvasPointer(gtx layout.Context, info viewportLayoutInfo, pe pointer.Event) {
	if !e.imageLoaded {
		return
	}
	dispPos := f32.Pt(pe.Position.X-float32(info.StageX), pe.Position.Y-float32(info.StageY))
	insideImage := dispPos.X >= 0 && dispPos.Y >= 0 && float64(dispPos.X) <= info.StageWidth && float64(dispPos.Y) <= info.StageHeight
	e.mousePos = dispPos
	if pe.Kind == pointer.Leave {
		e.hoverDispPos = nil
		if e.useTouchupTool && e.touchupResize == nil {
			e.touchupCursor = nil
		}
		return
	}
	dp := dispPos
	e.hoverDispPos = &dp

	updateTouchupCursor := func(resizing bool) {
		if !e.useTouchupTool {
			return
		}
		if !insideImage {
			if !resizing {
				e.touchupCursor = nil
			}
			return
		}
		ix, iy := displayToImageFloat(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
		e.touchupCursor = &touchupCursorState{X: ix, Y: iy, Resizing: resizing}
	}

	resizeBrush := func(clientX float32) {
		if e.touchupResize == nil {
			return
		}
		dxDp := float64(clientX-e.touchupResize.startX) / float64(gtx.Metric.PxPerDp)
		next := math.Round(e.touchupResize.startSize + dxDp)
		e.brushSize = clampF64(next, touchupBrushMin, touchupBrushMax)
	}

	switch pe.Kind {
	case pointer.Press:
		// Space+Drag pan works even while busy.
		if e.spaceDown && pe.Buttons.Contain(pointer.ButtonPrimary) {
			gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
			e.panDrag = &panDragState{
				startX:    pe.Position.X,
				startY:    pe.Position.Y,
				startLeft: e.mainViewport.ScrollX,
				startTop:  e.mainViewport.ScrollY,
			}
			return
		}
		if e.isBusy() {
			return
		}
		if e.useTouchupTool && pe.Buttons.Contain(pointer.ButtonSecondary) && pe.Modifiers.Contain(key.ModAlt) && insideImage {
			gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
			e.touchupResize = &touchupResizeDragState{
				startX:    pe.Position.X,
				startSize: clampF64(e.brushSize, touchupBrushMin, touchupBrushMax),
			}
			updateTouchupCursor(true)
			return
		}
		if !pe.Buttons.Contain(pointer.ButtonPrimary) {
			return
		}

		cornerRad := float32(gtx.Dp(10))
		edgeHalf := float32(gtx.Dp(8))

		if e.adjustmentToolReady() && e.adjustmentRect != nil {
			if h := hitTestRectHandle(e.adjustmentRect, dispPos, info, e.realImageDims, cornerRad, edgeHalf); h != "" {
				gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
				e.adjustmentHandleDrag = h
				e.adjustmentDragKind = "resize"
				e.dragging = true
				e.dragStart = &dp
				e.dragCurrent = &dp
				return
			}
		}
		if e.mode == "normal" && !e.useTouchupTool && !e.adjustmentToolReady() && e.normalRect != nil {
			if h := hitTestRectHandle(e.normalRect, dispPos, info, e.realImageDims, cornerRad, edgeHalf); h != "" {
				gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
				e.normalHandleDrag = h
				e.normalDragKind = "resize"
				e.dragging = true
				e.dragStart = &dp
				e.dragCurrent = &dp
				return
			}
		}
		if e.mode == "line" && !e.useTouchupTool && !e.linesProcessed {
			if lh := hitTestLineHandle(e.lines, dispPos, info, e.realImageDims, cornerRad); lh != nil {
				gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
				e.lineHandleDrag = lh
				e.lineDragKind = "edit"
				e.dragging = true
				e.dragStart = &dp
				e.dragCurrent = &dp
				return
			}
		}

		if !insideImage {
			if e.adjustmentToolReady() {
				gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
				e.adjustmentDragKind = "draw"
				e.adjustmentRect = nil
				e.adjustmentDragPending = true
			} else if e.mode == "normal" && !e.useTouchupTool {
				gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
				e.normalDragKind = "draw"
				e.normalRect = nil
				e.normalDragPending = true
			}
			return
		}

		if e.adjustmentToolReady() && e.adjustmentRect != nil {
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			if isPointInRectI(*e.adjustmentRect, imgPt) {
				gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
				e.adjustmentMoveDrag = &rectMoveDragState{
					startImg:  imgPt,
					startRect: normalizedRectI(*e.adjustmentRect),
				}
				e.adjustmentDragKind = "move"
				e.dragging = true
				e.dragStart = &dp
				e.dragCurrent = &dp
				return
			}
		}
		if e.adjustmentToolReady() {
			gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
			e.adjustmentDragKind = "draw"
			e.adjustmentRect = nil
			e.adjustmentDragActive = true
			e.dragging = true
			e.dragStart = &dp
			e.dragCurrent = &dp
			return
		}

		if e.mode == "normal" && !e.useTouchupTool && e.normalRect != nil {
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			if isPointInRectI(*e.normalRect, imgPt) {
				gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
				e.normalMoveDrag = &rectMoveDragState{
					startImg:  imgPt,
					startRect: normalizedRectI(*e.normalRect),
				}
				e.normalDragKind = "move"
				e.dragging = true
				e.dragStart = &dp
				e.dragCurrent = &dp
				return
			}
		}

		if e.mode == "corner" && !e.useTouchupTool {
			gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
			e.cornerMouseDown = true
			return
		}

		if e.useTouchupTool {
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			if e.adjustmentRect != nil && !isPointInRectI(*e.adjustmentRect, imgPt) {
				return
			}
			gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
			e.touchupDragging = true
			e.touchupStrokes = []TouchUpPoint{{X: float64(imgPt.X), Y: float64(imgPt.Y)}}
			e.dragging = true
			e.dragStart = &dp
			e.dragCurrent = &dp
			return
		}

		if e.useStraightEdgeTool && e.mode == "disc" && e.discActive {
			gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
			e.dragging = true
			e.dragStart = &dp
			e.dragCurrent = &dp
			return
		}

		if e.mode == "disc" && e.discActive && pe.Modifiers.Contain(key.ModShift) {
			gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
			e.ctrlDrag = nil
			e.shiftDrag = &discRotateDragState{rebase: true}
			e.dragging = true
			e.dragStart = &dp
			e.dragCurrent = &dp
			e.discLiveActive = true
			e.discLiveTransform = discLiveTransform{}
			e.liveGesture = discLivePose{}
			e.startDiscLive(info)
			e.updateDiscRotateDrag(gtx, info, pe.Position)
			return
		}

		if e.mode == "disc" && e.discActive && pe.Modifiers.Contain(key.ModCtrl) {
			gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
			e.shiftDrag = nil
			e.ctrlDrag = &discCtrlDragState{
				startClientX: pe.Position.X,
				startClientY: pe.Position.Y,
				startCenter:  e.discCenter,
				stageW:       info.StageWidth,
				stageH:       info.StageHeight,
				natDims:      info.StageDims,
				maxShift:     discShiftMaxShift(info),
			}
			e.dragging = true
			e.dragStart = &dp
			e.dragCurrent = &dp
			e.discLiveActive = true
			e.discLiveTransform = discLiveTransform{}
			e.liveGesture = discLivePose{}
			e.startDiscLive(info)
			return
		}

		if e.mode == "disc" && e.discActive {
			return
		}
		if e.mode == "line" && e.linesProcessed {
			return
		}

		gtx.Execute(pointer.GrabCmd{Tag: &e.mainViewport.canvasTag, ID: pe.PointerID})
		e.ctrlDrag = nil
		e.shiftDrag = nil
		if e.mode == "normal" && !e.useTouchupTool {
			e.normalDragKind = "draw"
			e.normalRect = nil
		}
		if e.mode == "line" {
			e.lineDragKind = "draw"
			pt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			e.lineStartImg = &pt
		}
		e.dragging = true
		e.dragStart = &dp
		e.dragCurrent = &dp
		if e.mode == "disc" && e.app != nil {
			e.lastDiscDragLogAt = time.Time{}
			e.app.Logf(
				"disc Press: pePos=(%.1f,%.1f) dispPos=(%.1f,%.1f) stage=(%.1f,%.1f %.1fx%.1f) vp=(%dx%d) win=(%dx%d) sidebarPx=%d pxPerDp=%.3f zoom=%.3f scroll=(%.1f,%.1f) realDims=%dx%d stageDims=%dx%d",
				pe.Position.X, pe.Position.Y, dispPos.X, dispPos.Y,
				info.StageX, info.StageY, info.StageWidth, info.StageHeight,
				info.ViewportW, info.ViewportH, e.lastWindowSz.X, e.lastWindowSz.Y,
				gtx.Dp(unit.Dp(e.sidebarWidthDp)), gtx.Metric.PxPerDp,
				e.mainViewport.Zoom, e.mainViewport.ScrollX, e.mainViewport.ScrollY,
				e.realImageDims.X, e.realImageDims.Y, info.StageDims.X, info.StageDims.Y,
			)
		}

	case pointer.Move, pointer.Drag:
		if e.touchupResize != nil {
			updateTouchupCursor(true)
			resizeBrush(pe.Position.X)
			return
		}
		if e.useTouchupTool {
			updateTouchupCursor(false)
		}
		if e.panDrag != nil {
			maxSX := math.Max(0, info.ContentWidth-float64(info.ViewportW))
			maxSY := math.Max(0, info.ContentHeight-float64(info.ViewportH))
			e.mainViewport.ScrollX = clampF64(e.panDrag.startLeft-float64(pe.Position.X-e.panDrag.startX), 0, maxSX)
			e.mainViewport.ScrollY = clampF64(e.panDrag.startTop-float64(pe.Position.Y-e.panDrag.startY), 0, maxSY)
			return
		}

		if e.adjustmentMoveDrag != nil {
			c := dp
			e.dragCurrent = &c
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			st := e.adjustmentMoveDrag.startRect
			w := max(0, st.X2-st.X1)
			h := max(0, st.Y2-st.Y1)
			dx := clamp(imgPt.X-e.adjustmentMoveDrag.startImg.X, -st.X1, max(0, e.realImageDims.X-st.X2))
			dy := clamp(imgPt.Y-e.adjustmentMoveDrag.startImg.Y, -st.Y1, max(0, e.realImageDims.Y-st.Y2))
			e.adjustmentRect = &rectI{X1: st.X1 + dx, Y1: st.Y1 + dy, X2: st.X1 + dx + w, Y2: st.Y1 + dy + h}
			return
		}
		if e.adjustmentHandleDrag != "" {
			c := dp
			e.dragCurrent = &c
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			e.adjustmentRect = updateRectFromHandle(e.adjustmentRect, e.adjustmentHandleDrag, imgPt, e.realImageDims)
			return
		}
		if e.adjustmentDragPending {
			if e.adjustmentToolReady() && insideImage {
				e.adjustmentDragPending = false
				e.adjustmentDragActive = true
				e.dragging = true
				s, c := dp, dp
				e.dragStart = &s
				e.dragCurrent = &c
			}
			return
		}

		if e.lineHandleDrag != nil {
			c := dp
			e.dragCurrent = &c
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			imgPt.X = clamp(imgPt.X, 0, max(0, e.realImageDims.X))
			imgPt.Y = clamp(imgPt.Y, 0, max(0, e.realImageDims.Y))
			idx := e.lineHandleDrag.index
			if idx >= 0 && idx < len(e.lines) {
				if e.lineHandleDrag.end == "start" {
					e.lines[idx].X1, e.lines[idx].Y1 = imgPt.X, imgPt.Y
				} else {
					e.lines[idx].X2, e.lines[idx].Y2 = imgPt.X, imgPt.Y
				}
			}
			return
		}

		if e.normalMoveDrag != nil {
			c := dp
			e.dragCurrent = &c
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			st := e.normalMoveDrag.startRect
			w := max(0, st.X2-st.X1)
			h := max(0, st.Y2-st.Y1)
			dx := clamp(imgPt.X-e.normalMoveDrag.startImg.X, -st.X1, max(0, e.realImageDims.X-st.X2))
			dy := clamp(imgPt.Y-e.normalMoveDrag.startImg.Y, -st.Y1, max(0, e.realImageDims.Y-st.Y2))
			e.normalRect = &rectI{X1: st.X1 + dx, Y1: st.Y1 + dy, X2: st.X1 + dx + w, Y2: st.Y1 + dy + h}
			return
		}
		if e.normalHandleDrag != "" {
			c := dp
			e.dragCurrent = &c
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			e.normalRect = updateRectFromHandle(e.normalRect, e.normalHandleDrag, imgPt, e.realImageDims)
			return
		}
		if e.normalDragPending {
			if e.mode == "normal" && !e.useTouchupTool && insideImage {
				e.normalDragPending = false
				e.ctrlDrag = nil
				e.shiftDrag = nil
				e.dragging = true
				s, c := dp, dp
				e.dragStart = &s
				e.dragCurrent = &c
			}
			return
		}

		if !e.dragging {
			return
		}
		if e.adjustmentToolReady() || (e.mode == "normal" && !e.useTouchupTool) {
			clamped := f32.Pt(
				float32(clampF64(float64(dispPos.X), 0, info.StageWidth)),
				float32(clampF64(float64(dispPos.Y), 0, info.StageHeight)),
			)
			lockSquare := pe.Modifiers.Contain(key.ModShift) && e.dragStart != nil &&
				((e.adjustmentToolReady() && e.adjustmentRect == nil) || (!e.adjustmentToolReady() && e.normalMoveDrag == nil && e.normalHandleDrag == "" && e.normalRect == nil))
			if lockSquare {
				sq := lockSquareDrag(*e.dragStart, clamped, float32(info.StageWidth), float32(info.StageHeight))
				e.dragCurrent = &sq
			} else {
				e.dragCurrent = &clamped
			}
		} else {
			c := dp
			e.dragCurrent = &c
			if e.mode == "disc" && !e.discActive && !e.useTouchupTool && !e.useStraightEdgeTool && !e.adjustmentToolReady() && e.dragStart != nil && e.app != nil {
				if e.lastDiscDragLogAt.IsZero() || time.Since(e.lastDiscDragLogAt) >= 80*time.Millisecond {
					e.lastDiscDragLogAt = time.Now()
					e.app.Logf(
						"disc Drag: kind=%v pePos=(%.1f,%.1f) dispPos=(%.1f,%.1f) dragStart=(%.1f,%.1f) dragCurrent=(%.1f,%.1f) stage=(%.1f,%.1f %.1fx%.1f) pxPerDp=%.3f",
						pe.Kind, pe.Position.X, pe.Position.Y, dispPos.X, dispPos.Y,
						e.dragStart.X, e.dragStart.Y, e.dragCurrent.X, e.dragCurrent.Y,
						info.StageX, info.StageY, info.StageWidth, info.StageHeight, gtx.Metric.PxPerDp,
					)
				}
			}
		}

		if e.useTouchupTool && e.touchupDragging {
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			if len(e.touchupStrokes) == 0 {
				e.touchupStrokes = append(e.touchupStrokes, TouchUpPoint{X: float64(imgPt.X), Y: float64(imgPt.Y)})
			} else {
				last := e.touchupStrokes[len(e.touchupStrokes)-1]
				if math.Hypot(last.X-float64(imgPt.X), last.Y-float64(imgPt.Y)) >= 3 {
					e.touchupStrokes = append(e.touchupStrokes, TouchUpPoint{X: float64(imgPt.X), Y: float64(imgPt.Y)})
				}
			}
			return
		}
		if e.useStraightEdgeTool {
			return
		}
		if e.shiftDrag != nil {
			e.updateDiscRotateDrag(gtx, info, pe.Position)
			return
		}
		if e.ctrlDrag != nil {
			if shift, ok := e.discCtrlDragShift(pe.Position); ok {
				e.discLiveTransform.Dx = shift.liveDx
				e.discLiveTransform.Dy = shift.liveDy
				if l := e.discShiftLive; l != nil && l.dragging {
					l.gesture = discLivePose{shift: image.Pt(shift.dx, shift.dy)}
				}
			}
			return
		}

	case pointer.Release, pointer.Cancel:
		if e.touchupResize != nil {
			if pe.Kind == pointer.Release {
				resizeBrush(pe.Position.X)
			}
			e.touchupResize = nil
			updateTouchupCursor(false)
			return
		}
		if e.panDrag != nil {
			e.panDrag = nil
			return
		}
		if pe.Kind == pointer.Cancel {
			e.cancelActiveCanvasDrag()
			return
		}
		if e.isBusy() {
			e.cancelActiveCanvasDrag()
			return
		}

		if e.mode == "line" && e.lineHandleDrag != nil {
			e.lineHandleDrag = nil
			e.lineDragKind = "none"
			e.dragging = false
			e.dragStart = nil
			e.dragCurrent = nil
			e.syncBackendLines()
			return
		}

		if e.adjustmentDragPending {
			e.adjustmentDragPending = false
			e.adjustmentRect = nil
			e.adjustmentDragKind = "none"
			return
		}
		if e.adjustmentMoveDrag != nil || e.adjustmentHandleDrag != "" {
			e.adjustmentMoveDrag = nil
			e.adjustmentHandleDrag = ""
			e.adjustmentDragActive = false
			e.adjustmentDragKind = "none"
			e.dragging = false
			e.dragStart = nil
			e.dragCurrent = nil
			return
		}
		if e.adjustmentToolReady() && (e.dragging || e.adjustmentDragActive) {
			e.adjustmentDragActive = false
			e.adjustmentDragKind = "none"
			e.dragging = false
			if e.dragStart != nil && e.dragCurrent != nil {
				start := displayToImage(float64(e.dragStart.X), float64(e.dragStart.Y), e.realImageDims, info.StageWidth, info.StageHeight)
				end := displayToImage(float64(e.dragCurrent.X), float64(e.dragCurrent.Y), e.realImageDims, info.StageWidth, info.StageHeight)
				if absInt(end.X-start.X) >= 5 && absInt(end.Y-start.Y) >= 5 {
					e.adjustmentRect = &rectI{X1: start.X, Y1: start.Y, X2: end.X, Y2: end.Y}
				} else {
					e.adjustmentRect = nil
				}
			}
			e.dragStart = nil
			e.dragCurrent = nil
			return
		}

		if e.normalDragPending {
			e.normalDragPending = false
			if e.mode == "normal" && !e.useTouchupTool {
				e.normalRect = nil
				e.normalDragKind = "none"
			}
			return
		}

		if e.useTouchupTool && e.touchupDragging {
			e.touchupDragging = false
			e.dragging = false
			e.dragStart = nil
			e.dragCurrent = nil
			if len(e.touchupStrokes) > 0 {
				e.commitTouchup()
			}
			return
		}

		if e.mode == "normal" && !e.useTouchupTool {
			if e.normalMoveDrag != nil || e.normalHandleDrag != "" {
				e.normalMoveDrag = nil
				e.normalHandleDrag = ""
				e.normalDragKind = "none"
				e.dragging = false
				e.dragStart = nil
				e.dragCurrent = nil
				return
			}
			e.normalDragKind = "none"
			wasDragging := e.dragging
			e.dragging = false
			if wasDragging && e.dragStart != nil && e.dragCurrent != nil {
				start := displayToImage(float64(e.dragStart.X), float64(e.dragStart.Y), e.realImageDims, info.StageWidth, info.StageHeight)
				end := displayToImage(float64(e.dragCurrent.X), float64(e.dragCurrent.Y), e.realImageDims, info.StageWidth, info.StageHeight)
				if absInt(end.X-start.X) >= 5 && absInt(end.Y-start.Y) >= 5 {
					e.normalRect = &rectI{X1: start.X, Y1: start.Y, X2: end.X, Y2: end.Y}
				} else {
					e.normalRect = nil
				}
			}
			e.dragStart = nil
			e.dragCurrent = nil
			return
		}

		if e.mode == "corner" && !e.useTouchupTool {
			hadDown := e.cornerMouseDown
			e.cornerMouseDown = false
			if !hadDown || !insideImage {
				return
			}
			if time.Since(e.lastResizeAt) < 300*time.Millisecond {
				return
			}
			isCustom := e.customCorner || pe.Modifiers.Contain(key.ModCtrl)
			if (e.cornerCount == 0 && !e.cornersDetected && !isCustom) || e.cornerCount >= 4 {
				return
			}
			imgPt := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			e.handleCornerClick(imgPt, isCustom)
			return
		}

		if !e.dragging || e.dragStart == nil {
			e.dragging = false
			return
		}
		e.dragging = false

		if e.useStraightEdgeTool && e.mode == "disc" && e.discActive {
			if !insideImage {
				e.dragStart = nil
				e.dragCurrent = nil
				return
			}
			dx := float64(dispPos.X - e.dragStart.X)
			dy := float64(dispPos.Y - e.dragStart.Y)
			e.dragStart = nil
			e.dragCurrent = nil
			if math.Hypot(dx, dy) >= 5 {
				angleDeg := math.Atan2(dy, dx) * 180 / math.Pi
				e.applyStraightEdgeRotate(angleDeg)
			}
			if !e.settings.StraightEdgeRemainsActive {
				e.useStraightEdgeTool = false
			}
			return
		}

		if e.mode == "disc" && e.shiftDrag != nil && e.discActive {
			e.updateDiscRotateDrag(gtx, info, pe.Position)
			angle := e.shiftDrag.applied
			if math.Abs(angle) < 0.01 {
				angle = 0
			}
			e.shiftDrag = nil
			e.dragStart = nil
			e.dragCurrent = nil
			// As with shifts, the live layer keeps showing until the
			// committed render is presented.
			e.releaseDiscLiveGesture(discLivePose{angle: angle})
			if angle != 0 {
				e.commitDiscRotateDrag(angle)
			} else {
				e.discLiveActive = false
				e.discLiveTransform = discLiveTransform{}
			}
			return
		}

		if e.mode == "disc" && e.ctrlDrag != nil && e.discActive {
			shift, ok := e.discCtrlDragShift(pe.Position)
			e.ctrlDrag = nil
			e.dragStart = nil
			e.dragCurrent = nil
			if !ok {
				shift = discShiftResult{}
			}
			// The live layer (if any) keeps showing, now including this
			// shift, until the committed render is presented.
			e.releaseDiscLiveGesture(discLivePose{shift: image.Pt(shift.dx, shift.dy)})
			if shift.dx != 0 || shift.dy != 0 {
				e.commitDiscShiftDrag(shift.dx, shift.dy)
			} else {
				e.discLiveActive = false
				e.discLiveTransform = discLiveTransform{}
			}
			return
		}

		if e.mode == "disc" {
			if !insideImage {
				if e.app != nil {
					e.app.Logf(
						"disc Release (outside): pePos=(%.1f,%.1f) dispPos=(%.1f,%.1f) dragStart=(%.1f,%.1f) stage=(%.1f,%.1f %.1fx%.1f)",
						pe.Position.X, pe.Position.Y, dispPos.X, dispPos.Y,
						e.dragStart.X, e.dragStart.Y,
						info.StageX, info.StageY, info.StageWidth, info.StageHeight,
					)
				}
				e.dragStart = nil
				e.dragCurrent = nil
				return
			}
			start := displayToImage(float64(e.dragStart.X), float64(e.dragStart.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			end := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			radius := int(math.Round(math.Hypot(float64(start.X-end.X), float64(start.Y-end.Y))))
			if e.app != nil {
				curStr := "nil"
				if e.dragCurrent != nil {
					curStr = fmt.Sprintf("(%.1f,%.1f)", e.dragCurrent.X, e.dragCurrent.Y)
				}
				e.app.Logf(
					"disc Release: pePos=(%.1f,%.1f) dispPos=(%.1f,%.1f) dragStart=(%.1f,%.1f) dragCurrent=%s stage=(%.1f,%.1f %.1fx%.1f) vp=(%dx%d) pxPerDp=%.3f realDims=%dx%d startImg=(%d,%d) endImg=(%d,%d) radiusImg=%d",
					pe.Position.X, pe.Position.Y, dispPos.X, dispPos.Y,
					e.dragStart.X, e.dragStart.Y, curStr,
					info.StageX, info.StageY, info.StageWidth, info.StageHeight,
					info.ViewportW, info.ViewportH, gtx.Metric.PxPerDp,
					e.realImageDims.X, e.realImageDims.Y, start.X, start.Y, end.X, end.Y, radius,
				)
			}
			e.dragStart = nil
			e.dragCurrent = nil
			if radius < 5 {
				return
			}
			e.applyDrawDisc(end.X, end.Y, radius)
			return
		}

		if e.mode == "line" && !e.linesProcessed {
			start := e.lineStartImg
			e.lineStartImg = nil
			e.dragStart = nil
			e.dragCurrent = nil
			if start == nil || !insideImage {
				e.lineDragKind = "none"
				return
			}
			end := displayToImage(float64(dispPos.X), float64(dispPos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			if math.Hypot(float64(end.X-start.X), float64(end.Y-start.Y)) < 5 {
				e.lineDragKind = "none"
				return
			}
			e.applyAddLine(*start, end)
			return
		}

		e.dragStart = nil
		e.dragCurrent = nil
	}
}

func (e *EditorUI) cancelActiveCanvasDrag() {
	e.cornerMouseDown = false
	e.touchupDragging = false
	e.dragging = false
	e.dragStart = nil
	e.dragCurrent = nil
	e.normalDragPending = false
	e.normalHandleDrag = ""
	e.normalMoveDrag = nil
	e.normalDragKind = "none"
	e.adjustmentDragPending = false
	e.adjustmentDragActive = false
	e.adjustmentHandleDrag = ""
	e.adjustmentMoveDrag = nil
	e.adjustmentDragKind = "none"
	e.lineHandleDrag = nil
	e.lineDragKind = "none"
	e.lineStartImg = nil
	e.ctrlDrag = nil
	e.shiftDrag = nil
	e.discLiveActive = false
	e.discLiveTransform = discLiveTransform{}
	e.cancelDiscLiveGesture()
}

// discCtrlDragShift converts the pointer position of the active Ctrl+drag
// into a disc-centre shift, bounded by the live layer's coverage.
func (e *EditorUI) discCtrlDragShift(pos f32.Point) (discShiftResult, bool) {
	ctrl := e.ctrlDrag
	if ctrl == nil {
		return discShiftResult{}, false
	}
	srcDims := e.discSourceDims
	if srcDims.X <= 0 || srcDims.Y <= 0 {
		srcDims = e.realImageDims
	}
	natDims := ctrl.natDims
	if l := e.discShiftLive; l != nil && l.dragging && l.geo.Width > 0 && l.geo.Height > 0 {
		// The render may carry an output scale (Resize); map the pointer in
		// unscaled render pixels, which is what a disc shift moves.
		natDims = image.Pt(l.geo.Width, l.geo.Height)
	}
	shift, ok := computeDiscShift(float64(pos.X-ctrl.startClientX), float64(pos.Y-ctrl.startClientY), srcDims,
		e.discRadius, e.liveDiscRotation(), e.discCenter, ctrl.startCenter, ctrl.stageW, ctrl.stageH, natDims, ctrl.maxShift)
	if ok {
		e.liveGesture = discLivePose{shift: image.Pt(shift.dx, shift.dy)}
		if l := e.discShiftLive; l != nil && l.dragging {
			g := l.limitGesture(e.liveGesture)
			shift.dx, shift.dy = g.shift.X, g.shift.Y
		}
	}
	return shift, ok
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
