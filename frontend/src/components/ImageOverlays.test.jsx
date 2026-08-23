// @vitest-environment jsdom
import React from 'react'
import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import ImageOverlays from './ImageOverlays'

afterEach(cleanup)

describe('selection hit targets', () => {
  it('clips boundary handles so they cannot enlarge the preview scroll extent', () => {
    const { container } = render(
      <ImageOverlays
        mode="normal"
        normalRect={{ x1: 10, y1: 10, x2: 100, y2: 100 }}
        realImageDims={{ w: 100, h: 100 }}
      />,
    )

    const overlay = container.querySelector('.preview-interaction-overlay')
    expect(overlay.style.overflow).toBe('hidden')
    expect(container.querySelector('[data-normal-handle="se"]')).not.toBeNull()
  })
})
