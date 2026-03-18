import React from 'react'

// NormalCropPanel renders the normal-crop mode controls in the sidebar.
// Props:
//   normalRect — {x1,y1,x2,y2} in image coords, or null if no selection
export default function NormalCropPanel({ normalRect }) {
  // TODO: Make visible in status bar?
  // const w = normalRect ? Math.abs(normalRect.x2 - normalRect.x1) : 0
  // const h = normalRect ? Math.abs(normalRect.y2 - normalRect.y1) : 0

  return (
    <div className="control-section">
      <div className="info-box">
        Drag on the image to draw a crop rectangle, then click Crop to apply.
      </div>
      {/* <div className="control-group">
        <div className="slider-row">
          <label style={{ margin: 0 }}>Selection</label>
          <span className="value-display">
            {normalRect ? `${w}×${h}` : '—'}
          </span>
        </div>
      </div> */}
    </div>
  )
}
