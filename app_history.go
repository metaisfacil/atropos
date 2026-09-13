package main

import (
	"bytes"
	"image"
	"image/color"
)

const (
	undoTileSize           = 128
	defaultUndoLimit       = 100
	defaultUndoBytes int64 = 1024 << 20
)

// History owns immutable tiles, never live image buffers. Each snapshot is
// independently restorable; removing an older entry cannot break later ones.
type undoTile struct{ pixels []byte }
type undoImage struct {
	rect  image.Rectangle
	tiles []*undoTile
}

// Disc rendering needs its source as well as the visible result when redo
// crosses the initial crop. Cache rasters are rebuilt, never kept in history.
type historyDisc struct {
	base            *undoImage
	center          image.Point
	radius, feather int
	background      color.NRGBA
	cutout          bool
	cutoutPercent   int
}

func (a *App) captureHistory(previous undoEntry) undoEntry {
	angle := a.rotationAngle
	entry := undoEntry{image: snapshotUndoImage(a.workingImage(), previous.image),
		preWarp: a.warpedImage == nil, rotationAngle: &angle,
		postDiscBlack: a.postDiscBlack, postDiscWhite: a.postDiscWhite,
		selectedCorners: append([]image.Point(nil), a.selectedCorners...)}
	if a.discRadius > 0 {
		base := previous.image
		if previous.disc != nil {
			base = previous.disc.base
		}
		entry.disc = &historyDisc{base: snapshotUndoImage(a.discBaseImage, base),
			center: a.discCenter, radius: a.discRadius, feather: a.featherSize,
			background: a.bgColor, cutout: a.discCenterCutout, cutoutPercent: a.discCutoutPercent}
	}
	return entry
}

func (a *App) restoreHistoryDisc(disc *historyDisc, base *image.NRGBA, unmasked string) {
	a.discWorkingCrop = nil
	a.discWorkingCropRect = image.Rectangle{}
	a.discBaseImage, a.discNoMaskPreview = base, unmasked
	a.discCenter, a.discRadius = image.Point{}, 0
	if disc == nil {
		return
	}
	a.discCenter, a.discRadius, a.featherSize = disc.center, disc.radius, disc.feather
	a.bgColor, a.discCenterCutout, a.discCutoutPercent = disc.background, disc.cutout, disc.cutoutPercent
}

func snapshotUndoImage(img *image.NRGBA, previous *undoImage) *undoImage {
	if img == nil {
		return nil
	}
	s := &undoImage{rect: img.Bounds()}
	i := 0
	for y := s.rect.Min.Y; y < s.rect.Max.Y; y += undoTileSize {
		for x := s.rect.Min.X; x < s.rect.Max.X; x += undoTileSize {
			r := image.Rect(x, y, min(x+undoTileSize, s.rect.Max.X), min(y+undoTileSize, s.rect.Max.Y))
			var old *undoTile
			if previous != nil && previous.rect == s.rect {
				old = previous.tiles[i]
			}
			width := r.Dx() * 4
			equal := old != nil
			if equal {
				for row := 0; row < r.Dy(); row++ {
					o := img.PixOffset(x, y+row)
					if !bytes.Equal(img.Pix[o:o+width], old.pixels[row*width:(row+1)*width]) {
						equal = false
						break
					}
				}
			}
			if equal {
				s.tiles = append(s.tiles, old)
			} else {
				p := make([]byte, width*r.Dy())
				for row := 0; row < r.Dy(); row++ {
					o := img.PixOffset(x, y+row)
					copy(p[row*width:(row+1)*width], img.Pix[o:o+width])
				}
				s.tiles = append(s.tiles, &undoTile{pixels: p})
			}
			i++
		}
	}
	return s
}

func (s *undoImage) restore() *image.NRGBA {
	if s == nil {
		return nil
	}
	img := image.NewNRGBA(s.rect)
	i := 0
	for y := s.rect.Min.Y; y < s.rect.Max.Y; y += undoTileSize {
		for x := s.rect.Min.X; x < s.rect.Max.X; x += undoTileSize {
			width := min(undoTileSize, s.rect.Max.X-x) * 4
			for row := 0; row < min(undoTileSize, s.rect.Max.Y-y); row++ {
				o := img.PixOffset(x, y+row)
				copy(img.Pix[o:o+width], s.tiles[i].pixels[row*width:(row+1)*width])
			}
			i++
		}
	}
	return img
}

// Count shared storage only once. Include tile references and conservative
// metadata overhead; this budget covers history, not live images or previews.
func (a *App) undoBytes() int64 {
	seen := make(map[*undoTile]bool)
	var size int64
	for _, stack := range [][]undoEntry{a.undoStack, a.redoStack} {
		for _, entry := range stack {
			size += 256
			images := []*undoImage{entry.image}
			if entry.disc != nil {
				images = append(images, entry.disc.base)
			}
			for _, img := range images {
				if img == nil {
					continue
				}
				size += int64(cap(img.tiles)) * 8
				for _, tile := range img.tiles {
					if !seen[tile] {
						seen[tile] = true
						size += int64(len(tile.pixels)) + 32
					}
				}
			}
		}
	}
	return size
}

func (a *App) trimUndo() {
	a.trimHistory(false)
}

func (a *App) trimHistory(keepRedo bool) {
	limit := a.undoLimit
	if limit <= 0 {
		limit = defaultUndoLimit
	}
	budget := a.undoMemoryLimit
	if budget <= 0 {
		budget = defaultUndoBytes
	}
	// Keep the closest return step even if one scan alone exceeds the budget.
	for len(a.undoStack)+len(a.redoStack) > 1 && (len(a.undoStack)+len(a.redoStack) > limit || a.undoBytes() > budget) {
		drop := &a.undoStack
		if !keepRedo {
			drop = &a.redoStack
		}
		if len(*drop) == 0 {
			if keepRedo {
				drop = &a.redoStack
			} else {
				drop = &a.undoStack
			}
		}
		(*drop)[0] = undoEntry{}
		*drop = (*drop)[1:]
	}
}
