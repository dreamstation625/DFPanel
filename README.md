<h1 align="center">DFPanel</h1>

<p align="center"><strong>一个面板，统一管理 frps、frpc 和隧道。</strong></p>

<p align="center">
  基于 <a href="https://github.com/fatedier/frp">frp</a> 的可视化管理面板。面板独立部署，远端 Agent 托管服务端与客户端。
</p>

<p align="center">
  <img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&amp;logoColor=white" />
  <img alt="Vue 3.5" src="https://img.shields.io/badge/Vue-3.5-42B883?logo=vuedotjs&amp;logoColor=white" />
  <img alt="Docker Ready" src="https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker&amp;logoColor=white" />
  <img alt="AGPL-3.0" src="https://img.shields.io/badge/License-AGPL--3.0-blue" />
</p>

<p align="center">
  <a href="#主要功能">功能</a> · <a href="#技术栈与版本">技术栈</a> · <a href="#安装">安装</a> · <a href="#卸载">卸载</a> · <a href="#文档与源码">文档</a>
</p>

## 主要功能

- **服务端与客户端管理**：创建多个 frps 服务端和 frpc 节点，配置 TCP、UDP、HTTP、HTTPS、STCP、SUDP、XTCP 等隧道。
- **分离部署**：服务端 Agent 与客户端 Agent 各自只托管一个 frps 服务端或 frpc 节点；支持进程和 Docker 运行时，同机可安装多个独立 Agent。
- **配置下发与恢复**：面板保存配置版本，支持查看和回滚。Agent 启动时可从面板恢复缺失的、最近成功应用的配置，不覆盖已有的非空本地配置。
- **自动启动**：面板和 Agent 重启后按自动启动开关恢复实例；被手动停止的实例不会自动拉起。
- **frp 版本管理**：面板准备并分发 frp 二进制；同一 Agent 上的 frps 与 frpc 共用该 Agent 的 frp 版本。
- **程序版本检测**：面板和 Agent 分别与 GitHub Releases 中的发布版本比较；正式版只检测正式版更新，预发布版也检测后续预发布版。检测仅提示可更新并提供发布页链接，不会自动升级。

## 技术栈与版本

| 部分 | 技术与版本 | 用途 |
| --- | --- | --- |
| 面板后端与 Agent | Go 1.26、Gin 1.12、GORM 1.31 | API、Agent 通信、配置和实例管理 |
| 数据库 | SQLite（`github.com/glebarez/sqlite` 1.11） | 面板数据、配置版本和指令队列 |
| 前端 | Vue 3.5、TypeScript 5.6、Element Plus 2.8、Vue Router 4.4 | 管理界面 |
| 前端构建与请求 | Vite 6、Axios 1.7 | 构建静态资源、调用 API |
| 部署 | Go 内嵌前端资源、Docker / Docker Compose | 单文件面板或容器部署 |

版本依据为 [go.mod](go.mod)、[web/package.json](web/package.json) 和 Dockerfile。源码构建需要 Go 1.26；前端构建建议使用 Node.js 22（与 Dockerfile、CI 一致）。使用已构建的二进制或镜像时，运行机器无需安装 Go 和 Node.js。

## 工作方式

```mermaid
flowchart LR
    Browser["浏览器"] -->|HTTP| Panel["DFPanel 面板<br/>Web UI · API · SQLite"]
    Agent["远端 Agent<br/>frps / frpc"] -->|主动连接| Panel
    Panel -->|配置与指令| Agent
    Agent --> Runtime["frp 实例<br/>进程或 Docker 容器"]
```

面板负责保存配置、分发二进制和下发指令；Agent 负责目标机器上的配置文件及实例运行。新建服务端或节点后，需要在面板点击一次「应用配置」。此后如果 Agent 本地配置文件缺失，启动时会尝试拉取最近成功应用的版本。

## 安装

> [!TIP]
> 首次部署建议先启动面板并完成初始化，再在「Agent 管理」中创建 Agent，最后到目标机器执行面板生成的安装命令。

### 面板部署脚本

Linux 使用 [deploy-panel.sh](deploy-panel.sh)，选择 Docker（生成 Compose 配置）或二进制（注册 `dfpanel.service`）：

```bash
sudo bash deploy-panel.sh --mode docker --public-url http://192.168.1.10:7226
sudo bash deploy-panel.sh --mode binary --public-url http://192.168.1.10:7226
# 使用已经准备好的二进制：
sudo bash deploy-panel.sh --mode binary --public-url http://192.168.1.10:7226 --binary ./output/dfpanel-linux-amd64
```

Windows 使用 [deploy-panel.ps1](deploy-panel.ps1)。二进制部署会注册开机启动的 **DFPanel Windows 服务**；选择 Docker 时，脚本仅显示手动部署步骤：

