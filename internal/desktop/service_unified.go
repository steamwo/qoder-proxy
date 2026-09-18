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
	Listen       string `json:"listen"`
	APIKey       string `json:"api_key"`
	QueueRetries int    `json:"queue_retries"`
	QueueMaxWait string `json:"queue_max_wait"`
	AutoStart    bool   `json:"auto_start"`
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

type unifiedQuotaView struct {
	Available             bool    `json:"available"`
	Plan                  string  `json:"plan,omitempty"`
	UserRemaining         float64 `json:"user_remaining,omitempty"`
	UserLimit             float64 `json:"user_limit,omitempty"`
	UserRemainingPercent  float64 `json:"user_remaining_percent,omitempty"`
	UserUsedPercent       float64 `json:"user_used_percent,omitempty"`
	OrganizationRemaining float64 `json:"organization_remaining,omitempty"`
	OrganizationLimit     float64 `json:"organization_limit,omitempty"`
	ResetAt               string  `json:"reset_at,omitempty"`
	FetchedAt             string  `json:"fetched_at,omitempty"`
	Error                 string  `json:"error,omitempty"`
}

type unifiedLogsView struct {
	Text          string `json:"text"`
	Requests      int    `json:"requests"`
	Errors        int    `json:"errors"`
	AverageMillis int64  `json:"average_millis"`
}

// RunUnifiedService serves both /v1/* and /admin/* on the configured proxy port.
// The admin surface stays restricted to loopback clients even when the proxy
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

	// The Windows service is linked with -H=windowsgui and may not have a valid
	// stderr handle. Keep the in-memory/persistent log as the first writer so a
	// failed console write can never prevent the admin log view from receiving
	// the event.
	var logWriter io.Writer = logbuf
	if _, statErr := os.Stderr.Stat(); statErr == nil {
		logWriter = io.MultiWriter(logbuf, os.Stderr)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: slog.LevelInfo})))

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
	app.Backend.SetCredentialPersister(s.store.Save)
	app.Backend.SetModelReasoningDefaults(settings.ModelReasoningDefaults)
	app.Backend.SetModelContextDefaults(settings.ModelContextDefaults)
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
	if !s.isEnabled() {
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
	case r.URL.Path == "/admin/api/quota" && r.Method == http.MethodGet:
		s.handleUnifiedQuota(w, r)
	case r.URL.Path == "/admin/api/logs" && r.Method == http.MethodGet:
		s.handleUnifiedLogs(w)
	case r.URL.Path == "/admin/api/logs/clear" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		s.logbuf.Clear()
		slog.Info("admin logs cleared")
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
		slog.Info("admin configuration reloaded")
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
		s.loginMu.Lock()
		s.loginEmail, s.loginName, s.loginURL, s.loginError, s.loginPending = "", "", "", "", false
		s.loginMu.Unlock()
		slog.Info("qoder account logged out from admin")
		unifiedWriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case r.URL.Path == "/admin/api/models" && r.Method == http.MethodGet:
		s.handleUnifiedModels(w, r)
	case r.URL.Path == "/admin/api/model-contexts" && r.Method == http.MethodGet:
		s.handleUnifiedModelContexts(w, r)
	case r.URL.Path == "/admin/api/models/default" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		s.handleUnifiedModelDefault(w, r)
	case r.URL.Path == "/admin/api/models/context-default" && r.Method == http.MethodPost:
		if !unifiedAllowMutation(w, r) { return }
		s.handleUnifiedModelContextDefault(w, r)
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
	text := s.logbuf.String()
	if len(text) > 128<<10 {
		text = text[len(text)-(128<<10):]
	}
	stats := s.logbuf.Stats()
	unifiedWriteJSON(w, http.StatusOK, unifiedLogsView{
		Text: text, Requests: stats.Requests, Errors: stats.Errors, AverageMillis: stats.AverageMillis,
	})
}

func (s *unifiedService) handleUnifiedQuota(w http.ResponseWriter, r *http.Request) {
	cred, err := s.store.Load()
	if err != nil || cred.Expired() {
		unifiedWriteJSON(w, http.StatusUnauthorized, unifiedQuotaView{Available: false, Error: "请先授权 Qoder 账号"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	snapshot, err := qoder.FetchQuota(ctx, http.DefaultClient, cred)
	if err != nil {
		unifiedWriteJSON(w, http.StatusBadGateway, unifiedQuotaView{Available: false, Error: err.Error()})
		return
	}
	view := unifiedQuotaView{Available: true, Plan: snapshot.Plan}
	if !snapshot.FetchedAt.IsZero() { view.FetchedAt = snapshot.FetchedAt.Format(time.RFC3339) }
	if snapshot.User != nil {
		view.UserRemaining = snapshot.User.Remaining
		view.UserLimit = snapshot.User.Limit
		view.UserRemainingPercent = snapshot.User.RemainingPercent
		view.UserUsedPercent = snapshot.User.UsedPercent
		if !snapshot.User.ResetAt.IsZero() { view.ResetAt = snapshot.User.ResetAt.Format(time.RFC3339) }
	}
	if snapshot.Organization != nil {
		view.OrganizationRemaining = snapshot.Organization.Remaining
		view.OrganizationLimit = snapshot.Organization.Limit
		if view.ResetAt == "" && !snapshot.Organization.ResetAt.IsZero() { view.ResetAt = snapshot.Organization.ResetAt.Format(time.RFC3339) }
	}
	unifiedWriteJSON(w, http.StatusOK, view)
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
	slog.Info("admin settings saved", "listen", settings.Listen, "queue_retries", settings.QueueRetries, "queue_max_wait", settings.QueueMaxWait, "auto_start", settings.AutoStart)
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
		if err == nil { err = s.store.Save(cred) }
		if err == nil {
			settings := LoadSettings()
			if settings.AutoStart || s.isEnabled() { err = s.enableProxy() }
		}
		s.loginMu.Lock()
		s.loginPending = false
		if err != nil {
			s.loginError = err.Error()
			slog.Warn("qoder authorization failed", "error", err)
		} else {
			s.loginError = ""
			s.loginEmail = cred.Email
			s.loginName = cred.Name
			slog.Info("qoder authorization completed", "email", cred.Email, "name", cred.Name)
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
	if backend != nil { registry = backend.Registry }
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
		if model.SupportsReasoningDisabled() { options = append(options, "none") }
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
	if strings.TrimSpace(req.Effort) == "" {
		delete(settings.ModelReasoningDefaults, model.UpstreamID)
	} else {
		settings.ModelReasoningDefaults[model.UpstreamID] = strings.ToLower(strings.TrimSpace(req.Effort))
	}
	if err := SaveSettings(settings); err != nil {
		unifiedWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.RLock()
	backend := s.backend
	s.mu.RUnlock()
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
