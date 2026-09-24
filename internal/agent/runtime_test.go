package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// indexOf 返回值在切片中的位置，找不到返回 -1
func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

// Agent 自己跑在容器里：挂载源用宿主路径，挂载目标与 -c 仍是 Agent 视角的路径
func TestDockerRunArgsUsesHostSources(t *testing.T) {
	dir := t.TempDir()
	slot := filepath.Join(dir, "frpc-container-linux-amd64")
	if err := os.WriteFile(slot, []byte("fake-frpc"), 0o755); err != nil {
		t.Fatal(err)
	}

	spec := Spec{
		Kind:          "frpc",
		Runtime:       "docker",
		Image:         DefaultFrpcImage,
		ConfigPath:    "/var/lib/dfpanel-agent/frpc-1.json",
		ConfigSource:  "/opt/dfpanel-agent/frpc-1.json",
		ContainerName: "dfpanel-frpc-1",
		MountBinary:   slot,
		MountSource:   "/opt/dfpanel-agent/bin/frpc-container-linux-amd64",
	}
	args := dockerRunArgs(spec, spec.Image, true)

	if indexOf(args, "/opt/dfpanel-agent/frpc-1.json:/var/lib/dfpanel-agent/frpc-1.json:ro") < 0 {
		t.Errorf("配置挂载应为「宿主源:Agent 路径」：%v", args)
	}
	if indexOf(args, "/opt/dfpanel-agent/bin/frpc-container-linux-amd64:"+ContainerFrpcPath+":ro") < 0 {
		t.Errorf("二进制挂载源应为宿主路径：%v", args)
	}
	// 容器里的参数用 Agent 视角的配置路径（也是挂载目标）
	idx := indexOf(args, "-c")
	if idx < 0 || idx+1 >= len(args) || args[idx+1] != spec.ConfigPath {
		t.Errorf("-c 应当用 Agent 视角的配置路径：%v", args)
	}
}

// 没有可挂载的二进制时，纯函数不注入 --entrypoint（真正的拦截在 requireDockerReady，
// 见 TestRequireDockerReady）
func TestDockerRunArgsWithoutMountedBinary(t *testing.T) {
	spec := Spec{
		Kind:          "frps",
		Runtime:       "docker",
		Image:         DefaultFrpsImage,
		ConfigPath:    "/var/lib/dfpanel-agent/frps-1.json",
		ContainerName: "dfpanel-frps-1",
	}
	args := dockerRunArgs(spec, spec.Image, true)

	if indexOf(args, "--entrypoint") >= 0 {
		t.Errorf("未挂载二进制时不应出现 --entrypoint：%v", args)
	}
	if got := indexOf(args, spec.ConfigPath+":"+spec.ConfigPath+":ro"); got < 0 {
		t.Errorf("应挂载配置文件：%v", args)
	}
	// 镜像名之后必须紧跟 -c <配置>
	imgIdx := indexOf(args, spec.Image)
	if imgIdx < 0 {
		t.Fatalf("缺少镜像名：%v", args)
	}
	if imgIdx+2 >= len(args) || args[imgIdx+1] != "-c" || args[imgIdx+2] != spec.ConfigPath {
		t.Errorf("镜像名后应为 [-c 配置]，实际：%v", args[imgIdx:])
	}
	if indexOf(args, "--network") < 0 {
		t.Errorf("hostNetwork=true 时应带 --network：%v", args)
	}
}

func TestDockerRunArgsWithMountedBinary(t *testing.T) {
	dir := t.TempDir()
	slot := filepath.Join(dir, "frps-container-linux-amd64")
	if err := os.WriteFile(slot, []byte("fake-linux-frps"), 0o755); err != nil {
		t.Fatal(err)
	}

	spec := Spec{
		Kind:          "frps",
		Runtime:       "docker",
		Image:         DefaultFrpsImage,
		ConfigPath:    "/var/lib/dfpanel-agent/frps-1.json",
		ContainerName: "dfpanel-frps-1",
		MountBinary:   slot,
	}
	args := dockerRunArgs(spec, spec.Image, true)

	// 必须把宿主机二进制挂到容器内固定路径
	wantMount := slot + ":" + ContainerFrpsPath + ":ro"
	if indexOf(args, wantMount) < 0 {
		t.Errorf("缺少二进制挂载 %q：%v", wantMount, args)
	}

	// --entrypoint 必须指向容器内路径，且位于镜像名之前（docker run 的选项不能出现在镜像名之后）
	epIdx := indexOf(args, "--entrypoint")
	if epIdx < 0 || epIdx+1 >= len(args) || args[epIdx+1] != ContainerFrpsPath {
		t.Fatalf("--entrypoint 参数不正确：%v", args)
	}
	imgIdx := indexOf(args, spec.Image)
	if imgIdx < 0 || epIdx > imgIdx {
		t.Errorf("--entrypoint 必须位于镜像名之前：%v", args)
	}
	// 镜像名后仍要带上 -c 配置，作为覆盖 entrypoint 后的命令参数
	if imgIdx+2 >= len(args) || args[imgIdx+1] != "-c" || args[imgIdx+2] != spec.ConfigPath {
		t.Errorf("镜像名后应为 [-c 配置]，实际：%v", args[imgIdx:])
	}
}

