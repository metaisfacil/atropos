import React from 'react'
import { AutoContrast, SetLevels } from '../../wailsjs/go/main/App'
import DelayedHint from './DelayedHint'

// AdjustmentsPanel renders the collapsible Adjustments section at the bottom
// of the sidebar: Auto Contrast button + Black/White Point sliders.
// Props:
//   adjPanelOpen / setAdjPanelOpen
//   autoContrastPending / setAutoContrastPending
//   blackPoint / setBlackPoint
//   whitePoint / setWhitePoint
//   imageLoaded
//   loading / setLoading
//   setPreview
export default function AdjustmentsPanel({
  adjPanelOpen, setAdjPanelOpen,
  autoContrastPending, setAutoContrastPending,
  blackPoint, setBlackPoint,
  whitePoint, setWhitePoint,
  imageLoaded,
  loading, setLoading,
  setPreview,
  useStretchPreprocess,
  setUseStretchPreprocess,
}) {
  const applyAutoContrast = async () => {
    if (!imageLoaded) return
    setAutoContrastPending(true)
    setLoading(true)
    try {
      const result = await AutoContrast()
      if (result?.preview) setPreview(result.preview)
      setBlackPoint(0)
      setWhitePoint(255)
    } catch (err) {
      console.error('AutoContrast error:', err)
    } finally {
      setAutoContrastPending(false)
      setLoading(false)
    }
  }

  const applyLevels = async (bp, wp) => {
    if (!imageLoaded) return
    setLoading(true)
    try {
      const result = await SetLevels({ black: bp, white: wp })
      if (result?.preview) setPreview(result.preview)
    } catch (err) {
      console.error('SetLevels error:', err)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="keyboard-shortcuts adj-panel">
      <div
        className="shortcut-title adj-panel-header"
        onClick={() => setAdjPanelOpen((o) => !o)}
        style={{ cursor: 'pointer', userSelect: 'none' }}
      >
        Adjustments <span className="shortcut-toggle">{adjPanelOpen ? '▾' : '▸'}</span>
      </div>

      {adjPanelOpen && (
        <>
          <div className="shortcut-item" style={{ marginBottom: 10, position: 'relative' }}>
            <DelayedHint hint="Clamps the image's luminance around the brightest and darkest points to enhance contrast.">
              <button
                className="primary"
                style={{ minWidth: 120 }}
                onClick={applyAutoContrast}
                disabled={autoContrastPending || !imageLoaded || loading}
              >
                {autoContrastPending ? 'Auto Contrast…' : 'Auto Contrast'}
              </button>
            </DelayedHint>
          </div>

          <div className="shortcut-item" style={{ marginBottom: 10 }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <input type="checkbox" checked={useStretchPreprocess} onChange={e => setUseStretchPreprocess(e.target.checked)} />
              <DelayedHint hint="Remaps 1%/99% luminance to full range before corner detection. This can improve outcomes on scans with dark backgrounds.">
                <span style={{ fontWeight: 500 }}>Pre-stretch contrast for detection</span>
              </DelayedHint>
            </label>
          </div>

          <div className="shortcut-item" style={{ marginBottom: 10 }}>
            <label style={{ fontWeight: 500 }}>Black Point</label>
            <input
              type="range"
              min="0"
              max={whitePoint - 1}
              value={blackPoint}
              onChange={(e) => setBlackPoint(Number(e.target.value))}
              onMouseUp={(e) => applyLevels(Number(e.target.value), whitePoint)}
              style={{ width: 120, marginLeft: 8 }}
              disabled={!imageLoaded}
            />
            <span style={{ marginLeft: 8 }}>{blackPoint}</span>
          </div>

          <div className="shortcut-item" style={{ marginBottom: 10 }}>
            <label style={{ fontWeight: 500 }}>White Point</label>
            <input
              type="range"
              min={blackPoint + 1}
              max="255"
              value={whitePoint}
              onChange={(e) => setWhitePoint(Number(e.target.value))}
              onMouseUp={(e) => applyLevels(blackPoint, Number(e.target.value))}
              style={{ width: 120, marginLeft: 8 }}
              disabled={!imageLoaded}
            />
            <span style={{ marginLeft: 8 }}>{whitePoint}</span>
          </div>
        </>
      )}
    </div>
  )
}
