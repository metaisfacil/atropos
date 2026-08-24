//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"math/bits"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clipboardFormatDIB = 8
	bitmapRGB          = 0
	bitmapBitfields    = 3
	bitmapAlphaFields  = 6
)

var (
	procGetClipboardData         = clipboardUser32.NewProc("GetClipboardData")
	procIsClipboardFormatPresent = clipboardUser32.NewProc("IsClipboardFormatAvailable")
	procRegisterClipboardFormat  = clipboardUser32.NewProc("RegisterClipboardFormatW")
	procClipboardGlobalSize      = clipboardKernel32.NewProc("GlobalSize")
)

func clipboardFormatAvailable(format uintptr) bool {
	available, _, _ := procIsClipboardFormatPresent.Call(format)
	return available != 0
}

func registeredClipboardFormat(name string) (uintptr, error) {
	wide, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	format, _, callErr := procRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(wide)))
	if format == 0 {
		return 0, fmt.Errorf("register clipboard format %q: %v", name, callErr)
	}
	return format, nil
}

func clipboardGlobalBytes(format uintptr) ([]byte, error) {
	handle, _, callErr := procGetClipboardData.Call(format)
	if handle == 0 {
		return nil, fmt.Errorf("get clipboard data: %v", callErr)
	}
	size, _, callErr := procClipboardGlobalSize.Call(handle)
	if size == 0 || size > uintptr(^uint(0)>>1) {
		return nil, fmt.Errorf("get clipboard data size: %v", callErr)
	}
	ptr, _, callErr := procClipboardGlobalLock.Call(handle)
	if ptr == 0 {
		return nil, fmt.Errorf("lock clipboard data: %v", callErr)
	}
	defer procClipboardGlobalUnlock.Call(handle)
	return unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size)), nil
}

func readImageFromClipboard() (*image.NRGBA, string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := openWindowsClipboard(); err != nil {
		return nil, "", err
	}
	defer procCloseClipboard.Call()

	var lastErr error
	for _, format := range []uintptr{clipboardFormatDIBV5, clipboardFormatDIB} {
		if !clipboardFormatAvailable(format) {
			continue
		}
		data, err := clipboardGlobalBytes(format)
		if err != nil {
			lastErr = err
			continue
		}
		img, err := decodeClipboardDIB(data)
		if err != nil {
			lastErr = fmt.Errorf("decode clipboard bitmap: %w", err)
			continue
		}
		return img, "BMP", nil
	}

	for _, name := range []string{"PNG", "image/png", "JFIF", "image/jpeg"} {
		format, err := registeredClipboardFormat(name)
		if err != nil || !clipboardFormatAvailable(format) {
			continue
		}
		data, err := clipboardGlobalBytes(format)
		if err != nil {
			lastErr = err
			continue
		}
		decoded, decodedFormat, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			continue
		}
		return toNRGBA(decoded), decodedFormat, nil
	}

	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("clipboard does not contain an image")
}

func dibChannel(value, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := bits.TrailingZeros32(mask)
	maximum := mask >> shift
	component := (value & mask) >> shift
	return uint8((uint64(component)*255 + uint64(maximum)/2) / uint64(maximum))
}

