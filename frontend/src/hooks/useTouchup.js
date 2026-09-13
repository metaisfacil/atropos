import { useState, useRef, useEffect } from 'react'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
import { adjustmentSelectionPayload } from '../utils/adjustmentSelection'

export function touchupPreviewPatch(data, baseSource) {
  const patch = data?.patch
  if (!baseSource || !data?.preview || !patch?.dataURL ||
      !Number.isFinite(patch.x) || !Number.isFinite(patch.y) ||
      !Number.isFinite(patch.width) || !Number.isFinite(patch.height) ||
      patch.width <= 0 || patch.height <= 0 || !data?.width || !data?.height) {
    return null
  }
  return {
    ...patch,
    baseSource,
    source: data.preview,
    imageWidth: data.width,
    imageHeight: data.height,
  }
}

export function useTouchup({
  imageLoaded, loading, setLoading, showStatus,
  touchupBackend, setErrorMessage, preview, setPreview, onDragEnd,
  flushPendingSaveRef,
  touchupRemainsActive, setUseTouchupTool, setUseDescreenTool,
  setUnsavedChanges,
  touchupDraggingRef,
  adjustmentRect,
}) {
  const [touchupStrokes, setTouchupStrokes] = useState([])
  const [brushSize, setBrushSize]           = useState(40)
  const [touchupPatch, setTouchupPatch]     = useState(null)
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
      let patchSize = Math.max(7, Math.floor(brushSize / 3))
      if (patchSize % 2 === 0) patchSize++
      const iterations = 5
      // Send image-space stroke geometry rather than constructing and encoding
      // a full-resolution PNG mask on the UI thread. Go rasterizes only the
      // small painted bounds and launches the fill immediately.
      // The result (preview, error, or cancellation) arrives via the
      // "touchup-done" event handled by touchupDoneHandlerRef. setLoading(false)
      // is therefore NOT called here — the event handler does it.
      await window['go']['main']['App']['TouchUpApplyStrokes']({
        points: touchupStrokes,
        brushSize,
        patchSize,
        iterations,
        selection: adjustmentSelectionPayload(adjustmentRect),
      })
      setTouchupStrokes([])
    } catch (err) {
      // Immediate errors (no image or invalid stroke geometry).
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
  touchupDoneHandlerRef.current = async (data) => {
    if (data?.cancelled) {
      setLoading(false)
      return
    }
    if (data?.error) {
      setLoading(false)
      showStatus('')
      const hint = touchupBackend === 'iopaint'
        ? '\n\nPlease make sure IOPaint is running and that you have the server address configured correctly. Alternatively, try switching to the PatchMatch backend in Options.'
        : ''
      setErrorMessage('Failed to inpaint.' + hint + '\n\n' + data.error)
      return
    }

    if (data?.preview) {
      // Let PreviewCanvas promote the already-decoded base raster with the
      // small lossless replacement before changing the authoritative source.
      // The immutable revision remains the fallback for an absent/invalid patch.
      const patch = touchupPreviewPatch(data, preview)
      if (patch) setTouchupPatch(patch)
      setPreview(data.preview)
    }
    setLoading(false)

    if (data?.preview) {
      if (data?.descreenReset || data?.preview) {
        setUseDescreenTool?.(false)
      }
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
    touchupPatch,
    touchupDraggingRef: touchupDraggingRefLocal,
    clearTouchup, commitTouchup,
  }
}
