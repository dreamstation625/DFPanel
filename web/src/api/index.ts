import request from './request'
import { genPassword } from '@/utils/password'

// 字段与 frp 官方服务端配置一一对应
// https://gofrp.org/zh-cn/docs/reference/server-configures/
// 数值型字段填 0 / 字符串填空表示「不写入配置」，由 frps 使用自身默认值
export interface FrpsServer {
  id: number
  name: string

  // 监听
  bindAddr: string
  bindPort: number
  kcpBindPort: number
  quicBindPort: number
  proxyBindAddr: string

  // 虚拟主机
  vhostHttpPort: number
  vhostHttpTimeout: number
  vhostHttpsPort: number
  subdomainHost: string
  custom404Page: string

  // tcpmux
  tcpmuxHttpConnectPort: number
  tcpmuxPassthrough: boolean

  // 鉴权
  authMethod: string
  authToken: string
  authAdditionalScopes: string
  /** tokenSource 与 authToken 互斥 */
  authTokenSourceType: string
  authTokenSourcePath: string
  /** 服务端插件（httpPlugins），JSON 数组文本 */
  authHttpPlugins: string
  authOidcIssuer: string
  authOidcAudience: string
  authOidcSkipExpiryCheck: boolean
  authOidcSkipIssuerCheck: boolean

  // Dashboard / webServer
  dashboardEnabled: boolean
  dashboardAddr: string
  dashboardPort: number
  dashboardUser: string
  dashboardPwd: string
  dashboardAssetsDir: string
  dashboardPprofEnable: boolean
  dashboardTlsCertFile: string
  dashboardTlsKeyFile: string

  // 端口池限制
  allowPorts: string
  maxPortsPerClient: number

  // 传输层
  tcpMux: boolean
  tcpMuxKeepaliveInterval: number
  tcpKeepalive: number
  maxPoolCount: number
  heartbeatTimeout: number
  quicKeepalivePeriod: number
  quicMaxIdleTimeout: number
  quicMaxIncomingStreams: number
  tlsForce: boolean
  tlsCertFile: string
  tlsKeyFile: string
  tlsTrustedCaFile: string
  tlsServerName: string

  // 其他顶层项
  detailedErrorsToClient: boolean
  userConnTimeout: number
  udpPacketSize: number
  natholeAnalysisDataReserveHours: number

  // SSH 隧道网关
  sshGatewayBindPort: number
  sshGatewayPrivateKeyFile: string
  sshGatewayAutoGenPrivateKeyPath: string
  sshGatewayAuthorizedKeysFile: string

  // 日志与监控
  logLevel: string
  logMaxDays: number
  logDisablePrintColor: boolean
  enablePrometheus: boolean

  // 额外 JSON 片段（顶层浅合并，可覆盖同类字段）
  extraJson: string

  // 部署模式：local = 面板本机托管；agent = 由远端 Agent 托管
  deployMode: 'local' | 'agent'
  agentId: number
  publicAddr: string

  status: string
}

export function emptyServer(): FrpsServer {
  return {
    id: 0,
    name: '默认服务端',

    bindAddr: '0.0.0.0',
    bindPort: 7000,
    kcpBindPort: 0,
    quicBindPort: 0,
    proxyBindAddr: '',

    vhostHttpPort: 80,
    vhostHttpTimeout: 0,
    vhostHttpsPort: 443,
    subdomainHost: '',
    custom404Page: '',

    tcpmuxHttpConnectPort: 0,
    tcpmuxPassthrough: false,

    authMethod: 'token',
    authToken: '',
    authAdditionalScopes: '',
    authTokenSourceType: '',
    authTokenSourcePath: '',
    authHttpPlugins: '',
    authOidcIssuer: '',
    authOidcAudience: '',
    authOidcSkipExpiryCheck: false,
    authOidcSkipIssuerCheck: false,

    // Dashboard 默认不启用；密码预置为 8 位随机值，开启后可直接使用
    dashboardEnabled: false,
    dashboardAddr: '',
    dashboardPort: 7500,
    dashboardUser: 'admin',
    dashboardPwd: genPassword(8),
    dashboardAssetsDir: '',
    dashboardPprofEnable: false,
    dashboardTlsCertFile: '',
    dashboardTlsKeyFile: '',

    allowPorts: '20000-30000',
    maxPortsPerClient: 0,

    tcpMux: true,
    tcpMuxKeepaliveInterval: 0,
    tcpKeepalive: 0,
    maxPoolCount: 0,
    heartbeatTimeout: 0,
    quicKeepalivePeriod: 0,
    quicMaxIdleTimeout: 0,
    quicMaxIncomingStreams: 0,
    tlsForce: false,
    tlsCertFile: '',
    tlsKeyFile: '',
    tlsTrustedCaFile: '',
    tlsServerName: '',

    detailedErrorsToClient: true,
    userConnTimeout: 0,
    udpPacketSize: 0,
    natholeAnalysisDataReserveHours: 0,

    sshGatewayBindPort: 0,
    sshGatewayPrivateKeyFile: '',
    sshGatewayAutoGenPrivateKeyPath: '',
    sshGatewayAuthorizedKeysFile: '',

    logLevel: 'info',
    logMaxDays: 7,
    logDisablePrintColor: false,
    enablePrometheus: false,

    extraJson: '',

    deployMode: 'local',
    agentId: 0,
    publicAddr: '',

    status: 'stopped',
  }
}

