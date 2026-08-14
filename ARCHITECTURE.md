# Atropos — Architecture Reference

This document contains the detailed system model, data flow, and operation ordering for Atropos. It is intended to be read after `AGENTS.md`.

---

## Frontend File Map

`App.jsx` is a thin coordination layer: state declarations, hook wiring, and JSX render. Most logic lives under `frontend/src/`.

| File | Responsibility |
|------|----------------|
| `hooks/useImageActions.js` | Go API action handlers: `loadFile`, `loadImageFromBytes`, `handleLoadImage`, `handleDetectCorners`, `handleSkipCrop`, `handleRecrop`, `handleResetCorners/Disc/Normal`, `handleNormalCrop`, `handleClearLines`, `handleSaveImage`, `flushPendingSave`, `handleModeSwitch`, `handleCompositorLoad`, `handleUndo`. Also owns the `OnFileDrop` / paste / URL-drop / launch-args effects, close-request event flow, corner-detect generation guards (`detectGenRef`), cached corner entry (`cornerEntryRef`), and deferred save/drop queues. |
| `hooks/useMouseHandlers.js` | All pointer interaction: `handleMouseDown/Move/Up/ImageMouseLeave` across all modes. Owns refs for corner click guards, disc drag state, line endpoint editing, and Normal-mode draw/move/resize (`normalDragPendingRef`, `normalMoveDragRef`, `normalHandleDragRef`). Uses shared `computeDiscShift` mapping from `utils/imageCoords` for consistent disc translation math across live preview and backend commit. Registers `window` `mouseup` and `mousemove` listeners to safely finish drags outside the canvas area. Returns `displayToImage` and `lineStartImgRef` for stable image-space coordinates during zoom changes mid-drag. |
| `hooks/useKeyboardShortcuts.js` | Single `keydown` effect: arrow keys (disc shift), `+`/`-` (feather), `Y` (eyedrop), `Ctrl+Z` (undo), `Ctrl+S` (save), `Ctrl+O` (load), `Ctrl+W`/`Cmd+W` (quit), `Enter` (apply Normal crop), and `WASDQE` (crop/rotate). In corner mode, `Ctrl+Z` first calls `UndoLastCorner` for in-progress corner picks (1–3) before using backend image undo. WASDQE are guarded by `canSave`; if no crop result exists, `showStatus` is shown instead of forwarding to Go. |
| `hooks/useTouchup.js` | Touch-up brush state machine: `touchupStrokes`, `brushSize`, `commitTouchup`, window mouseup effect, `EventsOn("touchup-done")` effect. |
| `hooks/useZoomPan.js` | Viewport camera state: `zoom`, `fitWidth`, `spacePanMode`, `canvasRef`, wheel zoom/feather handler, space-key pan, `ResizeObserver`, and scroll anchoring. `imgRef` points at the transparent logical image surface, so cursor anchoring and the existing pointer state machine use the same geometry as the canvas renderer without making image pixels a DOM `<img>`. |
| `hooks/usePersistentSettings.js` | File-backed settings (`touchupBackend`, `iopaintURL`, `warpFillMode`, `warpFillColor`, `discCenterCutout`, `discCutoutPercent`, `autoCornerParams`, `closeAfterSave`, `postSaveEnabled`, `postSaveCommand`, `touchupRemainsActive`, `straightEdgeRemainsActive`, `autoDetectOnModeSwitch`). Loads from Go (`GetAllSettings`) on mount and persists via `SaveAllSettings` on every change. Performs a one-time migration from `localStorage` on first launch of the file-backed version. |
| `hooks/useStatusMessage.js` | `imageInfo` + fade timer logic (`showStatus`). |
| `components/PreviewCanvas.jsx` | Visible viewport renderer. Requests only the visible/overscanned image-space region at the device density needed for the current zoom, receives that small JPEG through the Wails RPC bridge as a data URL, caches recent rasters, composites touch-up patches, and draws all visible image-space guides. |
| `components/ImageOverlays.jsx` | Transparent DOM hit targets for editable Normal/Line handles. Visible guides are drawn by `PreviewCanvas`; this component exists so the mature pointer state machine can keep DOM hit testing. |
| `components/StatusBar.jsx` | Bottom status bar. Shows file format, pixel dimensions, DPI when known, and zoom level. Zoom is clickable and resets to 100%. All fields use `DelayedHint`. |
| `components/DelayedHint.jsx` | Portal-rendered tooltip that appears after a 1 s hover delay. Uses a two-pass `useLayoutEffect` to clamp the tooltip inside the viewport before making it visible, avoiding flicker and edge clipping. |
| `components/*Panel.jsx` | Mode-specific sidebar controls. `AdjustmentsPanel` owns resize, trim borders, auto-contrast, levels, descreen, dust removal, touch-up brush, and disc straight-edge controls. `ShortcutsPanel` accepts `canSave` and `imageLoaded` props and applies `.shortcut-item--disabled` to unavailable shortcuts. `ToolsPanel` is a collapsible sidebar panel between Adjustments and Shortcuts that houses the Image Compositor. |
| `components/ResizeModal.jsx` | Modal for width/height or percentage resize with optional aspect lock and warning confirmation for large upscales. |
| `components/CompositorModal.jsx` | Modal for image stitching. Manages an ordered list of image paths and an orientation selector, calls `CompositorStitch`, shows a preview, and exposes a “Load output” button that calls `CompositorLoadResult` and triggers `handleCompositorLoad`. |

`ctrlDragRef` and `shiftDragRef` are defined in `App.jsx` rather than moved to `useMouseHandlers` because `PreviewCanvas` reads their live values for disc-guide rendering while the mouse handlers write them during drag interactions.

---

## Critical Image State Machine

This is the single most important concept in the codebase. Every operation must be understood in terms of which image field it reads and writes.

```text
originalImage  ── immutable after LoadImage; never modified
      │
      └── cloned to ──► currentImage  ── pre-warp working image
                                │         adjustments (levels, auto-contrast)
                                │         write here if warpedImage is nil
                                │
                 ┌──────────────┬───────────────────┬─────────────────┐
                 │              │                   │                 │
         CornerPanel      LinePanel            DiscPanel        NormalCropPanel
         warpFromCorners  ProcessLines         DrawDisc         NormalCrop
                 │              │                   │                 │
                 └──────────────┴───────────────────┴─────────────────► warpedImage
                                                          │
                                                   All subsequent ops:
                                                   Crop / Rotate / Undo
                                                   Levels / AutoContrast / Descreen
                                                   DustRemoval
                                                   TrimBorders / ResizeImage
                                                   TouchUpApply
                                                   SaveImage ◄────────────
```

### Fields (defined in `app.go` App struct)

