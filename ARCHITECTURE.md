# Atropos - Architecture Reference

This document contains the detailed system model, data flow, and operation ordering for Atropos. It is intended to be read after `AGENTS.md`.

## Table of Contents

- [UI File Map](#ui-file-map)
- [Critical Image State Machine](#critical-image-state-machine)
  - [Fields](#fields-defined-in-appgo-app-struct)
  - [`workingImage()`](#workingimage-in-app_adjustgo)
  - [`setWorkingImage(img)`](#setworkingimageimg-in-app_adjustgo)
  - [`saveUndo()`](#saveundo-in-app_adjustgo)
  - [`saveDiscRotationUndo()`](#savediscrotationundo-in-app_adjustgo)
- [Startup Sequence](#startup-sequence)
- [Image Loading](#image-loading-app_iogo)
- [Corner Mode](#corner-mode-app_cornergo)
  - [Corner Detection](#corner-detection)
  - [Clicking Corners](#clicking-corners)
  - [Undoing in-progress corner picks](#undoing-in-progress-corner-picks)
  - [Reset / Restore](#reset--restore)
  - [`RecropImage`](#recropimage-app_iogo)
- [Line Mode](#line-mode-app_linego)
- [Disc Mode](#disc-mode-app_discgo--app_adjustgo)
  - [`DrawDisc` - Entry Point](#drawdisc--entry-point)
  - [`redrawDisc` - The Full Disc Pipeline](#redrawdisc--the-full-disc-pipeline)
  - [Operations that trigger `redrawDisc`](#operations-that-trigger-redrawdisc)
  - [`ResetDisc`](#resetdisc)
- [Normal Mode](#normal-mode-app_normalgo)
  - [`NormalCrop`](#normalcrop)
  - [`ResetNormal`](#resetnormal)
  - [Frontend state](#frontend-state)
- [Adjustments](#adjustments-app_adjustgo)
  - [Crop / Rotate / Resize / `TrimBorders`](#crop--rotate--resize--trimborders)
  - [Descreen](#descreen-session-based-committing-adjustment)
  - [`SetLevels`](#setlevels-non-committing)
  - [`AutoContrast`](#autocontrast-committing)
  - [Undo and redo](#undo-and-redo)
  - [Dust Removal](#dust-removal-app_dustgo-internaldust)
- [Touch-Up](#touch-up-app_touchupgo-app_iopaintgo-internalpatchmatch)
  - [Availability](#availability)
  - [Touch-up cancellation](#touch-up-cancellation)
  - [`TouchUpApply`](#touchupapply-async-non-blocking)
  - [PatchMatch](#patchmatch-internalpatchmatch-used-via-patchmatchchunkedfill)
  - [`iopaintFill`](#iopaintfill-app_iopaintgo)
- [Mode Switching](#mode-switching)
  - [The Four Modes Are Mutually Exclusive](#the-four-modes-are-mutually-exclusive)
  - [Mode Switch Handler](#mode-switch-handler-useimageactionsjshandlemodeswitch)
  - [`GetCleanPreview`](#getcleanpreview-app_modego)
  - [Cached Corner Restoration](#cached-corner-restoration)
- [Internal Image Processing Packages](#internal-image-processing-packages)
  - [`applyWarpFill`](#applywarpfill-app_cornergo)
  - [Preview asset transport](#preview-asset-transport)
- [UI Coordinate System](#ui-coordinate-system)
- [Zoom and Fit Width](#zoom-and-fit-width-uiviewportgo)
  - [Space+Drag Pan](#spacedrag-pan)
- [Save](#save-app_iogo)
- [Host Bridge](#host-bridge-app_hostgo-uihostgo)
- [Error Handling](#error-handling)
- [Settings Storage](#settings-storage)
- [Image Compositor](#image-compositor-internalcompositor-app_compositorgo)
  - [App struct fields](#app-struct-fields)
  - [UI-facing methods](#ui-facing-methods-app_compositorgo)
  - [Stitching pipeline](#stitching-pipeline-internalcompositorstitchgo)
  - [UI flow](#ui-flow-uicompositorgo)
- [Corner Calibration](#corner-calibration-app_calibrationgo)
- [State transition ownership](#state-transition-ownership)

---

## UI File Map

`main.go` owns the Gio window event loop and delegates frame rendering and event dispatching to `EditorUI` (`ui/controller.go`). All UI code lives in `package ui` under `ui/`, communicating with the `package main` backend through `ui.Backend` (`ui/backend.go`) and shared UI-free DTOs/contracts in `internal/appapi/types.go`.

| File | Responsibility |
|------|----------------|
| `internal/appapi/types.go`, `app_api_aliases.go`, `ui/backend.go` | Shared UI-free request/result DTOs, `AppHost` interface, `Package main` type aliases, and the `ui.Backend` interface implemented by `*App`. |
| `app_host.go`, `ui/host.go` | `AppHost` helper methods and `gioHost` implementation bridging native `zenity` open/save/color dialogs, external URLs, window titles, and async touch-up notifications. |
| `ui/platform_windows.go`, `ui/platform_other.go` | OS window hooks (`WM_DROPFILES` drag-and-drop, window icon from `appIconPNG`), external URL open, and OS spatial keyboard layout queries (`W/A/S/D/Q/E` localized labels with QWERTY fallback). |
| `ui/theme.go`, `ui/widgets.go` | Palette constants matching Atropos dark theme, `material.Theme` setup, buttons, commit-on-release sliders, delayed hover tooltips (1 s delay), dropdowns, inputs, and animated accordions. |
| `ui/controller.go` | `EditorUI` state machine: sequential `backendJobs` worker, UI queue (`uiQueue`), mode switches, load/save/recrop/close handlers, corner/disc/line/normal/touchup orchestration, `detectGen` / `transitionGen` guards, and `layoutRoot`. |
| `ui/canvas_input.go` | Pointer state machine across all 4 modes, adjustments marquee, touch-up brush + right-drag resize, disc `Ctrl+Drag` shift (`computeDiscShift`) and `Shift+Drag` rotate (pointer angle around the pivot, `discRotateDragState` in `ui/disc_shift_session.go`), straight-edge drag, and line/rect handle editing. |
| `ui/disc_shift_live.go`, `ui/disc_shift_session.go` | Live disc `Ctrl+Drag` / `Shift+Drag` preview: content raster moving and rotating under a stationary, gamma-matched vignette overlay; multi-drag session lifecycle, rotation gesture, and background prewarming (see [Live Ctrl+Drag Shift Preview](#live-ctrldrag-shift-preview)). |
| `ui/shortcuts.go` | Global/modal keyboard shortcuts (`Ctrl+O/S/C/V/Z/Y/A/D/W`, `Space+Drag`, `Enter`, `Escape`, disc arrows / `+/-` / `Y`, spatial `WASDQE`) and optimistic edge-crop helpers. |
| `ui/viewport.go` | GPU viewport renderer (`viewportState`): in-process `*image.NRGBA` region downsampling on a background worker, 6-entry LRU `paint.ImageOp` cache, scrollbars, live disc affine transform, and vector guide rendering (`drawCanvasGuides`). |
| `ui/sidebar.go`, `ui/adjustments.go`, `ui/statusbar.go` | Resizable left sidebar (200–600 dp, double-click reset), mode panels, Adjustments accordion (selection toggle, tools, Descreen, Dust, Touch-up, Stretch/Levels), Tools/Shortcuts accordions, top toolbar, bottom status bar, and loading overlay. |
| `ui/modals.go`, `ui/options.go` | Backdrop pointer isolation, `ErrorModal`, `ConfirmationModal` (2- and 3-button), `AboutModal`, `ResizeModal` (aspect lock & 2M-px upscale prompt), and 6-tab `OptionsModal`. |
| `ui/compositor.go`, `ui/calibration.go` | `CompositorModal` (stitch + repeated-scan overlay mask brush) and isolated `CornerCalibrationModal` (batch queue, `ResolveCalibrationImage` viewport, 4-corner annotation, JSON export). |

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
| `discBaseImage` | `*image.NRGBA` | Snapshot of `currentImage` captured at `DrawDisc` time. `redrawDisc()` **always** sources from this image, not `warpedImage`, so every re-render of the disc is deterministic. Post-disc pixel edits (Dust removal, Descreen, Touch-up, selection-scoped Levels/Auto contrast) install an edited **copy** here (it is never mutated in place, because preview assets and history reference it); see [Post-disc edits](#post-disc-edits). |
| `discNoMaskPreview` | `string` | Versioned preview asset URL for the disc source without mask compositing. Used by frontend live drag/rotate preview so interactions stay fluid while backend commits asynchronously. |
| `discWorkingCrop` | `*image.NRGBA` | Pre-cropped sub-region of `discBaseImage` centred on the disc with a generous extra margin (`discWorkingCropShiftPadding = 500 px`). `redrawDisc` reads from this small image instead of the full `discBaseImage` to avoid cache thrashing on large images. Refreshed on `DrawDisc` and when a shift moves the disc outside the cached region. Cleared by `ResetDisc`. |
| `discWorkingCropRect` | `image.Rectangle` | Records the rect of `discBaseImage` that `discWorkingCrop` covers (in `discBaseImage` coordinates). Used to detect when a shift has moved the disc outside the working crop. |
| `discCenterCutout` | `bool` | When true, `redrawDisc` punches a circular hole at the disc centre to expose `bgColor`. Default: `true`. |
| `discCutoutPercent` | `int` | Diameter of the centre cutout as a percentage of the disc diameter (1–50). Cutout radius = `discRadius * discCutoutPercent / 100`. Default: `11`. |
| `undoStack` | `[]undoEntry` | Undo stack sharing a 100-step / 1024 MiB budget with `redoStack`. Each entry stores image pixels plus optional rotation metadata (`rotationAngle`), pre-warp flag (`preWarp`), and optional in-progress corner picks (`selectedCorners`) for undoing back into corner-selection phase. |

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

1. Clear redo, then snapshot the working image into immutable 128x128 pixel tiles, sharing byte-identical tiles with the preceding snapshot. Live image buffers never alias history.
2. Push an `undoEntry` with the tiled image, rotation and post-disc levels metadata, and `preWarp` (true when `warpedImage == nil`). Evict oldest entries until at most 100 steps and 1024 MiB remain, counting shared tiles once. Always keep the newest step even if that scan alone exceeds the budget.
3. **Clears `levelsBaseImage`, `descreenBaseImage`, and `descreenResultImage`** so the next levels/descreen session snapshots fresh baselines.

### `saveDiscRotationUndo()` (in `app_adjust.go`)

Delegates to `saveUndo()`. All entries now snapshot `rotationAngle` and post-disc levels; `StraightEdgeRotate` retains this helper so existing call sites remain explicit. Both paths invalidate levels and descreen sessions.

**Rule:** Every operation that commits a permanent change must call `saveUndo()` first - except `SetLevels` and intra-session `Descreen` parameter changes (to avoid flooding undo entries during live parameter tuning). This includes warp-entry operations (`ClickCorner` on the 4th click, `ProcessLines`, `DrawDisc`) and most post-crop adjustments. `StraightEdgeRotate` uses `saveDiscRotationUndo()` instead.

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
| `undoLimit` | 100 |
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
    4. originalImage = result (no extra allocation - reuse toNRGBA output)
       currentImage  = cloneImage(originalImage)
    5. Clear ALL transient state:
         warpedImage = nil
         levelsBaseImage = nil
         descreenBaseImage = nil
         descreenResultImage = nil
         selectedCorners = nil
         detectedCorners = nil
         lines = nil
         undoStack = nil; redoStack = nil
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

`LoadImageBytes(req)` follows the same pipeline and state reset for browser URL drops. `LoadImageFromClipboard()` reads clipboard pixels inside the backend; on Windows it prefers direct DIBV5/DIB conversion so large images do not become JavaScript blobs or byte arrays.

**Frontend `loadFile(filePath, autoDetect)` / `loadImageFromBytes(...)` flow:**

```
setLoading(true)
setZoom(1)
LoadImage(...), LoadImageBytes(...), or LoadImageFromClipboard()
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

**File dialog defaults:** `OpenImageDialog` and `OpenSaveDialog` derive their default directory from the last loaded file path. Before passing the directory to Wails, they check `os.Stat(dir)` - if the directory no longer exists (e.g. a removable drive), they fall back to an empty string (system default) rather than erroring.

---

## Corner Mode (`app_corner.go`)

### Corner Detection

```
DetectCorners(req)
    1. Downsample to ~1500px, producing accent/CLAHE and raw grayscale in one source traversal
    2. Apply complementary 230..255 and 240..255 highlight curves plus a perimeter-derived adaptive
       highlight curve; these suppress coloured/text detail while expanding media/background boundaries
    3. Estimate median scanner RGB and its normal variation from the outer perimeter, then create a
       colour-distance silhouette which suppresses textured grey/coloured backgrounds; run a small
       Shi-Tomasi pass on it and retain both localizations when the measured perimeter is dark
    4. Boundary-line proposal path over grayscale, highlight, and colour-distance maps:
       oriented Sobel/Hough voting -> finite supported segments -> merge
       collinear lines -> near-endpoint intersections -> convex, area, and interior-angle quad validation
    5. Weighted Shi-Tomasi (goodFeaturesToTrack): two highlight boundary passes at scale 1 plus
       regular detail/silhouette passes at scales [1, 2, 4, 16]
    6. Refine scale-16 silhouette coordinates against fine highlight candidates within one coarse cell
    7. Refine all except authoritative 240-highlight coordinates against block-11 raw corners within one coarse cell,
       then sharpen against block-5 raw corners within half a cell
    8. Append line-derived proposals after point candidates so deduplication preserves established point localization
    9. Deduplicate corners, map back to full-resolution coordinates, and store in detectedCorners
    10. Return clean currentImage preview + Corners array
         Dots are rendered by the frontend canvas overlay - never baked into the image
```

The detector selects `dark-background` when the perimeter model is both deeply
dark (luma at most 96) and sufficiently uniform (95th-percentile RGB-distance
noise at most 48). That profile adds a Gaussian-smoothed, sensitive RGB-distance
silhouette for dark paper while reducing the irrelevant severe-highlight
candidate budget. Medium-gray and irregular/image-filled perimeters stay on the
established path. Dark-profile-only point and line candidates that are neither
on silhouette foreground nor across a compact silhouette boundary are rejected
as scanner-bed texture. Established proposal sources remain untouched, so this
decluttering cannot remove a candidate from the standard detector. Profile
selection and filter counts are logged with each detection.

### Clicking Corners

```
ClickCorner(req)
    1. If not custom && detectedCorners exist:
           snap pt to nearest detected corner only when it is within the image-scaled bounded snap radius;
           otherwise retain the raw click coordinate
       Else (custom=true OR Ctrl+click):
           use raw click coordinate
    2. Append pt to selectedCorners
    3. If selectedCorners.length < 4:
           return SnappedX/SnappedY/Count/Message only - NO preview
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
    return clean currentImage preview + Corners + "Detected N corners - click 4 corners"

SkipCrop()
    require currentImage != nil
    warpedImage     = cloneImage(currentImage)
    selectedCorners = nil
    return dims + "Crop skipped - image ready to save"
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

Promotes the current output image to a new source for a second crop pass. The frontend shows a `ConfirmationModal` before calling this because it is irreversible within the session. On confirmation it resets zoom to fit view before the backend call, while preserving `fitWidth` until the replacement preview is presented.

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

## Disc Mode (`app_disc.go` / `app_disc_edit.go` / `app_adjust.go`)

Disc mode is the most stateful mode. Every re-render replays the full pipeline from `discBaseImage`.

### DrawDisc - Entry Point

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

### redrawDisc - The Full Disc Pipeline

```
redrawDisc()
    1. src = discBaseImage (or originalImage as emergency fallback)
    2. bbox = [discCenter ± discRadius]
    3. cropped = subImage(src, bbox)
    4. localCenter = discCenter − bbox.Min
    5. applyCircularMaskWithFeather(cropped, localCenter, discRadius, featherSize, bgColor)
         for each pixel: distance d to center
           effectiveFeather = min(featherSize, radius)
           d <= radius-effectiveFeather: alpha = 1.0 (opaque)
           d >= radius:              alpha = 0.0 (transparent, filled with bgColor)
           in between:               cosine interpolation
    6. if rotationAngle != 0:
           rotateArbitrary(feathered, rotationAngle, bgColor)
    7. if postDiscBlack != 0 OR postDiscWhite != 255:
           applyLevels(feathered, postDiscBlack, postDiscWhite)
    8. crop discCropInsets (edge crops / Trim borders)
    9. resize by discScale (output scale from Resize), if set
    10. warpedImage = result
    11. levelsBaseImage = nil
    12. Return preview
```

### Post-disc edits

While a disc is live (`discEditing()`), nothing writes `warpedImage` directly: the next redraw would discard it. Every post-disc operation either changes a redraw parameter or edits the source (`app_disc_edit.go`):

- **Source edits.** Dust removal, Descreen, Touch-up, and selection-scoped Levels/Auto contrast write an edited copy into `discBaseImage` (`installDiscSource`), then redraw. Shift, rotation, radius and feather therefore keep working afterwards, and edits move with the disc art.
  - Whole-image edits process `discEditRegion()`: the crop square plus one disc radius on every side. A later shift beyond that margin shows unedited source.
  - Result-space selections and touch-up strokes are mapped into source space with `discSourceMap` (the inverse of crop → rotate → inset crop → output scale). A selection becomes a rotated rectangle: `discSelectionInSource` returns its bounding box and a per-pixel inside test.
  - Descreen keeps its non-stacking session (`descreenBaseImage` / `descreenResultImage` hold disc sources) across its own redraws, and doesn't report `DescreenReset`. Selection-scoped slider Levels keep `levelsBaseImage` (a disc source) across ticks.
  - A disc touch-up fills `discBaseImage` (`touchupSource`), and its done event carries the redraw's preview.
- **Parameters.**
  - 90° `Rotate` adds ±90° to `rotationAngle` and rotates `discCropInsets` and `discScale` with it. `imageops.Rotate` pivots on pixel index w/2, so this can land one pixel off an exact flip.
  - `TrimBorders` and `Crop` add to `discCropInsets`. Amounts are converted from output pixels through `discScale`, and `Crop` redraws when a scale is set.
  - `ResizeImage` sets `discScale` relative to the unscaled size (`discPreScaleSize`). It is applied last on every redraw.
- History snapshots `discBaseImage` by tile and records `discScale`, so undo restores source edits and parameters together.

### Operations that trigger redrawDisc

- `RotateDisc(angle)` - pushes an undo step (shared with the unbroken nudge run when `Nudge` is set, as for Q/E), adds angle to rotationAngle, calls redrawDisc. Each Shift+drag release is its own step.
- `ShiftDisc(dx, dy)` - pushes an undo step (unless the centre is clamped unchanged), adjusts discCenter, calls redrawDisc. With `Nudge` set (arrow keys), an unbroken run of nudges shares one undo step: a nudge reuses the run's step while `warpedImage` and the undo depth are still what the previous nudge left (`nudgeRun`), so any other operation in between starts a new step. Each Ctrl+drag release is its own step.
- `SetFeatherRadius(radius)` - updates discRadius and crop dimensions, calls redrawDisc
- `SetFeatherSize(size)` - updates featherSize, calls redrawDisc if discRadius > 0
- `GetPixelColor(x, y)` - sets bgColor from discBaseImage pixel, calls redrawDisc
- `SetLevels(...)` - stores values in postDiscBlack/White, calls redrawDisc
- `AutoContrast()` - computes + stores values in postDiscBlack/White, calls redrawDisc

### Live Ctrl+Drag shift preview

During a disc `Ctrl+Drag` (shift) or `Shift+Drag` (rotate) the image content moves while the vignette (feather, cutout, background fill) stays put, so the release does not visibly change the viewport. The vignette is rotationally symmetric about the rotation pivot (the crop centre, which is the mask centre), so the same overlay serves both.

- `DiscShiftPreview()` (`app_disc.go`) is read-only. It returns `discBaseImage` as a preview asset plus the exact geometry `redrawDisc` uses: the crop pivot in source and result space, the mask centre, rotation, radius/feather/cutout, post-disc levels, and the levels-adjusted background colour. **Any change to the `redrawDisc` pipeline (crop rect, rotation pivot, inset crop, mask, levels order, output scale) must be mirrored there.** It shares `discSourceMap` with the post-disc edits. `TestDiscShiftPreviewMatchesCommittedShift` enforces this. Its pivot, mask centre and size are in unscaled render pixels; the UI maps them to the stage per axis, which also covers an output scale. Post-disc edits live in `discBaseImage`, so the preview stays available after them.
- `ui/disc_shift_live.go` builds a `discShiftLiveLayer`. It holds a content raster of the unmasked source, downsampled to stage scale and covering the disc plus the per-gesture shift bound (`discShiftMaxShift`, enforced through `computeDiscShift`'s `maxShift`). It also holds a vignette overlay rendered at screen resolution with `imageops.CircularMask`. The content is drawn through `Rotate(S − shift − SourcePivot, Rotation + angle) + Pivot`, and the overlay stays fixed. Pending changes are a `discLivePose` (centre shift plus extra angle). `limitGesture` bounds shifts by magnitude and then uses `covers` to stop any gesture where the visible part of the disc would need source pixels outside the raster. This only bites for large rotations while zoomed in, since the raster then covers just the visible area plus a margin.
- Gio composites in linear light, but the committed mask blends in gamma space. Feather pixels are therefore tuned for the content under them at press time (`vignetteTranslucent`). On release they are replaced with opaque, exactly composited colours for the final shift, rendered off the UI thread (`refreshDiscShiftOverlay`). `TestDiscShiftLiveGPUMatchesReference` checks press, drag and release on the headless GPU.
- The lifecycle lives in `ui/disc_shift_session.go`. One layer serves a whole chain of drags: the content raster doesn't depend on the disc centre, so each release adds its shift or rotation to `base` and queues a `ShiftDisc` / `RotateDisc`. Shifts and rotations can be mixed within one run of drags. A press while commits are still in flight continues on the same layer, limited so the total pose stays inside the raster (`limitGesture`). Shift-to-source mapping uses the rotation the screen shows, including rotations still in flight (`liveDiscRotation`).
- `Shift+Drag` rotation follows the pointer's angle around the rotation pivot (`discRotateDragState`): the angle is unwrapped across ±180°, so full turns accumulate. Within a 12 dp dead zone round the pivot the gesture pauses, and it re-anchors on leaving rather than flipping. A guide line from the pivot to the pointer shows the gesture angle (`discRotateGuide`). The layer hides only when nothing is dragging or pending and the last commit's render is presented. It then stays idle for reuse until the presented render changes for any other reason. If the backend clamps a shift differently, or a foreign render appears mid-chain, the layer is marked stale and dropped once the chain settles. Cancelling a gesture (`cancelActiveCanvasDrag`) keeps the chain; real transitions (`beginTransition`) and resets drop it.
- `maybeWarmDiscShift` prebuilds a layer about 200 ms after the presented source or the viewport layout settles. It only does so when the idle layer can't serve the next press (layout changed, or more than half the shift range used up). A press normally starts instantly. On a miss the layer is built on demand, and the whole-raster translate shows until it arrives.
- While the layer is showing, the viewport builds replacement rasters asynchronously (`PreferAsyncRaster`), so a commit landing mid-drag doesn't block a frame.

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
| `normalCropApplied` | `true` after first crop or Skip crop - unlocks touch-up |
| `adjustmentRect` | Persistent image-space rectangle limiting adjustment effects, or `null` |
| `adjustmentSelectionActive` | Whether the Adjustments marquee tool is active |

`normalRect` is purely frontend; never sent to Go until the user clicks **Crop**.

---

## Adjustments (`app_adjust.go`)

The Adjustments header exposes a rectangular marquee after the crop phase is
complete. Its rectangle stays in frontend image coordinates and is included as
an optional `selection` request field for Auto Contrast, Levels, Descreen, Dust
Removal, and touch-up. Each backend path clamps the rectangle to the current
working image and composites processed pixels only inside it. Resize Image and
Trim Borders remain whole-image operations and clear the frontend rectangle
because they change image geometry. The marquee remains after ordinary pixel
adjustments; Ctrl+D clears it without leaving the tool, while toggling the
header icon off clears it and deactivates the tool.

### Crop / Rotate / Resize / TrimBorders

```
Crop(req)          validate edge and requested pixel amount; clamp to current bounds leaving at least 1 pixel; saveUndo only if changed; crop; return preview
Rotate(req)        require warpedImage; saveUndo(); rotate90(flipCode 0=CCW,1=CW,2=180); return preview
ResizeImage(req)   require image loaded; saveUndo(); resize workingImage; setWorkingImage(result)
TrimBorders(req)   require warpedImage; saveUndo(); crop current bounds; return preview
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

`applyDescreen` uses a pure-Go, single-precision real FFT specialized for this
filter. Dimensions are padded to the next 2/3/5-smooth lengths (and an even
width), not independently to powers of two. For example, 5100×7020 becomes
5120×7200 rather than 8192×8192. `fftPlan32` caches the mixed-radix schedule,
digit-reversal permutation, and twiddles; the row real transform stores only
`width/2+1` canonical bins, followed by cache-blocked complex column
transforms. Channels run sequentially with all available workers so their large
frequency planes never compete for memory bandwidth.

When the optional fast mode is enabled, `applyDescreenLuminance` transforms and
filters a single Rec. 601 luminance plane. The filtered luminance delta is added
back to each RGB channel, preserving source chroma and alpha while avoiding the
other two channel FFT/mask passes. It may leave screen patterns that exist only
in chroma, so the independent RGB path remains the default.

The spectrum remains in natural FFT order. Threshold-mask construction maps
frequency coordinates into the legacy centered layout without physically
shifting the complex plane, and replaces per-bin `sqrt`/`log` evaluation with
an equivalent squared-magnitude comparison. The full centered binary mask is
retained so radius semantics stay compatible; square dilation uses running
binary counts in O(width×height), then the existing separable Gaussian is
applied. Inverse rows write each channel directly into the destination image,
avoiding three full-size float result planes. The test-only
`internal/descreen/legacy_test.go` keeps `applyDescreenLegacy` as
a test oracle for same-padding numerical comparisons and is not called by the
application.

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

### Undo and redo

`Undo()` and `Redo()` use the same `stepHistory` path. Before restoring a
snapshot, it captures the current state onto the opposite stack. Redo restores
exact pixels without rerunning the original processing operation. Both directions
cancel an in-flight touch-up without consuming history on that keypress.

Each snapshot owns immutable shared tiles plus pre-warp/corner state and disc
rendering metadata. Disc snapshots include a tiled source image so redoing the
initial disc crop also restores the source needed for later shifts and rotations.
Derived disc crop caches and unmasked preview URLs are rebuilt on restoration.
Levels/descreen sessions are cleared in both directions.

Undo and redo share a total cap of 100 steps and 1024 MiB, counting shared tiles
once across both stacks and disc sources. Eviction removes farthest entries,
preferring to keep the immediate reverse step, even when that single snapshot
exceeds the budget. Snapshots are independently restorable; eviction cannot break
a delta chain. Removed slots are cleared to release their tile references. The
budget excludes live images, previews, and transient restoration allocations.

A new committing edit clears redo through `saveUndo`. Non-committing levels,
disc renders, and new backend selections also clear redo. Document loads,
re-crops, compositor loads, and mode resets clear both stacks so another mode
cannot restore an incompatible crop. In-progress corner-pick undo retains its
existing behavior and clears image redo when the selection changes.

Ctrl+Z invokes undo; Ctrl+Y (also Cmd+Y) invokes redo before disc eyedropper
handling. Editable fields retain native text history. Repeated keydown events
are suppressed, as are history actions during disc drags or optimistic crops.
The action hook serializes history requests. It restores crop-phase controls,
disc geometry/parameters, and adjustment controls; syncing disc settings avoids
triggering a redraw that would invalidate redo. Empty history and touch-up
cancellation do not mark the document modified.

### Dust Removal (`app_dust.go`, `internal/dust/`)

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

## Touch-Up (`app_touchup.go`, `app_iopaint.go`, `internal/patchmatch/`)

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
        preview = imagePreviewURL(out)     ← authoritative immutable revision
        saveUndo(); setWorkingImage(out)
        if changed bounds <= 512×512 pixels:
            encode mask-covered output pixels plus transparent edge padding as a lossless PNG
        EventsEmit("touchup-done", {preview, patch, message, width, height, descreenReset})
```

Every touch-up registers a normal immutable preview revision, preserving the
same authoritative fallback and revision semantics as other edits. For normal
brush-sized changes, the completion event also carries the changed pixels on a
transparent, padded lossless PNG. Keeping untouched pixels transparent avoids
exposing the quality boundary between the PNG and the viewport JPEG. Fully
covered brush pixels are opaque; antialiased edge pixels retain their brush
coverage so a nearly untouched lossless pixel cannot trace a bright contour
over the lossy viewport.
`PreviewCanvas` decodes that small patch, retains
the already-decoded viewport bitmap, adds the patch to its raster-local overlay
list, and promotes the raster's cache key/source to the new revision. Rapid
touch-ups accumulate on that promoted raster, so they remain visible without
re-rendering or transporting the unchanged viewport. A later pan/zoom fetches
an authoritative raster for the current revision as usual. If the patch is
absent, invalid, cannot be chained from the active revision, or exceeds the
512×512-pixel cap, the normal viewport RPC path is used immediately.

### PatchMatch (`internal/patchmatch/`; used via `patchMatchChunkedFill`)

Both PatchMatch call sites (`patchMatchChunkedFillLogged` and `applyWarpFill`)
invoke the solver through the `patchMatchFill` variable in `app_touchup.go`,
which defaults to `patchmatch.Fill`. The indirection exists so an untracked
local file can substitute another engine at init time for side-by-side
comparison; `patchMatchEngine` names the active engine in the touch-up logs.

The built-in touch-up backend is a deterministic, translation-only,
coarse-to-fine synthesizer content-aware fill engine. A brush stroke is
solved inside a bounded working neighbourhood rather than preprocessing the
full scan, while the source search domain inside that neighbourhood remains
much larger than the painted region.

**Public solver entry points:**

```go
patchmatch.Fill(ctx, src, mask, patchSize, iterations)
    // Discovers mask bounds if necessary and returns a
    // full-size image.

patchmatch.FillBounds(ctx, src, mask, dirtyBounds, patchSize, iterations)
    // Same full-size result, but accepts the already-known brush bounds and
    // avoids scanning the full mask to rediscover them.

patchmatch.FillROI(ctx, src, mask, dirtyBounds, patchSize, iterations)
    // Lowest-latency API. Returns a zero-origin local image plus workBounds;
    // the caller may composite that ROI directly into a document/tile buffer.
```

`dirtyBounds` is expressed relative to the top-left of `src` and must contain
every non-zero mask pixel. `patchmatch.FillROI` budgets a roughly square source
region with nominal side `ceil(4 * sqrt(max(50, maskWidth) * max(50, maskHeight)))`,
expanded if necessary to contain the mask plus `patchSize + 8` pixels of support
on each side. The window shifts inward at document edges to retain context, then
clips to the image dimensions. All pyramids, planes, validity maps, field state
and vote buffers use these local coordinates. The app's `patchMatchChunkedFill`
also applies its existing outer crop and connected-region grouping before
entering this solver.

`patchSize` and `iterations` are caller hints rather than solver knobs. The
engine always matches 7x7 patches (`synthesisPatchSize`) and runs a fixed
per-level round schedule, so `patchSize` survives only as the ROI halo term
above and `iterations` is ignored entirely. Both remain in the signatures
because the app, its settings, and the replay harnesses pass them.

**Pyramid**

`synthesisPyramid` steps the scale by 0.7 from full resolution while the shorter
side stays at least `minCoarse` (35 px by default), adding a final level clamped
to exactly `minCoarse` when the next step would fall below it. Every level is
resampled from full resolution rather than from its parent, which is why the
pyramid is the most expensive single stage of a small dab.

Each level carries four images:

* `src` - a plain area-reduced source, sampled by patch costs and by the vote;
* `plane` - the opaque form of `src` handed to the SIMD cost kernel;
* `seed` - the masked, normalised target image the level's rounds start from,
  built by `synthesisChainPlane`: a colour plane with a per-pixel validity
  weight that is area-reduced and Gaussian-blurred (`synthesisPlaneSigma`,
  0.55 per 0.7 step), so the result is the known content's average wherever
  known pixels reach and black elsewhere;
* `mask` / `targetMask` - two different hole semantics.

**Mask semantics**

* `targetMask` is the conservative coverage mask (`synthesisAreaMask`) and
  drives the target/source classification. A centre whose 7x7 patch overlaps it
  is a target; a centre whose patch overlaps neither it nor the exclusion mask
  is a valid source. There is no centre-only validity fallback, so damaged
  content cannot re-enter the fill.
* `mask` is the point-sampled hole (`synthesisPointMask`) and decides which
  pixels the vote overwrites. Every covered pixel is replaced outright: the
  engine never blends the hole's own pixels back in, so a feathered brush edge
  cannot leave a rim of the removed content.

**Onion-peel pre-heal (`internal/patchmatch/reconstruct.go`)**

Holes that touch the image border are pre-healed before the ordinary pyramid
runs. `synthesisPeelScale` picks a power-of-two coarse scale from the hole depth
(bounded by `peelMaxCoarseSize` and `peelMaxDimension`), and that scale also
becomes the coarsest pyramid level. The hole is then grown inward from the known
content in 2 px rings; each ring gets a diffused initial guess
(`synthesisDiffuseFill`) and a short single-level solve before the next ring is
taken. Peel rings use a flat hole search window of `10 + 4*i` with no depth
ramp. The pre-healed coarse image seeds the first active level's target, but its
field is not inherited: a top band's first main dispatch starts from a uniformly
random field even where the peel already solved those rows.

**Hole search window**

`synthesisSearchWindow` reproduces the engine's automatic half-window from the
hole's pixel count and its depth - the L1 `maxDist` for interior holes, half of
it for holes touching the border. `synthesisWindowPlane` turns that scalar into
the per-pixel window plane the engine keeps alongside its state: the half-window
scaled into the level, plus each pixel's Manhattan distance into the hole, so
centres deep inside the hole may search farther than the boundary does.

The window is a rejection test, not a draw-range clip. Random search draws from
`best ± radius` clipped only to the valid rect, and `synthesisTry` then rejects
any candidate at or beyond the window. Narrowing the draw range instead would
change the modulo and therefore the drawn coordinate.

**Per-level round schedule**

```text
for level = coarse -> fine
        |
        +-- seed the working image (source, peel result, or parent vote)
        +-- upscale the parent field by displacement, refresh costs, vote
        |
        +-- round 0..N            (N = 30 on the first and penultimate
        |      |                   active levels, 25 otherwise)
        |      +-- solve a fresh restart field: uniform random sources
        |      |   refined by one primary dispatch
        |      +-- merge dispatch at radius 1 keeps the cheaper of the
        |      |   restart entry and the incumbent, pixel by pixel
        |      +-- coherence-weighted patch vote
        |
        +-- carry the working image and field to the next finer level
```

The field persists across rounds; what restarts is the proposal. Only the very
first round of a level with no inherited field starts from scratch. An inherited
field is never random-searched at the primary radius itself.

The primary dispatch spans the whole level on the early active levels and drops
to `synthesisFineRadius` (3) on the last three; the merge dispatch always uses
`synthesisSecondaryRadius` (1). The last ordinary levels additionally freeze the
interiors of coherent blocks of the incumbent (`synthesisFrozen`). The finest
level, when it inherits a field, runs a single round with no proposal at all:
one propagation dispatch, then a vote.

**Search kernel and RNG**

`synthesisSearchPixel` is one invocation of the engine's kernel: the two axial
predecessors of the current pass, then random search around the current best
with the radius halving to zero. `synthesisSearchSweep` runs the level as a
tiled anti-diagonal wavefront - the dependency graph allows it, and tiling
rather than per-pixel scheduling keeps the 7x7 column reuse in cache and the
barrier count low. Small or narrow levels stay serial.

**Patch cost (`internal/patchmatch/cost.go`)**

The cost is a raw RGB sum of squared differences over the 7x7 patch, with an
early exit once the running sum reaches the incumbent's cost.

On AVX2/FMA machines `pmPatchSSD7OpaqueAVX2` computes the same SSD in float32
(exact below 2^24) with the same early exit. `pmOpaqueKernelArgs` is the ABI
shared with that kernel, and `ActiveKernel()` reports which path was selected
for the touch-up logs. Other architectures use the scalar loop.

**Vote (`synthesisVote`)**

Every target centre whose patch covers a hole pixel contributes its matched
source colour, weighted by `synthesisCoherence` - a 0.1/0.2/0.3/0.6/1 table
keyed on how many of the four diagonal neighbours carry the same displacement.
The weighted average is written to the covered pixel; uncovered pixels keep
byte-for-byte source content.

**Performance and cancellation invariants**

* all expensive processing operates on the working ROI rather than the full scan;
* the field exists only over `level.active`, the centres capable of affecting
  painted output;
* field, cost and coherence storage is reused within a level, and the restart
  buffers are released as soon as the level collapses into the parent solution,
  so coarse levels do not retain a second field for the rest of the fill;
* `parallelRowsSized` applies a size threshold, so small dabs stay serial and
  avoid goroutine/`WaitGroup` overhead;
* resampling, search sweeps and voting are parallel; the wavefront synchronises
  once per tile diagonal rather than once per pixel;
* `ctx.Err()` is checked at entry and throughout pyramid, peel, round and vote
  work so an in-flight touch-up can be cancelled.

`patchmatch.FillBounds` is preferred when the caller already knows the stroke
bounds. `patchmatch.FillROI` is preferred when the editor can composite the
returned rectangle itself, because it also avoids the final full-document clone.

Regression coverage is behavioural rather than component-level, because the
solver no longer has separable photometric, structure or texture components:
basic defect completion, supplied-bounds equivalence, local working-ROI
behaviour, working-ROI context at document edges, repeated printing through thin
scratches and larger dabs, transparent edge outpainting with nonzero image
origins, flat colour and known-pixel preservation, stochastic texture retention,
texture beside a crossing edge, sharp slanted colour-edge preservation, the RNG
known-answer vector, the `parallelRowsSized` scheduling and cancellation
contract, and a logged real-scan replay. Architecture-specific tests verify the
AVX2 SSD kernel's equivalence to the scalar loop, its early exit, and the
`pmOpaqueKernelArgs` assembly layout.

### iopaintFill (`app_iopaint.go`)

**Does not send the full image.** Crops to the bounding box of the mask plus a 128 px margin, encodes the crop as JPEG (fast; iopaint does not need lossless input), sends it with the cropped grayscale PNG mask to `{iopaintURL}/api/v1/inpaint`. On success, composites only the masked pixels from the response patch back onto a full clone of the source image and returns a full-size `*image.NRGBA`.

**Outpaint (warp fill) always uses PatchMatch** - IOPaint is an inpainting model and produces black for outpainting.

---

## Mode Switching

### The Four Modes Are Mutually Exclusive

Switching modes always resets the warp result. `setMode(m)` is called at the **top** of `handleModeSwitch` - before any async operations - so the mode button updates immediately.

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

Max Corners and Min Distance are persistent manual settings and are reused across image loads and application restarts by default. When `autoCornerParams` is enabled in Options, image-derived suggestions replace the visible controls only for each new load; they do not overwrite the saved manual values. Turning the option off restores those manual values. Pressing **Detect** or automatically detecting after returning to Corner mode uses the values currently shown in the controls.

---

## Internal Image Processing Packages

State-free processing code lives below `internal/` and is grouped by domain:
`imageops` owns adjustments and geometric transforms; `raster` owns conversion,
resizing, and pixel SIMD kernels; `cornerdetect` owns point/line feature
detection and its scalar/SIMD kernels; `geometry` owns shared math; `descreen`
and `dust` own their respective processing engines. Architecture variants use
the conventional `<feature>_<arch>.go` / `.s` suffixes within their package.

### applyWarpFill (`app_corner.go`)

```
applyWarpFill(img, oobMask)
    if no OOB pixels → return img unchanged (fast path)
    if warpFillMode == "outpaint":
        patchMatchFill(img, oobMask, patchSize=9, iterations=5)   // defaults to patchmatch.Fill
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

ResolvePreviewImage(previewURL) / ResolveCalibrationImage(previewURL)
    look up the immutable *image.NRGBA pointer directly in-process (zero JPEG round-trip)
```

`viewportState` (`ui/viewport.go`) converts the scroller viewport through the logical stage geometry into a quantized, overscanned image-space source rectangle, asks the single-flight background worker to downsample only that visible region (`downsampleViewportRegion`), uploads the resulting `paint.ImageOp` into a 6-entry LRU cache, and composites it on the GPU. While panning or zooming, cached covering/neighboring rasters stay visible until the higher-density replacement completes. Disc live drag transforms the active raster via a GPU `f32.Affine2D` matrix while keeping the backend authoritative for final commit.

---

## UI Coordinate System

`viewportState.computeLayout` computes the exact stage rectangle (`StageX`, `StageY`, `StageWidth`, `StageHeight`) in device pixels from `fitWidthFor` and `Zoom`.

Interaction code converts display coordinates inside the stage back to source image coordinates via `displayToImage(dispX, dispY)`:

```go
displayToImage(dispX, dispY, dims, stageW, stageH)
    x = round(dispX * (dims.X / stageW))
    y = round(dispY * (dims.Y / stageH))
```

All vector guides (detected/selected corners, touch-up strokes/cursor, line endpoints, Normal marquee, adjustment selection marquee, disc circle/cutout, straight-edge preview, and compositor overlay stroke) are drawn directly by `drawCanvasGuides` using `stagePointFromImage`, so pointer hit testing and rendered overlays share one exact transform.

In line mode, `e.lineStartImg` captures the start point in image space on pointer press so wheel-zooming mid-drag does not distort the endpoint.

---

## Zoom and Fit Width (`ui/viewport.go`)

```go
fitWidth = min(vpW, vpH * aspectRatio)
displayWidth = fitWidth * zoom
displayHeight = displayWidth / aspectRatio
```

`maxUsefulZoom` allows at least `5x` zoom, or up to 2 display pixels per source pixel for large images, capped at `40x`. Wheel zooming anchors the cursor's image-space point under the pointer before adjusting `ScrollX` / `ScrollY`. Holding Space while dragging (or middle-mouse dragging) pans `ScrollX` / `ScrollY`. Custom scrollbars drawn after `canvasTag` allow dragging the horizontal and vertical thumbs directly.

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
    3. If CLI or settings post-save command is configured, run RunPostSaveCommand
    4. If CLI --post-save-exit or CloseAfterSave setting is enabled, close window
    5. Return "Saved to {path}"
```

**Only `warpedImage` is saved.** A user who has only done pre-warp adjustments will still get a save because `setWorkingImage` always writes to `warpedImage`.

---

## Host Bridge (`app_host.go`, `ui/host.go`)

`App` never imports a GUI toolkit directly in core backend files. Native OS dialogs, window title updates, quit notifications, external URL launches, and `touchup-done` notifications flow through the UI-free `AppHost` interface (`internal/appapi/types.go`, wrapped by `app_host.go`), implemented at runtime by `gioHost` (`ui/host.go`) via Gio and `github.com/ncruces/zenity`. In tests where `a.host` is nil, helpers fall back cleanly.

---

## Error Handling

- **Recoverable errors** (invalid state, bad args): Go returns `(nil, error)` → `EditorUI` displays `ErrorModal`
- **Touch-up failure**: `ErrorModal` with user-friendly message; if IOPaint backend, includes a hint to check the server
- **WASDQE before crop**: guarded in `ui/shortcuts.go` — calls `showStatus("Apply a crop first before adjusting")` instead of forwarding to Go
- **Destructive confirmation** (unsaved close, unsaved load discard, large resize upscale, dirty calibration close): `ConfirmationModal` before discarding state
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
| Crop-edge key amount | `cropEdgePixels` | `CropRequest.amount` | Integer 1–1000 (default `3`) |
| Disc centre cutout | `discCenterCutout` | `discCenterCutout` | `true` / `false` (default `true`) |
| Disc cutout size | `discCutoutPercent` | `discCutoutPercent` | Integer 0–50 (default `11`) |
| Manual maximum corners | `cornerMaxCorners` | *(frontend-only)* | `1`–`1000` (default `500`) |
| Manual minimum corner distance | `cornerMinDistance` | *(frontend-only)* | `1`–`200` (default `100`) |
| Auto-adjust corner params | `autoCornerParams` | *(frontend-only)* | `true` / `false` (default `false`) |
| Close after save | `closeAfterSave` | *(frontend-only)* | `true` / `false` (default `false`) |
| Post-save enabled | `postSaveEnabled` | *(frontend-only)* | `true` / `false` (default `false`) |
| Post-save command | `postSaveCommand` | *(frontend-only)* | Any string |
| Touch-up brush remains active | `touchupRemainsActive` | *(frontend-only)* | `true` / `false` (default `true`) |
| Straight edge remains active | `straightEdgeRemainsActive` | *(frontend-only)* | `true` / `false` (default `true`) |
| Auto-detect corners on mode switch | `autoDetectOnModeSwitch` | *(frontend-only)* | `true` / `false` (default `true`) |

`closeAfterSave` has no Go counterpart - consumed entirely in the frontend `handleSaveImage` handler.

---

## Image Compositor (`internal/compositor/`, `app_compositor.go`)

A standalone planar image compositor with two independent modes. Segment stitching remains the default. Repeated-scan overlay aligns rotated views of the same object to the first scan. Painted regions take pixels from the selected later scan; the unpainted output remains from the first scan. `internal/compositor` has no dependency on app state.

### App struct fields

| Field | Type | Purpose |
|-------|------|---------|
| `compositorResult` | `*image.NRGBA` | Cached full-resolution stitch output. Nil until a successful `CompositorStitch`. |
| `compositorOverlay` | `*compositor.OverlaySession` | Aligned repeated-scan layers and editable mask strokes. The full-resolution result is rendered when loaded or saved. |
| `compositorMu` | `sync.Mutex` | Protects `compositorResult`. |

### UI-facing methods (`app_compositor.go`)

- **`CompositorOpenFilesDialog()`** - multi-file picker returning selected paths
- **`CompositorStitch(req)`** - decodes all images, reverses path order for `"rtl"`/`"btt"` orientations, runs `stitchImages`, caches result, returns preview + dimensions
- **`CompositorAlignOverlay(req)`** - decodes scans, searches quarter-turn orientations for each later scan, registers each to the first scan, caches an editable overlay session, and returns a reduced preview
- **`CompositorAddMask(req)`**, **`CompositorUndoMask()`**, **`CompositorClearMasks()`** - edit normalized brush strokes on layers above the reference scan and rerender the preview
- **`CompositorRefreshPreview()`** - republishes the cached stitch or overlay preview when reopening the modal after a document load reset invalidated its previous preview URL
- **`CompositorLoadResult()`** - promotes the cached result into the main editing pipeline (full state reset, sets `originalImage`/`currentImage`, returns `ImageInfo`)
- **`CompositorSave(req)`** - encodes cached result to disk
- **`CompositorOpenSaveDialog()`** - save-file dialog

### Stitching pipeline (`internal/compositor/stitch.go`)

`stitchImages(imgs)`: detect Shi-Tomasi features on each image → match with SSD + Lowe's ratio test (threshold 0.75) → estimate homography via RANSAC (2000 iterations, 15 px inlier threshold) with DLT + Hartley normalisation → compose homographies so image[0] is the reference frame → render all images into a single canvas with distance-to-border feathering and bilinear interpolation (parallelised by row).

### Repeated-scan overlay (`internal/compositor/overlay.go`)

`AlignOverlay` reuses the stitch feature matcher and homography estimator but tests 0°, 90°, 180°, and 270° for every scan against the first. The first scan fixes output dimensions and supplies every unpainted pixel. A brush stroke is stored in normalized output coordinates for one later scan; its mask reveals that scan in the painted area. Each stroke stores a feather fraction of its radius: 0 makes a hard edge, while higher values linearly blend a wider rim (the UI defaults to 20%). If strokes overlap, later scans in the list are on top. Preview renders are limited to 1400 px on the longest side and fit within the modal's available preview area, preserving the source aspect ratio and brush coordinates. Load/save render from full-resolution sources. Mask edits invalidate the cached full-resolution result.

### UI flow (`ui/compositor.go`)

`compositorModalState.handleLoadOutput` calls `CompositorLoadResult`, closes `compositorOpen`, resets UI image state, and feeds the returned `ImageInfo` into `EditorUI.applyLoadedImageInfo` (updating image metadata, auto corner params if enabled, and detecting corners when in Corner mode).

Closing the compositor modal drops the modal without discarding backend alignment, masks, or paths. On reopening, `compositorModalState.open` calls `CompositorRefreshPreview()` to obtain a valid preview revision if a main document load reset invalidated the previous preview key.

---

## Corner Calibration (`app_calibration.go`)

Corner Calibration is a standalone ground-truth annotation tool opened from the
Debug tab in Options. A filesystem drop is routed to it while its modal is open, allowing
an ordered batch of scans to be reviewed without entering the normal image
pipeline.

The backend decodes one calibration scan at a time into
`calibrationPreviewAssets`, a dedicated one-revision `preview.Store`.
`CalibrationLoadImage` does not read or write `originalImage`, `currentImage`,
`warpedImage`, history, modes, or any adjustment state. The separate store is
important: reviewing many scans cannot evict the active document's preview.
`ResolveCalibrationImage` reads directly from `calibrationPreviewAssets`, keeping Fit, 25%, 50%, 100%, and 200% zoom annotation in full source-pixel coordinates without touching the main preview revision store.

Points are recorded in strict screen-clockwise semantic order: top-left,
top-right, bottom-right, bottom-left. Export writes schema-versioned JSON with
source dimensions, paths relative to the dataset file where possible, explicit
corner names, and integer source-image pixel coordinates. Backend validation
rejects missing, out-of-bounds, degenerate, reversed, and duplicate samples.
No scan pixels or sidecar files are modified during annotation; disk is written
only after the user chooses **Export JSON** and confirms a save path.

`app_corner_groundtruth_test.go` contains opt-in evaluators for the general and
dark-background corpora. They run the production `DetectCorners` request used
by the UI and score the nearest proposal to each labelled corner. Every fifth
ordered sample is reserved as a stable holdout, so detector parameters can be
chosen on the training scans and checked independently before acceptance. Full
runs enforce corpus and holdout accuracy gates; the dark evaluator also asserts
that every sample automatically selects the dark-background profile. Dataset,
split, index, and verbose diagnostic selection are controlled by
`ATROPOS_CORNER_GROUND_TRUTH*` environment variables; normal repository tests
skip the external corpora.


## State transition ownership

Document load/reset helpers cancel both touch-up and corner detection. Compositor
loads use `resetPipelineState`, including preview-store reset, adjustment selection
keys, descreen baselines, disc caches, and both history stacks. Corner detection
uses a captured source pointer and generation: only the newest, uncancelled job
may publish, and older cleanup cannot remove a newer cancellation handle.

Frontend transitions cancel detection and clear transient tool/drag/selection
state before awaiting backend work. Mode resets and file/clipboard loads share
a queue; mode buttons update immediately, but stale responses cannot replace the
latest preview or complete its busy state. History responses are also discarded
if the document or mode changed while they were pending. Loading, compositor
entry, and mode changes preserve fit geometry until replacement presentation;
they never clear it to zero.

A rejected fourth corner is removed without changing image history, leaving the
three valid picks available for correction. Edge cropping uses current dimensions
rather than accumulated offsets, so repeated crops and history traversal remain
consistent; invalid directions and one-pixel no-ops do not consume history.
Levels changes, disc redraws, and Skip Crop actually clear invalidated descreen
sessions as well as reporting `DescreenReset`.

Successful queued loads publish document state before the next load starts, even
when superseded. If a later load fails, the frontend retains the last successful
backend document; stale loads do not finish a newer request's busy state or start
auto-detection.

Before the first document is loaded, mode selection updates the mode preference
without invalidating an in-flight load. Clipboard success and failure both retain
ownership of their loading cleanup, and a successful load uses the selected mode.

History records `cropSkipped` separately from the pre-warp flag. Adjustments
preserve it, geometric crop commits clear it, and mode/document resets clear it.
Undo/redo return the recorded phase so Skip Crop controls stay disabled when
restoring an adjustment made without a geometric crop.
