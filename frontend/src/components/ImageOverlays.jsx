import React from 'react'

export default function ImageOverlays({
  realImageDims,
  mode,
  dragging, dragStart, dragCurrent,
  useTouchupTool, touchupStrokes, brushSize,
  useStraightEdgeTool,
  discActive,
  discLiveActive,
  discCenter,
  discRadius,
  discBgColor,
  discCenterCutout, discCutoutPercent,
  ctrlDragRef, shiftDragRef,
  detectedCornerPts, selectedCornerPts, dotRadius,
  normalRect,
  lines,
  displayToImage,
  lineStartImgRef,
}) {
  return (
    <>
      {useTouchupTool && touchupStrokes.length > 0 && (
        <svg
          viewBox={`0 0 ${realImageDims.w} ${realImageDims.h}`}
          preserveAspectRatio="none"
          style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6 }}
        >
          {touchupStrokes.map((pt, i) => (
            <circle key={i} cx={pt.x} cy={pt.y} r={brushSize / 2}
              fill="rgba(255,0,0,0.35)" stroke="rgba(255,0,0,0.8)"
              vectorEffect="non-scaling-stroke" />
          ))}
        </svg>
      )}
      {useStraightEdgeTool && dragging && dragStart && dragCurrent && (
        <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6, overflow: 'visible' }}>
          <line
            x1={dragStart.x} y1={dragStart.y}
            x2={dragCurrent.x} y2={dragCurrent.y}
            stroke="#ffff00" strokeWidth="2"
          />
        </svg>
      )}


      {mode === 'disc' && !useStraightEdgeTool && !useTouchupTool && dragging && dragStart && dragCurrent &&
       ctrlDragRef.current === null && shiftDragRef.current === null && (() => {
        const guideR = Math.sqrt((dragStart.x - dragCurrent.x) ** 2 + (dragStart.y - dragCurrent.y) ** 2)
        return (
          <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6, overflow: 'visible' }}>
            <circle
              cx={dragCurrent.x} cy={dragCurrent.y}
              r={guideR}
              stroke="#00ff00" strokeWidth="2" fill="none"
            />
            {discCenterCutout && discCutoutPercent > 0 && (
              <circle
                cx={dragCurrent.x} cy={dragCurrent.y}
                r={guideR * discCutoutPercent / 100}
                stroke="#00ff00" strokeWidth="2" fill="none" strokeDasharray="4 3"
              />
            )}
          </svg>
        )
       })()}
      {mode === 'corner' && (detectedCornerPts.length > 0 || selectedCornerPts.length > 0) && (() => {
        // Use viewBox to map image-space coords to display space (same pattern as line/touchup
        // overlays). Avoids reliance on fitWidth, which may be 0 on first render on Mac.
        const referenceSize = 1600 // base image size for slider values
        const minDim = Math.min(realImageDims.w, realImageDims.h)
        const imgScale = minDim > 0 ? minDim / referenceSize : 1
        const dotR = Math.max(2, Math.round(dotRadius * imgScale))
        const selectedR = Math.max(dotR * 1.5, dotR + 4)

        const xCoords = detectedCornerPts.map(p => p.X)
        const yCoords = detectedCornerPts.map(p => p.Y)
        console.log('[CornerOverlay] realImageDims:', realImageDims.w, 'x', realImageDims.h,
          '| corners:', detectedCornerPts.length,
          '| X range:', Math.min(...xCoords).toFixed(0), '-', Math.max(...xCoords).toFixed(0),
          '| Y range:', Math.min(...yCoords).toFixed(0), '-', Math.max(...yCoords).toFixed(0),
          '| dotR:', dotR)

        return (
          <svg
            ref={el => {
              if (el) {
                const r = el.getBoundingClientRect()
                const pr = el.parentElement?.getBoundingClientRect()
                console.log('[CornerOverlay] SVG rect:', r.width.toFixed(0), 'x', r.height.toFixed(0),
                  'at', r.left.toFixed(0), r.top.toFixed(0),
                  '| parent rect:', pr ? `${pr.width.toFixed(0)}x${pr.height.toFixed(0)} at ${pr.left.toFixed(0)},${pr.top.toFixed(0)}` : 'n/a')
              }
            }}
            viewBox={`0 0 ${realImageDims.w} ${realImageDims.h}`}
            preserveAspectRatio="none"
            style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6 }}
          >
            {detectedCornerPts.map((pt, i) => (
              <circle key={`d${i}`} cx={pt.X} cy={pt.Y} r={dotR}
                fill="rgba(255,0,0,0.6)" stroke="red" strokeWidth="1" vectorEffect="non-scaling-stroke" />
            ))}
            {selectedCornerPts.map((pt, i) => (
              <circle key={`s${i}`} cx={pt.X} cy={pt.Y} r={selectedR}
                fill="rgba(0,255,0,0.6)" stroke="lime" strokeWidth="2" vectorEffect="non-scaling-stroke" />
            ))}
          </svg>
        )
      })()}
      {mode === 'normal' && (() => {
        // During drag: show live rect in display-space coords
        if (dragging && dragStart && dragCurrent && !useTouchupTool) {
          const x = Math.min(dragStart.x, dragCurrent.x)
          const y = Math.min(dragStart.y, dragCurrent.y)
          const w = Math.abs(dragCurrent.x - dragStart.x)
          const h = Math.abs(dragCurrent.y - dragStart.y)
          return (
            <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6, overflow: 'visible' }}>
              <rect x={x} y={y} width={w} height={h}
                stroke="#00ff00" strokeWidth="2" fill="rgba(0,255,0,0.08)" strokeDasharray="6 3" />
            </svg>
          )
        }
        // Confirmed selection: use viewBox so it tracks zoom/resize
        if (normalRect) {
          const x1 = Math.min(normalRect.x1, normalRect.x2)
          const y1 = Math.min(normalRect.y1, normalRect.y2)
          const x2 = Math.max(normalRect.x1, normalRect.x2)
          const y2 = Math.max(normalRect.y1, normalRect.y2)
          return (
            <svg
              viewBox={`0 0 ${realImageDims.w} ${realImageDims.h}`}
              preserveAspectRatio="none"
              style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6 }}
            >
              <rect x={x1} y={y1} width={x2 - x1} height={y2 - y1}
                stroke="#00ff00" strokeWidth="2" fill="rgba(0,255,0,0.08)"
                strokeDasharray="6 3" vectorEffect="non-scaling-stroke" />
            </svg>
          )
        }
        return null
      })()}
      {mode === 'line' && (() => {
        const allLines = [...lines] // image-space coords
        if (dragging && dragStart && dragCurrent) {
          // Use the image-space start captured at mousedown when available.
          // This keeps the line stable if the user zooms mid-drag, since
          // dragStart.x is a CSS-pixel value that becomes stale after a zoom.
          const s = lineStartImgRef?.current || displayToImage(dragStart.x, dragStart.y)
          const e = displayToImage(dragCurrent.x, dragCurrent.y)
          allLines.push({ x1: s.x, y1: s.y, x2: e.x, y2: e.y })
        }
        return allLines.length > 0 ? (
          <svg
            viewBox={`0 0 ${realImageDims.w} ${realImageDims.h}`}
            preserveAspectRatio="none"
            style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none', zIndex: 6, overflow: 'visible' }}
          >
            {allLines.map((ln, i) => (
              <line key={i} x1={ln.x1} y1={ln.y1} x2={ln.x2} y2={ln.y2}
                stroke="#00ff00" strokeWidth="2" vectorEffect="non-scaling-stroke" />
            ))}
          </svg>
        ) : null
      })()}
    </>
  )
}
