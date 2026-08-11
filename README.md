# Atropos viewport-canvas rewrite — complete source files

This directory contains **complete replacement files and complete new files**, not a patch or diff, for the Atropos `master` layout inspected on 2026-08-11.

## Install into a checkout

The installer resolves files relative to itself, so it can be run from any working directory:

```sh
/path/to/atropos-rewrite/install.sh /path/to/atropos
```

Or copy the files manually while preserving their relative paths.

## Complete replacement files

- `AGENTS.md`
- `ARCHITECTURE.md`
- `preview_assets.go`
- `preview_assets_test.go`
- `frontend/src/App.jsx`
- `frontend/src/hooks/useZoomPan.js`
- `frontend/src/components/ImageOverlays.jsx`
- `frontend/src/components/StatusBar.jsx`

## New files

- `frontend/src/components/PreviewCanvas.jsx`
- `frontend/src/components/PreviewCanvas.css`
- `frontend/src/components/PreviewCanvas.test.jsx`

No `main.go` change is required: the existing Wails asset-server handler already delegates requests to `app.servePreviewAsset`.

`frontend/src/hooks/useProgressivePreview.js` is intentionally left in place. `App.jsx` no longer uses its low/full image-loading hook, but retains the presentation/session helpers so metadata remains frozen until the canvas has decoded and presented the matching immutable revision. Keeping the legacy hook avoids an unrelated removal and preserves compatibility with any secondary imports/tests.

## Backend protocol

A registered revision keeps its existing opaque URL:

```text
/__atropos/preview/<session>/<revision>.jpg
```

The canvas requests only the required source region and raster density:

```text
/__atropos/preview/<session>/<revision>.jpg?x=...&y=...&w=...&h=...&dw=...&dh=...&q=88
```

Coordinates are image-space pixels. `dw`/`dh` are the JPEG raster dimensions required for the current viewport and device-pixel ratio. Requests are bounded to 4096 pixels per dimension and 16 MiPixels. The client uses a 25% viewport overscan, 64-pixel source quantization, no intentional source upsampling, and a four-raster decode cache. The backend retains up to eight encoded viewport rasters per live revision. Because viewport revisions must retain source pixels for later pan/zoom requests, the outer revision store is capped at four live revisions (rather than relying on the old post-full-encode source release behavior).

The legacy full URL and `-low.jpg` URL continue to work.

## Verification performed

- `preview_assets.go` + `preview_assets_test.go`: `go test -race` passed in a standalone harness using the production `App.previewAssets`/`logf` interface.
- All changed/new JS and JSX files parsed successfully with the TypeScript JSX parser.
- `PreviewCanvas.css` parsed successfully with PostCSS.
- The exported viewport-request helper was executed directly and checked for overscan coverage, 64-pixel source quantization, no source upsampling, and raster-size limits.
- The bundled documentation was updated so the repository contract describes the canvas/viewport renderer rather than the retired visible-`<img>`/SVG preview model.

The execution sandbox could inspect GitHub source but could not clone the repository or install its frontend dependencies, so a repository-wide `go test ./...`, `npm run test:all`, and `npm run build` could not be run here. Run those commands in the real checkout after installing these files.
