//go:build windows

package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"testing"
)

func TestDecodeClipboardDIBV5RoundTrip(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	src.SetNRGBA(1, 1, color.NRGBA{R: 10, G: 20, B: 30, A: 40})
	src.SetNRGBA(2, 1, color.NRGBA{R: 50, G: 60, B: 70, A: 80})
	rect := image.Rect(1, 1, 3, 2)
	size, err := clipboardDIBV5Size(rect)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	if err := writeClipboardDIBV5(data, src, rect); err != nil {
		t.Fatal(err)
	}

	got, err := decodeClipboardDIB(data)
	if err != nil {
		t.Fatalf("decodeClipboardDIB returned error: %v", err)
	}
	if got.Bounds() != image.Rect(0, 0, 2, 1) {
		t.Fatalf("decoded bounds = %v", got.Bounds())
	}
	if got.NRGBAAt(0, 0) != (color.NRGBA{R: 10, G: 20, B: 30, A: 40}) ||
		got.NRGBAAt(1, 0) != (color.NRGBA{R: 50, G: 60, B: 70, A: 80}) {
		t.Fatalf("decoded pixels = %v, %v", got.NRGBAAt(0, 0), got.NRGBAAt(1, 0))
	}
}

func TestDecodeClipboardDIB24TopDown(t *testing.T) {
	const headerSize = 40
	data := make([]byte, headerSize+16) // two padded 2x24-bpp rows
	binary.LittleEndian.PutUint32(data[0:4], headerSize)
	binary.LittleEndian.PutUint32(data[4:8], 2)
	binary.LittleEndian.PutUint32(data[8:12], ^uint32(1))
	binary.LittleEndian.PutUint16(data[12:14], 1)
	binary.LittleEndian.PutUint16(data[14:16], 24)
	copy(data[40:48], []byte{3, 2, 1, 6, 5, 4, 0, 0})
	copy(data[48:56], []byte{9, 8, 7, 12, 11, 10, 0, 0})

	got, err := decodeClipboardDIB(data)
	if err != nil {
		t.Fatalf("decodeClipboardDIB returned error: %v", err)
	}
	want := []color.NRGBA{
		{R: 1, G: 2, B: 3, A: 255}, {R: 4, G: 5, B: 6, A: 255},
		{R: 7, G: 8, B: 9, A: 255}, {R: 10, G: 11, B: 12, A: 255},
	}
	for i, expected := range want {
		x, y := i%2, i/2
		if actual := got.NRGBAAt(x, y); actual != expected {
			t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, actual, expected)
		}
	}
}
