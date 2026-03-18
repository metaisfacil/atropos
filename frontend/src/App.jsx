import React, { useState, useRef, useEffect, useLayoutEffect, useCallback } from 'react'
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
  LogFrontend,
  SetTouchupSettings,
  SetWarpSettings,
  SetDiscSettings,
  RecropImage,
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

export default function App() {
  // ── Shared state ──────────────────────────────────────────────────────────
  const [mode, setMode]             = useState('corner')
  const modeRef = useRef(mode)
  useEffect(() => { modeRef.current = mode }, [mode])
  const [preview, setPreview]       = useState(null)
  const [imageInfo, setImageInfo]   = useState('')
  const [imageInfoVisible, setImageInfoVisible] = useState(true)
  const statusFadeTimer  = useRef(null)
  const statusClearTimer = useRef(null)
  const showStatus = (msg) => {
    clearTimeout(statusFadeTimer.current)
    clearTimeout(statusClearTimer.current)
    setImageInfo(msg)
    setImageInfoVisible(true)
    if (msg) {
      statusFadeTimer.current  = setTimeout(() => setImageInfoVisible(false), 4000)
      statusClearTimer.current = setTimeout(() => setImageInfo(''), 5000)
    }
  }
  const [imageLoaded, setImageLoaded] = useState(false)
  const [loading, setLoading]       = useState(false)
  const [loadingFull, setLoadingFull] = useState(false)
  const [errorMessage, setErrorMessage] = useState(null)
  const showError = (err) => setErrorMessage(err?.message || String(err))
  const [confirmDialog, setConfirmDialog] = useState(null) // { message, onConfirm }

  // Real image dimensions in Go (full resolution)
  const [realImageDims, setRealImageDims] = useState({ w: 1, h: 1 })
  // Natural dimensions of the <img> element (may be downscaled preview)
  const [imgNatural, setImgNatural] = useState({ w: 1, h: 1 })
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
  const [touchupStrokes, setTouchupStrokes] = useState([]) // array of {x,y} in image coords
  const [brushSize, setBrushSize] = useState(40)

  // ── Options state ─────────────────────────────────────────────────────────
  const [optionsOpen, setOptionsOpen]         = useState(false)
  const [touchupBackend, setTouchupBackendState] = useState(() =>
    localStorage.getItem('touchupBackend') || 'patchmatch'
  )
  const [iopaintURL, setIopaintURLState] = useState(() =>
    localStorage.getItem('iopaintURL') || 'http://127.0.0.1:8086/'
  )

  // Persist and push to backend whenever either setting changes.
  const setTouchupBackend = (v) => {
    setTouchupBackendState(v)
    localStorage.setItem('touchupBackend', v)
    SetTouchupSettings({ backend: v, iopaintUrl: iopaintURL }).catch(() => {})
  }
  const setIopaintURL = (v) => {
    setIopaintURLState(v)
    localStorage.setItem('iopaintURL', v)
    SetTouchupSettings({ backend: touchupBackend, iopaintUrl: v }).catch(() => {})
  }

  const [warpFillMode, setWarpFillModeState] = useState(() =>
    localStorage.getItem('warpFillMode') || 'clamp'
  )
  const [warpFillColor, setWarpFillColorState] = useState(() =>
    localStorage.getItem('warpFillColor') || '#ffffff'
  )

  const setWarpFillMode = (v) => {
    setWarpFillModeState(v)
    localStorage.setItem('warpFillMode', v)
    SetWarpSettings({ fillMode: v, fillColor: warpFillColor }).catch(() => {})
  }
  const setWarpFillColor = (v) => {
    setWarpFillColorState(v)
    localStorage.setItem('warpFillColor', v)
    SetWarpSettings({ fillMode: warpFillMode, fillColor: v }).catch(() => {})
  }

  const [discCenterCutout, setDiscCenterCutoutState] = useState(() => {
    const stored = localStorage.getItem('discCenterCutout')
    return stored === null ? true : stored === 'true'
  })

  const [discCutoutPercent, setDiscCutoutPercentState] = useState(() =>
    parseInt(localStorage.getItem('discCutoutPercent') || '11', 10)
  )

  const setDiscCenterCutout = (v) => {
    setDiscCenterCutoutState(v)
    localStorage.setItem('discCenterCutout', String(v))
    SetDiscSettings({ centerCutout: v, cutoutPercent: discCutoutPercent }).then((result) => {
      if (result?.preview) setPreview(result.preview)
    }).catch(() => {})
  }

  const setDiscCutoutPercent = (v) => {
    setDiscCutoutPercentState(v)
    localStorage.setItem('discCutoutPercent', String(v))
  }

  const [closeAfterSave, setCloseAfterSaveState] = useState(() =>
    localStorage.getItem('closeAfterSave') === 'true'
  )
  const setCloseAfterSave = (v) => {
    setCloseAfterSaveState(v)
    localStorage.setItem('closeAfterSave', String(v))
  }

  // Push all persisted settings to backend on startup.
  useEffect(() => {
    SetTouchupSettings({
      backend: localStorage.getItem('touchupBackend') || 'patchmatch',
      iopaintUrl: localStorage.getItem('iopaintURL') || 'http://127.0.0.1:8086/',
    }).catch(() => {})
    SetWarpSettings({
      fillMode:  localStorage.getItem('warpFillMode')  || 'clamp',
      fillColor: localStorage.getItem('warpFillColor') || '#ffffff',
    }).catch(() => {})
    const storedCutout = localStorage.getItem('discCenterCutout')
    const storedPercent = parseInt(localStorage.getItem('discCutoutPercent') || '11', 10)
    SetDiscSettings({
      centerCutout: storedCutout === null ? true : storedCutout === 'true',
      cutoutPercent: storedPercent,
    }).catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const clearTouchup = () => setTouchupStrokes([])

  const commitTouchup = async () => {
    if (!imageLoaded || touchupStrokes.length === 0) return
    setLoading(true)
    showStatus('Running touch-up…')
    try {
      const cw = realImageDims.w
      const ch = realImageDims.h
      const c = document.createElement('canvas')
      c.width = cw; c.height = ch
      const ctx = c.getContext('2d')
      if (!ctx) throw new Error('Canvas context unavailable')
      ctx.clearRect(0, 0, cw, ch)
      ctx.fillStyle = 'rgba(255,255,255,1)'
      ctx.beginPath()
      ctx.lineJoin = 'round'
      ctx.lineCap = 'round'
      ctx.lineWidth = brushSize
      for (let i = 0; i < touchupStrokes.length; i++) {
        const p = touchupStrokes[i]
        if (i === 0) ctx.moveTo(p.x, p.y)
        else ctx.lineTo(p.x, p.y)
      }
      ctx.stroke()
      for (const p of touchupStrokes) {
        ctx.beginPath(); ctx.arc(p.x, p.y, brushSize/2, 0, Math.PI*2); ctx.fill()
      }
      const data = c.toDataURL('image/png')
      const b64 = data.split(',')[1]
      let patchSize = Math.max(7, Math.floor(brushSize / 3))
      if (patchSize % 2 === 0) patchSize++
      const iterations = 5
      const result = await window['go']['main']['App']['TouchUpApply'](b64, patchSize, iterations)
      if (result?.preview) setPreview(result.preview)
      showStatus(result?.message || '')
    } catch (err) {
      console.error('TouchUp commit error:', err)
      showStatus('')
      const hint = touchupBackend === 'iopaint'
        ? '\n\nPlease make sure IOPaint is running and that you have the server address configured correctly. Alternatively, try switching to the PatchMatch backend in Options.'
        : ''
      setErrorMessage('Failed to inpaint.' + hint + '\n\n' + (err?.message || String(err)))
    } finally {
      setTouchupStrokes([])
      setLoading(false)
    }
  }

  // ── Zoom / scroll state ───────────────────────────────────────────────────
  const [zoom, setZoom]         = useState(1)
  const [fitWidth, setFitWidth] = useState(0)
  const [spacePanMode, setSpacePanMode] = useState(false)
  const canvasRef        = useRef(null)
  const pendingScrollRef = useRef(null)
  const mousePosRef      = useRef({ x: 0, y: 0 })
  const spaceDownRef     = useRef(false)
  const panDragRef       = useRef(null)  // {startX, startY, scrollLeft, scrollTop} while space+dragging
  const cornerMouseDownRef  = useRef(false) // true when mousedown fired on the image in corner mode
  const lastResizeRef       = useRef(0)     // timestamp of last window resize (to suppress post-maximize clicks)
  const touchupDraggingRef  = useRef(false) // true while a touch-up brush drag is in progress
  // Holds the latest touch-up commit handler for the window-level mouseup listener.
  // Updated every render so the closure always sees fresh state.
  const windowTouchupMouseUpRef = useRef(null)

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

  // ── Touch-up: catch mouseup outside the canvas div ────────────────────────
  // Updated every render so the closure always sees fresh state (touchupStrokes,
  // commitTouchup, etc.) without a dependency array.
  windowTouchupMouseUpRef.current = async () => {
    if (!touchupDraggingRef.current) return // already handled by the canvas-level handler
    touchupDraggingRef.current = false
    setDragging(false)
    setDragStart(null); setDragCurrent(null)
    try {
      if (touchupStrokes.length > 0) await commitTouchup()
    } catch (err) {
      console.error('Auto-commit touchup error:', err)
    }
  }
  useEffect(() => {
    const handler = () => windowTouchupMouseUpRef.current()
    window.addEventListener('mouseup', handler)
    return () => window.removeEventListener('mouseup', handler)
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

  // ── Image load / fit width ────────────────────────────────────────────────
  const handleImgLoad = () => {
    const el        = imgRef.current
    const container = canvasRef.current
    if (el) {
      const natW = el.naturalWidth; const natH = el.naturalHeight
      setImgNatural({ w: natW, h: natH })
      if (container && natW > 0 && natH > 0) {
        const aspect = natW / natH
        setFitWidth(Math.min(container.clientWidth, container.clientHeight * aspect))
      }
    }
  }

  useEffect(() => {
    const el = canvasRef.current
    if (!el || imgNatural.w <= 1) return
    const observer = new ResizeObserver(() => {
      const aspect = imgNatural.w / imgNatural.h
      setFitWidth(Math.min(el.clientWidth, el.clientHeight * aspect))
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [imgNatural])

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

  // ── Scroll-wheel zoom (+ Ctrl+Scroll feather in disc mode) ─────────────────
  useEffect(() => {
    const el  = canvasRef.current
    if (!el) return
    const log = (msg) => LogFrontend(msg).catch(() => {})

    const handler = async (e) => {
      e.preventDefault(); e.stopPropagation(); e.stopImmediatePropagation()
      if (e.ctrlKey && mode === 'disc' && discActive) {
        const delta = e.deltaY < 0 ? 1 : -1
        const newF  = Math.max(0, Math.min(100, featherSize + delta))
        setFeatherSize(newF)
        try {
          const result = await SetFeatherSize({ size: newF })
          if (result?.preview) setPreview(result.preview)
        } catch (err) { console.error(err) }
        return
      }

      const factor = e.deltaY < 0 ? 1.1 : 0.9
      setZoom(z => {
        const newZ = Math.min(5, Math.max(0.1, z * factor))
        if (newZ === z) return z
        const rect      = el.getBoundingClientRect()
        const cursorX   = e.clientX - rect.left + el.scrollLeft
        const cursorY   = e.clientY - rect.top  + el.scrollTop
        const ratio     = newZ / z
        pendingScrollRef.current = {
          left: cursorX * ratio - (e.clientX - rect.left),
          top:  cursorY * ratio - (e.clientY - rect.top),
        }
        return newZ
      })
    }

    const scrollSpy = () => {
    }
    el.addEventListener('scroll', scrollSpy, { passive: true })
    el.addEventListener('wheel', handler, { passive: false, capture: true })
    return () => {
      el.removeEventListener('wheel', handler, { capture: true })
      el.removeEventListener('scroll', scrollSpy)
    }
  }, [mode, discActive, featherSize])

  // ── Space-key pan mode ────────────────────────────────────────────────────
  useEffect(() => {
    const onKeyDown = (e) => {
      if (e.code !== 'Space') return
      const active = document.activeElement
      if (active && (['INPUT', 'TEXTAREA', 'SELECT'].includes(active.tagName) || active.isContentEditable)) return
      e.preventDefault()  // must preventDefault for every event, including repeats, to suppress native scroll
      if (e.repeat) return
      spaceDownRef.current = true
      setSpacePanMode(true)
    }
    const onKeyUp = (e) => {
      if (e.code !== 'Space') return
      spaceDownRef.current = false
      setSpacePanMode(false)
      panDragRef.current = null
    }
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('keyup', onKeyUp)
    }
  }, [])

  // Track window resizes so post-maximize stray clicks don't register as corners
  useEffect(() => {
    const onResize = () => { lastResizeRef.current = Date.now() }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  useLayoutEffect(() => {
    const el  = canvasRef.current
    const log = (msg) => LogFrontend(msg).catch(() => {})
    if (pendingScrollRef.current) {
      if (el) {
        el.scrollLeft = pendingScrollRef.current.left
        el.scrollTop  = pendingScrollRef.current.top
      }
      pendingScrollRef.current = null
    }
  }, [zoom])

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
                style={{
                  cursor: spacePanMode ? 'grab' : 'crosshair',
                  display: 'block',
                  ...(fitWidth > 0
                    ? { width: `${fitWidth * zoom}px`, height: 'auto', maxWidth: 'none', maxHeight: 'none' }
                    : { maxWidth: `${zoom * 100}%`, maxHeight: `${zoom * 100}%` }),
                }}
              />
              {useTouchupTool && touchupStrokes.length > 0 && (
                <svg
                  viewBox={`0 0 ${realImageDims.w} ${realImageDims.h}`}
                  preserveAspectRatio="none"
                  style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6 }}
                >
                  {touchupStrokes.map((pt, i) => (
                    <circle key={i} cx={pt.x} cy={pt.y} r={brushSize / 2}
                      fill="rgba(255,0,0,0.35)" stroke="rgba(255,0,0,0.8)"
                      vectorEffect="non-scaling-stroke" />
                  ))}
                </svg>
              )}
              {useStraightEdgeTool && dragging && dragStart && dragCurrent && (
                <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6, overflow: 'visible' }}>
                  <line
                    x1={dragStart.x} y1={dragStart.y}
                    x2={dragCurrent.x} y2={dragCurrent.y}
                    stroke="#ffff00" strokeWidth="2"
                  />
                </svg>
              )}
              {mode === 'disc' && !useStraightEdgeTool && dragging && dragStart && dragCurrent &&
               ctrlDragRef.current === null && shiftDragRef.current === null && (() => {
                const guideR = Math.sqrt((dragStart.x - dragCurrent.x) ** 2 + (dragStart.y - dragCurrent.y) ** 2)
                return (
                  <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6, overflow: 'visible' }}>
                    <circle
                      cx={dragCurrent.x} cy={dragCurrent.y}
                      r={guideR}
                      stroke="#00ff00" strokeWidth="2" fill="none"
                    />
                    {discCenterCutout && discCutoutPercent > 0 && (
                      <circle
                        cx={dragCurrent.x} cy={dragCurrent.y}
                        r={guideR * discCutoutPercent / 100}
                        stroke="#00ff00" strokeWidth="2" fill="none" strokeDasharray="4 3"
                      />
                    )}
                  </svg>
                )
               })()}
              {mode === 'corner' && (detectedCornerPts.length > 0 || selectedCornerPts.length > 0) && (
                <svg
                  viewBox={`0 0 ${realImageDims.w} ${realImageDims.h}`}
                  preserveAspectRatio="none"
                  style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6 }}
                >
                  {detectedCornerPts.map((pt, i) => (
                    <circle key={`d${i}`} cx={pt.X} cy={pt.Y} r={dotRadius}
                      fill="rgba(255,0,0,0.6)" stroke="red" strokeWidth="1"
                      vectorEffect="non-scaling-stroke" />
                  ))}
                  {selectedCornerPts.map((pt, i) => (
                    <circle key={`s${i}`} cx={pt.X} cy={pt.Y} r={Math.max(dotRadius * 1.5, dotRadius + 4)}
                      fill="rgba(0,255,0,0.6)" stroke="lime" strokeWidth="2"
                      vectorEffect="non-scaling-stroke" />
                  ))}
                </svg>
              )}
              {mode === 'normal' && (() => {
                // During drag: show live rect in display-space coords
                if (dragging && dragStart && dragCurrent && !useTouchupTool) {
                  const x = Math.min(dragStart.x, dragCurrent.x)
                  const y = Math.min(dragStart.y, dragCurrent.y)
                  const w = Math.abs(dragCurrent.x - dragStart.x)
                  const h = Math.abs(dragCurrent.y - dragStart.y)
                  return (
                    <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6, overflow: 'visible' }}>
                      <rect x={x} y={y} width={w} height={h}
                        stroke="#00ff00" strokeWidth="2" fill="rgba(0,255,0,0.08)" strokeDasharray="6 3" />
                    </svg>
                  )
                }
                // Confirmed selection: use viewBox so it tracks zoom/resize
                if (normalRect) {
                  const x1 = Math.min(normalRect.x1, normalRect.x2)
                  const y1 = Math.min(normalRect.y1, normalRect.y2)
                  const x2 = Math.max(normalRect.x1, normalRect.x2)
                  const y2 = Math.max(normalRect.y1, normalRect.y2)
                  return (
                    <svg
                      viewBox={`0 0 ${realImageDims.w} ${realImageDims.h}`}
                      preserveAspectRatio="none"
                      style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6 }}
                    >
                      <rect x={x1} y={y1} width={x2 - x1} height={y2 - y1}
                        stroke="#00ff00" strokeWidth="2" fill="rgba(0,255,0,0.08)"
                        strokeDasharray="6 3" vectorEffect="non-scaling-stroke" />
                    </svg>
                  )
                }
                return null
              })()}
              {mode === 'line' && (() => {
                const allLines = [...lines] // image-space coords
                if (dragging && dragStart && dragCurrent) {
                  const s = displayToImage(dragStart.x, dragStart.y)
                  const e = displayToImage(dragCurrent.x, dragCurrent.y)
                  allLines.push({ x1: s.x, y1: s.y, x2: e.x, y2: e.y })
                }
                return allLines.length > 0 ? (
                  <svg
                    viewBox={`0 0 ${realImageDims.w} ${realImageDims.h}`}
                    preserveAspectRatio="none"
                    style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6, overflow: 'visible' }}
                  >
                    {allLines.map((ln, i) => (
                      <line key={i} x1={ln.x1} y1={ln.y1} x2={ln.x2} y2={ln.y2}
                        stroke="#00ff00" strokeWidth="2" vectorEffect="non-scaling-stroke" />
                    ))}
                  </svg>
                ) : null
              })()}
            </div>
          ) : !loading ? (
            <div className="placeholder">Load or drop an image to begin</div>
          ) : null}
        </div>
      </main>
    </div>
  )
}
