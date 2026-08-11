import { describe, expect, it } from 'vitest'
import { buildViewportRequest, sourceRectContains } from './PreviewCanvas'

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
