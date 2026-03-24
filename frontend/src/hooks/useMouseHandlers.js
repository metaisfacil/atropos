import { useRef, useCallback, useEffect } from 'react'
import {
  ClickCorner, StraightEdgeRotate, RotateDisc, ShiftDisc, DrawDisc, AddLine, ProcessLines,
} from '../../wailsjs/go/main/App'
import { displayToImage as displayToImageHelper, computeDiscShift as computeDiscShiftHelper } from '../utils/imageCoords'
import { sanitizeArg, debugOptions } from '../utils/debugLogger'

export function useMouseHandlers({
  imageLoaded, loading, mode, dragging, dragStart, dragCurrent,
  useTouchupTool, useStraightEdgeTool, discActive, linesProcessed,
  touchupStrokes, cornerState, dotRadius, cornersDetected, customCorner, linesDone,
  realImageDims, discNoMaskPreview, discCenter, discRadius, discRotation,
  setDragging, setDragStart, setDragCurrent, setTouchupStrokes, setPreview,
  setLoading, setZoom, setRealImageDims, setCornerState, setDetectedCornerPts,
  setSelectedCornerPts, setDiscActive, setDiscNoMaskPreview, setDiscCenter, setDiscRadius,
  setDiscRotation, setDiscBgColor, setNormalRect, setLines, setLinesDone, setUnsavedChanges,
  setDiscLiveActive, setDiscLiveTransform, setLinesProcessed, setUseStraightEdgeTool,
  straightEdgeRemainsActive, spaceDownRef, panDragRef, canvasRef, ctrlDragRef, shiftDragRef,
  touchupDraggingRef, imgRef, lastResizeRef, mousePosRef,
  commitTouchup, showStatus, showError,
}) {

  // Frontend -> image coordinate mapping and Normal-mode drag rules
  //
  // `displayToImage(dispX, dispY)` maps display-space coordinates (pixels
  // relative to the <img> element) into full-resolution image-space using
  // `realImageDims` supplied by the backend. Formula:
  //   imgRect = imgRef.current.getBoundingClientRect()
  //   imageX = round(dispX * (realImageDims.w / imgRect.width))
  //   imageY = round(dispY * (realImageDims.h / imgRect.height))
  //
  // Normal-mode drag behaviour:
  // - Mousedown outside the image (on canvas wrapper) sets `normalDragPendingRef`.
  //   The pending state transitions to an active drag when the cursor first
  //   enters the image, at which point the first mousemove inside the image
  //   updates the selection immediately. This prevents the browser's native
  //   drag gesture from interfering and ensures the selection rectangle snaps
  //   precisely to the pixel where the cursor entered.
  const cornerMouseDownRef   = useRef(false)
  const ctrlDragBusy         = useRef(false)
  const shiftDragBusy        = useRef(false)
  const normalDragPendingRef = useRef(false) // mousedown outside image in Normal mode — waiting to enter bounds
  const normalDragActiveRef  = useRef(false) // drag started this frame; bridges the gap before React re-renders dragging=true
  const mouseUpHandledRef    = useRef(false) // set by canvas handler to suppress window handler double-fire

  const clamp = (value, min, max) => Math.min(Math.max(value, min), max)

  const computeDiscShift = (screenDx, screenDy) => {
    const el = imgRef.current
    if (!el || realImageDims.w <= 0 || realImageDims.h <= 0 || discRadius <= 0) {
      return null
    }

    // Use the <img> element's natural (intrinsic) pixel dimensions for the
    // scale factor.  After DrawDisc the displayed image is the disc crop
    // (much smaller than the source), while realImageDims still holds the
    // source dimensions.  naturalWidth/Height always matches the currently
    // displayed image, so the display-to-image scale is correct.
    const clientW = el.clientWidth
    const clientH = el.clientHeight
    if (clientW <= 0 || clientH <= 0) return null

    const natW = el.naturalWidth  || realImageDims.w
    const natH = el.naturalHeight || realImageDims.h
    const scaleX = natW / clientW
    const scaleY = natH / clientH

    // Map the pointer movement from display space into (possibly rotated)
    // disc-local image space, with inverted drag direction for working output.
    const angleRad = discRotation * Math.PI / 180
    const cos = Math.cos(angleRad)
    const sin = Math.sin(angleRad)

    // Use screen deltas directly in image-space to keep final backend shift proportional
    // to the visual pointer movement, avoiding double-scale drift.
    const desiredImgDx = -(screenDx * cos + screenDy * sin)
    const desiredImgDy = -(-screenDx * sin + screenDy * cos)

    const startCenter = ctrlDragRef.current?.startCenter || discCenter || { x: 0, y: 0 }
    const minCenterX = discRadius
    const maxCenterX = Math.max(discRadius, realImageDims.w - discRadius)
    const minCenterY = discRadius
    const maxCenterY = Math.max(discRadius, realImageDims.h - discRadius)

    const clampedCenterX = clamp(startCenter.x + desiredImgDx, minCenterX, maxCenterX)
    const clampedCenterY = clamp(startCenter.y + desiredImgDy, minCenterY, maxCenterY)

    const appliedImgDx = clampedCenterX - startCenter.x
    const appliedImgDy = clampedCenterY - startCenter.y

    // Apply integer shift to match backend ShiftDisc behavior.
    const roundedImgDx = Math.round(appliedImgDx)
    const roundedImgDy = Math.round(appliedImgDy)

    // Map the rounded image-space shift into screen-space (inverse drag) for live preview.
    // The preview transformation should exactly match the dragged cursor movement
    // in display coordinates, so that the final rendered output does not jump.
    const liveDx = screenDx
    const liveDy = screenDy


    return {
      dx: roundedImgDx,
      dy: roundedImgDy,
      liveDx,
      liveDy,
    }
  }

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
    return displayToImageHelper(dispX, dispY, realImageDims, el.clientWidth, el.clientHeight)
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
      setDiscLiveActive(true)
      setDiscLiveTransform({ dx: 0, dy: 0, angle: 0 })
      if (discNoMaskPreview) setPreview(discNoMaskPreview)
      return
    }
    if (mode === 'disc' && e.ctrlKey && discActive) {
      shiftDragRef.current = null
      const point = displayToImage(pos.x, pos.y)
      ctrlDragRef.current = {
        startImg: point,
        startClientX: e.clientX,
        startClientY: e.clientY,
        startCenter: discCenter,
      }
      ctrlDragBusy.current = false
      setDragging(true); setDragStart(pos); setDragCurrent(pos)
      setDiscLiveActive(true)
      setDiscLiveTransform({ dx: 0, dy: 0, angle: 0 })
      if (discNoMaskPreview) setPreview(discNoMaskPreview)
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

    if (pos && shiftDragRef.current) {
      const dx = e.clientX - shiftDragRef.current.startX
      const totalAngle = dx * 0.3
      setDiscLiveTransform(prev => ({ ...prev, angle: totalAngle }))
      return
    }

    if (ctrlDragRef.current) {
      const screenDx = e.clientX - ctrlDragRef.current.startClientX
      const screenDy = e.clientY - ctrlDragRef.current.startClientY

      const el = imgRef.current
      // Use the live natural dimensions for scale in disc mode (preview image may be cropped),
      // not the full source realImageDims.
      // DON’T use realImageDims for live scale in disc mode; pass naturalImageDims explicitly.
      const natural = { w: el?.naturalWidth || realImageDims.w, h: el?.naturalHeight || realImageDims.h }
      const shift = computeDiscShiftHelper(screenDx, screenDy, realImageDims, discRadius, discRotation, discCenter, ctrlDragRef.current.startCenter, el?.clientWidth || 0, el?.clientHeight || 0, natural)
      if (shift) {
        setDiscLiveTransform(prev => ({ ...prev, dx: shift.liveDx, dy: shift.liveDy }))
        if (debugOptions.verbose) {
          console.debug('Drag translation', { screenDx, screenDy, shiftDx: shift.dx, shiftDy: shift.dy, liveDx: shift.liveDx, liveDy: shift.liveDy })
        }
      }
      return
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
      if (!straightEdgeRemainsActive) setUseStraightEdgeTool(false)
      setDragStart(null); setDragCurrent(null)
      return
    }

    if (mode === 'disc' && shiftDragRef.current && discActive) {
      mouseUpHandledRef.current = true
      const dx = e.clientX - shiftDragRef.current.startX
      const totalAngle = dx * 0.3
      shiftDragRef.current = null; shiftDragBusy.current = false
      setLoading(true)
      showStatus('Applying disc rotation…')
      try {
        if (Math.abs(totalAngle) >= 0.5) {
          const result = await RotateDisc({ angle: totalAngle })
          if (result?.preview) setPreview(result.preview)
          if (result?.unmaskedPreview) setDiscNoMaskPreview(result.unmaskedPreview)
          if (result?.discCenterX !== undefined && result?.discCenterY !== undefined) {
            setDiscCenter({ x: result.discCenterX, y: result.discCenterY })
          }
          if (result?.discRadius !== undefined) setDiscRadius(result.discRadius)
          setDiscRotation(result?.discRotation ?? discRotation)
          if (result?.discBgR !== undefined) {
            setDiscBgColor({ r: result.discBgR, g: result.discBgG, b: result.discBgB })
          }
        }
      } catch (err) {
        console.error('RotateDisc drag error:', err)
      } finally {
        setDiscLiveActive(false)
        setDiscLiveTransform({ dx: 0, dy: 0, angle: 0 })
        setLoading(false)
        showStatus('')
      }
      setDragStart(null); setDragCurrent(null)
      return
    }

    if (mode === 'disc' && ctrlDragRef.current && discActive) {
      mouseUpHandledRef.current = true
      const screenDx = e.clientX - ctrlDragRef.current.startClientX
      const screenDy = e.clientY - ctrlDragRef.current.startClientY

      const el = imgRef.current
      // Use the live natural dimensions for scale in disc mode (preview image may be cropped),
      // not the full source realImageDims.
      // DON’T use realImageDims for live scale in disc mode; pass naturalImageDims explicitly.
      const natural = { w: el?.naturalWidth || realImageDims.w, h: el?.naturalHeight || realImageDims.h }
      const shift = computeDiscShiftHelper(screenDx, screenDy, realImageDims, discRadius, discRotation, discCenter, ctrlDragRef.current.startCenter, el?.clientWidth || 0, el?.clientHeight || 0, natural)
      let dx = 0
      let dy = 0
      if (shift) {
        dx = shift.dx
        dy = shift.dy
      }

      ctrlDragRef.current = null; ctrlDragBusy.current = false
      setLoading(true)
      showStatus('Applying disc shift…')
      try {
        if (Math.abs(dx) >= 1 || Math.abs(dy) >= 1) {
          const result = await ShiftDisc({ dx, dy })
          if (result?.preview) setPreview(result.preview)
          if (result?.unmaskedPreview) setDiscNoMaskPreview(result.unmaskedPreview)
          if (result?.discCenterX !== undefined && result?.discCenterY !== undefined) {
            setDiscCenter({ x: result.discCenterX, y: result.discCenterY })
          }
          if (result?.discRadius !== undefined) setDiscRadius(result.discRadius)
          if (result?.discRotation !== undefined) setDiscRotation(result.discRotation)
          if (result?.discBgR !== undefined) {
            setDiscBgColor({ r: result.discBgR, g: result.discBgG, b: result.discBgB })
          }
        }
      } catch (err) {
        console.error('ShiftDisc drag error:', err)
      } finally {
        setDiscLiveActive(false)
        setDiscLiveTransform({ dx: 0, dy: 0, angle: 0 })
        setLoading(false)
        showStatus('')
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
        if (result?.unmaskedPreview) setDiscNoMaskPreview(result.unmaskedPreview)
        setDiscRotation(result?.discRotation ?? 0)
        setDiscCenter({ x: end.x, y: end.y })
        setDiscRadius(radius)
        if (result?.discBgR !== undefined) {
          setDiscBgColor({ r: result.discBgR, g: result.discBgG, b: result.discBgB })
        }
        showStatus(`Disc: center=(${end.x},${end.y}) r=${radius} — Y=eyedrop, Arrows=shift, +/-=feather`)
        setDiscActive(true)
        if (setUnsavedChanges) setUnsavedChanges(true)
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

  const handleImageMouseLeave = () => {}

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
      if (mode === 'disc') {
        // If mouseup happened outside the image, ensure drag state is cleared.
        if (ctrlDragRef.current || shiftDragRef.current) {
          ctrlDragRef.current = null
          shiftDragRef.current = null
        }
        setDragging(false)
        normalDragActiveRef.current = false
        setDiscLiveActive(false)
        setDiscLiveTransform({ dx: 0, dy: 0, angle: 0 })
        setDragStart(null)
        setDragCurrent(null)
        return
      }
      if (!draggingRef.current) {
        setDiscLiveActive(false)
        setDiscLiveTransform({ dx: 0, dy: 0, angle: 0 })
        return
      }
      normalDragActiveRef.current = false
      setDragging(false)
      setDiscLiveActive(false)
      setDiscLiveTransform({ dx: 0, dy: 0, angle: 0 })
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
    const onWindowMouseMove = (e) => {
      if (!draggingRef.current) return
      const el = imgRef.current
      if (!el) return
      const rect = el.getBoundingClientRect()
      setDragCurrent({ x: e.clientX - rect.left, y: e.clientY - rect.top })
    }
    window.addEventListener('mouseup', onWindowMouseUp)
    window.addEventListener('mousemove', onWindowMouseMove)
    return () => {
      window.removeEventListener('mouseup', onWindowMouseUp)
      window.removeEventListener('mousemove', onWindowMouseMove)
    }
  }, [mode, useTouchupTool]) // eslint-disable-line react-hooks/exhaustive-deps

  return { handleMouseDown, handleMouseMove, handleMouseUp, handleImageMouseLeave, displayToImage }
}