func decodeClipboardDIB(data []byte) (*image.NRGBA, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("bitmap header is truncated")
	}
	headerSize := int(binary.LittleEndian.Uint32(data[0:4]))
	if headerSize < 40 || headerSize > len(data) {
		return nil, fmt.Errorf("unsupported bitmap header size %d", headerSize)
	}
	widthSigned := int32(binary.LittleEndian.Uint32(data[4:8]))
	heightSigned := int32(binary.LittleEndian.Uint32(data[8:12]))
	if widthSigned < 1 || heightSigned == 0 || heightSigned == -1<<31 {
		return nil, fmt.Errorf("invalid bitmap dimensions %dx%d", widthSigned, heightSigned)
	}
	width := int(widthSigned)
	height := int(heightSigned)
	topDown := height < 0
	if topDown {
		height = -height
	}
	maxInt := int64(^uint(0) >> 1)
	if int64(width)*int64(height) > maxInt/4 {
		return nil, fmt.Errorf("clipboard bitmap is too large")
	}

	planes := binary.LittleEndian.Uint16(data[12:14])
	bitCount := binary.LittleEndian.Uint16(data[14:16])
	compression := binary.LittleEndian.Uint32(data[16:20])
	if planes != 1 || (bitCount != 24 && bitCount != 32) {
		return nil, fmt.Errorf("unsupported clipboard bitmap: planes=%d bitCount=%d", planes, bitCount)
	}
	if compression != bitmapRGB && compression != bitmapBitfields && compression != bitmapAlphaFields {
		return nil, fmt.Errorf("unsupported clipboard bitmap compression %d", compression)
	}

	pixelOffset := headerSize
	redMask, greenMask, blueMask, alphaMask := uint32(0), uint32(0), uint32(0), uint32(0)
	if headerSize >= 52 {
		redMask = binary.LittleEndian.Uint32(data[40:44])
		greenMask = binary.LittleEndian.Uint32(data[44:48])
		blueMask = binary.LittleEndian.Uint32(data[48:52])
		if headerSize >= 56 {
			alphaMask = binary.LittleEndian.Uint32(data[52:56])
		}
	} else if compression == bitmapBitfields || compression == bitmapAlphaFields {
		maskCount := 3
		if compression == bitmapAlphaFields {
			maskCount = 4
		}
		maskBytes := maskCount * 4
		if pixelOffset+maskBytes > len(data) {
			return nil, fmt.Errorf("bitmap color masks are truncated")
		}
		redMask = binary.LittleEndian.Uint32(data[pixelOffset : pixelOffset+4])
		greenMask = binary.LittleEndian.Uint32(data[pixelOffset+4 : pixelOffset+8])
		blueMask = binary.LittleEndian.Uint32(data[pixelOffset+8 : pixelOffset+12])
		if maskCount == 4 {
			alphaMask = binary.LittleEndian.Uint32(data[pixelOffset+12 : pixelOffset+16])
		}
		pixelOffset += maskBytes
	}

	colorCount := int(binary.LittleEndian.Uint32(data[32:36]))
	if colorCount > 0 {
		colorBytes := int64(colorCount) * 4
		if colorBytes > int64(len(data)-pixelOffset) {
			return nil, fmt.Errorf("bitmap color table is truncated")
		}
		pixelOffset += int(colorBytes)
	}
	rowStride64 := ((int64(width)*int64(bitCount) + 31) / 32) * 4
	if rowStride64 > maxInt || int64(height) > maxInt/rowStride64 {
		return nil, fmt.Errorf("clipboard bitmap is too large")
	}
	rowStride := int(rowStride64)
	pixelBytes := rowStride * height
	if pixelOffset > len(data) || pixelBytes > len(data)-pixelOffset {
		return nil, fmt.Errorf("bitmap pixels are truncated")
	}

	useMasks := bitCount == 32 && redMask != 0 && greenMask != 0 && blueMask != 0
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	allAlphaZero := alphaMask != 0
	for y := 0; y < height; y++ {
		srcY := y
		if !topDown {
			srcY = height - 1 - y
		}
		row := data[pixelOffset+srcY*rowStride:]
		dst := img.Pix[y*img.Stride:]
		for x := 0; x < width; x++ {
			si := x * int(bitCount/8)
			di := x * 4
			if useMasks {
				value := binary.LittleEndian.Uint32(row[si : si+4])
				dst[di+0] = dibChannel(value, redMask)
				dst[di+1] = dibChannel(value, greenMask)
				dst[di+2] = dibChannel(value, blueMask)
				if alphaMask != 0 {
					dst[di+3] = dibChannel(value, alphaMask)
					if dst[di+3] != 0 {
						allAlphaZero = false
					}
				} else {
					dst[di+3] = 255
				}
			} else {
				dst[di+0] = row[si+2]
				dst[di+1] = row[si+1]
				dst[di+2] = row[si+0]
				dst[di+3] = 255
			}
		}
	}
	if allAlphaZero {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	return img, nil
}
