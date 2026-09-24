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
// 只发面板缓存里已有的那一份：上游抓取一律在设置页由管理员触发（可配镜像源、有结果反馈），
// Agent 侧不做「现抓上游」，缺什么就直接说缺什么，别让它在一次启动里悬着等十几分钟。
//
// version 可用 latest：优先取本地已缓存的最新版本，本地一份都没有才去问 GitHub。
// 具体版本号通过 X-Frp-Version 响应头回传，便于 Agent 侧以确定版本号记账。
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

	binDir := h.binDir()
	if version == distrib.LatestTag {
		cached := distrib.CachedVersions(binDir, kind, osName, arch)
		if len(cached) > 0 {
			version = cached[0]
		} else if latest, err := distrib.LatestVersion(); err == nil {
			version = latest
		} else {
			c.JSON(http.StatusBadGateway, gin.H{"error": "解析最新版本失败：" + err.Error()})
			return
		}
	}

	name := distrib.BinaryName(kind, version, osName, arch)
	path := filepath.Join(binDir, name)
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "面板没有 " + kind + " " + version + "（" + osName + "/" + arch + "）的二进制，" +
				"请先在面板「设置 → frp 二进制」里下载",
		})
		return
	}

	c.Header("X-Frp-Version", version)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", name))
	c.File(path)
}

// binDir 面板存放 frp 二进制的目录，与设置页的缓存清单、面板本机 frps 用的是同一个
func (h *InstallHandler) binDir() string {
	return filepath.Join(h.cfg.DataDir, "bin")
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

// frpVersionArg 面板记录的期望 frp 版本，写进安装命令/环境变量。
// 为空（不管理）或格式不合法时返回空：此时 Agent 首次启动会去找面板的 latest。
func frpVersionArg(agent *model.Agent) string {
	v := strings.TrimSpace(agent.FRPVersion)
	if !distrib.ValidVersion(v) {
		return ""
	}
	return v
}

func binaryInstallCommand(panelURL string, agent *model.Agent, osName, roles, runtime string) string {
	ver := frpVersionArg(agent)
	switch {
	case strings.HasPrefix(osName, "win"):
		args := fmt.Sprintf("-Panel %s -NodeKey %s -NodeSecret %s -Roles %s -Runtime %s",
			panelURL, agent.NodeKey, agent.Secret, roles, runtime)
		if ver != "" {
			args += " -FrpVersion " + ver
		}
		return fmt.Sprintf("powershell -ExecutionPolicy Bypass -Command \"irm %s/install.ps1 -OutFile install.ps1; .\\install.ps1 %s\"",
			panelURL, args)
	default:
		args := fmt.Sprintf("--panel %s --node-key %s --secret %s --roles %s --runtime %s",
			panelURL, agent.NodeKey, agent.Secret, roles, runtime)
		if ver != "" {
			args += " --frp-version " + ver
		}
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
	// 期望版本一并带过去：Agent 首次启动按它去面板取二进制，而不是自己挑 latest
	if ver := frpVersionArg(agent); ver != "" {
		lines = append(lines, fmt.Sprintf("  -e DFPANEL_FRPVERSION=%s \\", ver))
	}
	if runtime == "docker" {
		lines = append(lines,
			// Agent 在容器里、frp 容器由宿主创建：告诉它挂载路径该用宿主上的哪一份
			"  -e DFPANEL_HOST_DATA_DIR="+agentDataHostDir+" \\",
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
	versionLine := ""
	if ver := frpVersionArg(agent); ver != "" {
		versionLine = "      DFPANEL_FRPVERSION: " + ver + "\n"
	}
	if runtime == "docker" {
		// Agent 在容器里、frp 容器由宿主创建：挂载路径要用宿主上的那一份
		versionLine += "      DFPANEL_HOST_DATA_DIR: " + agentDataHostDir + "\n"
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
%s    volumes:
%s%s`, image, panelURL, agent.NodeKey, agent.Secret, roles, runtime, versionLine, volumes, tail)
}
