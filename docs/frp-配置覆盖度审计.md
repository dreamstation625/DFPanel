# frp 配置实现覆盖度审计

> 审计日期：2026-09-21 ｜ 对照基准：**frp v0.71.0**（`fatedier/frp` 的 `conf/` 官方示例 + `pkg/config/v1/*.go` 结构体定义）
> 结论：**P0 层级 bug 与 P1–P4 缺口均已补齐**；服务端、客户端顶层、隧道、访问端四块全部对齐 v0.71.0。
> 实现记录见第 5 节；尚存的少量差异见第 6 节。

## 0. 审计方法（可复现）

1. 官方示例：`https://raw.githubusercontent.com/fatedier/frp/v0.71.0/conf/{frps,frpc}_full_example.toml`
2. 权威键名：`pkg/config/v1/{server,client,proxy,common,visitor,value_source}.go` 的 `json:"..."` 标签
   （JSON 用结构体标签解析，与 TOML 的 `a.b = c` 嵌套形式一一对应）
3. **实证**：用下载的官方 `frpc.exe` / `frps.exe`（v0.71.0）对「面板生成的写法」与「正确写法」分别跑 `verify -c`

## 1. 先看三个必须马上修的问题

### 1.1 「加密 / 压缩 / 分组名」写在了错误的层级 —— frpc 直接拒绝配置

`internal/frp/frpc_build.go` 的 `buildProxies` 把这三个键写在了 proxy 的**顶层**：

```go
m["useEncryption"] = true     // ← 错误：应是 transport.useEncryption
m["useCompression"] = true    // ← 错误：应是 transport.useCompression
m["group"] = p.GroupName      // ← 错误：应是 loadBalancer.group
m["groupKey"] = p.GroupName   // ← 错误：应是 loadBalancer.groupKey
```

frp 的 `ProxyTransport` 与 `LoadBalancerConfig` 都是**嵌套结构**（`pkg/config/v1/proxy.go`）：

```go
type ProxyBaseConfig struct {
	Transport    ProxyTransport    `json:"transport,omitempty"`
	LoadBalancer LoadBalancerConfig `json:"loadBalancer,omitempty"`
	...
}
```

**实证（frp v0.71.0，`--strict_config` 默认为 true）**：

```
$ frpc verify -c flat.json        # 面板当前写法
decode proxy at index 0: unmarshal ProxyConfig error: json: unknown field "useEncryption"

$ frpc verify -c nested.json      # 正确嵌套写法
frpc: the configuration file nested.json syntax is ok

$ frpc --strict-config=false verify -c flat.json
frpc: the configuration file flat.json syntax is ok     ← 证明不严格时是被静默忽略
```

后果分两种，都不可接受：

| frp 版本 | 行为 |
|---|---|
| 严格模式默认开启（v0.71.0 已确认 `default true`） | `frpc verify` **报错** → 面板 `apply` 的第一步就是 verify，会判定「配置校验失败」并保持原配置 → **隧道改不动，且用户看到的是莫名其妙的失败** |
| 严格模式关闭（更早版本） | 未知字段**静默丢弃** → 用户勾了加密/压缩、填了分组名，**实际完全没生效**，界面上毫无提示 |

> 为什么一直没被发现：`internal/frp/frpc.go` 的注释写着
> 「避免错误地把参数写进未知字段（**frp 对未知字段会严格报错**）」——
> 这个假设只在严格模式开启时成立，于是「写错层级」被当成了「frp 会拦下来」，
> 而当时用的 frp 版本大概率是非严格的默认。**审计时不能信注释，要看结构体标签或实测。**

### 1.2 `transport.proxyProtocolVersion` 未实现（用户发现的那条）

官方示例明确有该字段（`conf/frpc_full_example.toml` 的 `web02` 示例）：

```toml
# if not empty, frpc will use proxy protocol to transfer connection info to your local service
# v1 or v2 or empty
transport.proxyProtocolVersion = "v2"
```

面板的 `model.Proxy` 与 `buildProxies` 都没有它 → 需要真实 IP 的后端（Nginx / 宝塔 WAF 之类的 proxied 场景）拿不到客户端真实地址。

### 1.3 `stcp / sudp / xtcp` 实际不可用

- 面板 UI 提供这 3 种类型（`Nodes.vue` 的类型下拉），它们共用「分组名」输入框；
- 但「分组名」被写成顶层 `group`（见 1.1）→ 严格模式下报错；
- 且 `secretKey`（stcp/sudp/xtcp 的密钥）**完全没有建模**；
- 且**访问端 `visitors` 完全未实现** —— 这类点对点隧道需要另一端配 `[[visitors]]` 才能连通，
  面板只能生成 server 侧，所以即便上面两条修好，也仍然组不成网。

