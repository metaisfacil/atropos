import { useState, useRef, useEffect } from 'react'
import { OnFileDrop, OnFileDropOff, EventsOn, EventsOff, Quit } from '../../wailsjs/runtime/runtime'
import { fitWidthFor } from '../utils/previewLayout'
import {
  LoadImage,
  DetectCorners,
  ResetCorners,
  SkipCrop,
  NormalCrop,
  ResetNormal,
  OpenImageDialog,
  OpenSaveDialog,
  GetLaunchArgs,
  ConfirmClose,
  GetCleanPreview,
  RestoreCornerOverlay,
  RecropImage,
  CancelCornerDetect,
  CancelTouchup,
  LoadImageFromClipboard,
  LoadImageBytes,
  ResetDisc,
  ClearLines,
  SaveImage,
  RunPostSaveCommand,
  Undo,
  Redo,
} from '../../wailsjs/go/main/App'

export function useImageActions({
  mode, loading, imageLoaded, discActive, linesProcessed, normalCropApplied,
  cornerState, dotRadius, useStretchPreprocess, autoCornerParams, normalRect, closeAfterSave, setCloseAfterSave, postSaveEnabled, setPostSaveEnabled, postSaveCommand, setPostSaveCommand, autoDetectOnModeSwitch,
  setMode, setPreview, setLoading, setImageLoaded, setRealImageDims, setInputImageDims, setImgNatural,
  setZoom, setFitWidth, setCornerState, setLinesDone, setLinesProcessed,
  setDiscActive, setDiscNoMaskPreview, setDiscCenter, setDiscRadius, setDiscRotation, setDiscBgColor, setNormalRect, setNormalCropApplied, setCropSkipped, setCornersDetected,
  setDetectedCornerPts, setSelectedCornerPts, setLines, setBlackPoint, setWhitePoint,
  setFeatherSize, syncHistoryDiscSettings,
  setUseTouchupTool, setUseDescreenTool, setUseStraightEdgeTool, setDragging, setDragStart, setDragCurrent,
  setConfirmDialog, setTouchupStrokes,
  setAdjustmentSelectionActive, setAdjustmentRect,
  touchupDraggingRef, canvasRef,
  showStatus, showError,
  setImageMeta,
  compositorDropRef, calibrationDropRef,
  unsavedChanges, setUnsavedChanges,
}) {
  const [loadingFull, setLoadingFull] = useState(false)
  const [saving, setSaving]          = useState(false)
  const modeRef            = useRef(mode)
  const lastDetectSettings      = useRef(null)
  const suggestedCornerParamsRef = useRef({})
  const detectGenRef            = useRef(0)
  const transitionRef           = useRef(0)
  const modeQueueRef            = useRef(Promise.resolve())
  const backendModeRef          = useRef(mode)
  const cornerEntryRef          = useRef(null) // { preview, width, height } captured on corner mode entry

  const markUnsavedChanges = () => {
    if (setUnsavedChanges) setUnsavedChanges(true)
  }
  const clearUnsavedChanges = () => {
    if (setUnsavedChanges) setUnsavedChanges(false)
  }
  const savingRef          = useRef(false)
  const pendingDropRef     = useRef(null)
  const pendingSaveRef     = useRef(false)
  const loadingRef         = useRef(false)
  // Wails registers the filesystem-drop callback once. Keep everything that
  // callback needs in refs so it never replays detector values from the first
  // render after the user has moved a slider or changed the Options toggle.
  const cornerStateRef       = useRef(cornerState)
  const dotRadiusRef         = useRef(dotRadius)
  const stretchPreprocessRef = useRef(useStretchPreprocess)
  const autoCornerParamsRef  = useRef(autoCornerParams)
  cornerStateRef.current = cornerState
  dotRadiusRef.current = dotRadius
  stretchPreprocessRef.current = useStretchPreprocess
  autoCornerParamsRef.current = autoCornerParams
  useEffect(() => { modeRef.current = mode }, [mode])
  useEffect(() => { loadingRef.current = loading }, [loading])

  const beginTransition = () => {
    const generation = ++transitionRef.current
    detectGenRef.current++
    CancelCornerDetect()
    CancelTouchup()
    cornerEntryRef.current = null
    setUseDescreenTool(false)
    setUseTouchupTool(false)
    setUseStraightEdgeTool(false)
    setTouchupStrokes([])
    touchupDraggingRef.current = false
    setDragging(false)
    setDragStart(null)
    setDragCurrent(null)
    setAdjustmentSelectionActive(false)
    setAdjustmentRect(null)
    return generation
  }

  // Loads share the mode-reset queue: overlapping drops must not make the
  // backend reject the newer load while the frontend ignores the older one.
  const queueLoad = (generation, request) => {
    const task = async () => {
      if (transitionRef.current !== generation) return null
      const result = await request()
      // A successful request has already replaced the backend document. Publish
      // it before the next queued request, even if that next request may fail.
      presentLoadedImage(result)
      return result
    }
    const pending = modeQueueRef.current.then(task, task)
    modeQueueRef.current = pending.then(() => {}, () => {})
    return pending
  }

  // ── Shared mode/image state reset (used by loadFile and handleRecrop) ────────
  const resetImageState = () => {
    setCornerState(s => ({ ...s, cornerCount: 0 }))
    setLinesDone(0)
    setLinesProcessed(false)
    setDiscActive(false)
    setNormalRect(null)
    setNormalCropApplied(false)
    setCropSkipped(false)
    setCornersDetected(false)
    lastDetectSettings.current = null
    cornerEntryRef.current = null
    setLines([])
    setTouchupStrokes([])
    setUseDescreenTool(false)
    setUseTouchupTool(false)
    setUseStraightEdgeTool(false)
    setDragging(false)
    setDragStart(null)
    setDragCurrent(null)
    setAdjustmentSelectionActive(false)
    setAdjustmentRect(null)
    touchupDraggingRef.current = false
    setDetectedCornerPts([])
    setSelectedCornerPts([])
    setBlackPoint(0)
    setWhitePoint(255)
    setDiscNoMaskPreview(null)
    setDiscCenter(null)
    setDiscRadius(0)
    setDiscRotation(0)
    setDiscBgColor({ r: 255, g: 255, b: 255 })
  }

  // ── Core corner detector (private — used by loadFile and handleDetectCorners) ─
  const runDetectCorners = async (overrides = {}) => {
    const gen = ++detectGenRef.current
    const currentCornerState = cornerStateRef.current
    const currentDotRadius   = dotRadiusRef.current
    const currentUseStretch  = stretchPreprocessRef.current
    const maxCorners  = overrides.maxCorners  ?? currentCornerState.maxCorners
    const minDistance = overrides.minDistance ?? currentCornerState.minDistance
    showStatus('Detecting corners…')
    let result
    try {
      result = await DetectCorners({
        maxCorners,
        qualityLevel: currentCornerState.qualityLevel,
        minDistance,
        accentValue:  currentCornerState.accent,
        dotRadius:    currentDotRadius,
        useStretch:      currentUseStretch,
        stretchLow:      0.01,
        stretchHigh:     0.99,
      })
    } catch (err) {
      if (detectGenRef.current !== gen) return  // cancelled by mode switch — discard silently
      throw err
    }
    if (detectGenRef.current !== gen) return
    cornerEntryRef.current = { preview: result.preview, width: result.width, height: result.height }
    setPreview(result.preview)
    showStatus(result.message + ' — click 4 corners')
    if (result.width && result.height) setRealImageDims({ w: result.width, h: result.height })
    setDetectedCornerPts(result.corners || [])
    setSelectedCornerPts([])
    setCornerState(s => ({ ...s, cornerCount: 0, maxCorners, minDistance }))
    setCornersDetected(true)
    lastDetectSettings.current = {
      maxCorners,
      qualityLevel:   currentCornerState.qualityLevel,
      minDistance,
      accent:         currentCornerState.accent,
      useStretch:     currentUseStretch,
    }
  }

  // ── Core image result applier (shared for LoadImage/LoadImageBytes) ─
  const presentLoadedImage = (result) => {
    setPreview(result.preview)
    setImageLoaded(true)
    setRealImageDims({ w: result.width, h: result.height })
    setInputImageDims({ w: result.width, h: result.height })
    setImgNatural({ w: result.width, h: result.height })
    if (setInputImageDims) setInputImageDims({ w: result.width, h: result.height })
    setImageMeta({ format: result.format || '', dpiX: result.dpiX || 0, dpiY: result.dpiY || 0 })
    resetImageState()
    backendModeRef.current = modeRef.current

    suggestedCornerParamsRef.current = result.suggestedCornerParams || {}

    clearUnsavedChanges()

  }

  const applyLoadedImage = async (result, autoDetect = true, generation = transitionRef.current) => {
    showStatus(`Loaded: ${result.width}x${result.height}`)
    setLoadingFull(false)

    if (autoDetect && modeRef.current === 'corner') {
      await runDetectCorners(autoCornerParamsRef.current ? suggestedCornerParamsRef.current : {})
    }

    if (transitionRef.current === generation) setLoading(false)
  }

  // ── Core file loader (private — used by dialog, drag-drop, and launch args) ─
  const loadFile = async (filePath, autoDetect = true) => {
    const generation = beginTransition()
    setLoading(true)
    setLoadingFull(true)
    setZoom(1)
    const name = filePath.split(/[/\\]/).pop()
    showStatus(`Loading ${name}…`)

    const result = await queueLoad(generation, () => LoadImage({ filePath }))
    if (transitionRef.current !== generation) return
    await applyLoadedImage(result, autoDetect, generation)
  }

  const loadImageFromBytes = async (arrayBuffer, sourceName = '[Clipboard Data]') => {
    const generation = beginTransition()
    setLoading(true)
    setLoadingFull(true)
    setZoom(1)
    showStatus(`Loading ${sourceName}…`)

    const bytes = Array.from(new Uint8Array(arrayBuffer))
    const result = await queueLoad(generation, () => LoadImageBytes({ data: bytes, name: sourceName }))
    if (transitionRef.current !== generation) return
    await applyLoadedImage(result, true, generation)
  }

  const loadImageFromClipboard = async () => {
    const generation = beginTransition()
    setLoading(true)

    const result = await queueLoad(generation, () => LoadImageFromClipboard())
    if (transitionRef.current !== generation) return
    setLoadingFull(true)
    setZoom(1)
    showStatus('Loading clipboard image…')
    await applyLoadedImage(result, true, generation)
  }

  const handlePasteImage = async () => {
    if (loadingRef.current || savingRef.current) return
    const generation = transitionRef.current + 1
    loadingRef.current = true
    try {
      await loadImageFromClipboard()
    } catch (err) {
      if (transitionRef.current !== generation) return
      console.error('Clipboard image load error:', err)
      const message = err?.message || String(err)
      if (!message.includes('clipboard does not contain an image')) {
        showError(err)
      }
    } finally {
      if (transitionRef.current === generation) {
        loadingRef.current = false
        setLoading(false)
        setLoadingFull(false)
      }
    }
  }

  // ── Load image (dialog) ───────────────────────────────────────────────────
  const handleLoadImage = async () => {
    if (loading && !savingRef.current) return
    let generation = transitionRef.current
    try {
      const filePath = await OpenImageDialog()
      if (!filePath) return
      if (savingRef.current) {
        pendingDropRef.current = filePath
        return
      }
      generation = transitionRef.current + 1
      await loadFile(filePath)
    } catch (err) {
      if (transitionRef.current !== generation) return
      console.error('Load error:', err)
      showError(err)
    } finally {
      if (!savingRef.current && transitionRef.current === generation) {
        setLoading(false)
        setLoadingFull(false)
      }
    }
  }

  // ── Drag-and-drop file loading ─────────────────────────────────────────────
  useEffect(() => {
    const suppressDefault = (e) => e.preventDefault()
    document.addEventListener('dragover', suppressDefault)
    document.addEventListener('drop', suppressDefault)

    OnFileDrop(async (_x, _y, paths) => {
      if (!paths || paths.length === 0) return
      const validExts = ['png', 'jpg', 'jpeg', 'tif', 'tiff', 'bmp', 'gif', 'webp']
      if (calibrationDropRef?.current) {
        const imagePaths = paths.filter(p => validExts.includes(p.split('.').pop().toLowerCase()))
        if (imagePaths.length > 0) calibrationDropRef.current(imagePaths)
        return
      }
      // If the compositor modal is open, forward the dropped paths to it instead
      if (compositorDropRef?.current) {
        const imagePaths = paths.filter(p => validExts.includes(p.split('.').pop().toLowerCase()))
        if (imagePaths.length > 0) compositorDropRef.current(imagePaths)
        return
      }
      const filePath = paths[0]
      const ext = filePath.split('.').pop().toLowerCase()
      if (!validExts.includes(ext)) return
      if (savingRef.current) {
        pendingDropRef.current = filePath
        return
      }
      const generation = transitionRef.current + 1
      try {
        await loadFile(filePath, modeRef.current === 'corner')
      } catch (err) {
        if (transitionRef.current !== generation) return
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

  // ── Browser URL drop image loading ───────────────────────────────────────────
  useEffect(() => {
    const onDrop = async (e) => {
      if (!e.dataTransfer) return
      // File drops from the filesystem are handled exclusively by Wails OnFileDrop
      // (which provides file paths). In WebView2, File.path is never set, so the
      // old !file.path guard couldn't distinguish Explorer drops from browser drags,
      // causing every Explorer drop to also trigger a slow bytes-based load here.
      // Only handle http/https URL drops (e.g. dragging an image URL from a browser).
      const url = e.dataTransfer.getData('text/uri-list') || e.dataTransfer.getData('text/plain')
      if (url && /^https?:\/\//.test(url)) {
        e.preventDefault()
        try {
          const resp = await fetch(url)
          if (!resp.ok) throw new Error(`Failed to fetch image from URL: ${resp.status}`)
          const buffer = await resp.arrayBuffer()
          await loadImageFromBytes(buffer, url)
          return
        } catch (err) {
          console.error('URL drop image load error:', err)
          showError(err)
        }
      }
    }

    window.addEventListener('drop', onDrop)

    return () => {
      window.removeEventListener('drop', onDrop)
    }
  }, [])

  // ── Launch arguments ───────────────────────────────────────────────────────
  // The `cancelled` flag guards against React StrictMode's double-invocation
  // of effects with [] deps.  In development StrictMode React mounts → runs
  // effects → unmounts (cleanup) → remounts → runs effects again.  Without
  // the flag both invocations would race to call LoadImage, causing a second
  // detection run or a spurious setLoading(false) mid-detection.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const args = (await GetLaunchArgs()) || {}
        if (cancelled) return
        if (args.mode) { modeRef.current = args.mode; backendModeRef.current = args.mode; setMode(args.mode) }
        // CLI-provided post-save overrides persisted settings (do not force quit)
        if (args.postSaveCommand) {
          setPostSaveCommand(args.postSaveCommand)
          setPostSaveEnabled(true)
          if (args.postSaveExit) setCloseAfterSave(true)
        }
        if (args.filePath) {
          const shouldDetect = args.mode ? (args.mode === 'corner') : (mode === 'corner')
          await loadFile(args.filePath, shouldDetect)
        } else {
          showStatus('No image loaded')
        }
      } catch (err) {
        if (cancelled) return
        console.error('Launch args error:', err)
        setLoading(false)
        setLoadingFull(false)
        showStatus('No image loaded')
      }
    })()
    return () => { cancelled = true }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Compositor: load result into corner-mode pipeline ───────────────────────
  // Called by CompositorModal via the onLoad prop.  Receives the ImageInfo
  // returned by CompositorLoadResult (Go) which the modal already called;
  // this function applies the corresponding React state reset, switches to
  // corner mode, and runs corner detection.
  const handleCompositorLoad = async (info) => {
    const generation = beginTransition()
    setLoading(true)
    try {
      setZoom(1)
      setPreview(info.preview)
      setImageLoaded(true)
      // Compositor gives a generated image; treat both dims like a fresh
      // load: `inputImageDims` records the compositor result size and
      // `realImageDims` is set to the same value initially.
      setRealImageDims({ w: info.width, h: info.height })
      setInputImageDims({ w: info.width, h: info.height })
      setImgNatural({ w: info.width, h: info.height })
      if (setInputImageDims) setInputImageDims({ w: info.width, h: info.height })
      setImageMeta({ format: '', dpiX: 0, dpiY: 0 })
      resetImageState()
      suggestedCornerParamsRef.current = info.suggestedCornerParams || {}
      modeRef.current = 'corner'
      backendModeRef.current = 'corner'
      setMode('corner')
      await runDetectCorners(autoCornerParamsRef.current ? suggestedCornerParamsRef.current : {})
    } catch (err) {
      showError(err)
    } finally {
      if (transitionRef.current === generation) setLoading(false)
    }
  }

  // ── Corner detection ───────────────────────────────────────────────────────

  const handleDetectCorners = async () => {
    const generation = detectGenRef.current + 1
    setLoading(true)
    try {
      // Suggested values are load-time defaults only. A manual Detect must use
      // the values currently shown in the controls, even when auto-adjust is on.
      await runDetectCorners()
    } catch (err) {
      console.error('Detect error:', err)
    } finally {
      if (detectGenRef.current === generation) setLoading(false)
    }
  }

  // ── Skip crop ─────────────────────────────────────────────────────────────
  const handleSkipCrop = async () => {
    const generation = beginTransition()
    setLoading(true)
    showStatus('Skipping crop…')
    try {
      const result = await SkipCrop()
      if (transitionRef.current !== generation) return
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
      markUnsavedChanges()
      showStatus(result?.message || 'Crop skipped')
    } catch (err) {
      console.error('SkipCrop error:', err)
      showError(err)
    } finally {
      if (transitionRef.current === generation) setLoading(false)
    }
  }

  // ── Re-crop ───────────────────────────────────────────────────────────────

  const handleRecrop = () => {
    setConfirmDialog({
      message: 'Re-crop will use the current output as a new source image, resetting all crop and adjustment state. Continue?',
      onConfirm: async () => {
        const generation = beginTransition()
        CancelTouchup()
        // End transient pointer ownership before waiting for the backend. In
        // particular, a touch-up drag must not survive until Lines is reset to
        // its pre-crop state and then be completed as a line gesture.
        setTouchupStrokes([])
        setUseTouchupTool(false)
        touchupDraggingRef.current = false
        setDragging(false)
        setDragStart(null)
        setDragCurrent(null)
        setConfirmDialog(null)
        setLoading(true)
        // Re-crop promotes the current output to a fresh source image. Reset
        // the camera just like a normal load, and preserve the current fit
        // geometry until the replacement preview is presented.
        setZoom(1)
        showStatus('Re-cropping…')
        try {
          const result = await RecropImage()
          if (transitionRef.current !== generation) return
          setPreview(result.preview)
          setRealImageDims({ w: result.width, h: result.height })
          if (setInputImageDims) setInputImageDims({ w: result.width, h: result.height })
          setImageMeta({ format: '', dpiX: 0, dpiY: 0 })
          resetImageState()
          markUnsavedChanges()
          showStatus(`Re-cropping from ${result.width}×${result.height} image`)
        } catch (err) {
          console.error('RecropImage error:', err)
          showError(err)
        } finally {
          if (transitionRef.current === generation) setLoading(false)
        }
      },
    })
  }

  // ── Mode-specific reset handlers ──────────────────────────────────────────
  const handleResetCorners = async () => {
    const generation = beginTransition()
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting corners…')
    try {
      const result = await ResetCorners()
      if (transitionRef.current !== generation) return
      setPreview(result.preview)
      showStatus(result.message)
      if (result.width && result.height) setRealImageDims({ w: result.width, h: result.height })
      setDetectedCornerPts(result.corners || [])
      setSelectedCornerPts([])
      setCornerState(s => ({ ...s, cornerCount: 0 }))
      setCropSkipped(false)
      setUseTouchupTool(false)
      markUnsavedChanges()
    } catch (err) {
      console.error('ResetCorners error:', err)
    } finally {
      if (transitionRef.current === generation) setLoading(false)
    }
  }

  const handleResetDisc = async () => {
    const generation = beginTransition()
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting disc…')
    try {
      const result = await ResetDisc()
      if (transitionRef.current !== generation) return
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      setDiscActive(false)
      setDiscNoMaskPreview(null)
      setDiscCenter(null)
      setDiscRadius(0)
      setDiscRotation(0)
      setDiscBgColor({ r: 255, g: 255, b: 255 })
      setCropSkipped(false)
      setUseTouchupTool(false)
      setUseStraightEdgeTool(false)
      setDragging(false)
      setDragStart(null)
      setDragCurrent(null)
      markUnsavedChanges()
      showStatus(result?.message || 'Disc selection reset')
    } catch (err) {
      console.error('ResetDisc error:', err)
    } finally {
      if (transitionRef.current === generation) setLoading(false)
    }
  }

  const handleResetNormal = async () => {
    const generation = beginTransition()
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting normal crop…')
    try {
      const result = await ResetNormal()
      if (transitionRef.current !== generation) return
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      setNormalRect(null)
      setNormalCropApplied(false)
      setCropSkipped(false)
      setUseTouchupTool(false)
      markUnsavedChanges()
      showStatus(result?.message || 'Normal crop reset')
    } catch (err) {
      console.error('ResetNormal error:', err)
    } finally {
      if (transitionRef.current === generation) setLoading(false)
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
      markUnsavedChanges()
    } catch (err) {
      console.error('NormalCrop error:', err)
      showError(err)
    } finally {
      setLoading(false)
    }
  }

  const handleClearLines = async () => {
    const generation = beginTransition()
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting lines…')
    try {
      const result = await ClearLines()
      if (transitionRef.current !== generation) return
      setLinesDone(0)
      setLines([])
      setLinesProcessed(false)
      setCropSkipped(false)
      setUseTouchupTool(false)
      markUnsavedChanges()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      showStatus(result?.message || 'Lines cleared')
    } catch (err) {
      console.error('ClearLines error:', err)
    } finally {
      if (transitionRef.current === generation) setLoading(false)
    }
  }

  // ── Undo ──────────────────────────────────────────────────────────────────

  const historyBusyRef = useRef(false)
  const handleHistory = async (redo) => {
    if (historyBusyRef.current) return
    historyBusyRef.current = true
    const generation = transitionRef.current
    setLoading(true)
    showStatus(redo ? 'Redoing...' : 'Undoing...')
    try {
      const res = await (redo ? Redo() : Undo())
      if (transitionRef.current !== generation) return
      if (res?.preview) setPreview(res.preview)
      if (res?.width && res?.height) setRealImageDims({ w: res.width, h: res.height })
      showStatus(res?.message || '')
      if (res?.descreenReset) setUseDescreenTool(false)
      if (res?.changed) {
        setCropSkipped(res.cropSkipped ?? false)
        setBlackPoint(res.black ?? 0)
        setWhitePoint(res.white ?? 255)
        setAdjustmentRect(null)
        if (mode === 'disc') {
          if (res.historyDiscSettings) {
            syncHistoryDiscSettings(res.historyDiscSettings)
            setFeatherSize(res.historyFeatherSize ?? 0)
          }
          setDiscRotation(res.discRotation ?? 0)
          setDiscCenter({ x: res.discCenterX ?? 0, y: res.discCenterY ?? 0 })
          setDiscRadius(res.discRadius ?? 0)
          setDiscNoMaskPreview(res.unmaskedPreview || null)
          if (res.discRadius > 0) setDiscBgColor({ r: res.discBgR ?? 0, g: res.discBgG ?? 0, b: res.discBgB ?? 0 })
        }
        if (redo && !res.uncropped) {
          if (mode === 'corner') {
            setCornerState(s => ({ ...s, cornerCount: 4 }))
            setSelectedCornerPts([])
            setDetectedCornerPts([])
          } else if (mode === 'disc') setDiscActive(true)
          else if (mode === 'line') setLinesProcessed(true)
          else if (mode === 'normal') {
            setNormalCropApplied(true)
            setNormalRect(null)
          }
        }
      }
      if (res?.uncropped) {
        // The undo took us back past the initial crop — return to the cropping
        // phase by resetting the mode-specific post-crop state.  Only act if
        // the frontend was actually in post-crop state to avoid clobbering
        // in-progress corner clicks or other pre-crop selections.
        if (mode === 'corner' && cornerState.cornerCount >= 4) {
          const restoredCorners = res.selectedCorners ?? []
          setCornerState(s => ({ ...s, cornerCount: restoredCorners.length }))
          setSelectedCornerPts(restoredCorners)
          setCropSkipped(false)
          setUseTouchupTool(false)
          // Restore detected corner dots if the backend still has them.
          if (res.corners && res.corners.length > 0) {
            setDetectedCornerPts(res.corners)
            setCornersDetected(true)
          }
        } else if (mode === 'disc' && discActive) {
          setDiscActive(false)
          setDragging(false)
          setDragStart(null)
          setDragCurrent(null)
          setCropSkipped(false)
          setUseTouchupTool(false)
          setUseStraightEdgeTool(false)
        } else if (mode === 'line' && linesProcessed) {
          setLinesProcessed(false)
          setCropSkipped(false)
          setUseTouchupTool(false)
        } else if (mode === 'normal' && normalCropApplied) {
          setNormalCropApplied(false)
          setNormalRect(null)
          setCropSkipped(false)
          setUseTouchupTool(false)
        }
        setBlackPoint(0)
        setWhitePoint(255)
      }
      if (res?.changed) markUnsavedChanges()
    } catch (err) {
      console.error('History error:', err)
      showError(err)
    } finally {
      historyBusyRef.current = false
      if (transitionRef.current === generation) setLoading(false)
    }
  }

  const handleUndo = () => handleHistory(false)
  const handleRedo = () => handleHistory(true)

  // ── Save ──────────────────────────────────────────────────────────────────
  // Core save implementation. Called by handleSaveImage (direct) and
  // flushPendingSave (deferred, after an operation completes).
  const _performSave = async () => {
    pendingSaveRef.current = false
    let saved = false
    try {
      const filePath = await OpenSaveDialog()
      if (!filePath) return false
      setLoading(true)
      savingRef.current = true
      setSaving(true)
      showStatus('Saving…')
      const result = await SaveImage({ outputPath: filePath })
      const savedName = filePath.split(/[\\/]/).pop()
      showStatus(result?.message || `Saved to ${savedName}`)
      clearUnsavedChanges()
      saved = true
      if (postSaveEnabled && postSaveCommand) RunPostSaveCommand(postSaveCommand, filePath).catch(err => console.error('Post-save command error:', err))
      if (closeAfterSave) Quit()
    } catch (err) {
      console.error('Save error:', err)
      showError(err)
    } finally {
      setLoading(false)
      savingRef.current = false
      setSaving(false)
      if (pendingDropRef.current) {
        const pendingPath = pendingDropRef.current
        pendingDropRef.current = null
        try {
          await loadFile(pendingPath, modeRef.current === 'corner')
        } catch (err) {
          console.error('Deferred drop load error:', err)
          showError(err)
          setLoading(false)
          setLoadingFull(false)
        }
      }
    }
    return saved
  }

  // If an operation is active, queue the save; otherwise save immediately.
  const handleSaveImage = async () => {
    if (loadingRef.current || touchupDraggingRef.current) {
      pendingSaveRef.current = true
      showStatus('Save queued…')
      return false
    }
    return await _performSave()
  }

  // Called by useKeyboardShortcuts and useTouchup once their operation finishes.
  const flushPendingSave = async () => {
    if (!pendingSaveRef.current) return
    await _performSave()
  }

  // ── Close request event (Wails OnBeforeClose) ─────────────────────────────
  useEffect(() => {
    const closeRequestHandler = async () => {
      if (!unsavedChanges) {
        await ConfirmClose()
        Quit()
        return
      }

      setConfirmDialog({
        message: 'You have unsaved changes. Save before quitting?',
        onYes: async () => {
          setConfirmDialog(null)
          const saved = await handleSaveImage()
          if (saved) {
            await ConfirmClose()
            Quit()
          }
        },
        onNo: async () => {
          setConfirmDialog(null)
          await ConfirmClose()
          Quit()
        },
        onCancel: () => setConfirmDialog(null),
        yesText: 'Yes',
        noText: 'No',
        cancelText: 'Cancel',
      })
    }

    EventsOn('app-close-requested', closeRequestHandler)
    return () => EventsOff('app-close-requested')
  }, [unsavedChanges, handleSaveImage])

  // ── Mode switch ───────────────────────────────────────────────────────────
  // Serialize backend resets while updating the selected mode immediately.
  // Generations prevent stale reset/detection responses from replacing newer UI.
  const handleModeSwitch = (m) => {
    if (m === modeRef.current) return Promise.resolve()
    modeRef.current = m
    setMode(m)
    // With no document, selecting a mode is only a preference for the pending
    // first load. Keep that load's generation and cleanup ownership intact.
    if (!imageLoaded) { backendModeRef.current = m; return Promise.resolve() }
    const generation = beginTransition()
    const current = () => transitionRef.current === generation
    setLoading(true)
    const task = async () => {
      if (!current()) return
      try {
        const reset = { corner: ResetCorners, disc: ResetDisc, line: ClearLines, normal: ResetNormal }[backendModeRef.current]
        const clean = await reset()
        backendModeRef.current = m
        if (!current()) return
        setCornerState(s => ({ ...s, cornerCount: 0 }))
        setSelectedCornerPts([])
        setDetectedCornerPts([])
        setCornersDetected(false)
        setLinesDone(0)
        setLines([])
        setLinesProcessed(false)
        setNormalRect(null)
        setNormalCropApplied(false)
        setDiscActive(false)
        setDiscNoMaskPreview(null)
        setDiscCenter(null)
        setDiscRadius(0)
        setDiscRotation(0)
        setCropSkipped(false)
        setBlackPoint(0)
        setWhitePoint(255)

        let res = clean
        const snap = lastDetectSettings.current
        const cached = m === 'corner' && snap &&
          snap.maxCorners === cornerState.maxCorners && snap.qualityLevel === cornerState.qualityLevel &&
          snap.minDistance === cornerState.minDistance && snap.accent === cornerState.accent &&
          snap.useStretch === useStretchPreprocess
        if (cached) {
          try {
            res = await RestoreCornerOverlay({ dotRadius })
            if (!current()) return
            setDetectedCornerPts(res.corners || [])
            setCornersDetected(true)
          } catch (err) {
            if (!current()) return
            res = null
          }
        }
        if (m === 'corner' && (!cached || !res) && autoDetectOnModeSwitch) {
          await runDetectCorners()
          return
        }
        if (!res) res = await GetCleanPreview()
        if (!current()) return
        if (res?.preview) {
          const c = canvasRef.current
          if (c && res.width && res.height) setFitWidth(fitWidthFor(c, { w: res.width, h: res.height }))
          setPreview(res.preview)
          if (m === 'corner') cornerEntryRef.current = { preview: res.preview, width: res.width, height: res.height }
        }
        if (res?.width && res?.height) setRealImageDims({ w: res.width, h: res.height })
        showStatus(res?.message || '')
      } catch (err) {
        if (current()) {
          modeRef.current = backendModeRef.current
          setMode(backendModeRef.current)
          showError(err)
        }
      } finally {
        if (current()) setLoading(false)
      }
    }
    const pending = modeQueueRef.current.then(task, task)
    modeQueueRef.current = pending
    return pending
  }

  return {
    loadingFull,
    saving,
    handleLoadImage,
    handlePasteImage,
    handleDetectCorners,
    handleCompositorLoad,
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
    handleRedo,
  }
}
