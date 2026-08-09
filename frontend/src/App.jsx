import React, { useState, useRef, useEffect } from 'react'
import './App.css'

import NormalCropPanel  from './components/NormalCropPanel'
import CornerPanel      from './components/CornerPanel'
import DiscPanel        from './components/DiscPanel'
import LinePanel        from './components/LinePanel'
import AdjustmentsPanel from './components/AdjustmentsPanel'
import ShortcutsPanel   from './components/ShortcutsPanel'
import OptionsModal     from './components/OptionsModal'
import ToolsPanel       from './components/ToolsPanel'
import CompositorModal  from './components/CompositorModal'
import AboutModal       from './components/AboutModal'
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
import { isPreviewPresentationPending, isPreviewVariant, previewAssetSession, usePresentedValue, useProgressivePreview } from './hooks/useProgressivePreview'

export default function App() {
  // ── Sidebar resize ────────────────────────────────────────────────────────
  const [sidebarWidth, setSidebarWidth] = useState(320)

  function onSidebarResizeStart(e) {
    e.preventDefault()
    const startX = e.clientX
    const startWidth = sidebarWidth

    function onMouseMove(e) {
      const newWidth = Math.min(600, Math.max(200, startWidth + e.clientX - startX))
      setSidebarWidth(newWidth)
    }
    function onMouseUp() {
      window.removeEventListener('mousemove', onMouseMove)
      window.removeEventListener('mouseup', onMouseUp)
    }
    window.addEventListener('mousemove', onMouseMove)
    window.addEventListener('mouseup', onMouseUp)
  }

  function onSidebarResizeReset() {
    setSidebarWidth(320)
  }

  // ── Shared state ──────────────────────────────────────────────────────────
  const [mode, setMode]             = useState('corner')
  const [preview, setPreview]       = useState(null)
  const [presentedPreview, setPresentedPreview] = useState(null)
  const [imageLoaded, setImageLoaded] = useState(false)
  const [loading, setLoading]       = useState(false)
  const [errorMessage, setErrorMessage] = useState(null)
  const showError = (err) => setErrorMessage(err?.message || String(err))
  const [confirmDialog, setConfirmDialog] = useState(null)

  // `realImageDims` is the current working/output image size (width, height)
  // as reported by the Go backend. It reflects committed or previewed
  // modifications (crops, warps, disc renders, trims) and is used for
  // coordinate mapping and overlays.
  const [realImageDims, setRealImageDims] = useState({ w: 1, h: 1 })

  // `inputImageDims` is the original source image dimensions as loaded
  // from disk (the input file). It remains the file's native size even if
  // `realImageDims` changes after edits. The UI shows both when they differ.
  const [inputImageDims, setInputImageDims] = useState({ w: 1, h: 1 })
  const [imageMeta, setImageMeta] = useState({ format: '', dpiX: 0, dpiY: 0 })
  const [unsavedChanges, setUnsavedChanges] = useState(false)
  const imgRef     = useRef(null)
  const ctrlDragRef  = useRef(null)
  const shiftDragRef = useRef(null)
  const touchupDraggingRef = useRef(false)
  const flushPendingSaveRef = useRef(null)

  // ── Drag / interaction state ───────────────────────────────────────────────
  const [dragging, setDragging]       = useState(false)
  const [dragStart, setDragStart]     = useState(null)
  const [dragCurrent, setDragCurrent] = useState(null)
  const [lines, setLines]             = useState([])

  // ── Corner mode ───────────────────────────────────────────────────────────
  const [cornerState, setCornerState] = useState({
    maxCorners: 500, qualityLevel: 1, minDistance: 100, accent: 20, cornerCount: 0,
  })
  const [dotRadius, setDotRadius]         = useState(5)
  const [customCorner, setCustomCorner]   = useState(false)
  const [cornersDetected, setCornersDetected] = useState(false)
  const [detectedCornerPts, setDetectedCornerPts] = useState([])
  const [selectedCornerPts, setSelectedCornerPts] = useState([])
  const [cropSkipped, setCropSkipped]     = useState(false)

  // ── Disc mode ─────────────────────────────────────────────────────────────
  const [featherSize, setFeatherSize] = useState(15)
  const [discActive, setDiscActive]   = useState(false)
  const [discNoMaskPreview, setDiscNoMaskPreview] = useState(null)
  const [discCenter, setDiscCenter] = useState(null)
  const [discRadius, setDiscRadius] = useState(0)
  const [discRotation, setDiscRotation] = useState(0)
  const [discBgColor, setDiscBgColor] = useState({ r: 255, g: 255, b: 255 })

  // Live drag preview state for disc translation/rotation
  const [discLiveActive, setDiscLiveActive] = useState(false)
  const [discLiveTransform, setDiscLiveTransform] = useState({ dx: 0, dy: 0, angle: 0 })

  // ── Line mode ─────────────────────────────────────────────────────────────
  const [linesDone, setLinesDone]         = useState(0)
  const [linesProcessed, setLinesProcessed] = useState(false)
  const [lineDragKind, setLineDragKind] = useState('none')

  // ── Normal crop mode ───────────────────────────────────────────────────────
  const [normalRect, setNormalRect]               = useState(null)
  const [normalCropApplied, setNormalCropApplied] = useState(false)
  const [normalDragKind, setNormalDragKind] = useState('none')

  // ── UI state ──────────────────────────────────────────────────────────────
  const compositorDropRef = useRef(null)

  const [shortcutsOpen, setShortcutsOpen] = useState(false)
  const [adjPanelOpen,  setAdjPanelOpen]  = useState(false)
  const [autoContrastPending, setAutoContrastPending] = useState(false)
  const [blackPoint, setBlackPoint] = useState(0)
  const [whitePoint, setWhitePoint] = useState(255)
  const [useStretchPreprocess, setUseStretchPreprocess] = useState(true)
  const [useTouchupTool, setUseTouchupTool] = useState(false)
  const [useDescreenTool, setUseDescreenTool] = useState(false)
  const [useStraightEdgeTool, setUseStraightEdgeTool] = useState(false)
  const [optionsOpen, setOptionsOpen]         = useState(false)
  const [aboutOpen,   setAboutOpen]           = useState(false)
  const [compositorOpen, setCompositorOpen]   = useState(false)
  const [toolsOpen, setToolsOpen]             = useState(false)

  // ── Refs ───────────────────────────────────────────────────────────────────
  const sidebarRef = useRef(null)

  // Synchronize CSS variable with open panel count so all panels shrink in sync
  // when a new one opens (avoids CSS :has() timing delays)
  useEffect(() => {
    if (!sidebarRef.current) return
    const openCount = (shortcutsOpen ? 1 : 0) + (adjPanelOpen ? 1 : 0) + (toolsOpen ? 1 : 0)
    const maxHeight = openCount === 0 ? '30vh' : openCount === 1 ? '30vh' : openCount === 2 ? '18vh' : '12vh'
    sidebarRef.current.style.setProperty('--keyboard-shortcuts-max-height', maxHeight)
  }, [shortcutsOpen, adjPanelOpen, toolsOpen])

  // ── Hooks ─────────────────────────────────────────────────────────────────
  const { imageInfo, imageInfoVisible, showStatus } = useStatusMessage()

  const {
    touchupBackend, setTouchupBackend,
    iopaintURL, setIopaintURL,
    warpFillMode, setWarpFillMode,
    warpFillColor, setWarpFillColor,
    discCenterCutout, setDiscCenterCutout,
    discCutoutPercent, setDiscCutoutPercent,
    autoCornerParams, setAutoCornerParams,
    closeAfterSave, setCloseAfterSave,
    postSaveEnabled, setPostSaveEnabled,
    postSaveCommand, setPostSaveCommand,
    touchupRemainsActive, setTouchupRemainsActive,
    straightEdgeRemainsActive, setStraightEdgeRemainsActive,
    autoDetectOnModeSwitch, setAutoDetectOnModeSwitch,
  } = usePersistentSettings({ setPreview })

  const activePreview = discLiveActive && discNoMaskPreview ? discNoMaskPreview : preview
  const displayPreview = useProgressivePreview(activePreview, loading || dragging)
  const previewPresentationPending = isPreviewPresentationPending(preview, presentedPreview)
  const previewSession = previewAssetSession(preview)
  const presentedSession = previewAssetSession(presentedPreview)
  const newImagePresentationPending = previewPresentationPending && previewSession !== presentedSession
  const busy = loading || previewPresentationPending

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

  const handleVisiblePreviewLoad = () => {
    handleImgLoad()
    if (isPreviewVariant(displayPreview, preview)) {
      setPresentedPreview(preview)
    }
  }

  const {
    touchupStrokes, setTouchupStrokes,
    brushSize, setBrushSize,
    clearTouchup, commitTouchup,
  } = useTouchup({
    imageLoaded, loading: busy, setLoading, showStatus,
    realImageDims, touchupBackend,
    setErrorMessage, setPreview,
    onDragEnd: () => { setDragging(false); setDragStart(null); setDragCurrent(null) },
    flushPendingSaveRef,
    touchupRemainsActive, setUseTouchupTool, setUseDescreenTool,
    setUnsavedChanges,
    touchupDraggingRef,
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
    flushPendingSave,
    handleModeSwitch,
    handleUndo,
    handleCompositorLoad,
  } = useImageActions({
    mode, loading: busy, imageLoaded, discActive, linesProcessed, normalCropApplied,
    cornerState, dotRadius, useStretchPreprocess, autoCornerParams, normalRect, closeAfterSave, setCloseAfterSave, postSaveEnabled, setPostSaveEnabled, postSaveCommand, setPostSaveCommand, autoDetectOnModeSwitch,
    setMode, setPreview, setLoading, setImageLoaded, setRealImageDims, setInputImageDims, setImgNatural,
    setZoom, setFitWidth, setCornerState, setLinesDone, setLinesProcessed,
    setDiscActive, setDiscNoMaskPreview, setDiscCenter, setDiscRadius, setDiscBgColor, setNormalRect, setNormalCropApplied, setCropSkipped, setCornersDetected,
    setDetectedCornerPts, setSelectedCornerPts, setLines, setBlackPoint, setWhitePoint,
    setUseTouchupTool, setUseStraightEdgeTool, setDragging, setDragStart, setDragCurrent,
    setConfirmDialog, setTouchupStrokes,
    touchupDraggingRef, canvasRef,
    showStatus, showError,
    setImageMeta,
    compositorDropRef,
    setDiscRotation,
    unsavedChanges, setUnsavedChanges,
  })
  flushPendingSaveRef.current = flushPendingSave

  const {
    handleMouseDown, handleMouseMove, handleMouseUp, handleImageMouseLeave, handleContextMenu, displayToImage, lineStartImgRef,
  } = useMouseHandlers({
    imageLoaded, loading: busy, mode, dragging, dragStart, dragCurrent,
    useTouchupTool, useStraightEdgeTool, discActive, linesProcessed, touchupStrokes,
    cornerState, dotRadius, cornersDetected, customCorner, linesDone, normalRect, lines,
    realImageDims, discNoMaskPreview, discCenter, discRadius, discRotation,
    setDragging, setDragStart, setDragCurrent, setTouchupStrokes, setPreview,
    setDiscRotation, setLoading, setZoom, setRealImageDims, setCornerState,
    setDetectedCornerPts, setSelectedCornerPts, setDiscActive, setDiscNoMaskPreview,
    setDiscCenter, setDiscRadius, setDiscBgColor, setNormalRect, setLines, setLinesDone,
    setDiscLiveActive, setDiscLiveTransform, setLinesProcessed, setUseStraightEdgeTool,
    setLineDragKind, straightEdgeRemainsActive, spaceDownRef, panDragRef, canvasRef, ctrlDragRef,
    shiftDragRef, touchupDraggingRef, imgRef, lastResizeRef, mousePosRef,
    commitTouchup, showStatus, showError, setUnsavedChanges, setNormalDragKind,
  })

  useKeyboardShortcuts({
    imageLoaded, mode, discActive, featherSize, discRotation,
    ctrlDragRef, shiftDragRef, mousePosRef,
    setPreview, setFeatherSize, setLoading, setRealImageDims,
    setDiscNoMaskPreview, setDiscCenter, setDiscRadius, setDiscBgColor, setDiscRotation,
    displayToImage, showStatus, showError, handleSaveImage, flushPendingSave, handleLoadImage,
    canSave: imageLoaded && (cropSkipped || normalCropApplied || linesProcessed || cornerState.cornerCount >= 4 || discActive),
    normalRect, handleNormalCrop, handleUndo,
    unsavedChanges, setUnsavedChanges,
    confirmClose: async () => {
      await window.go.main.App.ConfirmClose()
    },
    cornerState, setCornerState, setSelectedCornerPts,
  })

  // Image-result state is updated as soon as Go returns, but it must not be
  // rendered against the previous bitmap. Freeze the last presented canvas
  // metadata and status until the visible low-resolution image fires onLoad.
  const presentedVisual = usePresentedValue({
    imageInfo,
    imageInfoVisible,
    realImageDims,
    mode,
    dragging,
    dragStart,
    dragCurrent,
    useTouchupTool,
    touchupStrokes,
    brushSize,
    useStraightEdgeTool,
    discActive,
    discLiveActive,
    discCenter,
    discRadius,
    discBgColor,
    discCenterCutout,
    discCutoutPercent,
    detectedCornerPts,
    selectedCornerPts,
    dotRadius,
    normalRect,
    lines,
    lineDragKind,
  }, previewPresentationPending)

  React.useEffect(() => {
    const handleBeforeUnload = (e) => {
      if (!unsavedChanges) return
      e.preventDefault()
      e.returnValue = ''
      return ''
    }
    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => {
      window.removeEventListener('beforeunload', handleBeforeUnload)
    }
  }, [unsavedChanges])

  // ── Render ─────────────────────────────────────────────────────────────────
  return (
    <div className="app">
      {(loadingFull || newImagePresentationPending) && (
        <div className="loading-overlay opaque">
          <div className="spinner" />
          <div className="loading-text">{imageInfo}</div>
        </div>
      )}

      <aside ref={sidebarRef} className="sidebar" style={{ width: sidebarWidth }}>
        <div
          className="sidebar-resize-handle"
          onMouseDown={onSidebarResizeStart}
          onDoubleClick={onSidebarResizeReset}
        />
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
          <div className="sidebar-bottom-scroll">
            <div className="sidebar-actions">
              {mode === 'corner' && (
                <DelayedHint hint="Run corner detection, then click 4 corners to apply the perspective crop.">
                  <button className="primary mode-action-btn" onClick={handleDetectCorners} disabled={!imageLoaded || busy || cropSkipped}>
                    Detect
                  </button>
                </DelayedHint>
              )}
              {mode === 'normal' && (
                <DelayedHint hint="Apply a drawn rectangle as a crop to the image. You can also press Enter.">
                  <button className="primary mode-action-btn" onClick={handleNormalCrop} disabled={!imageLoaded || busy || !normalRect}>
                    Crop
                  </button>
                </DelayedHint>
              )}
              {((mode === 'corner' && cornerState.cornerCount < 4) ||
                (mode === 'disc'   && !discActive) ||
                (mode === 'line'   && !linesProcessed) ||
                (mode === 'normal' && !normalCropApplied)) && (
                <DelayedHint hint="Skip the cropping step and proceed to adjustments/touch-up. (You can re-crop later.)">
                  <button className="skip-crop-btn" onClick={handleSkipCrop} disabled={!imageLoaded || busy}>
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
                      <button className="recrop-btn" onClick={handleRecrop} disabled={!imageLoaded || busy}>
                        Re-crop
                      </button>
                    </DelayedHint>
                  )}
                  <DelayedHint hint="Reset this mode's crop/selection and clear the current warp result.">
                    <button
                      className="reset-btn-danger"
                      disabled={busy}
                      onClick={
                        mode === 'corner' ? handleResetCorners :
                        mode === 'disc'   ? handleResetDisc    :
                        mode === 'normal' ? handleResetNormal  :
                                            handleClearLines
                      }
                    >
                      Reset{mode === 'corner' && !cropSkipped ? ` (${cornerState.cornerCount}/4)` : ''}
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
            realImageDims={realImageDims}
            setRealImageDims={setRealImageDims}
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
            useDescreenTool={useDescreenTool}
            setUseDescreenTool={setUseDescreenTool}
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

          <ToolsPanel
            toolsOpen={toolsOpen} setToolsOpen={setToolsOpen}
            onOpenCompositor={() => setCompositorOpen(true)}
          />

          <ShortcutsPanel
            shortcutsOpen={shortcutsOpen} setShortcutsOpen={setShortcutsOpen}
            mode={mode}
            discActive={discActive}
            canSave={imageLoaded && (cropSkipped || normalCropApplied || linesProcessed || cornerState.cornerCount >= 4 || discActive)}
            imageLoaded={imageLoaded}
          />
          </div>  {/* end .sidebar-bottom-scroll */}

          <div className="file-ops">
            <DelayedHint hint="Open a file dialog to select and load an image into the app.">
              <button onClick={handleLoadImage} className="load-btn" disabled={busy && !saving}>
                Load image
              </button>
            </DelayedHint>
            <DelayedHint hint="Save the currently cropped/adjusted image to disk.">
              <button onClick={handleSaveImage} className="save-btn" disabled={busy || !(imageLoaded && (cropSkipped || normalCropApplied || linesProcessed || cornerState.cornerCount >= 4 || discActive))}>
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

      <CompositorModal
        open={compositorOpen}
        onClose={() => setCompositorOpen(false)}
        onLoad={async (info) => { setCompositorOpen(false); await handleCompositorLoad(info) }}
        dropRef={compositorDropRef}
      />
      <AboutModal open={aboutOpen} onClose={() => setAboutOpen(false)} />
      <ErrorModal message={errorMessage} onClose={() => setErrorMessage(null)} />
      <ConfirmationModal
        open={!!confirmDialog}
        message={confirmDialog?.message}
        onYes={confirmDialog?.onYes ?? confirmDialog?.onConfirm}
        onNo={confirmDialog?.onNo ?? confirmDialog?.onCancel}
        onCancel={confirmDialog?.onCancel ?? (() => setConfirmDialog(null))}
        yesText={confirmDialog?.yesText ?? 'Yes'}
        noText={confirmDialog?.noText ?? 'No'}
        cancelText={confirmDialog?.cancelText ?? 'Cancel'}
      />

      <OptionsModal
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
        autoCornerParams={autoCornerParams}
        setAutoCornerParams={setAutoCornerParams}
        closeAfterSave={closeAfterSave}
        setCloseAfterSave={setCloseAfterSave}
        postSaveEnabled={postSaveEnabled}
        setPostSaveEnabled={setPostSaveEnabled}
        postSaveCommand={postSaveCommand}
        setPostSaveCommand={setPostSaveCommand}
        touchupRemainsActive={touchupRemainsActive}
        setTouchupRemainsActive={setTouchupRemainsActive}
        straightEdgeRemainsActive={straightEdgeRemainsActive}
        setStraightEdgeRemainsActive={setStraightEdgeRemainsActive}
        autoDetectOnModeSwitch={autoDetectOnModeSwitch}
        setAutoDetectOnModeSwitch={setAutoDetectOnModeSwitch}
      />

      <main className="main-content">
        <header className="toolbar">
          {busy && <div className="header-spinner" />}
          <span className={presentedVisual.imageInfoVisible ? 'toolbar-message' : 'toolbar-message toolbar-message--fading'}>{presentedVisual.imageInfo}</span>
          <button className="about-btn" onClick={() => setAboutOpen(true)} aria-label="About">?</button>
        </header>
        <div
          ref={canvasRef}
          className="canvas-area"
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
          onContextMenu={handleContextMenu}
          style={spacePanMode ? { cursor: 'grab' } : undefined}
        >
          {displayPreview ? (
            <div style={{ position: 'relative', display: 'inline-block', lineHeight: 0, margin: 'auto', overflow: 'hidden', flexShrink: 0, alignSelf: 'center' }}>
              <img
                ref={imgRef}
                src={displayPreview}
                alt="preview"
                draggable={false}
                onLoad={handleVisiblePreviewLoad}
                onMouseLeave={handleImageMouseLeave}
                style={{
                  cursor: spacePanMode ? 'grab' : (normalDragKind === 'move' ? 'move' : 'crosshair'),
                  display: 'block',
                  transform: discLiveActive
                    ? `translate(${discLiveTransform.dx}px, ${discLiveTransform.dy}px) rotate(${discLiveTransform.angle}deg)`
                    : 'none',
                  transformOrigin: 'center center',
                  ...(fitWidth > 0
                    ? { width: `${fitWidth * zoom}px`, height: 'auto', maxWidth: 'none', maxHeight: 'none' }
                    : { maxWidth: `${zoom * 100}%`, height: 'auto' }),
                }}
              />
              <ImageOverlays
                realImageDims={presentedVisual.realImageDims}
                fitWidth={fitWidth}
                zoom={zoom}
                mode={presentedVisual.mode}
                dragging={presentedVisual.dragging}
                dragStart={presentedVisual.dragStart}
                dragCurrent={presentedVisual.dragCurrent}
                useTouchupTool={presentedVisual.useTouchupTool}
                touchupStrokes={presentedVisual.touchupStrokes}
                brushSize={presentedVisual.brushSize}
                useStraightEdgeTool={presentedVisual.useStraightEdgeTool}
                discActive={presentedVisual.discActive}
                discLiveActive={presentedVisual.discLiveActive}
                discCenter={presentedVisual.discCenter}
                discRadius={presentedVisual.discRadius}
                discBgColor={presentedVisual.discBgColor}
                discCenterCutout={presentedVisual.discCenterCutout}
                discCutoutPercent={presentedVisual.discCutoutPercent}
                ctrlDragRef={ctrlDragRef}
                shiftDragRef={shiftDragRef}
                detectedCornerPts={presentedVisual.detectedCornerPts}
                selectedCornerPts={presentedVisual.selectedCornerPts}
                dotRadius={presentedVisual.dotRadius}
                normalRect={presentedVisual.normalRect}
                lines={presentedVisual.lines}
                displayToImage={displayToImage}
                lineStartImgRef={lineStartImgRef}
                lineDragKind={presentedVisual.lineDragKind}
              />
            </div>
          ) : !preview && !busy ? (
            <div className="placeholder">Load or drop an image to begin</div>
          ) : null}
        </div>
        <StatusBar
          imageLoaded={imageLoaded}
          imageMeta={imageMeta}
          inputImageDims={inputImageDims}
          realImageDims={presentedVisual.realImageDims}
          zoom={zoom}
          onResetZoom={() => setZoom(1)}
        />
      </main>
    </div>
  )
}
