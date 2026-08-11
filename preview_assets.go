package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	previewAssetPathPrefix = "/__atropos/preview/"
	previewAssetCacheSize  = 4

	// Compatibility variants. The canvas renderer no longer depends on these,
	// but retaining them keeps preview URLs backwards compatible with older UI
	// builds and makes rolling frontend/backend upgrades safe.
	previewJPEGQuality     = 95
	previewLowJPEGQuality  = 85
	previewLowMaxDimension = 1600

	// Viewport renders are intentionally bounded. The frontend requests only
	// the visible image rectangle (plus modest overscan), at approximately the
	// number of device pixels needed to display it.
	previewViewportJPEGQuality    = 88
	previewViewportMaxDimension   = 4096
	previewViewportMaxPixels      = 16 * 1024 * 1024
	previewRasterCachePerRevision = 8
)

// previewAssetStore assigns immutable revision URLs to working images. The
// revision retains the source NRGBA pointer so arbitrary viewport crops can be
// rendered later without republishing or copying the full image through the
// Wails bridge. The store itself is bounded and revisions are LRU-evicted.
type previewAssetStore struct {
	mu          sync.Mutex
	nextID      uint64
	entries     map[uint64]*previewAssetRevision
	byImage     map[*image.NRGBA]uint64
	imageKeys   map[uint64]*image.NRGBA
	order       []uint64
	maxEntries  int
	session     string
	encodeToken chan struct{}
}

type previewAssetRevision struct {
	mu          sync.Mutex
	source      *image.NRGBA
	width       int
	height      int
	rasters     map[string]*previewRaster
	rasterOrder []string
}

type previewRaster struct {
	mu       sync.Mutex
	data     []byte
	err      error
	encoding bool
	ready    chan struct{}
}

// PreviewViewportRequest asks the backend for only the source rectangle needed
// by the current frontend viewport. The preview field is the opaque immutable
// revision URL previously returned by an image operation; it is never treated
// as a filesystem path.
type PreviewViewportRequest struct {
	Preview    string `json:"preview"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DestWidth  int    `json:"destWidth"`
	DestHeight int    `json:"destHeight"`
	Quality    int    `json:"quality"`
}

// PreviewViewportResponse carries a small viewport JPEG over the Wails binding
// as a data URL. This deliberately avoids the dynamic-asset HTTP lifecycle for
// canvas rasters: on Windows/WebView2 we observed requests reaching the Go
// AssetsHandler and completing there while HTMLImageElement never fired either
// load or error. Viewport JPEGs are small enough that one base64 crossing is a
// better tradeoff than depending on that ambiguous resource lifecycle.
type PreviewViewportResponse struct {
	DataURL      string `json:"dataURL"`
	X            int    `json:"x"`
	Y            int    `json:"y"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	RasterWidth  int    `json:"rasterWidth"`
	RasterHeight int    `json:"rasterHeight"`
}

type previewRenderRequest struct {
	rect    image.Rectangle
	width   int
	height  int
	quality int
	variant string
	key     string
}

func previewMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func newPreviewAssetStore(maxEntries int) *previewAssetStore {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &previewAssetStore{
		entries:     make(map[uint64]*previewAssetRevision),
		byImage:     make(map[*image.NRGBA]uint64),
		imageKeys:   make(map[uint64]*image.NRGBA),
		maxEntries:  maxEntries,
		session:     newPreviewAssetSession(),
		encodeToken: make(chan struct{}, 1),
	}
}

