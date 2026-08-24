// @vitest-environment jsdom
import React from 'react'
import { act, cleanup, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ShortcutsPanel from './ShortcutsPanel'

const originalKeyboard = Object.getOwnPropertyDescriptor(navigator, 'keyboard')

function makeKeyboard(entries) {
  const keyboard = new EventTarget()
  keyboard.getLayoutMap = vi.fn().mockResolvedValue(new Map(entries))
  Object.defineProperty(navigator, 'keyboard', {
    configurable: true,
    value: keyboard,
  })
  return keyboard
}

function renderPanel() {
  return render(
    <ShortcutsPanel
      shortcutsOpen
      setShortcutsOpen={vi.fn()}
      mode="normal"
      discActive={false}
      canSave
      imageLoaded
      canCopySelection={false}
    />,
  )
}

beforeEach(() => {
  makeKeyboard([
    ['KeyW', 'z'], ['KeyA', 'q'], ['KeyS', 's'], ['KeyD', 'd'],
    ['KeyQ', 'a'], ['KeyE', 'e'],
  ])
})

afterEach(() => {
  cleanup()
  if (originalKeyboard) Object.defineProperty(navigator, 'keyboard', originalKeyboard)
  else delete navigator.keyboard
})

describe('ShortcutsPanel keyboard layout labels', () => {
  it('shows the active layout characters for the physical spatial controls', async () => {
    const { container } = renderPanel()
    const rows = container.querySelectorAll('.shortcut-item')

    await waitFor(() => {
      expect([...rows[0].querySelectorAll('kbd')].map(key => key.textContent)).toEqual(['Z', 'Q', 'S', 'D'])
      expect([...rows[1].querySelectorAll('kbd')].map(key => key.textContent)).toEqual(['A', 'E'])
    })
  })

  it('refreshes the displayed keys when the active layout changes', async () => {
    let layout = new Map([
      ['KeyW', 'z'], ['KeyA', 'q'], ['KeyS', 's'], ['KeyD', 'd'],
      ['KeyQ', 'a'], ['KeyE', 'e'],
    ])
    const keyboard = makeKeyboard([])
    keyboard.getLayoutMap.mockImplementation(async () => layout)
    const { container } = renderPanel()
    const cropKeys = () => [...container.querySelectorAll('.shortcut-item')[0].querySelectorAll('kbd')]
      .map(key => key.textContent)

    await waitFor(() => expect(cropKeys()).toEqual(['Z', 'Q', 'S', 'D']))

    layout = new Map([
      ['KeyW', ','], ['KeyA', 'a'], ['KeyS', 'o'], ['KeyD', 'e'],
      ['KeyQ', "'"], ['KeyE', '.'],
    ])
    act(() => keyboard.dispatchEvent(new Event('layoutchange')))

    await waitFor(() => expect(cropKeys()).toEqual([',', 'A', 'O', 'E']))
  })
})
