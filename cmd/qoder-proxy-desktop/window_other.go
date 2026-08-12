//go:build desktop && !windows

package main

import "gioui.org/io/event"

// handlePlatformWindowEvent is a no-op where no native close interception is available.
// handlePlatformWindowEvent 在尚未提供原生关闭拦截的平台上不执行操作。
func (s *appState) handlePlatformWindowEvent(event.Event) {}

// restorePlatformWindow relies on Gio's portable ActionRaise outside Windows.
// restorePlatformWindow 在 Windows 之外依赖 Gio 的可移植 ActionRaise。
func (s *appState) restorePlatformWindow() {}
