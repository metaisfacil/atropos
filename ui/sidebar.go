package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

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

func (e *EditorUI) layoutSidebar(gtx layout.Context) layout.Dimensions {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, colSidebarBg, clip.Rect(image.Rectangle{Max: size}).Op())
	// 1dp right border
	borderW := max(1, gtx.Dp(1))
	paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(size.X-borderW, 0, size.X, size.Y)).Op())

	inner := gtx
	inner.Constraints = layout.Exact(image.Pt(max(1, size.X-borderW), size.Y))
	hPad := layout.Inset{Left: unit.Dp(20), Right: unit.Dp(20)}

	return layout.Inset{Top: unit.Dp(20), Bottom: unit.Dp(20)}.Layout(inner, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hPad.Layout(gtx, e.layoutModeTabs)
			}),
			layout.Flexed(1, e.layoutMiddleModeArea),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return hPad.Layout(gtx, e.layoutBottomIsland)
			}),
		)
	})
}

func (e *EditorUI) layoutSidebarSash(gtx layout.Context, sidebarW int) {
	sashHalf := max(3, gtx.Dp(3))
	rect := image.Rect(sidebarW-sashHalf, 0, sidebarW+sashHalf, gtx.Constraints.Max.Y)
	stack := clip.Rect(rect).Push(gtx.Ops)
	event.Op(gtx.Ops, &e.sidebarSashTag)
	pointer.CursorColResize.Add(gtx.Ops)
	stack.Pop()

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &e.sidebarSashTag,
			Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel,
		})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Press:
			if pe.Buttons.Contain(pointer.ButtonPrimary) {
				now := time.Now()
				if now.Sub(e.lastSashClickAt) < 350*time.Millisecond {
					e.sidebarWidthDp = defaultSidebarWidthDp
					e.lastSashClickAt = time.Time{}
					e.sidebarSashDrag = nil
					continue
				}
				e.lastSashClickAt = now
				gtx.Execute(pointer.GrabCmd{Tag: &e.sidebarSashTag, ID: pe.PointerID})
				e.sidebarSashDrag = &sidebarSashDragState{
					startX:     pe.Position.X,
					startWidth: e.sidebarWidthDp,
				}
			}
		case pointer.Drag:
			if e.sidebarSashDrag != nil {
				dxDp := (pe.Position.X - e.sidebarSashDrag.startX) / float32(gtx.Metric.PxPerDp)
				next := e.sidebarSashDrag.startWidth + dxDp
				if next < minSidebarWidthDp {
					next = minSidebarWidthDp
				}
				if next > maxSidebarWidthDp {
					next = maxSidebarWidthDp
				}
				e.sidebarWidthDp = next
			}
		case pointer.Release, pointer.Cancel:
			e.sidebarSashDrag = nil
		}
	}
}

func (e *EditorUI) layoutModeTabs(gtx layout.Context) layout.Dimensions {
	tabs := []struct {
		id   string
		name string
		hint string
		clk  *widget.Clickable
	}{
		{"corner", "Corner", "Switch to Corner mode (click 4 corners)", &e.btnModeCorner},
		{"disc", "Disc", "Switch to Disc mode (circular crop & level)", &e.btnModeDisc},
		{"line", "Line", "Switch to Line mode (draw 4 edges)", &e.btnModeLine},
		{"normal", "Normal", "Switch to Normal mode (rectangular crop)", &e.btnModeNormal},
	}

	preBusy := e.isBusy()
	for _, t := range tabs {
		if t.clk.Clicked(gtx) && !preBusy {
			e.handleModeSwitch(t.id)
		}
	}
	busy := e.isBusy()

	children := make([]layout.FlexChild, 0, len(tabs))
	for _, t := range tabs {
		t := t
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return e.tooltips.Wrap(gtx, "mode_"+t.id, t.hint, func(gtx layout.Context) layout.Dimensions {
				st := defaultButtonStyle(btnModeTab, t.name)
				st.Active = e.mode == t.id
				st.Enabled = !busy
				return layoutButton(gtx, e.th, t.clk, st)
			})
		}))
	}
	return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(6)}.Layout(gtx, children...)
}

