package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type btnVariant int

const (
	btnPrimary btnVariant = iota
	btnAdjust
	btnLoad
	btnSave
	btnSkip
	btnRecrop
	btnDanger
	btnSecondary
	btnModeTab
	btnOptionsTab
	btnGhost
	btnCalibrationFile
)

type overlayManager struct {
	drawFuncs   []func(layout.Context)
	mouseTag    uint8
	mousePos    f32.Point
	hasMousePos bool
	activeMenu  *uiDropdown
}

func (om *overlayManager) beginFrame(gtx layout.Context) {
	om.drawFuncs = om.drawFuncs[:0]
}

func (om *overlayManager) deferDraw(fn func(layout.Context)) {
	if fn != nil {
		om.drawFuncs = append(om.drawFuncs, fn)
	}
}

func (om *overlayManager) layoutOverlayPass(gtx layout.Context) {
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &om.mouseTag)
	area.Pop()
	pass.Pop()

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &om.mouseTag,
			Kinds:  pointer.Move | pointer.Drag | pointer.Press | pointer.Release | pointer.Enter,
		})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			om.mousePos = pe.Position
			om.hasMousePos = true
		}
	}

	for _, fn := range om.drawFuncs {
		fn(gtx)
	}
}

type tooltipTag struct {
	id string
}

type tooltipManager struct {
	tags         map[string]*tooltipTag
	hoveredID    string
	hoveredHint  string
	hoverStart   time.Time
	suppressed   bool
	seenThisFrm  bool
	overlays     *overlayManager
	activeBefore bool
}

func newTooltipManager(overlays *overlayManager) *tooltipManager {
	return &tooltipManager{
		tags:     make(map[string]*tooltipTag),
		overlays: overlays,
	}
}

func (tm *tooltipManager) beginFrame() {
	tm.seenThisFrm = false
}

func (tm *tooltipManager) endFrame(gtx layout.Context, th *material.Theme) {
	if !tm.seenThisFrm {
		tm.hoveredID = ""
		tm.hoveredHint = ""
		tm.suppressed = false
		return
	}
	if tm.hoveredID == "" || tm.hoveredHint == "" || tm.suppressed {
		return
	}
	elapsed := gtx.Now.Sub(tm.hoverStart)
	if elapsed < time.Second {
		gtx.Execute(op.InvalidateCmd{At: tm.hoverStart.Add(time.Second)})
		return
	}
	hint := tm.hoveredHint
	pos := tm.overlays.mousePos
	hasPos := tm.overlays.hasMousePos
	if !hasPos {
		pos = f32.Pt(float32(gtx.Constraints.Max.X)/2, float32(gtx.Constraints.Max.Y)/2)
	}
	tm.overlays.deferDraw(func(gtx layout.Context) {
		drawTooltipCard(gtx, th, hint, pos)
	})
}

func (tm *tooltipManager) tagFor(id string) *tooltipTag {
	t := tm.tags[id]
	if t == nil {
		t = &tooltipTag{id: id}
		tm.tags[id] = t
	}
	return t
}

func (tm *tooltipManager) Wrap(gtx layout.Context, id, hint string, w layout.Widget) layout.Dimensions {
	if tm == nil || hint == "" {
		return w(gtx)
	}
	dims := w(gtx)
	if dims.Size.X <= 0 || dims.Size.Y <= 0 {
		return dims
	}

	tag := tm.tagFor(id)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
	event.Op(gtx.Ops, tag)
	area.Pop()
	pass.Pop()

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: tag,
			Kinds:  pointer.Enter | pointer.Leave | pointer.Move | pointer.Press,
		})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Enter:
			if tm.hoveredID != id {
				tm.hoveredID = id
				tm.hoveredHint = hint
				tm.hoverStart = gtx.Now
				tm.suppressed = false
			}
			tm.seenThisFrm = true
		case pointer.Move:
			if tm.hoveredID != id {
				tm.hoveredID = id
				tm.hoveredHint = hint
				tm.hoverStart = gtx.Now
				tm.suppressed = false
			}
			tm.seenThisFrm = true
		case pointer.Press:
			tm.suppressed = true
		case pointer.Leave:
			if tm.hoveredID == id {
				tm.hoveredID = ""
				tm.hoveredHint = ""
				tm.suppressed = false
			}
		}
	}
	if tm.hoveredID == id {
		tm.hoveredHint = hint
		tm.seenThisFrm = true
	}
	return dims
}

func drawTooltipCard(gtx layout.Context, th *material.Theme, hint string, cursor f32.Point) {
	padX := gtx.Dp(8)
	padY := gtx.Dp(6)
	maxBoxW := gtx.Dp(260)
	if winW := gtx.Constraints.Max.X - gtx.Dp(16); winW > 0 && winW < maxBoxW {
		maxBoxW = winW
	}
	maxTextW := max(40, maxBoxW-padX*2)

	macro := op.Record(gtx.Ops)
	cgtx := gtx
	cgtx.Constraints = layout.Constraints{Max: image.Pt(maxTextW, gtx.Dp(320))}
	lbl := material.Label(th, unit.Sp(11.5), hint)
	lbl.Color = colText
	textDims := lbl.Layout(cgtx)
	call := macro.Stop()

	boxSize := image.Pt(textDims.Size.X+padX*2, textDims.Size.Y+padY*2)
	margin := gtx.Dp(8)
	x := int(math.Round(float64(cursor.X))) + gtx.Dp(12)
	y := int(math.Round(float64(cursor.Y))) + gtx.Dp(18)

	if x+boxSize.X > gtx.Constraints.Max.X-margin {
		x = gtx.Constraints.Max.X - margin - boxSize.X
	}
	if x < margin {
		x = margin
	}
	if y+boxSize.Y > gtx.Constraints.Max.Y-margin {
		above := int(math.Round(float64(cursor.Y))) - boxSize.Y - gtx.Dp(12)
		if above >= margin {
			y = above
		} else {
			y = max(margin, gtx.Constraints.Max.Y-margin-boxSize.Y)
		}
	}
	if y < margin {
		y = margin
	}

	off := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	rect := image.Rectangle{Max: boxSize}
	radius := gtx.Dp(4)
	fillRRect(gtx.Ops, rect, radius, colTooltipBg)
	strokeRRect(gtx.Ops, rect, radius, float32(gtx.Dp(1)), colBorderLight)
	textOff := op.Offset(image.Pt(padX, padY)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	textOff.Pop()
	off.Pop()
}

type uiButtonStyle struct {
	Variant      btnVariant
	Label        string
	Enabled      bool
	Active       bool
	Loading      bool
	FontSize     unit.Sp
	FontWeight   font.Weight
	CornerRadius unit.Dp
	Pad          layout.Inset
	AlignLeft    bool
	StateColor   color.NRGBA
}

func defaultButtonStyle(variant btnVariant, label string) uiButtonStyle {
	s := uiButtonStyle{
		Variant:      variant,
		Label:        label,
		Enabled:      true,
		FontSize:     unit.Sp(13),
		CornerRadius: unit.Dp(4),
		Pad:          layout.Inset{Top: unit.Dp(7), Bottom: unit.Dp(7), Left: unit.Dp(12), Right: unit.Dp(12)},
	}
	switch variant {
	case btnModeTab:
		s.FontSize = unit.Sp(11.5)
		s.Pad = layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(6), Right: unit.Dp(6)}
	case btnOptionsTab:
		s.FontSize = unit.Sp(12)
		s.Pad = layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}
		s.AlignLeft = true
	case btnAdjust:
		s.FontSize = unit.Sp(12)
		s.Pad = layout.Inset{Top: unit.Dp(7), Bottom: unit.Dp(7), Left: unit.Dp(6), Right: unit.Dp(6)}
	case btnCalibrationFile:
		s.FontSize = unit.Sp(11)
		s.Pad = layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}
		s.AlignLeft = true
	}
	return s
}

