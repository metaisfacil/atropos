// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { touchupPreviewPatch, useTouchup } from './useTouchup'

const runtimeMocks = vi.hoisted(() => ({
  EventsOn: vi.fn(),
  EventsOff: vi.fn(),
}))

vi.mock('../../wailsjs/runtime/runtime', () => runtimeMocks)

beforeEach(() => {
  runtimeMocks.EventsOn.mockReset()
  runtimeMocks.EventsOff.mockReset()
})

describe('touchupPreviewPatch', () => {
  it('maps a backend patch into full-image overlay coordinates', () => {
    expect(touchupPreviewPatch({
      width: 5100,
      height: 7020,
      patch: {
        source: 'data:image/png;base64,abc',
        x: 1200,
        y: 900,
        width: 48,
        height: 52,
      },
    })).toEqual({
      source: 'data:image/png;base64,abc',
      x: 1200,
      y: 900,
      width: 48,
      height: 52,
      imageWidth: 5100,
      imageHeight: 7020,
    })
  })

  it('rejects incomplete or empty patch payloads', () => {
    expect(touchupPreviewPatch(null)).toBeNull()
    expect(touchupPreviewPatch({ width: 100, height: 100, patch: { source: 'x', x: 0, y: 0, width: 0, height: 10 } })).toBeNull()
    expect(touchupPreviewPatch({ width: 0, height: 100, patch: { source: 'x', x: 0, y: 0, width: 10, height: 10 } })).toBeNull()
  })
})

describe('touch-up patch presentation', () => {
  it('keeps loading active until the patch overlay reports onLoad', async () => {
    let doneHandler
    runtimeMocks.EventsOn.mockImplementation((_name, handler) => { doneHandler = handler })
    const setLoading = vi.fn()
    let markPresented
    const presentTouchupPatch = vi.fn(() => new Promise(resolve => { markPresented = resolve }))

    renderHook(() => useTouchup({
      imageLoaded: true,
      loading: true,
      setLoading,
      showStatus: vi.fn(),
      touchupBackend: 'patchmatch',
      setErrorMessage: vi.fn(),
      setPreview: vi.fn(),
      onDragEnd: vi.fn(),
      flushPendingSaveRef: { current: vi.fn() },
      touchupRemainsActive: true,
      setUseTouchupTool: vi.fn(),
      setUseDescreenTool: vi.fn(),
      setUnsavedChanges: vi.fn(),
      touchupDraggingRef: { current: false },
      presentTouchupPatch,
    }))

    let completion
    act(() => {
      completion = doneHandler({
        width: 100,
        height: 80,
        patch: { source: 'data:image/png;base64,abc', x: 10, y: 12, width: 8, height: 9 },
      })
    })
    expect(presentTouchupPatch).toHaveBeenCalledOnce()
    expect(setLoading).not.toHaveBeenCalledWith(false)

    await act(async () => {
      markPresented()
      await completion
    })
    expect(setLoading).toHaveBeenCalledWith(false)
  })
})
