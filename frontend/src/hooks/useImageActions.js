import { useState, useRef, useEffect } from 'react'
import { OnFileDrop, OnFileDropOff, EventsOn, EventsOff, Quit } from '../../wailsjs/runtime/runtime'
import {
  LoadImage,
  DetectCorners,
  ResetCorners,
  ResetImage,
  NormalCrop,
  ResetNormal,
  OpenImageDialog,
  OpenSaveDialog,
  GetLaunchArgs,
  ConfirmClose,
  GetCleanPreview,
  RestoreCornerOverlay,
  CancelCornerDetect,
  CancelTouchup,
  LoadImageBytes,
  ResetDisc,
  ClearLines,
  CancelLineProgress,
  SaveImage,
  RunPostSaveCommand,
  Undo,
  CompositorLoadResult,
} from '../../wailsjs/go/main/App'

export function useImageActions({
  mode, loading, imageLoaded, discActive, linesProcessed, normalCropApplied,
  cornerState, dotRadius, useStretchPreprocess, autoCornerParams, normalRect, closeAfterSave, postSaveEnabled, postSaveCommand, autoDetectOnModeSwitch,
  setMode, setPreview, setLoading, setImageLoaded, setRealImageDims, setInputImageDims, setImgNatural,
  setZoom, setFitWidth, setCornerState, setLinesDone, setLinesProcessed,
  setDiscActive, setDiscNoMaskPreview, setDiscCenter, setDiscRadius, setDiscRotation, setDiscBgColor, setNormalRect, setNormalCropApplied, setCornersDetected,
  setDetectedCornerPts, setSelectedCornerPts, setLines, setBlackPoint, setWhitePoint,
  setUseTouchupTool, setUseStraightEdgeTool, setDragging, setDragStart, setDragCurrent,
  setConfirmDialog, setTouchupStrokes,
  touchupDraggingRef, canvasRef,
  showStatus, showError,
  setImageMeta,
  compositorDropRef,
  unsavedChanges, setUnsavedChanges,
}) {
  const [loadingFull, setLoadingFull] = useState(false)
  const [saving, setSaving]          = useState(false)
  const [resetImagePending, setResetImagePending] = useState(false)
  const modeRef            = useRef(mode)
  const lastDetectSettings      = useRef(null)
  const suggestedCornerParamsRef = useRef({})
  const detectGenRef            = useRef(0)
  const cornerEntryRef          = useRef(null) // { preview, width, height } captured on corner mode entry
  const resetImagePendingRef    = useRef(false)

  const setResetPending = (value) => {
    resetImagePendingRef.current = value
    setResetImagePending(value)
  }
  const clearResetImagePending = () => setResetPending(false)

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
  useEffect(() => { modeRef.current = mode }, [mode])
  useEffect(() => { loadingRef.current = loading }, [loading])

  // ── Shared mode/image state reset (used by loadFile) ─────────────────────────
  const resetImageState = () => {
    setCornerState(s => ({ ...s, cornerCount: 0 }))
    setLinesDone(0)
    setLinesProcessed(false)
    setDiscActive(false)
    setNormalRect(null)
    setNormalCropApplied(false)
    setCornersDetected(false)
    lastDetectSettings.current = null
    cornerEntryRef.current = null
    setLines([])
    setTouchupStrokes([])
    setUseTouchupTool(false)
    setUseStraightEdgeTool(false)
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
    const maxCorners  = overrides.maxCorners  ?? cornerState.maxCorners
    const minDistance = overrides.minDistance ?? cornerState.minDistance
    showStatus('Detecting corners…')
    let result
    try {
      result = await DetectCorners({
        maxCorners,
        qualityLevel: cornerState.qualityLevel,
        minDistance,
        accentValue:  cornerState.accent,
        dotRadius,
        useStretch:      useStretchPreprocess,
        stretchLow:      0.01,
        stretchHigh:     0.99,
      })
    } catch (err) {
      if (detectGenRef.current !== gen) { showStatus(''); return }  // cancelled by mode switch — discard silently
      throw err
    }
    if (detectGenRef.current !== gen) { showStatus(''); return }
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
      qualityLevel:   cornerState.qualityLevel,
      minDistance,
      accent:         cornerState.accent,
      useStretch:     useStretchPreprocess,
    }
  }

  // ── Core image result applier (shared for LoadImage/LoadImageBytes) ─
  const applyLoadedImage = async (result, autoDetect = true) => {
    showStatus(`Loaded: ${result.width}x${result.height}`)
    setFitWidth(0)
    setPreview(result.preview)
    setImageLoaded(true)
    setRealImageDims({ w: result.width, h: result.height })
    setInputImageDims({ w: result.width, h: result.height })
    setImgNatural({ w: result.width, h: result.height })
    if (setInputImageDims) setInputImageDims({ w: result.width, h: result.height })
    setImageMeta({ format: result.format || '', dpiX: result.dpiX || 0, dpiY: result.dpiY || 0 })
    resetImageState()

    suggestedCornerParamsRef.current = result.suggestedCornerParams || {}

    setLoadingFull(false)
    clearUnsavedChanges()

    if (autoDetect && modeRef.current === 'corner') {
      await runDetectCorners(autoCornerParams ? suggestedCornerParamsRef.current : {})
    }

    setLoading(false)
  }

  // ── Core file loader (private — used by dialog, drag-drop, and launch args) ─
  const loadFile = async (filePath, autoDetect = true) => {
    CancelTouchup()
    setLoading(true)
    setLoadingFull(true)
    setZoom(1)
    const name = filePath.split(/[\/]/).pop()
    showStatus(`Loading ${name}…`)

    const result = await LoadImage({ filePath })
    await applyLoadedImage(result, autoDetect)
  }

  const loadImageFromBytes = async (arrayBuffer, sourceName = 'clipboard') => {
    CancelTouchup()
    setLoading(true)
    setLoadingFull(true)
    setZoom(1)
    showStatus(`Loading ${sourceName}…`)

    const bytes = Array.from(new Uint8Array(arrayBuffer))
    const result = await LoadImageBytes({ data: bytes, name: sourceName })
    await applyLoadedImage(result, true)
  }

  // ── Load image (dialog) ───────────────────────────────────────────────────
  const handleLoadImage = async () => {
    if (loading && !savingRef.current) return
    try {
      const filePath = await OpenImageDialog()
      if (!filePath) return
      if (savingRef.current) {
        pendingDropRef.current = filePath
        return
      }
      await loadFile(filePath)
    } catch (err) {
      console.error('Load error:', err)
      showError(err)
    } finally {
      if (!savingRef.current) {
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

  // ── Clipboard paste / browser URL drop image loading ─────────────────────────
  useEffect(() => {
    const onPaste = async (e) => {
      const items = e.clipboardData?.items
      if (!items || items.length === 0) return

      const imageItem = Array.from(items).find(i => i.type.startsWith('image/'))
      if (!imageItem) return

      e.preventDefault()
      const file = imageItem.getAsFile()
      if (!file) return

      try {
        const buffer = await file.arrayBuffer()
        await loadImageFromBytes(buffer, file.name || 'clipboard')
      } catch (err) {
        console.error('Clipboard image load error:', err)
        showError(err)
      }
    }

    const onDrop = async (e) => {
      if (!e.dataTransfer) return
      const file = e.dataTransfer.files?.[0]

      if (file && file.type.startsWith('image/') && !file.path) {
        e.preventDefault()
        try {
          const buffer = await file.arrayBuffer()
          await loadImageFromBytes(buffer, file.name || 'dropped-image')
          return
        } catch (err) {
          console.error('Drop image load error:', err)
          showError(err)
        }
      }

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

    window.addEventListener('paste', onPaste)
    window.addEventListener('drop', onDrop)

    return () => {
      window.removeEventListener('paste', onPaste)
      window.removeEventListener('drop', onDrop)
    }
  }, [])

  // ── Launch arguments ───────────────────────────────────────────────────────
  useEffect(() => {
    (async () => {
      try {
        const args = (await GetLaunchArgs()) || {}
        if (args.mode) setMode(args.mode)
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
        console.error('Launch args error:', err)
        setLoading(false)
        setLoadingFull(false)
        showStatus('No image loaded')
      }
    })()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Compositor: load result into corner-mode pipeline ───────────────────────
  // Called by CompositorModal via the onLoad prop.  Receives the ImageInfo
  // returned by CompositorLoadResult (Go) which the modal already called;
  // this function applies the corresponding React state reset, switches to
  // corner mode, and runs corner detection.
  const handleCompositorLoad = async (info) => {
    setLoading(true)
    try {
      setFitWidth(0)
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
      setMode('corner')
      await runDetectCorners(autoCornerParams ? suggestedCornerParamsRef.current : {})
    } catch (err) {
      showError(err)
    } finally {
      setLoading(false)
    }
  }

  // ── Corner detection ───────────────────────────────────────────────────────
  const handleDetectCorners = async () => {
    clearResetImagePending()
    setLoading(true)
    try {
      await runDetectCorners(autoCornerParams ? suggestedCornerParamsRef.current : {})
    } catch (err) {
      console.error('Detect error:', err)
    } finally {
      setLoading(false)
    }
  }

  // ── Mode-specific reset handlers ──────────────────────────────────────────
  const handleResetCorners = async () => {
    clearResetImagePending()
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
      setUseTouchupTool(false)
      markUnsavedChanges()
    } catch (err) {
      console.error('ResetCorners error:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleResetDisc = async () => {
    clearResetImagePending()
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting disc…')
    try {
      const result = await ResetDisc()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      setDiscActive(false)
      setDiscNoMaskPreview(null)
      setDiscCenter(null)
      setDiscRadius(0)
      setDiscRotation(0)
      setDiscBgColor({ r: 255, g: 255, b: 255 })
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
      setLoading(false)
    }
  }

  const handleResetNormal = async () => {
    clearResetImagePending()
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting normal crop…')
    try {
      const result = await ResetNormal()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      setNormalRect(null)
      setNormalCropApplied(false)
      setUseTouchupTool(false)
      markUnsavedChanges()
      showStatus(result?.message || 'Normal crop reset')
    } catch (err) {
      console.error('ResetNormal error:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleResetImage = async () => {
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting to original image…')
    try {
      const result = await ResetImage()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) {
        setRealImageDims({ w: result.width, h: result.height })
        setInputImageDims({ w: result.width, h: result.height })
        setImgNatural({ w: result.width, h: result.height })
      }
      resetImageState()
      clearUnsavedChanges()
      showStatus(result?.message || 'Image restored to loaded state')
    } catch (err) {
      console.error('ResetImage error:', err)
      showError(err)
    } finally {
      setLoading(false)
      setResetPending(false)
    }
  }

  const handleReset = async () => {
    if (resetImagePendingRef.current) {
      setConfirmDialog({
        message: 'This will undo ALL modifications to the current image. Are you sure?',
        onYes: async () => {
          setConfirmDialog(null)
          await handleResetImage()
        },
        onNo: () => {
          setConfirmDialog(null)
          setResetPending(true)
          showStatus('Reset cancelled — press again to confirm full reset')
        },
        onCancel: () => {
          setConfirmDialog(null)
          setResetPending(true)
          showStatus('Reset cancelled — press again to confirm full reset')
        },
        yesText: 'Yes',
        noText: 'No',
        cancelText: 'Cancel',
      })
      return
    }

    // Clear the current mode-specific crop/selection first.
    setConfirmDialog(null)
    if (mode === 'corner') await handleResetCorners()
    else if (mode === 'disc') await handleResetDisc()
    else if (mode === 'normal') await handleResetNormal()
    else await handleClearLines()

    showStatus('Press Reset again to restore the original loaded image')
    setResetPending(true)
  }

  const handleNormalCrop = async () => {
    clearResetImagePending()
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
    clearResetImagePending()
    CancelTouchup()
    setLoading(true)
    showStatus('Resetting lines…')
    try {
      const result = await ClearLines()
      setLinesDone(0)
      setLines([])
      setLinesProcessed(false)
      setUseTouchupTool(false)
      markUnsavedChanges()
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) setRealImageDims({ w: result.width, h: result.height })
      showStatus(result?.message || 'Lines cleared')
    } catch (err) {
      console.error('ClearLines error:', err)
    } finally {
      setLoading(false)
    }
  }

  // ── Undo ──────────────────────────────────────────────────────────────────
  const handleUndo = async () => {
    clearResetImagePending()
    setLoading(true)
    showStatus('Undoing…')
    try {
      const res = await Undo()
      if (res?.preview) setPreview(res.preview)
      if (res?.width && res?.height) setRealImageDims({ w: res.width, h: res.height })
      showStatus(res?.message || '')
      if (res?.uncropped) {
        // The undo took us back past the initial crop — return to the cropping
        // phase by resetting the mode-specific post-crop state.  Only act if
        // the frontend was actually in post-crop state to avoid clobbering
        // in-progress corner clicks or other pre-crop selections.
        if (mode === 'corner' && cornerState.cornerCount >= 4) {
          setCornerState(s => ({ ...s, cornerCount: 0 }))
          setSelectedCornerPts([])
          setUseTouchupTool(false)
        } else if (mode === 'disc' && discActive) {
          setDiscActive(false)
          setDragging(false)
          setDragStart(null)
          setDragCurrent(null)
          setUseTouchupTool(false)
          setUseStraightEdgeTool(false)
        } else if (mode === 'line' && linesProcessed) {
          setLinesProcessed(false)
          setUseTouchupTool(false)
        } else if (mode === 'normal' && normalCropApplied) {
          setNormalCropApplied(false)
          setNormalRect(null)
          setUseTouchupTool(false)
        }
        setBlackPoint(0)
        setWhitePoint(255)
      }
      markUnsavedChanges()
    } catch (err) {
      console.error('Undo error:', err)
      showError(err)
    } finally {
      setLoading(false)
    }
  }

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
  // Clears mode-specific frontend state without resetting any committed crop.
  // When a crop has been committed in the current mode (cornerCount>=4,
  // discActive, linesProcessed, normalCropApplied), warpedImage is preserved so
  // the user can continue editing (e.g. apply adjustments or crop again in a
  // different mode) without losing prior work.
  //
  // When switching to `corner`, attempts cached restoration via
  // `RestoreCornerOverlay` if detection settings match; otherwise optionally
  // runs `DetectCorners` (when `autoDetectOnModeSwitch`), or falls back to
  // `GetCleanPreview` which returns the current working image.
  const handleModeSwitch = async (m) => {
    if (m === mode) return
    setUseTouchupTool(false)
    setUseStraightEdgeTool(false)
    setMode(m)
    if (imageLoaded) {
      CancelTouchup()
      try {
        if (mode === 'corner') {
          const cropCommitted = cornerState.cornerCount >= 4
          detectGenRef.current++
          CancelCornerDetect()
          showStatus('')
          setLoading(false)
          // Only restore the pre-detection preview if no warp was committed
          if (!cropCommitted && cornerEntryRef.current) {
            const { preview, width, height } = cornerEntryRef.current
            setPreview(preview)
            if (width && height) {
              setRealImageDims({ w: width, h: height })
              const c = canvasRef.current
              if (c) setFitWidth(Math.min(c.clientWidth, c.clientHeight * width / height))
            }
          }
          cornerEntryRef.current = null
          // If a warp was committed, the detect settings are stale (they were
          // for the pre-warp image); clear them so re-entering corner mode
          // runs fresh detection on the warped result.
          if (cropCommitted) lastDetectSettings.current = null
          setCornerState(s => ({ ...s, cornerCount: 0 }))
          setCornersDetected(false)
          setDetectedCornerPts([])
          setSelectedCornerPts([])
          // When no crop committed, call ResetCorners to clean up selectedCorners
          // on the backend and get the currentImage preview.
          if (!cropCommitted) await ResetCorners()
        } else if (mode === 'disc') {
          if (discActive) {
            // Disc crop committed; preserve warpedImage, just clear UI state
            setDiscActive(false)
            setDiscNoMaskPreview(null)
            setDragging(false)
            setDragStart(null)
            setDragCurrent(null)
          }
          // If !discActive: no committed crop, no backend state to clean up
        } else if (mode === 'line') {
          // CancelLineProgress clears a.lines without touching warpedImage
          CancelLineProgress()
          setLinesDone(0)
          setLines([])
          setLinesProcessed(false)
        } else if (mode === 'normal') {
          // normalRect is frontend-only; no backend state to clean up
          setNormalRect(null)
          setNormalCropApplied(false)
        }

        if (m === 'corner' && lastDetectSettings.current) {
          const snap = lastDetectSettings.current
          if (snap.maxCorners === cornerState.maxCorners &&
              snap.qualityLevel === cornerState.qualityLevel &&
              snap.minDistance === cornerState.minDistance &&
              snap.accent === cornerState.accent &&
              snap.useStretch === useStretchPreprocess) {
            const restoreGen = detectGenRef.current
            setLoading(true)
            showStatus('Loading cached corners…')
            try {
              const res = await RestoreCornerOverlay({ dotRadius })
              if (detectGenRef.current !== restoreGen) return
              cornerEntryRef.current = { preview: res.preview, width: res.width, height: res.height }
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
              showStatus(res.message || '')
              return
            } catch (_) {
              // RestoreCornerOverlay failed (e.g. stale cache) — fall through to GetCleanPreview
            } finally {
              setLoading(false)
            }
          }
        }

        if (m === 'corner' && autoDetectOnModeSwitch) {
          setLoading(true)
          try {
            await runDetectCorners(autoCornerParams ? suggestedCornerParamsRef.current : {})
          } finally {
            setLoading(false)
          }
          return
        }

        // GetCleanPreview returns workingImage() — warpedImage if a crop was committed,
        // currentImage otherwise. This is the correct preview for the incoming mode.
        const res = await GetCleanPreview()
        if (res?.preview) {
          const c = canvasRef.current
          if (c && res.width && res.height) {
            setFitWidth(Math.min(c.clientWidth, c.clientHeight * res.width / res.height))
          } else {
            setFitWidth(0)
          }
          setPreview(res.preview)
          if (m === 'corner') cornerEntryRef.current = { preview: res.preview, width: res.width, height: res.height }
        }
        if (res?.width && res?.height) setRealImageDims({ w: res.width, h: res.height })
      } catch (err) {
        console.error('Mode switch error:', err)
        showError(err)
      }
    }
  }

  return {
    loadingFull,
    saving,
    resetImagePending,
    handleLoadImage,
    handleDetectCorners,
    handleCompositorLoad,
    handleReset,
    handleResetCorners,
    handleResetDisc,
    handleResetNormal,
    handleNormalCrop,
    handleClearLines,
    handleSaveImage,
    flushPendingSave,
    handleModeSwitch,
    handleUndo,
  }
}
