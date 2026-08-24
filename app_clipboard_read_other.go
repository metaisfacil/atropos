//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"image"

	"atropos/internal/raster"

	"golang.design/x/clipboard"
)

func readImageFromClipboard() (*image.NRGBA, string, error) {
	if err := clipboard.Init(); err != nil {
		return nil, "", fmt.Errorf("initialize clipboard: %w", err)
	}
	data := clipboard.Read(clipboard.FmtImage)
	if len(data) == 0 {
		return nil, "", fmt.Errorf("clipboard does not contain an image")
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode clipboard image: %w", err)
	}
	return raster.ToNRGBA(decoded), format, nil
}
