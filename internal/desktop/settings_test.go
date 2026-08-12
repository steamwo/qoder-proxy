package desktop

import (
	"runtime"
	"testing"
)

// TestSettingsReasoningDefaultsPersistence verifies stable-ID choices survive the real save/load path.
// TestSettingsReasoningDefaultsPersistence 验证稳定 ID 选项可通过真实保存/加载路径持久化。
func TestSettingsReasoningDefaultsPersistence(t *testing.T) {
	configRoot := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", configRoot)
	} else {
		t.Setenv("XDG_CONFIG_HOME", configRoot)
	}
	settings := DefaultSettings()
	settings.ModelReasoningDefaults["model-id"] = "high"
	if err := SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	restored := LoadSettings()
	if restored.ModelReasoningDefaults["model-id"] != "high" {
		t.Fatalf("defaults=%v", restored.ModelReasoningDefaults)
	}
}
