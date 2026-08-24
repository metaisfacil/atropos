// @vitest-environment jsdom
import React, { useState } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const appMocks = vi.hoisted(() => ({
  SetFeatherRadius: vi.fn(),
  SetFeatherSize: vi.fn(),
  SetDiscSettings: vi.fn(),
}))
vi.mock('../../wailsjs/go/main/App', () => appMocks)

import DiscPanel from './DiscPanel'

function Harness({ initialRadius = 50 }) {
  const [discRadius, setDiscRadius] = useState(initialRadius)
  return (
    <DiscPanel
      discActive
      discRadius={discRadius}
      setDiscRadius={setDiscRadius}
      featherSize={15}
      setFeatherSize={vi.fn()}
      discCenterCutout={false}
      discCutoutPercent={11}
      setDiscCutoutPercent={vi.fn()}
      setPreview={vi.fn()}
      setRealImageDims={vi.fn()}
      setDiscNoMaskPreview={vi.fn()}
      disabled={false}
    />
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  appMocks.SetFeatherRadius.mockResolvedValue({
    preview: '/preview/resized',
    width: 1400,
    height: 1400,
    discRadius: 700,
  })
})

afterEach(cleanup)

describe('DiscPanel disc radius', () => {
  it('places the initially drawn radius at the slider midpoint', () => {
    render(<Harness initialRadius={500} />)
    const slider = screen.getByLabelText('Disc Radius')

    expect(Number(slider.min)).toBe(200)
    expect(Number(slider.max)).toBe(800)
    expect(Number(slider.value)).toBe(500)
    expect(Number(slider.value)).toBe((Number(slider.min) + Number(slider.max)) / 2)
  })

  it('caps the lower end at a valid one-pixel radius', () => {
    render(<Harness initialRadius={50} />)
    const slider = screen.getByLabelText('Disc Radius')

    expect(Number(slider.min)).toBe(1)
    expect(Number(slider.max)).toBe(350)
  })

  it('commits a larger radius after moving right', async () => {
    render(<Harness initialRadius={500} />)
    const slider = screen.getByLabelText('Disc Radius')

    fireEvent.change(slider, { target: { value: '700' } })
    expect(Number(slider.value)).toBe(700)
    fireEvent.pointerUp(slider)

    await waitFor(() => {
      expect(appMocks.SetFeatherRadius).toHaveBeenCalledWith({ radius: 700 })
    })
  })
})