func layoutButton(gtx layout.Context, th *material.Theme, clk *widget.Clickable, st uiButtonStyle) layout.Dimensions {
	if !st.Enabled {
		gtx = gtx.Disabled()
	}
	bg, borderCol, fg := resolveButtonColors(st, clk.Hovered(), clk.Pressed())
	if !st.Enabled {
		bg = scaleAlpha(bg, 0.5)
		borderCol = scaleAlpha(borderCol, 0.5)
		fg = scaleAlpha(fg, 0.5)
	}

	return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if st.Enabled && clk.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
		}
		radius := gtx.Dp(st.CornerRadius)
		macro := op.Record(gtx.Ops)
		dims := st.Pad.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			inner := func(gtx layout.Context) layout.Dimensions {
				if st.Loading {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return drawSpinner(gtx, unit.Dp(13), unit.Dp(2), color.NRGBA{R: 255, G: 255, B: 255, A: 64}, fg)
					})
				}
				lbl := material.Label(th, st.FontSize, st.Label)
				lbl.Color = fg
				lbl.Font.Weight = st.FontWeight
				lbl.MaxLines = 1
				if st.AlignLeft {
					lbl.Alignment = text.Start
				} else {
					lbl.Alignment = text.Middle
				}
				return lbl.Layout(gtx)
			}
			if st.AlignLeft {
				return layout.W.Layout(gtx, inner)
			}
			return layout.Center.Layout(gtx, inner)
		})
		call := macro.Stop()

		if gtx.Constraints.Min.X > dims.Size.X {
			dims.Size.X = gtx.Constraints.Min.X
		}
		rect := image.Rectangle{Max: dims.Size}
		if bg.A > 0 {
			fillRRect(gtx.Ops, rect, radius, bg)
		}
		if borderCol.A > 0 {
			bw := float32(gtx.Dp(1))
			if st.Variant == btnModeTab && st.Active {
				bw = float32(gtx.Dp(2))
			}
			strokeRRect(gtx.Ops, rect, radius, bw, borderCol)
		}
		call.Add(gtx.Ops)
		return dims
	})
}

func resolveButtonColors(st uiButtonStyle, hovered, pressed bool) (bg, borderCol, fg color.NRGBA) {
	fg = colWhite
	switch st.Variant {
	case btnPrimary:
		bg = colPrimary
		if pressed {
			bg = colPrimaryActive
		} else if hovered {
			bg = colPrimaryHover
		}
	case btnAdjust:
		bg = colAdjust
		if st.Active || pressed {
			bg = colAdjustActive
		} else if hovered {
			bg = colAdjustHover
		}
	case btnLoad:
		bg = colLoad
		if hovered || pressed {
			bg = colLoadHover
		}
	case btnSave:
		bg = colSave
		if hovered || pressed {
			bg = colSaveHover
		}
	case btnSkip:
		bg = colSkip
		if pressed {
			bg = colSkipActive
		} else if hovered {
			bg = colSkipHover
		}
	case btnRecrop:
		bg = colRecrop
		if pressed {
			bg = colRecropActive
		} else if hovered {
			bg = colRecropHover
		}
	case btnDanger:
		bg = colDanger
		if pressed {
			bg = colDangerActive
		} else if hovered {
			bg = colDangerHover
		}
	case btnSecondary:
		bg = colSecondary
		fg = colText
		if hovered || pressed {
			bg = colSecondaryHov
		}
	case btnModeTab:
		bg = colSecondary
		fg = colText
		if st.Active {
			borderCol = colPrimary
			fg = colPrimary
		} else if hovered {
			bg = colModeHover
		}
	case btnOptionsTab:
		bg = colSecondary
		borderCol = colSecondary
		fg = colText
		if st.Active {
			borderCol = colPrimary
			fg = colPrimary
		} else if hovered {
			bg = colSecondaryHov
			borderCol = colSecondaryHov
		}
	case btnGhost:
		fg = colTextMuted
		if hovered || pressed {
			bg = colSecondaryHov
			fg = colText
		}
	case btnCalibrationFile:
		bg = colSurface
		fg = colTextSubtle
		if st.Active {
			bg = color.NRGBA{R: 51, G: 51, B: 58, A: 255}
			borderCol = colPrimary
			fg = colWhite
		} else if hovered {
			bg = colSurfaceHover
		}
	}
	return bg, borderCol, fg
}

type uiSlider struct {
	dragging  bool
	hovered   bool
	dragValue float64
	lastRect  image.Rectangle
	tag       uint8
}

func (s *uiSlider) LayoutInt(
	gtx layout.Context,
	th *material.Theme,
	value, minVal, maxVal int,
	suffix string,
	pillMinDp unit.Dp,
	enabled bool,
	onChange func(int),
	onCommit func(int),
) layout.Dimensions {
	if maxVal < minVal {
		maxVal = minVal
	}
	cur := clamp(value, minVal, maxVal)
	displayVal := cur
	if s.dragging {
		displayVal = clamp(int(math.Round(s.dragValue)), minVal, maxVal)
	}
	return s.layoutSlider(gtx, th, float64(displayVal), float64(minVal), float64(maxVal),
		fmt.Sprintf("%d%s", displayVal, suffix), pillMinDp, enabled,
		func(v float64) {
			iv := clamp(int(math.Round(v)), minVal, maxVal)
			if onChange != nil && iv != cur {
				onChange(iv)
			}
		},
		func(v float64) {
			iv := clamp(int(math.Round(v)), minVal, maxVal)
			if onCommit != nil {
				onCommit(iv)
			}
		},
	)
}

