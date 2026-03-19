import { useState, useRef, useEffect } from 'react'
import { OnFileDrop, OnFileDropOff, Quit } from '../../wailsjs/runtime/runtime'
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
  GetCleanPreview,
  RestoreCornerOverlay,
  RecropImage,
  CancelTouchup,
  ResetDisc,
  ClearLines,
  SaveImage,
} from '../../wailsjs/go/main/App'

export function useImageActions({
  mode, loading, imageLoaded, discActive,
  cornerState, dotRadius, useStretchPreprocess, normalRect, closeAfterSave,
  setMode, setPreview, setLoading, setImageLoaded, setRealImageDims, setImgNatural,
  setZoom, setFitWidth, setCornerState, setLinesDone, setLinesProcessed,
  setDiscActive, setNormalRect, setNormalCropApplied, setCropSkipped, setCornersDetected,
  setDetectedCornerPts, setSelectedCornerPts, setLines, setBlackPoint, setWhitePoint,
  setUseTouchupTool, setUseStraightEdgeTool, setDragging, setDragStart, setDragCurrent,
  setConfirmDialog, setTouchupStrokes,
  touchupDraggingRef, canvasRef,
  showStatus, showError,
  setImageMeta,
}) {
  const [loadingFull, setLoadingFull] = useState(false)
  const [saving, setSaving]          = useState(false)
  const modeRef            = useRef(mode)
  const lastDetectSettings = useRef(null)
  const savingRef          = useRef(false)
  const pendingDropRef     = useRef(null)
  useEffect(() => { modeRef.current = mode }, [mode])

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
    setLines([])
    setTouchupStrokes([])
    setUseTouchupTool(false)
    setUseStraightEdgeTool(false)
    touchupDraggingRef.current = false
    setDetectedCornerPts([])
    setSelectedCornerPts([])
    setBlackPoint(0)
    setWhitePoint(255)
  }

  // ── Core corner detector (private — used by loadFile and handleDetectCorners) ─
  const runDetectCorners = async () => {
    showStatus('Detecting corners…')
    const result = await DetectCorners({
      maxCorners:   cornerState.maxCorners,
      qualityLevel: cornerState.qualityLevel,
      minDistance:  cornerState.minDistance,
      accentValue:  cornerState.accent,
      dotRadius,
      useStretch:   useStretchPreprocess,
      stretchLow:   0.01,
      stretchHigh:  0.99,
    })
    setPreview(result.preview)
    showStatus(result.message + ' — click 4 corners')
    if (result.width && result.height) setRealImageDims({ w: result.width, h: result.height })
    setDetectedCornerPts(result.corners || [])
    setSelectedCornerPts([])
    setCornerState(s => ({ ...s, cornerCount: 0 }))
    setCornersDetected(true)
    lastDetectSettings.current = {
      maxCorners:   cornerState.maxCorners,
      qualityLevel: cornerState.qualityLevel,
      minDistance:  cornerState.minDistance,
      accent:       cornerState.accent,
      useStretch:   useStretchPreprocess,
    }
  }

  // ── Core file loader (private — used by dialog, drag-drop, and launch args) ─
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
    setImageMeta({ format: result.format || '', dpiX: result.dpiX || 0, dpiY: result.dpiY || 0 })
    resetImageState()

    if (autoDetect && mode === 'corner') {
      await runDetectCorners()
    }

    setLoading(false)
    setLoadingFull(false)
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
      const filePath = paths[0]
      const ext = filePath.split('.').pop().toLowerCase()
      if (!['png', 'jpg', 'jpeg', 'tif', 'tiff', 'bmp', 'gif', 'webp'].includes(ext)) return
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

  // ── Launch arguments ───────────────────────────────────────────────────────
  useEffect(() => {
    (async () => {
      try {
        const args = await GetLaunchArgs()
        if (args.mode) setMode(args.mode)
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

  // ── Corner detection ───────────────────────────────────────────────────────
  const handleDetectCorners = async () => {
    setLoading(true)
    try {
      await runDetectCorners()
    } catch (err) {
      console.error('Detect error:', err)
    } finally {
      setLoading(false)
    }
  }

  // ── Skip crop ─────────────────────────────────────────────────────────────
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

  // ── Re-crop ───────────────────────────────────────────────────────────────
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
          setImageMeta({ format: '', dpiX: 0, dpiY: 0 })
          resetImageState()
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

  // ── Mode-specific reset handlers ──────────────────────────────────────────
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

  // ── Save ──────────────────────────────────────────────────────────────────
  const handleSaveImage = async () => {
    try {
      const filePath = await OpenSaveDialog()
      if (!filePath) return
      setLoading(true)
      savingRef.current = true
      setSaving(true)
      showStatus('Saving…')
      const result = await SaveImage({ outputPath: filePath })
      const savedName = filePath.split(/[\\/]/).pop()
      showStatus(result?.message || `Saved to ${savedName}`)
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
  }

  // ── Mode switch ───────────────────────────────────────────────────────────
  const handleModeSwitch = async (m) => {
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
          await ResetCorners()
        } else if (mode === 'disc' && discActive) {
          await ResetDisc(); setDiscActive(false); setCropSkipped(false)
        } else if (mode === 'line') {
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

        if (m === 'corner' && lastDetectSettings.current) {
          const snap = lastDetectSettings.current
          if (snap.maxCorners === cornerState.maxCorners &&
              snap.qualityLevel === cornerState.qualityLevel &&
              snap.minDistance === cornerState.minDistance &&
              snap.accent === cornerState.accent &&
              snap.useStretch === useStretchPreprocess) {
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
            } catch (_) {
              // RestoreCornerOverlay failed (e.g. stale cache) — fall through to GetCleanPreview
            }
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
      } catch (err) {
        console.error('Mode switch error:', err)
        showError(err)
      }
    }
    setMode(m)
  }

  return {
    loadingFull,
    saving,
    handleLoadImage,
    handleDetectCorners,
    handleSkipCrop,
    handleRecrop,
    handleResetCorners,
    handleResetDisc,
    handleResetNormal,
    handleNormalCrop,
    handleClearLines,
    handleSaveImage,
    handleModeSwitch,
  }
}
