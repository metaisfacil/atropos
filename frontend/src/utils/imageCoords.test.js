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
    // With 90deg rotation, screen-movement right should become positive dy in image space.
    expect(result.dx).toBe(0)
    expect(result.dy).toBe(20)
    // live preview is inverse of rotation transform and scaled back to display.
    expect(result.liveDx).toBe(10)
    expect(result.liveDy).toBeCloseTo(0)
  })
})