func (e *EditorUI) layoutMiddleModeArea(gtx layout.Context) layout.Dimensions {
	avail := gtx.Constraints.Max
	if avail.Y <= 0 {
		return layout.Dimensions{Size: avail}
	}

	contentW := max(1, avail.X-2*gtx.Dp(20))
	measureGtx := gtx
	measureGtx.Constraints = layout.Constraints{
		Min: image.Pt(contentW, 0),
		Max: image.Pt(contentW, 1<<20),
	}
	macro := op.Record(gtx.Ops)
	panelDims := e.layoutActiveModePanel(measureGtx)
	call := macro.Stop()
	e.modePanelH = panelDims.Size.Y

	if panelDims.Size.Y <= avail.Y {
		padTop := (avail.Y - panelDims.Size.Y) / 2
		st := op.Offset(image.Pt(gtx.Dp(20), padTop)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
		return layout.Dimensions{Size: avail}
	}

	layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return styledList(e.th, &e.sidebarList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(20), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				call.Add(gtx.Ops)
				return panelDims
			})
		})
	})
	return layout.Dimensions{Size: avail}
}

func (e *EditorUI) layoutActiveModePanel(gtx layout.Context) layout.Dimensions {
	switch e.mode {
	case "corner":
		return e.layoutCornerPanel(gtx)
	case "disc":
		return e.layoutDiscPanel(gtx)
	case "line":
		return e.layoutLinePanel(gtx)
	case "normal":
		return e.layoutNormalPanel(gtx)
	default:
		return layout.Dimensions{}
	}
}

func (e *EditorUI) layoutLabeledSlider(
	gtx layout.Context,
	label string,
	hintID, hint string,
	w layout.Widget,
) layout.Dimensions {
	return e.tooltips.Wrap(gtx, hintID, hint, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(4)}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(e.th, unit.Sp(12), label)
				lbl.Color = colTextSubtle
				return lbl.Layout(gtx)
			}),
			layout.Rigid(w),
		)
	})
}

func (e *EditorUI) layoutInfoBox(gtx layout.Context, text string) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Label(e.th, unit.Sp(12), text)
		lbl.Color = colTextHint
		return lbl.Layout(gtx)
	})
	call := macro.Stop()
	if gtx.Constraints.Max.X > dims.Size.X {
		dims.Size.X = gtx.Constraints.Max.X
	}
	rect := image.Rectangle{Max: dims.Size}
	fillRRect(gtx.Ops, rect, gtx.Dp(6), colSurface)
	strokeRRect(gtx.Ops, rect, gtx.Dp(6), float32(gtx.Dp(1)), colBorder)
	call.Add(gtx.Ops)
	return dims
}

