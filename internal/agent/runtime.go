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

	"dfpanel/internal/distrib"
)

// Spec 描述一个被托管的 frp 进程（或容器）
type Spec struct {
	Kind    string // frps / frpc
	Runtime string // process / docker
	BinPath string
	Image   string
	// ConfigPath 配置文件的路径：Agent 自己读写用它，docker 运行时会被拷进容器
	ConfigPath    string
	LogPath       string
	// PidPath 子进程 pid 文件：Agent 重启后据此接管仍在运行的实例（docker 运行时不用）
	PidPath       string
	ContainerName string
	// MountBinary docker 运行时专用：由面板下发的 frp 二进制（Agent 视角的本地路径）。
	// 底座镜像里没有 frp，它必须存在 —— 启动时用 docker cp 拷进容器并覆盖 entrypoint。
	MountBinary string
	// MountErr 拿不到 MountBinary 的原因（docker 不可用 / 容器平台不支持 / 槽位尚未就绪），
	// 用于把「为什么起不来」讲清楚，而不是笼统报一句缺文件。
	MountErr string
}

// fileReady 文件存在且非空（0 字节的槽位文件视为没有）
func fileReady(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}

// requireDockerReady 容器运行时的启动前校验：面板下发的二进制必须已经在本机就位。
//
// 底座镜像（alpine 之类）不含 frp，没有第二条退路：缺了就是起不来，
// 要的是在面板上直接看到「先去设置页下载」，而不是静默跑起一个版本不明的 frp。
func requireDockerReady(spec Spec) error {
	if spec.Runtime != "docker" {
		return nil
	}
	if spec.MountErr != "" {
		return fmt.Errorf("%s 容器无法启动：%s", spec.Kind, spec.MountErr)
	}
	if !fileReady(spec.MountBinary) {
		return fmt.Errorf("%s 容器无法启动：面板下发的 frp 二进制不存在（%s），"+
			"请先在面板「设置 → frp 二进制」里按平台下载", spec.Kind, spec.MountBinary)
	}
	return nil
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
	// adoptedPid 接管的实例 pid（上个 Agent 进程留下的），>0 时 Running / Stop 都走它
	adoptedPid int
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

	// 手动拷入 / 落在共享目录里的二进制可能缺执行位，启动前补一刀
	if err := distrib.EnsureExecutable(c.spec.BinPath); err != nil {
		_ = f.Close()
		return fmt.Errorf("%s 二进制不可用：%w", c.spec.Kind, err)
	}

	cmd := exec.Command(c.spec.BinPath, "-c", c.spec.ConfigPath)
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		_ = f.Close()
		return fmt.Errorf("启动 %s 失败：%w%s", c.spec.Kind, err, distrib.ExecHint(err))
	}

	c.gen++
	gen := c.gen
	c.cmd = cmd
	c.logFile = f
	c.exited = false
	c.adoptedPid = 0
	// 记下 pid：Agent 重启后靠它认出并接管这个实例（写失败不影响本次运行）
	_ = writePidFile(c.spec.PidPath, cmd.Process.Pid)

	go func() {
		_ = cmd.Wait()
		mine := false
		c.mu.Lock()
		// 仅当前代进程退出才更新状态，旧进程退出不影响新实例
		if c.gen == gen {
			c.exited = true
			c.logFile = nil
			c.adoptedPid = 0
			mine = true
		}
		c.mu.Unlock()
		// 只有自己这一代退出时才清 pid 文件，免得抹掉新进程刚写进去的
		if mine {
			removePidFile(c.spec.PidPath)
		}
		_ = f.Close()
	}()
	return nil
}

