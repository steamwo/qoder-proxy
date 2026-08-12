//go:build desktop

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/desktop"
	"github.com/steamwo/qoder-proxy/internal/desktop/tray"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

const version = "0.3.3-desktop-ui"

type page int

const (
	pageDashboard page = iota
	pageAccounts
	pageModels
	pageLogs
	pageSettings
)

// appState is the single desktop product state shared by UI, proxy, tray, and storage actions.
// appState 是界面、代理、托盘与存储操作共享的单一桌面产品状态。
type appState struct {
	mu        sync.RWMutex
	quitting  bool
	win       *app.Window
	store     *credential.Store
	cred      credential.Credential
	loggedIn  bool
	quota     qoder.QuotaSnapshot
	quotaErr  string
	models    []qoder.Model
	modelsErr string
	busy      string
	notice    string
	current   page
	settings  desktop.Settings
	dataPaths desktop.DataPaths
	proxy     desktop.ProxyManager
	logbuf    *desktop.LogBuffer
	tray      tray.Tray

	dashBtn, accountsBtn, modelsBtn, logsBtn, settingsBtn             widget.Clickable
	loginBtn, logoutBtn, toggleBtn, refreshBtn, clearLogsBtn, saveBtn widget.Clickable
	openConfigBtn, openLogsBtn, openLogFromPageBtn                    widget.Clickable
	trayStatusBtn, trayToggleBtn, trayRefreshBtn                      widget.Clickable
	modelList, logList, pageList, settingsList                        widget.List
	modelRows, logRows                                                []widget.Clickable
	selectedModel, selectedLog                                        int
	trayPopover                                                       bool
	logEditor                                                         widget.Editor
	listenEditor, apiKeyEditor, retriesEditor, maxWaitEditor          widget.Editor
	modelSearchEditor, logSearchEditor                                widget.Editor
	autoStart, minimizeToTray, trayNotifications, logAutoScroll       widget.Bool
}

func main() {
	go func() {
		if err := runDesktop(); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(0)
	}()
	app.Main()
}

// runDesktop owns the UI lifecycle so closing to the tray does not stop the proxy.
// runDesktop 管理 UI 生命周期，确保关闭到托盘时不会停止代理。
func runDesktop() error {
	credentialOverride := os.Getenv("QODER_PROXY_CREDENTIALS")
	dataPaths, err := desktop.ResolveDataPaths(credentialOverride)
	if err != nil {
		return err
	}
	lb, err := desktop.NewPersistentLogBuffer(dataPaths.Log, 512<<10, 4<<20)
	if err != nil {
		// The UI must remain usable even when a locked-down environment denies cache writes.
		// 即使受限环境拒绝写入缓存，界面也必须保持可用。
		lb = desktop.NewLogBuffer(512 << 10)
		fmt.Fprintln(os.Stderr, "persistent logging unavailable:", err)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, lb), &slog.HandlerOptions{Level: slog.LevelDebug})))
	st, err := credential.New(dataPaths.Credentials)
	if err != nil {
		return err
	}
	s := &appState{store: st, settings: desktop.LoadSettings(), dataPaths: dataPaths, logbuf: lb}
	s.modelList.Axis = layout.Vertical
	s.logList.Axis = layout.Vertical
	s.pageList.Axis = layout.Vertical
	s.settingsList.Axis = layout.Vertical
	s.logEditor.ReadOnly = true
	s.logEditor.SingleLine = false
	s.listenEditor.SingleLine = true
	s.apiKeyEditor.SingleLine = true
	s.retriesEditor.SingleLine = true
	s.maxWaitEditor.SingleLine = true
	s.modelSearchEditor.SingleLine = true
	s.logSearchEditor.SingleLine = true
	s.listenEditor.SetText(s.settings.Listen)
	s.apiKeyEditor.SetText(s.settings.APIKey)
	s.retriesEditor.SetText(strconv.Itoa(s.settings.QueueRetries))
	s.maxWaitEditor.SetText(s.settings.QueueMaxWait)
	s.autoStart.Value = s.settings.AutoStart
	s.minimizeToTray.Value = s.settings.MinimizeToTray
	s.trayNotifications.Value = s.settings.TrayNotifications
	s.logAutoScroll.Value = true
	w := new(app.Window)
	w.Option(app.Title("Qoder Proxy"), app.Size(unit.Dp(1180), unit.Dp(780)))
	w.Perform(system.ActionCenter)
	s.win = w
	s.loadCredential()
	if s.settings.MinimizeToTray {
		tr, _ := tray.Start(tray.Callbacks{OpenWindow: s.showWindow, ToggleProxy: s.toggleProxy, RefreshQuota: s.refreshQuota, Quit: s.quit})
		s.tray = tr
	}
	s.updateTray()
	if s.settings.AutoStart && s.loggedIn {
		_ = s.startProxy()
	}
	if s.loggedIn {
		go s.refreshAll()
	}
	defer func() {
		if s.tray != nil {
			_ = s.tray.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.proxy.Stop(ctx)
		_ = s.logbuf.Close()
	}()
	th := material.NewTheme()
	var ops op.Ops
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			if s.win != nil {
				s.win.Invalidate()
			}
		}
	}()
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			// A destroy event now means an explicit exit or an unrecoverable window failure.
			// 现在销毁事件仅表示显式退出或不可恢复的窗口故障。
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			s.handleClicks(gtx)
			s.layout(gtx, th)
			e.Frame(gtx.Ops)
		default:
			s.handlePlatformWindowEvent(e)
		}
	}
}

