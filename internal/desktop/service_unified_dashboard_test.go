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
	} {
		if !strings.Contains(unifiedDashboardHTML, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
}
