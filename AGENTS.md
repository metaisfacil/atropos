# Atropos — Agent Reference Guide

This document describes the complete system architecture, data flow, and operation ordering for the Atropos application. It exists to give any AI agent working on this codebase an accurate mental model before making changes.

---

## Technology Stack

- **Backend:** Go, Wails v2 (exposes Go methods to a WebView2 frontend via an auto-generated JS bridge)
- **Frontend:** React (JSX), plain CSS
- **Image processing:** Pure Go — no OpenCV, no external image libraries except `golang.org/x/image` for BMP/TIFF encode/decode
- **Wails FFI bridge:** `frontend/wailsjs/go/main/App.js` — manually maintained when new Go methods are added; `frontend/wailsjs/go/models.ts` for complex argument types

---

## Frontend File Map

`App.jsx` is a thin coordination layer (~420 lines): state declarations, hook wiring, and JSX render. All logic lives in hooks and components under `frontend/src/`.

| File | Responsibility |
|------|----------------|
| `hooks/useImageActions.js` | All Go API action handlers: `loadFile`, `handleLoadImage`, `handleDetectCorners`, `handleSkipCrop`, `handleRecrop`, `handleResetCorners/Disc/Normal`, `handleNormalCrop`, `handleClearLines`, `handleSaveImage`, `handleModeSwitch`. Also owns the `OnFileDrop` and launch-args `useEffect`s, plus internal `modeRef` and `lastDetectSettings`. |
| `hooks/useMouseHandlers.js` | All pointer interaction: `handleMouseDown/Move/Up/ImageMouseLeave` across all modes. Owns internal `cornerMouseDownRef`, `ctrlDragBusy`, `shiftDragBusy`. Defines and returns `displayToImage` (also forwarded to `<ImageOverlays>`). |
| `hooks/useKeyboardShortcuts.js` | Single `keydown` `useEffect`: arrow keys (disc shift), `+`/`-` (feather), `Y` (eyedrop), `Ctrl+Z` (undo), `Ctrl+S` (save), `WASDQE` (crop/rotate). |
| `hooks/useTouchup.js` | Touch-up brush state machine: `touchupStrokes`, `brushSize`, `commitTouchup`, window mouseup effect, `EventsOn("touchup-done")` effect. |
| `hooks/useZoomPan.js` | Canvas viewport: `zoom`, `fitWidth`, `spacePanMode`, `canvasRef`, wheel zoom/feather handler, space-key pan, `ResizeObserver`, scroll `useLayoutEffect`. |
| `hooks/usePersistentSettings.js` | localStorage-backed settings (`touchupBackend`, `iopaintURL`, `warpFillMode`, `warpFillColor`, `discCenterCutout`, `discCutoutPercent`, `closeAfterSave`). Syncs to Go on startup and on every change. |
| `hooks/useStatusMessage.js` | `imageInfo` + fade timer logic (`showStatus`). |
| `components/ImageOverlays.jsx` | Pure render: all SVG overlays (corner dots, touch-up circles, disc guide, straight-edge line, normal crop rect, line mode lines). |
| `components/*Panel.jsx` | Mode-specific sidebar controls. `AdjustmentsPanel` also owns the levels sliders, auto-contrast, and touch-up brush controls. |

`ctrlDragRef` and `shiftDragRef` are defined in `App.jsx` (not moved to `useMouseHandlers`) because they are read by `<ImageOverlays>` at render time for the disc guide condition, in addition to being written by the mouse handlers.

---

## Critical Image State Machine

This is the single most important concept in the codebase. Every operation must be understood in terms of which image field it reads and writes.

