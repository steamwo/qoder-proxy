package desktop

import (
	"os/exec"
	"runtime"
)

func OpenURL(u string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		c = exec.Command("open", u)
	default:
		c = exec.Command("xdg-open", u)
	}
	return c.Start()
}

// OpenPath reveals a local file or directory with the platform file manager.
// OpenPath 使用平台文件管理器显示本地文件或目录。
func OpenPath(path string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command("explorer.exe", path)
	case "darwin":
		c = exec.Command("open", path)
	default:
		c = exec.Command("xdg-open", path)
	}
	return c.Start()
}
