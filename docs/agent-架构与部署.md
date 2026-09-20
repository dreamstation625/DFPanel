# DFPanel 分离式部署：Agent 架构与 Docker 部署

> 本文描述本次新增的能力：面板可独立部署在任意服务器；frps 与 frpc 统一由远端 Agent 托管；
> 同一个 Agent 既可承载 frps 也可承载 frpc；配置下发失败（连不上服务端）时自动回滚上一版配置。
> 面板本机一体化托管（M1）保留为 `deployMode = local`，两种模式并存。

## 1. 拓扑

```
        ┌──────────────── 面板（dfpanel） ────────────────┐
        │  Web UI   REST API   AgentHub(WebSocket Server) │
        │  SQLite   配置版本库   指令队列                 │
        └───────────────▲────────────────────────────────┘
                        │ Agent 主动反连（面板不主动外连）
         ┌──────────────┴───────────┐        ┌────────────────────┐
         │  Agent（服务器 A）        │        │  Agent（服务器 B）   │
         │  roles = frps,frpc       │        │  roles = frpc       │
         │  ├ frps-1 (进程/容器)     │        │  └ frpc-7 (进程/容器)│
         │  └ frpc-3 (进程/容器)     │        └────────────────────┘
         └──────────────────────────┘
```

- **面板可独立部署**：面板只存配置、发指令，不要求与 frps 同机。
- **Agent 反连**：Agent 主动连面板，因此可以在 NAT / 内网之后，面板只需有一个可达地址。
- **一个 Agent 多角色**：`DFPANEL_ROLES=frps,frpc`，同一进程同时托管多个 frps 实例与多个 frpc 节点，按 `targetType:targetId` 区分。

## 2. 组件

| 位置 | 包 / 文件 | 职责 |
|---|---|---|
| 面板 | `internal/proto` | 面板与 Agent 共用的协议与签名（零依赖，Agent 不引入 gorm/sqlite） |
| 面板 | `internal/agenthub` | WebSocket 连接管理、指令下发、心跳状态缓存 |
| 面板 | `internal/handler/agent.go` | Agent 面接口：注册 / 心跳 / 拉指令 / 结果上报 / WS |
| 面板 | `internal/handler/agents.go` | 管理面：Agent CRUD、令牌重置、指令与配置版本历史 |
| 面板 | `internal/handler/node.go` | 节点（frpc）管理与配置下发 |
| 面板 | `internal/handler/server.go` | frps 服务端：`local` 与 `agent` 两种托管模式 |
| 面板 | `internal/handler/install.go` | 安装命令生成（二进制 / Docker / Compose）与脚本分发 |
| Agent | `cmd/agent`、`internal/agent` | 远端守护程序：通信、启停、配置应用、健康探测、回滚 |
| 分发 | `internal/distrib` | 内嵌 install.sh / install.ps1；frp 二进制按需下载与缓存 |

数据模型新增：`agents`、`config_versions`、`agent_commands`；
`frps_servers` 增加 `deployMode` / `agentId` / `publicAddr`；`nodes` 增加 `agentId`。

## 3. 通信

- **长连接**：`GET /api/agent/ws`，Agent 主动反连；面板借此实时下发指令并接收结果。
- **降级**：WebSocket 不可用时，Agent 轮询 `GET /api/agent/commands`（15s 级退避），结果通过 `POST /api/agent/report` 上报。
- **离线排队**：Agent 离线时面板把指令写入 `agent_commands`（`pending`），Agent 上线后自动补发。
- **鉴权**：`sign = HMAC-SHA256(secret, "nodeKey.ts")`，时间戳 ±5 分钟防重放，不依赖 Origin。

消息信封：

```jsonc
{ "type": "command", "id": "cmd-12", "data": {
    "commandId": 12, "type": "apply", "targetType": "server", "targetId": 1,
    "payload": "{ frps 配置全文 }", "version": 3 } }
```

指令类型：`apply` / `start` / `stop` / `restart` / `log` / `rollback`。

## 4. 配置应用与回滚（核心）