export const authApi = {
  initStatus: () => request.get<unknown, { initialized: boolean; version: string }>('/init-status'),
  init: (data: { username: string; password: string }) => request.post('/init', data),
  login: (data: { username: string; password: string }) =>
    request.post<unknown, { token: string; user: { id: number; username: string; role: string } }>(
      '/auth/login',
      data,
    ),
  profile: () => request.get<unknown, { id: number; username: string }>('/auth/profile'),
}

export interface ApplyResult {
  message: string
  ok?: boolean
  queued?: boolean
  rolledBack?: boolean
  /** 已应用但未在观察窗口内确认连通（不回滚） */
  unverified?: boolean
  version?: number
  running?: boolean
  status?: string
}

// 下发配置耗时较长（Agent 需重启并做健康探测，失败还要回滚），单独放大超时
const APPLY_TIMEOUT = 180000

export const serverApi = {
  list: () => request.get<unknown, FrpsServer[]>('/servers'),
  create: (data: FrpsServer) => request.post<unknown, FrpsServer>('/servers', data),
  update: (id: number, data: FrpsServer) => request.post<unknown, FrpsServer>(`/servers/${id}/update`, data),
  remove: (id: number) => request.post(`/servers/${id}/delete`),
  preview: (id: number) => request.get<unknown, { content: string }>(`/servers/${id}/config`),
  apply: (id: number) => request.post<unknown, ApplyResult>(`/servers/${id}/apply`, null, { timeout: APPLY_TIMEOUT }),
  start: (id: number) => request.post<unknown, ApplyResult>(`/servers/${id}/start`, null, { timeout: APPLY_TIMEOUT }),
  stop: (id: number) => request.post<unknown, ApplyResult>(`/servers/${id}/stop`, null, { timeout: APPLY_TIMEOUT }),
  restart: (id: number) => request.post<unknown, ApplyResult>(`/servers/${id}/restart`, null, { timeout: APPLY_TIMEOUT }),
  log: (id: number) =>
    request.get<unknown, { content: string; running?: boolean; message?: string; queued?: boolean }>(
      `/servers/${id}/log`,
    ),
  versions: (id: number) => request.get<unknown, VersionsResult>(`/servers/${id}/versions`),
  rollback: (id: number, version: number) =>
    request.post<unknown, ApplyResult>(`/servers/${id}/rollback`, { version }, { timeout: APPLY_TIMEOUT }),
}

/** 托管 Agent（一台机器上的守护进程，可同时承载 frps 与 frpc） */
export interface AgentInfo {
  id: number
  name: string
  remark: string
  nodeKey: string
  secret: string
  roles: string
  /** 运行时：process（直起子进程）/ docker（起容器） */
  runtime: string
  os: string
  arch: string
  hostname: string
  version: string
  status: string
  lastSeen: string | null
  remoteAddr: string
  lastError: string
  /** frp（frps/frpc）期望版本，空表示不管理 */
  frpVersion: string
  /** frp active 槽位实际版本 */
  frpInstalledVersion: string
  /** 已缓存的 frp 版本，逗号分隔 */
  frpCachedVersions: string
  frpUpdatedAt: string | null
  createdAt: string
  updatedAt: string
  online?: boolean
}

