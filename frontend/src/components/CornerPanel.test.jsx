// @vitest-environment jsdom
import React from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import CornerPanel from './CornerPanel'

describe('CornerPanel persistent parameters', () => {
  it('routes manual Max Corners and Min Distance changes through persistence callbacks', () => {
    const onMaxCornersChange = vi.fn()
    const onMinDistanceChange = vi.fn()
    render(
      <CornerPanel
        state={{ maxCorners: 500, qualityLevel: 1, minDistance: 100, accent: 20, cornerCount: 0 }}
        setState={vi.fn()}
        onMaxCornersChange={onMaxCornersChange}
        onMinDistanceChange={onMinDistanceChange}
        dotRadius={5}
        setDotRadius={vi.fn()}
        customCorner={false}
        setCustomCorner={vi.fn()}
        disabled={false}
      />,
    )

    const sliders = screen.getAllByRole('slider')
    fireEvent.change(sliders[0], { target: { value: '275' } })
    fireEvent.change(sliders[2], { target: { value: '37' } })

    expect(onMaxCornersChange).toHaveBeenCalledWith(275)
    expect(onMinDistanceChange).toHaveBeenCalledWith(37)
  })
})
