//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	"golang.design/x/clipboard"
)

// Non-Windows system clipboards conventionally exchange raster images as PNG.
// Keep encoding and clipboard ownership entirely in the backend; Windows uses
// the direct DIBV5 path and does not pay this encoding cost.
func copyImageRegionToClipboard(src *image.NRGBA, rect image.Rectangle) error {
	if src == nil {
		return fmt.Errorf("no image loaded")
	}
	rect = rect.Intersect(src.Bounds())
	if rect.Empty() {
		return fmt.Errorf("clipboard selection is empty")
	}

	var buf bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&buf, src.SubImage(rect)); err != nil {
		return fmt.Errorf("encode clipboard image: %w", err)
	}
	if err := clipboard.Init(); err != nil {
		return fmt.Errorf("initialize clipboard: %w", err)
	}
	if changed := clipboard.Write(clipboard.FmtImage, buf.Bytes()); changed == nil {
		return fmt.Errorf("native clipboard rejected the image")
	}
	return nil
}
