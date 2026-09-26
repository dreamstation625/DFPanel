package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

func newAgentDownloadCtx(goos, goarch string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/downloads/agent/"+goos+"/"+goarch, nil)
	c.Params = gin.Params{{Key: "os", Value: goos}, {Key: "arch", Value: goarch}}
	return c, rec
}

func TestDownloadAgentUsesBundledBinaryAndAllowsDataOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewInstallHandler(&config.Config{DataDir: t.TempDir()})
	h.bundledAgentDir = t.TempDir()
	name := "agent-linux-amd64"
	bundled := filepath.Join(h.bundledAgentDir, name)
	if err := os.WriteFile(bundled, []byte("bundled-agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.bundledAgentDir, "VERSION.agent"), []byte("0.0.1-beta.18"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, rec := newAgentDownloadCtx("linux", "amd64")
	h.DownloadAgent(c)
	if rec.Code != http.StatusOK || rec.Body.String() != "bundled-agent" {
		t.Fatalf("应使用镜像内置 Agent，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Agent-Version") != "0.0.1-beta.18" || len(rec.Header().Get("X-Agent-SHA256")) != 64 {
		t.Fatal("下载响应应提供 Agent 版本及 SHA256")
	}

	customDir := filepath.Join(h.cfg.DataDir, "bin")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(customDir, name), []byte("custom-agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, rec = newAgentDownloadCtx("linux", "amd64")
	h.DownloadAgent(c)
	if rec.Code != http.StatusOK || rec.Body.String() != "bundled-agent" {
		t.Fatalf("镜像内置 Agent 应优先于旧数据目录文件，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if err := os.Remove(bundled); err != nil {
		t.Fatal(err)
	}
	c, rec = newAgentDownloadCtx("linux", "amd64")
	h.DownloadAgent(c)
	if rec.Code != http.StatusOK || rec.Body.String() != "custom-agent" {
		t.Fatalf("二进制面板应兼容旧数据目录 Agent，实际 %d：%s", rec.Code, rec.Body.String())
	}

	c, rec = newAgentDownloadCtx("linux", "386")
	h.DownloadAgent(c)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("不支持的平台应返回 400，实际 %d", rec.Code)
	}
}

func TestDownloadAgentUsesActiveBinaryBundle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	h := NewInstallHandler(&config.Config{DataDir: dataDir})
	h.bundledAgentDir = t.TempDir()
	bundleDir := filepath.Join(dataDir, "agent-bundles", "0.0.1-beta.19")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("release-agent"))
	metadata := fmt.Sprintf(`{"panelVersion":"0.0.1-beta.19","agentVersion":"0.0.1-beta.18","files":{"agent-linux-amd64":"%s"}}`, hex.EncodeToString(sum[:]))
	if err := os.WriteFile(filepath.Join(bundleDir, "manifest.json"), []byte(metadata), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "agent-linux-amd64"), []byte("release-agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "agent-bundles", "current"), []byte("0.0.1-beta.19"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, rec := newAgentDownloadCtx("linux", "amd64")
	h.DownloadAgent(c)
	if rec.Code != http.StatusOK || rec.Body.String() != "release-agent" || rec.Header().Get("X-Agent-Version") != "0.0.1-beta.18" {
		t.Fatalf("二进制面板应分发已激活 Agent：%d %s", rec.Code, rec.Body.String())
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "agent-linux-amd64"), []byte("damaged"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, rec = newAgentDownloadCtx("linux", "amd64")
	h.DownloadAgent(c)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("损坏的 Agent 文件不得下发：%d", rec.Code)
	}
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
	// 同机多个 Agent 必须各自使用独立容器名和宿主数据目录。
	if !strings.Contains(res.Compose, "./dfpanel-agent-data/KEY:/var/lib/dfpanel-agent") ||
		!strings.Contains(res.Compose, "container_name: dfpanel-agent-KEY") {
		t.Errorf("compose 的实例隔离信息不正确：%s", res.Compose)
	}
	if strings.Contains(res.Compose, "DFPANEL_HOST_DATA_DIR") {
		t.Errorf("compose 不该出现宿主数据目录（容器内的文件用 docker cp 送）：%s", res.Compose)
	}
	if !strings.Contains(res.UninstallBinary, "--uninstall --instance KEY") || strings.Contains(res.UninstallBinary, "SECRET") {
		t.Errorf("Linux 卸载命令应定位实例且不包含签名密钥：%s", res.UninstallBinary)
	}
	if !strings.Contains(res.UninstallDocker, "docker rm -f dfpanel-agent-KEY") ||
		!strings.Contains(res.UninstallDocker, "/opt/dfpanel-agent/KEY/frp[sc]-*.json") {
		t.Errorf("docker run 卸载命令应清理 Agent 及其托管容器：%s", res.UninstallDocker)
	}
	if !strings.Contains(res.UninstallCompose, "docker compose -p dfpanel-agent-KEY -f docker-compose.agent.yml down") ||
		!strings.Contains(res.UninstallCompose, "./dfpanel-agent-data/KEY/frp[sc]-*.json") {
		t.Errorf("Compose 卸载命令应在对应项目中清理实例：%s", res.UninstallCompose)
	}

	winRes := h.buildInstallCommands("http://panel:7226", a, "windows", "process")
	if !strings.Contains(winRes.Binary, "-FrpVersion 0.71.0") {
		t.Errorf("Windows 安装命令应带上版本：%s", winRes.Binary)
	}
	if winRes.Docker != "" || winRes.Compose != "" {
		t.Errorf("Windows 安装命令不应提供未经验证的 Docker 模式：%+v", winRes)
	}
	if !strings.Contains(winRes.UninstallBinary, "-Uninstall -Instance KEY") ||
		strings.Contains(winRes.UninstallBinary, "SECRET") || winRes.UninstallDocker != "" || winRes.UninstallCompose != "" {
		t.Errorf("Windows 应只提供不含密钥的脚本卸载命令：%+v", winRes)
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
