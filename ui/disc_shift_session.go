package ui

import (
	"image"
	"image/draw"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
)

// discRotateDeadZoneDp is the radius around the rotation pivot inside which
// Shift+drag ignores the pointer (its angle there is unstable).
const discRotateDeadZoneDp = 12

// Lifecycle of the live Ctrl+drag layer (rendering lives in
// disc_shift_live.go). All methods run on the UI thread.
//
// A layer is built once (prewarmed, or on demand at press) and then serves a
// chain of drags: each release adds its shift or rotation to base and queues
// a ShiftDisc / RotateDisc commit, and the layer stays on screen until the render of the last commit
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

// startDiscLive picks the live layer for the Ctrl+drag (shift) or
// Shift+drag (rotate) that just started: the current chain's layer if one is
// showing or still matches the screen, else the prewarmed one, else one built
// on demand (the legacy whole-raster transform shows until it lands).
func (e *EditorUI) startDiscLive(info viewportLayoutInfo) {
	key := e.discShiftKey(info)
	warm := e.discShiftWarm
	warmOK := warm != nil && e.discShiftWarmKey == key
	if l := e.discShiftLive; l != nil {
		matches := !l.stale && l.validSource == key.source && l.key.info == info
		if l.showing || (matches && !(l.coverageLow() && warmOK)) {
			e.beginDiscLiveGesture(l)
			return
		}
	}
	e.clearDiscShiftLive()
	if warmOK {
		e.discShiftWarm = nil
		e.discShiftLive = warm
		e.beginDiscLiveGesture(warm)
		return
	}
	gen := e.discShiftLiveGen
	e.buildDiscShiftLive(key, func(l *discShiftLiveLayer) {
		if l == nil || gen != e.discShiftLiveGen || (e.ctrlDrag == nil && e.shiftDrag == nil) || e.discShiftLive != nil {
			return
		}
		e.discShiftLive = l
		e.beginDiscLiveGesture(l)
		l.gesture = l.limitGesture(e.liveGesture)
		e.invalidate()
	})
}

// beginDiscLiveGesture makes l show and follow the pointer from its current
// accumulated pose.
func (e *EditorUI) beginDiscLiveGesture(l *discShiftLiveLayer) {
	l.dragging, l.showing = true, true
	l.gesture = discLivePose{}
	if l.next != nil {
		l.overlay, l.overlayExact, l.next = *l.next, false, nil
	}
	if e.ctrlDrag != nil {
		// Clamp against where the disc will be once queued commits land.
		e.ctrlDrag.startCenter = image.Pt(l.geo.CenterX+l.base.shift.X, l.geo.CenterY+l.base.shift.Y)
	}
}

// liveDiscRotation is the disc rotation the screen currently shows, counting
// chain commits that have not landed yet.
func (e *EditorUI) liveDiscRotation() float64 {
	if l := e.discShiftLive; l != nil && l.showing {
		return l.rotation(l.base)
	}
	return e.discRotation
}

// releaseDiscLiveGesture ends the current gesture. A non-zero change joins
// base and its commit is tracked; a zero change just settles.
func (e *EditorUI) releaseDiscLiveGesture(p discLivePose) {
	e.liveGesture = discLivePose{}
	l := e.discShiftLive
	if l == nil || !l.dragging {
		return
	}
	l.dragging = false
	l.gesture = discLivePose{}
	if p.isZero() {
		e.settleDiscShiftLive(l)
		return
	}
	l.base = l.base.add(p)
	l.pending++
	e.refreshDiscShiftOverlay(l)
}

