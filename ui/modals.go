package ui

import (
	"fmt"
	"image"
	"image/color"
	"math"

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

type resizeModalState struct {
	origW      int
	origH      int
	width      int
	height     int
	scalePct   int
	aspectLock bool

	widthIn    uiIntInput
	heightIn   uiIntInput
	scaleIn    uiIntInput
	chkLock    widget.Clickable
	btnCancel  widget.Clickable
	btnApply   widget.Clickable
}

func (r *resizeModalState) open(e *EditorUI) {
	w := max(1, e.realImageDims.X)
	h := max(1, e.realImageDims.Y)
	r.origW = w
	r.origH = h
	r.width = w
	r.height = h
	r.scalePct = 100
	r.aspectLock = true
	e.resizeOpen = true
}

func (r *resizeModalState) setWidth(w int) {
	r.width = clamp(w, 1, 20000)
	if r.aspectLock && r.origW > 0 && r.origH > 0 {
		r.height = clamp(int(math.Round(float64(r.width)*float64(r.origH)/float64(r.origW))), 1, 20000)
	}
	if r.origW > 0 {
		r.scalePct = clamp(int(math.Round(float64(r.width)*100/float64(r.origW))), 1, 1000)
	}
}

func (r *resizeModalState) setHeight(h int) {
	r.height = clamp(h, 1, 20000)
	if r.aspectLock && r.origW > 0 && r.origH > 0 {
		r.width = clamp(int(math.Round(float64(r.height)*float64(r.origW)/float64(r.origH))), 1, 20000)
	}
	if r.origH > 0 {
		r.scalePct = clamp(int(math.Round(float64(r.height)*100/float64(r.origH))), 1, 1000)
	}
}

func (r *resizeModalState) setScale(pct int) {
	r.scalePct = clamp(pct, 1, 1000)
	r.width = clamp(int(math.Round(float64(r.origW)*float64(r.scalePct)/100.0)), 1, 20000)
	r.height = clamp(int(math.Round(float64(r.origH)*float64(r.scalePct)/100.0)), 1, 20000)
}

func (r *resizeModalState) tryApply(e *EditorUI) {
	if r.width < 1 || r.height < 1 {
		return
	}
	deltaPx := r.width*r.height - r.origW*r.origH
	if deltaPx > 2_000_000 {
		w, h := r.width, r.height
		e.confirmDialog = &confirmDialogState{
			Title:       "Large Upscale",
			Message:     fmt.Sprintf("Upscaling from %d×%d to %d×%d adds more than 2 million pixels and may take extra time. Continue?", r.origW, r.origH, w, h),
			YesLabel:    "Continue",
			CancelLabel: "Cancel",
			YesVariant:  btnPrimary,
			OnYes: func() {
				e.confirmDialog = nil
				e.handleResizeApply(w, h)
			},
			OnCancel: func() {
				e.confirmDialog = nil
			},
		}
		return
	}
	e.handleResizeApply(r.width, r.height)
}

var (
	modalScrimTag    uint8
	modalCardStopTag uint8
)

func layoutCenteredModal(
	gtx layout.Context,
	scrimCol color.NRGBA,
	maxW, maxH unit.Dp,
	onBackdropClick func(),
	body layout.Widget,
) {
	dismissed := false
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &modalScrimTag,
			Kinds:  pointer.Press | pointer.Release | pointer.Move | pointer.Drag | pointer.Scroll,
		})
		if !ok {
			break
		}
		if onBackdropClick != nil && !dismissed {
			if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Press && pe.Buttons.Contain(pointer.ButtonPrimary) {
				onBackdropClick()
				dismissed = true
			}
		}
	}
	if dismissed {
		gtx.Execute(op.InvalidateCmd{})
		return
	}

	full := gtx.Constraints.Max
	fullRect := image.Rectangle{Max: full}
	paint.FillShape(gtx.Ops, scrimCol, clip.Rect(fullRect).Op())

	// Absorb all pointer events over the full backdrop so underlying widgets never see them.
	st := clip.Rect(fullRect).Push(gtx.Ops)
	event.Op(gtx.Ops, &modalScrimTag)
	st.Pop()

	wPx := min(gtx.Dp(maxW), max(120, full.X-gtx.Dp(32)))
	hPx := min(gtx.Dp(maxH), max(120, full.Y-gtx.Dp(32)))

	layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		cg := gtx
		cg.Constraints.Min = image.Pt(wPx, 0)
		cg.Constraints.Max = image.Pt(wPx, hPx)

		macro := op.Record(gtx.Ops)
		dims := body(cg)
		call := macro.Stop()

		if dims.Size.X < wPx {
			dims.Size.X = wPx
		}
		cardRect := image.Rectangle{Max: dims.Size}
		cardSt := clip.Rect(cardRect).Push(gtx.Ops)
		event.Op(gtx.Ops, &modalCardStopTag)
		cardSt.Pop()
		// Drain card stop tag events so clicks on card never bubble to backdrop.
		for {
			if _, ok := gtx.Event(pointer.Filter{
				Target: &modalCardStopTag,
				Kinds:  pointer.Press | pointer.Release | pointer.Move | pointer.Drag | pointer.Scroll,
			}); !ok {
				break
			}
		}

		fillRRect(gtx.Ops, cardRect, gtx.Dp(8), colSurface)
		strokeRRect(gtx.Ops, cardRect, gtx.Dp(8), float32(gtx.Dp(1)), colBorder)
		bodyClip := clip.Rect(cardRect).Push(gtx.Ops)
		call.Add(gtx.Ops)
		bodyClip.Pop()
		return dims
	})
}

