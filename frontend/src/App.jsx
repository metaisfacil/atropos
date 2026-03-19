import React, { useState, useRef, useEffect, useCallback } from 'react'
import './App.css'
import { OnFileDrop, OnFileDropOff, Quit } from '../wailsjs/runtime/runtime'
import {
  LoadImage,
  DetectCorners,
  ClickCorner,
  ResetCorners,
  Crop,
  Rotate,
  Undo,
  DrawDisc,
  RotateDisc,
  StraightEdgeRotate,
  GetPixelColor,
  ShiftDisc,
  SetFeatherSize,
  ResetDisc,
  AddLine,
  ProcessLines,
  ClearLines,
  SaveImage,
  SkipCrop,
  NormalCrop,
  ResetNormal,
  OpenImageDialog,
  OpenSaveDialog,
  GetLaunchArgs,
  GetCleanPreview,
  RestoreCornerOverlay,
  RecropImage,
  CancelTouchup,
} from '../wailsjs/go/main/App'

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

import { useStatusMessage }      from './hooks/useStatusMessage'
import { usePersistentSettings } from './hooks/usePersistentSettings'
import { useZoomPan }            from './hooks/useZoomPan'
import { useTouchup }            from './hooks/useTouchup'

export default function App() {
  // ── Shared state ──────────────────────────────────────────────────────────
  const [mode, setMode]             = useState('corner')
  const modeRef = useRef(mode)
  useEffect(() => { modeRef.current = mode }, [mode])
  const [preview, setPreview]       = useState(null)
  const { imageInfo, imageInfoVisible, showStatus } = useStatusMessage()
  const [imageLoaded, setImageLoaded] = useState(false)
  const [loading, setLoading]       = useState(false)
  const [loadingFull, setLoadingFull] = useState(false)
  const [errorMessage, setErrorMessage] = useState(null)
  const showError = (err) => setErrorMessage(err?.message || String(err))
  const [confirmDialog, setConfirmDialog] = useState(null) // { message, onConfirm }

  // Real image dimensions in Go (full resolution)
  const [realImageDims, setRealImageDims] = useState({ w: 1, h: 1 })
  const imgRef  = useRef(null)

  // ── Drag / interaction state ───────────────────────────────────────────────
  const [dragging, setDragging]         = useState(false)
  const [dragStart, setDragStart]       = useState(null)   // {x, y} display coords
  const [dragCurrent, setDragCurrent]   = useState(null)
  const [lines, setLines]               = useState([])     // SVG overlay lines

  // ── Corner mode ───────────────────────────────────────────────────────────
  const [cornerState, setCornerState] = useState({
    maxCorners: 500, qualityLevel: 1, minDistance: 100, accent: 20, cornerCount: 0,
  })
  const [dotRadius, setDotRadius]         = useState(20)
  const [customCorner, setCustomCorner]   = useState(false)
  const [cornersDetected, setCornersDetected] = useState(false)
  const [detectedCornerPts, setDetectedCornerPts] = useState([])   // full-res coords from Go
  const [selectedCornerPts, setSelectedCornerPts] = useState([])   // snapped clicks 1-3
  const lastDetectSettings = useRef(null) // snapshot of params used for last successful detection
  const [cropSkipped, setCropSkipped]     = useState(false)

  // ── Disc mode ─────────────────────────────────────────────────────────────
  const [featherSize, setFeatherSize]   = useState(15)
  const [discActive, setDiscActive]     = useState(false)
  const ctrlDragRef   = useRef(null)   // {lastImg:{x,y}} during Ctrl+drag
  const ctrlDragBusy  = useRef(false)
  const shiftDragRef  = useRef(null)   // {startX, appliedAngle} during Shift+drag
  const shiftDragBusy = useRef(false)

  // ── Line mode ─────────────────────────────────────────────────────────────
  const [linesDone, setLinesDone]         = useState(0)
  const [linesProcessed, setLinesProcessed] = useState(false)

  // ── Normal crop mode ───────────────────────────────────────────────────────
  const [normalRect, setNormalRect]               = useState(null)  // {x1,y1,x2,y2} image-space, or null
  const [normalCropApplied, setNormalCropApplied] = useState(false)

  // ── UI state ──────────────────────────────────────────────────────────────
  const [shortcutsOpen, setShortcutsOpen] = useState(false)
  const [adjPanelOpen,  setAdjPanelOpen]  = useState(false)
  const [autoContrastPending, setAutoContrastPending] = useState(false)
  const [blackPoint, setBlackPoint] = useState(0)
  const [whitePoint, setWhitePoint] = useState(255)
  // Toggle to enable percentile pre-stretch before corner detection
  const [useStretchPreprocess, setUseStretchPreprocess] = useState(true)
  // Toggle to enable the touch-up brush tool (PatchMatch fill)
  const [useTouchupTool, setUseTouchupTool] = useState(false)
  // Toggle to enable the straight edge rotation tool (disc mode only)
  const [useStraightEdgeTool, setUseStraightEdgeTool] = useState(false)

  // ── Options state ─────────────────────────────────────────────────────────
  const [optionsOpen, setOptionsOpen]         = useState(false)
  const {
    touchupBackend, setTouchupBackend,
    iopaintURL, setIopaintURL,
    warpFillMode, setWarpFillMode,
    warpFillColor, setWarpFillColor,
    discCenterCutout, setDiscCenterCutout,
    discCutoutPercent, setDiscCutoutPercent,
    closeAfterSave, setCloseAfterSave,
  } = usePersistentSettings({ setPreview })

  // ── Touch-up ──────────────────────────────────────────────────────────────
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

  // ── Zoom / scroll state ───────────────────────────────────────────────────
  const cornerMouseDownRef  = useRef(false) // true when mousedown fired on the image in corner mode
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

  // ── Coordinate helpers ────────────────────────────────────────────────────
  const displayToImage = useCallback((dispX, dispY) => {
    const el = imgRef.current
    if (!el) return { x: 0, y: 0 }
    const rect = el.getBoundingClientRect()
    return {
      x: Math.round(dispX * (realImageDims.w / rect.width)),
      y: Math.round(dispY * (realImageDims.h / rect.height)),
    }
  }, [realImageDims])

  const getRelPos = useCallback((e) => {
    const el = imgRef.current
    if (!el) return null
    const rect = el.getBoundingClientRect()
    return { x: e.clientX - rect.left, y: e.clientY - rect.top }
  }, [])

  // ── Load image (dialog) ───────────────────────────────────────────────────
  const handleLoadImage = async () => {
    if (loading) return
    try {
      const filePath = await OpenImageDialog()
      if (!filePath) return
      await loadFile(filePath)
    } catch (err) {
      console.error('Load error:', err)
      showError(err)
    } finally {
      setLoading(false)
      setLoadingFull(false)
    }
  }

  // Shared logic for loading a file path (used by dialog + drag-drop + CLI args)
  const loadFile = async (filePath, autoDetect = true) => {
    CancelTouchup()
    setLoading(true)
    setLoadingFull(true)
    setZoom(1)
    const name = filePath.split(/[\\/]/).pop()
    showStatus(`Loading ${name}…`)

    const result = await LoadImage({ filePath })
    showStatus(`Loaded: ${result.width}x${result.height}`)
    setFitWidth(0)
    setPreview(result.preview)
    setImageLoaded(true)
    setRealImageDims({ w: result.width, h: result.height })
    setImgNatural({ w: result.width, h: result.height })
    setCornerState(s => ({ ...s, cornerCount: 0 }))
    setLinesDone(0)
    setLinesProcessed(false)
    setDiscActive(false)
    setNormalRect(null)
    setNormalCropApplied(false)
    setCropSkipped(false)
    setCornersDetected(false)
    lastDetectSettings.current = null
    setLines([])
    setTouchupStrokes([])
    setUseTouchupTool(false)
    setUseStraightEdgeTool(false)
    touchupDraggingRef.current = false
    setDetectedCornerPts([])
    setSelectedCornerPts([])
    setBlackPoint(0)
    setWhitePoint(255)

    if (autoDetect && mode === 'corner') {
      showStatus('Detecting corners…')
      const dr = await DetectCorners({
        maxCorners: cornerState.maxCorners,
        qualityLevel: cornerState.qualityLevel,
        minDistance: cornerState.minDistance,
        accentValue: cornerState.accent,
        dotRadius,
        useStretch: useStretchPreprocess,
        stretchLow: 0.01,
        stretchHigh: 0.99,
      })
      setPreview(dr.preview)
      showStatus(dr.message + ' — click 4 corners')
      if (dr.width && dr.height) setRealImageDims({ w: dr.width, h: dr.height })
      setDetectedCornerPts(dr.corners || [])
      setSelectedCornerPts([])
      setCornersDetected(true)
      lastDetectSettings.current = {
        maxCorners: cornerState.maxCorners,
        qualityLevel: cornerState.qualityLevel,
        minDistance: cornerState.minDistance,
        accent: cornerState.accent,
        useStretch: useStretchPreprocess,
      }
    }

    setLoading(false)
    setLoadingFull(false)
  }

  // ── Drag-and-drop file loading ─────────────────────────────────────────────
  useEffect(() => {
    // Prevent the browser/WebView2 from navigating to (opening) a dropped file.
    // Without these handlers the default behaviour fires when the drag happens
    // faster than Wails intercepts it, causing the image to open in the window.
    const suppressDefault = (e) => e.preventDefault()
    document.addEventListener('dragover', suppressDefault)
    document.addEventListener('drop', suppressDefault)

    OnFileDrop(async (_x, _y, paths) => {
      if (!paths || paths.length === 0) return
      const filePath = paths[0]
      const ext = filePath.split('.').pop().toLowerCase()
      if (!['png','jpg','jpeg','tif','tiff','bmp','gif','webp'].includes(ext)) return
      try {
        await loadFile(filePath, modeRef.current === 'corner')
      } catch (err) {
        console.error('Drop load error:', err)
        showError(err)
        setLoading(false)
        setLoadingFull(false)
      }
    }, false)
    return () => {
      OnFileDropOff()
      document.removeEventListener('dragover', suppressDefault)
      document.removeEventListener('drop', suppressDefault)
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Launch arguments ───────────────────────────────────────────────────────
  useEffect(() => {
    (async () => {
      try {
        const args = await GetLaunchArgs()
        if (args.mode) setMode(args.mode)
        if (args.filePath) {
          const shouldDetect = args.mode ?
            (args.mode === 'corner') : (mode === 'corner')
          await loadFile(args.filePath, shouldDetect)
        } else {
          showStatus('No image loaded')
        }
      } catch (err) {
        console.error('Launch args error:', err)
        setLoading(false)
        setLoadingFull(false)
        showStatus('No image loaded')
      }
    })()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Corner detection ───────────────────────────────────────────────────────
  const handleDetectCorners = async () => {
    setLoading(true)
    showStatus('Detecting corners…')
    try {
      const result = await DetectCorners({
        maxCorners:   cornerState.maxCorners,
        qualityLevel: cornerState.qualityLevel,
        minDistance:  cornerState.minDistance,
        accentValue:  cornerState.accent,
        dotRadius,
        useStretch: useStretchPreprocess,
        stretchLow: 0.01,
        stretchHigh: 0.99,
      })
      setPreview(result.preview)
      showStatus(result.message + ' — click 4 corners')
      if (result.width && result.height) setRealImageDims({ w: result.width, h: result.height })
      setDetectedCornerPts(result.corners || [])
      setSelectedCornerPts([])
      setCornerState(s => ({ ...s, cornerCount: 0 }))
      setCornersDetected(true)
      lastDetectSettings.current = {
        maxCorners: cornerState.maxCorners,
        qualityLevel: cornerState.qualityLevel,
        minDistance: cornerState.minDistance,
        accent: cornerState.accent,
        useStretch: useStretchPreprocess,
      }
    } catch (err) {
      console.error('Detect error:', err)
    } finally {
      setLoading(false)
    }
  }

  // ── Reset handlers ────────────────────────────────────────────────────────
  const handleSkipCrop = async () => {
    setLoading(true)
    showStatus('Skipping crop…')
    try {
      const result = await SkipCrop()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      if (mode === 'corner') {
        setCornerState(s => ({ ...s, cornerCount: 4 }))
        setDetectedCornerPts([])
        setSelectedCornerPts([])
      } else if (mode === 'disc') {
        setDiscActive(true)
        setDragging(false)
        setDragStart(null)
        setDragCurrent(null)
      } else if (mode === 'line') {
        setLinesProcessed(true)
      } else if (mode === 'normal') {
        setNormalCropApplied(true)
        setNormalRect(null)
      }
      setCropSkipped(true)
      showStatus(result?.message || 'Crop skipped')
    } catch (err) {
      console.error('SkipCrop error:', err)
      showError(err)
    } finally {
      setLoading(false)
    }
  }

  const handleRecrop = () => {
    setConfirmDialog({
      message: 'Re-crop will use the current output as a new source image, resetting all crop and adjustment state. Continue?',
      onConfirm: async () => {
        CancelTouchup()
        setConfirmDialog(null)
        setLoading(true)
        showStatus('Re-cropping…')
        try {
          const result = await RecropImage()
          setFitWidth(0)
          setPreview(result.preview)
          setRealImageDims({ w: result.width, h: result.height })
          setCornerState(s => ({ ...s, cornerCount: 0 }))
          setLinesDone(0)
          setLinesProcessed(false)
          setDiscActive(false)
          setNormalRect(null)
          setNormalCropApplied(false)
          setCropSkipped(false)
          setCornersDetected(false)
          lastDetectSettings.current = null
          setLines([])
          setTouchupStrokes([])
          setDetectedCornerPts([])
          setSelectedCornerPts([])
          setBlackPoint(0)
          setWhitePoint(255)
          showStatus(`Re-cropping from ${result.width}×${result.height} image`)
        } catch (err) {
          console.error('RecropImage error:', err)
          showError(err)
        } finally {
          setLoading(false)
        }
      },
    })
  }

  const handleResetCorners = async () => {
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting corners…')
    try {
      const result = await ResetCorners()
      setPreview(result.preview)
      showStatus(result.message)
      if (result.width && result.height) setRealImageDims({ w: result.width, h: result.height })
      setDetectedCornerPts(result.corners || [])
      setSelectedCornerPts([])
      setCornerState(s => ({ ...s, cornerCount: 0 }))
      setCropSkipped(false)
      setUseTouchupTool(false)
    } catch (err) {
      console.error('ResetCorners error:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleResetDisc = async () => {
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting disc…')
    try {
      const result = await ResetDisc()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      setDiscActive(false)
      setCropSkipped(false)
      setUseTouchupTool(false)
      setUseStraightEdgeTool(false)
      setDragging(false)
      setDragStart(null)
      setDragCurrent(null)
      showStatus(result?.message || 'Disc selection reset')
    } catch (err) {
      console.error('ResetDisc error:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleResetNormal = async () => {
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting normal crop…')
    try {
      const result = await ResetNormal()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      setNormalRect(null)
      setNormalCropApplied(false)
      setCropSkipped(false)
      setUseTouchupTool(false)
      showStatus(result?.message || 'Normal crop reset')
    } catch (err) {
      console.error('ResetNormal error:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleNormalCrop = async () => {
    if (!normalRect) return
    setLoading(true)
    showStatus('Applying crop…')
    try {
      const result = await NormalCrop({ x1: normalRect.x1, y1: normalRect.y1, x2: normalRect.x2, y2: normalRect.y2 })
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      showStatus(result?.message || 'Crop applied')
      setNormalRect(null)
      setNormalCropApplied(true)
    } catch (err) {
      console.error('NormalCrop error:', err)
      showError(err)
    } finally {
      setLoading(false)
    }
  }

  const handleClearLines = async () => {
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting lines…')
    try {
      const result = await ClearLines()
      setLinesDone(0)
      setLines([])
      setLinesProcessed(false)
      setCropSkipped(false)
      setUseTouchupTool(false)
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      showStatus(result?.message || 'Lines cleared')
    } catch (err) {
      console.error('ClearLines error:', err)
    } finally {
      setLoading(false)
    }
  }

  // ── Save ─────────────────────────────────────────────────────────────────
  const handleSaveImage = async () => {
    try {
      const filePath = await OpenSaveDialog()
      if (!filePath) return
      setLoading(true)
      showStatus('Saving…')
      const result = await SaveImage({ outputPath: filePath })
      // Prefer a backend-provided message; fall back to a simple path note.
      const savedName = filePath.split(/[\\/]/).pop()
      showStatus(result?.message || `Saved to ${savedName}`)
      if (closeAfterSave) Quit()
    } catch (err) {
      console.error('Save error:', err)
      showError(err)
    } finally {
      setLoading(false)
    }
  }

  // ── Canvas mouse handlers ────────────────────────────────────────────────
  const handleMouseDown = (e) => {
    if (!imageLoaded || loading) return
    // Space+drag pan — works anywhere in the canvas area
    if (spaceDownRef.current) {
      const el = canvasRef.current
      if (!el) return
      e.preventDefault()
      panDragRef.current = {
        startX: e.clientX,
        startY: e.clientY,
        scrollLeft: el.scrollLeft,
        scrollTop:  el.scrollTop,
      }
      return
    }
    if (e.target !== imgRef.current) return
    e.preventDefault()
    const pos = getRelPos(e)
    if (!pos) return

    if (mode === 'corner' && !useTouchupTool) { cornerMouseDownRef.current = true; return } // corner clicks handled in mouseUp

    if (useTouchupTool) {
      // Start a touch-up stroke; record image-space coordinates
      const imgPt = displayToImage(pos.x, pos.y)
      touchupDraggingRef.current = true
      setTouchupStrokes([{ x: imgPt.x, y: imgPt.y }])
      setDragging(true); setDragStart(pos); setDragCurrent(pos)
      return
    }

    if (useStraightEdgeTool && mode === 'disc' && discActive) {
      setDragging(true); setDragStart(pos); setDragCurrent(pos)
      return
    }

    if (mode === 'disc' && e.shiftKey && discActive) {
      ctrlDragRef.current = null
      shiftDragRef.current = { startX: e.clientX, appliedAngle: 0 }
      shiftDragBusy.current = false
      setDragging(true); setDragStart(pos); setDragCurrent(pos)
      return
    }
    if (mode === 'disc' && e.ctrlKey && discActive) {
      shiftDragRef.current = null
      ctrlDragRef.current = { lastImg: displayToImage(pos.x, pos.y) }
      ctrlDragBusy.current = false
      setDragging(true); setDragStart(pos); setDragCurrent(pos)
      return
    }
    if (mode === 'disc' && discActive) return // must Reset before drawing new disc
    if (mode === 'line' && linesProcessed) return // crop was skipped; no new lines allowed

    ctrlDragRef.current = null
    shiftDragRef.current = null
    setDragging(true); setDragStart(pos); setDragCurrent(pos)
  }

  const handleMouseMove = async (e) => {
    // Space+drag pan
    if (panDragRef.current) {
      const el = canvasRef.current
      if (el) {
        el.scrollLeft = panDragRef.current.scrollLeft - (e.clientX - panDragRef.current.startX)
        el.scrollTop  = panDragRef.current.scrollTop  - (e.clientY - panDragRef.current.startY)
      }
      return
    }
    const pos = getRelPos(e)
    if (pos) mousePosRef.current = pos
    if (!dragging) return
    if (pos) setDragCurrent(pos)

    if (useTouchupTool) {
      if (pos) {
        const imgPt = displayToImage(pos.x, pos.y)
        setTouchupStrokes(s => {
          const last = s[s.length - 1]
          if (last && Math.hypot(last.x - imgPt.x, last.y - imgPt.y) < 3) return s
          return [...s, { x: imgPt.x, y: imgPt.y }]
        })
      }
      return
    }

    if (useStraightEdgeTool) return

    if (pos && shiftDragRef.current && !shiftDragBusy.current) {
      const dx = e.clientX - shiftDragRef.current.startX
      const totalAngle = dx * 0.3
      const delta = totalAngle - shiftDragRef.current.appliedAngle
      if (Math.abs(delta) < 0.5) return
      shiftDragRef.current.appliedAngle = totalAngle
      shiftDragBusy.current = true
      try {
        const result = await RotateDisc({ angle: delta })
        if (result?.preview) setPreview(result.preview)
      } catch (_) {}
      shiftDragBusy.current = false
      return
    }

    if (pos && ctrlDragRef.current && !ctrlDragBusy.current) {
      const imgPt = displayToImage(pos.x, pos.y)
      const last  = ctrlDragRef.current.lastImg
      const dx = last.x - imgPt.x
      const dy = last.y - imgPt.y
      if (Math.abs(dx) < 1 && Math.abs(dy) < 1) return
      ctrlDragRef.current.lastImg = imgPt
      ctrlDragBusy.current = true
      try {
        const result = await ShiftDisc({ dx, dy })
        if (result?.preview) setPreview(result.preview)
      } catch (_) {}
      ctrlDragBusy.current = false
    }
  }

  const handleMouseUp = async (e) => {
    if (panDragRef.current) { panDragRef.current = null; return }
    if (!imageLoaded || loading) return

    // Touch-up: commit wherever the mouse is released within the canvas area.
    // The window-level listener (see useEffect below) covers releases outside the canvas entirely.
    // touchupDraggingRef prevents double-commit between these two paths.
    if (useTouchupTool && touchupDraggingRef.current) {
      touchupDraggingRef.current = false
      setDragging(false)
      setDragStart(null); setDragCurrent(null)
      try {
        if (touchupStrokes.length > 0) await commitTouchup()
      } catch (err) {
        console.error('Auto-commit touchup error:', err)
      }
      return
    }

    if (e.target !== imgRef.current) return
    const pos = getRelPos(e)
    if (!pos) return

    // Corner click
    if (mode === 'corner' && !useTouchupTool) {
      const hadMouseDown = cornerMouseDownRef.current
      cornerMouseDownRef.current = false
      if (!hadMouseDown) return // no matching mousedown on canvas (e.g. stray mouseup from window maximize)
      if (Date.now() - lastResizeRef.current < 300) return // ignore clicks immediately after window resize/maximize
      if ((cornerState.cornerCount === 0 && !cornersDetected && !customCorner) || cornerState.cornerCount >= 4) return
      const imgPt = displayToImage(pos.x, pos.y)
      try {
        if (cornerState.cornerCount === 3) {
          setLoading(true)
          showStatus('Applying perspective warp…')
        }
        const result = await ClickCorner({ x: imgPt.x, y: imgPt.y, custom: customCorner, dotRadius })
        if (result.preview) setPreview(result.preview)
        showStatus(result.message)
        setCornerState(s => ({ ...s, cornerCount: result.count }))
        if (result.done) {
          setDetectedCornerPts([])
          setSelectedCornerPts([])
          if (result.width && result.height) setRealImageDims({ w: result.width, h: result.height })
        } else {
          setSelectedCornerPts(prev => [...prev, { X: result.snappedX, Y: result.snappedY }])
        }
      } catch (err) {
        console.error('ClickCorner error:', err)
      } finally {
        setLoading(false)
      }
      return
    }

    if (!dragging || !dragStart) { setDragging(false); return }
    setDragging(false)

    // Straight edge rotation: compute angle of drawn line and rotate disc to make it horizontal
    if (useStraightEdgeTool && mode === 'disc' && discActive) {
      const dx = pos.x - dragStart.x
      const dy = pos.y - dragStart.y
      if (Math.sqrt(dx * dx + dy * dy) >= 5) {
        const angleDeg = Math.atan2(dy, dx) * 180 / Math.PI
        setLoading(true)
        showStatus('Applying straight edge rotation…')
        try {
          const result = await StraightEdgeRotate({ angleDeg })
          if (result?.preview) setPreview(result.preview)
          showStatus('Straight edge rotation applied')
        } catch (err) {
          console.error('StraightEdge error:', err)
          showError(err)
        } finally {
          setLoading(false)
        }
      }
      setUseStraightEdgeTool(false)
      setDragStart(null); setDragCurrent(null)
      return
    }

    // Shift+drag final residual rotation
    if (mode === 'disc' && shiftDragRef.current && discActive) {
      const dx = e.clientX - shiftDragRef.current.startX
      const totalAngle = dx * 0.3
      const delta = totalAngle - shiftDragRef.current.appliedAngle
      shiftDragRef.current = null; shiftDragBusy.current = false
      if (Math.abs(delta) >= 0.5) {
        try {
          const result = await RotateDisc({ angle: delta })
          if (result?.preview) setPreview(result.preview)
        } catch (err) { console.error('RotateDisc drag error:', err) }
      }
      setDragStart(null); setDragCurrent(null)
      return
    }

    // Ctrl+drag final residual shift
    if (mode === 'disc' && ctrlDragRef.current && discActive) {
      const last  = ctrlDragRef.current.lastImg
      const imgPt = displayToImage(pos.x, pos.y)
      const dx = last.x - imgPt.x; const dy = last.y - imgPt.y
      ctrlDragRef.current = null; ctrlDragBusy.current = false
      if (Math.abs(dx) >= 1 || Math.abs(dy) >= 1) {
        try {
          const result = await ShiftDisc({ dx, dy })
          if (result?.preview) setPreview(result.preview)
        } catch (err) { console.error('ShiftDisc drag error:', err) }
      }
      setDragStart(null); setDragCurrent(null)
      return
    }

    // Disc draw
    if (mode === 'disc') {
      const start  = displayToImage(dragStart.x, dragStart.y)
      const end    = displayToImage(pos.x, pos.y)
      const radius = Math.round(Math.sqrt((start.x - end.x) ** 2 + (start.y - end.y) ** 2))
      if (radius < 5) return
      setZoom(1)
      setLoading(true)
      showStatus('Applying disc crop…')
      try {
        const result = await DrawDisc({ centerX: end.x, centerY: end.y, radius })
        setPreview(result.preview)
        showStatus(`Disc: center=(${end.x},${end.y}) r=${radius} — Y=eyedrop, Arrows=shift, +/-=feather`)
        setDiscActive(true)
      } catch (err) {
        console.error('DrawDisc error:', err)
      } finally {
        setLoading(false)
      }
    }

    // Normal crop: record the selection rectangle in image-space coords
    if (mode === 'normal' && !useTouchupTool) {
      const start = displayToImage(dragStart.x, dragStart.y)
      const end   = displayToImage(pos.x, pos.y)
      const w = Math.abs(end.x - start.x)
      const h = Math.abs(end.y - start.y)
      if (w >= 5 && h >= 5) {
        setNormalRect({ x1: start.x, y1: start.y, x2: end.x, y2: end.y })
      }
      setDragStart(null); setDragCurrent(null)
      return
    }

    // Line draw
    if (mode === 'line' && !linesProcessed) {
      const start = displayToImage(dragStart.x, dragStart.y)
      const end   = displayToImage(pos.x, pos.y)
      const dx = end.x - start.x; const dy = end.y - start.y
      if (Math.sqrt(dx * dx + dy * dy) < 5) return
      try {
        setLines(prev => [...prev, { x1: start.x, y1: start.y, x2: end.x, y2: end.y }])
        const result   = await AddLine({ x1: start.x, y1: start.y, x2: end.x, y2: end.y })
        const newCount = linesDone + 1
        setLinesDone(newCount)
        showStatus(result.message)
        if (newCount >= 4) {
          setLoading(true)
          showStatus('Applying perspective correction…')
          const proc = await ProcessLines()
          setPreview(proc.preview)
          if (proc.width && proc.height) setRealImageDims({ w: proc.width, h: proc.height })
          showStatus('Perspective correction applied')
          setLinesDone(0); setLines([]); setLinesProcessed(true)
          setLoading(false)
        }
      } catch (err) {
        console.error('Line error:', err)
      }
    }

    setDragStart(null); setDragCurrent(null)
  }

  // Abort in-progress drawing drags when the cursor leaves the image element.
  // Only the initial "drawing" gestures are cancelled; post-draw adjustments
  // (Ctrl+drag shift, Shift+drag rotate) are not affected.
  const handleImageMouseLeave = () => {
    if (!dragging) return
    if (mode === 'line' && !linesProcessed) {
      setDragging(false); setDragStart(null); setDragCurrent(null)
    } else if (mode === 'disc' && !discActive) {
      setDragging(false); setDragStart(null); setDragCurrent(null)
    } else if (useStraightEdgeTool && mode === 'disc' && discActive) {
      setDragging(false); setDragStart(null); setDragCurrent(null)
    }
  }

  // ── Keyboard shortcuts ────────────────────────────────────────────────────
  useEffect(() => {
    const handleKeyDown = async (e) => {
      if (!imageLoaded) return
      try {
        let result

        if (mode === 'disc' && discActive) {
          const shiftStep = e.shiftKey ? 20 : 5
          switch (e.key) {
            case 'ArrowUp':    e.preventDefault(); result = await ShiftDisc({ dx: 0, dy: -shiftStep }); if (result?.preview) setPreview(result.preview); return
            case 'ArrowDown':  e.preventDefault(); result = await ShiftDisc({ dx: 0, dy:  shiftStep }); if (result?.preview) setPreview(result.preview); return
            case 'ArrowLeft':  e.preventDefault(); result = await ShiftDisc({ dx: -shiftStep, dy: 0 }); if (result?.preview) setPreview(result.preview); return
            case 'ArrowRight': e.preventDefault(); result = await ShiftDisc({ dx:  shiftStep, dy: 0 }); if (result?.preview) setPreview(result.preview); return
            case '+': case '=': {
              const newF = Math.min(100, featherSize + 1); setFeatherSize(newF)
              result = await SetFeatherSize({ size: newF }); if (result?.preview) setPreview(result.preview); return
            }
            case '-': {
              const newF = Math.max(0, featherSize - 1); setFeatherSize(newF)
              result = await SetFeatherSize({ size: newF }); if (result?.preview) setPreview(result.preview); return
            }
            case 'y': case 'Y': {
              const mp = mousePosRef.current
              const imgPt = displayToImage(mp.x, mp.y)
              result = await GetPixelColor({ x: imgPt.x, y: imgPt.y })
              if (result?.preview) setPreview(result.preview)
              return
            }
          }
        }

        const key = e.key.toLowerCase()
        // Ctrl/Cmd+Z -> Undo
        if ((e.ctrlKey || e.metaKey) && e.code === 'KeyZ') {
          if (e.repeat) return
          const active = document.activeElement
          if (active && (['INPUT','TEXTAREA','SELECT'].includes(active.tagName) || active.isContentEditable)) return
          // Don't undo mid-drag: Ctrl+drag (disc shift) and Shift+drag (disc
          // rotate) leave disc state unsaved, so undoing partway through would
          // desync warpedImage from discCenter/rotationAngle.
          if (ctrlDragRef.current !== null || shiftDragRef.current !== null) return
          e.preventDefault()
          setLoading(true); showStatus('Undoing…')
          try {
            const res = await Undo()
            if (res?.preview) setPreview(res.preview)
            if (res?.width && res?.height) setRealImageDims({ w: res.width, h: res.height })
            showStatus(res?.message || '')
          } catch (err) {
            console.error('Undo shortcut error:', err)
            showError(err)
          } finally {
            setLoading(false)
          }
          return
        }

        // Ctrl/Cmd+S -> Save
        if ((e.ctrlKey || e.metaKey) && e.code === 'KeyS') {
          if (e.repeat) return
          e.preventDefault()
          try {
            await handleSaveImage()
          } catch (err) {
            console.error('Save shortcut error:', err)
            showError(err)
          }
          return
        }
        switch (key) {
          case 'w': result = await Crop({ direction: 'top'    }); if (result?.preview) setPreview(result.preview); break
          case 's': result = await Crop({ direction: 'bottom' }); if (result?.preview) setPreview(result.preview); break
          case 'a': result = await Crop({ direction: 'left'   }); if (result?.preview) setPreview(result.preview); break
          case 'd': result = await Crop({ direction: 'right'  }); if (result?.preview) setPreview(result.preview); break
          case 'q':
            setLoading(true); showStatus('Rotating…')
            result = mode === 'disc' && discActive
              ? await RotateDisc({ angle: -15 })
              : await Rotate({ flipCode: 2 })
            if (result?.preview) setPreview(result.preview); showStatus(''); setLoading(false); break
          case 'e':
            setLoading(true); showStatus('Rotating…')
            result = mode === 'disc' && discActive
              ? await RotateDisc({ angle: 15 })
              : await Rotate({ flipCode: 1 })
            if (result?.preview) setPreview(result.preview); showStatus(''); setLoading(false); break

          default:
            break
        }
      } catch (err) {
        console.error('Shortcut error:', err)
        showError(err)
        setLoading(false)
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [imageLoaded, mode, discActive, featherSize, displayToImage]) // eslint-disable-line react-hooks/exhaustive-deps

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
        {/* Mode selector (always visible) */}
        <div className="mode-selector">
          {['corner', 'disc', 'line', 'normal'].map(m => (
            <DelayedHint key={m} hint={`Switch to ${m.charAt(0).toUpperCase() + m.slice(1)} mode`}>
              <button
                className={`mode-btn ${mode === m ? 'active' : ''}`}
                onClick={async () => {
                if (m === mode) return
                setUseTouchupTool(false)
                setUseStraightEdgeTool(false)
                if (imageLoaded) {
                  CancelTouchup()
                  try {
                    if (mode === 'corner') {
                      setCornerState(s => ({ ...s, cornerCount: 0 }))
                      setCornersDetected(false)
                      setDetectedCornerPts([])
                      setSelectedCornerPts([])
                      setCropSkipped(false)
                      await ResetCorners() // clears warpedImage so GetCleanPreview returns currentImage
                    } else if (mode === 'disc' && discActive) {
                      await ResetDisc(); setDiscActive(false); setCropSkipped(false)
                    } else if (mode === 'line') {
                      // Always clear lines when leaving, regardless of how many were drawn.
                      // setLines([]) must be called here so the SVG overlay is wiped
                      // immediately — without it, drawn lines persist visually even though
                      // linesDone is reset to 0.
                      await ClearLines()
                      setLinesDone(0)
                      setLines([])
                      setLinesProcessed(false)
                      setCropSkipped(false)
                    } else if (mode === 'normal') {
                      await ResetNormal()
                      setNormalRect(null)
                      setNormalCropApplied(false)
                      setCropSkipped(false)
                    }

                    // Switching to corners: restore cached overlay if detection settings unchanged.
                    if (m === 'corner' && lastDetectSettings.current) {
                      const s = lastDetectSettings.current
                      if (s.maxCorners === cornerState.maxCorners &&
                          s.qualityLevel === cornerState.qualityLevel &&
                          s.minDistance === cornerState.minDistance &&
                          s.accent === cornerState.accent &&
                          s.useStretch === useStretchPreprocess) {
                        try {
                          const res = await RestoreCornerOverlay({ dotRadius })
                          const c = canvasRef.current
                          if (c && res.width && res.height) {
                            setFitWidth(Math.min(c.clientWidth, c.clientHeight * res.width / res.height))
                          } else {
                            setFitWidth(0)
                          }
                          setPreview(res.preview)
                          if (res.width && res.height) setRealImageDims({ w: res.width, h: res.height })
                          setDetectedCornerPts(res.corners || [])
                          setSelectedCornerPts([])
                          setCornersDetected(true)
                          setCornerState(s => ({ ...s, cornerCount: 0 }))
                          setMode(m)
                          return
                        } catch (_) {}
                      }
                    }

                    const res = await GetCleanPreview()
                    if (res?.preview) {
                      const c = canvasRef.current
                      if (c && res.width && res.height) {
                        setFitWidth(Math.min(c.clientWidth, c.clientHeight * res.width / res.height))
                      } else {
                        setFitWidth(0)
                      }
                      setPreview(res.preview)
                    }
                    if (res?.width && res?.height) setRealImageDims({ w: res.width, h: res.height })
                  } catch (_) {}
                }
                setMode(m)
              }}
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

          </div>{/* .sidebar-scroll-inner */}
        </div>{/* .sidebar-scroll */}
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
              <DelayedHint hint="Apply a drawn rectangle as a crop to the image.">
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
            loading={loading}                     setLoading={setLoading}
            setPreview={setPreview}
            useStretchPreprocess={useStretchPreprocess}
            setUseStretchPreprocess={setUseStretchPreprocess}
            touchupAvailable={
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
          />

          <div className="file-ops">
            <DelayedHint hint="Open a file dialog to select and load an image into the app.">
              <button onClick={handleLoadImage} className="load-btn" disabled={loading}>
                Load image
              </button>
            </DelayedHint>
            <DelayedHint hint="Save the currently cropped/adjusted image to disk.">
              <button onClick={handleSaveImage} className="save-btn" disabled={loading}>
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
      </main>
    </div>
  )
}