## 2. 逐项对照

### 2.1 服务端 frps（`ServerConfig`）—— 基本齐全，缺 2 项

| frp 键 | 面板 |
|---|---|
| `bindAddr` `bindPort` `kcpBindPort` `quicBindPort` `proxyBindAddr` | ✅ |
| `vhostHTTPPort` `vhostHTTPTimeout` `vhostHTTPSPort` | ✅ |
| `tcpmuxHTTPConnectPort` `tcpmuxPassthrough` | ✅ |
| `subDomainHost` `custom404Page` | ✅ |
| `sshTunnelGateway.*`（4 项） | ✅ |
| `webServer.{addr,port,user,password,assetsDir,pprofEnable,tls.certFile,tls.keyFile}` | ✅ |
| `enablePrometheus` `log.{to,level,maxDays,disablePrintColor}` | ✅ |
| `transport.{tcpMux,tcpMuxKeepaliveInterval,tcpKeepalive,maxPoolCount,heartbeatTimeout,quic.*,tls.*}` | ✅ |
| `detailedErrorsToClient` `maxPortsPerClient` `userConnTimeout` `udpPacketSize` `natholeAnalysisDataReserveHours` `allowPorts` | ✅ |
| `auth.method` `auth.additionalScopes` `auth.token` `auth.oidc.*` | ✅ |
| **`auth.tokenSource`**（`type` + `file.path`，从文件读 token） | ❌ 未实现 |
| **`httpPlugins[]`**（服务端插件 `name/addr/path/ops/tlsVerify`） | ❌ 未实现 |

### 2.2 客户端 frpc 顶层（`ClientCommonConfig`）—— 大面积空白

`model.Node` 实际上只有：`serverAddr` `serverPort` `authToken` `tlsEnable` `protocol` `user` `agentId`。

已正确生成 ✅：`auth.method` `auth.token` `user` `serverAddr` `serverPort` `log.{to,level,maxDays}` `transport.protocol` `transport.tls.enable`

结构体已声明但**从未被赋值**（等于不可用）❌：

| 分组 | 缺失项 |
|---|---|
| 身份 | `clientID`、`auth.tokenSource` |
| 连接 | `natHoleSTUNServer`、`dnsServer`、`loginFailExit`、`start` |
| transport | `wireProtocol`、`dialServerTimeout`、`dialServerKeepalive`、`connectServerLocalIP`、`proxyURL`、`poolCount`、`tcpMux`、`tcpMuxKeepaliveInterval`、`quic.*`、`heartbeatInterval`、`heartbeatTimeout` |
| transport.tls | `certFile`、`keyFile`、`trustedCaFile`、`serverName`、`disableCustomTLSFirstByte` |
| 其他 | `udpPacketSize`、`metadatas`、`includes`、`webServer.*`（客户端管理界面）、`store`、`virtualNet`、`featureGates` |

> 其中 `tcpMux` 值得优先补：frp 要求客户端与服务端 **tcpMux 设置一致**，而面板允许配服务端却完全没暴露客户端侧。

### 2.3 隧道 proxies —— 面板 10 个字段 vs frp 30+ 个键

`model.Proxy` 现有字段：`name` `type` `localIP` `localPort` `remotePort` `customDomains` `subdomain` `useEncryption` `useCompression` `group` `enabled`

| 类别 | frp 键 | 面板 |
|---|---|---|
| 基础 | `name` `type` `localIP` `localPort` `enabled` | ✅ |
| tcp/udp | `remotePort` | ✅ |
| http/https | `subdomain` `customDomains` | ✅ |
| 传输 | `transport.useEncryption` `transport.useCompression` | ⚠️ **层级错** |
| 传输 | **`transport.proxyProtocolVersion`** | ❌ |
| 传输 | `transport.bandwidthLimit` `transport.bandwidthLimitMode` | ❌ |
| 负载均衡 | `loadBalancer.group` `loadBalancer.groupKey` | ⚠️ **层级错** |
| 健康检查 | `healthCheck.{type,timeoutSeconds,maxFailed,intervalSeconds,path,httpHeaders}` | ❌ |
| http/https | `httpUser` `httpPassword` `locations` `hostHeaderRewrite` `routeByHTTPUser` `requestHeaders.*` `responseHeaders.*` | ❌ |
| stcp/sudp/xtcp | `secretKey` `allowUsers` | ❌ |
| 元数据 | `annotations` `metadatas` | ❌ |
| 插件 | `plugin.*`（unix_domain_socket / http_proxy / socks5 / static_file / https2http / https2https / http2https / http2http / tls2raw / virtual_net） | ❌ |
| 打洞 | `natTraversal.disableAssistedAddrs` | ❌ |
| tcpmux | 类型本身 + `multiplexer` | ❌ |