/** 客户端节点（frpc 实例，永远由某个 Agent 托管） */
export interface NodeInfo {
  id: number
  name: string
  serverId: number
  groupName: string
  remark: string
  nodeKey: string
  // 身份
  clientId: string
  user: string
  // 连接
  serverAddr: string
  serverPort: number
  natHoleStunServer: string
  dnsServer: string
  loginFailExit: boolean | null
  // 鉴权
  authMethod: string
  authToken: string
  authTokenSourceType: string
  authTokenSourcePath: string
  authOidcClientId: string
  authOidcClientSecret: string
  authOidcAudience: string
  authOidcScope: string
  authOidcTokenEndpointUrl: string
  authOidcAdditionalParams: string
  // transport
  protocol: string
  wireProtocol: string
  dialServerTimeout: number
  dialServerKeepalive: number
  connectServerLocalIp: string
  proxyUrl: string
  poolCount: number
  tcpMux: boolean | null
  tcpMuxKeepaliveInterval: number
  heartbeatInterval: number
  heartbeatTimeout: number
  // transport.tls
  tlsEnable: boolean
  disableCustomTlsFirstByte: boolean | null
  tlsCertFile: string
  tlsKeyFile: string
  tlsTrustedCaFile: string
  tlsServerName: string
  // 其他
  udpPacketSize: number
  metadatas: string

  agentId: number
  os: string
  arch: string
  version: string
  status: string
  lastSeen: string | null
  remoteAddr: string
  createdAt: string
  updatedAt: string
}

/** 隧道（frpc 代理） */
export interface ProxyConfig {
  id: number
  nodeId: number
  name: string
  type: string
  localIp: string
  localPort: number
  remotePort: number
  customDomains: string
  subdomain: string
  // 传输层
  useEncryption: boolean
  useCompression: boolean
  /** PROXY protocol 版本：v1 / v2，留空表示不用 */
  proxyProtocolVersion: string
  bandwidthLimit: string
  bandwidthLimitMode: string
  natTraversalDisableAddrs: boolean
  // 负载均衡
  group: string
  // 点对点隧道
  secretKey: string
  allowUsers: string
  // http / tcpmux
  httpUser: string
  httpPassword: string
  locations: string
  hostHeaderRewrite: string
  routeByHttpUser: string
  multiplexer: string
  // 健康检查
  healthCheckType: string
  healthCheckTimeoutSeconds: number
  healthCheckMaxFailed: number
  healthCheckIntervalSeconds: number
  healthCheckPath: string
  // 元信息与插件
  annotations: string
  metadatas: string
  pluginType: string
  pluginConfig: string

  enabled: boolean
  createdAt?: string
  updatedAt?: string
}

/** 访问端（点对点隧道的访问侧） */
export interface VisitorInfo {
  id: number
  nodeId: number
  name: string
  type: string
  serverName: string
  serverUser: string
  secretKey: string
  bindAddr: string
  bindPort: number
  useEncryption: boolean
  useCompression: boolean
  keepTunnelOpen: boolean
  maxRetriesAnHour: number
  minRetryInterval: number
  fallbackTo: string
  fallbackTimeoutMs: number
  natTraversalDisableAddrs: boolean
  enabled: boolean
  createdAt?: string
  updatedAt?: string
}

/** 一键安装命令（面板同时给出二进制与 Docker 两种形态） */
export interface InstallCommands {
  panelUrl: string
  nodeKey: string
  secret: string
  roles: string
  binary: string
  docker: string
  compose: string
}

/** Agent 指令历史 */
export interface CommandHistory {
  id: number
  agentId: number
  type: string
  targetType: string
  targetId: number
  status: string
  result: string
  version: number
  createdAt: string
  sentAt: string | null
  doneAt: string | null
}

/** 配置版本记录（面板侧记录 + Agent 本地快照合并） */
export interface ConfigVersionItem {
  id?: number
  version: number
  status: string
  message: string
  checksum: string
  createdAt: string
  /** Agent 本地快照文件大小 */
  size?: number
  /** Agent 本地快照时间（unix 秒） */
  time?: number
  /** 是否与当前生效配置一致 */
  current?: boolean
  /** Agent 本地是否保留该版本快照（可回滚） */
  onAgent?: boolean
}