`POST /api/servers/:id/apply` 或 `POST /api/nodes/:id/apply` 触发，Agent 侧流程：

1. **备份 + 快照**：当前（可用）配置复制为 `<config>.bak`；本次收到的配置写入 `<dataDir>/history/<kind>-<id>.v<version>.json`（无论后续成败都保留，默认最近 30 份）。
2. **写入**：原子写新配置（先写 `.tmp` 再 rename），避免半截文件。
3. **语法校验**：`frps verify -c <config>`；不通过则直接还原，**不动正在运行的服务**。
4. **重启**：停止旧进程 / 移除旧容器（等待进程真正退出，避免端口未释放），用新配置启动。
5. **健康探测**（默认 25s，三态判定）：

| 结论 | 触发条件 | 动作 |
|---|---|---|
| **可用** | 日志出现 `frps started successfully` / `login to server success` 等成功关键字且 TCP 探测通过；或稳定超过 3s 且探测通过 | 上报 `applied` |
| **明确故障** | 日志命中 `address already in use`、`login to server failed`、`connect to server error`、`token in login doesn't match`、`authentication failed`、`connection refused`、`invalid configuration` 等；或进程直接退出 | **回滚**到上一版并重新拉起，上报 `rolled_back` + 原因 |
| **未确认** | 超时仍未连通，但进程仍在运行且日志无任何明确报错（临时网络抖动、依赖服务稍慢等） | **不回滚**，保留新配置，上报 `unverified`，面板提示人工确认 |

刻意不把 `i/o timeout` 这类临时性信息纳入失败判定，避免误回滚。

6. **成功后**记录生效版本；**仅「明确故障」会触发回滚**。

面板侧每次下发都写入 `config_versions`（版本、指纹、状态、失败原因），可在 `/api/config-versions` 追溯，并由 `rollback` 指令主动回退。

冒烟验证（本机 Windows，Agent `process` 模式）：

| 场景 | 结果 |
|---|---|
| 下发可用 frps 配置（bindPort 7020） | `ok=true`，`applied` |
| 再次下发（改 bindPort 7021） | `ok=true`，`applied`（历史中保留 v2 快照） |
| 下发一个端口被占用的配置 | `ok=false`，命中 `bind: Only one usage of each socket address`，`rolled_back=true` 并恢复上一版 |
| 查看历史版本 | `fromAgent=true`，返回 v2（当前）/ v1（failed） |
| 回滚到 v2 | `ok=true`，消息「已回滚到历史版本 v2：配置已生效」，历史新增 v4 并标记当前 |

### 4.1 历史版本与按版本回滚

- **Agent 侧快照**：每次收到下发的配置都会在 `<dataDir>/history/` 写一份 `frps-1.v3.json` / `frpc-7.v5.json`（默认保留最近 30 份），无论该次应用成功还是失败。
- **面板查看**：
  - `GET /api/servers/:id/versions`、`GET /api/nodes/:id/versions`
  - Agent 在线时下发 `versions` 指令读取其本地快照（版本号、时间、大小、指纹、是否当前生效、是否仍可回滚），并叠加面板侧记录的状态与说明；
  - Agent 离线或本机托管模式退回面板侧版本记录（仍可回滚，面板会重新下发该版本配置）。
- **按版本回滚**：
  - `POST /api/servers/:id/rollback`、`POST /api/nodes/:id/rollback`，body `{"version": N}`
  - Agent 模式：下发 `rollback` 指令，Agent 读取本地快照后走**完整流程**（写入 → 校验 → 重启 → 健康探测），若出现明确故障会再退回回滚前的配置；
  - 本机托管模式：面板写入目标版本配置并重启 frps；
  - 回滚本身也会生成一个新的版本号（内容为所选版本），历史中可完整追溯。
- **前端入口**：frps 服务端页「历史版本」按钮、客户端节点列表「版本」按钮，弹窗内展示版本列表与「回滚」操作，当前生效版本标记为「当前」且不可回滚。

## 5. 部署

### 5.1 面板（Docker）

