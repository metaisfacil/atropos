import { describe, it, expect } from 'vitest'
import { displayToImage, computeDiscShift } from './imageCoords'

describe('image coordinate helpers', () => {
  it('displayToImage uses explicit client size, not bounding rect', () => {
    const realImageDims = { w: 1600, h: 1200 }
    const point = displayToImage(100, 150, realImageDims, 800, 600)
    expect(point).toEqual({ x: 200, y: 300 })
  })

  it('displayToImage clamps invalid size', () => {
    const point = displayToImage(100, 150, { w: 1, h: 1 }, 0, 0)
    expect(point).toEqual({ x: 0, y: 0 })
  })

  it('computeDiscShift returns null on invalid input', () => {
    expect(computeDiscShift(10, 10, { w: 0, h: 0 }, 0, 0, { x: 0, y: 0 }, null, 100, 100)).toBeNull()
  })

  it('computeDiscShift responds properly to rotation and translation', () => {
    const result = computeDiscShift(20, 0,
      { w: 1000, h: 1000 },
      100,
      90,
      { x: 500, y: 500 },
      { x: 500, y: 500 },
      500,
      500,
      { w: 1000, h: 1000 },
    )
    expect(result).not.toBeNull()
    expect(result.dx).toBeGreaterThanOrEqual(-100)
    expect(result.dx).toBeLessThanOrEqual(100)
    expect(result.liveDx).toBeDefined()
    expect(result.liveDy).toBeDefined()
  })

  it('computeDiscShift keeps translation relative to current rotation', () => {
    const result = computeDiscShift(10, 0,
      { w: 1000, h: 1000 },
      100,
      90,
      { x: 500, y: 500 },
      { x: 500, y: 500 },
      500,
      500,
    )

    expect(result).not.toBeNull()
    // With 90deg rotation and scale 2, screen-movement right 10px becomes image-space dy=20.
    expect(result.dx).toBe(0)
    expect(result.dy).toBe(20)
    // live preview is inverse transform (image->display), so 20 image px becomes 10 screen px.
    expect(result.liveDx).toBe(10)
    expect(result.liveDy).toBeCloseTo(0)
  })

  it('computeDiscShift live offset corresponds to backend shift scale', () => {
    const result = computeDiscShift(2, 0,
      { w: 1000, h: 1000 },
      100,
      0,
      { x: 500, y: 500 },
      { x: 500, y: 500 },
      500,
      500,
      { w: 1000, h: 1000 },
    )

    expect(result).not.toBeNull()
    // screen drag 2px at scale 2 maps to image shift -4 and preview 2.
    expect(result.dx).toBe(-4)
    expect(result.dy).toBe(0)
    expect(result.liveDx).toBe(2)
    expect(result.liveDy).toBeCloseTo(0)
  })

  it('computeDiscShift uses natural display source dims for live scale in disc mode', () => {
    // Real image is 2800x2764, preview natural is 784x776 (disc crop), so scale should be based on 784x776.
    const result = computeDiscShift(1, 1,
      { w: 2800, h: 2764 },
      100,
      0,
      { x: 500, y: 500 },
      { x: 500, y: 500 },
      784,
      776,
      { w: 784, h: 776 },
    )

    expect(result).not.toBeNull()
    // If using real dims here, scale would incorrectly be far larger.
    expect(result.liveDx).toBeGreaterThanOrEqual(0)
    expect(result.liveDy).toBeGreaterThanOrEqual(0)
  })

  it('computeDiscShift works for rotation + down-drag (regression sample)', () => {
    const result = computeDiscShift(0, 2,
      { w: 2800, h: 2764 },
      377,
      -37.5,
      { x: 959, y: 1156 },
      { x: 959, y: 1156 },
      784,
      784,
    )

    expect(result).not.toBeNull()
    expect(result.dx).toBe(4)
    expect(result.dy).toBe(-6)
    expect(result.liveDx).toBe(0)
    expect(result.liveDy).toBe(2)
    expect(result).toHaveProperty('match', true)
  })

  it('computeDiscShift works for rotation + diagonal-down-drag (regression sample #2)', () => {
    const result = computeDiscShift(-3, 53,
      { w: 2800, h: 2764 },
      675,
      -30.3,
      { x: 977, y: 1273 },
      { x: 977, y: 1273 },
      784,
      784,
    )

    expect(result).not.toBeNull()
    expect(result.liveDx).toBe(-3)
    expect(result.liveDy).toBe(53)
    expect(result).toHaveProperty('match', true)
  })
})