```
originalImage  ──  immutable after LoadImage; never modified
      │
      └── cloned to ──► currentImage  ──  pre-warp working image
                                │         adjustments (levels, auto-contrast)
                                │         write here if warpedImage is nil
                                │
                 ┌──────────────┬───────────────────┬─────────────────┐
                 │              │                   │                 │
         CornerPanel      LinePanel            DiscPanel        NormalCropPanel
         warpFromCorners  ProcessLines         DrawDisc         NormalCrop
                 │              │                   │                 │
                 └──────────────┴───────────────────┴─────────────────►  warpedImage
                                                          │
                                                   All subsequent ops:
                                                   Crop / Rotate / Undo
                                                   Levels / AutoContrast
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
| `discBaseImage` | `*image.NRGBA` | Snapshot of `currentImage` captured at `DrawDisc` time. `redrawDisc()` **always** sources from this image, not `warpedImage`, so every re-render of the disc is deterministic. |
| `undoStack` | `[]undoEntry` | LIFO stack capped at `undoLimit` (10). Each entry is an `undoEntry{image, rotationAngle}`. `rotationAngle` is non-nil only for `saveDiscRotationUndo()` snapshots (e.g. `StraightEdgeRotate`). |
| `discWorkingCrop` | `*image.NRGBA` | Pre-cropped sub-region of `discBaseImage` centred on the disc with a generous extra margin (`discWorkingCropShiftPadding = 500 px`). `redrawDisc` reads from this small image instead of the full `discBaseImage` to avoid cache thrashing on large images. Refreshed on `DrawDisc` and when a shift moves the disc outside the cached region. Cleared by `ResetDisc`. |
| `discWorkingCropRect` | `image.Rectangle` | Records the rect of `discBaseImage` that `discWorkingCrop` covers (in `discBaseImage` coordinates). Used to detect when a shift has moved the disc outside the working crop. |
| `discCenterCutout` | `bool` | When true, `redrawDisc` punches a circular hole at the disc centre to expose `bgColor`. Default: `true`. |
| `discCutoutPercent` | `int` | Diameter of the centre cutout as a percentage of the disc diameter (1–50). Cutout radius = `discRadius * discCutoutPercent / 100`. Default: `11`. |

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
2. Push an `undoEntry{image: clone of workingImage(), rotationAngle: nil}`.
3. **Clears `levelsBaseImage`** — next `SetLevels` call gets a fresh snapshot.

### `saveDiscRotationUndo()` (in `app_adjust.go`)

Same as `saveUndo()` but also snapshots the current `rotationAngle` into the entry (`rotationAngle: &angle`). Used by `StraightEdgeRotate` so that Undo restores both the image pixels and the accumulated rotation angle, keeping disc re-renders consistent.

**Rule:** Every operation that commits a permanent change must call `saveUndo()` first — except `SetLevels`, which deliberately does not (slider ticks must not flood the undo stack). This includes the warp-entry operations: `ClickCorner` (4th click), `ProcessLines`, and `DrawDisc`. `StraightEdgeRotate` uses `saveDiscRotationUndo()` instead.

---

## Startup Sequence

```
Wails runtime
    └── NewApp()             set all defaults (see below)
    └── App.startup(ctx)     store context
    └── Wails injects JS bridge
    └── React mounts
    └── useEffect (mount)
        ├── SetTouchupSettings({ localStorage values })  // push persisted settings to Go
        ├── SetWarpSettings({ localStorage values })
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

**Settings persistence:** Go fields are in-memory only. The frontend persists settings to `localStorage` and re-hydrates the Go side on every startup via `SetTouchupSettings` / `SetWarpSettings`. The frontend is the source of truth.

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
         selectedCorners = nil
         detectedCorners = nil       ← caches invalidated on new image
         lines = nil
         undoStack = nil
         discCenter = zero
         discRadius = 0
         rotationAngle = 0
         discBaseImage = nil
         discWorkingCrop = nil
         discWorkingCropRect = zero
         postDiscBlack = 0
         postDiscWhite = 255
    6. imageToBase64(currentImage)   JPEG, downscaled if > 1600px longest side
    7. Update window title
    8. Return ImageInfo{Width, Height, Preview}
```

**Frontend `loadFile(filePath, autoDetect)` flow:**

```
setLoading(true)
setZoom(1)
LoadImage({filePath})
    → setPreview, setImageLoaded, setRealImageDims
    → reset ALL mode-specific frontend state (cornerCount, linesDone, discActive,
      touchupStrokes, useTouchupTool, useStraightEdgeTool, lastDetectSettings, etc.)
if autoDetect && mode === 'corner':
    DetectCorners(...)
    → setPreview, setCornersDetected(true)
    → lastDetectSettings.current = current detection params   ← enables restore on next mode switch
setLoading(false)
```

---

## Corner Mode (`app_corner.go`)

### Corner Detection

```
DetectCorners(req)
    1. Downsample currentImage to max ~1500px on longest side (scaleFactor)
    2. applyAccentAdjustment(currentImage, accentValue)
    3. toGrayscale(adjusted)
    4. Contrast stretch (one of two paths, mutually exclusive):
         if useStretch:
             stretched = stretchGrayPercentiles(workGray, stretchLow, stretchHigh)
             workGray  = applyCLAHE(stretched, clipLimit=2.0, tileSize=8)
         else if mean(workGray) < 100:   ← auto-dark detection
             stretched = stretchGrayPercentiles(workGray, 0.01, 0.99)
             workGray  = applyCLAHE(stretched, clipLimit=2.0, tileSize=8)
    5. Multi-scale Shi-Tomasi detection at scales [1, 2, 4]:
         for each scale:
             resizeGray (box-filter averaging when downsampling)
             goodFeaturesToTrack(scaled, perScale, quality, minDist, blockSize=7)
               ↳ internally applies gaussianBlurGray before Sobel gradient computation
             scale results back up to working resolution
         accumulate all corners
    6. Deduplicate: enforce minDistSq/4 between all retained corners
    7. Map working-space corners → full-resolution coordinates (divide by scaleFactor)
    8. Store in detectedCorners
    9. Return clean (unmodified) currentImage preview + Corners array + "Detected N corners"
         Dots are rendered by the frontend as an SVG overlay — never baked into the image
