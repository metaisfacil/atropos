import { useEffect, useRef, useState } from 'react'

const FADE_MS = 150

// ConfirmationModal renders a modal dialog asking the user to confirm an action.
// Props:
//   message   – string to display (falsy = closed)
//   onConfirm – called when the user confirms
//   onCancel  – called when the user cancels or dismisses
export default function ConfirmationModal({ message, onConfirm, onCancel }) {
  const [mounted, setMounted] = useState(false)
  const [shown, setShown]     = useState(false)
  const fadeOutTimer = useRef(null)
  const open = Boolean(message)

  useEffect(() => {
    if (open) {
      clearTimeout(fadeOutTimer.current)
      setMounted(true)
      requestAnimationFrame(() => requestAnimationFrame(() => setShown(true)))
    } else {
      setShown(false)
      fadeOutTimer.current = setTimeout(() => setMounted(false), FADE_MS)
    }
    return () => clearTimeout(fadeOutTimer.current)
  }, [open])

  useEffect(() => {
    if (!open) return
    const handler = (e) => {
      if (e.key === 'Escape') onCancel()
      if (e.key === 'Enter')  onConfirm()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, onConfirm, onCancel])

  if (!mounted) return null

  return (
    <div className={`options-backdrop ${shown ? 'visible' : ''}`} onClick={onCancel}>
      <div
        className="options-dialog"
        onClick={(e) => e.stopPropagation()}
        role="alertdialog"
        aria-modal="true"
        aria-label="Confirm"
      >
        <div className="options-header">
          <span className="options-title">Confirm</span>
          <button className="options-close" onClick={onCancel} aria-label="Close">✕</button>
        </div>

        <div className="options-body">
          <p className="error-message">{message}</p>
        </div>

        <div className="options-footer" style={{ gap: '10px' }}>
          <button className="options-ok-btn" style={{ background: '#555' }} onClick={onCancel}>Cancel</button>
          <button className="options-ok-btn recrop-btn" onClick={onConfirm}>Continue</button>
        </div>
      </div>
    </div>
  )
}
