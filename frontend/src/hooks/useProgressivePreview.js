import { useEffect, useLayoutEffect, useRef, useState } from 'react'

export const PREVIEW_ASSET_PREFIX = '/__atropos/preview/'
export const FULL_RESOLUTION_PROMOTION_DELAY_MS = 750

export function lowResolutionPreviewURL(source) {
  if (typeof source !== 'string' || !source.startsWith(PREVIEW_ASSET_PREFIX) || !source.endsWith('.jpg')) {
    return null
  }
  return source.slice(0, -4) + '-low.jpg'
}

export function previewAssetSession(source) {
  if (typeof source !== 'string' || !source.startsWith(PREVIEW_ASSET_PREFIX)) return null
  const remainder = source.slice(PREVIEW_ASSET_PREFIX.length)
  const separator = remainder.indexOf('/')
  return separator > 0 ? remainder.slice(0, separator) : null
}

export function isPreviewVariant(displaySource, previewSource) {
  return Boolean(previewSource) && (
    displaySource === previewSource || displaySource === lowResolutionPreviewURL(previewSource)
  )
}

export function isPreviewPresentationPending(previewSource, presentedSource) {
  return Boolean(previewSource) && previewSource !== presentedSource
}

// Keeps result metadata visually paired with the image revision it describes.
// The last presented value remains rendered while a replacement preview is
// decoding, then the current value is released on the same render that marks
// the replacement preview as presented.
export function usePresentedValue(value, presentationPending) {
  const presentedRef = useRef(value)

  useLayoutEffect(() => {
    if (!presentationPending) presentedRef.current = value
  }, [value, presentationPending])

  return presentationPending ? presentedRef.current : value
}

// Keeps the previous image visible while the next small preview loads, then
// promotes the same immutable revision to its full-resolution resource.
function abortImageLoad(loader) {
  if (!loader) return
  loader.onload = null
  loader.onerror = null
  if (typeof loader.removeAttribute === 'function') loader.removeAttribute('src')
}

export function useProgressivePreview(source, deferFullResolution = false) {
  const [displaySource, setDisplaySource] = useState(null)
  const [lowState, setLowState] = useState({ source: null, status: 'idle' })

  useEffect(() => {
    if (!source) {
      setDisplaySource(null)
      setLowState({ source: null, status: 'idle' })
      return undefined
    }

    const lowSource = lowResolutionPreviewURL(source)
    if (!lowSource) {
      setDisplaySource(source)
      setLowState({ source, status: 'unavailable' })
      return undefined
    }

    let cancelled = false
    let settled = false
    setLowState({ source, status: 'loading' })
    const lowLoader = new Image()
    lowLoader.onload = () => {
      if (cancelled) return
      settled = true
      setDisplaySource(lowSource)
      setLowState({ source, status: 'ready' })
    }
    lowLoader.onerror = () => {
      if (cancelled) return
      settled = true
      setLowState({ source, status: 'failed' })
    }
    lowLoader.src = lowSource

    return () => {
      cancelled = true
      if (!settled) abortImageLoad(lowLoader)
    }
  }, [source])

  useEffect(() => {
    const lowSource = lowResolutionPreviewURL(source)
    if (!lowSource || lowState.source !== source) return undefined

    const lowFailed = lowState.status === 'failed'
    const lowPresented = lowState.status === 'ready' && displaySource === lowSource
    if (!lowFailed && !lowPresented) return undefined
    if (deferFullResolution && !lowFailed) return undefined

    let cancelled = false
    let settled = false
    let fullLoader = null
    const loadFullResolution = () => {
      if (cancelled) return
      fullLoader = new Image()
      fullLoader.onload = () => {
        if (cancelled) return
        settled = true
        setDisplaySource(source)
      }
      // Retain the previous or low-resolution image on failure. A URL is
      // assigned to the visible element only after successful browser decode.
      fullLoader.onerror = () => { settled = true }
      fullLoader.src = source
    }

    // A failed low asset has no visible fallback, so request full immediately.
    // Otherwise wait for an idle window before starting the expensive encode.
    let fullTimer = null
    if (lowFailed) {
      loadFullResolution()
    } else {
      fullTimer = setTimeout(loadFullResolution, FULL_RESOLUTION_PROMOTION_DELAY_MS)
    }

    return () => {
      cancelled = true
      if (fullTimer !== null) clearTimeout(fullTimer)
      if (!settled) abortImageLoad(fullLoader)
    }
  }, [source, lowState, displaySource, deferFullResolution])

  if (!source) return null
  const sourceSession = previewAssetSession(source)
  const displaySession = previewAssetSession(displaySource)
  if (sourceSession && sourceSession !== displaySession) return null
  return displaySource
}
