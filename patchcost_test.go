package main

import (
	"image"
	"math"
	"testing"
	"unsafe"
)

func TestPMKernelArgsAssemblyLayout(t *testing.T) {
	var args pmKernelArgs
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"targetR", unsafe.Offsetof(args.targetR), 0},
		{"targetG", unsafe.Offsetof(args.targetG), 8},
		{"targetB", unsafe.Offsetof(args.targetB), 16},
		{"targetA", unsafe.Offsetof(args.targetA), 24},
		{"sourceR", unsafe.Offsetof(args.sourceR), 32},
		{"sourceG", unsafe.Offsetof(args.sourceG), 40},
		{"sourceB", unsafe.Offsetof(args.sourceB), 48},
		{"sourceA", unsafe.Offsetof(args.sourceA), 56},
		{"confidence", unsafe.Offsetof(args.confidence), 64},
		{"stride", unsafe.Offsetof(args.stride), 72},
		{"patchSize", unsafe.Offsetof(args.patchSize), 80},
		{"limit", unsafe.Offsetof(args.limit), 88},
	}
	for _, offset := range offsets {
		if offset.got != offset.want {
			t.Errorf("%s offset=%d, assembly expects %d", offset.name, offset.got, offset.want)
		}
	}
	if size := unsafe.Sizeof(args); size != 96 {
		t.Errorf("pmKernelArgs size=%d, assembly expects 96", size)
	}
}

func makePMKernelTestArgs(patchSize int) pmKernelArgs {
	const width, height = 40, 36
	targetImage := image.NewNRGBA(image.Rect(0, 0, width, height))
	sourceImage := image.NewNRGBA(image.Rect(0, 0, width, height))
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*targetImage.Stride + x*4
			targetImage.Pix[i] = byte((x*17 + y*3) & 0xff)
			targetImage.Pix[i+1] = byte((x*5 + y*19) & 0xff)
			targetImage.Pix[i+2] = byte((x*11 + y*7) & 0xff)
			targetImage.Pix[i+3] = byte(80 + (x*9+y*13)%176)
			sourceImage.Pix[i] = byte((x*23 + y*5 + 31) & 0xff)
			sourceImage.Pix[i+1] = byte((x*7 + y*29 + 17) & 0xff)
			sourceImage.Pix[i+2] = byte((x*13 + y*11 + 47) & 0xff)
			sourceImage.Pix[i+3] = byte(80 + (x*15+y*7)%176)
			mask.Pix[y*mask.Stride+x] = byte((x*3 + y*5) % 220)
		}
	}
	target := packPMPixels(targetImage)
	source := packPMPixels(sourceImage)
	confidence, stride, _ := packPMConfidence(mask)
	targetIndex := 7*stride + 8
	sourceIndex := 10*stride + 12
	return pmKernelArgs{
		targetR:    &target.channel[0][targetIndex],
		targetG:    &target.channel[1][targetIndex],
		targetB:    &target.channel[2][targetIndex],
		targetA:    &target.channel[3][targetIndex],
		sourceR:    &source.channel[0][sourceIndex],
		sourceG:    &source.channel[1][sourceIndex],
		sourceB:    &source.channel[2][sourceIndex],
		sourceA:    &source.channel[3][sourceIndex],
		confidence: &confidence[targetIndex],
		stride:     stride,
		patchSize:  patchSize,
		limit:      float32(math.Inf(1)),
	}
}

func absFloat32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
