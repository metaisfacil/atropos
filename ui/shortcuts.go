package ui

import (
	"fmt"
	"image"
	"math"
	"strings"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
)

type pointerEventAlias = pointer.Event

func optimisticCropDimensions(dims image.Point, direction string, amount int) (image.Point, bool) {
	if dims.X <= 0 || dims.Y <= 0 {
		return image.Point{}, false
	}
	crop := max(1, amount)
	out := dims
	switch direction {
	case "left", "right":
		out.X = max(1, dims.X-crop)
	case "top", "bottom":
		out.Y = max(1, dims.Y-crop)
	default:
		return image.Point{}, false
	}
	return out, true
}

func (e *EditorUI) processGlobalShortcuts(gtx layout.Context) {
	modalOpen := e.hasOpenModal()

	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameSpace, Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt},
			key.Filter{Name: key.NameReturn, Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt},
			key.Filter{Name: key.NameEnter, Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt},
			key.Filter{Name: key.NameEscape, Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt},
			key.Filter{Name: key.NameUpArrow, Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt},
			key.Filter{Name: key.NameDownArrow, Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt},
			key.Filter{Name: key.NameLeftArrow, Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt},
			key.Filter{Name: key.NameRightArrow, Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt},
			key.Filter{Name: "", Optional: key.ModCtrl | key.ModCommand | key.ModShift | key.ModAlt},
		)
		if !ok {
			break
		}
		ke, ok := ev.(key.Event)
		if !ok {
			continue
		}
		gtx.Execute(op.InvalidateCmd{})
		if ke.Name == key.NameSpace {
			if ke.State == key.Press {
				if !modalOpen && !e.anyEditorFocused && !e.spaceDown {
					e.spaceDown = true
					e.spacePanMode = true
				}
			} else if ke.State == key.Release {
				e.spaceDown = false
				e.spacePanMode = false
				e.panDrag = nil
			}
			continue
		}
		if ke.State != key.Press {
			continue
		}
		if modalOpen {
			e.handleModalShortcut(ke)
			continue
		}
		e.handleEditorKeyDown(ke)
	}
}

func (e *EditorUI) handleModalShortcut(ke key.Event) {
	if e.errorMessage != "" {
		if ke.Name == key.NameEscape || ke.Name == key.NameReturn || ke.Name == key.NameEnter {
			e.errorMessage = ""
		}
		return
	}
	if e.confirmDialog != nil {
		if ke.Name == key.NameEscape && e.confirmDialog.OnCancel != nil {
			e.confirmDialog.OnCancel()
		} else if (ke.Name == key.NameReturn || ke.Name == key.NameEnter) && e.confirmDialog.OnYes != nil {
			e.confirmDialog.OnYes()
		}
		return
	}
	if e.calibrationOpen {
		if ke.Name == key.NameEscape {
			e.calibState.requestClose()
		} else if ke.Modifiers.Contain(key.ModShortcut) && strings.EqualFold(string(ke.Name), "Z") {
			e.calibState.undoCorner()
		} else if (ke.Name == key.NameReturn || ke.Name == key.NameEnter) && len(e.calibState.corners) == 4 {
			e.calibState.saveAndNext()
		}
		return
	}
	if e.resizeOpen {
		if ke.Name == key.NameEscape {
			e.resizeOpen = false
		} else if (ke.Name == key.NameReturn || ke.Name == key.NameEnter) && e.resizeState.width > 0 && e.resizeState.height > 0 {
			e.resizeState.tryApply(e)
		}
		return
	}
	if e.compositorOpen {
		if ke.Name == key.NameEscape {
			e.compositorOpen = false
		}
		return
	}
	if e.optionsOpen {
		if ke.Name == key.NameEscape {
			e.optionsOpen = false
		}
		return
	}
	if e.aboutOpen {
		if ke.Name == key.NameEscape || ke.Name == key.NameReturn || ke.Name == key.NameEnter {
			e.aboutOpen = false
		}
		return
	}
}

