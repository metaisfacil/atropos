// @vitest-environment jsdom
import React from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AutoContrast, Descreen, DustRemoval } from '../../wailsjs/go/main/App'
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
  adjustmentSelectionActive: false,
  setAdjustmentSelectionActive: vi.fn(),
  adjustmentRect: null,
  setAdjustmentRect: vi.fn(),
  clearTouchup: vi.fn(),
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

    await waitFor(() => expect(DustRemoval).toHaveBeenCalledWith({ level: 'high', dpi: 301, selection: null }))
    expect(props.setPreview).toHaveBeenCalledWith('/preview/dust.jpg')
    expect(props.setUnsavedChanges).toHaveBeenCalledWith(true)
    expect(props.setUseDescreenTool).toHaveBeenCalledWith(false)
  })
})

describe('AdjustmentsPanel descreen', () => {
  it('passes the luminance-only fast mode choice to the backend', async () => {
    Descreen.mockResolvedValue({ preview: '/preview/descreen.jpg' })
    const props = { ...baseProps, useDescreenTool: true, setPreview: vi.fn() }
    render(React.createElement(AdjustmentsPanel, props))

    const fastMode = screen.getByRole('checkbox', { name: 'Fast mode (luminance only)' })
    expect(fastMode.checked).toBe(false)
    fireEvent.click(fastMode)
    fireEvent.click(screen.getByRole('button', { name: 'Apply descreen' }))

    await waitFor(() => expect(Descreen).toHaveBeenCalledWith({
      thresh: 92,
      radius: 6,
      middle: 4,
      highlight: 0,
      fast: true,
      selection: null,
    }))
    expect(props.setPreview).toHaveBeenCalledWith('/preview/descreen.jpg')
  })
})

describe('AdjustmentsPanel selection', () => {
  it('shows the selection icon only while expanded and toggles the tool', () => {
    const props = {
      ...baseProps,
      adjustmentSelectionActive: false,
      setAdjustmentSelectionActive: vi.fn(),
      setAdjustmentRect: vi.fn(),
      setUseTouchupTool: vi.fn(),
      setUseStraightEdgeTool: vi.fn(),
    }
    const { rerender } = render(React.createElement(AdjustmentsPanel, { ...props, adjPanelOpen: false }))
    expect(screen.queryByRole('button', { name: 'Adjustment selection' })).toBeNull()

    rerender(React.createElement(AdjustmentsPanel, { ...props, adjPanelOpen: true }))
    fireEvent.click(screen.getByRole('button', { name: 'Adjustment selection' }))
    expect(props.setAdjustmentSelectionActive).toHaveBeenCalledWith(true)
    expect(props.setUseTouchupTool).toHaveBeenCalledWith(false)
    expect(props.setUseStraightEdgeTool).toHaveBeenCalledWith(false)
  })

  it('passes the persistent image-space selection to auto contrast', async () => {
    AutoContrast.mockResolvedValue({ preview: '/preview/contrast.jpg', black: 10, white: 240 })
    const props = {
      ...baseProps,
      adjustmentSelectionActive: true,
      adjustmentRect: { x1: 80, y1: 60, x2: 20, y2: 10 },
      setAdjustmentRect: vi.fn(),
    }
    render(React.createElement(AdjustmentsPanel, props))
    fireEvent.click(screen.getByRole('button', { name: 'Auto-contrast' }))

    await waitFor(() => expect(AutoContrast).toHaveBeenCalledWith({
      selection: { x1: 20, y1: 10, x2: 80, y2: 60 },
    }))
    expect(props.setAdjustmentRect).not.toHaveBeenCalled()
  })

  it('exits rectangular selection when Straight edge is activated', () => {
    const props = {
      ...baseProps,
      mode: 'disc',
      discActive: true,
      adjustmentSelectionActive: true,
      adjustmentRect: { x1: 10, y1: 20, x2: 80, y2: 90 },
      setAdjustmentSelectionActive: vi.fn(),
      setAdjustmentRect: vi.fn(),
      setUseStraightEdgeTool: vi.fn(),
    }
    render(React.createElement(AdjustmentsPanel, props))

    fireEvent.click(screen.getByRole('button', { name: 'Straight edge' }))

    expect(props.setAdjustmentSelectionActive).toHaveBeenCalledWith(false)
    expect(props.setAdjustmentRect).toHaveBeenCalledWith(null)
    expect(props.setUseStraightEdgeTool).toHaveBeenCalledWith(true)
  })
})
