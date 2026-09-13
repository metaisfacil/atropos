// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useTouchup } from './useTouchup'

const runtimeMocks = vi.hoisted(() => ({
  EventsOn: vi.fn(),
  EventsOff: vi.fn(),
}))

vi.mock('../../wailsjs/runtime/runtime', () => runtimeMocks)

beforeEach(() => {
  runtimeMocks.EventsOn.mockReset()
  runtimeMocks.EventsOff.mockReset()
})

describe('touch-up preview presentation', () => {
  it('publishes the completed immutable preview revision through the normal renderer path', async () => {
    let doneHandler
    runtimeMocks.EventsOn.mockImplementation((_name, handler) => { doneHandler = handler })

    const setLoading = vi.fn()
    const setPreview = vi.fn()
    const setUseDescreenTool = vi.fn()
    const setUnsavedChanges = vi.fn()
    const showStatus = vi.fn()
    const flushPendingSave = vi.fn()

    const { result } = renderHook(() => useTouchup({
      imageLoaded: true,
      loading: true,
      setLoading,
      showStatus,
      touchupBackend: 'patchmatch',
      setErrorMessage: vi.fn(),
      preview: '/__atropos/preview/session/41.jpg',
      setPreview,
      onDragEnd: vi.fn(),
      flushPendingSaveRef: { current: flushPendingSave },
      touchupRemainsActive: true,
      setUseTouchupTool: vi.fn(),
      setUseDescreenTool,
      setUnsavedChanges,
      touchupDraggingRef: { current: false },
    }))

    const preview = '/__atropos/preview/session/42.jpg'
    await act(async () => {
      await doneHandler({
        preview,
        width: 100,
        height: 80,
        patch: { dataURL: 'data:image/png;base64,cGF0Y2g=', x: 10, y: 20, width: 8, height: 6 },
        message: 'Touch-up applied.',
        descreenReset: true,
      })
    })

    expect(setPreview).toHaveBeenCalledOnce()
    expect(setPreview).toHaveBeenCalledWith(preview)
    expect(result.current.touchupPatch).toEqual({
      dataURL: 'data:image/png;base64,cGF0Y2g=',
      x: 10,
      y: 20,
      width: 8,
      height: 6,
      baseSource: '/__atropos/preview/session/41.jpg',
      source: preview,
      imageWidth: 100,
      imageHeight: 80,
    })
    expect(setLoading).toHaveBeenCalledWith(false)
    expect(setUseDescreenTool).toHaveBeenCalledWith(false)
    expect(setUnsavedChanges).toHaveBeenCalledWith(true)
    expect(showStatus).toHaveBeenCalledWith('Touch-up applied.')
    expect(flushPendingSave).toHaveBeenCalledOnce()
  })

  it('does not mark an operation successful when completion has no preview', async () => {
    let doneHandler
    runtimeMocks.EventsOn.mockImplementation((_name, handler) => { doneHandler = handler })

    const setLoading = vi.fn()
    const setPreview = vi.fn()
    const setUnsavedChanges = vi.fn()

    renderHook(() => useTouchup({
      imageLoaded: true,
      loading: true,
      setLoading,
      showStatus: vi.fn(),
      touchupBackend: 'patchmatch',
      setErrorMessage: vi.fn(),
      setPreview,
      onDragEnd: vi.fn(),
      flushPendingSaveRef: { current: vi.fn() },
      touchupRemainsActive: true,
      setUseTouchupTool: vi.fn(),
      setUseDescreenTool: vi.fn(),
      setUnsavedChanges,
      touchupDraggingRef: { current: false },
    }))

    await act(async () => {
      await doneHandler({ message: 'Touch-up applied.' })
    })

    expect(setPreview).not.toHaveBeenCalled()
    expect(setUnsavedChanges).not.toHaveBeenCalled()
    expect(setLoading).toHaveBeenCalledWith(false)
  })
})
