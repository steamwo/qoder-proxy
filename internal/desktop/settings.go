package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Settings is the durable desktop product configuration, including per-model behavior keyed by upstream ID.
// Settings 是桌面产品的持久配置，模型行为按上游 ID 存储。
type Settings struct {
	Listen                 string            `json:"listen"`
	APIKey                 string            `json:"api_key,omitempty"`
	QueueRetries           int               `json:"queue_retries"`
	QueueMaxWait           string            `json:"queue_max_wait"`
	AutoStart              bool              `json:"auto_start"`
	MinimizeToTray         bool              `json:"minimize_to_tray"`
	TrayNotifications      bool              `json:"tray_notifications"`
	ModelReasoningDefaults map[string]string `json:"model_reasoning_defaults,omitempty"`
	ModelContextDefaults   map[string]int    `json:"model_context_defaults,omitempty"`
}

// DefaultSettings initializes maps eagerly so first-run model choices can be saved safely.
// DefaultSettings 预先初始化映射，确保首次运行即可安全保存模型选项。
func DefaultSettings() Settings {
	return Settings{
		Listen: "127.0.0.1:9000", QueueRetries: 20, QueueMaxWait: "10m", AutoStart: true,
		MinimizeToTray: true, TrayNotifications: true,
		ModelReasoningDefaults: map[string]string{}, ModelContextDefaults: map[string]int{},
	}
}

// SettingsPath exposes the resolved file location so users can inspect their local data.
// SettingsPath 暴露解析后的文件位置，便于用户检查本地数据。
func SettingsPath() (string, error) {
	d, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(d, "qoder-proxy", "desktop.json"), nil
}

// LoadSettings tolerates missing or partially invalid files so the desktop app always starts.
// LoadSettings 容忍文件缺失或部分字段无效，确保桌面应用始终可以启动。
func LoadSettings() Settings {
	s := DefaultSettings()
	p, e := SettingsPath()
	if e != nil {
		return s
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	if s.Listen == "" {
		s.Listen = "127.0.0.1:9000"
	}
	if s.QueueRetries < 0 {
		s.QueueRetries = 0
	}
	if _, e := time.ParseDuration(s.QueueMaxWait); e != nil {
		s.QueueMaxWait = "10m"
	}
	// Older configuration files omit these maps; normalize them before the UI mutates them.
	// 旧配置文件没有这些映射；在界面修改前先将其规范化。
	if s.ModelReasoningDefaults == nil {
		s.ModelReasoningDefaults = map[string]string{}
	}
	if s.ModelContextDefaults == nil {
		s.ModelContextDefaults = map[string]int{}
	}
	return s
}

// SaveSettings writes private local configuration with restrictive permissions.
// SaveSettings 使用严格权限写入本地私有配置。
func SaveSettings(s Settings) error {
	p, e := SettingsPath()
	if e != nil {
		return e
	}
	if s.Listen == "" {
		return errors.New("listen address is empty")
	}
	// The Gio settings form predates context defaults and rebuilds Settings from
	// its visible fields. Preserve the existing map when that caller leaves it nil.
	if s.ModelContextDefaults == nil {
		var existing struct {
			ModelContextDefaults map[string]int `json:"model_context_defaults,omitempty"`
		}
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &existing) == nil && existing.ModelContextDefaults != nil {
			s.ModelContextDefaults = existing.ModelContextDefaults
		} else {
			s.ModelContextDefaults = map[string]int{}
		}
	}
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(p, append(b, '\n'), 0600)
}
