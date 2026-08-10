// @vitest-environment jsdom
import React from 'react'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import StatusBar from './StatusBar'

const baseProps = {
  imageLoaded: true,
  imageMeta: { format: 'JPEG' },
  inputImageDims: { w: 2400, h: 1600 },
  realImageDims: { w: 2400, h: 1600 },
  zoom: 1,
  onResetZoom: () => {},
}

afterEach(cleanup)

describe('StatusBar preview resolution', () => {
  it('shows the low-resolution tier', () => {
    render(React.createElement(StatusBar, { ...baseProps, previewResolution: 'low' }))
    expect(screen.getByText('Low-res preview')).toBeTruthy()
    expect(screen.queryByText('High-res preview')).toBeNull()
  })

  it('shows the high-resolution tier', () => {
    render(React.createElement(StatusBar, { ...baseProps, previewResolution: 'high' }))
    expect(screen.getByText('High-res preview')).toBeTruthy()
    expect(screen.queryByText('Low-res preview')).toBeNull()
  })
})
