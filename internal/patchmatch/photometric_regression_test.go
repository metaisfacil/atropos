package patchmatch

import (
	"bytes"
	"context"
	"testing"
)

func TestPhotoFillPreservesFlatColorAndKnownPixels(t *testing.T) {
	src, mask := makeFilledSrc(96, 72)
	for y := 0; y < 72; y++ {
		for x := 0; x < 96; x++ {
			if mask.Pix[y*mask.Stride+x] != 0 {
				i := y*src.Stride + x*4
				src.Pix[i], src.Pix[i+1], src.Pix[i+2] = 5, 20, 240
			}
		}
	}
	before := append([]byte(nil), src.Pix...)
	out, err := Fill(context.Background(), src, mask, 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, src.Pix) {
		t.Fatal("fill modified source")
	}
	for y := 0; y < 72; y++ {
		for x := 0; x < 96; x++ {
			i := y*out.Stride + x*4
			if mask.Pix[y*mask.Stride+x] == 0 {
				if !bytes.Equal(out.Pix[i:i+4], before[i:i+4]) {
					t.Fatalf("known pixel changed at (%d,%d)", x, y)
				}
				continue
			}
			for c, want := range []byte{180, 176, 168, 255} {
				// Pyramid/vote quantization may differ by one code value.
				if absInt(int(out.Pix[i+c])-int(want)) > 1 {
					t.Fatalf("flat repair drifted at (%d,%d) channel %d: got %d want %d +/-1", x, y, c, out.Pix[i+c], want)
				}
			}
		}
	}
}

func BenchmarkPatchMatchPhotoFill(b *testing.B) {
	src, mask := makeFilledSrc(176, 132)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Fill(context.Background(), src, mask, 7, 4); err != nil {
			b.Fatal(err)
		}
	}
}
