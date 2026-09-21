<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  agentApi,
  nodeApi,
  proxyApi,
  serverApi,
  statusLabel,
  statusType,
  visitorApi,
  type AgentInfo,
  type ConfigVersionItem,
  type FrpsServer,
  type InstallCommands,
  type NodeInfo,
  type ProxyConfig,
  type VisitorInfo,
} from '@/api'
import FieldHelp from '@/components/FieldHelp.vue'
import FrpVersionPicker from '@/components/FrpVersionPicker.vue'
import VersionHistory from '@/components/VersionHistory.vue'
import { DOC, proxyDoc } from '@/utils/docs'

const loading = ref(false)
const nodes = ref<NodeInfo[]>([])
const agents = ref<AgentInfo[]>([])
const servers = ref<FrpsServer[]>([])

// frp 版本：frpc 的版本跟随其托管 Agent（一个 Agent 上 frps 与 frpc 共用一个版本）
const frpVisible = ref(false)
const frpTarget = ref<AgentInfo | null>(null)

function frpVersionOf(agentId: number) {
  const a = agents.value.find((x) => x.id === agentId)
  if (!a) return '未知'
  if (!a.frpInstalledVersion) return a.runtime === 'docker' ? '镜像自带' : '未知'
  return a.frpInstalledVersion
}

function openFrpForAgent(agentId: number) {
  const a = agents.value.find((x) => x.id === agentId)
  if (!a) {
    ElMessage.warning('该节点未绑定 Agent，无法管理 frp 版本')
    return
  }
  frpTarget.value = a
  frpVisible.value = true
}

// 节点表单
const formVisible = ref(false)
const editingId = ref(0)
const saving = ref(false)
/** 节点表单默认值：与 frpc 的 client 段字段一一对应 */
function emptyNodeForm() {
  return {
    name: '',
    agentId: 0,
    serverId: 0,
    remark: '',
    // 身份
    clientId: '',
    user: '',
    // 连接
    serverAddr: '',
    serverPort: 0,
    natHoleStunServer: '',
    dnsServer: '',
    loginFailExit: true,
    // 鉴权
    authMethod: 'token',
    authToken: '',
    authTokenSourceType: '',
    authTokenSourcePath: '',
    authOidcClientId: '',
    authOidcClientSecret: '',
    authOidcAudience: '',
    authOidcScope: '',
    authOidcTokenEndpointUrl: '',
    authOidcAdditionalParams: '',
    // transport
    protocol: 'tcp',
    wireProtocol: '',
    dialServerTimeout: 0,
    dialServerKeepalive: 0,
    connectServerLocalIp: '',
    proxyUrl: '',
    poolCount: 0,
    tcpMux: true,
    tcpMuxKeepaliveInterval: 0,
    heartbeatInterval: 0,
    heartbeatTimeout: 0,
    // transport.tls
    tlsEnable: false,
    disableCustomTlsFirstByte: true,
    tlsCertFile: '',
    tlsKeyFile: '',
    tlsTrustedCaFile: '',
    tlsServerName: '',
    // 其他
    udpPacketSize: 0,
    metadatas: '',
  }
}

/** 从后端节点记录还原表单（null 表示未设置，回落到 frp 默认） */
function nodeFormFrom(n: NodeInfo) {
  return {
    ...emptyNodeForm(),
    name: n.name,
    agentId: n.agentId,
    serverId: n.serverId,
    remark: n.remark,
    clientId: n.clientId || '',
    user: n.user || '',
    serverAddr: n.serverAddr || '',
    serverPort: n.serverPort || 0,
    natHoleStunServer: n.natHoleStunServer || '',
    dnsServer: n.dnsServer || '',
    loginFailExit: n.loginFailExit !== false,
    authMethod: n.authMethod || 'token',
    authToken: n.authToken || '',
    authTokenSourceType: n.authTokenSourceType || '',
    authTokenSourcePath: n.authTokenSourcePath || '',
    authOidcClientId: n.authOidcClientId || '',
    authOidcClientSecret: n.authOidcClientSecret || '',
    authOidcAudience: n.authOidcAudience || '',
    authOidcScope: n.authOidcScope || '',
    authOidcTokenEndpointUrl: n.authOidcTokenEndpointUrl || '',
    authOidcAdditionalParams: n.authOidcAdditionalParams || '',
    protocol: n.protocol || 'tcp',
    wireProtocol: n.wireProtocol || '',
    dialServerTimeout: n.dialServerTimeout || 0,
    dialServerKeepalive: n.dialServerKeepalive || 0,
    connectServerLocalIp: n.connectServerLocalIp || '',
    proxyUrl: n.proxyUrl || '',
    poolCount: n.poolCount || 0,
    tcpMux: n.tcpMux !== false,
    tcpMuxKeepaliveInterval: n.tcpMuxKeepaliveInterval || 0,
    heartbeatInterval: n.heartbeatInterval || 0,
    heartbeatTimeout: n.heartbeatTimeout || 0,
    tlsEnable: !!n.tlsEnable,
    disableCustomTlsFirstByte: n.disableCustomTlsFirstByte !== false,
    tlsCertFile: n.tlsCertFile || '',
    tlsKeyFile: n.tlsKeyFile || '',
    tlsTrustedCaFile: n.tlsTrustedCaFile || '',
    tlsServerName: n.tlsServerName || '',
    udpPacketSize: n.udpPacketSize || 0,
    metadatas: n.metadatas || '',
  }
}

const form = reactive(emptyNodeForm())

// 隧道抽屉
const drawerVisible = ref(false)
const drawerNode = ref<NodeInfo | null>(null)
const proxies = ref<ProxyConfig[]>([])
const proxyLoading = ref(false)

// 隧道表单
const proxyVisible = ref(false)
const proxyEditingId = ref(0)
const proxySaving = ref(false)
const proxyForm = reactive(emptyProxyForm())

function emptyProxyForm() {
  return {
    name: '',
    type: 'tcp',
    localIp: '127.0.0.1',
    localPort: 0,
    remotePort: 0,
    customDomains: '',
    subdomain: '',
    useEncryption: false,
    useCompression: false,
    proxyProtocolVersion: '',
    bandwidthLimit: '',
    bandwidthLimitMode: '',
    natTraversalDisableAddrs: false,
    group: '',
    secretKey: '',
    allowUsers: '',
    httpUser: '',
    httpPassword: '',
    locations: '',
    hostHeaderRewrite: '',
    routeByHttpUser: '',
    multiplexer: 'httpconnect',
    healthCheckType: '',
    healthCheckTimeoutSeconds: 0,
    healthCheckMaxFailed: 0,
    healthCheckIntervalSeconds: 0,
    healthCheckPath: '',
    annotations: '',
    metadatas: '',
    pluginType: '',
    pluginConfig: '',
    enabled: true,
  }
}

// 访问端（点对点隧道的访问侧）
const visitors = ref<VisitorInfo[]>([])
const visitorLoading = ref(false)
const visitorVisible = ref(false)
const visitorEditingId = ref(0)
const visitorSaving = ref(false)
const visitorForm = reactive(emptyVisitorForm())

