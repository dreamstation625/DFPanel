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
  initStatus: () => request.get<unknown, { initialized: boolean }>('/init-status'),
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
  log: (id: number) => request.get<unknown, { content: string; running?: boolean }>(`/servers/${id}/log`),
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
  os: string
  arch: string
  hostname: string
  version: string
  status: string
  lastSeen: string | null
  remoteAddr: string
  lastError: string
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
  serverAddr: string
  serverPort: number
  authToken: string
  tlsEnable: boolean
  protocol: string
  user: string
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
  useEncryption: boolean
  useCompression: boolean
  group: string
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
  create: (data: { name: string; remark?: string; roles: string }) =>
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

export const versionApi = {
  list: (targetType: 'server' | 'node', targetId: number) =>
    request.get<unknown, ConfigVersionItem[]>('/config-versions', { params: { targetType, targetId } }),
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