```

### Clicking Corners

```
ClickCorner(req)
    1. If not custom && detectedCorners exist:
           snap pt to nearest detected corner
       Else:
           use raw click coordinate
    2. Append pt to selectedCorners
    3. If selectedCorners.length < 4:
           return SnappedX/SnappedY/Count/Message only — NO preview
           (frontend adds point to selectedCornerPts SVG overlay)
    4. On 4th corner → saveUndo() → warpFromCorners(selectedCorners[:4]):
           sortVertices (→ TL, TR, BL, BR)
           compute outW = max(widthTop, widthBot)
           compute outH = max(heightLeft, heightRight)
           if warpFillMode == "clamp":
               perspectiveTransform(currentImage, src, dst, outW, outH)
           else:
               perspectiveTransformWithMask(currentImage, src, dst, outW, outH)
               applyWarpFill(warped, oobMask)   ← solid fill or PatchMatch outpaint
           warpedImage = result
           cropTop/Bottom/Left/Right = 0
    5. selectedCorners = nil
    6. Return preview + Width + Height + "Perspective corrected to W×H"
```

### Reset / Restore

```
ResetCorners()
    selectedCorners = nil
    warpedImage = nil          ← CRITICAL: so GetCleanPreview returns currentImage
    return clean currentImage preview + Corners (detectedCorners preserved)

RestoreCornerOverlay({dotRadius})
    if detectedCorners empty → error "no cached corners"
    return clean currentImage preview + Corners + "Detected N corners — click 4 corners"
    (dotRadius arg accepted but unused — dot size is handled entirely by the SVG overlay)

SkipCrop()
    require currentImage != nil
    warpedImage     = cloneImage(currentImage)   ← makes SaveImage available immediately
    selectedCorners = nil
    return currentImage preview + dims + "Crop skipped — image ready to save"
```

`SkipCrop` is available in all four modes. It is the backend half of the "Skip crop" button. The frontend sets mode-specific state to transition past the 1st phase without performing a warp:

| Mode | Frontend state change after SkipCrop |
|------|--------------------------------------|
| Corner | `cornerCount = 4`, clears `detectedCornerPts` |
| Disc | `discActive = true` |
| Line | `linesProcessed = true` |
| Normal | `normalCropApplied = true` |

The frontend also sets `cropSkipped = true`, which disables all 1st-phase sidebar controls (Corner detection sliders and Detect button; Disc feather/cutout sliders) until Reset is clicked. Line drawing on the canvas is also blocked when `linesProcessed` is true. `cropSkipped` is cleared by every reset path: `handleResetCorners`, `handleResetDisc`, `handleClearLines`, `handleResetNormal`, `loadFile`, and all mode-switch reset branches.

**Key:** `detectedCorners` is NOT cleared on mode switch (only on `LoadImage`). This enables the cached-corners restore path.

### RecropImage (`app_io.go`)

```
RecropImage()
    require warpedImage != nil
    originalImage   = warpedImage        ← promote output to new source
    currentImage    = cloneImage(warpedImage)
    warpedImage     = nil
    levelsBaseImage = nil
    selectedCorners = nil
    detectedCorners = nil
    lines           = nil
    undoStack       = nil
    discCenter      = zero
    discRadius      = 0
    rotationAngle   = 0
    discBaseImage   = nil
    discWorkingCrop = nil
    discWorkingCropRect = zero
    postDiscBlack   = 0
    postDiscWhite   = 255
    return ImageInfo{Width, Height, Preview}
```

`RecropImage` is the backend half of the **Re-crop** button, which appears in all four modes once phase 1 is complete (i.e. when `warpedImage != nil`). It promotes the current output image to become the new source, allowing the user to apply a second crop mode without saving and reloading. The frontend resets all mode-specific state identically to `loadFile` (corner count, disc active, lines, undo history, levels sliders, touch-up strokes, etc.) and shows a `ConfirmationModal` before calling this method, because the operation is irreversible within the session.

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
           perspectiveTransformWithMask + applyWarpFill
    7. warpedImage = result
    8. lines = nil
    9. Return preview

ClearLines()
    lines = nil
    warpedImage = nil           ← consistent with ResetCorners
    return currentImage preview
```

**Note:** Line mode warps from `originalImage`, not `currentImage`. Corner mode warps from `currentImage`.

---

## Disc Mode (`app_disc.go` / `app_adjust.go`)

Disc mode is the most stateful mode. Every re-render replays the full pipeline from `discBaseImage`.

### DrawDisc — Entry Point

```
DrawDisc(req)
    discCenter    = req.centerX, req.centerY
    discRadius    = req.radius
    rotationAngle = 0
    discBaseImage = cloneImage(currentImage)  ← snapshot BEFORE disc
    postDiscBlack = 0
    postDiscWhite = 255
    redrawDisc()
```

