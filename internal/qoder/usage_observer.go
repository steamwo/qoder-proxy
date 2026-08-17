package qoder

import (
	"bytes"
	"io"
	"log/slog"
	"sync"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

const usageObserverTailBytes = 512 << 10

type usageObservingReadCloser struct {
	body     io.ReadCloser
	observe  func(protocol.Usage)
	tail     []byte
	finalize sync.Once
}

func observeUsageBody(body io.ReadCloser, observe func(protocol.Usage)) io.ReadCloser {
	if body == nil || observe == nil {
		return body
	}
	return &usageObservingReadCloser{body: body, observe: observe}
}

func (r *usageObservingReadCloser) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.appendTail(p[:n])
	}
	if err == io.EOF {
		r.finish()
	}
	return n, err
}

func (r *usageObservingReadCloser) Close() error {
	r.finish()
	return r.body.Close()
}

func (r *usageObservingReadCloser) appendTail(data []byte) {
	if len(data) >= usageObserverTailBytes {
		r.tail = append(r.tail[:0], data[len(data)-usageObserverTailBytes:]...)
		return
	}
	if overflow := len(r.tail) + len(data) - usageObserverTailBytes; overflow > 0 {
		copy(r.tail, r.tail[overflow:])
		r.tail = r.tail[:len(r.tail)-overflow]
	}
	r.tail = append(r.tail, data...)
}

func (r *usageObservingReadCloser) finish() {
	r.finalize.Do(func() {
		if len(r.tail) == 0 {
			return
		}

		// Temporary real-upstream diagnostic: dump the raw tail of Qoder's SSE
		// response so cache/usage fields can be inspected exactly as returned.
		// This may include model output content and must be removed after testing.
		slog.Info("qoder raw response tail", "bytes", len(r.tail), "raw", string(r.tail))

		var last protocol.Usage
		found := false
		_ = ParseStream(bytes.NewReader(r.tail), func(ev protocol.Event) error {
			if ev.Kind == protocol.EventUsage {
				last = ev.Usage
				found = true
			}
			return nil
		})
		if !found {
			return
		}
		if last.TotalTokens == 0 {
			last.TotalTokens = last.InputTokens + last.OutputTokens
		}
		if last.InputTokens == 0 && last.OutputTokens == 0 && last.TotalTokens == 0 {
			return
		}
		r.observe(last)
	})
}
