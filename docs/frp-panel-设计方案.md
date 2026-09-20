# FRP Panel 系统设计方案

> 版本：v0.2（技术选型已确认）
> 日期：2026-09-20
> 状态：技术选型已确认，进入 M1 开发

---

## 一、需求理解

### 1.1 系统目标

做一个 **frp 的可视化统一控制台**：用户在中心服务器（或 Docker）部署后，通过浏览器即可完成 frps 服务端的配置、frpc 客户端的创建与配置、客户端的一键安装，客户端安装后能自动初始化并连接回中心。

### 1.2 核心业务闭环

```
① 部署           ② Web 配置 frps      ③ 创建 & 配置客户端
中心服务器 / Docker ▶ 浏览器访问 :8080 ▶ 建节点 + 建隧道
                     设置 frps 服务端     生成安装令牌
                                            │
                                            ▼
                                      ④ 生成安装指令
                                     （二进制 / Docker）
                                            │
   ⑥ Web 运维                           ⑤ 客户端一键安装
   状态/流量/增删 ◀──── 面板 ◀────── Agent 自动注册
                                      ─▶ 拉取 frpc 配置
                                      ─▶ 启动 frpc 连回 frps
```

### 1.3 关键要素拆解

| 需求原话 | 设计含义 |
|---|---|
| 中心服务器部署或 Docker 部署 | 面板支持二进制 + Docker 两种交付，单文件/单镜像 |
| 监听端口，浏览器访问可设置 frps 服务端 | 面板自带 Web UI + 配置管理 frps，并可选托管 frps 进程 |
| 创建客户端、配置客户端 | 节点管理 + 节点下的隧道/代理配置 |
| 生成客户端安装指令（二进制或 Docker） | 面板按节点动态生成一键安装命令 |
| 客户端自动初始化 frpc 并获取 frps 配置 | 客户端侧 Agent 自动注册、拉取配置、启动并守护 frpc |

---

## 二、总体架构

```
                            ┌──────────────────────────────────────────────┐
   浏览器 ──HTTP(S)───────▶ │                面板中心 Panel Core           │
                            │  ┌────────────────────────────────────────┐  │
                            │  │ Web UI（前端构建产物 embed 进二进制）   │  │
                            │  │ REST API（管理面，JWT 鉴权）            │  │
                            │  │ Agent Hub（WebSocket 长连接，反连接入） │  │
                            │  │ frps 配置生成 / 校验 / 进程管理         │  │
                            │  │ frpc 配置生成 / 校验 / 版本回滚         │  │
                            │  │ 端口池分配 / 冲突检测                   │  │
                            │  │ 监控聚合（读 frps Admin API）           │  │
                            │  │ 存储（SQLite 默认 / MySQL·PG 可选）     │  │
                            │  └────────────────────────────────────────┘  │
                            └───────┬───────────────────────┬──────────────┘
                                    │                       │
                    (查询聚合) frps Admin API         WebSocket（Agent 主动反连）
                                    │                       │
                            ┌───────▼───────┐      ┌────────▼─────────────────┐
                            │    frps       │      │   节点 Agent（每台机器） │
                            │ bindPort 7000 │◀──── │  · 注册 / 心跳            │
                            │ dashboard     │      │  · 拉取 frpc 配置         │
                            │ vhost 80/443  │      │  · 写快照 + 启停 frpc     │
                            └───────────────┘      │  · 进程守护（service）    │
                                      ▲            └────────┬─────────────────┘
                                      │ 7000                │ 127.0.0.1:7400
                                      └─────────────────────┤（本机回环）
                                                            ▼
                                                          frpc
```

### 2.1 组件职责

| 组件 | 职责 |
|---|---|
| **Panel Core** | 面板后端：配置管理、下发、监控、鉴权、静态资源托管 |
| **frps** | frp 服务端，负责接受 frpc 连接与端口映射 |
| **Agent** | 客户端侧守护进程，自动注册、拉配置、启停与守护 frpc |
| **frpc** | frp 客户端，运行在目标机器，连回 frps |

### 2.2 两种部署拓扑

| 拓扑 | 说明 | 适用场景 |
|---|---|---|
| **一体化**（✅ 本项目已确认采用） | 面板与 frps 同机/同容器，面板生成 `frps.json` 并管理 frps 进程 | 自建穿透服务，开箱即用 |
| **分离式**（后续可扩展） | 面板只做配置生成与导出，frps 独立部署 | 已有 frps / 需独立运维 |

