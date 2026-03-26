# Atropos — Working with Claude

This file explains how to work on Atropos. **Read AGENTS.md and ARCHITECTURE.md first** — this file assumes you are already familiar with them.

## Before You Start

- **AGENTS.md** — read this for rules, pitfalls, file map, and the image state model
- **ARCHITECTURE.md** — read this for operation sequences, data flow, and detailed field semantics when needed
- Use `git log --oneline` and `git blame <file>` for context on why patterns exist

---

## Making Code Changes

### When Touching Image State or Modes

- Before editing `app_*.go`, consult AGENTS.md: "Critical Image State" and "Non-Negotiable Rules"
- Every operation must respect which field it reads/writes (see ARCHITECTURE.md's "Critical Image State Machine")
- Modes are independent; mode-switch logic must test all four transitions (see AGENTS.md: "Mode Model")
- Undo is always required: call `saveUndo()` *before* the change (exception: `SetLevels` deliberately does not). See AGENTS.md: "Non-Negotiable Rules"
- Reset/load paths must clear all state mentioned in AGENTS.md: "Non-Negotiable Rules" (touch-up, `cornerEntryRef`, mode-specific fields)

### Frontend Changes

When adding a new UI control:

1. Put state in the appropriate hook (`useImageActions`, `useMouseHandlers`, `useTouchup`, etc.)
2. Reset state in: `loadFile()`, `handleSkipCrop()`, `handleModeSwitch()`, and relevant reset handlers
3. Use `<ImageOverlays>` for visual feedback (don't use `offsetLeft/offsetTop` — coordinates are in image space)
4. For disc-related changes, verify `computeDiscShift()` is used (maps screen delta → image delta → applies rotation)

---

## Testing

**Before opening a PR:**
- Test mode transitions: corner → disc → line → normal, and back. Verify overlays clear and undo works across transitions.
- Test undo after: crop, rotate, levels, touch-up, mode switch, reset
- Load a new image while in preview mode in each mode; verify no stale state remains
- Run Vitest locally; do not skip failing tests

**Use Vitest for:**
- Mode transitions and state cleanup
- Undo behavior (especially after mode switches)
- Reset/load paths
- Frontend hook state isolation

**Integration tests** should hit the Go API via the Wails bridge, not mock it.

---

## PRs and Commits

**Commit messages:** In active voice, reference the rule or section affected (e.g., `Fix undo in corner mode (preserve cornerEntryRef on recrop)`)

**PR checklist:**
- [ ] If you changed mode-switch logic, list which modes you tested
- [ ] If you touched image state, note which fields changed
- [ ] If you added a Go method, verify the bridge was regenerated
- [ ] Run tests locally; do not skip failures
- [ ] Do not hand-edit `frontend/wailsjs/go/main/App.js` or `models.ts` — regenerate via Wails

---

## Documentation Updates

- **AGENTS.md:** Add a new pitfall if you discover a non-obvious failure mode
- **ARCHITECTURE.md:** Update operation sequences or field semantics if they change
- **CLAUDE.md:** Update if working patterns or testing approach change

---

## Common Workflows

### Adding a new image operation

1. Choose which image to read (see ARCHITECTURE.md: "Critical Image State Machine")
2. Call `saveUndo()` *before* modifying
3. Write to `setWorkingImage()`
4. Call `SetPreview()` to update the frontend

### Changing mode-switch logic

1. Consult AGENTS.md: "Mode Model" and check `app_mode.go`
2. Test all four mode transitions (corner↔disc, corner↔line, corner↔normal, disc↔line, etc.)
3. Verify undo works across mode boundaries

### Adding a new UI control

1. Put state in the appropriate hook
2. Add reset logic to: `loadFile()`, `handleSkipCrop()`, `handleModeSwitch()`, and relevant reset handlers
3. Use `<ImageOverlays>` for visual feedback if needed
4. Test the full cycle: load → trigger control → switch modes → switch images → undo

---

## Debugging

| Symptom | Check |
|---------|-------|
| Image state is stale | Are you calling `workingImage()` or reading fields directly? |
| Overlay doesn't reset | Is the state cleared in all reset paths? (`loadFile`, `handleSkipCrop`, `handleModeSwitch`, etc.) |
| Undo doesn't work | Did you call `saveUndo()` *before* the change? |
| Bridge call is undefined | Did you regenerate the bridge after adding a Go method? |
| Preview flickers on load | Did you null `cornerEntryRef` and `discBaseImage` in the load path? |

---

## References

- AGENTS.md — rules, pitfalls, file map, image state model
- ARCHITECTURE.md — operation sequences, field semantics, startup flow
- `git log --oneline -p <file>` — similar changes in history
