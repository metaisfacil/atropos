package main

import goruntime "runtime"

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

func applyLevelsPixels(pix []uint8, blackPt int, scale float64) {
	vectorN := levelsVectorCount(len(pix))
	if vectorN > 0 {
		blocks := vectorN / 16
		workers := 1
		if vectorN >= 1<<20 {
			workers = goruntime.NumCPU()
		}
		pFor(blocks, workers, func(start, end int) {
			startByte, n := start*16, (end-start)*16
			args := levelsKernelArgs{pix: &pix[startByte], n: n, blackPt: float64(blackPt), scale: scale}
			applyLevelsSIMD(&args)
		})
	} else if len(pix) >= 1<<20 {
		pFor(len(pix)/4, goruntime.NumCPU(), func(start, end int) {
			applyLevelsScalar(pix, blackPt, scale, start*4, end*4)
		})
		return
	}
	applyLevelsScalar(pix, blackPt, scale, vectorN, len(pix))
}

func applyLevelsScalar(pix []uint8, blackPt int, scale float64, start, end int) {
	for i := start; i < end; i += 4 {
		pix[i] = clampByte(int(float64(int(pix[i])-blackPt) * scale))
		pix[i+1] = clampByte(int(float64(int(pix[i+1])-blackPt) * scale))
		pix[i+2] = clampByte(int(float64(int(pix[i+2])-blackPt) * scale))
	}
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
		r := uint32(clampByte(int(src[off]) + accent))
		g := uint32(clampByte(int(src[off+1]) + accent))
		b := uint32(clampByte(int(src[off+2]) + accent))
		dst[x] = uint8((19595*r + 38470*g + 7471*b + 32768) >> 16)
	}
}

func maskBlendRow(src, dst []uint8, alpha []float64, bgR, bgG, bgB uint8) {
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
		dst[di] = clampByte(int(float64(src[si])*a + float64(bgR)*inv))
		dst[di+1] = clampByte(int(float64(src[si+1])*a + float64(bgG)*inv))
		dst[di+2] = clampByte(int(float64(src[si+2])*a + float64(bgB)*inv))
		dst[di+3] = 255
	}
}
