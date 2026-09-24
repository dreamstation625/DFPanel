# frp 版本管理方案

> 状态：**已实现**（2026-09-21）｜ 起草：2026-09-21 ｜ 目标版本：`0.0.1-beta.04`
>
> 需求：面板上可对服务端（frps）、客户端（frpc）**下载 / 更新指定的版本或最新版本**。
>
> 版本列表来源：**固定使用 GitHub 官方接口**（不做镜像）；下载支持配置镜像源模板。

## 1. 已确定的决策

| 决策项 | 结论 | 影响 |
|---|---|---|
| 版本粒度 | **每个 Agent 统一一个版本**（该 Agent 上的 frps 与 frpc 共用一个 frp 版本） | 版本成为 Agent 级属性，不是每实例属性；切换时该 Agent 全部实例一起生效 |
| 面板本机 frps | 同样「统一一个版本」，在设置页配置 | 与 Agent 语义对称，无需在 `FrpsServer` 上加字段 |
| Docker 运行时 | **支持版本替换**：镜像只当运行时底座（**底座镜像里不含 frp**），frp 版本完全由面板下发的二进制决定 | 见第 4 节；镜像 tag 与 frp 版本彻底无关 |
| Agent 镜像构建 | 构建时解析最新版本（现状已是此行为） | `Dockerfile.agent` 无需改动，仍可 `--build-arg FRP_VERSION=x.y.z` 固定 |
| 镜像源 | **支持配置**，并新增**面板设置页**可在线修改 | 新增设置表 + `views/Settings.vue` |
| 存量数据（版本为空） | **不动**，只提示「可更新」 | 面板升级不会批量重启在跑的服务；容器底座没有 frp，未下发前容器不会启动，界面上显示「未下发」 |
| 手填版本号 | **保留**作为兜底 | 版本列表拉取失败 / 内网环境仍可用 |

## 2. 现状与必须先修的问题

1. **`distrib.EnsureFRPBinary` 的缓存键不含版本**（`internal/distrib/download.go:69`）
   落地文件名固定 `frps-<os>-<arch>`，先拉过 `latest` 之后再请求 `0.61.1`，会**直接返回旧的缓存文件**，等于「指定版本」完全失效。**本方案的头号阻塞点。**
2. **面板本机 frps 从不下载二进制**
   `frp.Manager.BinPath()` 固定 `bin/frps[.exe]`；`Server.Start` 在 `IsInstalled()` 为假时报「请先放置到 …」。而分发端点 `GET /downloads/:kind/:version/:os/:arch` 本就支持任意版本，只是面板自己没用。
3. **`DFPANEL_FRPVERSION` 未接线**
   `agent.Config.FRPVersion` 有 JSON 字段、`client.DownloadBinary` 也读它，但 `LoadConfig` 没有读这个环境变量（注释声称支持，实际漏写）。
4. **`Node.Version` / `Agent.Version` 是 dfpanel-agent 自身版本**，与 frp 版本不是一回事，新字段必须另起名。
5. **`GET /downloads/:kind/:version/:os/:arch` 是免鉴权路由且 `version` 完全可控**
   （`internal/router/router.go:118`，在 JWT 分组之外）。一旦把 `version` 拼进可配置的 URL 模板，就出现路径穿越风险 → **必须对版本号做严格格式校验**。

## 3. 设计

### 3.1 二进制存储：版本化存放 + 固定 active 槽位

这是本方案的关键简化点。**active 路径保持不变**，因此 `frp.Manager.BinaryPath()`、`agent.Config.BinaryPath()`、`Spec.BinPath`（在 `Agent.controller()` 创建时就快照进 `a.ctrls[]` 缓存）**全都不用改**，`verify` / `Start` / `waitHealthy` 整条链路零改动，也不需要重建 controller。

```
<dataDir>/bin/
  frps-0.62.1-linux-amd64        ← 版本化存储，多版本共存（天然就是回滚备份）
  frps-0.61.1-linux-amd64
  frps                           ← active 槽位（现有代码认这个路径）
  frpc-0.62.1-linux-amd64
  frpc
```