func (s *previewAssetStore) register(img *image.NRGBA) (string, error) {
	if img == nil || img.Bounds().Empty() {
		return "", fmt.Errorf("cannot publish an empty preview image")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.byImage[img]; ok {
		if _, present := s.entries[id]; present {
			s.touchLocked(id)
			return s.url(id), nil
		}
	}

	s.nextID++
	id := s.nextID
	b := img.Bounds()
	s.entries[id] = &previewAssetRevision{
		source:  img,
		width:   b.Dx(),
		height:  b.Dy(),
		rasters: make(map[string]*previewRaster),
	}
	s.byImage[img] = id
	s.imageKeys[id] = img
	s.order = append(s.order, id)

	for len(s.order) > s.maxEntries {
		evictID := s.order[0]
		s.order = s.order[1:]
		if imageKey := s.imageKeys[evictID]; imageKey != nil && s.byImage[imageKey] == evictID {
			delete(s.byImage, imageKey)
		}
		delete(s.imageKeys, evictID)
		delete(s.entries, evictID)
	}
	return s.url(id), nil
}

func (s *previewAssetStore) get(session string, id uint64) *previewAssetRevision {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session != s.session {
		return nil
	}
	entry := s.entries[id]
	if entry != nil {
		s.touchLocked(id)
	}
	return entry
}

func (s *previewAssetStore) touchLocked(id uint64) {
	for i, existing := range s.order {
		if existing != id {
			continue
		}
		copy(s.order[i:], s.order[i+1:])
		s.order[len(s.order)-1] = id
		return
	}
}

func (s *previewAssetStore) reset() {
	s.mu.Lock()
	s.entries = make(map[uint64]*previewAssetRevision)
	s.byImage = make(map[*image.NRGBA]uint64)
	s.imageKeys = make(map[uint64]*image.NRGBA)
	s.order = nil
	s.session = newPreviewAssetSession()
	s.mu.Unlock()
}

func newPreviewAssetSession() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func (s *previewAssetStore) url(id uint64) string {
	return previewAssetPathPrefix + s.session + "/" + strconv.FormatUint(id, 10) + ".jpg"
}

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

func (a *App) imagePreviewURL(img *image.NRGBA) (string, error) {
	return a.previewAssets.register(img)
}

func (a *App) previewRevisionFromURL(preview string) (uint64, *previewAssetRevision, error) {
	path := strings.SplitN(preview, "?", 2)[0]
	if !strings.HasPrefix(path, previewAssetPathPrefix) || !strings.HasSuffix(path, ".jpg") {
		return 0, nil, fmt.Errorf("invalid preview revision")
	}
	relativePath := strings.TrimPrefix(path, previewAssetPathPrefix)
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
	revision := a.previewAssets.get(parts[0], id)
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

// RenderPreviewViewport is the canvas transport used by the frontend. It uses
// the same bounded, cached raster encoder as the AssetsHandler, but returns the
// JPEG through the Wails binding as a data URL. This avoids a WebView2/Wails
// dynamic-asset edge case where the Go handler completes but the image element
// never reaches either onload or onerror.
func (a *App) RenderPreviewViewport(request PreviewViewportRequest) (PreviewViewportResponse, error) {
	id, revision, err := a.previewRevisionFromURL(request.Preview)
	if err != nil {
		return PreviewViewportResponse{}, err
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
		return PreviewViewportResponse{}, err
	}

	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a.logf(
		"preview RPC request %d: rect=%v render=%dx%d q=%d",
		id,
		renderReq.rect,
		renderReq.width,
		renderReq.height,
		renderReq.quality,
	)
	started := time.Now()
	raster := revision.rasterFor(renderReq.key)
	data, err := raster.encoded(ctx, a.previewAssets.encodeToken, revision.source, renderReq)
	if err != nil {
		return PreviewViewportResponse{}, err
	}
	a.logf(
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
	return PreviewViewportResponse{
		DataURL:      "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data),
		X:            renderReq.rect.Min.X,
		Y:            renderReq.rect.Min.Y,
		Width:        renderReq.rect.Dx(),
		Height:       renderReq.rect.Dy(),
		RasterWidth:  renderReq.width,
		RasterHeight: renderReq.height,
	}, nil
}

// servePreviewAsset only accepts opaque session/revision IDs. Viewport query
// parameters describe image-space source bounds and destination raster size;
// no filesystem path ever crosses the frontend/backend boundary.
func (a *App) servePreviewAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.URL.Path, previewAssetPathPrefix) || !strings.HasSuffix(r.URL.Path, ".jpg") {
		http.NotFound(w, r)
		return
	}

	relativePath := strings.TrimPrefix(r.URL.Path, previewAssetPathPrefix)
	parts := strings.Split(relativePath, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}

	idText := strings.TrimSuffix(parts[1], ".jpg")
	legacyLow := strings.HasSuffix(idText, "-low")
	idText = strings.TrimSuffix(idText, "-low")
	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil || id == 0 {
		http.NotFound(w, r)
		return
	}

	revision := a.previewAssets.get(parts[0], id)
	if revision == nil || revision.source == nil {
		http.NotFound(w, r)
		return
	}

	renderReq, err := parsePreviewRenderRequest(r, revision, legacyLow)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	started := time.Now()
	raster := revision.rasterFor(renderReq.key)
	data, err := raster.encoded(r.Context(), a.previewAssets.encodeToken, revision.source, renderReq)
	if err != nil {
		if r.Context().Err() == nil {
			a.logf("preview asset %d: %s encode failed: %v", id, renderReq.variant, err)
			http.Error(w, "preview encoding failed", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Atropos-Preview-Variant", renderReq.variant)
	w.Header().Set("X-Atropos-Source-Rect", fmt.Sprintf("%d,%d,%d,%d", renderReq.rect.Min.X, renderReq.rect.Min.Y, renderReq.rect.Dx(), renderReq.rect.Dy()))
	w.Header().Set("X-Atropos-Render-Size", fmt.Sprintf("%d,%d", renderReq.width, renderReq.height))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)

	a.logf(
		"preview asset %d (%s): source %dx%d rect=%v render=%dx%d %d bytes in %s",
		id,
		renderReq.variant,
		revision.width,
		revision.height,
		renderReq.rect,
		renderReq.width,
		renderReq.height,
		len(data),
		time.Since(started),
	)
}

func parsePreviewRenderRequest(r *http.Request, revision *previewAssetRevision, legacyLow bool) (previewRenderRequest, error) {
	bounds := revision.source.Bounds()
	query := r.URL.Query()
	viewportRequested := query.Has("x") || query.Has("y") || query.Has("w") || query.Has("h") || query.Has("dw") || query.Has("dh")

	if !viewportRequested {
		width, height := bounds.Dx(), bounds.Dy()
		quality := previewJPEGQuality
		variant := "full"
		if legacyLow && (width > previewLowMaxDimension || height > previewLowMaxDimension) {
			scale := math.Min(
				float64(previewLowMaxDimension)/float64(width),
				float64(previewLowMaxDimension)/float64(height),
			)
			width = previewMaxInt(1, int(math.Round(float64(width)*scale)))
			height = previewMaxInt(1, int(math.Round(float64(height)*scale)))
			quality = previewLowJPEGQuality
			variant = "low"
		} else if legacyLow {
			quality = previewLowJPEGQuality
			variant = "low"
		}
		key := fmt.Sprintf("legacy:%s:%dx%d:q%d", variant, width, height, quality)
		return previewRenderRequest{rect: bounds, width: width, height: height, quality: quality, variant: variant, key: key}, nil
	}

	x, err := requiredIntQuery(query.Get("x"), "x")
	if err != nil {
		return previewRenderRequest{}, err
	}
	y, err := requiredIntQuery(query.Get("y"), "y")
	if err != nil {
		return previewRenderRequest{}, err
	}
	width, err := requiredPositiveIntQuery(query.Get("w"), "w")
	if err != nil {
		return previewRenderRequest{}, err
	}
	height, err := requiredPositiveIntQuery(query.Get("h"), "h")
	if err != nil {
		return previewRenderRequest{}, err
	}
	destWidth, err := requiredPositiveIntQuery(query.Get("dw"), "dw")
	if err != nil {
		return previewRenderRequest{}, err
	}
	destHeight, err := requiredPositiveIntQuery(query.Get("dh"), "dh")
	if err != nil {
		return previewRenderRequest{}, err
	}

	if destWidth > previewViewportMaxDimension || destHeight > previewViewportMaxDimension || destWidth > previewViewportMaxPixels/destHeight {
		return previewRenderRequest{}, fmt.Errorf("requested preview raster is too large")
	}

	// Avoid integer overflow in x+width/y+height before constructing an
	// image.Rectangle. Query values are untrusted even though normal requests
	// come from the embedded frontend.
	if (x > 0 && width > math.MaxInt-x) || (y > 0 && height > math.MaxInt-y) {
		return previewRenderRequest{}, fmt.Errorf("requested preview rectangle is out of range")
	}
	requested := image.Rect(x, y, x+width, y+height).Intersect(bounds)
	if requested.Empty() {
		return previewRenderRequest{}, fmt.Errorf("requested preview rectangle is outside the image")
	}

	// If clamping trimmed the source request at an edge, scale the destination
	// to the same ratio so pixels remain square and coordinate mapping stays
	// exact.
	if requested.Dx() != width {
		destWidth = previewMaxInt(1, int(math.Round(float64(destWidth)*float64(requested.Dx())/float64(width))))
	}
	if requested.Dy() != height {
		destHeight = previewMaxInt(1, int(math.Round(float64(destHeight)*float64(requested.Dy())/float64(height))))
	}

	quality := previewViewportJPEGQuality
	if text := query.Get("q"); text != "" {
		parsed, parseErr := strconv.Atoi(text)
		if parseErr != nil || parsed < 40 || parsed > 95 {
			return previewRenderRequest{}, fmt.Errorf("q must be an integer between 40 and 95")
		}
		quality = parsed
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

func requiredIntQuery(value, name string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("missing %s", name)
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func requiredPositiveIntQuery(value, name string) (int, error) {
	parsed, err := requiredIntQuery(value, name)
	if err != nil {
		return 0, err
	}
	if parsed < 1 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return parsed, nil
}
