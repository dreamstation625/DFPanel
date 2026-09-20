package frp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// proc 单个 frps 实例的运行态
type proc struct {
	cmd     *exec.Cmd
	logFile *os.File
}

// Manager 负责 frps 进程的生命周期（一体化模式）
type Manager struct {
	mu      sync.Mutex
	procs   map[uint]*proc
	binDir  string
	dataDir string
}

// NewManager 创建 frps 进程管理器
func NewManager(binDir, dataDir string) *Manager {
	return &Manager{
		procs:   make(map[uint]*proc),
		binDir:  binDir,
		dataDir: dataDir,
	}
}

// BinPath frps 二进制路径
func (m *Manager) BinPath() string {
	name := "frps"
	if runtime.GOOS == "windows" {
		name = "frps.exe"
	}
	return filepath.Join(m.binDir, name)
}

// ConfigPath 某实例的 frps.json 路径
func (m *Manager) ConfigPath(id uint) string {
	return filepath.Join(m.dataDir, fmt.Sprintf("frps-%d.json", id))
}

// LogPath 某实例的日志路径
func (m *Manager) LogPath(id uint) string {
	return filepath.Join(m.dataDir, fmt.Sprintf("frps-%d.log", id))
}

// IsInstalled 是否已放置 frps 二进制
func (m *Manager) IsInstalled() bool {
	_, err := os.Stat(m.BinPath())
	return err == nil
}

// Running 判断实例是否在运行
func (m *Manager) Running(id uint) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.procs[id] != nil
}

// WriteConfig 将生成内容写入磁盘（0644）
func (m *Manager) WriteConfig(id uint, content string) error {
	return os.WriteFile(m.ConfigPath(id), []byte(content), 0o644)
}

// Start 启动实例：调用 frps -c <config>
func (m *Manager) Start(id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.procs[id] != nil {
		return errors.New("frps 已在运行")
	}
	bin := m.BinPath()
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("未找到 frps 二进制：%s", bin)
	}

	logFile, err := os.OpenFile(m.LogPath(id), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}

	cmd := exec.Command(bin, "-c", m.ConfigPath(id))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("启动 frps 失败: %w", err)
	}

	m.procs[id] = &proc{cmd: cmd, logFile: logFile}

	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		m.mu.Lock()
		delete(m.procs, id)
		m.mu.Unlock()
	}()

	return nil
}

// Stop 停止实例
func (m *Manager) Stop(id uint) error {
	m.mu.Lock()
	p := m.procs[id]
	m.mu.Unlock()

	if p == nil || p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("停止 frps 失败: %w", err)
	}
	return nil
}

// Restart 重启实例
func (m *Manager) Restart(id uint) error {
	if err := m.Stop(id); err != nil {
		return err
	}
	// 等待旧进程退出（进程退出后会从 map 中移除）
	for i := 0; i < 50; i++ {
		if !m.Running(id) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return m.Start(id)
}

// TailLog 读取实例日志末尾 maxBytes 字节
func (m *Manager) TailLog(id uint, maxBytes int64) (string, error) {
	f, err := os.Open(m.LogPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := stat.Size()
	offset := int64(0)
	if size > maxBytes {
		offset = size - maxBytes
	}
	if _, err := f.Seek(offset, 0); err != nil {
		return "", err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}
