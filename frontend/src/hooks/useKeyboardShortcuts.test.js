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

async function pressCopy() {
  const event = new KeyboardEvent('keydown', {
    key: 'c',
    code: 'KeyC',
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
})
