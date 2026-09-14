import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  CalibrationClearPreview,
  CalibrationLoadImage,
  CalibrationOpenFilesDialog,
  CalibrationSaveDataset,
  RenderCalibrationPreviewViewport,
} from '../../wailsjs/go/main/App'
import PreviewCanvas from './PreviewCanvas'

export const CALIBRATION_CORNER_LABELS = ['TL', 'TR', 'BR', 'BL']
const VALID_EXTENSIONS = new Set(['png', 'jpg', 'jpeg', 'tif', 'tiff', 'bmp', 'gif', 'webp'])
const ZOOM_LEVELS = [0, 0.25, 0.5, 1, 2]

function pathKey(path) {
  return String(path || '').replaceAll('\\', '/').toLowerCase()
}

export function normalizeCalibrationPaths(existing, additions) {
  const result = [...existing]
  const seen = new Set(existing.map(pathKey))
  for (const path of additions || []) {
    const extension = String(path).split('.').pop().toLowerCase()
    const key = pathKey(path)
    if (!path || !VALID_EXTENSIONS.has(extension) || seen.has(key)) continue
    seen.add(key)
    result.push(path)
  }
  return result
}

export function calibrationPointFromEvent(event, element, dimensions) {
  if (!element || !dimensions?.w || !dimensions?.h) return null
  const rect = element.getBoundingClientRect()
  if (!(rect.width > 0) || !(rect.height > 0)) return null
  const x = Math.round(((event.clientX - rect.left) / rect.width) * dimensions.w)
  const y = Math.round(((event.clientY - rect.top) / rect.height) * dimensions.h)
  return {
    x: Math.max(0, Math.min(dimensions.w - 1, x)),
    y: Math.max(0, Math.min(dimensions.h - 1, y)),
  }
}

function fileName(path) {
  return String(path || '').replace(/.*[\\/]/, '')
}

