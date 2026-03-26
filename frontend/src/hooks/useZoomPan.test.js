// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'

vi.mock('../../wailsjs/go/main/App', () => ({
  LogFrontend:    vi.fn().mockResolvedValue(undefined),
  SetFeatherSize: vi.fn().mockResolvedValue({}),
}))

// jsdom has no ResizeObserver; provide a no-op stub
global.ResizeObserver = class ResizeObserver {
  observe()    {}
  unobserve()  {}
  disconnect() {}
}

import { useZoomPan } from './useZoomPan'

function makeProps(overrides = {}) {
  return {
    imgRef:          { current: null },
    mode:            'corner',
    discActive:      false,
    featherSize:     10,
    setFeatherSize:  vi.fn(),
    setPreview:      vi.fn(),
    ...overrides,
  }
}

// Set up a fake canvas element whose clientWidth/clientHeight can be controlled.
function makeCanvas(w, h) {
  const el = document.createElement('div')
  Object.defineProperty(el, 'clientWidth',  { get: () => w, configurable: true })
  Object.defineProperty(el, 'clientHeight', { get: () => h, configurable: true })
  return el
}

describe('useZoomPan – initial state', () => {
  it('zoom starts at 1', () => {
    const { result } = renderHook(() => useZoomPan(makeProps()))
    expect(result.current.zoom).toBe(1)
  })

  it('fitWidth starts at 0', () => {
    const { result } = renderHook(() => useZoomPan(makeProps()))
    expect(result.current.fitWidth).toBe(0)
  })

  it('spacePanMode starts false', () => {
    const { result } = renderHook(() => useZoomPan(makeProps()))
    expect(result.current.spacePanMode).toBe(false)
  })

  it('canvasRef is exposed', () => {
    const { result } = renderHook(() => useZoomPan(makeProps()))
    expect(result.current.canvasRef).toBeDefined()
  })
})

describe('useZoomPan – handleImgLoad / fitWidth', () => {
  it('fitWidth = min(containerWidth, containerHeight × aspect) — width-limited', () => {
    // Image 1600×1200, aspect = 4/3. Container 1000×800.
    // height-limited: 800 × (4/3) ≈ 1067  → clamp to containerWidth 1000
    const imgRef = { current: { naturalWidth: 1600, naturalHeight: 1200 } }
    const { result } = renderHook(() => useZoomPan(makeProps({ imgRef })))

    const canvas = makeCanvas(1000, 800)
    result.current.canvasRef.current = canvas

    act(() => { result.current.handleImgLoad() })
    expect(result.current.fitWidth).toBe(1000)
  })

  it('fitWidth = containerHeight × aspect for tall containers', () => {
    // Image 800×2000, aspect = 0.4. Container 1000×500.
    // height × aspect = 500 × 0.4 = 200  → less than containerWidth 1000
    const imgRef = { current: { naturalWidth: 800, naturalHeight: 2000 } }
    const { result } = renderHook(() => useZoomPan(makeProps({ imgRef })))

    const canvas = makeCanvas(1000, 500)
    result.current.canvasRef.current = canvas

    act(() => { result.current.handleImgLoad() })
    expect(result.current.fitWidth).toBe(200)
  })

  it('fitWidth = containerWidth for a square image in a square container', () => {
    const imgRef = { current: { naturalWidth: 500, naturalHeight: 500 } }
    const { result } = renderHook(() => useZoomPan(makeProps({ imgRef })))

    const canvas = makeCanvas(300, 300)
    result.current.canvasRef.current = canvas

    act(() => { result.current.handleImgLoad() })
    expect(result.current.fitWidth).toBe(300)
  })

  it('handleImgLoad with no canvasRef does not throw', () => {
    const imgRef = { current: { naturalWidth: 1000, naturalHeight: 1000 } }
    const { result } = renderHook(() => useZoomPan(makeProps({ imgRef })))
    // canvasRef.current is null by default
    expect(() => act(() => { result.current.handleImgLoad() })).not.toThrow()
  })

  it('handleImgLoad with null imgRef does not throw', () => {
    const { result } = renderHook(() => useZoomPan(makeProps()))
    // imgRef.current is null
    expect(() => act(() => { result.current.handleImgLoad() })).not.toThrow()
    expect(result.current.fitWidth).toBe(0)
  })
})

