// @vitest-environment jsdom
import React from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import OptionsModal from './OptionsModal'

afterEach(cleanup)

function renderOptions(overrides = {}) {
  const props = {
    open: true,
    onClose: vi.fn(),
    touchupBackend: 'patchmatch',
    setTouchupBackend: vi.fn(),
    iopaintURL: '',
    setIopaintURL: vi.fn(),
    warpFillMode: 'clamp',
    setWarpFillMode: vi.fn(),
    warpFillColor: '#ffffff',
    setWarpFillColor: vi.fn(),
    cropEdgePixels: 3,
    setCropEdgePixels: vi.fn(),
    discCenterCutout: true,
    setDiscCenterCutout: vi.fn(),
    autoCornerParams: true,
    setAutoCornerParams: vi.fn(),
    closeAfterSave: false,
    setCloseAfterSave: vi.fn(),
    postSaveEnabled: false,
    setPostSaveEnabled: vi.fn(),
    postSaveCommand: '',
    setPostSaveCommand: vi.fn(),
    touchupRemainsActive: true,
    setTouchupRemainsActive: vi.fn(),
    straightEdgeRemainsActive: true,
    setStraightEdgeRemainsActive: vi.fn(),
    autoDetectOnModeSwitch: true,
    setAutoDetectOnModeSwitch: vi.fn(),
    onOpenCornerCalibration: vi.fn(),
    ...overrides,
  }
  render(<OptionsModal {...props} />)
  return props
}

describe('OptionsModal Debug tab', () => {
  it('moves Corner Calibration out of the Tools surface and launches it from Debug', async () => {
    const props = renderOptions()

    fireEvent.click(await screen.findByRole('tab', { name: 'Debug' }))
    const launch = await screen.findByRole('button', { name: 'Open Corner Calibration' })
    fireEvent.click(launch)

    expect(props.onClose).toHaveBeenCalledTimes(1)
    expect(props.onOpenCornerCalibration).toHaveBeenCalledTimes(1)
  })
})

describe('OptionsModal corner parameters', () => {
  it('offers automatic load adjustment as an opt-in setting', async () => {
    const props = renderOptions({ autoCornerParams: false })

    fireEvent.click(await screen.findByRole('tab', { name: 'Modes' }))
    const checkbox = await screen.findByRole('checkbox', { name: /Auto-adjust parameters on load/ })
    expect(checkbox.checked).toBe(false)
    expect(screen.getByText('(default: off)')).toBeTruthy()

    fireEvent.click(checkbox)
    expect(props.setAutoCornerParams).toHaveBeenCalledWith(true)
  })
})

describe('OptionsModal crop-edge keys', () => {
  it('edits the persistent pixels-per-keypress value', async () => {
    const props = renderOptions()

    fireEvent.click(await screen.findByRole('tab', { name: 'Fill & crop' }))
    const input = await screen.findByRole('spinbutton', { name: 'Pixels per keypress' })
    expect(input.value).toBe('3')

    fireEvent.change(input, { target: { value: '12' } })
    expect(props.setCropEdgePixels).toHaveBeenCalledWith(12)
  })
})
