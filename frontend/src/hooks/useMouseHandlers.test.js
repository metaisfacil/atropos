// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useMouseHandlers } from './useMouseHandlers'
import { DrawDisc } from '../../wailsjs/go/main/App'

vi.mock('../../wailsjs/go/main/App', () => ({
  ClickCorner: vi.fn(),
  StraightEdgeRotate: vi.fn(),
  RotateDisc: vi.fn(),
  ShiftDisc: vi.fn(),
  DrawDisc: vi.fn(),
  AddLine: vi.fn(),
  ProcessLines: vi.fn(),
  ClearLines: vi.fn(),
}))

const mounted = []
const unmounts = []
afterEach(() => {
  while (unmounts.length) unmounts.pop()?.()
  while (mounted.length) mounted.pop()?.remove()
})

function makeHitSurface() {
  const stage = document.createElement('div')
  const hit = document.createElement('div')
  stage.appendChild(hit)
  document.body.appendChild(stage)
  mounted.push(stage)

  Object.defineProperty(hit, 'clientWidth', { configurable: true, value: 400 })
  Object.defineProperty(hit, 'clientHeight', { configurable: true, value: 300 })
  Object.defineProperty(hit, 'naturalWidth', { configurable: true, value: 800 })
  Object.defineProperty(hit, 'naturalHeight', { configurable: true, value: 600 })
  hit.getBoundingClientRect = () => ({
    left: 10, top: 20, right: 410, bottom: 320,
    width: 400, height: 300, x: 10, y: 20, toJSON: () => ({}),
  })
  return hit
}

function makeArgs(overrides = {}) {
  const hit = makeHitSurface()
  const scroller = document.createElement('div')
  return {
    imageLoaded: true,
    loading: false,
    mode: 'normal',
    dragging: false,
    dragStart: null,
    dragCurrent: null,
    useTouchupTool: true,
    useStraightEdgeTool: false,
    discActive: false,
    linesProcessed: false,
    touchupStrokes: [],
    brushSize: 40,
    cornerState: { cornerCount: 0 },
    dotRadius: 5,
    cornersDetected: false,
    customCorner: false,
    linesDone: 0,
    normalRect: null,
    lines: [],
    realImageDims: { w: 800, h: 600 },
    discNoMaskPreview: null,
    discCenter: null,
    discRadius: 0,
    discRotation: 0,
    setDragging: vi.fn(),
    setDragStart: vi.fn(),
    setDragCurrent: vi.fn(),
    setTouchupStrokes: vi.fn(),
    setBrushSize: vi.fn(),
    setTouchupCursor: vi.fn(),
    setPreview: vi.fn(),
    setLoading: vi.fn(),
    setZoom: vi.fn(),
    setRealImageDims: vi.fn(),
    setCornerState: vi.fn(),
    setDetectedCornerPts: vi.fn(),
    setSelectedCornerPts: vi.fn(),
    setDiscActive: vi.fn(),
    setDiscNoMaskPreview: vi.fn(),
    setDiscCenter: vi.fn(),
    setDiscRadius: vi.fn(),
    setDiscRotation: vi.fn(),
    setDiscBgColor: vi.fn(),
    setNormalRect: vi.fn(),
    setLines: vi.fn(),
    setLinesDone: vi.fn(),
    setUnsavedChanges: vi.fn(),
    setDiscLiveActive: vi.fn(),
    setDiscLiveTransform: vi.fn(),
    setLinesProcessed: vi.fn(),
    setUseStraightEdgeTool: vi.fn(),
    setLineDragKind: vi.fn(),
    setNormalDragKind: vi.fn(),
    straightEdgeRemainsActive: false,
    spaceDownRef: { current: false },
    panDragRef: { current: null },
    canvasRef: { current: scroller },
    ctrlDragRef: { current: null },
    shiftDragRef: { current: null },
    touchupDraggingRef: { current: false },
    imgRef: { current: hit },
    lastResizeRef: { current: 0 },
    mousePosRef: { current: null },
    commitTouchup: vi.fn(),
    showStatus: vi.fn(),
    showError: vi.fn(),
    ...overrides,
  }
}