| Field | Type | Meaning |
|-------|------|---------|
| `originalImage` | `*image.NRGBA` | Full-resolution decoded source. Never modified after `LoadImage`. |
| `currentImage` | `*image.NRGBA` | Working image before any warp/disc operation. Pre-warp levels adjustments write here. |
| `warpedImage` | `*image.NRGBA` | The current "committed" result. `SaveImage` reads **only** this field. If nil, `workingImage()` falls back to `currentImage`. |
| `levelsBaseImage` | `*image.NRGBA` | Snapshot taken on the **first** slider drag after any committing operation. All subsequent slider ticks apply levels to this base, preventing value stacking. Cleared by `saveUndo()`. |
| `descreenBaseImage` | `*image.NRGBA` | Snapshot captured at the start of a Descreen session. Parameter tweaks in the same session always re-apply to this base image, not the already-descreened output. |
| `descreenResultImage` | `*image.NRGBA` | Pointer to the last Descreen output. Used to detect external working-image changes (for example SetLevels) so Descreen re-snapshots on next apply instead of reusing stale base state. |
| `discBaseImage` | `*image.NRGBA` | Snapshot of `currentImage` captured at `DrawDisc` time. `redrawDisc()` **always** sources from this image, not `warpedImage`, so every re-render of the disc is deterministic. |
| `discNoMaskPreview` | `string` | Versioned preview asset URL for the disc source without mask compositing. Used by frontend live drag/rotate preview so interactions stay fluid while backend commits asynchronously. |
| `discWorkingCrop` | `*image.NRGBA` | Pre-cropped sub-region of `discBaseImage` centred on the disc with a generous extra margin (`discWorkingCropShiftPadding = 500 px`). `redrawDisc` reads from this small image instead of the full `discBaseImage` to avoid cache thrashing on large images. Refreshed on `DrawDisc` and when a shift moves the disc outside the cached region. Cleared by `ResetDisc`. |
| `discWorkingCropRect` | `image.Rectangle` | Records the rect of `discBaseImage` that `discWorkingCrop` covers (in `discBaseImage` coordinates). Used to detect when a shift has moved the disc outside the working crop. |
| `discCenterCutout` | `bool` | When true, `redrawDisc` punches a circular hole at the disc centre to expose `bgColor`. Default: `true`. |
| `discCutoutPercent` | `int` | Diameter of the centre cutout as a percentage of the disc diameter (1–50). Cutout radius = `discRadius * discCutoutPercent / 100`. Default: `11`. |
| `undoStack` | `[]undoEntry` | LIFO stack capped at `undoLimit` (10). Each entry stores image pixels plus optional rotation metadata (`rotationAngle`), pre-warp flag (`preWarp`), and optional in-progress corner picks (`selectedCorners`) for undoing back into corner-selection phase. |

### `workingImage()` (in `app_adjust.go`)

```go
func (a *App) workingImage() *image.NRGBA {
    if a.warpedImage != nil {
        return a.warpedImage
    }
    return a.currentImage
}
```

**Every read operation uses this.** Never read `currentImage` or `warpedImage` directly unless you specifically need one of them.

### `setWorkingImage(img)` (in `app_adjust.go`)

Always writes to `warpedImage`. This ensures `SaveImage` always has a result, even if the user runs levels/contrast before cropping.

### `saveUndo()` (in `app_adjust.go`)

1. If `undoStack` is full, shift out the oldest entry.
2. Push an `undoEntry` with cloned working image and `preWarp` metadata (true when `warpedImage == nil`).
3. **Clears `levelsBaseImage`, `descreenBaseImage`, and `descreenResultImage`** so the next levels/descreen session snapshots fresh baselines.

### `saveDiscRotationUndo()` (in `app_adjust.go`)

Same as `saveUndo()` but also snapshots the current `rotationAngle` into the entry. Used by `StraightEdgeRotate` so that Undo restores both the image pixels and the accumulated rotation angle, keeping disc re-renders consistent.

**Rule:** Every operation that commits a permanent change must call `saveUndo()` first — except `SetLevels` and intra-session `Descreen` parameter changes (to avoid flooding undo entries during live parameter tuning). This includes warp-entry operations (`ClickCorner` on the 4th click, `ProcessLines`, `DrawDisc`) and most post-crop adjustments. `StraightEdgeRotate` uses `saveDiscRotationUndo()` instead.

---

## Startup Sequence

```
Wails runtime
    └── NewApp()             set all defaults (see below)
    └── App.startup(ctx)     store context
    └── Wails injects JS bridge
    └── React mounts
    └── useEffect (mount)
        ├── GetAllSettings()   // load shared settings file; migrate localStorage on first run
        └── GetLaunchArgs()
            ├── if filePath → loadFile(filePath, autoDetect)
            └── else        → showStatus('No image loaded')
```

**NewApp() defaults:**

| Field | Default |
|-------|---------|
| `undoLimit` | 10 |
| `featherSize` | 15 |
| `cropAmount` | 3 |
| `bgColor` | white (255,255,255,255) |
| `postDiscWhite` | 255 |
| `touchupBackend` | `"patchmatch"` |
| `iopaintURL` | `"http://127.0.0.1:8086/"` |
| `warpFillMode` | `"clamp"` |
| `warpFillColor` | white |
| `discCenterCutout` | `true` |
| `discCutoutPercent` | 11 |

**Settings persistence:** Settings are stored in `%AppData%\atropos\settings.json` (Windows) / `~/.config/atropos/settings.json` (other platforms) as JSON, written by the Go backend via `SaveAllSettings`. The file is the source of truth and is shared across all simultaneously running instances. Go applies backend-relevant fields to its in-memory state inside `SaveAllSettings`; it never reads `localStorage`. On first launch after upgrading from an older version, `usePersistentSettings` detects that the file does not yet exist (`Initialized=false`) and performs a one-time migration from `localStorage`.

---

## Image Loading (`app_io.go`)

```
LoadImage(req)
    1. Acquire loadMu mutex (reject concurrent loads)
    2. Decode file:
         TIFF  → try ImageMagick first, fall back to Go decoder
         Other → Go stdlib decoder, fall back to ImageMagick for exotic formats
    3. toNRGBA(src)           convert to NRGBA (RGBA un-premultiply is parallelized)
    4. originalImage = result (no extra allocation — reuse toNRGBA output)
       currentImage  = cloneImage(originalImage)
    5. Clear ALL transient state:
         warpedImage = nil
         levelsBaseImage = nil
         descreenBaseImage = nil
         descreenResultImage = nil
         selectedCorners = nil
         detectedCorners = nil
         lines = nil
         undoStack = nil
         discCenter = zero
         discRadius = 0
         rotationAngle = 0
         discBaseImage = nil
         discNoMaskPreview = ""
         discWorkingCrop = nil
         discWorkingCropRect = zero
         postDiscBlack = 0
         postDiscWhite = 255
    6. imagePreviewURL(currentImage)   register an immutable preview revision whose source can be rasterized on demand
    7. Update window title
    8. extractFileMeta(filePath)      read format name + DPI from file header
    9. Return ImageInfo{Width, Height, Preview, Format, DPIX, DPIY, SuggestedCornerParams{MinDistance, MaxCorners}}
```

`LoadImageBytes(req)` follows the same pipeline and state reset, but decodes from raw bytes (clipboard or browser URL drop) instead of a filesystem path.

**Frontend `loadFile(filePath, autoDetect)` / `loadImageFromBytes(...)` flow:**

