import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import {
  CompositorOpenFilesDialog,
  CompositorStitch,
  CompositorAlignOverlay,
  CompositorAddMask,
  CompositorUndoMask,
  CompositorClearMasks,
  CompositorRefreshPreview,
  CompositorLoadResult,
} from '../../wailsjs/go/main/App'
import DelayedHint from './DelayedHint'
import { isPreviewPresentationPending, isPreviewVariant, useProgressivePreview } from '../hooks/useProgressivePreview'

const FADE_MS = 150
const MODE_TRANSITION_MS = 180

const ORIENTATIONS = [
  { value: 'ltr', label: '→  Left to right' },
  { value: 'rtl', label: '←  Right to left' },
  { value: 'ttb', label: '↓  Top to bottom' },
  { value: 'btt', label: '↑  Bottom to top' },
]

// CompositorModal — segment stitching and repeated-scan overlay.
//
// Props:
//   open    – whether the modal is visible
//   onClose – called when the user clicks Close or the backdrop
//   onLoad  – called with an ImageInfo-shaped object when the user loads the
//             result into the main editing pipeline
//   dropRef – ref whose .current is set to a callback while the modal is open;
//             the parent's OnFileDrop handler calls it with dropped image paths
export default function CompositorModal({ open, onClose, onLoad, dropRef }) {
  const [mounted, setMounted]       = useState(false)
  const [shown, setShown]           = useState(false)
  const fadeTimer                   = useRef(null)
  const dialogRef = useRef(null)
  const modeStartSizeRef = useRef(null)

  const [paths, setPaths]           = useState([])
  const [orientation, setOrientation] = useState('ltr')
  const [composeMode, setComposeMode] = useState('stitch')
  const [maskLayer, setMaskLayer] = useState(1)
  const [brushSize, setBrushSize] = useState(3)
  const [featherPercent, setFeatherPercent] = useState(20)
  const [maskCount, setMaskCount] = useState(0)
  const [maskBusy, setMaskBusy] = useState(false)
  const [activeStroke, setActiveStroke] = useState(null)
  const strokeRef = useRef(null)
  const previewFrameRef = useRef(null)
  const [previewFrameSize, setPreviewFrameSize] = useState({ width: 0, height: 0 })
  const [previewZoom, setPreviewZoom] = useState(1)
  const zoomAnchorRef = useRef(null)
  const panRef = useRef(null)
  const [stitching, setStitching]   = useState(false)
  const [loading, setLoading]       = useState(false)
  const [status, setStatus]         = useState('')
  const [preview, setPreview]       = useState(null)
  const [presentedPreview, setPresentedPreview] = useState(null)
  const [resultDims, setResultDims] = useState(null)
  const [rotation, setRotation]     = useState(0)   // 0 | 90 | 180 | 270

  // Hooks must run in the same order whether the fading modal is mounted or
  // not. Calling this only after `mounted` becomes true crashes React when the
  // compositor is opened because that render has more hooks than the previous
  // closed render.
  const displayPreview = useProgressivePreview(preview)

  useEffect(() => {
    if (!open || composeMode !== 'overlay' || !resultDims) return
    const frame = previewFrameRef.current
    if (!frame) return
    const updateSize = () => {
      const { width, height } = frame.getBoundingClientRect()
      setPreviewFrameSize(prev => prev.width === width && prev.height === height ? prev : { width, height })
    }
    updateSize()
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', updateSize)
      return () => window.removeEventListener('resize', updateSize)
    }
    const observer = new ResizeObserver(updateSize)
    observer.observe(frame)
    return () => observer.disconnect()
  }, [open, composeMode, resultDims])

  useLayoutEffect(() => {
    const anchor = zoomAnchorRef.current
    const frame = previewFrameRef.current
    if (!anchor || !frame) return
    zoomAnchorRef.current = null
    const image = frame.querySelector('.compositor-mask-surface')
    if (!image) return
    const rect = image.getBoundingClientRect()
    frame.scrollLeft += rect.left + anchor.x * rect.width - anchor.clientX
    frame.scrollTop += rect.top + anchor.y * rect.height - anchor.clientY
  }, [previewZoom, previewFrameSize])

  useEffect(() => {
    if (!open || !mounted || composeMode !== 'overlay' || !resultDims) return
    const frame = previewFrameRef.current
    if (!frame) return
    frame.addEventListener('wheel', zoomPreview, { passive: false })
    return () => frame.removeEventListener('wheel', zoomPreview)
  }, [open, mounted, composeMode, resultDims, previewZoom, previewFrameSize])

  useLayoutEffect(() => {
    const start = modeStartSizeRef.current
    modeStartSizeRef.current = null
    const dialog = dialogRef.current
    if (!open || !start || !dialog?.animate || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return

    const style = getComputedStyle(dialog)
    const resize = dialog.animate([
      start,
      { width: style.width, height: style.height, maxHeight: style.maxHeight },
    ], { duration: MODE_TRANSITION_MS, easing: 'cubic-bezier(0.2, 0.8, 0.2, 1)' })
    const fades = [...dialog.querySelectorAll('.compositor-body, .compositor-footer')].map(element =>
      element.animate([{ opacity: 0.55 }, { opacity: 1 }], { duration: MODE_TRANSITION_MS, easing: 'ease-out' }),
    )
    return () => {
      resize.cancel()
      fades.forEach(animation => animation.cancel())
    }
  }, [composeMode, open])

  // Promoting the result into the editor resets the shared preview store.
  // The compositor keeps its layers, so request a new URL whenever it reopens.
  useEffect(() => {
    if (!open || !resultDims || preview) return
    let cancelled = false
    setStatus('Refreshing compositor preview…')
    CompositorRefreshPreview().then(res => {
      if (cancelled) return
      setPresentedPreview(null)
      setPreview(res.preview)
      setResultDims({ w: res.width, h: res.height })
      setStatus(res.message)
    }).catch(err => {
      if (cancelled) return
      setResultDims(null)
      setMaskCount(0)
      setStatus('Preview unavailable. Align again: ' + (err?.message || String(err)))
    })
    return () => { cancelled = true }
  }, [open, resultDims, preview])

  useEffect(() => {
    setPreview(null)
    setResultDims(null)
    setPresentedPreview(null)
    setPreviewFrameSize({ width: 0, height: 0 })
    setPreviewZoom(1)
    zoomAnchorRef.current = null
    panRef.current = null
    setRotation(0)
    setMaskCount(0)
    setActiveStroke(null)
    strokeRef.current = null
    setStatus('')
  }, [paths, orientation, composeMode])

  // ── Fade mount/unmount ───────────────────────────────────────────────────
  useEffect(() => {
    if (open) {
      clearTimeout(fadeTimer.current)
      setMounted(true)
      requestAnimationFrame(() => requestAnimationFrame(() => setShown(true)))
    } else {
      setShown(false)
      setPreview(null)
      setPresentedPreview(null)
      strokeRef.current = null
      setActiveStroke(null)
      setPreviewZoom(1)
      zoomAnchorRef.current = null
      panRef.current = null
      fadeTimer.current = setTimeout(() => setMounted(false), FADE_MS)
    }
    return () => clearTimeout(fadeTimer.current)
  }, [open])

  useEffect(() => {
    if (!open) return
    const handler = (e) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, onClose])

  // Register drop handler so the parent's OnFileDrop can forward paths here
  useEffect(() => {
    if (!dropRef) return
    if (open) {
      dropRef.current = (imagePaths) => {
        setPaths(prev => {
          const next = [...prev, ...imagePaths]
          setStatus(`${next.length} image(s) queued`)
          return next
        })
      }
    } else {
      dropRef.current = null
    }
    return () => { if (dropRef) dropRef.current = null }
  }, [open, dropRef])

  if (!mounted) return null

  // ── Actions ──────────────────────────────────────────────────────────────

  function handleModeChange(e) {
    const nextMode = e.target.value
    if (nextMode === composeMode) return
    const dialog = dialogRef.current
    if (dialog) {
      // Capture the current animated size too, so rapid switches stay smooth.
      const style = getComputedStyle(dialog)
      modeStartSizeRef.current = { width: style.width, height: style.height, maxHeight: style.maxHeight }
    }
    // Measure the incoming layout without the previous mode's result.
    setPreview(null)
    setPresentedPreview(null)
    setResultDims(null)
    setStatus('')
    setComposeMode(nextMode)
  }

  async function handleAddFiles() {
    try {
      const selected = await CompositorOpenFilesDialog()
      if (selected && selected.length > 0) {
        setPaths(prev => {
          const next = [...prev, ...selected]
          setStatus(`${next.length} image(s) queued`)
          return next
        })
      }
    } catch (err) {
      setStatus('Error opening file dialog: ' + (err?.message || err))
    }
  }

  function handleRemove(idx) {
    setPaths(prev => prev.filter((_, i) => i !== idx))
    setPreview(null)
    setResultDims(null)
    setStatus('')
  }

  function handleMoveUp(idx) {
    if (idx === 0) return
    setPaths(prev => {
      const next = [...prev]
      ;[next[idx - 1], next[idx]] = [next[idx], next[idx - 1]]
      return next
    })
  }

  function handleMoveDown(idx) {
    setPaths(prev => {
      if (idx >= prev.length - 1) return prev
      const next = [...prev]
      ;[next[idx], next[idx + 1]] = [next[idx + 1], next[idx]]
      return next
    })
  }

  function handleClearAll() {
    setPaths([])
    setPreview(null)
    setResultDims(null)
    setStatus('')
  }

  async function handleStitch() {
    if (paths.length < 2) {
      setStatus('Add at least 2 images first.')
      return
    }
    setStitching(true)
    setPreview(null)
    setResultDims(null)
    setStatus(composeMode === 'overlay' ? 'Aligning scans…' : 'Stitching…')
    try {
      const res = composeMode === 'overlay'
        ? await CompositorAlignOverlay({ imagePaths: paths })
        : await CompositorStitch({ imagePaths: paths, orientation })
      setPreview(res.preview)
      setResultDims({ w: res.width, h: res.height })
      setMaskLayer(paths.length - 1)
      setMaskCount(0)
      setRotation(0)
      setPreviewZoom(1)
      zoomAnchorRef.current = null
      setStatus(res.message)
    } catch (err) {
      setStatus((composeMode === 'overlay' ? 'Alignment failed: ' : 'Stitch failed: ') + (err?.message || String(err)))
    } finally {
      setStitching(false)
    }
  }

  function pointFromEvent(e) {
    const rect = e.currentTarget.getBoundingClientRect()
    return {
      x: Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)),
      y: Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height)),
    }
  }

  function zoomPreview(e) {
    if (composeMode !== 'overlay' || !overlayFit) return
    e.preventDefault()
    const frame = previewFrameRef.current
    const image = frame?.querySelector('.compositor-mask-surface')
    if (!frame || !image) return
    const rect = image.getBoundingClientRect()
    const next = Math.max(1, Math.min(8, previewZoom * Math.exp(-e.deltaY * 0.0015)))
    if (Math.abs(next - previewZoom) < 0.001) return
    zoomAnchorRef.current = {
      x: Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)),
      y: Math.max(0, Math.min(1, (e.clientY - rect.top) / rect.height)),
      clientX: e.clientX,
      clientY: e.clientY,
    }
    setPreviewZoom(next)
  }

  function startPan(e) {
    if (composeMode !== 'overlay' || e.button !== 1) return
    e.preventDefault()
    e.currentTarget.setPointerCapture(e.pointerId)
    panRef.current = { x: e.clientX, y: e.clientY, left: e.currentTarget.scrollLeft, top: e.currentTarget.scrollTop }
  }

  function movePan(e) {
    if (!panRef.current) return
    e.currentTarget.scrollLeft = panRef.current.left + panRef.current.x - e.clientX
    e.currentTarget.scrollTop = panRef.current.top + panRef.current.y - e.clientY
  }

  function finishPan() { panRef.current = null }

  function startMask(e) {
    if (e.button !== 0 || composeMode !== 'overlay' || stitchBusy || !hasResult) return
    e.preventDefault()
    e.currentTarget.setPointerCapture(e.pointerId)
    const points = [pointFromEvent(e)]
    strokeRef.current = points
    setActiveStroke(points)
  }

  function moveMask(e) {
    if (!strokeRef.current) return
    const point = pointFromEvent(e)
    const last = strokeRef.current[strokeRef.current.length - 1]
    if (Math.hypot(point.x - last.x, point.y - last.y) < .002) return
    const points = [...strokeRef.current, point]
    strokeRef.current = points
    setActiveStroke(points)
  }

  async function finishMask(e) {
    if (!strokeRef.current) return
    const points = strokeRef.current
    strokeRef.current = null
    setActiveStroke(null)
    if (e.type === 'pointercancel') return
    setMaskBusy(true)
    setStatus('Updating mask…')
    try {
      const res = await CompositorAddMask({ stroke: {
        layer: maskLayer,
        radius: brushSize / 100,
        feather: featherPercent / 100,
        points,
      } })
      setPreview(res.preview)
      setMaskCount(count => count + 1)
      setStatus('Painted area taken from the selected scan. Paint more or load the output.')
    } catch (err) {
      setStatus('Mask failed: ' + (err?.message || String(err)))
    } finally {
      setMaskBusy(false)
    }
  }

  async function changeMasks(clear) {
    setMaskBusy(true)
    try {
      const res = clear ? await CompositorClearMasks() : await CompositorUndoMask()
      setPreview(res.preview)
      setMaskCount(count => clear ? 0 : Math.max(0, count - 1))
      setStatus(clear ? 'All painted areas cleared.' : 'Last paint stroke undone.')
    } catch (err) {
      setStatus('Mask update failed: ' + (err?.message || String(err)))
    } finally {
      setMaskBusy(false)
    }
  }

  async function handleLoadForCropping() {
    setLoading(true)
    setStatus('')
    try {
      const steps = (((rotation / 90) % 4) + 4) % 4
      const info = await CompositorLoadResult({ rotationSteps: steps })
      onLoad(info)
    } catch (err) {
      setStatus('Load failed: ' + (err?.message || String(err)))
    } finally {
      setLoading(false)
    }
  }

  // ── Render ───────────────────────────────────────────────────────────────
  const hasResult = preview !== null
  const previewPresentationPending = isPreviewPresentationPending(preview, presentedPreview)
  const stitchBusy = stitching || maskBusy || previewPresentationPending || (open && resultDims !== null && preview === null)
  const canStitch = paths.length >= 2 && !stitchBusy
  const fitScale = composeMode === 'overlay' && resultDims && previewFrameSize.width > 0 && previewFrameSize.height > 0
    ? Math.min(previewFrameSize.width / resultDims.w, previewFrameSize.height / resultDims.h)
    : 0
  const overlayFit = fitScale > 0
    ? { width: Math.max(1, Math.floor(resultDims.w * fitScale)), height: Math.max(1, Math.floor(resultDims.h * fitScale)) }
    : null
  const overlaySize = overlayFit && { width: overlayFit.width * previewZoom, height: overlayFit.height * previewZoom }
  const overlayStage = overlaySize && {
    width: Math.max(previewFrameSize.width, overlaySize.width),
    height: Math.max(previewFrameSize.height, overlaySize.height),
  }

  return (
    <div
      className={`options-backdrop ${shown ? 'visible' : ''}`}
      onClick={onClose}
    >
      <div
        ref={dialogRef}
        className={`options-dialog compositor-dialog${composeMode === 'overlay' ? ' compositor-dialog--overlay' : ''}`}
        onClick={e => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Image Compositor"
      >
        {/* Header */}
        <div className="options-header compositor-header">
          <span className="options-title">Image Compositor</span>
          {composeMode === 'overlay' && hasResult && <div className="compositor-mask-tools">
            <label htmlFor="compositor-mask-layer">Paint from</label>
            <select id="compositor-mask-layer" value={maskLayer} onChange={e => setMaskLayer(Number(e.target.value))} disabled={stitchBusy}>
              {paths.slice(1).map((p, i) => <option key={i + 1} value={i + 1}>{i + 2}: {p.replace(/.*[\\/]/, '')}</option>)}
            </select>
            <label htmlFor="compositor-brush-size">Brush</label>
            <input id="compositor-brush-size" type="range" min="0.5" max="10" step="0.5" value={brushSize}
              onChange={e => setBrushSize(Number(e.target.value))} disabled={stitchBusy} />
            <span className="compositor-slider-value">{brushSize}%</span>
            <label htmlFor="compositor-feather" title="0% is a hard edge; higher values blend a wider rim.">Feather</label>
            <input id="compositor-feather" type="range" min="0" max="100" step="1" value={featherPercent}
              title="0% is a hard edge; higher values blend a wider rim. Applies to new strokes."
              onChange={e => setFeatherPercent(Number(e.target.value))} disabled={stitchBusy} />
            <span className="compositor-slider-value">{featherPercent}%</span>
            <button aria-label="Undo paint" onClick={() => changeMasks(false)} disabled={!maskCount || stitchBusy}>Undo</button>
            <button aria-label="Clear paint" onClick={() => changeMasks(true)} disabled={!maskCount || stitchBusy}>Clear</button>
          </div>}
          <button className="options-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        {/* Body */}
        <div className="options-body compositor-body">
          <p className="compositor-hint">
            {composeMode === 'stitch'
              ? 'Add overlapping scan segments in order to extend them into one image.'
              : 'Put the complete scan first. Add clean detail scans after it, then paint only the areas to take from those scans. Unpainted areas stay from the first scan. The compositor finds quarter-turn rotations automatically.'}
          </p>

          <div className="compositor-orientation-row">
            <label className="compositor-orientation-label" htmlFor="compositor-mode">Mode</label>
            <select id="compositor-mode" className="compositor-orientation-select" value={composeMode}
              onChange={handleModeChange} disabled={stitchBusy}>
              <option value="stitch">Stitch segments</option>
              <option value="overlay">Overlay repeated scans</option>
            </select>
          </div>

          {/* Orientation selector */}
          {composeMode === 'stitch' && <div className="compositor-orientation-row">
            <label className="compositor-orientation-label">Orientation</label>
            <select
              className="compositor-orientation-select"
              value={orientation}
              onChange={e => setOrientation(e.target.value)}
              disabled={stitching}
            >
              {ORIENTATIONS.map(o => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>}

          {/* File list */}
          <div className="compositor-file-list">
            {paths.length === 0 && (
              <div className="compositor-empty">No images added yet.</div>
            )}
            {paths.map((p, idx) => (
              <div key={idx} className="compositor-file-row">
                <span className="compositor-file-index">{idx + 1}</span>
                <DelayedHint hint={p}>
                  <span className="compositor-file-name">
                    {p.replace(/.*[\\/]/, '')}
                  </span>
                </DelayedHint>
                <div className="compositor-file-actions">
                  <DelayedHint hint={composeMode === 'overlay' ? 'Move lower in the layer stack.' : 'Move earlier in the sequence.'}>
                    <button
                      className="compositor-arrow"
                      onClick={() => handleMoveUp(idx)}
                      disabled={idx === 0 || stitching}
                    >▲</button>
                  </DelayedHint>
                  <DelayedHint hint={composeMode === 'overlay' ? 'Move higher in the layer stack.' : 'Move later in the sequence.'}>
                    <button
                      className="compositor-arrow"
                      onClick={() => handleMoveDown(idx)}
                      disabled={idx === paths.length - 1 || stitching}
                    >▼</button>
                  </DelayedHint>
                  <DelayedHint hint="Remove from sequence.">
                    <button
                      className="compositor-remove"
                      onClick={() => handleRemove(idx)}
                      disabled={stitching}
                    >✕</button>
                  </DelayedHint>
                </div>
              </div>
            ))}
          </div>

          <div className="compositor-file-controls">
            <DelayedHint hint="Open a file picker to add images to the sequence.">
              <button className="load-btn" onClick={handleAddFiles} disabled={stitchBusy}>
                Add images…
              </button>
            </DelayedHint>
            {paths.length > 0 && (
              <DelayedHint hint="Remove all images from the sequence.">
                <button
                  className="reset-btn-danger"
                  onClick={handleClearAll}
                  disabled={stitchBusy}
                >
                  Clear all
                </button>
              </DelayedHint>
            )}
          </div>

          {/* Status */}
          {status && (
            <div className="compositor-status">{status}</div>
          )}

          {/* Preview */}
          {hasResult && (
            <div className={`compositor-preview-wrap${composeMode === 'overlay' ? ' compositor-preview-wrap--overlay' : ''}`}>
              {composeMode === 'overlay' && previewZoom > 1.01 && <button className="compositor-preview-fit"
                onClick={() => { zoomAnchorRef.current = null; setPreviewZoom(1) }} title="Fit the full image in the preview">Fit</button>}
              <div className="compositor-rotate-row">
                {composeMode === 'stitch' && <DelayedHint hint="Rotate 90° counter-clockwise.">
                  <button
                    className="compositor-rotate-btn"
                    onClick={() => setRotation(r => r - 90)}
                  >↺</button>
                </DelayedHint>}
                <div ref={previewFrameRef} className={`compositor-preview-frame${composeMode === 'overlay' ? ' compositor-preview-frame--overlay' : ''}${Math.abs(rotation) % 180 !== 0 ? ' compositor-preview-frame--sideways' : ''}`}
                  onPointerDown={startPan} onPointerMove={movePan} onPointerUp={finishPan} onPointerCancel={finishPan}
                  onAuxClick={e => { if (composeMode === 'overlay' && e.button === 1) e.preventDefault() }}
                  title={composeMode === 'overlay' ? 'Scroll to zoom; middle-drag to pan' : undefined}>
                  {displayPreview && (
                    <div className={composeMode === 'overlay' ? 'compositor-preview-stage' : undefined}
                      style={composeMode === 'overlay' ? (overlayStage || { width: 1, height: 1, visibility: 'hidden' }) : undefined}>
                    <div className={`compositor-mask-surface${composeMode === 'overlay' ? ' compositor-mask-surface--paint' : ''}`}
                      style={composeMode === 'overlay' ? (overlaySize || { width: 1, height: 1, visibility: 'hidden' }) : undefined}
                      onPointerDown={startMask} onPointerMove={moveMask} onPointerUp={finishMask} onPointerCancel={finishMask}>
                      <img
                        src={displayPreview}
                        alt={composeMode === 'overlay' ? 'Aligned scan preview' : 'Stitched preview'}
                        draggable={false}
                        className="compositor-preview"
                        style={composeMode === 'overlay' ? undefined : { transform: `rotate(${rotation}deg)` }}
                        onLoad={() => {
                          if (isPreviewVariant(displayPreview, preview)) {
                            setPresentedPreview(preview)
                          }
                        }}
                      />
                      {composeMode === 'overlay' && activeStroke && resultDims && <svg className="compositor-mask-drawing"
                        viewBox={`0 0 ${resultDims.w} ${resultDims.h}`} preserveAspectRatio="none" aria-hidden="true">
                        <polyline points={activeStroke.map(p => `${p.x * resultDims.w},${p.y * resultDims.h}`).join(' ')}
                          fill="none" stroke="#ffc36b" strokeOpacity="0.75" strokeLinecap="round" strokeLinejoin="round"
                          strokeWidth={brushSize * Math.min(resultDims.w, resultDims.h) / 50} />
                        {activeStroke.length === 1 && <circle cx={activeStroke[0].x * resultDims.w} cy={activeStroke[0].y * resultDims.h}
                          r={brushSize * Math.min(resultDims.w, resultDims.h) / 100} fill="#ffc36b" fillOpacity="0.55" />}
                      </svg>}
                    </div>
                    </div>
                  )}
                </div>
                {composeMode === 'stitch' && <DelayedHint hint="Rotate 90° clockwise.">
                  <button
                    className="compositor-rotate-btn"
                    onClick={() => setRotation(r => r + 90)}
                  >↻</button>
                </DelayedHint>}
              </div>
              {composeMode === 'stitch' && resultDims && (() => {
                const normDeg = ((rotation % 360) + 360) % 360
                const sideways = normDeg === 90 || normDeg === 270
                const dispDeg  = normDeg === 0 ? 0 : (normDeg <= 180 ? normDeg : normDeg - 360)
                return (
                  <div className="compositor-dims">
                    {sideways
                      ? `${resultDims.h} × ${resultDims.w} px`
                      : `${resultDims.w} × ${resultDims.h} px`}
                    {dispDeg !== 0 && <span className="compositor-dims-rotation"> ({dispDeg > 0 ? '+' : ''}{dispDeg}°)</span>}
                  </div>
                )
              })()}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="options-footer compositor-footer">
          <div className="compositor-footer-row">
            <DelayedHint hint={composeMode === 'overlay' ? 'Align all scans to the first scan.' : 'Align and blend all queued segments.'}>
              <button
                className="primary"
                onClick={handleStitch}
                disabled={!canStitch}
              >
                {stitchBusy ? <span className="btn-spinner" /> : composeMode === 'overlay' ? 'Align scans' : 'Stitch images'}
              </button>
            </DelayedHint>
            <DelayedHint hint="Load the composite as the active image for cropping and adjustment.">
              <button
                className="load-btn"
                onClick={handleLoadForCropping}
                disabled={!hasResult || stitchBusy || loading}
              >
                {loading ? <span className="btn-spinner" /> : 'Load output'}
              </button>
            </DelayedHint>
            <button className="options-btn" onClick={onClose}>Close</button>
          </div>
        </div>
      </div>
    </div>
  )
}
