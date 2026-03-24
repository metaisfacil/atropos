import { useEffect, useRef, useState } from 'react'

const FADE_MS = 150

// ConfirmationModal renders a modal dialog asking the user to confirm an action.
// Props:
//   open       – bool to show the dialog
//   message    – string to display
//   onYes      – called when the user chooses 'Yes'
//   onNo       – called when the user chooses 'No'
//   onCancel   – called when the user cancels/dismisses
//   yesText    – label for Yes button (default: 'Yes')
//   noText     – label for No button (default: 'No')
//   cancelText – label for Cancel button (default: 'Cancel')
export default function ConfirmationModal({
  open,
  message,
  onYes,
  onNo,
  onCancel,
  yesText = 'Yes',
  noText = 'No',
  cancelText = 'Cancel',
  onConfirm,
}) {
  const actualOpen = open || Boolean(message)
  const onYesAction = onYes || onConfirm
  const onNoAction = onNo || onCancel
  const [mounted, setMounted] = useState(false)
  const [shown, setShown]     = useState(false)
  const fadeOutTimer = useRef(null)

  useEffect(() => {
    if (actualOpen) {
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
    if (!actualOpen) return
    const handler = (e) => {
      if (e.key === 'Escape') onCancel?.()
      if (e.key === 'Enter')  onYesAction?.()
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [actualOpen, onYesAction, onCancel])

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
          <button className="options-ok-btn recrop-btn" onClick={onYesAction} style={{ flex: 1 }}>{yesText}</button>
          <button className="options-ok-btn recrop-btn" onClick={onNoAction} style={{ flex: 1 }}>{noText}</button>
          <button className="modal-cancel-btn" onClick={onCancel} style={{ flex: 1 }}>{cancelText}</button>
        </div>
      </div>
    </div>
  )
}
