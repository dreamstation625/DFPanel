package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Spec 描述一个被托管的 frp 进程（或容器）
type Spec struct {
	Kind          string // frps / frpc
	Runtime       string // process / docker
	BinPath       string
	Image         string
	ConfigPath    string
	LogPath       string
	ContainerName string
}

// Controller 负责单个 frps / frpc 的启停与日志，屏蔽「进程 / 容器」两种运行时差异
type Controller struct {
	spec Spec

	mu      sync.Mutex
	cmd     *exec.Cmd
	exited  bool
	logFile *os.File
	// gen 进程代次：只用于区分「当前这一代进程」的退出事件，
	// 避免旧进程的 Wait 返回后把刚启动的新进程误标为已退出
	gen int
}

// NewController 创建控制器
func NewController(spec Spec) *Controller {
	return &Controller{spec: spec}
}

// Start 启动 frp（容器运行时会先清理同名容器）
func (c *Controller) Start() error {
	if c.spec.Runtime == "docker" {
		return c.startDocker()
	}
	return c.startProcess()
}

func (c *Controller) startProcess() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.spec.BinPath == "" {
		return fmt.Errorf("缺少 %s 二进制", c.spec.Kind)
	}
	if _, err := os.Stat(c.spec.BinPath); err != nil {
		return fmt.Errorf("未找到 %s 二进制：%w", c.spec.Kind, err)
	}
	if _, err := os.Stat(c.spec.ConfigPath); err != nil {
		return fmt.Errorf("未找到配置文件 %s：%w", c.spec.ConfigPath, err)
	}

	f, err := os.OpenFile(c.spec.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志文件失败：%w", err)
	}

	cmd := exec.Command(c.spec.BinPath, "-c", c.spec.ConfigPath)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		_ = f.Close()
		return fmt.Errorf("启动 %s 失败：%w", c.spec.Kind, err)
	}

	c.gen++
	gen := c.gen
	c.cmd = cmd
	c.logFile = f
	c.exited = false

	go func() {
		_ = cmd.Wait()
		c.mu.Lock()
		// 仅当前代进程退出才更新状态，旧进程退出不影响新实例
		if c.gen == gen {
			c.exited = true
			c.logFile = nil
		}
		c.mu.Unlock()
		_ = f.Close()
	}()
	return nil
}

func (c *Controller) startDocker() error {
	// 容器不可变：每次启动前移除旧容器，保证使用新配置
	_ = exec.Command("docker", "rm", "-f", c.spec.ContainerName).Run()

	args := []string{"run", "-d", "--name", c.spec.ContainerName, "--restart", "unless-stopped",
		"-v", fmt.Sprintf("%s:%s:ro", c.spec.ConfigPath, c.spec.ConfigPath)}
	// --network host 仅 Linux 支持，Windows / macOS 退化为默认网络
	if runtime.GOOS == "linux" {
		args = append(args, "--network", "host")
	}
	image := c.spec.Image
	if image == "" {
		image = DefaultFrpcImage
	}
	args = append(args, image, "-c", c.spec.ConfigPath)

	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("启动 %s 容器失败：%v，输出：%s", c.spec.Kind, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Stop 停止并清理
func (c *Controller) Stop() error {
	if c.spec.Runtime == "docker" {
		out, err := exec.Command("docker", "rm", "-f", c.spec.ContainerName).CombinedOutput()
		if err != nil && !strings.Contains(string(out), "No such container") {
			return fmt.Errorf("停止 %s 容器失败：%v", c.spec.Kind, err)
		}
		return nil
	}

	c.mu.Lock()
	cmd := c.cmd
	if cmd == nil || cmd.Process == nil {
		c.cmd = nil
		c.mu.Unlock()
		return nil
	}
	gen := c.gen
	alreadyExited := c.exited
	c.cmd = nil
	c.mu.Unlock()

	if !alreadyExited {
		_ = cmd.Process.Kill()
	}

	// 等待进程真正退出：端口释放需要时间，立刻启动新实例会因端口被占而失败
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		done := c.exited && c.gen == gen
		c.mu.Unlock()
		if done {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// Running 判断是否在运行
func (c *Controller) Running() bool {
	if c.spec.Runtime == "docker" {
		out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", c.spec.ContainerName).Output()
		if err != nil {
			return false
		}
		return strings.TrimSpace(string(out)) == "true"
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cmd != nil && c.cmd.Process != nil && !c.exited
}

// Logs 读取运行日志（容器运行时取 docker logs，进程运行时读日志文件尾部）
func (c *Controller) Logs(maxBytes int64) (string, error) {
	if c.spec.Runtime == "docker" {
		out, err := exec.Command("docker", "logs", "--tail", "300", c.spec.ContainerName).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("读取容器日志失败：%v", err)
		}
		s := string(out)
		if int64(len(s)) > maxBytes {
			s = s[int64(len(s))-maxBytes:]
		}
		return s, nil
	}
	return tailFile(c.spec.LogPath, maxBytes)
}

// LogSize 当前日志文件大小，用于增量扫描新增日志
func (c *Controller) LogSize() int64 {
	if c.spec.Runtime == "docker" {
		return 0
	}
	fi, err := os.Stat(c.spec.LogPath)
	if err != nil {
		return 0
	}
	return fi.Size()
}

func tailFile(path string, maxBytes int64) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", nil
	}
	start := int64(0)
	if fi.Size() > maxBytes {
		start = fi.Size() - maxBytes
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	buf := make([]byte, fi.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && len(buf) == 0 {
		return "", err
	}
	return string(buf), nil
}

// Restart 重启
func (c *Controller) Restart() error {
	if err := c.Stop(); err != nil {
		return err
	}
	// 容器删除后立即重建可能尚未释放同名资源，短暂等待
	if c.spec.Runtime == "docker" {
		time.Sleep(time.Second)
	}
	return c.Start()
}

// execCommand 带超时的命令执行（用于 frp verify 等一次性校验）
func execCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}