### redrawDisc — The Full Disc Pipeline (called on every parameter change)

```
redrawDisc()
    1. src = discBaseImage (or originalImage as emergency fallback)
    2. bbox = [discCenter ± (discRadius + featherSize)]   ← include feather margin
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
    9. levelsBaseImage = nil     ← fresh base for next SetLevels session
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

Normal mode is the simplest mode: the user drags a rectangle on the image and clicks **Crop** to apply it. There is no algorithm, no warp, and no additional Go state beyond the shared image fields.

### NormalCrop — Entry Point

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

**Key:** `NormalCrop` calls `workingImage()`, so it works on `currentImage` (pre-warp) or `warpedImage` (post any prior operation) transparently. Each call commits a new undo entry, so sequential crops are each individually reversible.

### ResetNormal

```
ResetNormal()
    warpedImage = nil
    return currentImage preview + dims + "Normal crop reset"
```

Consistent with `ResetCorners` / `ClearLines` / `ResetDisc`: clears `warpedImage` so that `GetCleanPreview` returns `currentImage` after a mode switch.

### Frontend state

| State | Meaning |
|-------|---------|
| `normalRect` | `{x1,y1,x2,y2}` image-space selection drawn by the user, or `null` if no pending selection |
| `normalCropApplied` | `true` after the first crop (or Skip crop) — unlocks touch-up and marks phase 1 done |

`normalRect` is purely frontend; it is never sent to Go until the user clicks **Crop**. After `NormalCrop` returns, `normalRect` is cleared to `null` and `normalCropApplied` is set to `true`. Unlike other modes, the user can draw and apply additional rectangles after the first crop without resetting — each further crop operates on the already-cropped `warpedImage`.

---

## Adjustments (`app_adjust.go`)

All adjustment operations respect the pre-warp / post-warp / post-disc branching.

### Crop

```
Crop(req)
    require warpedImage != nil
    saveUndo()
    adjust rectangle based on direction, increment crop offset counter
    warpedImage = subImage(warpedImage, adjustedRect)
    return preview
```

### Rotate

```
Rotate(req)
    require warpedImage != nil
    saveUndo()
    warpedImage = rotate90(warpedImage, flipCode)
      (flipCode 0 = CCW 90°, 1 = CW 90°, 2 = 180°)
    return preview
```

### SetLevels (non-committing)

```
SetLevels(req)
    preWarp = (warpedImage == nil)

    On first call after a commit:
        levelsBaseImage = clone of workingImage()

    if preWarp:
        apply levels to levelsBaseImage → write to currentImage
        if detectedCorners exist: redraw corner overlay
    else if discRadius > 0:
        postDiscBlack = req.black
        postDiscWhite = req.white
        redrawDisc()
    else:
        apply levels to levelsBaseImage → warpedImage = result

    return preview
    NOTE: saveUndo() is NOT called here
```

### AutoContrast (committing)

```
AutoContrast()
    preWarp = (warpedImage == nil)

    base = levelsBaseImage ?? workingImage()
    preLevelsBase = clone(base)

    saveUndo()              ← commits; clears levelsBaseImage

    (blackPt, whitePt) = computeAutoContrastPoints(base)
    result = applyLevels(base, blackPt, whitePt)

    if preWarp:
        currentImage = result
        levelsBaseImage = preLevelsBase    ← restore so slider still works
        if detectedCorners: redraw overlay
    else if discRadius > 0:
        postDiscBlack = blackPt
        postDiscWhite = whitePt
        levelsBaseImage = preLevelsBase
        redrawDisc()
    else:
        warpedImage = result
        levelsBaseImage = preLevelsBase

    return preview + black/white values
```

### Undo

```
Undo()
    if undoStack empty → "Nothing to undo"
    entry = pop from undoStack
    warpedImage = entry.image
    if entry.rotationAngle != nil:
        rotationAngle = *entry.rotationAngle   <- restores disc angle for StraightEdgeRotate undo
    return preview