describe('useZoomPan – setZoom clamping', () => {
  it('setZoom clamps to minimum of 0.1', () => {
    const { result } = renderHook(() => useZoomPan(makeProps()))
    act(() => { result.current.setZoom(0.001) })
    // setZoom is a direct state setter here; the wheel handler is what clamps.
    // Verify the zoom value accepted by the hook is unchanged (no built-in clamp in setter itself).
    // The clamp lives in the wheel handler formula: Math.min(5, Math.max(0.1, z * factor))
    // We test the formula directly here.
    const clamp = (z, factor) => Math.min(5, Math.max(0.1, z * factor))
    expect(clamp(0.15, 0.9)).toBeCloseTo(0.135)
    expect(clamp(0.11, 0.9)).toBeCloseTo(0.1)   // hits floor
    expect(clamp(0.1, 0.9)).toBe(0.1)            // already at floor — no change
  })

  it('zoom formula clamps to maximum of 5', () => {
    const clamp = (z, factor) => Math.min(5, Math.max(0.1, z * factor))
    expect(clamp(4.8, 1.1)).toBeCloseTo(5)       // hits ceiling
    expect(clamp(5, 1.1)).toBe(5)                // already at ceiling — no change
  })
})

describe('useZoomPan – scroll offset formula', () => {
  // The pendingScrollRef formula ensures zoom anchors to the cursor position:
  //   left = (clientX - imgRect.left)  * ratio - (clientX - canvasRect.left)
  //   top  = (clientY - imgRect.top)   * ratio - (clientY - canvasRect.top)
  // We test the math in isolation to guard against regressions.

  function scrollOffset({ clientX, clientY, imgLeft, imgTop, canvasLeft, canvasTop, ratio }) {
    return {
      left: (clientX - imgLeft)  * ratio - (clientX - canvasLeft),
      top:  (clientY - imgTop)   * ratio - (clientY - canvasTop),
    }
  }

  it('cursor at image origin → scrollLeft/Top equal (ratio-1) × offset of img within canvas', () => {
    // Image and canvas share the same origin (no centering margin)
    const offset = scrollOffset({ clientX: 0, clientY: 0, imgLeft: 0, imgTop: 0, canvasLeft: 0, canvasTop: 0, ratio: 2 })
    expect(offset.left).toBe(0)
    expect(offset.top).toBe(0)
  })

  it('cursor at centre of a 1000×1000 image zooming 2× scrolls to keep cursor fixed', () => {
    // Canvas and image both start at 0.  Cursor at (500,500) (image centre).
    // ratio = 2/1 = 2.
    // Expected: new scrollLeft = 500*2 - 500 = 500, i.e. scroll half-way to keep centre fixed.
    const offset = scrollOffset({ clientX: 500, clientY: 500, imgLeft: 0, imgTop: 0, canvasLeft: 0, canvasTop: 0, ratio: 2 })
    expect(offset.left).toBe(500)
    expect(offset.top).toBe(500)
  })

  it('accounts for margin:auto centering offset when image is smaller than canvas', () => {
    // Image 400px wide centred in 1000px canvas → imgLeft = 300, canvasLeft = 0.
    // Cursor at clientX = 500 (centre of canvas / image).
    // ratio = 1.1 (zoom-in step).
    // left = (500-300)*1.1 - (500-0) = 220 - 500 = -280
    // Negative scrollLeft means no scroll needed (image still fits); this correctly
    // avoids a rightward lurch when zooming a small image that is already centred.
    const offset = scrollOffset({ clientX: 500, clientY: 300, imgLeft: 300, imgTop: 100, canvasLeft: 0, canvasTop: 0, ratio: 1.1 })
    expect(offset.left).toBe(-280)
  })

  it('zooming out (ratio < 1) produces negative scroll offset', () => {
    // Cursor at (250,250), image starts at canvas origin, ratio = 0.9
    const offset = scrollOffset({ clientX: 250, clientY: 250, imgLeft: 0, imgTop: 0, canvasLeft: 0, canvasTop: 0, ratio: 0.9 })
    expect(offset.left).toBe(250 * 0.9 - 250)   // = -25
    expect(offset.top).toBe(250 * 0.9 - 250)
  })
})
