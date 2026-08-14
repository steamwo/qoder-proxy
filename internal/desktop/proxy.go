package desktop

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/qoder"
	"github.com/steamwo/qoder-proxy/internal/server"
)

// ProxyManager owns one live server and its mutable desktop policy snapshot.
// ProxyManager 管理单个运行中服务及其可变的桌面策略快照。
type ProxyManager struct {
	mu      sync.RWMutex
	srv     *http.Server
	ln      net.Listener
	running bool
	addr    string
	started time.Time
	backend *server.Backend
}

func (p *ProxyManager) Running() bool { p.mu.RLock(); defer p.mu.RUnlock(); return p.running }
func (p *ProxyManager) Addr() string  { p.mu.RLock(); defer p.mu.RUnlock(); return p.addr }
func (p *ProxyManager) Uptime() time.Duration {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.running || p.started.IsZero() {
		return 0
	}
	return time.Since(p.started)
}

// Start applies the durable model defaults to the server instance created for this proxy run.
// Start 将持久化的模型默认值应用到本次代理运行所创建的服务实例。
func (p *ProxyManager) Start(cred credential.Credential, s Settings) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	ln, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return err
	}
	app := server.New(http.DefaultClient, cred, s.APIKey)
	app.Backend.SetModelReasoningDefaults(s.ModelReasoningDefaults)
	app.Backend.SetModelContextDefaults(s.ModelContextDefaults)
	maxWait, _ := time.ParseDuration(s.QueueMaxWait)
	if maxWait <= 0 {
		maxWait = 10 * time.Minute
	}
	app.Backend.Qoder.QueueRetry = qoder.QueueRetryPolicy{MaxRetries: s.QueueRetries, MaxWait: maxWait}
	srv := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second}
	p.srv = srv
	p.ln = ln
	p.running = true
	p.addr = ln.Addr().String()
	p.started = time.Now()
	p.backend = app.Backend
	slog.Info("desktop proxy started", "listen", p.addr, "queue_retries", s.QueueRetries, "queue_max_wait", maxWait)
	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("desktop proxy stopped unexpectedly", "error", err)
		}
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
	}()
	return nil
}

// UpdateModelReasoningDefaults applies saved choices without interrupting active connections.
// UpdateModelReasoningDefaults 无需中断现有连接即可应用已保存的选择。
func (p *ProxyManager) UpdateModelReasoningDefaults(defaults map[string]string) {
	p.mu.RLock()
	backend := p.backend
	p.mu.RUnlock()
	if backend != nil {
		backend.SetModelReasoningDefaults(defaults)
	}
}

// UpdateModelContextDefaults applies saved context windows without interrupting active connections.
func (p *ProxyManager) UpdateModelContextDefaults(defaults map[string]int) {
	p.mu.RLock()
	backend := p.backend
	p.mu.RUnlock()
	if backend != nil {
		backend.SetModelContextDefaults(defaults)
	}
}

// Stop clears the live backend reference so later settings edits cannot target a stopped server.
// Stop 清除运行中后端引用，避免后续设置修改作用到已停止的服务。
func (p *ProxyManager) Stop(ctx context.Context) error {
	p.mu.Lock()
	srv := p.srv
	p.mu.Unlock()
	if srv == nil {
		return nil
	}
	err := srv.Shutdown(ctx)
	p.mu.Lock()
	p.running = false
	p.srv = nil
	p.ln = nil
	p.backend = nil
	p.started = time.Time{}
	p.mu.Unlock()
	slog.Info("desktop proxy stopped")
	return err
}