function emptyVisitorForm() {
  return {
    name: '',
    type: 'stcp',
    serverName: '',
    serverUser: '',
    secretKey: '',
    bindAddr: '127.0.0.1',
    bindPort: 0,
    useEncryption: false,
    useCompression: false,
    keepTunnelOpen: false,
    maxRetriesAnHour: 0,
    minRetryInterval: 0,
    fallbackTo: '',
    fallbackTimeoutMs: 0,
    natTraversalDisableAddrs: false,
    enabled: true,
  }
}

// 安装命令
const installVisible = ref(false)
const installTarget = ref<NodeInfo | null>(null)
const installOS = ref('linux')
const installRuntime = ref<'process' | 'docker'>('process')
const installMode = ref<'binary' | 'docker' | 'compose'>('binary')
const installCmds = ref<InstallCommands | null>(null)
const installLoading = ref(false)

// 配置预览 / 日志
const previewText = ref('')
const previewVisible = ref(false)
const logText = ref('')
const logVisible = ref(false)

// 历史版本（每次下发都会在 Agent 侧生成快照，可按版本回滚）
const versionVisible = ref(false)
const versionLoading = ref(false)
const versions = ref<ConfigVersionItem[]>([])
const versionsFromAgent = ref(false)
const versionNode = ref<NodeInfo | null>(null)

const agentName = (id: number) => agents.value.find((a) => a.id === id)?.name ?? (id ? `#${id}` : '未绑定')
const agentOnline = (id: number) => !!agents.value.find((a) => a.id === id)?.online
const serverName = (id: number) => servers.value.find((s) => s.id === id)?.name ?? '未关联'

/** 只有启用了 frpc 角色的 Agent 才能承载客户端节点 */
const frpcAgents = computed(() => agents.value.filter((a) => (a.roles || '').split(',').includes('frpc')))

/** frp 客户端插件类型（不同插件参数差异大，用 JSON 承载参数） */
const PLUGIN_TYPES = [
  'unix_domain_socket',
  'http_proxy',
  'socks5',
  'static_file',
  'https2http',
  'https2https',
  'http2https',
  'http2http',
  'tls2raw',
  'virtual_net',
]

const PLUGIN_PLACEHOLDER: Record<string, string> = {
  unix_domain_socket: '{ "unixPath": "/var/run/docker.sock" }',
  http_proxy: '{ "httpUser": "abc", "httpPassword": "abc" }',
  socks5: '{ "username": "abc", "password": "abc" }',
  static_file: '{ "localPath": "/var/www", "stripPrefix": "static", "httpUser": "abc", "httpPassword": "abc" }',
  https2http: '{ "localAddr": "127.0.0.1:80", "crtPath": "./a.crt", "keyPath": "./a.key", "hostHeaderRewrite": "127.0.0.1" }',
  https2https: '{ "localAddr": "127.0.0.1:443", "crtPath": "./a.crt", "keyPath": "./a.key" }',
  http2https: '{ "localAddr": "127.0.0.1:443", "hostHeaderRewrite": "127.0.0.1" }',
  http2http: '{ "localAddr": "127.0.0.1:80", "hostHeaderRewrite": "127.0.0.1" }',
  tls2raw: '{ "localAddr": "127.0.0.1:80", "crtPath": "./a.crt", "keyPath": "./a.key" }',
  virtual_net: '{}',
}

const pluginPlaceholder = computed(() => PLUGIN_PLACEHOLDER[proxyForm.pluginType] || '{}')

const installCommand = computed(() => {
  const c = installCmds.value
  if (!c) return ''
  if (installMode.value === 'binary') return c.binary
  if (installMode.value === 'docker') return c.docker
  return c.compose
})

async function load() {
  loading.value = true
  try {
    const [n, a, s] = await Promise.all([nodeApi.list(), agentApi.list(), serverApi.list()])
    nodes.value = n
    agents.value = a
    servers.value = s
  } finally {
    loading.value = false
  }
}

function openCreate() {
  if (!frpcAgents.value.length) {
    ElMessage.warning('还没有启用 frpc 角色的 Agent，请先到「Agent 管理」创建')
    return
  }
  editingId.value = 0
  Object.assign(form, emptyNodeForm(), {
    agentId: frpcAgents.value[0]?.id ?? 0,
    serverId: servers.value[0]?.id ?? 0,
  })
  formVisible.value = true
}

function openEdit(n: NodeInfo) {
  editingId.value = n.id
  Object.assign(form, nodeFormFrom(n))
  formVisible.value = true
}

async function submitForm() {
  if (!form.name.trim()) {
    ElMessage.warning('请填写节点名称')
    return
  }
  if (!form.agentId) {
    ElMessage.warning('请选择托管 Agent：客户端节点必须由 Agent 承载')
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await nodeApi.update(editingId.value, { ...form })
    } else {
      await nodeApi.create({ ...form })
    }
    formVisible.value = false
    ElMessage.success('已保存')
    await load()
  } finally {
    saving.value = false
  }
}

function openDrawer(n: NodeInfo) {
  drawerNode.value = n
  drawerVisible.value = true
  loadProxies(n.id)
  loadVisitors(n.id)
}

async function loadProxies(nodeId: number) {
  proxyLoading.value = true
  try {
    proxies.value = await proxyApi.list(nodeId)
  } finally {
    proxyLoading.value = false
  }
}

async function loadVisitors(nodeId: number) {
  visitorLoading.value = true
  try {
    visitors.value = await visitorApi.list(nodeId)
  } finally {
    visitorLoading.value = false
  }
}

function openProxyCreate() {
  proxyEditingId.value = 0
  Object.assign(proxyForm, emptyProxyForm())
  proxyVisible.value = true
}

function openProxyEdit(p: ProxyConfig) {
  proxyEditingId.value = p.id
  Object.assign(proxyForm, emptyProxyForm(), {
    name: p.name,
    type: p.type,
    localIp: p.localIp,
    localPort: p.localPort,
    remotePort: p.remotePort,
    customDomains: p.customDomains,
    subdomain: p.subdomain,
    useEncryption: p.useEncryption,
    useCompression: p.useCompression,
    proxyProtocolVersion: p.proxyProtocolVersion || '',
    bandwidthLimit: p.bandwidthLimit || '',
    bandwidthLimitMode: p.bandwidthLimitMode || '',
    natTraversalDisableAddrs: !!p.natTraversalDisableAddrs,
    group: p.group,
    secretKey: p.secretKey || '',
    allowUsers: p.allowUsers || '',
    httpUser: p.httpUser || '',
    httpPassword: p.httpPassword || '',
    locations: p.locations || '',
    hostHeaderRewrite: p.hostHeaderRewrite || '',
    routeByHttpUser: p.routeByHttpUser || '',
    multiplexer: p.multiplexer || 'httpconnect',
    healthCheckType: p.healthCheckType || '',
    healthCheckTimeoutSeconds: p.healthCheckTimeoutSeconds || 0,
    healthCheckMaxFailed: p.healthCheckMaxFailed || 0,
    healthCheckIntervalSeconds: p.healthCheckIntervalSeconds || 0,
    healthCheckPath: p.healthCheckPath || '',
    annotations: p.annotations || '',
    metadatas: p.metadatas || '',
    pluginType: p.pluginType || '',
    pluginConfig: p.pluginConfig || '',
    enabled: p.enabled,
  })
  proxyVisible.value = true
}

