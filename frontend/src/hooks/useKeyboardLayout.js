import { useEffect, useState } from 'react'
import {
  DEFAULT_SPATIAL_KEY_LABELS,
  spatialKeyLabels,
} from '../utils/keyboardShortcuts'

export function useKeyboardLayout() {
  const [labels, setLabels] = useState(DEFAULT_SPATIAL_KEY_LABELS)

  useEffect(() => {
    const keyboard = typeof navigator !== 'undefined' ? navigator.keyboard : null
    if (typeof keyboard?.getLayoutMap !== 'function') return undefined

    let active = true
    const refresh = async () => {
      try {
        const layoutMap = await keyboard.getLayoutMap()
        if (active) setLabels(spatialKeyLabels(layoutMap))
      } catch {
        // Keyboard Map is permission/policy gated. QWERTY labels remain a safe
        // fallback while the physical shortcuts themselves continue to work.
      }
    }

    refresh()
    keyboard.addEventListener?.('layoutchange', refresh)
    window.addEventListener('focus', refresh)
    return () => {
      active = false
      keyboard.removeEventListener?.('layoutchange', refresh)
      window.removeEventListener('focus', refresh)
    }
  }, [])

  return labels
}