```
setLoading(true)
setZoom(1)
LoadImage(...) or LoadImageBytes(...)
    → setPreview, setImageLoaded, setRealImageDims
    → setImageMeta({ format, dpiX, dpiY })
    → reset ALL mode-specific frontend state (cornerCount, linesDone, discActive,
            touchupStrokes, useTouchupTool, useStraightEdgeTool, detected/selected corner overlays,
            lastDetectSettings, cornerEntryRef, etc.)
    → suggestedCornerParamsRef.current = result.suggestedCornerParams
setLoadingFull(false)                    ← hides opaque loading overlay before detect runs
if autoDetect && mode === 'corner':
    DetectCorners(autoCornerParams ? suggestedCornerParamsRef.current overrides : {})
    → setPreview, setCornersDetected(true)
    → setCornerState updated with overridden maxCorners/minDistance values
    → lastDetectSettings.current = detection params used
setLoading(false)
```

**File dialog defaults:** `OpenImageDialog` and `OpenSaveDialog` derive their default directory from the last loaded file path. Before passing the directory to Wails, they check `os.Stat(dir)` — if the directory no longer exists (e.g. a removable drive), they fall back to an empty string (system default) rather than erroring.

---

## Corner Mode (`app_corner.go`)

### Corner Detection

```
DetectCorners(req)
    1. Downsample to ~1500px, apply accent/CLAHE contrast prep
    2. Multi-scale Shi-Tomasi (goodFeaturesToTrack) at scales [1, 2, 4]
    3. Deduplicate corners, map back to full-resolution coordinates
    4. Store in detectedCorners
    5. Return clean currentImage preview + Corners array
         Dots are rendered by the frontend canvas overlay — never baked into the image
```

### Clicking Corners

```
ClickCorner(req)
    1. If not custom && detectedCorners exist:
           snap pt to nearest detected corner
       Else (custom=true OR Ctrl+click):
           use raw click coordinate
    2. Append pt to selectedCorners
    3. If selectedCorners.length < 4:
           return SnappedX/SnappedY/Count/Message only — NO preview
    4. On 4th corner → saveUndo() → store first 3 selected corners into newest undo entry → warpFromCorners(selectedCorners[:4]):
           sortVertices (→ TL, TR, BL, BR)
           compute outW = max(widthTop, widthBot)
           compute outH = max(heightLeft, heightRight)
           if warpFillMode == "clamp":
               perspectiveTransform(currentImage, src, dst, outW, outH)
           else:
               perspectiveTransformWithMask(currentImage, src, dst, outW, outH)
               applyWarpFill(warped, oobMask)
           warpedImage = result
    5. selectedCorners = nil
    6. Return preview + Width + Height + "Perspective corrected to W×H"
```

### Undoing in-progress corner picks

`UndoLastCorner()` pops one point from `selectedCorners` without touching `undoStack`. Frontend keyboard handling uses this for `Ctrl+Z` when corner count is 1–3, so users can step back corner picks before the 4th-click warp commit.

**Ctrl+click** always uses the raw click coordinate regardless of the `customCorner` checkbox state.

### Reset / Restore

```
ResetCorners()
    selectedCorners = nil
    warpedImage = nil          ← CRITICAL: so GetCleanPreview returns currentImage
    return clean currentImage preview + Corners (detectedCorners preserved)

RestoreCornerOverlay({dotRadius})
    if detectedCorners empty → error "no cached corners"
    return clean currentImage preview + Corners + "Detected N corners — click 4 corners"

SkipCrop()
    require currentImage != nil
    warpedImage     = cloneImage(currentImage)
    selectedCorners = nil
    return dims + "Crop skipped — image ready to save"
```

`SkipCrop` deliberately does not publish a preview revision: its committed
pixels are identical to the preview already on screen, so replacing the asset
would only trigger a redundant low/full decode and redraw.

`SkipCrop` is available in all four modes. The frontend sets mode-specific state to transition past phase 1 without performing a warp:

| Mode | Frontend state change after SkipCrop |
|------|--------------------------------------|
| Corner | `cornerCount = 4`, clears `detectedCornerPts` |
| Disc | `discActive = true` |
| Line | `linesProcessed = true` |
| Normal | `normalCropApplied = true` |

`cropSkipped = true` disables all 1st-phase controls until Reset. `detectedCorners` is NOT cleared on mode switch (only on `LoadImage`).

### RecropImage (`app_io.go`)

```
RecropImage()
    require warpedImage != nil
    originalImage = warpedImage
    currentImage  = cloneImage(warpedImage)
    reset all transient state identically to LoadImage
    return ImageInfo{Width, Height, Preview}
```

Promotes the current output image to a new source for a second crop pass. The frontend shows a `ConfirmationModal` before calling this because it is irreversible within the session.

---

## Line Mode (`app_line.go`)

```
AddLine(req)
    append {X1,Y1,X2,Y2} to lines
    return "Lines: N/4"

ProcessLines()
    require len(lines) == 4
    1. Compute all 6 pairwise line intersections
    2. Filter to intersections within ±50% of image bounds
    3. If > 4 valid: pick 4 farthest from centroid
    4. orderPoints → TL, TR, BR, BL
    5. Compute outW, outH from max edge lengths
    6. if warpFillMode == "clamp":
           perspectiveTransform(originalImage, src, dst, outW, outH)
       else:
           perspectiveTransformWithMask(originalImage, src, dst, outW, outH)
           applyWarpFill(warped, oobMask)
    7. warpedImage = result
    8. lines = nil
    9. Return preview

ClearLines()
    lines = nil
    warpedImage = nil
    return currentImage preview
```

**Note:** Line mode warps from `originalImage`, not `currentImage`. Corner mode warps from `currentImage`.

---

## Disc Mode (`app_disc.go` / `app_adjust.go`)

Disc mode is the most stateful mode. Every re-render replays the full pipeline from `discBaseImage`.

### DrawDisc — Entry Point

```
DrawDisc(req)
    saveUndo()
    discCenter    = req.centerX, req.centerY
    discRadius    = req.radius
    rotationAngle = 0
    discBaseImage = cloneImage(currentImage)
    refreshDiscWorkingCrop()
    cache discNoMaskPreview
    postDiscBlack = 0
    postDiscWhite = 255
    redrawDisc()
```

### redrawDisc — The Full Disc Pipeline

```
redrawDisc()
    1. src = discBaseImage (or originalImage as emergency fallback)
    2. bbox = [discCenter ± (discRadius + featherSize)]
    3. cropped = subImage(src, bbox)
    4. localCenter = discCenter − bbox.Min
    5. applyCircularMaskWithFeather(cropped, localCenter, discRadius, featherSize, bgColor)
         for each pixel: distance d to center
           d <= radius:              alpha = 1.0 (opaque)
           d >= radius+featherSize:  alpha = 0.0 (transparent, filled with bgColor)
           in between:               cosine interpolation
    6. if rotationAngle != 0:
           rotateArbitrary(feathered, rotationAngle, bgColor)
    7. if postDiscBlack != 0 OR postDiscWhite != 255:
           applyLevels(feathered, postDiscBlack, postDiscWhite)
    8. warpedImage = result
    9. levelsBaseImage = nil
    10. Return preview
```

### Operations that trigger redrawDisc

- `RotateDisc(angle)` — adds angle to rotationAngle, calls redrawDisc
- `ShiftDisc(dx, dy)` — adjusts discCenter, calls redrawDisc
- `SetFeatherSize(size)` — updates featherSize, calls redrawDisc if discRadius > 0
- `GetPixelColor(x, y)` — sets bgColor from discBaseImage pixel, calls redrawDisc
- `SetLevels(...)` — stores values in postDiscBlack/White, calls redrawDisc
- `AutoContrast()` — computes + stores values in postDiscBlack/White, calls redrawDisc