```bash
docker compose up -d            # 或：docker build -t dreamstation625/dfpanel:latest . && docker run ...
```

- 镜像：`Dockerfile`（node 构建前端 → go 构建后端 → alpine 运行，内含 frps 二进制）
- 数据卷：`./data:/data`（SQLite、frps 配置、frp 二进制缓存）
- 关键环境变量：`DFPANEL_PUBLIC_URL`（面板对外地址，用于生成安装命令与 frpc 的 serverAddr）
- Linux 用 `network_mode: host`；Windows/macOS 桌面版改用 `ports` 映射（见 `docker-compose.yml` 注释）

### 5.2 Agent（Docker）

```bash
docker compose -f docker-compose.agent.yml up -d
```

或直接运行（面板「Agent → 安装命令」会给出带令牌的现成命令）：

```bash
docker run -d --name dfpanel-agent --restart unless-stopped \
  -e DFPANEL_URL=http://<panel>:8080 \
  -e DFPANEL_NODE_KEY=<nodeKey> \
  -e DFPANEL_NODE_SECRET=<secret> \
  -e DFPANEL_ROLES=frps,frpc \
  -e DFPANEL_RUNTIME=process \
  -v dfpanel-agent-data:/var/lib/dfpanel-agent \
  --network host \
  dreamstation625/dfpanel-agent:latest
```

镜像 `Dockerfile.agent` 已内置 frps / frpc 二进制，构建时自动从 frp 官方 release 获取（可用 `--build-arg FRP_VERSION=0.61.1` 固定版本）。

### 5.3 frps / frpc 容器化运行（runtime=docker）

Agent 支持两种运行时，`DFPANEL_RUNTIME` 控制，对 frps 与 frpc 分别生效：

| 运行时 | 行为 | 适用 |
|---|---|---|
| `process`（默认） | Agent 直接拉起 `frps` / `frpc` 子进程 | 裸机、systemd、Agent 容器内置二进制 |
| `docker` | Agent 用 docker CLI 起容器（挂载配置、host 网络、按容器状态做健康检查） | 希望隧道进程独立隔离、统一镜像管理 |

`runtime=docker` 时需要给 Agent 挂载 `/var/run/docker.sock`（镜像已内置 docker-cli）：

```yaml
volumes:
  - dfpanel-agent-data:/var/lib/dfpanel-agent
  - /var/run/docker.sock:/var/run/docker.sock
```

回滚逻辑与 process 运行时完全一致：容器起不来 / 连不上服务端 → 还原配置 → 重建容器。

### 5.4 Agent 二进制一键安装

面板会生成（也可直接从面板取脚本）：

```bash
# Linux / macOS（systemd / launchd 自动注册）
curl -fsSL http://<panel>:8080/install.sh | sudo bash -s -- \
  --panel http://<panel>:8080 --node-key <KEY> --secret <SECRET> --roles frps,frpc

# Windows（计划任务开机自启）
powershell -ExecutionPolicy Bypass -Command "irm http://<panel>:8080/install.ps1 -OutFile install.ps1; .\install.ps1 -Panel http://<panel>:8080 -NodeKey <KEY> -NodeSecret <SECRET> -Roles frps,frpc"
```

分发端点：

| 端点 | 说明 |
|---|---|
| `GET /install.sh`、`GET /install.ps1` | 安装脚本（内嵌分发） |
| `GET /downloads/agent/:os/:arch` | Agent 二进制（构建脚本产出后放到 `data/bin/`） |
| `GET /downloads/:kind/:version/:os/:arch` | frps / frpc 二进制，本地缺失时自动从官方 release 下载并缓存 |

## 6. 构建

```powershell
.\build.ps1                       # 面板（前端 + 后端 -> dfpanel.exe）
.\build.ps1 -Agent                # 编译 Agent -> dist/dfpanel-agent-<os>-<arch>
.\build.ps1 -Agent -AllPlatforms  # 一次产出 linux/amd64、linux/arm64、windows/amd64、darwin/arm64
.\build.ps1 -Docker               # 构建面板镜像 dreamstation625/dfpanel:latest
.\build.ps1 -Docker -Agent        # 构建 Agent 镜像 dreamstation625/dfpanel-agent:latest
.\build.ps1 -Docker -Image my/panel:v1   # 指定标签
```

