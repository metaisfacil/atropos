package preview

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestPreviewAssetStoreReusesImageRevision(t *testing.T) {
	store := NewStore(4, nil)
	img := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	first, err := store.Register(img)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Register(img)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same image received different revisions: %q != %q", first, second)
	}
	if !strings.HasPrefix(first, PathPrefix) {
		t.Fatalf("unexpected preview URL: %q", first)
	}
}

func TestServePreviewAssetPreservesLegacyFullAndLowVariants(t *testing.T) {
	store := NewStore(DefaultCacheSize, nil)
	img := image.NewNRGBA(image.Rect(0, 0, 1801, 37))
	for y := 0; y < 37; y++ {
		for x := 0; x < 1801; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y * 5), B: 90, A: 255})
		}
	}
	url, err := store.Register(img)
	if err != nil {
		t.Fatal(err)
	}

	request := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		res := httptest.NewRecorder()
		store.ServeHTTP(res, req)
		return res
	}

	lowURL := strings.TrimSuffix(url, ".jpg") + "-low.jpg"
	low := request(lowURL)
	if low.Code != http.StatusOK {
		t.Fatalf("low-resolution status = %d, body=%q", low.Code, low.Body.String())
	}
	lowConfig, err := jpeg.DecodeConfig(low.Body)
	if err != nil {
		t.Fatalf("decode low-resolution JPEG: %v", err)
	}
	if lowConfig.Width != previewLowMaxDimension || lowConfig.Height >= 37 {
		t.Fatalf("low-resolution dimensions = %dx%d", lowConfig.Width, lowConfig.Height)
	}
	if got := low.Header().Get("X-Atropos-Preview-Variant"); got != "low" {
		t.Fatalf("low variant header = %q", got)
	}

	first := request(url)
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", first.Code, first.Body.String())
	}
	if got := first.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("Content-Type = %q", got)
	}
	if !strings.Contains(first.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("missing immutable cache policy: %q", first.Header().Get("Cache-Control"))
	}
	firstBytes := append([]byte(nil), first.Body.Bytes()...)
	config, err := jpeg.DecodeConfig(first.Body)
	if err != nil {
		t.Fatalf("decode served JPEG: %v", err)
	}
	if config.Width != 1801 || config.Height != 37 {
		t.Fatalf("served dimensions = %dx%d, want 1801x37", config.Width, config.Height)
	}
	if got := first.Header().Get("X-Atropos-Preview-Variant"); got != "full" {
		t.Fatalf("full variant header = %q", got)
	}

	second := request(url)
	if second.Code != http.StatusOK || !bytes.Equal(second.Body.Bytes(), firstBytes) {
		t.Fatal("cached preview response changed")
	}
}

