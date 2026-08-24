//go:build windows

package main

import (
	"bytes"
	"image"
	"image/color"
	"testing"
	"unsafe"
)

func TestWriteClipboardDIBV5UsesBottomUpBGRA(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	src.SetNRGBA(1, 1, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	src.SetNRGBA(2, 1, color.NRGBA{R: 40, G: 50, B: 60, A: 255})
	src.SetNRGBA(1, 2, color.NRGBA{R: 70, G: 80, B: 90, A: 255})
	src.SetNRGBA(2, 2, color.NRGBA{R: 100, G: 110, B: 120, A: 255})
	rect := image.Rect(1, 1, 3, 3)
	size, err := clipboardDIBV5Size(rect)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, size)
	if err := writeClipboardDIBV5(buf, src, rect); err != nil {
		t.Fatal(err)
	}

	header := (*clipboardBitmapV5Header)(unsafe.Pointer(&buf[0]))
	if header.Size != 124 || header.Width != 2 || header.Height != 2 || header.BitCount != 32 {
		t.Fatalf("unexpected DIBV5 header: %+v", *header)
	}
	offset := int(header.Size)
	if got := buf[offset : offset+4]; !bytes.Equal(got, []byte{90, 80, 70, 255}) {
		t.Fatalf("first bottom-up BGRA pixel = %v, want [90 80 70 255]", got)
	}
	if got := buf[offset+8 : offset+12]; !bytes.Equal(got, []byte{30, 20, 10, 255}) {
		t.Fatalf("first top-row BGRA pixel = %v, want [30 20 10 255]", got)
	}
}
