import { useEffect, useLayoutEffect, useRef, useState } from 'react'

export const PREVIEW_ASSET_PREFIX = '/__atropos/preview/'

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
export function useProgressivePreview(source) {
  const [displaySource, setDisplaySource] = useState(null)

  useEffect(() => {
    if (!source) {
      setDisplaySource(null)
      return undefined
    }

    const lowSource = lowResolutionPreviewURL(source)
    if (!lowSource) {
      setDisplaySource(source)
      return undefined
    }

    let cancelled = false
    let fullLoader = null
    const loadFullResolution = () => {
      fullLoader = new Image()
      fullLoader.onload = () => {
        if (!cancelled) setDisplaySource(source)
      }
      // Retain the previous or low-resolution image on failure. A URL is
      // assigned to the visible element only after successful browser decode.
      fullLoader.onerror = () => {}
      fullLoader.src = source
    }
    const lowLoader = new Image()
    lowLoader.onload = () => {
      if (cancelled) return
      setDisplaySource(lowSource)
      loadFullResolution()
    }
    lowLoader.onerror = () => {
      if (!cancelled) loadFullResolution()
    }
    lowLoader.src = lowSource

    return () => {
      cancelled = true
      lowLoader.onload = null
      lowLoader.onerror = null
      if (fullLoader) {
        fullLoader.onload = null
        fullLoader.onerror = null
      }
    }
  }, [source])

  if (!source) return null
  const sourceSession = previewAssetSession(source)
  const displaySession = previewAssetSession(displaySource)
  if (sourceSession && sourceSession !== displaySession) return null
  return displaySource
}
