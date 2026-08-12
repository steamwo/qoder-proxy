//go:build windows

package tray

import (
	"testing"
	"time"
)

// TestWindowsTrayMessageLoopDispatchesCommands verifies callbacks survive beyond tray initialization.
// TestWindowsTrayMessageLoopDispatchesCommands 验证托盘初始化后消息循环仍能持续分发回调。
func TestWindowsTrayMessageLoopDispatchesCommands(t *testing.T) {
	opened := make(chan struct{}, 1)
	quit := make(chan struct{}, 1)
	trayInstance, err := start(Callbacks{
		OpenWindow: func() { opened <- struct{}{} },
		Quit:       func() { quit <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = trayInstance.Close() })

	windowsTray, ok := trayInstance.(*winTray)
	if !ok {
		t.Fatalf("tray type = %T, want *winTray", trayInstance)
	}
	postTrayGesture(t, windowsTray.hwnd, wmLButtonUp)
	waitForTrayCallback(t, opened, "open")
	postTrayCommand(t, windowsTray.hwnd, idQuit)
	waitForTrayCallback(t, quit, "quit")
}

// postTrayGesture exercises the Shell_NotifyIcon callback message used by real mouse input.
// postTrayGesture 覆盖 Shell_NotifyIcon 在真实鼠标输入时使用的回调消息。
func postTrayGesture(t *testing.T, hwnd uintptr, gesture uint32) {
	t.Helper()
	if r, _, err := pPostMessage.Call(hwnd, wmTray, 0, uintptr(gesture)); r == 0 {
		t.Fatalf("PostMessageW(tray gesture %d): %v", gesture, err)
	}
}

// postTrayCommand uses the native queue to exercise the same WM_COMMAND path as the popup menu.
// postTrayCommand 使用原生消息队列，覆盖弹出菜单实际经过的 WM_COMMAND 路径。
func postTrayCommand(t *testing.T, hwnd uintptr, command int) {
	t.Helper()
	if r, _, err := pPostMessage.Call(hwnd, wmCommand, uintptr(command), 0); r == 0 {
		t.Fatalf("PostMessageW(%d): %v", command, err)
	}
}

// waitForTrayCallback fails quickly when the Win32 message pump is no longer attached to its owner thread.
// waitForTrayCallback 在 Win32 消息泵脱离所属线程时快速失败。
func waitForTrayCallback(t *testing.T, callback <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-callback:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s tray callback", name)
	}
}
