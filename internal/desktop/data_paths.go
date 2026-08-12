package desktop

import (
	"os"
	"path/filepath"
)

// DataPaths describes every user-owned file so the UI and runtime share one source of truth.
// DataPaths 描述所有用户数据文件，确保界面与运行时使用同一套路径来源。
type DataPaths struct {
	Settings    string
	Credentials string
	Log         string
}

// ResolveDataPaths separates durable configuration from disposable runtime logs by OS convention.
// ResolveDataPaths 按操作系统约定分离持久配置与可清理的运行日志。
func ResolveDataPaths(credentialOverride string) (DataPaths, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return DataPaths{}, err
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return DataPaths{}, err
	}
	appConfigDir := filepath.Join(configDir, "qoder-proxy")
	credentials := credentialOverride
	// An explicit credential path remains authoritative for CLI and desktop compatibility.
	// 显式凭据路径保持最高优先级，以兼容命令行与桌面应用。
	if credentials == "" {
		credentials = filepath.Join(appConfigDir, "credentials.json")
	}
	return DataPaths{
		Settings:    filepath.Join(appConfigDir, "desktop.json"),
		Credentials: filepath.Clean(credentials),
		Log:         filepath.Join(cacheDir, "qoder-proxy", "logs", "desktop.log"),
	}, nil
}
