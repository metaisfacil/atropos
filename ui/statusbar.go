package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func (e *EditorUI) layoutToolbar(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(51)
	w := gtx.Constraints.Max.X
	if w <= 0 {
		w = 1
	}
	borderH := max(1, gtx.Dp(1))

	paint.FillShape(gtx.Ops, colSurface, clip.Rect(image.Rect(0, 0, w, h)).Op())
	paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, h-borderH, w, h)).Op())

	if e.btnAbout.Clicked(gtx) {
		e.aboutOpen = true
	}

	padX := gtx.Dp(16)
	innerW := max(0, w-2*padX)
	innerH := max(0, h-borderH)
	inner := gtx
	inner.Constraints = layout.Constraints{
		Min: image.Pt(innerW, 0),
		Max: image.Pt(innerW, innerH),
	}

	children := make([]layout.FlexChild, 0, 3)
	if e.isBusy() {
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawSpinner(gtx, unit.Dp(18), unit.Dp(2), colBorder, colPrimary)
		}))
	}
	children = append(children,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if e.statusText == "" || e.statusOpacity <= 0 {
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, 0)}
			}
			lbl := material.Label(e.th, unit.Sp(13), e.statusText)
			lbl.Color = scaleAlpha(colText, e.statusOpacity)
			lbl.MaxLines = 1
			return lbl.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.tooltips.Wrap(gtx, "btn_about", "About Atropos", func(gtx layout.Context) layout.Dimensions {
				d := gtx.Dp(24)
				return e.btnAbout.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					if e.btnAbout.Hovered() {
						pointer.CursorPointer.Add(gtx.Ops)
					}
					bg := colSurfaceHover
					borderCol := colBorderLight
					fg := colTextSubtle
					if e.btnAbout.Hovered() {
						bg = colSecondaryHov
						borderCol = colTextSubtle
						fg = colWhite
					}
					rect := image.Rect(0, 0, d, d)
					paint.FillShape(gtx.Ops, bg, clip.Ellipse(rect).Op(gtx.Ops))
					strokeW := float32(max(1, gtx.Dp(1)))
					c := f32.Pt(float32(d)/2, float32(d)/2)
					drawStrokeCircle(gtx.Ops, c, float32(d)/2-strokeW*0.5, strokeW, borderCol)

					btnGtx := gtx
					btnGtx.Constraints = layout.Exact(image.Pt(d, d))
					layout.Center.Layout(btnGtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(e.th, unit.Sp(12), "?")
						lbl.Color = fg
						lbl.Font.Weight = font.Bold
						lbl.MaxLines = 1
						return lbl.Layout(gtx)
					})
					return layout.Dimensions{Size: image.Pt(d, d)}
				})
			})
		}),
	)

	macro := op.Record(gtx.Ops)
	rowDims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(10)}.Layout(inner, children...)
	call := macro.Stop()

	offY := max(0, (innerH-rowDims.Size.Y)/2)
	tr := op.Offset(image.Pt(padX, offY)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	tr.Pop()

	return layout.Dimensions{Size: image.Pt(w, h)}
}

