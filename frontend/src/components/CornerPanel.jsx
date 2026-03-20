import React from 'react'
import DelayedHint from './DelayedHint'

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
  disabled,
}) {
  return (
    <div className="control-section">
      <div className="control-group">
        <label>Max Corners</label>
        <DelayedHint hint="Maximum number of detected corners to return; higher may find more candidates.">
          <div className="slider-row">
            <input
              type="range"
              min="1"
              max="1000"
              value={state.maxCorners}
              disabled={disabled}
              onChange={(e) => setState({ ...state, maxCorners: parseInt(e.target.value) })}
            />
            <span className="value-display">{state.maxCorners}</span>
          </div>
        </DelayedHint>
      </div>

      <div className="control-group">
        <label>Quality Level</label>
        <DelayedHint hint="Quality threshold for corner detection; higher = fewer, stronger corners.">
          <div className="slider-row">
            <input
              type="range"
              min="1"
              max="100"
              value={state.qualityLevel}
              disabled={disabled}
              onChange={(e) => setState({ ...state, qualityLevel: parseFloat(e.target.value) })}
            />
            <span className="value-display">{state.qualityLevel}</span>
          </div>
        </DelayedHint>
      </div>

      <div className="control-group">
        <label>Min Distance</label>
        <DelayedHint hint="Minimum allowed distance (pixels) between detected corners.">
          <div className="slider-row">
            <input
              type="range"
              min="1"
              max="200"
              value={state.minDistance}
              disabled={disabled}
              onChange={(e) => setState({ ...state, minDistance: parseInt(e.target.value) })}
            />
            <span className="value-display">{state.minDistance}</span>
          </div>
        </DelayedHint>
      </div>

      <div className="control-group">
        <label>Accent</label>
        <DelayedHint hint="Pre-detection accent/boost applied to improve faint edge visibility.">
          <div className="slider-row">
            <input
              type="range"
              min="0"
              max="30"
              value={state.accent}
              disabled={disabled}
              onChange={(e) => setState({ ...state, accent: parseInt(e.target.value) })}
            />
            <span className="value-display">{state.accent}</span>
          </div>
        </DelayedHint>
      </div>

      <div className="control-group">
        <label>Corner Dot Size</label>
        <DelayedHint hint="Adjust the displayed corner dot radius in the preview.">
          <div className="slider-row">
            <input
              type="range"
              min="2"
              max="80"
              value={dotRadius}
              disabled={disabled}
              onChange={(e) => setDotRadius(parseInt(e.target.value))}
            />
            <span className="value-display">{dotRadius}</span>
          </div>
        </DelayedHint>
      </div>

      <div className="control-group" style={{ marginTop: 20 }}>
        <label className="checkbox-label">
          <input
            type="checkbox"
            checked={customCorner}
            disabled={disabled}
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
