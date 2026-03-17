import { useEffect, useRef, useState } from 'react'

const FADE_MS = 150

// ErrorModal renders a modal dialog displaying an error message.
// Props:
//   message   – string to display (falsy = closed)
//   onClose   – called when the user dismisses
export default function ErrorModal({ message, onClose }) {
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
    const handler = (e) => { if (e.key === 'Escape' || e.key === 'Enter') onClose() }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, onClose])

  if (!mounted) return null

  return (
    <div className={`options-backdrop ${shown ? 'visible' : ''}`} onClick={onClose}>
      <div
        className="options-dialog error-dialog"
        onClick={(e) => e.stopPropagation()}
        role="alertdialog"
        aria-modal="true"
        aria-label="Error"
      >
        <div className="options-header">
          <span className="options-title error-title">Error</span>
          <button className="options-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        <div className="options-body">
          <p className="error-message">{message}</p>
        </div>

        <div className="options-footer">
          <button className="options-ok-btn" onClick={onClose}>OK</button>
        </div>
      </div>
    </div>
  )
}