`build.sh` 参数等价：`--agent --all-platforms --docker --image`。

启动面板时建议指定对外地址，否则安装命令只能按请求地址推断：

```bash
dfpanel -listen :8080 -data ./data -public-url http://1.2.3.4:8080
dfpanel -version                                  # 查看版本号
dfpanel -agent-image myrepo/dfpanel-agent:0.2.0   # 生成安装命令时使用的 Agent 镜像
```

### 6.1 版本号与镜像发布

版本号只有一个来源：根目录 `VERSION` 文件，从 `0.0.1` 起，当前为 `0.0.1-beta.01`。

| 形式 | 示例 | 镜像标签 | `latest` |
|---|---|---|---|
| 正式版本 | `0.0.1` | `0.0.1`、`v0.0.1`、`sha-xxxxxxx` | 更新 |
| 预发布 | `0.0.1-beta`、`0.0.1-beta.2`、`0.0.1-beta.01`、`0.0.1-rc.1` | `0.0.1-beta.01`、`v0.0.1-beta.01`、`sha-xxxxxxx` | 不动 |

`VERSION` 会在 CI 的 `verify` 任务里做格式校验（`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`），格式不符直接失败，避免打出非法镜像标签。

注意 `0.0.1-beta.01` 不是严格 SemVer（前导零的数字标识不合法，`beta.1` 才合法），所以 Docker 官方 `metadata-action` 的 `type=semver` 会跳过这类版本号；镜像的版本标签由 `type=raw,value=<VERSION>` 兜底，标签内容与 `VERSION` 完全一致。

| 位置 | 注入方式 | 查看方式 |
|---|---|---|
| 面板二进制 | `build.ps1` / `build.sh` 读取 `VERSION`，`-ldflags "-X main.version=<ver>"` | `dfpanel -version`、启动日志、侧边栏底部 |
| Agent 二进制 | 同上，`-ldflags "-X dfpanel/internal/agent.Version=<ver>"` | 「Agent 管理」列表的版本列（随心跳上报） |
| 面板 / Agent 镜像 | Dockerfile 构建阶段 `-ldflags "-X ...=$(cat VERSION)"` | `docker image inspect`、`dfpanel -version` |

未注入时面板与 Agent 的版本均为 `dev`。面板版本号通过 `GET /api/init-status` 一并返回（`{"initialized": bool, "version": "0.0.1-beta.01"}`），前端在侧边栏底部展示，含 `-` 的预发布版本会单独标色。

镜像发布由 `.github/workflows/docker.yml` 完成：

| 触发 | 行为 |
|---|---|
| push `main` 且改动 `VERSION` 或 workflow 文件 | 构建并推送 `linux/amd64` + `linux/arm64` |
| 推送 `v*` tag | 校验 tag 与 `VERSION` 一致后再推送，附带版本标签 |
| `workflow_dispatch` | 手动强制构建 |

- 镜像名：`<DOCKERHUB_USERNAME>/dfpanel`、`<DOCKERHUB_USERNAME>/dfpanel-agent`
- 凭据：仓库 Environment `DOCKERHUB` 的 `DOCKERHUB_USERNAME`（variable）与 `DOCKERHUB_TOKEN`（secret）
- 推送前会先在 `verify` 任务里编译并 `go vet`，版本号不一致或格式非法直接失败
- `latest` 是否更新由 `verify` 输出决定（仅正式版本 + 默认分支或正式 tag），`metadata-action` 的自动 latest 已关闭

发布新版本：

```bash
# 预发布：只推版本标签，latest 保持稳定版
echo "0.0.2-beta" > VERSION
git add VERSION && git commit -m "chore: 0.0.2-beta"
git push origin main

# 正式版：打 tag 一并推送
echo "0.0.2" > VERSION
git add VERSION && git commit -m "chore: 升级版本号至 0.0.2"
git tag v0.0.2 && git push origin main --tags
```