func (s *appState) invalidate() {
	if s.win != nil {
		s.win.Invalidate()
	}
}
func (s *appState) loadCredential() {
	c, err := s.store.Load()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.loggedIn = false
		s.cred = credential.Credential{}
		return
	}
	s.cred = c
	s.loggedIn = c.Valid() && !c.Expired()
}
func (s *appState) refreshAll() { s.refreshQuota(); s.refreshModels() }
func (s *appState) refreshQuota() {
	s.mu.Lock()
	if !s.loggedIn {
		s.mu.Unlock()
		return
	}
	c := s.cred
	s.busy = "刷新额度…"
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	q, err := qoder.FetchQuota(ctx, http.DefaultClient, c)
	s.mu.Lock()
	if err != nil {
		s.quotaErr = err.Error()
		s.notice = "额度刷新失败"
	} else {
		s.quota = q
		s.quotaErr = ""
		s.notice = "额度已刷新"
	}
	s.busy = ""
	s.mu.Unlock()
	s.updateTray()
	s.invalidate()
}
func (s *appState) refreshModels() {
	s.mu.RLock()
	if !s.loggedIn {
		s.mu.RUnlock()
		return
	}
	c := s.cred
	s.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := qoder.NewRegistry(http.DefaultClient, c).List(ctx)
	s.mu.Lock()
	if err != nil {
		s.modelsErr = err.Error()
	} else {
		s.models = m
		s.modelsErr = ""
	}
	s.mu.Unlock()
	s.invalidate()
}
func (s *appState) login() {
	s.mu.Lock()
	if s.busy != "" {
		s.mu.Unlock()
		return
	}
	s.busy = "等待 Qoder 授权…"
	s.mu.Unlock()
	go func() {
		sess, err := qoder.StartLogin()
		if err == nil {
			err = desktop.OpenURL(sess.URL)
		}
		if err != nil {
			s.setNotice("登录启动失败: " + err.Error())
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		c, err := qoder.PollLogin(ctx, http.DefaultClient, sess)
		if err != nil {
			s.setNotice("登录失败: " + err.Error())
			return
		}
		if err = s.store.Save(c); err != nil {
			s.setNotice("保存登录失败: " + err.Error())
			return
		}
		s.mu.Lock()
		s.cred = c
		s.loggedIn = true
		s.busy = ""
		s.notice = "登录成功"
		s.mu.Unlock()
		if s.proxy.Running() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = s.proxy.Stop(ctx)
			cancel()
			_ = s.startProxy()
		}
		go s.refreshAll()
		s.updateTray()
		s.invalidate()
	}()
}
func (s *appState) logout() {
	if s.proxy.Running() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.proxy.Stop(ctx)
		cancel()
	}
	_ = s.store.Delete()
	s.mu.Lock()
	s.loggedIn = false
	s.cred = credential.Credential{}
	s.quota = qoder.QuotaSnapshot{}
	s.models = nil
	s.notice = "已退出账号"
	s.mu.Unlock()
	s.updateTray()
	s.invalidate()
}
func (s *appState) startProxy() error {
	s.mu.RLock()
	c := s.cred
	cfg := s.settings
	ok := s.loggedIn
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("请先登录 Qoder")
	}
	if err := s.proxy.Start(c, cfg); err != nil {
		s.setNotice("代理启动失败: " + err.Error())
		return err
	}
	s.setNotice("代理已启动")
	s.updateTray()
	return nil
}
func (s *appState) toggleProxy() {
	if s.proxy.Running() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.proxy.Stop(ctx)
		cancel()
		if err != nil {
			s.setNotice("停止代理失败: " + err.Error())
		} else {
			s.setNotice("代理已停止")
		}
	} else {
		_ = s.startProxy()
	}
	s.updateTray()
	s.invalidate()
}

// quit marks the close as intentional before asking the native window to close.
// quit 在请求关闭原生窗口前标记为主动退出，避免被“关闭到托盘”拦截。
func (s *appState) quit() {
	s.mu.Lock()
	s.quitting = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = s.proxy.Stop(ctx)
	cancel()
	if s.win != nil {
		s.win.Perform(system.ActionClose)
	}
}

