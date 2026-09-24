package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/config"
	"dfpanel/internal/distrib"
	"dfpanel/internal/model"
)

func newFRPCtx(kind, version, goos, goarch string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet,
		"/downloads/"+kind+"/"+version+"/"+goos+"/"+goarch, nil)
	c.Params = gin.Params{
		{Key: "kind", Value: kind},
		{Key: "version", Value: version},
		{Key: "os", Value: goos},
		{Key: "arch", Value: goarch},
	}
	return c, rec
}

// 分发端点只发面板已有的二进制：缺了直接 404 并说清去哪下载，不替 Agent 去抓上游。
// 上游抓取一律在设置页由管理员触发（可配镜像源、有结果反馈）。
func TestDownloadFRPOnlyServesCached(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewInstallHandler(&config.Config{DataDir: t.TempDir()})

	c, rec := newFRPCtx("frpc", "0.62.1", "linux", "amd64")
	h.DownloadFRP(c)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("面板没有缓存时应 404，实际 %d：%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"frpc", "0.62.1", "linux/amd64", "设置"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("错误提示应包含 %q，实际：%s", want, rec.Body.String())
		}
	}

	// 备好一份就照常发出去，并带上具体版本号
	if err := os.MkdirAll(h.binDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.binDir(), distrib.BinaryName("frpc", "0.62.1", "linux", "amd64"))
	if err := os.WriteFile(path, []byte("fake-frpc"), 0o755); err != nil {
		t.Fatal(err)
	}

	c, rec = newFRPCtx("frpc", "0.62.1", "linux", "amd64")
	h.DownloadFRP(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("有缓存时应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Frp-Version"); got != "0.62.1" {
		t.Fatalf("应回传版本号，实际 %q", got)
	}

	// latest 优先命中本地已缓存的最新版，不必为了发个文件去连 GitHub
	c, rec = newFRPCtx("frpc", "latest", "linux", "amd64")
	h.DownloadFRP(c)
	if rec.Code != http.StatusOK || rec.Header().Get("X-Frp-Version") != "0.62.1" {
		t.Fatalf("latest 应命中本地缓存，实际 %d / %q", rec.Code, rec.Header().Get("X-Frp-Version"))
	}

	// 版本号白名单照旧生效（免鉴权端点，必须防路径穿越）
	c, rec = newFRPCtx("frpc", "../../etc/passwd", "linux", "amd64")
	h.DownloadFRP(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法版本号应 400，实际 %d", rec.Code)
	}
}

// 面板记录的期望版本要写进安装命令：Agent 首次启动按它去面板取，而不是自己挑 latest
func TestInstallCommandsCarryFrpVersion(t *testing.T) {
	h := NewInstallHandler(&config.Config{DataDir: t.TempDir()})
	a := &model.Agent{NodeKey: "KEY", Secret: "SECRET", Roles: "frpc", FRPVersion: "0.71.0"}

	res := h.buildInstallCommands("http://panel:7226", a, "linux", "docker")
	if !strings.Contains(res.Binary, "--frp-version 0.71.0") {
		t.Errorf("Linux 安装命令应带上版本：%s", res.Binary)
	}
	if !strings.Contains(res.Docker, "DFPANEL_FRPVERSION=0.71.0") {
		t.Errorf("docker run 命令应带上版本：%s", res.Docker)
	}
	if !strings.Contains(res.Compose, "DFPANEL_FRPVERSION: 0.71.0") {
		t.Errorf("compose 片段应带上版本：%s", res.Compose)
	}
	// docker 运行时的数据目录用当前目录（相对 compose 文件），不用写死的 /opt/...
	if !strings.Contains(res.Compose, "./dfpanel-agent-data:/var/lib/dfpanel-agent") {
		t.Errorf("compose 的数据目录应挂当前目录下的相对路径：%s", res.Compose)
	}
	if strings.Contains(res.Compose, "DFPANEL_HOST_DATA_DIR") {
		t.Errorf("compose 不该出现宿主数据目录（容器内的文件用 docker cp 送）：%s", res.Compose)
	}

	winRes := h.buildInstallCommands("http://panel:7226", a, "windows", "process")
	if !strings.Contains(winRes.Binary, "-FrpVersion 0.71.0") {
		t.Errorf("Windows 安装命令应带上版本：%s", winRes.Binary)
	}

	// 没设期望版本（不管理）时不该硬塞参数进去
	plain := h.buildInstallCommands("http://panel:7226",
		&model.Agent{NodeKey: "KEY", Secret: "SECRET", Roles: "frpc"}, "linux", "process")
	if strings.Contains(plain.Binary, "frp-version") {
		t.Errorf("未设置版本时不该出现版本参数：%s", plain.Binary)
	}
	if strings.Contains(plain.Docker, "DFPANEL_FRPVERSION") {
		t.Errorf("未设置版本时不该出现版本环境变量：%s", plain.Docker)
	}
}
