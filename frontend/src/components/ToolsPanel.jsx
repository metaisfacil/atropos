import React from 'react'

// ToolsPanel renders a collapsible "Tools" section in the sidebar.
// It hosts standalone utility features that aren't part of the main
// crop/adjust pipeline — currently the Image Compositor.
//
// Props:
//   toolsOpen / setToolsOpen  — expand/collapse state
//   onOpenCompositor          — called when the Compositor button is clicked
export default function ToolsPanel({ toolsOpen, setToolsOpen, onOpenCompositor }) {
  return (
    <div className={`keyboard-shortcuts ${toolsOpen ? 'expanded' : ''}`}>
      <div
        className="shortcut-title"
        onClick={() => setToolsOpen(s => !s)}
        style={{ cursor: 'pointer', userSelect: 'none' }}
      >
        Tools <span className="shortcut-toggle">{toolsOpen ? '▾' : '▸'}</span>
      </div>

      <div className={`keyboard-shortcuts-content ${toolsOpen ? 'open' : 'closed'}`}>
        <div className="tools-panel-body">
          <button className="tools-panel-btn" onClick={onOpenCompositor}>
            Image Compositor
          </button>
          <p className="tools-panel-hint">
            Stitch multiple overlapping scan segments into a single continuous image.
          </p>
        </div>
      </div>
    </div>
  )
}
