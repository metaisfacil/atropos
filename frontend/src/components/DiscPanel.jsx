import React from 'react'
import { SetFeatherSize, SetDiscSettings } from '../../wailsjs/go/main/App'

// DiscPanel renders the disc-mode controls in the sidebar.
// Props:
//   discActive            — bool (a disc has been drawn)
//   featherSize           — current feather radius
//   setFeatherSize        — setter
//   discCenterCutout      — bool (cutout enabled via Options)
//   discCutoutPercent     — current cutout diameter percentage
//   setDiscCutoutPercent  — setter
//   setPreview            — update the canvas preview
export default function DiscPanel({
  discActive,
  featherSize,
  setFeatherSize,
  discCenterCutout,
  discCutoutPercent,
  setDiscCutoutPercent,
  setPreview,
  disabled,
}) {
  return (
    <div className="control-section">
      <div className="info-box">
        Click and drag on the image to draw a circle around the disc.
      </div>
      {discActive && (
        <>
          <div className="control-group">
            <label>Feather Size</label>
            <div className="slider-row">
              <input
                type="range"
                min="0"
                max="100"
                value={featherSize}
                disabled={disabled}
                onChange={(e) => setFeatherSize(parseInt(e.target.value))}
                onMouseUp={async (e) => {
                  try {
                    const result = await SetFeatherSize({ size: parseInt(e.target.value) })
                    if (result?.preview) setPreview(result.preview)
                  } catch (err) {
                    console.error(err)
                  }
                }}
              />
              <span className="value-display">{featherSize}</span>
            </div>
          </div>
          {discCenterCutout && (
            <div className="control-group">
              <label>Cutout %</label>
              <div className="slider-row">
                <input
                  type="range"
                  min="0"
                  max="50"
                  value={discCutoutPercent}
                  disabled={disabled}
                  onChange={(e) => setDiscCutoutPercent(parseInt(e.target.value))}
                  onMouseUp={async (e) => {
                    try {
                      const result = await SetDiscSettings({
                        centerCutout: discCenterCutout,
                        cutoutPercent: parseInt(e.target.value),
                      })
                      if (result?.preview) setPreview(result.preview)
                    } catch (err) {
                      console.error(err)
                    }
                  }}
                />
                <span className="value-display">{discCutoutPercent}%</span>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
