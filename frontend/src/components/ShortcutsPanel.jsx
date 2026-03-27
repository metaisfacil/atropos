import React from 'react'

// ShortcutsPanel renders the collapsible accordion-panel reference at the
// bottom of the sidebar.
// Props:
//   shortcutsOpen / setShortcutsOpen
//   mode        — 'corner' | 'disc' | 'line'
//   discActive  — bool (show disc-specific shortcuts only when a disc is live)
//   canSave     — bool (a crop result exists; gates crop/rotate/undo/save shortcuts)
//   imageLoaded — bool (an image is loaded; gates pan shortcut)
export default function ShortcutsPanel({ shortcutsOpen, setShortcutsOpen, mode, discActive, canSave, imageLoaded }) {
  const cls = (active) => `shortcut-item${active ? '' : ' shortcut-item--disabled'}`

  return (
    <div className={`accordion-panel ${shortcutsOpen ? 'expanded' : ''}`}>
      <div
        className="accordion-title"
        onClick={() => setShortcutsOpen((s) => !s)}
        style={{ cursor: 'pointer', userSelect: 'none' }}
      >
        Shortcuts <span className="accordion-toggle">{shortcutsOpen ? '▾' : '▸'}</span>
      </div>

      <div className="accordion-content-outer">
        <div className={`accordion-content ${shortcutsOpen ? 'open' : 'closed'}`}>
          <div className={cls(canSave)}>
            <div className="keys"><kbd>W</kbd><kbd>A</kbd><kbd>S</kbd><kbd>D</kbd></div>
            <div className="caption">Crop edges</div>
          </div>
          <div className={cls(canSave)}>
            <div className="keys"><kbd>Q</kbd><kbd>E</kbd></div>
            <div className="caption">Rotate {mode === 'disc' ? '±15°' : '±90°'}</div>
          </div>
          <div className={cls(canSave)}><div className="keys"><kbd>Ctrl</kbd>/<kbd>⌘</kbd>+<kbd>Z</kbd></div><div className="caption">Undo</div></div>
          <div className="shortcut-item"><div className="keys"><kbd>Ctrl</kbd>/<kbd>⌘</kbd>+<kbd>O</kbd></div><div className="caption">Load</div></div>
          <div className={cls(canSave)}><div className="keys"><kbd>Ctrl</kbd>/<kbd>⌘</kbd>+<kbd>S</kbd></div><div className="caption">Save</div></div>
          <div className="shortcut-item"><div className="keys"><kbd>Ctrl</kbd>/<kbd>⌘</kbd>+<kbd>W</kbd></div><div className="caption">Quit</div></div>
          <div className={cls(imageLoaded)}><div className="keys"><kbd>Space</kbd>+<kbd>Drag</kbd></div><div className="caption">Pan canvas</div></div>

          {mode === 'disc' && discActive && (
            <>
              <div className="shortcut-divider" />
              <div className="shortcut-item"><div className="keys"><kbd>Y</kbd></div><div className="caption">Eyedrop background</div></div>
              <div className="shortcut-item"><div className="keys"><kbd>←</kbd><kbd>↑</kbd><kbd>→</kbd><kbd>↓</kbd></div><div className="caption">Shift disc</div></div>
              <div className="shortcut-item"><div className="keys"><kbd>Ctrl</kbd>+<kbd>Drag</kbd></div><div className="caption">Shift disc</div></div>
              <div className="shortcut-item"><div className="keys"><kbd>Shift</kbd>+<kbd>Drag</kbd></div><div className="caption">Rotate disc</div></div>
              <div className="shortcut-item"><div className="keys"><kbd>+</kbd>/<kbd>-</kbd></div><div className="caption">Feather radius</div></div>
              <div className="shortcut-item"><div className="keys"><kbd>Ctrl</kbd>+<kbd>Scroll</kbd></div><div className="caption">Feather radius</div></div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