### ResetDisc

```
ResetDisc()
    discCenter    = zero
    discRadius    = 0
    rotationAngle = 0
    discBaseImage = nil
    postDiscBlack = 0
    postDiscWhite = 255
    warpedImage   = nil
    levelsBaseImage = nil
    return currentImage preview
```

---

## Normal Mode (`app_normal.go`)

Normal mode is the simplest mode: the user drags a rectangle on the image and clicks **Crop** to apply it.

**Drag interaction rules (all enforced in `useMouseHandlers`):**
- A drag may begin on the canvas area outside image bounds; the selection starts when the cursor first enters the image.
- While dragging, `dragCurrent` is clamped to the image boundary.
- A click (drag smaller than 5 px in either dimension) clears any existing `normalRect`.
- `normalDragPendingRef` tracks the outside-image mousedown state; `e.preventDefault()` suppresses the browser's native drag gesture.
- `normalDragActiveRef` is set `true` synchronously when the pending drag transitions to active so subsequent `mousemove` events bypass the `if (!dragging) return` guard before React re-renders.
- `mouseUpHandledRef` prevents the `window` `mouseup` listener from double-committing the same gesture.

### NormalCrop

```
NormalCrop(req)
    img = workingImage()
    normalise coordinates (swap if x1>x2 or y1>y2)
    clamp to img.Bounds()
    if region is empty → error
    saveUndo()
    warpedImage = subImage(img, rect)
    return preview + width + height + "Cropped to W×H"
```

`NormalCrop` calls `workingImage()`, so it works on `currentImage` or `warpedImage` transparently. Each call commits a new undo entry.

### ResetNormal

```
ResetNormal()
    warpedImage = nil
    return currentImage preview + dims + "Normal crop reset"
```

### Frontend state

| State | Meaning |
|-------|---------|
| `normalRect` | `{x1,y1,x2,y2}` image-space selection, or `null` |
| `normalCropApplied` | `true` after first crop or Skip crop — unlocks touch-up |

`normalRect` is purely frontend; never sent to Go until the user clicks **Crop**.

---

## Adjustments (`app_adjust.go`)

### Crop / Rotate / Resize / TrimBorders

```
Crop(req)          require warpedImage; saveUndo(); crop rect; return preview
Rotate(req)        require warpedImage; saveUndo(); rotate90(flipCode 0=CCW,1=CW,2=180); return preview
ResizeImage(req)   require image loaded; saveUndo(); resize workingImage; setWorkingImage(result)
TrimBorders(req)   require warpedImage; saveUndo(); adjust crop offsets; return preview
```

### Descreen (session-based committing adjustment)

```
Descreen(req)
    if descreenBaseImage == nil OR workingImage() != descreenResultImage:
        saveUndo()                    ← start a new descreen session
        descreenBaseImage = clone(workingImage())

    filtered = applyDescreen(descreenBaseImage, thresh, radius, middle, highlight)
    setWorkingImage(filtered)
    descreenResultImage = filtered
    return preview
```

Descreen is session-based: parameter changes in the same session re-apply to `descreenBaseImage` (non-stacking). If another operation changes the working image, pointer mismatch (`workingImage() != descreenResultImage`) starts a fresh session automatically.

### SetLevels (non-committing)

```
SetLevels(req)
    On first call after a commit: levelsBaseImage = clone of workingImage()

    if preWarp (warpedImage == nil):
        apply levels to levelsBaseImage → write to currentImage
    else if discRadius > 0:
        postDiscBlack = req.black; postDiscWhite = req.white
        redrawDisc()
    else:
        apply levels to levelsBaseImage → warpedImage = result

    NOTE: saveUndo() is NOT called here
```

### AutoContrast (committing)

```
AutoContrast()
    base = levelsBaseImage ?? workingImage()
    preLevelsBase = clone(base)
    saveUndo()              ← commits; clears levels/descreen baselines

    (blackPt, whitePt) = computeAutoContrastPoints(base)
    result = applyLevels(base, blackPt, whitePt)

    if preWarp:    currentImage = result; levelsBaseImage = preLevelsBase
    if disc:       postDiscBlack/White = values; levelsBaseImage = preLevelsBase; redrawDisc()
    else:          warpedImage = result; levelsBaseImage = preLevelsBase

    return preview + black/white values
```

### Undo

```
Undo()
    if undoStack empty → "Nothing to undo"
    entry = pop from undoStack
    if entry.preWarp:
        currentImage = entry.image
        warpedImage = nil
        selectedCorners = entry.selectedCorners (if any)
        clear disc state and adjustment baselines
    else:
        warpedImage = entry.image
        if entry.rotationAngle != nil: rotationAngle = *entry.rotationAngle
    return preview
```

Undo is blocked in the frontend while any drag operation is active (disc shift, rotation, etc.) to prevent undo from firing mid-drag and corrupting disc state.

### Dust Removal (`app_dust.go`, `imgproc_dust.go`)

`DustRemoval({level, dpi})` is exposed in the Adjustments accordion after crop/skip.
It reads `workingImage()`, applies a calibration profile, calls `saveUndo()`
only when pixels actually change, and writes the result back to the matching
pre-warp or post-warp image field. Missing DPI metadata defaults to 300 DPI.

```text
8-bit NRGBA -> BT.601 luminance -> 3x3 Sobel magnitude
    -> normalize by the image-wide maximum and threshold at 0.09
    -> 3x3 dilation at <=100 DPI, otherwise 5x5
    -> 8-connected component shape/area classification
    -> intersect with original Sobel seeds -> fill enclosed holes
    -> in-place 3x3 target / 5x5 unmasked-sample median repair
```

The Low/Medium/High dense/sparse/elongated area banks at the 400-DPI
reflective baseline are respectively `160/280/400`, `240/420/600`, and
`320/600/900`. Below 400 DPI they scale by `dpi/400`; above 400 DPI detection
is performed on a nearest-neighbour 400-DPI proxy and the mask is scaled back
to the working image.

---

## Touch-Up (`app_touchup.go`, `app_iopaint.go`, `patchmatch.go`)

### Availability

Touch-up is disabled until phase 1 is complete (crop committed or Skip crop):

| Mode | Enabled when |
|------|--------------|
| Corner | `cornerState.cornerCount === 4` |
| Line | `linesProcessed === true` |
| Disc | `discActive === true` |
| Normal | `normalCropApplied === true` |

`useTouchupTool` is reset to `false` on mode switch and on image load. After a successful commit it is also reset unless `touchupRemainsActive` is `true` (default).

### Touch-up cancellation

An in-flight `TouchUpApply` can be aborted via:
- **Internal** (`cancelTouchup()`): called at the top of every reset and load handler in Go.
- **External** (`CancelTouchup()`): Wails-bound, called fire-and-forget from the frontend before any reset IPC call.

`cancelTouchup()` is called at the top of: `ResetCorners`, `ResetDisc`, `ClearLines`, `ResetNormal`, `LoadImage`, `RecropImage`.

### TouchUpApply (async, non-blocking)

