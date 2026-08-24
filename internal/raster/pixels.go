package raster

import (
	goruntime "runtime"
	"sync"
)

type levelsKernelArgs struct {
	pix     *uint8
	n       int
	blackPt float64
	scale   float64
}

type grayscaleKernelArgs struct {
	src      *uint8
	dst      *uint8
	n        int
	accent   uint8
	subtract uint8
	_pad     [6]uint8
}

type maskBlendKernelArgs struct {
	src   *uint8
	dst   *uint8
	alpha *float64
	n     int
	bgR   float64
	bgG   float64
	bgB   float64
}

// ApplyLevelsPixels applies a levels transform to an NRGBA pixel buffer.
func ApplyLevelsPixels(pix []uint8, blackPt int, scale float64) {
	vectorN := levelsVectorCount(len(pix))
	if vectorN > 0 {
		blocks := vectorN / 16
		workers := 1
		if vectorN >= 1<<20 {
			workers = goruntime.NumCPU()
		}
		parallelFor(blocks, workers, func(start, end int) {
			startByte, n := start*16, (end-start)*16
			args := levelsKernelArgs{pix: &pix[startByte], n: n, blackPt: float64(blackPt), scale: scale}
			applyLevelsSIMD(&args)
		})
	} else if len(pix) >= 1<<20 {
		parallelFor(len(pix)/4, goruntime.NumCPU(), func(start, end int) {
			applyLevelsScalar(pix, blackPt, scale, start*4, end*4)
		})
		return
	}
	applyLevelsScalar(pix, blackPt, scale, vectorN, len(pix))
}

func applyLevelsScalar(pix []uint8, blackPt int, scale float64, start, end int) {
	for i := start; i < end; i += 4 {
		pix[i] = ClampByte(int(float64(int(pix[i])-blackPt) * scale))
		pix[i+1] = ClampByte(int(float64(int(pix[i+1])-blackPt) * scale))
		pix[i+2] = ClampByte(int(float64(int(pix[i+2])-blackPt) * scale))
	}
}

// ClampByte constrains an integer to the byte range.
func ClampByte(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func parallelFor(total, workers int, fn func(start, end int)) {
	if workers <= 1 || total <= 1 {
		fn(0, total)
		return
	}
	if workers > total {
		workers = total
	}

	var wg sync.WaitGroup
	chunk := (total + workers - 1) / workers
	for worker := 0; worker < workers; worker++ {
		start := worker * chunk
		if start >= total {
			break
		}
		end := start + chunk
		if end > total {
			end = total
		}
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			fn(start, end)
		}(start, end)
	}
	wg.Wait()
}

func grayscaleAccentRow(src, dst []uint8, accent int) {
	vectorN := grayscaleVectorCount(len(dst))
	if vectorN > 0 {
		magnitude := accent
		subtract := uint8(0)
		if magnitude < 0 {
			magnitude = -magnitude
			subtract = 1
		}
		if magnitude > 255 {
			magnitude = 255
		}
		args := grayscaleKernelArgs{
			src: &src[0], dst: &dst[0], n: vectorN,
			accent: uint8(magnitude), subtract: subtract,
		}
		grayscaleAccentSIMD(&args)
	}
	for x := vectorN; x < len(dst); x++ {
		off := x * 4
		r := uint32(ClampByte(int(src[off]) + accent))
		g := uint32(ClampByte(int(src[off+1]) + accent))
		b := uint32(ClampByte(int(src[off+2]) + accent))
		dst[x] = uint8((19595*r + 38470*g + 7471*b + 32768) >> 16)
	}
}

// BlendMaskRow composites one NRGBA row over an opaque background.
func BlendMaskRow(src, dst []uint8, alpha []float64, bgR, bgG, bgB uint8) {
	vectorN := maskBlendVectorCount(len(alpha))
	if vectorN > 0 {
		args := maskBlendKernelArgs{
			src: &src[0], dst: &dst[0], alpha: &alpha[0], n: vectorN,
			bgR: float64(bgR), bgG: float64(bgG), bgB: float64(bgB),
		}
		maskBlendSIMD(&args)
	}
	for x := vectorN; x < len(alpha); x++ {
		a := alpha[x]
		si, di := x*4, x*4
		if a <= 0 {
			dst[di], dst[di+1], dst[di+2], dst[di+3] = bgR, bgG, bgB, 255
			continue
		}
		if a >= 1 {
			dst[di], dst[di+1], dst[di+2], dst[di+3] = src[si], src[si+1], src[si+2], 255
			continue
		}
		inv := 1 - a
		dst[di] = ClampByte(int(float64(src[si])*a + float64(bgR)*inv))
		dst[di+1] = ClampByte(int(float64(src[si+1])*a + float64(bgG)*inv))
		dst[di+2] = ClampByte(int(float64(src[si+2])*a + float64(bgB)*inv))
		dst[di+3] = 255
	}
}
