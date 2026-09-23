package router

import (
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"dfpanel/internal/agenthub"
	"dfpanel/internal/config"
	"dfpanel/internal/frp"
	"dfpanel/internal/handler"
	"dfpanel/internal/middleware"
	"dfpanel/web"
)

// Setup 组装路由并返回 gin 引擎
func Setup(cfg *config.Config) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	mgr := frp.NewManager(filepath.Join(cfg.DataDir, "bin"), cfg.DataDir)
	hub := agenthub.New()

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	authHandler := handler.NewAuthHandler(cfg)
	serverHandler := handler.NewServerHandler(mgr, hub)
	// 面板启动时把「开了自动启动、且没被手动停过」的本机 frps 拉起来（异步，不拖慢启动）
	go serverHandler.StartAutoServers()
	agentHandler := handler.NewAgentHandler(hub)
	agentManage := handler.NewAgentManageHandler(hub)
	nodeHandler := handler.NewNodeHandler(hub, cfg)
	proxyHandler := handler.NewProxyHandler()
	visitorHandler := handler.NewVisitorHandler()
	installHandler := handler.NewInstallHandler(cfg)
	frpHandler := handler.NewFrpHandler(cfg, mgr, hub)

	api := r.Group("/api")
	{
		api.GET("/init-status", authHandler.InitStatus)
		api.POST("/init", authHandler.Init)
		api.POST("/auth/login", authHandler.Login)

		// Agent 面：签名鉴权（nodeKey + HMAC），不使用 JWT
		agentAPI := api.Group("/agent")
		{
			agentAPI.POST("/register", agentHandler.Register)
			agentAPI.POST("/heartbeat", agentHandler.Heartbeat)
			// 拉取指令会把指令标记为已下发，属于写操作，因此用 POST
			agentAPI.POST("/commands", agentHandler.Commands)
			// 兼容早期版本 Agent 的 GET 轮询（新版 Agent 使用 POST）
			agentAPI.GET("/commands", agentHandler.Commands)
			agentAPI.POST("/report", agentHandler.Report)
			agentAPI.GET("/ws", agentHandler.WS)
		}

		authed := api.Group("")
		authed.Use(middleware.JWT(cfg.JWTSecret))
		{
			authed.GET("/auth/profile", authHandler.Profile)

			// 写操作统一使用 POST（兼容性优先，不使用 PUT / DELETE），读操作使用 GET
			authed.GET("/servers", serverHandler.List)
			authed.POST("/servers", serverHandler.Create)
			authed.GET("/servers/:id", serverHandler.Get)
			authed.POST("/servers/:id/update", serverHandler.Update)
			authed.POST("/servers/:id/delete", serverHandler.Delete)
			authed.GET("/servers/:id/config", serverHandler.Preview)
			authed.POST("/servers/:id/apply", serverHandler.Apply)
			authed.POST("/servers/:id/start", serverHandler.Start)
			authed.POST("/servers/:id/stop", serverHandler.Stop)
			authed.POST("/servers/:id/restart", serverHandler.Restart)
			authed.GET("/servers/:id/log", serverHandler.Log)
			authed.GET("/servers/:id/versions", serverHandler.Versions)
			authed.POST("/servers/:id/rollback", serverHandler.Rollback)

			// Agent 管理
			authed.GET("/agents", agentManage.List)
			authed.POST("/agents", agentManage.Create)
			authed.GET("/agents/:id", agentManage.Get)
			authed.POST("/agents/:id/update", agentManage.Update)
			authed.POST("/agents/:id/delete", agentManage.Delete)
			authed.POST("/agents/:id/token/reset", agentManage.ResetToken)
			authed.GET("/agents/:id/commands", agentManage.Commands)
			authed.GET("/agents/:id/install-command", installHandler.AgentInstallCommand)

			// 节点（frpc）管理
			authed.GET("/nodes", nodeHandler.List)
			authed.POST("/nodes", nodeHandler.Create)
			authed.GET("/nodes/:id", nodeHandler.Get)
			authed.POST("/nodes/:id/update", nodeHandler.Update)
			authed.POST("/nodes/:id/delete", nodeHandler.Delete)
			authed.GET("/nodes/:id/config", nodeHandler.Preview)
			authed.POST("/nodes/:id/apply", nodeHandler.Apply)
			authed.POST("/nodes/:id/start", nodeHandler.Start)
			authed.POST("/nodes/:id/stop", nodeHandler.Stop)
			authed.POST("/nodes/:id/restart", nodeHandler.Restart)
			authed.GET("/nodes/:id/log", nodeHandler.Log)
			authed.GET("/nodes/:id/install-command", installHandler.NodeInstallCommand)
			authed.GET("/nodes/:id/versions", nodeHandler.Versions)
			authed.POST("/nodes/:id/rollback", nodeHandler.Rollback)

			// 隧道（frpc 代理）
			authed.GET("/nodes/:id/proxies", proxyHandler.List)
			authed.POST("/nodes/:id/proxies", proxyHandler.Create)
			authed.POST("/proxies/:id/update", proxyHandler.Update)
			authed.POST("/proxies/:id/delete", proxyHandler.Delete)

			// 访问端（点对点隧道的访问侧）
			authed.GET("/nodes/:id/visitors", visitorHandler.List)
			authed.POST("/nodes/:id/visitors", visitorHandler.Create)
			authed.POST("/visitors/:id/update", visitorHandler.Update)
			authed.POST("/visitors/:id/delete", visitorHandler.Delete)

			// 配置版本历史（回滚依据）
			authed.GET("/config-versions", agentManage.Versions)

			// frp 版本管理与系统设置
			authed.GET("/settings", frpHandler.GetSettings)
			authed.POST("/settings", frpHandler.SaveSettings)
			authed.GET("/frp-versions", frpHandler.ListVersions)
			authed.GET("/frp/local", frpHandler.LocalFrp)
			authed.POST("/frp/local/download", frpHandler.LocalFrpDownload)
			authed.POST("/frp/local/activate", frpHandler.LocalFrpActivate)
			authed.GET("/agents/:id/frp", frpHandler.AgentFrp)
			authed.POST("/agents/:id/frp/download", frpHandler.AgentFrpDownload)
			authed.POST("/agents/:id/frp/activate", frpHandler.AgentFrpActivate)
		}
	}

	// 分发端点：安装脚本与二进制
	r.GET("/install.sh", installHandler.ScriptSh)
	r.GET("/install.ps1", installHandler.ScriptPs1)
	r.GET("/downloads/agent/:os/:arch", installHandler.DownloadAgent)
	r.GET("/downloads/:kind/:version/:os/:arch", installHandler.DownloadFRP)

	setupStatic(r)
	return r
}

// setupStatic 提供内嵌的前端静态资源，并支持 SPA 路由回退
func setupStatic(r *gin.Engine) {
	distFS, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		log.Printf("前端资源不可用: %v", err)
		return
	}
	fileServer := http.FileServer(http.FS(distFS))

	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "接口不存在"})
			return
		}

		// 命中真实静态文件则直接返回
		if p := strings.TrimPrefix(c.Request.URL.Path, "/"); p != "" {
			if f, err := distFS.Open(p); err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}

		// 否则回退到 index.html（前端路由接管）
		index, err := fs.ReadFile(distFS, "index.html")
		if err != nil {
			c.String(http.StatusNotFound, "前端资源未构建")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
}