切换 active 的方式：

- **Linux / macOS**：`os.Symlink(版本化文件, bin/frps)` + `os.Rename` 原子替换链接
- **Windows**：软链需要开发者模式或管理员权限，退化为**拷贝**：先 `bin/frps.new` 完整写入并 `Chmod`，再 `os.Rename` 覆盖（同卷 rename 原子）

两种方式都保证 active 路径字面不变。

### 3.2 设置存储与优先级

新增 key-value 表，避免为几个配置项加列：

```go
type Setting struct {
    Key   string `gorm:"primaryKey;size:64"`
    Value string `gorm:"type:text"`
}
```

配置项：

| Key | 说明 | 默认值 |
|---|---|---|
| `frpDownloadBase` | 下载 URL 模板，支持 `{version}` `{asset}` `{os}` `{arch}` | `https://github.com/fatedier/frp/releases/download/v{version}/{asset}` |
| `frpReleaseAPI` | 版本列表 API | `https://api.github.com/repos/fatedier/frp/releases` |
| `frpManualVersions` | 手填兜底版本，逗号或换行分隔 | 空 |
| `panelFrpVersion` | 面板本机 frps 使用的版本 | 空（= 不管理） |

**优先级：数据库设置（非空）> 启动参数/环境变量 > 内置默认。**
理由：设置页在线可改，优先于启动参数，免得每次改镜像源都要重启面板。同时保留 `-frp-download-base` / `-frp-release-api` 两个 flag 与对应 `DFPANEL_` 环境变量，用于「设置表还没初始化就要生效」的场景。

镜像源模板示例：

```
官方  https://github.com/fatedier/frp/releases/download/v{version}/{asset}
代理  https://ghproxy.net/https://github.com/fatedier/frp/releases/download/v{version}/{asset}
自建  https://mirror.example.com/frp/v{version}/{asset}
```

保存时校验模板必须同时包含 `{version}` 与 `{asset}`，否则拒绝。

### 3.3 版本号校验（安全）

`version` 来自免鉴权 URL 参数，且会被拼进可配置模板。统一入口校验：

