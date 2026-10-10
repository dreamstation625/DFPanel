package logutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogRotationKeepsInheritedHandleAndCapsBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "process.log")
	w, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	child, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	for i := 0; i < 3; i++ {
		if _, err := child.Write(make([]byte, MaxBytes+1)); err != nil {
			t.Fatal(err)
		}
		if err := w.Check(); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{".1", ".2"} {
		fi, err := os.Stat(path + suffix)
		if err != nil || fi.Size() > MaxBytes {
			t.Fatalf("日志备份未限量 %s: %v", suffix, err)
		}
	}
	child.WriteString("still-owned")
	data, _ := os.ReadFile(path)
	if string(data) != "still-owned" {
		t.Fatal("继承句柄未写入原日志")
	}
}
