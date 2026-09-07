package patchmatch

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"testing"
)

func TestWorkingROIRetainsLocalContextAtDocumentEdges(t *testing.T) {
	document := image.Rect(0, 0, 1600, 1200)
	for _, dirty := range []image.Rectangle{
		image.Rect(790, 590, 810, 610),
		image.Rect(0, 0, 20, 20),
		image.Rect(1580, 1180, 1600, 1200),
		image.Rect(600, 599, 1000, 601),
	} {
		roi := pmWorkingROI(dirty, document, 7)
		if !dirty.In(roi) || !roi.In(document) {
			t.Fatalf("ROI %v must contain mask %v and stay inside %v", roi, dirty, document)
		}
		if roi.Dx()*roi.Dy() > 360000 {
			t.Fatalf("small repair used excessive source context: %v for %v", roi, dirty)
		}
		if roi.Dx() < 150 || roi.Dy() < 150 {
			t.Fatalf("edge clipping discarded donor context: %v", roi)
		}
	}
}

func repeatedPatternFixture() (*image.NRGBA, *image.NRGBA, *image.Alpha) {
	return repeatedPatternFixtureForMask(image.Rect(118, 60, 138, 116))
}

func repeatedPatternFixtureForMask(dirty image.Rectangle) (*image.NRGBA, *image.NRGBA, *image.Alpha) {
	original := image.NewNRGBA(image.Rect(0, 0, 256, 176))
	for y := 0; y < 176; y++ {
		for x := 0; x < 256; x++ {
			v := color.NRGBA{R: 185, G: 163, B: 113, A: 255}
			// Repeating slanted printing, with two-pixel fine lines. Removing a
			// scratch must continue the line phase rather than copy blank canvas.
			if (x+2*y)%24 < 4 {
				v = color.NRGBA{R: 54, G: 47, B: 41, A: 255}
			}
			original.SetNRGBA(x, y, v)
		}
	}
	src := cloneNRGBA(original)
	mask := image.NewAlpha(src.Bounds())
	for y := dirty.Min.Y; y < dirty.Max.Y; y++ {
		for x := dirty.Min.X; x < dirty.Max.X; x++ {
			mask.SetAlpha(x, y, color.Alpha{A: 255})
			src.SetNRGBA(x, y, color.NRGBA{R: 244, G: 244, B: 244, A: 255})
		}
	}
	return original, src, mask
}

func TestFillContinuesRepeatedPrinting(t *testing.T) {
	for _, test := range []struct {
		name     string
		dirty    image.Rectangle
		maxError float64
	}{
		{"scratch", image.Rect(118, 60, 138, 116), 10},
		{"thin_scratch", image.Rect(125, 46, 131, 130), 2},
		{"large_dab", image.Rect(104, 64, 152, 112), 14},
	} {
		t.Run(test.name, func(t *testing.T) {
			original, src, mask := repeatedPatternFixtureForMask(test.dirty)
			out, err := Fill(context.Background(), src, mask, 7, 4)
			if err != nil {
				t.Fatal(err)
			}
			var difference, count int
			for y := test.dirty.Min.Y; y < test.dirty.Max.Y; y++ {
				for x := test.dirty.Min.X; x < test.dirty.Max.X; x++ {
					for c := 0; c < 3; c++ {
						i := y*src.Stride + x*4 + c
						difference += absInt(int(out.Pix[i]) - int(original.Pix[i]))
						count++
					}
				}
			}
			mae := float64(difference) / float64(count)
			t.Logf("repeated printing masked RGB error %.3f", mae)
			if mae > test.maxError {
				t.Fatalf("printing did not continue through repair: mean RGB error %.3f", mae)
			}
		})
	}
}

func TestReconstructionConvergenceDoesNotHideMovingDetail(t *testing.T) {
	previous := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	next := cloneNRGBA(previous)
	mask := image.NewAlpha(previous.Bounds())
	for i := range mask.Pix {
		mask.Pix[i] = 255
	}
	// One moving pixel barely registers in the average over a large hole, but
	// must still keep the level running for another round.
	next.Pix[64*next.Stride+64*4] = 20
	if pmReconstructionChange(previous, next, mask, mask.Bounds()).settled() {
		t.Fatal("moving detail was hidden by the average reconstruction error")
	}
	next.Pix[64*next.Stride+64*4] = 1
	if !pmReconstructionChange(previous, next, mask, mask.Bounds()).settled() {
		t.Fatal("isolated rounding noise prevented convergence")
	}
	for i := range next.Pix {
		next.Pix[i] = 1
	}
	if pmReconstructionChange(previous, next, mask, mask.Bounds()).settled() {
		t.Fatal("widespread one-code-value drift was treated as converged")
	}
}