func (s *uiSlider) LayoutFloat(
	gtx layout.Context,
	th *material.Theme,
	value, minVal, maxVal, step float64,
	pillText string,
	pillMinDp unit.Dp,
	enabled bool,
	onChange func(float64),
	onCommit func(float64),
) layout.Dimensions {
	if maxVal < minVal {
		maxVal = minVal
	}
	snap := func(v float64) float64 {
		if step > 0 {
			v = math.Round((v-minVal)/step)*step + minVal
		}
		if v < minVal {
			v = minVal
		}
		if v > maxVal {
			v = maxVal
		}
		return v
	}
	cur := snap(value)
	displayVal := cur
	if s.dragging {
		displayVal = snap(s.dragValue)
	}
	return s.layoutSlider(gtx, th, displayVal, minVal, maxVal, pillText, pillMinDp, enabled,
		func(v float64) {
			sv := snap(v)
			if onChange != nil && math.Abs(sv-cur) > 1e-6 {
				onChange(sv)
			}
		},
		func(v float64) {
			sv := snap(v)
			if onCommit != nil {
				onCommit(sv)
			}
		},
	)
}

func (s *uiSlider) layoutSlider(
	gtx layout.Context,
	th *material.Theme,
	value, minVal, maxVal float64,
	pillText string,
	pillMinDp unit.Dp,
	enabled bool,
	onChange func(float64),
	onCommit func(float64),
) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(8)}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return s.layoutTrack(gtx, value, minVal, maxVal, enabled, onChange, onCommit)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutValuePill(gtx, th, pillText, pillMinDp, enabled)
		}),
	)
}

func (s *uiSlider) layoutTrack(
	gtx layout.Context,
	value, minVal, maxVal float64,
	enabled bool,
	onChange func(float64),
	onCommit func(float64),
) layout.Dimensions {
	h := gtx.Dp(20)
	w := gtx.Constraints.Max.X
	if w < gtx.Dp(40) {
		w = gtx.Dp(40)
	}
	size := image.Pt(w, h)
	thumbR := float32(gtx.Dp(6))
	padX := thumbR + float32(gtx.Dp(1))
	trackLen := float32(w) - padX*2
	if trackLen < 1 {
		trackLen = 1
	}

	valueAtX := func(px float32) float64 {
		if maxVal <= minVal {
			return minVal
		}
		frac := float64(px-padX) / float64(trackLen)
		if frac < 0 {
			frac = 0
		} else if frac > 1 {
			frac = 1
		}
		return minVal + frac*(maxVal-minVal)
	}

	if enabled {
		area := clip.Rect{Max: size}.Push(gtx.Ops)
		event.Op(gtx.Ops, &s.tag)
		if s.hovered || s.dragging {
			pointer.CursorPointer.Add(gtx.Ops)
		}
		area.Pop()

		for {
			ev, ok := gtx.Event(pointer.Filter{
				Target: &s.tag,
				Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Enter | pointer.Leave,
			})
			if !ok {
				break
			}
			pe, ok := ev.(pointer.Event)
			if !ok {
				continue
			}
			switch pe.Kind {
			case pointer.Enter:
				s.hovered = true
			case pointer.Leave:
				s.hovered = false
			case pointer.Press:
				if pe.Buttons.Contain(pointer.ButtonPrimary) {
					s.dragging = true
					gtx.Execute(pointer.GrabCmd{Tag: &s.tag, ID: pe.PointerID})
					v := valueAtX(pe.Position.X)
					s.dragValue = v
					if onChange != nil {
						onChange(v)
					}
				}
			case pointer.Drag:
				if s.dragging {
					v := valueAtX(pe.Position.X)
					s.dragValue = v
					if onChange != nil {
						onChange(v)
					}
				}
			case pointer.Release:
				if s.dragging {
					v := valueAtX(pe.Position.X)
					s.dragValue = v
					s.dragging = false
					if onChange != nil {
						onChange(v)
					}
					if onCommit != nil {
						onCommit(v)
					}
				}
			case pointer.Cancel:
				if s.dragging {
					v := s.dragValue
					s.dragging = false
					if onCommit != nil {
						onCommit(v)
					}
				}
			}
		}
	} else {
		s.dragging = false
		s.hovered = false
	}

	frac := float32(0)
	if maxVal > minVal {
		frac = float32((value - minVal) / (maxVal - minVal))
		if frac < 0 {
			frac = 0
		} else if frac > 1 {
			frac = 1
		}
	}

	trackH := gtx.Dp(4)
	trackY := (h - trackH) / 2
	trackRect := image.Rect(int(padX), trackY, int(padX+trackLen), trackY+trackH)
	trackCol := colBorder
	fillCol := colPrimary
	thumbCol := colText
	if !enabled {
		trackCol = scaleAlpha(trackCol, 0.5)
		fillCol = scaleAlpha(fillCol, 0.5)
		thumbCol = scaleAlpha(thumbCol, 0.5)
	}
	fillRRect(gtx.Ops, trackRect, trackH/2, trackCol)

	thumbX := padX + frac*trackLen
	if thumbX > padX {
		activeRect := image.Rect(int(padX), trackY, int(math.Round(float64(thumbX))), trackY+trackH)
		fillRRect(gtx.Ops, activeRect, trackH/2, fillCol)
	}

	cy := float32(h) / 2
	drawSolidCircle(gtx.Ops, f32.Pt(thumbX, cy), thumbR, thumbCol)
	if s.hovered || s.dragging {
		drawStrokeCircle(gtx.Ops, f32.Pt(thumbX, cy), thumbR, float32(gtx.Dp(1.5)), fillCol)
	}
	return layout.Dimensions{Size: size}
}

func layoutValuePill(gtx layout.Context, th *material.Theme, textStr string, minWidthDp unit.Dp, enabled bool) layout.Dimensions {
	if minWidthDp <= 0 {
		minWidthDp = unit.Dp(36)
	}
	pad := layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}
	bg := colBorder
	fg := colText
	if !enabled {
		bg = scaleAlpha(bg, 0.5)
		fg = scaleAlpha(fg, 0.5)
	}
	macro := op.Record(gtx.Ops)
	cgtx := gtx
	cgtx.Constraints.Min.X = gtx.Dp(minWidthDp)
	dims := pad.Layout(cgtx, func(gtx layout.Context) layout.Dimensions {
		return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(th, unit.Sp(12), textStr)
			lbl.Color = fg
			lbl.MaxLines = 1
			return lbl.Layout(gtx)
		})
	})
	call := macro.Stop()
	fillRRect(gtx.Ops, image.Rectangle{Max: dims.Size}, gtx.Dp(3), bg)
	call.Add(gtx.Ops)
	return dims
}

