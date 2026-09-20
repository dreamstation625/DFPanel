package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/distrib"
	"dfpanel/internal/model"
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
	c.JSON(http.StatusOK, buildInstallCommands(h.panelURL(c), &agent, osName, runtime))
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
	c.JSON(http.StatusOK, buildInstallCommands(h.panelURL(c), &agent, osName, runtime))
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
// 本地缓存缺失时自动从 frp 官方 release 下载并缓存（version 可用 latest）
func (h *InstallHandler) DownloadFRP(c *gin.Context) {
	kind := strings.ToLower(c.Param("kind"))
	if kind != "frps" && kind != "frpc" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "类型只能是 frps / frpc"})
		return
	}
	version := c.Param("version")
	if version == "" {
		version = "latest"
	}
	osName := strings.ToLower(c.Param("os"))
	arch := strings.ToLower(c.Param("arch"))

	path, err := distrib.EnsureFRPBinary(kind, version, osName, arch, filepath.Join(h.cfg.DataDir, "bin"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "获取 " + kind + " 二进制失败：" + err.Error()})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(path)))
	c.File(path)
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
func buildInstallCommands(panelURL string, agent *model.Agent, osName, runtime string) installCommands {
	roles := strings.TrimSpace(agent.Roles)
	if roles == "" {
		roles = "frpc"
	}
	if runtime != "docker" {
		runtime = "process"
	}

	res := installCommands{
		PanelURL: panelURL,
		NodeKey:  agent.NodeKey,
		Secret:   agent.Secret,
		Roles:    roles,
	}
	res.Binary = binaryInstallCommand(panelURL, agent, osName, roles, runtime)
	res.Docker = dockerRunCommand(panelURL, agent, roles, runtime)
	res.Compose = dockerComposeSnippet(panelURL, agent, roles, runtime)
	return res
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

func dockerRunCommand(panelURL string, agent *model.Agent, roles, runtime string) string {
	lines := []string{
		"docker run -d --name dfpanel-agent --restart unless-stopped \\",
		fmt.Sprintf("  -e DFPANEL_URL=%s \\", panelURL),
		fmt.Sprintf("  -e DFPANEL_NODE_KEY=%s \\", agent.NodeKey),
		fmt.Sprintf("  -e DFPANEL_NODE_SECRET=%s \\", agent.Secret),
		fmt.Sprintf("  -e DFPANEL_ROLES=%s \\", roles),
		fmt.Sprintf("  -e DFPANEL_RUNTIME=%s \\", runtime),
		"  -v dfpanel-agent-data:/var/lib/dfpanel-agent \\",
	}
	if runtime == "docker" {
		lines = append(lines, "  -v /var/run/docker.sock:/var/run/docker.sock \\")
	}
	lines = append(lines,
		"  --network host \\",
		"  dfpanel/agent:latest",
	)
	return strings.Join(lines, "\n")
}

func dockerComposeSnippet(panelURL string, agent *model.Agent, roles, runtime string) string {
	volumes := "    volumes:\n      - dfpanel-agent-data:/var/lib/dfpanel-agent\n"
	if runtime == "docker" {
		volumes += "      - /var/run/docker.sock:/var/run/docker.sock\n"
	}
	return fmt.Sprintf(`services:
  dfpanel-agent:
    image: dfpanel/agent:latest
    container_name: dfpanel-agent
    restart: unless-stopped
    network_mode: host
    environment:
      DFPANEL_URL: %s
      DFPANEL_NODE_KEY: %s
      DFPANEL_NODE_SECRET: %s
      DFPANEL_ROLES: %s
      DFPANEL_RUNTIME: %s
%svolumes:
  dfpanel-agent-data:`, panelURL, agent.NodeKey, agent.Secret, roles, runtime, volumes)
}
