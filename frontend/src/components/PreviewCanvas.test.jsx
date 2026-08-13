import { describe, expect, it } from 'vitest'
import { buildViewportRequest, clippedRasterDrawRect, sourceRectContains } from './PreviewCanvas'

describe('buildViewportRequest', () => {
  it('requests an overscanned, quantized source region at viewport density', () => {
    const request = buildViewportRequest({
      source: '/__atropos/preview/session/7.jpg',
      dims: { w: 6000, h: 4000 },
      visibleRect: { x: 2000, y: 1200, w: 1000, h: 800 },
      displayWidth: 1200,
      dpr: 2,
    })

    expect(request).not.toBeNull()
    expect(request.url).toContain('/__atropos/preview/session/7.jpg?')
    expect(request.url).toContain('dw=')
    expect(request.url).toContain('dh=')
    expect(sourceRectContains(request.rect, { x: 2000, y: 1200, w: 1000, h: 800 })).toBe(true)
    expect(request.rect.x % 64).toBe(0)
    expect(request.rect.y % 64).toBe(0)
    expect(request.width).toBeLessThanOrEqual(4096)
    expect(request.height).toBeLessThanOrEqual(4096)
    expect(request.width * request.height).toBeLessThanOrEqual(16 * 1024 * 1024)
  })

  it('never asks the backend to upsample beyond source pixels', () => {
    const request = buildViewportRequest({
      source: '/__atropos/preview/session/9.jpg',
      dims: { w: 800, h: 600 },
      visibleRect: { x: 0, y: 0, w: 800, h: 600 },
      displayWidth: 3200,
      dpr: 2,
    })

    expect(request.width).toBeLessThanOrEqual(request.rect.w)
    expect(request.height).toBeLessThanOrEqual(request.rect.h)
  })

  it('leaves non-preview sources on the legacy full-image path', () => {
    const source = 'data:image/jpeg;base64,abc'
    const request = buildViewportRequest({
      source,
      dims: { w: 20, h: 10 },
      visibleRect: { x: 0, y: 0, w: 20, h: 10 },
      displayWidth: 20,
      dpr: 1,
    })

    expect(request.url).toBe(source)
    expect(request.rect).toEqual({ x: 0, y: 0, w: 20, h: 10 })
  })
})

describe('sourceRectContains', () => {
  it('recognizes a contained visible region', () => {
    expect(sourceRectContains(
      { x: 64, y: 64, w: 512, h: 512 },
      { x: 100, y: 120, w: 200, h: 180 },
    )).toBe(true)
  })
})

describe('clippedRasterDrawRect', () => {
  it('keeps a high-zoom full-frame fallback draw bounded to the viewport', () => {
    const draw = clippedRasterDrawRect(
      {
        dims: { w: 5100, h: 7020 },
        rect: { x: 0, y: 0, w: 5100, h: 7020 },
        width: 1082,
        height: 1489,
        bitmap: { naturalWidth: 1082, naturalHeight: 1489 },
      },
      {
        stageX: -2100,
        stageY: -3000,
        stageWidth: 4324.1,
        stageHeight: 5952,
      },
      { w: 880, h: 744 },
    )

    expect(draw).not.toBeNull()
    expect(draw.destination).toEqual({ x: 0, y: 0, w: 880, h: 744 })
    expect(draw.source.x).toBeGreaterThan(0)
    expect(draw.source.y).toBeGreaterThan(0)
    expect(draw.source.w).toBeLessThan(1082)
    expect(draw.source.h).toBeLessThan(1489)
  })

  it('returns null for a cached raster outside the viewport', () => {
    expect(clippedRasterDrawRect(
      {
        dims: { w: 1000, h: 1000 },
        rect: { x: 0, y: 0, w: 100, h: 100 },
        bitmap: { naturalWidth: 100, naturalHeight: 100 },
      },
      { stageX: 1000, stageY: 1000, stageWidth: 1000, stageHeight: 1000 },
      { w: 500, h: 500 },
    )).toBeNull()
  })
})
