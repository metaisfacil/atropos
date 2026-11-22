package ui

import (
	"fmt"
	"image"
	"math"
	"path/filepath"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type calibQueueItem struct {
	path    string
	status  string // "", "done", "skipped"
	width   int
	height  int
	corners []CalibrationPoint
}

type calibrationModalState struct {
	owner      *EditorUI
	queue      []calibQueueItem
	activeIdx  int
	previewURL string
	dims       image.Point
	corners    []CalibrationPoint
	dirty      bool
	busy       bool
	statusMsg  string

	panDrag *panDragState

	viewport  *viewportState
	queueList widget.List
	itemBtns  []widget.Clickable
	chipBtns  [4]widget.Clickable

	btnAddScans   widget.Clickable
	btnZoomFit    widget.Clickable
	btnZoom25     widget.Clickable
	btnZoom50     widget.Clickable
	btnZoom100    widget.Clickable
	btnZoom200    widget.Clickable
	btnUndoPoint  widget.Clickable
	btnClearPts   widget.Clickable
	btnSkip       widget.Clickable
	btnSaveNext   widget.Clickable
	btnExportJSON widget.Clickable
	btnClose      widget.Clickable
}

var calibCornerNames = []string{"TL", "TR", "BR", "BL"}

func (c *calibrationModalState) ensureInit() {
	if c.viewport == nil {
		c.viewport = newViewportState()
	}
	c.queueList.Axis = layout.Vertical
}

func (c *calibrationModalState) open(e *EditorUI) {
	c.ensureInit()
	c.owner = e
	e.calibrationOpen = true
	if len(c.queue) > 0 && c.previewURL == "" && c.activeIdx >= 0 && c.activeIdx < len(c.queue) {
		c.loadItem(e, c.activeIdx)
	}
}

func (c *calibrationModalState) undoCorner() {
	if len(c.corners) > 0 {
		c.corners = c.corners[:len(c.corners)-1]
		c.dirty = true
	}
}

func (c *calibrationModalState) saveAndNext() {
	e := c.owner
	if e == nil || c.activeIdx < 0 || c.activeIdx >= len(c.queue) || len(c.corners) != 4 {
		return
	}
	c.queue[c.activeIdx].status = "done"
	c.queue[c.activeIdx].width = c.dims.X
	c.queue[c.activeIdx].height = c.dims.Y
	c.queue[c.activeIdx].corners = append([]CalibrationPoint(nil), c.corners...)
	c.dirty = false
	c.advanceNextUnannotated(e)
}

func (c *calibrationModalState) advanceNextUnannotated(e *EditorUI) {
	if len(c.queue) == 0 {
		return
	}
	for off := 1; off <= len(c.queue); off++ {
		idx := (c.activeIdx + off) % len(c.queue)
		if c.queue[idx].status == "" {
			c.loadItem(e, idx)
			return
		}
	}
	if c.activeIdx+1 < len(c.queue) {
		c.loadItem(e, c.activeIdx+1)
	}
}

func (c *calibrationModalState) requestClose() {
	e := c.owner
	if e == nil {
		return
	}
	if c.dirty || (len(c.corners) > 0 && len(c.corners) < 4) {
		e.confirmDialog = &confirmDialogState{
			Title:       "Discard Calibration Draft?",
			Message:     "Current scan has unsaved corner points. Close without saving this scan?",
			YesLabel:    "Discard",
			CancelLabel: "Cancel",
			YesVariant:  btnDanger,
			OnYes: func() {
				e.confirmDialog = nil
				c.closeNow(e)
			},
			OnCancel: func() { e.confirmDialog = nil },
		}
		return
	}
	c.closeNow(e)
}

func (c *calibrationModalState) closeNow(e *EditorUI) {
	e.calibrationOpen = false
	c.previewURL = ""
	c.viewport.ResetCache()
	if e.app != nil {
		e.runBackend(func() {
			e.app.CalibrationClearPreview()
		})
	}
}

func (c *calibrationModalState) loadItem(e *EditorUI, idx int) {
	if e.app == nil || idx < 0 || idx >= len(c.queue) {
		return
	}
	c.activeIdx = idx
	c.busy = true
	c.dirty = false
	item := c.queue[idx]
	c.statusMsg = fmt.Sprintf("Loading %s…", filepath.Base(item.path))
	e.runBackend(func() {
		info, err := e.app.CalibrationLoadImage(CalibrationLoadRequest{FilePath: item.path})
		e.postUI(func() {
			c.busy = false
			if err != nil {
				e.showError(err)
				c.statusMsg = err.Error()
				return
			}
			if info != nil {
				c.viewport.ResetCache()
				c.viewport.Zoom = 1
				c.viewport.FitWidth = 0
				c.viewport.ScrollX = 0
				c.viewport.ScrollY = 0
				c.previewURL = info.Preview
				c.dims = image.Pt(info.Width, info.Height)
				c.queue[idx].width = info.Width
				c.queue[idx].height = info.Height
				c.corners = append([]CalibrationPoint(nil), c.queue[idx].corners...)
				c.statusMsg = filepath.Base(info.ImagePath)
			}
		})
	})
}

func (c *calibrationModalState) addPaths(paths []string, e *EditorUI) {
	if len(paths) == 0 || c.busy {
		return
	}
	hadEmpty := len(c.queue) == 0
	startIdx := len(c.queue)
	for _, p := range paths {
		if p != "" {
			c.queue = append(c.queue, calibQueueItem{path: p})
		}
	}
	if hadEmpty && len(c.queue) > 0 {
		c.loadItem(e, 0)
	} else if c.previewURL == "" && startIdx < len(c.queue) {
		c.loadItem(e, startIdx)
	}
}

func (c *calibrationModalState) handleAddScans(e *EditorUI) {
	if e.app == nil || c.busy {
		return
	}
	e.runBackend(func() {
		paths, err := e.app.CalibrationOpenFilesDialog()
		e.postUI(func() {
			if err != nil {
				e.showError(err)
				return
			}
			c.addPaths(paths, e)
		})
	})
}

func (c *calibrationModalState) handleExportJSON(e *EditorUI) {
	if e.app == nil || c.busy {
		return
	}
	var entries []CalibrationEntryRequest
	for _, it := range c.queue {
		if it.status == "done" && len(it.corners) == 4 {
			entries = append(entries, CalibrationEntryRequest{
				ImagePath: it.path,
				Width:     it.width,
				Height:    it.height,
				Corners:   append([]CalibrationPoint(nil), it.corners...),
			})
		}
	}
	if len(entries) == 0 {
		return
	}
	c.busy = true
	e.runBackend(func() {
		res, err := e.app.CalibrationSaveDataset(CalibrationSaveRequest{Entries: entries})
		e.postUI(func() {
			c.busy = false
			if err != nil {
				e.showError(err)
				return
			}
			if res != nil && !res.Cancelled {
				c.statusMsg = fmt.Sprintf("Exported %d entries to %s", res.Count, filepath.Base(res.OutputPath))
			}
		})
	})
}

func (c *calibrationModalState) setImagePixelZoom(scale float64) {
	if c.dims.X <= 0 || c.viewport == nil || c.viewport.FitWidth <= 0 {
		return
	}
	z := (scale * float64(c.dims.X)) / c.viewport.FitWidth
	maxZ := maxUsefulZoom(c.dims.X, c.viewport.FitWidth)
	c.viewport.SetZoomKeepCenter(clampF64(z, 0.1, maxZ))
}

func (c *calibrationModalState) handlePointer(gtx layout.Context, info viewportLayoutInfo, pe pointer.Event) {
	if c.dims.X <= 0 || c.dims.Y <= 0 {
		return
	}
	if c.viewport.hScrollDrag != nil || c.viewport.vScrollDrag != nil {
		return
	}
	dispX := float64(pe.Position.X) - info.StageX
	dispY := float64(pe.Position.Y) - info.StageY
	inside := info.StageWidth > 0 && info.StageHeight > 0 &&
		dispX >= 0 && dispX <= info.StageWidth && dispY >= 0 && dispY <= info.StageHeight

	switch pe.Kind {
	case pointer.Press:
		if pe.Buttons.Contain(pointer.ButtonTertiary) {
			gtx.Execute(pointer.GrabCmd{Tag: &c.viewport.canvasTag, ID: pe.PointerID})
			c.panDrag = &panDragState{
				startX:    pe.Position.X,
				startY:    pe.Position.Y,
				startLeft: c.viewport.ScrollX,
				startTop:  c.viewport.ScrollY,
			}
			return
		}
		if pe.Buttons.Contain(pointer.ButtonPrimary) && inside && len(c.corners) < 4 {
			pt := displayToImage(dispX, dispY, c.dims, info.StageWidth, info.StageHeight)
			pt.X = clamp(pt.X, 0, c.dims.X-1)
			pt.Y = clamp(pt.Y, 0, c.dims.Y-1)
			c.corners = append(c.corners, CalibrationPoint{X: pt.X, Y: pt.Y})
			c.dirty = true
		}
	case pointer.Drag:
		if c.panDrag != nil {
			maxSX := math.Max(0, info.ContentWidth-float64(info.ViewportW))
			maxSY := math.Max(0, info.ContentHeight-float64(info.ViewportH))
			c.viewport.ScrollX = clampF64(c.panDrag.startLeft-float64(pe.Position.X-c.panDrag.startX), 0, maxSX)
			c.viewport.ScrollY = clampF64(c.panDrag.startTop-float64(pe.Position.Y-c.panDrag.startY), 0, maxSY)
		}
	case pointer.Release, pointer.Cancel:
		c.panDrag = nil
	}
}

func (c *calibrationModalState) layout(gtx layout.Context, e *EditorUI) {
	c.ensureInit()
	c.owner = e

	if c.btnClose.Clicked(gtx) {
		c.requestClose()
		return
	}
	if c.btnAddScans.Clicked(gtx) {
		c.handleAddScans(e)
	}
	if c.btnZoomFit.Clicked(gtx) {
		c.viewport.SetZoomKeepCenter(1)
	}
	if c.btnZoom25.Clicked(gtx) {
		c.setImagePixelZoom(0.25)
	}
	if c.btnZoom50.Clicked(gtx) {
		c.setImagePixelZoom(0.50)
	}
	if c.btnZoom100.Clicked(gtx) {
		c.setImagePixelZoom(1.0)
	}
	if c.btnZoom200.Clicked(gtx) {
		c.setImagePixelZoom(2.0)
	}
	if c.btnUndoPoint.Clicked(gtx) {
		c.undoCorner()
	}
	if c.btnClearPts.Clicked(gtx) {
		c.corners = nil
		c.dirty = len(c.queue) > 0 && c.activeIdx >= 0 && c.activeIdx < len(c.queue) && c.queue[c.activeIdx].status == "done"
	}
	if c.btnSkip.Clicked(gtx) && c.activeIdx >= 0 && c.activeIdx < len(c.queue) {
		c.queue[c.activeIdx].status = "skipped"
		c.corners = nil
		c.dirty = false
		c.advanceNextUnannotated(e)
	}
	if c.btnSaveNext.Clicked(gtx) && len(c.corners) == 4 {
		c.saveAndNext()
	}

	doneCount := 0
	for _, it := range c.queue {
		if it.status == "done" && len(it.corners) == 4 {
			doneCount++
		}
	}
	if c.btnExportJSON.Clicked(gtx) && doneCount > 0 {
		c.handleExportJSON(e)
	}

	for i := range c.corners {
		if i < 4 && c.chipBtns[i].Clicked(gtx) {
			c.corners = c.corners[:i]
			c.dirty = true
			break
		}
	}

	if len(c.itemBtns) < len(c.queue) {
		c.itemBtns = make([]widget.Clickable, len(c.queue))
	}
	for i := range c.queue {
		if c.itemBtns[i].Clicked(gtx) && i != c.activeIdx && !c.busy {
			c.loadItem(e, i)
			break
		}
	}

	maxW := unit.Dp(min(1480, int(float32(gtx.Constraints.Max.X)*0.96/float32(gtx.Metric.PxPerDp))))
	maxH := unit.Dp(min(940, int(float32(gtx.Constraints.Max.Y)*0.94/float32(gtx.Metric.PxPerDp))))

	layoutCenteredModal(gtx, colBackdropDark, maxW, maxH, nil, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Header bar
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(14), Bottom: unit.Dp(12), Left: unit.Dp(18), Right: unit.Dp(18)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(10)}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(e.th, unit.Sp(15), "Corner Calibration")
							lbl.Color = colWhite
							lbl.Font.Weight = font.Bold
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(e.th, unit.Sp(12), c.statusMsg)
							lbl.Color = colTextSubtle
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							st := defaultButtonStyle(btnSecondary, "Close")
							st.Pad = layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(12), Right: unit.Dp(12)}
							return layoutButton(gtx, e.th, &c.btnClose, st)
						}),
					)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				w := gtx.Constraints.Max.X
				paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, 0, w, 1)).Op())
				return layout.Dimensions{Size: image.Pt(w, 1)}
			}),
			// Main split body
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					// Left queue sidebar (230dp)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						qw := gtx.Dp(230)
						cg := gtx
						cg.Constraints = layout.Exact(image.Pt(qw, gtx.Constraints.Max.Y))
						paint.FillShape(gtx.Ops, colSidebarBg, clip.Rect(image.Rect(0, 0, qw, gtx.Constraints.Max.Y)).Op())
						paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(qw-1, 0, qw, gtx.Constraints.Max.Y)).Op())

						layout.UniformInset(unit.Dp(10)).Layout(cg, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(8)}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									st := defaultButtonStyle(btnPrimary, "Add scans…")
									st.Enabled = !c.busy
									return layoutButton(gtx, e.th, &c.btnAddScans, st)
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									if len(c.queue) == 0 {
										return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Label(e.th, unit.Sp(12), "No scans added")
											lbl.Color = colTextMuted
											return lbl.Layout(gtx)
										})
									}
									return styledList(e.th, &c.queueList).Layout(gtx, len(c.queue), func(gtx layout.Context, i int) layout.Dimensions {
										it := c.queue[i]
										mark := "·"
										if it.status == "done" {
											mark = "✓"
										} else if it.status == "skipped" {
											mark = "–"
										} else if i == c.activeIdx && len(c.corners) > 0 {
											mark = fmt.Sprintf("%d", len(c.corners))
										}
										st := defaultButtonStyle(btnCalibrationFile, fmt.Sprintf("%s  %s", mark, filepath.Base(it.path)))
										st.Active = i == c.activeIdx
										return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layoutButton(gtx, e.th, &c.itemBtns[i], st)
										})
									})
								}),
							)
						})
						return layout.Dimensions{Size: image.Pt(qw, gtx.Constraints.Max.Y)}
					}),

					// Right annotation workspace
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// Toolbar
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									zbtn := func(gtx layout.Context, clk *widget.Clickable, label string) layout.Dimensions {
										st := defaultButtonStyle(btnSecondary, label)
										st.FontSize = unit.Sp(11.5)
										st.Enabled = c.previewURL != ""
										st.Pad = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}
										return layoutButton(gtx, e.th, clk, st)
									}
									prompt := "Add scans to begin annotating"
									if c.previewURL != "" {
										if len(c.corners) < 4 {
											prompt = fmt.Sprintf("Click %s (%d/4)", calibCornerNames[len(c.corners)], len(c.corners)+1)
										} else {
											prompt = "All 4 corners marked — Save & next (Enter)"
										}
									}
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(6)}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions { return zbtn(gtx, &c.btnZoomFit, "Fit") }),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions { return zbtn(gtx, &c.btnZoom25, "25%") }),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions { return zbtn(gtx, &c.btnZoom50, "50%") }),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions { return zbtn(gtx, &c.btnZoom100, "100%") }),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions { return zbtn(gtx, &c.btnZoom200, "200%") }),
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Label(e.th, unit.Sp(12), "  "+prompt)
											lbl.Color = colAmber
											return lbl.Layout(gtx)
										}),
									)
								})
							}),
							// Calibration image viewport (strictly isolated via ResolveCalibrationImage)
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								selPts := make([]image.Point, len(c.corners))
								for i, pt := range c.corners {
									selPts[i] = image.Pt(pt.X, pt.Y)
								}
								cur := pointer.CursorDefault
								if c.panDrag != nil {
									cur = pointer.CursorGrabbing
								} else if c.previewURL != "" && len(c.corners) < 4 {
									cur = pointer.CursorCrosshair
								}
								return c.viewport.Layout(
									gtx,
									e.th,
									func(src string) (*image.NRGBA, bool) {
										if e.app == nil || src == "" {
											return nil, false
										}
										return e.app.ResolveCalibrationImage(src)
									},
									c.previewURL,
									c.dims,
									c.previewURL == "",
									"Select or add a scan to mark ground-truth corners",
									nil,
									viewportVisualState{
										RealImageDims:      c.dims,
										SelectedCornerPts:  selPts,
										DotRadius:          6,
										ShowCornerSequence: true,
										CornerLabels:       calibCornerNames,
									},
									viewportCallbacks{
										Cursor:     cur,
										OnPointer:  c.handlePointer,
										Invalidate: e.invalidate,
									},
								)
							}),
							// Coordinate chips + Action footer
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								w := gtx.Constraints.Max.X
								paint.FillShape(gtx.Ops, colBorder, clip.Rect(image.Rect(0, 0, w, 1)).Op())
								return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									var chips []layout.FlexChild
									for i := 0; i < 4; i++ {
										i := i
										textStr := calibCornerNames[i] + ": —"
										placed := i < len(c.corners)
										if placed {
											textStr = fmt.Sprintf("%s: %d,%d", calibCornerNames[i], c.corners[i].X, c.corners[i].Y)
										}
										chips = append(chips, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											st := defaultButtonStyle(btnSecondary, textStr)
											st.Enabled = placed
											st.FontSize = unit.Sp(11)
											st.Pad = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}
											return layoutButton(gtx, e.th, &c.chipBtns[i], st)
										}))
									}
									chips = append(chips, layout.Flexed(1, flexSpacer))
									chips = append(chips,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											st := defaultButtonStyle(btnSecondary, "Undo point")
											st.Enabled = len(c.corners) > 0 && !c.busy
											return layoutButton(gtx, e.th, &c.btnUndoPoint, st)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											st := defaultButtonStyle(btnSecondary, "Clear points")
											st.Enabled = len(c.corners) > 0 && !c.busy
											return layoutButton(gtx, e.th, &c.btnClearPts, st)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											st := defaultButtonStyle(btnSecondary, "Skip")
											st.Enabled = len(c.queue) > 0 && !c.busy
											return layoutButton(gtx, e.th, &c.btnSkip, st)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											st := defaultButtonStyle(btnPrimary, "Save & next")
											st.Enabled = len(c.corners) == 4 && !c.busy
											return layoutButton(gtx, e.th, &c.btnSaveNext, st)
										}),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											st := defaultButtonStyle(btnLoad, fmt.Sprintf("Export JSON (%d)", doneCount))
											st.Enabled = doneCount > 0 && !c.busy
											return layoutButton(gtx, e.th, &c.btnExportJSON, st)
										}),
									)
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(6)}.Layout(gtx, chips...)
								})
							}),
						)
					}),
				)
			}),
		)
	})
	_ = op.Ops{}
}
