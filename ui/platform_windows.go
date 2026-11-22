//go:build windows

package ui

import (
	"bytes"
	"image"
	"image/png"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf16"
	"unsafe"

	"gioui.org/app"
	"github.com/ncruces/zenity"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/sys/windows"
)

const (
	wmDropFiles  = 0x0233
	wmDpiChanged = 0x02E0
	wmSetIcon    = 0x0080
	iconSmall    = 0
	iconBig      = 1
	gclpHIcon    = ^uintptr(13) // -14
	gclpHIconSm  = ^uintptr(33) // -34
	smCxIcon     = 11
	smCyIcon     = 12
	smCxSmIcon   = 49
	smCySmIcon   = 50
	mapVkVscToVk = 3
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modShell32  = windows.NewLazySystemDLL("shell32.dll")
	modComctl32 = windows.NewLazySystemDLL("comctl32.dll")
	modShcore   = windows.NewLazySystemDLL("shcore.dll")

	procSetProcessDpiAwarenessContext = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDpiAwareness        = modShcore.NewProc("SetProcessDpiAwareness")
	procSetProcessDPIAware            = modUser32.NewProc("SetProcessDPIAware")
	procSetWindowSubclass             = modComctl32.NewProc("SetWindowSubclass")
	procDefSubclassProc               = modComctl32.NewProc("DefSubclassProc")
	procDragAcceptFiles               = modShell32.NewProc("DragAcceptFiles")
	procDragQueryFileW                = modShell32.NewProc("DragQueryFileW")
	procDragFinish                    = modShell32.NewProc("DragFinish")
	procCreateIconFromResourceEx      = modUser32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon                   = modUser32.NewProc("DestroyIcon")
	procSendMessageW                  = modUser32.NewProc("SendMessageW")
	procSetClassLongPtrW              = modUser32.NewProc("SetClassLongPtrW")
	procGetDpiForWindow               = modUser32.NewProc("GetDpiForWindow")
	procGetDpiForSystem               = modUser32.NewProc("GetDpiForSystem")
	procGetSystemMetricsForDpi        = modUser32.NewProc("GetSystemMetricsForDpi")
	procGetSystemMetrics              = modUser32.NewProc("GetSystemMetrics")
	procGetKeyboardLayout             = modUser32.NewProc("GetKeyboardLayout")
	procMapVirtualKeyExW              = modUser32.NewProc("MapVirtualKeyExW")
	procToUnicodeEx                   = modUser32.NewProc("ToUnicodeEx")

	winHookMu      sync.Mutex
	winHookedHWND  uintptr
	winDropHandler func([]string)
	winIconImg     image.Image
	winIconBig     uintptr
	winIconSmall   uintptr
	winSubclassCB  = syscall.NewCallback(winWindowSubclassProc)
)

// InitPlatformDPI sets process-wide PerMonitorV2 DPI awareness before any
// window or thread initialization so Win32 pointer messages stay in physical pixels.
func InitPlatformDPI() string {
	if procSetProcessDpiAwarenessContext.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = (DPI_AWARENESS_CONTEXT)-4
		if r, _, _ := procSetProcessDpiAwarenessContext.Call(^uintptr(3)); r != 0 {
			return "PerMonitorV2"
		}
	}
	if procSetProcessDpiAwareness.Find() == nil {
		const processPerMonitorDPIAware = 2
		const eAccessDenied = 0x80070005
		if r, _, _ := procSetProcessDpiAwareness.Call(processPerMonitorDPIAware); r == 0 || r == eAccessDenied {
			return "PerMonitor"
		}
	}
	if procSetProcessDPIAware.Find() == nil {
		if r, _, _ := procSetProcessDPIAware.Call(); r != 0 {
			return "System"
		}
	}
	return "unavailable"
}

func winWindowSubclassProc(hwnd, msg, wParam, lParam, uIdSubclass, dwRefData uintptr) uintptr {
	if msg == wmDropFiles {
		hDrop := wParam
		count, _, _ := procDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
		paths := make([]string, 0, count)
		for i := uintptr(0); i < count; i++ {
			n, _, _ := procDragQueryFileW.Call(hDrop, i, 0, 0)
			if n == 0 {
				continue
			}
			buf := make([]uint16, n+1)
			procDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&buf[0])), n+1)
			if s := windows.UTF16ToString(buf); s != "" {
				paths = append(paths, s)
			}
		}
		procDragFinish.Call(hDrop)

		winHookMu.Lock()
		handler := winDropHandler
		winHookMu.Unlock()
		if handler != nil && len(paths) > 0 {
			handler(paths)
		}
		return 0
	}
	if msg == wmDpiChanged {
		ret, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
		setWindowsWindowIcon(hwnd, nil, wParam&0xFFFF)
		return ret
	}
	ret, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
	return ret
}

func (e *EditorUI) handlePlatformViewEvent(ev app.ViewEvent) {
	wev, ok := ev.(app.Win32ViewEvent)
	if !ok || !wev.Valid() {
		return
	}
	hwnd := wev.HWND
	if e.host != nil {
		e.host.hwnd.Store(hwnd)
	}
	icon := e.appIconPNG
	go e.win.Run(func() {
		attachWindowsWindowHooks(hwnd, icon, e.handleDroppedFiles)
	})
}

