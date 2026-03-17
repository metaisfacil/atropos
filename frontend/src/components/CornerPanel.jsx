import React from 'react'

// CornerPanel renders the corner-detection mode controls in the sidebar.
// Props:
//   state / setState      — detection params (maxCorners, qualityLevel, minDistance, accent, cornerCount)
//   dotRadius             — current dot radius
//   setDotRadius          — setter
//   customCorner          — bool
//   setCustomCorner       — setter
//   cornersDetected       — bool (detection has been run)
//   imageLoaded           — bool
//   setPreview            — update the canvas preview
export default function CornerPanel({
  state, setState,
  dotRadius, setDotRadius,
  customCorner, setCustomCorner,
}) {
  return (
    <div className="control-section">
      <div className="control-group">
        <label>Max Corners</label>
        <div className="slider-row">
          <input
            type="range"
            min="1"
            max="1000"
            value={state.maxCorners}
            onChange={(e) => setState({ ...state, maxCorners: parseInt(e.target.value) })}
          />
          <span className="value-display">{state.maxCorners}</span>
        </div>
      </div>

      <div className="control-group">
        <label>Quality Level</label>
        <div className="slider-row">
          <input
            type="range"
            min="1"
            max="100"
            value={state.qualityLevel}
            onChange={(e) => setState({ ...state, qualityLevel: parseFloat(e.target.value) })}
          />
          <span className="value-display">{state.qualityLevel}</span>
        </div>
      </div>

      <div className="control-group">
        <label>Min Distance</label>
        <div className="slider-row">
          <input
            type="range"
            min="1"
            max="200"
            value={state.minDistance}
            onChange={(e) => setState({ ...state, minDistance: parseInt(e.target.value) })}
          />
          <span className="value-display">{state.minDistance}</span>
        </div>
      </div>

      <div className="control-group">
        <label>Accent</label>
        <div className="slider-row">
          <input
            type="range"
            min="0"
            max="30"
            value={state.accent}
            onChange={(e) => setState({ ...state, accent: parseInt(e.target.value) })}
          />
          <span className="value-display">{state.accent}</span>
        </div>
      </div>

      <div className="control-group">
        <label>Corner Dot Size</label>
        <div className="slider-row">
          <input
            type="range"
            min="2"
            max="80"
            value={dotRadius}
            onChange={(e) => setDotRadius(parseInt(e.target.value))}
          />
          <span className="value-display">{dotRadius}</span>
        </div>
      </div>

      <div className="control-group" style={{ marginTop: 40 }}>
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={customCorner}
            onChange={(e) => setCustomCorner(e.target.checked)}
          />
          Custom corner placement
        </label>
        <div className="hint">
          When enabled, click anywhere to place a corner instead of snapping to detected corners.
        </div>
      </div>
    </div>
  )
}
