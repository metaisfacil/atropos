package main

import (
	"bytes"
	"image"
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
	for _, entry := range a.undoStack {
		size += 256
		if entry.image == nil {
			continue
		}
		size += int64(cap(entry.image.tiles)) * 8
		for _, tile := range entry.image.tiles {
			if !seen[tile] {
				seen[tile] = true
				size += int64(len(tile.pixels)) + 32
			}
		}
	}
	return size
}

func (a *App) trimUndo() {
	limit := a.undoLimit
	if limit <= 0 {
		limit = defaultUndoLimit
	}
	budget := a.undoMemoryLimit
	if budget <= 0 {
		budget = defaultUndoBytes
	}
	// Always retain the most recent undo, even for a scan larger than budget.
	for len(a.undoStack) > 1 && (len(a.undoStack) > limit || a.undoBytes() > budget) {
		a.undoStack[0] = undoEntry{}
		a.undoStack = a.undoStack[1:]
	}
}
