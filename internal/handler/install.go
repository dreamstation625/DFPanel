package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/distrib"
	"dfpanel/internal/model"
	"dfpanel/internal/setting"
)

// InstallHandler 安装脚本分发与一键安装命令生成（支持二进制与 Docker 两种形态）
type InstallHandler struct {
	cfg *config.Config
}

// NewInstallHandler 创建安装处理器
func NewInstallHandler(cfg *config.Config) *InstallHandler {
	return &InstallHandler{cfg: cfg}
}

// installCommands 生成结果：同时给出二进制与 Docker 两种形态，前端按需要展示
type installCommands struct {
	PanelURL string `json:"panelUrl"`
	NodeKey  string `json:"nodeKey"`
	Secret   string `json:"secret"`
	Roles    string `json:"roles"`
	Binary   string `json:"binary"`
	Docker   string `json:"docker"`
	Compose  string `json:"compose"`
}

// AgentInstallCommand GET /api/agents/:id/install-command 生成某 Agent 的一键安装命令
func (h *InstallHandler) AgentInstallCommand(c *gin.Context) {
	var agent model.Agent
	if err := database.DB.First(&agent, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}
	osName := strings.ToLower(c.DefaultQuery("os", "linux"))
	runtime := strings.ToLower(c.DefaultQuery("runtime", "process"))
	c.JSON(http.StatusOK, h.buildInstallCommands(h.panelURL(c), &agent, osName, runtime))
}

// NodeInstallCommand GET /api/nodes/:id/install-command 生成节点（frpc）所在 Agent 的安装命令
func (h *InstallHandler) NodeInstallCommand(c *gin.Context) {
	var node model.Node
	if err := database.DB.First(&node, c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "节点不存在"})
		return
	}
	if node.AgentID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该节点尚未绑定 Agent，请先在节点上选择托管 Agent"})
		return
	}
	var agent model.Agent
	if err := database.DB.First(&agent, node.AgentID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Agent 不存在"})
		return
	}
	osName := strings.ToLower(c.DefaultQuery("os", "linux"))
	runtime := strings.ToLower(c.DefaultQuery("runtime", "process"))
	c.JSON(http.StatusOK, h.buildInstallCommands(h.panelURL(c), &agent, osName, runtime))
}

// ScriptSh GET /install.sh
func (h *InstallHandler) ScriptSh(c *gin.Context) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, distrib.InstallSh)
}

// ScriptPs1 GET /install.ps1
func (h *InstallHandler) ScriptPs1(c *gin.Context) {
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, distrib.InstallPs1)
}

// DownloadAgent GET /downloads/agent/:os/:arch 分发 Agent 二进制
// 二进制由构建脚本产出到 <dataDir>/bin/agent-<os>-<arch>[.exe]
func (h *InstallHandler) DownloadAgent(c *gin.Context) {
	osName := strings.ToLower(c.Param("os"))
	arch := strings.ToLower(c.Param("arch"))
	name := fmt.Sprintf("agent-%s-%s", osName, arch)
	if osName == "windows" {
		name += ".exe"
	}
	path := filepath.Join(h.cfg.DataDir, "bin", name)
	if _, err := os.Stat(path); err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("未找到 Agent 二进制 %s，请先在面板服务器执行构建脚本生成并放置到 %s", name, path),
		})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", name))
	c.File(path)
}

// DownloadFRP GET /downloads/:kind/:version/:os/:arch 分发 frps / frpc 二进制
//
// 本地缓存缺失时由面板按配置的下载地址模板（可指向镜像源）抓取，并落为版本化文件。
// version 可用 latest，解析出的具体版本号通过 X-Frp-Version 响应头回传，
// 便于 Agent 侧以确定版本号记账。
//
// 该端点免鉴权且 version 完全由 URL 控制，因此必须先做版本号白名单校验。
func (h *InstallHandler) DownloadFRP(c *gin.Context) {
	kind := strings.ToLower(c.Param("kind"))
	if kind != "frps" && kind != "frpc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "类型只能是 frps / frpc"})
		return
	}
	version := strings.TrimSpace(c.Param("version"))
	if version == "" {
		version = distrib.LatestTag
	}
	if version != distrib.LatestTag && !distrib.ValidVersion(version) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "版本号格式不合法：" + version})
		return
	}
	osName := strings.ToLower(c.Param("os"))
	arch := strings.ToLower(c.Param("arch"))
	if osName == "" {
		osName = runtime.GOOS
	}
	if arch == "" {
		arch = runtime.GOARCH
	}

	base := setting.Load(h.baseFallback()).FrpDownloadBase
	path, err := distrib.EnsureFRPBinary(kind, version, osName, arch, filepath.Join(h.cfg.DataDir, "bin"), base)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "获取 " + kind + " 二进制失败：" + err.Error()})
		return
	}

	resolved := distrib.ParseBinaryName(filepath.Base(path), kind)
	if resolved != "" {
		c.Header("X-Frp-Version", resolved)
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(path)))
	c.File(path)
}

// baseFallback 启动参数/环境变量提供的下载地址模板，作为设置表为空时的回退
func (h *InstallHandler) baseFallback() string {
	if h.cfg == nil {
		return ""
	}
	return h.cfg.FRPDownloadBase
}

