import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import ImageOverlays from './ImageOverlays'
import { RenderPreviewViewport } from '../../wailsjs/go/main/App'
import './PreviewCanvas.css'

const PREVIEW_PREFIX = '/__atropos/preview/'
const OVERSCAN_FRACTION = 0.25
const SOURCE_QUANTUM = 64
const MAX_RENDER_DIMENSION = 4096
const MAX_RENDER_PIXELS = 16 * 1024 * 1024
const CLIENT_RASTER_CACHE_SIZE = 4
const REQUEST_DEBOUNCE_MS = 120
const CHECKER_SIZE = 16
const CHECKER_DARK = '#252525'
const CHECKER_LIGHT = '#303030'

const checkerPatternCache = new WeakMap()

function clamp(value, min, max) {
  return Math.max(min, Math.min(max, value))
}

function validDims(dims) {
  return dims && Number.isFinite(dims.w) && Number.isFinite(dims.h) && dims.w > 0 && dims.h > 0
}

function sameDims(a, b) {
  return validDims(a) && validDims(b) && a.w === b.w && a.h === b.h
}

function normalizeRect(rect) {
  return {
    x: rect.x,
    y: rect.y,
    w: Math.max(1, rect.w),
    h: Math.max(1, rect.h),
  }
}

export function sourceRectContains(container, inner, epsilon = 1) {
  if (!container || !inner) return false
  return (
    inner.x >= container.x - epsilon &&
    inner.y >= container.y - epsilon &&
    inner.x + inner.w <= container.x + container.w + epsilon &&
    inner.y + inner.h <= container.y + container.h + epsilon
  )
}

function intersectRect(a, b) {
  const x1 = Math.max(a.x, b.x)
  const y1 = Math.max(a.y, b.y)
  const x2 = Math.min(a.x + a.w, b.x + b.w)
  const y2 = Math.min(a.y + a.h, b.y + b.h)
  if (x2 <= x1 || y2 <= y1) return null
  return { x: x1, y: y1, w: x2 - x1, h: y2 - y1 }
}

function quantizeRect(rect, dims) {
  const x1 = clamp(Math.floor(rect.x / SOURCE_QUANTUM) * SOURCE_QUANTUM, 0, dims.w)
  const y1 = clamp(Math.floor(rect.y / SOURCE_QUANTUM) * SOURCE_QUANTUM, 0, dims.h)
  const x2 = clamp(Math.ceil((rect.x + rect.w) / SOURCE_QUANTUM) * SOURCE_QUANTUM, 0, dims.w)
  const y2 = clamp(Math.ceil((rect.y + rect.h) / SOURCE_QUANTUM) * SOURCE_QUANTUM, 0, dims.h)
  return {
    x: Math.floor(x1),
    y: Math.floor(y1),
    w: Math.max(1, Math.ceil(x2 - x1)),
    h: Math.max(1, Math.ceil(y2 - y1)),
  }
}

function boundedDestinationSize(sourceRect, cssScale, dpr) {
  let width = Math.max(1, Math.min(sourceRect.w, Math.ceil(sourceRect.w * cssScale * dpr)))
  let height = Math.max(1, Math.min(sourceRect.h, Math.ceil(sourceRect.h * cssScale * dpr)))

  let factor = Math.min(1, MAX_RENDER_DIMENSION / width, MAX_RENDER_DIMENSION / height)
  if (width * height * factor * factor > MAX_RENDER_PIXELS) {
    factor = Math.min(factor, Math.sqrt(MAX_RENDER_PIXELS / (width * height)))
  }
  if (factor < 1) {
    width = Math.max(1, Math.floor(width * factor))
    height = Math.max(1, Math.floor(height * factor))
  }
  return { width, height }
}

export function buildViewportRequest({ source, dims, visibleRect, displayWidth, dpr = 1 }) {
  if (!source || !validDims(dims) || !(displayWidth > 0)) return null

  const scale = displayWidth / dims.w
  const visible = normalizeRect(visibleRect || { x: 0, y: 0, w: dims.w, h: dims.h })
  const padX = Math.max(32, visible.w * OVERSCAN_FRACTION)
  const padY = Math.max(32, visible.h * OVERSCAN_FRACTION)
  const expanded = {
    x: clamp(visible.x - padX, 0, dims.w),
    y: clamp(visible.y - padY, 0, dims.h),
    w: 1,
    h: 1,
  }
  const right = clamp(visible.x + visible.w + padX, 0, dims.w)
  const bottom = clamp(visible.y + visible.h + padY, 0, dims.h)
  expanded.w = Math.max(1, right - expanded.x)
  expanded.h = Math.max(1, bottom - expanded.y)

  const rect = quantizeRect(expanded, dims)
  const destination = boundedDestinationSize(rect, scale, Math.max(1, dpr || 1))

  if (!source.startsWith(PREVIEW_PREFIX)) {
    return {
      url: source,
      rect: { x: 0, y: 0, w: dims.w, h: dims.h },
      width: dims.w,
      height: dims.h,
      key: `${source}:legacy-full`,
    }
  }

  const params = new URLSearchParams({
    x: String(rect.x),
    y: String(rect.y),
    w: String(rect.w),
    h: String(rect.h),
    dw: String(destination.width),
    dh: String(destination.height),
    q: '88',
  })
  const separator = source.includes('?') ? '&' : '?'

  return {
    // Keep the URL for diagnostics/backwards compatibility, but the canvas
    // renderer deliberately transports preview rasters through the Wails RPC
    // binding. WebView2 has been observed to let the custom AssetsHandler
    // finish a query-parameterized image response without ever settling the
    // corresponding HTMLImageElement load/error lifecycle.
    url: `${source}${separator}${params.toString()}`,
    rpc: {
      preview: source,
      x: rect.x,
      y: rect.y,
      width: rect.w,
      height: rect.h,
      destWidth: destination.width,
      destHeight: destination.height,
      quality: 88,
    },
    rect,
    width: destination.width,
    height: destination.height,
    key: `${source}:${rect.x},${rect.y},${rect.w},${rect.h}:${destination.width}x${destination.height}`,
  }
}

