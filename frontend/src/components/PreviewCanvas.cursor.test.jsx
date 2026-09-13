// @vitest-environment jsdom
import React, { useRef } from 'react'
import { act, cleanup, render } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import PreviewCanvas from './PreviewCanvas'
import { usePresentedValue } from '../hooks/useProgressivePreview'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('moves and clears the brush ring while preview metadata is frozen', () => {
  const frames = new Map()
  let frameID = 0
  vi.stubGlobal('requestAnimationFrame', callback => {
    frames.set(++frameID, callback)
    return frameID
  })
  vi.stubGlobal('cancelAnimationFrame', id => frames.delete(id))
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    disconnect() {}
  })
  const context = {
    setTransform: vi.fn(), clearRect: vi.fn(), beginPath: vi.fn(),
    arc: vi.fn(), stroke: vi.fn(), save: vi.fn(), restore: vi.fn(),
  }
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(context)
  const drawFrame = () => act(() => {
    const callbacks = [...frames.values()]
    frames.clear()
    callbacks.forEach(callback => callback())
  })
  const dims = { w: 800, h: 600 }
  function Preview({ pending, cursor }) {
    const scrollRef = useRef(null)
    const imgRef = useRef(null)
    const visual = usePresentedValue({
      realImageDims: dims, useTouchupTool: true, brushSize: pending ? 80 : 40,
    }, pending)
    return <PreviewCanvas
      imageDims={dims} displayWidth={400} scrollRef={scrollRef} imgRef={imgRef}
      visual={visual} touchupCursor={cursor}
    />
  }

  const { rerender } = render(<Preview pending={false} cursor={{ x: 100, y: 100 }} />)
  drawFrame()
  expect(context.arc).toHaveBeenLastCalledWith(50, 50, 10, 0, Math.PI * 2)
  context.arc.mockClear()

  rerender(<Preview pending cursor={{ x: 200, y: 160 }} />)
  drawFrame()
  // Position advances, but brush size remains paired with the presented visual.
  expect(context.arc).toHaveBeenLastCalledWith(100, 80, 10, 0, Math.PI * 2)
  context.arc.mockClear()

  rerender(<Preview pending cursor={null} />)
  drawFrame()
  expect(context.arc).not.toHaveBeenCalled()
})