> **已确认**：本项目采用 **一体化** 模式——面板负责生成 `frps.json` 并托管 frps 进程的启动/停止/重启/日志。

---

## 三、功能模块

### 3.1 服务端（frps）管理

- **表单化配置**：`bindAddr` / `bindPort`、`kcpBindPort`、`quicBindPort`、`vhostHTTPPort`、`vhostHTTPSPort`、`subDomainHost`、`allowPorts`、`maxPortsPerClient`、`auth.method` / `auth.token`、`transport.tcpMux` / `transport.tls`、`webServer`（dashboard）、`log.*`、`enablePrometheus`。
- **配置产物**：`frps.json` 实时预览、语法校验、保存、导出下载。
- **进程管理**（一体化模式）：启动 / 停止 / 重启 / 状态 / 日志查看 / 端口占用预检。
- **Dashboard 对接**：配置 dashboard 账号密码，供面板读取客户端与流量。

### 3.2 客户端（节点）管理

- **创建节点**：名称、分组、备注、关联 frps、公共参数（`serverAddr` / `serverPort` / `auth.token` / `transport.tls.enable` / `transport.protocol` / `user`）。
- **安装令牌**：生成 `nodeKey` + `secret`，支持重置与有效期。
- **节点列表**：在线状态、版本、内网 IP、最后心跳、代理数量、所属分组。
- **远程操作**：重启 frpc、升级 Agent、卸载。

### 3.3 隧道 / 代理管理

- **支持类型**：`tcp` / `udp` / `http` / `https` / `stcp` / `sudp` / `xtcp`。
- **配置字段**：`name`、`localIP`、`localPort`、`remotePort`（由端口池分配）、`customDomains`、`subdomain`、`useEncryption`、`useCompression`、`group`、`healthCheck.*`。
- **端口池**：全局 `remotePort` 统一分配、冲突检测、释放回收（面板核心价值）。
- **校验**：发布前使用 `frpc verify -c <file> --strict_config` 校验。
- **版本历史 / 回滚**：每次变更落库留痕，支持一键回滚。
- **模板化**：同一模板批量套用到多个节点。

### 3.4 Agent（客户端侧组件）

| 能力 | 说明 |
|---|---|
| 注册 | 用 `nodeKey` 换长期凭据，上报 OS/arch/版本 |
| 心跳 | 定时上报在线状态、内网 IP、frpc 运行状态 |
| 配置同步 | 拉取 `frpc.json`（hash 比对），有变更才写盘 |
| 生效策略 | 仅代理变更 → 调用 frpc `reload`；公共参数变更 → 重启 frpc 进程 |
| 进程守护 | systemd / nssm / WinSW / 容器 restart policy |
| 自升级 | 面板下发新版本 URL，Agent 自替换并重启 |
| 事件上报 | 日志片段、frpc 版本、异常事件 |

> Agent 与面板统一走一条 **WebSocket 长连接**（Agent 主动反连，天然穿透 NAT）；连接不可用时降级为 HTTP 定时轮询。

### 3.5 安装与分发

面板内置分发端点，脚本与二进制全部从面板获取，无需外网：

| 端点 | 作用 |
|---|---|
| `GET /install.sh` | Linux / macOS 一键安装脚本 |
| `GET /install.ps1` | Windows 一键安装脚本 |
| `GET /downloads/agent/{os}/{arch}` | Agent 二进制下载 |
| `GET /downloads/frpc/{version}/{os}/{arch}` | frpc 二进制下载（缓存 frp 官方 release） |

安装命令由面板**按节点令牌 + 部署形态动态生成**，用户复制即用。

> Docker 形态不走分发端点：`dpanel/agent` 镜像由 CI 多阶段构建发布（内置 frpc），面板生成的 `docker run` / compose 片段只带 `PANEL_URL` 与 `NODE_KEY`。

### 3.6 监控统计

- **总览**：节点总数 / 在线数、代理数、总流量、连接数。
- **数据源**：frps 的客户端列表、代理列表、流量接口；可选 Prometheus `/metrics`。
- **趋势**：按小时 / 天聚合流量、在线率。

### 3.7 系统管理

- 首次访问引导初始化管理员；JWT 会话。
- 可选：多用户 / RBAC / 节点归属 / 操作审计日志。

---

## 四、数据模型（核心表）