async function submitProxy() {
  const node = drawerNode.value
  if (!node) return
  if (!proxyForm.name.trim()) {
    ElMessage.warning('请填写隧道名称')
    return
  }
  proxySaving.value = true
  try {
    if (proxyEditingId.value) {
      await proxyApi.update(proxyEditingId.value, { ...proxyForm })
    } else {
      await proxyApi.create(node.id, { ...proxyForm })
    }
    proxyVisible.value = false
    ElMessage.success('已保存，记住点击「应用配置」才会生效')
    await loadProxies(node.id)
    await load()
  } finally {
    proxySaving.value = false
  }
}

async function removeProxy(p: ProxyConfig) {
  await ElMessageBox.confirm(`确认删除隧道「${p.name}」？`, '提示', { type: 'warning' })
  await proxyApi.remove(p.id)
  if (drawerNode.value) await loadProxies(drawerNode.value.id)
  await load()
}

// ---------- 访问端 ----------

function openVisitorCreate() {
  visitorEditingId.value = 0
  Object.assign(visitorForm, emptyVisitorForm())
  visitorVisible.value = true
}

function openVisitorEdit(v: VisitorInfo) {
  visitorEditingId.value = v.id
  Object.assign(visitorForm, emptyVisitorForm(), {
    name: v.name,
    type: v.type,
    serverName: v.serverName,
    serverUser: v.serverUser || '',
    secretKey: v.secretKey || '',
    bindAddr: v.bindAddr || '127.0.0.1',
    bindPort: v.bindPort || 0,
    useEncryption: !!v.useEncryption,
    useCompression: !!v.useCompression,
    keepTunnelOpen: !!v.keepTunnelOpen,
    maxRetriesAnHour: v.maxRetriesAnHour || 0,
    minRetryInterval: v.minRetryInterval || 0,
    fallbackTo: v.fallbackTo || '',
    fallbackTimeoutMs: v.fallbackTimeoutMs || 0,
    natTraversalDisableAddrs: !!v.natTraversalDisableAddrs,
    enabled: v.enabled,
  })
  visitorVisible.value = true
}

async function submitVisitor() {
  const node = drawerNode.value
  if (!node) return
  if (!visitorForm.name.trim()) {
    ElMessage.warning('请填写访问端名称')
    return
  }
  if (!visitorForm.serverName.trim()) {
    ElMessage.warning('请填写要访问的服务端代理名')
    return
  }
  visitorSaving.value = true
  try {
    if (visitorEditingId.value) {
      await visitorApi.update(visitorEditingId.value, { ...visitorForm })
    } else {
      await visitorApi.create(node.id, { ...visitorForm })
    }
    visitorVisible.value = false
    ElMessage.success('已保存，记住点击「应用配置」才会生效')
    await loadVisitors(node.id)
    await load()
  } finally {
    visitorSaving.value = false
  }
}

async function removeVisitor(v: VisitorInfo) {
  await ElMessageBox.confirm(`确认删除访问端「${v.name}」？`, '提示', { type: 'warning' })
  await visitorApi.remove(v.id)
  if (drawerNode.value) await loadVisitors(drawerNode.value.id)
  await load()
}

async function applyConfig(n: NodeInfo) {
  if (!n.agentId) {
    ElMessage.warning('请先为该节点绑定托管 Agent')
    return
  }
  const res = await nodeApi.apply(n.id)
  if (res.queued) {
    ElMessage.warning(res.message)
  } else if (res.rolledBack) {
    ElMessage.warning(`新配置未生效，已自动回滚上一版：${res.message}`)
  } else if (res.unverified) {
    ElMessage.warning(res.message)
  } else if (res.ok) {
    ElMessage.success(res.message)
  } else {
    ElMessage.error(res.message)
  }
  await load()
}

async function doAction(n: NodeInfo, action: 'start' | 'stop' | 'restart') {
  const res = await nodeApi[action](n.id)
  ElMessage.success(res.message ?? '操作完成')
  await load()
}

async function preview(n: NodeInfo) {
  const res = await nodeApi.preview(n.id)
  previewText.value = res.content
  previewVisible.value = true
}

async function viewLog(n: NodeInfo) {
  const res = await nodeApi.log(n.id)
  logText.value = res.content || '（暂无日志）'
  logVisible.value = true
}

async function openInstall(n: NodeInfo) {
  installTarget.value = n
  installMode.value = 'binary'
  installRuntime.value = 'process'
  installVisible.value = true
  await loadInstall()
}

async function openVersions(n: NodeInfo) {
  versionNode.value = n
  versionVisible.value = true
  versionLoading.value = true
  try {
    const res = await nodeApi.versions(n.id)
    versions.value = res.versions || []
    versionsFromAgent.value = !!res.fromAgent
    if (res.message) ElMessage.warning(res.message)
  } finally {
    versionLoading.value = false
  }
}

async function rollbackVersion(version: number) {
  const n = versionNode.value
  if (!n) return
  await ElMessageBox.confirm(
    `确认把节点「${n.name}」回滚到版本 v${version}？Agent 会应用该版本配置并做健康校验，若出现明确的 frpc 报错会自动退回当前配置。`,
    '回滚确认',
    { type: 'warning' },
  )
  const res = await nodeApi.rollback(n.id, version)
  if (res.queued || res.unverified || res.rolledBack) {
    ElMessage.warning(res.message)
  } else if (res.ok === false) {
    ElMessage.error(res.message)
  } else {
    ElMessage.success(res.message)
  }
  await load()
  if (versionNode.value) await openVersions(versionNode.value)
}

async function loadInstall() {
  const n = installTarget.value
  if (!n) return
  installLoading.value = true
  try {
    installCmds.value = await nodeApi.installCommand(n.id, installOS.value, installRuntime.value)
  } finally {
    installLoading.value = false
  }
}

async function copy(text: string) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
  ElMessage.success('已复制到剪贴板')
}

async function removeNode(n: NodeInfo) {
  await ElMessageBox.confirm(`确认删除节点「${n.name}」及其隧道配置？`, '提示', { type: 'warning' })
  await nodeApi.remove(n.id)
  ElMessage.success('已删除')
  await load()
}

