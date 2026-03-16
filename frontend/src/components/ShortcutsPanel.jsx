import React from 'react'

// ShortcutsPanel renders the collapsible keyboard-shortcuts reference at the
// bottom of the sidebar.
// Props:
//   shortcutsOpen / setShortcutsOpen
//   mode       — 'corner' | 'disc' | 'line'
//   discActive — bool (show disc-specific shortcuts only when a disc is live)
export default function ShortcutsPanel({ shortcutsOpen, setShortcutsOpen, mode, discActive }) {
  return (
    <div className={`keyboard-shortcuts ${shortcutsOpen ? 'expanded' : ''}`}>
      <div
        className="shortcut-title"
        onClick={() => setShortcutsOpen((s) => !s)}
        style={{ cursor: 'pointer', userSelect: 'none' }}
      >
        Shortcuts <span className="shortcut-toggle">{shortcutsOpen ? '▾' : '▸'}</span>
      </div>

      <div className={`keyboard-shortcuts-content ${shortcutsOpen ? 'open' : 'closed'}`}>
          <div className="shortcut-item">
            <div className="keys"><kbd>W</kbd><kbd>A</kbd><kbd>S</kbd><kbd>D</kbd></div>
            <div className="caption">Crop edges</div>
          </div>
          <div className="shortcut-item">
            <div className="keys"><kbd>E</kbd><kbd>R</kbd></div>
            <div className="caption">Rotate {mode === 'disc' ? '±15°' : '±90°'}</div>
          </div>
          <div className="shortcut-item"><div className="keys"><kbd>Tab</kbd></div><div className="caption">Undo</div></div>
          <div className="shortcut-item"><div className="keys"><kbd>Q</kbd></div><div className="caption">Save</div></div>

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
  )
}
