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
  LoadImageFromClipboard: vi.fn(),
  LoadImageBytes: vi.fn(),
  ResetDisc: vi.fn(),
  ClearLines: vi.fn(),
  SaveImage: vi.fn(),
  RunPostSaveCommand: vi.fn(),
  Undo: vi.fn(),
  Redo: vi.fn(),
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
    'setFeatherSize', 'syncHistoryDiscSettings', 'setUseDescreenTool', 'setWhitePoint', 'setUseTouchupTool', 'setUseStraightEdgeTool', 'setDragging',
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
    calibrationDropRef: { current: null },
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

  it('ends touch-up pointer state before starting the backend re-crop', async () => {
    let resolveRecrop
    appMocks.RecropImage.mockReturnValue(new Promise(resolve => { resolveRecrop = resolve }))
    const props = makeProps()
    props.mode = 'line'
    props.linesProcessed = true
    props.touchupDraggingRef.current = true
    const { result } = renderHook(() => useImageActions(props))

    await waitFor(() => expect(appMocks.GetLaunchArgs).toHaveBeenCalled())
    act(() => result.current.handleRecrop())
    const dialog = props.setConfirmDialog.mock.calls.at(-1)[0]

    let confirmation
    act(() => { confirmation = dialog.onConfirm() })

    expect(appMocks.RecropImage).toHaveBeenCalledOnce()
    expect(props.touchupDraggingRef.current).toBe(false)
    expect(props.setUseTouchupTool).toHaveBeenCalledWith(false)
    expect(props.setTouchupStrokes).toHaveBeenCalledWith([])
    expect(props.setDragging).toHaveBeenCalledWith(false)
    expect(props.setDragStart).toHaveBeenCalledWith(null)
    expect(props.setDragCurrent).toHaveBeenCalledWith(null)
    expect(props.setUseTouchupTool.mock.invocationCallOrder[0]).toBeLessThan(appMocks.RecropImage.mock.invocationCallOrder[0])

    resolveRecrop({
      preview: '/__atropos/preview/session-2/1.jpg',
      width: 2400,
      height: 1600,
    })
    await act(async () => confirmation)
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

describe('Backend clipboard load', () => {
  it('loads clipboard pixels without transferring frontend bytes', async () => {
    const props = makeProps()
    appMocks.LoadImageFromClipboard.mockResolvedValue({
      preview: '/__atropos/preview/session-4/0.jpg',
      width: 7200,
      height: 3600,
      format: 'BMP',
      dpiX: 0,
      dpiY: 0,
    })
    const { result } = renderHook(() => useImageActions(props))
    await waitFor(() => expect(appMocks.GetLaunchArgs).toHaveBeenCalled())

    await act(async () => result.current.handlePasteImage())

    expect(appMocks.CancelTouchup).toHaveBeenCalled()
    expect(appMocks.CancelCornerDetect).toHaveBeenCalled()
    expect(appMocks.LoadImageFromClipboard).toHaveBeenCalledOnce()
    expect(appMocks.LoadImageBytes).not.toHaveBeenCalled()
    expect(props.setPreview).toHaveBeenCalledWith('/__atropos/preview/session-4/0.jpg')
    expect(props.setRealImageDims).toHaveBeenCalledWith({ w: 7200, h: 3600 })
    expect(props.setAdjustmentRect).toHaveBeenCalledWith(null)
  })

  it('logs an empty clipboard error without displaying an alert', async () => {
    const props = makeProps()
    const error = new Error('LoadImageFromClipboard: read error: clipboard does not contain an image')
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
    appMocks.LoadImageFromClipboard.mockRejectedValue(error)
    const { result } = renderHook(() => useImageActions(props))
    await waitFor(() => expect(appMocks.GetLaunchArgs).toHaveBeenCalled())

    await act(async () => result.current.handlePasteImage())

    expect(consoleError).toHaveBeenCalledWith('Clipboard image load error:', error)
    expect(props.showError).not.toHaveBeenCalled()
    expect(props.showStatus).not.toHaveBeenCalledWith('Loading clipboard image…')
    expect(props.setZoom).not.toHaveBeenCalled()
    consoleError.mockRestore()
  })
})


describe('Undo state synchronization', () => {
  it('clears adjustment sessions and restores disc rotation after a history step', async () => {
    const props = { ...makeProps(), mode: 'disc', discActive: true }
    appMocks.Undo.mockResolvedValue({ changed: true, descreenReset: true, black: 10, white: 230, discRotation: 12 })
    const { result } = renderHook(() => useImageActions(props))
    await act(async () => result.current.handleUndo())
    expect(props.setUseDescreenTool).toHaveBeenCalledWith(false)
    expect(props.setBlackPoint).toHaveBeenCalledWith(10)
    expect(props.setWhitePoint).toHaveBeenCalledWith(230)
    expect(props.setDiscRotation).toHaveBeenCalledWith(12)
    expect(props.setAdjustmentRect).toHaveBeenCalledWith(null)
    expect(props.setUnsavedChanges).toHaveBeenCalledWith(true)
  })

  it('does not mark the document modified when history is empty', async () => {
    const props = makeProps()
    appMocks.Undo.mockResolvedValue({ message: 'Nothing to undo' })
    const { result } = renderHook(() => useImageActions(props))
    await act(async () => result.current.handleUndo())
    expect(props.setUnsavedChanges).not.toHaveBeenCalled()
    expect(props.setBlackPoint).not.toHaveBeenCalled()
  })
})


describe('Redo', () => {
  it.each(['normal', 'disc', 'line', 'corner'])('restores the committed phase in %s mode', async mode => {
    const props = { ...makeProps(), mode, normalCropApplied: false }
    appMocks.Redo.mockResolvedValue({ changed: true, descreenReset: true, preview: '/redo', width: 30, height: 20, discRadius: 12, discRotation: 15, historyDiscSettings: { centerCutout: false, cutoutPercent: 9 }, historyFeatherSize: 7 })
    const { result } = renderHook(() => useImageActions(props))
    await act(async () => result.current.handleRedo())
    expect(appMocks.Redo).toHaveBeenCalledOnce()
    expect(props.setPreview).toHaveBeenCalledWith('/redo')
    expect(props.setRealImageDims).toHaveBeenCalledWith({ w: 30, h: 20 })
    if (mode === 'normal') expect(props.setNormalCropApplied).toHaveBeenCalledWith(true)
    if (mode === 'disc') {
      expect(props.setDiscActive).toHaveBeenCalledWith(true)
      expect(props.syncHistoryDiscSettings).toHaveBeenCalledWith({ centerCutout: false, cutoutPercent: 9 })
      expect(props.setFeatherSize).toHaveBeenCalledWith(7)
    }
    if (mode === 'line') expect(props.setLinesProcessed).toHaveBeenCalledWith(true)
    if (mode === 'corner') expect(props.setCornerState.mock.calls.at(-1)[0]({}).cornerCount).toBe(4)
  })

  it('serializes undo and redo while a history request is pending', async () => {
    let finish
    appMocks.Redo.mockReturnValue(new Promise(resolve => { finish = resolve }))
    const { result } = renderHook(() => useImageActions(makeProps()))
    let pending
    act(() => { pending = result.current.handleRedo() })
    await act(async () => result.current.handleUndo())
    expect(appMocks.Undo).not.toHaveBeenCalled()
    await act(async () => { finish({ message: 'Nothing to redo' }); await pending })
  })
})


it('resets backend disc history when switching modes after undoing the disc crop', async () => {
  const props = { ...makeProps(), mode: 'disc', discActive: false }
  appMocks.ResetDisc.mockResolvedValue({ preview: '/clean', width: 100, height: 80 })
  appMocks.GetCleanPreview.mockResolvedValue({ preview: '/clean', width: 100, height: 80 })
  const { result } = renderHook(() => useImageActions(props))
  await act(async () => result.current.handleModeSwitch('normal'))
  expect(appMocks.ResetDisc).toHaveBeenCalledOnce()
})


describe('State transition ownership', () => {
  it('serializes rapid mode resets and only presents the newest response', async () => {
    let finishFirst
    appMocks.ResetNormal.mockReturnValue(new Promise(resolve => { finishFirst = resolve }))
    appMocks.ResetDisc.mockResolvedValue({ preview: '/latest', width: 100, height: 80 })
    const props = makeProps()
    const { result } = renderHook(() => useImageActions(props))
    let first, second
    await act(async () => { first = result.current.handleModeSwitch('disc') })
    await act(async () => { second = result.current.handleModeSwitch('line') })
    expect(props.setMode).toHaveBeenLastCalledWith('line')
    expect(appMocks.ResetDisc).not.toHaveBeenCalled()
    await act(async () => { finishFirst({ preview: '/stale', width: 7, height: 9 }); await first; await second })
    expect(appMocks.ResetDisc).toHaveBeenCalledOnce()
    expect(props.setPreview).not.toHaveBeenCalledWith('/stale')
    expect(props.setPreview).toHaveBeenLastCalledWith('/latest')
    expect(props.setFitWidth).not.toHaveBeenCalledWith(0)
  })

  it('does not let cancelled detection replace a reset preview or clear its busy state', async () => {
    let finishDetect, finishReset
    appMocks.DetectCorners.mockReturnValue(new Promise(resolve => { finishDetect = resolve }))
    appMocks.ResetCorners.mockReturnValue(new Promise(resolve => { finishReset = resolve }))
    const props = { ...makeProps(), mode: 'corner' }
    const { result } = renderHook(() => useImageActions(props))
    let detection, transition
    act(() => { detection = result.current.handleDetectCorners() })
    await act(async () => { transition = result.current.handleModeSwitch('normal') })
    props.setLoading.mockClear()
    await act(async () => { finishDetect({ preview: '/stale', width: 7, height: 9, corners: [] }); await detection })
    expect(props.setLoading).not.toHaveBeenCalledWith(false)
    expect(props.setPreview).not.toHaveBeenCalledWith('/stale')
    await act(async () => { finishReset({ preview: '/clean', width: 100, height: 80 }); await transition })
    expect(props.setPreview).toHaveBeenLastCalledWith('/clean')
  })

  it('ignores an old history response after a mode change', async () => {
    let finishUndo
    appMocks.Undo.mockReturnValue(new Promise(resolve => { finishUndo = resolve }))
    appMocks.ResetNormal.mockResolvedValue({ preview: '/clean', width: 100, height: 80 })
    const props = makeProps()
    const { result } = renderHook(() => useImageActions(props))
    let undo
    act(() => { undo = result.current.handleUndo() })
    await act(async () => result.current.handleModeSwitch('line'))
    await act(async () => { finishUndo({ changed: true, preview: '/old-history', width: 8, height: 6 }); await undo })
    expect(props.setPreview).not.toHaveBeenCalledWith('/old-history')
  })

  it('clears transient tools and preserves fit when loading another image', async () => {
    appMocks.LoadImageFromClipboard.mockResolvedValue({ preview: '/new-image', width: 120, height: 90 })
    const props = makeProps()
    props.touchupDraggingRef.current = true
    const { result } = renderHook(() => useImageActions(props))
    await act(async () => result.current.handlePasteImage())
    expect(props.setUseDescreenTool).toHaveBeenCalledWith(false)
    expect(props.setAdjustmentRect).toHaveBeenCalledWith(null)
    expect(props.touchupDraggingRef.current).toBe(false)
    expect(props.setFitWidth).not.toHaveBeenCalledWith(0)
    expect(props.setPreview).toHaveBeenLastCalledWith('/new-image')
  })
})


it('routes the full dropped batch to Corner Calibration while it is open', async () => {
  const props = makeProps()
  props.calibrationDropRef.current = vi.fn()
  props.compositorDropRef.current = vi.fn()
  renderHook(() => useImageActions(props))
  const drop = runtimeMocks.OnFileDrop.mock.calls.at(-1)[0]

  await act(async () => drop(0, 0, ['one.tif', 'notes.txt', 'two.JPG']))

  expect(props.calibrationDropRef.current).toHaveBeenCalledWith(['one.tif', 'two.JPG'])
  expect(props.compositorDropRef.current).not.toHaveBeenCalled()
  expect(appMocks.LoadImage).not.toHaveBeenCalled()
})

it('queues overlapping drops and keeps the presented document synchronized', async () => {
  let firstResolve, secondResolve
  appMocks.LoadImage.mockImplementationOnce(() => new Promise(resolve => { firstResolve = resolve }))
    .mockImplementationOnce(() => new Promise(resolve => { secondResolve = resolve }))
  const props = makeProps()
  renderHook(() => useImageActions(props))
  const drop = runtimeMocks.OnFileDrop.mock.calls.at(-1)[0]
  let first, second
  await act(async () => { first = drop(0, 0, ['first.png']) })
  await act(async () => { second = drop(0, 0, ['second.png']) })
  expect(appMocks.LoadImage).toHaveBeenCalledOnce()
  props.setLoading.mockClear()
  await act(async () => { firstResolve({ preview: '/first', width: 10, height: 10 }); await first })
  expect(appMocks.LoadImage).toHaveBeenCalledTimes(2)
  expect(props.setPreview).toHaveBeenLastCalledWith('/first')
  expect(props.setLoading).not.toHaveBeenCalledWith(false)
  await act(async () => { secondResolve({ preview: '/second', width: 20, height: 20 }); await second })
  expect(props.setPreview).toHaveBeenLastCalledWith('/second')
})


it('retains the successful document when a newer queued load fails', async () => {
  let finishFirst
  appMocks.LoadImage.mockImplementationOnce(() => new Promise(resolve => { finishFirst = resolve }))
    .mockRejectedValueOnce(new Error('Cannot decode second image'))
  const props = makeProps()
  renderHook(() => useImageActions(props))
  const drop = runtimeMocks.OnFileDrop.mock.calls.at(-1)[0]
  let first, second
  await act(async () => { first = drop(0, 0, ['first.png']) })
  await act(async () => { second = drop(0, 0, ['invalid.png']) })
  await act(async () => { finishFirst({ preview: '/first', width: 20, height: 30, format: 'PNG' }); await first; await second })
  expect(props.setPreview).toHaveBeenLastCalledWith('/first')
  expect(props.setRealImageDims).toHaveBeenLastCalledWith({ w: 20, h: 30 })
  expect(props.setImageMeta).toHaveBeenLastCalledWith({ format: 'PNG', dpiX: 0, dpiY: 0 })
  expect(props.setNormalCropApplied).toHaveBeenLastCalledWith(false)
  expect(props.setUnsavedChanges).toHaveBeenLastCalledWith(false)
  expect(props.setLoading).toHaveBeenLastCalledWith(false)
  expect(props.showError).toHaveBeenCalled()
})


it.each([false, true])('keeps the first clipboard load owned across mode changes (failure=%s)', async fail => {
  let finishRead, rejectRead
  appMocks.LoadImageFromClipboard.mockReturnValueOnce(new Promise((resolve, reject) => { finishRead = resolve; rejectRead = reject }))
    .mockResolvedValue({ preview: '/next-paste', width: 40, height: 30 })
  const props = { ...makeProps(), imageLoaded: false }
  const { result } = renderHook(() => useImageActions(props))
  let paste
  await act(async () => { paste = result.current.handlePasteImage() })
  await act(async () => result.current.handleModeSwitch('disc'))
  await act(async () => result.current.handleModeSwitch('line'))
  await act(async () => {
    if (fail) rejectRead(new Error('clipboard does not contain an image'))
    else finishRead({ preview: '/clipboard', width: 20, height: 30 })
    await paste
  })
  expect(props.setMode).toHaveBeenLastCalledWith('line')
  expect(props.setLoading).toHaveBeenLastCalledWith(false)
  expect(result.current.loadingFull).toBe(false)
  if (!fail) {
    expect(props.setImageLoaded).toHaveBeenCalledWith(true)
    expect(props.setPreview).toHaveBeenLastCalledWith('/clipboard')
  }
  await act(async () => result.current.handlePasteImage())
  expect(appMocks.LoadImageFromClipboard).toHaveBeenCalledTimes(2)
  expect(props.setPreview).toHaveBeenLastCalledWith('/next-paste')
})


it.each(['corner', 'disc', 'line', 'normal'])('restores Skip Crop on undo and redo in %s mode', async mode => {
  const props = { ...makeProps(), mode, discActive: mode === 'disc' }
  const response = { changed: true, cropSkipped: true, preview: '/skipped-edit', width: 100, height: 80 }
  appMocks.Undo.mockResolvedValue(response)
  appMocks.Redo.mockResolvedValue(response)
  const { result } = renderHook(() => useImageActions(props))
  await act(async () => result.current.handleUndo())
  expect(props.setCropSkipped).toHaveBeenLastCalledWith(true)
  await act(async () => result.current.handleRedo())
  expect(props.setCropSkipped).toHaveBeenLastCalledWith(true)
  appMocks.Redo.mockResolvedValue({ ...response, cropSkipped: false })
  await act(async () => result.current.handleRedo())
  expect(props.setCropSkipped).toHaveBeenLastCalledWith(false)
})
