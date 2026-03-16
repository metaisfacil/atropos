# Atropos

![Atropos social preview](https://github.com/metaisfacil/atropos/blob/master/preview-2.png)

Atropos is a desktop image processing tool for perspective correction and circular cropping. It is specifically intended for those digitizing musical materials such as CD, vinyl, and cassette inserts. The application is built with [Wails v2](https://wails.io), combining a Go backend for all pixel-level operations with a React frontend. All image processing is implemented in pure Go with no C dependencies; the only optional external dependency is ImageMagick, used as a decode fallback for TIFF files.

---

## Modes

The application operates in three mutually exclusive modes, selectable from the sidebar. Switching modes resets the active selection for the departing mode and fetches a clean preview from the backend.

### Corner mode

Corner mode performs perspective correction by warping a quadrilateral region of the image into a rectangle.

On image load, Shi–Tomasi corner detection runs automatically. Detection uses a downsampled working copy (longest edge capped at 1500 px) for speed, then maps detected coordinates back to full-resolution image space.

Detection is performed at multiple scales: the detector is applied to the working resolution and to coarser scales; candidate points are aggregated, deduplicated, and mapped back to full resolution. An optional percentile pre‑stretch (default 1%/99% luminance → full range), available in the Adjustments panel, remaps low/high luminance prior to detection to improve robustness on dark or uneven backgrounds.

Detection parameters exposed in the sidebar are:

- **Max Corners** — upper bound on the number of candidates returned
- **Quality Level** — fraction of the strongest corner response used as a threshold (1–100 maps to 0.01–1.0)
- **Min Distance** — minimum pixel separation between accepted corners, scaled to the working resolution
- **Accent** — additive brightness shift applied before detection to bring out faint edges

Before detection, a CLAHE (Contrast Limited Adaptive Histogram Equalization) pass is applied to the grayscale working image. CLAHE uses 8x8 tiles with bilinear blending at tile boundaries and a clip limit of 2.0.

Detected corners are rendered as red dots on the preview. Clicking a dot selects it (snaps to the nearest detected corner); selecting four triggers the perspective warp automatically. Custom corner placement mode bypasses snapping and uses the raw click coordinate. The dot radius is configurable and updates the overlay without re-running detection.

The perspective transform is computed via the Direct Linear Transform (DLT): an 8×2 linear system relating the source quadrilateral to the destination rectangle, solved by Gaussian elimination to produce a 3×3 homography matrix. Pixel sampling uses bilinear interpolation.

### Disc mode

Disc mode crops a circular region from the image with a feathered edge, designed for digitising vinyl records, coins, or similar circular subjects.

The user draws a circle by dragging on the image. The drag start defines the centre; the drag distance defines the radius. After commit, the following refinements are available:

- **Arrow keys** — shift the disc centre by 5 px (20 px with Shift held)
- **Ctrl+Drag** — live shift; the centre tracks the cursor delta in image space
- **Shift+Drag** — live rotation; horizontal drag distance maps to angle at 0.3 degrees/pixel
- **E / R keys** — rotate ±15 degrees
- **+ / - keys or Ctrl+Scroll** — adjust feather radius (0–100 px)
- **Y key** — eyedropper: samples the pixel under the cursor and sets it as the background fill colour for the feathered edge

The circular mask is applied to a sub-image cropped with the feather margin included. The feathered transition is computed per-pixel as a smoothstep based on distance from the disc boundary. After cropping and masking, any accumulated rotation is re-applied so that shift and feather-size adjustments do not discard the current rotation.

The background colour defaults to white and can be set via the eyedropper or `SetBackgroundColor`.

### Line mode

Line mode performs perspective correction by inferring the four corners of a document from four user-drawn lines (two pairs of roughly parallel edges).

The user drags four lines on the image. After the fourth line is committed, perspective correction is applied automatically. The algorithm:

1. Computes all pairwise intersections of the four lines (six in total)
2. Filters out intersections that fall more than 50% of the image dimension outside the image bounds
3. If more than four valid intersections remain, selects the four with the greatest distance from the centroid
4. Orders the four points as TL/TR/BR/BL using the sum/difference heuristic
5. Derives output dimensions from the max of opposite edge lengths
6. Applies the same DLT perspective transform used by corner mode

---

## Adjustments

The Adjustments panel (collapsible, at the bottom of the sidebar) provides tonal controls that operate independently of the three main modes.

### Auto Contrast

Scans all opaque pixels for the minimum and maximum luminance using ITU-R BT.601 integer weights `(19595R + 38470G + 7471B) >> 16`, then stretches all channels linearly so that the minimum maps to 0 and the maximum maps to 255. This matches the behaviour of Photoshop's Image > Auto Contrast.

### Black Point / White Point sliders

Apply an explicit linear stretch: each channel value `v` is mapped to `clamp((v - black) * 255 / (white - black), 0, 255)`. The preview updates on mouse release rather than on every tick to avoid flooding the backend.

Both operations are non-destructive in the sense that they operate against a snapshot (`levelsBaseImage`) taken at the start of a slider session. The snapshot is cleared whenever any committing operation (Crop, Rotate, AutoContrast, DrawDisc, etc.) calls `saveUndo`, so the next slider drag always starts from the post-operation state rather than re-stretching an already-stretched image.

In corner mode (before any warp), these adjustments write to `currentImage` and re-render the corner overlay so detected dots remain visible.

---

## Common operations

### Crop

WASD keys crop 3 pixels from the top, left, bottom, and right edges respectively. Crops are cumulative and push an undo snapshot before each operation.

### Rotate

E and R rotate the working image 90 degrees counter-clockwise and clockwise respectively. In disc mode these instead rotate ±15 degrees via the arbitrary-angle rotator.

### Undo

Tab reverts the last committing operation. The undo stack holds up to 10 snapshots. Level slider adjustments intentionally do not push undo entries to avoid flooding the stack during a drag session.

### Save

Q or the Save Image button opens a file picker. The output format is determined by extension: PNG (default), JPEG (95% quality), BMP, or TIFF. The working image (`warpedImage`) is written at full resolution. The preview shown in the UI is a JPEG-encoded downscale capped at 1600 px on the longest edge.

---

## Image format support

Input formats are handled in the following priority order:

1. For `.tif`/`.tiff`, ImageMagick (`magick convert` on IM7+, `convert` on IM6) is tried first, converting to BMP3 in a pipe for minimal overhead.
2. Go's standard library decoders handle PNG, JPEG, BMP, GIF, WebP.
3. If both fail, ImageMagick is tried as a final fallback for any format.

Output supports PNG, JPEG, BMP, and TIFF via Go's standard encoders plus `golang.org/x/image`.

All internal processing uses `*image.NRGBA` (non-premultiplied RGBA). The `toNRGBA` conversion function has fast paths for `*image.NRGBA` (straight copy) and `*image.RGBA` (parallel un-premultiply), with a generic fallback for other types. Un-premultiplication and full-image processing are parallelised across available CPU cores.

---

## Keyboard reference

| Key | Action |
|---|---|
| W A S D | Crop top / left / bottom / right |
| E | Rotate CCW 90° (or disc -15°) |
| R | Rotate CW 90° (or disc +15°) |
| Tab | Undo |
| Q | Save |
| Y | Eyedropper (disc mode) |
| Arrow keys | Shift disc 5 px (20 px with Shift) |
| + / - | Feather radius +1 / -1 (disc mode) |
| Ctrl+Drag | Live shift disc centre |
| Shift+Drag | Live rotate disc |
| Ctrl+Scroll | Feather radius (disc mode) |
| Scroll | Zoom in/out (cursor-anchored) |

---

## Zoom

Scroll wheel zooms the canvas between 0.1× and 5×. Zoom is cursor-anchored: the pixel under the cursor remains stationary. The zoom state is stored in React and the scroll position is applied synchronously in a `useLayoutEffect` so it takes effect before the browser paints the resized image.

---

## Prerequisites

| Dependency | Version | Notes |
|---|---|---|
| Go | 1.22+ | |
| Node.js | 20+ | |
| Wails CLI | v2.11+ | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| ImageMagick | 6 or 7 | Optional; needed only for TIFF decode fast path and exotic formats |

Linux also requires GTK3 and WebKit2GTK development headers (see CI workflow for the exact apt packages).

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

`make build` computes a version string from the last Git commit (`YYYYMMDD-xxxxxx`) and passes it to the Go linker via `-ldflags "-X main.AppVersion=..."`. Running `wails build` or `go run` directly without ldflags produces a binary that reports `Atropos dev`.

To build for a specific platform from CI or manually:

```bash
# Windows
wails build -o atropos.exe -ldflags "-X main.AppVersion=$(git log -1 --format=%cd-%h --date=format:%Y%m%d --abbrev=6)"

# Linux (WebKit 4.1)
wails build -tags webkit2_41 -ldflags "..."

# macOS (universal binary configured in wails.json)
wails build -ldflags "..."
```

---

## CLI flags

```
atropos [--debug] [--corners | --disc | --lines] [image_path]
```

`--corners`, `--disc`, and `--lines` set the initial mode. An image path as a positional argument loads the file on startup and, in corner mode, runs detection immediately. These are used by OS file associations and shell integration.

Pass `--debug` on the command line to enable a timestamped debug log written to `debug/YYYYMMDD_HHMMSS.txt` in the working directory. The log file is held open for the lifetime of the process. The frontend can write into the same log via `LogFrontend(msg)`, which is used by the zoom/scroll subsystem to record detailed wheel and layout events.
