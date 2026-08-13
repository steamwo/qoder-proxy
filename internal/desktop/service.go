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
)

const defaultControlListen = "127.0.0.1:39091"

// ServiceOptions configures the lightweight background process. The control
// listener is always intended to stay on loopback; it is not a public API.
type ServiceOptions struct {
	ControlListen string
	OpenBrowser   bool
}

type backgroundService struct {
	mu        sync.Mutex
	proxy     ProxyManager
	store     *credential.Store
	dataPaths DataPaths
	logbuf    *LogBuffer
	shutdown  context.CancelFunc
}

type serviceStatus struct {
	Running          bool   `json:"running"`
	ProxyAddress     string `json:"proxy_address"`
	ConfiguredListen string `json:"configured_listen"`
	UptimeSeconds    int64  `json:"uptime_seconds"`
	CredentialReady  bool   `json:"credential_ready"`
	SettingsPath     string `json:"settings_path"`
	LogPath          string `json:"log_path"`
	HeapAllocMB      uint64 `json:"heap_alloc_mb"`
	RuntimeSysMB     uint64 `json:"runtime_sys_mb"`
	Goroutines       int    `json:"goroutines"`
}

// RunBackgroundService runs the proxy independently from any Gio window and
// exposes a small loopback-only management page. Closing the management page
// does not stop this process or the proxy.
func RunBackgroundService(parent context.Context, opts ServiceOptions) error {
	controlListen := strings.TrimSpace(opts.ControlListen)
	if controlListen == "" {
		controlListen = strings.TrimSpace(os.Getenv("QODER_PROXY_CONTROL_LISTEN"))
	}
	if controlListen == "" {
		controlListen = defaultControlListen
	}
	if err := requireLoopback(controlListen); err != nil {
		return err
	}

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
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	svc := &backgroundService{store: store, dataPaths: dataPaths, logbuf: logbuf, shutdown: cancel}

	settings := LoadSettings()
	if settings.AutoStart {
		if err := svc.startProxy(); err != nil {
			slog.Warn("background proxy auto-start skipped", "error", err)
		}
	}

	ln, err := net.Listen("tcp", controlListen)
	if err != nil {
		return fmt.Errorf("control listener %s: %w", controlListen, err)
	}
	defer ln.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", svc.handleDashboard)
	mux.HandleFunc("/api/status", svc.handleStatus)
	mux.HandleFunc("/api/logs", svc.handleLogs)
	mux.HandleFunc("/api/start", svc.handleStart)
	mux.HandleFunc("/api/stop", svc.handleStop)
	mux.HandleFunc("/api/restart", svc.handleRestart)
	mux.HandleFunc("/api/shutdown", svc.handleShutdown)
	controlServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	controlURL := "http://" + ln.Addr().String() + "/"
	slog.Info("background service started", "control", controlURL, "proxy_running", svc.proxy.Running())
	if opts.OpenBrowser {
		go func() {
			timer := time.NewTimer(250 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				if err := OpenURL(controlURL); err != nil {
					slog.Warn("open service dashboard", "error", err)
				}
			case <-ctx.Done():
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		err := controlServer.Serve(ln)
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
	_ = controlServer.Shutdown(shutdownCtx)
	_ = svc.proxy.Stop(shutdownCtx)
	slog.Info("background service stopped")
	return nil
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid control listen address %q: %w", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("control listener must use a loopback IP, got %q", host)
	}
	return nil
}

func (s *backgroundService) startProxy() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proxy.Running() {
		return nil
	}
	cred, err := s.store.Load()
	if err != nil {
		return err
	}
	if cred.Expired() {
		return fmt.Errorf("stored Qoder credential is expired; log in with the desktop app")
	}
	return s.proxy.Start(cred, LoadSettings())
}

func (s *backgroundService) stopProxy() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.proxy.Stop(ctx)
}

func (s *backgroundService) restartProxy() error {
	if err := s.stopProxy(); err != nil {
		return err
	}
	return s.startProxy()
}

func (s *backgroundService) status() serviceStatus {
	settings := LoadSettings()
	_, credentialErr := s.store.Load()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	const mib = 1024 * 1024
	return serviceStatus{
		Running:          s.proxy.Running(),
		ProxyAddress:     s.proxy.Addr(),
		ConfiguredListen: settings.Listen,
		UptimeSeconds:    int64(s.proxy.Uptime().Seconds()),
		CredentialReady:  credentialErr == nil,
		SettingsPath:     s.dataPaths.Settings,
		LogPath:          s.dataPaths.Log,
		HeapAllocMB:      mem.HeapAlloc / mib,
		RuntimeSysMB:     mem.Sys / mib,
		Goroutines:       runtime.NumGoroutine(),
	}
}

