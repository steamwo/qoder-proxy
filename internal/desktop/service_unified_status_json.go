package desktop

import (
	"encoding/json"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

type unifiedStatusAlias unifiedStatus

// MarshalJSON keeps the existing admin status fields for compatibility while
// adding dashboard-friendly process memory and process-lifetime token totals.
func (s unifiedStatus) MarshalJSON() ([]byte, error) {
	input, output, total := qoder.RuntimeUsageTotals()
	return json.Marshal(struct {
		unifiedStatusAlias
		MemoryMB    uint64 `json:"memory_mb"`
		TokenInput  uint64 `json:"token_input"`
		TokenOutput uint64 `json:"token_output"`
		TokenTotal  uint64 `json:"token_total"`
	}{
		unifiedStatusAlias: unifiedStatusAlias(s),
		MemoryMB:           s.RuntimeSysMB,
		TokenInput:         input,
		TokenOutput:        output,
		TokenTotal:         total,
	})
}