| 表 | 关键字段 |
|---|---|
| `users` | id, username, password_hash, role, created_at |
| `frps_servers` | id, name, bind_port, kcp_port, quic_port, token, dashboard_port/user/pwd, vhost_http_port, vhost_https_port, subdomain_host, allow_ports, log_level, extra_json, status |
| `nodes` | id, name, server_id, group_name, node_key, secret, public_params(json), os, arch, version, status, last_seen, remote_addr |
| `proxies` | id, node_id, name, type, local_ip, local_port, remote_port, custom_domains, subdomain, use_encryption, use_compression, group_name, extra_json, enabled |
| `port_pool` | id, server_id, port, node_id, proxy_id, status, allocated_at |
| `config_versions` | id, target_type, target_id, content, version, created_by, created_at |
| `traffic_stats` | id, node_id, proxy_name, date, in_bytes, out_bytes |
| `audit_logs` | id, user_id, action, target, detail, ip, created_at |

---

## 五、API 设计（骨架）

### 5.1 管理面（JWT 鉴权）

```
POST   /api/auth/login
GET    /api/auth/profile

GET|POST        /api/servers
GET|PUT|DELETE  /api/servers/{id}
GET             /api/servers/{id}/config          # 预览 frps.json
POST            /api/servers/{id}/apply           # 保存并重启 frps（一体化）

GET|POST        /api/nodes
GET|PUT|DELETE  /api/nodes/{id}
GET             /api/nodes/{id}/install-command   # ?type=binary|docker&os=linux
POST            /api/nodes/{id}/token/reset
POST            /api/nodes/{id}/restart|upgrade

GET|POST             /api/nodes/{id}/proxies
GET|PUT|DELETE       /api/nodes/{id}/proxies/{pid}
POST                 /api/nodes/{id}/proxies/{pid}/toggle

GET    /api/stats/overview
GET    /api/stats/traffic?range=24h
```

### 5.2 Agent 面（NodeToken 鉴权）

```
POST /api/agent/register      # 用 nodeKey 注册，返回凭据
POST /api/agent/heartbeat     # 状态上报
GET  /api/agent/config        # 返回 frpc.json 内容 + 版本号
POST /api/agent/report        # 版本 / 日志 / 异常
WS   /api/agent/ws            # 长连接（推送配置变更 / 请求转发）
```

---

## 六、客户端部署与自动初始化

> 客户端 Agent **同时支持两种等价部署形态**：**二进制直接部署** 与 **Docker 部署**。
> 两者共用同一套 Agent 代码与同一套面板 API，仅在「安装入口」和「进程守护方式」上不同；面板侧不区分形态，只由 `install-command` 接口按 `type=binary|docker` 生成对应指令。

### 6.0 形态总览（✅ 均已确认支持）

| 形态 | 目标场景 | Agent 载体 | frpc 供给 | 进程守护 |
|---|---|---|---|---|
| **二进制直接部署** | 物理机 / 虚拟机 / Windows 主机 | Agent 二进制 → 注册为系统服务 | 面板分发端点按需下载 | systemd / Windows Service |
| **Docker 部署** | 容器化宿主机 / NAS / 边缘设备 | `dpanel/agent` 镜像（内置 frpc） | 镜像内置 | 容器 `restart: always` + Agent 内置 supervisor |

无论哪种形态，Agent 行为链路完全一致：**注册 → 拉取 `frpc.json` → 拉起 frpc → 心跳 → 变更同步**。

### 6.1 形态一：二进制直接部署（Linux / macOS）

```bash
curl -fsSL http://panel:8080/install.sh | sudo bash -s -- \
  --panel http://panel:8080 \
  --node-key <NODE_KEY>
```

`install.sh` 步骤：

1. 识别 OS / 架构；
2. 从面板下载 `agent` 与 `frpc` 二进制；
3. 安装到 `/usr/local/bin`，配置放置 `/etc/frp/`；
4. 生成 `/etc/frp/agent.json`（含 `panel_url` + `node_key`）；
5. 写入 `/etc/frp/frpc.json` 占位（随后由 Agent 覆盖）；
6. 注册 systemd 服务（`Restart=always`）并启动。

```ini
# /etc/systemd/system/frp-agent.service
[Service]
ExecStart=/usr/local/bin/frp-agent --config /etc/frp/agent.json
Restart=always
RestartSec=5
```

Agent 启动后：

