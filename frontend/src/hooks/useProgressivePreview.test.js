// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import {
  FULL_RESOLUTION_PROMOTION_DELAY_MS,
  isPreviewPresentationPending,
  isPreviewVariant,
  lowResolutionPreviewURL,
  previewAssetSession,
  usePresentedValue,
  useProgressivePreview,
} from './useProgressivePreview'

describe('lowResolutionPreviewURL', () => {
  it('derives the low-resolution variant from an internal preview URL', () => {
    expect(lowResolutionPreviewURL('/__atropos/preview/session-token/42.jpg'))
      .toBe('/__atropos/preview/session-token/42-low.jpg')
  })

  it('does not rewrite data, remote, or unrelated image sources', () => {
    expect(lowResolutionPreviewURL('data:image/jpeg;base64,abc')).toBeNull()
    expect(lowResolutionPreviewURL('https://example.test/image.jpg')).toBeNull()
    expect(lowResolutionPreviewURL('/assets/image.jpg')).toBeNull()
    expect(lowResolutionPreviewURL(null)).toBeNull()
  })

  it('extracts only valid internal preview sessions', () => {
    expect(previewAssetSession('/__atropos/preview/session-token/42.jpg')).toBe('session-token')
    expect(previewAssetSession('/__atropos/preview/42.jpg')).toBeNull()
    expect(previewAssetSession('/assets/session/42.jpg')).toBeNull()
  })
})

describe('preview presentation state', () => {
  const full = '/__atropos/preview/session/8.jpg'

  it('recognises both low and full resources as the same visible revision', () => {
    expect(isPreviewVariant(full, full)).toBe(true)
    expect(isPreviewVariant('/__atropos/preview/session/8-low.jpg', full)).toBe(true)
    expect(isPreviewVariant('/__atropos/preview/session/7.jpg', full)).toBe(false)
  })

  it('remains pending until that exact revision has been presented', () => {
    expect(isPreviewPresentationPending(full, null)).toBe(true)
    expect(isPreviewPresentationPending(full, '/__atropos/preview/session/7.jpg')).toBe(true)
    expect(isPreviewPresentationPending(full, full)).toBe(false)
    expect(isPreviewPresentationPending(null, full)).toBe(false)
  })

  it('freezes related visual state until preview presentation completes', () => {
    const { result, rerender } = renderHook(
      ({ value, pending }) => usePresentedValue(value, pending),
      { initialProps: { value: 'three corners', pending: false } },
    )

    expect(result.current).toBe('three corners')

    rerender({ value: 'warp complete', pending: true })
    expect(result.current).toBe('three corners')

    rerender({ value: 'warp complete', pending: false })
    expect(result.current).toBe('warp complete')
  })
})

