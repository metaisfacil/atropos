import { useEffect, useRef, useState } from 'react'
import DelayedHint from './DelayedHint'

const FADE_MS = 150

// OptionsPanel renders a modal dialog for configuring application options.
// Props:
//   open / onClose
//   touchupBackend / setTouchupBackend  ('patchmatch' | 'iopaint')
//   iopaintURL / setIopaintURL
//   warpFillMode / setWarpFillMode  ('clamp' | 'fill' | 'outpaint')
//   warpFillColor / setWarpFillColor  (CSS hex string)
export default function OptionsPanel({
  open,
  onClose,
  touchupBackend,
  setTouchupBackend,
  iopaintURL,
  setIopaintURL,
  warpFillMode,
  setWarpFillMode,
  warpFillColor,
  setWarpFillColor,
  discCenterCutout,
  setDiscCenterCutout,
  autoCornerParams,
  setAutoCornerParams,
  closeAfterSave,
  setCloseAfterSave,
  postSaveEnabled,
  setPostSaveEnabled,
  postSaveCommand,
  setPostSaveCommand,
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

          <DelayedHint hint="PatchMatch is a built-in content-aware fill. It samples nearby patches to reconstruct the masked area entirely on your CPU, no server or internet connection required.">
            <label className="options-radio-label">
              <input
                type="radio"
                name="touchup-backend"
                value="patchmatch"
                checked={touchupBackend === 'patchmatch'}
                onChange={() => setTouchupBackend('patchmatch')}
              />
              PatchMatch <span className="options-hint">(default)</span>
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

          <div className="options-divider" />

          <DelayedHint hint="When the perspective crop extends beyond the scan boundary, this setting controls how those regions are handled.">
            <div className="options-section-title" tabIndex={0}>Out-of-bounds fill</div>
          </DelayedHint>

          <DelayedHint hint="Clamps source coordinates to the image boundary, repeating edge pixels into any out-of-bounds region. No region is ever left blank or transparent.">
            <label className="options-radio-label">
              <input
                type="radio"
                name="warp-fill-mode"
                value="clamp"
                checked={warpFillMode === 'clamp'}
                onChange={() => setWarpFillMode('clamp')}
              />
              Clamp to boundary <span className="options-hint">(default)</span>
            </label>
          </DelayedHint>

          <DelayedHint hint="Fills the out-of-bounds region with a solid colour. Use the colour picker to choose the fill colour (default: white).">
            <label className="options-radio-label">
              <input
                type="radio"
                name="warp-fill-mode"
                value="fill"
                checked={warpFillMode === 'fill'}
                onChange={() => setWarpFillMode('fill')}
              />
              Solid fill
            </label>
          </DelayedHint>

          <div className={`options-iopaint-url ${warpFillMode === 'fill' ? 'visible' : ''}`}>
            <label className="options-field-label" htmlFor="warp-fill-color">Fill colour</label>
            <DelayedHint hint="The colour used to fill regions outside the scan. White works well for most scanned documents.">
              <input
                id="warp-fill-color"
                className="options-color-input"
                type="color"
                value={warpFillColor}
                onChange={(e) => setWarpFillColor(e.target.value)}
              />
            </DelayedHint>
          </div>

          <DelayedHint hint="Outpaints using PatchMatch, synthesising plausible content from the surrounding scan rather than leaving a flat colour.">
            <label className="options-radio-label">
              <input
                type="radio"
                name="warp-fill-mode"
                value="outpaint"
                checked={warpFillMode === 'outpaint'}
                onChange={() => setWarpFillMode('outpaint')}
              />
              Outpaint <span className="options-hint">(built-in PatchMatch)</span>
            </label>
          </DelayedHint>

          <div className="options-divider" />

          <DelayedHint hint="Settings that apply when cropping in Disc mode.">
            <div className="options-section-title" tabIndex={0}>Disc mode</div>
          </DelayedHint>

          <DelayedHint hint="Punches a small centred hole (11% of the disc diameter) filled with the background colour, so the eyedropper can affect the spindle area in the middle of the disc.">
            <label className="options-radio-label">
              <input
                type="checkbox"
                checked={discCenterCutout}
                onChange={(e) => setDiscCenterCutout(e.target.checked)}
              />
              Center cutout <span className="options-hint">(default: on)</span>
            </label>
          </DelayedHint>

          <div className="options-divider" />

          <DelayedHint hint="Settings for the corner detection mode.">
            <div className="options-section-title" tabIndex={0}>Corner detection</div>
          </DelayedHint>

          <DelayedHint hint="When on, Min Distance and Max Corners are automatically set from image dimensions each time an image is loaded. You can still adjust them manually after loading.">
            <label className="options-radio-label">
              <input
                type="checkbox"
                checked={autoCornerParams}
                onChange={(e) => setAutoCornerParams(e.target.checked)}
              />
              Auto-adjust parameters on load <span className="options-hint">(default: on)</span>
            </label>
          </DelayedHint>

          <div className="options-divider" />

          <DelayedHint hint="Actions to perform automatically after a file is saved.">
            <div className="options-section-title" tabIndex={0}>Post-save actions</div>
          </DelayedHint>

          <DelayedHint hint="Run a program automatically after each save. Use {path} in the command as a placeholder for the saved image path.">
            <label className="options-radio-label">
              <input
                type="checkbox"
                checked={postSaveEnabled}
                onChange={(e) => setPostSaveEnabled(e.target.checked)}
              />
              Run after save <span className="options-hint">(default: off)</span>
            </label>
          </DelayedHint>

          <div className={`options-iopaint-url ${postSaveEnabled ? 'visible' : ''}`}>
            <label className="options-field-label" htmlFor="post-save-command">Command</label>
            <DelayedHint hint="Command to run after saving. Use {path} as a placeholder for the saved image path. The first token is the executable; the rest are arguments. Example: C:\tools\viewer.exe {path}">
              <input
                id="post-save-command"
                className="options-text-input"
                type="text"
                value={postSaveCommand}
                onChange={(e) => setPostSaveCommand(e.target.value)}
                placeholder={'e.g. C:\\tools\\viewer.exe {path}'}
                spellCheck={false}
              />
            </DelayedHint>
          </div>

          <DelayedHint hint="Automatically closes Atropos after a file is saved successfully. This will occur after any post-save command, if specified.">
            <label className="options-radio-label">
              <input
                type="checkbox"
                checked={closeAfterSave}
                onChange={(e) => setCloseAfterSave(e.target.checked)}
              />
              Close after save <span className="options-hint">(default: off)</span>
            </label>
          </DelayedHint>
        </div>

        <div className="options-footer">
          <button className="options-ok-btn" onClick={onClose}>OK</button>
        </div>
      </div>
    </div>
  )
}