```go
var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$`)
```

`{asset}` 由固定常量拼装（`frp_<ver>_<os>_<arch>.tar.gz|.zip`），不引入额外输入。校验不通过直接 400，不进入下载流程。

### 3.4 两个动作：download 与 activate 分离

刻意拆成两步，而不是「一键换版本并重启」：

| 动作 | 作用 | 风险 |
|---|---|---|
| **download** | 面板出网拉取 → 落为面板 `bin/` 下的版本化文件（`<kind>-<ver>-<os>-<arch>`）→ `-v` 自检比对版本号 | 不碰 active、不重启，失败无影响 |
| **activate** | 切 active 槽位 → 逐个重启该 Agent 上全部托管实例 | 触及在跑服务，复用现有健康三态与回滚 |

下载**只在设置页做**（按版本 + 类型 + 目标平台挑），Agent 侧没有下载入口：

- 好处：能提前给不同架构的 Agent 备好二进制，切换时只是「换槽位」，耗时可控、不会卡在上游下载上；
  也让离线环境可以先备好再切。
- 代价：切到一个面板没缓存的版本会直接被拒（400，提示先去设置页下载），多一步手工操作 —— 这是刻意的，
  好过让切换在十几分钟的上游下载里悬着。

**activate 的回滚策略**（沿用现有机制，不新增判定规则）：

1. 记录切换前的 active 版本
2. 切换 active 槽位
3. 逐个实例 `Stop → 等真退 → Start → waitHealthy`
4. 任一实例判定为**明确故障** → 把 active 切回旧版本 → 重启受影响的实例
5. 全部实例逐个上报结果（`applied` / `rolled_back` / `unverified`），汇总为一次操作结果

> 注：`unverified` 沿用现有语义——进程在跑、日志无明确报错，**不回滚**，提示人工确认。

### 3.5 协议新增

`internal/proto/proto.go`：

```go
CmdFrpActivate = "frp_activate" // payload: {"version":"0.62.1"}
```

- Agent 只做切换，没有 `frp_download`：二进制统一由面板在设置页预下载（`CmdFrpDownload` 已移除）。
- 版本号一律由**面板侧**解析：`latest` 永远在面板解析成具体版本号再下发，避免各 Agent 各自解析产生版本漂移。
- 走现有 `agent_commands` 队列（离线排队、上线补发），无需新通道。
- `HeartbeatData` 增加 `frpVersion`（当前 active 版本）与 `frpCached`（已缓存的版本列表），Agent 心跳上报。

**出网原则不破例**：Agent 只从面板 `GET /downloads/...` 拉取，永远不需要能访问 GitHub。镜像源只在面板侧配置。

### 3.6 API（写 POST / 读 GET）

```
GET  /api/settings                 # 读设置（密钥类字段脱敏）
POST /api/settings                 # 写设置
GET  /api/frp-versions             # 可用版本 + 手填 + 已缓存（Agent 侧二进制也从这里选版本）
GET  /api/frp/cache                # 面板已缓存的二进制清单（kind / version / os / arch）
POST /api/frp/cache                # 预下载：{"version":"0.62.1","kinds":["frps","frpc"],"os":"linux","arch":"amd64"}
DELETE /api/frp/cache?kind=&version=&os=&arch=
GET  /api/agents/:id/frp           # 该 Agent：期望版本 / active 版本 / 已缓存列表
POST /api/agents/:id/frp/activate  # {"version":"0.62.1"}；面板缺该版本/平台时 400 拒绝
GET  /api/frp/local                # 面板本机：当前版本 / 已缓存 / 配置的版本
POST /api/frp/local/download       # 面板本机下载
POST /api/frp/local/activate       # 面板本机切换并重启全部本机 frps 实例
```

### 3.7 前端

| 位置 | 内容 |
|---|---|
| `views/Settings.vue`（新） | 镜像源模板、版本 API、手填版本、面板本机 frp 版本；带模板格式校验与「测试连通」 |
| `views/Agents.vue` | 新增 frp 版本卡片：active 版本 / 期望版本 / 可更新提示；「更新 frp 版本」弹窗 |
| `components/FrpVersionPicker.vue`（新） | 版本下拉（最新置顶 + 历史列表 + 已缓存标记）+ 手填输入 + 下载/激活两个按钮，`Agents.vue` 与设置页共用 |
| `views/ServerConfig.vue` | `deployMode=local` 显示本机 frp 版本；`agent` 模式显示所属 Agent 版本（只读 + 跳转） |
| `layout/MainLayout.vue` + `router/index.ts` | 侧边栏新增「设置」，路由 `/settings` |

## 4. 改动清单

**后端**
- `internal/distrib/download.go`：版本化落地名、模板化 URL、版本号校验、`ListVersions()`、`CachedVersions(dir)`
- `internal/model/agent.go`：`FRPVersion`（期望）、`FRPInstalledVersion`（active 实际）、`FRPCachedVersions`
- `internal/model/setting.go`（新）、`internal/database/database.go`：AutoMigrate 增表
- `internal/config/config.go`：`-frp-download-base` / `-frp-release-api`
- `internal/setting/store.go`（新）：读写 + 默认值 + 优先级合并 + 10 分钟版本列表缓存
- `internal/proto/proto.go`：两个新指令 + heartbeat 字段
- `internal/agent/config.go`：`LoadConfig` 接 `DFPANEL_FRPVERSION`；新增版本化/active 路径方法
- `internal/agent/client.go`：`DownloadBinary(kind, version)`
- `internal/agent/agent.go`：处理两个新指令；重启该 Agent 全部托管实例；心跳带上 frp 版本
- `internal/frp/process.go`：Manager 增加 active 切换与当前版本查询
- `internal/handler/settings.go`（新）、`handler/agents.go`、`handler/agent.go`（心跳落库）、`handler/install.go`（版本校验）
- `internal/router/router.go`：注册新路由

**前端**
- `api/index.ts`、`views/Settings.vue`（新）、`components/FrpVersionPicker.vue`（新）、`views/Agents.vue`、`views/ServerConfig.vue`、`layout/MainLayout.vue`、`router/index.ts`

**构建（可选）**
- `Dockerfile.agent` 支持传镜像源做构建期拉取；`build.ps1` / `build.sh` 透传

## 5. 不做的事

- Windows 容器（`docker info` 报 OSType=windows）的版本替换 —— 只支持 Linux 容器；
  非 Linux 容器平台会返回明确错误并建议改用 process 运行时。
- 每实例独立版本（已选 Agent 级统一）
- 存量实例自动升级（版本为空即不动）
- Agent 自动升级灰度（属另一条待办，本次不碰）
- Agent 自身以容器方式运行、再去托管兄弟容器时的文件传递：不 bind-mount，
  改用 `docker cp` 把二进制与配置送进容器（见下文「不 bind-mount」一节）。

## 5.1 Docker 运行时的版本替换（第 4 节）

镜像只作为**运行时底座**（默认 `alpine:3.20`，可用 `DFPANEL_FRPS_IMAGE` / `DFPANEL_FRPC_IMAGE` 覆盖），
frp 二进制由面板下发，版本与镜像 tag 彻底解耦。启动是「create → cp → start」三步：

```
docker create --name dfpanel-frps-1 --restart unless-stopped \
  --network host \
  --entrypoint /dfpanel-frps \
  alpine:3.20 -c /dfpanel-frpc.json
