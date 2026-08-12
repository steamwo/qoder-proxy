package desktop

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LogEntry is a UI-friendly view of one structured slog line. The original
// line is kept so the details panel never hides information from operators.
type LogEntry struct {
	Time       time.Time
	Level      string
	Message    string
	Method     string
	Path       string
	Model      string
	Status     int
	DurationMS int64
	Remote     string
	Raw        string
}

type LogStats struct {
	Requests      int
	Errors        int
	AverageMillis int64
}

// LogBuffer keeps a fast UI tail while optionally persisting a bounded diagnostic file.
// LogBuffer 保留供界面快速读取的日志尾部，并可持久化为有限大小的诊断文件。
type LogBuffer struct {
	mu      sync.RWMutex
	b       bytes.Buffer
	max     int
	file    *os.File
	path    string
	maxFile int64
}

// NewLogBuffer creates an in-memory buffer for environments where disk logging is unavailable.
// NewLogBuffer 为无法写入磁盘的环境创建内存日志缓冲区。
func NewLogBuffer(max int) *LogBuffer {
	if max <= 0 {
		max = 256 << 10
	}
	return &LogBuffer{max: max}
}

// NewPersistentLogBuffer restores the latest log tail and appends future events to a bounded file.
// NewPersistentLogBuffer 恢复最近的日志尾部，并将后续事件追加到有限大小的文件。
func NewPersistentLogBuffer(path string, maxMemory int, maxFile int64) (*LogBuffer, error) {
	// A conservative default prevents an unattended tray process from growing logs indefinitely.
	// 保守默认值可防止长期驻留托盘的进程无限增长日志。
	if maxFile <= 0 {
		maxFile = 4 << 20
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	l := NewLogBuffer(maxMemory)
	l.path, l.maxFile = path, maxFile
	if data, err := os.ReadFile(path); err == nil {
		l.writeMemory(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		// Permission errors are surfaced so callers can choose an in-memory fallback.
		// 权限异常会返回给调用方，由其决定是否降级为内存日志。
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	l.file = f
	return l, nil
}

// writeMemory trims from the oldest side because the UI only needs recent diagnostics.
// writeMemory 从最旧一侧裁剪，因为界面只需要最近的诊断信息。
func (l *LogBuffer) writeMemory(p []byte) {
	_, _ = l.b.Write(p)
	if l.b.Len() <= l.max {
		return
	}
	data := append([]byte(nil), l.b.Bytes()...)
	keep := l.max * 3 / 4
	if keep < len(data) {
		data = data[len(data)-keep:]
	}
	l.b.Reset()
	_, _ = l.b.Write(data)
}

// Write updates both views atomically so clearing and rotation cannot race with log emission.
// Write 原子更新内存与文件视图，避免清理或轮转与日志写入发生竞争。
func (l *LogBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.writeMemory(p)
	if l.file != nil {
		if _, err := l.file.Write(p); err != nil {
			return 0, err
		}
		if info, err := l.file.Stat(); err == nil && info.Size() > l.maxFile {
			// Rotation stays on the writer path to preserve deterministic ordering.
			// 轮转保持在写入路径内，以确保日志顺序确定。
			if err := l.rotateFile(); err != nil {
				return 0, err
			}
		}
	}
	return len(p), nil
}
func (l *LogBuffer) String() string { l.mu.RLock(); defer l.mu.RUnlock(); return l.b.String() }

// rotateFile retains the newest three quarters to cap disk growth without losing current context.
// rotateFile 保留最新的四分之三内容，在限制磁盘增长的同时保留当前上下文。
func (l *LogBuffer) rotateFile() error {
	if err := l.file.Close(); err != nil {
		return err
	}
	data, err := os.ReadFile(l.path)
	if err != nil {
		return err
	}
	keep := int(l.maxFile * 3 / 4)
	if keep < len(data) {
		data = data[len(data)-keep:]
	}
	if err := os.WriteFile(l.path, data, 0o600); err != nil {
		return err
	}
	l.file, err = os.OpenFile(l.path, os.O_APPEND|os.O_WRONLY, 0o600)
	return err
}

// Clear removes both visible and persisted diagnostics because the UI promises a real deletion.
// Clear 同时删除界面与磁盘日志，兑现界面中的真实清理承诺。
func (l *LogBuffer) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b.Reset()
	if l.file != nil {
		// Reopening is required because Windows append handles do not reliably truncate in place.
		// Windows 的追加句柄无法可靠地原地截断，因此需要关闭后重新打开。
		_ = l.file.Close()
		_ = os.WriteFile(l.path, nil, 0o600)
		l.file, _ = os.OpenFile(l.path, os.O_APPEND|os.O_WRONLY, 0o600)
	}
}

// Path returns the persistent log location for product transparency.
// Path 返回持久日志位置，用于向用户透明展示数据去向。
func (l *LogBuffer) Path() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.path
}

// Close flushes the operating-system file handle during orderly application shutdown.
// Close 在应用正常退出时关闭操作系统文件句柄。
func (l *LogBuffer) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// Entries returns newest entries first. It understands the stable key/value
// shape emitted by slog.TextHandler and gracefully falls back to the raw line.
func (l *LogBuffer) Entries(limit int) []LogEntry {
	l.mu.RLock()
	text := l.b.String()
	l.mu.RUnlock()
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if limit <= 0 || limit > len(lines) {
		limit = len(lines)
	}
	out := make([]LogEntry, 0, limit)
	for i := len(lines) - 1; i >= 0 && len(out) < limit; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		e := LogEntry{
			Raw:     line,
			Level:   valueFor(line, "level"),
			Message: valueFor(line, "msg"),
			Method:  valueFor(line, "method"),
			Path:    valueFor(line, "path"),
			Model:   firstValue(valueFor(line, "model"), valueFor(line, "display_name")),
			Remote:  valueFor(line, "remote"),
		}
		e.Time, _ = time.Parse(time.RFC3339Nano, valueFor(line, "time"))
		e.Status, _ = strconv.Atoi(valueFor(line, "status"))
		e.DurationMS, _ = strconv.ParseInt(valueFor(line, "duration_ms"), 10, 64)
		out = append(out, e)
	}
	return out
}

func (l *LogBuffer) Stats() LogStats {
	entries := l.Entries(0)
	var stats LogStats
	var total int64
	for _, e := range entries {
		if e.Method == "" && e.Status == 0 {
			continue
		}
		stats.Requests++
		if e.Status >= 400 || strings.EqualFold(e.Level, "ERROR") {
			stats.Errors++
		}
		if e.DurationMS > 0 {
			total += e.DurationMS
		}
	}
	if stats.Requests > 0 {
		stats.AverageMillis = total / int64(stats.Requests)
	}
	return stats
}

func valueFor(line, key string) string {
	marker := key + "="
	i := strings.Index(line, marker)
	if i < 0 {
		return ""
	}
	rest := line[i+len(marker):]
	if strings.HasPrefix(rest, "\"") {
		rest = rest[1:]
		var escaped bool
		for j, r := range rest {
			if r == '\\' && !escaped {
				escaped = true
				continue
			}
			if r == '"' && !escaped {
				return rest[:j]
			}
			escaped = false
		}
		return strings.TrimSuffix(rest, "\"")
	}
	if j := strings.IndexAny(rest, " \t"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func firstValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
