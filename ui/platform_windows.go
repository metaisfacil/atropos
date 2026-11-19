//go:build windows

package ui

import (
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf16"
	"unsafe"

	"github.com/ncruces/zenity"
	"golang.org/x/sys/windows"
)

const (
	wmDropFiles   = 0x0233
	wmSetIcon     = 0x0080
	iconSmall     = 0
	iconBig       = 1
	lrDefaultSize = 0x00000040
	mapVkVscToVk  = 3
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
	procSendMessageW                  = modUser32.NewProc("SendMessageW")
	procGetKeyboardLayout             = modUser32.NewProc("GetKeyboardLayout")
	procMapVirtualKeyExW              = modUser32.NewProc("MapVirtualKeyExW")
	procToUnicodeEx                   = modUser32.NewProc("ToUnicodeEx")

	winHookMu      sync.Mutex
	winHookedHWND  uintptr
	winDropHandler func([]string)
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
	ret, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
	return ret
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
	setWindowsWindowIcon(hwnd, iconPNG)
}

func setWindowsWindowIcon(hwnd uintptr, iconPNG []byte) {
	if hwnd == 0 || len(iconPNG) == 0 || procCreateIconFromResourceEx.Find() != nil {
		return
	}
	for _, spec := range []struct {
		wParam uintptr
		size   uintptr
	}{
		{iconBig, 48},
		{iconSmall, 16},
	} {
		hIcon, _, _ := procCreateIconFromResourceEx.Call(
			uintptr(unsafe.Pointer(&iconPNG[0])),
			uintptr(len(iconPNG)),
			1,
			0x00030000,
			spec.size,
			spec.size,
			lrDefaultSize,
		)
		if hIcon != 0 {
			procSendMessageW.Call(hwnd, wmSetIcon, spec.wParam, hIcon)
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