// cancelDiscLiveGesture abandons the current gesture without committing,
// keeping any chain of earlier commits on screen.
func (e *EditorUI) cancelDiscLiveGesture() {
	e.liveGesture = discLivePose{}
	if l := e.discShiftLive; l != nil && l.dragging {
		l.dragging = false
		l.gesture = discLivePose{}
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

// discLiveCommitted records the outcome of one of the chain's commits.
func (e *EditorUI) discLiveCommitted(l *discShiftLiveLayer, p discLivePose, res *ProcessResult) {
	if l == nil || e.discShiftLive != l {
		return
	}
	l.pending = max(0, l.pending-1)
	l.settled = l.settled.add(p)
	want := l.geo
	if res == nil || res.DiscCenterX != want.CenterX+l.settled.shift.X || res.DiscCenterY != want.CenterY+l.settled.shift.Y ||
		math.Abs(res.DiscRotation-l.rotation(l.settled)) > 1e-6 {
		// The backend ended up somewhere else (e.g. clamped the centre).
		l.stale = true
	}
	if res != nil && res.Preview != "" {
		l.validSource = res.Preview
		l.own[res.Preview] = true
	}
	e.settleDiscShiftLive(l)
}

// discRotateDragState turns a Shift+drag into a rotation: the disc turns by
// the angle the pointer sweeps around the rotation pivot, like grabbing the
// disc and turning it. The angle is unwrapped so full turns accumulate.
type discRotateDragState struct {
	pivot   f32.Point // screen px the last angle was measured around
	pointer f32.Point // last pointer position (for the guide)
	lastRaw float64   // last pointer angle around pivot (degrees)
	rebase  bool      // next sample only re-anchors lastRaw
	total   float64   // unwrapped gesture angle (degrees, clockwise positive)
	applied float64   // angle actually shown (after coverage limits)
}

// update folds a pointer sample into the gesture. Within deadZone of the
// pivot the angle is meaningless, so the gesture pauses there and re-anchors
// on leaving it instead of jumping; a moved pivot re-anchors likewise.
func (r *discRotateDragState) update(p, pivot f32.Point, deadZone float32) {
	r.pointer = p
	v := p.Sub(pivot)
	if math.Hypot(float64(v.X), float64(v.Y)) < float64(deadZone) {
		r.rebase = true
		return
	}
	raw := math.Atan2(float64(v.Y), float64(v.X)) * 180 / math.Pi
	if r.rebase || pivot != r.pivot {
		r.lastRaw, r.pivot, r.rebase = raw, pivot, false
		return
	}
	d := math.Mod(raw-r.lastRaw+540, 360) - 180
	r.total += d
	r.lastRaw = raw
}

// discRotatePivot is the screen position the disc rotates around: the live
// layer's pivot when available, else the stage centre the legacy transform
// uses.
func (e *EditorUI) discRotatePivot(info viewportLayoutInfo) f32.Point {
	if l := e.discShiftLive; l != nil && l.dragging && l.geo.Width > 0 && l.geo.Height > 0 {
		return f32.Pt(
			float32(info.StageX+l.geo.PivotX*info.StageWidth/float64(l.geo.Width)),
			float32(info.StageY+l.geo.PivotY*info.StageHeight/float64(l.geo.Height)),
		)
	}
	return f32.Pt(float32(info.StageX+info.StageWidth/2), float32(info.StageY+info.StageHeight/2))
}

// discRotateGuide describes the rotation handle to draw, if a Shift+drag is
// active.
func (e *EditorUI) discRotateGuide() *discRotateGuide {
	r := e.shiftDrag
	if r == nil || r.pivot == (f32.Point{}) {
		return nil
	}
	return &discRotateGuide{Pivot: r.pivot, Pointer: r.pointer, Angle: r.applied}
}

// updateDiscRotateDrag applies a pointer sample to the active Shift+drag.
func (e *EditorUI) updateDiscRotateDrag(gtx layout.Context, info viewportLayoutInfo, pos f32.Point) {
	r := e.shiftDrag
	if r == nil {
		return
	}
	r.update(pos, e.discRotatePivot(info), float32(gtx.Dp(discRotateDeadZoneDp)))
	e.liveGesture = discLivePose{angle: r.total}
	r.applied = r.total
	if l := e.discShiftLive; l != nil && l.dragging {
		l.gesture = l.limitGesture(e.liveGesture)
		r.applied = l.gesture.angle
	}
	e.discLiveTransform.Angle = float32(r.applied)
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
	e.liveGesture = discLivePose{}
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