func (e *EditorUI) handleEditorKeyDown(ke key.Event) {
	name := string(ke.Name)
	lower := strings.ToLower(name)
	shortcutMod := ke.Modifiers.Contain(key.ModShortcut)

	if shortcutMod && lower == "w" {
		e.handleCloseRequest()
		return
	}
	if shortcutMod && lower == "o" {
		e.handleLoadImage()
		return
	}
	if shortcutMod && lower == "v" {
		if e.anyEditorFocused {
			return
		}
		e.handlePasteImage()
		return
	}
	if !e.imageLoaded {
		return
	}

	if shortcutMod && lower == "a" && e.adjustmentSelectionActive {
		if e.anyEditorFocused {
			return
		}
		e.adjustmentRect = &rectI{X1: 0, Y1: 0, X2: e.realImageDims.X, Y2: e.realImageDims.Y}
		e.showStatus("Whole image selected")
		return
	}
	if shortcutMod && lower == "d" && (e.adjustmentSelectionActive || e.adjustmentRect != nil) {
		e.adjustmentRect = nil
		e.showStatus("Adjustment selection cleared")
		return
	}

	copyRect := e.adjustmentRect
	if copyRect == nil && e.mode == "normal" {
		copyRect = e.normalRect
	}
	if shortcutMod && lower == "c" {
		if e.anyEditorFocused {
			return
		}
		rect := rectI{X1: 0, Y1: 0, X2: e.realImageDims.X, Y2: e.realImageDims.Y}
		hasSub := copyRect != nil
		if hasSub {
			rect = *copyRect
			e.showStatus("Copying selection…")
		} else {
			e.showStatus("Copying image…")
		}
		sel := AdjustmentSelection{X1: float64(rect.X1), Y1: float64(rect.Y1), X2: float64(rect.X2), Y2: float64(rect.Y2)}
		e.runBackend(func() {
			msg, err := e.app.CopySelectionToClipboard(sel)
			e.postUI(func() {
				if err != nil {
					e.showError(err)
					return
				}
				if msg != "" {
					e.showStatus(msg)
				} else if hasSub {
					e.showStatus("Selection copied to clipboard")
				} else {
					e.showStatus("Image copied to clipboard")
				}
			})
		})
		return
	}

	if shortcutMod && (lower == "z" || lower == "y") {
		if e.anyEditorFocused {
			return
		}
		if e.ctrlDrag != nil || e.shiftDrag != nil || e.optimisticCrop != nil {
			return
		}
		if lower == "y" {
			e.handleHistory(true)
			return
		}
		if e.mode == "corner" && e.cornerCount > 0 && e.cornerCount < 4 {
			newCount := e.cornerCount - 1
			e.runBackend(func() {
				_ = e.app.UndoLastCorner()
				e.postUI(func() {
					e.cornerCount = newCount
					if len(e.selectedCornerPts) > 0 {
						e.selectedCornerPts = e.selectedCornerPts[:len(e.selectedCornerPts)-1]
					}
					if newCount == 0 {
						e.showStatus("Corner selection cleared — click to select corners")
					} else {
						e.showStatus(fmt.Sprintf("Corner %d of 4 selected", newCount))
					}
				})
			})
			return
		}
		e.handleHistory(false)
		return
	}

	if e.anyEditorFocused {
		return
	}

	if e.mode == "disc" && e.discActive && e.discRadius > 0 {
		step := 5
		if ke.Modifiers.Contain(key.ModShift) {
			step = 20
		}
		applyShift := func(visDx, visDy int) {
			rad := e.discRotation * math.Pi / 180
			cos := math.Cos(rad)
			sin := math.Sin(rad)
			dx := int(math.Round(cos*float64(visDx) + sin*float64(visDy)))
			dy := int(math.Round(-sin*float64(visDx) + cos*float64(visDy)))
			e.runBackend(func() {
				res, err := e.app.ShiftDisc(ShiftDiscRequest{DX: dx, DY: dy, Nudge: true})
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
		switch ke.Name {
		case key.NameUpArrow:
			applyShift(0, -step)
			return
		case key.NameDownArrow:
			applyShift(0, step)
			return
		case key.NameLeftArrow:
			applyShift(-step, 0)
			return
		case key.NameRightArrow:
			applyShift(step, 0)
			return
		}
		if name == "+" || name == "=" {
			next := clamp(e.featherSize+1, 0, 100)
			e.setFeatherSizeAndApply(next)
			return
		}
		if name == "-" {
			next := clamp(e.featherSize-1, 0, 100)
			e.setFeatherSizeAndApply(next)
			return
		}
		if lower == "y" && ke.Modifiers == 0 {
			info := e.mainViewport.LastLayout
			imgPt := displayToImage(float64(e.mousePos.X), float64(e.mousePos.Y), e.realImageDims, info.StageWidth, info.StageHeight)
			e.runBackend(func() {
				res, err := e.app.GetPixelColor(PixelColorRequest{X: imgPt.X, Y: imgPt.Y})
				e.postUI(func() {
					if err != nil {
						e.showError(err)
						return
					}
					e.syncDiscResponse(res)
					e.unsavedChanges = true
				})
			})
			return
		}
	}

	if shortcutMod && lower == "s" {
		if !e.canSave() {
			return
		}
		e.handleSaveImage(nil)
		return
	}

	if (ke.Name == key.NameReturn || ke.Name == key.NameEnter) && e.mode == "normal" && e.normalRect != nil {
		e.handleNormalCrop()
		return
	}

	if ke.Modifiers&(key.ModCtrl|key.ModCommand|key.ModAlt) == 0 {
		spatial := e.resolveSpatialKey(name)
		if spatial != "" {
			if !e.canSave() {
				e.showStatus("Apply a crop first before adjusting")
				return
			}
			switch spatial {
			case "w", "a", "s", "d":
				dir := map[string]string{"w": "top", "s": "bottom", "a": "left", "d": "right"}[spatial]
				e.applyOptimisticKeyboardCrop(dir)
			case "q":
				e.applyKeyboardRotate(true)
			case "e":
				e.applyKeyboardRotate(false)
			}
		}
	}
}

func (e *EditorUI) resolveSpatialKey(name string) string {
	upper := strings.ToUpper(name)
	if v, ok := e.spatialKeyMap[upper]; ok {
		return v
	}
	switch upper {
	case "W", "A", "S", "D", "Q", "E":
		return strings.ToLower(upper)
	}
	return ""
}

func (e *EditorUI) setFeatherSizeAndApply(size int) {
	e.featherSize = clamp(size, 0, 100)
	e.runBackend(func() {
		res, err := e.app.SetFeatherSize(FeatherSizeRequest{Size: e.featherSize})
		e.postUI(func() {
			if err != nil {
				e.showError(err)
				return
			}
			if res.Preview != "" {
				e.setPreview(res.Preview)
			}
			if res.Width > 0 && res.Height > 0 {
				e.realImageDims = image.Pt(res.Width, res.Height)
			}
			e.unsavedChanges = true
		})
	})
}

func (e *EditorUI) applyOptimisticKeyboardCrop(direction string) {
	if e.optimisticCrop != nil || e.preview == "" {
		return
	}
	amt := e.settings.CropEdgePixels
	if amt < 1 {
		amt = 3
	}
	target, ok := optimisticCropDimensions(e.realImageDims, direction, amt)
	if !ok {
		return
	}
	srcDims := e.realImageDims
	pending := &optimisticCropState{
		Source:     e.preview,
		SourceDims: srcDims,
		TargetDims: target,
		Direction:  direction,
		Amount:     amt,
	}
	e.optimisticCrop = pending
	e.realImageDims = target
	e.runBackend(func() {
		res, err := e.app.Crop(CropRequest{Direction: direction, Amount: amt})
		e.postUI(func() {
			if err != nil {
				e.realImageDims = srcDims
				e.optimisticCrop = nil
				e.showError(err)
				return
			}
			if res.Preview != "" {
				e.setPreview(res.Preview)
			}
			if res.Width > 0 && res.Height > 0 {
				e.realImageDims = image.Pt(res.Width, res.Height)
			}
			if res.DescreenReset {
				e.useDescreenTool = false
			}
			e.unsavedChanges = true
			e.flushPendingSave()
		})
	})
}

func (e *EditorUI) applyKeyboardRotate(ccw bool) {
	e.loading = true
	e.showStatus("Rotating…")
	isDisc := e.mode == "disc" && e.discActive && e.discRadius > 0
	e.runBackend(func() {
		var res *ProcessResult
		var err error
		if isDisc {
			ang := 15.0
			if ccw {
				ang = -15.0
			}
			res, err = e.app.RotateDisc(DiscRotateRequest{Angle: ang, Nudge: true})
		} else {
			flip := 1
			if ccw {
				flip = 2
			}
			res, err = e.app.Rotate(RotateRequest{FlipCode: flip})
		}
		e.postUI(func() {
			e.loading = false
			if err != nil {
				e.showError(err)
				return
			}
			if isDisc {
				e.syncDiscResponse(res)
			} else if res != nil {
				if res.Preview != "" {
					e.setPreview(res.Preview)
				}
				if res.Width > 0 && res.Height > 0 {
					e.realImageDims = image.Pt(res.Width, res.Height)
				}
				if res.DescreenReset {
					e.useDescreenTool = false
				}
			}
			e.unsavedChanges = true
			e.showStatus("")
			e.flushPendingSave()
		})
	})
}
