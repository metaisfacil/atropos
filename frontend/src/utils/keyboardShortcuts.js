export const SPATIAL_SHORTCUT_KEYS = Object.freeze({
  KeyW: 'w',
  KeyA: 'a',
  KeyS: 's',
  KeyD: 'd',
  KeyQ: 'q',
  KeyE: 'e',
})

export const DEFAULT_SPATIAL_KEY_LABELS = Object.freeze({
  KeyW: 'W',
  KeyA: 'A',
  KeyS: 'S',
  KeyD: 'D',
  KeyQ: 'Q',
  KeyE: 'E',
})

export const CROP_SHORTCUT_CODES = Object.freeze(['KeyW', 'KeyA', 'KeyS', 'KeyD'])
export const ROTATE_SHORTCUT_CODES = Object.freeze(['KeyQ', 'KeyE'])

// Spatial editing controls intentionally follow physical key positions. This
// makes the QWERTY WASD/QE cluster become ZQSD/AE on AZERTY, for example.
export function spatialShortcutKey(event) {
  if (event.ctrlKey || event.metaKey || event.altKey) return null
  return SPATIAL_SHORTCUT_KEYS[event.code] || null
}

function displayKeyLabel(value, fallback) {
  if (typeof value !== 'string' || value.length === 0) return fallback
  return value.length === 1 ? value.toLocaleUpperCase() : value
}

export function spatialKeyLabels(layoutMap) {
  return Object.fromEntries(
    Object.entries(DEFAULT_SPATIAL_KEY_LABELS).map(([code, fallback]) => [
      code,
      displayKeyLabel(layoutMap?.get?.(code), fallback),
    ]),
  )
}
