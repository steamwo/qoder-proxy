//go:build windows

package tray

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmCommand        = 0x0111
	wmLButtonUp      = 0x0202
	wmRButtonUp      = 0x0205
	wmLButtonDblClk  = 0x0203
	wmTray           = 0x0400 + 41
	nimAdd           = 0
	nimModify        = 1
	nimDelete        = 2
	nifMessage       = 1
	nifIcon          = 2
	nifTip           = 4
	lrDefaultSize    = 0x0040
	lrShared         = 0x8000
	imageIcon        = 1
	appIconResource  = 1
	mfString         = 0
	mfDisabled       = 0x0002
	mfSeparator      = 0x0800
	tpmRightButton   = 0x0002
	tpmBottomAlign   = 0x0020
	tpmReturnCmd     = 0x0100
	errorClassExists = 1410
	idToggle         = 1001
	idRefresh        = 1002
	idQuit           = 1003
	idOpen           = 1004
)

type wndClassEx struct {
	cbSize, style                      uint32
	wndProc                            uintptr
	clsExtra, wndExtra                 int32
	instance, icon, cursor, background uintptr
	menuName, className                *uint16
	iconSm                             uintptr
}
type notifyIconData struct {
	cbSize                        uint32
	hwnd                          uintptr
	uid, uFlags, uCallbackMessage uint32
	hIcon                         uintptr
	tip                           [128]uint16
	state, stateMask              uint32
	info                          [256]uint16
	version                       uint32
	infoTitle                     [64]uint16
	infoFlags                     uint32
	guid                          [16]byte
	balloonIcon                   uintptr
}
type point struct{ x, y int32 }
type message struct {
	hwnd           uintptr
	msg            uint32
	wParam, lParam uintptr
	time           uint32
	pt             point
	private        uint32
}

var (
	user32         = syscall.NewLazyDLL("user32.dll")
	shell32        = syscall.NewLazyDLL("shell32.dll")
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	pRegister      = user32.NewProc("RegisterClassExW")
	pCreate        = user32.NewProc("CreateWindowExW")
	pDef           = user32.NewProc("DefWindowProcW")
	pGetMsg        = user32.NewProc("GetMessageW")
	pTranslate     = user32.NewProc("TranslateMessage")
	pDispatch      = user32.NewProc("DispatchMessageW")
	pPostQuit      = user32.NewProc("PostQuitMessage")
	pPostMessage   = user32.NewProc("PostMessageW")
	pLoadIcon      = user32.NewProc("LoadIconW")
	pShellNotify   = shell32.NewProc("Shell_NotifyIconW")
	pCreateMenu    = user32.NewProc("CreatePopupMenu")
	pAppendMenu    = user32.NewProc("AppendMenuW")
	pTrackMenu     = user32.NewProc("TrackPopupMenu")
	pDestroyMenu   = user32.NewProc("DestroyMenu")
	pGetCursor     = user32.NewProc("GetCursorPos")
	pSetForeground = user32.NewProc("SetForegroundWindow")
	wndProc        = syscall.NewCallback(windowProc)
	trayMu         sync.Mutex
	trays          = map[uintptr]*winTray{}
)

type winTray struct {
	mu      sync.RWMutex
	hwnd    uintptr
	nid     notifyIconData
	cb      Callbacks
	running bool
	status  Status
	done    chan struct{}
	close   sync.Once
}

// start waits until the native icon is registered so callers never receive a half-ready tray.
// start 等待原生图标注册完成，避免调用方拿到尚未就绪的托盘实例。
func start(cb Callbacks) (Tray, error) {
	t := &winTray{cb: cb, done: make(chan struct{})}
	ready := make(chan error, 1)
	go t.loop(ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

// loop stays on one OS thread because Win32 delivers window messages to the creating thread's queue.
// loop 固定在同一 OS 线程，因为 Win32 会把窗口消息投递到创建线程的消息队列。
func (t *winTray) loop(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.done)
	inst, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	cls, _ := syscall.UTF16PtrFromString("QoderProxyTrayWindow")
	wc := wndClassEx{cbSize: uint32(unsafe.Sizeof(wndClassEx{})), wndProc: wndProc, instance: inst, className: cls}
	if r, _, e := pRegister.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && e != syscall.Errno(errorClassExists) {
		ready <- fmt.Errorf("RegisterClassExW: %v", e)
		return
	}
	hwnd, _, e := pCreate.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(cls)), 0, 0, 0, 0, 0, 0, 0, inst, 0)
	if hwnd == 0 {
		ready <- fmt.Errorf("CreateWindowExW: %v", e)
		return
	}
	t.mu.Lock()
	t.hwnd = hwnd
	t.mu.Unlock()
	icon := loadAppIcon(inst)
	t.nid = notifyIconData{cbSize: uint32(unsafe.Sizeof(notifyIconData{})), hwnd: hwnd, uid: 1, uFlags: nifMessage | nifIcon | nifTip, uCallbackMessage: wmTray, hIcon: icon}
	setTip(&t.nid.tip, "Qoder Proxy")
	trayMu.Lock()
	trays[hwnd] = t
	trayMu.Unlock()
	if r, _, e := pShellNotify.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid))); r == 0 {
		ready <- fmt.Errorf("Shell_NotifyIconW: %v", e)
		return
	}
	ready <- nil
	var m message
	for {
		r, _, _ := pGetMsg.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslate.Call(uintptr(unsafe.Pointer(&m)))
		pDispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
	pShellNotify.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
	trayMu.Lock()
	delete(trays, hwnd)
	trayMu.Unlock()
}

