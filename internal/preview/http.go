package preview

import (
	"fmt"
	"image"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ServeHTTP only accepts opaque session/revision IDs. Viewport query
// parameters describe image-space source bounds and destination raster size;
// no filesystem path ever crosses the frontend/backend boundary.
func (s *Store) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.URL.Path, PathPrefix) || !strings.HasSuffix(r.URL.Path, ".jpg") {
		http.NotFound(w, r)
		return
	}

	relativePath := strings.TrimPrefix(r.URL.Path, PathPrefix)
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

	revision := s.get(parts[0], id)
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
	data, err := raster.encoded(r.Context(), s.encodeToken, revision.source, renderReq)
	if err != nil {
		if r.Context().Err() == nil {
			s.log("preview asset %d: %s encode failed: %v", id, renderReq.variant, err)
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

	s.log(
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