```
TouchUpApply(maskB64, patchSize, iterations) → ProcessResult{Message:"running"}, nil
    cancelTouchup()
    ctx, cancel = context.WithCancel(Background)
    mask = buildMask(maskB64)         ← synchronous

    go func():
        defer: cancel(); touchupCancel = nil
        if touchupBackend == "iopaint":
            out, err = iopaintFill(ctx, workingImage, mask)
        else:
            out, err = patchMatchChunkedFill(ctx, workingImage, mask, patchSize, iterations)

        if cancelled  → EventsEmit("touchup-done", {cancelled:true})
        if error      → EventsEmit("touchup-done", {error})
        saveUndo(); setWorkingImage(out)
        encode only the nonzero mask bounds as a transparent PNG replacement
        EventsEmit("touchup-done", {patch, message, width, height, descreenReset})
```

Touch-up does not register a new full-frame preview revision. The frontend
positions the returned patch in full-image coordinates above the current base
image and keeps it there across that revision's low-to-full promotion. Rapid
touch-ups therefore add only their changed rectangles; neither base asset is
re-encoded or swapped. The busy indicator remains active until the patch
element fires `onLoad`. Any later non-touch-up preview revision contains the
committed pixels and clears the temporary patch stack.

### PatchMatch (`patchmatch.go`, `patchcost.go`, `patchreconstruct.go`, `patchstructure.go`, `patchtexture.go`; used via `patchMatchChunkedFill`)

The built-in touch-up backend is a deterministic, translation-only, coarse-to-fine PatchMatch synthesizer tuned for small repairs on scanned print material. The implementation is deliberately ROI-first: a brush stroke is solved inside a bounded working neighbourhood rather than preprocessing the full scan, while the source search domain inside that neighbourhood remains much larger than the painted region. 

**Public solver entry points:**

```go
PatchMatchFill(ctx, src, mask, patchSize, iterations)
    // Compatibility API. Discovers mask bounds if necessary and returns a
    // full-size image.

PatchMatchFillBounds(ctx, src, mask, dirtyBounds, patchSize, iterations)
    // Same full-size result, but accepts the already-known brush bounds and
    // avoids scanning the full mask to rediscover them.

PatchMatchFillROI(ctx, src, mask, dirtyBounds, patchSize, iterations)
    // Lowest-latency API. Returns a zero-origin local image plus workBounds;
    // the caller may composite that ROI directly into a document/tile buffer.
```

`dirtyBounds` is expressed relative to the top-left of `src` and must contain every non-zero mask pixel. `PatchMatchFillROI` computes a working rectangle from the painted bounds plus the random-search radius, patch support, and descriptor halo. The nominal search radius is `max(48, brushSpan*6 + patchSize*2)`; the working ROI keeps additional search and filter padding so random search can move away from the target without forcing full-document analysis. All expensive pyramids, packed planes, validity maps, structure descriptors, texture fields, NNF state, and reconstruction buffers are built in this local coordinate system.

Within each level, the **active NNF rectangle is smaller still**: only patch centres whose patches can overlap a painted output pixel are solved. It is approximately the mask bounds expanded by `patchRadius + 1`. Source candidates may come from anywhere in the much larger working ROI.

**Pyramid and mask semantics**

`buildPatchPyramid` keeps image appearance, target coverage, and source exclusion separate:

* The image pyramid uses a separable `[1 4 6 4 1]` binomial low-pass in premultiplied-alpha space before each approximately 2× reduction. This avoids aliasing halftone dots, fine type, line art, scanner grain, and other print texture into misleading coarse structures.
* `targetMasks` preserve fractional/antialiased coverage by area averaging. They control confidence and the final soft compositing edge.
* `sourceMasks` are conservative binary masks: if any represented fine pixel is painted, the coarse source pixel is excluded.
* A source centre is valid only when the **entire search/vote patch** avoids the source-exclusion mask. There is no centre-only validity fallback, so damaged content cannot re-enter the fill merely because a coarse patch centre lies outside the brush.

Patch size is normalized to an odd value in the range 3–15. The normal touch-up setting is 7×7. The pyramid has at most seven levels and stops when its shortest side reaches `max(32, patchSize*4)`. If a level has no legal source patches or no active target centres, that level is skipped rather than weakening source validity.

**Per-level solve and EM loop**

```text
local source + target/source masks
        |
        v
build image/mask pyramid
        |
        v
for level = coarse -> fine
        |
        +-- prepare strict source validity + active target centres
        +-- seed working image from source / bilinear parent result
        +-- at finest level only: build structure + texture models
        |
        +-- EM round (maximum 3 on the first solved level, 2 thereafter)
        |      |
        |      +-- update target confidence
        |      +-- initialize/reuse NNF
        |      +-- alternating PatchMatch propagation + random search
        |      +-- structure-aware patch vote
        |      +-- coherent texture-detail restoration
        |      +-- soft-compose result through target mask
        |
        +-- carry working image + NNF to next finer level
```

The first/coarsest working image is simply the local source clone. Covered pixels are harmless at this point because their round-zero confidence is zero; the original dust or damage therefore cannot contribute to target SSD. Finer levels bilinearly upsample the previous reconstruction only inside the target mask, leaving known source pixels untouched.

Known pixels always keep their true confidence from the antialiased target mask. Reconstructed pixels acquire only provisional confidence in later EM rounds. That confidence is attenuated by distance into the hole, local texture strength, and structural-edge strength. This prevents a smooth or blurred first reconstruction from becoming authoritative evidence and self-validating on the next E-step.

The NNF and cost buffers are allocated once per level and reused across EM rounds. On a same-level EM round, the previous NNF is retained and only its costs are recomputed against the updated working image.

**NNF initialization and PatchMatch search**

The NNF stores an absolute source centre, but coarse-to-fine seeding explicitly upsamples the **displacement** `source - target`, not the absolute source coordinate. This preserves a constant translation field across odd/even child pixels and avoids the one-pixel phase/checkerboard error caused by scaling absolute source coordinates.

Initialization is deterministic:

1. An unpainted target centre whose full patch is legal maps to itself.
2. Otherwise, use the displacement-preserving parent NNF seed when available.
3. Otherwise, search outward in deterministic local rings for a legal source.
4. For unusually large holes with no nearby legal centre, choose a deterministic entry from the legal source list to bootstrap random search.

There is no full-ROI nearest-source/Voronoi preprocessing in the active path.

Each requested PatchMatch pass contains two different execution modes:

* **Propagation is classic in-place directional PatchMatch.** Even passes scan top-left → bottom-right and test transported left/up matches; odd passes scan bottom-right → top-left and test right/down. The sweep is intentionally sequential so a good displacement can cascade through a coherent region in a single pass.
* **Random search is row-parallel.** Each target centre samples successively smaller windows around its current winner. The PRNG is a deterministic coordinate/pass hash, so parallel scheduling does not change the result.

`iterations` is a maximum, not guaranteed work. At least one forward and one reverse pass are performed; after that, the level stops when fewer than about 0.4% of active centres improve. Random-search radius is adaptive: an uncertain first pass keeps the broad search, while seeded later passes and EM rounds start from progressively smaller radii.

**Patch cost (`patchcost.go`)**

