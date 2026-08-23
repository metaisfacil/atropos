import { describe, expect, it } from 'vitest'
import { fitWidthFor } from './previewLayout'

function makeScroller({ outerWidth, outerHeight, clientWidth, clientHeight }) {
  return {
    clientWidth,
    clientHeight,
    getBoundingClientRect: () => ({ width: outerWidth, height: outerHeight }),
  }
}

describe('fitWidthFor', () => {
  it('does not change when scrollbars reduce the visible client area', () => {
    const withoutScrollbars = makeScroller({
      outerWidth: 1000,
      outerHeight: 800,
      clientWidth: 1000,
      clientHeight: 800,
    })
    const withScrollbars = makeScroller({
      outerWidth: 1000,
      outerHeight: 800,
      clientWidth: 983,
      clientHeight: 783,
    })

    expect(fitWidthFor(withoutScrollbars, { w: 1000, h: 1000 })).toBe(800)
    expect(fitWidthFor(withScrollbars, { w: 1000, h: 1000 })).toBe(800)
    expect(fitWidthFor(withoutScrollbars, { w: 1600, h: 1200 })).toBe(1000)
    expect(fitWidthFor(withScrollbars, { w: 1600, h: 1200 })).toBe(1000)
  })

  it('falls back to client dimensions when no rendered border box is available', () => {
    expect(fitWidthFor({
      clientWidth: 600,
      clientHeight: 400,
      getBoundingClientRect: () => ({ width: 0, height: 0 }),
    }, { w: 1200, h: 600 })).toBe(600)
  })
})
