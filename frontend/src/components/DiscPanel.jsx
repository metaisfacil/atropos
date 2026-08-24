import React, { useRef } from 'react'
import DelayedHint from './DelayedHint'
import { SetFeatherRadius, SetFeatherSize, SetDiscSettings } from '../../wailsjs/go/main/App'

const DISC_RADIUS_ADJUSTMENT_LIMIT = 300

// DiscPanel renders the disc-mode controls in the sidebar.
// Props:
//   discActive            — bool (a disc has been drawn)
//   discRadius            — current fixed outer radius
//   setDiscRadius         — setter
//   featherSize           — current inward feather width
//   setFeatherSize        — setter
//   discCenterCutout      — bool (cutout enabled via Options)
//   discCutoutPercent     — current cutout diameter percentage
//   setDiscCutoutPercent  — setter
//   setPreview            — update the canvas preview
//   setRealImageDims      — update the logical canvas to the rendered crop
export default function DiscPanel({
  discActive,
  discRadius,
  setDiscRadius,
  featherSize,
  setFeatherSize,
  discCenterCutout,
  discCutoutPercent,
  setDiscCutoutPercent,
  setPreview,
  setRealImageDims,
  setDiscNoMaskPreview,
  disabled,
}) {
  // Capture the radius selected by the initial drag. It remains the neutral
  // midpoint even as discRadius changes, and is cleared when the crop resets.
  const initialRadiusRef = useRef(0)
  if (!discActive) initialRadiusRef.current = 0
  if (discActive && initialRadiusRef.current === 0 && discRadius > 0) {
    initialRadiusRef.current = discRadius
  }
  const initialRadius = initialRadiusRef.current
  const radiusMin = Math.max(1, initialRadius - DISC_RADIUS_ADJUSTMENT_LIMIT)
  const radiusMax = Math.max(radiusMin, initialRadius + DISC_RADIUS_ADJUSTMENT_LIMIT)

  const commitDiscRadius = async (radius) => {
    try {
      const result = await SetFeatherRadius({ radius })
      if (result?.preview) setPreview(result.preview)
      if (result?.width && result?.height) {
        setRealImageDims({ w: result.width, h: result.height })
      }
      if (result?.unmaskedPreview) setDiscNoMaskPreview(result.unmaskedPreview)
      if (result?.discRadius !== undefined) setDiscRadius(result.discRadius)
    } catch (err) {
      console.error(err)
    }
  }

  return (
    <div className="control-section">
      <div className="info-box">
        Click and drag on the image to draw a circle around the disc.
      </div>
      {discActive && (
        <>
          <div className="control-group">
            <label>Disc Radius</label>
            <DelayedHint hint="Outer radius of the disc crop. The initially drawn radius is the slider midpoint and can be adjusted by up to 300 pixels in either direction.">
              <div className="slider-row">
                <input
                  aria-label="Disc Radius"
                  type="range"
                  min={radiusMin}
                  max={radiusMax}
                  value={discRadius}
                  disabled={disabled}
                  onChange={(e) => setDiscRadius(parseInt(e.target.value))}
                  onPointerUp={(e) => commitDiscRadius(parseInt(e.currentTarget.value))}
                  onKeyUp={(e) => {
                    if (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End', 'PageUp', 'PageDown'].includes(e.key)) {
                      commitDiscRadius(parseInt(e.currentTarget.value))
                    }
                  }}
                />
                <span className="value-display">{discRadius}</span>
              </div>
            </DelayedHint>
          </div>
          <div className="control-group">
            <label>Feather Size</label>
            <DelayedHint hint="Width of the feather drawn inward from the disc edge; larger values produce a softer transition without changing the crop size.">
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
                      if (result?.width && result?.height) {
                        setRealImageDims({ w: result.width, h: result.height })
                      }
                    } catch (err) {
                      console.error(err)
                    }
                  }}
                />
                <span className="value-display">{featherSize}</span>
              </div>
            </DelayedHint>
          </div>
          {discCenterCutout && (
            <div className="control-group">
              <label>Cutout %</label>
              <DelayedHint hint="Diameter of the centre cutout as a percentage of the disc diameter.">
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
              </DelayedHint>
            </div>
          )}
        </>
      )}
    </div>
  )
}