func TestDockerRunArgsFrpcUsesOwnMountPoint(t *testing.T) {
	dir := t.TempDir()
	slot := filepath.Join(dir, "frpc-container-linux-arm64")
	if err := os.WriteFile(slot, []byte("fake-linux-frpc"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Kind:          "frpc",
		ConfigPath:    "/var/lib/dfpanel-agent/frpc-7.json",
		ContainerName: "dfpanel-frpc-7",
		MountBinary:   slot,
	}
	args := dockerRunArgs(spec, DefaultFrpcImage, false)

	if indexOf(args, ContainerFrpcPath) < 0 {
		t.Errorf("frpc 应挂到 %s：%v", ContainerFrpcPath, args)
	}
	if indexOf(args, ContainerFrpsPath) >= 0 {
		t.Errorf("frpc 不应使用 frps 的挂载点：%v", args)
	}
	// 非 Linux 宿主不带 --network host（该选项在 Docker Desktop 上无意义）
	if indexOf(args, "--network") >= 0 {
		t.Errorf("hostNetwork=false 时不应带 --network：%v", args)
	}
}

func TestDockerRunArgsIgnoresEmptyMountFile(t *testing.T) {
	// 空文件视为无效槽位：不能拿它当 entrypoint（容器会直接起不来）
	dir := t.TempDir()
	slot := filepath.Join(dir, "frps-container-linux-amd64")
	if err := os.WriteFile(slot, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		Kind:        "frps",
		Runtime:     "docker",
		ConfigPath:  "/c.json",
		MountBinary: slot,
	}
	args := dockerRunArgs(spec, "img:latest", false)
	if indexOf(args, "--entrypoint") >= 0 {
		t.Errorf("空槽位文件不应触发覆盖：%v", args)
	}
	if err := requireDockerReady(spec); err == nil {
		t.Error("空槽位文件应当被拦下")
	}
}

// 容器底座镜像里没有 frp：没有面板下发的二进制就不许起容器，
// 且要把「为什么没有」讲清楚（docker 不可用 / 槽位没就绪 / 路径宿主看不到）
func TestRequireDockerReady(t *testing.T) {
	dir := t.TempDir()
	slot := filepath.Join(dir, "frpc-container-linux-amd64")
	if err := os.WriteFile(slot, []byte("fake-frpc"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := requireDockerReady(Spec{Kind: "frpc", Runtime: "process", BinPath: slot}); err != nil {
		t.Fatalf("process 运行时不该受容器约束：%v", err)
	}
	if err := requireDockerReady(Spec{Kind: "frpc", Runtime: "docker", MountBinary: slot}); err != nil {
		t.Fatalf("挂载就绪时应当放行：%v", err)
	}

	err := requireDockerReady(Spec{Kind: "frpc", Runtime: "docker"})
	if err == nil {
		t.Fatal("没挂载二进制时应当报错")
	}
	for _, want := range []string{"frpc", "设置"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误提示应包含 %q，实际：%v", want, err)
		}
	}

	// 平台探测失败（docker 不可用）时要把原始原因透出，而不是笼统说缺文件
	err = requireDockerReady(Spec{
		Kind:     "frpc",
		Runtime:  "docker",
		MountErr: "无法探测 docker 容器平台（docker info 失败）：Cannot connect to the Docker daemon",
	})
	if err == nil || !strings.Contains(err.Error(), "Docker daemon") {
		t.Fatalf("应透出平台探测失败的原因，实际：%v", err)
	}

	// Agent 自己跑在容器里、没配宿主数据目录：挂载路径宿主看不到，必须拦下来
	err = requireDockerReady(Spec{
		Kind:        "frpc",
		Runtime:     "docker",
		MountBinary: slot,
		PathErr:     "Agent 自身跑在容器里，但没有配置宿主机数据目录（DFPANEL_HOST_DATA_DIR）",
	})
	if err == nil || !strings.Contains(err.Error(), "DFPANEL_HOST_DATA_DIR") {
		t.Fatalf("应拦下宿主路径不可用的情况，实际：%v", err)
	}
}

// Agent 跑在容器里时，给 docker daemon 的挂载路径要换成宿主路径
func TestHostPath(t *testing.T) {
	cfg := &Config{DataDir: "/var/lib/dfpanel-agent"}
	// 没配宿主机目录（Agent 直装在宿主上）：原样返回
	if got := cfg.HostPath("/var/lib/dfpanel-agent/bin/frpc"); got != "/var/lib/dfpanel-agent/bin/frpc" {
		t.Fatalf("未配置时应原样返回，实际 %s", got)
	}

	cfg.HostDataDir = "/opt/dfpanel-agent"
	// 期望值用 filepath.Join 拼，免得在 Windows 上跑测试时被分隔符绊住
	if got, want := cfg.HostPath("/var/lib/dfpanel-agent/bin/frpc-container-linux-amd64"),
		filepath.Join("/opt/dfpanel-agent", "bin", "frpc-container-linux-amd64"); got != want {
		t.Fatalf("应翻译成宿主路径，期望 %s，实际 %s", want, got)
	}
	if got, want := cfg.HostPath("/var/lib/dfpanel-agent/frpc-1.json"),
		filepath.Join("/opt/dfpanel-agent", "frpc-1.json"); got != want {
		t.Fatalf("应翻译成宿主路径，期望 %s，实际 %s", want, got)
	}
	// 不在数据目录下的路径不硬翻，免得出错到别的地方
	if got := cfg.HostPath("/tmp/elsewhere"); got != "/tmp/elsewhere" {
		t.Fatalf("数据目录外的路径应原样返回，实际 %s", got)
	}
}
