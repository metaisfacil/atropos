// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useKeyboardShortcuts } from './useKeyboardShortcuts'

const appMocks = vi.hoisted(() => ({
  Crop: vi.fn(),
  Rotate: vi.fn(),
  ShiftDisc: vi.fn(),
  RotateDisc: vi.fn(),
  SetFeatherSize: vi.fn(),
  GetPixelColor: vi.fn(),
  ConfirmClose: vi.fn(),
  CopySelectionToClipboard: vi.fn(),
  UndoLastCorner: vi.fn(),
}))
vi.mock('../../wailsjs/go/main/App', () => appMocks)
vi.mock('../../wailsjs/runtime/runtime', () => ({ Quit: vi.fn() }))

function makeProps(overrides = {}) {
  return {
    imageLoaded: true,
    mode: 'normal',
    discActive: false,
    featherSize: 10,
    discRotation: 0,
    ctrlDragRef: { current: null },
    shiftDragRef: { current: null },
    mousePosRef: { current: { x: 0, y: 0 } },
    setPreview: vi.fn(),
    setFeatherSize: vi.fn(),
    setLoading: vi.fn(),
    setRealImageDims: vi.fn(),
    preview: '/__atropos/preview/session/1.jpg',
    realImageDims: { w: 100, h: 100 },
    optimisticCrop: null,
    setOptimisticCrop: vi.fn(),
    setDiscNoMaskPreview: vi.fn(),
    setDiscCenter: vi.fn(),
    setDiscRadius: vi.fn(),
    setDiscBgColor: vi.fn(),
    setDiscRotation: vi.fn(),
    displayToImage: vi.fn(),
    showStatus: vi.fn(),
    showError: vi.fn(),
    handleSaveImage: vi.fn(),
    flushPendingSave: vi.fn(),
    handleLoadImage: vi.fn(),
    handlePasteImage: vi.fn(),
    canSave: false,
    normalRect: null,
    handleNormalCrop: vi.fn(),
    handleUndo: vi.fn(),
    unsavedChanges: false,
    setUnsavedChanges: vi.fn(),
    confirmClose: vi.fn(),
    cornerState: { cornerCount: 0 },
    setCornerState: vi.fn(),
    setSelectedCornerPts: vi.fn(),
    adjustmentSelectionActive: false,
    adjustmentRect: null,
    setAdjustmentRect: vi.fn(),
    ...overrides,
  }
}

async function pressCopy(code = 'KeyC') {
  const event = new KeyboardEvent('keydown', {
    key: 'c',
    code,
    ctrlKey: true,
    bubbles: true,
    cancelable: true,
  })
  act(() => window.dispatchEvent(event))
  await waitFor(() => expect(appMocks.CopySelectionToClipboard).toHaveBeenCalled())
  return event
}

beforeEach(() => {
  vi.clearAllMocks()
  appMocks.CopySelectionToClipboard.mockResolvedValue('Copied 60×50 selection to clipboard')
})

afterEach(cleanup)

describe('Ctrl+C selection copy', () => {
  it('copies the active adjustment rectangle and normalizes its coordinates', async () => {
    const props = makeProps({
      normalRect: { x1: 1, y1: 2, x2: 30, y2: 40 },
      adjustmentRect: { x1: 80, y1: 60, x2: 20, y2: 10 },
    })
    renderHook(() => useKeyboardShortcuts(props))

    const event = await pressCopy()

    expect(event.defaultPrevented).toBe(true)
    expect(appMocks.CopySelectionToClipboard).toHaveBeenCalledWith({ x1: 20, y1: 10, x2: 80, y2: 60 })
    expect(props.showStatus).toHaveBeenLastCalledWith('Copied 60×50 selection to clipboard')
  })

  it('copies an uncommitted Normal crop when there is no adjustment selection', async () => {
    const props = makeProps({
      normalRect: { x1: 90, y1: 70, x2: 15, y2: 5 },
    })
    renderHook(() => useKeyboardShortcuts(props))

    await pressCopy()

    expect(appMocks.CopySelectionToClipboard).toHaveBeenCalledWith({ x1: 15, y1: 5, x2: 90, y2: 70 })
  })

  it('uses the layout-aware key instead of the physical QWERTY key code', async () => {
    const props = makeProps({
      adjustmentRect: { x1: 10, y1: 20, x2: 30, y2: 40 },
    })
    renderHook(() => useKeyboardShortcuts(props))

    await pressCopy('KeyJ')

    expect(appMocks.CopySelectionToClipboard).toHaveBeenCalledWith({ x1: 10, y1: 20, x2: 30, y2: 40 })
  })
})

