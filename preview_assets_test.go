package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPreviewAssetStoreReusesImageRevision(t *testing.T) {
	store := newPreviewAssetStore(4)
	img := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	first, err := store.register(img)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.register(img)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same image received different revisions: %q != %q", first, second)
	}
	if !strings.HasPrefix(first, previewAssetPathPrefix) {
		t.Fatalf("unexpected preview URL: %q", first)
	}
}

func TestServePreviewAssetPreservesFullDimensionsAndCaches(t *testing.T) {
	app := NewApp()
	img := image.NewNRGBA(image.Rect(0, 0, 1801, 37))
	for y := 0; y < 37; y++ {
		for x := 0; x < 1801; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y * 5), B: 90, A: 255})
		}
	}
	url, err := app.imagePreviewURL(img)
	if err != nil {
		t.Fatal(err)
	}

	request := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		res := httptest.NewRecorder()
		app.servePreviewAsset(res, req)
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

	second := request(url)
	if second.Code != http.StatusOK || !bytes.Equal(second.Body.Bytes(), firstBytes) {
		t.Fatal("cached preview response changed")
	}
}

func TestServePreviewAssetRejectsArbitraryPathsAndExpiredRevisions(t *testing.T) {
	app := NewApp()
	for _, path := range []string{"/etc/passwd", previewAssetPathPrefix + "not-a-number.jpg"} {
		res := httptest.NewRecorder()
		app.servePreviewAsset(res, httptest.NewRequest(http.MethodGet, path, nil))
		if res.Code != http.StatusNotFound {
			t.Fatalf("path %q returned %d, want 404", path, res.Code)
		}
	}

	url, err := app.imagePreviewURL(image.NewNRGBA(image.Rect(0, 0, 5, 5)))
	if err != nil {
		t.Fatal(err)
	}
	app.previewAssets.reset()
	newURL, err := app.imagePreviewURL(image.NewNRGBA(image.Rect(0, 0, 5, 5)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Split(strings.TrimPrefix(newURL, previewAssetPathPrefix), "/")[0] ==
		strings.Split(strings.TrimPrefix(url, previewAssetPathPrefix), "/")[0] {
		t.Fatal("preview session did not rotate after image-state reset")
	}
	res := httptest.NewRecorder()
	app.servePreviewAsset(res, httptest.NewRequest(http.MethodGet, url, nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("expired revision returned %d, want 404", res.Code)
	}
}

func TestServePreviewAssetConcurrentRequestsShareEncoding(t *testing.T) {
	app := NewApp()
	url, err := app.imagePreviewURL(image.NewNRGBA(image.Rect(0, 0, 800, 600)))
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
			app.servePreviewAsset(res, httptest.NewRequest(http.MethodGet, url, nil))
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