function beginImageLoad(source) {
  const image = new Image()
  image.decoding = 'async'
  image.draggable = false

  let settled = false
  let rejectPromise = null
  const promise = new Promise((resolve, reject) => {
    rejectPromise = reject
    image.onload = () => {
      if (settled) return
      settled = true
      image.onload = null
      image.onerror = null
      resolve(image)
    }
    image.onerror = () => {
      if (settled) return
      settled = true
      image.onload = null
      image.onerror = null
      reject(new Error(`Preview image decode failed: ${source}`))
    }
    image.src = source
  })

  const abort = () => {
    if (settled) return
    settled = true
    image.onload = null
    image.onerror = null
    // Cancel the browser-side decode. Viewport rasters are data URLs, so this
    // does not cancel backend work; backend viewport calls
    // are separately serialized/coalesced by requestRaster.
    image.removeAttribute('src')
    const error = new Error('Preview image load aborted')
    error.name = 'AbortError'
    rejectPromise?.(error)
  }

  return { promise, abort }
}

function closeRaster(raster) {
  try {
    if (typeof raster?.bitmap?.close === 'function') {
      raster.bitmap.close()
    } else {
      // HTMLImageElement is the canvas raster source. Dropping src lets the
      // decoded surface be reclaimed after LRU eviction.
      raster?.bitmap?.removeAttribute?.('src')
    }
  } catch (_) {
    // Raster release is best-effort.
  }
}

function canvasPointFromImage(point, layout, dims) {
  return {
    x: layout.stageX + (point.x / dims.w) * layout.stageWidth,
    y: layout.stageY + (point.y / dims.h) * layout.stageHeight,
  }
}

function canvasRectFromImage(rect, layout, dims) {
  const a = canvasPointFromImage({ x: rect.x, y: rect.y }, layout, dims)
  const b = canvasPointFromImage({ x: rect.x + rect.w, y: rect.y + rect.h }, layout, dims)
  return { x: a.x, y: a.y, w: b.x - a.x, h: b.y - a.y }
}

// WebKit can distort a canvas draw when its destination rectangle grows past
// the GPU texture limit, even when almost all of that rectangle is clipped by
// the canvas. This happens at high zoom while the full-frame fit raster remains
// in the cache behind a small viewport raster. Convert the canvas intersection
// back into bitmap coordinates so drawImage only receives viewport-sized
// source and destination rectangles.
export function clippedRasterDrawRect(raster, layout, viewport) {
  if (!raster || !validDims(raster.dims) || !(viewport?.w > 0) || !(viewport?.h > 0)) return null

  const destination = canvasRectFromImage(raster.rect, layout, raster.dims)
  if (!(destination.w > 0) || !(destination.h > 0)) return null

  const visible = intersectRect(destination, { x: 0, y: 0, w: viewport.w, h: viewport.h })
  if (!visible) return null

  const bitmapWidth = raster.bitmap?.naturalWidth || raster.bitmap?.width || raster.width
  const bitmapHeight = raster.bitmap?.naturalHeight || raster.bitmap?.height || raster.height
  if (!(bitmapWidth > 0) || !(bitmapHeight > 0)) return null

  return {
    source: {
      x: (visible.x - destination.x) * bitmapWidth / destination.w,
      y: (visible.y - destination.y) * bitmapHeight / destination.h,
      w: visible.w * bitmapWidth / destination.w,
      h: visible.h * bitmapHeight / destination.h,
    },
    destination: visible,
  }
}

function visibleImageRect(layout, dims, scrollLeft, scrollTop, viewport) {
  if (!validDims(dims) || !(layout.stageWidth > 0) || !(layout.stageHeight > 0)) {
    return { x: 0, y: 0, w: dims?.w || 1, h: dims?.h || 1 }
  }

  const viewportInContent = { x: scrollLeft, y: scrollTop, w: viewport.w, h: viewport.h }
  const stageInContent = {
    x: layout.stageLeft,
    y: layout.stageTop,
    w: layout.stageWidth,
    h: layout.stageHeight,
  }
  const intersection = intersectRect(viewportInContent, stageInContent)
  if (!intersection) return { x: 0, y: 0, w: dims.w, h: dims.h }

  const localX = intersection.x - layout.stageLeft
  const localY = intersection.y - layout.stageTop
  return {
    x: clamp((localX / layout.stageWidth) * dims.w, 0, dims.w),
    y: clamp((localY / layout.stageHeight) * dims.h, 0, dims.h),
    w: clamp((intersection.w / layout.stageWidth) * dims.w, 1, dims.w),
    h: clamp((intersection.h / layout.stageHeight) * dims.h, 1, dims.h),
  }
}