// loadAppIcon uses the executable's resource ID 1 so tray and taskbar share one brand asset.
// loadAppIcon 使用可执行文件的资源 ID 1，使托盘与任务栏共享同一品牌图标。
func loadAppIcon(instance uintptr) uintptr {
	loadImage := user32.NewProc("LoadImageW")
	icon, _, _ := loadImage.Call(instance, appIconResource, imageIcon, 0, 0, lrDefaultSize|lrShared)
	if icon != 0 {
		return icon
	}
	// Falling back prevents a missing resource from making the entire tray unavailable.
	// 回退逻辑避免资源缺失时导致整个托盘不可用。
	icon, _, _ = pLoadIcon.Call(0, 32512)
	return icon
}
func (t *winTray) SetStatus(s Status) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running = s.Running
	t.status = s
	tip := s.Tooltip
	if tip == "" {
		tip = "Qoder Proxy"
	}
	setTip(&t.nid.tip, tip)
	pShellNotify.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
}

// Close posts WM_CLOSE because DestroyWindow cannot safely destroy a window owned by another thread.
// Close 投递 WM_CLOSE，因为 DestroyWindow 无法安全销毁其他线程拥有的窗口。
func (t *winTray) Close() error {
	var closeErr error
	t.close.Do(func() {
		t.mu.RLock()
		hwnd := t.hwnd
		t.mu.RUnlock()
		if hwnd != 0 {
			if r, _, e := pPostMessage.Call(hwnd, wmClose, 0, 0); r == 0 {
				closeErr = fmt.Errorf("PostMessageW(WM_CLOSE): %v", e)
				return
			}
		}
		select {
		case <-t.done:
		case <-time.After(2 * time.Second):
			closeErr = fmt.Errorf("timed out waiting for tray message loop to close")
		}
	})
	return closeErr
}
func setTip(dst *[128]uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	for i := range dst {
		dst[i] = 0
	}
	if len(u) > len(dst) {
		u = u[:len(dst)]
	}
	copy(dst[:], u)
}

// windowProc dispatches tray gestures and commands without blocking the Win32 message pump.
// windowProc 分发托盘手势与命令，同时避免阻塞 Win32 消息泵。
func windowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	trayMu.Lock()
	t := trays[hwnd]
	trayMu.Unlock()
	if t != nil {
		switch msg {
		case wmTray:
			if lParam == wmRButtonUp {
				showMenu(t)
			} else if (lParam == wmLButtonUp || lParam == wmLButtonDblClk) && t.cb.OpenWindow != nil {
				go t.cb.OpenWindow()
			}
			return 0
		case wmCommand:
			dispatchCommand(t, int(wParam&0xffff))
			return 0
		case wmClose:
			user32.NewProc("DestroyWindow").Call(hwnd)
			return 0
		case wmDestroy:
			pPostQuit.Call(0)
			return 0
		}
	}
	r, _, _ := pDef.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// showMenu runs on the tray window thread so Windows can route the selected command back reliably.
// showMenu 在托盘窗口线程运行，确保 Windows 能可靠地回传所选菜单命令。
func showMenu(t *winTray) {
	menu, _, _ := pCreateMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	t.mu.RLock()
	running, status := t.running, t.status
	t.mu.RUnlock()
	label := "启动代理"
	if running {
		label = "停止代理"
	}
	state := "代理已停止"
	if running {
		state = "代理运行中"
	}
	if status.Endpoint != "" {
		state += " · " + status.Endpoint
	}
	statusText, _ := syscall.UTF16PtrFromString(state)
	open, _ := syscall.UTF16PtrFromString("打开主窗口")
	a, _ := syscall.UTF16PtrFromString(label)
	b, _ := syscall.UTF16PtrFromString("刷新额度")
	c, _ := syscall.UTF16PtrFromString("退出")
	pAppendMenu.Call(menu, mfString|mfDisabled, 0, uintptr(unsafe.Pointer(statusText)))
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	pAppendMenu.Call(menu, mfString, idOpen, uintptr(unsafe.Pointer(open)))
	pAppendMenu.Call(menu, mfString, idToggle, uintptr(unsafe.Pointer(a)))
	pAppendMenu.Call(menu, mfString, idRefresh, uintptr(unsafe.Pointer(b)))
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	pAppendMenu.Call(menu, mfString, idQuit, uintptr(unsafe.Pointer(c)))
	var pt point
	pGetCursor.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForeground.Call(t.hwnd)
	command, _, _ := pTrackMenu.Call(menu, tpmRightButton|tpmBottomAlign|tpmReturnCmd, uintptr(pt.x), uintptr(pt.y), 0, t.hwnd, 0)
	if command != 0 {
		dispatchCommand(t, int(command))
	}
}

// dispatchCommand keeps native menu and synthetic WM_COMMAND handling behavior identical.
// dispatchCommand 统一原生菜单与测试 WM_COMMAND 的行为。
func dispatchCommand(t *winTray, command int) {
	switch command {
	case idOpen:
		if t.cb.OpenWindow != nil {
			go t.cb.OpenWindow()
		}
	case idToggle:
		if t.cb.ToggleProxy != nil {
			go t.cb.ToggleProxy()
		}
	case idRefresh:
		if t.cb.RefreshQuota != nil {
			go t.cb.RefreshQuota()
		}
	case idQuit:
		if t.cb.Quit != nil {
			go t.cb.Quit()
		}
	}
}