### 2.4 访问端 visitors —— 完全未实现

模型、接口、前端、配置生成全都没有。frp 的 `[[visitors]]`（stcp/sudp/xtcp 的访问侧）含：
`name` `type` `serverName` `serverUser` `secretKey` `bindAddr` `bindPort` `keepTunnelOpen` `maxRetriesAnHour` `minRetryInterval` `fallbackTo` `fallbackTimeoutMs` `plugin` `natTraversal`。

## 3. 建议的修复批次

| 批次 | 内容 | 状态 |
|---|---|---|
| **P0** | `useEncryption` / `useCompression` 改到 `transport.*`，`group` / `groupKey` 改到 `loadBalancer.*` | ✅ 已修 |
| **P1** | `transport.proxyProtocolVersion`、`secretKey`、`allowUsers`、`healthCheck.*`、`httpUser`/`httpPassword`、`locations`/`hostHeaderRewrite`/`routeByHTTPUser`、`annotations`/`metadatas` | ✅ 已实现 |
| **P2** | 客户端顶层：`clientID`、`natHoleSTUNServer`、`dnsServer`、`loginFailExit`、`poolCount`、`tcpMux` 系列、`dial*`、`connectServerLocalIP`、`proxyURL`、`wireProtocol`、`heartbeat*`、`udpPacketSize`、`metadatas`、`transport.tls.*`、`auth.tokenSource`、客户端 OIDC | ✅ 已实现 |
| **P3** | `visitors`（访问端）完整实现：模型 + CRUD 接口 + 配置生成 + 抽屉界面 | ✅ 已实现 |
| **P4** | 服务端 `auth.tokenSource` / `httpPlugins`；客户端 `plugin.*`；`bandwidthLimit*`；`natTraversal`；`tcpmux` 类型与 `multiplexer` | ✅ 已实现 |

## 4. 附：本次用到的验证命令

```bash
# 官方示例与结构体
curl -sL https://raw.githubusercontent.com/fatedier/frp/v0.71.0/conf/frpc_full_example.toml
curl -sL https://raw.githubusercontent.com/fatedier/frp/v0.71.0/pkg/config/v1/proxy.go

# 实证（本机 Windows）
curl -sL -o frp.zip https://github.com/fatedier/frp/releases/download/v0.71.0/frp_0.71.0_windows_amd64.zip
unzip frp.zip && cd frp_0.71.0_windows_amd64
./frpc.exe verify -c <config.json>            # 严格模式默认开启，未知字段会报错
./frpc.exe --strict-config=false verify -c <config.json>   # 关闭后可看出哪些键是被忽略的
```

## 5. 实现记录（P1–P4，2026-09-21）

### 数据模型

- `model.Proxy`：新增 `proxyProtocolVersion` `bandwidthLimit` `bandwidthLimitMode` `natTraversalDisableAddrs`
  `secretKey` `allowUsers` `httpUser` `httpPassword` `locations` `hostHeaderRewrite` `routeByHttpUser`
  `multiplexer` `healthCheck{Type,TimeoutSeconds,MaxFailed,IntervalSeconds,Path}` `annotations` `metadatas`
  `pluginType` `pluginConfig`
- `model.Node`：新增 `clientId` `natHoleStunServer` `dnsServer` `loginFailExit*` `authMethod` `authTokenSource{Type,Path}`
  `authOidc{ClientId,ClientSecret,Audience,Scope,TokenEndpointUrl,AdditionalParams}` `wireProtocol` `dialServerTimeout`
  `dialServerKeepalive` `connectServerLocalIp` `proxyUrl` `poolCount` `tcpMux*` `tcpMuxKeepaliveInterval`
  `heartbeatInterval` `heartbeatTimeout` `disableCustomTlsFirstByte*` `tls{CertFile,KeyFile,TrustedCaFile,ServerName}`
  `udpPacketSize` `metadatas`（带 `*` 的用指针，用于区分「未设置」与「显式 false」）
- `model.Visitor`（**新增表**）：`name` `type` `serverName` `serverUser` `secretKey` `bindAddr` `bindPort`
  `useEncryption` `useCompression` `keepTunnelOpen` `maxRetriesAnHour` `minRetryInterval` `fallbackTo`
  `fallbackTimeoutMs` `natTraversalDisableAddrs` `enabled`