func (e *EditorUI) layoutCornerPanel(gtx layout.Context) layout.Dimensions {
	enabled := !e.cropSkipped && !e.isBusy()
	if e.chkCustomCorner.Clicked(gtx) && enabled {
		e.customCorner = !e.customCorner
	}

	return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutLabeledSlider(gtx, "Max Corners", "sld_max_corners",
				"Maximum number of Shi-Tomasi corners to detect",
				func(gtx layout.Context) layout.Dimensions {
					return e.sldMaxCorners.LayoutInt(gtx, e.th, e.maxCorners, 1, 1000, "", unit.Dp(44), enabled,
						func(v int) { e.maxCorners = v },
						func(v int) {
							e.maxCorners = v
							next := e.settings
							next.CornerMaxCorners = v
							e.saveSettings(next)
						},
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutLabeledSlider(gtx, "Quality Level", "sld_quality",
				"Minimum relative corner quality threshold (1–100)",
				func(gtx layout.Context) layout.Dimensions {
					return e.sldQualityLevel.LayoutInt(gtx, e.th, e.qualityLevel, 1, 100, "", unit.Dp(44), enabled,
						func(v int) { e.qualityLevel = v }, nil)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutLabeledSlider(gtx, "Min Distance", "sld_min_dist",
				"Minimum pixel separation between detected corners",
				func(gtx layout.Context) layout.Dimensions {
					return e.sldMinDistance.LayoutInt(gtx, e.th, e.minDistance, 1, 200, "", unit.Dp(44), enabled,
						func(v int) { e.minDistance = v },
						func(v int) {
							e.minDistance = v
							next := e.settings
							next.CornerMinDistance = v
							e.saveSettings(next)
						},
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutLabeledSlider(gtx, "Accent", "sld_accent",
				"Corner enhancement / sharpening strength before detection",
				func(gtx layout.Context) layout.Dimensions {
					return e.sldAccent.LayoutInt(gtx, e.th, e.accentValue, 0, 30, "", unit.Dp(44), enabled,
						func(v int) { e.accentValue = v }, nil)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutLabeledSlider(gtx, "Corner Dot Size", "sld_dot_radius",
				"Visual radius of corner markers in preview",
				func(gtx layout.Context) layout.Dimensions {
					return e.sldDotRadius.LayoutInt(gtx, e.th, e.dotRadius, 2, 80, "", unit.Dp(44), enabled,
						func(v int) { e.dotRadius = v }, nil)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(4)}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutCheckbox(gtx, e.th, &e.chkCustomCorner, e.customCorner, "Custom corner placement", "", enabled)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(e.th, unit.Sp(11), "When enabled, click anywhere to place a corner instead of snapping to detected corners.")
						lbl.Color = colTextMuted
						if !enabled {
							lbl.Color = scaleAlpha(lbl.Color, 0.5)
						}
						return lbl.Layout(gtx)
					}),
				)
			})
		}),
	)
}

func (e *EditorUI) layoutDiscPanel(gtx layout.Context) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutInfoBox(gtx, "Click and drag on the image to draw a circle around the disc.")
		}),
	}
	if e.discActive && e.discRadius > 0 {
		enabled := !e.isBusy()
		minR := max(1, e.discInitialRadius-300)
		maxR := max(minR+1, e.discInitialRadius+300)
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return e.layoutLabeledSlider(gtx, "Disc Radius", "sld_disc_radius",
					"Adjust the radius of the extracted disc circle",
					func(gtx layout.Context) layout.Dimensions {
						return e.sldDiscRadius.LayoutInt(gtx, e.th, e.discRadius, minR, maxR, "px", unit.Dp(54), enabled,
							func(v int) { e.discRadius = v },
							func(v int) { e.handleDiscRadiusCommit(v) },
						)
					})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return e.layoutLabeledSlider(gtx, "Feather Size", "sld_disc_feather",
					"Edge blend softness around the circular disc boundary (Shift+Wheel)",
					func(gtx layout.Context) layout.Dimensions {
						return e.sldDiscFeather.LayoutInt(gtx, e.th, e.featherSize, 0, 100, "", unit.Dp(44), enabled,
							func(v int) { e.featherSize = v },
							func(v int) { e.handleDiscFeatherCommit(v) },
						)
					})
			}),
		)
		if e.settings.DiscCenterCutout {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return e.layoutLabeledSlider(gtx, "Cutout %", "sld_disc_cutout",
					"Center hole cutout radius as percentage of disc radius",
					func(gtx layout.Context) layout.Dimensions {
						return e.sldDiscCutout.LayoutInt(gtx, e.th, e.settings.DiscCutoutPercent, 0, 50, "%", unit.Dp(44), enabled,
							func(v int) { e.settings.DiscCutoutPercent = v },
							func(v int) { e.handleDiscCutoutCommit(v) },
						)
					})
			}))
		}
	}
	return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx, children...)
}

func (e *EditorUI) layoutLinePanel(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutInfoBox(gtx, "Click and drag to draw 4 lines defining the document edges. Perspective correction is applied automatically after the 4th line.")
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			macro := op.Record(gtx.Ops)
			dims := layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(e.th, unit.Sp(12), "Lines Drawn")
						lbl.Color = colTextSubtle
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(e.th, unit.Sp(13), fmt.Sprintf("%d/4", len(e.lines)))
						lbl.Color = colPrimary
						lbl.Font.Weight = font.Bold
						return lbl.Layout(gtx)
					}),
				)
			})
			call := macro.Stop()
			if gtx.Constraints.Max.X > dims.Size.X {
				dims.Size.X = gtx.Constraints.Max.X
			}
			rect := image.Rectangle{Max: dims.Size}
			fillRRect(gtx.Ops, rect, gtx.Dp(6), colSurface)
			strokeRRect(gtx.Ops, rect, gtx.Dp(6), float32(gtx.Dp(1)), colBorder)
			call.Add(gtx.Ops)
			return dims
		}),
	)
}