func (e *EditorUI) layoutStatusBar(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(24)
	w := gtx.Constraints.Max.X
	if w <= 0 {
		w = 1
	}
	borderH := max(1, gtx.Dp(1))

	paint.FillShape(gtx.Ops, colSurface, clip.Rect(image.Rect(0, 0, w, h)).Op())
	paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, 0, w, borderH)).Op())

	if !e.imageLoaded {
		return layout.Dimensions{Size: image.Pt(w, h)}
	}

	if e.btnStatusZoom.Clicked(gtx) {
		e.mainViewport.SetZoomKeepCenter(1)
	}

	sep := func() layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			sepW := max(1, gtx.Dp(1))
			sepH := gtx.Dp(12)
			paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, 0, sepW, sepH)).Op())
			return layout.Dimensions{Size: image.Pt(sepW, sepH)}
		})
	}
	textItem := func(id, text, hint string) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.tooltips.Wrap(gtx, id, hint, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(e.th, unit.Sp(11), text)
				lbl.Color = colTextMuted
				lbl.MaxLines = 1
				return lbl.Layout(gtx)
			})
		})
	}

	var items []layout.FlexChild
	addItem := func(child layout.FlexChild) {
		if len(items) > 0 {
			items = append(items, sep())
		}
		items = append(items, child)
	}

	if e.imageMeta.Format != "" {
		addItem(textItem("sb_fmt", e.imageMeta.Format, "Source image format"))
	}
	if e.imageMeta.InputDims.X > 0 && e.imageMeta.InputDims.Y > 0 {
		addItem(textItem("sb_in_dims",
			fmt.Sprintf("%d × %d", e.imageMeta.InputDims.X, e.imageMeta.InputDims.Y),
			"Original scan dimensions",
		))
	}
	if e.realImageDims.X > 0 && e.realImageDims.Y > 0 && e.realImageDims != e.imageMeta.InputDims {
		addItem(textItem("sb_out_dims",
			fmt.Sprintf("%d × %d", e.realImageDims.X, e.realImageDims.Y),
			"Current working / output dimensions",
		))
	}
	if e.imageMeta.DPIX > 0 {
		var dpiText string
		if e.imageMeta.DPIY > 0 && math.Abs(e.imageMeta.DPIX-e.imageMeta.DPIY) >= 0.5 {
			dpiText = fmt.Sprintf("%d × %d DPI", int(math.Round(e.imageMeta.DPIX)), int(math.Round(e.imageMeta.DPIY)))
		} else {
			dpiText = fmt.Sprintf("%d DPI", int(math.Round(e.imageMeta.DPIX)))
		}
		addItem(textItem("sb_dpi", dpiText, "Embedded scan DPI metadata"))
	}
	addItem(textItem("sb_vp", "Viewport preview", "Full-resolution raster sampled on demand for current zoom"))
	addItem(layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return e.tooltips.Wrap(gtx, "sb_zoom", "Click to reset zoom to fit (100%)", func(gtx layout.Context) layout.Dimensions {
			return e.btnStatusZoom.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if e.btnStatusZoom.Hovered() {
					pointer.CursorPointer.Add(gtx.Ops)
				}
				lbl := material.Label(e.th, unit.Sp(11), fmt.Sprintf("%d%%", int(math.Round(e.mainViewport.Zoom*100))))
				lbl.MaxLines = 1
				if e.btnStatusZoom.Hovered() {
					lbl.Color = colText
				} else {
					lbl.Color = colTextMuted
				}
				return lbl.Layout(gtx)
			})
		})
	}))

	padX := gtx.Dp(12)
	innerH := max(0, h-borderH)
	inner := gtx
	inner.Constraints = layout.Constraints{
		Min: image.Pt(0, 0),
		Max: image.Pt(max(0, w-2*padX), innerH),
	}

	macro := op.Record(gtx.Ops)
	rowDims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(10)}.Layout(inner, items...)
	call := macro.Stop()

	offY := borderH + max(0, (innerH-rowDims.Size.Y)/2)
	tr := op.Offset(image.Pt(padX, offY)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	tr.Pop()

	return layout.Dimensions{Size: image.Pt(w, h)}
}

func (e *EditorUI) layoutLoadingOverlay(gtx layout.Context) layout.Dimensions {
	size := gtx.Constraints.Max
	// Absorb all pointer events beneath loading screen
	e.modalBackdropBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		paint.FillShape(gtx.Ops, colLoadingOpaque, clip.Rect(image.Rectangle{Max: size}).Op())
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			macro := op.Record(gtx.Ops)
			dims := layout.Inset{Top: unit.Dp(20), Bottom: unit.Dp(20), Left: unit.Dp(28), Right: unit.Dp(28)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(14)}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return drawSpinner(gtx, unit.Dp(24), unit.Dp(3), colBorder, colPrimary)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(e.th, unit.Sp(14), e.loadingMessage)
						lbl.Color = colText
						return lbl.Layout(gtx)
					}),
				)
			})
			call := macro.Stop()
			rect := image.Rectangle{Max: dims.Size}
			fillRRect(gtx.Ops, rect, gtx.Dp(8), colSurface)
			strokeRRect(gtx.Ops, rect, gtx.Dp(8), float32(gtx.Dp(1)), colBorder)
			call.Add(gtx.Ops)
			return dims
		})
	})
	_ = color.NRGBA{}
	return layout.Dimensions{Size: size}
}
