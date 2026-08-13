package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/qoder"
	"github.com/steamwo/qoder-proxy/internal/server"
)

// UnifiedServiceOptions configures the lightweight single-port service.
type UnifiedServiceOptions struct {
	OpenBrowser bool
}

type unifiedService struct {
	mu sync.RWMutex

	proxyHandler http.Handler
	backend      *server.Backend
	enabled      bool
	started      time.Time
	listen       string

	store     *credential.Store
	dataPaths DataPaths
	logbuf    *LogBuffer
	shutdown  context.CancelFunc

	loginMu      sync.RWMutex
	loginPending bool
	loginURL     string
	loginError   string
	loginEmail   string
	loginName    string
}

type unifiedStatus struct {
	Running          bool   `json:"running"`
	ProxyAddress     string `json:"proxy_address"`
	ConfiguredListen string `json:"configured_listen"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	CredentialReady  bool   `json:"credential_ready"`
	CredentialEmail  string `json:"credential_email,omitempty"`
	CredentialName   string `json:"credential_name,omitempty"`
	SettingsPath     string `json:"settings_path"`
	LogPath          string `json:"log_path"`
	HeapAllocMB      uint64 `json:"heap_alloc_mb"`
	RuntimeSysMB     uint64 `json:"runtime_sys_mb"`
	Goroutines       int    `json:"goroutines"`
	RestartRequired  bool   `json:"restart_required"`
}

type unifiedLoginStatus struct {
	Pending bool   `json:"pending"`
	URL     string `json:"url,omitempty"`
	Error   string `json:"error,omitempty"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
}

type unifiedSettingsRequest struct {
	Listen          string `json:"listen"`
	APIKey          string `json:"api_key"`
	QueueRetries    int    `json:"queue_retries"`
	QueueMaxWait    string `json:"queue_max_wait"`
	AutoStart       bool   `json:"auto_start"`
}

