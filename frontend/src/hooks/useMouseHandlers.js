import { useRef, useCallback, useEffect } from 'react'
import {
  ClickCorner, StraightEdgeRotate, RotateDisc, ShiftDisc, DrawDisc, AddLine, ProcessLines,
} from '../../wailsjs/go/main/App'

export function useMouseHandlers({
  imageLoaded, loading, mode, dragging, dragStart, dragCurrent,
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
}) {
  const cornerMouseDownRef   = useRef(false)
  const ctrlDragBusy         = useRef(false)
  const shiftDragBusy        = useRef(false)
  const normalDragPendingRef = useRef(false) // mousedown outside image in Normal mode — waiting to enter bounds
  const normalDragActiveRef  = useRef(false) // drag started this frame; bridges the gap before React re-renders dragging=true
  const mouseUpHandledRef    = useRef(false) // set by canvas handler to suppress window handler double-fire

  // Refs that shadow state/callback values so the window mouseup listener
  // can read current values without being re-registered on every render.
  const draggingRef        = useRef(dragging)
  const dragStartRef       = useRef(dragStart)
  const dragCurrentRef     = useRef(dragCurrent)
  const displayToImageRef  = useRef(null)
  draggingRef.current      = dragging
  dragStartRef.current     = dragStart
  dragCurrentRef.current   = dragCurrent

  const displayToImage = displayToImageRef.current = useCallback((dispX, dispY) => {
    const el = imgRef.current
    if (!el) return { x: 0, y: 0 }
    const rect = el.getBoundingClientRect()
    return {
      x: Math.round(dispX * (realImageDims.w / rect.width)),
      y: Math.round(dispY * (realImageDims.h / rect.height)),
    }
  }, [realImageDims]) // eslint-disable-line react-hooks/exhaustive-deps

  const getRelPos = useCallback((e) => {
    const el = imgRef.current
    if (!el) return null
    const rect = el.getBoundingClientRect()
    return { x: e.clientX - rect.left, y: e.clientY - rect.top }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const handleMouseDown = (e) => {
    if (!imageLoaded || loading) return
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
    if (e.target !== imgRef.current) {
      if (mode === 'normal' && !useTouchupTool) {
        e.preventDefault()
        normalDragPendingRef.current = true
      }
      return
    }
    e.preventDefault()
    const pos = getRelPos(e)
    if (!pos) return

    if (mode === 'corner' && !useTouchupTool) { cornerMouseDownRef.current = true; return }

    if (useTouchupTool) {
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
    if (mode === 'disc' && discActive) return
    if (mode === 'line' && linesProcessed) return

    ctrlDragRef.current = null
    shiftDragRef.current = null
    setDragging(true); setDragStart(pos); setDragCurrent(pos)
  }

  const handleMouseMove = async (e) => {
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

    const imgRect = imgRef.current ? imgRef.current.getBoundingClientRect() : null
    const insideImage = pos && imgRect &&
      pos.x >= 0 && pos.y >= 0 && pos.x <= imgRect.width && pos.y <= imgRect.height

    if (normalDragPendingRef.current) {
      if (mode === 'normal' && !useTouchupTool && insideImage) {
        normalDragPendingRef.current = false
        normalDragActiveRef.current = true
        ctrlDragRef.current = null
        shiftDragRef.current = null
        setDragging(true); setDragStart(pos); setDragCurrent(pos)
      }
      return
    }

    if (!dragging && !normalDragActiveRef.current) return
    if (pos) {
      if (mode === 'normal' && !useTouchupTool && imgRect) {
        setDragCurrent({
          x: Math.max(0, Math.min(pos.x, imgRect.width)),
          y: Math.max(0, Math.min(pos.y, imgRect.height)),
        })
      } else {
        setDragCurrent(pos)
      }
    }

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

    if (normalDragPendingRef.current) {
      normalDragPendingRef.current = false
      if (mode === 'normal' && !useTouchupTool) { mouseUpHandledRef.current = true; setNormalRect(null) }
      return
    }

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

    if (mode === 'normal' && !useTouchupTool) {
      mouseUpHandledRef.current = true
      normalDragActiveRef.current = false
      if (!dragging || !dragStart || !dragCurrent) { setDragging(false); return }
      setDragging(false)
      const start = displayToImage(dragStart.x, dragStart.y)
      const end   = displayToImage(dragCurrent.x, dragCurrent.y)
      const w = Math.abs(end.x - start.x)
      const h = Math.abs(end.y - start.y)
      if (w >= 5 && h >= 5) {
        setNormalRect({ x1: start.x, y1: start.y, x2: end.x, y2: end.y })
      } else {
        setNormalRect(null)
      }
      setDragStart(null); setDragCurrent(null)
      return
    }

    if (e.target !== imgRef.current) return
    const pos = getRelPos(e)
    if (!pos) return

    if (mode === 'corner' && !useTouchupTool) {
      const hadMouseDown = cornerMouseDownRef.current
      cornerMouseDownRef.current = false
      if (!hadMouseDown) return
      if (Date.now() - lastResizeRef.current < 300) return
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

    if (useStraightEdgeTool && mode === 'disc' && discActive) {
      const dx = pos.x - dragStart.x
      const dy = pos.y - dragStart.y
      if (Math.hypot(dx, dy) >= 5) {
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

    if (mode === 'disc') {
      const start  = displayToImage(dragStart.x, dragStart.y)
      const end    = displayToImage(pos.x, pos.y)
      const radius = Math.round(Math.hypot(start.x - end.x, start.y - end.y))
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

    if (mode === 'line' && !linesProcessed) {
      const start = displayToImage(dragStart.x, dragStart.y)
      const end   = displayToImage(pos.x, pos.y)
      const dx = end.x - start.x; const dy = end.y - start.y
      if (Math.hypot(dx, dy) < 5) return
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

  // Catch mouseup events that fire outside the canvas-area (sidebar, outside window, etc.)
  // so that a Normal mode drag is never left stuck in an active state.
  useEffect(() => {
    const onWindowMouseUp = () => {
      if (mouseUpHandledRef.current) { mouseUpHandledRef.current = false; return }
      if (normalDragPendingRef.current) {
        normalDragPendingRef.current = false
        setNormalRect(null)
        return
      }
      if (!draggingRef.current) return
      normalDragActiveRef.current = false
      setDragging(false)
      const ds = dragStartRef.current
      const dc = dragCurrentRef.current
      if (ds && dc) {
        const d2i = displayToImageRef.current
        const start = d2i(ds.x, ds.y)
        const end   = d2i(dc.x, dc.y)
        const w = Math.abs(end.x - start.x)
        const h = Math.abs(end.y - start.y)
        if (w >= 5 && h >= 5) {
          setNormalRect({ x1: start.x, y1: start.y, x2: end.x, y2: end.y })
        } else {
          setNormalRect(null)
        }
      } else {
        setNormalRect(null)
      }
      setDragStart(null)
      setDragCurrent(null)
    }
    window.addEventListener('mouseup', onWindowMouseUp)
    return () => window.removeEventListener('mouseup', onWindowMouseUp)
  }, [mode, useTouchupTool]) // eslint-disable-line react-hooks/exhaustive-deps

  return { handleMouseDown, handleMouseMove, handleMouseUp, handleImageMouseLeave, displayToImage }
}