describe('Ctrl+V clipboard load', () => {
  it('invokes the backend paste path even when no document is loaded', async () => {
    const props = makeProps({ imageLoaded: false })
    renderHook(() => useKeyboardShortcuts(props))
    const event = new KeyboardEvent('keydown', {
      key: 'v', code: 'KeyV', ctrlKey: true, bubbles: true, cancelable: true,
    })

    act(() => window.dispatchEvent(event))
    await waitFor(() => expect(props.handlePasteImage).toHaveBeenCalledOnce())

    expect(event.defaultPrevented).toBe(true)
  })

  it('leaves native paste intact for editable controls', async () => {
    const props = makeProps()
    renderHook(() => useKeyboardShortcuts(props))
    const input = document.createElement('input')
    document.body.appendChild(input)
    input.focus()
    const event = new KeyboardEvent('keydown', {
      key: 'v', code: 'KeyV', ctrlKey: true, bubbles: true, cancelable: true,
    })

    act(() => window.dispatchEvent(event))

    expect(event.defaultPrevented).toBe(false)
    expect(props.handlePasteImage).not.toHaveBeenCalled()
    input.remove()
  })

  it('recognizes the typed letter when its physical key code differs', async () => {
    const props = makeProps({ imageLoaded: false })
    renderHook(() => useKeyboardShortcuts(props))
    const event = new KeyboardEvent('keydown', {
      key: 'v', code: 'Period', ctrlKey: true, bubbles: true, cancelable: true,
    })

    act(() => window.dispatchEvent(event))
    await waitFor(() => expect(props.handlePasteImage).toHaveBeenCalledOnce())

    expect(event.defaultPrevented).toBe(true)
  })
})

describe('layout-aware command shortcuts', () => {
  it('saves using the typed S key regardless of physical position', async () => {
    const props = makeProps({ canSave: true })
    renderHook(() => useKeyboardShortcuts(props))
    const event = new KeyboardEvent('keydown', {
      key: 's', code: 'KeyO', ctrlKey: true, bubbles: true, cancelable: true,
    })

    act(() => window.dispatchEvent(event))
    await waitFor(() => expect(props.handleSaveImage).toHaveBeenCalledOnce())

    expect(event.defaultPrevented).toBe(true)
  })

  it('undoes using the typed Z key regardless of physical position', async () => {
    const props = makeProps()
    renderHook(() => useKeyboardShortcuts(props))
    const event = new KeyboardEvent('keydown', {
      key: 'z', code: 'Semicolon', ctrlKey: true, bubbles: true, cancelable: true,
    })

    act(() => window.dispatchEvent(event))
    await waitFor(() => expect(props.handleUndo).toHaveBeenCalledOnce())

    expect(event.defaultPrevented).toBe(true)
  })

  it('does not trigger a command from its old physical QWERTY position', () => {
    const props = makeProps({ canSave: true })
    renderHook(() => useKeyboardShortcuts(props))
    const event = new KeyboardEvent('keydown', {
      key: 'o', code: 'KeyS', ctrlKey: true, bubbles: true, cancelable: true,
    })

    act(() => window.dispatchEvent(event))

    expect(props.handleSaveImage).not.toHaveBeenCalled()
    expect(props.handleLoadImage).toHaveBeenCalledOnce()
  })
})

describe('layout-independent spatial shortcuts', () => {
  it.each([
    ['z', 'KeyW', 'top'],
    ['q', 'KeyA', 'left'],
    ['s', 'KeyS', 'bottom'],
    ['d', 'KeyD', 'right'],
  ])('maps AZERTY %s at %s to crop %s', async (key, code, direction) => {
    const props = makeProps({ canSave: true })
    renderHook(() => useKeyboardShortcuts(props))
    const event = new KeyboardEvent('keydown', {
      key, code, bubbles: true, cancelable: true,
    })

    act(() => window.dispatchEvent(event))
    await waitFor(() => expect(appMocks.Crop).toHaveBeenCalledWith({ direction }))
  })

  it('uses the physical Q/E positions for rotation on Dvorak', async () => {
    const props = makeProps({ canSave: true })
    renderHook(() => useKeyboardShortcuts(props))
    const event = new KeyboardEvent('keydown', {
      key: "'", code: 'KeyQ', bubbles: true, cancelable: true,
    })

    act(() => window.dispatchEvent(event))
    await waitFor(() => expect(appMocks.Rotate).toHaveBeenCalledWith({ flipCode: 2 }))
  })

  it('does not treat Ctrl plus a spatial position as a crop command', () => {
    const props = makeProps({ canSave: true })
    renderHook(() => useKeyboardShortcuts(props))
    const event = new KeyboardEvent('keydown', {
      key: 'x', code: 'KeyW', ctrlKey: true, bubbles: true, cancelable: true,
    })

    act(() => window.dispatchEvent(event))

    expect(appMocks.Crop).not.toHaveBeenCalled()
  })
})
