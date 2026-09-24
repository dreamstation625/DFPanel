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

// frp 容器的参数：create 起壳，二进制与配置随后 docker cp 送进去，所以不出现任何 -v
func TestDockerCreateArgs(t *testing.T) {
	spec := Spec{
		Kind:          "frps",
		Runtime:       "docker",
		Image:         DefaultFrpsImage,
		ConfigPath:    "/var/lib/dfpanel-agent/frps-1.json",
		ContainerName: "dfpanel-frps-1",
	}
	args := dockerCreateArgs(spec, spec.Image, true)

	if args[0] != "create" {
		t.Errorf("应当是 docker create：%v", args)
	}
	if indexOf(args, "-v") >= 0 || indexOf(args, "--volume") >= 0 {
		t.Errorf("不该挂载任何文件（改用 docker cp）：%v", args)
	}
	if indexOf(args, "--restart") < 0 {
		t.Errorf("缺少重启策略：%v", args)
	}
	if indexOf(args, "--network") < 0 {
		t.Errorf("hostNetwork=true 时应带 --network：%v", args)
	}

	// --entrypoint 必须指向容器内的固定路径，且位于镜像名之前
	epIdx := indexOf(args, "--entrypoint")
	if epIdx < 0 || epIdx+1 >= len(args) || args[epIdx+1] != ContainerFrpsPath {
		t.Fatalf("--entrypoint 参数不正确：%v", args)
	}
	imgIdx := indexOf(args, spec.Image)
	if imgIdx < 0 || epIdx > imgIdx {
		t.Errorf("--entrypoint 必须位于镜像名之前：%v", args)
	}
	// 镜像名之后是 [-c 容器内配置路径]
	if imgIdx+2 >= len(args) || args[imgIdx+1] != "-c" || args[imgIdx+2] != ContainerConfigPath {
		t.Errorf("镜像名后应为 [-c %s]，实际：%v", ContainerConfigPath, args[imgIdx:])
	}
}

// frpc 用自己那份容器内路径，别串到 frps 的
func TestDockerCreateArgsFrpcUsesOwnPath(t *testing.T) {
	spec := Spec{Kind: "frpc", Runtime: "docker", ContainerName: "dfpanel-frpc-7"}
	args := dockerCreateArgs(spec, DefaultFrpcImage, false)

	if indexOf(args, ContainerFrpcPath) < 0 {
		t.Errorf("frpc 的 entrypoint 应为 %s：%v", ContainerFrpcPath, args)
	}
	if indexOf(args, ContainerFrpsPath) >= 0 {
		t.Errorf("frpc 不该用 frps 的路径：%v", args)
	}
	// 非 Linux 宿主不带 --network host（该选项在 Docker Desktop 上无意义）
	if indexOf(args, "--network") >= 0 {
		t.Errorf("hostNetwork=false 时不应带 --network：%v", args)
	}
}

// 容器底座镜像里没有 frp：没有面板下发的二进制就不许起容器，
// 且要把「为什么没有」讲清楚（docker 不可用 / 槽位没就绪）
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
		t.Fatalf("二进制就绪时应当放行：%v", err)
	}

	err := requireDockerReady(Spec{Kind: "frpc", Runtime: "docker"})
	if err == nil {
		t.Fatal("没拿到二进制时应当报错")
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

	// 0 字节的槽位文件视为没有（docker 之前把挂载源建成目录也会留下这种东西）
	empty := filepath.Join(dir, "frps-container-linux-amd64")
	if err := os.WriteFile(empty, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := requireDockerReady(Spec{Kind: "frps", Runtime: "docker", MountBinary: empty}); err == nil {
		t.Fatal("空槽位文件应当被拦下")
	}
}