func flexSpacer(gtx layout.Context) layout.Dimensions {
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
}

func (e *EditorUI) layoutModals(gtx layout.Context) {
	if e.optionsOpen {
		e.optionsState.layout(gtx, e)
	}
	if e.compositorOpen {
		e.compositorState.layout(gtx, e)
	}
	if e.calibrationOpen {
		e.calibState.layout(gtx, e)
	}
	if e.resizeOpen {
		e.layoutResizeModal(gtx)
	}
	if e.aboutOpen {
		e.layoutAboutModal(gtx)
	}
	if e.confirmDialog != nil {
		e.layoutConfirmModal(gtx)
	}
	if e.errorMessage != "" {
		e.layoutErrorModal(gtx)
	}
}

func (e *EditorUI) layoutErrorModal(gtx layout.Context) {
	if e.btnCloseError.Clicked(gtx) {
		e.errorMessage = ""
		return
	}
	layoutCenteredModal(gtx, colBackdrop, unit.Dp(420), unit.Dp(360), nil, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(14)}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(15), "Error")
					lbl.Color = colDanger
					lbl.Font.Weight = font.Bold
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(13), e.errorMessage)
					lbl.Color = colText
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Flexed(1, flexSpacer),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							st := defaultButtonStyle(btnPrimary, "OK")
							return layoutButton(gtx, e.th, &e.btnCloseError, st)
						}),
					)
				}),
			)
		})
	})
}

func (e *EditorUI) layoutConfirmModal(gtx layout.Context) {
	dlg := e.confirmDialog
	if dlg == nil {
		return
	}
	if e.btnConfirmYes.Clicked(gtx) && dlg.OnYes != nil {
		dlg.OnYes()
		return
	}
	if e.btnConfirmNo.Clicked(gtx) && dlg.OnNo != nil {
		dlg.OnNo()
		return
	}
	if e.btnConfirmCancel.Clicked(gtx) {
		if dlg.OnCancel != nil {
			dlg.OnCancel()
		} else {
			e.confirmDialog = nil
		}
		return
	}

	layoutCenteredModal(gtx, colBackdropDark, unit.Dp(430), unit.Dp(320), nil, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(14)}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(15), dlg.Title)
					lbl.Color = colWhite
					lbl.Font.Weight = font.Bold
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(13), dlg.Message)
					lbl.Color = colText
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					var btns []layout.FlexChild
					btns = append(btns, layout.Flexed(1, flexSpacer))
					if dlg.CancelLabel != "" {
						btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							st := defaultButtonStyle(btnSecondary, dlg.CancelLabel)
							return layoutButton(gtx, e.th, &e.btnConfirmCancel, st)
						}))
					}
					if dlg.NoLabel != "" {
						btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							v := dlg.NoVariant
							if v == 0 {
								v = btnDanger
							}
							st := defaultButtonStyle(v, dlg.NoLabel)
							return layoutButton(gtx, e.th, &e.btnConfirmNo, st)
						}))
					}
					if dlg.YesLabel != "" {
						btns = append(btns, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							v := dlg.YesVariant
							st := defaultButtonStyle(v, dlg.YesLabel)
							return layoutButton(gtx, e.th, &e.btnConfirmYes, st)
						}))
					}
					return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(8)}.Layout(gtx, btns...)
				}),
			)
		})
	})
}

