// Package preview manages immutable preview revisions and bounded raster rendering.
package preview

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"strconv"
	"sync"
	"time"
)

const (
	// PathPrefix identifies opaque preview revision URLs.
	PathPrefix = "/__atropos/preview/"
	// DefaultCacheSize is the number of full image revisions retained.
	DefaultCacheSize = 4

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

// Store assigns immutable revision URLs to working images. The
// revision retains the source NRGBA pointer so arbitrary viewport crops can be
// rendered later without republishing or copying the full image through the
// Wails bridge. The store itself is bounded and revisions are LRU-evicted.
type Store struct {
	mu          sync.Mutex
	nextID      uint64
	entries     map[uint64]*previewAssetRevision
	byImage     map[*image.NRGBA]uint64
	imageKeys   map[uint64]*image.NRGBA
	order       []uint64
	maxEntries  int
	session     string
	encodeToken chan struct{}
	logf        func(string, ...interface{})
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

// RenderRequest asks the backend for only the source rectangle needed
// by the current frontend viewport. The preview field is the opaque immutable
// revision URL previously returned by an image operation; it is never treated
// as a filesystem path.
type RenderRequest struct {
	Preview    string `json:"preview"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DestWidth  int    `json:"destWidth"`
	DestHeight int    `json:"destHeight"`
	Quality    int    `json:"quality"`
}

// RenderResponse carries a small viewport JPEG over the Wails binding
// as a data URL. This deliberately avoids the dynamic-asset HTTP lifecycle for
// canvas rasters: on Windows/WebView2 we observed requests reaching the Go
// AssetsHandler and completing there while HTMLImageElement never fired either
// load or error. Viewport JPEGs are small enough that one base64 crossing is a
// better tradeoff than depending on that ambiguous resource lifecycle.
type RenderResponse struct {
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

// NewStore creates a bounded preview revision store.
func NewStore(maxEntries int, logf func(string, ...interface{})) *Store {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &Store{
		entries:     make(map[uint64]*previewAssetRevision),
		byImage:     make(map[*image.NRGBA]uint64),
		imageKeys:   make(map[uint64]*image.NRGBA),
		maxEntries:  maxEntries,
		session:     newPreviewAssetSession(),
		encodeToken: make(chan struct{}, 1),
		logf:        logf,
	}
}

func (s *Store) log(format string, args ...interface{}) {
	if s.logf != nil {
		s.logf(format, args...)
	}
}

// Register publishes an immutable image revision and returns its opaque URL.
func (s *Store) Register(img *image.NRGBA) (string, error) {
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

func (s *Store) get(session string, id uint64) *previewAssetRevision {
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

func (s *Store) touchLocked(id uint64) {
	for i, existing := range s.order {
		if existing != id {
			continue
		}
		copy(s.order[i:], s.order[i+1:])
		s.order[len(s.order)-1] = id
		return
	}
}

// Reset expires every published revision and starts a new session namespace.
func (s *Store) Reset() {
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

func (s *Store) url(id uint64) string {
	return PathPrefix + s.session + "/" + strconv.FormatUint(id, 10) + ".jpg"
}