func layoutCheckbox(
	gtx layout.Context,
	th *material.Theme,
	clk *widget.Clickable,
	checked bool,
	label string,
	subText string,
	enabled bool,
) layout.Dimensions {
	if !enabled {
		gtx = gtx.Disabled()
	}
	boxSize := gtx.Dp(16)
	boxCol := colPrimary
	borderCol := colBorderLight
	fg := colText
	if !enabled {
		boxCol = scaleAlpha(boxCol, 0.5)
		borderCol = scaleAlpha(borderCol, 0.5)
		fg = scaleAlpha(fg, 0.5)
	}

	return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if enabled && clk.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
		}
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				rect := image.Rect(0, 0, boxSize, boxSize)
				radius := gtx.Dp(3)
				if checked {
					fillRRect(gtx.Ops, rect, radius, boxCol)
					var p clip.Path
					p.Begin(gtx.Ops)
					p.MoveTo(f32.Pt(float32(boxSize)*0.22, float32(boxSize)*0.52))
					p.LineTo(f32.Pt(float32(boxSize)*0.42, float32(boxSize)*0.74))
					p.LineTo(f32.Pt(float32(boxSize)*0.78, float32(boxSize)*0.28))
					spec := p.End()
					checkCol := colWhite
					if !enabled {
						checkCol = scaleAlpha(checkCol, 0.5)
					}
					paint.FillShape(gtx.Ops, checkCol, clip.Stroke{Path: spec, Width: float32(gtx.Dp(2))}.Op())
				} else {
					fillRRect(gtx.Ops, rect, radius, colCanvasBg)
					strokeRRect(gtx.Ops, rect, radius, float32(gtx.Dp(1)), borderCol)
				}
				return layout.Dimensions{Size: image.Pt(boxSize, boxSize)}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(th, unit.Sp(13), label)
				lbl.Color = fg
				return lbl.Layout(gtx)
			}),
		}
		if subText != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				sub := material.Label(th, unit.Sp(11), subText)
				sub.Color = colTextMuted
				if !enabled {
					sub.Color = scaleAlpha(sub.Color, 0.5)
				}
				return sub.Layout(gtx)
			}))
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(8)}.Layout(gtx, children...)
	})
}

func layoutRadio(
	gtx layout.Context,
	th *material.Theme,
	clk *widget.Clickable,
	selected bool,
	label string,
	subText string,
	enabled bool,
) layout.Dimensions {
	if !enabled {
		gtx = gtx.Disabled()
	}
	sz := gtx.Dp(16)
	accent := colPrimary
	borderCol := colBorderLight
	fg := colText
	if !enabled {
		accent = scaleAlpha(accent, 0.5)
		borderCol = scaleAlpha(borderCol, 0.5)
		fg = scaleAlpha(fg, 0.5)
	}

	return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if enabled && clk.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
		}
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				c := f32.Pt(float32(sz)/2, float32(sz)/2)
				r := float32(sz)/2 - float32(gtx.Dp(1))
				drawSolidCircle(gtx.Ops, c, r, colCanvasBg)
				if selected {
					drawStrokeCircle(gtx.Ops, c, r, float32(gtx.Dp(1.5)), accent)
					drawSolidCircle(gtx.Ops, c, r*0.5, accent)
				} else {
					drawStrokeCircle(gtx.Ops, c, r, float32(gtx.Dp(1.5)), borderCol)
				}
				return layout.Dimensions{Size: image.Pt(sz, sz)}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(th, unit.Sp(13), label)
				lbl.Color = fg
				return lbl.Layout(gtx)
			}),
		}
		if subText != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				sub := material.Label(th, unit.Sp(11), subText)
				sub.Color = colTextMuted
				return sub.Layout(gtx)
			}))
		}
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(8)}.Layout(gtx, children...)
	})
}

type uiSelectOption struct {
	Value string
	Label string
}

type uiDropdown struct {
	btn          widget.Clickable
	dismissBtn   widget.Clickable
	optBtns      []widget.Clickable
	open         bool
	posTag       uint8
	lastLocalPos f32.Point
	hasLocalPos  bool
	needsAnchor  bool
	anchorPos    f32.Point
	triggerW     int
	triggerH     int
}

func (d *uiDropdown) Layout(
	gtx layout.Context,
	th *material.Theme,
	overlays *overlayManager,
	options []uiSelectOption,
	currentValue string,
	enabled bool,
	onChange func(string),
) layout.Dimensions {
	if len(d.optBtns) < len(options) {
		d.optBtns = make([]widget.Clickable, len(options))
	}
	if !enabled {
		d.open = false
		gtx = gtx.Disabled()
	}

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &d.posTag,
			Kinds:  pointer.Press | pointer.Release,
		})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			d.lastLocalPos = pe.Position
			d.hasLocalPos = true
		}
	}

	for i, opt := range options {
		if d.optBtns[i].Clicked(gtx) {
			d.open = false
			if overlays != nil && overlays.activeMenu == d {
				overlays.activeMenu = nil
			}
			if onChange != nil && opt.Value != currentValue {
				onChange(opt.Value)
			}
		}
	}
	if d.dismissBtn.Clicked(gtx) {
		d.open = false
		if overlays != nil && overlays.activeMenu == d {
			overlays.activeMenu = nil
		}
	}

	// Drain Clicked BEFORE btn.Layout, which consumes pending clicks.
	btnClicked := enabled && d.btn.Clicked(gtx)

	displayLabel := currentValue
	for _, opt := range options {
		if opt.Value == currentValue {
			displayLabel = opt.Label
			break
		}
	}

	bg := colBorder
	border := colBorderLight
	fg := colText
	if !enabled {
		bg = scaleAlpha(bg, 0.5)
		border = scaleAlpha(border, 0.5)
		fg = scaleAlpha(fg, 0.5)
	} else if d.btn.Hovered() {
		bg = colSecondaryHov
	}

	dims := d.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if enabled && d.btn.Hovered() {
			pointer.CursorPointer.Add(gtx.Ops)
		}
		pad := layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}
		macro := op.Record(gtx.Ops)
		sz := pad.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(6)}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(th, unit.Sp(12), displayLabel)
					lbl.Color = fg
					lbl.MaxLines = 1
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(th, unit.Sp(11), "▾")
					lbl.Color = colTextMuted
					return lbl.Layout(gtx)
				}),
			)
		})
		call := macro.Stop()
		if gtx.Constraints.Min.X > sz.Size.X {
			sz.Size.X = gtx.Constraints.Min.X
		}
		rect := image.Rectangle{Max: sz.Size}
		fillRRect(gtx.Ops, rect, gtx.Dp(4), bg)
		strokeRRect(gtx.Ops, rect, gtx.Dp(4), float32(gtx.Dp(1)), border)
		call.Add(gtx.Ops)
		return sz
	})

	d.triggerW = dims.Size.X
	d.triggerH = dims.Size.Y

	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
	event.Op(gtx.Ops, &d.posTag)
	area.Pop()
	pass.Pop()

	if btnClicked {
		if d.open {
			d.open = false
			if overlays != nil && overlays.activeMenu == d {
				overlays.activeMenu = nil
			}
		} else {
			d.open = true
			d.needsAnchor = true
			if overlays != nil {
				if overlays.activeMenu != nil && overlays.activeMenu != d {
					overlays.activeMenu.open = false
				}
				overlays.activeMenu = d
			}
			gtx.Execute(op.InvalidateCmd{})
		}
	}

	if d.open && overlays != nil {
		overlays.deferDraw(func(gtx layout.Context) {
			if d.needsAnchor {
				if overlays.hasMousePos {
					if d.hasLocalPos {
						d.anchorPos = f32.Pt(
							overlays.mousePos.X-d.lastLocalPos.X,
							overlays.mousePos.Y-d.lastLocalPos.Y+float32(d.triggerH),
						)
					} else {
						d.anchorPos = overlays.mousePos
					}
				}
				d.needsAnchor = false
			}
			d.drawPopup(gtx, th, options, currentValue)
		})
	}
	return dims
}

