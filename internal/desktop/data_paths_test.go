package desktop

import (
	"path/filepath"
	"testing"
)

// TestResolveDataPathsHonorsCredentialOverride protects the documented escape hatch for shared CLI credentials.
// TestResolveDataPathsHonorsCredentialOverride 保护命令行与桌面端共享凭据时的显式覆盖约定。
func TestResolveDataPathsHonorsCredentialOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "account.json")
	paths, err := ResolveDataPaths(override)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Credentials != filepath.Clean(override) {
		t.Fatalf("credentials path = %q, want %q", paths.Credentials, override)
	}
	if filepath.Base(paths.Settings) != "desktop.json" {
		t.Fatalf("settings path = %q", paths.Settings)
	}
	if filepath.Base(paths.Log) != "desktop.log" {
		t.Fatalf("log path = %q", paths.Log)
	}
}
