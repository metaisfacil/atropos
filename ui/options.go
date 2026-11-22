package ui

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type optionsModalState struct {
	activeTab string
	contentLt widget.List

	tabBtns [6]widget.Clickable
	btnDone widget.Clickable

	// Inpainting
	radPatchMatch widget.Clickable
	radIOPaint    widget.Clickable
	iopaintURLIn  uiTextInput

	// Fill & crop
	radFillClamp    widget.Clickable
	radFillSolid    widget.Clickable
	radFillOutpaint widget.Clickable
	btnColorSwatch  widget.Clickable
	fillColorIn     uiTextInput
	cropEdgeIn      uiIntInput

	// Modes
	chkDiscCutout      widget.Clickable
	sldDiscCutoutPct   uiSlider
	chkAutoDetectMode  widget.Clickable
	chkAutoCornerParam widget.Clickable

	// Workflow
	chkTouchupSticky      widget.Clickable
	chkStraightEdgeSticky widget.Clickable

	// Save
	chkCloseAfterSave  widget.Clickable
	chkPostSaveEnabled widget.Clickable
	postSaveCmdIn      uiTextInput

	// Debug
	btnOpenCalib widget.Clickable
}

var optionsTabs = []struct {
	id    string
	label string
}{
	{"inpainting", "Inpainting"},
	{"fill", "Fill & crop"},
	{"modes", "Modes"},
	{"workflow", "Workflow"},
	{"save", "Save"},
	{"debug", "Debug"},
}

func (o *optionsModalState) open(e *EditorUI) {
	if o.activeTab == "" {
		o.activeTab = "inpainting"
	}
	o.contentLt.Axis = layout.Vertical
	o.iopaintURLIn.EnsureInit(e.settings.IOPaintURL, "")
	o.fillColorIn.EnsureInit(e.settings.WarpFillColor, "")
	o.postSaveCmdIn.EnsureInit(e.settings.PostSaveCommand, "")
	e.optionsOpen = true
}

func (o *optionsModalState) layout(gtx layout.Context, e *EditorUI) {
	if o.btnDone.Clicked(gtx) {
		e.optionsOpen = false
		return
	}
	for i, t := range optionsTabs {
		if o.tabBtns[i].Clicked(gtx) {
			o.activeTab = t.id
		}
	}

	layoutCenteredModal(gtx, colBackdrop, unit.Dp(540), unit.Dp(480), func() { e.optionsOpen = false }, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(12), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(15), "Options")
					lbl.Color = colWhite
					lbl.Font.Weight = font.Bold
					return lbl.Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				w := gtx.Constraints.Max.X
				h := max(1, gtx.Dp(1))
				paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, 0, w, h)).Op())
				return layout.Dimensions{Size: image.Pt(w, h)}
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						railW := gtx.Dp(138)
						cg := gtx
						cg.Constraints = layout.Exact(image.Pt(railW, gtx.Constraints.Max.Y))
						paint.FillShape(gtx.Ops, colSidebarBg, clip.Rect(image.Rect(0, 0, railW, gtx.Constraints.Max.Y)).Op())
						borderW := max(1, gtx.Dp(1))
						paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(railW-borderW, 0, railW, gtx.Constraints.Max.Y)).Op())
						layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(8), Right: unit.Dp(10)}.Layout(cg, func(gtx layout.Context) layout.Dimensions {
							rows := make([]layout.FlexChild, len(optionsTabs))
							for i, t := range optionsTabs {
								i, t := i, t
								rows[i] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									st := defaultButtonStyle(btnOptionsTab, t.label)
									st.Active = o.activeTab == t.id
									return layoutButton(gtx, e.th, &o.tabBtns[i], st)
								})
							}
							return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(4)}.Layout(gtx, rows...)
						})
						return layout.Dimensions{Size: image.Pt(railW, gtx.Constraints.Max.Y)}
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(18)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return styledList(e.th, &o.contentLt).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
								return o.layoutTabBody(gtx, e)
							})
						})
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				w := gtx.Constraints.Max.X
				h := max(1, gtx.Dp(1))
				paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, 0, w, h)).Op())
				return layout.Dimensions{Size: image.Pt(w, h)}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(20), Right: unit.Dp(20)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Flexed(1, flexSpacer),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							st := defaultButtonStyle(btnPrimary, "Close")
							return layoutButton(gtx, e.th, &o.btnDone, st)
						}),
					)
				})
			}),
		)
	})
}