```powershell
# 请在管理员 PowerShell 中执行
.\deploy-panel.ps1 -Mode Binary -PublicUrl http://192.168.1.10:7226
.\deploy-panel.ps1 -Mode Binary -PublicUrl http://192.168.1.10:7226 -BinaryPath .\output\dfpanel-windows-amd64.exe
.\deploy-panel.ps1 -Mode Docker
```

二进制模式默认下载同目录 `VERSION` 对应的 GitHub Release（包括 Pre-release）中的面板程序及 Agent 全平台包，并校验 SHA256。Release 不存在时，可使用本地 `output/`，有 Go 源码时部署脚本也能构建 Agent 全平台包；亦可通过 `--agent-bundle` / `-AgentBundle` 指定本地包。可通过 `--version` / `-Version` 指定面板版本。Linux 的数据目录为 `/var/lib/dfpanel`，Windows 为 `%ProgramData%\DFPanel\data`；重复部署不会删除数据。Windows 服务日志写入数据目录的 `panel.log`。

Windows 脚本会检查二进制是否支持 Windows 服务。若当前 Release 是旧版本，请先用 `build.ps1` 构建当前源码，再用 `-BinaryPath` 指定生成的程序。

### Docker Compose

1. 修改 [docker-compose.yml](docker-compose.yml) 中的 `DFPANEL_PUBLIC_URL`，填入 Agent 能访问的面板地址。
2. 启动面板：

   ```bash
   docker compose up -d
   ```

3. 打开 `http://<面板地址>:7226`，按初始化页面创建管理员账号。
4. 在「Agent 管理」按服务端或客户端分别新建 Agent，每个 Agent 绑定一个服务端或节点，再复制页面生成的安装命令到目标机器执行。

也可以修改 [docker-compose.agent.yml](docker-compose.agent.yml) 中的面板地址，在 `.env` 中设置 `DFPANEL_NODE_KEY`、`DFPANEL_NODE_SECRET` 和 `DFPANEL_ROLES` 后启动 Agent：

```bash
docker compose -f docker-compose.agent.yml up -d
```

Compose 示例默认使用 Linux host 网络。同机运行多个 Agent 时，为每个 Agent 使用独立的 nodeKey、数据目录和 Compose 项目名。使用端口映射时，请按实际 frps 监听端口、Dashboard 端口及隧道端口调整映射。Agent 的 `DFPANEL_RUNTIME` 默认为 `process`；选择 `docker` 时还需挂载 Docker socket，示例见 [Agent 部署文档](docs/agent-架构与部署.md)。请保留面板和 Agent 的数据目录；其中的 `instance-id` 用于防止同一 Agent 身份在多处重复安装。

### 二进制

