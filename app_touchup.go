package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
)

// TouchUpFill accepts a base64-encoded PNG mask (white where the user painted)
// and returns a non-mutating preview produced by the PatchMatch-based filler.
func (a *App) TouchUpFill(maskB64 string, patchSize int, iterations int) (*ProcessResult, error) {
	a.logf("TouchUpFill: patchSize=%d iterations=%d", patchSize, iterations)
	if a.currentImage == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}

	data, err := base64.StdEncoding.DecodeString(maskB64)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	// Convert decoded image to *image.Alpha
	b := img.Bounds()
	mask := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			aVal := c.A
			if aVal == 0 {
				// if no alpha channel, use luminance threshold
				lum := (299*uint32(c.R) + 587*uint32(c.G) + 114*uint32(c.B)) / 1000
				if lum > 10 {
					aVal = 255
				}
			}
			mask.Pix[(y-b.Min.Y)*mask.Stride+(x-b.Min.X)] = aVal
		}
	}

	// Resize mask to the working image size if needed
	srcImg := a.workingImage()
	if srcImg == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}
	tgtBounds := srcImg.Bounds()
	if !mask.Bounds().Eq(tgtBounds) {
		// convert Alpha -> Gray -> resizeGray -> Alpha
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
				v := resized.Pix[y*resized.Stride+x]
				newMask.Pix[y*newMask.Stride+x] = v
			}
		}
		mask = newMask
	}

	// Run PatchMatchFill (non-destructive preview) on the working image
	out := PatchMatchFill(srcImg, mask, patchSize, iterations)

	preview, err := imageToBase64(out)
	if err != nil {
		return nil, err
	}
	b2 := out.Bounds()
	return &ProcessResult{Preview: preview, Message: "Touch-up preview", Width: b2.Dx(), Height: b2.Dy()}, nil
}

// TouchUpApply applies a touch-up fill to the working image, saving an undo
// snapshot so the change can be reverted. Returns the new preview.
func (a *App) TouchUpApply(maskB64 string, patchSize int, iterations int) (*ProcessResult, error) {
	a.logf("TouchUpApply: patchSize=%d iterations=%d", patchSize, iterations)
	if a.currentImage == nil && a.warpedImage == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}

	data, err := base64.StdEncoding.DecodeString(maskB64)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	// Convert decoded image to *image.Alpha
	b := img.Bounds()
	mask := image.NewAlpha(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			aVal := c.A
			if aVal == 0 {
				lum := (299*uint32(c.R) + 587*uint32(c.G) + 114*uint32(c.B)) / 1000
				if lum > 10 {
					aVal = 255
				}
			}
			mask.Pix[(y-b.Min.Y)*mask.Stride+(x-b.Min.X)] = aVal
		}
	}

	// Resize mask to the working image size if needed
	srcImg := a.workingImage()
	if srcImg == nil {
		return &ProcessResult{Message: "No image loaded"}, nil
	}
	tgtBounds := srcImg.Bounds()
	if !mask.Bounds().Eq(tgtBounds) {
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
				v := resized.Pix[y*resized.Stride+x]
				newMask.Pix[y*newMask.Stride+x] = v
			}
		}
		mask = newMask
	}

	// Run PatchMatchFill and apply result
	out := PatchMatchFill(srcImg, mask, patchSize, iterations)

	// save undo snapshot and apply
	a.saveUndo()
	a.setWorkingImage(out)

	preview, err := imageToBase64(out)
	if err != nil {
		return nil, err
	}
	b2 := out.Bounds()
	return &ProcessResult{Preview: preview, Message: "Touch-up applied.", Width: b2.Dx(), Height: b2.Dy()}, nil
}
