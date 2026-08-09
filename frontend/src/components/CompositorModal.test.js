// @vitest-environment jsdom
import React from 'react'
import { act, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CompositorModal from './CompositorModal'

vi.mock('../../wailsjs/go/main/App', () => ({
  CompositorOpenFilesDialog: vi.fn(),
  CompositorStitch: vi.fn(),
  CompositorLoadResult: vi.fn(),
}))

describe('CompositorModal', () => {
  afterEach(() => {
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
})
