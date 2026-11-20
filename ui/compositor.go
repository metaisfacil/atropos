package ui

import (
	"fmt"
	"image"
	"math"
	"path/filepath"
	"strings"

	"atropos/internal/compositor"
	"atropos/internal/imageops"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type compositorModalState struct {
	owner         *EditorUI
	orientation   string
	paths         []string
	previewURL    string
	previewDims   image.Point
	statusMsg     string
	errMsg        string
	busy          bool
	rotationSteps int

	// Overlay mode state
	alignedLayers int
	activeLayer   int
	brushPct      float64
	featherPct    int
	maskStroke    []f32.Point
	maskDragging  bool
	panDrag       *panDragState

	viewport *viewportState

	ddOrientation uiDropdown
	ddLayer       uiDropdown
	fileList      widget.List
	fileListRowH  int
	upBtns        []widget.Clickable
	downBtns      []widget.Clickable
	delBtns       []widget.Clickable

	btnAddImages  widget.Clickable
	btnClearAll   widget.Clickable
	btnRotCCW     widget.Clickable
	btnRotCW      widget.Clickable
	btnFit        widget.Clickable
	btnUndoMask   widget.Clickable
	btnClearMasks widget.Clickable
	btnRun        widget.Clickable
	btnLoadOutput widget.Clickable
	btnClose      widget.Clickable

	sldBrush   uiSlider
	sldFeather uiSlider
}

func (c *compositorModalState) ensureInit() {
	if c.orientation == "" {
		c.orientation = "ltr"
	}
	if c.brushPct <= 0 {
		c.brushPct = 2.5
	}
	if c.featherPct == 0 && c.viewport == nil {
		c.featherPct = 35
	}
	if c.viewport == nil {
		c.viewport = newViewportState()
	}
	c.fileList.Axis = layout.Vertical
}

func (c *compositorModalState) open(e *EditorUI) {
	c.ensureInit()
	c.owner = e
	c.errMsg = ""
	e.compositorOpen = true
	if e.app != nil && c.previewURL != "" {
		e.runBackend(func() {
			res, err := e.app.CompositorRefreshPreview()
			e.postUI(func() {
				if err != nil {
					// Previous session was cleared; silently drop stale preview
					c.previewURL = ""
					c.previewDims = image.Point{}
					return
				}
				if res != nil {
					c.previewURL = res.Preview
					c.previewDims = image.Pt(res.Width, res.Height)
					c.statusMsg = res.Message
				}
			})
		})
	}
}

func (c *compositorModalState) effectivePreviewDims() image.Point {
	if c.orientation != "overlay" && c.rotationSteps%2 != 0 {
		return image.Pt(c.previewDims.Y, c.previewDims.X)
	}
	return c.previewDims
}

func (c *compositorModalState) effectivePreviewKey() string {
	if c.previewURL == "" {
		return ""
	}
	if c.orientation != "overlay" && c.rotationSteps%4 != 0 {
		return fmt.Sprintf("%s#rot=%d", c.previewURL, c.rotationSteps%4)
	}
	return c.previewURL
}

func (c *compositorModalState) resolveCompositorImage(e *EditorUI, src string) (*image.NRGBA, bool) {
	if c.previewURL == "" {
		return nil, false
	}
	img, ok := e.resolveMainPreview(c.previewURL)
	if !ok || img == nil {
		return nil, false
	}
	if c.orientation != "overlay" {
		steps := ((c.rotationSteps % 4) + 4) % 4
		for i := 0; i < steps; i++ {
			img = imageops.Rotate90(img, 1)
		}
	}
	return img, true
}

func (c *compositorModalState) clearResult() {
	c.previewURL = ""
	c.previewDims = image.Point{}
	c.alignedLayers = 0
	c.rotationSteps = 0
	c.maskStroke = nil
	c.maskDragging = false
	c.errMsg = ""
	if c.viewport != nil {
		c.viewport.ResetCache()
		c.viewport.Zoom = 1
		c.viewport.FitWidth = 0
		c.viewport.ScrollX = 0
		c.viewport.ScrollY = 0
	}
}

func (c *compositorModalState) moveItem(from, to int, e *EditorUI) {
	if from < 0 || from >= len(c.paths) || to < 0 || to >= len(c.paths) || from == to {
		return
	}
	c.paths[from], c.paths[to] = c.paths[to], c.paths[from]
	if c.previewURL != "" || c.alignedLayers > 0 {
		c.clearResult()
		if c.orientation == "overlay" {
			c.statusMsg = "Order updated — click \"Align scans\" to update"
		} else {
			c.statusMsg = "Order updated — click \"Stitch images\" to update"
		}
	} else {
		c.errMsg = ""
	}
	if e != nil {
		e.invalidate()
	}
}

func (c *compositorModalState) removeItem(idx int, e *EditorUI) {
	if idx < 0 || idx >= len(c.paths) {
		return
	}
	c.paths = append(c.paths[:idx], c.paths[idx+1:]...)
	c.clearResult()
	if len(c.paths) < 2 {
		c.activeLayer = 1
	} else if c.activeLayer >= len(c.paths) {
		c.activeLayer = len(c.paths) - 1
	} else if c.activeLayer < 1 {
		c.activeLayer = 1
	}
	if len(c.paths) == 0 {
		c.statusMsg = ""
	} else {
		c.statusMsg = fmt.Sprintf("%d image(s) queued", len(c.paths))
	}
	if e != nil {
		e.invalidate()
	}
}

func compositorDisplayName(paths []string, idx int) string {
	if idx < 0 || idx >= len(paths) {
		return ""
	}
	p := paths[idx]
	base := filepath.Base(p)
	dup := false
	for j, other := range paths {
		if j != idx && strings.EqualFold(filepath.Base(other), base) {
			dup = true
			break
		}
	}
	if !dup {
		return base
	}
	parent := filepath.Base(filepath.Dir(p))
	if parent == "" || parent == "." || parent == "/" || parent == `\` || parent == string(filepath.Separator) {
		return base
	}
	return parent + "/" + base
}

func measureLabelWidth(gtx layout.Context, th *material.Theme, size unit.Sp, s string) int {
	if th == nil || s == "" {
		return 0
	}
	var ops op.Ops
	mg := gtx
	mg.Ops = &ops
	mg.Constraints = layout.Constraints{Max: image.Pt(1e6, 1e6)}
	lbl := material.Label(th, size, s)
	lbl.MaxLines = 1
	return lbl.Layout(mg).Size.X
}

func middleTruncateLabel(gtx layout.Context, th *material.Theme, size unit.Sp, maxWidth int, prefix, name string) string {
	full := prefix + name
	if maxWidth <= 0 || name == "" || measureLabelWidth(gtx, th, size, full) <= maxWidth {
		return full
	}
	runes := []rune(name)
	if len(runes) <= 1 {
		return full
	}
	format := func(keep int) string {
		left := (keep * 4) / 10
		if keep >= 2 && left < 1 {
			left = 1
		}
		right := keep - left
		return prefix + string(runes[:left]) + "…" + string(runes[len(runes)-right:])
	}
	lo, hi := 1, len(runes)-1
	best := 0
	for lo <= hi {
		mid := (lo + hi) / 2
		if measureLabelWidth(gtx, th, size, format(mid)) <= maxWidth {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best == 0 {
		return prefix + "…" + string(runes[len(runes)-1:])
	}
	return format(best)
}

func (c *compositorModalState) addPaths(paths []string, e *EditorUI) {
	if c.busy || len(paths) == 0 {
		return
	}
	c.ensureInit()
	added := 0
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		c.paths = append(c.paths, trimmed)
		added++
	}
	if added == 0 {
		return
	}
	c.errMsg = ""
	c.statusMsg = fmt.Sprintf("%d image(s) queued", len(c.paths))
	if e != nil {
		e.invalidate()
	}
}

func (c *compositorModalState) handleAddImages(e *EditorUI) {
	if e.app == nil || c.busy {
		return
	}
	e.runBackend(func() {
		paths, err := e.app.CompositorOpenFilesDialog()
		e.postUI(func() {
			if err != nil {
				c.errMsg = err.Error()
				return
			}
			c.addPaths(paths, e)
		})
	})
}

func (c *compositorModalState) handleRun(e *EditorUI) {
	if e.app == nil || c.busy || len(c.paths) < 2 {
		return
	}
	c.busy = true
	c.errMsg = ""
	c.rotationSteps = 0
	c.alignedLayers = 0
	c.viewport.ResetCache()
	c.viewport.Zoom = 1
	c.viewport.FitWidth = 0
	c.viewport.ScrollX = 0
	c.viewport.ScrollY = 0
	req := CompositorStitchRequest{
		ImagePaths:  append([]string(nil), c.paths...),
		Orientation: c.orientation,
	}
	isOverlay := c.orientation == "overlay"
	if isOverlay {
		c.statusMsg = "Aligning scans…"
	} else {
		c.statusMsg = "Stitching images…"
	}
	e.runBackend(func() {
		var res *CompositorResult
		var err error
		if isOverlay {
			res, err = e.app.CompositorAlignOverlay(req)
		} else {
			res, err = e.app.CompositorStitch(req)
		}
		e.postUI(func() {
			c.busy = false
			if err != nil {
				c.errMsg = err.Error()
				return
			}
			if res != nil {
				c.previewURL = res.Preview
				c.previewDims = image.Pt(res.Width, res.Height)
				c.statusMsg = res.Message
				if isOverlay {
					c.alignedLayers = len(req.ImagePaths)
					if c.activeLayer < 1 || c.activeLayer >= c.alignedLayers {
						c.activeLayer = 1
					}
				}
			}
		})
	})
}

func (c *compositorModalState) commitMaskStroke(e *EditorUI) {
	if e.app == nil || len(c.maskStroke) == 0 || c.busy || c.alignedLayers < 2 {
		c.maskStroke = nil
		return
	}
	pts := make([]compositor.MaskPoint, len(c.maskStroke))
	for i, p := range c.maskStroke {
		pts[i] = compositor.MaskPoint{
			X: clampF64(float64(p.X), 0, 1),
			Y: clampF64(float64(p.Y), 0, 1),
		}
	}
	layer := clamp(c.activeLayer, 1, c.alignedLayers-1)
	stroke := compositor.MaskStroke{
		Layer:   layer,
		Radius:  c.brushPct / 100.0,
		Feather: float64(c.featherPct) / 100.0,
		Points:  pts,
	}
	c.maskStroke = nil
	c.busy = true
	e.runBackend(func() {
		res, err := e.app.CompositorAddMask(CompositorMaskRequest{Stroke: stroke})
		e.postUI(func() {
			c.busy = false
			if err != nil {
				c.errMsg = err.Error()
				return
			}
			if res != nil {
				c.previewURL = res.Preview
				c.previewDims = image.Pt(res.Width, res.Height)
				c.statusMsg = res.Message
			}
		})
	})
}

func (c *compositorModalState) handleLoadOutput(e *EditorUI) {
	if e.app == nil || c.busy || c.previewURL == "" {
		return
	}
	doLoad := func() {
		steps := 0
		if c.orientation != "overlay" {
			steps = ((c.rotationSteps % 4) + 4) % 4
		}
		gen := e.beginTransition()
		e.resetImageState(true)
		e.loading = true
		e.loadingFull = true
		e.loadingMessage = "Loading compositor output…"
		e.compositorOpen = false
		e.runBackend(func() {
			info, err := e.app.CompositorLoadResult(CompositorLoadResultRequest{RotationSteps: steps})
			e.postUI(func() {
				if e.transitionGen != gen {
					return
				}
				if err != nil {
					e.loading = false
					e.loadingFull = false
					e.loadingMessage = ""
					e.showError(err)
					return
				}
				e.applyLoadedImageInfo(info, gen)
			})
		})
	}
	if e.unsavedChanges {
		e.confirmDialog = &confirmDialogState{
			Title:       "Unsaved Changes",
			Message:     "Discard unsaved changes in the main editor and load compositor output?",
			YesLabel:    "Discard & Load",
			CancelLabel: "Cancel",
			YesVariant:  btnDanger,
			OnYes: func() {
				e.confirmDialog = nil
				doLoad()
			},
			OnCancel: func() { e.confirmDialog = nil },
		}
		return
	}
	doLoad()
}

func (c *compositorModalState) handleOverlayPointer(gtx layout.Context, info viewportLayoutInfo, pe pointer.Event) {
	dims := c.effectivePreviewDims()
	if dims.X <= 0 || dims.Y <= 0 {
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
		if pe.Buttons.Contain(pointer.ButtonPrimary) && c.alignedLayers >= 2 && !c.busy && inside {
			gtx.Execute(pointer.GrabCmd{Tag: &c.viewport.canvasTag, ID: pe.PointerID})
			nx := float32(clampF64(dispX/info.StageWidth, 0, 1))
			ny := float32(clampF64(dispY/info.StageHeight, 0, 1))
			c.maskDragging = true
			c.maskStroke = []f32.Point{f32.Pt(nx, ny)}
		}
	case pointer.Drag:
		if c.panDrag != nil {
			maxSX := math.Max(0, info.ContentWidth-float64(info.ViewportW))
			maxSY := math.Max(0, info.ContentHeight-float64(info.ViewportH))
			c.viewport.ScrollX = clampF64(c.panDrag.startLeft-float64(pe.Position.X-c.panDrag.startX), 0, maxSX)
			c.viewport.ScrollY = clampF64(c.panDrag.startTop-float64(pe.Position.Y-c.panDrag.startY), 0, maxSY)
			return
		}
		if c.maskDragging && info.StageWidth > 0 && info.StageHeight > 0 {
			nx := float32(clampF64(dispX/info.StageWidth, 0, 1))
			ny := float32(clampF64(dispY/info.StageHeight, 0, 1))
			last := c.maskStroke[len(c.maskStroke)-1]
			if math.Hypot(float64(nx-last.X), float64(ny-last.Y)) >= 0.002 {
				c.maskStroke = append(c.maskStroke, f32.Pt(nx, ny))
			}
		}
	case pointer.Release, pointer.Cancel:
		if c.panDrag != nil {
			c.panDrag = nil
			return
		}
		if c.maskDragging {
			c.maskDragging = false
			if pe.Kind == pointer.Cancel {
				c.maskStroke = nil
			} else if e := c.owner; e != nil {
				c.commitMaskStroke(e)
			}
		}
	}
}

func (c *compositorModalState) layout(gtx layout.Context, e *EditorUI) {
	c.ensureInit()
	c.owner = e

	isOverlay := c.orientation == "overlay"
	maxW := unit.Dp(620)
	maxH := unit.Dp(720)
	if isOverlay {
		maxW = unit.Dp(min(1100, int(float32(gtx.Constraints.Max.X)*0.96/float32(gtx.Metric.PxPerDp))))
		maxH = unit.Dp(int(float32(gtx.Constraints.Max.Y) * 0.94 / float32(gtx.Metric.PxPerDp)))
	}

	if c.btnClose.Clicked(gtx) {
		e.compositorOpen = false
		return
	}
	if c.btnAddImages.Clicked(gtx) {
		c.handleAddImages(e)
	}
	if c.btnClearAll.Clicked(gtx) && !c.busy {
		c.paths = nil
		c.clearResult()
		c.statusMsg = ""
	}
	if c.btnRotCCW.Clicked(gtx) && !c.busy && c.previewURL != "" {
		c.rotationSteps = (c.rotationSteps + 3) % 4
	}
	if c.btnRotCW.Clicked(gtx) && !c.busy && c.previewURL != "" {
		c.rotationSteps = (c.rotationSteps + 1) % 4
	}
	if c.btnFit.Clicked(gtx) {
		c.viewport.SetZoomKeepCenter(1)
	}
	if c.btnUndoMask.Clicked(gtx) && !c.busy && c.alignedLayers >= 2 {
		c.busy = true
		e.runBackend(func() {
			res, err := e.app.CompositorUndoMask()
			e.postUI(func() {
				c.busy = false
				if err != nil {
					c.errMsg = err.Error()
					return
				}
				if res != nil {
					c.previewURL = res.Preview
					c.previewDims = image.Pt(res.Width, res.Height)
					c.statusMsg = res.Message
				}
			})
		})
	}
	if c.btnClearMasks.Clicked(gtx) && !c.busy && c.alignedLayers >= 2 {
		c.busy = true
		e.runBackend(func() {
			res, err := e.app.CompositorClearMasks()
			e.postUI(func() {
				c.busy = false
				if err != nil {
					c.errMsg = err.Error()
					return
				}
				if res != nil {
					c.previewURL = res.Preview
					c.previewDims = image.Pt(res.Width, res.Height)
					c.statusMsg = res.Message
				}
			})
		})
	}
	if c.btnRun.Clicked(gtx) {
		c.handleRun(e)
	}
	if c.btnLoadOutput.Clicked(gtx) {
		c.handleLoadOutput(e)
		return
	}

	// Process per-item list button events before layout
	for len(c.upBtns) < len(c.paths) {
		c.upBtns = append(c.upBtns, widget.Clickable{})
		c.downBtns = append(c.downBtns, widget.Clickable{})
		c.delBtns = append(c.delBtns, widget.Clickable{})
	}
	for i := range c.paths {
		if i > 0 && !c.busy && c.upBtns[i].Clicked(gtx) {
			c.moveItem(i, i-1, e)
			break
		}
		if i+1 < len(c.paths) && !c.busy && c.downBtns[i].Clicked(gtx) {
			c.moveItem(i, i+1, e)
			break
		}
		if !c.busy && c.delBtns[i].Clicked(gtx) {
			c.removeItem(i, e)
			break
		}
	}

	oriOpts := []uiSelectOption{
		{Value: "ltr", Label: "Left to right"},
		{Value: "rtl", Label: "Right to left"},
		{Value: "ttb", Label: "Top to bottom"},
		{Value: "btt", Label: "Bottom to top"},
		{Value: "overlay", Label: "Overlay repeated scans"},
	}

	layoutCenteredModal(gtx, colBackdropDark, maxW, maxH, nil, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(18)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(12)}.Layout(gtx,
				// Header
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(10)}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(e.th, unit.Sp(15), "Image Compositor")
							lbl.Color = colWhite
							lbl.Font.Weight = font.Bold
							return lbl.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if !c.busy {
								return layout.Dimensions{}
							}
							return drawSpinner(gtx, unit.Dp(16), unit.Dp(2), colBorder, colPrimary)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							cg := gtx
							cg.Constraints.Min.X = gtx.Dp(200)
							cg.Constraints.Max.X = gtx.Dp(225)
							cg.Constraints.Min.Y = 0
							return c.ddOrientation.Layout(cg, e.th, e.overlays, oriOpts, c.orientation, !c.busy, func(v string) {
								if v != c.orientation {
									c.orientation = v
									c.clearResult()
									c.statusMsg = ""
								}
							})
						}),
					)
				}),

				// Main body: controls + file list + preview
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if isOverlay {
						return layout.Flex{Axis: layout.Horizontal, Gap: gtx.Dp(14)}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								cg := gtx
								cg.Constraints = layout.Exact(image.Pt(gtx.Dp(300), gtx.Constraints.Max.Y))
								return c.layoutFilesSection(cg, e)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return c.layoutOverlayPreviewPane(gtx, e)
							}),
						)
					}
					return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(10)}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							cg := gtx
							cg.Constraints.Min.Y = 0
							cg.Constraints.Max.Y = gtx.Dp(185)
							return c.layoutFilesSection(cg, e)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return c.layoutStitchPreviewPane(gtx, e)
						}),
					)
				}),

				// Status / error row
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					msg := c.errMsg
					col := colDanger
					if msg == "" {
						msg = c.statusMsg
						col = colTextSubtle
					}
					if msg == "" {
						return layout.Dimensions{}
					}
					lbl := material.Label(e.th, unit.Sp(12), msg)
					lbl.Color = col
					return lbl.Layout(gtx)
				}),

				// Footer
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					runLabel := "Stitch images"
					if isOverlay {
						runLabel = "Align scans"
					}
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(8)}.Layout(gtx,
						layout.Flexed(1, flexSpacer),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							st := defaultButtonStyle(btnPrimary, runLabel)
							st.Enabled = len(c.paths) >= 2 && !c.busy
							st.Loading = c.busy
							return layoutButton(gtx, e.th, &c.btnRun, st)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							st := defaultButtonStyle(btnLoad, "Load output")
							st.Enabled = c.previewURL != "" && !c.busy
							return layoutButton(gtx, e.th, &c.btnLoadOutput, st)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							st := defaultButtonStyle(btnSecondary, "Close")
							return layoutButton(gtx, e.th, &c.btnClose, st)
						}),
					)
				}),
			)
		})
	})
}

func (c *compositorModalState) layoutFilesSection(gtx layout.Context, e *EditorUI) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(6)}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(6)}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(12), fmt.Sprintf("Input scans (%d)", len(c.paths)))
					lbl.Color = colTextSubtle
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					st := defaultButtonStyle(btnSecondary, "Add images…")
					st.Enabled = !c.busy
					st.FontSize = unit.Sp(11.5)
					st.Pad = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}
					return layoutButton(gtx, e.th, &c.btnAddImages, st)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					st := defaultButtonStyle(btnSecondary, "Clear all")
					st.Enabled = len(c.paths) > 0 && !c.busy
					st.FontSize = unit.Sp(11.5)
					st.Pad = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}
					return layoutButton(gtx, e.th, &c.btnClearAll, st)
				}),
			)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			rect := image.Rectangle{Max: gtx.Constraints.Max}
			fillRRect(gtx.Ops, rect, gtx.Dp(4), colCanvasBg)
			strokeRRect(gtx.Ops, rect, gtx.Dp(4), float32(gtx.Dp(1)), colBorder)
			if len(c.paths) == 0 {
				layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(12), "Add at least 2 scans")
					lbl.Color = colTextMuted
					return lbl.Layout(gtx)
				})
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}
			layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				rowH := c.fileListRowH
				if rowH <= 0 {
					rowH = gtx.Dp(29)
				}
				scrollable := len(c.paths)*rowH > gtx.Constraints.Max.Y ||
					c.fileList.Position.First > 0 ||
					c.fileList.Position.Offset > 0
				ls := styledList(e.th, &c.fileList)
				if !scrollable {
					ls.AnchorStrategy = material.Overlay
				}
				listDims := ls.Layout(gtx, len(c.paths), func(gtx layout.Context, i int) layout.Dimensions {
					rowDims := layout.Inset{Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						macro := op.Record(gtx.Ops)
						dims := layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							smallBtn := func(gtx layout.Context, clk *widget.Clickable, sym string, enabled bool) layout.Dimensions {
								st := defaultButtonStyle(btnSecondary, sym)
								st.Enabled = enabled
								st.FontSize = unit.Sp(10.5)
								st.Pad = layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(5), Right: unit.Dp(5)}
								return layoutButton(gtx, e.th, clk, st)
							}
							prefix := fmt.Sprintf("%d. ", i+1)
							upHint := "Move earlier in the sequence."
							downHint := "Move later in the sequence."
							if c.orientation == "overlay" {
								if i == 0 {
									prefix = "Ref. "
								}
								upHint = "Move lower in the layer stack."
								downHint = "Move higher in the layer stack."
							}
							displayName := compositorDisplayName(c.paths, i)
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(3)}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									labelText := middleTruncateLabel(gtx, e.th, unit.Sp(12), gtx.Constraints.Max.X, prefix, displayName)
									return e.tooltips.Wrap(gtx, fmt.Sprintf("comp-file-%d", i), c.paths[i], func(gtx layout.Context) layout.Dimensions {
										lbl := material.Label(e.th, unit.Sp(12), labelText)
										lbl.Color = colText
										lbl.MaxLines = 1
										return lbl.Layout(gtx)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return e.tooltips.Wrap(gtx, fmt.Sprintf("comp-up-%d", i), upHint, func(gtx layout.Context) layout.Dimensions {
										return smallBtn(gtx, &c.upBtns[i], "▲", i > 0 && !c.busy)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return e.tooltips.Wrap(gtx, fmt.Sprintf("comp-down-%d", i), downHint, func(gtx layout.Context) layout.Dimensions {
										return smallBtn(gtx, &c.downBtns[i], "▼", i+1 < len(c.paths) && !c.busy)
									})
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return e.tooltips.Wrap(gtx, fmt.Sprintf("comp-del-%d", i), "Remove from sequence.", func(gtx layout.Context) layout.Dimensions {
										return smallBtn(gtx, &c.delBtns[i], "✕", !c.busy)
									})
								}),
							)
						})
						call := macro.Stop()
						if gtx.Constraints.Max.X > dims.Size.X {
							dims.Size.X = gtx.Constraints.Max.X
						}
						fillRRect(gtx.Ops, image.Rectangle{Max: dims.Size}, gtx.Dp(3), colSidebarBg)
						call.Add(gtx.Ops)
						return dims
					})
					if rowDims.Size.Y > 0 {
						c.fileListRowH = rowDims.Size.Y
					}
					return rowDims
				})
				actualScrollable := c.fileList.Position.Length > gtx.Constraints.Max.Y ||
					c.fileList.Position.First > 0 ||
					c.fileList.Position.Offset > 0
				if actualScrollable != scrollable {
					gtx.Execute(op.InvalidateCmd{})
				}
				return listDims
			})
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
	)
}

func layoutBorderedFrame(gtx layout.Context, w layout.Widget) layout.Dimensions {
	size := gtx.Constraints.Max
	layout.UniformInset(unit.Dp(1)).Layout(gtx, w)
	strokeRRect(gtx.Ops, image.Rectangle{Max: size}, gtx.Dp(4), float32(gtx.Dp(1)), colBorder)
	return layout.Dimensions{Size: size}
}

func (c *compositorModalState) layoutStitchPreviewPane(gtx layout.Context, e *EditorUI) layout.Dimensions {
	dims := c.effectivePreviewDims()
	rotLabel := ""
	switch ((c.rotationSteps % 4) + 4) % 4 {
	case 1:
		rotLabel = " (+90°)"
	case 2:
		rotLabel = " (+180°)"
	case 3:
		rotLabel = " (-90°)"
	}
	previewTitle := "Preview"
	if dims.X > 0 && dims.Y > 0 {
		previewTitle = fmt.Sprintf("Preview %d × %d px%s", dims.X, dims.Y, rotLabel)
	}
	return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(6)}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			rotBtn := func(gtx layout.Context, clk *widget.Clickable, txt string) layout.Dimensions {
				st := defaultButtonStyle(btnSecondary, txt)
				st.Enabled = c.previewURL != "" && !c.busy
				st.FontSize = unit.Sp(12)
				st.Pad = layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}
				return layoutButton(gtx, e.th, clk, st)
			}
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(6)}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(12), previewTitle)
					lbl.Color = colTextSubtle
					lbl.MaxLines = 1
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return rotBtn(gtx, &c.btnRotCCW, "↺ 90°") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return rotBtn(gtx, &c.btnRotCW, "↻ 90°") }),
			)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layoutBorderedFrame(gtx, func(gtx layout.Context) layout.Dimensions {
				return c.viewport.Layout(
					gtx,
					e.th,
					func(src string) (*image.NRGBA, bool) { return c.resolveCompositorImage(e, src) },
					c.effectivePreviewKey(),
					dims,
					c.previewURL == "",
					"Click \"Stitch images\" to preview result",
					nil,
					viewportVisualState{RealImageDims: dims},
					viewportCallbacks{
						DisableWheelZoom: false,
						Invalidate:       e.invalidate,
					},
				)
			})
		}),
	)
}

func (c *compositorModalState) layoutOverlayPreviewPane(gtx layout.Context, e *EditorUI) layout.Dimensions {
	dims := c.effectivePreviewDims()
	viewportDims := dims
	if c.previewURL != "" {
		if img, ok := e.resolveMainPreview(c.previewURL); ok && img != nil && !img.Bounds().Empty() {
			viewportDims = img.Bounds().Size()
		}
	}
	var layerOpts []uiSelectOption
	for i := 1; i < len(c.paths); i++ {
		layerOpts = append(layerOpts, uiSelectOption{
			Value: fmt.Sprintf("%d", i),
			Label: middleTruncateLabel(gtx, e.th, unit.Sp(12), gtx.Dp(135), fmt.Sprintf("Scan %d: ", i+1), compositorDisplayName(c.paths, i)),
		})
	}
	if len(layerOpts) == 0 {
		layerOpts = []uiSelectOption{{Value: "1", Label: "Scan 2"}}
	}

	return layout.Flex{Axis: layout.Vertical, Gap: gtx.Dp(8)}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle, Gap: gtx.Dp(6)}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(12), "Paint from")
					lbl.Color = colTextSubtle
					return lbl.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					cg := gtx
					cg.Constraints.Min.X = gtx.Dp(130)
					cg.Constraints.Max.X = gtx.Dp(165)
					cg.Constraints.Min.Y = 0
					return c.ddLayer.Layout(cg, e.th, e.overlays, layerOpts, fmt.Sprintf("%d", c.activeLayer), c.alignedLayers >= 2 && !c.busy, func(v string) {
						var idx int
						fmt.Sscanf(v, "%d", &idx)
						if idx >= 1 {
							c.activeLayer = idx
						}
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(11.5), "Brush")
					lbl.Color = colTextSubtle
					return lbl.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return c.sldBrush.LayoutFloat(gtx, e.th, c.brushPct, 0.5, 10.0, 0.5, fmt.Sprintf("%.1f%%", c.brushPct), unit.Dp(44), !c.busy,
						func(v float64) { c.brushPct = v }, nil)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(e.th, unit.Sp(11.5), "Feather")
					lbl.Color = colTextSubtle
					return lbl.Layout(gtx)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return c.sldFeather.LayoutInt(gtx, e.th, c.featherPct, 0, 100, "%", unit.Dp(40), !c.busy,
						func(v int) { c.featherPct = v }, nil)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					st := defaultButtonStyle(btnSecondary, "Undo")
					st.Enabled = c.alignedLayers >= 2 && !c.busy
					st.FontSize = unit.Sp(11.5)
					st.Pad = layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(8), Right: unit.Dp(8)}
					return layoutButton(gtx, e.th, &c.btnUndoMask, st)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					st := defaultButtonStyle(btnSecondary, "Clear")
					st.Enabled = c.alignedLayers >= 2 && !c.busy
					st.FontSize = unit.Sp(11.5)
					st.Pad = layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(8), Right: unit.Dp(8)}
					return layoutButton(gtx, e.th, &c.btnClearMasks, st)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					st := defaultButtonStyle(btnSecondary, "Fit")
					st.FontSize = unit.Sp(11.5)
					st.Pad = layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(8), Right: unit.Dp(8)}
					return layoutButton(gtx, e.th, &c.btnFit, st)
				}),
			)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			overlayVisual := func() viewportVisualState {
				return viewportVisualState{
					RealImageDims:   viewportDims,
					OverlayStroke:   c.maskStroke,
					OverlayBrushPct: c.brushPct,
				}
			}
			cur := pointer.CursorDefault
			if c.panDrag != nil {
				cur = pointer.CursorGrabbing
			} else if c.alignedLayers >= 2 {
				cur = pointer.CursorCrosshair
			}
			return layoutBorderedFrame(gtx, func(gtx layout.Context) layout.Dimensions {
				return c.viewport.Layout(
					gtx,
					e.th,
					func(src string) (*image.NRGBA, bool) { return e.resolveMainPreview(src) },
					c.previewURL,
					viewportDims,
					c.previewURL == "",
					"Align scans first, then paint mask strokes from Scan 2+ onto Scan 1",
					nil,
					overlayVisual(),
					viewportCallbacks{
						Cursor:     cur,
						GetVisual:  overlayVisual,
						OnPointer:  c.handleOverlayPointer,
						Invalidate: e.invalidate,
					},
				)
			})
		}),
	)
}
