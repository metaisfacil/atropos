package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
)

// buildMask decodes a base64-encoded PNG mask (white/opaque = fill region) and
// returns an *image.Alpha sized to match the current working image.
func (a *App) buildMask(maskB64 string) (*image.Alpha, error) {
	data, err := base64.StdEncoding.DecodeString(maskB64)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	b := img.Bounds()
	mask := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			aVal := c.A
			if aVal == 0 {
				// No alpha channel: use luminance threshold.
				lum := (299*uint32(c.R) + 587*uint32(c.G) + 114*uint32(c.B)) / 1000
				if lum > 10 {
					aVal = 255
				}
			}
			mask.Pix[(y-b.Min.Y)*mask.Stride+(x-b.Min.X)] = aVal
		}
	}

	srcImg := a.workingImage()
	if srcImg == nil {
		return nil, fmt.Errorf("no image loaded")
	}
	tgtBounds := srcImg.Bounds()
	if mask.Bounds().Eq(tgtBounds) {
		return mask, nil
	}

	// Resize mask to working image dimensions.
	gray := image.NewGray(mask.Bounds())
	for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
		for x := mask.Bounds().Min.X; x < mask.Bounds().Max.X; x++ {
			v := mask.Pix[(y-mask.Bounds().Min.Y)*mask.Stride+(x-mask.Bounds().Min.X)]
			gray.Pix[(y-mask.Bounds().Min.Y)*gray.Stride+(x-mask.Bounds().Min.X)] = v
		}
	}
	resized := resizeGray(gray, tgtBounds.Dx(), tgtBounds.Dy())
	newMask := image.NewAlpha(tgtBounds)
	for y := 0; y < tgtBounds.Dy(); y++ {
		for x := 0; x < tgtBounds.Dx(); x++ {
			newMask.Pix[y*newMask.Stride+x] = resized.Pix[y*resized.Stride+x]
		}
	}
	return newMask, nil
}

// TouchUpApply applies a touch-up fill to the working image using the configured
// backend, saving an undo snapshot so the change can be reverted.
func (a *App) TouchUpApply(maskB64 string, patchSize int, iterations int) (*ProcessResult, error) {
	a.logf("TouchUpApply: backend=%q patchSize=%d iterations=%d", a.touchupBackend, patchSize, iterations)
	if a.currentImage == nil && a.warpedImage == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}

	mask, err := a.buildMask(maskB64)
	if err != nil {
		return nil, err
	}

	srcImg := a.workingImage()
	if srcImg == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}

	var out *image.NRGBA
	if a.touchupBackend == "iopaint" {
		out, err = a.iopaintFill(srcImg, mask)
		if err != nil {
			return nil, fmt.Errorf("IOPaint fill: %w", err)
		}
	} else {
		out = PatchMatchFill(srcImg, mask, patchSize, iterations)
	}

	a.saveUndo()
	a.setWorkingImage(out)

	preview, err := imageToBase64(out)
	if err != nil {
		return nil, err
	}
	b := out.Bounds()
	return &ProcessResult{Preview: preview, Message: "Touch-up applied.", Width: b.Dx(), Height: b.Dy()}, nil
}
