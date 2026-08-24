package preview

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (r *previewAssetRevision) rasterFor(key string) *previewRaster {
	r.mu.Lock()
	defer r.mu.Unlock()

	if raster := r.rasters[key]; raster != nil {
		r.touchRasterLocked(key)
		return raster
	}

	raster := &previewRaster{}
	r.rasters[key] = raster
	r.rasterOrder = append(r.rasterOrder, key)
	for len(r.rasterOrder) > previewRasterCachePerRevision {
		evict := r.rasterOrder[0]
		r.rasterOrder = r.rasterOrder[1:]
		delete(r.rasters, evict)
	}
	return raster
}

func (r *previewAssetRevision) touchRasterLocked(key string) {
	for i, existing := range r.rasterOrder {
		if existing != key {
			continue
		}
		copy(r.rasterOrder[i:], r.rasterOrder[i+1:])
		r.rasterOrder[len(r.rasterOrder)-1] = key
		return
	}
}

// encoded ensures concurrent requests for the same immutable raster share one
// JPEG encode. A global token serializes expensive encoding across revisions;
// cancelled WebView requests can leave the queue without forcing stale work.
func (p *previewRaster) encoded(
	ctx context.Context,
	token chan struct{},
	source *image.NRGBA,
	req previewRenderRequest,
) ([]byte, error) {
	for {
		p.mu.Lock()
		if p.data != nil || p.err != nil {
			data, err := p.data, p.err
			p.mu.Unlock()
			return data, err
		}
		if p.encoding {
			ready := p.ready
			p.mu.Unlock()
			select {
			case <-ready:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		p.encoding = true
		p.ready = make(chan struct{})
		ready := p.ready
		p.mu.Unlock()

		select {
		case token <- struct{}{}:
		case <-ctx.Done():
			p.finishAttempt(ready, nil, nil, false)
			return nil, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			<-token
			p.finishAttempt(ready, nil, nil, false)
			return nil, err
		}

		// Do not materialize a full-resolution crop just to downsample it. At
		// fit-to-window a source image may be hundreds of megabytes while the
		// requested raster is only a couple of megapixels. Resize directly from
		// the requested source rectangle into the destination buffer.
		var encodeImage image.Image = source
		if req.rect.Dx() != req.width || req.rect.Dy() != req.height {
			encodeImage = resizePreviewRegion(source, req.rect, req.width, req.height)
		} else if req.rect != source.Bounds() {
			encodeImage = source.SubImage(req.rect)
		}

		var buf bytes.Buffer
		err := jpeg.Encode(&buf, encodeImage, &jpeg.Options{Quality: req.quality})
		<-token
		if err != nil {
			p.finishAttempt(ready, nil, err, true)
			return nil, err
		}
		data := buf.Bytes()
		p.finishAttempt(ready, data, nil, true)
		return data, nil
	}
}

func resizePreviewRegion(src *image.NRGBA, rect image.Rectangle, newW, newH int) *image.NRGBA {
	rect = rect.Intersect(src.Bounds())
	if rect.Empty() || newW < 1 || newH < 1 {
		return image.NewNRGBA(image.Rect(0, 0, previewMaxInt(1, newW), previewMaxInt(1, newH)))
	}

	dst := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	srcW, srcH := rect.Dx(), rect.Dy()
	nCPU := runtime.NumCPU()
	if nCPU > newH {
		nCPU = newH
	}
	rowsPerWorker := (newH + nCPU - 1) / nCPU
	var wg sync.WaitGroup
	for worker := 0; worker < nCPU; worker++ {
		y0 := worker * rowsPerWorker
		y1 := (worker + 1) * rowsPerWorker
		if y1 > newH {
			y1 = newH
		}
		if y0 >= y1 {
			break
		}
		wg.Add(1)
		go func(startY, endY int) {
			defer wg.Done()
			for y := startY; y < endY; y++ {
				sy := rect.Min.Y + y*srcH/newH
				srcRow := src.PixOffset(rect.Min.X, sy)
				dstRow := y * dst.Stride
				for x := 0; x < newW; x++ {
					sx := x * srcW / newW
					si := srcRow + sx*4
					di := dstRow + x*4
					copy(dst.Pix[di:di+4], src.Pix[si:si+4])
				}
			}
		}(y0, y1)
	}
	wg.Wait()
	return dst
}

func (p *previewRaster) finishAttempt(ready chan struct{}, data []byte, err error, cache bool) {
	p.mu.Lock()
	if cache {
		p.data = data
		p.err = err
	}
	p.encoding = false
	if p.ready == ready {
		close(p.ready)
		p.ready = nil
	}
	p.mu.Unlock()
}

func (s *Store) revisionFromURL(preview string) (uint64, *previewAssetRevision, error) {
	path := strings.SplitN(preview, "?", 2)[0]
	if !strings.HasPrefix(path, PathPrefix) || !strings.HasSuffix(path, ".jpg") {
		return 0, nil, fmt.Errorf("invalid preview revision")
	}
	relativePath := strings.TrimPrefix(path, PathPrefix)
	parts := strings.Split(relativePath, "/")
	if len(parts) != 2 || parts[0] == "" {
		return 0, nil, fmt.Errorf("invalid preview revision")
	}
	idText := strings.TrimSuffix(parts[1], ".jpg")
	idText = strings.TrimSuffix(idText, "-low")
	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil || id == 0 {
		return 0, nil, fmt.Errorf("invalid preview revision")
	}
	revision := s.get(parts[0], id)
	if revision == nil || revision.source == nil {
		return 0, nil, fmt.Errorf("preview revision is no longer available")
	}
	return id, revision, nil
}

func previewViewportRenderRequest(bounds image.Rectangle, x, y, width, height, destWidth, destHeight, quality int) (previewRenderRequest, error) {
	if width < 1 || height < 1 || destWidth < 1 || destHeight < 1 {
		return previewRenderRequest{}, fmt.Errorf("preview rectangle and raster dimensions must be positive")
	}
	if destWidth > previewViewportMaxDimension || destHeight > previewViewportMaxDimension || destWidth > previewViewportMaxPixels/destHeight {
		return previewRenderRequest{}, fmt.Errorf("requested preview raster is too large")
	}
	if (x > 0 && width > math.MaxInt-x) || (y > 0 && height > math.MaxInt-y) {
		return previewRenderRequest{}, fmt.Errorf("requested preview rectangle is out of range")
	}
	requested := image.Rect(x, y, x+width, y+height).Intersect(bounds)
	if requested.Empty() {
		return previewRenderRequest{}, fmt.Errorf("requested preview rectangle is outside the image")
	}
	if requested.Dx() != width {
		destWidth = previewMaxInt(1, int(math.Round(float64(destWidth)*float64(requested.Dx())/float64(width))))
	}
	if requested.Dy() != height {
		destHeight = previewMaxInt(1, int(math.Round(float64(destHeight)*float64(requested.Dy())/float64(height))))
	}
	if quality == 0 {
		quality = previewViewportJPEGQuality
	}
	if quality < 40 || quality > 95 {
		return previewRenderRequest{}, fmt.Errorf("quality must be between 40 and 95")
	}
	key := fmt.Sprintf(
		"viewport:%d,%d,%d,%d:%dx%d:q%d",
		requested.Min.X,
		requested.Min.Y,
		requested.Dx(),
		requested.Dy(),
		destWidth,
		destHeight,
		quality,
	)
	return previewRenderRequest{
		rect:    requested,
		width:   destWidth,
		height:  destHeight,
		quality: quality,
		variant: "viewport",
		key:     key,
	}, nil
}

// Render is the canvas transport used by the frontend. It uses
// the same bounded, cached raster encoder as the AssetsHandler, but returns the
// JPEG through the Wails binding as a data URL. This avoids a WebView2/Wails
// dynamic-asset edge case where the Go handler completes but the image element
// never reaches either onload or onerror.
func (s *Store) Render(ctx context.Context, request RenderRequest) (RenderResponse, error) {
	id, revision, err := s.revisionFromURL(request.Preview)
	if err != nil {
		return RenderResponse{}, err
	}
	renderReq, err := previewViewportRenderRequest(
		revision.source.Bounds(),
		request.X,
		request.Y,
		request.Width,
		request.Height,
		request.DestWidth,
		request.DestHeight,
		request.Quality,
	)
	if err != nil {
		return RenderResponse{}, err
	}

	if ctx == nil {
		ctx = context.Background()
	}
	s.log(
		"preview RPC request %d: rect=%v render=%dx%d q=%d",
		id,
		renderReq.rect,
		renderReq.width,
		renderReq.height,
		renderReq.quality,
	)
	started := time.Now()
	raster := revision.rasterFor(renderReq.key)
	data, err := raster.encoded(ctx, s.encodeToken, revision.source, renderReq)
	if err != nil {
		return RenderResponse{}, err
	}
	s.log(
		"preview RPC %d: source %dx%d rect=%v render=%dx%d %d bytes in %s",
		id,
		revision.width,
		revision.height,
		renderReq.rect,
		renderReq.width,
		renderReq.height,
		len(data),
		time.Since(started),
	)
	return RenderResponse{
		DataURL:      "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data),
		X:            renderReq.rect.Min.X,
		Y:            renderReq.rect.Min.Y,
		Width:        renderReq.rect.Dx(),
		Height:       renderReq.rect.Dy(),
		RasterWidth:  renderReq.width,
		RasterHeight: renderReq.height,
	}, nil
}
