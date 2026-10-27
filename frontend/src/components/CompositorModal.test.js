// @vitest-environment jsdom
import React from 'react'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CompositorModal from './CompositorModal'
import { CompositorAddMask, CompositorAlignOverlay, CompositorRefreshPreview, CompositorStitch } from '../../wailsjs/go/main/App'

vi.mock('../../wailsjs/go/main/App', () => ({
  CompositorOpenFilesDialog: vi.fn(),
  CompositorStitch: vi.fn(),
  CompositorAlignOverlay: vi.fn(),
  CompositorAddMask: vi.fn(),
  CompositorUndoMask: vi.fn(),
  CompositorClearMasks: vi.fn(),
  CompositorRefreshPreview: vi.fn(),
  CompositorLoadResult: vi.fn(),
}))

describe('CompositorModal', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('can transition from closed to open without changing hook order', async () => {
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation(callback => {
      callback()
      return 1
    })
    const onClose = vi.fn()
    const onLoad = vi.fn()
    const dropRef = { current: null }
    const props = { open: false, onClose, onLoad, dropRef }
    const { rerender } = render(React.createElement(CompositorModal, props))

    await act(async () => {
      rerender(React.createElement(CompositorModal, { ...props, open: true }))
    })

    expect(screen.getByRole('dialog', { name: 'Image Compositor' })).toBeTruthy()
  })

  it('keeps stitching as default and routes repeated scans to overlay alignment', async () => {
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation(callback => { callback(); return 1 })
    CompositorAlignOverlay.mockResolvedValue({ preview: 'preview-1', width: 320, height: 240, message: 'Aligned' })
    const dropRef = { current: null }
    render(React.createElement(CompositorModal, { open: true, onClose: vi.fn(), onLoad: vi.fn(), dropRef }))
    expect(screen.getByRole('button', { name: 'Stitch images' })).toBeTruthy()
    await act(async () => { dropRef.current(['a.tif', 'b.tif']) })
    fireEvent.change(screen.getByLabelText('Mode'), { target: { value: 'overlay' } })
    expect(screen.getByText(/Put the complete scan first/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Align scans' }))
    await waitFor(() => expect(CompositorAlignOverlay).toHaveBeenCalledWith({ imagePaths: ['a.tif', 'b.tif'] }))
    expect(screen.getByLabelText('Paint from')).toBeTruthy()
    const dialog = screen.getByRole('dialog', { name: 'Image Compositor' })
    expect(dialog.querySelector('.compositor-header .compositor-mask-tools')).toBeTruthy()
    expect(dialog.querySelector('.compositor-body .compositor-mask-tools')).toBeNull()
    expect(CompositorStitch).not.toHaveBeenCalled()
  })

  it('sends the selected feather value with a new paint stroke', async () => {
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation(callback => { callback(); return 1 })
    CompositorAlignOverlay.mockResolvedValue({ preview: 'preview-1', width: 320, height: 240, message: 'Aligned' })
    CompositorAddMask.mockResolvedValue({ preview: 'preview-2', width: 320, height: 240, message: 'Painted' })
    const dropRef = { current: null }
    render(React.createElement(CompositorModal, { open: true, onClose: vi.fn(), onLoad: vi.fn(), dropRef }))
    await act(async () => { dropRef.current(['a.tif', 'b.tif']) })
    fireEvent.change(screen.getByLabelText('Mode'), { target: { value: 'overlay' } })
    fireEvent.click(screen.getByRole('button', { name: 'Align scans' }))
    const image = await screen.findByAltText('Aligned scan preview')
    fireEvent.load(image)
    fireEvent.change(screen.getByLabelText('Feather'), { target: { value: '0' } })
    const surface = image.parentElement
    surface.setPointerCapture = vi.fn()
    surface.getBoundingClientRect = () => ({ left: 0, top: 0, width: 320, height: 240 })
    fireEvent.pointerDown(surface, { pointerId: 1, clientX: 160, clientY: 120 })
    fireEvent.pointerUp(surface, { pointerId: 1, clientX: 160, clientY: 120 })
    await waitFor(() => expect(CompositorAddMask).toHaveBeenCalledWith({ stroke: {
      layer: 1, radius: .03, feather: 0, points: [{ x: .5, y: .5 }],
    } }))
  })

  it('zooms the overlay preview with the wheel and returns to fit', async () => {
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation(callback => { callback(); return 1 })
    CompositorAlignOverlay.mockResolvedValue({ preview: 'preview-1', width: 320, height: 240, message: 'Aligned' })
    const dropRef = { current: null }
    render(React.createElement(CompositorModal, { open: true, onClose: vi.fn(), onLoad: vi.fn(), dropRef }))
    await act(async () => { dropRef.current(['a.tif', 'b.tif']) })
    fireEvent.change(screen.getByLabelText('Mode'), { target: { value: 'overlay' } })
    fireEvent.click(screen.getByRole('button', { name: 'Align scans' }))
    const image = await screen.findByAltText('Aligned scan preview')
    const surface = image.parentElement
    const frame = surface.parentElement.parentElement
    frame.getBoundingClientRect = () => ({ left: 0, top: 0, width: 320, height: 240 })
    surface.getBoundingClientRect = () => ({ left: 0, top: 0, width: parseFloat(surface.style.width), height: parseFloat(surface.style.height) })
    fireEvent(window, new Event('resize'))
    await waitFor(() => expect(parseFloat(surface.style.width)).toBe(320))

    fireEvent.wheel(frame, { deltaY: -120, clientX: 160, clientY: 120 })
    expect(parseFloat(surface.style.width)).toBeGreaterThan(320)
    fireEvent.click(screen.getByRole('button', { name: 'Fit' }))
    expect(parseFloat(surface.style.width)).toBe(320)
  })

  it('requests a fresh preview URL when a result is reopened', async () => {
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation(callback => { callback(); return 1 })
    CompositorAlignOverlay.mockResolvedValue({ preview: 'preview-1', width: 320, height: 240, message: 'Aligned' })
    CompositorRefreshPreview.mockResolvedValue({ preview: 'preview-2', width: 320, height: 240, message: 'Aligned' })
    const dropRef = { current: null }
    const props = { open: true, onClose: vi.fn(), onLoad: vi.fn(), dropRef }
    const { rerender } = render(React.createElement(CompositorModal, props))
    await act(async () => { dropRef.current(['a.tif', 'b.tif']) })
    fireEvent.change(screen.getByLabelText('Mode'), { target: { value: 'overlay' } })
    fireEvent.click(screen.getByRole('button', { name: 'Align scans' }))
    const firstImage = await screen.findByAltText('Aligned scan preview')
    expect(firstImage.getAttribute('src')).toBe('preview-1')
    await act(async () => { rerender(React.createElement(CompositorModal, { ...props, open: false })) })
    expect(screen.queryByAltText('Aligned scan preview')).toBeNull()
    await act(async () => { rerender(React.createElement(CompositorModal, props)) })
    await waitFor(() => expect(CompositorRefreshPreview).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(screen.getByAltText('Aligned scan preview').getAttribute('src')).toBe('preview-2'))
  })
})
