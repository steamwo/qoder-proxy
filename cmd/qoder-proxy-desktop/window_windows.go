//go:build desktop && windows

package main

import (
	"log/slog"
	"sync"
	"syscall"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

const (
	wmClose   = 0x0010
	swHide    = 0
	swRestore = 9
)

var (
	closeHookOnce       sync.Once
	closeHookErr        error
	closeHookHWND       uintptr
	closeHookOriginal   uintptr
	closeHookShouldHide func() bool
	closeHookCallback   = syscall.NewCallback(closeHookWindowProc)
	user32              = syscall.NewLazyDLL("user32.dll")
	callWindowProc      = user32.NewProc("CallWindowProcW")
	showWindow          = user32.NewProc("ShowWindow")
	setForegroundWindow = user32.NewProc("SetForegroundWindow")
)

// handlePlatformWindowEvent installs the Win32 close hook after Gio exposes its HWND.
// handlePlatformWindowEvent 在 Gio 提供 HWND 后安装 Win32 关闭钩子。
func (s *appState) handlePlatformWindowEvent(e event.Event) {
	view, ok := e.(app.Win32ViewEvent)
	if !ok || !view.Valid() {
		return
	}
	closeHookOnce.Do(func() {
		closeHookErr = installCloseHook(view.HWND, s.shouldHideOnClose)
	})
	if closeHookErr != nil {
		slog.Warn("failed to install close-to-tray hook", "error", closeHookErr)
	}
}

// installCloseHook subclasses the Gio window because DestroyEvent arrives after native destruction.
// installCloseHook 对 Gio 窗口进行子类化，因为 DestroyEvent 到达时原生窗口已被销毁。
func installCloseHook(hwnd uintptr, shouldHide func() bool) error {
	procSuffix := "PtrW"
	if unsafe.Sizeof(uintptr(0)) == 4 {
		procSuffix = "W"
	}
	getWindowLong := user32.NewProc("GetWindowLong" + procSuffix)
	setWindowLong := user32.NewProc("SetWindowLong" + procSuffix)
	wndProcIndex := ^uintptr(3)
	original, _, getErr := getWindowLong.Call(hwnd, wndProcIndex)
	if original == 0 {
		return getErr
	}
	closeHookHWND = hwnd
	closeHookOriginal = original
	closeHookShouldHide = shouldHide
	previous, _, setErr := setWindowLong.Call(hwnd, wndProcIndex, closeHookCallback)
	if previous == 0 {
		return setErr
	}
	return nil
}

// closeHookWindowProc consumes WM_CLOSE only when the app should remain available in the tray.
// closeHookWindowProc 仅在应用需要驻留托盘时拦截 WM_CLOSE。
func closeHookWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	if hwnd == closeHookHWND && msg == wmClose && closeHookShouldHide != nil && closeHookShouldHide() {
		// Hiding preserves the Gio window and its event loop so the tray can raise it later.
		// 隐藏会保留 Gio 窗口及事件循环，之后可从托盘重新唤起。
		showWindow.Call(hwnd, swHide)
		return 0
	}
	result, _, _ := callWindowProc.Call(closeHookOriginal, hwnd, uintptr(msg), wParam, lParam)
	return result
}

// restorePlatformWindow reverses the native hide operation before Gio raises the window.
// restorePlatformWindow 在 Gio 置顶窗口前撤销原生隐藏状态。
func (s *appState) restorePlatformWindow() {
	if closeHookHWND == 0 {
		return
	}
	showWindow.Call(closeHookHWND, swRestore)
	setForegroundWindow.Call(closeHookHWND)
}
