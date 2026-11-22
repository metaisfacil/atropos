package ui

import (
	"image"
	"image/draw"
	"time"
)

// Lifecycle of the live Ctrl+drag layer (rendering lives in
// disc_shift_live.go). All methods run on the UI thread.
//
// A layer is built once (prewarmed, or on demand at press) and then serves a
// chain of drags: each release adds its shift to base and queues a ShiftDisc
// commit, and the layer stays on screen until the render of the last commit
// is presented. A press during that time simply continues on the same layer,
// so rapid successive drags never fall back to the stale raster.

// discShiftLiveKey identifies the presentation and layout a layer was built for.
type discShiftLiveKey struct {
	source   string
	info     viewportLayoutInfo
	maxShift float64
}

// discShiftWarmWant is the prewarm target last scheduled.
type discShiftWarmWant struct {
	key      discShiftLiveKey
	eligible bool
}

func (e *EditorUI) discShiftKey(info viewportLayoutInfo) discShiftLiveKey {
	return discShiftLiveKey{
		source:   e.mainViewport.PresentedSource,
		info:     info,
		maxShift: discShiftMaxShift(info),
	}
}

// visibleDiscShiftLive returns the layer to draw this frame, if any.
func (e *EditorUI) visibleDiscShiftLive() *discShiftLiveLayer {
	if l := e.discShiftLive; l != nil && l.showing {
		return l
	}
	return nil
}

// runDetached runs fn off the UI thread (inline in synchronous test mode).
func (e *EditorUI) runDetached(fn func()) {
	if e.syncJobs {
		fn()
		return
	}
	go fn()
}

// buildDiscShiftLive fetches the disc geometry on the backend worker, builds
// the layer on a separate goroutine (so it never delays queued edits), and
// calls done on the UI thread with the layer, or nil if unavailable.
func (e *EditorUI) buildDiscShiftLive(key discShiftLiveKey, done func(*discShiftLiveLayer)) {
	if e.app == nil {
		done(nil)
		return
	}
	e.runBackend(func() {
		geo, err := e.app.DiscShiftPreview()
		var src *image.NRGBA
		ok := err == nil && geo != nil
		if ok {
			src, ok = e.app.ResolvePreviewImage(geo.Source)
		}
		if !ok {
			e.postUI(func() { done(nil) })
			return
		}
		e.runDetached(func() {
			l := buildDiscShiftLiveLayer(*geo, src, key.info, key.maxShift)
			if l != nil {
				l.key.source = key.source
				l.validSource = key.source
				l.own = map[string]bool{key.source: true}
			}
			e.postUI(func() { done(l) })
		})
	})
}

// startDiscShiftLive picks the live layer for the Ctrl+drag that just
// started: the current chain's layer if one is showing or still matches the
// screen, else the prewarmed one, else one built on demand (the legacy
// whole-raster translate shows until it lands).
func (e *EditorUI) startDiscShiftLive(info viewportLayoutInfo) {
	key := e.discShiftKey(info)
	warm := e.discShiftWarm
	warmOK := warm != nil && e.discShiftWarmKey == key
	if l := e.discShiftLive; l != nil {
		matches := !l.stale && l.validSource == key.source && l.key.info == info
		if l.showing || (matches && !(l.coverageLow() && warmOK)) {
			e.beginDiscShiftGesture(l)
			return
		}
	}
	e.clearDiscShiftLive()
	if warmOK {
		e.discShiftWarm = nil
		e.discShiftLive = warm
		e.beginDiscShiftGesture(warm)
		return
	}
	gen := e.discShiftLiveGen
	e.buildDiscShiftLive(key, func(l *discShiftLiveLayer) {
		if l == nil || gen != e.discShiftLiveGen || e.ctrlDrag == nil || e.discShiftLive != nil {
			return
		}
		e.discShiftLive = l
		e.beginDiscShiftGesture(l)
		l.dx, l.dy = l.clampGesture(e.ctrlDragShift.X, e.ctrlDragShift.Y)
		e.invalidate()
	})
}

// beginDiscShiftGesture makes l show and follow the pointer from its current
// accumulated shift.
func (e *EditorUI) beginDiscShiftGesture(l *discShiftLiveLayer) {
	l.dragging, l.showing = true, true
	l.dx, l.dy = 0, 0
	if l.next != nil {
		l.overlay, l.overlayExact, l.next = *l.next, false, nil
	}
	if e.ctrlDrag != nil {
		// Clamp against where the disc will be once queued commits land.
		e.ctrlDrag.startCenter = image.Pt(l.geo.CenterX+l.base.X, l.geo.CenterY+l.base.Y)
	}
}

// releaseDiscShiftGesture ends the current gesture. A non-zero shift joins
// base and its commit is tracked; a zero shift just settles.
func (e *EditorUI) releaseDiscShiftGesture(dx, dy int) {
	l := e.discShiftLive
	if l == nil || !l.dragging {
		return
	}
	l.dragging = false
	l.dx, l.dy = 0, 0
	if dx == 0 && dy == 0 {
		e.settleDiscShiftLive(l)
		return
	}
	l.base = l.base.Add(image.Pt(dx, dy))
	l.pending++
	e.refreshDiscShiftOverlay(l)
}

// cancelDiscShiftGesture abandons the current gesture without committing,
// keeping any chain of earlier commits on screen.
func (e *EditorUI) cancelDiscShiftGesture() {
	e.ctrlDragShift = image.Point{}
	if l := e.discShiftLive; l != nil && l.dragging {
		l.dragging = false
		l.dx, l.dy = 0, 0
		e.settleDiscShiftLive(l)
	}
}