// shouldHideOnClose keeps the process alive only when a usable tray is enabled.
// shouldHideOnClose 仅在托盘可用且已启用时保持进程驻留。
func (s *appState) shouldHideOnClose() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.quitting && s.settings.MinimizeToTray && s.tray != nil
}
func (s *appState) showWindow() {
	if s.win != nil {
		s.restorePlatformWindow()
		s.win.Perform(system.ActionRaise)
		s.invalidate()
	}
}
func (s *appState) setNotice(v string) {
	s.mu.Lock()
	s.busy = ""
	s.notice = v
	s.mu.Unlock()
	s.invalidate()
}
func (s *appState) updateTray() {
	if s.tray == nil {
		return
	}
	s.mu.RLock()
	name := first(s.cred.Email, s.cred.Name, "未登录")
	quotaSnapshot, settings := s.quota, s.settings
	s.mu.RUnlock()
	status := "Stopped"
	if s.proxy.Running() {
		status = "Running"
	}
	quota := ""
	if quotaSnapshot.User != nil {
		quota = fmt.Sprintf("%.0f%% remaining", quotaSnapshot.User.RemainingPercent)
	}
	tooltip := "Qoder Proxy · " + status
	if settings.TrayNotifications {
		tooltip += " · " + name
		if quota != "" {
			tooltip += " · " + quota
		}
	}
	s.tray.SetStatus(tray.Status{
		Running:  s.proxy.Running(),
		Tooltip:  tooltip,
		Endpoint: first(s.proxy.Addr(), settings.Listen),
		Account:  name,
		Quota:    quota,
	})
}
func first(v ...string) string {
	for _, x := range v {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

// handleClicks centralizes product actions so navigation and destructive operations stay predictable.
// handleClicks 集中处理产品操作，确保导航与清理等动作保持可预期。
func (s *appState) handleClicks(gtx layout.Context) {
	if s.dashBtn.Clicked(gtx) {
		s.current = pageDashboard
	}
	if s.accountsBtn.Clicked(gtx) {
		s.current = pageAccounts
	}
	if s.modelsBtn.Clicked(gtx) {
		s.current = pageModels
	}
	if s.logsBtn.Clicked(gtx) {
		s.current = pageLogs
	}
	if s.settingsBtn.Clicked(gtx) {
		s.current = pageSettings
	}
	if s.trayStatusBtn.Clicked(gtx) {
		s.trayPopover = !s.trayPopover
	}
	if s.trayToggleBtn.Clicked(gtx) {
		go s.toggleProxy()
		s.trayPopover = false
	}
	if s.trayRefreshBtn.Clicked(gtx) {
		go s.refreshAll()
		s.trayPopover = false
	}
	if s.loginBtn.Clicked(gtx) {
		s.login()
	}
	if s.logoutBtn.Clicked(gtx) {
		s.logout()
	}
	if s.toggleBtn.Clicked(gtx) {
		go s.toggleProxy()
	}
	if s.refreshBtn.Clicked(gtx) {
		go s.refreshAll()
	}
	if s.clearLogsBtn.Clicked(gtx) {
		s.logbuf.Clear()
		s.setNotice("运行日志已从内存和磁盘清除")
	}
	if s.openLogFromPageBtn.Clicked(gtx) || s.openLogsBtn.Clicked(gtx) {
		if err := desktop.OpenPath(filepath.Dir(s.dataPaths.Log)); err != nil {
			s.setNotice("无法打开日志目录: " + err.Error())
		}
	}
	if s.openConfigBtn.Clicked(gtx) {
		if err := desktop.OpenPath(filepath.Dir(s.dataPaths.Settings)); err != nil {
			s.setNotice("无法打开配置目录: " + err.Error())
		}
	}
	if s.saveBtn.Clicked(gtx) {
		s.saveSettings()
	}
}

// saveSettings validates operator input before replacing the durable configuration snapshot.
// saveSettings 在替换持久配置快照前验证用户输入。
func (s *appState) saveSettings() {
	retries, err := strconv.Atoi(strings.TrimSpace(s.retriesEditor.Text()))
	if err != nil || retries < 0 {
		s.setNotice("queue retries 必须是非负整数")
		return
	}
	maxWait := strings.TrimSpace(s.maxWaitEditor.Text())
	if _, err := time.ParseDuration(maxWait); err != nil {
		s.setNotice("queue max wait 格式无效，例如 10m")
		return
	}
	ns := desktop.Settings{
		Listen:            strings.TrimSpace(s.listenEditor.Text()),
		APIKey:            s.apiKeyEditor.Text(),
		QueueRetries:      retries,
		QueueMaxWait:      maxWait,
		AutoStart:         s.autoStart.Value,
		MinimizeToTray:    s.minimizeToTray.Value,
		TrayNotifications: s.trayNotifications.Value,
	}
	if err := desktop.SaveSettings(ns); err != nil {
		s.setNotice("保存设置失败: " + err.Error())
		return
	}
	s.mu.Lock()
	s.settings = ns
	s.mu.Unlock()
	s.setNotice("设置已保存；监听地址/API Key 的修改会在下次启动代理时生效")
}
