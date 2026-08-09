package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	previewAssetPathPrefix = "/__atropos/preview/"
	previewAssetCacheSize  = 8
	previewJPEGQuality     = 95
	previewLowJPEGQuality  = 85
	previewLowMaxDimension = 1600
)

// previewAssetStore gives immutable working-image snapshots small versioned
// URLs. The encoded bytes travel through Wails' asset server instead of its
// JSON/base64 method bridge. Entries are bounded; the WebView also receives
// immutable cache headers and normally retains already displayed revisions.
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
	full   *previewAsset
	low    *previewAsset
	width  int
	height int
}

type previewAsset struct {
	mu       sync.Mutex
	image    *image.NRGBA
	width    int
	height   int
	data     []byte
	err      error
	encoding bool
	ready    chan struct{}
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
		full:   &previewAsset{image: img, width: b.Dx(), height: b.Dy()},
		low:    &previewAsset{image: img, width: b.Dx(), height: b.Dy()},
		width:  b.Dx(),
		height: b.Dy(),
	}
	s.byImage[img] = id
	s.imageKeys[id] = img
	s.order = append(s.order, id)
	for len(s.order) > s.maxEntries {
		evictID := s.order[0]
		s.order = s.order[1:]
		if imageKey := s.imageKeys[evictID]; imageKey != nil {
			if s.byImage[imageKey] == evictID {
				delete(s.byImage, imageKey)
			}
		}
		delete(s.imageKeys, evictID)
		delete(s.entries, evictID)
	}
	return s.url(id), nil
}

// releaseImageKey drops the store's strong reference to source pixels after
// encoding. The bounded entry retains only compressed bytes; active App and
// undo state independently own any image pointers they still require.
func (s *previewAssetStore) releaseImageKey(id uint64) {
	s.mu.Lock()
	if imageKey := s.imageKeys[id]; imageKey != nil {
		if s.byImage[imageKey] == id {
			delete(s.byImage, imageKey)
		}
	}
	delete(s.imageKeys, id)
	s.mu.Unlock()
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

func (p *previewAssetRevision) releaseSources() {
	p.full.mu.Lock()
	p.full.image = nil
	p.full.mu.Unlock()
	p.low.mu.Lock()
	p.low.image = nil
	p.low.mu.Unlock()
}

// encoded returns a cached full-resolution JPEG. Only one JPEG encode runs at
// a time; superseded WebView requests can cancel while waiting, preventing a
// fast slider drag from queueing an encode for every intermediate revision.
func (p *previewAsset) encoded(ctx context.Context, token chan struct{}, lowResolution bool) ([]byte, error) {
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
		img := p.image
		p.mu.Unlock()
		if img == nil {
			err := fmt.Errorf("preview source pixels are no longer available")
			p.finishAttempt(ready, nil, err, true)
			return nil, err
		}

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

		encodeImage := img
		quality := previewJPEGQuality
		if lowResolution && (p.width > previewLowMaxDimension || p.height > previewLowMaxDimension) {
			scale := math.Min(
				float64(previewLowMaxDimension)/float64(p.width),
				float64(previewLowMaxDimension)/float64(p.height),
			)
			encodeImage = resizeNRGBA(img, maxInt(1, int(float64(p.width)*scale)), maxInt(1, int(float64(p.height)*scale)))
			quality = previewLowJPEGQuality
		}

		var buf bytes.Buffer
		err := jpeg.Encode(&buf, encodeImage, &jpeg.Options{Quality: quality})
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

func (p *previewAsset) finishAttempt(ready chan struct{}, data []byte, err error, cache bool) {
	p.mu.Lock()
	if cache {
		p.data = data
		p.err = err
		p.image = nil
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

// servePreviewAsset is deliberately restricted to opaque numeric IDs. It
// never accepts filesystem paths from the frontend.
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
	lowResolution := strings.HasSuffix(idText, "-low")
	idText = strings.TrimSuffix(idText, "-low")
	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil || id == 0 {
		http.NotFound(w, r)
		return
	}
	revision := a.previewAssets.get(parts[0], id)
	if revision == nil {
		http.NotFound(w, r)
		return
	}
	entry := revision.full
	variant := "full"
	if lowResolution {
		entry = revision.low
		variant = "low"
	}

	started := time.Now()
	data, err := entry.encoded(r.Context(), a.previewAssets.encodeToken, lowResolution)
	if err != nil {
		a.previewAssets.releaseImageKey(id)
		if r.Context().Err() == nil {
			a.logf("preview asset %d: encode failed: %v", id, err)
			http.Error(w, "preview encoding failed", http.StatusInternalServerError)
		}
		return
	}
	if !lowResolution {
		revision.releaseSources()
		a.previewAssets.releaseImageKey(id)
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	a.logf("preview asset %d (%s): served source %dx%d, %d bytes in %s", id, variant, revision.width, revision.height, len(data), time.Since(started))
}