/** 历史版本查询结果 */
export interface VersionsResult {
  /** true 表示来自 Agent 本地快照（可回滚） */
  fromAgent: boolean
  current?: boolean
  message?: string
  versions: ConfigVersionItem[]
}

export const agentApi = {
  list: () => request.get<unknown, AgentInfo[]>('/agents'),
  create: (data: { name: string; remark?: string; roles: string; frpVersion?: string }) =>
    request.post<unknown, AgentInfo>('/agents', data),
  update: (id: number, data: Partial<AgentInfo>) => request.post<unknown, AgentInfo>(`/agents/${id}/update`, data),
  remove: (id: number) => request.post(`/agents/${id}/delete`),
  resetToken: (id: number) => request.post<unknown, AgentInfo>(`/agents/${id}/token/reset`),
  commands: (id: number) => request.get<unknown, CommandHistory[]>(`/agents/${id}/commands`),
  installCommand: (id: number, os = 'linux', runtime = 'process') =>
    request.get<unknown, InstallCommands>(`/agents/${id}/install-command`, { params: { os, runtime } }),
}

export const nodeApi = {
  list: () => request.get<unknown, NodeInfo[]>('/nodes'),
  create: (data: Partial<NodeInfo>) => request.post<unknown, NodeInfo>('/nodes', data),
  update: (id: number, data: Partial<NodeInfo>) => request.post<unknown, NodeInfo>(`/nodes/${id}/update`, data),
  remove: (id: number) => request.post(`/nodes/${id}/delete`),
  preview: (id: number) => request.get<unknown, { content: string }>(`/nodes/${id}/config`),
  apply: (id: number) => request.post<unknown, ApplyResult>(`/nodes/${id}/apply`, null, { timeout: APPLY_TIMEOUT }),
  start: (id: number) => request.post<unknown, ApplyResult>(`/nodes/${id}/start`, null, { timeout: APPLY_TIMEOUT }),
  stop: (id: number) => request.post<unknown, ApplyResult>(`/nodes/${id}/stop`, null, { timeout: APPLY_TIMEOUT }),
  restart: (id: number) => request.post<unknown, ApplyResult>(`/nodes/${id}/restart`, null, { timeout: APPLY_TIMEOUT }),
  log: (id: number) => request.get<unknown, { content: string; running?: boolean }>(`/nodes/${id}/log`),
  versions: (id: number) => request.get<unknown, VersionsResult>(`/nodes/${id}/versions`),
  rollback: (id: number, version: number) =>
    request.post<unknown, ApplyResult>(`/nodes/${id}/rollback`, { version }, { timeout: APPLY_TIMEOUT }),
  installCommand: (id: number, os = 'linux', runtime = 'process') =>
    request.get<unknown, InstallCommands>(`/nodes/${id}/install-command`, { params: { os, runtime } }),
}

export const proxyApi = {
  list: (nodeId: number) => request.get<unknown, ProxyConfig[]>(`/nodes/${nodeId}/proxies`),
  create: (nodeId: number, data: Partial<ProxyConfig>) =>
    request.post<unknown, ProxyConfig>(`/nodes/${nodeId}/proxies`, data),
  update: (id: number, data: Partial<ProxyConfig>) => request.post<unknown, ProxyConfig>(`/proxies/${id}/update`, data),
  remove: (id: number) => request.post(`/proxies/${id}/delete`),
}

/** 访问端：点对点隧道（stcp / sudp / xtcp）的访问侧，与服务侧隧道配套使用 */
export const visitorApi = {
  list: (nodeId: number) => request.get<unknown, VisitorInfo[]>(`/nodes/${nodeId}/visitors`),
  create: (nodeId: number, data: Partial<VisitorInfo>) =>
    request.post<unknown, VisitorInfo>(`/nodes/${nodeId}/visitors`, data),
  update: (id: number, data: Partial<VisitorInfo>) =>
    request.post<unknown, VisitorInfo>(`/visitors/${id}/update`, data),
  remove: (id: number) => request.post(`/visitors/${id}/delete`),
}

export const versionApi = {
  list: (targetType: 'server' | 'node', targetId: number) =>
    request.get<unknown, ConfigVersionItem[]>('/config-versions', { params: { targetType, targetId } }),
}

// ---------- frp 版本管理与系统设置 ----------