镜像不在 Docker Hub 时，用 `-agent-image` / `DFPANEL_AGENT_IMAGE` 指定，面板生成的 docker run / compose 安装命令会随之更新。

### 6.2 前端依赖与 package-lock.json

面板镜像的前端阶段用 `npm ci` 严格按 `web/package-lock.json` 安装。注意 npm 的一个行为：**生成 lock 时会按当前平台裁剪可选依赖**，所以只在一台机器上生成的 lock 可能缺少其它平台的原生包。

典型症状：本机（Windows）构建正常，容器里却报

```
process "/bin/sh -c npm run build" did not complete successfully: exit code: 1
  Error: Cannot find module '@rollup/rollup-linux-x64-musl'
```

排查与修复：

```bash
# 检查 lock 里是否有当前平台需要（或全平台）的原生包
node -e "const l=require('./package-lock.json');console.log(Object.keys(l.packages).filter(k=>/@rollup|@esbuild/.test(k)))"

# lock 不完整时，删掉重新解析一次（会补齐所有平台的条目，已锁定的依赖版本不变）
cd web && rm package-lock.json && npm install --package-lock-only
```

一份完整的 lock 应包含 `@rollup/rollup-linux-x64-musl`（alpine 用）、`@rollup/rollup-linux-x64-gnu`、`@esbuild/linux-x64` 等所有平台条目（本项目为 153 条，缺平台包时只有 100 条）。`npm ci` 会在容器里自动挑选当前平台需要的那个。不要用 `--omit=optional` 生成 lock。

## 7. 接口一览

Agent 面（签名鉴权）：`POST /api/agent/register`、`POST /api/agent/heartbeat`、`POST /api/agent/commands`、`POST /api/agent/report`、`GET /api/agent/ws`