```

**Frontend note:** Undo is blocked while any drag operation is active (disc shift drag, rotation drag, etc.) to prevent undo from firing mid-drag and corrupting disc state.

---

## Touch-Up (`app_touchup.go`, `app_iopaint.go`, `patchmatch.go`)

### Availability

The touch-up brush button is **disabled** until the initial crop has been committed in the current mode (either by completing the normal 1st-phase operation or by clicking "Skip crop"):

| Mode | Enabled when |
|------|--------------|
| Corner | `cornerState.cornerCount === 4` (warp applied or Skip crop) |
| Line | `linesProcessed === true` |
| Disc | `discActive === true` |
| Normal | `normalCropApplied === true` |

Switching modes always resets `useTouchupTool` to `false`, and so does loading a new image (`loadFile`). The rationale: both Disc and Line modes use mouse drag for their first-stage input (drawing the disc / drawing lines), which would conflict with the touch-up brush drag if it were accidentally left on.

### buildMask(maskB64)

```
1. Base64-decode PNG mask
2. Per pixel: if alpha channel present → use alpha; else use luminance threshold (> 10 → 255)
3. If mask dimensions != workingImage: resize mask to match
4. Return *image.Alpha  (alpha > 0 = region to fill)
```

### Touch-up cancellation

An in-flight `TouchUpApply` can be aborted via two paths:

- **Internal** (`cancelTouchup()`): called at the top of every reset and load handler in Go so that a slow fill never commits stale results after the user has moved on.
- **External** (`CancelTouchup()`): a public Wails-bound method called **fire-and-forget** (no `await`) from the frontend immediately before any reset IPC call. This ensures the cancel signal arrives at the backend before the reset call, even though both are in-flight concurrently.

| Field / method | Location | Purpose |
|----------------|----------|---------|
| `touchupCancel` | `App` struct | `context.CancelFunc` for the current operation; `nil` when idle |
| `touchupMu` | `App` struct | `sync.Mutex` protecting `touchupCancel` |
| `cancelTouchup()` | `app_touchup.go` | Acquires lock, calls and nils `touchupCancel`. Safe from any goroutine. |
| `CancelTouchup()` | `app_touchup.go` | Wails-bound public wrapper around `cancelTouchup()`. Called fire-and-forget from the frontend. |

`cancelTouchup()` is called at the top of: `ResetCorners`, `ResetDisc`, `ClearLines`, `ResetNormal`, `LoadImage`, `RecropImage`.

### TouchUpApply (commits) — async

`TouchUpApply` is **non-blocking**: it builds the mask synchronously (fast), then launches a goroutine for the fill and returns `ProcessResult{Message:"running"}` immediately. This keeps the Wails IPC queue free so `CancelTouchup()` can arrive and execute while PatchMatch is still running. The fill result is delivered via the `"touchup-done"` Wails event.

```
TouchUpApply(maskB64, patchSize, iterations) → ProcessResult{Message:"running"}, nil
    cancelTouchup()                   ← abort any prior in-flight operation
    ctx, cancel = context.WithCancel(Background)
    store cancel in touchupCancel (mutex-protected)

    mask = buildMask(maskB64)         ← synchronous; takes ~1s on large images

    go func():
        defer: cancel(); touchupCancel = nil
        if touchupBackend == "iopaint":
            out, err = iopaintFill(ctx, workingImage, mask)
        else:
            out, err = PatchMatchFill(ctx, workingImage, mask, patchSize, iterations)

        if err is context.Canceled → EventsEmit("touchup-done", {cancelled:true})
        if err != nil              → EventsEmit("touchup-done", {error: err})
        if ctx.Err() != nil        → EventsEmit("touchup-done", {cancelled:true})

        saveUndo()
        setWorkingImage(out)
        EventsEmit("touchup-done", {preview, message, width, height})
```

The frontend registers a single persistent `EventsOn("touchup-done", ...)` handler (via `useEffect([], [])`) that calls a ref-backed callback so it always sees current state. On `cancelled`, it silently clears the loading spinner. On `error`, it shows an ErrorModal. On success, it updates the preview.

### PatchMatchFill (patchmatch.go)

```
PatchMatchFill(ctx, src, mask, patchSize, iterations) → (*image.NRGBA, error)
    0. ctx.Err() check at entry → return immediately if already cancelled
    1. Collect all target patches (center pixel masked)
         ctx.Err() check every 64 rows → return if cancelled during init
    2. Collect all source patches (no mask overlap)
         ctx.Err() check every 64 rows → return if cancelled during init
    3. Randomly initialise nearest-neighbour field (NNF)
    4. For each iteration:
         check ctx.Done() → return nil, ctx.Err() if cancelled
         forward pass (parallelised):
             each worker checks ctx.Done() per target → returns early if cancelled
         wg.Wait(); check ctx.Err() again
         reverse pass (parallelised):
             each worker checks ctx.Done() per target → returns early if cancelled
         wg.Wait(); check ctx.Err() again
    5. Reconstruct: for each target pixel, average contributions from best source patch
    6. Return dst, nil