func (o *optionsModalState) layoutTabBody(gtx layout.Context, e *EditorUI) layout.Dimensions {
	s := e.settings
	sectionTitle := func(txt string) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(e.th, unit.Sp(13), txt)
			lbl.Color = colWhite
			lbl.Font.Weight = font.Bold
			return lbl.Layout(gtx)
		})
	}
	mutedNote := func(txt string) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(e.th, unit.Sp(11.5), txt)
			lbl.Color = colTextMuted
			return lbl.Layout(gtx)
		})
	}

	switch o.activeTab {
	case "inpainting":
		if o.radPatchMatch.Clicked(gtx) {
			next := s
			next.TouchupBackend = "patchmatch"
			e.saveSettings(next)
		}
		if o.radIOPaint.Clicked(gtx) {
			next := s
			next.TouchupBackend = "iopaint"
			e.saveSettings(next)
		}
		backend := s.TouchupBackend
		if backend == "" {
			backend = "patchmatch"
		}
		o.iopaintURLIn.SyncExternal(gtx, s.IOPaintURL)

		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx,
			sectionTitle("Touch-up Brush Backend"),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutRadio(gtx, e.th, &o.radPatchMatch, backend == "patchmatch", "PatchMatch (built-in CPU inpainting)", "", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutRadio(gtx, e.th, &o.radIOPaint, backend == "iopaint", "IOPaint Server (external HTTP model)", "", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(4)}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(e.th, unit.Sp(12), "IOPaint Server URL")
						lbl.Color = colTextSubtle
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return o.iopaintURLIn.Layout(gtx, e.th, "http://127.0.0.1:8080", backend == "iopaint", &e.anyEditorFocused,
							func(v string) {
								next := e.settings
								next.IOPaintURL = strings.TrimSpace(v)
								e.saveSettings(next)
							}, nil)
					}),
				)
			}),
			mutedNote("When IOPaint fails or is unreachable, touch-up requests return an error rather than altering settings."),
		)

	case "fill":
		if o.radFillClamp.Clicked(gtx) {
			next := s
			next.WarpFillMode = "clamp"
			e.saveSettings(next)
		}
		if o.radFillSolid.Clicked(gtx) {
			next := s
			next.WarpFillMode = "fill"
			e.saveSettings(next)
		}
		if o.radFillOutpaint.Clicked(gtx) {
			next := s
			next.WarpFillMode = "outpaint"
			e.saveSettings(next)
		}
		if o.btnColorSwatch.Clicked(gtx) && e.host != nil {
			cur, err := parseHexColor(s.WarpFillColor)
			if err != nil {
				cur = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
			}
			go func() {
				chosen, ok, cErr := e.host.SelectColorDialog(cur)
				e.postUI(func() {
					if cErr != nil {
						e.showError(cErr)
						return
					}
					if ok {
						hex := fmt.Sprintf("#%02x%02x%02x", chosen.R, chosen.G, chosen.B)
						next := e.settings
						next.WarpFillColor = hex
						e.saveSettings(next)
					}
				})
			}()
		}

		fillMode := s.WarpFillMode
		if fillMode == "" {
			fillMode = "clamp"
		}
		o.fillColorIn.SyncExternal(gtx, s.WarpFillColor)
		curCol, colErr := parseHexColor(s.WarpFillColor)
		if colErr != nil {
			curCol = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		}

		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx,
			sectionTitle("Perspective Out-of-Bounds Fill"),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutRadio(gtx, e.th, &o.radFillClamp, fillMode == "clamp", "Edge clamp (repeat nearest border)", "", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutRadio(gtx, e.th, &o.radFillSolid, fillMode == "fill", "Solid color", "", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutRadio(gtx, e.th, &o.radFillOutpaint, fillMode == "outpaint", "PatchMatch outpainting", "", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(8)}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(28)
						cg := gtx
						if fillMode != "fill" {
							cg = cg.Disabled()
						}
						return o.btnColorSwatch.Layout(cg, func(gtx layout.Context) layout.Dimensions {
							if fillMode == "fill" && o.btnColorSwatch.Hovered() {
								pointer.CursorPointer.Add(gtx.Ops)
							}
							rect := image.Rect(0, 0, sz, sz)
							fillRRect(gtx.Ops, rect, gtx.Dp(4), curCol)
							strokeRRect(gtx.Ops, rect, gtx.Dp(4), float32(gtx.Dp(1)), colBorderLight)
							return layout.Dimensions{Size: image.Pt(sz, sz)}
						})
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return o.fillColorIn.Layout(gtx, e.th, "#ffffff", fillMode == "fill", &e.anyEditorFocused,
							func(v string) {
								v = strings.TrimSpace(v)
								if _, err := parseHexColor(v); err == nil {
									next := e.settings
									next.WarpFillColor = v
									e.saveSettings(next)
								}
							}, nil)
					}),
				)
			}),
			sectionTitle("Keyboard Edge Crop"),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(10)}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(e.th, unit.Sp(12), "Edge crop step (W/A/S/D, px)")
						lbl.Color = colTextSubtle
						return lbl.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						cg := gtx
						cg.Constraints.Min.X = gtx.Dp(76)
						cg.Constraints.Max.X = gtx.Dp(86)
						amt := s.CropEdgePixels
						if amt < 1 {
							amt = 3
						}
						return o.cropEdgeIn.LayoutInt(cg, e.th, amt, 1, 200, true, &e.anyEditorFocused,
							func(v int) {
								next := e.settings
								next.CropEdgePixels = v
								e.saveSettings(next)
							}, nil)
					}),
				)
			}),
		)

	case "modes":
		if o.chkDiscCutout.Clicked(gtx) {
			next := s
			next.DiscCenterCutout = !s.DiscCenterCutout
			e.saveSettings(next)
			if e.discActive && e.app != nil {
				e.runBackend(func() {
					res, err := e.app.SetDiscSettings(DiscSettings{
						CenterCutout:  next.DiscCenterCutout,
						CutoutPercent: next.DiscCutoutPercent,
					})
					e.postUI(func() {
						if err != nil {
							e.showError(err)
							return
						}
						e.syncDiscResponse(res)
						e.unsavedChanges = true
					})
				})
			}
		}
		if o.chkAutoDetectMode.Clicked(gtx) {
			next := s
			next.AutoDetectOnModeSwitch = !s.AutoDetectOnModeSwitch
			e.saveSettings(next)
		}
		if o.chkAutoCornerParam.Clicked(gtx) {
			next := s
			next.AutoCornerParams = !s.AutoCornerParams
			e.saveSettings(next)
		}

		items := []layout.FlexChild{
			sectionTitle("Disc Mode"),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCheckbox(gtx, e.th, &o.chkDiscCutout, s.DiscCenterCutout, "Center hole cutout", "", true)
			}),
		}
		if s.DiscCenterCutout {
			items = append(items, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return e.layoutLabeledSlider(gtx, "Default Cutout %", "opt_disc_cutout", "Center hole percentage of disc radius",
					func(gtx layout.Context) layout.Dimensions {
						return o.sldDiscCutoutPct.LayoutInt(gtx, e.th, s.DiscCutoutPercent, 0, 50, "%", unit.Dp(44), true,
							func(v int) { e.settings.DiscCutoutPercent = v },
							func(v int) { e.handleDiscCutoutCommit(v) },
						)
					})
			}))
		}
		items = append(items,
			sectionTitle("Corner Mode"),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCheckbox(gtx, e.th, &o.chkAutoDetectMode, s.AutoDetectOnModeSwitch, "Auto-detect corners on mode switch", "", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCheckbox(gtx, e.th, &o.chkAutoCornerParam, s.AutoCornerParams, "Auto-compute corner params per scan", "", true)
			}),
		)
		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx, items...)

	case "workflow":
		if o.chkTouchupSticky.Clicked(gtx) {
			next := s
			next.TouchupRemainsActive = !s.TouchupRemainsActive
			e.saveSettings(next)
		}
		if o.chkStraightEdgeSticky.Clicked(gtx) {
			next := s
			next.StraightEdgeRemainsActive = !s.StraightEdgeRemainsActive
			e.saveSettings(next)
		}
		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx,
			sectionTitle("Tool Persistence"),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCheckbox(gtx, e.th, &o.chkTouchupSticky, s.TouchupRemainsActive, "Keep Touch-up brush active after stroke", "", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCheckbox(gtx, e.th, &o.chkStraightEdgeSticky, s.StraightEdgeRemainsActive, "Keep Straight edge active after rotate", "", true)
			}),
		)

	case "save":
		if o.chkCloseAfterSave.Clicked(gtx) {
			next := s
			next.CloseAfterSave = !s.CloseAfterSave
			e.saveSettings(next)
		}
		if o.chkPostSaveEnabled.Clicked(gtx) {
			next := s
			next.PostSaveEnabled = !s.PostSaveEnabled
			e.saveSettings(next)
		}
		o.postSaveCmdIn.SyncExternal(gtx, s.PostSaveCommand)
		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx,
			sectionTitle("Save Behavior"),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCheckbox(gtx, e.th, &o.chkCloseAfterSave, s.CloseAfterSave, "Close window after saving", "", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCheckbox(gtx, e.th, &o.chkPostSaveEnabled, s.PostSaveEnabled, "Run command after saving", "", true)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return o.postSaveCmdIn.Layout(gtx, e.th, "Command template (use %s for output path)", s.PostSaveEnabled, &e.anyEditorFocused,
					func(v string) {
						next := e.settings
						next.PostSaveCommand = v
						e.saveSettings(next)
					}, nil)
			}),
			mutedNote("The saved output path is substituted for %s, or appended as an argument if %s is omitted."),
		)

	case "debug":
		if o.btnOpenCalib.Clicked(gtx) {
			e.optionsOpen = false
			e.calibState.open(e)
		}
		return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx,
			sectionTitle("Calibration & Diagnostics"),
			mutedNote("Annotate ground-truth corners on raw scans into an isolated JSON calibration dataset without affecting active editor state."),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				st := defaultButtonStyle(btnSecondary, "Open Corner Calibration")
				return layoutButton(gtx, e.th, &o.btnOpenCalib, st)
			}),
		)
	}
	return layout.Dimensions{}
}