// refreshDiscShiftOverlay re-renders the vignette for l.base off the UI
// thread: first the exact variant shown while waiting for the commit, then
// the translucent one the next gesture starts from.
func (e *EditorUI) refreshDiscShiftOverlay(l *discShiftLiveLayer) {
	l.overlayGen++
	l.next = nil
	gen, total := l.overlayGen, l.base
	e.runDetached(func() {
		exact := l.computeOverlay(total, true)
		e.postUI(func() {
			if l.overlayGen == gen && !l.dragging {
				l.overlay, l.overlayExact = exact, true
			}
		})
		translucent := l.computeOverlay(total, false)
		e.postUI(func() {
			if l.overlayGen != gen {
				return
			}
			if l.dragging {
				l.overlay, l.overlayExact = translucent, false
			} else {
				l.next = &translucent
			}
		})
	})
}

// discShiftCommitted records the outcome of one of the chain's commits.
func (e *EditorUI) discShiftCommitted(l *discShiftLiveLayer, dx, dy int, res *ProcessResult) {
	if l == nil || e.discShiftLive != l {
		return
	}
	l.pending = max(0, l.pending-1)
	l.settled = l.settled.Add(image.Pt(dx, dy))
	if res == nil || res.DiscCenterX != l.geo.CenterX+l.settled.X || res.DiscCenterY != l.geo.CenterY+l.settled.Y {
		// The backend clamped differently; the layer no longer matches.
		l.stale = true
	}
	if res != nil && res.Preview != "" {
		l.validSource = res.Preview
		l.own[res.Preview] = true
	}
	e.settleDiscShiftLive(l)
}

// settleDiscShiftLive hides l once nothing is moving or pending and the
// matching render is on screen.
func (e *EditorUI) settleDiscShiftLive(l *discShiftLiveLayer) {
	if l.dragging || l.pending > 0 || e.mainViewport.PresentedSource != l.validSource {
		return
	}
	if l.showing {
		l.showing = false
		e.invalidate()
	}
	if l.stale {
		e.clearDiscShiftLive()
		return
	}
	l.own = map[string]bool{l.validSource: true}
}

// onDiscShiftPresented tracks which render is on screen.
func (e *EditorUI) onDiscShiftPresented(source string) {
	l := e.discShiftLive
	if l == nil {
		return
	}
	if l.own[source] {
		e.settleDiscShiftLive(l)
		return
	}
	// Something else changed the render (feather, undo, keyboard nudge...).
	if l.dragging || l.pending > 0 {
		l.stale = true
		return
	}
	e.clearDiscShiftLive()
}

// maybeWarmDiscShift is called every frame. Shortly after the disc render or
// viewport layout settles it prebuilds a layer in the background when the
// current one cannot serve the next press, so a Ctrl+drag normally shows the
// moving-content preview from the first frame.
func (e *EditorUI) maybeWarmDiscShift() {
	if e.syncJobs {
		return
	}
	key := e.discShiftKey(e.mainViewport.LastLayout)
	eligible := e.app != nil && e.imageLoaded && e.mode == "disc" && e.discActive && key.source != ""
	if l := e.discShiftLive; l != nil && eligible {
		if l.showing {
			eligible = false // mid-chain: the geometry is still moving
		} else if !l.stale && l.validSource == key.source && l.key.info == key.info && !l.coverageLow() {
			eligible = false // the idle layer already serves the next press
		}
	}
	want := discShiftWarmWant{key: key, eligible: eligible}
	if want == e.discShiftWarmWant {
		return
	}
	e.discShiftWarmWant = want
	e.discShiftWarmGen++
	if e.discShiftWarmKey != key {
		e.discShiftWarm = nil
	}
	if !eligible || e.discShiftWarm != nil {
		return
	}
	gen := e.discShiftWarmGen
	time.AfterFunc(discShiftWarmDelay, func() {
		e.postUI(func() {
			if gen != e.discShiftWarmGen {
				return
			}
			if e.ctrlDrag != nil || e.isBusy() {
				// Try again once things are idle.
				e.discShiftWarmWant = discShiftWarmWant{}
				return
			}
			e.buildDiscShiftLive(key, func(l *discShiftLiveLayer) {
				if l != nil && gen == e.discShiftWarmGen {
					e.discShiftWarm, e.discShiftWarmKey = l, key
				}
			})
		})
	})
}

// clearDiscShiftLive drops the live layer (and any chain it was tracking)
// and invalidates an in-flight on-demand build.
func (e *EditorUI) clearDiscShiftLive() {
	e.discShiftLiveGen++
	e.discShiftLive = nil
	e.ctrlDragShift = image.Point{}
}

// gpuRGBA returns img as the premultiplied *image.RGBA that paint.NewImageOp
// uses without copying. A tightly packed, origin-based, fully opaque image
// shares its pixels (NRGBA and RGBA are identical then); anything else is
// converted exactly as NewImageOp itself would.
func gpuRGBA(img *image.NRGBA) *image.RGBA {
	packed := img.Rect.Min == (image.Point{}) && img.Stride == 4*img.Rect.Dx() && len(img.Pix) == img.Stride*img.Rect.Dy()
	if packed {
		opaque := true
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] != 255 {
				opaque = false
				break
			}
		}
		if opaque {
			return &image.RGBA{Pix: img.Pix, Stride: img.Stride, Rect: img.Rect}
		}
	}
	dst := image.NewRGBA(image.Rectangle{Max: img.Rect.Size()})
	draw.Draw(dst, dst.Rect, img, img.Rect.Min, draw.Src)
	return dst
}
