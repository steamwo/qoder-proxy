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

type ProxyManager struct {
	mu      sync.RWMutex
	srv     *http.Server
	ln      net.Listener
	running bool
	addr    string
	started time.Time
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
	p.started = time.Time{}
	p.mu.Unlock()
	slog.Info("desktop proxy stopped")
	return err
}