```

The entry-point `ctx.Err()` check is critical: `buildMask` can take ~1s on large images, so the context may already be cancelled by the time `PatchMatchFill` starts. Without the upfront check, PatchMatch would run the entire O(w×h×patchSize²) initialization (billions of ops on a 5000px image) before reaching any cancellation point. Workers check `ctx.Done()` via a non-blocking `select` at the top of every per-target loop. `ctx.Done()` is pre-fetched into a local `done` variable before the goroutines start; for `context.Background()` (used by warp outpaint) the channel is nil and the select always takes the default branch with zero overhead.

### iopaintFill (app_iopaint.go)

```
iopaintFill(ctx, src, mask) → (*image.NRGBA, error)
    1. Encode src as PNG → base64 data URI
    2. Build grayscale mask PNG (white=fill, black=keep) → base64 data URI
    3. http.NewRequestWithContext(ctx, POST, {iopaintURL}/api/v1/inpaint, body)
       client.Do(req)   ← request is torn down immediately if ctx is cancelled
    4. Parse response:
         try raw image.Decode on body bytes
         fall back to JSON {"image": "data:...;base64,..."}
    5. return toNRGBA(decoded), nil
```

**Failure handling:** `iopaintFill` errors (other than `context.Canceled`) propagate to `TouchUpApply` as hard errors. The frontend displays a modal with a user-friendly message. There is **no automatic fallback** to PatchMatch for touch-up.

**Outpaint (warp fill) always uses PatchMatch** — IOPaint is not used for out-of-bounds warp regions because it is an inpainting (not outpainting) model. Warp fill passes `context.Background()` to `PatchMatchFill` and is not cancellable.

---

## Mode Switching

### The Four Modes Are Mutually Exclusive

Corner, Disc, Line, and Normal modes are not a pipeline. Each mode operates independently on the same source scan. A user crops one object with Corners, saves, then might switch to Lines for a different object in the same scan. Switching modes always resets the warp result.

### Mode Switch Handler (`useImageActions.js:handleModeSwitch`)

```
onClick (mode button):
    if leaving 'corner':
        ResetCorners()              ← clears selectedCorners + warpedImage
        setCornersDetected(false)
        setCornerState({...cornerCount: 0})
        setCropSkipped(false)

    if leaving 'disc' && discActive:
        ResetDisc()                 ← clears all disc state + warpedImage
        setDiscActive(false)
        setCropSkipped(false)

    if leaving 'line':
        ClearLines()                ← clears lines + warpedImage
        setLinesDone(0), setLines([]), setLinesProcessed(false)
        setCropSkipped(false)

    if leaving 'normal':
        ResetNormal()               ← clears warpedImage
        setNormalRect(null), setNormalCropApplied(false)
        setCropSkipped(false)

    if arriving at 'corner' && lastDetectSettings matches current settings:
        RestoreCornerOverlay({dotRadius})   ← re-render cached corners
        setFitWidth(min(container.w, container.h * res.w/res.h))  ← compute correct value immediately
        setPreview, setRealImageDims
        setCornersDetected(true)
        setMode('corner'); return           ← early return, skip GetCleanPreview

    setFitWidth(min(container.w, container.h * res.w/res.h))  ← compute correct value immediately
    GetCleanPreview()                       ← returns currentImage (warpedImage now nil)
    setPreview, setRealImageDims
    setMode(m)
```

### GetCleanPreview (app_mode.go)

```
GetCleanPreview()
    selectedCorners = nil      ← clear in-progress selection
    detectedCorners preserved  ← so RestoreCornerOverlay still works later
    img = workingImage()       ← currentImage at this point (warpedImage cleared above)
    return preview + dims + "Ready"
```

### Cached Corner Restoration

When switching back to corner mode, if all of `{maxCorners, qualityLevel, minDistance, accent, useStretch}` match the values stored in `lastDetectSettings.current` at detection time, `RestoreCornerOverlay` is called instead of `GetCleanPreview`. This avoids re-running the (potentially slow) Shi-Tomasi detector.

`lastDetectSettings.current` is:
- **Set** after a successful `DetectCorners` call (both manual and auto-detect on image load)
- **Cleared** when a new image is loaded (`loadFile`)
- **Not cleared** on mode switches

---

## Image Processing Kernels (`imgproc.go`)

### perspectiveTransform

```
perspectiveTransform(src, srcPts[4], dstPts[4], outW, outH)
    1. Compute homography H mapping srcPts → dstPts
    2. Invert H → H_inv
    3. For each output pixel (x, y):
           (sx, sy) = H_inv * (x, y, 1)  (homogeneous)
           bilinear interpolate src at (sx, sy), clamp to bounds
           write to output
    4. Return *image.NRGBA
```

### perspectiveTransformWithMask

Same as above but when `(ix0, iy0)` lies outside `[sb.Min, sb.Max-2]`:
- Set `oobMask.Pix = 255` at that output pixel
- Leave output pixel transparent (do not clamp)

Used by `warpFillMode != "clamp"` paths.

### applyWarpFill (app_corner.go)

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

### imageToBase64

```
imageToBase64(img)
    if max(w, h) > 1600:
        resize to fit 1600px (preserving aspect)
    encode as JPEG quality 85 (fast; "data:image/jpeg;base64,...")
    (fallback: PNG for very small images)