export default function CornerCalibrationModal({ open, onClose, dropRef }) {
  const [paths, setPaths] = useState([])
  const [currentIndex, setCurrentIndex] = useState(-1)
  const [imageInfo, setImageInfo] = useState(null)
  const [presentedSource, setPresentedSource] = useState(null)
  const [corners, setCorners] = useState([])
  const [drafts, setDrafts] = useState({})
  const [annotations, setAnnotations] = useState({})
  const [skipped, setSkipped] = useState({})
  const [zoom, setZoom] = useState(0)
  const [loading, setLoading] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [status, setStatus] = useState('Drop scans here or add them with the file picker.')
  const [dirty, setDirty] = useState(false)
  const scrollRef = useRef(null)
  const imageRef = useRef(null)
  const loadGenerationRef = useRef(0)
  const draftsRef = useRef(drafts)
  const annotationsRef = useRef(annotations)
  const panRef = useRef(null)

  draftsRef.current = drafts
  annotationsRef.current = annotations
  const currentPath = currentIndex >= 0 ? paths[currentIndex] : null
  const annotatedCount = Object.keys(annotations).length
  const previewPending = !!imageInfo && presentedSource !== imageInfo.preview
  const busy = loading || previewPending

  const addPaths = useCallback(additions => {
    const supported = normalizeCalibrationPaths([], additions)
    if (supported.length === 0) {
      setStatus('No new supported images were added.')
      return
    }
    setPaths(previous => {
      const next = normalizeCalibrationPaths(previous, supported)
      setStatus(`${next.length} scan${next.length === 1 ? '' : 's'} queued.`)
      return next
    })
    setCurrentIndex(index => index < 0 ? 0 : index)
  }, [])

  useEffect(() => {
    if (!dropRef) return undefined
    dropRef.current = open ? addPaths : null
    return () => { if (dropRef.current === addPaths) dropRef.current = null }
  }, [addPaths, dropRef, open])

  useEffect(() => {
    if (!open || !currentPath) {
      setImageInfo(null)
      setCorners([])
      return undefined
    }
    const generation = ++loadGenerationRef.current
    const saved = draftsRef.current[currentPath] || annotationsRef.current[currentPath]?.corners || []
    setCorners(saved)
    setImageInfo(null)
    setPresentedSource(null)
    setLoading(true)
    setZoom(0)
    setStatus(`Loading ${fileName(currentPath)}…`)
    CalibrationLoadImage({ filePath: currentPath }).then(result => {
      if (loadGenerationRef.current !== generation) return
      setImageInfo({
        path: currentPath,
        preview: result.preview,
        width: result.width,
        height: result.height,
      })
      setStatus(`Click ${CALIBRATION_CORNER_LABELS[saved.length] || 'Save & next'} — order is TL, TR, BR, BL.`)
    }).catch(error => {
      if (loadGenerationRef.current !== generation) return
      setStatus(`Could not load ${fileName(currentPath)}: ${error?.message || error}`)
    }).finally(() => {
      if (loadGenerationRef.current === generation) setLoading(false)
    })
    return () => { loadGenerationRef.current++ }
  }, [currentPath, open])

  const rememberDraft = useCallback((path, points) => {
    setDrafts(previous => ({ ...previous, [path]: points }))
    setDirty(true)
  }, [])

  const undoCorner = useCallback(() => {
    if (!currentPath) return
    setCorners(previous => {
      const next = previous.slice(0, -1)
      rememberDraft(currentPath, next)
      setStatus(`Click ${CALIBRATION_CORNER_LABELS[next.length] || 'the next corner'}.`)
      return next
    })
  }, [currentPath, rememberDraft])

  const clearCorners = useCallback(() => {
    if (!currentPath) return
    setCorners([])
    rememberDraft(currentPath, [])
    setStatus('Click TL — top-left corner.')
  }, [currentPath, rememberDraft])

  const rewindToCorner = useCallback(index => {
    if (!currentPath) return
    const next = corners.slice(0, index)
    setCorners(next)
    rememberDraft(currentPath, next)
    setStatus(`Re-place ${CALIBRATION_CORNER_LABELS[index]}.`)
  }, [corners, currentPath, rememberDraft])

  const goToNextPending = useCallback(() => {
    if (paths.length === 0) return
    for (let offset = 1; offset < paths.length; offset++) {
      const index = (currentIndex + offset) % paths.length
      const path = paths[index]
      if (!annotationsRef.current[path] && !skipped[path]) {
        setCurrentIndex(index)
        return
      }
    }
    if (currentIndex < paths.length - 1) setCurrentIndex(currentIndex + 1)
  }, [currentIndex, paths, skipped])

  const saveAndNext = useCallback(() => {
    if (!currentPath || !imageInfo || corners.length !== 4) return
    const entry = {
      imagePath: currentPath,
      width: imageInfo.width,
      height: imageInfo.height,
      corners: corners.map(point => ({ x: point.x, y: point.y })),
    }
    setAnnotations(previous => ({ ...previous, [currentPath]: entry }))
    setSkipped(previous => {
      const next = { ...previous }
      delete next[currentPath]
      return next
    })
    rememberDraft(currentPath, corners)
    setDirty(true)
    setStatus(`Annotated ${fileName(currentPath)}.`)
    annotationsRef.current = { ...annotationsRef.current, [currentPath]: entry }
    goToNextPending()
  }, [corners, currentPath, goToNextPending, imageInfo, rememberDraft])

  const skipAndNext = useCallback(() => {
    if (!currentPath) return
    setSkipped(previous => ({ ...previous, [currentPath]: true }))
    setStatus(`Skipped ${fileName(currentPath)}.`)
    goToNextPending()
  }, [currentPath, goToNextPending])

  const handleCanvasMouseDown = useCallback(event => {
    if (event.button === 1 && scrollRef.current) {
      event.preventDefault()
      panRef.current = {
        x: event.clientX,
        y: event.clientY,
        left: scrollRef.current.scrollLeft,
        top: scrollRef.current.scrollTop,
      }
      return
    }
    if (event.button !== 0 || event.target !== imageRef.current || busy || !imageInfo || corners.length >= 4) return
    const point = calibrationPointFromEvent(event, imageRef.current, { w: imageInfo.width, h: imageInfo.height })
    if (!point) return
    const next = [...corners, point]
    setCorners(next)
    rememberDraft(currentPath, next)
    setStatus(next.length === 4
      ? 'Four corners set. Verify the outline, then choose Save & next.'
      : `Click ${CALIBRATION_CORNER_LABELS[next.length]} — order is TL, TR, BR, BL.`)
  }, [busy, corners, currentPath, imageInfo, rememberDraft])

  const handleCanvasMouseMove = useCallback(event => {
    const pan = panRef.current
    if (!pan || !scrollRef.current) return
    scrollRef.current.scrollLeft = pan.left - (event.clientX - pan.x)
    scrollRef.current.scrollTop = pan.top - (event.clientY - pan.y)
  }, [])

  const stopPan = useCallback(() => { panRef.current = null }, [])

  const exportDataset = useCallback(async () => {
    const entries = paths.map(path => annotations[path]).filter(Boolean)
    if (entries.length === 0) return
    setExporting(true)
    setStatus('Preparing ground-truth dataset…')
    try {
      const result = await CalibrationSaveDataset({ entries })
      if (!result?.cancelled) {
        setDirty(false)
        setStatus(`Exported ${result.count} sample${result.count === 1 ? '' : 's'} to ${result.outputPath}`)
      } else {
        setStatus('Export cancelled; annotations are still in this session.')
      }
    } catch (error) {
      setStatus(`Export failed: ${error?.message || error}`)
    } finally {
      setExporting(false)
    }
  }, [annotations, paths])

  const requestClose = useCallback(() => {
    if (dirty && !window.confirm('Close without exporting the latest corner annotations?')) return
    loadGenerationRef.current++
    CalibrationClearPreview().catch(() => {})
    onClose()
  }, [dirty, onClose])

  useEffect(() => {
    if (!open) return undefined
    const handler = event => {
      // This modal owns keyboard focus even though the editor remains mounted
      // behind it. Do not let unhandled crop/save shortcuts reach the editor.
      event.stopImmediatePropagation()
      if (event.key === 'Escape') {
        event.preventDefault()
        requestClose()
      } else if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'z') {
        event.preventDefault()
        undoCorner()
      } else if (event.key === 'Enter' && corners.length === 4) {
        event.preventDefault()
        saveAndNext()
      }
    }
    window.addEventListener('keydown', handler, true)
    return () => window.removeEventListener('keydown', handler, true)
  }, [corners.length, open, requestClose, saveAndNext, undoCorner])

  const visual = useMemo(() => ({
    mode: 'corner',
    realImageDims: imageInfo ? { w: imageInfo.width, h: imageInfo.height } : { w: 1, h: 1 },
    detectedCornerPts: [],
    selectedCornerPts: corners.map(point => ({ X: point.x, Y: point.y })),
    dotRadius: 5,
    showCornerSequence: true,
    cornerLabels: CALIBRATION_CORNER_LABELS,
  }), [corners, imageInfo])

  if (!open) return null

  return (
    <div className="options-backdrop visible calibration-backdrop" onMouseDown={event => {
      if (event.target === event.currentTarget) requestClose()
    }}>
      <div className="options-dialog calibration-dialog" role="dialog" aria-modal="true" aria-label="Corner calibration">
        <div className="options-header">
          <div>
            <div className="options-title">Corner ground-truth calibration</div>
            <div className="calibration-subtitle">Source pixels · click TL → TR → BR → BL</div>
          </div>
          <button className="options-close" onClick={requestClose} aria-label="Close">✕</button>
        </div>

        <div className="calibration-body">
          <aside className="calibration-queue">
            <div className="calibration-summary">{annotatedCount}/{paths.length} annotated</div>
            <div className="calibration-file-list">
              {paths.length === 0 && <div className="calibration-empty">Drop a batch of scans anywhere in this window.</div>}
              {paths.map((path, index) => {
                const state = annotations[path] ? 'done' : skipped[path] ? 'skipped' : drafts[path]?.length ? 'draft' : 'pending'
                return (
                  <button
                    key={pathKey(path)}
                    className={`calibration-file calibration-file--${state}${index === currentIndex ? ' active' : ''}`}
                    onClick={() => setCurrentIndex(index)}
                    title={path}
                  >
                    <span className="calibration-file-state">{state === 'done' ? '✓' : state === 'skipped' ? '–' : state === 'draft' ? drafts[path].length : '·'}</span>
                    <span className="calibration-file-name">{fileName(path)}</span>
                  </button>
                )
              })}
            </div>
            <button className="load-btn calibration-add" onClick={async () => {
              try { addPaths(await CalibrationOpenFilesDialog()) } catch (error) { setStatus(error?.message || String(error)) }
            }} disabled={busy || exporting}>Add scans…</button>
          </aside>

          <section className="calibration-workspace">
            <div className="calibration-toolbar">
              <div className="calibration-zoom" aria-label="Preview zoom">
                {ZOOM_LEVELS.map(level => (
                  <button key={level} className={zoom === level ? 'active' : ''} onClick={() => setZoom(level)} disabled={!imageInfo}>
                    {level === 0 ? 'Fit' : `${Math.round(level * 100)}%`}
                  </button>
                ))}
              </div>
              {imageInfo && <span>{imageInfo.width} × {imageInfo.height}px</span>}
              <span className="calibration-next-corner">{corners.length < 4 ? `Next: ${CALIBRATION_CORNER_LABELS[corners.length]}` : 'Ready to save'}</span>
            </div>

            <div className="calibration-canvas-shell">
              {busy && <div className="calibration-loading"><span className="btn-spinner" /> {loading ? 'Loading full-resolution scan…' : 'Rendering preview…'}</div>}
              <PreviewCanvas
                source={imageInfo?.preview || null}
                imageDims={imageInfo ? { w: imageInfo.width, h: imageInfo.height } : { w: 1, h: 1 }}
                displayWidth={imageInfo && zoom > 0 ? imageInfo.width * zoom : null}
                scrollRef={scrollRef}
                imgRef={imageRef}
                cursor="crosshair"
                onImageMouseLeave={stopPan}
                onMouseDown={handleCanvasMouseDown}
                onMouseMove={handleCanvasMouseMove}
                onMouseUp={stopPan}
                onContextMenu={event => event.preventDefault()}
                scrollerStyle={zoom === 0
                  ? { overflow: 'hidden' }
                  : { overflow: 'scroll', scrollbarGutter: 'stable' }}
                showPlaceholder={!imageInfo && !loading}
                visual={visual}
                renderPreviewViewport={RenderCalibrationPreviewViewport}
                onPresented={source => setPresentedSource(source)}
              />
            </div>

            <div className="calibration-status">
              <span className="calibration-status-message">{status}</span>
              <div className="calibration-coordinates">
                {corners.map((point, index) => (
                  <button key={CALIBRATION_CORNER_LABELS[index]} onClick={() => rewindToCorner(index)} title={`Re-place ${CALIBRATION_CORNER_LABELS[index]}`}>
                    {CALIBRATION_CORNER_LABELS[index]} {point.x},{point.y}
                  </button>
                ))}
              </div>
            </div>
            <div className="calibration-actions">
              <button className="options-btn" onClick={undoCorner} disabled={corners.length === 0 || busy}>Undo point</button>
              <button className="reset-btn-danger" onClick={clearCorners} disabled={corners.length === 0 || busy}>Clear points</button>
              <button className="options-btn" onClick={skipAndNext} disabled={!currentPath || busy}>Skip</button>
              <button className="primary" onClick={saveAndNext} disabled={corners.length !== 4 || busy}>Save & next</button>
            </div>
          </section>
        </div>

        <div className="options-footer calibration-footer">
          <span>Middle-drag to pan · Ctrl+Z undoes a point · Enter saves</span>
          <button className="load-btn" onClick={exportDataset} disabled={annotatedCount === 0 || exporting}>
            {exporting ? <span className="btn-spinner" /> : `Export JSON (${annotatedCount})`}
          </button>
          <button className="options-btn" onClick={requestClose}>Close</button>
        </div>
      </div>
    </div>
  )
}