管理面（JWT）。**方法约定：写操作一律 POST，读操作 GET，不使用 PUT / DELETE。**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/init-status` | 是否已初始化 + 面板版本号（免鉴权） |
| POST | `/api/init`、`/api/auth/login` | 初始化管理员 / 登录（免鉴权） |
| GET | `/api/agents`、`/api/agents/:id` | Agent 列表 / 详情 |
| POST | `/api/agents` | 创建 Agent |
| POST | `/api/agents/:id/update` | 更新 Agent |
| POST | `/api/agents/:id/delete` | 删除 Agent |
| POST | `/api/agents/:id/token/reset` | 重置安装令牌 |
| GET | `/api/agents/:id/commands` | 指令历史 |
| GET | `/api/agents/:id/install-command` | 安装命令（`os`、`runtime` 可选） |
| GET | `/api/nodes`、`/api/nodes/:id` | 节点（frpc）列表 / 详情 |
| POST | `/api/nodes` | 创建节点 |
| POST | `/api/nodes/:id/update` | 更新节点 |
| POST | `/api/nodes/:id/delete` | 删除节点 |
| POST | `/api/nodes/:id/apply` | 生成 frpc 配置并下发 |
| POST | `/api/nodes/:id/start\|stop\|restart`、GET `/log` | 生命周期与日志 |
| GET | `/api/nodes/:id/install-command` | 该节点所属 Agent 的安装命令 |
| GET | `/api/nodes/:id/proxies` | 节点下隧道列表 |
| POST | `/api/nodes/:id/proxies` | 新增隧道 |
| POST | `/api/proxies/:id/update` | 修改隧道 |
| POST | `/api/proxies/:id/delete` | 删除隧道 |
| GET | `/api/config-versions?targetType=&targetId=` | 配置版本历史（回滚依据） |
| GET | `/api/servers/:id/versions`、`/api/nodes/:id/versions` | 历史版本列表（Agent 本地快照 + 面板记录） |
| POST | `/api/servers/:id/rollback`、`/api/nodes/:id/rollback` | 回滚到指定版本，body `{"version": N}` |

frps 服务端：`GET /api/servers`、`POST /api/servers`、`GET /api/servers/:id`、`POST /api/servers/:id/update`、`POST /api/servers/:id/delete`、`GET /api/servers/:id/config`、`POST /api/servers/:id/apply|start|stop|restart`、`GET /api/servers/:id/log`。

frps 服务端：`deployMode=agent` + `agentId` 时，`apply` / `start` / `stop` / `restart` / `log` 全部经 Agent 执行；`deployMode=local` 时保持面板本机一体化行为不变。

## 8. 前端页面

| 页面 | 路由 | 能力 |
|---|---|---|
| 概览 | `/dashboard` | frps / Agent / 节点统计，服务端与节点的部署位置与状态 |
| frps 服务端 | `/server` | 多服务端配置：新建（自动挑选空闲端口）/ 下拉切换 / 保存 / 保存并应用 / 删除；部署模式切换（本机托管 / 远端 Agent 托管）、选择托管 Agent、填写 publicAddr；下发失败自动回滚有明确提示 |
| Agent 管理 | `/agents` | 列表（在线状态、主机、版本、最后心跳）、创建/编辑（角色）、安装命令（Linux/Windows × 进程/Docker × 脚本/docker run/compose）、重置令牌、指令历史 |
| 客户端节点 | `/nodes` | 节点 CRUD（绑定托管 Agent、关联 frps、连接地址、TLS、协议）、隧道管理抽屉、应用配置、启停重启、配置预览、日志、安装命令 |
| 隧道 | 节点页抽屉 | tcp / udp / http / https / stcp / sudp / xtcp，本地与远端端口、域名/子域名、分组、加密压缩、启用开关 |

节点与隧道的修改不会立即生效，需点击「应用配置」由 Agent 下发并做健康校验。

### 8.1 官方文档与字段帮助

- 侧边栏「官方文档」子菜单：文档首页、服务端配置参考、客户端配置参考、通用配置、代理配置、功能说明，点击在新窗口打开。
- frps / frpc 配置项标签右侧带 `?` 角标：悬停显示该字段功能说明，点击跳转到官方文档对应小节，
  例如 `reference/server-configures/#serverconfig`、`reference/common/#webserverconfig`、`reference/proxy/#tcpproxyconfig`。
  链接集中维护在 `web/src/utils/docs.ts`，说明角标组件为 `web/src/components/FieldHelp.vue`。

### 8.2 Dashboard 默认策略

- 新建 frps 服务端时 `dashboardEnabled = false`，且 **不向 frps.json 写入 webServer 段**（端口为 0 时同样不写）。
- 同时预置一个 **8 位随机密码**（字符集剔除 0/O/1/l/I），创建后即可见，开启 Dashboard 时直接使用；
  表单提供「随机生成」与「复制」。后端在 `POST /api/servers` 时兜底生成，前端 `emptyServer()` 也会本地生成。
- 历史记录中 `dashboard_enabled` 为 NULL 时按「未启用」处理，因此升级后旧服务端默认不再下发 webServer 段，需要时在界面开启即可。

### 8.3 多服务端与端口冲突

- 服务端可以建多个：`POST /api/servers` 新建，前端「新建服务端」会自动选择一个同机上未被占用的 `bindPort`（从 7000 起找空位）。
- 端口冲突校验（后端 `checkPortConflict`，创建与更新都会校验）：
  - **同一台机器**的定义 —— `deployMode=local` 都视为面板本机；`deployMode=agent` 按 `agentId` 区分。
  - 同机上的 `bindPort` 不能重复；两边都启用 Dashboard 时 `dashboardPort` 也不能重复。
  - 不同机器（不同 Agent）可以使用相同端口，互不冲突。
- 新建为草稿态（`id=0`），顶部标签显示「未保存」，预览 / 日志 / 启停按钮在保存前不可用；删除后自动选中剩余的第一个服务端。

## 9. 待办

- Agent 自动升级与灰度。
- Docker 镜像推送到仓库后的版本化 `install` 命令。
- 配置版本历史页面化（当前 `rollback` 指令与版本接口已具备，前端仅提示回滚结果）。
