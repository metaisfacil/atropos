import { useEffect, useRef, useState } from 'react'
import {
  CompositorOpenFilesDialog,
  CompositorStitch,
  CompositorLoadResult,
} from '../../wailsjs/go/main/App'
import DelayedHint from './DelayedHint'

const FADE_MS = 150

const ORIENTATIONS = [
  { value: 'ltr', label: '→  Left to right' },
  { value: 'rtl', label: '←  Right to left' },
  { value: 'ttb', label: '↓  Top to bottom' },
  { value: 'btt', label: '↑  Bottom to top' },
]

// CompositorModal — proof-of-concept UI for the image stitching feature.
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

  const [paths, setPaths]           = useState([])
  const [orientation, setOrientation] = useState('ltr')
  const [stitching, setStitching]   = useState(false)
  const [loading, setLoading]       = useState(false)
  const [status, setStatus]         = useState('')
  const [preview, setPreview]       = useState(null)
  const [resultDims, setResultDims] = useState(null)
  const [rotation, setRotation]     = useState(0)   // 0 | 90 | 180 | 270

  // ── Fade mount/unmount ───────────────────────────────────────────────────
  useEffect(() => {
    if (open) {
      clearTimeout(fadeTimer.current)
      setMounted(true)
      requestAnimationFrame(() => requestAnimationFrame(() => setShown(true)))
    } else {
      setShown(false)
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
    setStatus('Stitching…')
    try {
      const res = await CompositorStitch({ imagePaths: paths, orientation })
      setPreview(res.preview)
      setResultDims({ w: res.width, h: res.height })
      setRotation(0)
      setStatus(res.message)
    } catch (err) {
      setStatus('Stitch failed: ' + (err?.message || String(err)))
    } finally {
      setStitching(false)
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
  const canStitch = paths.length >= 2 && !stitching
  const hasResult = preview !== null

  return (
    <div
      className={`options-backdrop ${shown ? 'visible' : ''}`}
      onClick={onClose}
    >
      <div
        className="options-dialog compositor-dialog"
        onClick={e => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Image Compositor"
      >
        {/* Header */}
        <div className="options-header">
          <span className="options-title">Image Compositor</span>
          <button className="options-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        {/* Body */}
        <div className="options-body compositor-body">
          <p className="compositor-hint">
            Add two or more overlapping scan segments in order, then stitch them
            into a single continuous image. Use the orientation selector to
            describe how the segments are arranged.
          </p>

          {/* Orientation selector */}
          <div className="compositor-orientation-row">
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
          </div>

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
                  <DelayedHint hint="Move earlier in the sequence.">
                    <button
                      className="compositor-arrow"
                      onClick={() => handleMoveUp(idx)}
                      disabled={idx === 0 || stitching}
                    >▲</button>
                  </DelayedHint>
                  <DelayedHint hint="Move later in the sequence.">
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
              <button className="load-btn" onClick={handleAddFiles} disabled={stitching}>
                Add images…
              </button>
            </DelayedHint>
            {paths.length > 0 && (
              <DelayedHint hint="Remove all images from the sequence.">
                <button
                  className="reset-btn-danger"
                  onClick={handleClearAll}
                  disabled={stitching}
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
            <div className="compositor-preview-wrap">
              <div className="compositor-rotate-row">
                <DelayedHint hint="Rotate 90° counter-clockwise.">
                  <button
                    className="compositor-rotate-btn"
                    onClick={() => setRotation(r => r - 90)}
                  >↺</button>
                </DelayedHint>
                <div className={`compositor-preview-frame${Math.abs(rotation) % 180 !== 0 ? ' compositor-preview-frame--sideways' : ''}`}>
                  <img
                    src={preview}
                    alt="Stitched preview"
                    className="compositor-preview"
                    style={{ transform: `rotate(${rotation}deg)` }}
                  />
                </div>
                <DelayedHint hint="Rotate 90° clockwise.">
                  <button
                    className="compositor-rotate-btn"
                    onClick={() => setRotation(r => r + 90)}
                  >↻</button>
                </DelayedHint>
              </div>
              {resultDims && (() => {
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
            <DelayedHint hint="Align and blend all queued images into a single stitched output.">
              <button
                className="primary"
                onClick={handleStitch}
                disabled={!canStitch}
              >
                {stitching ? 'Stitching…' : 'Stitch images'}
              </button>
            </DelayedHint>
            <DelayedHint hint="Load the stitched result as the active image for cropping and adjustment.">
              <button
                className="load-btn"
                onClick={handleLoadForCropping}
                disabled={!hasResult || stitching || loading}
              >
                Load output
              </button>
            </DelayedHint>
            <button className="options-btn" onClick={onClose}>Close</button>
          </div>
        </div>
      </div>
    </div>
  )
}