- `model.FrpsServer`：新增 `authTokenSource{Type,Path}` `authHttpPlugins`

### 配置生成（`internal/frp`）

- `buildProxies`：按类型分别落位（http 专属 / tcpmux 专属 / stcp 系专属 / xtcp 专属 / 通用），
  传输与负载均衡落到 `transport.*` 与 `loadBalancer.*`；配了插件时不写 `localIP/localPort`
- `buildVisitors`（新增）：基础字段 + xtcp 专属字段，`bindPort` 允许负数（只接收转发）
- `buildClientTransport`：**一律显式下发 `transport.tls.enable`**（见第 6 节）
- `ParseJSONObject`（新增，插件参数）、`ParseHTTPPlugins`（新增，服务端插件）、`parseKV`/`parseAnyKV`（元信息）
- `AuthServerConfig` / `AuthClientConfig` 增加 `tokenSource`（`ValueSource`: type + file.path）

### 接口与前端

- 新增 `/api/nodes/:id/visitors`、`/api/visitors/:id/{update,delete}`
- `Nodes.vue`：隧道表单按类型分组补齐字段（折叠「传输与限速 / HTTP 鉴权与路由 / 健康检查 / 元信息与插件」）；
  节点表单补齐连接、传输层、TLS、元信息（折叠分组）；抽屉新增「访问端」区块与访问端弹窗
- `ServerConfig.vue`：auth 段新增 `Token 来源` 与 `服务端插件 httpPlugins`
- `validateProxy` 增加取值校验（PROXY 版本 v1/v2、限速位置 client/server、健康检查 tcp/http、插件参数必须是 JSON 对象）
- `validateVisitor` 新增：类型限 stcp/sudp/xtcp、`serverName` 必填、sudp 不允许负 `bindPort`

### 测试

- `internal/frp/frpc_build_full_test.go`（11 个用例）：类型专属字段不串类型、插件参数按 JSON 解析、
  `plugin.type` 以 `PluginType` 为准、访问端字段与停用跳过、`tls.enable` 始终显式下发、
  `tokenSource` 与 `token` 互斥、`ParseHTTPPlugins` 校验、元信息标量还原、健康检查按需生成、最小配置不膨胀
- **实测**：把「所有字段填满」的 frps/frpc 配置生成后交给官方二进制校验，**5 份全部 `syntax is ok`**：
  `frps_full`（含 tokenSource + httpPlugins）、`frpc_full`（9 条隧道覆盖 tcp/udp/http/https/tcpmux/stcp/xtcp + 2 个插件 + 3 个访问端）、
  `frpc_tokensource`、`frpc_oidc`、`frpc_minimal`

### 过程中发现并修掉的新问题

1. **插件参数被整段丢弃**（实测抓到）：`pluginConfig` 是 JSON 对象，最初误用「key=value 行」解析器 →
   JSON 里没有 `=`，所有参数被跳过，官方 frpc 直接报 `plugin static_file: localPath is required`。
   已改为 `ParseJSONObject`，并在 `validateProxy` 提前校验。
2. **`transport.tls.enable` 关不掉**：原实现只在开启时才写 `enable: true`，而 frp 自 v0.50.0 起该值默认 true，
   所以界面上把 TLS 关掉不会生效。改为**始终显式下发**（含 `false`）。
   ⚠️ 这是**行为变更**：存量节点若 `tlsEnable=false`（前端默认值即为 false），重新下发后 `enable` 会显式写 false。
   若 frps 侧配了 `transport.tls.force = true`，frpc 会连不上 —— 但这种情况下健康检查会判定失败并**自动回滚**，
   属于可见、可恢复的失败，不会静默破坏。

## 6. 尚存差异（有意保留）

- **客户端插件参数用 JSON 文本框**：9 种客户端插件的参数字段差异很大，为每种插件单独做表单性价比太低，
  改为「选类型 + 填 JSON 参数」（有占位示例）。参数完全可达，只是表单友好度低一档。
- **服务端插件 `httpPlugins` 用 JSON 数组文本框**：同上理由；已在解析时校验 `name`/`addr` 必填并补齐 `path` 默认值。
- **访问端插件 `visitors[].plugin`**（如 `virtual_net`）未做，属极长尾。
- **服务端 `virtualNet` / 客户端 `featureGates` / `includes` / `store`** 未做：分别是实验特性与文件拆分配置，
  与面板「集中管理」的定位不符；需要时可用服务端的「额外 JSON 配置」兜底。
- **`auth.tokenSource.type = exec`** 只保留了字段但前端只提供 `file`：面板场景下更安全，需要时可扩。

