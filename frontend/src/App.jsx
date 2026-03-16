import React, { useState, useRef, useEffect, useLayoutEffect, useCallback } from 'react'
import './App.css'
import { OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime'
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
  GetPixelColor,
  ShiftDisc,
  SetFeatherSize,
  ResetDisc,
  AddLine,
  ProcessLines,
  ClearLines,
  SaveImage,
  OpenImageDialog,
  OpenSaveDialog,
  GetLaunchArgs,
  GetCleanPreview,
  LogFrontend,
} from '../wailsjs/go/main/App'

import CornerPanel      from './components/CornerPanel'
import DiscPanel        from './components/DiscPanel'
import LinePanel        from './components/LinePanel'
import AdjustmentsPanel from './components/AdjustmentsPanel'
import ShortcutsPanel   from './components/ShortcutsPanel'

export default function App() {
  // ── Shared state ──────────────────────────────────────────────────────────
  const [mode, setMode]             = useState('corner')
  const [preview, setPreview]       = useState(null)
  const [imageInfo, setImageInfo]   = useState('No image loaded')
  const [imageLoaded, setImageLoaded] = useState(false)
  const [loading, setLoading]       = useState(false)
  const [loadingFull, setLoadingFull] = useState(false)

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

  // ── UI state ──────────────────────────────────────────────────────────────
  const [shortcutsOpen, setShortcutsOpen] = useState(true)
  const [adjPanelOpen,  setAdjPanelOpen]  = useState(false)
  const [autoContrastPending, setAutoContrastPending] = useState(false)
  const [blackPoint, setBlackPoint] = useState(0)
  const [whitePoint, setWhitePoint] = useState(255)
  // Toggle to enable percentile pre-stretch before corner detection
  const [useStretchPreprocess, setUseStretchPreprocess] = useState(true)

  // ── Zoom / scroll state ───────────────────────────────────────────────────
  const [zoom, setZoom]         = useState(1)
  const [fitWidth, setFitWidth] = useState(0)
  const canvasRef        = useRef(null)
  const pendingScrollRef = useRef(null)
  const mousePosRef      = useRef({ x: 0, y: 0 })

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
      setImageInfo('Load failed — see debug log')
    } finally {
      setLoading(false)
      setLoadingFull(false)
    }
  }

  // Shared logic for loading a file path (used by dialog + drag-drop + CLI args)
  const loadFile = async (filePath) => {
    setLoading(true)
    setLoadingFull(true)
    setZoom(1)
    const name = filePath.split(/[\\/]/).pop()
    setImageInfo(`Loading ${name}…`)

    const result = await LoadImage({ filePath })
    setImageInfo(`Loaded: ${result.width}x${result.height}`)
    setPreview(result.preview)
    setImageLoaded(true)
    setRealImageDims({ w: result.width, h: result.height })
    setImgNatural({ w: result.width, h: result.height })
    setCornerState(s => ({ ...s, cornerCount: 0 }))
    setLinesDone(0)
    setLinesProcessed(false)
    setDiscActive(false)
    setCornersDetected(false)
    setLines([])

    if (mode === 'corner') {
      setImageInfo('Detecting corners…')
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
      setImageInfo(dr.message + ' — click 4 corners')
      if (dr.width && dr.height) setRealImageDims({ w: dr.width, h: dr.height })
      setCornersDetected(true)
    }

    setLoading(false)
    setLoadingFull(false)
  }

  // ── Drag-and-drop file loading ─────────────────────────────────────────────
  useEffect(() => {
    OnFileDrop(async (_x, _y, paths) => {
      if (!paths || paths.length === 0) return
      const filePath = paths[0]
      const ext = filePath.split('.').pop().toLowerCase()
      if (!['png','jpg','jpeg','tif','tiff','bmp','gif','webp'].includes(ext)) return
      try {
        await loadFile(filePath)
      } catch (err) {
        console.error('Drop load error:', err)
        setImageInfo('Load failed — see debug log')
        setLoading(false)
        setLoadingFull(false)
      }
    }, false)
    return () => OnFileDropOff()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Launch arguments ───────────────────────────────────────────────────────
  useEffect(() => {
    (async () => {
      try {
        const args = await GetLaunchArgs()
        if (args.mode) setMode(args.mode)
        if (args.filePath) {
          await loadFile(args.filePath)
        }
      } catch (err) {
        console.error('Launch args error:', err)
        setLoading(false)
        setLoadingFull(false)
      }
    })()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Corner detection ───────────────────────────────────────────────────────
  const handleDetectCorners = async () => {
    setLoading(true)
    setImageInfo('Detecting corners…')
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
      setImageInfo(result.message + ' — click 4 corners')
      if (result.width && result.height) setRealImageDims({ w: result.width, h: result.height })
      setCornerState(s => ({ ...s, cornerCount: 0 }))
      setCornersDetected(true)
    } catch (err) {
      console.error('Detect error:', err)
    } finally {
      setLoading(false)
    }
  }

  // ── Reset handlers ────────────────────────────────────────────────────────
  const handleResetCorners = async () => {
    setLoading(true)
    setImageInfo('Resetting corners…')
    try {
      const result = await ResetCorners()
      setPreview(result.preview)
      setImageInfo(result.message)
      if (result.width && result.height) setRealImageDims({ w: result.width, h: result.height })
      setCornerState(s => ({ ...s, cornerCount: 0 }))
    } catch (err) {
      console.error('ResetCorners error:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleResetDisc = async () => {
    setLoading(true)
    setImageInfo('Resetting disc…')
    try {
      const result = await ResetDisc()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      setDiscActive(false)
      setImageInfo(result?.message || 'Disc selection reset')
    } catch (err) {
      console.error('ResetDisc error:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleClearLines = async () => {
    setLoading(true)
    setImageInfo('Resetting lines…')
    try {
      const result = await ClearLines()
      setLinesDone(0)
      setLines([])
      setLinesProcessed(false)
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      setImageInfo(result?.message || 'Lines cleared')
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
      const result = await SaveImage({ outputPath: filePath })
      setImageInfo(result.message?.startsWith('Saved to:')
        ? result.message
        : `Saved to: ${result.message}`)
    } catch (err) {
      console.error('Save error:', err)
    }
  }

  // ── Canvas mouse handlers ────────────────────────────────────────────────
  const handleMouseDown = (e) => {
    if (!imageLoaded || loading) return
    if (e.target !== imgRef.current) return
    e.preventDefault()
    const pos = getRelPos(e)
    if (!pos) return

    if (mode === 'corner') return // corner clicks handled in mouseUp

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

    ctrlDragRef.current = null
    shiftDragRef.current = null
    setDragging(true); setDragStart(pos); setDragCurrent(pos)
  }

  const handleMouseMove = async (e) => {
    const pos = getRelPos(e)
    if (pos) mousePosRef.current = pos
    if (!dragging) return
    if (pos) setDragCurrent(pos)

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
    if (!imageLoaded || loading) return
    if (e.target !== imgRef.current) return
    const pos = getRelPos(e)
    if (!pos) return

    // Corner click
    if (mode === 'corner') {
      if (!cornersDetected || cornerState.cornerCount >= 4) return
      const imgPt = displayToImage(pos.x, pos.y)
      try {
        if (cornerState.cornerCount === 3) {
          setLoading(true)
          setImageInfo('Applying perspective warp…')
        }
        const result = await ClickCorner({ x: imgPt.x, y: imgPt.y, custom: customCorner, dotRadius })
        setPreview(result.preview)
        setImageInfo(result.message)
        setCornerState(s => ({ ...s, cornerCount: result.count }))
        if (result.done) {
          const m = result.message.match(/(\d+)×(\d+)/)
          if (m) setRealImageDims({ w: parseInt(m[1]), h: parseInt(m[2]) })
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
      setImageInfo('Applying disc crop…')
      try {
        const result = await DrawDisc({ centerX: end.x, centerY: end.y, radius })
        setPreview(result.preview)
        setImageInfo(`Disc: center=(${end.x},${end.y}) r=${radius} — Y=eyedrop, Arrows=shift, +/-=feather`)
        setDiscActive(true)
      } catch (err) {
        console.error('DrawDisc error:', err)
      } finally {
        setLoading(false)
      }
    }

    // Line draw
    if (mode === 'line') {
      const start = displayToImage(dragStart.x, dragStart.y)
      const end   = displayToImage(pos.x, pos.y)
      const dx = end.x - start.x; const dy = end.y - start.y
      if (Math.sqrt(dx * dx + dy * dy) < 5) return
      try {
        setLines(prev => [...prev, { x1: dragStart.x, y1: dragStart.y, x2: dragCurrent.x, y2: dragCurrent.y }])
        const result   = await AddLine({ x1: start.x, y1: start.y, x2: end.x, y2: end.y })
        const newCount = linesDone + 1
        setLinesDone(newCount)
        setImageInfo(result.message)
        if (newCount >= 4) {
          setLoading(true)
          setImageInfo('Applying perspective correction…')
          const proc = await ProcessLines()
          setPreview(proc.preview)
          setImageInfo('Perspective correction applied')
          setLinesDone(0); setLines([]); setLinesProcessed(true)
          setLoading(false)
        }
      } catch (err) {
        console.error('Line error:', err)
      }
    }

    setDragStart(null); setDragCurrent(null)
  }

  // ── SVG overlay (disc draw circle / line preview) ─────────────────────────
  const renderOverlay = () => {
    if ((mode !== 'disc' && mode !== 'line') || !imgRef.current) return null
    const el = imgRef.current
    const imgStyle = {
      position: 'absolute',
      left: el.offsetLeft + 'px', top: el.offsetTop + 'px',
      width: el.offsetWidth + 'px', height: el.offsetHeight + 'px',
      pointerEvents: 'none', zIndex: 5, overflow: 'visible',
    }

    if (mode === 'disc') {
      if (!dragging || !dragStart || !dragCurrent) return null
      if (ctrlDragRef.current !== null || shiftDragRef.current !== null) return null
      const r = Math.sqrt((dragStart.x - dragCurrent.x) ** 2 + (dragStart.y - dragCurrent.y) ** 2)
      return (
        <svg style={imgStyle}>
          <circle cx={dragCurrent.x} cy={dragCurrent.y} r={r} stroke="#00ff00" strokeWidth="2" fill="none" />
        </svg>
      )
    }

    if (mode === 'line') {
      const allLines = [...lines]
      if (dragging && dragStart && dragCurrent)
        allLines.push({ x1: dragStart.x, y1: dragStart.y, x2: dragCurrent.x, y2: dragCurrent.y })
      return (
        <svg style={imgStyle}>
          {allLines.map((ln, i) => (
            <line key={i} x1={ln.x1} y1={ln.y1} x2={ln.x2} y2={ln.y2}
              stroke="#00ff00" strokeWidth="2" />
          ))}
        </svg>
      )
    }

    return null
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
            case 'y': case 'Y': {
              const imgPt = displayToImage(mousePosRef.current.x, mousePosRef.current.y)
              result = await GetPixelColor({ x: imgPt.x, y: imgPt.y })
              if (result?.preview) setPreview(result.preview)
              if (result?.message) setImageInfo(result.message)
              return
            }
            case 'ArrowUp':    e.preventDefault(); result = await ShiftDisc({ dx: 0, dy: -shiftStep }); if (result?.preview) setPreview(result.preview); return
            case 'ArrowDown':  e.preventDefault(); result = await ShiftDisc({ dx: 0, dy: shiftStep });  if (result?.preview) setPreview(result.preview); return
            case 'ArrowLeft':  e.preventDefault(); result = await ShiftDisc({ dx: -shiftStep, dy: 0 }); if (result?.preview) setPreview(result.preview); return
            case 'ArrowRight': e.preventDefault(); result = await ShiftDisc({ dx: shiftStep, dy: 0 });  if (result?.preview) setPreview(result.preview); return
            case '+': case '=': { const nF = featherSize + 1; setFeatherSize(nF); result = await SetFeatherSize({ size: nF }); if (result?.preview) setPreview(result.preview); return }
            case '-': case '_': { const nF = Math.max(0, featherSize - 1); setFeatherSize(nF); result = await SetFeatherSize({ size: nF }); if (result?.preview) setPreview(result.preview); return }
            case 'e': case 'E': setLoading(true); setImageInfo('Rotating disc…'); result = await RotateDisc({ angle: -15 }); if (result?.preview) setPreview(result.preview); setLoading(false); return
            case 'r': case 'R': setLoading(true); setImageInfo('Rotating disc…'); result = await RotateDisc({ angle:  15 }); if (result?.preview) setPreview(result.preview); setLoading(false); return
            default: break
          }
        }

        switch (e.key.toLowerCase()) {
          case 'w': setLoading(true); setImageInfo('Cropping…'); result = await Crop({ direction: 'top'    }); if (result?.preview) setPreview(result.preview); setZoom(1); setLoading(false); break
          case 'a': setLoading(true); setImageInfo('Cropping…'); result = await Crop({ direction: 'left'   }); if (result?.preview) setPreview(result.preview); setZoom(1); setLoading(false); break
          case 's': setLoading(true); setImageInfo('Cropping…'); result = await Crop({ direction: 'bottom' }); if (result?.preview) setPreview(result.preview); setZoom(1); setLoading(false); break
          case 'd': setLoading(true); setImageInfo('Cropping…'); result = await Crop({ direction: 'right'  }); if (result?.preview) setPreview(result.preview); setZoom(1); setLoading(false); break
          case 'e': setLoading(true); setImageInfo('Rotating…'); result = await Rotate({ flipCode: 0 }); if (result?.preview) setPreview(result.preview); setLoading(false); break
          case 'r': setLoading(true); setImageInfo('Rotating…'); result = await Rotate({ flipCode: 1 }); if (result?.preview) setPreview(result.preview); setLoading(false); break
          case 'q': handleSaveImage(); break
          case 'tab':
            e.preventDefault()
            setLoading(true); setImageInfo('Undoing…')
            result = await Undo()
            if (result?.preview) setPreview(result.preview)
            if (result?.message) setImageInfo(result.message)
            setLoading(false)
            break
        }
      } catch (err) {
        console.error('Shortcut error:', err)
        setImageInfo(err?.message || String(err))
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
      log(`[ZOOM-WHEEL] deltaY=${e.deltaY} ctrlKey=${e.ctrlKey} mode=${mode} discActive=${discActive}`)
      log(`[ZOOM-WHEEL] BEFORE: scrollLeft=${el.scrollLeft} scrollTop=${el.scrollTop} scrollWidth=${el.scrollWidth} scrollHeight=${el.scrollHeight} clientWidth=${el.clientWidth} clientHeight=${el.clientHeight}`)

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
        log(`[ZOOM-SETZOOM] oldZ=${z.toFixed(4)} newZ=${newZ.toFixed(4)}`)
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
      log(`[ZOOM-WHEEL] AFTER setZoom call: scrollLeft=${el.scrollLeft} scrollTop=${el.scrollTop}`)
    }

    const scrollSpy = () => {
      log(`[SCROLL-SPY] scrollLeft=${el.scrollLeft} scrollTop=${el.scrollTop}`)
    }
    el.addEventListener('scroll', scrollSpy, { passive: true })
    el.addEventListener('wheel', handler, { passive: false, capture: true })
    return () => {
      el.removeEventListener('wheel', handler, { capture: true })
      el.removeEventListener('scroll', scrollSpy)
    }
  }, [mode, discActive, featherSize])

  useLayoutEffect(() => {
    const el  = canvasRef.current
    const log = (msg) => LogFrontend(msg).catch(() => {})
    if (pendingScrollRef.current) {
      log(`[ZOOM-LAYOUT] zoom=${zoom.toFixed(4)} BEFORE apply: scrollLeft=${el?.scrollLeft} scrollTop=${el?.scrollTop}`)
      if (el) {
        el.scrollLeft = pendingScrollRef.current.left
        el.scrollTop  = pendingScrollRef.current.top
        log(`[ZOOM-LAYOUT] AFTER apply: scrollLeft=${el.scrollLeft} scrollTop=${el.scrollTop}`)
      }
      pendingScrollRef.current = null
    } else {
      log(`[ZOOM-LAYOUT] zoom=${zoom.toFixed(4)} NO pending scroll. scrollLeft=${el?.scrollLeft} scrollTop=${el?.scrollTop}`)
    }
  }, [zoom])

  // ── Render ─────────────────────────────────────────────────────────────────
  return (
    <div className="app">
      <aside className="sidebar">
        <div className="sidebar-scroll">
        {/* Mode selector */}
        <div className="mode-selector">
          {['corner', 'disc', 'line'].map(m => (
            <button
              key={m}
              className={`mode-btn ${mode === m ? 'active' : ''}`}
              onClick={async () => {
                if (m === mode) return
                if (imageLoaded) {
                  try {
                    if (mode === 'corner') {
                      setCornerState(s => ({ ...s, cornerCount: 0 }))
                      setCornersDetected(false)
                    } else if (mode === 'disc' && discActive) {
                      await ResetDisc(); setDiscActive(false)
                    } else if (mode === 'line' && (linesDone > 0 || linesProcessed)) {
                      await ClearLines(); setLinesDone(0); setLinesProcessed(false)
                    }
                    const res = await GetCleanPreview()
                    if (res?.preview) setPreview(res.preview)
                    if (res?.width && res?.height) setRealImageDims({ w: res.width, h: res.height })
                  } catch (_) {}
                }
                setMode(m)
              }}
            >
              {m.charAt(0).toUpperCase() + m.slice(1)}
            </button>
          ))}
        </div>

        {/* Mode-specific panel */}
        {mode === 'corner' && (
          <CornerPanel
            state={cornerState}        setState={setCornerState}
            dotRadius={dotRadius}      setDotRadius={setDotRadius}
            customCorner={customCorner} setCustomCorner={setCustomCorner}
            cornersDetected={cornersDetected}
            imageLoaded={imageLoaded}
            setPreview={setPreview}
          />
        )}
        {mode === 'disc' && (
          <DiscPanel
            discActive={discActive}
            featherSize={featherSize}  setFeatherSize={setFeatherSize}
            setPreview={setPreview}
          />
        )}
        {mode === 'line' && (
          <LinePanel linesDone={linesDone} />
        )}

        </div>{/* .sidebar-scroll */}
        {/* Bottom section: actions, adjustments, shortcuts, file ops */}
        <div className="sidebar-bottom">
          <div className="sidebar-actions">
            {mode === 'corner' && (
              <button className="primary" onClick={handleDetectCorners} disabled={!imageLoaded || loading}>
                Detect
              </button>
            )}
            {((mode === 'corner' && cornerState.cornerCount > 0) ||
              (mode === 'disc'   && discActive) ||
              (mode === 'line'   && (linesDone > 0 || linesProcessed))) && (
              <button
                className="reset-btn-danger"
                onClick={
                  mode === 'corner' ? handleResetCorners :
                  mode === 'disc'   ? handleResetDisc    :
                                      handleClearLines
                }
              >
                Reset{mode === 'corner' ? ` (${cornerState.cornerCount}/4)` : ''}
              </button>
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
          />

          <ShortcutsPanel
            shortcutsOpen={shortcutsOpen} setShortcutsOpen={setShortcutsOpen}
            mode={mode}
            discActive={discActive}
          />

          <div className="file-ops">
            <button onClick={handleLoadImage} className="load-btn" disabled={loading}>
              {loading ? 'Loading…' : 'Load Image'}
            </button>
            <button onClick={handleSaveImage} className="save-btn" disabled={loading}>
              Save Image
            </button>
          </div>
        </div>
      </aside>

      <main className="main-content">
        <header className="toolbar">
          <span>{imageInfo}</span>
        </header>
        <div
          ref={canvasRef}
          className="canvas-area"
          onMouseDown={handleMouseDown}
          onMouseMove={handleMouseMove}
          onMouseUp={handleMouseUp}
        >
          {loading && (
            <div className={`loading-overlay${loadingFull ? ' opaque' : ''}`}>
              <div className="spinner" />
              <div className="loading-text">{imageInfo}</div>
            </div>
          )}
          {preview ? (
            <img
              ref={imgRef}
              src={preview}
              alt="preview"
              draggable={false}
              onLoad={handleImgLoad}
              style={{
                cursor: 'crosshair',
                ...(fitWidth > 0
                  ? { width: `${fitWidth * zoom}px`, height: 'auto', maxWidth: 'none', maxHeight: 'none' }
                  : { maxWidth: `${zoom * 100}%`, maxHeight: `${zoom * 100}%` }),
              }}
            />
          ) : !loading ? (
            <div className="placeholder">Load or drop an image to begin</div>
          ) : null}
          {renderOverlay()}
        </div>
      </main>
    </div>
  )
}