onMounted(load)
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="page-header">
      <div class="page-title">客户端节点</div>
      <div class="actions">
        <el-button @click="load">刷新</el-button>
        <el-tooltip
          v-if="!frpcAgents.length"
          content="客户端节点必须由 Agent 承载：请先到「Agent 管理」创建并安装一个启用 frpc 角色的 Agent"
          placement="bottom"
        >
          <span>
            <el-button type="primary" disabled>新建节点</el-button>
          </span>
        </el-tooltip>
        <el-button v-else type="primary" @click="openCreate">新建节点</el-button>
      </div>
    </div>

    <el-alert
      v-if="!agents.length"
      type="warning"
      show-icon
      :closable="false"
      title="还没有 Agent，无法创建客户端节点"
      description="客户端节点必须由 Agent 承载。请先到「Agent 管理」创建一个启用 frpc 角色的 Agent，并在目标机器上安装，再回到本页创建节点。"
      style="margin-bottom: 16px"
    />
    <el-alert
      v-else-if="!frpcAgents.length"
      type="warning"
      show-icon
      :closable="false"
      title="没有启用 frpc 角色的 Agent"
      description="现有 Agent 都没有勾选 frpc 角色，无法承载客户端节点。请到「Agent 管理」编辑任一 Agent 并勾选 frpc。"
      style="margin-bottom: 16px"
    />

    <el-card>
      <el-table v-if="nodes.length" :data="nodes" style="width: 100%">
        <el-table-column prop="name" label="节点" min-width="150">
          <template #default="{ row }">
            <div class="name-cell">{{ row.name }}</div>
            <div v-if="row.remark" class="sub">{{ row.remark }}</div>
          </template>
        </el-table-column>
        <el-table-column label="托管 Agent" min-width="150">
          <template #default="{ row }">
            <div>{{ agentName(row.agentId) }}</div>
            <div class="sub">
              <el-tag size="small" :type="agentOnline(row.agentId) ? 'success' : 'info'">
                {{ agentOnline(row.agentId) ? '在线' : '离线' }}
              </el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="关联服务端" min-width="130">
          <template #default="{ row }">{{ serverName(row.serverId) }}</template>
        </el-table-column>
        <el-table-column label="frpc 版本" width="110">
          <template #default="{ row }">
            <el-tooltip content="frpc 版本跟随托管 Agent，点击可切换" placement="top">
              <el-button link type="primary" @click="openFrpForAgent(row.agentId)">
                {{ frpVersionOf(row.agentId) }}
              </el-button>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="连接地址" min-width="160">
          <template #default="{ row }">
            <span class="mono">{{ row.serverAddr || '(取自服务端)' }}</span>
            <span class="sub"> : {{ row.serverPort || '(默认)' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" effect="dark">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="380" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDrawer(row)">隧道</el-button>
            <el-button link type="primary" @click="applyConfig(row)">应用配置</el-button>
            <el-button link @click="doAction(row, 'restart')">重启</el-button>
            <el-button link @click="doAction(row, 'stop')">停止</el-button>
            <el-button link @click="preview(row)">配置</el-button>
            <el-button link @click="viewLog(row)">日志</el-button>
            <el-button link @click="openVersions(row)">版本</el-button>
            <el-button link @click="openInstall(row)">安装</el-button>
            <el-button link @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="removeNode(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else description="还没有客户端节点" />
    </el-card>

    <el-dialog v-model="formVisible" :title="editingId ? '编辑节点' : '新建节点'" width="620px">
      <el-form label-width="210px">
        <el-form-item label="节点名称">
          <el-input v-model="form.name" placeholder="如 内网-数据库机" />
        </el-form-item>
        <el-form-item label="托管 Agent" required>
          <el-select
            v-model="form.agentId"
            placeholder="选择承载 frpc 的 Agent"
            style="width: 100%"
            :disabled="!frpcAgents.length"
          >
            <el-option
              v-for="a in frpcAgents"
              :key="a.id"
              :value="a.id"
              :label="`${a.name}（${a.online ? '在线' : '离线'}）`"
            />
          </el-select>
          <span v-if="frpcAgents.length" class="hint">frpc 永远由 Agent 承载；这里只列出启用了 frpc 角色的 Agent</span>
          <span v-else class="hint" style="color: #e6a23c">还没有启用 frpc 角色的 Agent，请先到「Agent 管理」创建</span>
        </el-form-item>
        <el-form-item label="关联 frps">
          <el-select v-model="form.serverId" clearable placeholder="选择该节点连接的服务端" style="width: 100%">
            <el-option v-for="s in servers" :key="s.id" :value="s.id" :label="`${s.name}（${s.bindPort}）`" />
          </el-select>
          <span class="hint">留空则使用下面手填的连接地址</span>
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">服务端地址 serverAddr</span>
            <FieldHelp :doc="DOC.clientCommon" text="留空取关联 frps 的公网地址" />
          </template>
          <el-input v-model="form.serverAddr" placeholder="留空自动取 frps 公网地址 / 面板地址" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">服务端端口 serverPort</span>
            <FieldHelp :doc="DOC.clientCommon" text="0 表示取关联 frps 的 bindPort" />
          </template>
          <el-input-number v-model="form.serverPort" :min="0" :max="65535" controls-position="right" />
          <span class="hint">0 表示使用关联 frps 的 bindPort</span>
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">认证方式 auth.method</span>
            <FieldHelp :doc="DOC.clientAuth" text="token 或 oidc，默认 token" />
          </template>
          <el-select v-model="form.authMethod" style="width: 160px">
            <el-option label="token" value="token" />
            <el-option label="oidc" value="oidc" />
          </el-select>
        </el-form-item>
        <template v-if="form.authMethod === 'token'">
          <el-form-item>
            <template #label>
              <span class="lb">认证 Token</span>
              <FieldHelp :doc="DOC.clientAuth" text="留空使用 frps 的 auth.token" />
            </template>
            <el-input v-model="form.authToken" placeholder="留空使用 frps 的 auth.token" />
          </el-form-item>
          <el-form-item>
            <template #label>
              <span class="lb">Token 来源</span>
              <FieldHelp :doc="DOC.clientAuth" text="tokenSource.type，与上面的 token 互斥" />
            </template>
            <el-select v-model="form.authTokenSourceType" clearable placeholder="直接填写 token" style="width: 140px">
              <el-option label="file" value="file" />
            </el-select>
            <el-input
              v-if="form.authTokenSourceType"
              v-model="form.authTokenSourcePath"
              placeholder="token 文件路径，如 /etc/frp/token"
              style="width: 260px; margin-left: 8px"
            />
          </el-form-item>
        </template>
        <template v-else>
          <el-form-item label="OIDC ClientID">
            <el-input v-model="form.authOidcClientId" style="width: 260px" />
          </el-form-item>
          <el-form-item label="OIDC ClientSecret">
            <el-input v-model="form.authOidcClientSecret" show-password style="width: 260px" />
          </el-form-item>
          <el-form-item label="OIDC Audience">
            <el-input v-model="form.authOidcAudience" style="width: 260px" />
          </el-form-item>
          <el-form-item label="OIDC Scope">
            <el-input v-model="form.authOidcScope" style="width: 260px" />
          </el-form-item>
          <el-form-item label="OIDC TokenEndpoint">
            <el-input v-model="form.authOidcTokenEndpointUrl" placeholder="https://example.com/token" style="width: 320px" />
          </el-form-item>
          <el-form-item label="OIDC 附加参数">
            <el-input v-model="form.authOidcAdditionalParams" type="textarea" :rows="2" placeholder="每行 key=value" />
          </el-form-item>
        </template>
        <el-form-item>
          <template #label>
            <span class="lb">用户标识 user</span>
            <FieldHelp :doc="DOC.clientCommon" text="代理名会变为 {user}.{proxyName}" />
          </template>
          <el-input v-model="form.user" placeholder="user，用于限定该用户可用的端口/域名" />
        </el-form-item>

        <el-collapse class="adv-collapse">
          <el-collapse-item name="conn" title="连接（可留空）">
            <el-form-item>
              <template #label>
                <span class="lb">客户端标识 clientID</span>
                <FieldHelp :doc="DOC.clientCommon" text="clientID" />
              </template>
              <el-input v-model="form.clientId" style="width: 240px" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">STUN 服务器</span>
                <FieldHelp :doc="DOC.clientCommon" text="natHoleSTUNServer" />
              </template>
              <el-input v-model="form.natHoleStunServer" placeholder="stun.example.com:3478" style="width: 260px" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">DNS 服务器</span>
                <FieldHelp :doc="DOC.clientCommon" text="dnsServer" />
              </template>
              <el-input v-model="form.dnsServer" placeholder="8.8.8.8" style="width: 180px" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">登录失败即退出</span>
                <FieldHelp :doc="DOC.clientCommon" text="loginFailExit，关闭则持续重连" />
              </template>
              <el-switch v-model="form.loginFailExit" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">连接代理 proxyURL</span>
                <FieldHelp :doc="DOC.clientTransport" text="transport.proxyURL，仅 tcp 生效" />
              </template>
              <el-input v-model="form.proxyUrl" placeholder="http://user:pass@host:8080" style="width: 320px" />
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item name="trans" title="传输层（tcpMux 需与服务端一致）">
            <el-form-item>
              <template #label>
                <span class="lb">传输协议 protocol</span>
                <FieldHelp :doc="DOC.clientTransport" text="tcp / kcp / quic / websocket / wss，默认 tcp" />
              </template>
              <el-select v-model="form.protocol" style="width: 160px">
                <el-option label="tcp" value="tcp" />
                <el-option label="kcp" value="kcp" />
                <el-option label="quic" value="quic" />
                <el-option label="websocket" value="websocket" />
                <el-option label="wss" value="wss" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">链路协议 wireProtocol</span>
                <FieldHelp :doc="DOC.clientTransport" text="v1 / v2，留空为 v1" />
              </template>
              <el-select v-model="form.wireProtocol" clearable placeholder="v1" style="width: 120px">
                <el-option label="v1" value="v1" />
                <el-option label="v2" value="v2" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">TCP 多路复用</span>
                <FieldHelp :doc="DOC.clientTransport" text="transport.tcpMux，必须与服务端一致" />
              </template>
              <el-switch v-model="form.tcpMux" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">连接池 / 超时 / 保活</span>
                <FieldHelp :doc="DOC.clientTransport" text="poolCount / dialServerTimeout / dialServerKeepalive" />
              </template>
              <el-input-number v-model="form.poolCount" :min="0" controls-position="right" style="width: 120px" />
              <el-input-number v-model="form.dialServerTimeout" :min="0" controls-position="right" style="width: 120px; margin-left: 8px" />
              <el-input-number v-model="form.dialServerKeepalive" :min="0" controls-position="right" style="width: 130px; margin-left: 8px" />
              <span class="hint">个 / 秒 / 秒，0 使用 frp 默认</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">心跳间隔 / 超时</span>
                <FieldHelp :doc="DOC.clientTransport" text="heartbeatInterval / heartbeatTimeout" />
              </template>
              <el-input-number v-model="form.heartbeatInterval" :min="0" controls-position="right" style="width: 130px" />
              <el-input-number v-model="form.heartbeatTimeout" :min="0" controls-position="right" style="width: 130px; margin-left: 8px" />
              <span class="hint">秒，0 使用 frp 默认</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">绑定本机 IP</span>
                <FieldHelp :doc="DOC.clientTransport" text="connectServerLocalIP，仅 protocol 为 tcp / websocket 时生效" />
              </template>
              <el-input v-model="form.connectServerLocalIp" placeholder="留空不绑定" style="width: 180px" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">UDP 包大小</span>
                <FieldHelp :doc="DOC.clientCommon" text="udpPacketSize，需与服务端一致" />
              </template>
              <el-input-number v-model="form.udpPacketSize" :min="0" controls-position="right" />
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item name="tls" title="TLS 证书（transport.tls）">
            <el-form-item>
              <template #label>
                <span class="lb">TLS 加密</span>
                <FieldHelp :doc="DOC.clientTls" text="transport.tls.enable，关掉要确保服务端未强制 TLS" />
              </template>
              <el-switch v-model="form.tlsEnable" />
              <span class="hint">关闭后会显式下发 enable=false，而不是依赖 frp 默认值</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">禁用 TLS 首字节</span>
                <FieldHelp :doc="DOC.clientTls" text="transport.tls.disableCustomTLSFirstByte" />
              </template>
              <el-switch v-model="form.disableCustomTlsFirstByte" />
            </el-form-item>
            <el-form-item label="客户端证书 certFile">
              <el-input v-model="form.tlsCertFile" style="width: 280px" />
            </el-form-item>
            <el-form-item label="客户端私钥 keyFile">
              <el-input v-model="form.tlsKeyFile" style="width: 280px" />
            </el-form-item>
            <el-form-item label="可信 CA trustedCaFile">
              <el-input v-model="form.tlsTrustedCaFile" style="width: 280px" />
            </el-form-item>
            <el-form-item label="校验用 serverName">
              <el-input v-model="form.tlsServerName" style="width: 280px" />
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item name="node-meta" title="元信息">
            <el-form-item>
              <template #label>
                <span class="lb">元数据 metadatas</span>
                <FieldHelp :doc="DOC.clientCommon" text="每行 key=value" />
              </template>
              <el-input v-model="form.metadatas" type="textarea" :rows="2" placeholder="env=prod" />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
        <el-form-item label="备注">
          <el-input v-model="form.remark" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="formVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitForm">保存</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="drawerVisible" :title="`隧道配置 · ${drawerNode?.name ?? ''}`" size="72%">
      <div class="drawer-bar">
        <span class="sub">保存后需点「应用配置」才会下发（失败自动回滚）。</span>
        <div>
          <el-button @click="drawerNode && loadProxies(drawerNode.id)">刷新</el-button>
          <el-button type="primary" @click="openProxyCreate">新增隧道</el-button>
        </div>
      </div>

      <el-table v-loading="proxyLoading" :data="proxies" style="width: 100%">
        <el-table-column prop="name" label="名称" min-width="120" />
        <el-table-column prop="type" label="类型" width="80" />
        <el-table-column label="本地" min-width="130">
          <template #default="{ row }">{{ row.localIp }}:{{ row.localPort }}</template>
        </el-table-column>
        <el-table-column label="远端" min-width="150">
          <template #default="{ row }">
            <span v-if="row.remotePort">:{{ row.remotePort }}</span>
            <span v-else>{{ row.customDomains || row.subdomain || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="开关" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button link @click="openProxyEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="removeProxy(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-divider content-position="left">访问端（点对点隧道的访问侧）</el-divider>
      <div class="drawer-bar">
        <span class="sub">stcp / sudp / xtcp 需要两端配合：服务侧配隧道，访问侧在这里配访问端，缺一不可。</span>
        <div>
          <el-button @click="drawerNode && loadVisitors(drawerNode.id)">刷新</el-button>
          <el-button type="primary" @click="openVisitorCreate">新增访问端</el-button>
        </div>
      </div>
      <el-table v-loading="visitorLoading" :data="visitors" style="width: 100%">
        <el-table-column prop="name" label="名称" min-width="120" />
        <el-table-column prop="type" label="类型" width="80" />
        <el-table-column label="访问目标" min-width="160">
          <template #default="{ row }">
            <span>{{ row.serverName }}</span>
            <span v-if="row.serverUser" class="sub"> @{{ row.serverUser }}</span>
          </template>
        </el-table-column>
        <el-table-column label="本地绑定" min-width="140">
          <template #default="{ row }">{{ row.bindAddr }}:{{ row.bindPort }}</template>
        </el-table-column>
        <el-table-column label="开关" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button link @click="openVisitorEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="removeVisitor(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-drawer>

    <el-dialog v-model="visitorVisible" :title="visitorEditingId ? '编辑访问端' : '新增访问端'" width="600px">
      <el-form label-width="200px">
        <el-form-item>
          <template #label>
            <span class="lb">名称 name</span>
            <FieldHelp :doc="DOC.proxyStcp" text="同一节点内不可重名，创建后不可改名" />
          </template>
          <el-input v-model="visitorForm.name" :disabled="!!visitorEditingId" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">类型 type</span>
            <FieldHelp :doc="DOC.proxyStcp" text="需与服务侧的隧道类型一致" />
          </template>
          <el-select v-model="visitorForm.type" style="width: 160px">
            <el-option label="stcp" value="stcp" />
            <el-option label="sudp" value="sudp" />
            <el-option label="xtcp" value="xtcp" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">服务端代理名 serverName</span>
            <FieldHelp :doc="DOC.proxyStcp" text="对应服务侧隧道的 name" />
          </template>
          <el-input v-model="visitorForm.serverName" placeholder="服务侧已创建的隧道名称" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">服务方用户 serverUser</span>
            <FieldHelp :doc="DOC.proxyStcp" text="留空表示当前用户" />
          </template>
          <el-input v-model="visitorForm.serverUser" style="width: 220px" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">访问密钥 secretKey</span>
            <FieldHelp :doc="DOC.proxyStcp" text="必须与服务侧隧道一致" />
          </template>
          <el-input v-model="visitorForm.secretKey" show-password />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">本地绑定 bindAddr : bindPort</span>
            <FieldHelp :doc="DOC.proxyStcp" text="负数表示不监听，仅接受其它访问端转发" />
          </template>
          <el-input v-model="visitorForm.bindAddr" style="width: 150px" />
          <el-input-number v-model="visitorForm.bindPort" :min="-1" :max="65535" controls-position="right" style="width: 150px; margin-left: 8px" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">加密压缩</span>
            <FieldHelp :doc="DOC.featureEncryption" text="visitors[].transport" />
          </template>
          <el-checkbox v-model="visitorForm.useEncryption">加密</el-checkbox>
          <el-checkbox v-model="visitorForm.useCompression">压缩</el-checkbox>
        </el-form-item>
        <template v-if="visitorForm.type === 'xtcp'">
          <el-form-item>
            <template #label>
              <span class="lb">保持隧道常开</span>
              <FieldHelp :doc="DOC.proxyXtcp" text="keepTunnelOpen" />
            </template>
            <el-switch v-model="visitorForm.keepTunnelOpen" />
            <span class="hint">打洞失败时按下面的频率重试</span>
          </el-form-item>
          <el-form-item v-if="visitorForm.keepTunnelOpen">
            <template #label>
              <span class="lb">每小时重试 / 最小间隔</span>
              <FieldHelp :doc="DOC.proxyXtcp" text="maxRetriesAnHour / minRetryInterval" />
            </template>
            <el-input-number v-model="visitorForm.maxRetriesAnHour" :min="0" controls-position="right" style="width: 140px" />
            <el-input-number v-model="visitorForm.minRetryInterval" :min="0" controls-position="right" style="width: 140px; margin-left: 8px" />
            <span class="hint">次 / 秒</span>
          </el-form-item>
          <el-form-item>
            <template #label>
              <span class="lb">回退访问端 fallbackTo</span>
              <FieldHelp :doc="DOC.proxyXtcp" text="打洞失败时改用的访问端名称" />
            </template>
            <el-input v-model="visitorForm.fallbackTo" style="width: 200px" />
            <el-input-number v-model="visitorForm.fallbackTimeoutMs" :min="0" controls-position="right" style="width: 140px; margin-left: 8px" />
            <span class="hint">毫秒</span>
          </el-form-item>
          <el-form-item>
            <template #label>
              <span class="lb">打洞不使用本地网卡</span>
              <FieldHelp :doc="DOC.proxyXtcp" text="natTraversal.disableAssistedAddrs" />
            </template>
            <el-switch v-model="visitorForm.natTraversalDisableAddrs" />
          </el-form-item>
        </template>
        <el-form-item label="启用">
          <el-switch v-model="visitorForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="visitorVisible = false">取消</el-button>
        <el-button type="primary" :loading="visitorSaving" @click="submitVisitor">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="proxyVisible" :title="proxyEditingId ? '编辑隧道' : '新增隧道'" width="560px">
      <el-form label-width="220px">
        <el-form-item>
          <template #label>
            <span class="lb">隧道名称 name</span>
            <FieldHelp :doc="proxyDoc(proxyForm.type)" text="同一节点内不可重名，创建后不可改名" />
          </template>
          <el-input v-model="proxyForm.name" placeholder="frp 代理标识，创建后不可改名" :disabled="!!proxyEditingId" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">类型 type</span>
            <FieldHelp :doc="DOC.proxy" text="tcp/udp 走端口，http/https 走域名，stcp/sudp/xtcp 点对点" />
          </template>
          <el-select v-model="proxyForm.type" style="width: 180px">
            <el-option label="tcp" value="tcp" />
            <el-option label="udp" value="udp" />
            <el-option label="http" value="http" />
            <el-option label="https" value="https" />
            <el-option label="tcpmux" value="tcpmux" />
            <el-option label="stcp" value="stcp" />
            <el-option label="sudp" value="sudp" />
            <el-option label="xtcp" value="xtcp" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">本地地址 localIP</span>
            <FieldHelp :doc="proxyDoc(proxyForm.type)" text="默认 127.0.0.1，容器内需改为可达地址" />
          </template>
          <el-input v-model="proxyForm.localIp" placeholder="127.0.0.1" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">本地端口 localPort</span>
            <FieldHelp :doc="proxyDoc(proxyForm.type)" text="如 3306、8080" />
          </template>
          <el-input-number v-model="proxyForm.localPort" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item v-if="proxyForm.type === 'tcp' || proxyForm.type === 'udp'">
          <template #label>
            <span class="lb">远端端口 remotePort</span>
            <FieldHelp :doc="proxyDoc(proxyForm.type)" text="服务端监听此端口，流量转发到本地" />
          </template>
          <el-input-number v-model="proxyForm.remotePort" :min="1" :max="65535" />
          <span class="hint">frps 上对外暴露的端口</span>
        </el-form-item>
        <el-form-item v-if="['http', 'https', 'tcpmux'].includes(proxyForm.type)">
          <template #label>
            <span class="lb">自定义域名 customDomains</span>
            <FieldHelp :doc="DOC.featureVirtualHost" text="多个用逗号分隔" />
          </template>
          <el-input v-model="proxyForm.customDomains" placeholder="多个用逗号分隔，如 a.com,b.com" />
        </el-form-item>
        <el-form-item v-if="['http', 'https', 'tcpmux'].includes(proxyForm.type)">
          <template #label>
            <span class="lb">子域名 subdomain</span>
            <FieldHelp :doc="DOC.featureSubdomain" text="访问域名 {subdomain}.{subDomainHost}" />
          </template>
          <el-input v-model="proxyForm.subdomain" placeholder="配合 frps 的 subDomainHost 使用" />
        </el-form-item>
        <el-form-item v-if="proxyForm.type === 'tcpmux'">
          <template #label>
            <span class="lb">复用器 multiplexer</span>
            <FieldHelp :doc="DOC.proxy" text="目前仅支持 httpconnect" />
          </template>
          <el-select v-model="proxyForm.multiplexer" style="width: 180px">
            <el-option label="httpconnect" value="httpconnect" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">负载均衡分组 group</span>
            <FieldHelp :doc="DOC.proxyTcp" text="loadBalancer.group，同组同名代理分担流量" />
          </template>
          <el-input v-model="proxyForm.group" placeholder="留空表示不参与负载均衡" />
        </el-form-item>
        <el-form-item v-if="['stcp', 'sudp', 'xtcp'].includes(proxyForm.type)">
          <template #label>
            <span class="lb">访问密钥 secretKey</span>
            <FieldHelp :doc="DOC.proxyStcp" text="访问端的 secretKey 必须与此一致" />
          </template>
          <el-input v-model="proxyForm.secretKey" show-password placeholder="点对点隧道的连接密钥" />
        </el-form-item>
        <el-form-item v-if="['stcp', 'sudp', 'xtcp'].includes(proxyForm.type)">
          <template #label>
            <span class="lb">允许的用户 allowUsers</span>
            <FieldHelp :doc="DOC.proxyStcp" text="逗号分隔，* 表示允许所有用户" />
          </template>
          <el-input v-model="proxyForm.allowUsers" placeholder="留空表示同用户可访问，* 表示所有人" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">加密压缩</span>
            <FieldHelp :doc="DOC.featureEncryption" text="useEncryption / useCompression，加密与压缩" />
          </template>
          <el-checkbox v-model="proxyForm.useEncryption">加密</el-checkbox>
          <el-checkbox v-model="proxyForm.useCompression">压缩</el-checkbox>
        </el-form-item>

        <el-collapse class="adv-collapse">
          <el-collapse-item name="transport" title="传输与限速（可留空）">
            <el-form-item>
              <template #label>
                <span class="lb">PROXY 协议版本</span>
                <FieldHelp :doc="DOC.proxyTcp" text="transport.proxyProtocolVersion" />
              </template>
              <el-select v-model="proxyForm.proxyProtocolVersion" clearable placeholder="不启用" style="width: 140px">
                <el-option label="v1" value="v1" />
                <el-option label="v2" value="v2" />
              </el-select>
              <span class="hint">把客户端真实 IP 透传给本地服务，后端需支持 PROXY protocol</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">限速 bandwidthLimit</span>
                <FieldHelp :doc="DOC.proxyTcp" text="transport.bandwidthLimit，如 1MB" />
              </template>
              <el-input v-model="proxyForm.bandwidthLimit" placeholder="如 1MB，留空不限速" style="width: 160px" />
              <el-select v-model="proxyForm.bandwidthLimitMode" clearable placeholder="client" style="width: 120px; margin-left: 8px">
                <el-option label="client" value="client" />
                <el-option label="server" value="server" />
              </el-select>
              <span class="hint">限速位置，默认 client</span>
            </el-form-item>
            <el-form-item v-if="proxyForm.type === 'xtcp'">
              <template #label>
                <span class="lb">打洞不使用本地网卡</span>
                <FieldHelp :doc="DOC.proxyXtcp" text="natTraversal.disableAssistedAddrs" />
              </template>
              <el-switch v-model="proxyForm.natTraversalDisableAddrs" />
              <span class="hint">仅用 STUN 发现的公网地址，慢速 VPN 下可改善</span>
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item v-if="proxyForm.type === 'http' || proxyForm.type === 'tcpmux'" name="http" title="HTTP 鉴权与路由（仅 http / tcpmux）">
            <el-form-item>
              <template #label>
                <span class="lb">BasicAuth 用户名</span>
                <FieldHelp :doc="DOC.proxyHttp" text="httpUser" />
              </template>
              <el-input v-model="proxyForm.httpUser" style="width: 200px" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">BasicAuth 密码</span>
                <FieldHelp :doc="DOC.proxyHttp" text="httpPassword" />
              </template>
              <el-input v-model="proxyForm.httpPassword" show-password style="width: 200px" />
            </el-form-item>
            <el-form-item v-if="proxyForm.type === 'http'">
              <template #label>
                <span class="lb">路由前缀 locations</span>
                <FieldHelp :doc="DOC.proxyHttp" text="locations，逗号分隔" />
              </template>
              <el-input v-model="proxyForm.locations" placeholder="如 /、/api" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">按用户路由</span>
                <FieldHelp :doc="DOC.proxyHttp" text="routeByHTTPUser" />
              </template>
              <el-input v-model="proxyForm.routeByHttpUser" placeholder="BasicAuth 用户名等于该值时才路由到本隧道" style="width: 240px" />
            </el-form-item>
            <el-form-item v-if="proxyForm.type === 'http'">
              <template #label>
                <span class="lb">改写 Host 头</span>
                <FieldHelp :doc="DOC.proxyHttp" text="hostHeaderRewrite" />
              </template>
              <el-input v-model="proxyForm.hostHeaderRewrite" style="width: 240px" />
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item name="health" title="健康检查（探测本地服务）">
            <el-form-item>
              <template #label>
                <span class="lb">类型 healthCheck.type</span>
                <FieldHelp :doc="DOC.featureHealthCheck" text="tcp / http，留空不启用" />
              </template>
              <el-select v-model="proxyForm.healthCheckType" clearable placeholder="不启用" style="width: 140px">
                <el-option label="tcp" value="tcp" />
                <el-option label="http" value="http" />
              </el-select>
            </el-form-item>
            <el-form-item v-if="proxyForm.healthCheckType === 'http'">
              <template #label>
                <span class="lb">探测路径 path</span>
                <FieldHelp :doc="DOC.featureHealthCheck" text="healthCheck.path" />
              </template>
              <el-input v-model="proxyForm.healthCheckPath" placeholder="/status" style="width: 200px" />
            </el-form-item>
            <el-form-item v-if="proxyForm.healthCheckType">
              <template #label>
                <span class="lb">间隔 / 超时 / 失败次数</span>
                <FieldHelp :doc="DOC.featureHealthCheck" text="intervalSeconds / timeoutSeconds / maxFailed" />
              </template>
              <el-input-number v-model="proxyForm.healthCheckIntervalSeconds" :min="0" controls-position="right" style="width: 130px" />
              <el-input-number v-model="proxyForm.healthCheckTimeoutSeconds" :min="0" controls-position="right" style="width: 130px; margin-left: 8px" />
              <el-input-number v-model="proxyForm.healthCheckMaxFailed" :min="0" controls-position="right" style="width: 130px; margin-left: 8px" />
              <span class="hint">秒 / 秒 / 次，0 使用 frp 默认</span>
            </el-form-item>
          </el-collapse-item>

          <el-collapse-item name="meta" title="元信息与插件">
            <el-form-item>
              <template #label>
                <span class="lb">注解 annotations</span>
                <FieldHelp :doc="DOC.proxy" text="每行 key=value，显示在 frps dashboard" />
              </template>
              <el-input v-model="proxyForm.annotations" type="textarea" :rows="2" placeholder="owner=team-a" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">元数据 metadatas</span>
                <FieldHelp :doc="DOC.proxy" text="每行 key=value，供服务端插件读取" />
              </template>
              <el-input v-model="proxyForm.metadatas" type="textarea" :rows="2" placeholder="env=prod" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">本地插件 plugin</span>
                <FieldHelp :doc="DOC.proxy" text="配了插件后 localIP / localPort 不生效" />
              </template>
              <el-select v-model="proxyForm.pluginType" clearable placeholder="不使用插件" style="width: 220px">
                <el-option v-for="t in PLUGIN_TYPES" :key="t" :label="t" :value="t" />
              </el-select>
            </el-form-item>
            <el-form-item v-if="proxyForm.pluginType">
              <template #label>
                <span class="lb">插件参数</span>
                <FieldHelp :doc="DOC.proxy" text="JSON 对象，按插件类型填" />
              </template>
              <el-input
                v-model="proxyForm.pluginConfig"
                type="textarea"
                :rows="3"
                :placeholder="pluginPlaceholder"
              />
            </el-form-item>
          </el-collapse-item>
        </el-collapse>

        <el-form-item label="启用">
          <el-switch v-model="proxyForm.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="proxyVisible = false">取消</el-button>
        <el-button type="primary" :loading="proxySaving" @click="submitProxy">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="installVisible" title="在该节点所在机器安装 Agent" width="860px">
      <div v-if="installTarget" v-loading="installLoading">
        <el-descriptions :column="2" border size="small" style="margin-bottom: 12px">
          <el-descriptions-item label="节点">{{ installTarget.name }}</el-descriptions-item>
          <el-descriptions-item label="nodeKey">
            <span class="mono">{{ installTarget.nodeKey }}</span>
            <el-button link type="primary" @click="copy(installTarget?.nodeKey || '')">复制</el-button>
          </el-descriptions-item>
        </el-descriptions>

        <div class="install-bar">
          <el-radio-group v-model="installOS" size="small" @change="loadInstall">
            <el-radio-button value="linux">Linux / macOS</el-radio-button>
            <el-radio-button value="windows">Windows</el-radio-button>
          </el-radio-group>
          <el-radio-group v-model="installRuntime" size="small" style="margin-left: 12px" @change="loadInstall">
            <el-radio-button value="process">进程运行</el-radio-button>
            <el-radio-button value="docker">Docker 容器运行</el-radio-button>
          </el-radio-group>
        </div>

        <el-tabs v-model="installMode">
          <el-tab-pane label="一键脚本" name="binary" />
          <el-tab-pane label="docker run" name="docker" />
          <el-tab-pane label="docker compose" name="compose" />
        </el-tabs>

        <pre class="code-block">{{ installCommand }}</pre>
      </div>
      <template #footer>
        <el-button @click="installVisible = false">关闭</el-button>
        <el-button type="primary" @click="copy(installCommand)">复制命令</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="previewVisible" title="frpc.json 预览" width="720px">
      <pre class="code-block">{{ previewText }}</pre>
    </el-dialog>

    <el-dialog v-model="logVisible" title="frpc 日志" width="760px">
      <pre class="code-block">{{ logText }}</pre>
    </el-dialog>

    <el-dialog v-model="versionVisible" :title="`历史版本 · ${versionNode?.name ?? ''}`" width="920px">
      <VersionHistory
        :versions="versions"
        :loading="versionLoading"
        :from-agent="versionsFromAgent"
        @rollback="rollbackVersion"
      />
    </el-dialog>

    <FrpVersionPicker
      v-if="frpTarget"
      v-model="frpVisible"
      target="agent"
      :target-id="frpTarget.id"
      :target-name="`${frpTarget.name}（该 Agent 上的 frpc 与 frps 共用此版本）`"
      @closed="load"
    />
  </div>
</template>

<style scoped>
.actions {
  display: flex;
  align-items: center;
}

/* 高级选项折叠区：与上方表单项留出间距，内部表单项收紧标签宽度 */
.adv-collapse {
  margin: 4px 0 16px;
  border-top: 1px solid var(--el-border-color-lighter);
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.adv-collapse :deep(.el-collapse-item__header) {
  font-size: 13px;
  color: var(--el-text-color-regular);
}

.adv-collapse :deep(.el-form-item) {
  margin-bottom: 12px;
}

.adv-collapse :deep(.el-form-item__label) {
  width: 200px !important;
}

.name-cell {
  font-weight: 600;
}

.sub {
  color: #909399;
  font-size: 12px;
}

.mono {
  font-family: Consolas, Monaco, monospace;
}

.lb {
  white-space: nowrap;
}

.hint {
  margin-left: 10px;
  color: #909399;
  font-size: 12px;
}

.drawer-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
  gap: 12px;
}

.install-bar {
  display: flex;
  align-items: center;
  margin-bottom: 4px;
}

.code-block {
  margin: 0;
  max-height: 460px;
  overflow: auto;
  background: #1f2d3d;
  color: #e5e9f0;
  padding: 14px;
  border-radius: 6px;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
