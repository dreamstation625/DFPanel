package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAgentLogPersistsAndRotates(t *testing.T) {
	dir := t.TempDir()
	w, err := openAgentLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	w.maxBytes = 12
	for _, line := range []string{"first\n", "second\n", "third\n", "fourth\n"} {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"agent.log":   "fourth\n",
		"agent.log.1": "third\n",
		"agent.log.2": "second\n",
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s: got %q, want %q", name, got, want)
		}
	}
	w, err = openAgentLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("restart\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "agent.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "fourth\nrestart\n" {
		t.Fatalf("重启后应追加 Agent 日志，实际：%q", got)
	}
}
