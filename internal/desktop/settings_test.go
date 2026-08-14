package desktop

import (
	"runtime"
	"testing"
)

func setTestConfigRoot(t *testing.T) {
	t.Helper()
	configRoot := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", configRoot)
	} else {
		t.Setenv("XDG_CONFIG_HOME", configRoot)
	}
}

// TestSettingsModelDefaultsPersistence verifies stable-ID choices survive the real save/load path.
func TestSettingsModelDefaultsPersistence(t *testing.T) {
	setTestConfigRoot(t)
	settings := DefaultSettings()
	settings.ModelReasoningDefaults["model-id"] = "high"
	settings.ModelContextDefaults["model-id"] = 1000000
	if err := SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	restored := LoadSettings()
	if restored.ModelReasoningDefaults["model-id"] != "high" {
		t.Fatalf("reasoning defaults=%v", restored.ModelReasoningDefaults)
	}
	if restored.ModelContextDefaults["model-id"] != 1000000 {
		t.Fatalf("context defaults=%v", restored.ModelContextDefaults)
	}
}

func TestSaveSettingsPreservesContextDefaultsWhenLegacyCallerOmitsMap(t *testing.T) {
	setTestConfigRoot(t)
	settings := DefaultSettings()
	settings.ModelContextDefaults["model-id"] = 400000
	if err := SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	legacyStyle := DefaultSettings()
	legacyStyle.ModelContextDefaults = nil
	legacyStyle.APIKey = "updated"
	if err := SaveSettings(legacyStyle); err != nil {
		t.Fatal(err)
	}
	restored := LoadSettings()
	if restored.ModelContextDefaults["model-id"] != 400000 {
		t.Fatalf("context defaults were lost: %v", restored.ModelContextDefaults)
	}
}