func (e *EditorUI) layoutNormalPanel(gtx layout.Context) layout.Dimensions {
	return e.layoutInfoBox(gtx, "Drag on the image to draw a crop rectangle. Press Ctrl+C to copy it, or click Crop or press Enter to apply.")
}

func (e *EditorUI) layoutBottomIsland(gtx layout.Context) layout.Dimensions {
	winH := e.lastWindowSz.Y
	if winH <= 0 {
		winH = gtx.Dp(900)
	}
	openCount := 0
	if e.accAdjustmentsOpen {
		openCount++
	}
	if e.accToolsOpen {
		openCount++
	}
	if e.accShortcutsOpen {
		openCount++
	}
	maxAccH := int(0.30 * float64(winH))
	switch openCount {
	case 2:
		maxAccH = int(0.18 * float64(winH))
	case 3:
		maxAccH = int(0.12 * float64(winH))
	}
	if maxAccH < gtx.Dp(110) {
		maxAccH = gtx.Dp(110)
	}

	children := []layout.FlexChild{
		layout.Rigid(e.layoutFadeStrip),
		layout.Rigid(e.layoutActionButtons),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutAdjustmentsAccordion(gtx, maxAccH)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutToolsAccordion(gtx, maxAccH)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutShortcutsAccordion(gtx, maxAccH)
		}),
		layout.Rigid(e.layoutFooterCard),
	}
	return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(6)}.Layout(gtx, children...)
}

func (e *EditorUI) layoutFadeStrip(gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(10)
	w := gtx.Constraints.Max.X
	steps := 5
	stepH := max(1, h/steps)
	for i := 0; i < steps; i++ {
		alpha := uint8((i + 1) * 255 / steps)
		c := color.NRGBA{R: colSidebarBg.R, G: colSidebarBg.G, B: colSidebarBg.B, A: alpha}
		y0 := i * stepH
		y1 := y0 + stepH
		if i == steps-1 {
			y1 = h
		}
		paint.FillShape(gtx.Ops, c, clip.Rect(image.Rect(0, y0, w, y1)).Op())
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func (e *EditorUI) canResetMode() bool {
	if e.canRecrop || e.cropSkipped {
		return true
	}
	switch e.mode {
	case "corner":
		return e.cornerCount > 0
	case "disc":
		return e.discActive
	case "line":
		return len(e.lines) > 0
	case "normal":
		return e.normalRect != nil
	}
	return false
}

func (e *EditorUI) layoutActionButtons(gtx layout.Context) layout.Dimensions {
	busy := e.isBusy()
	if !e.canRecrop && !e.cropSkipped && e.btnSkipCrop.Clicked(gtx) && e.imageLoaded && !busy {
		e.handleSkipCrop()
	}
	if (e.canRecrop || e.cropSkipped) && e.btnRecrop.Clicked(gtx) && e.imageLoaded && !busy {
		e.handleRecrop()
	}
	if e.canResetMode() && e.btnResetAction.Clicked(gtx) && e.imageLoaded && !busy {
		e.handleResetAction()
	}

	var rows []layout.FlexChild

	if e.mode == "corner" {
		if e.btnPrimaryAction.Clicked(gtx) && e.imageLoaded && !busy && !e.cropSkipped {
			e.handleDetectCorners()
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.tooltips.Wrap(gtx, "btn_detect", "Detect candidate corners in the scan", func(gtx layout.Context) layout.Dimensions {
				st := defaultButtonStyle(btnPrimary, "Detect")
				st.Enabled = e.imageLoaded && !busy && !e.cropSkipped
				return layoutButton(gtx, e.th, &e.btnPrimaryAction, st)
			})
		}))
	} else if e.mode == "normal" {
		if e.btnPrimaryAction.Clicked(gtx) && e.imageLoaded && !busy && e.normalRect != nil && !e.cropSkipped {
			e.handleNormalCrop()
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.tooltips.Wrap(gtx, "btn_normal_crop", "Crop image to selected rectangle (Enter)", func(gtx layout.Context) layout.Dimensions {
				st := defaultButtonStyle(btnPrimary, "Crop")
				st.Enabled = e.imageLoaded && !busy && e.normalRect != nil && !e.cropSkipped
				return layoutButton(gtx, e.th, &e.btnPrimaryAction, st)
			})
		}))
	}

	showSkip := !e.canRecrop && !e.cropSkipped
	showRecrop := e.canRecrop || e.cropSkipped
	showReset := e.canResetMode()

	if showSkip {
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.tooltips.Wrap(gtx, "btn_skip_crop", "Skip cropping and proceed directly to adjustments / saving", func(gtx layout.Context) layout.Dimensions {
				st := defaultButtonStyle(btnSkip, "Skip crop")
				st.Enabled = e.imageLoaded && !busy
				return layoutButton(gtx, e.th, &e.btnSkipCrop, st)
			})
		}))
	}

	if showRecrop || showReset {
		resetLabel := "Reset"
		if e.mode == "corner" && !e.cropSkipped && e.cornerCount > 0 && e.cornerCount < 4 {
			resetLabel = fmt.Sprintf("Reset (%d/4)", e.cornerCount)
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			var cols []layout.FlexChild
			if showRecrop {
				cols = append(cols, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return e.tooltips.Wrap(gtx, "btn_recrop", "Promote the current output to be the new source image and restart cropping", func(gtx layout.Context) layout.Dimensions {
						st := defaultButtonStyle(btnRecrop, "Re-crop")
						st.Enabled = e.imageLoaded && !busy
						return layoutButton(gtx, e.th, &e.btnRecrop, st)
					})
				}))
			}
			if showReset {
				cols = append(cols, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return e.tooltips.Wrap(gtx, "btn_reset", "Reset current selection or crop and return to pre-crop phase", func(gtx layout.Context) layout.Dimensions {
						st := defaultButtonStyle(btnDanger, resetLabel)
						st.Enabled = e.imageLoaded && !busy
						return layoutButton(gtx, e.th, &e.btnResetAction, st)
					})
				}))
			}
			return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(8)}.Layout(gtx, cols...)
		}))
	}

	if len(rows) == 0 {
		return layout.Dimensions{}
	}
	return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(6)}.Layout(gtx, rows...)
	})
}

