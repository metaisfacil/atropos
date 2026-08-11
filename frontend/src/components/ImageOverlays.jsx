import React from 'react'

function pct(value, total) {
  if (!(total > 0)) return 0
  return (value / total) * 100
}

function NormalHitTargets({ rect, dims }) {
  if (!rect || !(dims?.w > 0) || !(dims?.h > 0)) return null

  const left = Math.min(rect.x1, rect.x2)
  const right = Math.max(rect.x1, rect.x2)
  const top = Math.min(rect.y1, rect.y2)
  const bottom = Math.max(rect.y1, rect.y2)

  const l = pct(left, dims.w)
  const r = pct(right, dims.w)
  const t = pct(top, dims.h)
  const b = pct(bottom, dims.h)
  const width = Math.max(0, r - l)
  const height = Math.max(0, b - t)

  const edge = (name, style, cursor, orientation) => (
    <div
      key={name}
      data-normal-handle={name}
      className={`preview-hit-target preview-hit-edge preview-hit-edge--${orientation}`}
      style={{ ...style, cursor }}
    />
  )

  const corner = (name, x, y, cursor) => (
    <div
      key={name}
      data-normal-handle={name}
      className="preview-hit-target preview-hit-corner"
      style={{ left: `${x}%`, top: `${y}%`, cursor }}
    />
  )

  return (
    <>
      {edge('n', { left: `${l}%`, top: `${t}%`, width: `${width}%` }, 'ns-resize', 'horizontal')}
      {edge('e', { left: `${r}%`, top: `${t}%`, height: `${height}%`, width: 0 }, 'ew-resize', 'vertical')}
      {edge('s', { left: `${l}%`, top: `${b}%`, width: `${width}%` }, 'ns-resize', 'horizontal')}
      {edge('w', { left: `${l}%`, top: `${t}%`, height: `${height}%`, width: 0 }, 'ew-resize', 'vertical')}
      {corner('nw', l, t, 'nwse-resize')}
      {corner('ne', r, t, 'nesw-resize')}
      {corner('se', r, b, 'nwse-resize')}
      {corner('sw', l, b, 'nesw-resize')}
    </>
  )
}

function LineHitTargets({ lines, dims }) {
  if (!Array.isArray(lines) || !(dims?.w > 0) || !(dims?.h > 0)) return null
  return lines.flatMap((line, index) => [
    <div
      key={`${index}-start`}
      data-line-handle-index={index}
      data-line-handle-end="start"
      className="preview-hit-target preview-hit-line-point"
      style={{ left: `${pct(line.x1, dims.w)}%`, top: `${pct(line.y1, dims.h)}%` }}
    />,
    <div
      key={`${index}-end`}
      data-line-handle-index={index}
      data-line-handle-end="end"
      className="preview-hit-target preview-hit-line-point"
      style={{ left: `${pct(line.x2, dims.w)}%`, top: `${pct(line.y2, dims.h)}%` }}
    />,
  ])
}

// Visible drawing moved to PreviewCanvas so image pixels and vector guides are
// composited by one renderer. This component now exists solely to preserve
// large, accessible DOM hit targets for resize/edit handles. Keeping hit
// testing in the DOM lets the existing mouse state machine remain unchanged
// without using DOM image elements as a rendering primitive.
export default function ImageOverlays({
  mode,
  normalRect,
  lines,
  realImageDims,
}) {
  return (
    <div className="preview-interaction-overlay" aria-hidden="true">
      {mode === 'normal' && normalRect && (
        <NormalHitTargets rect={normalRect} dims={realImageDims} />
      )}
      {mode === 'line' && (
        <LineHitTargets lines={lines} dims={realImageDims} />
      )}
    </div>
  )
}
