// @vitest-environment jsdom
import React from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import OptionsModal from './OptionsModal'

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
