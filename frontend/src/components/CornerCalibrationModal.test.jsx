// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import CornerCalibrationModal, {
  CALIBRATION_CORNER_LABELS,
  calibrationPointFromEvent,
  normalizeCalibrationPaths,
} from './CornerCalibrationModal'

const appMocks = vi.hoisted(() => ({
  CalibrationClearPreview: vi.fn(() => Promise.resolve()),
  CalibrationLoadImage: vi.fn(() => Promise.resolve({
    preview: '/__atropos/preview/calibration/1.jpg',
    width: 1000,
    height: 800,
  })),
  CalibrationOpenFilesDialog: vi.fn(),
  CalibrationSaveDataset: vi.fn(() => Promise.resolve({ outputPath: 'ground-truth.json', count: 1 })),
  RenderCalibrationPreviewViewport: vi.fn(),
}))

vi.mock('../../wailsjs/go/main/App', () => appMocks)
vi.mock('./PreviewCanvas', async () => {
  const React = await import('react')
  return {
    default: ({ source, imgRef, onMouseDown, onPresented }) => {
      const ref = React.useRef(null)
      React.useEffect(() => {
        imgRef.current = ref.current
        if (source) onPresented?.(source, { w: 1000, h: 800 })
      }, [imgRef, onPresented, source])
      return <div data-testid="calibration-canvas" ref={ref} onMouseDown={onMouseDown} />
    },
  }
})

describe('corner calibration helpers', () => {
  it('keeps supported scans in drop order and removes path duplicates', () => {
    expect(normalizeCalibrationPaths(
      ['C:\\scans\\One.TIF'],
      ['c:\\SCANS\\one.tif', 'C:\\scans\\two.png', 'notes.txt', 'C:\\scans\\three.webp'],
    )).toEqual([
      'C:\\scans\\One.TIF',
      'C:\\scans\\two.png',
      'C:\\scans\\three.webp',
    ])
  })

  it('maps fitted and zoomed display clicks to clamped source pixels', () => {
    const element = {
      getBoundingClientRect: () => ({ left: 10, top: 20, width: 500, height: 400 }),
    }
    expect(calibrationPointFromEvent(
      { clientX: 260, clientY: 220 },
      element,
      { w: 1000, h: 800 },
    )).toEqual({ x: 500, y: 400 })
    expect(calibrationPointFromEvent(
      { clientX: 900, clientY: -30 },
      element,
      { w: 1000, h: 800 },
    )).toEqual({ x: 999, y: 0 })
  })

  it('defines the exported semantic order explicitly', () => {
    expect(CALIBRATION_CORNER_LABELS).toEqual(['TL', 'TR', 'BR', 'BL'])
  })

  it('accepts a dropped batch and exports four source-pixel points', async () => {
    const dropRef = { current: null }
    render(<CornerCalibrationModal open onClose={vi.fn()} dropRef={dropRef} />)

    act(() => dropRef.current(['C:\\scans\\one.tif', 'C:\\scans\\two.tif']))
    await waitFor(() => expect(appMocks.CalibrationLoadImage).toHaveBeenCalledWith({ filePath: 'C:\\scans\\one.tif' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Skip' }).disabled).toBe(false))

    const canvas = screen.getByTestId('calibration-canvas')
    canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 100, height: 80 })
    fireEvent.mouseDown(canvas, { button: 0, clientX: 1, clientY: 2 })
    fireEvent.mouseDown(canvas, { button: 0, clientX: 98, clientY: 2 })
    fireEvent.mouseDown(canvas, { button: 0, clientX: 98, clientY: 78 })
    fireEvent.mouseDown(canvas, { button: 0, clientX: 1, clientY: 78 })
    fireEvent.click(screen.getByRole('button', { name: 'Save & next' }))
    const exportButton = await screen.findByRole('button', { name: 'Export JSON (1)' })
    fireEvent.click(exportButton)

    await waitFor(() => expect(appMocks.CalibrationSaveDataset).toHaveBeenCalledWith({
      entries: [{
        imagePath: 'C:\\scans\\one.tif',
        width: 1000,
        height: 800,
        corners: [
          { x: 10, y: 20 },
          { x: 980, y: 20 },
          { x: 980, y: 780 },
          { x: 10, y: 780 },
        ],
      }],
    }))
  })
})
