// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useImageActions } from './useImageActions'

const appMocks = vi.hoisted(() => ({
  LoadImage: vi.fn(),
  DetectCorners: vi.fn(),
  ResetCorners: vi.fn(),
  SkipCrop: vi.fn(),
  NormalCrop: vi.fn(),
  ResetNormal: vi.fn(),
  OpenImageDialog: vi.fn(),
  OpenSaveDialog: vi.fn(),
  GetLaunchArgs: vi.fn(),
  ConfirmClose: vi.fn(),
  GetCleanPreview: vi.fn(),
  RestoreCornerOverlay: vi.fn(),
  RecropImage: vi.fn(),
  CancelCornerDetect: vi.fn(),
  CancelTouchup: vi.fn(),
  LoadImageBytes: vi.fn(),
  ResetDisc: vi.fn(),
  ClearLines: vi.fn(),
  SaveImage: vi.fn(),
  RunPostSaveCommand: vi.fn(),
  Undo: vi.fn(),
}))

const runtimeMocks = vi.hoisted(() => ({
  OnFileDrop: vi.fn(),
  OnFileDropOff: vi.fn(),
  EventsOn: vi.fn(),
  EventsOff: vi.fn(),
  Quit: vi.fn(),
}))

vi.mock('../../wailsjs/go/main/App', () => appMocks)
vi.mock('../../wailsjs/runtime/runtime', () => runtimeMocks)

function makeProps() {
  const setters = [
    'setMode', 'setPreview', 'setLoading', 'setImageLoaded', 'setRealImageDims',
    'setInputImageDims', 'setImgNatural', 'setZoom', 'setFitWidth', 'setCornerState',
    'setLinesDone', 'setLinesProcessed', 'setDiscActive', 'setDiscNoMaskPreview',
    'setDiscCenter', 'setDiscRadius', 'setDiscRotation', 'setDiscBgColor',
    'setNormalRect', 'setNormalCropApplied', 'setCropSkipped', 'setCornersDetected',
    'setDetectedCornerPts', 'setSelectedCornerPts', 'setLines', 'setBlackPoint',
    'setWhitePoint', 'setUseTouchupTool', 'setUseStraightEdgeTool', 'setDragging',
    'setDragStart', 'setDragCurrent', 'setConfirmDialog', 'setTouchupStrokes',
    'setAdjustmentSelectionActive', 'setAdjustmentRect', 'setCloseAfterSave',
    'setPostSaveEnabled', 'setPostSaveCommand', 'setImageMeta', 'setUnsavedChanges',
  ]
  const props = Object.fromEntries(setters.map(name => [name, vi.fn()]))
  return {
    ...props,
    mode: 'normal',
    loading: false,
    imageLoaded: true,
    discActive: false,
    linesProcessed: false,
    normalCropApplied: true,
    cornerState: { cornerCount: 0, maxCorners: 50, minDistance: 10, qualityLevel: 0.01, accent: 0 },
    dotRadius: 5,
    useStretchPreprocess: false,
    autoCornerParams: false,
    normalRect: null,
    closeAfterSave: false,
    postSaveEnabled: false,
    postSaveCommand: '',
    autoDetectOnModeSwitch: false,
    adjustmentSelectionActive: false,
    touchupDraggingRef: { current: false },
    canvasRef: { current: null },
    compositorDropRef: { current: null },
    showStatus: vi.fn(),
    showError: vi.fn(),
    unsavedChanges: false,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  appMocks.GetLaunchArgs.mockResolvedValue({})
  appMocks.RecropImage.mockResolvedValue({
    preview: '/__atropos/preview/session-2/1.jpg',
    width: 2400,
    height: 1600,
  })
})

describe('Re-crop viewport reset', () => {
  it('returns to fit view without clearing the presented fit geometry', async () => {
    const props = makeProps()
    const { result } = renderHook(() => useImageActions(props))

    await waitFor(() => expect(appMocks.GetLaunchArgs).toHaveBeenCalled())
    act(() => result.current.handleRecrop())
    const dialog = props.setConfirmDialog.mock.calls.at(-1)[0]

    await act(async () => dialog.onConfirm())

    expect(props.setZoom).toHaveBeenCalledWith(1)
    expect(props.setZoom.mock.invocationCallOrder[0]).toBeLessThan(appMocks.RecropImage.mock.invocationCallOrder[0])
    expect(props.setFitWidth).not.toHaveBeenCalled()
    expect(props.setPreview).toHaveBeenCalledWith('/__atropos/preview/session-2/1.jpg')
  })
})

describe('Manual corner parameters', () => {
  it('does not reapply load-time suggestions when Detect is pressed', async () => {
    const props = makeProps()
    props.mode = 'corner'
    props.autoCornerParams = true
    appMocks.DetectCorners.mockResolvedValue({
      preview: '/__atropos/preview/session-3/1.jpg',
      width: 2400,
      height: 1600,
      corners: [],
      message: 'Detected 0 corners',
    })

    const { result, rerender } = renderHook(currentProps => useImageActions(currentProps), {
      initialProps: props,
    })
    await waitFor(() => expect(appMocks.GetLaunchArgs).toHaveBeenCalled())

    await act(async () => result.current.handleCompositorLoad({
      preview: '/__atropos/preview/session-3/0.jpg',
      width: 2400,
      height: 1600,
      suggestedCornerParams: { maxCorners: 500, minDistance: 80 },
    }))

    const manuallyAdjusted = {
      ...props,
      cornerState: { ...props.cornerState, maxCorners: 275, minDistance: 37 },
    }
    rerender(manuallyAdjusted)
    await act(async () => result.current.handleDetectCorners())

    expect(appMocks.DetectCorners).toHaveBeenLastCalledWith(expect.objectContaining({
      maxCorners: 275,
      minDistance: 37,
    }))
  })
})
