function validDims(value) {
  return value && Number.isFinite(value.w) && Number.isFinite(value.h) && value.w > 0 && value.h > 0
}

// clientWidth/clientHeight exclude active scrollbars. Using them as the fit
// baseline makes zoom self-referential: a scrollbar shrinks the baseline,
// which can remove the scrollbar and restore the old baseline indefinitely.
// The border box stays constant while scrollbars come and go.
export function fitWidthFor(container, dims) {
  if (!container || !validDims(dims)) return 0
  const bounds = container.getBoundingClientRect?.()
  const width = Number.isFinite(bounds?.width) && bounds.width > 0
    ? bounds.width
    : container.clientWidth
  const height = Number.isFinite(bounds?.height) && bounds.height > 0
    ? bounds.height
    : container.clientHeight
  const availableWidth = Math.max(1, Number(width) || 0)
  const availableHeight = Math.max(1, Number(height) || 0)
  return Math.min(availableWidth, availableHeight * (dims.w / dims.h))
}
