import React from 'react'

// ShortcutsPanel renders the collapsible keyboard-shortcuts reference at the
// bottom of the sidebar.
// Props:
//   shortcutsOpen / setShortcutsOpen
//   mode       — 'corner' | 'disc' | 'line'
//   discActive — bool (show disc-specific shortcuts only when a disc is live)
export default function ShortcutsPanel({ shortcutsOpen, setShortcutsOpen, mode, discActive }) {
  return (
    <div className="keyboard-shortcuts">
      <div
        className="shortcut-title"
        onClick={() => setShortcutsOpen((s) => !s)}
        style={{ cursor: 'pointer', userSelect: 'none' }}
      >
        Shortcuts <span className="shortcut-toggle">{shortcutsOpen ? '▾' : '▸'}</span>
      </div>

      {shortcutsOpen && (
        <>
          <div className="shortcut-item">
            <kbd>W</kbd><kbd>A</kbd><kbd>S</kbd><kbd>D</kbd> Crop edges
          </div>
          <div className="shortcut-item">
            <kbd>E</kbd><kbd>R</kbd> Rotate {mode === 'disc' ? '±15°' : '±90°'}
          </div>
          <div className="shortcut-item"><kbd>Tab</kbd> Undo</div>
          <div className="shortcut-item"><kbd>Q</kbd> Save</div>

          {mode === 'disc' && discActive && (
            <>
              <div className="shortcut-divider" />
              <div className="shortcut-item"><kbd>Y</kbd> Eyedrop background</div>
              <div className="shortcut-item">
                <kbd>←</kbd><kbd>↑</kbd><kbd>→</kbd><kbd>↓</kbd> Shift disc
              </div>
              <div className="shortcut-item"><kbd>Ctrl</kbd>+<kbd>Drag</kbd> Shift disc</div>
              <div className="shortcut-item"><kbd>Shift</kbd>+<kbd>Drag</kbd> Rotate disc</div>
              <div className="shortcut-item"><kbd>+</kbd>/<kbd>-</kbd> Feather radius</div>
              <div className="shortcut-item"><kbd>Ctrl</kbd>+<kbd>Scroll</kbd> Feather radius</div>
            </>
          )}
        </>
      )}
    </div>
  )
}
