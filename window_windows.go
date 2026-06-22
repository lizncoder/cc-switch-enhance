//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// Win32 plumbing to toggle the window's taskbar presence. Wails v2 has no
// built-in "hide from taskbar" runtime method, so we flip the extended window
// style directly: WS_EX_TOOLWINDOW hides it (still visible as an overlay, but
// gone from the taskbar and Alt-Tab), WS_EX_APPWINDOW forces it back.

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	procFindWindowW       = user32.NewProc("FindWindowW")
	procGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
)

const (
	gwlExStyle      = ^uintptr(19) // GWL_EXSTYLE == -20 (Go forbids negative uintptr constants)
	wsExToolWindow  = 0x00000080
	wsExAppWindow   = 0x00040000
	swpNoMove       = 0x0002
	swpNoSize       = 0x0001
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	windowTitle     = "cc-enhance"
	// GetSystemMetrics indices for the virtual screen (bounding box of all monitors).
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

// setTaskbarVisible shows or hides the overlay's taskbar entry.
func setTaskbarVisible(visible bool) {
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(windowTitle))))
	if hwnd == 0 {
		return // window not found yet (e.g. called before fully created)
	}
	ex, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(gwlExStyle))
	style := ex
	if visible {
		style &^= wsExToolWindow
		style |= wsExAppWindow
	} else {
		style |= wsExToolWindow
		style &^= wsExAppWindow
	}
	procSetWindowLongPtrW.Call(hwnd, uintptr(gwlExStyle), style)
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		uintptr(swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged))
}

// pointOnScreen reports whether (x, y) lies within the virtual screen — the
// bounding box of all monitors. Used to ignore saved window positions that are
// off-screen, e.g. after an external monitor is disconnected (the window would
// otherwise be restored to coordinates the user can't see).
func pointOnScreen(x, y int) bool {
	vx, _, _ := procGetSystemMetrics.Call(smXVirtualScreen)
	vy, _, _ := procGetSystemMetrics.Call(smYVirtualScreen)
	vw, _, _ := procGetSystemMetrics.Call(smCXVirtualScreen)
	vh, _, _ := procGetSystemMetrics.Call(smCYVirtualScreen)
	if vw == 0 || vh == 0 {
		return true // can't determine the virtual screen; assume visible
	}
	return x >= int(vx) && x < int(vx)+int(vw) && y >= int(vy) && y < int(vy)+int(vh)
}