1. 读取 `agent.json` → `POST /api/agent/register`；
2. `GET /api/agent/config` 拉取 `frpc.json` → 写盘 → 启动 frpc（管理接口固定绑 `127.0.0.1:7400`）；
3. 进入心跳 + 变更同步循环。

### 6.2 形态一：二进制直接部署（Windows）

```powershell
irm http://panel:8080/install.ps1 -OutFile install.ps1
.\install.ps1 -Panel http://panel:8080 -NodeKey <NODE_KEY>
```

- 使用 **WinSW / nssm** 注册系统服务，规避 `sc.exe` 的引号转义与无工作目录问题；
- 数据目录使用 `C:\ProgramData\frp\`，避开 `Program Files` 的 UAC 写权限限制；
- `frpc.exe` 与 `agent.exe` 均从面板分发端点下载，落盘后由服务拉起。

### 6.3 形态二：Docker 部署

```bash
docker run -d --name frp-agent --restart always \
  -e PANEL_URL=http://panel:8080 \
  -e NODE_KEY=<NODE_KEY> \
  -v frp-data:/etc/frp \
  --network host \
  dpanel/agent:latest
```

```yaml
# docker-compose.yml（bridge 模式示例）
services:
  agent:
    image: dpanel/agent:latest
    container_name: frp-agent
    restart: always
    environment:
      PANEL_URL: http://panel:8080
      NODE_KEY: <NODE_KEY>
    volumes:
      - frp-data:/etc/frp        # agent.json / frpc.json 持久化
    extra_hosts:
      - "host.docker.internal:host-gateway"   # 让容器访问宿主机上的被穿透服务
volumes:
  frp-data:
```

Docker 形态的关键设计：

- **entrypoint = Agent**（PID 1），Agent 在容器内以子进程方式拉起并守护 frpc，**容器里不跑 systemd**；
- 镜像内已打包 frpc，无需运行时下载，离线可用；
- frpc 管理接口仍固定绑容器内回环 `127.0.0.1:7400`，**不向宿主机暴露任何端口**；
- 需要穿透**宿主机上**的服务时，Linux 用 `--network host` 最简单；bridge 模式下把隧道的 `localIP` 填 `host.docker.internal` 或宿主机内网 IP；
- 升级方式推荐换镜像 tag 后 `docker compose up -d`（也可沿用 Agent 自升级，但不推荐，容器应视为不可变）。

### 6.4 两种形态对比

| 维度 | 二进制直接部署 | Docker 部署 |
|---|---|---|
| 前置依赖 | 无（自带运行时） | 需 Docker 环境 |
| 覆盖平台 | Linux / Windows / macOS | 支持 Docker 的 Linux / Windows |
| 进程守护 | systemd / Windows Service | 容器 restart policy + Agent supervisor |
| 穿透宿主机服务 | 直接访问 | 需 host 网络或 `host-gateway` |
| Agent 升级 | 面板下发自升级（原子替换） | 换镜像 tag 重启容器（推荐） |
| 典型场景 | 服务器、Windows 桌面/工控机 | NAS、边缘设备、已有容器编排环境 |

---

## 七、技术选型（重点：开发语言）

### 7.1 结论（已确认 ✅）

> **确定：Go 作为唯一主语言（后端 + Agent），前端使用 Vue3 + TypeScript + Element Plus。**

### 7.2 推荐理由

1. **与 frp 同源**：frp 本身用 Go 编写，可直接复用/参考其配置包来生成与**严格校验** `frps.json` / `frpc.json`，从源头避免手写模板导致的字段错误与版本漂移。
2. **交付极简**：前端 `vite build` 后用 `go:embed` 打进二进制 → **单文件分发**；Docker 多阶段构建镜像可控制在 **30MB 以内**。
3. **Agent 必须跨平台单二进制**：Go 一套代码交叉编译 `linux/amd64|arm64`、`windows/amd64`、`darwin`；用 `kardianos/service` 统一抽象 systemd / nssm / WinSW 服务化。
4. **长连接友好**：goroutine + WebSocket Hub 可轻松维持大量 Agent 连接，内存占用低。
5. **生态成熟**：Gin / Echo、GORM、gorilla/websocket 稳定可靠。

### 7.3 语言对比

| 语言 | 后端 | Agent（跨平台分发） | 综合评价 |
|---|---|---|---|
| **Go** | 强 | 强（静态单二进制） | **最优，强烈推荐** |
| Java (Spring Boot) | 强 | 弱（JVM 体积 / 内存大） | 后端可选，Agent 不推荐 |
| Node.js (NestJS) | 中 | 弱（需 node runtime） | 快速原型可用，生产分发差 |
| Python (FastAPI) | 中 | 弱（打包分发麻烦） | 同上 |

> 若团队强依赖 JVM 生态，可退化为「Spring Boot 后端 + Go Agent 混合」，但会引入两套配置模型与构建链路，整体成本更高。

### 7.4 具体技术栈

| 组件 | 选型（已确认 ✅） |
|---|---|
| **开发语言** | **Go**（后端 + Agent 统一） |
| 后端框架 | Go + Gin + GORM |
| **前端** | **Vue3 + TypeScript + Vite + Element Plus** |
| **数据库** | **SQLite**（纯 Go 驱动 `glebarez/sqlite`，无 CGO，后续可切 MySQL / PostgreSQL） |
| frps 模式 | **一体化**（面板托管 frps 进程） |
| **配置格式** | **JSON**（frp v0.52.0+ 原生支持 TOML / YAML / JSON，本项目统一用 JSON） |
| 实时通道 | gorilla/websocket（Agent 反连） |
| 鉴权 | JWT（管理面）+ NodeToken（Agent 面） |
| **客户端部署形态** | **二进制直接部署（Linux / Windows / macOS）+ Docker 部署**，两种形态均为正式支持 |
| Agent 服务化 | `kardianos/service`（统一 systemd / nssm / WinSW） |
| 打包 | `go:embed`（前端）+ Docker 多阶段构建 |

---

## 八、部署形态

### 8.1 二进制 + systemd（一体化）

```ini
# /etc/systemd/system/frp-panel.service
[Service]
ExecStart=/usr/local/bin/frp-panel --config /etc/frp-panel/config.json
Restart=always
```

### 8.2 Docker（一体化，面板 + frps 同容器）

```yaml
# docker-compose.yml
services:
  panel:
    image: dpanel/panel:latest
    container_name: frp-panel
    restart: always
    network_mode: host            # Linux 一键放行 frps 全部端口
    volumes:
      - ./data:/data              # DB + frps.json + 日志
    # bridge 模式需显式映射：8080(面板) 7000(frps) 7500(dashboard) 80/443(vhost)
