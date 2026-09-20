# DFPanel

frp 的可视化控制面板。面板与 frps / frpc 分离部署，通过 Agent 统一托管服务端与客户端。

配置字段对应 frp 官方文档（https://gofrp.org/zh-cn/docs/），菜单里有官方文档入口，配置项旁的 `?` 角标可悬停查看说明、点击跳转对应文档小节。

## 功能

### 服务端（frps）

- frps 配置可视化：监听地址与端口、KCP / QUIC、虚拟主机、tcpmux、认证（token / OIDC）、Dashboard、传输层与 TLS、端口池限制、SSH 隧道网关、日志
- 多服务端配置：新建 / 下拉切换 / 删除；新建时自动挑选同机空闲端口，并校验端口冲突
- 两种部署模式：
  - `local`：面板本机托管，由面板直接拉起 frps
  - `agent`：远端 Agent 托管，面板只生成配置并下发
- 启动 / 停止 / 重启、frps.json 预览、日志查看

### Agent

- 同一个 Agent 进程可同时托管多个 frps 与 frpc（`roles=frps,frpc`）
- Agent 主动反连面板（WebSocket），可部署在内网 / NAT 之后的机器
- 断线自动重连，WebSocket 不可用时降级为 HTTP 轮询
- Agent 离线期间的指令会入队，上线后自动补发执行
- 两种运行时：`process` 直接拉起 frp 子进程；`docker` 用容器运行 frps / frpc
- Agent 重启后扫描本地配置，把之前托管的实例重新拉起

### 客户端节点（frpc）

- 节点管理：绑定托管 Agent、关联 frps、连接地址与端口、认证 Token、传输协议、TLS、用户标识
- 隧道管理：tcp / udp / http / https / stcp / sudp / xtcp，支持远端端口、自定义域名、子域名、分组名、加密与压缩
- 配置下发、启停重启、frpc.json 预览、日志查看

### 配置回滚

- 每次下发都会备份上一版配置，并在 Agent 侧生成历史快照（`history/frps-1.v3.json`，默认保留最近 30 份，成功失败都保留）
- 健康判定分三种结果，只在明确故障时回滚：

  | 结果 | 条件 | 动作 |
  |---|---|---|
  | 可用 | 命中成功关键字且端口探测通过，或稳定超过观察窗口且探测通过 | 上报 `applied` |
  | 明确故障 | `address already in use`、`login to server failed`、`connect to server error`、`token in login doesn't match`、`authentication failed`、`connection refused` 等，或进程直接退出 | 回滚上一版并重新拉起 |
  | 未确认 | 超时未连通，但进程仍在运行且日志无明确报错 | 不回滚，保留新配置并提示人工确认 |

- 面板可查看历史版本（时间、大小、状态、是否当前生效），并回滚到指定版本；回滚同样走校验与健康探测流程
- 下发前的语法校验（`frp verify`）不通过时，不影响正在运行的服务

### 面板

- 初始化向导、JWT 登录、概览统计（服务端 / Agent / 节点）
- Agent 管理：列表与在线状态、创建与角色分配、安装命令（Linux / Windows，进程 / Docker，脚本 / docker run / compose）、重置令牌、指令历史
- 菜单内置 frp 官方文档

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

- 面板只保存配置、下发指令，可与 frps 不同机
- Agent 反连面板，目标机器无需公网入口
- 面板与 Agent 之间用 `HMAC-SHA256(secret, "nodeKey.ts")` 签名鉴权，带时间戳校验

## 快速开始

### 1. 构建

Windows：

```powershell
.\build.ps1                       # 前端 + 后端 -> dfpanel.exe
.\build.ps1 -SkipFrontend         # 只编译后端（复用现有 web/dist）
.\build.ps1 -Agent                # 编译 Agent -> dist/dfpanel-agent-<os>-<arch>
.\build.ps1 -Agent -AllPlatforms  # 一次产出 linux/windows x amd64/arm64
.\build.ps1 -Docker               # 构建面板镜像 dfpanel/panel:latest
.\build.ps1 -Docker -Agent        # 构建 Agent 镜像 dfpanel/agent:latest
```

Linux / macOS 用 `./build.sh`，参数等价（`--skip-frontend --agent --all-platforms --docker --image`）。

### 2. 启动面板

```bash
./dfpanel -listen :8080 -data ./data -public-url http://1.2.3.4:8080
```

| 参数 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `-listen` | `DFPANEL_LISTEN` | `:8080` | 监听地址 |
| `-data` | `DFPANEL_DATA_DIR` | `./data` | 数据目录（SQLite、配置、日志） |
| `-public-url` | `DFPANEL_PUBLIC_URL` | 按请求推断 | 面板对外地址，用于生成安装命令与 frpc 连接地址 |
| `-jwt-secret` | `DFPANEL_JWT_SECRET` | 自动生成并持久化 | JWT 签名密钥 |
| `-token-expire` | — | `24` | 登录有效期（小时） |

首次访问进入初始化向导，设置管理员账号后登录使用。

### 3. 添加 Agent

在「Agent 管理」新建 Agent（勾选 frps / frpc 角色），复制面板生成的安装命令到目标服务器执行。

Docker：

```bash
docker run -d --name dfpanel-agent --restart unless-stopped \
  -e DFPANEL_URL=http://<panel>:8080 \
  -e DFPANEL_NODE_KEY=<nodeKey> \
  -e DFPANEL_NODE_SECRET=<secret> \
  -e DFPANEL_ROLES=frps,frpc \
  -e DFPANEL_RUNTIME=process \
  -v dfpanel-agent-data:/var/lib/dfpanel-agent \
  --network host \
  dfpanel/agent:latest
```

二进制（面板会生成带令牌的完整命令）：

```bash
curl -fsSL http://<panel>:8080/install.sh | sudo bash -s -- \
  --panel http://<panel>:8080 --node-key <KEY> --secret <SECRET> --roles frps,frpc
```

### 4. Docker Compose

```bash
docker compose up -d                              # 面板（含内置 frps）
docker compose -f docker-compose.agent.yml up -d  # Agent
```

面板数据放在 `./data`；Linux 推荐 `network_mode: host`，Windows / macOS 改用 `ports` 映射（见 `docker-compose.yml` 注释）。

## 目录结构

```
cmd/agent/                  Agent 程序入口
internal/
  agent/                    Agent 实现：通信、运行时（进程 / 容器）、配置应用、健康判定、历史快照
  agenthub/                 面板侧 WebSocket 连接管理与指令下发
  proto/                    面板与 Agent 共用协议（零依赖，Agent 不引入 gorm/sqlite）
  frp/                      frps / frpc 配置生成与本地进程管理
  handler/                  HTTP 接口（服务端、节点、隧道、Agent、安装分发）
  distrib/                  安装脚本内嵌与 frp 二进制按需下载
  router/  middleware/  model/  database/  config/
web/                        Vue 3 + Element Plus 前端（vite 构建，产物嵌入二进制）
docs/                       架构与部署文档
Dockerfile  Dockerfile.agent  docker-compose*.yml
build.ps1  build.sh
```

## 相关文档

- [docs/agent-架构与部署.md](docs/agent-架构与部署.md)：分离式部署、Agent 双角色与运行时、回滚判定、历史版本、Docker 部署、接口一览

## 灵感与参考

- 灵感来源：[VaalaCat/frp-panel](https://github.com/VaalaCat/frp-panel)
- 底层项目：[fatedier/frp](https://github.com/fatedier/frp)
- 官方文档：[gofrp.org](https://gofrp.org/zh-cn/docs/)

## 支持

本项目由 **WorkBuddy** 大力支持。
