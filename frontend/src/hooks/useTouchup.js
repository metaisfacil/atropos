import { useState, useRef, useEffect } from 'react'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'

export function useTouchup({
  imageLoaded, loading, setLoading, showStatus,
  realImageDims, touchupBackend, setErrorMessage, setPreview, onDragEnd,
  flushPendingSaveRef,
  touchupRemainsActive, setUseTouchupTool,
  setUnsavedChanges,
  touchupDraggingRef,
}) {
  const [touchupStrokes, setTouchupStrokes] = useState([])
  const [brushSize, setBrushSize]           = useState(40)
  const touchupDraggingRefLocal = touchupDraggingRef || useRef(false) // true while a touch-up brush drag is in progress
  // Holds the latest touch-up commit handler for the window-level mouseup listener.
  // Updated every render so the closure always sees fresh state.
  const windowTouchupMouseUpRef = useRef(null)
  // Holds the latest "touchup-done" event handler. Updated every render so the
  // closure always sees current state (preview, error helpers, etc.).
  const touchupDoneHandlerRef   = useRef(null)

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
      // TouchUpApply returns immediately after launching the fill goroutine.
      // The result (preview, error, or cancellation) arrives via the
      // "touchup-done" event handled by touchupDoneHandlerRef. setLoading(false)
      // is therefore NOT called here — the event handler does it.
      await window['go']['main']['App']['TouchUpApply'](b64, patchSize, iterations)
      setTouchupStrokes([])
    } catch (err) {
      // Immediate errors (no image, mask decode failure, canvas issues).
      console.error('TouchUp commit error:', err)
      showStatus('')
      const hint = touchupBackend === 'iopaint'
        ? '\n\nPlease make sure IOPaint is running and that you have the server address configured correctly. Alternatively, try switching to the PatchMatch backend in Options.'
        : ''
      setErrorMessage('Failed to inpaint.' + hint + '\n\n' + (err?.message || String(err)))
      setTouchupStrokes([])
      setLoading(false)
    }
  }

  // ── Touch-up: catch mouseup outside the canvas div ────────────────────────
  // Updated every render so the closure always sees fresh state (touchupStrokes,
  // commitTouchup, etc.) without a dependency array.
  windowTouchupMouseUpRef.current = async () => {
    if (!touchupDraggingRefLocal.current) return // already handled by the canvas-level handler
    touchupDraggingRefLocal.current = false
    onDragEnd()
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

  // ── Touch-up done event (result of async TouchUpApply goroutine) ──────────
  // Handler is refreshed every render; the useEffect subscribes once at mount.
  touchupDoneHandlerRef.current = (data) => {
    setLoading(false)
    if (data?.cancelled) return
    if (data?.error) {
      showStatus('')
      const hint = touchupBackend === 'iopaint'
        ? '\n\nPlease make sure IOPaint is running and that you have the server address configured correctly. Alternatively, try switching to the PatchMatch backend in Options.'
        : ''
      setErrorMessage('Failed to inpaint.' + hint + '\n\n' + data.error)
    } else if (data?.preview) {
      setPreview(data.preview)
      if (setUnsavedChanges) setUnsavedChanges(true)
      showStatus(data.message || '')
      if (!touchupRemainsActive) setUseTouchupTool(false)
      flushPendingSaveRef?.current?.()
    }
  }
  useEffect(() => {
    EventsOn('touchup-done', (data) => touchupDoneHandlerRef.current?.(data))
    return () => EventsOff('touchup-done')
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  return {
    touchupStrokes, setTouchupStrokes,
    brushSize, setBrushSize,
    touchupDraggingRef: touchupDraggingRefLocal,
    clearTouchup, commitTouchup,
  }
}
