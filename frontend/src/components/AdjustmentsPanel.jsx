import React, { useEffect, useState } from 'react'
import { AutoContrast, SetLevels, TrimBorders, ResizeImage, Descreen, DustRemoval } from '../../wailsjs/go/main/App'
import DelayedHint from './DelayedHint'
import ResizeModal from './ResizeModal'
import { adjustmentSelectionPayload } from '../utils/adjustmentSelection'

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
  useDescreenTool,
  setUseDescreenTool,
  brushSize,
  setBrushSize,
  mode,
  discActive,
  useStraightEdgeTool,
  setUseStraightEdgeTool,
  realImageDims,
  setRealImageDims,
  loading,
  imageMeta,
  showStatus,
  setErrorMessage,
  setUnsavedChanges,
  adjustmentSelectionActive,
  setAdjustmentSelectionActive,
  adjustmentRect,
  setAdjustmentRect,
  clearTouchup,
}) {
  const selection = adjustmentSelectionPayload(adjustmentRect)

  const handleDescreenReset = (result) => {
    if (result?.descreenReset) {
      setUseDescreenTool(false)
    }
  }

  const applyTrimBorders = async () => {
    if (!imageLoaded) return
    setLoading(true)
    try {
      const result = await TrimBorders()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      handleDescreenReset(result)
      setAdjustmentRect?.(null)
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
      const result = await AutoContrast({ selection })
      if (result?.preview) setPreview(result.preview)
      // AutoContrast returns a message like "Auto Contrast applied (black=12, white=243)".
      if (typeof result?.black === 'number' && typeof result?.white === 'number') {
        setBlackPoint(result.black)
        setWhitePoint(result.white)
      }
      handleDescreenReset(result)
      setUnsavedChanges?.(true)
    } catch (err) {
      console.error('AutoContrast error:', err)
    } finally {
      setAutoContrastPending(false)
      setLoading(false)
    }
  }

  const [resizeModalOpen, setResizeModalOpen] = useState(false)

  const [descreenThresh, setDescreenThresh] = useState(92)
  const [descreenRadius, setDescreenRadius] = useState(6)
  const [descreenMiddle, setDescreenMiddle] = useState(4)
  const [descreenHighlight, setDescreenHighlight] = useState(0)
  const [descreenPending, setDescreenPending] = useState(false)

  const [useDustRemovalTool, setUseDustRemovalTool] = useState(false)
  const [dustLevel, setDustLevel] = useState('medium')
  const [dustPending, setDustPending] = useState(false)

  useEffect(() => {
    if (!postCropAvailable) setUseDustRemovalTool(false)
  }, [postCropAvailable])

  useEffect(() => {
    if (postCropAvailable) return
    setAdjustmentSelectionActive?.(false)
    setAdjustmentRect?.(null)
  }, [postCropAvailable, setAdjustmentRect, setAdjustmentSelectionActive])

  const applyDustRemoval = async () => {
    if (!imageLoaded || !postCropAvailable || dustPending) return
    const dpiX = Number(imageMeta?.dpiX) || 0
    const dpiY = Number(imageMeta?.dpiY) || 0
    const dpi = dpiX > 0 && dpiY > 0 ? (dpiX + dpiY) / 2 : Math.max(dpiX, dpiY)
    setDustPending(true)
    setLoading(true)
    showStatus?.(`Removing dust (${dustLevel})…`)
    try {
      const result = await DustRemoval({ level: dustLevel, dpi, selection })
      if (result?.preview) setPreview(result.preview)
      handleDescreenReset(result)
      if (result?.changed) setUnsavedChanges?.(true)
      showStatus?.(result?.message || 'Dust removal complete')
    } catch (err) {
      console.error('DustRemoval error:', err)
      setErrorMessage?.(err?.message || String(err))
    } finally {
      setDustPending(false)
      setLoading(false)
    }
  }

  const applyDescreen = async () => {
    if (!imageLoaded) return
    setDescreenPending(true)
    setLoading(true)
    try {
      const result = await Descreen({ thresh: descreenThresh, radius: descreenRadius, middle: descreenMiddle, highlight: descreenHighlight, selection })
      if (result?.preview) setPreview(result.preview)
      handleDescreenReset(result)
      setUseDescreenTool(false)
      setUnsavedChanges?.(true)
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
      handleDescreenReset(result)
      setAdjustmentRect?.(null)
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
      const result = await SetLevels({ black: bp, white: wp, selection })
      if (result?.preview) setPreview(result.preview)
      handleDescreenReset(result)
      setUnsavedChanges?.(true)
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
        <span>Adjustments</span>
        <span className="adj-panel-header-actions">
          {adjPanelOpen && (
            <DelayedHint hint="Select a rectangular area for adjustments. Ctrl+D clears the selection.">
              <button
                type="button"
                className={`adjustment-selection-btn ${adjustmentSelectionActive ? 'active' : ''}`}
                aria-label="Adjustment selection"
                aria-pressed={adjustmentSelectionActive}
                disabled={!imageLoaded || !postCropAvailable || loading}
                onClick={(event) => {
                  event.stopPropagation()
                  if (adjustmentSelectionActive) {
                    setAdjustmentSelectionActive?.(false)
                    setAdjustmentRect?.(null)
                    return
                  }
                  clearTouchup?.()
                  setUseTouchupTool(false)
                  setUseStraightEdgeTool(false)
                  setAdjustmentSelectionActive?.(true)
                }}
              >
                <span className="adjustment-selection-icon" aria-hidden="true" />
              </button>
            </DelayedHint>
          )}
          <span className="accordion-toggle">{adjPanelOpen ? '▾' : '▸'}</span>
        </span>
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

            <DelayedHint hint="Dust removal filter. Toggle to choose Low, Medium, or High strength, then apply.">
              <button
                className={`adjustments-btn ${useDustRemovalTool ? 'active' : ''}`}
                onClick={() => setUseDustRemovalTool((value) => !value)}
                disabled={!imageLoaded || !postCropAvailable || dustPending}
                aria-pressed={useDustRemovalTool}
              >
                Dust removal
              </button>
            </DelayedHint>

            <DelayedHint hint="Toggles the touch-up brush which uses a PatchMatch-style content-aware fill. Draw strokes on the preview to build a mask, then commit to fill. Hold Alt and right-drag horizontally on the preview to resize the brush; the brush outline and pixel-size readout update live.">
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

            {mode === 'disc' && (
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
            )}
          </div>

          {postCropAvailable && (
            <div className={`touchup-slider dust-removal-controls ${useDustRemovalTool ? 'open' : 'closed'}`}>
              <div className="dust-removal-row">
                <select
                  className="dust-removal-select"
                  aria-label="Dust removal strength"
                  value={dustLevel}
                  onChange={(event) => setDustLevel(event.target.value)}
                  disabled={loading || dustPending}
                >
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                </select>
                <DelayedHint hint="Removes dust from the image - higher strength repairs larger defects. Uses the scan DPI when available, or 300 DPI otherwise.">
                  <button
                    className="adjustments-btn dust-removal-apply"
                    onClick={applyDustRemoval}
                    disabled={loading || dustPending || !imageLoaded}
                  >
                    {dustPending ? 'Removing dust…' : 'Apply'}
                  </button>
                </DelayedHint>
              </div>
              <div style={{ marginTop: '10px' }}></div>
            </div>
          )}

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
            <DelayedHint hint="Highlight restoration. At minimum (0) the descreened result is used as-is. Increasing this blends original highlights back over bright areas to hide any screen-pattern artifact left in near-white regions.">
              <div className="shortcut-item level-row">
                <label className="level-label">Highs</label>
                <input className="level-range" type="range" min="0" max="100" value={descreenHighlight} onChange={(e) => setDescreenHighlight(Number(e.target.value))} />
                <span className="level-value">{descreenHighlight}</span>
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
            <div style={{ marginTop: '10px' }}></div>
          </div>

          <div className={`touchup-slider ${useTouchupTool ? 'open' : 'closed'}`}>
            <div className="shortcut-item level-row">
              <label className="level-label">Radius</label>
              <input className="level-range" type="range" min="4" max="200" value={brushSize} onChange={e => setBrushSize(Number(e.target.value))} />
              <span className="level-value">{brushSize}px</span>
            </div>
            <div style={{ marginTop: '20px' }}></div>
          </div>

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
