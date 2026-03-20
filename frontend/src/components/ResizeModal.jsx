import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

const FADE_MS = 150

export default function ResizeModal({ open, initialWidth, initialHeight, onClose, onApply }) {
  const [mounted, setMounted] = useState(false)
  const [shown, setShown] = useState(false)
  const fadeOutTimer = useRef(null)

  const [width, setWidth] = useState(initialWidth)
  const [height, setHeight] = useState(initialHeight)
  const [percent, setPercent] = useState(100)
  const [lockAspect, setLockAspect] = useState(true)

  useEffect(() => {
    if (open) {
      clearTimeout(fadeOutTimer.current)
      setMounted(true)
      requestAnimationFrame(() => requestAnimationFrame(() => setShown(true)))
      setWidth(initialWidth)
      setHeight(initialHeight)
      setPercent(100)
      setLockAspect(true)
    } else {
      setShown(false)
      fadeOutTimer.current = setTimeout(() => setMounted(false), FADE_MS)
    }
    return () => clearTimeout(fadeOutTimer.current)
  }, [open, initialWidth, initialHeight])

  useEffect(() => {
    if (!open) return
    const handler = (e) => {
      if (e.key === 'Escape') onClose()
      if (e.key === 'Enter' && width > 0 && height > 0) onApply({ width, height })
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, onClose, onApply, width, height])

  const clampPositiveInt = (value) => {
    const parsed = Number(value)
    if (!Number.isFinite(parsed) || parsed <= 0) {
      return 1
    }
    return Math.max(1, Math.round(parsed))
  }

  const updatePercentFromSize = (w, h) => {
    if (initialWidth <= 0 || initialHeight <= 0) {
      setPercent(100)
      return
    }
    const pct = ((w / initialWidth) + (h / initialHeight)) / 2 * 100
    setPercent(Math.max(1, Math.round(pct)))
  }

  const onWidthInput = (value) => {
    const w = clampPositiveInt(value)
    let h = height
    if (lockAspect && initialWidth > 0) {
      h = Math.max(1, Math.round(w * initialHeight / initialWidth))
    }
    setWidth(w)
    setHeight(h)
    if (!lockAspect) {
      updatePercentFromSize(w, h)
    } else {
      setPercent(Math.max(1, Math.round(w / initialWidth * 100)))
    }
  }

  const onHeightInput = (value) => {
    const h = clampPositiveInt(value)
    let w = width
    if (lockAspect && initialHeight > 0) {
      w = Math.max(1, Math.round(h * initialWidth / initialHeight))
    }
    setHeight(h)
    setWidth(w)
    if (!lockAspect) {
      updatePercentFromSize(w, h)
    } else {
      setPercent(Math.max(1, Math.round(h / initialHeight * 100)))
    }
  }

  const onPercentInput = (value) => {
    const p = clampPositiveInt(value)
    const w = Math.max(1, Math.round(initialWidth * p / 100))
    const h = Math.max(1, Math.round(initialHeight * p / 100))
    setPercent(p)
    setWidth(w)
    setHeight(h)
  }

  if (!mounted) return null

  const modal = (
    <div className={`options-backdrop ${shown ? 'visible' : ''}`} onMouseDown={(e) => { if (e.target === e.currentTarget) onClose() }}>
      <div
        className="options-dialog resize-dialog"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Resize image"
      >
        <div className="options-header">
          <span className="options-title">Resize image</span>
          <button className="options-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        <div className="options-body">
          <div className="resize-dimensions-row">
            <div className="resize-field">
              <span className="options-field-label">Width</span>
              <input
                type="number"
                className="options-text-input"
                value={width}
                min="1"
                step="1"
                onChange={(e) => onWidthInput(e.target.value)}
              />
            </div>
            <span className="resize-dimensions-sep">×</span>
            <div className="resize-field">
              <span className="options-field-label">Height</span>
              <input
                type="number"
                className="options-text-input"
                value={height}
                min="1"
                step="1"
                onChange={(e) => onHeightInput(e.target.value)}
              />
            </div>
            <button
              type="button"
              className="resize-aspect-btn"
              onClick={() => setLockAspect((v) => !v)}
              aria-label={lockAspect ? 'Unlock aspect ratio' : 'Lock aspect ratio'}
            >
              {lockAspect ? '🔒' : '🔓'}
            </button>
          </div>

          <div>
            <span className="options-field-label">Scale (%)</span>
            <input
              type="number"
              className="options-text-input"
              value={percent}
              min="1"
              step="1"
              onChange={(e) => onPercentInput(e.target.value)}
            />
          </div>
        </div>

        <div className="options-footer" style={{ gap: '10px' }}>
          <button className="modal-cancel-btn" onClick={onClose}>Cancel</button>
          <button
            className="options-ok-btn recrop-btn"
            onClick={() => onApply({ width, height })}
            disabled={width <= 0 || height <= 0}
          >
            Apply
          </button>
        </div>
      </div>
    </div>
  )

  return createPortal(modal, document.body)
}
