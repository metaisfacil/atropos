import React from 'react'

// LinePanel renders the line-mode controls in the sidebar.
// Props:
//   linesDone — number of lines drawn so far (0–4)
export default function LinePanel({ linesDone }) {
  return (
    <div className="control-section">
      <div className="info-box">
        Click and drag to draw 4 lines defining the document edges. Perspective
        correction is applied automatically after the 4th line.
      </div>
      <div className="control-group">
        <div className="slider-row">
          <label style={{ margin: 0 }}>Lines Drawn</label>
          <span className="value-display">{linesDone}/4</span>
        </div>
      </div>
    </div>
  )
}