```

### 8.3 分离式

面板仅提供 frps 配置生成 / 导出，frps 独立部署（host 网络或显式端口映射）。

---

## 九、实施里程碑

| 阶段 | 交付内容 |
|---|---|
| **M1** | 面板骨架：初始化管理员、登录、frps 配置表单 + `frps.json` 生成/校验、一体化进程管理 |
| **M2** | 节点管理 + 安装命令生成（`binary` / `docker`）+ `install.sh` + Agent 注册/心跳/拉取配置 + 自动启动 frpc（**Linux 二进制打通最小闭环**） |
| **M3** | 代理/隧道配置 + 全局端口池 + 配置校验 + 版本历史/回滚 + Windows `install.ps1` |
| **M4** | Agent 跨平台构建分发（Linux / Windows 二进制）+ `dpanel/agent` Docker 镜像 + WebSocket 实时通道 + 监控统计 |
| **M5** | Agent 自升级、多用户 RBAC、审计日志、域名/证书管理 |

---

## 十、技术决策（已确认 ✅）

| # | 决策项 | 结论 |
|---|---|---|
| 1 | **开发语言** | **Go**（后端 + Agent 统一，前端 TypeScript） |
| 2 | **前端框架** | **Vue3 + Element Plus** |
| 3 | **frps 管理方式** | **一体化**（面板生成 `frps.json` 并托管 frps 进程） |
| 4 | **数据库** | **SQLite**（纯 Go 驱动，零依赖，后续可平滑切换 MySQL / PostgreSQL） |
| 5 | **客户端部署形态** | **二进制直接部署 + Docker 部署**，两种均为正式形态；平台覆盖 Linux / Windows（二进制另含 macOS） |
| 6 | **配置文件格式** | **JSON**（`frps.json` / `frpc.json` / `agent.json`），frp v0.52.0+ 已原生支持 JSON，INI 已弃用 |

> 统一使用 JSON 的理由：frp v0.52.0+ 原生支持 TOML / YAML / JSON，且新特性仅在非 INI 格式中可用；JSON 与面板前后端的数据结构天然同构（Go struct tag ↔ TS interface），无需模板拼接与片段追加，额外参数直接做对象合并即可。
