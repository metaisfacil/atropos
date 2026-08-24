import { describe, expect, it } from 'vitest'
import {
  DEFAULT_SPATIAL_KEY_LABELS,
  spatialKeyLabels,
  spatialShortcutKey,
} from './keyboardShortcuts'

describe('spatialShortcutKey', () => {
  it('uses the physical key code rather than the layout character', () => {
    expect(spatialShortcutKey({ key: 'z', code: 'KeyW' })).toBe('w')
    expect(spatialShortcutKey({ key: 'q', code: 'KeyA' })).toBe('a')
  })

  it('does not intercept modified command keys', () => {
    expect(spatialShortcutKey({ code: 'KeyW', ctrlKey: true })).toBeNull()
    expect(spatialShortcutKey({ code: 'KeyQ', metaKey: true })).toBeNull()
    expect(spatialShortcutKey({ code: 'KeyE', altKey: true })).toBeNull()
  })
})

describe('spatialKeyLabels', () => {
  it('formats an AZERTY layout as ZQSD and AE', () => {
    const labels = spatialKeyLabels(new Map([
      ['KeyW', 'z'], ['KeyA', 'q'], ['KeyS', 's'], ['KeyD', 'd'],
      ['KeyQ', 'a'], ['KeyE', 'e'],
    ]))

    expect([labels.KeyW, labels.KeyA, labels.KeyS, labels.KeyD]).toEqual(['Z', 'Q', 'S', 'D'])
    expect([labels.KeyQ, labels.KeyE]).toEqual(['A', 'E'])
  })

  it('preserves punctuation used by Dvorak', () => {
    const labels = spatialKeyLabels(new Map([
      ['KeyW', ','], ['KeyA', 'a'], ['KeyS', 'o'], ['KeyD', 'e'],
      ['KeyQ', "'"], ['KeyE', '.'],
    ]))

    expect([labels.KeyW, labels.KeyA, labels.KeyS, labels.KeyD]).toEqual([',', 'A', 'O', 'E'])
    expect([labels.KeyQ, labels.KeyE]).toEqual(["'", '.'])
  })

  it('falls back to QWERTY for missing layout entries', () => {
    expect(spatialKeyLabels(null)).toEqual(DEFAULT_SPATIAL_KEY_LABELS)
  })
})
