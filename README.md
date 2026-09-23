# DFPanel

frp 的可视化控制面板。面板与 frps / frpc 分离部署，通过 Agent 统一托管服务端与客户端。

后端 Go，前端 Vue 3，打成一个二进制。

## 功能

- **服务端** —— frps 的各项参数都在界面上改，能建多个，新建时自动挑同机空闲端口
- **Agent** —— 一个进程同时托管多个 frps 和 frpc，进程 / 容器两种运行时，重启后自动恢复
- **节点与隧道** —— tcp / udp / http / https / stcp / sudp / xtcp，含访问端配置，能下发、启停、看日志
- **frp 版本** —— 二进制由面板统一下发，按实例切版本，下载源可换镜像
- **下发与回滚** —— 下发前先校验，出明确故障自动回滚上一版，也能看历史版本手动回滚

## 架构

```
        ┌──────────────── 面板（dfpanel） ────────────────┐
        │  Web UI   REST API   AgentHub(WebSocket Server) │
        │  SQLite   配置版本库   指令队列                 │
        └───────────────▲────────────────────────────────┘
                        │ Agent 主动反连（面板不主动外连）
         ┌──────────────┴───────────┐        ┌────────────────────┐
         │  Agent（服务器 A）        │        │  Agent（服务器 B）   │
         │  roles = frps,frpc       │        │  roles = frpc       │
         │  ├ frps-1 (进程 / 容器)   │        │  └ frpc-7 (进程/容器)│
         │  └ frpc-3 (进程 / 容器)   │        └────────────────────┘
         └──────────────────────────┘
```

面板只存配置和下发指令，跟 frps 不在一台机器上也行。

## 快速开始

```powershell
.\build.ps1                       # 前端 + 后端 -> dfpanel.exe
.\build.ps1 -SkipFrontend         # 只编译后端（复用现有 web/dist）
.\build.ps1 -Agent                # 编译 Agent -> dist/dfpanel-agent-<os>-<arch>
.\build.ps1 -Agent -AllPlatforms  # 一次产出 linux/windows/darwin 共 4 个平台
.\build.ps1 -Docker [-Agent]      # 构建面板 / Agent 镜像
```

Linux / macOS 用 `./build.sh`，参数等价。改完前端依赖要用普通 `npm install` 重新生成 `web/package-lock.json`，否则容器里 `vite build` 会因缺平台原生包失败。

启动面板：

```bash
# 二进制
./dfpanel -listen :7226 -data ./data -public-url http://1.2.3.4:7226

# Docker
docker run -d --name dfpanel --restart unless-stopped \
  -e DFPANEL_PUBLIC_URL=http://1.2.3.4:7226 \
  -v dfpanel-data:/data \
  --network host \
  dreamstation625/dfpanel:latest

# Compose（面板 + 内置 frps，数据在 ./data）
docker compose up -d
```

`--network host` 时不用映射端口，frps 的 7000、dashboard 7500、vhost 80/443 直接生效；非 Linux 环境改成 `-p 7226:7226 -p 7000:7000 -p 7500:7500`（见 `docker-compose.yml` 注释）。

| 参数 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `-listen` | `DFPANEL_LISTEN` | `:7226` | 监听地址 |
| `-data` | `DFPANEL_DATA_DIR` | `./data` | 数据目录 |
| `-public-url` | `DFPANEL_PUBLIC_URL` | 按请求推断 | 面板对外地址，用于生成安装命令和 frpc 连接地址 |
| `-agent-image` | `DFPANEL_AGENT_IMAGE` | `dreamstation625/dfpanel-agent:latest` | Docker 安装命令用的 Agent 镜像 |
| `-jwt-secret` | `DFPANEL_JWT_SECRET` | 自动生成并持久化 | JWT 签名密钥 |
| `-token-expire` | — | `24` | 登录有效期（小时） |

第一次访问进初始化向导，设好管理员账号就能登录。

装 Agent：在「Agent 管理」新建一个（勾上 frps / frpc 角色），面板会给一条安装命令，复制到目标机器上跑。

```bash
# Docker
docker run -d --name dfpanel-agent --restart unless-stopped \
  -e DFPANEL_URL=http://<panel>:7226 \
  -e DFPANEL_NODE_KEY=<nodeKey> \
  -e DFPANEL_NODE_SECRET=<secret> \
  -e DFPANEL_ROLES=frps,frpc \
  -e DFPANEL_RUNTIME=process \
  -v dfpanel-agent-data:/var/lib/dfpanel-agent \
  --network host \
  dreamstation625/dfpanel-agent:latest

# 二进制
curl -fsSL http://<panel>:7226/install.sh | sudo bash -s -- \
  --panel http://<panel>:7226 --node-key <KEY> --secret <SECRET> --roles frps,frpc

# Compose（先把 docker-compose.agent.yml 里的地址和令牌填上）
docker compose -f docker-compose.agent.yml up -d
```

## 目录结构

```
cmd/agent/                        Agent 入口
internal/                         Agent 实现、面板后端、frp 配置与版本管理、HTTP 接口
web/                              Vue 3 前端（产物嵌入二进制）
docs/                             架构与部署文档
Dockerfile*  docker-compose*.yml  build.ps1  build.sh
.github/workflows/                CI：镜像构建、release 二进制
```

## 文档

- [docs/agent-架构与部署.md](docs/agent-架构与部署.md)：分离部署、Agent 运行时、回滚判定、Docker 部署、接口一览
- [docs/frp-版本管理方案.md](docs/frp-版本管理方案.md)：版本化存储、下载与激活流程

## 参考

- [VaalaCat/frp-panel](https://github.com/VaalaCat/frp-panel)（灵感来源）
- [fatedier/frp](https://github.com/fatedier/frp) ｜ [gofrp.org](https://gofrp.org/zh-cn/docs/)（官方文档）

## 支持

本项目由 **WorkBuddy** 大力支持。