docker cp <binDir>/frps-container-linux-amd64 dfpanel-frps-1:/dfpanel-frps
docker cp <dataDir>/frps-1.json                dfpanel-frps-1:/dfpanel-frpc.json
docker start dfpanel-frps-1
```

几个关键设计点：

1. **显式 `--entrypoint` 指到我们自己的固定路径**（`/dfpanel-frps`、`/dfpanel-frpc`），
   不去猜镜像里的二进制位置与入口，因此对**任意**底座镜像都成立（官方 frp 是
   `CGO_ENABLED=0` 静态编译，alpine / busybox 都能跑）。
2. **不 bind-mount，用 `docker cp` 送文件**：挂载源必须是宿主能看到的路径，Agent 自己跑在
   容器里时就有一堆坑（宿主上不存在 → docker 建出同名空目录 → `exec: "/dfpanel-frpc": is a directory`；
   compose 写相对路径时宿主绝对路径又只有 daemon 知道）。`docker cp` 走 daemon 传输，没有这个问题。
3. **容器内路径刻意用单层**（`/dfpanel-frps`、`/dfpanel-frpc.json`）：cp 过去时不必担心
   镜像里有没有多级父目录。
4. **容器槽位独立于 process 槽位**：`<binDir>/<kind>-container-<os>-<arch>`，
   与 process 的 `<dataDir>/<kind>[.exe]` 分开。同一台机器上两种运行时可并存，
   也避免 Windows 宿主上 `.exe` 后缀污染容器二进制。
5. **强制由面板下发，没有第二条退路**：底座镜像里没有 frp，`startDocker` 先过
   `requireDockerReady`，槽位缺失或 docker 不可用就直接报错（提示去「设置 → frp 二进制」下载），
   不会静默跑起一个版本不明的 frp。因此界面上只会出现「未下发」，不再有「镜像自带」。
5. **平台按容器探测**，不是宿主平台：用 `docker info --format '{{.OSType}}/{{.Architecture}}'`
   （`x86_64→amd64`、`aarch64→arm64`），因为 Windows/macOS 的 Docker Desktop 跑的是 Linux 容器，
   必须下载 linux/amd64 的 frp。结果缓存 5 分钟。
6. **不自检 `-v`**：宿主可能根本执行不了 linux 二进制，`DownloadBinary` 的自检逻辑
   「只在能跑起来时才校验」，跨平台时自动跳过，版本正确性交给容器启动后的日志与健康探测。
7. activate 流程与 process 完全一致：停容器 → 换槽位 → 重建容器 → `waitHealthy` 三态；
   失败则把槽位切回旧版本重建；原先就没有槽位的则删除槽位（回到「未下发」，等面板下发后再启动）。

## 6. 验收要点

1. 面板本机 frps：指定版本下载后切换，`-v` 输出的版本与期望一致，实例正常监听
2. Agent frps + frpc：`download` 后 active 不变；`activate` 后该 Agent 全部实例切到新版本并正常
3. 切换到一个**不能启动**的版本（用一个假二进制模拟）→ 自动切回旧版本并恢复运行，面板显示 `rolled_back`
4. `latest` 在面板解析为具体版本号，Agent 侧收到的始终是确定版本
5. 改镜像源为代理地址后，下载走代理；把版本 API 改成不可达地址 → 前端仍可用手填版本完成下载
6. 版本为空的历史数据升级后行为不变
7. 版本号传 `../../etc/passwd` 之类 → 400 拒绝，不产生任何请求

## 7. 实现记录（2026-09-21）

### 已按方案落地的部分

- `distrib`：版本化落地名（`frps-0.62.1-linux-amd64`）、模板化下载地址、版本号白名单、`ListVersions`（GitHub 官方）、`CachedVersions`、`ActivateBinary`（软链/拷贝）、`ActiveVersion`、`QueryVersion`
- 设置表 `settings` + `internal/setting`（优先级：DB > flag/env > 默认）；`-frp-download-base` / `DFPANEL_FRP_DOWNLOAD_BASE`
- Agent：`CmdFrpActivate` / `CmdFrpStatus`（`CmdFrpDownload` 已于 2026-09-24 移除）；`DFPANEL_FRPVERSION` 已接线；心跳上报 active 版本与已缓存列表；`Agent.FRPVersion / FRPInstalledVersion / FRPCachedVersions / FRPUpdatedAt`
- 面板本机：`frp.Manager` 增加 active 版本查询、已缓存列表、下载与切换、`RunningIDs`
- 接口：`/api/settings`、`/api/frp-versions`、`/api/frp/cache`、`/api/frp/local{,/download,/activate}`、`/api/agents/:id/frp{,/activate}`
- 前端：`views/Settings.vue`、`components/FrpVersionPicker.vue`、Agents 页版本列与入口、Nodes 页 frpc 版本列、ServerConfig 页版本标签、侧边栏「设置」

### 与方案有出入的地方（实现时调整）

1. **本机 activate 不用三态健康探测**：本机托管在改造前就没有健康探测能力（`frp.Manager` 无 `waitHealthy`），
   因此改为「启动后等 1.5s 确认进程仍存活」，能识别架构不匹配这类起不来的二进制；
   Agent 侧仍完整复用 `waitHealthy` 的三态判定与回滚。
2. **修了一处依赖的既有缺陷**：`frp.Manager.Stop()` 原先不等待进程退出（只有 `Restart()` 里手写了等待循环），
   而版本切换正是「停 → 换槽位 → 启」，会踩到端口未释放 / Windows 文件占用。
   现将等待逻辑收进 `Stop()`（`waitStopped`，最多 5s），与 Agent 侧 `Controller.Stop()` 语义对齐，`Restart()` 复用。
   顺带消除了「stop 接口返回 running」的状态竞态。
3. **`version` 白名单正则放宽**：允许 `0.62.1-beta.1` 这类带后缀的版本（`^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$`）。
4. **`ParseBinaryName` 不能按 `-` 切分**：版本号自身可能含横杠，改为剥离已知的 `-<os>-<arch>` 后缀（单元测试发现）。

### 顺带修掉的既有问题

- `distrib.EnsureFRPBinary` 缓存键不含版本号 → 指定版本永远命中旧缓存（本方案的头号阻塞点）
- `agent.Config.LoadConfig` 未读取 `DFPANEL_FRPVERSION`（注释声称支持，实际漏写）
- 补 `internal/distrib/download_test.go`：版本号防穿越、模板展开、文件名反解、版本排序、激活与缓存扫描

### Docker 版本替换（第二轮追加，2026-09-21）

- 新增 `internal/agent/dockerplat.go`：容器平台探测（`docker info` → `OSType/Architecture`，映射
  `x86_64→amd64`、`aarch64→arm64`），结果缓存 5 分钟；非 Linux 容器平台返回明确错误。
- `internal/agent/config.go`：新增 `ContainerSlotPath` / `ContainerVersionSidecar` /
  `ContainerSlotVersion` / `ContainerCachedVersions` / `ContainerBinaryInContainer`；
  容器槽位 `<binDir>/<kind>-container-<os>-<arch>` 与 process 槽位彻底分离。
- `internal/agent/runtime.go`：`Spec.MountBinary`；`startDocker` 拆出纯函数 `dockerRunArgs`
  便于无 docker 环境单测；槽位存在才挂载 + `--entrypoint`。
- `internal/agent/client.go`：`DownloadBinary` 增加 `goos/goarch`，支持按容器平台取二进制。
- `internal/agent/frpver.go`：`frpPlatform` / `slotPath` / `slotVersion` / `slotCached`
  统一按运行时分支；`ensureVersionedBinary` 按平台落盘；保留 `frpPlatformError` 把
  「docker 不可用」的原因透出到面板，而不是静默显示未知。
- `proto.HeartbeatData.Runtime`、`model.Agent.Runtime`（注册与心跳都上报）；
  `AgentFrp` 返回 `runtime` 与 `message`。
- 前端：`FrpVersionPicker` 按运行时切换说明与按钮文案（docker 下为「切换并重建容器」、
  未接管时显示「镜像自带」）；`Agents.vue` 新增「运行时」列；`Nodes.vue` / `ServerConfig.vue`
  的版本展示同步区分。
- 单测：`internal/agent/dockerplat_test.go`（平台映射 + 路径约束）、
  `internal/agent/runtime_test.go`（docker run 参数顺序：`--entrypoint` 必须在镜像名前、
  `-c 配置` 在镜像名后、空槽位文件不覆盖、frpc 用独立挂载点）。
- 重新构建：`output/dfpanel-<本机平台>-amd64` 与 `output/dfpanel-agent-{linux-amd64,linux-arm64,windows-amd64,darwin-arm64}`。

### 下载收口到设置页（第三轮追加，2026-09-24）

动机：原先 Agent 侧能自己触发下载（`CmdFrpDownload`、版本弹窗的「仅下载」、创建 Agent 时预置），
切换一个没缓存的版本时会现抓上游，十几分钟没个准信，且不同架构的 Agent 各抓各的、缓存无法复用。
现改为**面板统一备料、Agent 只切换**：

- `distrib` 新增 `CachedBinary` / `ListCachedBinaries` / `ParseBinaryPlatform` / `RemoveCachedBinary`：
  扫描面板 `bin/` 下的版本化文件（含 os/arch 反解），active 槽位（`frps`、`frps.exe`）不计入。
- `handler/frpver.go` 新增 `CacheList` / `CacheDownload` / `CacheDelete`（`GET|POST|DELETE /api/frp/cache`），
  下载按 **版本 + 类型（frps/frpc）+ 平台（os/arch）** 三个维度指定，默认取面板自身平台。
- `AgentFrpActivate` 下发前先过 `ensurePanelCache`：按 `Agent.OS/Arch` 与角色检查面板 `bin/` 里有没有对应文件
  （Agent 自己已缓存该版本则放行）；缺一份就 400，提示「请先到设置 → frp 二进制里下载」。
  Agent 未上报平台信息时不拦截，交给 Agent 自己判。
- 移除 `CmdFrpDownload` 与 `handleFrpDownload`；`AgentManageHandler.Create` 不再下发预置下载指令，
  期望版本只写进记录。
- 前端：`Settings.vue` 新增「frp 二进制（供 Agent 下载）」卡片（版本下拉 + 类型多选 + 平台下拉 + 下载按钮 + 缓存清单表格）；
  `FrpVersionPicker.vue` 在 Agent 模式下隐藏「仅下载」按钮、关闭自动预取；`Agents.vue` / `Nodes.vue`
  未接管时的文案改为「未下发」（当时还是「未下载」/「镜像自带（未接管）」，第四轮统一），
  创建提示改为「先到设置页下载再到 Agent 上切换」。
- 单测：`internal/distrib/cache_test.go`（清单扫描、平台反解、删除与越界防护）、
  `internal/handler/frpver_cache_test.go`（面板缺缓存时切换被拒、已放行场景）。
- 重新构建：`output/dfpanel-{windows,linux}-amd64`、`output/dfpanel-agent-{windows,linux}-amd64`。

### 容器底座不再内置 frp（第四轮追加，2026-09-24）

docker 运行时以前允许「没接管就沿用镜像自带的 frp」（镜像 `snowdreamtech/frps|frpc:latest`），
结果是面板上看到「镜像自带」这种没有版本信息的状态，版本也不受控。现改为**容器底座里不含 frp，
一律由面板下发**：

- 底座镜像默认换成 `alpine:3.20`（`DefaultFrpsImage` / `DefaultFrpcImage`，仍可用
  `DFPANEL_FRPS_IMAGE` / `DFPANEL_FRPC_IMAGE` 覆盖），不再依赖第三方镜像里的 frp。
- 新增 `requireDockerReady`：`startDocker` 启动前必须确认容器槽位里的二进制已就位，
  否则直接返回错误（提示去「设置 → frp 二进制」下载）；`Spec.MountErr` 把
  「docker 不可用 / 槽位没就绪」的具体原因带到面板，不再笼统报缺文件。
- `/downloads/:kind/:version/:os/:arch` 只发面板已缓存的二进制：不再替 Agent 现抓上游，
  缺什么就 404 并说明「请先在设置页下载」；`latest` 优先命中本地已缓存的最新版本。
- 安装命令与安装脚本把面板记录的期望版本一起带下去（`--frp-version` / `-FrpVersion` /
  `DFPANEL_FRPVERSION`），Agent 首次启动按它取二进制，而不是自己挑 latest；
  安装脚本里原先 `curl .../downloads/frpc/latest/...` 的预置下载（新语义下只会静默 404）已移除。
- 前端：Agent 管理 / 客户端节点 / 服务端配置的版本列改为「未下发」（不再有「镜像自带」），
  未下发时带提示说明先去设置页下载；版本弹窗的 docker 说明同步。
- 单测：`internal/agent/runtime_test.go` 增加 `TestRequireDockerReady`；
  新增 `internal/handler/install_test.go`（只发已缓存、latest 命中本地、版本白名单、
  安装命令携带期望版本）。

### 本轮未覆盖（需在有 docker 的机器上验证）

- 真实 `docker run` 挂载 + `--entrypoint` 在 alpine 底座上的运行结果；
  本机无 docker，只能靠参数构造的单测保证正确性。
- 容器内运行被挂载的 linux frp 二进制时的可执行权限（宿主文件 0755，一般没问题；
  若宿主是 Windows，Docker Desktop 的挂载权限与 exec 行为需实测）。
- 容器日志能否满足 `waitHealthy` 的关键字匹配（`docker logs` 路径现有实现已有，但未在
  「挂载外来二进制」场景下验证过 `frps started successfully` 是否照常输出）。

### 已知遗留

- `web/package.json` 的 version 仍是 `0.0.1-beta.02`，与根目录 `VERSION`（`0.0.1-beta.03`）不同步
  （实测无功能影响：全仓库无人读取它，且 `npm ci` 不校验根部 version）
- `internal/frp/config_test.go` 的 `TestBuildFrpsJSONDashboardUnspecified` 与本项目既定的
  「Dashboard 未指定 = 不启用」策略相反，改造前即为失败状态（与本次改动无关）
- 主 `VERSION` 是**构建期注入**的：改完 `VERSION` 必须重新构建，否则本机跑着的旧实例一直报旧版本号


