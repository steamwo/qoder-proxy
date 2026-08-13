package qoder

import (
	"sync/atomic"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

var runtimeUsage struct {
	input  atomic.Uint64
	output atomic.Uint64
	total  atomic.Uint64
}

// RecordRuntimeUsage accumulates usage for the lifetime of the current process.
// It is intentionally in-memory only: restarting qoder-proxy resets all totals.
func RecordRuntimeUsage(usage protocol.Usage) {
	input := positiveUsageValue(usage.InputTokens)
	output := positiveUsageValue(usage.OutputTokens)
	total := positiveUsageValue(usage.TotalTokens)
	if total == 0 {
		total = input + output
	}
	if input == 0 && output == 0 && total == 0 {
		return
	}
	runtimeUsage.input.Add(input)
	runtimeUsage.output.Add(output)
	runtimeUsage.total.Add(total)
}

func positiveUsageValue(value int) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

// RuntimeUsageTotals returns process-lifetime token totals reported by Qoder.
func RuntimeUsageTotals() (input, output, total uint64) {
	return runtimeUsage.input.Load(), runtimeUsage.output.Load(), runtimeUsage.total.Load()
}
