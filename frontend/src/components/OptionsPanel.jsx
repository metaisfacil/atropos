import { useEffect, useRef, useState } from 'react'
import DelayedHint from './DelayedHint'

const FADE_MS = 150

// OptionsPanel renders a modal dialog for configuring application options.
// Currently exposes touch-up backend selection (PatchMatch vs IOPaint).
// Props:
//   open / onClose
//   touchupBackend / setTouchupBackend  ('patchmatch' | 'iopaint')
//   iopaintURL / setIopaintURL
export default function OptionsPanel({
  open,
  onClose,
  touchupBackend,
  setTouchupBackend,
  iopaintURL,
  setIopaintURL,
}) {
  const dialogRef = useRef(null)
  const [mounted, setMounted] = useState(false)
  const [shown, setShown]     = useState(false)
  const fadeOutTimer = useRef(null)

  useEffect(() => {
    if (open) {
      clearTimeout(fadeOutTimer.current)
      setMounted(true)
      // one frame delay so the browser registers the initial opacity:0 before transitioning
      requestAnimationFrame(() => requestAnimationFrame(() => setShown(true)))
    } else {
      setShown(false)
      fadeOutTimer.current = setTimeout(() => setMounted(false), FADE_MS)
    }
    return () => clearTimeout(fadeOutTimer.current)
  }, [open])

  // Close on Escape key
  useEffect(() => {
    if (!open) return
    const handler = (e) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, onClose])

  if (!mounted) return null

  return (
    <div className={`options-backdrop ${shown ? 'visible' : ''}`} onClick={onClose}>
      <div
        ref={dialogRef}
        className="options-dialog"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="Options"
      >
        <div className="options-header">
          <span className="options-title">Options</span>
          <button className="options-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        <div className="options-body">
          <DelayedHint hint="The touch-up brush lets you paint over blemishes or unwanted areas. The backend controls how the masked region is filled in.">
            <div className="options-section-title" tabIndex={0}>Touch-up backend</div>
          </DelayedHint>

          <DelayedHint hint="PatchMatch is a built-in content-aware fill. It samples nearby patches to reconstruct the masked area entirely on your CPU, no server required.">
            <label className="options-radio-label">
              <input
                type="radio"
                name="touchup-backend"
                value="patchmatch"
                checked={touchupBackend === 'patchmatch'}
                onChange={() => setTouchupBackend('patchmatch')}
              />
              PatchMatch <span className="options-hint">(built-in, works offline)</span>
            </label>
          </DelayedHint>

          <DelayedHint hint="IOPaint (formerly lama-cleaner) is an AI inpainting server you run locally. It can produce much higher quality touch-ups than conventional algorithms by leveraging deep-learning models.">
            <label className="options-radio-label">
              <input
                type="radio"
                name="touchup-backend"
                value="iopaint"
                checked={touchupBackend === 'iopaint'}
                onChange={() => setTouchupBackend('iopaint')}
              />
              IOPaint <span className="options-hint">(AI inpainting via local server)</span>
            </label>
          </DelayedHint>

          <div className={`options-iopaint-url ${touchupBackend === 'iopaint' ? 'visible' : ''}`}>
            <label className="options-field-label" htmlFor="iopaint-url">IOPaint endpoint URL</label>
            <DelayedHint hint="Base URL of your running IOPaint server, e.g. by default, the app will make requests to /api/v1/inpaint at http://127.0.0.1:8086/.">
              <input
                id="iopaint-url"
                className="options-text-input"
                type="text"
                value={iopaintURL}
                onChange={(e) => setIopaintURL(e.target.value)}
                placeholder="http://127.0.0.1:8086/"
                spellCheck={false}
              />
            </DelayedHint>
          </div>
        </div>

        <div className="options-footer">
          <button className="options-ok-btn" onClick={onClose}>OK</button>
        </div>
      </div>
    </div>
  )
}