func (d *uiDropdown) drawPopup(gtx layout.Context, th *material.Theme, options []uiSelectOption, currentValue string) {
	// Full-window transparent click catcher behind menu.
	d.dismissBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	})

	menuW := max(d.triggerW, gtx.Dp(130))
	if menuW > gtx.Constraints.Max.X-gtx.Dp(16) {
		menuW = gtx.Constraints.Max.X - gtx.Dp(16)
	}
	macro := op.Record(gtx.Ops)
	mgtx := gtx
	mgtx.Constraints = layout.Constraints{Min: image.Pt(menuW, 0), Max: image.Pt(menuW, gtx.Dp(320))}
	items := make([]layout.FlexChild, 0, len(options))
	for i, opt := range options {
		idx := i
		item := opt
		items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return d.optBtns[idx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if d.optBtns[idx].Hovered() {
					pointer.CursorPointer.Add(gtx.Ops)
				}
				selected := item.Value == currentValue
				rowBg := colSurface
				if selected {
					rowBg = colPrimary
				} else if d.optBtns[idx].Hovered() {
					rowBg = colSecondaryHov
				}
				m := op.Record(gtx.Ops)
				rd := layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(th, unit.Sp(12), item.Label)
					lbl.Color = colText
					if selected {
						lbl.Color = colWhite
					}
					lbl.MaxLines = 1
					return lbl.Layout(gtx)
				})
				rc := m.Stop()
				if rd.Size.X < menuW {
					rd.Size.X = menuW
				}
				paint.FillShape(gtx.Ops, rowBg, clip.Rect{Max: rd.Size}.Op())
				rc.Add(gtx.Ops)
				return rd
			})
		}))
	}
	menuDims := layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(mgtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items...)
	})
	call := macro.Stop()

	x := int(math.Round(float64(d.anchorPos.X)))
	y := int(math.Round(float64(d.anchorPos.Y))) + gtx.Dp(2)
	margin := gtx.Dp(6)
	if x+menuDims.Size.X > gtx.Constraints.Max.X-margin {
		x = gtx.Constraints.Max.X - margin - menuDims.Size.X
	}
	if x < margin {
		x = margin
	}
	if y+menuDims.Size.Y > gtx.Constraints.Max.Y-margin {
		above := int(math.Round(float64(d.anchorPos.Y))) - d.triggerH - menuDims.Size.Y - gtx.Dp(2)
		if above >= margin {
			y = above
		} else {
			y = max(margin, gtx.Constraints.Max.Y-margin-menuDims.Size.Y)
		}
	}
	if y < margin {
		y = margin
	}

	off := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	rect := image.Rectangle{Max: menuDims.Size}
	fillRRect(gtx.Ops, rect, gtx.Dp(4), colSurface)
	call.Add(gtx.Ops)
	strokeRRect(gtx.Ops, rect, gtx.Dp(4), float32(gtx.Dp(1)), colBorderLight)
	off.Pop()
}

type uiTextInput struct {
	editor     widget.Editor
	lastSynced string
	initDone   bool
}

func (t *uiTextInput) EnsureInit(text string, filter string) {
	if !t.initDone {
		t.editor.SingleLine = true
		t.editor.Submit = true
		t.editor.Filter = filter
		t.editor.SetText(text)
		t.lastSynced = text
		t.initDone = true
	}
}

func (t *uiTextInput) SyncExternal(gtx layout.Context, value string) {
	if !t.initDone {
		t.EnsureInit(value, "")
		return
	}
	if !gtx.Focused(&t.editor) && t.lastSynced != value {
		t.editor.SetText(value)
		t.lastSynced = value
	}
}

func (t *uiTextInput) Layout(
	gtx layout.Context,
	th *material.Theme,
	hint string,
	enabled bool,
	anyFocused *bool,
	onChange func(string),
	onSubmit func(string),
) layout.Dimensions {
	if !t.initDone {
		t.EnsureInit("", "")
	}
	if !enabled {
		gtx = gtx.Disabled()
	}
	focused := gtx.Focused(&t.editor)
	if focused && anyFocused != nil {
		*anyFocused = true
	}

	for {
		ev, ok := t.editor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			txt := t.editor.Text()
			t.lastSynced = txt
			if onChange != nil {
				onChange(txt)
			}
		case widget.SubmitEvent:
			if onSubmit != nil {
				onSubmit(t.editor.Text())
			}
		}
	}

	border := colBorder
	if focused {
		border = colPrimary
	}
	fg := colText
	if !enabled {
		fg = scaleAlpha(fg, 0.5)
	}

	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		ed := material.Editor(th, &t.editor, hint)
		ed.TextSize = unit.Sp(13)
		ed.Color = fg
		ed.HintColor = colTextMuted
		ed.SelectionColor = scaleAlpha(colPrimary, 0.45)
		return ed.Layout(gtx)
	})
	call := macro.Stop()
	if gtx.Constraints.Min.X > dims.Size.X {
		dims.Size.X = gtx.Constraints.Min.X
	}
	rect := image.Rectangle{Max: dims.Size}
	fillRRect(gtx.Ops, rect, gtx.Dp(4), colCanvasBg)
	strokeRRect(gtx.Ops, rect, gtx.Dp(4), float32(gtx.Dp(1)), border)
	call.Add(gtx.Ops)
	return dims
}

type uiIntInput struct {
	uiTextInput
	minVal int
	maxVal int
}