func (e *EditorUI) layoutToolsAccordion(gtx layout.Context, maxH int) layout.Dimensions {
	if e.btnOpenCompositor.Clicked(gtx) && !e.isBusy() {
		e.compositorState.open(e)
	}
	return e.accTools.Layout(gtx, e.th, "Tools", e.accToolsOpen, maxH, nil,
		func() { e.accToolsOpen = !e.accToolsOpen },
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(6)}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					st := defaultButtonStyle(btnSecondary, "Image Compositor")
					st.Enabled = !e.isBusy()
					return layoutButton(gtx, e.th, &e.btnOpenCompositor, st)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(11), "Stitch multiple overlapping scan segments into a single continuous image.")
					lbl.Color = colTextMuted
					return lbl.Layout(gtx)
				}),
			)
		},
	)
}

func (e *EditorUI) layoutShortcutsAccordion(gtx layout.Context, maxH int) layout.Dimensions {
	lblFor := func(code, fallback string) string {
		if v := strings.TrimSpace(e.spatialLabels[code]); v != "" {
			return v
		}
		return fallback
	}
	keyW := lblFor("KeyW", "W")
	keyA := lblFor("KeyA", "A")
	keyS := lblFor("KeyS", "S")
	keyD := lblFor("KeyD", "D")
	keyQ := lblFor("KeyQ", "Q")
	keyE := lblFor("KeyE", "E")

	rowItem := func(keys []string, desc string, enabled bool) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			rowGtx := gtx
			rowGtx.Constraints.Min.X = rowGtx.Constraints.Max.X
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: rowGtx.Dp(8)}.Layout(rowGtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					badges := make([]layout.FlexChild, 0, len(keys))
					for _, k := range keys {
						k := k
						badges = append(badges, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return drawKbdBadge(gtx, e.th, k, enabled)
						}))
					}
					return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(3)}.Layout(gtx, badges...)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(11.5), desc)
					lbl.Alignment = text.End
					lbl.Color = colTextSubtle
					if !enabled {
						lbl.Color = scaleAlpha(lbl.Color, 0.35)
					}
					cg := gtx
					cg.Constraints.Min.X = cg.Constraints.Max.X
					return lbl.Layout(cg)
				}),
			)
		})
	}

	return e.accShortcuts.Layout(gtx, e.th, "Shortcuts", e.accShortcutsOpen, maxH, nil,
		func() { e.accShortcutsOpen = !e.accShortcutsOpen },
		func(gtx layout.Context) layout.Dimensions {
			postReady := e.canSave()
			loaded := e.imageLoaded
			// Mirrors the isDisc condition in applyKeyboardRotate.
			rotateLabel := "Rotate 90° CCW / CW"
			if e.mode == "disc" && e.discActive && e.discRadius > 0 {
				rotateLabel = "Rotate disc ±15°"
			}
			items := []layout.FlexChild{
				rowItem([]string{keyW, keyA, keyS, keyD}, "Crop edges", postReady),
				rowItem([]string{keyQ, keyE}, rotateLabel, postReady),
				rowItem([]string{"Ctrl+Z", "Ctrl+Y"}, "Undo / Redo", loaded),
				rowItem([]string{"Ctrl+C"}, "Copy image or selection", loaded),
				rowItem([]string{"Ctrl+V"}, "Paste image", true),
				rowItem([]string{"Ctrl+O", "Ctrl+S"}, "Open / Save", true),
				rowItem([]string{"Space+Drag"}, "Pan viewport", loaded),
			}
			if e.mode == "disc" && e.discActive && e.discRadius > 0 {
				items = append(items,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						w := gtx.Constraints.Max.X
						paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, 2, w, 3)).Op())
						return layout.Dimensions{Size: image.Pt(w, 5)}
					}),
					rowItem([]string{"Arrows"}, "Nudge disc (+Shift 20px)", true),
					rowItem([]string{"Ctrl+Drag"}, "Shift disc", true),
					rowItem([]string{"Shift+Drag"}, "Rotate disc (circle the centre)", true),
					rowItem([]string{"+", "−"}, "Feather size (or Shift+Wheel)", true),
					rowItem([]string{"Y"}, "Pick disc background at cursor", true),
				)
			}
			return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(5)}.Layout(gtx, items...)
		},
	)
}

