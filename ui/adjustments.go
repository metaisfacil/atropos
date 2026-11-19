package ui

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func (e *EditorUI) layoutAdjustmentsAccordion(gtx layout.Context, maxH int) layout.Dimensions {
	canPost := e.imageLoaded && e.postCropAvailable() && !e.isBusy()

	if e.btnSelToggle.Clicked(gtx) && canPost {
		next := !e.adjustmentSelectionActive
		e.adjustmentSelectionActive = next
		if next {
			e.useTouchupTool = false
			e.touchupStrokes = nil
			e.touchupCursor = nil
			e.useStraightEdgeTool = false
		} else {
			e.adjustmentRect = nil
		}
	}

	headerExtra := func(gtx layout.Context) layout.Dimensions {
		if !e.accAdjustmentsOpen {
			return layout.Dimensions{}
		}
		return e.tooltips.Wrap(gtx, "btn_adj_selection", "Limit adjustments to a rectangular selection", func(gtx layout.Context) layout.Dimensions {
			cg := gtx
			if !canPost {
				cg = cg.Disabled()
			}
			w, h := cg.Dp(24), cg.Dp(22)
			return e.btnSelToggle.Layout(cg, func(gtx layout.Context) layout.Dimensions {
				if canPost && e.btnSelToggle.Hovered() {
					pointer.CursorPointer.Add(gtx.Ops)
				}
				bg := colSurface
				border := colBorderLight
				fg := colTextSubtle
				if e.adjustmentSelectionActive {
					bg = colPrimary
					border = colPrimary
					fg = colWhite
				} else if e.btnSelToggle.Hovered() && canPost {
					bg = colSurfaceHover
				}
				if !canPost {
					bg = scaleAlpha(bg, 0.5)
					border = scaleAlpha(border, 0.5)
					fg = scaleAlpha(fg, 0.5)
				}
				rect := image.Rect(0, 0, w, h)
				fillRRect(gtx.Ops, rect, gtx.Dp(4), bg)
				strokeRRect(gtx.Ops, rect, gtx.Dp(4), float32(gtx.Dp(1)), border)

				padX := float32(w) * 0.25
				padY := float32(h) * 0.25
				drawDashedRect(gtx.Ops, padX, padY, float32(w)-2*padX, float32(h)-2*padY,
					float32(gtx.Dp(1.5)), float32(gtx.Dp(2)), float32(gtx.Dp(2)),
					color.NRGBA{}, fg)
				return layout.Dimensions{Size: image.Pt(w, h)}
			})
		})
	}

	return e.accAdjustments.Layout(gtx, e.th, "Adjustments", e.accAdjustmentsOpen, maxH, headerExtra,
		func() { e.accAdjustmentsOpen = !e.accAdjustmentsOpen },
		func(gtx layout.Context) layout.Dimensions {
			return e.layoutAdjustmentsBody(gtx)
		},
	)
}