describe('touch-up brush resize gesture', () => {
  it('resizes with Alt + right-drag without painting and suppresses the context menu', async () => {
    const args = makeArgs()
    const { result, unmount } = renderHook(() => useMouseHandlers(args))
    unmounts.push(unmount)
    const downPreventDefault = vi.fn()

    act(() => {
      result.current.handleMouseDown({
        target: args.imgRef.current,
        button: 2,
        altKey: true,
        clientX: 100,
        clientY: 100,
        preventDefault: downPreventDefault,
      })
    })

    expect(downPreventDefault).toHaveBeenCalledOnce()
    expect(args.setTouchupCursor).toHaveBeenLastCalledWith({ x: 180, y: 160, resizing: true })
    expect(args.setTouchupStrokes).not.toHaveBeenCalled()
    expect(args.touchupDraggingRef.current).toBe(false)

    await act(async () => {
      await result.current.handleMouseMove({
        clientX: 135,
        clientY: 160,
        preventDefault: vi.fn(),
      })
    })
    expect(args.setBrushSize).toHaveBeenLastCalledWith(75)
    expect(args.setTouchupCursor).toHaveBeenLastCalledWith({ x: 250, y: 280, resizing: true })

    await act(async () => {
      await result.current.handleMouseMove({
        clientX: 400,
        clientY: 40,
        preventDefault: vi.fn(),
      })
    })
    expect(args.setBrushSize).toHaveBeenLastCalledWith(200)

    const upPreventDefault = vi.fn()
    await act(async () => {
      await result.current.handleMouseUp({ clientX: -100, clientY: 100, preventDefault: upPreventDefault })
    })
    expect(upPreventDefault).toHaveBeenCalledOnce()
    expect(args.setBrushSize).toHaveBeenLastCalledWith(4)
    expect(args.setTouchupCursor).toHaveBeenLastCalledWith(null)
    expect(args.commitTouchup).not.toHaveBeenCalled()

    const contextPreventDefault = vi.fn()
    act(() => {
      result.current.handleContextMenu({
        target: args.imgRef.current,
        altKey: false,
        preventDefault: contextPreventDefault,
      })
    })
    expect(contextPreventDefault).toHaveBeenCalledOnce()
  })


  it('tracks a live brush-outline cursor while the touch-up tool is active', async () => {
    const args = makeArgs()
    const { result, unmount } = renderHook(() => useMouseHandlers(args))
    unmounts.push(unmount)

    await act(async () => {
      await result.current.handleMouseMove({
        clientX: 210,
        clientY: 170,
        preventDefault: vi.fn(),
      })
    })

    expect(args.setTouchupCursor).toHaveBeenLastCalledWith({ x: 400, y: 300, resizing: false })

    act(() => result.current.handleImageMouseLeave())
    expect(args.setTouchupCursor).toHaveBeenLastCalledWith(null)
  })

  it('keeps ordinary left-drag painting behavior intact', () => {
    const args = makeArgs()
    const { result, unmount } = renderHook(() => useMouseHandlers(args))
    unmounts.push(unmount)

    act(() => {
      result.current.handleMouseDown({
        target: args.imgRef.current,
        button: 0,
        altKey: false,
        clientX: 110,
        clientY: 120,
        preventDefault: vi.fn(),
      })
    })

    expect(args.touchupDraggingRef.current).toBe(true)
    expect(args.setTouchupStrokes).toHaveBeenCalledWith([{ x: 200, y: 200 }])
    expect(args.setBrushSize).not.toHaveBeenCalled()
  })
})

describe('disc crop presentation', () => {
  it('publishes the cropped dimensions returned by DrawDisc', async () => {
    DrawDisc.mockResolvedValue({
      preview: '/preview/2',
      width: 130,
      height: 130,
      discRotation: 0,
    })
    const args = makeArgs({
      mode: 'disc',
      useTouchupTool: false,
      dragging: true,
      dragStart: { x: 100, y: 100 },
      dragCurrent: { x: 200, y: 150 },
    })
    const { result, unmount } = renderHook(() => useMouseHandlers(args))
    unmounts.push(unmount)

    await act(async () => {
      await result.current.handleMouseUp({
        target: args.imgRef.current,
        clientX: 210,
        clientY: 170,
      })
    })

    expect(args.setPreview).toHaveBeenCalledWith('/preview/2')
    expect(args.setRealImageDims).toHaveBeenCalledWith({ w: 130, h: 130 })
  })
})
