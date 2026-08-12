package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPersistentLogBufferPersistsBoundsAndClears verifies the product promises made in the data UI.
// TestPersistentLogBufferPersistsBoundsAndClears 验证数据界面中关于持久化、限额与清理的产品承诺。
func TestPersistentLogBufferPersistsBoundsAndClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "desktop.log")
	logBuffer, err := NewPersistentLogBuffer(path, 128, 160)
	if err != nil {
		t.Fatal(err)
	}
	defer logBuffer.Close()

	for i := 0; i < 12; i++ {
		if _, err := logBuffer.Write([]byte(strings.Repeat("x", 24) + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 160 {
		t.Fatalf("rotated log size = %d, want <= 160", info.Size())
	}
	if logBuffer.String() == "" {
		t.Fatal("memory tail should retain recent logs")
	}

	logBuffer.Clear()
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 || logBuffer.String() != "" {
		t.Fatalf("clear left disk=%d memory=%q", info.Size(), logBuffer.String())
	}
}