func (e *EditorUI) layoutAdjustmentsBody(gtx layout.Context) layout.Dimensions {
	busy := e.isBusy()
	postReady := e.imageLoaded && e.postCropAvailable() && !busy

	if e.btnResize.Clicked(gtx) && postReady {
		e.resizeState.open(e)
	}
	if e.btnTrim.Clicked(gtx) && postReady {
		e.handleTrimBorders()
	}
	if e.btnAutoContrast.Clicked(gtx) && postReady {
		e.handleAutoContrast()
	}
	if e.btnDescreenToggle.Clicked(gtx) && postReady {
		next := !e.useDescreenTool
		e.useDescreenTool = next
		if next {
			e.useDustRemovalTool = false
			e.useTouchupTool = false
			e.touchupStrokes = nil
			e.touchupCursor = nil
		}
	}
	if e.btnDustToggle.Clicked(gtx) && postReady {
		next := !e.useDustRemovalTool
		e.useDustRemovalTool = next
		if next {
			e.useDescreenTool = false
			e.useTouchupTool = false
			e.touchupStrokes = nil
			e.touchupCursor = nil
		}
	}
	if e.btnTouchupToggle.Clicked(gtx) && postReady {
		next := !e.useTouchupTool
		e.useTouchupTool = next
		if next {
			e.useDescreenTool = false
			e.useDustRemovalTool = false
			e.useStraightEdgeTool = false
			e.adjustmentSelectionActive = false
		} else {
			e.touchupStrokes = nil
			e.touchupCursor = nil
		}
	}
	if e.btnStraightEdgeToggle.Clicked(gtx) && e.mode == "disc" && e.discActive && !e.useTouchupTool && !busy {
		e.useStraightEdgeTool = !e.useStraightEdgeTool
	}
	if e.btnApplyDust.Clicked(gtx) && postReady {
		e.handleDustRemoval()
	}
	if e.chkDescreenFast.Clicked(gtx) && postReady {
		e.descreenFast = !e.descreenFast
	}
	if e.btnApplyDescreen.Clicked(gtx) && postReady {
		e.handleDescreen()
	}
	if e.chkUseStretch.Clicked(gtx) && e.imageLoaded && !busy {
		e.useStretch = !e.useStretch
	}

	makeBtn := func(clk *widget.Clickable, label string, enabled, active bool) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			st := defaultButtonStyle(btnAdjust, label)
			st.Enabled = enabled
			st.Active = active
			return layoutButton(gtx, e.th, clk, st)
		}
	}

	autoLabel := "Auto-contrast"
	if e.autoContrastPend {
		autoLabel = "Auto-contrast…"
	}

	rows := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(6)}.Layout(gtx,
				layout.Flexed(1, makeBtn(&e.btnResize, "Resize image", postReady, false)),
				layout.Flexed(1, makeBtn(&e.btnTrim, "Trim borders", postReady, false)),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(6)}.Layout(gtx,
				layout.Flexed(1, makeBtn(&e.btnAutoContrast, autoLabel, postReady, false)),
				layout.Flexed(1, makeBtn(&e.btnDescreenToggle, "Descreen", postReady, e.useDescreenTool)),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(6)}.Layout(gtx,
				layout.Flexed(1, makeBtn(&e.btnDustToggle, "Dust removal", postReady, e.useDustRemovalTool)),
				layout.Flexed(1, makeBtn(&e.btnTouchupToggle, "Touch-up brush", postReady, e.useTouchupTool)),
			)
		}),
	}

	if e.mode == "disc" {
		seEnabled := e.discActive && !e.useTouchupTool && !busy
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.tooltips.Wrap(gtx, "btn_straight_edge",
				"Draw a line along an edge that should become horizontal",
				makeBtn(&e.btnStraightEdgeToggle, "Straight edge", seEnabled, e.useStraightEdgeTool),
			)
		}))
	}

	if postReady && e.useDustRemovalTool {
		applyDustLabel := "Apply"
		if e.dustRemovalPending {
			applyDustLabel = "Removing dust…"
		}
		dustOpts := []uiSelectOption{
			{Value: "low", Label: "Low"},
			{Value: "medium", Label: "Medium"},
			{Value: "high", Label: "High"},
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(6)}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return e.ddDustLevel.Layout(gtx, e.th, e.overlays, dustOpts, e.dustLevel, !busy, func(val string) {
							e.dustLevel = val
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						st := defaultButtonStyle(btnPrimary, applyDustLabel)
						st.Enabled = !busy
						st.FontSize = unit.Sp(12)
						return layoutButton(gtx, e.th, &e.btnApplyDust, st)
					}),
				)
			})
		}))
	}

	if postReady && e.useDescreenTool {
		applyDescreenLabel := "Apply descreen"
		if e.descreenPending {
			applyDescreenLabel = "Descreening…"
		}
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(6)}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return e.layoutLabeledSlider(gtx, "Thresh", "sld_desc_thresh", "FFT peak detection threshold",
							func(gtx layout.Context) layout.Dimensions {
								return e.sldDescreenThresh.LayoutInt(gtx, e.th, e.descreenThresh, 50, 150, "", unit.Dp(40), !busy,
									func(v int) { e.descreenThresh = v }, nil)
							})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return e.layoutLabeledSlider(gtx, "Radius", "sld_desc_radius", "Suppression radius around detected peaks",
							func(gtx layout.Context) layout.Dimensions {
								return e.sldDescreenRadius.LayoutInt(gtx, e.th, e.descreenRadius, 1, 20, "", unit.Dp(40), !busy,
									func(v int) { e.descreenRadius = v }, nil)
							})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return e.layoutLabeledSlider(gtx, "Middle", "sld_desc_middle", "Center DC protection band",
							func(gtx layout.Context) layout.Dimensions {
								return e.sldDescreenMiddle.LayoutInt(gtx, e.th, e.descreenMiddle, 1, 10, "", unit.Dp(40), !busy,
									func(v int) { e.descreenMiddle = v }, nil)
							})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return e.layoutLabeledSlider(gtx, "Highs", "sld_desc_highs", "High-frequency preservation weight",
							func(gtx layout.Context) layout.Dimensions {
								return e.sldDescreenHighlight.LayoutInt(gtx, e.th, e.descreenHighlight, 0, 100, "", unit.Dp(40), !busy,
									func(v int) { e.descreenHighlight = v }, nil)
							})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layoutCheckbox(gtx, e.th, &e.chkDescreenFast, e.descreenFast, "Fast mode (luminance only)", "", !busy)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						st := defaultButtonStyle(btnPrimary, applyDescreenLabel)
						st.Enabled = !busy
						st.FontSize = unit.Sp(12)
						return layoutButton(gtx, e.th, &e.btnApplyDescreen, st)
					}),
				)
			})
		}))
	}

	if postReady && e.useTouchupTool {
		rows = append(rows, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutLabeledSlider(gtx, "Brush Radius", "sld_touchup_radius",
				"Brush size in pixels (Right-drag horizontally on canvas to resize)",
				func(gtx layout.Context) layout.Dimensions {
					return e.sldBrushSize.LayoutInt(gtx, e.th, int(e.brushSize), touchupBrushMin, touchupBrushMax, "px", unit.Dp(50), !busy,
						func(v int) { e.brushSize = float64(v) }, nil)
				})
		}))
	}

	levelsEnabled := e.imageLoaded && !busy
	rows = append(rows,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layoutCheckbox(gtx, e.th, &e.chkUseStretch, e.useStretch, "Pre-stretch contrast for detection", "", e.imageLoaded && !busy)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutLabeledSlider(gtx, "Black", "sld_black", "Black input level",
				func(gtx layout.Context) layout.Dimensions {
					maxBlack := max(0, e.whitePoint-1)
					return e.sldBlackPoint.LayoutInt(gtx, e.th, e.blackPoint, 0, maxBlack, "", unit.Dp(40), levelsEnabled,
						func(v int) { e.blackPoint = v },
						func(v int) { e.handleLevelsCommit(v, e.whitePoint) },
					)
				})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return e.layoutLabeledSlider(gtx, "White", "sld_white", "White input level",
				func(gtx layout.Context) layout.Dimensions {
					minWhite := min(255, e.blackPoint+1)
					return e.sldWhitePoint.LayoutInt(gtx, e.th, e.whitePoint, minWhite, 255, "", unit.Dp(40), levelsEnabled,
						func(v int) { e.whitePoint = v },
						func(v int) { e.handleLevelsCommit(e.blackPoint, v) },
					)
				})
		}),
	)

	_ = f32.Point{}
	_ = clip.Rect{}
	_ = paint.ColorOp{}
	_ = material.Label
	return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(6)}.Layout(gtx, rows...)
}