```

---

## Frontend Coordinate System

All interaction coordinates go through `displayToImage(dispX, dispY)`:

```javascript
displayToImage(dispX, dispY) {
    rect = imgRef.current.getBoundingClientRect()
    return {
        x: round(dispX * (realImageDims.w / rect.width)),
        y: round(dispY * (realImageDims.h / rect.height)),
    }
}
```

- `dispX/dispY` are pixel offsets relative to the `<img>` element's top-left corner
- `realImageDims` is the full-resolution image size as reported by Go
- The ratio `realImageDims.w / rect.width` is the display-to-image scale factor

All SVG overlays use a `viewBox="0 0 W H"` spanning the full image with `preserveAspectRatio="none"` inside a `position: relative` wrapper around the `<img>`, so they always track the image at any zoom level without DOM measurements:

| Overlay | State | Condition |
|---------|-------|-----------|
| Corner dots (detected) | `detectedCornerPts` — `{X,Y}[]` image-space | `mode === 'corner'` |
| Corner dots (selected, clicks 1–3) | `selectedCornerPts` — `{X,Y}[]` image-space | `mode === 'corner'` |
| Touch-up strokes | `touchupStrokes` — `{x,y}[]` image-space | `useTouchupTool && strokes.length > 0` |
| Line preview | `lines` — `{x1,y1,x2,y2}[]` image-space | `mode === 'line' && lines.length > 0` |

Corner dots: detected corners are red circles (radius `dotRadius`); selected clicks 1–3 are green circles (radius `max(dotRadius*1.5, dotRadius+4)`).

Other overlays (disc drag circle) use display-space coordinates — acceptable for transient drag previews.

---

## Zoom and Fit Width

```javascript
fitWidth = min(container.clientWidth, container.clientHeight * aspectRatio)
```

The `<img>` style:
- If `fitWidth > 0`: `width: fitWidth * zoom px, height: auto, maxWidth: none`
- Else: `maxWidth: zoom*100%, maxHeight: zoom*100%`

`fitWidth` is recalculated by:
1. `handleImgLoad` — fires when a new image is decoded by the browser
2. `ResizeObserver` on `canvasRef` — fires when the container is resized

**Critical:** In the mode switch handler, `fitWidth` is recomputed directly from the Go response dimensions and container size (`Math.min(container.clientWidth, container.clientHeight * w/h)`) before calling `setPreview`. Do not zero it and rely on `handleImgLoad`: if the preview src string is unchanged (same `currentImage`, deterministic JPEG encoding), the browser skips `onLoad`, leaving `fitWidth` at 0. With `fitWidth = 0`, the image falls back to percentage-based `maxWidth`/`maxHeight` that resolve circularly against the `inline-block` wrapper and collapse to natural size; the absolutely-positioned SVG overlay then expands to match, flooding the entire canvas instead of tracking the image.

### Space+Drag Pan

Holding Space while dragging pans the canvas by directly adjusting `canvasRef.current.scrollLeft/scrollTop`. Key refs involved:

| Ref/State | Purpose |
|-----------|---------|
| `spaceDownRef` | Tracks whether Space is currently held (ref, not state — no re-render on key events). |
| `panDragRef` | Active pan anchor: `{startX, startY, scrollLeft, scrollTop}`. Set on mousedown, cleared on mouseup or Space release. |
| `spacePanMode` | React state that drives the `grab` cursor; set in the space keydown/keyup `useEffect`. |

The space `keydown` handler calls `e.preventDefault()` for **every** space keydown event including repeats (to suppress the browser's native scroll-on-space), but only updates `spaceDownRef`/`spacePanMode` on the first press (`e.repeat === false`). Pan is handled at the top of `handleMouseDown/Move/Up`, before the `e.target !== imgRef.current` guard, so it works anywhere in the canvas area.

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
    3. Return "Saved to {path}"
```

**Only `warpedImage` is saved.** A user who has only done pre-warp adjustments (levels on the loaded image, no warp) will still get a save because `setWorkingImage` always writes to `warpedImage`.

---

## Wails FFI Bridge (`frontend/wailsjs/go/main/App.js`)

Every Go method exposed to the frontend must have an entry here. The format is:

```javascript
export function MethodName(arg1) {
    return window['go']['main']['App']['MethodName'](arg1);
}
```

When adding new Go methods, add the corresponding entry here manually. The file has `// @ts-check` but all `arg1` parameters are untyped — this is a pre-existing pattern, not an error to fix.

Complex argument/return types are defined in `frontend/wailsjs/go/models.ts`.

---

## Error Handling

