# Atropos

![Atropos social preview](https://github.com/metaisfacil/atropos/blob/master/preview-2.png)

Atropos is a desktop image processing tool for perspective correction, circular cropping, and rectangular cropping. It is primarily intended for digitising musical materials such as CD, vinyl, and cassette inserts.

---

## Modes

### Corner mode

Automatically detects corners in the loaded image. Click four corners to apply a perspective correction warp. Detection parameters (sensitivity, corner count, etc.) are adjustable in the sidebar.

### Disc mode

Crop a circular region with a feathered edge — designed for vinyl records, coins, or similar subjects. Drag to place and size the circle, then refine with arrow keys, rotation, and feather controls.

### Line mode

Draw four lines along the edges of a document. Atropos infers the four corners from the line intersections and applies a perspective warp automatically.

### Normal mode

A standard rectangular crop. Drag to select a region and click **Crop** to apply. Additional crops can be stacked without resetting.

---

## Common operations

### Re-crop

Once a crop has been applied in any mode, the **Re-crop** button appears. Clicking it (after confirmation) promotes the current output image to the new source, resets all state, and returns to phase 1 — allowing you to chain crop modes (e.g. perspective-correct with Corner mode, then circular-crop the result with Disc mode) without saving an intermediate file.

### Adjustments

The Adjustments panel (collapsible, bottom of the sidebar) provides auto-contrast and black/white point sliders. These become available once a crop has been committed or skipped.

### Touch-up brush

A content-aware brush for removing dust, scratches, or other blemishes. Enable it in the Adjustments panel, set the brush size, and paint over areas to fill. Commits are individually undoable.

### Crop edges

WASD keys trim 3 px from the top, left, bottom, and right edges of the working image.

### Rotate

Q and E rotate 90° counter-clockwise and clockwise. In disc mode these instead rotate ±15°.

### Undo

Ctrl/⌘ + Z. Holds up to 10 snapshots.

### Save

Ctrl/⌘ + S or the **Save image** button. Output format is determined by the file extension you choose: PNG (default), JPEG, BMP, or TIFF.

---

## Keyboard reference

| Key | Action |
|---|---|
| W A S D | Crop top / left / bottom / right |
| Q | Rotate CCW 90° (or disc −15°) |
| E | Rotate CW 90° (or disc +15°) |
| Ctrl/⌘ + Z | Undo |
| Ctrl/⌘ + S | Save |
| Y | Eyedropper — set background colour (disc mode) |
| Arrow keys | Shift disc 5 px (20 px with Shift) |
| + / − | Feather radius ±1 (disc mode) |
| Ctrl+Drag | Live-shift disc centre |
| Shift+Drag | Live-rotate disc |
| Ctrl+Scroll | Feather radius (disc mode) |
| Scroll | Zoom in/out (cursor-anchored) |
| Space+Drag | Pan canvas |

---

## Prerequisites

| Dependency | Version | Notes |
|---|---|---|
| Go | 1.22+ | |
| Node.js | 20+ | |
| Wails CLI | v2.11+ | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| ImageMagick | 6 or 7 | Optional; needed only for TIFF files and exotic formats |

---

## Building

```bash
# Install Wails CLI and npm dependencies (first time only)
make setup

# Development mode with hot reload
make dev

# Production binary
make build
```

---

## CLI flags

```
atropos [--debug] [--corners | --disc | --lines | --normal] [image_path] [--post-save "command"] [--post-save-exit]
```

`--corners`, `--disc`, `--lines`, and `--normal` set the initial mode. Passing an image path loads it on startup. `--debug` logs debug output to stderr.

`--post-save "command"` instructs Atropos to launch the given command immediately after a successful save. The command string may include the placeholder `{path}` which will be replaced with the saved file path. The command is started detached — Atropos does not wait for it to finish. Example:

```
atropos --post-save "oxipng.exe {path}"
```

If you want Atropos to exit after launching the command, also pass `--post-save-exit`:

```
atropos --post-save "oxipng.exe {path}" --post-save-exit
```

This particular example would require oxipng.exe to be exposed through your PATH environment variable, but you may also specify an absolute path. Please also note that `--post-save` and `--post-save-exit` override the equivalent settings in the Options dialog.

---

## For AI agents

[AGENTS.md](AGENTS.md) contains a detailed reference covering the image state machine, data flow, operation ordering, and common pitfalls. Read it before making changes.