func TestEMProgressEndsOnlySlowDecay(t *testing.T) {
	// A level whose change is still shrinking noticeably every round must run its
	// full budget, however many rounds that takes.
	progress := newPMEMProgress()
	for mean := 4.0; mean > 1e-3; mean /= 2 {
		if progress.observe(mean) {
			t.Fatalf("steady progress was treated as exhausted at mean %.4f", mean)
		}
	}

	// Big swings are not the same as slow decay. Something is still moving
	// visibly, so the level keeps its rounds even though no round improves on
	// the best so far.
	progress = newPMEMProgress()
	for round, mean := range []float64{4.0, 3.6, 4.1, 3.7, 4.2, 3.8, 4.0} {
		if progress.observe(mean) {
			t.Fatalf("round %d ended the level while it still moved by %.2f code values", round, mean)
		}
	}

	// Measured from the second-finest level of the 43px stroke. Its change falls
	// quickly for a few rounds, then shrinks by only a few percent per round
	// while the search swaps between source patches that score the same.
	series := []float64{
		0.5250, 0.2805, 0.2284, 0.1476, 0.1228, 0.1162, 0.1154, 0.1039, 0.1038,
		0.0960, 0.0776, 0.0685, 0.0675, 0.0696, 0.0703, 0.0650, 0.0839,
	}
	progress = newPMEMProgress()
	stopped := -1
	for round, mean := range series {
		if progress.observe(mean) && stopped < 0 {
			stopped = round
		}
	}
	if stopped < 0 {
		t.Fatal("slow decay never ended the level")
	}
	if stopped > len(series)/2 {
		t.Fatalf("slow decay ended at round %d of %d, too late to save the churn", stopped, len(series))
	}
	// Nothing may stop the level while the change is still falling quickly.
	if series[stopped] > 0.25 {
		t.Fatalf("round %d still moved by %.4f code values", stopped, series[stopped])
	}
}

func TestFillOutpaintPreservesSourceAndCoverage(t *testing.T) {
	// Nonzero origins and a subimage stride exercise the same public API used
	// by warp fill without assuming tightly packed zero-origin input.
	backing := image.NewNRGBA(image.Rect(0, 0, 120, 100))
	src := backing.SubImage(image.Rect(11, 9, 107, 81)).(*image.NRGBA)
	mask := image.NewAlpha(image.Rect(3, 5, 99, 77))
	for y := 0; y < 72; y++ {
		for x := 0; x < 96; x++ {
			src.SetNRGBA(x+11, y+9, color.NRGBA{R: 144, G: 172, B: 195, A: 255})
			if x < 10 {
				mask.SetAlpha(x+3, y+5, color.Alpha{A: 255})
				src.SetNRGBA(x+11, y+9, color.NRGBA{})
			} else if x == 10 {
				mask.SetAlpha(x+3, y+5, color.Alpha{A: 128})
			}
		}
	}
	before := append([]byte(nil), backing.Pix...)
	out, err := Fill(context.Background(), src, mask, 9, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(backing.Pix, before) {
		t.Fatal("source backing was modified")
	}
	for y := 0; y < 72; y++ {
		for x := 0; x < 96; x++ {
			got := out.NRGBAAt(x, y)
			if x >= 11 {
				if got != src.NRGBAAt(x+11, y+9) {
					t.Fatalf("known pixel changed at %d,%d", x, y)
				}
			} else if got.A < 250 || absInt(int(got.R)-144) > 2 || absInt(int(got.G)-172) > 2 || absInt(int(got.B)-195) > 2 {
				t.Fatalf("outpaint failed at %d,%d: %v", x, y, got)
			}
		}
	}
}

func BenchmarkPatchMatchRepeatedPrinting(b *testing.B) {
	_, src, mask := repeatedPatternFixture()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Fill(context.Background(), src, mask, 7, 4); err != nil {
			b.Fatal(err)
		}
	}
}
