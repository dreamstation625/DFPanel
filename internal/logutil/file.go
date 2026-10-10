// Package logutil 管理继承给子进程的日志句柄，保留接管能力并限制日志积累。
package logutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const MaxBytes int64 = 10 << 20

// File 不重命名正在被子进程持有的文件；保留尾部副本后原地截断，Windows 也可轮转。
type File struct {
	*os.File
	path string
	stop chan struct{}
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
}

func Open(path string) (*File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	w := &File{File: f, path: path, stop: make(chan struct{}), done: make(chan struct{})}
	if err := w.Check(); err != nil {
		f.Close()
		return nil, err
	}
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = w.Check()
			case <-w.stop:
				return
			}
		}
	}()
	return w, nil
}

func (w *File) Check() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	info, err := w.File.Stat()
	if err != nil {
		return err
	}
	if info.Size() < MaxBytes {
		return nil
	}
	// 只复制尾部，轮转间隔内突发的大日志也不会导致备份文件无限增长。
	in, err := os.Open(w.path)
	if err != nil {
		return err
	}
	defer in.Close()
	backup := w.path + ".1"
	if err := os.Remove(w.path + ".2"); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(backup, w.path+".2"); err != nil && !os.IsNotExist(err) {
		return err
	}
	out, err := os.OpenFile(backup, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	offset := info.Size() - MaxBytes
	_, copyErr := io.Copy(out, io.NewSectionReader(in, offset, MaxBytes))
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Truncate(w.path, 0); err != nil {
		return fmt.Errorf("截断日志失败：%w", err)
	}
	return nil
}

func (w *File) Close() error {
	var err error
	w.once.Do(func() { close(w.stop); <-w.done; err = w.File.Close() })
	return err
}
