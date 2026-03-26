import { useState, useRef, useEffect, useLayoutEffect } from 'react'
import { LogFrontend, SetFeatherSize } from '../../wailsjs/go/main/App'

export function useZoomPan({ imgRef, mode, discActive, featherSize, setFeatherSize, setPreview }) {
  const [zoom, setZoom]               = useState(1)
  const [fitWidth, setFitWidth]       = useState(0)
  const [spacePanMode, setSpacePanMode] = useState(false)
  const [imgNatural, setImgNatural]   = useState({ w: 1, h: 1 })
  const canvasRef        = useRef(null)
  const pendingScrollRef = useRef(null)
  const mousePosRef      = useRef({ x: 0, y: 0 })
  const spaceDownRef     = useRef(false)
  const panDragRef       = useRef(null)  // {startX, startY, scrollLeft, scrollTop} while space+dragging
  const lastResizeRef    = useRef(0)     // timestamp of last window resize (to suppress post-maximize clicks)

  const handleImgLoad = () => {
    const el        = imgRef.current
    const container = canvasRef.current
    if (el) {
      const natW = el.naturalWidth; const natH = el.naturalHeight
      setImgNatural({ w: natW, h: natH })
      if (container && natW > 0 && natH > 0) {
        const aspect = natW / natH
        setFitWidth(Math.min(container.clientWidth, container.clientHeight * aspect))
      }
    }
  }

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

  // ── Scroll-wheel zoom (+ Ctrl+Scroll feather in disc mode) ─────────────────
  useEffect(() => {
    const el  = canvasRef.current
    if (!el) return
    const log = (msg) => LogFrontend(msg).catch(() => {})

    const handler = async (e) => {
      e.preventDefault(); e.stopPropagation(); e.stopImmediatePropagation()
      if (e.ctrlKey && mode === 'disc' && discActive) {
        const delta = e.deltaY < 0 ? 1 : -1
        const newF  = Math.max(0, Math.min(100, featherSize + delta))
        setFeatherSize(newF)
        try {
          const result = await SetFeatherSize({ size: newF })
          if (result?.preview) setPreview(result.preview)
        } catch (err) { console.error(err) }
        return
      }

      const factor = e.deltaY < 0 ? 1.1 : 0.9
      setZoom(z => {
        const newZ = Math.min(5, Math.max(0.1, z * factor))
        if (newZ === z) return z
        const canvasRect = el.getBoundingClientRect()
        const imgEl      = imgRef.current
        const imgRect    = imgEl ? imgEl.getBoundingClientRect() : canvasRect
        const ratio      = newZ / z
        // Cursor position relative to the image's own left/top edge, accounting
        // for any centering margin (margin:auto) that offsets the image within
        // the canvas container when the image is smaller than the viewport.
        pendingScrollRef.current = {
          left: (e.clientX - imgRect.left) * ratio - (e.clientX - canvasRect.left),
          top:  (e.clientY - imgRect.top)  * ratio - (e.clientY - canvasRect.top),
        }
        return newZ
      })
    }

    const scrollSpy = () => {
    }
    el.addEventListener('scroll', scrollSpy, { passive: true })
    el.addEventListener('wheel', handler, { passive: false, capture: true })
    return () => {
      el.removeEventListener('wheel', handler, { capture: true })
      el.removeEventListener('scroll', scrollSpy)
    }
  }, [mode, discActive, featherSize]) // eslint-disable-line react-hooks/exhaustive-deps

  // ── Space-key pan mode ────────────────────────────────────────────────────
  useEffect(() => {
    const onKeyDown = (e) => {
      if (e.code !== 'Space') return
      const active = document.activeElement
      if (active && (['INPUT', 'TEXTAREA', 'SELECT'].includes(active.tagName) || active.isContentEditable)) return
      e.preventDefault()  // must preventDefault for every event, including repeats, to suppress native scroll
      if (e.repeat) return
      spaceDownRef.current = true
      setSpacePanMode(true)
    }
    const onKeyUp = (e) => {
      if (e.code !== 'Space') return
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

  // Track window resizes so post-maximize stray clicks don't register as corners
  useEffect(() => {
    const onResize = () => { lastResizeRef.current = Date.now() }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [])

  useLayoutEffect(() => {
    const el  = canvasRef.current
    const log = (msg) => LogFrontend(msg).catch(() => {})
    if (pendingScrollRef.current) {
      if (el) {
        el.scrollLeft = pendingScrollRef.current.left
        el.scrollTop  = pendingScrollRef.current.top
      }
      pendingScrollRef.current = null
    }
  }, [zoom])

  return {
    zoom, setZoom,
    fitWidth, setFitWidth,
    spacePanMode,
    canvasRef,
    mousePosRef, spaceDownRef, panDragRef,
    lastResizeRef,
    handleImgLoad,
    setImgNatural,
  }
}
