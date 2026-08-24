//go:build windows

package main

import (
	"fmt"
	"image"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	clipboardFormatDIBV5 = 17
	globalMemoryMoveable = 0x0002
)

type clipboardBitmapV5Header struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
	RedMask       uint32
	GreenMask     uint32
	BlueMask      uint32
	AlphaMask     uint32
	CSType        uint32
	Endpoints     [9]int32
	GammaRed      uint32
	GammaGreen    uint32
	GammaBlue     uint32
	Intent        uint32
	ProfileData   uint32
	ProfileSize   uint32
	Reserved      uint32
}

var (
	clipboardUser32           = windows.NewLazySystemDLL("user32.dll")
	clipboardKernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard         = clipboardUser32.NewProc("OpenClipboard")
	procCloseClipboard        = clipboardUser32.NewProc("CloseClipboard")
	procEmptyClipboard        = clipboardUser32.NewProc("EmptyClipboard")
	procSetClipboardData      = clipboardUser32.NewProc("SetClipboardData")
	procClipboardGlobalAlloc  = clipboardKernel32.NewProc("GlobalAlloc")
	procClipboardGlobalLock   = clipboardKernel32.NewProc("GlobalLock")
	procClipboardGlobalUnlock = clipboardKernel32.NewProc("GlobalUnlock")
	procClipboardGlobalFree   = clipboardKernel32.NewProc("GlobalFree")
)

func openWindowsClipboard() error {
	deadline := time.Now().Add(750 * time.Millisecond)
	for {
		opened, _, callErr := procOpenClipboard.Call(0)
		if opened != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("open clipboard: %v", callErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func clipboardDIBV5Size(rect image.Rectangle) (int, error) {
	width, height := rect.Dx(), rect.Dy()
	if width < 1 || height < 1 {
		return 0, fmt.Errorf("clipboard selection is empty")
	}
	if int64(width) > int64(^uint32(0)>>1) || int64(height) > int64(^uint32(0)>>1) {
		return 0, fmt.Errorf("clipboard selection is too large")
	}
	headerSize := int64(unsafe.Sizeof(clipboardBitmapV5Header{}))
	pixels := int64(width) * int64(height)
	maxInt := int64(^uint(0) >> 1)
	if pixels > int64(^uint32(0))/4 || pixels > (maxInt-headerSize)/4 {
		return 0, fmt.Errorf("clipboard selection is too large")
	}
	pixelBytes := pixels * 4
	return int(headerSize + pixelBytes), nil
}

func writeClipboardDIBV5(dst []byte, src *image.NRGBA, rect image.Rectangle) error {
	rect = rect.Intersect(src.Bounds())
	size, err := clipboardDIBV5Size(rect)
	if err != nil {
		return err
	}
	if len(dst) < size {
		return fmt.Errorf("clipboard bitmap buffer is too small")
	}

	headerSize := int(unsafe.Sizeof(clipboardBitmapV5Header{}))
	header := (*clipboardBitmapV5Header)(unsafe.Pointer(&dst[0]))
	*header = clipboardBitmapV5Header{
		Size:      uint32(headerSize),
		Width:     int32(rect.Dx()),
		Height:    int32(rect.Dy()), // positive height stores bottom row first
		Planes:    1,
		BitCount:  32,
		SizeImage: uint32(rect.Dx() * rect.Dy() * 4),
		RedMask:   0x00ff0000,
		GreenMask: 0x0000ff00,
		BlueMask:  0x000000ff,
		AlphaMask: 0xff000000,
		CSType:    0x73524742, // LCS_sRGB
		Intent:    4,          // LCS_GM_IMAGES
	}

	width := rect.Dx()
	for dstY := 0; dstY < rect.Dy(); dstY++ {
		srcY := rect.Max.Y - 1 - dstY
		srcOffset := src.PixOffset(rect.Min.X, srcY)
		dstOffset := headerSize + dstY*width*4
		for x := 0; x < width; x++ {
			si := srcOffset + x*4
			di := dstOffset + x*4
			dst[di+0] = src.Pix[si+2]
			dst[di+1] = src.Pix[si+1]
			dst[di+2] = src.Pix[si+0]
			dst[di+3] = src.Pix[si+3]
		}
	}
	return nil
}

func copyImageRegionToClipboard(src *image.NRGBA, rect image.Rectangle) error {
	if src == nil {
		return fmt.Errorf("no image loaded")
	}
	rect = rect.Intersect(src.Bounds())
	size, err := clipboardDIBV5Size(rect)
	if err != nil {
		return err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := openWindowsClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()

	hMem, _, callErr := procClipboardGlobalAlloc.Call(globalMemoryMoveable, uintptr(size))
	if hMem == 0 {
		return fmt.Errorf("allocate clipboard bitmap: %v", callErr)
	}
	ownedByApp := true
	defer func() {
		if ownedByApp {
			procClipboardGlobalFree.Call(hMem)
		}
	}()

	ptr, _, callErr := procClipboardGlobalLock.Call(hMem)
	if ptr == 0 {
		return fmt.Errorf("lock clipboard bitmap: %v", callErr)
	}
	pixels := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)
	if err := writeClipboardDIBV5(pixels, src, rect); err != nil {
		procClipboardGlobalUnlock.Call(hMem)
		return err
	}
	procClipboardGlobalUnlock.Call(hMem)

	if emptied, _, callErr := procEmptyClipboard.Call(); emptied == 0 {
		return fmt.Errorf("empty clipboard: %v", callErr)
	}
	if handle, _, callErr := procSetClipboardData.Call(clipboardFormatDIBV5, hMem); handle == 0 {
		return fmt.Errorf("set clipboard bitmap: %v", callErr)
	}
	// SetClipboardData transfers ownership of the movable memory to Windows.
	ownedByApp = false
	return nil
}
