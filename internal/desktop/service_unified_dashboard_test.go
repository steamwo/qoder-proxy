package desktop

import (
	"strings"
	"testing"
)

func TestUnifiedDashboardLinksAuthorAndContextControls(t *testing.T) {
	for _, want := range []string{
		`href="https://github.com/steamwo"`,
		`/admin/api/model-contexts`,
		`/admin/api/models/context-default`,
		`默认上下文`,
		`<th>倍率</th>`,
		`priceFactorLabel(m.price_factor)`,
	} {
		if !strings.Contains(unifiedDashboardHTML, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
	if strings.Contains(unifiedDashboardHTML, `<th>输出上限</th>`) {
		t.Fatal("dashboard still renders output-token column instead of multiplier")
	}
}
