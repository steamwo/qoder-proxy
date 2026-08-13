//go:build desktop

package main

import (
	"log/slog"
	"runtime"
	"sync"
	"time"
)

// startMemoryDiagnostics records Go runtime memory separately from the process
// working set so native/GPU allocations are easier to distinguish from Go heap growth.
// startMemoryDiagnostics 单独记录 Go 运行时内存，便于区分 Go 堆增长与原生/GPU 工作集。
func startMemoryDiagnostics() func() {
	logMemoryStats("startup")

	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				logMemoryStats("periodic")
			case <-done:
				return
			}
		}
	}()

	return func() {
		once.Do(func() { close(done) })
	}
}

func logMemoryStats(reason string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	const mib = 1024 * 1024
	slog.Debug("runtime memory",
		"reason", reason,
		"heap_alloc_mb", m.HeapAlloc/mib,
		"heap_inuse_mb", m.HeapInuse/mib,
		"heap_sys_mb", m.HeapSys/mib,
		"heap_released_mb", m.HeapReleased/mib,
		"stack_inuse_mb", m.StackInuse/mib,
		"runtime_sys_mb", m.Sys/mib,
		"next_gc_mb", m.NextGC/mib,
		"num_gc", m.NumGC,
		"goroutines", runtime.NumGoroutine(),
	)
}
