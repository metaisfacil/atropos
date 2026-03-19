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
  postCropAvailable,
  useTouchupTool,
  setUseTouchupTool,
  brushSize,
  setBrushSize,
  mode,
  discActive,
  useStraightEdgeTool,
  setUseStraightEdgeTool,
}) {
  const applyAutoContrast = async () => {
    if (!imageLoaded) return
    setAutoContrastPending(true)
    setLoading(true)
    try {
      const result = await AutoContrast()
      if (result?.preview) setPreview(result.preview)
      // AutoContrast returns a message like "Auto Contrast applied (black=12, white=243)".
      if (typeof result?.black === 'number' && typeof result?.white === 'number') {
        setBlackPoint(result.black)
        setWhitePoint(result.white)
      }
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
    <div className={`keyboard-shortcuts adj-panel ${adjPanelOpen ? 'expanded' : ''}`}>
      <div
        className="shortcut-title adj-panel-header"
        onClick={() => setAdjPanelOpen((o) => !o)}
        style={{ cursor: 'pointer', userSelect: 'none' }}
      >
        Adjustments <span className="shortcut-toggle">{adjPanelOpen ? '▾' : '▸'}</span>
      </div>

      <div className={`keyboard-shortcuts-content ${adjPanelOpen ? 'open' : 'closed'}`}>
          <div className="shortcut-item">
            <DelayedHint hint="Toggles the touch-up brush which uses a PatchMatch-style content-aware fill. Draw strokes on the preview to build a mask, then commit to fill.">
              <button
                className={`primary touchup-btn ${useTouchupTool ? 'active' : ''}`}
                onClick={() => {
                  if (!useTouchupTool) setUseStraightEdgeTool(false)
                  setUseTouchupTool(!useTouchupTool)
                }}
                disabled={!postCropAvailable}
                aria-pressed={useTouchupTool}
              >
                Touch-up brush
              </button>
            </DelayedHint>
          </div>

          <div className={`touchup-slider ${useTouchupTool ? 'open' : 'closed'}`}>
            <div className="shortcut-item level-row">
              <label className="level-label">Radius</label>
              <input className="level-range" type="range" min="4" max="200" value={brushSize} onChange={e => setBrushSize(Number(e.target.value))} />
              <span className="level-value">{brushSize}px</span>
            </div>
          </div>

          {mode === 'disc' && (
            <div className="shortcut-item">
              <DelayedHint hint="Draw a line along a known horizontal edge. The disc will be rotated so that edge becomes level. Available only after the disc has been cropped.">
                <button
                  className={`primary straight-edge-btn ${useStraightEdgeTool ? 'active' : ''}`}
                  onClick={() => {
                    if (!useStraightEdgeTool) setUseTouchupTool(false)
                    setUseStraightEdgeTool(!useStraightEdgeTool)
                  }}
                  disabled={!discActive || useTouchupTool}
                  aria-pressed={useStraightEdgeTool}
                >
                  Straight edge
                </button>
              </DelayedHint>
            </div>
          )}

          <div className="shortcut-item" style={{ position: 'relative' }}>
            <DelayedHint hint="Clamps the image's luminance around the brightest and darkest points to enhance contrast.">
              <button
                className="primary auto-contrast-btn"
                style={{ minWidth: 120 }}
                onClick={applyAutoContrast}
                disabled={autoContrastPending || !imageLoaded || loading || !postCropAvailable}
              >
                {autoContrastPending ? 'Auto-contrast…' : 'Auto-contrast'}
              </button>
            </DelayedHint>
          </div>

          <div className="shortcut-item">
            <label style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <input type="checkbox" checked={useStretchPreprocess} onChange={e => setUseStretchPreprocess(e.target.checked)} />
              <DelayedHint hint="Remaps 1%/99% luminance to full range before corner detection. This can improve outcomes on scans with dark backgrounds.">
                <span style={{ fontWeight: 500 }}>Pre-stretch contrast for detection</span>
              </DelayedHint>
            </label>
          </div>

          <div className="shortcut-item level-row">
            <label className="level-label">Black</label>
              <input
                className="level-range"
                type="range"
                min="0"
                max={whitePoint - 1}
                value={blackPoint}
                onChange={(e) => setBlackPoint(Number(e.target.value))}
                onMouseUp={(e) => applyLevels(Number(e.target.value), whitePoint)}
                disabled={!imageLoaded || !postCropAvailable}
              />
              <span className="level-value">{blackPoint}</span>
          </div>

          <div className="shortcut-item level-row">
            <label className="level-label">White</label>
              <input
                className="level-range"
                type="range"
                min={blackPoint + 1}
                max="255"
                value={whitePoint}
                onChange={(e) => setWhitePoint(Number(e.target.value))}
                onMouseUp={(e) => applyLevels(blackPoint, Number(e.target.value))}
                disabled={!imageLoaded || !postCropAvailable}
              />
              <span className="level-value">{whitePoint}</span>
          </div>
        </div>
    </div>
  )
}