function checkerboardPattern(ctx) {
  const cached = checkerPatternCache.get(ctx)
  if (cached) return cached

  const tile = document.createElement('canvas')
  tile.width = CHECKER_SIZE * 2
  tile.height = CHECKER_SIZE * 2
  const tileCtx = tile.getContext('2d')
  if (!tileCtx) return null

  tileCtx.fillStyle = CHECKER_DARK
  tileCtx.fillRect(0, 0, tile.width, tile.height)
  tileCtx.fillStyle = CHECKER_LIGHT
  tileCtx.fillRect(0, 0, CHECKER_SIZE, CHECKER_SIZE)
  tileCtx.fillRect(CHECKER_SIZE, CHECKER_SIZE, CHECKER_SIZE, CHECKER_SIZE)

  const pattern = ctx.createPattern(tile, 'repeat')
  if (pattern) checkerPatternCache.set(ctx, pattern)
  return pattern
}

export function shouldDrawCheckerboard(source, dims) {
  return Boolean(source) && validDims(dims)
}

export function drawCheckerboardSkeleton(ctx, layout, viewport = null) {
  if (!ctx || !layout || !(layout.stageWidth > 0) || !(layout.stageHeight > 0)) return

  const pattern = checkerboardPattern(ctx)
  if (!pattern) return

  const stageRect = {
    x: layout.stageX,
    y: layout.stageY,
    w: layout.stageWidth,
    h: layout.stageHeight,
  }
  const fill = viewport?.w > 0 && viewport?.h > 0
    ? intersectRect(stageRect, { x: 0, y: 0, w: viewport.w, h: viewport.h })
    : stageRect
  if (!fill) return

  // Anchor the checkerboard to image space rather than viewport space. Panning
  // therefore moves the skeleton with the logical image instead of making the
  // pattern appear to swim underneath it. Explicitly limit the fill to the
  // viewport because WebKit can mishandle oversized offscreen canvas draws.
  ctx.save()
  ctx.translate(layout.stageX, layout.stageY)
  ctx.fillStyle = pattern
  ctx.fillRect(
    fill.x - layout.stageX,
    fill.y - layout.stageY,
    fill.w,
    fill.h,
  )
  ctx.restore()
}

function drawCircle(ctx, x, y, radius, fill, stroke, lineWidth = 1) {
  ctx.beginPath()
  ctx.arc(x, y, Math.max(0, radius), 0, Math.PI * 2)
  if (fill) {
    ctx.fillStyle = fill
    ctx.fill()
  }
  if (stroke) {
    ctx.strokeStyle = stroke
    ctx.lineWidth = lineWidth
    ctx.stroke()
  }
}

function drawDashedRect(ctx, x, y, width, height, {
  fill = 'rgba(0,255,0,0.08)',
  stroke = '#00ff00',
} = {}) {
  ctx.save()
  ctx.fillStyle = fill
  ctx.fillRect(x, y, width, height)
  ctx.strokeStyle = stroke
  ctx.lineWidth = 2
  ctx.setLineDash([6, 3])
  ctx.strokeRect(x, y, width, height)
  ctx.restore()
}

