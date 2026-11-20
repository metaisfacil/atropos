//go:build windows

package ui

import (
	"image"
	"image/color"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestCreateResampledIcon(t *testing.T) {
	modGdi32 := windows.NewLazySystemDLL("gdi32.dll")
	procGetIconInfo := modUser32.NewProc("GetIconInfo")
	procGetObjectW := modGdi32.NewProc("GetObjectW")
	procDeleteObject := modGdi32.NewProc("DeleteObject")

	type iconInfo struct {
		FIcon    int32
		XHotspot uint32
		YHotspot uint32
		HbmMask  uintptr
		HbmColor uintptr
	}
	type bitmap struct {
		BmType       int32
		BmWidth      int32
		BmHeight     int32
		BmWidthBytes int32
		BmPlanes     uint16
		BmBitsPixel  uint16
		BmBits       uintptr
	}

	src := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 180, A: 255})
		}
	}

	for _, sz := range []int{16, 20, 32, 40} {
		hIcon := createResampledIcon(src, sz, sz)
		if hIcon == 0 {
			t.Fatalf("createResampledIcon(%d) returned 0", sz)
		}

		var ii iconInfo
		ok, _, err := procGetIconInfo.Call(hIcon, uintptr(unsafe.Pointer(&ii)))
		if ok == 0 {
			procDestroyIcon.Call(hIcon)
			t.Fatalf("GetIconInfo(%d) failed: %v", sz, err)
		}

		var bm bitmap
		got, _, err := procGetObjectW.Call(ii.HbmColor, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm)))
		if ii.HbmColor != 0 {
			procDeleteObject.Call(ii.HbmColor)
		}
		if ii.HbmMask != 0 {
			procDeleteObject.Call(ii.HbmMask)
		}
		procDestroyIcon.Call(hIcon)

		if got == 0 {
			t.Fatalf("GetObjectW(%d) failed: %v", sz, err)
		}
		if int(bm.BmWidth) != sz || int(bm.BmHeight) != sz {
			t.Fatalf("size %d: got bitmap %dx%d, want %dx%d", sz, bm.BmWidth, bm.BmHeight, sz, sz)
		}
	}
}
