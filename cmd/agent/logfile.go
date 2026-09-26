package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	agentLogMaxBytes = 10 << 20
	agentLogBackups  = 2
)

// agentLogWriter 将 Agent 自身日志保存在数据目录，避免计划任务的标准错误输出丢失。
// 单文件达到上限时保留两份旧日志；frps / frpc 的日志仍由各自的控制器管理。
type agentLogWriter struct {
	mu       sync.Mutex
	path     string
	file     *os.File
	size     int64
	maxBytes int64
}

func openAgentLog(dataDir string) (*agentLogWriter, error) {
	path := filepath.Join(dataDir, "agent.log")
	w := &agentLogWriter{path: path, maxBytes: agentLogMaxBytes}
	if err := w.open(); err != nil {
		return nil, fmt.Errorf("打开 Agent 日志 %s 失败：%w", path, err)
	}
	return w, nil
}

func (w *agentLogWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func (w *agentLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *agentLogWriter) rotate() (err error) {
	if err = w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	defer func() {
		if err != nil && w.file == nil {
			_ = w.open()
		}
	}()
	if err = os.Remove(fmt.Sprintf("%s.%d", w.path, agentLogBackups)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := agentLogBackups - 1; i >= 1; i-- {
		if err = os.Rename(fmt.Sprintf("%s.%d", w.path, i), fmt.Sprintf("%s.%d", w.path, i+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err = os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	err = w.open()
	return err
}

func (w *agentLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