/** 系统设置（镜像源、手填版本、面板本机 frp 版本） */
export interface SettingsValues {
  /** 下载地址模板，支持 {version} {asset} {os} {arch} 占位符 */
  frpDownloadBase: string
  /** 手填兜底版本，逗号分隔 */
  frpManualVersions: string
  /** 面板本机 frps 使用的版本，空表示不管理 */
  panelFrpVersion: string
  /** 版本列表接口，固定为 GitHub 官方，只读 */
  frpVersionApi: string
  /** 默认模板，用于「恢复默认」 */
  frpDownloadBaseDefault: string
}

/** 可选版本列表 */
export interface FrpVersionsResult {
  latest?: string
  available: string[]
  manual: string[]
  cached: string[]
  merged: string[]
  api: string
  download: string
  message?: string
}

/** 某个目标的 frp 版本状态 */
export interface FrpVersionState {
  /** 期望版本（未设置表示不管理） */
  expected: string
  /** active 槽位实际版本 */
  active: string
  /** 本地已缓存版本 */
  cached: string[]
  /** 期望与实际不一致 */
  updatable?: boolean
  /** 缓存里有比当前更新的版本 */
  outdated?: boolean
  online?: boolean
  updatedAt?: string | null
  /** Agent 运行时可，process / docker */
  runtime?: string
  /** 该目标下已托管的实例数，0 表示切换时没有任何服务会被重启 */
  instances?: number
  /** 附加说明（例如 docker 平台探测失败的原因） */
  message?: string
}

/** frp 版本操作结果 */
export interface FrpVersionResult {
  ok?: boolean
  message: string
  queued?: boolean
  rolledBack?: boolean
  active?: string
  cached?: string[]
  expect?: string
}

// 版本切换要下载 + 重启 + 逐个健康探测，失败还会回滚，超时给足
const FRP_VERSION_TIMEOUT = 420000

export const settingApi = {
  get: () => request.get<unknown, SettingsValues>('/settings'),
  save: (data: Partial<SettingsValues>) => request.post<unknown, SettingsValues>('/settings', data),
  frpVersions: () => request.get<unknown, FrpVersionsResult>('/frp-versions'),
}

export const frpVersionApi = {
  /** 面板本机 frps 的版本状态 */
  local: () => request.get<unknown, FrpVersionState>('/frp/local'),
  localDownload: (version: string) =>
    request.post<unknown, FrpVersionResult>('/frp/local/download', { version }, { timeout: FRP_VERSION_TIMEOUT }),
  localActivate: (version: string) =>
    request.post<unknown, FrpVersionResult>('/frp/local/activate', { version }, { timeout: FRP_VERSION_TIMEOUT }),
  /** 某个 Agent 的 frp 版本状态 */
  agent: (id: number) => request.get<unknown, FrpVersionState>(`/agents/${id}/frp`),
  agentDownload: (id: number, version: string) =>
    request.post<unknown, FrpVersionResult>(`/agents/${id}/frp/download`, { version }, { timeout: FRP_VERSION_TIMEOUT }),
  agentActivate: (id: number, version: string) =>
    request.post<unknown, FrpVersionResult>(`/agents/${id}/frp/activate`, { version }, { timeout: FRP_VERSION_TIMEOUT }),
}

/** 运行状态标签文案与颜色 */
export const STATUS_MAP: Record<string, { label: string; type: 'success' | 'info' | 'warning' | 'danger' }> = {
  running: { label: '运行中', type: 'success' },
  stopped: { label: '已停止', type: 'info' },
  not_installed: { label: 'frps 未安装', type: 'warning' },
  agent_offline: { label: 'Agent 离线', type: 'danger' },
  unbound: { label: '未绑定 Agent', type: 'warning' },
  online: { label: '在线', type: 'success' },
  offline: { label: '离线', type: 'info' },
  done: { label: '成功', type: 'success' },
  failed: { label: '失败', type: 'danger' },
  pending: { label: '排队中', type: 'warning' },
  sent: { label: '已下发', type: 'info' },
  applied: { label: '已生效', type: 'success' },
  unverified: { label: '已应用(未确认)', type: 'warning' },
  rolled_back: { label: '已回滚', type: 'warning' },
}

export function statusLabel(status?: string): string {
  return STATUS_MAP[status ?? '']?.label ?? status ?? '未知'
}

export function statusType(status?: string): 'success' | 'info' | 'warning' | 'danger' {
  return STATUS_MAP[status ?? '']?.type ?? 'info'
}