function drawVisualGuides(ctx, visual, layout, displayToImage, lineStartImgRef, ctrlDragRef, shiftDragRef) {
  if (!visual || !validDims(visual.realImageDims)) return
  const dims = visual.realImageDims
  const imageScale = layout.stageWidth / dims.w
  const toCanvas = point => canvasPointFromImage(point, layout, dims)

  if (visual.adjustmentSelectionActive) {
    const palette = { fill: 'rgba(145,145,145,0.08)', stroke: '#a9a9a9' }
    if (visual.dragging && visual.dragStart && visual.dragCurrent && !visual.adjustmentRect && !visual.useTouchupTool) {
      const x = layout.stageX + Math.min(visual.dragStart.x, visual.dragCurrent.x)
      const y = layout.stageY + Math.min(visual.dragStart.y, visual.dragCurrent.y)
      const width = Math.abs(visual.dragCurrent.x - visual.dragStart.x)
      const height = Math.abs(visual.dragCurrent.y - visual.dragStart.y)
      drawDashedRect(ctx, x, y, width, height, palette)
    } else if (visual.adjustmentRect) {
      const x1 = Math.min(visual.adjustmentRect.x1, visual.adjustmentRect.x2)
      const y1 = Math.min(visual.adjustmentRect.y1, visual.adjustmentRect.y2)
      const x2 = Math.max(visual.adjustmentRect.x1, visual.adjustmentRect.x2)
      const y2 = Math.max(visual.adjustmentRect.y1, visual.adjustmentRect.y2)
      const a = toCanvas({ x: x1, y: y1 })
      const b = toCanvas({ x: x2, y: y2 })
      drawDashedRect(ctx, a.x, a.y, b.x - a.x, b.y - a.y, palette)
      if (!visual.useTouchupTool) {
        for (const point of [a, { x: b.x, y: a.y }, b, { x: a.x, y: b.y }]) {
          drawCircle(ctx, point.x, point.y, 7 * imageScale, '#a9a9a9', '#343434', 1.5)
        }
      }
    }
  }

  if (visual.useTouchupTool && Array.isArray(visual.touchupStrokes)) {
    const radius = (visual.brushSize || 1) * imageScale / 2
    for (const point of visual.touchupStrokes) {
      const p = toCanvas(point)
      drawCircle(ctx, p.x, p.y, radius, 'rgba(255,0,0,0.35)', 'rgba(255,0,0,0.8)', 1)
    }
  }

  if (visual.useTouchupTool && visual.touchupCursor) {
    const p = toCanvas(visual.touchupCursor)
    const radius = (visual.brushSize || 1) * imageScale / 2

    // Photoshop-style brush cursor: a high-contrast double ring stays legible
    // over both dark and light image content while the diameter updates live.
    drawCircle(ctx, p.x, p.y, radius, null, 'rgba(0,0,0,0.78)', 3)
    drawCircle(ctx, p.x, p.y, radius, null, 'rgba(255,255,255,0.95)', 1)

    if (visual.touchupCursor.resizing) {
      const label = `${Math.round(visual.brushSize || 1)} px`
      ctx.save()
      ctx.font = '12px sans-serif'
      ctx.textBaseline = 'middle'
      const padX = 6
      const height = 22
      const width = Math.ceil(ctx.measureText(label).width) + padX * 2
      const gap = Math.max(12, radius + 8)
      const stageRight = layout.stageX + layout.stageWidth
      const stageBottom = layout.stageY + layout.stageHeight
      const rightX = p.x + gap
      const leftX = p.x - gap - width
      const labelX = rightX + width <= stageRight ? rightX : Math.max(layout.stageX, leftX)
      const labelY = clamp(p.y - height / 2, layout.stageY, Math.max(layout.stageY, stageBottom - height))
      ctx.fillStyle = 'rgba(0,0,0,0.78)'
      ctx.fillRect(labelX, labelY, width, height)
      ctx.fillStyle = 'rgba(255,255,255,0.96)'
      ctx.fillText(label, labelX + padX, labelY + height / 2)
      ctx.restore()
    }
  }

  if (visual.useStraightEdgeTool && visual.dragging && visual.dragStart && visual.dragCurrent) {
    ctx.save()
    ctx.strokeStyle = '#ffff00'
    ctx.lineWidth = 2
    ctx.beginPath()
    ctx.moveTo(layout.stageX + visual.dragStart.x, layout.stageY + visual.dragStart.y)
    ctx.lineTo(layout.stageX + visual.dragCurrent.x, layout.stageY + visual.dragCurrent.y)
    ctx.stroke()
    ctx.restore()
  }

  if (
    visual.mode === 'disc' &&
    !visual.useStraightEdgeTool &&
    !visual.useTouchupTool &&
    visual.dragging && visual.dragStart && visual.dragCurrent &&
    !ctrlDragRef?.current && !shiftDragRef?.current
  ) {
    const dx = visual.dragStart.x - visual.dragCurrent.x
    const dy = visual.dragStart.y - visual.dragCurrent.y
    const radius = Math.sqrt(dx * dx + dy * dy)
    const cx = layout.stageX + visual.dragCurrent.x
    const cy = layout.stageY + visual.dragCurrent.y
    ctx.save()
    ctx.strokeStyle = '#00ff00'
    ctx.lineWidth = 2
    drawCircle(ctx, cx, cy, radius, null, '#00ff00', 2)
    if (visual.discCenterCutout && visual.discCutoutPercent > 0) {
      ctx.setLineDash([4, 3])
      drawCircle(ctx, cx, cy, radius * visual.discCutoutPercent / 100, null, '#00ff00', 2)
    }
    ctx.restore()
  }

  if (visual.mode === 'corner') {
    const referenceSize = 1600
    const minDim = Math.min(dims.w, dims.h)
    const dotImageRadius = Math.max(2, Math.round((visual.dotRadius || 5) * (minDim > 0 ? minDim / referenceSize : 1)))
    const selectedImageRadius = Math.max(dotImageRadius * 1.5, dotImageRadius + 4)
    for (const point of visual.detectedCornerPts || []) {
      const p = toCanvas({ x: point.X, y: point.Y })
      drawCircle(ctx, p.x, p.y, dotImageRadius * imageScale, 'rgba(255,0,0,0.6)', 'red', 1)
    }
    for (const point of visual.selectedCornerPts || []) {
      const p = toCanvas({ x: point.X, y: point.Y })
      drawCircle(ctx, p.x, p.y, selectedImageRadius * imageScale, 'rgba(0,255,0,0.6)', 'lime', 2)
    }
  }

  if (visual.mode === 'normal') {
    if (visual.dragging && visual.dragStart && visual.dragCurrent && !visual.useTouchupTool && !visual.normalRect) {
      const x = layout.stageX + Math.min(visual.dragStart.x, visual.dragCurrent.x)
      const y = layout.stageY + Math.min(visual.dragStart.y, visual.dragCurrent.y)
      const width = Math.abs(visual.dragCurrent.x - visual.dragStart.x)
      const height = Math.abs(visual.dragCurrent.y - visual.dragStart.y)
      drawDashedRect(ctx, x, y, width, height)
    } else if (visual.normalRect) {
      const x1 = Math.min(visual.normalRect.x1, visual.normalRect.x2)
      const y1 = Math.min(visual.normalRect.y1, visual.normalRect.y2)
      const x2 = Math.max(visual.normalRect.x1, visual.normalRect.x2)
      const y2 = Math.max(visual.normalRect.y1, visual.normalRect.y2)
      const a = toCanvas({ x: x1, y: y1 })
      const b = toCanvas({ x: x2, y: y2 })
      drawDashedRect(ctx, a.x, a.y, b.x - a.x, b.y - a.y)
      for (const point of [a, { x: b.x, y: a.y }, b, { x: a.x, y: b.y }]) {
        drawCircle(ctx, point.x, point.y, 8 * imageScale, '#00ff00', '#0b1a0b', 1.5)
      }
    }
  }

  if (visual.mode === 'line') {
    ctx.save()
    ctx.strokeStyle = '#00ff00'
    ctx.lineWidth = 2
    for (const line of visual.lines || []) {
      const a = toCanvas({ x: line.x1, y: line.y1 })
      const b = toCanvas({ x: line.x2, y: line.y2 })
      ctx.beginPath()
      ctx.moveTo(a.x, a.y)
      ctx.lineTo(b.x, b.y)
      ctx.stroke()
      drawCircle(ctx, a.x, a.y, 6 * imageScale, '#00ff00', '#0b1a0b', 1.5)
      drawCircle(ctx, b.x, b.y, 6 * imageScale, '#00ff00', '#0b1a0b', 1.5)
    }

    if (visual.dragging && visual.dragStart && visual.dragCurrent && visual.lineDragKind !== 'edit') {
      const startImage = lineStartImgRef?.current || displayToImage?.(visual.dragStart.x, visual.dragStart.y)
      const endImage = displayToImage?.(visual.dragCurrent.x, visual.dragCurrent.y)
      if (startImage && endImage) {
        const a = toCanvas(startImage)
        const b = toCanvas(endImage)
        ctx.beginPath()
        ctx.moveTo(a.x, a.y)
        ctx.lineTo(b.x, b.y)
        ctx.stroke()
      }
    }
    ctx.restore()
  }
}

