import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { LogFrontend, SetFeatherSize } from '../../wailsjs/go/main/App'
import { fitWidthFor } from '../utils/previewLayout'

export function maxUsefulZoom(naturalWidth, fitWidth) {
  if (!(naturalWidth > 0) || !(fitWidth > 0)) return 5
  return Math.min(40, Math.max(5, (naturalWidth / fitWidth) * 2))
}

function validDims(value) {
  return value && Number.isFinite(value.w) && Number.isFinite(value.h) && value.w > 0 && value.h > 0
}

export function useZoomPan({
  imgRef,
  imageDims,
  mode,
  discActive,
  featherSize,
  setFeatherSize,
  setPreview,
  setRealImageDims,
}) {
  const [zoom, setZoom] = useState(1)
  const [fitWidth, setFitWidth] = useState(0)
  const [spacePanMode, setSpacePanMode] = useState(false)
  const [imgNatural, setImgNatural] = useState(validDims(imageDims) ? imageDims : { w: 1, h: 1 })

  const canvasRef = useRef(null)
  const pendingAnchorRef = useRef(null)
  const mousePosRef = useRef({ x: 0, y: 0 })
  const spaceDownRef = useRef(false)
  const panDragRef = useRef(null)
  const lastResizeRef = useRef(0)

  const handleImgLoad = (dims) => {
    const next = validDims(dims)
      ? dims
      : {
          w: Number(imgRef.current?.dataset?.naturalWidth) || Number(imgRef.current?.naturalWidth) || imgNatural.w,
          h: Number(imgRef.current?.dataset?.naturalHeight) || Number(imgRef.current?.naturalHeight) || imgNatural.h,
        }
    if (!validDims(next)) return
    setImgNatural(next)
    const fitted = fitWidthFor(canvasRef.current, next)
    if (fitted > 0) setFitWidth(fitted)
  }

  // The image dimensions now come from backend state, not from an <img>
  // decode. This keeps zoom math authoritative even while a viewport raster is
  // still loading.
  useEffect(() => {
    if (!validDims(imageDims)) return
    setImgNatural(current => (
      current.w === imageDims.w && current.h === imageDims.h ? current : imageDims
    ))
  }, [imageDims?.w, imageDims?.h])

  useEffect(() => {
    const el = canvasRef.current
    if (!el || !validDims(imgNatural)) return

    const updateFit = () => {
      const fitted = fitWidthFor(el, imgNatural)
      if (fitted > 0) setFitWidth(fitted)
    }
    updateFit()

    const observer = new ResizeObserver(updateFit)
    observer.observe(el)
    return () => observer.disconnect()
  }, [imgNatural])

  // Wheel zoom remains cursor anchored. Because the visible pixels are now a
  // viewport canvas, imgRef points to the transparent logical image surface.
  useEffect(() => {
    const el = canvasRef.current
    if (!el) return undefined

    const log = message => LogFrontend(message).catch(() => {})
    const handler = async event => {
      event.preventDefault()
      event.stopPropagation()
      event.stopImmediatePropagation()

      if (event.ctrlKey && mode === 'disc' && discActive) {
        const delta = event.deltaY < 0 ? 1 : -1
        const nextFeather = Math.max(0, Math.min(100, featherSize + delta))
        setFeatherSize(nextFeather)
        try {
          const result = await SetFeatherSize({ size: nextFeather })
          if (result?.preview) setPreview(result.preview)
          if (result?.width && result?.height) {
            setRealImageDims?.({ w: result.width, h: result.height })
          }
        } catch (error) {
          log(`SetFeatherSize failed: ${error?.message || error}`)
        }
        return
      }

      const imageEl = imgRef.current
      if (!imageEl) return
      const imageRect = imageEl.getBoundingClientRect()
      if (!(imageRect.width > 0) || !(imageRect.height > 0)) return

      const factor = event.deltaY < 0 ? 1.1 : 0.9
      const limit = maxUsefulZoom(imgNatural.w, fitWidth)
      setZoom(current => {
        const next = Math.min(limit, Math.max(0.1, current * factor))
        if (next === current) return current

        pendingAnchorRef.current = {
          u: Math.max(0, Math.min(1, (event.clientX - imageRect.left) / imageRect.width)),
          v: Math.max(0, Math.min(1, (event.clientY - imageRect.top) / imageRect.height)),
          clientX: event.clientX,
          clientY: event.clientY,
        }
        return next
      })
    }

    el.addEventListener('wheel', handler, { passive: false, capture: true })
    return () => el.removeEventListener('wheel', handler, { capture: true })
  }, [mode, discActive, featherSize, fitWidth, imgNatural.w, setFeatherSize, setPreview, setRealImageDims])

  useEffect(() => {
    const onKeyDown = event => {
      if (event.code !== 'Space') return
      const active = document.activeElement
      if (active && (['INPUT', 'TEXTAREA', 'SELECT'].includes(active.tagName) || active.isContentEditable)) return
      event.preventDefault()
      if (event.repeat) return
      spaceDownRef.current = true
      setSpacePanMode(true)
    }

    const onKeyUp = event => {
      if (event.code !== 'Space') return
      spaceDownRef.current = false
      setSpacePanMode(false)
      panDragRef.current = null
    }

    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('keyup', onKeyUp)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('keyup', onKeyUp)
    }
  }, [])

  useEffect(() => {
    const onResize = () => {
      lastResizeRef.current = Date.now()
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  useLayoutEffect(() => {
    const anchor = pendingAnchorRef.current
    const container = canvasRef.current
    const imageEl = imgRef.current
    if (!anchor || !container || !imageEl) return

    const rect = imageEl.getBoundingClientRect()
    const anchorX = rect.left + anchor.u * rect.width
    const anchorY = rect.top + anchor.v * rect.height
    container.scrollLeft += anchorX - anchor.clientX
    container.scrollTop += anchorY - anchor.clientY
    pendingAnchorRef.current = null
  }, [zoom])

  return {
    zoom,
    setZoom,
    fitWidth,
    setFitWidth,
    spacePanMode,
    canvasRef,
    mousePosRef,
    spaceDownRef,
    panDragRef,
    lastResizeRef,
    handleImgLoad,
    setImgNatural,
  }
}