func (c *Controller) startDocker() error {
	// 底座镜像里没有 frp，二进制得先就位
	if err := requireDockerReady(c.spec); err != nil {
		return err
	}

	// 容器不可变：每次启动前移除旧容器，保证用上新的配置与二进制
	_ = exec.Command("docker", "rm", "-f", c.spec.ContainerName).Run()

	image := c.spec.Image
	if image == "" {
		image = DefaultFrpcImage
	}
	// --network host 仅 Linux 支持，Windows / macOS 退化为默认网络
	args := dockerCreateArgs(c.spec, image, runtime.GOOS == "linux")
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("创建 %s 容器失败：%v，输出：%s", c.spec.Kind, err, strings.TrimSpace(string(out)))
	}

	// 二进制与配置用 docker cp 送进去：走 daemon 传输，不要求宿主机看得到这些文件，
	// 于是 Agent 装在宿主上还是跑在容器里都成立（bind-mount 就得宿主路径，很容易踩空）
	for _, f := range []struct{ src, dst, label string }{
		{c.spec.MountBinary, ContainerBinaryInContainer(c.spec.Kind), c.spec.Kind + " 二进制"},
		{c.spec.ConfigPath, ContainerConfigPath, "配置"},
	} {
		out, err := exec.Command("docker", "cp", f.src, c.spec.ContainerName+":"+f.dst).CombinedOutput()
		if err != nil {
			_ = exec.Command("docker", "rm", "-f", c.spec.ContainerName).Run()
			return fmt.Errorf("把 %s 拷进容器失败：%v，输出：%s", f.label, err, strings.TrimSpace(string(out)))
		}
	}

	if out, err := exec.Command("docker", "start", c.spec.ContainerName).CombinedOutput(); err != nil {
		return fmt.Errorf("启动 %s 容器失败：%v，输出：%s", c.spec.Kind, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// dockerCreateArgs 构造 frp 容器的 docker create 参数。
//
// 拆成纯函数是为了能在没有 docker 的环境下单测校验参数顺序
// （--entrypoint 必须位于镜像名之前，`-c 配置` 必须位于镜像名之后）。
//
// 刻意不挂载任何文件：底座镜像里没有 frp，二进制与配置都在 create 之后用 docker cp 拷进去，
// 因此容器内用的是固定路径（见 ContainerBinaryInContainer / ContainerConfigPath）。
func dockerCreateArgs(spec Spec, image string, hostNetwork bool) []string {
	args := []string{"create", "--name", spec.ContainerName, "--restart", "unless-stopped"}
	if hostNetwork {
		args = append(args, "--network", "host")
	}
	args = append(args, "--entrypoint", ContainerBinaryInContainer(spec.Kind))
	return append(args, image, "-c", ContainerConfigPath)
}

// Adopt 尝试接管上一个 Agent 进程留下的实例。
//
// Agent 退出时刻意不杀自己拉起的 frp（这样重启时隧道不会中断），所以重启后先看 pid 文件：
// 进程还在、且确实是本实例（配置路径 / 映像路径对得上）就直接接管，不再重复启动 ——
// 否则旧进程占着端口，新进程起来必然失败。
func (c *Controller) Adopt() bool {
	if c.spec.Runtime == "docker" || c.spec.PidPath == "" {
		return false
	}
	pid := readPidFile(c.spec.PidPath)
	if pid <= 0 {
		return false
	}
	if !processAlive(pid) || !processMatchesBinary(pid, c.spec.BinPath, c.spec.ConfigPath) {
		// pid 文件过期（进程已退出，或 pid 被复用）：清掉，走正常启动
		removePidFile(c.spec.PidPath)
		return false
	}

	c.mu.Lock()
	c.adoptedPid = pid
	c.exited = false
	c.mu.Unlock()
	return true
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

	// 接管的实例没有 cmd 句柄，只能按 pid 杀
	c.mu.Lock()
	if c.adoptedPid > 0 {
		pid := c.adoptedPid
		c.adoptedPid = 0
		c.mu.Unlock()
		_ = killProcess(pid)
		// 等它真的退出：端口与文件占用要靠它释放
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !processAlive(pid) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		removePidFile(c.spec.PidPath)
		return nil
	}

	cmd := c.cmd
	if cmd == nil || cmd.Process == nil {
		c.cmd = nil
		c.mu.Unlock()
		removePidFile(c.spec.PidPath)
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
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	removePidFile(c.spec.PidPath)
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
	if c.adoptedPid > 0 {
		return processAlive(c.adoptedPid)
	}
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