func (e *EditorUI) layoutAboutModal(gtx layout.Context) {
	if e.btnAboutOK.Clicked(gtx) {
		e.aboutOpen = false
		return
	}
	if e.btnAboutLink.Clicked(gtx) && e.app != nil {
		url := "https://github.com/metaisfacil/atropos"
		e.runBackend(func() { _ = e.app.OpenURL(url) })
	}

	layoutCenteredModal(gtx, colBackdrop, unit.Dp(320), unit.Dp(420), func() { e.aboutOpen = false }, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			centerLbl := func(sz unit.Sp, txt string, c color.NRGBA, weight font.Weight) layout.FlexChild {
				return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(e.th, sz, txt)
						lbl.Color = c
						lbl.Font.Weight = weight
						lbl.Alignment = text.Middle
						return lbl.Layout(gtx)
					})
				})
			}

			var items []layout.FlexChild
			if e.appIconLoaded {
				items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(72)
						cg := gtx
						cg.Constraints = layout.Exact(image.Pt(sz, sz))
						return widget.Image{Src: e.appIconOp, Fit: widget.Contain}.Layout(cg)
					})
				}))
			}
			items = append(items,
				centerLbl(18, "Atropos", colWhite, font.Bold),
				centerLbl(12, "Version "+e.appVersion, colTextMuted, font.Normal),
				centerLbl(13, "Desktop image processing tool\nfor musical materials", colText, font.Normal),
				centerLbl(12, "by metaisfacil", colTextSubtle, font.Normal),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return e.btnAboutLink.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							if e.btnAboutLink.Hovered() {
								pointer.CursorPointer.Add(gtx.Ops)
							}
							lbl := material.Label(e.th, unit.Sp(12), "https://github.com/metaisfacil/atropos")
							if e.btnAboutLink.Hovered() {
								lbl.Color = colLinkHover
							} else {
								lbl.Color = colPrimary
							}
							return lbl.Layout(gtx)
						})
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						st := defaultButtonStyle(btnPrimary, "OK")
						return layoutButton(gtx, e.th, &e.btnAboutOK, st)
					})
				}),
			)
			return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx, items...)
		})
	})
}

func (e *EditorUI) layoutResizeModal(gtx layout.Context) {
	r := &e.resizeState
	if r.btnCancel.Clicked(gtx) {
		e.resizeOpen = false
		return
	}
	if r.btnApply.Clicked(gtx) {
		r.tryApply(e)
		return
	}
	if r.chkLock.Clicked(gtx) {
		r.aspectLock = !r.aspectLock
	}

	layoutCenteredModal(gtx, colBackdrop, unit.Dp(280), unit.Dp(380), func() { e.resizeOpen = false }, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(12)}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(15), "Resize Image")
					lbl.Color = colWhite
					lbl.Font.Weight = font.Bold
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(11.5), fmt.Sprintf("Current: %d × %d px", r.origW, r.origH))
					lbl.Color = colTextMuted
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(8)}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(4)}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(e.th, unit.Sp(11.5), "Width (px)")
									lbl.Color = colTextSubtle
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return r.widthIn.LayoutInt(gtx, e.th, r.width, 1, 20000, true, &e.anyEditorFocused,
										func(v int) { r.setWidth(v) },
										func() { r.tryApply(e) },
									)
								}),
							)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(4)}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(e.th, unit.Sp(11.5), "Height (px)")
									lbl.Color = colTextSubtle
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return r.heightIn.LayoutInt(gtx, e.th, r.height, 1, 20000, true, &e.anyEditorFocused,
										func(v int) { r.setHeight(v) },
										func() { r.tryApply(e) },
									)
								}),
							)
						}),
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutCheckbox(gtx, e.th, &r.chkLock, r.aspectLock, "Lock aspect ratio", "", true)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(10)}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(e.th, unit.Sp(12), "Scale (%)")
							lbl.Color = colTextSubtle
							return lbl.Layout(gtx)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return r.scaleIn.LayoutInt(gtx, e.th, r.scalePct, 1, 1000, true, &e.anyEditorFocused,
								func(v int) { r.setScale(v) },
								func() { r.tryApply(e) },
							)
						}),
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(8)}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								st := defaultButtonStyle(btnSecondary, "Cancel")
								return layoutButton(gtx, e.th, &r.btnCancel, st)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								st := defaultButtonStyle(btnPrimary, "Apply")
								st.Enabled = r.width > 0 && r.height > 0
								return layoutButton(gtx, e.th, &r.btnApply, st)
							}),
						)
					})
				}),
			)
		})
	})
}