export default function PreviewCanvas({
  source,
  imageDims,
  displayWidth,
  scrollRef,
  imgRef,
  cursor,
  onImageMouseLeave,
  onMouseDown,
  onMouseMove,
  onMouseUp,
  onContextMenu,
  scrollerStyle,
  showPlaceholder,
  onPresented,
  visual,
  discLiveActive,
  discLiveTransform,
  ctrlDragRef,
  shiftDragRef,
  displayToImage,
  lineStartImgRef,
}) {
  const canvasRef = useRef(null)
  const [viewport, setViewport] = useState({ w: 1, h: 1 })
  const [presented, setPresented] = useState({ source: null, dims: null })

  const activeRasterRef = useRef(null)
  const rasterCacheRef = useRef(new Map())
  const layoutRef = useRef(null)
  const latestPropsRef = useRef(null)
  const drawFrameRef = useRef(0)
  const requestTimerRef = useRef(0)
  const requestControllerRef = useRef(null)
  const requestGenerationRef = useRef(0)
  const requestRasterRef = useRef(null)
  const requestInFlightRef = useRef(false)
  const requestPendingRef = useRef(false)
  const lastPresentedRef = useRef(null)
  const debuggedSourceRef = useRef(null)
  const onPresentedRef = useRef(onPresented)
  onPresentedRef.current = onPresented

  const stageDims = validDims(presented.dims)
    ? presented.dims
    : (validDims(imageDims) ? imageDims : { w: 1, h: 1 })
  const stageWidth = useMemo(() => {
    if (displayWidth > 0) return displayWidth
    if (!validDims(stageDims)) return 1
    return Math.max(1, Math.min(viewport.w, viewport.h * stageDims.w / stageDims.h))
  }, [displayWidth, stageDims?.w, stageDims?.h, viewport.w, viewport.h])
  const stageHeight = validDims(stageDims) ? stageWidth * stageDims.h / stageDims.w : 1
  const contentWidth = Math.max(viewport.w, stageWidth)
  const contentHeight = Math.max(viewport.h, stageHeight)
  const stageLeft = (contentWidth - stageWidth) / 2
  const stageTop = (contentHeight - stageHeight) / 2

  const layout = {
    stageLeft,
    stageTop,
    stageWidth,
    stageHeight,
    contentWidth,
    contentHeight,
    stageX: stageLeft - (scrollRef.current?.scrollLeft || 0),
    stageY: stageTop - (scrollRef.current?.scrollTop || 0),
  }
  layoutRef.current = layout
  latestPropsRef.current = {
    source,
    imageDims,
    visual,
    discLiveActive,
    discLiveTransform,
    ctrlDragRef,
    shiftDragRef,
    displayToImage,
    lineStartImgRef,
  }

  const scheduleDraw = useCallback(() => {
    if (drawFrameRef.current) return
    drawFrameRef.current = requestAnimationFrame(() => {
      drawFrameRef.current = 0
      const canvas = canvasRef.current
      const scroller = scrollRef.current
      const currentLayout = layoutRef.current
      const props = latestPropsRef.current
      if (!canvas || !scroller || !currentLayout || !props) return

      const dpr = Math.max(1, window.devicePixelRatio || 1)
      const width = Math.max(1, scroller.clientWidth)
      const height = Math.max(1, scroller.clientHeight)
      const pixelWidth = Math.max(1, Math.round(width * dpr))
      const pixelHeight = Math.max(1, Math.round(height * dpr))
      if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
        canvas.width = pixelWidth
        canvas.height = pixelHeight
      }
      canvas.style.width = `${width}px`
      canvas.style.height = `${height}px`

      const ctx = canvas.getContext('2d')
      if (!ctx) return
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      ctx.clearRect(0, 0, width, height)
      ctx.imageSmoothingEnabled = true
      ctx.imageSmoothingQuality = 'high'

      const drawLayout = {
        ...currentLayout,
        stageX: currentLayout.stageLeft - scroller.scrollLeft,
        stageY: currentLayout.stageTop - scroller.scrollTop,
      }
      // The logical image stage always gets a muted checkerboard first. Any
      // cached/current raster paints opaquely over it, so the skeleton remains
      // visible only in source areas that have not been rendered yet. This is
      // especially useful when zooming out exposes more of a large image than
      // the currently cached viewport raster covers.
      if (shouldDrawCheckerboard(props.source, props.imageDims)) {
        drawCheckerboardSkeleton(ctx, drawLayout, { w: width, h: height })
      }

      const raster = activeRasterRef.current
      if (raster && validDims(raster.dims)) {
        const drawRaster = candidate => {
          const destination = canvasRectFromImage(candidate.rect, drawLayout, candidate.dims)
          if (props.discLiveActive) {
            // The disc preview applies an additional canvas transform. Its
            // inverse-transformed viewport is not axis-aligned, so retain the
            // full draw during the brief live gesture.
            ctx.drawImage(candidate.bitmap, destination.x, destination.y, destination.w, destination.h)
            return destination
          }

          const clipped = clippedRasterDrawRect(candidate, drawLayout, { w: width, h: height })
          if (!clipped) return destination
          ctx.drawImage(
            candidate.bitmap,
            clipped.source.x,
            clipped.source.y,
            clipped.source.w,
            clipped.source.h,
            clipped.destination.x,
            clipped.destination.y,
            clipped.destination.w,
            clipped.destination.h,
          )
          return destination
        }

        ctx.save()
        if (props.discLiveActive) {
          const transform = props.discLiveTransform || { dx: 0, dy: 0, angle: 0 }
          const cx = drawLayout.stageX + drawLayout.stageWidth / 2
          const cy = drawLayout.stageY + drawLayout.stageHeight / 2
          ctx.translate(cx + (transform.dx || 0), cy + (transform.dy || 0))
          ctx.rotate((transform.angle || 0) * Math.PI / 180)
          ctx.translate(-cx, -cy)
        }

        // Reuse neighboring cached viewport rasters while a pan request is in
        // flight. The active raster is drawn last so the most recently selected
        // density wins in overlap areas. This behaves like a tiny tile cache
        // without requiring a tile protocol on the backend.
        for (const cached of rasterCacheRef.current.values()) {
          if (cached === raster) continue
          if (cached.source !== raster.source || !sameDims(cached.dims, raster.dims)) continue
          drawRaster(cached)
        }
        const activeDestination = drawRaster(raster)
        if (import.meta.env.DEV && debuggedSourceRef.current !== raster.source) {
          debuggedSourceRef.current = raster.source
          const canvasBounds = canvas.getBoundingClientRect()
          console.info('[Atropos preview] first canvas draw', {
            source: raster.source,
            decoded: `${raster.bitmap.naturalWidth || raster.width}x${raster.bitmap.naturalHeight || raster.height}`,
            sourceRect: raster.rect,
            destination: activeDestination,
            canvasCSS: `${canvasBounds.width}x${canvasBounds.height}`,
            canvasPixels: `${canvas.width}x${canvas.height}`,
            canvasZ: getComputedStyle(canvas).zIndex,
          })
        }
        ctx.restore()
      }

      drawVisualGuides(
        ctx,
        props.visual,
        drawLayout,
        props.displayToImage,
        props.lineStartImgRef,
        props.ctrlDragRef,
        props.shiftDragRef,
      )
    })
  }, [scrollRef])

  const activateRaster = useCallback((raster) => {
    const cache = rasterCacheRef.current
    if (raster.key && cache.get(raster.key) === raster) {
      cache.delete(raster.key)
      cache.set(raster.key, raster)
    }
    activeRasterRef.current = raster
    setPresented(current => (
      current.source === raster.source && sameDims(current.dims, raster.dims)
        ? current
        : { source: raster.source, dims: raster.dims }
    ))
    scheduleDraw()

    if (lastPresentedRef.current !== raster.source) {
      lastPresentedRef.current = raster.source
      onPresentedRef.current?.(raster.source, raster.dims)
    }
  }, [scheduleDraw])

  const findCoveringRaster = useCallback((wantedSource, wantedDims, visibleRect, minScaleX, minScaleY) => {
    let best = null
    let bestArea = Infinity
    for (const raster of rasterCacheRef.current.values()) {
      if (raster.source !== wantedSource || !sameDims(raster.dims, wantedDims)) continue
      if (!sourceRectContains(raster.rect, visibleRect)) continue
      const scaleX = raster.width / raster.rect.w
      const scaleY = raster.height / raster.rect.h
      if (scaleX + 1e-6 < minScaleX || scaleY + 1e-6 < minScaleY) continue
      const area = raster.rect.w * raster.rect.h
      if (area < bestArea) {
        best = raster
        bestArea = area
      }
    }
    return best
  }, [])

  const pruneRasterCache = useCallback(() => {
    const cache = rasterCacheRef.current
    while (cache.size > CLIENT_RASTER_CACHE_SIZE) {
      const [oldestKey, oldest] = cache.entries().next().value
      cache.delete(oldestKey)
      if (oldest !== activeRasterRef.current) closeRaster(oldest)
    }
  }, [])

  const requestRaster = useCallback(async () => {
    // Wails RPC calls are not abortable from JavaScript. Keep exactly one
    // viewport encode in flight and coalesce any pan/zoom activity that occurs
    // while it runs into one follow-up request for the latest camera state.
    if (requestInFlightRef.current) {
      requestPendingRef.current = true
      return
    }
    requestInFlightRef.current = true
    requestPendingRef.current = false

    const requestedSource = source
    try {
      const scroller = scrollRef.current
      const currentLayout = layoutRef.current
      if (!scroller || !currentLayout || !requestedSource || !validDims(imageDims)) return

      // Request geometry is based on the incoming image dimensions, even while
      // the previous revision is still presented. This avoids fetching a crop
      // through the old revision's aspect ratio during an atomic source swap.
      const requestStageWidth = displayWidth > 0
        ? displayWidth
        : Math.max(1, Math.min(
            scroller.clientWidth,
            scroller.clientHeight * imageDims.w / imageDims.h,
          ))
      const requestStageHeight = requestStageWidth * imageDims.h / imageDims.w
      const requestContentWidth = Math.max(scroller.clientWidth, requestStageWidth)
      const requestContentHeight = Math.max(scroller.clientHeight, requestStageHeight)
      const requestLayout = {
        stageLeft: (requestContentWidth - requestStageWidth) / 2,
        stageTop: (requestContentHeight - requestStageHeight) / 2,
        stageWidth: requestStageWidth,
        stageHeight: requestStageHeight,
      }
      const visible = visibleImageRect(
        requestLayout,
        imageDims,
        scroller.scrollLeft,
        scroller.scrollTop,
        { w: scroller.clientWidth, h: scroller.clientHeight },
      )

      const request = buildViewportRequest({
        source: requestedSource,
        dims: imageDims,
        visibleRect: visible,
        displayWidth: requestStageWidth,
        dpr: window.devicePixelRatio || 1,
      })
      if (!request) return

      const requiredScaleX = request.width / request.rect.w
      const requiredScaleY = request.height / request.rect.h
      const cachedCover = findCoveringRaster(
        requestedSource,
        imageDims,
        visible,
        requiredScaleX,
        requiredScaleY,
      )
      if (cachedCover) {
        activateRaster(cachedCover)
        return
      }

      const exactCached = rasterCacheRef.current.get(request.key)
      if (exactCached) {
        activateRaster(exactCached)
        return
      }

      let imageSource = request.url
      let rasterRect = request.rect
      let rasterWidth = request.width
      let rasterHeight = request.height

      if (request.rpc) {
        // Transport only the viewport-sized JPEG through Wails IPC. The full
        // authoritative image never crosses the bridge; on a typical fit view
        // this is tens of kilobytes, and at zoom it remains bounded by the
        // viewport pixel budget.
        const response = await RenderPreviewViewport(request.rpc)
        if (latestPropsRef.current?.source !== requestedSource) return
        if (!response?.dataURL) throw new Error('Viewport renderer returned no image data')
        imageSource = response.dataURL
        rasterRect = {
          x: response.x,
          y: response.y,
          w: response.width,
          h: response.height,
        }
        rasterWidth = response.rasterWidth
        rasterHeight = response.rasterHeight
      }

      requestControllerRef.current?.abort()
      const loader = beginImageLoad(imageSource)
      requestControllerRef.current = loader
      const generation = ++requestGenerationRef.current
      const bitmap = await loader.promise
      if (generation !== requestGenerationRef.current || latestPropsRef.current?.source !== requestedSource) {
        bitmap.removeAttribute?.('src')
        return
      }

      const raster = {
        source: requestedSource,
        dims: { ...imageDims },
        rect: rasterRect,
        width: bitmap.naturalWidth || rasterWidth,
        height: bitmap.naturalHeight || rasterHeight,
        bitmap,
        key: request.key,
      }
      rasterCacheRef.current.set(request.key, raster)
      activateRaster(raster)
      pruneRasterCache()
    } catch (error) {
      if (error?.name !== 'AbortError') console.error('Viewport preview failed:', error)
    } finally {
      requestInFlightRef.current = false
      if (requestPendingRef.current) {
        requestPendingRef.current = false
        window.clearTimeout(requestTimerRef.current)
        requestTimerRef.current = window.setTimeout(() => requestRasterRef.current?.(), 0)
      }
    }
  }, [activateRaster, displayWidth, findCoveringRaster, imageDims, pruneRasterCache, scrollRef, source])

  // Keep scheduling stable across zoom/layout renders. The previous version
  // closed over requestRaster, whose identity changes with displayWidth; that
  // made the "new source" effect fire on every wheel step and generated an
  // immediate backend render for every intermediate zoom level. The stable
  // scheduler always invokes the latest request function after the debounce.
  requestRasterRef.current = requestRaster
  const scheduleRequest = useCallback((immediate = false) => {
    window.clearTimeout(requestTimerRef.current)
    requestTimerRef.current = window.setTimeout(
      () => requestRasterRef.current?.(),
      immediate ? 0 : REQUEST_DEBOUNCE_MS,
    )
  }, [])

  useEffect(() => {
    const scroller = scrollRef.current
    if (!scroller) return undefined

    const updateSize = () => {
      setViewport({
        w: Math.max(1, scroller.clientWidth),
        h: Math.max(1, scroller.clientHeight),
      })
      scheduleDraw()
      scheduleRequest(false)
    }
    updateSize()

    const observer = new ResizeObserver(updateSize)
    observer.observe(scroller)
    const onScroll = () => {
      scheduleDraw()
      scheduleRequest(false)
    }
    scroller.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      observer.disconnect()
      scroller.removeEventListener('scroll', onScroll)
    }
  }, [scheduleDraw, scheduleRequest, scrollRef])

  useEffect(() => {
    scheduleDraw()
    if (source) scheduleRequest(false)
  }, [
    source,
    stageWidth,
    stageHeight,
    viewport.w,
    viewport.h,
    scheduleDraw,
    scheduleRequest,
  ])

  useEffect(() => {
    // A new immutable revision invalidates any in-flight request. Keep drawing
    // the previous raster until the replacement is decoded, then swap in one
    // frame and notify App so frozen metadata can advance atomically. Clearing
    // the source is the one case where the previous bitmap must disappear
    // immediately. Zoom/layout changes are deliberately NOT an immediate
    // request trigger; they are handled by the settled-layout effect below.
    requestControllerRef.current?.abort()
    if (!source) {
      activeRasterRef.current = null
      lastPresentedRef.current = null
      debuggedSourceRef.current = null
      scheduleDraw()
      return
    }
    scheduleRequest(true)
  }, [source, imageDims?.w, imageDims?.h, scheduleDraw, scheduleRequest])

  useEffect(() => {
    scheduleDraw()
  }, [
    visual,
    discLiveActive,
    discLiveTransform,
    scheduleDraw,
  ])

  useEffect(() => () => {
    requestControllerRef.current?.abort()
    requestControllerRef.current = null

    window.clearTimeout(requestTimerRef.current)
    requestTimerRef.current = 0

    // IMPORTANT: React.StrictMode intentionally runs effect setup -> cleanup ->
    // setup once in development. scheduleDraw() uses a non-zero RAF id as its
    // "frame already scheduled" guard. If cleanup cancels that frame but leaves
    // the id behind, every later scheduleDraw() returns forever and the preview
    // is permanently blank even though raster loading/presentation succeeds.
    if (drawFrameRef.current) {
      cancelAnimationFrame(drawFrameRef.current)
      drawFrameRef.current = 0
    }

    requestPendingRef.current = false

    for (const raster of rasterCacheRef.current.values()) closeRaster(raster)
    rasterCacheRef.current.clear()
    activeRasterRef.current = null
  }, [])

  const logicalRef = useCallback(node => {
    imgRef.current = node
    if (!node || !validDims(stageDims)) return
    // Generic HTMLElements permit expando properties. Existing disc gesture
    // code reads naturalWidth/Height; expose the logical image dimensions so
    // that code can remain independent of the raster resolution actually used.
    node.naturalWidth = stageDims.w
    node.naturalHeight = stageDims.h
    node.dataset.naturalWidth = String(stageDims.w)
    node.dataset.naturalHeight = String(stageDims.h)
  }, [imgRef, stageDims?.w, stageDims?.h])

  return (
    <div className="preview-host">
      <div
        ref={scrollRef}
        className="canvas-area"
        onMouseDown={onMouseDown}
        onMouseMove={onMouseMove}
        onMouseUp={onMouseUp}
        onContextMenu={onContextMenu}
        style={scrollerStyle}
      >
        {source ? (
          <div
            className="preview-scroll-surface"
            style={{ width: `${contentWidth}px`, height: `${contentHeight}px` }}
          >
            <div
              className="preview-logical-stage"
              onMouseLeave={onImageMouseLeave}
              style={{
                left: `${stageLeft}px`,
                top: `${stageTop}px`,
                width: `${stageWidth}px`,
                height: `${stageHeight}px`,
              }}
            >
              <div
                ref={logicalRef}
                className="preview-hit-surface"
                style={{ cursor }}
                role="img"
                aria-label="preview"
              />
              <ImageOverlays
                mode={visual?.mode}
                normalRect={visual?.normalRect}
                adjustmentSelectionActive={visual?.adjustmentSelectionActive}
                adjustmentRect={visual?.adjustmentRect}
                useTouchupTool={visual?.useTouchupTool}
                lines={visual?.lines}
                realImageDims={visual?.realImageDims || stageDims}
              />
            </div>
          </div>
        ) : showPlaceholder ? (
          <div className="placeholder">Load or drop an image to begin</div>
        ) : null}
      </div>
      <canvas ref={canvasRef} className="preview-viewport-canvas" aria-hidden="true" />
    </div>
  )
}