describe('useProgressivePreview', () => {
  it('retains the old image, promotes the low variant, then the detailed variant', () => {
    vi.useFakeTimers()
    const NativeImage = global.Image
    const loaders = []
    global.Image = class MockImage {
      set src(value) {
        this.source = value
        loaders.push(this)
      }
    }

    try {
      const { result, rerender } = renderHook(
        ({ source }) => useProgressivePreview(source),
        { initialProps: { source: '/__atropos/preview/session/1.jpg' } },
      )
      expect(result.current).toBeNull()
      expect(loaders[0].source).toBe('/__atropos/preview/session/1-low.jpg')

      act(() => loaders[0].onload())
      expect(result.current).toBe('/__atropos/preview/session/1-low.jpg')
      expect(loaders).toHaveLength(1)

      act(() => vi.advanceTimersByTime(FULL_RESOLUTION_PROMOTION_DELAY_MS))
      expect(loaders[1].source).toBe('/__atropos/preview/session/1.jpg')

      act(() => loaders[1].onload())
      expect(result.current).toBe('/__atropos/preview/session/1.jpg')

      rerender({ source: '/__atropos/preview/session/2.jpg' })
      expect(result.current).toBe('/__atropos/preview/session/1.jpg')
      act(() => loaders[2].onload())
      expect(result.current).toBe('/__atropos/preview/session/2-low.jpg')
    } finally {
      global.Image = NativeImage
      vi.useRealTimers()
    }
  })

  it('does not promote superseded rapid revisions to full resolution', () => {
    vi.useFakeTimers()
    const NativeImage = global.Image
    const loaders = []
    global.Image = class MockImage {
      set src(value) {
        this.source = value
        loaders.push(this)
      }
      removeAttribute() {
        this.aborted = true
      }
    }

    try {
      const { result, rerender } = renderHook(
        ({ source }) => useProgressivePreview(source),
        { initialProps: { source: '/__atropos/preview/session/1.jpg' } },
      )
      act(() => loaders[0].onload())
      expect(result.current).toBe('/__atropos/preview/session/1-low.jpg')

      act(() => vi.advanceTimersByTime(FULL_RESOLUTION_PROMOTION_DELAY_MS - 1))
      rerender({ source: '/__atropos/preview/session/2.jpg' })
      expect(loaders).toHaveLength(2)
      expect(loaders[1].source).toBe('/__atropos/preview/session/2-low.jpg')

      act(() => loaders[1].onload())
      act(() => vi.advanceTimersByTime(FULL_RESOLUTION_PROMOTION_DELAY_MS))
      expect(loaders).toHaveLength(3)
      expect(loaders[2].source).toBe('/__atropos/preview/session/2.jpg')
    } finally {
      global.Image = NativeImage
      vi.useRealTimers()
    }
  })

  it('restarts the promotion idle window after an image interaction', () => {
    vi.useFakeTimers()
    const NativeImage = global.Image
    const loaders = []
    global.Image = class MockImage {
      set src(value) {
        this.source = value
        loaders.push(this)
      }
      removeAttribute() {
        this.aborted = true
      }
    }

    try {
      const { rerender } = renderHook(
        ({ deferred }) => useProgressivePreview('/__atropos/preview/session/1.jpg', deferred),
        { initialProps: { deferred: false } },
      )
      act(() => loaders[0].onload())
      act(() => vi.advanceTimersByTime(FULL_RESOLUTION_PROMOTION_DELAY_MS - 1))

      rerender({ deferred: true })
      act(() => vi.advanceTimersByTime(FULL_RESOLUTION_PROMOTION_DELAY_MS * 2))
      expect(loaders).toHaveLength(1)

      rerender({ deferred: false })
      act(() => vi.advanceTimersByTime(FULL_RESOLUTION_PROMOTION_DELAY_MS))
      expect(loaders).toHaveLength(2)
      expect(loaders[1].source).toBe('/__atropos/preview/session/1.jpg')
    } finally {
      global.Image = NativeImage
      vi.useRealTimers()
    }
  })

  it('never exposes an asset URL that failed preloading', () => {
    const NativeImage = global.Image
    const loaders = []
    global.Image = class MockImage {
      set src(value) {
        this.source = value
        loaders.push(this)
      }
    }

    try {
      const { result } = renderHook(() => useProgressivePreview('/__atropos/preview/session/9.jpg'))
      expect(result.current).toBeNull()

      act(() => loaders[0].onerror())
      expect(result.current).toBeNull()
      expect(loaders[1].source).toBe('/__atropos/preview/session/9.jpg')

      act(() => loaders[1].onerror())
      expect(result.current).toBeNull()
    } finally {
      global.Image = NativeImage
    }
  })

  it('immediately hides a prior image when the backend session changes', () => {
    vi.useFakeTimers()
    const NativeImage = global.Image
    const loaders = []
    global.Image = class MockImage {
      set src(value) {
        this.source = value
        loaders.push(this)
      }
    }

    try {
      const { result, rerender } = renderHook(
        ({ source }) => useProgressivePreview(source),
        { initialProps: { source: '/__atropos/preview/old-session/1.jpg' } },
      )
      act(() => loaders[0].onload())
      act(() => vi.advanceTimersByTime(FULL_RESOLUTION_PROMOTION_DELAY_MS))
      act(() => loaders[1].onload())
      expect(result.current).toBe('/__atropos/preview/old-session/1.jpg')

      rerender({ source: '/__atropos/preview/new-session/2.jpg' })
      expect(result.current).toBeNull()
    } finally {
      global.Image = NativeImage
      vi.useRealTimers()
    }
  })
})