// panelURL 面板对外地址：优先取 -public-url，其次按请求推断
func (h *InstallHandler) panelURL(c *gin.Context) string {
	if h.cfg != nil && strings.TrimSpace(h.cfg.PublicURL) != "" {
		return strings.TrimRight(strings.TrimSpace(h.cfg.PublicURL), "/")
	}
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	if host := c.GetHeader("X-Forwarded-Host"); host != "" {
		return fmt.Sprintf("%s://%s", scheme, host)
	}
	return fmt.Sprintf("%s://%s", scheme, c.Request.Host)
}

// buildInstallCommands 生成二进制 / Docker / Compose 三种安装指令
func (h *InstallHandler) buildInstallCommands(panelURL string, agent *model.Agent, osName, runtime string) installCommands {
	roles := strings.TrimSpace(agent.Roles)
	if roles == "" {
		roles = "frpc"
	}
	if runtime != "docker" {
		runtime = "process"
	}
	image := h.agentImage()

	res := installCommands{
		PanelURL: panelURL,
		NodeKey:  agent.NodeKey,
		Secret:   agent.Secret,
		Roles:    roles,
	}
	res.Binary = binaryInstallCommand(panelURL, agent, osName, roles, runtime)
	res.Docker = dockerRunCommand(panelURL, agent, roles, runtime, image)
	res.Compose = dockerComposeSnippet(panelURL, agent, roles, runtime, image)
	return res
}

// agentImage 生成 Docker 安装命令使用的 Agent 镜像，可经 -agent-image 覆盖
func (h *InstallHandler) agentImage() string {
	if h.cfg != nil && strings.TrimSpace(h.cfg.AgentImage) != "" {
		return strings.TrimSpace(h.cfg.AgentImage)
	}
	return config.DefaultAgentImage
}

func binaryInstallCommand(panelURL string, agent *model.Agent, osName, roles, runtime string) string {
	switch {
	case strings.HasPrefix(osName, "win"):
		return fmt.Sprintf("powershell -ExecutionPolicy Bypass -Command \"irm %s/install.ps1 -OutFile install.ps1; .\\install.ps1 -Panel %s -NodeKey %s -NodeSecret %s -Roles %s -Runtime %s\"",
			panelURL, panelURL, agent.NodeKey, agent.Secret, roles, runtime)
	default:
		args := fmt.Sprintf("--panel %s --node-key %s --secret %s --roles %s --runtime %s",
			panelURL, agent.NodeKey, agent.Secret, roles, runtime)
		return fmt.Sprintf("curl -fsSL %s/install.sh | sudo bash -s -- %s", panelURL, args)
	}
}

// agentDataHostDir docker 运行时的数据目录：必须落在宿主机上。
// frp 运行时要把容器里的配置与 frp 二进制 bind-mount 进 frp 容器，宿主机的 docker daemon
// 得能直接看到这些文件；命名卷实际在 /var/lib/docker/volumes 下，路径对不上，挂不进去。
const agentDataHostDir = "/opt/dfpanel-agent"

func dockerRunCommand(panelURL string, agent *model.Agent, roles, runtime, image string) string {
	lines := []string{
		"docker run -d --name dfpanel-agent --restart unless-stopped \\",
		fmt.Sprintf("  -e DFPANEL_URL=%s \\", panelURL),
		fmt.Sprintf("  -e DFPANEL_NODE_KEY=%s \\", agent.NodeKey),
		fmt.Sprintf("  -e DFPANEL_NODE_SECRET=%s \\", agent.Secret),
		fmt.Sprintf("  -e DFPANEL_ROLES=%s \\", roles),
		fmt.Sprintf("  -e DFPANEL_RUNTIME=%s \\", runtime),
	}
	if runtime == "docker" {
		lines = append(lines,
			"  -v "+agentDataHostDir+":/var/lib/dfpanel-agent \\",
			"  -v /var/run/docker.sock:/var/run/docker.sock \\")
	} else {
		lines = append(lines, "  -v dfpanel-agent-data:/var/lib/dfpanel-agent \\")
	}
	lines = append(lines,
		"  --network host \\",
		"  "+image,
	)
	return strings.Join(lines, "\n")
}

func dockerComposeSnippet(panelURL string, agent *model.Agent, roles, runtime, image string) string {
	volumes := "      - dfpanel-agent-data:/var/lib/dfpanel-agent\n"
	tail := "\nvolumes:\n  dfpanel-agent-data:\n"
	if runtime == "docker" {
		// 数据目录用宿主机路径，理由同 agentDataHostDir
		volumes = "      - " + agentDataHostDir + ":/var/lib/dfpanel-agent\n" +
			"      - /var/run/docker.sock:/var/run/docker.sock\n"
		tail = ""
	}
	return fmt.Sprintf(`services:
  dfpanel-agent:
    image: %s
    container_name: dfpanel-agent
    restart: unless-stopped
    network_mode: host
    environment:
      DFPANEL_URL: %s
      DFPANEL_NODE_KEY: %s
      DFPANEL_NODE_SECRET: %s
      DFPANEL_ROLES: %s
      DFPANEL_RUNTIME: %s
    volumes:
%s%s`, image, panelURL, agent.NodeKey, agent.Secret, roles, runtime, volumes, tail)
}
