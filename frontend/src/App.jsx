import React, { useState, useRef, useEffect, useLayoutEffect, useCallback } from 'react'
import ReactDOM from 'react-dom'
import './App.css'
import { OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime'
import { 
  LoadImage, 
  DetectCorners, 
  ClickCorner,
  ResetCorners,
  SetCornerDotRadius,
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
  AutoContrast,
  SetLevels
} from '../wailsjs/go/main/App'

// DelayedHint: shows a tooltip after hovering for a delay (default 1s), rendered in a portal to avoid clipping
function DelayedHint({ children, hint, delay = 1000, offset = 10 }) {
  const [showHint, setShowHint] = React.useState(false);
  const [hintPos, setHintPos] = React.useState({ top: 0, left: 0 });
  const hintTimeout = React.useRef();
  const childRef = React.useRef();

  // Show tooltip after delay, and measure position
  const handleShow = () => {
    hintTimeout.current = setTimeout(() => {
      if (childRef.current) {
        const rect = childRef.current.getBoundingClientRect();
        setHintPos({
          top: rect.top + rect.height / 2,
          left: rect.right + offset
        });
      }
      setShowHint(true);
    }, delay);
  };
  const handleHide = () => {
    clearTimeout(hintTimeout.current);
    setShowHint(false);
  };

  // Render tooltip in portal
  const tooltip = showHint ? ReactDOM.createPortal(
    <span
      style={{
        position: 'fixed',
        left: hintPos.left,
        top: hintPos.top,
        transform: 'translateY(-50%)',
        background: '#222',
        color: '#fff',
        fontSize: 13,
        borderRadius: 4,
        padding: '4px 10px',
        whiteSpace: 'nowrap',
        zIndex: 9999,
        boxShadow: '0 2px 8px rgba(0,0,0,0.15)'
      }}
    >
      {hint}
    </span>,
    document.body
  ) : null;

  // Clone child to attach ref and handlers
  const child = React.Children.only(children);
  const childWithProps = React.cloneElement(child, {
    ref: childRef,
    onMouseEnter: handleShow,
    onMouseLeave: handleHide,
    onBlur: handleHide,
    tabIndex: child.props.tabIndex || 0 // ensure focusable for keyboard
  });

  return (
    <>
      {childWithProps}
      {tooltip}
    </>
  );
}

export default function App() {
  const [mode, setMode] = useState('corner')
  const [preview, setPreview] = useState(null)
  const [imageInfo, setImageInfo] = useState('No image loaded')
  const [imageLoaded, setImageLoaded] = useState(false)
  const [loading, setLoading] = useState(false)

  // Real image dimensions in Go (full resolution, e.g. 5100x6144)
  const [realImageDims, setRealImageDims] = useState({ w: 1, h: 1 })
  // Natural dimensions of the <img> element (may be downscaled preview)
  const [imgNatural, setImgNatural] = useState({ w: 1, h: 1 })
  const imgRef = useRef(null)

  // Interactive drawing state
  const [dragging, setDragging] = useState(false)
  const [dragStart, setDragStart] = useState(null)   // {x, y} in display coords
  const [dragCurrent, setDragCurrent] = useState(null)
  const [linesDone, setLinesDone] = useState(0)
  // Store all drawn lines in Lines mode: [{x1, y1, x2, y2}, ...]
  const [lines, setLines] = useState([])

  // Corner controls
  const [dotRadius, setDotRadius] = useState(20)
  const [customCorner, setCustomCorner] = useState(false)
  const [cornersDetected, setCornersDetected] = useState(false)

  // Disc mode state
  const [featherSize, setFeatherSizeState] = useState(15)
  const [discActive, setDiscActive] = useState(false)
  const ctrlDragRef = useRef(null) // null when inactive, {lastImg:{x,y}} during Ctrl+drag
  const ctrlDragBusy = useRef(false) // throttle flag for live shift updates
  const shiftDragRef = useRef(null) // null when inactive, {startX, appliedAngle} during Shift+drag
  const shiftDragBusy = useRef(false) // throttle flag for live rotation updates

  // Line mode state — tracks whether perspective correction has been applied
  const [linesProcessed, setLinesProcessed] = useState(false)
  const mousePosRef = useRef({ x: 0, y: 0 }) // display-space mouse pos

  // Collapsible shortcuts panel
  const [shortcutsOpen, setShortcutsOpen] = useState(true)

  // Zoom state
  const [zoom, setZoom] = useState(1)
  const canvasRef = useRef(null)
  const pendingScrollRef = useRef(null) // scroll target after zoom change
  const [fitWidth, setFitWidth] = useState(0) // image display width at zoom=1 (fits container)

  // Adjustment Curve Panel state
  const [adjPanelOpen, setAdjPanelOpen] = useState(false)
  const [autoContrastPending, setAutoContrastPending] = useState(false)
  const [blackPoint, setBlackPoint] = useState(0)
  const [whitePoint, setWhitePoint] = useState(255)

  const applyAutoContrast = async () => {
      setAutoContrastPending(true)
      setLoading(true)
      try {
          const result = await AutoContrast()
          setPreview(result.preview)
          // backend returns the actual computed points
          // (you can expose them in ProcessResult if you want to sync the sliders)
          setBlackPoint(0)   // reset to "neutral" UI state after baking in
          setWhitePoint(255)
      } catch (err) {
          console.error('AutoContrast error:', err)
      } finally {
          setAutoContrastPending(false)
          setLoading(false)
      }
  }

  const applyLevels = async (bp, wp) => {
      setBlackPoint(bp)
      setWhitePoint(wp)
      setLoading(true)
      try {
          const result = await SetLevels({ black: bp, white: wp })
          setPreview(result.preview)
      } catch (err) {
          console.error('SetLevels error:', err)
      } finally {
          setLoading(false)
      }
  }

  // Loading overlay: opaque for image loads, semi-transparent for operations
  const [loadingFull, setLoadingFull] = useState(false)

  const [state, setState] = useState({
    maxCorners: 500,
    qualityLevel: 1,
    minDistance: 100,
    accent: 20,
    cornerCount: 0,
  })

  // --- Drag-and-drop file loading ---
  useEffect(() => {
    OnFileDrop(async (x, y, paths) => {
      if (!paths || paths.length === 0) return
      const filePath = paths[0]
      // Only accept image extensions
      const ext = filePath.split('.').pop().toLowerCase()
      if (!['png','jpg','jpeg','tif','tiff','bmp','gif','webp'].includes(ext)) return
      setLoading(true)
      setLoadingFull(true)
      setZoom(1)
      const name = filePath.split(/[\\/]/).pop()
      setImageInfo(`Loading ${name}…`)
      try {
        const result = await LoadImage({ filePath })
        setImageInfo(`Loaded: ${result.width}x${result.height}`)
        setPreview(result.preview)
        setImageLoaded(true)
        setRealImageDims({ w: result.width, h: result.height })
        setImgNatural({ w: result.width, h: result.height })
        setState(s => ({ ...s, cornerCount: 0 }))
        setLinesDone(0)
        setLinesProcessed(false)
        setDiscActive(false)
        setCornersDetected(false)
        // Auto-detect corners if in corner mode
        if (mode === 'corner') {
          setImageInfo('Detecting corners…')
          const detectResult = await DetectCorners({
            maxCorners: state.maxCorners, qualityLevel: state.qualityLevel,
            minDistance: state.minDistance, accentValue: state.accent, dotRadius
          })
          setPreview(detectResult.preview)
          setImageInfo(detectResult.message + ' — click 4 corners')
          if (detectResult.width && detectResult.height) {
            setRealImageDims({ w: detectResult.width, h: detectResult.height })
          }
          setCornersDetected(true)
          // Ensure dot size matches slider after detection
          try {
            const dotResult = await SetCornerDotRadius({ dotRadius });
            if (dotResult?.preview) setPreview(dotResult.preview);
          } catch (_) {}
        }
      } catch (err) {
        console.error('Drop load error:', err)
        setImageInfo('Load failed — see debug log')
      } finally {
        setLoading(false)
        setLoadingFull(false)
      }
    }, false) // false = accept drops anywhere, not just --wails-drop-target
    return () => OnFileDropOff()
  }, [])

  // --- Launch arguments: load file and set mode from CLI ---
  useEffect(() => {
    (async () => {
      try {
        const args = await GetLaunchArgs()
        if (args.mode) setMode(args.mode)
        if (args.filePath) {
          setLoading(true)
          setLoadingFull(true)
          setZoom(1)
          const name = args.filePath.split(/[\\/]/).pop()
          setImageInfo(`Loading ${name}…`)
          const result = await LoadImage({ filePath: args.filePath })
          setImageInfo(`Loaded: ${result.width}x${result.height}`)
          setPreview(result.preview)
          setImageLoaded(true)
          setRealImageDims({ w: result.width, h: result.height })
          setImgNatural({ w: result.width, h: result.height })
          setState(s => ({ ...s, cornerCount: 0 }))
          setLinesDone(0)
          setLinesProcessed(false)
          setDiscActive(false)
          setCornersDetected(false)
          // Auto-detect corners if in corner mode (or default mode)
          const effectiveMode = args.mode || 'corner'
          if (effectiveMode === 'corner') {
            setImageInfo('Detecting corners…')
            const detectResult = await DetectCorners({
              maxCorners: 500, qualityLevel: 1,
              minDistance: 100, accentValue: 20, dotRadius: 20
            })
            setPreview(detectResult.preview)
            setImageInfo(detectResult.message + ' — click 4 corners')
            if (detectResult.width && detectResult.height) {
              setRealImageDims({ w: detectResult.width, h: detectResult.height })
            }
            setCornersDetected(true)
          }
          setLoading(false)
          setLoadingFull(false)
        }
      } catch (err) {
        console.error('Launch args error:', err)
        setLoading(false)
        setLoadingFull(false)
      }
    })()
  }, [])

  // --- Scroll wheel zoom (or Ctrl+Scroll feather in disc mode) ---
  useEffect(() => {
    const el = canvasRef.current
    if (!el) return
    const log = (msg) => LogFrontend(msg).catch(() => {})
    const handler = async (e) => {
      e.preventDefault()
      e.stopPropagation()
      e.stopImmediatePropagation()
      log(`[ZOOM-WHEEL] deltaY=${e.deltaY} ctrlKey=${e.ctrlKey} mode=${mode} discActive=${discActive}`)
      log(`[ZOOM-WHEEL] BEFORE: scrollLeft=${el.scrollLeft} scrollTop=${el.scrollTop} scrollWidth=${el.scrollWidth} scrollHeight=${el.scrollHeight} clientWidth=${el.clientWidth} clientHeight=${el.clientHeight}`)
      // Ctrl+Scroll in disc mode = feather radius
      if (e.ctrlKey && mode === 'disc' && discActive) {
        const delta = e.deltaY < 0 ? 1 : -1
        const newF = Math.max(0, Math.min(100, featherSize + delta))
        setFeatherSizeState(newF)
        try {
          const result = await SetFeatherSize({ size: newF })
          if (result?.preview) setPreview(result.preview)
        } catch (err) { console.error(err) }
        return
      }
      const factor = e.deltaY < 0 ? 1.1 : 0.9
      setZoom(z => {
        const newZ = Math.min(5, Math.max(0.1, z * factor))
        log(`[ZOOM-SETZOOM] oldZ=${z.toFixed(4)} newZ=${newZ.toFixed(4)} factor=${factor.toFixed(2)} clamped=${newZ === z}`)
        if (newZ === z) {
          log('[ZOOM-SETZOOM] AT LIMIT — returning same z, no scroll change')
          return z
        }
        const rect = el.getBoundingClientRect()
        log(`[ZOOM-SETZOOM] rect: left=${rect.left.toFixed(1)} top=${rect.top.toFixed(1)} width=${rect.width.toFixed(1)} height=${rect.height.toFixed(1)}`)
        log(`[ZOOM-SETZOOM] el.scrollLeft=${el.scrollLeft} el.scrollTop=${el.scrollTop}`)
        log(`[ZOOM-SETZOOM] cursor: clientX=${e.clientX} clientY=${e.clientY}`)
        const cursorX = e.clientX - rect.left + el.scrollLeft
        const cursorY = e.clientY - rect.top + el.scrollTop
        const ratio = newZ / z
        const targetLeft = cursorX * ratio - (e.clientX - rect.left)
        const targetTop = cursorY * ratio - (e.clientY - rect.top)
        log(`[ZOOM-SETZOOM] cursorInContent: x=${cursorX.toFixed(1)} y=${cursorY.toFixed(1)} ratio=${ratio.toFixed(4)}`)
        log(`[ZOOM-SETZOOM] pendingScroll: left=${targetLeft.toFixed(1)} top=${targetTop.toFixed(1)}`)
        pendingScrollRef.current = { left: targetLeft, top: targetTop }
        return newZ
      })
      log(`[ZOOM-WHEEL] AFTER setZoom call: scrollLeft=${el.scrollLeft} scrollTop=${el.scrollTop}`)
    }

    // Also monitor any scroll events on the container
    const scrollSpy = () => {
      log(`[SCROLL-SPY] scrollLeft=${el.scrollLeft} scrollTop=${el.scrollTop} scrollWidth=${el.scrollWidth} scrollHeight=${el.scrollHeight}`)
    }
    el.addEventListener('scroll', scrollSpy, { passive: true })

    // Capture phase ensures we intercept before the container's native scroll
    el.addEventListener('wheel', handler, { passive: false, capture: true })
    return () => {
      el.removeEventListener('wheel', handler, { capture: true })
      el.removeEventListener('scroll', scrollSpy)
    }
  }, [mode, discActive, featherSize])

  // Apply pending scroll position synchronously after zoom DOM update, before paint
  useLayoutEffect(() => {
    const el = canvasRef.current
    const log = (msg) => LogFrontend(msg).catch(() => {})
    if (pendingScrollRef.current) {
      log(`[ZOOM-LAYOUT] zoom=${zoom.toFixed(4)} BEFORE apply: scrollLeft=${el?.scrollLeft} scrollTop=${el?.scrollTop} scrollWidth=${el?.scrollWidth} scrollHeight=${el?.scrollHeight}`)
      if (el) {
        el.scrollLeft = pendingScrollRef.current.left
        el.scrollTop = pendingScrollRef.current.top
        log(`[ZOOM-LAYOUT] AFTER apply: scrollLeft=${el.scrollLeft} scrollTop=${el.scrollTop} (requested left=${pendingScrollRef.current.left.toFixed(1)} top=${pendingScrollRef.current.top.toFixed(1)})`)
      }
      pendingScrollRef.current = null
    } else {
      log(`[ZOOM-LAYOUT] zoom=${zoom.toFixed(4)} NO pending scroll. scrollLeft=${el?.scrollLeft} scrollTop=${el?.scrollTop}`)
    }
  }, [zoom])

  // --- Coordinate helpers ---
  // Convert display-space (relative to img element) → real image-space coords
  const displayToImage = useCallback((dispX, dispY) => {
    const el = imgRef.current
    if (!el) return { x: 0, y: 0 }
    const rect = el.getBoundingClientRect()
    const scaleX = realImageDims.w / rect.width
    const scaleY = realImageDims.h / rect.height
    return {
      x: Math.round(dispX * scaleX),
      y: Math.round(dispY * scaleY),
    }
  }, [realImageDims])

  // Get mouse position relative to img element
  const getRelPos = useCallback((e) => {
    const el = imgRef.current
    if (!el) return null
    const rect = el.getBoundingClientRect()
    return { x: e.clientX - rect.left, y: e.clientY - rect.top }
  }, [])

  // --- Load Image ---
  const handleLoadImage = async () => {
    if (loading) return
    try {
      const filePath = await OpenImageDialog()
      if (!filePath) return

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
      // Reset interaction state
      setState(s => ({ ...s, cornerCount: 0 }))
      setLinesDone(0)
      setDiscActive(false)
      setCornersDetected(false)
      // Auto-detect corners if in corner mode
      if (mode === 'corner') {
        setImageInfo('Detecting corners…')
        const detectResult = await DetectCorners({
          maxCorners: state.maxCorners, qualityLevel: state.qualityLevel,
          minDistance: state.minDistance, accentValue: state.accent, dotRadius
        })
        setPreview(detectResult.preview)
        setImageInfo(detectResult.message + ' — click 4 corners')
        if (detectResult.width && detectResult.height) {
          setRealImageDims({ w: detectResult.width, h: detectResult.height })
        }
        setCornersDetected(true)
      }
    } catch (err) {
      console.error('Load error:', err)
      setImageInfo('Load failed — see debug log')
    } finally {
      setLoading(false)
      setLoadingFull(false)
    }
  }

  // --- Corner Detection ---
  const handleDetectCorners = async () => {
    setLoading(true)
    setImageInfo('Detecting corners…')
    try {
      const result = await DetectCorners({
        maxCorners: state.maxCorners,
        qualityLevel: state.qualityLevel,
        minDistance: state.minDistance,
        accentValue: state.accent,
        dotRadius: dotRadius
      })
      setPreview(result.preview)
      setImageInfo(result.message + ' — click 4 corners')
      if (result.width && result.height) {
        setRealImageDims({ w: result.width, h: result.height })
      }
      setState(s => ({ ...s, cornerCount: 0 }))
      setCornersDetected(true)
    } catch (err) {
      console.error('Detect error:', err)
    } finally {
      setLoading(false)
    }
  }

  // --- Canvas mouse handlers ---
  const handleMouseDown = (e) => {
    if (!imageLoaded || loading) return
    // Ignore clicks on the scrollbar (only act when clicking on the image itself)
    if (e.target !== imgRef.current) return
    e.preventDefault() // prevent browser image drag
    const pos = getRelPos(e)
    if (!pos) return

    if (mode === 'corner') {
      // Corner mode: single click → handled in handleMouseUp
      return
    }

    // Disc mode: Shift+drag = rotate disc
    if (mode === 'disc' && e.shiftKey && discActive) {
      ctrlDragRef.current = null
      shiftDragRef.current = { startX: e.clientX, appliedAngle: 0 }
      shiftDragBusy.current = false
      setDragging(true)
      setDragStart(pos)
      setDragCurrent(pos)
      return
    }

    // Disc mode: Ctrl+drag = shift disc
    if (mode === 'disc' && e.ctrlKey && discActive) {
      shiftDragRef.current = null
      const imgPt = displayToImage(pos.x, pos.y)
      ctrlDragRef.current = { lastImg: imgPt }
      ctrlDragBusy.current = false
      setDragging(true)
      setDragStart(pos)
      setDragCurrent(pos)
      return
    }

    // Disc mode: block regular drag if a disc region already exists (must Reset first)
    if (mode === 'disc' && discActive) return

    // Disc & Line mode: start drag
    ctrlDragRef.current = null
    shiftDragRef.current = null
    setDragging(true)
    setDragStart(pos)
    setDragCurrent(pos)
  }

  const handleMouseMove = async (e) => {
    // Always track mouse position for eyedropper
    const pos = getRelPos(e)
    if (pos) mousePosRef.current = pos
    if (!dragging) return
    if (pos) setDragCurrent(pos)

    // Live update during Shift+drag rotation in disc mode
    if (pos && shiftDragRef.current && !shiftDragBusy.current) {
      const dx = e.clientX - shiftDragRef.current.startX
      const totalAngle = dx * 0.3 // 0.3 degrees per pixel
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

    // Live update during Ctrl+drag in disc mode
    if (pos && ctrlDragRef.current && !ctrlDragBusy.current) {
      const imgPt = displayToImage(pos.x, pos.y)
      const last = ctrlDragRef.current.lastImg
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
    // Only respond to mouseup events on the image itself (not scrollbars or container)
    if (e.target !== imgRef.current) return
    const pos = getRelPos(e)
    if (!pos) return

    if (mode === 'corner') {
      // Corner mode: click → select corner (blocked before detection or after 4 selected)
      if (!cornersDetected || state.cornerCount >= 4) return
      const imgPt = displayToImage(pos.x, pos.y)
      try {
        // 4th corner triggers a perspective warp — show spinner
        if (state.cornerCount === 3) {
          setLoading(true)
          setImageInfo('Applying perspective warp…')
        }
        const result = await ClickCorner({ x: imgPt.x, y: imgPt.y, custom: customCorner, dotRadius: dotRadius })
        setPreview(result.preview)
        setImageInfo(result.message)
        setState(s => ({ ...s, cornerCount: result.count }))
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

    if (!dragging || !dragStart) {
      setDragging(false)
      return
    }
    setDragging(false)

    // Shift+drag in disc mode = apply final residual rotation
    if (mode === 'disc' && shiftDragRef.current && discActive) {
      const dx = e.clientX - shiftDragRef.current.startX
      const totalAngle = dx * 0.3
      const delta = totalAngle - shiftDragRef.current.appliedAngle
      shiftDragRef.current = null
      shiftDragBusy.current = false
      if (Math.abs(delta) >= 0.5) {
        try {
          const result = await RotateDisc({ angle: delta })
          if (result?.preview) setPreview(result.preview)
        } catch (err) {
          console.error('RotateDisc drag error:', err)
        }
      }
      setDragStart(null)
      setDragCurrent(null)
      return
    }

    // Ctrl+drag in disc mode = apply final residual shift
    if (mode === 'disc' && ctrlDragRef.current && discActive) {
      const last = ctrlDragRef.current.lastImg
      const imgPt = displayToImage(pos.x, pos.y)
      const dx = last.x - imgPt.x
      const dy = last.y - imgPt.y
      ctrlDragRef.current = null
      ctrlDragBusy.current = false
      if (Math.abs(dx) >= 1 || Math.abs(dy) >= 1) {
        try {
          const result = await ShiftDisc({ dx, dy })
          if (result?.preview) setPreview(result.preview)
        } catch (err) {
          console.error('ShiftDisc drag error:', err)
        }
      }
      setDragStart(null)
      setDragCurrent(null)
      return
    }

    if (mode === 'disc') {
      // Disc: center = mouseup pos, radius = distance from start to mouseup
      const start = displayToImage(dragStart.x, dragStart.y)
      const end = displayToImage(pos.x, pos.y)
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

    if (mode === 'line') {
      // Line: commit the drawn line
      const start = displayToImage(dragStart.x, dragStart.y)
      const end = displayToImage(pos.x, pos.y)
      const dx = end.x - start.x, dy = end.y - start.y
      if (Math.sqrt(dx * dx + dy * dy) < 5) return
      try {
        // Add to local state for overlay
        setLines(prev => [...prev, { x1: dragStart.x, y1: dragStart.y, x2: dragCurrent.x, y2: dragCurrent.y }])
        const result = await AddLine({ x1: start.x, y1: start.y, x2: end.x, y2: end.y })
        const newCount = linesDone + 1
        setLinesDone(newCount)
        setImageInfo(result.message)
        if (newCount >= 4) {
          // Auto-process
          setLoading(true)
          setImageInfo('Applying perspective correction…')
          const procResult = await ProcessLines()
          setPreview(procResult.preview)
          setImageInfo('Perspective correction applied')
          setLinesDone(0)
          setLines([])
          setLinesProcessed(true)
          setLoading(false)
        }
      } catch (err) {
        console.error('Line error:', err)
      }
    }

    setDragStart(null)
    setDragCurrent(null)
  }

  // --- SVG overlay for live disc/line preview ---
  const renderOverlay = () => {
    // Only overlay in disc/line mode
    if ((mode !== 'disc' && mode !== 'line') || !imgRef.current) return null
    const el = imgRef.current
    const imgStyle = {
      position: 'absolute',
      left: el.offsetLeft + 'px',
      top: el.offsetTop + 'px',
      width: el.offsetWidth + 'px',
      height: el.offsetHeight + 'px',
      pointerEvents: 'none',
      zIndex: 5,
      overflow: 'visible',
    }

    if (mode === 'disc') {
      if (!dragging || !dragStart || !dragCurrent) return null
      // No overlay for Ctrl+drag or Shift+drag disc shift/rotate
      if (ctrlDragRef.current !== null || shiftDragRef.current !== null) return null
      const r = Math.sqrt((dragStart.x - dragCurrent.x) ** 2 + (dragStart.y - dragCurrent.y) ** 2)
      return (
        <svg style={imgStyle}>
          <circle cx={dragCurrent.x} cy={dragCurrent.y} r={r} stroke="#00ff00" strokeWidth="2" fill="none" />
        </svg>
      )
    }
    if (mode === 'line') {
      // Draw all completed lines, plus the live preview if dragging
      const allLines = [...lines]
      if (dragging && dragStart && dragCurrent) {
        allLines.push({ x1: dragStart.x, y1: dragStart.y, x2: dragCurrent.x, y2: dragCurrent.y })
      }
      return (
        <svg style={imgStyle}>
          {allLines.map((ln, i) => (
            <line
              key={i}
              x1={ln.x1} y1={ln.y1}
              x2={ln.x2} y2={ln.y2}
              stroke="#00ff00" strokeWidth="2"
            />
          ))}
        </svg>
      )
    }
    return null
  }

  // Update imgNatural when img loads (for overlay positioning; real dims are tracked separately)
  // Also compute fitWidth: the image display width at zoom=1 that fits inside the container
  const handleImgLoad = () => {
    const el = imgRef.current
    const container = canvasRef.current
    if (el) {
      const natW = el.naturalWidth
      const natH = el.naturalHeight
      setImgNatural({ w: natW, h: natH })
      if (container && natW > 0 && natH > 0) {
        const aspect = natW / natH
        setFitWidth(Math.min(container.clientWidth, container.clientHeight * aspect))
      }
    }
  }

  // Recompute fitWidth when container resizes
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

  // --- Keyboard shortcuts ---
  useEffect(() => {
    const handleKeyDown = async (e) => {
      if (!imageLoaded) return
      
      try {
        let result

        // --- Disc-mode shortcuts ---
        if (mode === 'disc' && discActive) {
          const shiftStep = e.shiftKey ? 20 : 5
          switch (e.key) {
            case 'y':
            case 'Y': {
              // Eyedropper: sample colour under cursor
              const imgPt = displayToImage(mousePosRef.current.x, mousePosRef.current.y)
              result = await GetPixelColor({ x: imgPt.x, y: imgPt.y })
              if (result?.preview) setPreview(result.preview)
              if (result?.message) setImageInfo(result.message)
              return
            }
            case 'ArrowUp':
              e.preventDefault()
              result = await ShiftDisc({ dx: 0, dy: -shiftStep })
              if (result?.preview) setPreview(result.preview)
              return
            case 'ArrowDown':
              e.preventDefault()
              result = await ShiftDisc({ dx: 0, dy: shiftStep })
              if (result?.preview) setPreview(result.preview)
              return
            case 'ArrowLeft':
              e.preventDefault()
              result = await ShiftDisc({ dx: -shiftStep, dy: 0 })
              if (result?.preview) setPreview(result.preview)
              return
            case 'ArrowRight':
              e.preventDefault()
              result = await ShiftDisc({ dx: shiftStep, dy: 0 })
              if (result?.preview) setPreview(result.preview)
              return
            case '+':
            case '=': {
              const newF = featherSize + 1
              setFeatherSizeState(newF)
              result = await SetFeatherSize({ size: newF })
              if (result?.preview) setPreview(result.preview)
              return
            }
            case '-':
            case '_': {
              const newF = Math.max(0, featherSize - 1)
              setFeatherSizeState(newF)
              result = await SetFeatherSize({ size: newF })
              if (result?.preview) setPreview(result.preview)
              return
            }
            case 'e':
            case 'E':
              setLoading(true); setImageInfo('Rotating disc…')
              result = await RotateDisc({ angle: -15 })
              if (result?.preview) setPreview(result.preview)
              setLoading(false)
              return
            case 'r':
            case 'R':
              setLoading(true); setImageInfo('Rotating disc…')
              result = await RotateDisc({ angle: 15 })
              if (result?.preview) setPreview(result.preview)
              setLoading(false)
              return
            default:
              break
          }
        }

        // --- Global shortcuts ---
        switch(e.key.toLowerCase()) {
          case 'w':
            setLoading(true); setImageInfo('Cropping…')
            result = await Crop({ direction: 'top' })
            if (result?.preview) setPreview(result.preview)
            setZoom(1); setLoading(false)
            break
          case 'a':
            setLoading(true); setImageInfo('Cropping…')
            result = await Crop({ direction: 'left' })
            if (result?.preview) setPreview(result.preview)
            setZoom(1); setLoading(false)
            break
          case 's':
            setLoading(true); setImageInfo('Cropping…')
            result = await Crop({ direction: 'bottom' })
            if (result?.preview) setPreview(result.preview)
            setZoom(1); setLoading(false)
            break
          case 'd':
            setLoading(true); setImageInfo('Cropping…')
            result = await Crop({ direction: 'right' })
            if (result?.preview) setPreview(result.preview)
            setZoom(1); setLoading(false)
            break
          case 'e':
            setLoading(true); setImageInfo('Rotating…')
            result = await Rotate({ flipCode: 0 })
            if (result?.preview) setPreview(result.preview)
            setLoading(false)
            break
          case 'r':
            setLoading(true); setImageInfo('Rotating…')
            result = await Rotate({ flipCode: 1 })
            if (result?.preview) setPreview(result.preview)
            setLoading(false)
            break
          case 'q':
            handleSaveImage()
            break
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
  }, [imageLoaded, mode, discActive, featherSize, displayToImage])

  const handleSaveImage = async () => {
    try {
      const filePath = await OpenSaveDialog()
      if (!filePath) return

      const result = await SaveImage({ outputPath: filePath })
      // Avoid double 'Saved to:' if backend already includes it
      if (result.message && result.message.startsWith('Saved to:')) {
        setImageInfo(result.message)
      } else {
        setImageInfo(`Saved to: ${result.message}`)
      }
    } catch (err) {
      console.error('Save error:', err)
    }
  }

  // --- Reset handlers ---
  const handleResetCorners = async () => {
    setLoading(true)
    setImageInfo('Resetting corners…')
    try {
      const result = await ResetCorners()
      setPreview(result.preview)
      setImageInfo(result.message)
      if (result.width && result.height) {
        setRealImageDims({ w: result.width, h: result.height })
      }
      setState(s => ({ ...s, cornerCount: 0 }))
    } catch (err) {
      console.error('ResetCorners error:', err)
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
      if (result?.width && result?.height) {
        setRealImageDims({ w: result.width, h: result.height })
      }
      setImageInfo(result?.message || 'Lines cleared')
    } catch (err) {
      console.error('ClearLines error:', err)
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
      if (result?.width && result?.height) {
        setRealImageDims({ w: result.width, h: result.height })
      }
      setDiscActive(false)
      setImageInfo(result?.message || 'Disc selection reset')
    } catch (err) {
      console.error('ResetDisc error:', err)
    } finally {
      setLoading(false)
    }
  }



  return (
    <div className="app">
      <aside className="sidebar">
        <div className="mode-selector">
          {['corner', 'disc', 'line'].map(m => (
            <button
              key={m}
              className={`mode-btn ${mode === m ? 'active' : ''}`}
              onClick={async () => {
                if (m === mode) return
                if (imageLoaded) {
                  try {
                    // Reset the departing mode's selection
                    if (mode === 'corner') {
                      setState(s => ({ ...s, cornerCount: 0 }))
                      setCornersDetected(false)
                    } else if (mode === 'disc' && discActive) {
                      await ResetDisc()
                      setDiscActive(false)
                    } else if (mode === 'line' && (linesDone > 0 || linesProcessed)) {
                      await ClearLines()
                      setLinesDone(0)
                      setLinesProcessed(false)
                    }
                    // Get a clean preview without any overlay
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

        {mode === 'corner' && (
          <div className="control-section">
            <div className="control-group">
              <label>Max Corners</label>
              <div className="slider-row">
                <input
                  type="range"
                  min="1"
                  max="1000"
                  value={state.maxCorners}
                  onChange={(e) => setState({...state, maxCorners: parseInt(e.target.value)})}
                />
                <span className="value-display">{state.maxCorners}</span>
              </div>
            </div>

            <div className="control-group">
              <label>Quality Level</label>
              <div className="slider-row">
                <input
                  type="range"
                  min="1"
                  max="100"
                  value={state.qualityLevel}
                  onChange={(e) => setState({...state, qualityLevel: parseFloat(e.target.value)})}
                />
                <span className="value-display">{state.qualityLevel}</span>
              </div>
            </div>

            <div className="control-group">
              <label>Min Distance</label>
              <div className="slider-row">
                <input
                  type="range"
                  min="1"
                  max="200"
                  value={state.minDistance}
                  onChange={(e) => setState({...state, minDistance: parseInt(e.target.value)})}
                />
                <span className="value-display">{state.minDistance}</span>
              </div>
            </div>

            <div className="control-group">
              <label>Accent</label>
              <div className="slider-row">
                <input
                  type="range"
                  min="0"
                  max="30"
                  value={state.accent}
                  onChange={(e) => setState({...state, accent: parseInt(e.target.value)})}
                />
                <span className="value-display">{state.accent}</span>
              </div>
            </div>

            <div className="control-group">
              <label>Corner Dot Size</label>
              <div className="slider-row">
                <input
                  type="range"
                  min="2"
                  max="80"
                  value={dotRadius}
                  onChange={(e) => setDotRadius(parseInt(e.target.value))}
                  onMouseUp={async (e) => {
                    const newR = parseInt(e.target.value)
                    if (cornersDetected && imageLoaded) {
                      try {
                        const result = await SetCornerDotRadius({ dotRadius: newR })
                        if (result?.preview) setPreview(result.preview)
                      } catch (_) {}
                    }
                  }}
                />
                <span className="value-display">{dotRadius}</span>
              </div>
            </div>

            <div className="control-group" style={{ marginTop: 40 }}>
              <label className="checkbox-label">
                <input
                  type="checkbox"
                  checked={customCorner}
                  onChange={(e) => setCustomCorner(e.target.checked)}
                />
                Custom corner placement
              </label>
              <div className="hint">When enabled, click anywhere to place a corner instead of snapping to detected corners.</div>
            </div>
          </div>
        )}

        {mode === 'disc' && (
          <div className="control-section">
            <div className="info-box">Click and drag on the image to draw a circle around the disc.</div>
            {discActive && (
              <div className="control-group">
                <label>Feather Size</label>
                <div className="slider-row">
                  <input
                    type="range"
                    min="0"
                    max="100"
                    value={featherSize}
                    onChange={(e) => setFeatherSizeState(parseInt(e.target.value))}
                    onMouseUp={async (e) => {
                      try {
                        const result = await SetFeatherSize({ size: parseInt(e.target.value) })
                        if (result?.preview) setPreview(result.preview)
                      } catch (err) { console.error(err) }
                    }}
                  />
                  <span className="value-display">{featherSize}</span>
                </div>
              </div>
            )}
          </div>
        )}

        {mode === 'line' && (
          <div className="control-section">
            <div className="info-box">Click and drag to draw 4 lines defining the document edges. Perspective correction is applied automatically after the 4th line.</div>
            <div className="control-group">
              <div className="slider-row">
                <label style={{ margin: 0 }}>Lines Drawn</label>
                <span className="value-display">{linesDone}/4</span>
              </div>
            </div>
          </div>
        )}

        <div className="sidebar-bottom">
          <div className="sidebar-actions">
            {mode === 'corner' && (
              <button onClick={handleDetectCorners} className="primary" disabled={!imageLoaded || loading}>
                Detect
              </button>
            )}
            {/* Unified Reset — always in the same spot, just above shortcuts */}
            {(
              (mode === 'corner' && state.cornerCount > 0) ||
              (mode === 'disc' && discActive) ||
              (mode === 'line' && (linesDone > 0 || linesProcessed))
            ) && (
              <button
                className="reset-btn-danger"
                onClick={mode === 'corner' ? handleResetCorners : mode === 'disc' ? handleResetDisc : handleClearLines}
              >
                Reset{mode === 'corner' ? ` (${state.cornerCount}/4)` : ''}
              </button>
            )}
          </div>
          {/* === Adjustment Curve Panel (styled like shortcuts panel) === */}
          <div className="keyboard-shortcuts adj-panel">
            <div className="shortcut-title adj-panel-header" onClick={() => setAdjPanelOpen(o => !o)} style={{ cursor: 'pointer', userSelect: 'none' }}>
              Adjustments <span className="shortcut-toggle">{adjPanelOpen ? '▾' : '▸'}</span>
            </div>
            {adjPanelOpen && (
              <>
                <div className="shortcut-item" style={{ marginBottom: 10, position: 'relative' }}>
                  <DelayedHint hint="Sets black/white points to min/max luma">
                    <button
                      className="primary"
                      style={{ minWidth: 120 }}
                      onClick={applyAutoContrast}
                      disabled={autoContrastPending || !imageLoaded}
                    >
                      {autoContrastPending ? 'Auto Contrast…' : 'Auto Contrast'}
                    </button>
                  </DelayedHint>
                </div>
                <div className="shortcut-item" style={{ marginBottom: 10 }}>
                  <label style={{ fontWeight: 500 }}>Black Point</label>
                  <input
                    type="range"
                    min="0"
                    max={whitePoint - 1}
                    value={blackPoint}
                    onChange={e => applyLevels(Number(e.target.value), whitePoint)}
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
                    onChange={e => applyLevels(blackPoint, Number(e.target.value))}
                    style={{ width: 120, marginLeft: 8 }}
                    disabled={!imageLoaded}
                  />
                  <span style={{ marginLeft: 8 }}>{whitePoint}</span>
                </div>
              </>
            )}
          </div>
          <div className="keyboard-shortcuts">
            <div className="shortcut-title" onClick={() => setShortcutsOpen(s => !s)} style={{ cursor: 'pointer', userSelect: 'none' }}>
              Shortcuts <span className="shortcut-toggle">{shortcutsOpen ? '▾' : '▸'}</span>
            </div>
            {shortcutsOpen && (
              <>
                <div className="shortcut-item"><kbd>W</kbd><kbd>A</kbd><kbd>S</kbd><kbd>D</kbd> Crop edges</div>
                <div className="shortcut-item"><kbd>E</kbd><kbd>R</kbd> Rotate {mode === 'disc' ? '±15°' : '±90°'}</div>
                <div className="shortcut-item"><kbd>Tab</kbd> Undo</div>
                <div className="shortcut-item"><kbd>Q</kbd> Save</div>
                {mode === 'disc' && discActive && (
                  <>
                    <div className="shortcut-divider" />
                    <div className="shortcut-item"><kbd>Y</kbd> Eyedrop background</div>
                    <div className="shortcut-item"><kbd>←</kbd><kbd>↑</kbd><kbd>→</kbd><kbd>↓</kbd> Shift disc</div>
                    <div className="shortcut-item"><kbd>Ctrl</kbd>+<kbd>Drag</kbd> Shift disc</div>
                    <div className="shortcut-item"><kbd>Shift</kbd>+<kbd>Drag</kbd> Rotate disc</div>
                    <div className="shortcut-item"><kbd>+</kbd>/<kbd>-</kbd> Feather radius</div>
                    <div className="shortcut-item"><kbd>Ctrl</kbd>+<kbd>Scroll</kbd> Feather radius</div>
                  </>
                )}
              </>
            )}
          </div>

          <div className="file-ops">
            <button onClick={handleLoadImage} className="load-btn" disabled={loading}>
              {loading ? 'Loading…' : 'Load Image'}
            </button>
            <button onClick={handleSaveImage} className="save-btn" disabled={loading}>Save Image</button>
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