func (i *uiIntInput) LayoutInt(
	gtx layout.Context,
	th *material.Theme,
	value, minVal, maxVal int,
	enabled bool,
	anyFocused *bool,
	onValid func(int),
	onSubmit func(),
) layout.Dimensions {
	i.minVal = minVal
	i.maxVal = maxVal
	if !i.initDone {
		i.EnsureInit(strconv.Itoa(value), "0123456789")
	}
	strVal := strconv.Itoa(value)
	if !gtx.Focused(&i.editor) && i.lastSynced != strVal {
		i.editor.SetText(strVal)
		i.lastSynced = strVal
	}
	return i.Layout(gtx, th, "", enabled, anyFocused,
		func(s string) {
			s = strings.TrimSpace(s)
			if s == "" {
				return
			}
			n, err := strconv.Atoi(s)
			if err != nil {
				return
			}
			n = clamp(n, minVal, maxVal)
			if onValid != nil {
				onValid(n)
			}
		},
		func(_ string) {
			if onSubmit != nil {
				onSubmit()
			}
		},
	)
}

type uiAccordion struct {
	titleBtn       widget.Clickable
	arrowBtn       widget.Clickable
	list           widget.List
	anim           float32
	displayH       float32
	lastLayoutMaxH int
	animating      bool
	lastTime       time.Time
	initialized    bool
}

func (a *uiAccordion) Layout(
	gtx layout.Context,
	th *material.Theme,
	title string,
	open bool,
	maxContentHeight int,
	headerExtra layout.Widget,
	onToggle func(),
	body layout.Widget,
) layout.Dimensions {
	a.list.Axis = layout.Vertical

	toggled := a.titleBtn.Clicked(gtx)
	if a.arrowBtn.Clicked(gtx) {
		toggled = true
	}
	if toggled && onToggle != nil {
		onToggle()
		open = !open
	}

	var dt float32
	if a.initialized {
		if !a.animating {
			a.lastTime = gtx.Now
		} else {
			dt = float32(gtx.Now.Sub(a.lastTime).Seconds())
			if dt < 0 {
				dt = 0
			} else if dt > 0.05 {
				dt = 0.05
			}
			a.lastTime = gtx.Now
		}
	}

	minHeaderH := gtx.Dp(22)
	hdrInset := layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12)}
	macro := op.Record(gtx.Ops)
	pad := layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10)}
	dims := pad.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hdrInset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							cg := gtx
							cg.Constraints.Min.Y = max(cg.Constraints.Min.Y, minHeaderH)
							return a.titleBtn.Layout(cg, func(gtx layout.Context) layout.Dimensions {
								if a.titleBtn.Hovered() {
									pointer.CursorPointer.Add(gtx.Ops)
								}
								return layout.W.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lg := gtx
									lg.Constraints.Min.Y = 0
									lbl := material.Label(th, unit.Sp(11.5), title)
									lbl.Color = colText
									lbl.Font.Weight = font.Bold
									return lbl.Layout(lg)
								})
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if headerExtra == nil {
								return layout.Dimensions{}
							}
							return headerExtra(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							cg := gtx
							cg.Constraints.Min.Y = max(cg.Constraints.Min.Y, minHeaderH)
							return a.arrowBtn.Layout(cg, func(gtx layout.Context) layout.Dimensions {
								if a.arrowBtn.Hovered() {
									pointer.CursorPointer.Add(gtx.Ops)
								}
								return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										arrow := "▸"
										if open {
											arrow = "▾"
										}
										lbl := material.Label(th, unit.Sp(14), arrow)
										lbl.Color = colTextMuted
										return lbl.Layout(gtx)
									})
								})
							})
						}),
					)
				})
			}),
		}

		if (!a.initialized && open) || open || a.displayH > 0.5 {
			dividerTop := gtx.Dp(6)
			dividerLine := max(1, gtx.Dp(1))
			dividerH := dividerTop + dividerLine

			listMaxH := maxContentHeight
			if open {
				a.lastLayoutMaxH = maxContentHeight
				if a.initialized {
					if curBody := int(math.Ceil(float64(a.displayH))) - dividerH; curBody > listMaxH {
						listMaxH = curBody
					}
				}
			} else if a.lastLayoutMaxH > 0 {
				// Keep closing scrollable content capped at its pre-close height
				// even if remaining open panels change maxContentHeight.
				listMaxH = a.lastLayoutMaxH
			}

			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				cm := op.Record(gtx.Ops)
				hdrInsetX := gtx.Dp(12)
				divW := max(0, gtx.Constraints.Max.X-hdrInsetX*2)
				divStack := op.Offset(image.Pt(hdrInsetX, dividerTop)).Push(gtx.Ops)
				paint.FillShape(gtx.Ops, color.NRGBA{R: 62, G: 62, B: 66, A: 153}, clip.Rect{Max: image.Pt(divW, dividerLine)}.Op())
				divStack.Pop()

				var naturalBodyH int
				bodyStack := op.Offset(image.Pt(0, dividerH)).Push(gtx.Ops)
				cgtx := gtx
				cgtx.Constraints.Min.Y = 0
				cgtx.Constraints.Max.Y = max(0, listMaxH)
				ls := styledList(th, &a.list)
				cdims := ls.Layout(cgtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
					d := layout.Inset{Top: unit.Dp(6), Left: unit.Dp(16), Right: unit.Dp(6)}.Layout(gtx, body)
					naturalBodyH = d.Size.Y
					return d
				})
				bodyStack.Pop()
				ccall := cm.Stop()

				renderedH := dividerH + cdims.Size.Y
				targetH := float32(0)
				targetAlpha := float32(0)
				if open {
					targetH = float32(dividerH + min(naturalBodyH, maxContentHeight))
					targetAlpha = 1
				}

				if !a.initialized {
					a.displayH = targetH
					a.anim = targetAlpha
					a.lastTime = gtx.Now
					a.initialized = true
					a.animating = false
				} else {
					if !open && a.displayH > float32(renderedH) {
						a.displayH = float32(renderedH)
					}
					if dt > 0 {
						k := float32(1 - math.Exp(float64(-dt)*16.0))
						a.displayH += (targetH - a.displayH) * k
						if math.Abs(float64(targetH-a.displayH)) < 0.75 {
							a.displayH = targetH
						}
						a.anim += (targetAlpha - a.anim) * k
						if math.Abs(float64(targetAlpha-a.anim)) < 0.01 {
							a.anim = targetAlpha
						}
					}
					a.animating = a.displayH != targetH || a.anim != targetAlpha
					if a.animating {
						gtx.Execute(op.InvalidateCmd{})
					}
				}

				visH := min(int(math.Round(float64(a.displayH))), renderedH)
				if visH <= 0 {
					return layout.Dimensions{}
				}
				clipW := max(cdims.Size.X, gtx.Constraints.Max.X)
				st := clip.Rect{Max: image.Pt(clipW, visH)}.Push(gtx.Ops)
				if a.anim < 0.999 {
					opStack := paint.PushOpacity(gtx.Ops, float32(clampF64(float64(a.anim), 0, 1)))
					ccall.Add(gtx.Ops)
					opStack.Pop()
				} else {
					ccall.Add(gtx.Ops)
				}
				st.Pop()
				return layout.Dimensions{Size: image.Pt(cdims.Size.X, visH)}
			}))
		} else if !a.initialized {
			a.lastTime = gtx.Now
			a.initialized = true
			a.animating = false
		} else {
			a.displayH = 0
			a.anim = 0
			a.animating = false
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	call := macro.Stop()

	if gtx.Constraints.Min.X > dims.Size.X {
		dims.Size.X = gtx.Constraints.Min.X
	}
	radius := gtx.Dp(10)
	rect := image.Rectangle{Max: dims.Size}
	fillTopRRect(gtx.Ops, rect, radius, colSurface)
	// Top border 1px across the top edge.
	strokeTopRRect(gtx.Ops, rect, radius, float32(gtx.Dp(1)), colBorder)
	call.Add(gtx.Ops)
	return dims
}