type unifiedModelView struct {
	UpstreamID      string   `json:"upstream_id"`
	DisplayName     string   `json:"display_name"`
	MaxInputTokens  int      `json:"max_input_tokens"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	Options         []string `json:"options"`
	Default         string   `json:"default"`
}

type unifiedModelDefaultRequest struct {
	ModelID string `json:"model_id"`
	Effort  string `json:"effort"`
}

// RunUnifiedService serves both /v1/* and /admin/* on the configured proxy port.
// The admin surface is still restricted to loopback clients even when the proxy
// listener itself is intentionally exposed to the LAN.
func RunUnifiedService(parent context.Context, opts UnifiedServiceOptions) error {
	dataPaths, err := ResolveDataPaths(os.Getenv("QODER_PROXY_CREDENTIALS"))
	if err != nil {
		return err
	}
	logbuf, err := NewPersistentLogBuffer(dataPaths.Log, 512<<10, 4<<20)
	if err != nil {
		logbuf = NewLogBuffer(512 << 10)
		fmt.Fprintln(os.Stderr, "persistent logging unavailable:", err)
	}
	defer logbuf.Close()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, logbuf), &slog.HandlerOptions{Level: slog.LevelInfo})))

	store, err := credential.New(dataPaths.Credentials)
	if err != nil {
		return err
	}
	settings := LoadSettings()
	if strings.TrimSpace(settings.Listen) == "" {
		return errors.New("listen address is empty")
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	svc := &unifiedService{
		store:     store,
		dataPaths: dataPaths,
		logbuf:    logbuf,
		shutdown:  cancel,
		listen:    settings.Listen,
	}
	if settings.AutoStart {
		if err := svc.enableProxy(); err != nil {
			slog.Warn("unified proxy auto-start skipped", "error", err)
		}
	}

	ln, err := net.Listen("tcp", settings.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", settings.Listen, err)
	}
	defer ln.Close()
	svc.mu.Lock()
	svc.listen = ln.Addr().String()
	svc.mu.Unlock()

	httpServer := &http.Server{Handler: svc, ReadHeaderTimeout: 10 * time.Second}
	adminURL := "http://" + localAdminAddress(ln.Addr().String()) + "/admin/"
	slog.Info("unified background service started", "listen", ln.Addr().String(), "admin", adminURL, "proxy_running", svc.isEnabled())
	if opts.OpenBrowser {
		go func() {
			timer := time.NewTimer(250 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				if err := OpenURL(adminURL); err != nil {
					slog.Warn("open unified service dashboard", "error", err)
				}
			case <-ctx.Done():
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		err := httpServer.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	slog.Info("unified background service stopped")
	return nil
}

// ShutdownUnifiedService explicitly stops a running single-port service using
// the same loopback-only admin endpoint used by the dashboard.
func ShutdownUnifiedService(ctx context.Context) error {
	settings := LoadSettings()
	addr := localAdminAddress(settings.Listen)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/admin/api/shutdown", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("service shutdown returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func localAdminAddress(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	ip := net.ParseIP(host)
	if host == "" || host == "0.0.0.0" || (ip != nil && ip.IsUnspecified()) {
		if strings.Contains(host, ":") {
			return net.JoinHostPort("::1", port)
		}
		return net.JoinHostPort("127.0.0.1", port)
	}
	return net.JoinHostPort(host, port)
}

func (s *unifiedService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" {
		if isLoopbackRequest(r) {
			http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
			return
		}
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/admin") {
		if !isLoopbackRequest(r) {
			http.Error(w, "admin interface is local-only", http.StatusForbidden)
			return
		}
		s.handleAdmin(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/") {
		s.mu.RLock()
		h := s.proxyHandler
		enabled := s.enabled
		s.mu.RUnlock()
		if !enabled || h == nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "proxy is stopped or Qoder is not authorized", "type": "service_unavailable", "code": "proxy_stopped"}})
			return
		}
		h.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *unifiedService) isEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

func (s *unifiedService) enableProxy() error {
	cred, err := s.store.Load()
	if err != nil {
		return err
	}
	if cred.Expired() {
		return errors.New("stored Qoder credential is expired; authorize again from /admin/")
	}
	settings := LoadSettings()
	app := server.New(http.DefaultClient, cred, settings.APIKey)
	app.Backend.SetModelReasoningDefaults(settings.ModelReasoningDefaults)
	maxWait, _ := time.ParseDuration(settings.QueueMaxWait)
	if maxWait <= 0 {
		maxWait = 10 * time.Minute
	}
	app.Backend.Qoder.QueueRetry = qoder.QueueRetryPolicy{MaxRetries: settings.QueueRetries, MaxWait: maxWait}

	s.mu.Lock()
	s.proxyHandler = app.Handler()
	s.backend = app.Backend
	s.enabled = true
	s.started = time.Now()
	s.mu.Unlock()
	slog.Info("unified proxy enabled", "listen", settings.Listen, "queue_retries", settings.QueueRetries, "queue_max_wait", maxWait)
	return nil
}

func (s *unifiedService) disableProxy() {
	s.mu.Lock()
	s.enabled = false
	s.proxyHandler = nil
	s.backend = nil
	s.started = time.Time{}
	s.mu.Unlock()
	slog.Info("unified proxy disabled")
}

func (s *unifiedService) reloadProxy() error {
	wasEnabled := s.isEnabled()
	if !wasEnabled {
		return nil
	}
	return s.enableProxy()
}

func (s *unifiedService) status() unifiedStatus {
	settings := LoadSettings()
	cred, credentialErr := s.store.Load()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	const mib = 1024 * 1024

	s.mu.RLock()
	running := s.enabled
	started := s.started
	actualListen := s.listen
	s.mu.RUnlock()
	uptime := int64(0)
	if running && !started.IsZero() {
		uptime = int64(time.Since(started).Seconds())
	}
	return unifiedStatus{
		Running:          running,
		ProxyAddress:     actualListen,
		ConfiguredListen: settings.Listen,
		UptimeSeconds:    uptime,
		CredentialReady:  credentialErr == nil && !cred.Expired(),
		CredentialEmail:  cred.Email,
		CredentialName:   cred.Name,
		SettingsPath:     s.dataPaths.Settings,
		LogPath:          s.dataPaths.Log,
		HeapAllocMB:      mem.HeapAlloc / mib,
		RuntimeSysMB:     mem.Sys / mib,
		Goroutines:       runtime.NumGoroutine(),
		RestartRequired:  strings.TrimSpace(actualListen) != strings.TrimSpace(settings.Listen),
	}
}

func (s *unifiedService) handleAdmin(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/admin" && r.Method == http.MethodGet:
		http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
	case r.URL.Path == "/admin/" && r.Method == http.MethodGet:
		s.handleUnifiedDashboard(w)
	case r.URL.Path == "/admin/api/status" && r.Method == http.MethodGet:
		unifiedWriteJSON(w, http.StatusOK, s.status())
	case r.URL.Path == "/admin/api/logs" && r.Method == http.MethodGet:
		s.handleUnifiedLogs(w)
	case r.URL.Path == "/admin/api/start" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		if err := s.enableProxy(); err != nil { unifiedWriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()}); return }
		unifiedWriteJSON(w, http.StatusOK, s.status())
	case r.URL.Path == "/admin/api/stop" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		s.disableProxy()
		unifiedWriteJSON(w, http.StatusOK, s.status())
	case r.URL.Path == "/admin/api/reload" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		if err := s.reloadProxy(); err != nil { unifiedWriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()}); return }
		unifiedWriteJSON(w, http.StatusOK, s.status())
	case r.URL.Path == "/admin/api/shutdown" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		unifiedWriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
		go s.shutdown()
	case r.URL.Path == "/admin/api/settings" && r.Method == http.MethodGet:
		s.handleUnifiedSettingsGet(w)
	case r.URL.Path == "/admin/api/settings" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		s.handleUnifiedSettingsSave(w, r)
	case r.URL.Path == "/admin/api/login/start" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		s.handleUnifiedLoginStart(w, r)
	case r.URL.Path == "/admin/api/login/status" && r.Method == http.MethodGet:
		unifiedWriteJSON(w, http.StatusOK, s.loginStatus())
	case r.URL.Path == "/admin/api/logout" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		s.disableProxy()
		if err := s.store.Delete(); err != nil { unifiedWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()}); return }
		s.loginMu.Lock(); s.loginEmail, s.loginName, s.loginURL, s.loginError, s.loginPending = "", "", "", "", false; s.loginMu.Unlock()
		unifiedWriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.URL.Path == "/admin/api/models" && r.Method == http.MethodGet:
		s.handleUnifiedModels(w, r)
	case r.URL.Path == "/admin/api/models/default" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		s.handleUnifiedModelDefault(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *unifiedService) handleUnifiedDashboard(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	_, _ = io.WriteString(w, unifiedDashboardHTML)
}

func (s *unifiedService) handleUnifiedLogs(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	text := s.logbuf.String()
	if len(text) > 64<<10 {
		text = text[len(text)-(64<<10):]
	}
	_, _ = io.WriteString(w, text)
}

func (s *unifiedService) handleUnifiedSettingsGet(w http.ResponseWriter) {
	settings := LoadSettings()
	unifiedWriteJSON(w, http.StatusOK, unifiedSettingsRequest{
		Listen: settings.Listen, APIKey: settings.APIKey, QueueRetries: settings.QueueRetries,
		QueueMaxWait: settings.QueueMaxWait, AutoStart: settings.AutoStart,
	})
}

func (s *unifiedService) handleUnifiedSettingsSave(w http.ResponseWriter, r *http.Request) {
	var req unifiedSettingsRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid settings payload"})
		return
	}
	if strings.TrimSpace(req.Listen) == "" {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "listen address is required"})
		return
	}
	if _, _, err := net.SplitHostPort(req.Listen); err != nil {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid listen address: " + err.Error()})
		return
	}
	if req.QueueRetries < 0 {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "queue retries cannot be negative"})
		return
	}
	if _, err := time.ParseDuration(req.QueueMaxWait); err != nil {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid queue max wait: " + err.Error()})
		return
	}
	settings := LoadSettings()
	settings.Listen = strings.TrimSpace(req.Listen)
	settings.APIKey = req.APIKey
	settings.QueueRetries = req.QueueRetries
	settings.QueueMaxWait = req.QueueMaxWait
	settings.AutoStart = req.AutoStart
	if err := SaveSettings(settings); err != nil {
		unifiedWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.reloadProxy(); err != nil {
		unifiedWriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	unifiedWriteJSON(w, http.StatusOK, s.status())
}

func (s *unifiedService) handleUnifiedLoginStart(w http.ResponseWriter, r *http.Request) {
	s.loginMu.Lock()
	if s.loginPending {
		status := unifiedLoginStatus{Pending: true, URL: s.loginURL, Error: s.loginError, Email: s.loginEmail, Name: s.loginName}
		s.loginMu.Unlock()
		unifiedWriteJSON(w, http.StatusOK, status)
		return
	}
	session, err := qoder.StartLogin()
	if err != nil {
		s.loginMu.Unlock()
		unifiedWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.loginPending = true
	s.loginURL = session.URL
	s.loginError = ""
	s.loginEmail = ""
	s.loginName = ""
	s.loginMu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		cred, err := qoder.PollLogin(ctx, http.DefaultClient, session)
		if err == nil {
			err = s.store.Save(cred)
		}
		if err == nil {
			settings := LoadSettings()
			if settings.AutoStart || s.isEnabled() {
				err = s.enableProxy()
			}
		}
		s.loginMu.Lock()
		s.loginPending = false
		if err != nil {
			s.loginError = err.Error()
		} else {
			s.loginError = ""
			s.loginEmail = cred.Email
			s.loginName = cred.Name
		}
		s.loginMu.Unlock()
	}()
	unifiedWriteJSON(w, http.StatusOK, unifiedLoginStatus{Pending: true, URL: session.URL})
}

func (s *unifiedService) loginStatus() unifiedLoginStatus {
	s.loginMu.RLock()
	defer s.loginMu.RUnlock()
	return unifiedLoginStatus{Pending: s.loginPending, URL: s.loginURL, Error: s.loginError, Email: s.loginEmail, Name: s.loginName}
}

func (s *unifiedService) handleUnifiedModels(w http.ResponseWriter, r *http.Request) {
	cred, err := s.store.Load()
	if err != nil || cred.Expired() {
		unifiedWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "authorize Qoder first"})
		return
	}
	settings := LoadSettings()
	s.mu.RLock()
	backend := s.backend
	s.mu.RUnlock()
	registry := qoder.NewRegistry(http.DefaultClient, cred)
	if backend != nil {
		registry = backend.Registry
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	models, err := registry.List(ctx)
	if err != nil {
		unifiedWriteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	out := make([]unifiedModelView, 0, len(models))
	for _, model := range models {
		options := []string{""}
		if model.SupportsReasoningDisabled() {
			options = append(options, "none")
		}
		options = append(options, model.SupportedReasoningEfforts()...)
		out = append(out, unifiedModelView{
			UpstreamID: model.UpstreamID, DisplayName: model.DisplayName,
			MaxInputTokens: model.MaxInputTokens, MaxOutputTokens: model.MaxOutputTokens,
			Options: options, Default: settings.ModelReasoningDefaults[model.UpstreamID],
		})
	}
	unifiedWriteJSON(w, http.StatusOK, out)
}

func (s *unifiedService) handleUnifiedModelDefault(w http.ResponseWriter, r *http.Request) {
	var req unifiedModelDefaultRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid model default payload"})
		return
	}
	cred, err := s.store.Load()
	if err != nil || cred.Expired() {
		unifiedWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "authorize Qoder first"})
		return
	}
	registry := qoder.NewRegistry(http.DefaultClient, cred)
	s.mu.RLock()
	if s.backend != nil { registry = s.backend.Registry }
	s.mu.RUnlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	model, err := registry.Resolve(ctx, req.ModelID)
	if err != nil {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if _, err := model.NormalizeReasoningEffort(req.Effort); err != nil {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	settings := LoadSettings()
	if settings.ModelReasoningDefaults == nil { settings.ModelReasoningDefaults = map[string]string{} }
	if strings.TrimSpace(req.Effort) == "" { delete(settings.ModelReasoningDefaults, model.UpstreamID) } else { settings.ModelReasoningDefaults[model.UpstreamID] = strings.ToLower(strings.TrimSpace(req.Effort)) }
	if err := SaveSettings(settings); err != nil {
		unifiedWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.RLock(); backend := s.backend; s.mu.RUnlock()
	if backend != nil { backend.SetModelReasoningDefaults(settings.ModelReasoningDefaults) }
	unifiedWriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func unifiedAllowMutation(w http.ResponseWriter, r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		expected := "http://" + r.Host
		if origin != expected {
			http.Error(w, "cross-origin admin request rejected", http.StatusForbidden)
			return false
		}
	}
	return true
}

func unifiedWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

const unifiedDashboardHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Qoder Proxy</title>
<style>
:root{font-family:Inter,"Segoe UI",system-ui,sans-serif;color:#151922;background:#f4f6f9;color-scheme:light}*{box-sizing:border-box}body{margin:0}.shell{max-width:1120px;margin:0 auto;padding:30px 22px 56px}.top{display:flex;justify-content:space-between;gap:18px;align-items:flex-start;margin-bottom:18px}.brand{font-size:30px;font-weight:750}.sub{font-size:13px;color:#667085;margin-top:5px}.pill{display:inline-flex;padding:8px 12px;border-radius:999px;background:#eef2f6;font-size:13px;font-weight:650}.pill.on{background:#e5f7f1;color:#087e63}.tabs{display:flex;gap:8px;margin:18px 0}.tab{background:#e9edf3}.tab.active{background:#1769e8;color:#fff}.panel{display:none}.panel.active{display:block}.grid{display:grid;grid-template-columns:1.35fr .65fr;gap:16px}.card{background:#fff;border:1px solid #e0e5ec;border-radius:17px;padding:20px;box-shadow:0 7px 26px rgba(31,41,55,.045);margin-bottom:16px}.card h2{font-size:15px;margin:0 0 16px}.metrics{display:grid;grid-template-columns:repeat(3,1fr);gap:10px}.metric{background:#f6f8fb;border-radius:12px;padding:13px}.k{font-size:11px;color:#8c96a8;margin-bottom:6px}.v{font-size:16px;font-weight:700;overflow-wrap:anywhere}.actions{display:flex;gap:9px;flex-wrap:wrap;margin-top:16px}button{border:0;border-radius:10px;padding:9px 14px;font:inherit;font-size:13px;font-weight:650;cursor:pointer;background:#edf1f6;color:#344054}button.primary{background:#1769e8;color:#fff}button.danger{background:#fff0ef;color:#c33e38}button:disabled{opacity:.45;cursor:wait}.form{display:grid;grid-template-columns:180px 1fr;gap:12px 14px;align-items:center}.form label{font-size:13px;color:#667085}.form input{width:100%;padding:10px 11px;border:1px solid #d9e0e8;border-radius:10px;font:inherit}.check{display:flex;align-items:center;gap:8px}.check input{width:auto}.notice{font-size:12px;color:#8c96a8;margin-top:10px}.error{display:none;color:#c33e38;font-size:13px;margin-top:10px}.logbox{height:260px;overflow:auto;background:#10151d;color:#cbd5e1;border-radius:13px;padding:13px;font:12px/1.55 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap}.models{width:100%;border-collapse:collapse;font-size:13px}.models th,.models td{text-align:left;padding:11px 9px;border-bottom:1px solid #edf0f4}.models th{color:#8c96a8;font-size:11px}.models select{padding:7px;border:1px solid #d9e0e8;border-radius:9px;background:#fff}.authrow{display:flex;align-items:center;justify-content:space-between;gap:12px}.muted{color:#667085;font-size:13px}@media(max-width:780px){.grid{grid-template-columns:1fr}.metrics{grid-template-columns:1fr}.form{grid-template-columns:1fr}.top{flex-direction:column}}
</style>
</head>
<body>
<div class="shell">
 <div class="top"><div><div class="brand">Qoder Proxy</div><div class="sub">同一端口提供 /v1/* 代理接口与 /admin 管理界面。关闭网页不会停止后台服务。</div></div><div id="state" class="pill">读取状态…</div></div>
 <div class="tabs"><button class="tab active" data-tab="overview">状态</button><button class="tab" data-tab="settings">配置</button><button class="tab" data-tab="models">模型</button><button class="tab" data-tab="logs">日志</button></div>
 <section id="overview" class="panel active">
  <div class="grid">
   <div class="card"><h2>运行状态</h2><div class="metrics"><div class="metric"><div class="k">监听端点</div><div id="endpoint" class="v">—</div></div><div class="metric"><div class="k">运行时长</div><div id="uptime" class="v">—</div></div><div class="metric"><div class="k">Runtime Sys</div><div id="memory" class="v">—</div></div></div><div class="actions"><button id="start" class="primary" onclick="action('start')">启动代理</button><button id="stop" onclick="action('stop')">停止代理</button><button onclick="action('reload')">重载配置</button><button class="danger" onclick="shutdownService()">显式退出后台服务</button></div><div id="overviewError" class="error"></div></div>
   <div class="card"><h2>Qoder 授权</h2><div class="authrow"><div><div id="account" class="v">未授权</div><div id="authHint" class="muted">可直接从网页发起 Qoder 登录。</div></div><div class="actions"><button id="login" class="primary" onclick="startLogin()">登录 / 重新授权</button><button onclick="logout()">退出账号</button></div></div></div>
  </div>
 </section>
 <section id="settings" class="panel"><div class="card"><h2>代理配置</h2><div class="form"><label>监听地址</label><input id="listen" placeholder="127.0.0.1:9000"><label>本地 API Key</label><input id="apiKey" type="password" placeholder="留空则不要求本地鉴权"><label>队列重试次数</label><input id="queueRetries" type="number" min="0"><label>最大排队时间</label><input id="queueMaxWait" placeholder="10m"><label>后台启动后自动启用代理</label><div class="check"><input id="autoStart" type="checkbox"><span class="muted">启用</span></div></div><div class="actions"><button class="primary" onclick="saveSettings()">保存配置</button></div><div class="notice">监听地址修改后需要退出并重新启动后台服务；其它配置会立即重载到正在运行的代理。</div><div id="settingsError" class="error"></div></div></section>
 <section id="models" class="panel"><div class="card"><h2>模型默认思考深度</h2><div class="notice" style="margin-bottom:12px">选项严格来自每个模型实时的 Qoder thinking_config；自动表示不覆盖上游默认行为。</div><div id="modelArea" class="muted">打开此页后加载模型…</div></div></section>
 <section id="logs" class="panel"><div class="card"><h2>后台日志</h2><div id="logbox" class="logbox">读取日志…</div></div></section>
</div>
<script>
var busy=false;
function duration(sec){sec=Math.max(0,Number(sec)||0);var h=Math.floor(sec/3600),m=Math.floor(sec%3600/60),ss=Math.floor(sec%60);return h?(h+'h '+m+'m'):(m?(m+'m '+ss+'s'):(ss+'s'));}
function err(id,text){var e=document.getElementById(id);e.textContent=text||'';e.style.display=text?'block':'none';}
async function jsonFetch(url,opt){var r=await fetch(url,opt||{cache:'no-store'});var j=await r.json();if(!r.ok)throw new Error(j.error||('HTTP '+r.status));return j;}
async function refresh(){try{var s=await jsonFetch('/admin/api/status');document.getElementById('state').className='pill'+(s.running?' on':'');document.getElementById('state').textContent=s.running?'代理运行中':'代理已停止';document.getElementById('endpoint').textContent=s.proxy_address||s.configured_listen||'—';document.getElementById('uptime').textContent=s.running?duration(s.uptime_seconds):'—';document.getElementById('memory').textContent=s.runtime_sys_mb+' MB';document.getElementById('start').disabled=busy||s.running;document.getElementById('stop').disabled=busy||!s.running;var account=s.credential_email||s.credential_name||(s.credential_ready?'已授权':'未授权');document.getElementById('account').textContent=account;if(s.restart_required)document.getElementById('endpoint').textContent+='（配置端口待重启）';}catch(e){err('overviewError',String(e));}}
async function loadSettings(){try{var s=await jsonFetch('/admin/api/settings');document.getElementById('listen').value=s.listen||'';document.getElementById('apiKey').value=s.api_key||'';document.getElementById('queueRetries').value=s.queue_retries;document.getElementById('queueMaxWait').value=s.queue_max_wait||'';document.getElementById('autoStart').checked=!!s.auto_start;}catch(e){err('settingsError',String(e));}}
async function saveSettings(){err('settingsError','');try{var body={listen:document.getElementById('listen').value,api_key:document.getElementById('apiKey').value,queue_retries:Number(document.getElementById('queueRetries').value),queue_max_wait:document.getElementById('queueMaxWait').value,auto_start:document.getElementById('autoStart').checked};await jsonFetch('/admin/api/settings',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});await refresh();}catch(e){err('settingsError',e.message||String(e));}}
async function action(name){busy=true;err('overviewError','');try{await jsonFetch('/admin/api/'+name,{method:'POST'});}catch(e){err('overviewError',e.message||String(e));}finally{busy=false;await refresh();}}
async function startLogin(){err('overviewError','');try{var s=await jsonFetch('/admin/api/login/start',{method:'POST'});if(s.url)window.open(s.url,'_blank','noopener');document.getElementById('authHint').textContent='已打开 Qoder 授权页，完成后此处会自动更新。';pollLogin();}catch(e){err('overviewError',e.message||String(e));}}
async function pollLogin(){for(var i=0;i<150;i++){await new Promise(function(resolve){setTimeout(resolve,2000);});try{var s=await jsonFetch('/admin/api/login/status');if(!s.pending){if(s.error){err('overviewError',s.error);}else{document.getElementById('authHint').textContent='授权成功';await refresh();await loadModels();}return;}}catch(e){return;}}}
async function logout(){if(!confirm('退出 Qoder 账号并停止代理？'))return;try{await jsonFetch('/admin/api/logout',{method:'POST'});await refresh();}catch(e){err('overviewError',e.message||String(e));}}
async function refreshLogs(){try{var r=await fetch('/admin/api/logs',{cache:'no-store'});var t=await r.text();var b=document.getElementById('logbox');var near=b.scrollHeight-b.scrollTop-b.clientHeight<50;b.textContent=t||'暂无日志';if(near)b.scrollTop=b.scrollHeight;}catch(e){}}
function effortLabel(v){if(v==='')return '自动';if(v==='none')return '关闭';if(v==='minimal')return '最小';if(v==='low')return '低';if(v==='medium')return '中';if(v==='high')return '高';if(v==='xhigh'||v==='max')return '最高';return v;}
async function loadModels(){var area=document.getElementById('modelArea');area.textContent='正在加载模型…';try{var models=await jsonFetch('/admin/api/models');var html='<table class="models"><thead><tr><th>模型</th><th>上下文</th><th>输出</th><th>默认思考</th></tr></thead><tbody>';models.forEach(function(m){html+='<tr><td>'+escapeHTML(m.display_name)+'</td><td>'+m.max_input_tokens+'</td><td>'+m.max_output_tokens+'</td><td><select data-model="'+escapeAttr(m.upstream_id)+'" onchange="saveModelDefault(this)">';m.options.forEach(function(o){html+='<option value="'+escapeAttr(o)+'"'+(o===m.default?' selected':'')+'>'+escapeHTML(effortLabel(o))+'</option>';});html+='</select></td></tr>';});html+='</tbody></table>';area.innerHTML=html;}catch(e){area.textContent=e.message||String(e);}}
async function saveModelDefault(sel){try{await jsonFetch('/admin/api/models/default',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({model_id:sel.getAttribute('data-model'),effort:sel.value})});}catch(e){alert(e.message||String(e));await loadModels();}}
function escapeHTML(s){return String(s).replace(/[&<>"']/g,function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];});}
function escapeAttr(s){return escapeHTML(s);}
async function shutdownService(){if(!confirm('确定显式退出后台服务？代理也会停止。'))return;try{await jsonFetch('/admin/api/shutdown',{method:'POST'});document.getElementById('state').textContent='后台服务已退出';}catch(e){}}
document.querySelectorAll('.tab').forEach(function(btn){btn.addEventListener('click',function(){document.querySelectorAll('.tab').forEach(function(x){x.classList.remove('active');});document.querySelectorAll('.panel').forEach(function(x){x.classList.remove('active');});btn.classList.add('active');document.getElementById(btn.getAttribute('data-tab')).classList.add('active');if(btn.getAttribute('data-tab')==='settings')loadSettings();if(btn.getAttribute('data-tab')==='models')loadModels();if(btn.getAttribute('data-tab')==='logs')refreshLogs();});});
refresh();loadSettings();refreshLogs();setInterval(refresh,2500);setInterval(refreshLogs,4000);
</script>
</body>
</html>`
