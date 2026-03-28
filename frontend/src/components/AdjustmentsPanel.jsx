import React, { useState } from 'react'
import { AutoContrast, SetLevels, TrimBorders, ResizeImage, Descreen } from '../../wailsjs/go/main/App'
import DelayedHint from './DelayedHint'
import ResizeModal from './ResizeModal'

// AdjustmentsPanel renders the collapsible Adjustments section at the bottom
// of the sidebar: Auto Contrast button + Black/White Point sliders.
// Props:
//   adjPanelOpen / setAdjPanelOpen
//   autoContrastPending / setAutoContrastPending
//   blackPoint / setBlackPoint
//   whitePoint / setWhitePoint
//   imageLoaded
//   setLoading
//   setPreview
export default function AdjustmentsPanel({
  adjPanelOpen, setAdjPanelOpen,
  autoContrastPending, setAutoContrastPending,
  blackPoint, setBlackPoint,
  whitePoint, setWhitePoint,
  imageLoaded,
  setLoading,
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
  realImageDims,
  setRealImageDims,
}) {
  const applyTrimBorders = async () => {
    if (!imageLoaded) return
    setLoading(true)
    try {
      const result = await TrimBorders()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
    } catch (err) {
      console.error('TrimBorders error:', err)
    } finally {
      setLoading(false)
    }
  }

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

  const [resizeModalOpen, setResizeModalOpen] = useState(false)

  const [useDescreenTool, setUseDescreenTool] = useState(false)
  const [descreenThresh, setDescreenThresh] = useState(92)
  const [descreenRadius, setDescreenRadius] = useState(6)
  const [descreenMiddle, setDescreenMiddle] = useState(4)
  const [descreenPending, setDescreenPending] = useState(false)

  const applyDescreen = async () => {
    if (!imageLoaded) return
    setDescreenPending(true)
    setLoading(true)
    try {
      const result = await Descreen({ thresh: descreenThresh, radius: descreenRadius, middle: descreenMiddle })
      if (result?.preview) setPreview(result.preview)
    } catch (err) {
      console.error('Descreen error:', err)
    } finally {
      setDescreenPending(false)
      setLoading(false)
    }
  }

  const applyResize = async (width, height) => {
    if (!imageLoaded) return
    setLoading(true)
    try {
      const result = await ResizeImage({ width, height })
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
    } catch (err) {
      console.error('ResizeImage error:', err)
    } finally {
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
    <div className={`accordion-panel adj-panel ${adjPanelOpen ? 'expanded' : ''}`}>
      <div
        className="accordion-title adj-panel-header"
        onClick={() => setAdjPanelOpen((o) => !o)}
        style={{ cursor: 'pointer', userSelect: 'none' }}
      >
        Adjustments <span className="accordion-toggle">{adjPanelOpen ? '▾' : '▸'}</span>
      </div>

      <div className="accordion-content-outer">
        <div className={`accordion-content ${adjPanelOpen ? 'open' : 'closed'}`}>
          <div className="adj-btn-grid">
            <DelayedHint hint="Resize the image by width/height or scale percentage.">
              <button
                className="adjustments-btn"
                onClick={() => setResizeModalOpen(true)}
                disabled={!imageLoaded || !postCropAvailable}
              >
                Resize image
              </button>
            </DelayedHint>

            <DelayedHint hint="Detects and removes solid white or black border strips from each edge of the image.">
              <button
                className="adjustments-btn"
                onClick={applyTrimBorders}
                disabled={!imageLoaded || !postCropAvailable}
              >
                Trim borders
              </button>
            </DelayedHint>

            <DelayedHint hint="Toggles the touch-up brush which uses a PatchMatch-style content-aware fill. Draw strokes on the preview to build a mask, then commit to fill.">
              <button
                className={`adjustments-btn touchup-btn ${useTouchupTool ? 'active' : ''}`}
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

            <DelayedHint hint="Clamps the image's luminance around the brightest and darkest points to enhance contrast.">
              <button
                className="adjustments-btn"
                onClick={applyAutoContrast}
                disabled={autoContrastPending || !imageLoaded || !postCropAvailable}
              >
                {autoContrastPending ? 'Auto-contrast…' : 'Auto-contrast'}
              </button>
            </DelayedHint>

            <DelayedHint hint="FFT-based halftone descreen filter. Removes dot/line screen patterns from scanned printed images. Toggle to reveal controls, then click Apply.">
              <button
                className={`adjustments-btn ${useDescreenTool ? 'active' : ''}`}
                onClick={() => setUseDescreenTool((v) => !v)}
                disabled={!imageLoaded || !postCropAvailable}
                aria-pressed={useDescreenTool}
              >
                Descreen
              </button>
            </DelayedHint>
          </div>

          <div className={`touchup-slider descreen-controls ${useDescreenTool ? 'open' : 'closed'}`}>
            <DelayedHint hint="Threshold for the distance-weighted log-magnitude spectrum. Higher values filter only the strongest screen patterns; lower values are more aggressive.">
              <div className="shortcut-item level-row">
                <label className="level-label">Thresh</label>
                <input className="level-range" type="range" min="50" max="150" value={descreenThresh} onChange={(e) => setDescreenThresh(Number(e.target.value))} />
                <span className="level-value">{descreenThresh}</span>
              </div>
            </DelayedHint>
            <DelayedHint hint="Radius used to dilate and blur the suppression mask around detected screen peaks. Larger values remove more of the surrounding frequency content.">
              <div className="shortcut-item level-row">
                <label className="level-label">Radius</label>
                <input className="level-range" type="range" min="1" max="20" value={descreenRadius} onChange={(e) => setDescreenRadius(Number(e.target.value))} />
                <span className="level-value">{descreenRadius}</span>
              </div>
            </DelayedHint>
            <DelayedHint hint="Controls the size of the protected DC region at the centre of the spectrum. Higher values preserve more low-frequency content and reduce blurring of broad tones.">
              <div className="shortcut-item level-row">
                <label className="level-label">Middle</label>
                <input className="level-range" type="range" min="1" max="10" value={descreenMiddle} onChange={(e) => setDescreenMiddle(Number(e.target.value))} />
                <span className="level-value">{descreenMiddle}</span>
              </div>
            </DelayedHint>
            <div className="shortcut-item">
              <button
                className="adjustments-btn"
                onClick={applyDescreen}
                disabled={descreenPending || !imageLoaded || !postCropAvailable}
                style={{ width: '100%' }}
              >
                {descreenPending ? 'Descreening…' : 'Apply descreen'}
              </button>
            </div>
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
                  className={`adjustments-btn straight-edge-btn ${useStraightEdgeTool ? 'active' : ''}`}
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

          <ResizeModal
            open={resizeModalOpen}
            initialWidth={realImageDims.w}
            initialHeight={realImageDims.h}
            onClose={() => setResizeModalOpen(false)}
            onApply={async ({ width, height }) => {
              setResizeModalOpen(false)
              await applyResize(width, height)
            }}
          />

          <div className="shortcut-item adj-prestretch">
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
    </div>
  )
}