func styledList(th *material.Theme, list *widget.List) material.ListStyle {
	ls := material.List(th, list)
	ls.AnchorStrategy = material.Occupy
	ls.ScrollbarStyle.Indicator = material.ScrollIndicatorStyle{
		MajorMinLen:  unit.Dp(20),
		MinorWidth:   unit.Dp(6),
		Color:        colBorder,
		HoverColor:   colBorderLight,
		CornerRadius: unit.Dp(3),
	}
	ls.ScrollbarStyle.Track.Color = color.NRGBA{}
	ls.ScrollbarStyle.Track.MinorPadding = unit.Dp(2)
	ls.ScrollbarStyle.Track.MajorPadding = unit.Dp(2)
	return ls
}

func drawSpinner(gtx layout.Context, sizeDp, strokeDp unit.Dp, trackCol, arcCol color.NRGBA) layout.Dimensions {
	sz := gtx.Dp(sizeDp)
	if sz < 4 {
		sz = 4
	}
	sw := float32(gtx.Dp(strokeDp))
	if sw < 1 {
		sw = 1
	}
	c := f32.Pt(float32(sz)/2, float32(sz)/2)
	r := float32(sz)/2 - sw
	if r < 1 {
		r = 1
	}

	drawStrokeCircle(gtx.Ops, c, r, sw, trackCol)

	period := 800 * time.Millisecond
	phase := float64(gtx.Now.UnixNano()%int64(period)) / float64(period)
	startAngle := float32(phase * 2 * math.Pi)
	sweep := float32(1.4 * math.Pi)
	p0 := f32.Pt(c.X+r*float32(math.Cos(float64(startAngle))), c.Y+r*float32(math.Sin(float64(startAngle))))
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(p0)
	path.ArcTo(c, c, sweep)
	paint.FillShape(gtx.Ops, arcCol, clip.Stroke{Path: path.End(), Width: sw}.Op())
	gtx.Execute(op.InvalidateCmd{})
	return layout.Dimensions{Size: image.Pt(sz, sz)}
}

func drawKbdBadge(gtx layout.Context, th *material.Theme, label string, enabled bool) layout.Dimensions {
	pad := layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}
	bg := colBorder
	fg := colText
	if !enabled {
		bg = scaleAlpha(bg, 0.35)
		fg = scaleAlpha(fg, 0.35)
	}
	macro := op.Record(gtx.Ops)
	dims := pad.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Label(th, unit.Sp(11), label)
		lbl.Font.Typeface = "Go Mono"
		lbl.Color = fg
		lbl.MaxLines = 1
		return lbl.Layout(gtx)
	})
	call := macro.Stop()
	fillRRect(gtx.Ops, image.Rectangle{Max: dims.Size}, gtx.Dp(3), bg)
	call.Add(gtx.Ops)
	return dims
}

func fillRRect(ops *op.Ops, rect image.Rectangle, radius int, c color.NRGBA) {
	if rect.Empty() || c.A == 0 {
		return
	}
	paint.FillShape(ops, c, clip.UniformRRect(rect, radius).Op(ops))
}

func strokeRRect(ops *op.Ops, rect image.Rectangle, radius int, width float32, c color.NRGBA) {
	if rect.Empty() || c.A == 0 || width <= 0 {
		return
	}
	path := clip.UniformRRect(rect, radius).Path(ops)
	paint.FillShape(ops, c, clip.Stroke{Path: path, Width: width}.Op())
}

func fillTopRRect(ops *op.Ops, rect image.Rectangle, radius int, c color.NRGBA) {
	if rect.Empty() || c.A == 0 {
		return
	}
	paint.FillShape(ops, c, clip.RRect{Rect: rect, NW: radius, NE: radius}.Op(ops))
}

func strokeTopRRect(ops *op.Ops, rect image.Rectangle, radius int, width float32, c color.NRGBA) {
	if rect.Empty() || c.A == 0 || width <= 0 {
		return
	}
	var p clip.Path
	p.Begin(ops)
	x0, y0 := float32(rect.Min.X), float32(rect.Min.Y)
	x1 := float32(rect.Max.X)
	r := float32(radius)
	if r*2 > x1-x0 {
		r = (x1 - x0) / 2
	}
	p.MoveTo(f32.Pt(x0, y0+r))
	p.QuadTo(f32.Pt(x0, y0), f32.Pt(x0+r, y0))
	p.LineTo(f32.Pt(x1-r, y0))
	p.QuadTo(f32.Pt(x1, y0), f32.Pt(x1, y0+r))
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: width}.Op())
}

func fillBottomRRect(ops *op.Ops, rect image.Rectangle, radius int, c color.NRGBA) {
	if rect.Empty() || c.A == 0 {
		return
	}
	paint.FillShape(ops, c, clip.RRect{Rect: rect, SW: radius, SE: radius}.Op(ops))
}

func drawSolidCircle(ops *op.Ops, center f32.Point, radius float32, c color.NRGBA) {
	if radius <= 0 || c.A == 0 {
		return
	}
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(center.X+radius, center.Y))
	p.ArcTo(center, center, 2*math.Pi)
	p.Close()
	paint.FillShape(ops, c, clip.Outline{Path: p.End()}.Op())
}

func drawStrokeCircle(ops *op.Ops, center f32.Point, radius, width float32, c color.NRGBA) {
	if radius <= 0 || width <= 0 || c.A == 0 {
		return
	}
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(center.X+radius, center.Y))
	p.ArcTo(center, center, 2*math.Pi)
	p.Close()
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: width}.Op())
}