The appearance term uses the same raw source appearance that reconstruction will render. Pixels are packed as premultiplied RGBA structure-of-arrays planes; alpha is deliberately downweighted relative to RGB. The hot patch SSD is confidence-weighted and dispatched to the retained AVX2/FMA or NEON assembly kernel when available, with a scalar fallback. The kernel receives an early-exit limit derived from the current best candidate so obviously worse matches can stop before finishing the patch.

The complete candidate cost is:

```text
confidence-normalized premultiplied RGBA SSD
    + weak locality prior where target evidence is missing
    + fine-texture energy mismatch penalty
    + low-frequency structure mismatch penalty
```

There is deliberately no mean subtraction and no search-only gain/bias model: search and reconstruction must agree about what source appearance will actually be copied. The locality prior falls away as a target patch gains observed or reconstructed evidence.

**Fine-level structure model (`patchstructure.go`)**

Low-frequency structure is computed only at native resolution; coarse levels are responsible for large displacement, not exact colour-edge placement.

`pmStructureGuideImage` builds a guide through the painted region from observed pixels only. Mask-boundary colours seed the hole, onion-peel propagation provides a stable initial value throughout it, and bounded relaxation continues the surrounding low-frequency colour field while known pixels remain fixed constraints.

The source image and guide image are then low-passed with repeated separable 5-tap binomial filtering and converted to a compact three-plane structure field:

```text
strength = low-frequency edge magnitude mapped to 0..1
orientX  = (Jxx - Jyy) / (Jxx + Jyy)
orientY  = 2*Jxy / (Jxx + Jyy)
```

`orientX/orientY` are coherence-weighted double-angle edge orientation, so an undirected edge has the same representation in either tangent direction. The PatchMatch structure penalty samples the centre, axial points, and diagonals; it penalizes both missing/extra structure and orientation disagreement. Expensive tensor normalization is performed once during descriptor construction, not per candidate.

**Fine-level texture model (`patchtexture.go`)**

Texture is modeled independently of provisional reconstructed RGB. The source texture field is local RMS gradient energy computed only from legal, unpainted source pixels. Integral images make the neighbourhood energy query cheap. It responds to scanner noise, plastic or paper speckle, fibres, and halftone microtexture while staying low on genuinely smooth colour fields.

A scalar texture guide is propagated from known pixels through the brush region and harmonically relaxed. The E-step compares source texture energy with this guide, so ordinary patch averaging cannot make a smooth first-pass result self-validate and progressively erase native grain.

**Reconstruction (`patchreconstruct.go`)**

The M-step deliberately treats flat appearance, structural edges, and stochastic detail differently.

First, `pmNNFCoherenceWeights` scores each NNF centre by local agreement of its translation with the four neighbouring centres, allowing ±1 px drift. Ordinary overlapping-patch voting uses the **same patch support as search** and weights each contribution by:

```text
Gaussian spatial patch weight
    * NNF coherence
    * amount of target evidence in the matched patch
    * inverse match cost
```

This weighted average is retained in flat and texture regions, where combining plausible patches gives stable low-frequency colour. At a structural edge, however, averaging equally sharp but one-pixel-misaligned source edges would manufacture blur. Reconstruction therefore hashes overlapping votes into **exact displacement clusters**. Nearby ±1 px buckets may support selection of the dominant family, but the rendered structural sample comes from one exact winning displacement. Strong, well-supported structure progressively switches from the ordinary average to that direct source sample, preserving colour-edge position and sharpness.

Texture detail is restored afterward from a separate coherent displacement field stored only over the painted rectangle. Overlapping NNF hypotheses are clustered by displacement, the dominant texture warp is smoothed only across compatible colour/structure neighbourhoods, and the high-frequency source residual is transferred onto the voted low-frequency result. Flat stochastic areas use a linear low-pass residual decomposition so the full texture phase is available; near colour edges the base becomes edge-aware. Texture mixing falls continuously with structure strength and is completely disabled at very strong structural pixels, so the detail pass cannot re-soften an edge that the structure-aware vote just preserved.

Finally, only target-mask pixels are replaced. Antialiased/partial mask coverage soft-composites the synthesized result with the original source, while known pixels remain byte-for-byte source content.

**Performance and cancellation invariants**

The performance model is intentionally local:

* all expensive processing is performed on the working ROI, not the full scan;
* the active NNF exists only around patch centres capable of affecting the painted output;
* NNF/cost/coherence storage is reused within a level;
* texture-warp storage is mask-bounds-sized rather than working-image-sized;
* structure/texture relaxation updates only covered pixels and does not copy known ROI pixels every iteration;
* row-parallel work has a size threshold, so tiny dabs stay serial instead of paying goroutine/`WaitGroup` overhead;
* independent initialization, descriptor passes, random search, voting, and reconstruction are parallelized where useful, while directional propagation remains sequential for correctness.

`PatchMatchFillBounds` should be preferred when the caller already knows the stroke bounds; `PatchMatchFillROI` is the preferred integration point when the editor can composite the returned rectangle itself, because it also avoids the final full-document clone. `ctx.Err()` is checked at entry and throughout pyramid/search/reconstruction work so an in-flight touch-up can be cancelled.

The normal scanned-print setting is `patchSize=7`, `iterations=4`; iterations are a maximum because stable levels terminate early. Regression coverage includes displacement-preserving pyramid seeding, strict whole-patch source validity, separate target/source mask semantics, local working-ROI behavior, supplied-bounds equivalence, basic defect completion, stochastic texture retention, texture beside a crossing edge, and sharp slanted colour-edge preservation. Architecture-specific tests additionally verify AVX2/NEON SSD equivalence, early-exit behavior, dispatch, and the `pmKernelArgs` assembly layout. 

### iopaintFill (`app_iopaint.go`)

**Does not send the full image.** Crops to the bounding box of the mask plus a 128 px margin, encodes the crop as JPEG (fast; iopaint does not need lossless input), sends it with the cropped grayscale PNG mask to `{iopaintURL}/api/v1/inpaint`. On success, composites only the masked pixels from the response patch back onto a full clone of the source image and returns a full-size `*image.NRGBA`.

**Outpaint (warp fill) always uses PatchMatch** — IOPaint is an inpainting model and produces black for outpainting.

---

## Mode Switching

### The Four Modes Are Mutually Exclusive

Switching modes always resets the warp result. `setMode(m)` is called at the **top** of `handleModeSwitch` — before any async operations — so the mode button updates immediately.

### Mode Switch Handler (`useImageActions.js:handleModeSwitch`)

```
leaving 'corner'  → ResetCorners(); reset cornerCount, cornersDetected, cropSkipped
leaving 'disc'    → ResetDisc() if discActive; reset discActive, cropSkipped
leaving 'line'    → ClearLines(); reset linesDone, lines, linesProcessed, cropSkipped
leaving 'normal'  → ResetNormal(); reset normalRect, normalCropApplied, cropSkipped

arriving at 'corner' && lastDetectSettings matches current settings:
    RestoreCornerOverlay() → reuse cached corners; setFitWidth directly; early return

arriving at 'corner' && autoDetectOnModeSwitch:
    DetectCorners() → same as manual Detect; early return

otherwise:
    setFitWidth(min(container.w, container.h * w/h))  ← compute directly, do not zero
    GetCleanPreview()
    setPreview, setRealImageDims
```