- **Recoverable errors** (invalid state, bad args): Go returns `(nil, error)` → Wails rejects the JS promise → `catch` block in `App.jsx`
- **Touch-up failure**: displays an `ErrorModal` with a user-friendly message; if IOPaint backend, includes a hint to check the server. Clears the status bar message and touch-up strokes.
- **Save failure / load failure / shortcut errors**: `ErrorModal`
- **Destructive confirmation** (Re-crop): displays a `ConfirmationModal` (Cancel / Continue) before calling `RecropImage`. `ConfirmationModal` is a close variant of `ErrorModal` with two buttons; it is driven by `confirmDialog` state `{ message, onConfirm }` in `App.jsx`. Use this pattern for any future action that irreversibly discards session state.
- **Warp outpaint failure**: propagates as a hard error; there is no automatic fallback
- **IOPaint for warp**: NOT used. Only PatchMatch is used for out-of-bounds warp fill.

---

## Settings Storage

| Setting | Go field | localStorage key | Valid values |
|---------|----------|------------------|--------------|
| Touch-up backend | `touchupBackend` | `touchupBackend` | `"patchmatch"`, `"iopaint"` |
| IOPaint URL | `iopaintURL` | `iopaintURL` | Any URL string |
| Warp fill mode | `warpFillMode` | `warpFillMode` | `"clamp"`, `"fill"`, `"outpaint"` |
| Warp fill color | `warpFillColor` | `warpFillColor` | CSS hex `"#rrggbb"` |
| Disc centre cutout | `discCenterCutout` | `discCenterCutout` | `"true"`, `"false"` (default `true`) |
| Disc cutout size | `discCutoutPercent` | `discCutoutPercent` | Integer 0–50 (default `11`) |
| Close after save | *(frontend-only)* | `closeAfterSave` | `"true"`, `"false"` (default `false`) |

On every app start, the frontend reads localStorage and calls `SetTouchupSettings` + `SetWarpSettings` + `SetDiscSettings` to synchronise the Go side. `closeAfterSave` has no Go counterpart — it is consumed entirely in the frontend `handleSaveImage` handler.

---

## Common Pitfalls

1. **Reading `warpedImage` directly instead of `workingImage()`** — breaks pre-warp paths
2. **Forgetting `saveUndo()` before a committing operation** — makes the operation non-undoable
3. **Calling `saveUndo()` in `SetLevels`** — floods the undo stack on every slider tick; it must not
4. **Not clearing `warpedImage` in a mode-reset function** — `GetCleanPreview` will return the stale warped result after a mode switch (the ResetCorners/ClearLines/ResetDisc bug pattern)
5. **Adding a new Go method without updating `App.js`** — the frontend will silently call undefined and get a JS error
6. **Using `el.offsetLeft/offsetTop` for persistent overlays** — stale on first render after state change; use a `viewBox`-based SVG inside a relative wrapper instead
7. **Setting `fitWidth` to 0 and relying on `handleImgLoad` to correct it during a mode switch** — if the preview src string is unchanged the browser skips `onLoad`, leaving `fitWidth` at 0 and causing the SVG overlay to flood the entire canvas. Always recompute `fitWidth` directly from the known dimensions when switching modes.
8. **Using IOPaint for the warp out-of-bounds fill** — IOPaint is an inpainting model and produces black for outpainting; always use PatchMatch for `applyWarpFill`
9. **Forgetting `e.preventDefault()` for repeated keydown events** — the browser fires repeated `keydown` events while a key is held; if you only prevent default on the first press (`!e.repeat`), native scroll or other browser behaviour fires on every subsequent tick. Guard the state update with `if (e.repeat) return`, but call `e.preventDefault()` unconditionally before that guard.
10. **Not clearing purely-frontend selection state in `handleSkipCrop`** — Skip crop bypasses phase 1 without going through the normal commit path, so any pending frontend-only selection (e.g. `normalRect` in Normal mode) must be explicitly cleared there. If it isn't, the selection overlay remains visible even though the user is now past the crop phase.
11. **Omitting disc state from `LoadImage` / `RecropImage` resets** — `discRadius`, `discBaseImage`, `rotationAngle`, and related fields are only cleared by `ResetDisc`. If a new image is loaded (or re-cropped) without resetting these, a stale `discRadius > 0` causes `AutoContrast` (and other post-warp paths) to enter the disc code path and call `redrawDisc()`, which renders from the old `discBaseImage` and shows a previously loaded image.
12. **Forgetting to reset tool-toggle state in `loadFile`** — `useTouchupTool` and `useStraightEdgeTool` must be reset to `false` when a new image is loaded, just as they are on mode switches. If omitted, the brush or straight-edge tool stays visually active on the new image and cannot be toggled off without switching modes.
13. **Forgetting `cancelTouchup()` in a new reset or load path** — any Go path that clears `warpedImage` and returns the app to a "clean" state must call `a.cancelTouchup()` first. Any JS reset handler must also call `CancelTouchup()` (fire-and-forget, no `await`) before the reset IPC call. Without both, an in-flight `TouchUpApply` (PatchMatch or IOPaint) can finish and emit `touchup-done` or call `saveUndo()` / `setWorkingImage()` after the reset has already cleared the image.