func attachWindowsWindowHooks(hwnd uintptr, iconPNG []byte, onFilesDropped func([]string)) {
	if hwnd == 0 {
		return
	}
	winHookMu.Lock()
	winDropHandler = onFilesDropped
	alreadyHooked := winHookedHWND == hwnd
	if !alreadyHooked {
		winHookedHWND = hwnd
	}
	winHookMu.Unlock()

	if alreadyHooked {
		return
	}

	procDragAcceptFiles.Call(hwnd, 1)
	if procSetWindowSubclass.Find() == nil {
		procSetWindowSubclass.Call(hwnd, winSubclassCB, 1, 0)
	}
	setWindowsWindowIcon(hwnd, iconPNG, 0)
}

func windowsWindowDPI(hwnd uintptr) uintptr {
	if hwnd != 0 && procGetDpiForWindow.Find() == nil {
		if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi > 0 {
			return dpi
		}
	}
	if procGetDpiForSystem.Find() == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
			return dpi
		}
	}
	return 96
}

func windowsIconSize(hwnd, dpi uintptr, smX, smY, fallback int) (int, int) {
	if dpi == 0 {
		dpi = windowsWindowDPI(hwnd)
	}
	var w, h int
	if procGetSystemMetricsForDpi.Find() == nil {
		rx, _, _ := procGetSystemMetricsForDpi.Call(uintptr(smX), dpi)
		ry, _, _ := procGetSystemMetricsForDpi.Call(uintptr(smY), dpi)
		w, h = int(rx), int(ry)
	} else if procGetSystemMetrics.Find() == nil {
		rx, _, _ := procGetSystemMetrics.Call(uintptr(smX))
		ry, _, _ := procGetSystemMetrics.Call(uintptr(smY))
		w, h = int(rx), int(ry)
	}
	if w <= 0 || h <= 0 {
		return fallback, fallback
	}
	return w, h
}

func createResampledIcon(src image.Image, w, h int) uintptr {
	if src == nil || w <= 0 || h <= 0 || procCreateIconFromResourceEx.Find() != nil {
		return 0
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return 0
	}
	raw := buf.Bytes()
	if len(raw) == 0 {
		return 0
	}
	hIcon, _, _ := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&raw[0])),
		uintptr(len(raw)),
		1,
		0x00030000,
		uintptr(w),
		uintptr(h),
		0,
	)
	return hIcon
}

func setWindowsWindowIcon(hwnd uintptr, iconPNG []byte, dpi uintptr) {
	if hwnd == 0 || procCreateIconFromResourceEx.Find() != nil {
		return
	}

	winHookMu.Lock()
	img := winIconImg
	winHookMu.Unlock()
	if img == nil && len(iconPNG) > 0 {
		if decoded, err := png.Decode(bytes.NewReader(iconPNG)); err == nil {
			winHookMu.Lock()
			if winIconImg == nil {
				winIconImg = decoded
			}
			img = winIconImg
			winHookMu.Unlock()
		}
	}
	if img == nil {
		return
	}

	bigW, bigH := windowsIconSize(hwnd, dpi, smCxIcon, smCyIcon, 32)
	smallW, smallH := windowsIconSize(hwnd, dpi, smCxSmIcon, smCySmIcon, 16)
	hBig := createResampledIcon(img, bigW, bigH)
	hSmall := createResampledIcon(img, smallW, smallH)

	hasSetClassLong := procSetClassLongPtrW.Find() == nil
	if hBig != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, hBig)
		if hasSetClassLong {
			procSetClassLongPtrW.Call(hwnd, gclpHIcon, hBig)
		}
	}
	if hSmall != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, hSmall)
		if hasSetClassLong {
			procSetClassLongPtrW.Call(hwnd, gclpHIconSm, hSmall)
		}
	}

	var prevBig, prevSmall uintptr
	winHookMu.Lock()
	if hBig != 0 {
		prevBig = winIconBig
		winIconBig = hBig
	}
	if hSmall != 0 {
		prevSmall = winIconSmall
		winIconSmall = hSmall
	}
	winHookMu.Unlock()

	if procDestroyIcon.Find() == nil {
		if prevBig != 0 && prevBig != hBig {
			procDestroyIcon.Call(prevBig)
		}
		if prevSmall != 0 && prevSmall != hSmall {
			procDestroyIcon.Call(prevSmall)
		}
	}
}

func attachWindowOption(hwnd uintptr) (zenity.Option, bool) {
	if hwnd == 0 {
		return nil, false
	}
	return zenity.Attach(hwnd), true
}

func openSystemURL(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func querySpatialKeyLabels() map[string]string {
	labels := defaultSpatialKeyLabels()
	hkl, _, _ := procGetKeyboardLayout.Call(0)
	if hkl == 0 {
		return labels
	}
	scanCodes := []struct {
		code string
		scan uintptr
	}{
		{"KeyW", 0x11},
		{"KeyA", 0x1E},
		{"KeyS", 0x1F},
		{"KeyD", 0x20},
		{"KeyQ", 0x10},
		{"KeyE", 0x12},
	}
	var keyState [256]byte
	var out [8]uint16
	for _, item := range scanCodes {
		vk, _, _ := procMapVirtualKeyExW.Call(item.scan, mapVkVscToVk, hkl)
		if vk == 0 {
			continue
		}
		n, _, _ := procToUnicodeEx.Call(
			vk,
			item.scan,
			uintptr(unsafe.Pointer(&keyState[0])),
			uintptr(unsafe.Pointer(&out[0])),
			uintptr(len(out)),
			0,
			hkl,
		)
		if int32(n) > 0 {
			runes := utf16.Decode(out[:int(n)])
			if len(runes) > 0 && !unicode.IsControl(runes[0]) {
				lbl := strings.ToUpper(string(runes[0]))
				if lbl != "" {
					labels[item.code] = lbl
				}
			}
		}
	}
	return labels
}