func TestServePreviewAssetViewportCropAndResize(t *testing.T) {
	store := NewStore(DefaultCacheSize, nil)
	img := image.NewNRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			if x < 400 {
				img.SetNRGBA(x, y, color.NRGBA{R: 240, G: 30, B: 20, A: 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{R: 20, G: 30, B: 240, A: 255})
			}
		}
	}

	url, err := store.Register(img)
	if err != nil {
		t.Fatal(err)
	}
	target := url + "?x=64&y=96&w=256&h=192&dw=128&dh=96&q=90"
	res := httptest.NewRecorder()
	store.ServeHTTP(res, httptest.NewRequest(http.MethodGet, target, nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", res.Code, res.Body.String())
	}
	if got := res.Header().Get("X-Atropos-Preview-Variant"); got != "viewport" {
		t.Fatalf("variant = %q", got)
	}
	if got := res.Header().Get("X-Atropos-Source-Rect"); got != "64,96,256,192" {
		t.Fatalf("source rect header = %q", got)
	}
	if got := res.Header().Get("X-Atropos-Render-Size"); got != "128,96" {
		t.Fatalf("render size header = %q", got)
	}

	config, err := jpeg.DecodeConfig(bytes.NewReader(res.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode viewport config: %v", err)
	}
	if config.Width != 128 || config.Height != 96 {
		t.Fatalf("viewport dimensions = %dx%d", config.Width, config.Height)
	}

	decoded, err := jpeg.Decode(bytes.NewReader(res.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode viewport JPEG: %v", err)
	}
	r, _, b, _ := decoded.At(64, 48).RGBA()
	if r <= b {
		t.Fatalf("viewport did not sample requested red half: r=%d b=%d", r, b)
	}
}

func TestServePreviewAssetViewportClampsAtImageEdge(t *testing.T) {
	store := NewStore(DefaultCacheSize, nil)
	url, err := store.Register(image.NewNRGBA(image.Rect(0, 0, 100, 80)))
	if err != nil {
		t.Fatal(err)
	}

	res := httptest.NewRecorder()
	store.ServeHTTP(res, httptest.NewRequest(http.MethodGet,
		url+"?x=80&y=60&w=40&h=40&dw=200&dh=200", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", res.Code, res.Body.String())
	}
	if got := res.Header().Get("X-Atropos-Source-Rect"); got != "80,60,20,20" {
		t.Fatalf("source rect header = %q", got)
	}
	if got := res.Header().Get("X-Atropos-Render-Size"); got != "100,100" {
		t.Fatalf("render size header = %q", got)
	}
}

func TestServePreviewAssetViewportRejectsBadOrOversizedRequests(t *testing.T) {
	store := NewStore(DefaultCacheSize, nil)
	url, err := store.Register(image.NewNRGBA(image.Rect(0, 0, 100, 80)))
	if err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"?x=0&y=0&w=100&h=80&dw=4097&dh=1",
		"?x=0&y=0&w=100&h=80&dw=4096&dh=4096&q=10",
		"?x=1000&y=1000&w=10&h=10&dw=10&dh=10",
		"?x=" + strconv.FormatInt(int64(^uint(0)>>1), 10) + "&y=0&w=2&h=1&dw=1&dh=1",
		"?x=0&y=0&w=0&h=10&dw=10&dh=10",
	}
	for _, query := range cases {
		res := httptest.NewRecorder()
		store.ServeHTTP(res, httptest.NewRequest(http.MethodGet, url+query, nil))
		if res.Code != http.StatusBadRequest {
			t.Fatalf("query %q returned %d, want 400; body=%q", query, res.Code, res.Body.String())
		}
	}
}

func TestServePreviewAssetRejectsArbitraryPathsAndExpiredRevisions(t *testing.T) {
	store := NewStore(DefaultCacheSize, nil)
	for _, path := range []string{"/etc/passwd", PathPrefix + "not-a-number.jpg"} {
		res := httptest.NewRecorder()
		store.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusNotFound {
			t.Fatalf("path %q returned %d, want 404", path, res.Code)
		}
	}

	url, err := store.Register(image.NewNRGBA(image.Rect(0, 0, 5, 5)))
	if err != nil {
		t.Fatal(err)
	}
	store.Reset()
	newURL, err := store.Register(image.NewNRGBA(image.Rect(0, 0, 5, 5)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Split(strings.TrimPrefix(newURL, PathPrefix), "/")[0] ==
		strings.Split(strings.TrimPrefix(url, PathPrefix), "/")[0] {
		t.Fatal("preview session did not rotate after image-state reset")
	}
	res := httptest.NewRecorder()
	store.ServeHTTP(res, httptest.NewRequest(http.MethodGet, url, nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("expired revision returned %d, want 404", res.Code)
	}
}

func TestServePreviewAssetConcurrentViewportRequestsShareEncoding(t *testing.T) {
	store := NewStore(DefaultCacheSize, nil)
	url, err := store.Register(image.NewNRGBA(image.Rect(0, 0, 800, 600)))
	if err != nil {
		t.Fatal(err)
	}
	target := url + "?x=64&y=64&w=512&h=384&dw=256&dh=192"

	const requests = 8
	responses := make([][]byte, requests)
	errors := make(chan string, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			res := httptest.NewRecorder()
			store.ServeHTTP(res, httptest.NewRequest(http.MethodGet, target, nil))
			if res.Code != http.StatusOK {
				errors <- res.Body.String()
				return
			}
			responses[index] = append([]byte(nil), res.Body.Bytes()...)
		}(i)
	}
	wg.Wait()
	close(errors)
	for message := range errors {
		t.Fatalf("concurrent request failed: %s", message)
	}
	for i := 1; i < requests; i++ {
		if !bytes.Equal(responses[0], responses[i]) {
			t.Fatalf("response %d did not share the cached encoding", i)
		}
	}
}

func TestServePreviewAssetConcurrentLegacyFullRequestsShareEncoding(t *testing.T) {
	store := NewStore(DefaultCacheSize, nil)
	url, err := store.Register(image.NewNRGBA(image.Rect(0, 0, 800, 600)))
	if err != nil {
		t.Fatal(err)
	}

	const requests = 8
	responses := make([][]byte, requests)
	errors := make(chan string, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			res := httptest.NewRecorder()
			store.ServeHTTP(res, httptest.NewRequest(http.MethodGet, url, nil))
			if res.Code != http.StatusOK {
				errors <- res.Body.String()
				return
			}
			responses[index] = append([]byte(nil), res.Body.Bytes()...)
		}(i)
	}
	wg.Wait()
	close(errors)
	for message := range errors {
		t.Fatalf("concurrent request failed: %s", message)
	}
	for i := 1; i < requests; i++ {
		if !bytes.Equal(responses[0], responses[i]) {
			t.Fatalf("response %d did not share the cached encoding", i)
		}
	}
}
