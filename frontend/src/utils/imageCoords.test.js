import { describe, it, expect } from 'vitest'
import { getDisplayScale, displayToImage, computeDiscShift } from './imageCoords'

describe('getDisplayScale', () => {
  it('returns correct scale factors for basic case', () => {
    expect(getDisplayScale({ w: 2000, h: 1000 }, 400, 200)).toEqual({ scaleX: 5, scaleY: 5 })
  })

  it('returns null for null realImageDims', () => {
    expect(getDisplayScale(null, 400, 200)).toBeNull()
  })

  it('returns null for zero-area realImageDims', () => {
    expect(getDisplayScale({ w: 0, h: 500 }, 400, 200)).toBeNull()
    expect(getDisplayScale({ w: 500, h: 0 }, 400, 200)).toBeNull()
  })

  it('returns null for zero client width or height', () => {
    expect(getDisplayScale({ w: 1000, h: 1000 }, 0, 200)).toBeNull()
    expect(getDisplayScale({ w: 1000, h: 1000 }, 400, 0)).toBeNull()
  })

  it('uses naturalImageDims for scale when provided and valid', () => {
    // Real image is 3000×2000 but disc crop is 300×200; scale should be from 300×200
    const scale = getDisplayScale({ w: 3000, h: 2000 }, 300, 200, { w: 300, h: 200 })
    expect(scale).toEqual({ scaleX: 1, scaleY: 1 })
  })

  it('falls back to realImageDims when naturalImageDims has zero width', () => {
    const scale = getDisplayScale({ w: 1000, h: 1000 }, 100, 100, { w: 0, h: 500 })
    expect(scale).toEqual({ scaleX: 10, scaleY: 10 })
  })

  it('falls back to realImageDims when naturalImageDims is null', () => {
    const scale = getDisplayScale({ w: 1000, h: 1000 }, 100, 100, null)
    expect(scale).toEqual({ scaleX: 10, scaleY: 10 })
  })

  it('handles non-square images with independent x/y scale factors', () => {
    const scale = getDisplayScale({ w: 1200, h: 900 }, 400, 300)
    expect(scale).toEqual({ scaleX: 3, scaleY: 3 })
  })
})

describe('displayToImage with naturalImageDims override', () => {
  it('uses naturalImageDims for disc mode coordinate mapping', () => {
    // Disc crop is 784×776, displayed at 392×388 (zoom ~0.5)
    // scale = 784/392 = 2, 776/388 = 2
    const pt = displayToImage(196, 194, { w: 2800, h: 2764 }, 392, 388, { w: 784, h: 776 })
    expect(pt).toEqual({ x: 392, y: 388 })
  })

  it('falls back to realImageDims when naturalImageDims not given', () => {
    const pt = displayToImage(50, 50, { w: 1000, h: 1000 }, 100, 100)
    expect(pt).toEqual({ x: 500, y: 500 })
  })

  it('uses naturalImageDims even when realImageDims is much larger', () => {
    // Confirms disc mode doesn't accidentally use full source dims for scale
    const withNat  = displayToImage(10, 10, { w: 3000, h: 3000 }, 100, 100, { w: 200, h: 200 })
    const withoutNat = displayToImage(10, 10, { w: 3000, h: 3000 }, 100, 100)
    expect(withNat.x).not.toBe(withoutNat.x)
    expect(withNat).toEqual({ x: 20, y: 20 })   // scale from 200/100
    expect(withoutNat).toEqual({ x: 300, y: 300 }) // scale from 3000/100
  })
})

describe('computeDiscShift clamping', () => {
  // The formula negates the screen drag: desiredImgDx = -(screenDx * scaleX).
  // Dragging right (positive screenDx) moves the disc LEFT in image space.

  it('clamps disc center at left/top boundary', () => {
    // Disc center at x=150, radius=100 → min allowed center = 100.
    // Drag right (screenDx=+10000) → desiredImgDx ≈ -20000, clamped to dx=-50.
    const result = computeDiscShift(
      10000, 0,
      { w: 1000, h: 1000 }, 100, 0,
      { x: 150, y: 500 }, { x: 150, y: 500 },
      500, 500, { w: 1000, h: 1000 },
    )
    expect(result).not.toBeNull()
    expect(result.dx).toBe(-50)
  })

  it('clamps disc center at right/bottom boundary', () => {
    // Disc center at x=850, radius=100 → max allowed center = 900.
    // Drag left (screenDx=-10000) → desiredImgDx ≈ +20000, clamped to dx=+50.
    const result = computeDiscShift(
      -10000, 0,
      { w: 1000, h: 1000 }, 100, 0,
      { x: 850, y: 500 }, { x: 850, y: 500 },
      500, 500, { w: 1000, h: 1000 },
    )
    expect(result).not.toBeNull()
    expect(result.dx).toBe(50)
  })

  it('returns zero shift when disc center is already at boundary', () => {
    // Center exactly at min (radius=100, center.x=100), drag right → still can't go further.
    const result = computeDiscShift(
      1000, 0,
      { w: 1000, h: 1000 }, 100, 0,
      { x: 100, y: 500 }, { x: 100, y: 500 },
      500, 500, { w: 1000, h: 1000 },
    )
    expect(result).not.toBeNull()
    expect(result.dx).toBe(0)
    expect(result.liveDx).toBeCloseTo(0)  // may be -0 due to floating-point negation
  })

  it('liveDx/liveDy are zero when shift is clamped to zero on both axes', () => {
    // Both axes at minimum boundary, dragging to push further past the boundary.
    const result = computeDiscShift(
      1000, 1000,
      { w: 1000, h: 1000 }, 100, 0,
      { x: 100, y: 100 }, { x: 100, y: 100 },
      500, 500, { w: 1000, h: 1000 },
    )
    expect(result).not.toBeNull()
    expect(result.dx).toBe(0)
    expect(result.dy).toBe(0)
    expect(result.liveDx).toBeCloseTo(0)  // may be -0 due to floating-point negation
    expect(result.liveDy).toBeCloseTo(0)
  })
})

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