func (e *EditorUI) layoutFooterCard(gtx layout.Context) layout.Dimensions {
	busy := e.isBusy()
	if e.btnLoadImage.Clicked(gtx) && !busy {
		e.handleLoadImage()
	}
	if e.btnSaveImage.Clicked(gtx) && !busy && e.canSave() {
		e.handleSaveImage(nil)
	}
	if e.btnOptions.Clicked(gtx) {
		e.optionsState.open(e)
	}

	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(10), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(8)}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(8)}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return e.tooltips.Wrap(gtx, "btn_load", "Open an image file (Ctrl+O)", func(gtx layout.Context) layout.Dimensions {
							st := defaultButtonStyle(btnLoad, "Load image")
							st.Enabled = !busy
							return layoutButton(gtx, e.th, &e.btnLoadImage, st)
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return e.tooltips.Wrap(gtx, "btn_save", "Save processed image (Ctrl+S)", func(gtx layout.Context) layout.Dimensions {
							st := defaultButtonStyle(btnSave, "Save image")
							st.Enabled = !busy && e.canSave()
							return layoutButton(gtx, e.th, &e.btnSaveImage, st)
						})
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				st := defaultButtonStyle(btnSecondary, "Options")
				st.Enabled = true
				return layoutButton(gtx, e.th, &e.btnOptions, st)
			}),
		)
	})
	call := macro.Stop()
	if gtx.Constraints.Max.X > dims.Size.X {
		dims.Size.X = gtx.Constraints.Max.X
	}
	rect := image.Rectangle{Max: dims.Size}
	r := gtx.Dp(10)
	fillBottomRRect(gtx.Ops, rect, r, colSurface)
	paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, 0, dims.Size.X, max(1, gtx.Dp(1)))).Op())
	call.Add(gtx.Ops)
	return dims
}