### GetCleanPreview (`app_mode.go`)

```
GetCleanPreview()
    selectedCorners = nil      ← clear in-progress selection
    detectedCorners preserved  ← RestoreCornerOverlay still works later
    img = workingImage()       ← currentImage (warpedImage now nil after reset)
    return preview + dims + "Ready"
```

### Cached Corner Restoration

When switching back to corner mode, if all of `{maxCorners, qualityLevel, minDistance, accent, useStretch}` match `lastDetectSettings.current`, `RestoreCornerOverlay` is called instead of re-detecting. `lastDetectSettings.current` is set after every successful `DetectCorners` call and cleared by `resetImageState` (used by `loadFile`, `loadImageFromBytes`, and compositor-load promotion).

---

## Image Processing Kernels (`imgproc.go`)

### applyWarpFill (`app_corner.go`)

```
applyWarpFill(img, oobMask)
    if no OOB pixels → return img unchanged (fast path)
    if warpFillMode == "outpaint":
        PatchMatchFill(img, oobMask, patchSize=9, iterations=5)
        return out
    // warpFillMode == "fill":
    for each OOB pixel: img.SetNRGBA(x, y, warpFillColor)
    return img
```

### Preview asset transport

```
imagePreviewURL(img)
    register the immutable *image.NRGBA pointer as a numbered revision
    return /__atropos/preview/{session}/{revision}.jpg as the opaque revision identity

RenderPreviewViewport({ preview, x, y, width, height, destWidth, destHeight, quality })
    validate the opaque revision identity
    validate and clamp the requested source rectangle
    rasterize directly from that source rectangle to destWidth×destHeight
    serialize JPEG encodes so work cannot pile up inside the encoder
    cache immutable encoded rasters by revision + rectangle + output size + quality
    return only that viewport JPEG through Wails RPC as data:image/jpeg;base64,...

Compatibility URLs:
GET /__atropos/preview/{session}/{revision}.jpg?x=X&y=Y&w=W&h=H&dw=DW&dh=DH&q=88
    legacy/debug viewport HTTP transport
GET /__atropos/preview/{session}/{revision}-low.jpg
    legacy 1600px-max JPEG quality 85
GET /__atropos/preview/{session}/{revision}.jpg
    legacy full-frame JPEG quality 95
```

The full-resolution preview never travels through the Wails JSON/base64 method
bridge. `imagePreviewURL` sends only an opaque revision URL; the backend retains
the immutable source pointer while that revision remains in the bounded preview
store. The *viewport-sized encoded JPEG* does cross the Wails bridge. This is
intentional: on Windows/WebView2, the dynamic AssetsHandler can complete a
query-parameterized image request while a programmatically created image never
fires either `load` or `error`. Sending the already-small viewport raster through
the normal Wails promise lifecycle avoids that ambiguous resource path without
returning to full-frame IPC. Because revisions retain source pixels, the live
revision store is intentionally capped at four entries.

`PreviewCanvas` converts the scroller viewport through the logical image surface
into an image-space source rectangle, adds a modest overscan margin, quantizes
source bounds to reduce cache churn, and asks for only the raster density needed
for the current CSS zoom and device-pixel ratio. It never intentionally upsamples
beyond source-pixel density. Backend requests are defensively capped at 4096
pixels per dimension and 16 MiPixels. The server resizer samples directly from
the requested source rectangle, so a fit-to-window request does not first
allocate a full-resolution crop.

Only one Wails viewport request is in flight from the frontend at a time. Pan and
zoom activity that occurs during an encode is coalesced into one follow-up
request for the latest camera state. Successful data-URL responses decode through
`HTMLImageElement` and enter a small LRU-style raster cache. During pan/zoom,
cached covering/neighboring rasters can be drawn immediately while a better
raster is in flight. The visible canvas stays viewport-sized (scaled by DPR); it
is never allocated at full source-image dimensions merely because the source is
large.

Before drawing, the frontend clips each cached raster and the checkerboard
skeleton to the visible canvas and maps that intersection back to bitmap
coordinates. This keeps `drawImage`/`fillRect` destination rectangles bounded
to the viewport. In particular, it avoids a macOS WebKit rendering failure when
a cached full-frame raster would otherwise be drawn through an offscreen
destination wider than the GPU texture limit at high zoom.

The prior presented revision stays visible until the replacement raster has
decoded and is ready to draw. `App.jsx` therefore still distinguishes the
authoritative backend `preview` URL from `presentedPreview`; user-facing busy
state remains `backend loading || preview presentation pending`. Dimensions,
status metadata, and guide state advance with canvas presentation rather than
with a DOM image `onLoad`. A full image-session token change still prevents a
persistent WebView cache from confusing revisions across loads or launches.

Touch-up commits remain incremental. Their transparent changed-pixel patch is
decoded and composited by `PreviewCanvas` over the active revision, avoiding a
full-frame refresh. Disc live drag likewise uses the renderer to transform the
unmasked preview locally while the backend remains authoritative for the final
commit.

---

## Frontend Coordinate System

`imgRef` no longer points at a visible DOM `<img>`. It points at a transparent
logical image surface whose CSS width/height are exactly the currently presented
image dimensions after fit-width and zoom. The surface defines scroll extent,
pointer hit testing, and the shared display geometry for the viewport renderer.

Existing interaction code continues to use `displayToImage(dispX, dispY)`:

```javascript
displayToImage(dispX, dispY) {
    rect = imgRef.current.getBoundingClientRect()
    return {
        x: round(dispX * (realImageDims.w / rect.width)),
        y: round(dispY * (realImageDims.h / rect.height)),
    }
}
```

- `dispX/dispY` are CSS-pixel offsets relative to the logical image surface.
- `realImageDims` is the authoritative source size reported by Go.
- The ratio `realImageDims.w / rect.width` is the display-to-image scale factor.
- `PreviewCanvas` uses the same logical surface rectangle to map image-space
  pixels into viewport canvas coordinates, so pointer math and visible pixels
  cannot drift because of independent DOM image sizing.

Visible persistent guides are drawn directly into the canvas from image-space
state: detected/selected corners, touch-up strokes, line geometry, Normal crop,
disc guide/cutout, and straight-edge state. `ImageOverlays.jsx` now supplies
only transparent DOM hit targets for editable Normal/Line handles. Transient
and persistent drawings therefore share one camera transform while preserving
large DOM hit areas for the existing mouse state machine.

**`lineStartImgRef`:** In line mode, image-space coordinates are captured in
`lineStartImgRef` at mousedown so the live drag overlay and final committed line
start remain stable if the user scrollwheels to zoom mid-drag.

---

## Zoom and Fit Width (`hooks/useZoomPan.js`)

```javascript
fitWidth = min(container.clientWidth, container.clientHeight * aspectRatio)
displayWidth = fitWidth * zoom
displayHeight = displayWidth / aspectRatio
```

The transparent logical image surface is assigned that display size and owns
scroll extent. The visible canvas remains fixed to the scroller viewport and is
backed at approximately `clientWidth × devicePixelRatio` by
`clientHeight × devicePixelRatio`; it does not grow to source-image dimensions.
`PreviewCanvas` derives the visible source rectangle from the scroller and
logical surface, then requests an appropriate LOD raster for that rectangle.

