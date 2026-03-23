import React from 'react'
import DelayedHint from './DelayedHint'

export default function StatusBar({ imageLoaded, imageMeta, realImageDims, inputImageDims, zoom, onResetZoom }) {
  // Each item: { text, hint, onClick? }
  const items = []

  if (imageLoaded) {
    if (imageMeta?.format) {
      items.push({ text: imageMeta.format, hint: 'Image file format' })
    }

    const inputChanged = inputImageDims &&
      (inputImageDims.w !== realImageDims.w || inputImageDims.h !== realImageDims.h)

    items.push({
      text: `${inputImageDims?.w ?? realImageDims.w} × ${inputImageDims?.h ?? realImageDims.h}`,
      hint: 'Input (original) image dimensions in pixels as loaded from the file',
    })

    if (inputChanged) {
      items.push({
        text: `${realImageDims.w} × ${realImageDims.h}`,
        hint: 'Current output dimensions in pixels after edits (crops/warps/trim)',
      })
    }

    const { dpiX = 0, dpiY = 0 } = imageMeta || {}
    if (dpiX > 0 && dpiY > 0) {
      const dpiText = Math.abs(dpiX - dpiY) < 0.5
        ? `${Math.round(dpiX)} DPI`
        : `${Math.round(dpiX)} × ${Math.round(dpiY)} DPI`
      items.push({ text: dpiText, hint: 'Print resolution stored in the image file (dots per inch)' })
    }

    items.push({ text: `${Math.round(zoom * 100)}%`, hint: 'Current zoom level — click to reset to 100%', onClick: onResetZoom })
  }

  return (
    <div className="status-bar">
      {items.map((item, i) => (
        <React.Fragment key={i}>
          {i > 0 && <span className="status-bar-sep" />}
          <DelayedHint hint={item.hint}>
            <span
              className={`status-bar-item${item.onClick ? ' status-bar-item--clickable' : ''}`}
              onClick={item.onClick}
            >
              {item.text}
            </span>
          </DelayedHint>
        </React.Fragment>
      ))}
    </div>
  )
}