func (s *backgroundService) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	_, _ = io.WriteString(w, serviceDashboardHTML)
}

func (s *backgroundService) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, s.status())
}

func (s *backgroundService) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	text := s.logbuf.String()
	if len(text) > 64<<10 {
		text = text[len(text)-(64<<10):]
	}
	_, _ = io.WriteString(w, text)
}

func (s *backgroundService) handleStart(w http.ResponseWriter, r *http.Request) {
	if !allowMutation(w, r) {
		return
	}
	if err := s.startProxy(); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.status())
}

func (s *backgroundService) handleStop(w http.ResponseWriter, r *http.Request) {
	if !allowMutation(w, r) {
		return
	}
	if err := s.stopProxy(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.status())
}

func (s *backgroundService) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !allowMutation(w, r) {
		return
	}
	if err := s.restartProxy(); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.status())
}

func (s *backgroundService) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !allowMutation(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	go s.shutdown()
}

func allowMutation(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return false
	}
	// Browsers serving the dashboard send a same-origin Origin header. Reject a
	// cross-origin browser POST so an unrelated website cannot control localhost.
	if origin := r.Header.Get("Origin"); origin != "" {
		expected := "http://" + r.Host
		if origin != expected {
			http.Error(w, "cross-origin control request rejected", http.StatusForbidden)
			return false
		}
	}
	return true
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

const serviceDashboardHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Qoder Proxy Service</title>
<style>
:root{font-family:Inter,"Segoe UI",system-ui,sans-serif;color:#151922;background:#f4f6f9;color-scheme:light}*{box-sizing:border-box}body{margin:0;min-height:100vh}.shell{max-width:1040px;margin:0 auto;padding:40px 24px 56px}.hero{display:flex;align-items:flex-start;justify-content:space-between;gap:24px;margin-bottom:24px}.eyebrow{font-size:12px;font-weight:700;letter-spacing:.12em;text-transform:uppercase;color:#1769e8}.title{font-size:32px;font-weight:700;margin:6px 0 8px}.sub{color:#667085;font-size:14px}.pill{display:inline-flex;align-items:center;gap:8px;padding:8px 12px;border-radius:999px;background:#eef2f6;font-size:13px;font-weight:650}.dot{width:8px;height:8px;border-radius:50%;background:#98a2b3}.pill.on{background:#e5f7f1;color:#087e63}.pill.on .dot{background:#11a982}.grid{display:grid;grid-template-columns:1.35fr .65fr;gap:18px}.card{background:#fff;border:1px solid #e0e5ec;border-radius:18px;padding:22px;box-shadow:0 8px 30px rgba(31,41,55,.05)}.card h2{font-size:15px;margin:0 0 18px}.metrics{display:grid;grid-template-columns:repeat(3,1fr);gap:12px}.metric{background:#f6f8fb;border-radius:13px;padding:14px}.metric .k{font-size:11px;color:#8c96a8;margin-bottom:7px}.metric .v{font-size:18px;font-weight:700;overflow-wrap:anywhere}.actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:18px}button{border:0;border-radius:11px;padding:10px 15px;font:inherit;font-size:13px;font-weight:650;cursor:pointer;background:#edf1f6;color:#344054}button.primary{background:#1769e8;color:white}button.danger{background:#fff0ef;color:#c33e38}button:disabled{opacity:.5;cursor:wait}.kv{display:grid;grid-template-columns:110px 1fr;gap:8px 14px;font-size:13px}.kv .k{color:#8c96a8}.kv .v{overflow-wrap:anywhere}.logs{margin-top:18px}.logbox{height:280px;overflow:auto;background:#10151d;color:#cbd5e1;border-radius:14px;padding:14px;font:12px/1.55 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap}.hint{margin-top:14px;color:#8c96a8;font-size:12px}.error{display:none;margin-top:12px;color:#c33e38;font-size:13px}@media(max-width:760px){.grid{grid-template-columns:1fr}.hero{flex-direction:column}.metrics{grid-template-columns:1fr 1fr}.shell{padding:24px 16px}.title{font-size:27px}}
</style>
</head>
<body>
<div class="shell">
  <div class="hero">
    <div><div class="eyebrow">Lightweight service</div><div class="title">Qoder Proxy</div><div class="sub">代理常驻在轻量后台进程中；关闭此页面不会停止服务。</div></div>
    <div id="statePill" class="pill"><span class="dot"></span><span id="stateText">读取状态…</span></div>
  </div>
  <div class="grid">
    <section class="card">
      <h2>运行状态</h2>
      <div class="metrics">
        <div class="metric"><div class="k">代理端点</div><div class="v" id="endpoint">—</div></div>
        <div class="metric"><div class="k">运行时长</div><div class="v" id="uptime">—</div></div>
        <div class="metric"><div class="k">Go Runtime Sys</div><div class="v" id="memory">—</div></div>
      </div>
      <div class="actions">
        <button id="start" class="primary" onclick="act('start')">启动代理</button>
        <button id="stop" onclick="act('stop')">停止代理</button>
        <button id="restart" onclick="act('restart')">重启并加载设置</button>
        <button class="danger" onclick="shutdownService()">退出后台服务</button>
      </div>
      <div id="error" class="error"></div>
    </section>
    <aside class="card">
      <h2>本地信息</h2>
      <div class="kv">
        <div class="k">配置监听</div><div class="v" id="configured">—</div>
        <div class="k">凭据</div><div class="v" id="credential">—</div>
        <div class="k">Go Heap</div><div class="v" id="heap">—</div>
        <div class="k">Goroutines</div><div class="v" id="goroutines">—</div>
        <div class="k">设置文件</div><div class="v" id="settingsPath">—</div>
        <div class="k">日志文件</div><div class="v" id="logPath">—</div>
      </div>
      <div class="hint">完整设置、账号额度和模型管理仍可使用原来的 Gio 桌面版；其代码和构建均保留。</div>
    </aside>
  </div>
  <section class="card logs">
    <h2>后台日志</h2>
    <div id="logs" class="logbox">读取日志…</div>
  </section>
</div>
<script>
let busy=false;
function duration(sec){sec=Math.max(0,Number(sec)||0);const h=Math.floor(sec/3600),m=Math.floor(sec%3600/60),s=Math.floor(sec%60);return h?(h+'h '+m+'m'):(m?(m+'m '+s+'s'):(s+'s'))}
async function refresh(){try{const r=await fetch('/api/status',{cache:'no-store'});const s=await r.json();document.getElementById('statePill').className='pill'+(s.running?' on':'');document.getElementById('stateText').textContent=s.running?'代理运行中':'代理已停止';document.getElementById('endpoint').textContent=s.proxy_address||s.configured_listen||'—';document.getElementById('uptime').textContent=s.running?duration(s.uptime_seconds):'—';document.getElementById('memory').textContent=String(s.runtime_sys_mb)+' MB';document.getElementById('configured').textContent=s.configured_listen||'—';document.getElementById('credential').textContent=s.credential_ready?'可用':'未登录 / 已失效';document.getElementById('heap').textContent=String(s.heap_alloc_mb)+' MB';document.getElementById('goroutines').textContent=s.goroutines;document.getElementById('settingsPath').textContent=s.settings_path;document.getElementById('logPath').textContent=s.log_path;document.getElementById('start').disabled=busy||s.running;document.getElementById('stop').disabled=busy||!s.running;document.getElementById('restart').disabled=busy;}catch(e){showError(String(e))}}
async function refreshLogs(){try{const r=await fetch('/api/logs',{cache:'no-store'});const t=await r.text();const box=document.getElementById('logs');const nearBottom=box.scrollHeight-box.scrollTop-box.clientHeight<50;box.textContent=t||'暂无日志';if(nearBottom)box.scrollTop=box.scrollHeight}catch(e){}}
function showError(t){const el=document.getElementById('error');el.textContent=t;el.style.display=t?'block':'none'}
async function act(name){busy=true;showError('');await refresh();try{const r=await fetch('/api/'+name,{method:'POST'});const j=await r.json();if(!r.ok)throw new Error(j.error||('HTTP '+r.status));}catch(e){showError(e.message||String(e))}finally{busy=false;await refresh();await refreshLogs()}}
async function shutdownService(){if(!confirm('退出后台服务并停止代理？'))return;busy=true;try{await fetch('/api/shutdown',{method:'POST'});document.getElementById('stateText').textContent='后台服务正在退出';}catch(e){} }
refresh();refreshLogs();setInterval(refresh,2000);setInterval(refreshLogs,3000);
</script>
</body>
</html>`
