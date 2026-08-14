// @vitest-environment jsdom
import React from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DustRemoval } from '../../wailsjs/go/main/App'
import AdjustmentsPanel from './AdjustmentsPanel'

vi.mock('../../wailsjs/go/main/App', () => ({
  AutoContrast: vi.fn(),
  SetLevels: vi.fn(),
  TrimBorders: vi.fn(),
  ResizeImage: vi.fn(),
  Descreen: vi.fn(),
  DustRemoval: vi.fn(),
}))

const baseProps = {
  adjPanelOpen: true,
  setAdjPanelOpen: vi.fn(),
  autoContrastPending: false,
  setAutoContrastPending: vi.fn(),
  blackPoint: 0,
  setBlackPoint: vi.fn(),
  whitePoint: 255,
  setWhitePoint: vi.fn(),
  imageLoaded: true,
  setLoading: vi.fn(),
  setPreview: vi.fn(),
  useStretchPreprocess: true,
  setUseStretchPreprocess: vi.fn(),
  postCropAvailable: true,
  useTouchupTool: false,
  setUseTouchupTool: vi.fn(),
  useDescreenTool: false,
  setUseDescreenTool: vi.fn(),
  brushSize: 20,
  setBrushSize: vi.fn(),
  mode: 'normal',
  discActive: false,
  useStraightEdgeTool: false,
  setUseStraightEdgeTool: vi.fn(),
  realImageDims: { w: 1200, h: 800 },
  setRealImageDims: vi.fn(),
  loading: false,
  imageMeta: { dpiX: 300, dpiY: 302 },
  showStatus: vi.fn(),
  setErrorMessage: vi.fn(),
  setUnsavedChanges: vi.fn(),
}

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

describe('AdjustmentsPanel dust removal', () => {
  it('keeps Dust removal disabled until after crop', () => {
    const { rerender } = render(React.createElement(AdjustmentsPanel, { ...baseProps, postCropAvailable: false }))
    expect(screen.getByRole('button', { name: 'Dust removal' }).disabled).toBe(true)

    rerender(React.createElement(AdjustmentsPanel, baseProps))
    expect(screen.getByRole('button', { name: 'Dust removal' }).disabled).toBe(false)
  })

  it('reveals controls and applies the selected level with scan DPI', async () => {
    DustRemoval.mockResolvedValue({
      preview: '/preview/dust.jpg',
      message: 'Dust removal applied',
      changed: true,
      descreenReset: true,
    })
    const props = { ...baseProps, setPreview: vi.fn(), setUnsavedChanges: vi.fn(), setUseDescreenTool: vi.fn() }
    render(React.createElement(AdjustmentsPanel, props))

    const toggle = screen.getByRole('button', { name: 'Dust removal' })
    fireEvent.click(toggle)
    expect(toggle.getAttribute('aria-pressed')).toBe('true')
    fireEvent.change(screen.getByRole('combobox', { name: 'Dust removal strength' }), { target: { value: 'high' } })
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => expect(DustRemoval).toHaveBeenCalledWith({ level: 'high', dpi: 301 }))
    expect(props.setPreview).toHaveBeenCalledWith('/preview/dust.jpg')
    expect(props.setUnsavedChanges).toHaveBeenCalledWith(true)
    expect(props.setUseDescreenTool).toHaveBeenCalledWith(false)
  })
})
