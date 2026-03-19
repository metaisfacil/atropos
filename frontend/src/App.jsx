import React, { useState, useRef } from 'react'
import './App.css'

import NormalCropPanel  from './components/NormalCropPanel'
import CornerPanel      from './components/CornerPanel'
import DiscPanel        from './components/DiscPanel'
import LinePanel        from './components/LinePanel'
import AdjustmentsPanel from './components/AdjustmentsPanel'
import ShortcutsPanel   from './components/ShortcutsPanel'
import OptionsPanel     from './components/OptionsPanel'
import ErrorModal       from './components/ErrorModal'
import ConfirmationModal from './components/ConfirmationModal'
import DelayedHint      from './components/DelayedHint'
import ImageOverlays    from './components/ImageOverlays'
import StatusBar        from './components/StatusBar'

import { useStatusMessage }      from './hooks/useStatusMessage'
import { usePersistentSettings } from './hooks/usePersistentSettings'
import { useZoomPan }            from './hooks/useZoomPan'
import { useTouchup }            from './hooks/useTouchup'
import { useImageActions }       from './hooks/useImageActions'
import { useMouseHandlers }      from './hooks/useMouseHandlers'
import { useKeyboardShortcuts }  from './hooks/useKeyboardShortcuts'

export default function App() {
  // ── Shared state ──────────────────────────────────────────────────────────
  const [mode, setMode]             = useState('corner')
  const [preview, setPreview]       = useState(null)
  const [imageLoaded, setImageLoaded] = useState(false)
  const [loading, setLoading]       = useState(false)
  const [errorMessage, setErrorMessage] = useState(null)
  const showError = (err) => setErrorMessage(err?.message || String(err))
  const [confirmDialog, setConfirmDialog] = useState(null)

  const [realImageDims, setRealImageDims] = useState({ w: 1, h: 1 })
  const [imageMeta, setImageMeta] = useState({ format: '', dpiX: 0, dpiY: 0 })
  const imgRef     = useRef(null)
  const ctrlDragRef  = useRef(null)
  const shiftDragRef = useRef(null)

  // ── Drag / interaction state ───────────────────────────────────────────────
  const [dragging, setDragging]       = useState(false)
  const [dragStart, setDragStart]     = useState(null)
  const [dragCurrent, setDragCurrent] = useState(null)
  const [lines, setLines]             = useState([])

  // ── Corner mode ───────────────────────────────────────────────────────────
  const [cornerState, setCornerState] = useState({
    maxCorners: 500, qualityLevel: 1, minDistance: 100, accent: 20, cornerCount: 0,
  })
  const [dotRadius, setDotRadius]         = useState(20)
  const [customCorner, setCustomCorner]   = useState(false)
  const [cornersDetected, setCornersDetected] = useState(false)
  const [detectedCornerPts, setDetectedCornerPts] = useState([])
  const [selectedCornerPts, setSelectedCornerPts] = useState([])
  const [cropSkipped, setCropSkipped]     = useState(false)

  // ── Disc mode ─────────────────────────────────────────────────────────────
  const [featherSize, setFeatherSize] = useState(15)
  const [discActive, setDiscActive]   = useState(false)

  // ── Line mode ─────────────────────────────────────────────────────────────
  const [linesDone, setLinesDone]         = useState(0)
  const [linesProcessed, setLinesProcessed] = useState(false)

  // ── Normal crop mode ───────────────────────────────────────────────────────
  const [normalRect, setNormalRect]               = useState(null)
  const [normalCropApplied, setNormalCropApplied] = useState(false)

  // ── UI state ──────────────────────────────────────────────────────────────
  const [shortcutsOpen, setShortcutsOpen] = useState(false)
  const [adjPanelOpen,  setAdjPanelOpen]  = useState(false)
  const [autoContrastPending, setAutoContrastPending] = useState(false)
  const [blackPoint, setBlackPoint] = useState(0)
  const [whitePoint, setWhitePoint] = useState(255)
  const [useStretchPreprocess, setUseStretchPreprocess] = useState(true)
  const [useTouchupTool, setUseTouchupTool] = useState(false)
  const [useStraightEdgeTool, setUseStraightEdgeTool] = useState(false)
  const [optionsOpen, setOptionsOpen] = useState(false)

  // ── Hooks ─────────────────────────────────────────────────────────────────
  const { imageInfo, imageInfoVisible, showStatus } = useStatusMessage()

  const {
    touchupBackend, setTouchupBackend,
    iopaintURL, setIopaintURL,
    warpFillMode, setWarpFillMode,
    warpFillColor, setWarpFillColor,
    discCenterCutout, setDiscCenterCutout,
    discCutoutPercent, setDiscCutoutPercent,
    closeAfterSave, setCloseAfterSave,
  } = usePersistentSettings({ setPreview })

  const {
    zoom, setZoom,
    fitWidth, setFitWidth,
    spacePanMode,
    canvasRef,
    mousePosRef, spaceDownRef, panDragRef,
    lastResizeRef,
    handleImgLoad,
    setImgNatural,
  } = useZoomPan({ imgRef, mode, discActive, featherSize, setFeatherSize, setPreview })

  const {
    touchupStrokes, setTouchupStrokes,
    brushSize, setBrushSize,
    touchupDraggingRef,
    clearTouchup, commitTouchup,
  } = useTouchup({
    imageLoaded, loading, setLoading, showStatus,
    realImageDims, touchupBackend,
    setErrorMessage, setPreview,
    onDragEnd: () => { setDragging(false); setDragStart(null); setDragCurrent(null) },
  })

  const {
    loadingFull,
    saving,
    handleLoadImage,
    handleDetectCorners,
    handleSkipCrop,
    handleRecrop,
    handleResetCorners,
    handleResetDisc,
    handleResetNormal,
    handleNormalCrop,
    handleClearLines,
    handleSaveImage,
    handleModeSwitch,
  } = useImageActions({
    mode, loading, imageLoaded, discActive,
    cornerState, dotRadius, useStretchPreprocess, normalRect, closeAfterSave,
    setMode, setPreview, setLoading, setImageLoaded, setRealImageDims, setImgNatural,
    setZoom, setFitWidth, setCornerState, setLinesDone, setLinesProcessed,
    setDiscActive, setNormalRect, setNormalCropApplied, setCropSkipped, setCornersDetected,
    setDetectedCornerPts, setSelectedCornerPts, setLines, setBlackPoint, setWhitePoint,
    setUseTouchupTool, setUseStraightEdgeTool, setDragging, setDragStart, setDragCurrent,
    setConfirmDialog, setTouchupStrokes,
    touchupDraggingRef, canvasRef,
    showStatus, showError,
    setImageMeta,
  })

  const {
    handleMouseDown, handleMouseMove, handleMouseUp, handleImageMouseLeave,
    displayToImage,
  } = useMouseHandlers({
    imageLoaded, loading, mode, dragging, dragStart,
    useTouchupTool, useStraightEdgeTool, discActive, linesProcessed,
    touchupStrokes, cornerState, dotRadius, cornersDetected, customCorner, linesDone,
    realImageDims,
    setDragging, setDragStart, setDragCurrent, setTouchupStrokes, setPreview,
    setLoading, setZoom, setRealImageDims, setCornerState, setDetectedCornerPts,
    setSelectedCornerPts, setDiscActive, setNormalRect, setLines, setLinesDone,
    setLinesProcessed, setUseStraightEdgeTool,
    spaceDownRef, panDragRef, canvasRef, ctrlDragRef, shiftDragRef,
    touchupDraggingRef, imgRef, lastResizeRef, mousePosRef,
    commitTouchup, showStatus, showError,
  })

  useKeyboardShortcuts({
    imageLoaded, mode, discActive, featherSize,
    ctrlDragRef, shiftDragRef, mousePosRef,
    setPreview, setFeatherSize, setRealImageDims, setLoading,
    displayToImage, showStatus, showError, handleSaveImage,
    canSave: imageLoaded && (cropSkipped || normalCropApplied || linesProcessed || cornerState.cornerCount >= 4 || discActive),
    normalRect, handleNormalCrop,
  })

  // ── Render ─────────────────────────────────────────────────────────────────
  return (
    <div className="app">
      {loadingFull && (
        <div className="loading-overlay opaque">
          <div className="spinner" />
          <div className="loading-text">{imageInfo}</div>
        </div>
      )}

      <aside className="sidebar">
        {/* Mode selector */}
        <div className="mode-selector">
          {['corner', 'disc', 'line', 'normal'].map(m => (
            <DelayedHint key={m} hint={`Switch to ${m.charAt(0).toUpperCase() + m.slice(1)} mode`}>
              <button
                className={`mode-btn ${mode === m ? 'active' : ''}`}
                onClick={() => handleModeSwitch(m)}
              >
                {m.charAt(0).toUpperCase() + m.slice(1)}
              </button>
            </DelayedHint>
          ))}
        </div>

        <div className="sidebar-scroll">
          <div className="sidebar-scroll-inner">
            {/* Mode-specific panels: always rendered, toggled via `.active` for smooth fades */}
            <div className={`mode-panel ${mode === 'corner' ? 'active' : ''}`}>
              <CornerPanel
                state={cornerState}        setState={setCornerState}
                dotRadius={dotRadius}      setDotRadius={setDotRadius}
                customCorner={customCorner} setCustomCorner={setCustomCorner}
                disabled={cropSkipped}
              />
            </div>

            <div className={`mode-panel ${mode === 'disc' ? 'active' : ''}`}>
              <DiscPanel
                discActive={discActive}
                featherSize={featherSize}  setFeatherSize={setFeatherSize}
                discCenterCutout={discCenterCutout}
                discCutoutPercent={discCutoutPercent}
                setDiscCutoutPercent={setDiscCutoutPercent}
                setPreview={setPreview}
                disabled={cropSkipped}
              />
            </div>

            <div className={`mode-panel ${mode === 'line' ? 'active' : ''}`}>
              <LinePanel linesDone={linesDone} />
            </div>

            <div className={`mode-panel ${mode === 'normal' ? 'active' : ''}`}>
              <NormalCropPanel normalRect={normalRect} />
            </div>
          </div>
        </div>

        {/* Bottom section: actions, adjustments, shortcuts, file ops */}
        <div className="sidebar-bottom">
          <div className="sidebar-actions">
            {mode === 'corner' && (
              <DelayedHint hint="Run corner detection, then click 4 corners to apply the perspective crop.">
                <button className="primary" onClick={handleDetectCorners} disabled={!imageLoaded || loading || cropSkipped}>
                  Detect
                </button>
              </DelayedHint>
            )}
            {mode === 'normal' && (
              <DelayedHint hint="Apply a drawn rectangle as a crop to the image. You can also press Enter.">
                <button className="primary" onClick={handleNormalCrop} disabled={!imageLoaded || loading || !normalRect}>
                  Crop
                </button>
              </DelayedHint>
            )}
            {((mode === 'corner' && cornerState.cornerCount < 4) ||
              (mode === 'disc'   && !discActive) ||
              (mode === 'line'   && !linesProcessed) ||
              (mode === 'normal' && !normalCropApplied)) && (
              <DelayedHint hint="Skip the cropping step and proceed to adjustments/touch-up. (You can re-crop later.)">
                <button className="skip-crop-btn" onClick={handleSkipCrop} disabled={!imageLoaded || loading}>
                  Skip crop
                </button>
              </DelayedHint>
            )}
            {((mode === 'corner' && cornerState.cornerCount > 0) ||
              (mode === 'disc'   && discActive) ||
              (mode === 'line'   && (linesDone > 0 || linesProcessed)) ||
              (mode === 'normal' && (normalRect !== null || normalCropApplied))) && (
              <div style={{ display: 'flex', gap: '10px' }}>
                {((mode === 'corner' && cornerState.cornerCount === 4) ||
                  (mode === 'disc'   && discActive) ||
                  (mode === 'line'   && linesProcessed) ||
                  (mode === 'normal' && normalCropApplied)) && (
                  <DelayedHint hint="Promote the current output to be the new source image and restart cropping.">
                    <button className="recrop-btn" onClick={handleRecrop} disabled={!imageLoaded || loading}>
                      Re-crop
                    </button>
                  </DelayedHint>
                )}
                <DelayedHint hint="Reset this mode's crop/selection and clear the current warp result.">
                  <button
                    className="reset-btn-danger"
                    disabled={loading}
                    onClick={
                      mode === 'corner' ? handleResetCorners :
                      mode === 'disc'   ? handleResetDisc    :
                      mode === 'normal' ? handleResetNormal  :
                                          handleClearLines
                    }
                  >
                    Reset{mode === 'corner' ? ` (${cornerState.cornerCount}/4)` : ''}
                  </button>
                </DelayedHint>
              </div>
            )}
          </div>

          <AdjustmentsPanel
            adjPanelOpen={adjPanelOpen}           setAdjPanelOpen={setAdjPanelOpen}
            autoContrastPending={autoContrastPending} setAutoContrastPending={setAutoContrastPending}
            blackPoint={blackPoint}               setBlackPoint={setBlackPoint}
            whitePoint={whitePoint}               setWhitePoint={setWhitePoint}
            imageLoaded={imageLoaded}
            setLoading={setLoading}
            setPreview={setPreview}
            useStretchPreprocess={useStretchPreprocess}
            setUseStretchPreprocess={setUseStretchPreprocess}
            postCropAvailable={
              (mode === 'corner' && cornerState.cornerCount === 4) ||
              (mode === 'line'   && linesProcessed) ||
              (mode === 'disc'   && discActive) ||
              (mode === 'normal' && normalCropApplied)
            }
            useTouchupTool={useTouchupTool}
            setUseTouchupTool={setUseTouchupTool}
            touchupStrokes={touchupStrokes}
            commitTouchup={commitTouchup}
            clearTouchup={clearTouchup}
            brushSize={brushSize}
            setBrushSize={setBrushSize}
            mode={mode}
            discActive={discActive}
            useStraightEdgeTool={useStraightEdgeTool}
            setUseStraightEdgeTool={setUseStraightEdgeTool}
          />

          <ShortcutsPanel
            shortcutsOpen={shortcutsOpen} setShortcutsOpen={setShortcutsOpen}
            mode={mode}
            discActive={discActive}
            canSave={imageLoaded && (cropSkipped || normalCropApplied || linesProcessed || cornerState.cornerCount >= 4 || discActive)}
            imageLoaded={imageLoaded}
          />

          <div className="file-ops">
            <DelayedHint hint="Open a file dialog to select and load an image into the app.">
              <button onClick={handleLoadImage} className="load-btn" disabled={loading && !saving}>
                Load image
              </button>
            </DelayedHint>
            <DelayedHint hint="Save the currently cropped/adjusted image to disk.">
              <button onClick={handleSaveImage} className="save-btn" disabled={loading || !(imageLoaded && (cropSkipped || normalCropApplied || linesProcessed || cornerState.cornerCount >= 4 || discActive))}>
                Save image
              </button>
            </DelayedHint>
            <DelayedHint hint="Open application Options and settings.">
              <button className="options-btn" onClick={() => setOptionsOpen(true)}>
                Options
              </button>
            </DelayedHint>
          </div>
        </div>
      </aside>

      <ErrorModal message={errorMessage} onClose={() => setErrorMessage(null)} />
      <ConfirmationModal
        message={confirmDialog?.message}
        onConfirm={confirmDialog?.onConfirm ?? (() => {})}
        onCancel={() => setConfirmDialog(null)}
      />

      <OptionsPanel
        open={optionsOpen}
        onClose={() => setOptionsOpen(false)}
        touchupBackend={touchupBackend}
        setTouchupBackend={setTouchupBackend}
        iopaintURL={iopaintURL}
        setIopaintURL={setIopaintURL}
        warpFillMode={warpFillMode}
        setWarpFillMode={setWarpFillMode}
        warpFillColor={warpFillColor}
        setWarpFillColor={setWarpFillColor}
        discCenterCutout={discCenterCutout}
        setDiscCenterCutout={setDiscCenterCutout}
        closeAfterSave={closeAfterSave}
        setCloseAfterSave={setCloseAfterSave}
      />

      <main className="main-content">
        <header className="toolbar">
          {loading && <div className="header-spinner" />}
          <span className={imageInfoVisible ? 'toolbar-message' : 'toolbar-message toolbar-message--fading'}>{imageInfo}</span>
        </header>
        <div
          ref={canvasRef}
          className="canvas-area"
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
          style={spacePanMode ? { cursor: 'grab' } : undefined}
        >
          {preview ? (
            <div style={{ position: 'relative', display: 'inline-block', lineHeight: 0, margin: 'auto' }}>
              <img
                ref={imgRef}
                src={preview}
                alt="preview"
                draggable={false}
                onLoad={handleImgLoad}
                onMouseLeave={handleImageMouseLeave}
                style={{
                  cursor: spacePanMode ? 'grab' : 'crosshair',
                  display: 'block',
                  ...(fitWidth > 0
                    ? { width: `${fitWidth * zoom}px`, height: 'auto', maxWidth: 'none', maxHeight: 'none' }
                    : { maxWidth: `${zoom * 100}%`, maxHeight: `${zoom * 100}%` }),
                }}
              />
              <ImageOverlays
                realImageDims={realImageDims}
                mode={mode}
                dragging={dragging}
                dragStart={dragStart}
                dragCurrent={dragCurrent}
                useTouchupTool={useTouchupTool}
                touchupStrokes={touchupStrokes}
                brushSize={brushSize}
                useStraightEdgeTool={useStraightEdgeTool}
                discCenterCutout={discCenterCutout}
                discCutoutPercent={discCutoutPercent}
                ctrlDragRef={ctrlDragRef}
                shiftDragRef={shiftDragRef}
                detectedCornerPts={detectedCornerPts}
                selectedCornerPts={selectedCornerPts}
                dotRadius={dotRadius}
                normalRect={normalRect}
                lines={lines}
                displayToImage={displayToImage}
              />
            </div>
          ) : !loading ? (
            <div className="placeholder">Load or drop an image to begin</div>
          ) : null}
        </div>
        <StatusBar
          imageLoaded={imageLoaded}
          imageMeta={imageMeta}
          realImageDims={realImageDims}
          zoom={zoom}
          onResetZoom={() => setZoom(1)}
        />
      </main>
    </div>
  )
}