`fitWidth` is recalculated by:
1. `handleImgLoad(dims)` — now called when `PreviewCanvas` presents a decoded revision
2. `ResizeObserver` on `canvasRef` — fires when the viewport container is resized

Do not zero `fitWidth` while waiting for raster decode. Preserve the prior
presented geometry until the replacement revision is ready, then update fit
from the presented dimensions. This keeps interaction metadata and pixels
atomic without depending on browser `<img>` load behavior.

**Scroll-wheel zoom formula:** The wheel handler uses
`imgRef.current.getBoundingClientRect()` to compute cursor position relative to
the logical image surface's actual left/top edge. This accounts for centering
when the image is smaller than the viewport. `pendingScrollRef` is set inside
the `setZoom` updater and applied synchronously in `useLayoutEffect([zoom])`
after the DOM commit.

The maximum zoom is image-aware: at least the historical 5× limit, or enough
to render two CSS pixels per source pixel for a large source image, with a
defensive ceiling of 40×. The viewport raster request itself does not upsample
past source-pixel density; extra zoom is performed by the canvas presentation
transform.

**Scroll-surface invariant:** `.preview-scroll-surface` / the logical image
stage, not the visible canvas, defines overflow dimensions. Do not let that
surface flex-shrink to the viewport or hide its scroll extent, or horizontal and
vertical pan will disappear.

### Space+Drag Pan

Holding Space while dragging pans via `canvasRef.current.scrollLeft/scrollTop`. `e.preventDefault()` is called for **every** space keydown including repeats (suppresses native scroll), but `spaceDownRef`/`spacePanMode` only update on the first press.

---

## Save (`app_io.go`)

```
SaveImage(req)
    require warpedImage != nil
    1. Create output file at req.outputPath
    2. Branch on extension:
         .jpg/.jpeg → JPEG quality 95
         .bmp       → BMP
         .tiff/.tif → TIFF
         default    → PNG with BestSpeed compression + 1MiB bufio.Writer
    3. If CLI post-save command is configured, launch it with `{path}` substitution
    4. If CLI post-save-exit is enabled, quit after launching command
    5. Return "Saved to {path}"
```

**Only `warpedImage` is saved.** A user who has only done pre-warp adjustments will still get a save because `setWorkingImage` always writes to `warpedImage`.

---

## Wails FFI Bridge (`frontend/wailsjs/go/main/App.js`)

This file is auto-generated by Wails during `wails dev` / `wails build`.

- Do not edit `frontend/wailsjs/go/main/App.js` by hand.
- If you add/rename Go methods exported from `App`, rerun the Wails toolchain to regenerate the bridge.
- Complex argument/return types are declared in `frontend/wailsjs/go/models.ts`.

---

## Error Handling

- **Recoverable errors** (invalid state, bad args): Go returns `(nil, error)` → Wails rejects the JS promise → `catch` block in the frontend handler → `ErrorModal`
- **Touch-up failure**: `ErrorModal` with user-friendly message; if IOPaint backend, includes a hint to check the server
- **WASDQE before crop**: guarded in `useKeyboardShortcuts` — calls `showStatus(...)` instead of forwarding to Go (prevents error modal)
- **Destructive confirmation** (Re-crop): `ConfirmationModal` (Cancel / Continue) before calling `RecropImage`. Use this pattern for any future action that irreversibly discards session state.
- **Warp outpaint failure**: hard error; no fallback
- **Ctrl+W / Cmd+W**: fires before the `imageLoaded` guard, so quitting works even when no image is loaded

---

## Settings Storage

Settings are persisted to `%AppData%\atropos\settings.json` (Windows) / `~/.config/atropos/settings.json` by the Go backend. All running instances share this file. Invalid values are sanitised to defaults by `sanitizeSettings` in `app_settings.go`.

| Setting | JSON key | Go field | Valid values |
|---------|----------|----------|--------------|
| Touch-up backend | `touchupBackend` | `touchupBackend` | `"patchmatch"`, `"iopaint"` |
| IOPaint URL | `iopaintUrl` | `iopaintURL` | Any non-empty URL string |
| Warp fill mode | `warpFillMode` | `warpFillMode` | `"clamp"`, `"fill"`, `"outpaint"` |
| Warp fill color | `warpFillColor` | `warpFillColor` | CSS hex `"#rrggbb"` |
| Disc centre cutout | `discCenterCutout` | `discCenterCutout` | `true` / `false` (default `true`) |
| Disc cutout size | `discCutoutPercent` | `discCutoutPercent` | Integer 0–50 (default `11`) |
| Auto-adjust corner params | `autoCornerParams` | *(frontend-only)* | `true` / `false` (default `true`) |
| Close after save | `closeAfterSave` | *(frontend-only)* | `true` / `false` (default `false`) |
| Post-save enabled | `postSaveEnabled` | *(frontend-only)* | `true` / `false` (default `false`) |
| Post-save command | `postSaveCommand` | *(frontend-only)* | Any string |
| Touch-up brush remains active | `touchupRemainsActive` | *(frontend-only)* | `true` / `false` (default `true`) |
| Straight edge remains active | `straightEdgeRemainsActive` | *(frontend-only)* | `true` / `false` (default `true`) |
| Auto-detect corners on mode switch | `autoDetectOnModeSwitch` | *(frontend-only)* | `true` / `false` (default `true`) |

`closeAfterSave` has no Go counterpart — consumed entirely in the frontend `handleSaveImage` handler.

---

## Image Compositor (`compositor.go`, `app_compositor.go`)

A standalone planar image stitching feature. `compositor.go` has no dependency on app state.

### App struct fields

| Field | Type | Purpose |
|-------|------|---------|
| `compositorResult` | `*image.NRGBA` | Cached full-resolution stitch output. Nil until a successful `CompositorStitch`. |
| `compositorMu` | `sync.Mutex` | Protects `compositorResult`. |

### Wails-facing methods

- **`CompositorOpenFilesDialog()`** — multi-file picker returning selected paths
- **`CompositorStitch(req)`** — decodes all images, reverses path order for `"rtl"`/`"btt"` orientations, runs `stitchImages`, caches result, returns preview + dimensions
- **`CompositorLoadResult()`** — promotes the cached result into the main editing pipeline (full state reset, sets `originalImage`/`currentImage`, returns `ImageInfo`)
- **`CompositorSave(req)`** — encodes cached result to disk
- **`CompositorOpenSaveDialog()`** — save-file dialog

### Stitching pipeline (`compositor.go`)

`stitchImages(imgs)`: detect Shi-Tomasi features on each image → match with SSD + Lowe's ratio test (threshold 0.75) → estimate homography via RANSAC (2000 iterations, 15 px inlier threshold) with DLT + Hartley normalisation → compose homographies so image[0] is the reference frame → render all images into a single canvas with distance-to-border feathering and bilinear interpolation (parallelised by row).

### Frontend flow

`handleCompositorLoad(info)` receives the `ImageInfo` from `CompositorLoadResult`, updates all image state, calls `resetImageState()`, updates `suggestedCornerParamsRef.current`, then switches to Corner mode and runs corner detection. The `CompositorModal` `onLoad` callback closes the modal **before** calling `handleCompositorLoad`.