func drawDashedCircle(ops *op.Ops, center f32.Point, radius, width, dashLen, gapLen float32, c color.NRGBA) {
	if radius <= 0 || width <= 0 || c.A == 0 {
		return
	}
	circ := 2 * math.Pi * radius
	step := dashLen + gapLen
	if step <= 0 {
		drawStrokeCircle(ops, center, radius, width, c)
		return
	}
	n := int(math.Max(6, math.Round(float64(circ/step))))
	angStep := float32(2*math.Pi) / float32(n)
	angDash := angStep * (dashLen / step)
	var p clip.Path
	p.Begin(ops)
	for i := 0; i < n; i++ {
		a0 := float64(float32(i) * angStep)
		p0 := f32.Pt(center.X+radius*float32(math.Cos(a0)), center.Y+radius*float32(math.Sin(a0)))
		p.MoveTo(p0)
		p.ArcTo(center, center, angDash)
	}
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: width}.Op())
}

func drawStrokeLine(ops *op.Ops, a, b f32.Point, width float32, c color.NRGBA) {
	if width <= 0 || c.A == 0 {
		return
	}
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(a)
	p.LineTo(b)
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: width}.Op())
}

func addDashedSegment(p *clip.Path, a, b f32.Point, dashLen, gapLen float32) {
	dx := b.X - a.X
	dy := b.Y - a.Y
	dist := float32(math.Hypot(float64(dx), float64(dy)))
	if dist < 1e-3 {
		return
	}
	ux, uy := dx/dist, dy/dist
	pos := float32(0)
	for pos < dist {
		end := pos + dashLen
		if end > dist {
			end = dist
		}
		p.MoveTo(f32.Pt(a.X+ux*pos, a.Y+uy*pos))
		p.LineTo(f32.Pt(a.X+ux*end, a.Y+uy*end))
		pos += dashLen + gapLen
	}
}

func drawDashedRect(ops *op.Ops, x, y, w, h, lineWidth, dashLen, gapLen float32, fillCol, strokeCol color.NRGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	if fillCol.A > 0 {
		ri := image.Rect(int(math.Floor(float64(x))), int(math.Floor(float64(y))), int(math.Ceil(float64(x+w))), int(math.Ceil(float64(y+h))))
		if !ri.Empty() {
			paint.FillShape(ops, fillCol, clip.Rect(ri).Op())
		}
	}
	if strokeCol.A > 0 && lineWidth > 0 {
		var p clip.Path
		p.Begin(ops)
		tl := f32.Pt(x, y)
		tr := f32.Pt(x+w, y)
		br := f32.Pt(x+w, y+h)
		bl := f32.Pt(x, y+h)
		addDashedSegment(&p, tl, tr, dashLen, gapLen)
		addDashedSegment(&p, tr, br, dashLen, gapLen)
		addDashedSegment(&p, br, bl, dashLen, gapLen)
		addDashedSegment(&p, bl, tl, dashLen, gapLen)
		paint.FillShape(ops, strokeCol, clip.Stroke{Path: p.End(), Width: lineWidth}.Op())
	}
}

func drawDashedPolyline(ops *op.Ops, pts []f32.Point, closed bool, lineWidth, dashLen, gapLen float32, c color.NRGBA) {
	if len(pts) < 2 || lineWidth <= 0 || c.A == 0 {
		return
	}
	var p clip.Path
	p.Begin(ops)
	for i := 1; i < len(pts); i++ {
		addDashedSegment(&p, pts[i-1], pts[i], dashLen, gapLen)
	}
	if closed && len(pts) > 2 {
		addDashedSegment(&p, pts[len(pts)-1], pts[0], dashLen, gapLen)
	}
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: lineWidth}.Op())
}

var (
	checkerboardTileMu    sync.Mutex
	checkerboardTileCache = make(map[int]paint.ImageOp)
)

func buildCheckerboardTileImage(tileSize int) *image.RGBA {
	if tileSize <= 0 {
		return nil
	}
	period := tileSize * 2
	side := ((512 + period - 1) / period) * period
	img := image.NewRGBA(image.Rectangle{Max: image.Pt(side, side)})
	cells := side / tileSize
	for row := 0; row < cells; row++ {
		y0 := row * tileSize
		y1 := y0 + tileSize
		for col := 0; col < cells; col++ {
			c := colCheckerDark
			if ((row + col) & 1) == 0 {
				c = colCheckerLight
			}
			x0 := col * tileSize
			x1 := x0 + tileSize
			for py := y0; py < y1; py++ {
				off := img.PixOffset(x0, py)
				for px := x0; px < x1; px++ {
					img.Pix[off+0] = c.R
					img.Pix[off+1] = c.G
					img.Pix[off+2] = c.B
					img.Pix[off+3] = c.A
					off += 4
				}
			}
		}
	}
	return img
}

func checkerboardTileOp(tileSize int) (paint.ImageOp, int) {
	if tileSize <= 0 {
		return paint.ImageOp{}, 0
	}
	checkerboardTileMu.Lock()
	imgOp, ok := checkerboardTileCache[tileSize]
	if !ok {
		img := buildCheckerboardTileImage(tileSize)
		imgOp = paint.NewImageOp(img)
		imgOp.Filter = paint.FilterNearest
		checkerboardTileCache[tileSize] = imgOp
	}
	checkerboardTileMu.Unlock()
	return imgOp, imgOp.Size().X
}

func drawCheckerboard(ops *op.Ops, rect image.Rectangle, originX, originY float32, tileSize int) {
	if rect.Empty() || tileSize <= 0 {
		return
	}
	tileOp, tileImgSize := checkerboardTileOp(tileSize)
	if tileImgSize <= 0 {
		return
	}
	ox := int(math.Round(float64(originX)))
	oy := int(math.Round(float64(originY)))
	remX := ((rect.Min.X-ox)%tileImgSize + tileImgSize) % tileImgSize
	remY := ((rect.Min.Y-oy)%tileImgSize + tileImgSize) % tileImgSize
	startX := rect.Min.X - remX
	startY := rect.Min.Y - remY

	st := clip.Rect(rect).Push(ops)
	tileBox := image.Pt(tileImgSize, tileImgSize)
	for y := startY; y < rect.Max.Y; y += tileImgSize {
		for x := startX; x < rect.Max.X; x += tileImgSize {
			off := op.Offset(image.Pt(x, y)).Push(ops)
			cl := clip.Rect{Max: tileBox}.Push(ops)
			tileOp.Add(ops)
			paint.PaintOp{}.Add(ops)
			cl.Pop()
			off.Pop()
		}
	}
	st.Pop()
}