如果项目已发布对应平台的二进制，可从 [Releases](https://github.com/dreamstation625/DFPanel/releases) 下载；也可以在源码目录构建：

```bash
# Linux / macOS：面板和 Agent 分别构建
./build.sh
./build.sh --agent
```

```powershell
# Windows PowerShell：面板和 Agent 分别构建
.\build.ps1
.\build.ps1 -Agent
```

构建产物位于 `output/`，文件名含操作系统与架构。以 Linux amd64 面板为例：

```bash
./output/dfpanel-linux-amd64 -listen :7226 -data ./data -public-url http://<面板地址>:7226
```

首次访问面板完成初始化。安装远端 Agent 时，使用「Agent 管理 → 安装命令」生成的命令：Linux / macOS 脚本注册系统服务，Windows 脚本注册开机启动的计划任务。安装命令包含 Agent 密钥，请在目标机器上执行并妥善保管。

使用本次更新构建的 Docker 面板镜像已内置 Linux、Windows 和 macOS 安装脚本支持的 Agent 二进制，直接执行「Agent 管理 → 安装命令」即可。Agent 版本取自镜像构建时的 `VERSION.agent`；升级时拉取新面板镜像并重新创建容器。

通过 `deploy-panel.sh` 或 `deploy-panel.ps1` 安装的二进制面板会自动准备七种平台的 Agent：Linux amd64、arm64、arm，Windows amd64、386，以及 macOS amd64、arm64。需要本地包时，可运行：

```bash
./build.sh --agent-bundle
```

Windows 使用 `.\build.ps1 -AgentBundle`。直接运行面板二进制而不使用部署脚本时，先用 `dfpanel -prepare-agent-bundle <包路径> <数据目录>` 校验并准备，再用 `dfpanel -activate-agent-bundle <数据目录> <面板版本>` 激活。旧版手动放在数据目录 `bin/` 的文件仍可读取；Docker 镜像内置的 Agent 优先，避免旧文件遮盖新镜像。
本地二进制部署可将生成的包传给 `deploy-panel.sh --agent-bundle <包路径>` 或 `deploy-panel.ps1 -AgentBundle <包路径>`。包名使用面板发布版本，包内清单独立记录 `VERSION.agent`；面板 `beta.19` 可分发 Agent `beta.18`。

若安装脚本返回 404，请先确认面板已更新并已准备 Agent 全平台包；详细说明见 [Agent 部署文档](docs/agent-架构与部署.md)。

### 常用配置

| 面板参数 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `-listen` | `DFPANEL_LISTEN` | `:7226` | 面板监听地址 |
| `-data` | `DFPANEL_DATA_DIR` | `./data` | 面板数据库、配置和二进制缓存目录 |
| `-public-url` | `DFPANEL_PUBLIC_URL` | 按请求推断 | Agent 能访问的面板地址 |
| `-agent-image` | `DFPANEL_AGENT_IMAGE` | `dreamstation625/dfpanel-agent:latest` | 面板生成 Docker 安装命令时使用的镜像 |

Agent 常用环境变量为 `DFPANEL_URL`、`DFPANEL_NODE_KEY`、`DFPANEL_NODE_SECRET`、`DFPANEL_ROLES`、`DFPANEL_RUNTIME` 和 `DFPANEL_DATA_DIR`。角色只能设为 `frps` 或 `frpc`。完整参数和部署示例见 [Agent 部署文档](docs/agent-架构与部署.md)。

此前预发布版本若已将一个 Agent 绑定多个对象，本次单绑定数据库索引不会自动改写这些记录。升级前请备份面板数据；不需要旧数据时，使用全新数据目录初始化面板，再为每个服务端或节点分别创建并安装 Agent。

## 卸载

| 安装方式 | 卸载命令或操作 | 数据处理 |
| --- | --- | --- |
| Docker Compose 面板 | `docker compose down` | `./data` 目录保留 |
| Linux 部署脚本的 Docker 面板 | `docker compose -f /opt/dfpanel/compose.yml down` | `/var/lib/dfpanel` 保留 |
| Docker Compose Agent | 在对应 Compose 项目目录执行 `docker compose -f docker-compose.agent.yml down` | `/opt/dfpanel-agent/<nodeKey>` 保留 |
| `docker run` | 面板执行 `docker rm -f dfpanel`；Agent 执行 `docker rm -f dfpanel-agent-<nodeKey>` | 挂载的数据目录保留 |
| 二进制面板 | 停止面板进程并删除面板程序 | `-data` 指定的目录保留，需自行决定是否删除 |
| Linux 部署脚本的二进制面板 | `sudo systemctl disable --now dfpanel`，删除 `/etc/systemd/system/dfpanel.service` 和 `/usr/local/bin/dfpanel`，再执行 `sudo systemctl daemon-reload` | `/var/lib/dfpanel` 保留 |
| Windows 部署脚本的二进制面板 | 管理员 PowerShell 执行 `Stop-Service DFPanel`、`sc.exe delete DFPanel`，服务退出后执行 `Remove-Item (Join-Path $env:ProgramData 'DFPanel\dfpanel.exe')` | `%ProgramData%\DFPanel\data` 保留 |
| 脚本安装的 Agent | 运行下方对应平台的卸载脚本 | 默认保留 Agent 数据目录；`--purge` / `-Purge` 连数据一起删除 |

Linux / macOS：

```bash
curl -fsSL http://<面板地址>:7226/install.sh -o install.sh
sudo bash install.sh --uninstall --instance <安装时的 nodeKey>
# 确定不再需要该 Agent 数据时：sudo bash install.sh --uninstall --instance <安装时的 nodeKey> --purge
```

Windows（管理员 PowerShell）：

```powershell
Invoke-WebRequest -Uri 'http://<面板地址>:7226/install.ps1' -OutFile .\install.ps1
.\install.ps1 -Uninstall -Instance <安装时的 nodeKey>
# 确定不再需要该 Agent 数据时：.\install.ps1 -Uninstall -Instance <安装时的 nodeKey> -Purge
```

Agent 卸载脚本会停止其托管的 frp 实例并注销系统服务或计划任务。卸载后，可在面板的「Agent 管理」中删除对应记录。

## 文档与源码

| 入口 | 内容 |
| --- | --- |
| [Agent 架构与部署](docs/agent-架构与部署.md) | 分离部署、安装、运行时与配置回滚 |
| [frp 版本管理方案](docs/frp-版本管理方案.md) | 二进制缓存、下载和版本切换 |
| [项目源码](https://github.com/dreamstation625/DFPanel) | 源码与发布记录 |
| [问题反馈](https://github.com/dreamstation625/DFPanel/issues) | 使用问题与功能建议 |

## 来源与许可

本项目的界面管理思路受到 [VaalaCat/frp-panel](https://github.com/VaalaCat/frp-panel) 启发；隧道能力基于 [fatedier/frp](https://github.com/fatedier/frp)，配置项可查阅 [frp 官方文档](https://gofrp.org/zh-cn/docs/)。本项目按仓库中的 [GNU AGPLv3 许可证](LICENSE) 发布。

## 项目支持

本项目由 **WorkBuddy** 支持。使用问题和功能建议可通过 [GitHub Issues](https://github.com/dreamstation625/DFPanel/issues) 提交。
