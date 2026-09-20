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
  type AgentInfo,
  type ConfigVersionItem,
  type FrpsServer,
  type InstallCommands,
  type NodeInfo,
  type ProxyConfig,
} from '@/api'
import FieldHelp from '@/components/FieldHelp.vue'
import VersionHistory from '@/components/VersionHistory.vue'
import { DOC, proxyDoc } from '@/utils/docs'

const loading = ref(false)
const nodes = ref<NodeInfo[]>([])
const agents = ref<AgentInfo[]>([])
const servers = ref<FrpsServer[]>([])

// 节点表单
const formVisible = ref(false)
const editingId = ref(0)
const saving = ref(false)
const form = reactive({
  name: '',
  agentId: 0,
  serverId: 0,
  serverAddr: '',
  serverPort: 0,
  authToken: '',
  tlsEnable: false,
  protocol: 'tcp',
  user: '',
  remark: '',
})

// 隧道抽屉
const drawerVisible = ref(false)
const drawerNode = ref<NodeInfo | null>(null)
const proxies = ref<ProxyConfig[]>([])
const proxyLoading = ref(false)

// 隧道表单
const proxyVisible = ref(false)
const proxyEditingId = ref(0)
const proxySaving = ref(false)
const proxyForm = reactive({
  name: '',
  type: 'tcp',
  localIp: '127.0.0.1',
  localPort: 0,
  remotePort: 0,
  customDomains: '',
  subdomain: '',
  useEncryption: false,
  useCompression: false,
  group: '',
  enabled: true,
})

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
  editingId.value = 0
  Object.assign(form, {
    name: '',
    agentId: agents.value[0]?.id ?? 0,
    serverId: servers.value[0]?.id ?? 0,
    serverAddr: '',
    serverPort: 0,
    authToken: '',
    tlsEnable: false,
    protocol: 'tcp',
    user: '',
    remark: '',
  })
  formVisible.value = true
}

function openEdit(n: NodeInfo) {
  editingId.value = n.id
  Object.assign(form, {
    name: n.name,
    agentId: n.agentId,
    serverId: n.serverId,
    serverAddr: n.serverAddr,
    serverPort: n.serverPort,
    authToken: n.authToken,
    tlsEnable: n.tlsEnable,
    protocol: n.protocol || 'tcp',
    user: n.user,
    remark: n.remark,
  })
  formVisible.value = true
}

async function submitForm() {
  if (!form.name.trim()) {
    ElMessage.warning('请填写节点名称')
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
}

async function loadProxies(nodeId: number) {
  proxyLoading.value = true
  try {
    proxies.value = await proxyApi.list(nodeId)
  } finally {
    proxyLoading.value = false
  }
}

function openProxyCreate() {
  proxyEditingId.value = 0
  Object.assign(proxyForm, {
    name: '',
    type: 'tcp',
    localIp: '127.0.0.1',
    localPort: 0,
    remotePort: 0,
    customDomains: '',
    subdomain: '',
    useEncryption: false,
    useCompression: false,
    group: '',
    enabled: true,
  })
  proxyVisible.value = true
}

function openProxyEdit(p: ProxyConfig) {
  proxyEditingId.value = p.id
  Object.assign(proxyForm, {
    name: p.name,
    type: p.type,
    localIp: p.localIp,
    localPort: p.localPort,
    remotePort: p.remotePort,
    customDomains: p.customDomains,
    subdomain: p.subdomain,
    useEncryption: p.useEncryption,
    useCompression: p.useCompression,
    group: p.group,
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
        <el-button type="primary" @click="openCreate">新建节点</el-button>
      </div>
    </div>

    <el-alert
      v-if="!agents.length"
      type="warning"
      show-icon
      :closable="false"
      title="还没有可用的 Agent"
      description="客户端节点必须由某个 Agent 托管。请先在「Agent 管理」中创建 Agent 并在目标服务器上安装，再回到本页创建节点。"
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
        <el-form-item label="托管 Agent">
          <el-select v-model="form.agentId" placeholder="选择承载 frpc 的 Agent" style="width: 100%">
            <el-option v-for="a in agents" :key="a.id" :value="a.id" :label="`${a.name}（${a.online ? '在线' : '离线'}）`" />
          </el-select>
          <span class="hint">frpc 永远由 Agent 承载；Agent 需具备 frpc 角色</span>
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
            <FieldHelp :doc="DOC.clientCommon" text="frpc 连接服务端的地址，留空自动取关联 frps 的公网地址或面板地址。" />
          </template>
          <el-input v-model="form.serverAddr" placeholder="留空自动取 frps 公网地址 / 面板地址" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">服务端端口 serverPort</span>
            <FieldHelp :doc="DOC.clientCommon" text="连接服务端的端口，默认 7000；填 0 表示使用关联 frps 的 bindPort。" />
          </template>
          <el-input-number v-model="form.serverPort" :min="0" :max="65535" controls-position="right" />
          <span class="hint">0 表示使用关联 frps 的 bindPort</span>
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">认证 Token</span>
            <FieldHelp :doc="DOC.clientAuth" text="auth.token，需与服务端 auth.token 一致才能鉴权通过。" />
          </template>
          <el-input v-model="form.authToken" placeholder="留空使用 frps 的 auth.token" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">传输协议 protocol</span>
            <FieldHelp :doc="DOC.clientTransport" text="与 frps 的通信协议，可选 tcp / kcp / quic / websocket，默认 tcp。" />
          </template>
          <el-select v-model="form.protocol" style="width: 160px">
            <el-option label="tcp" value="tcp" />
            <el-option label="kcp" value="kcp" />
            <el-option label="quic" value="quic" />
            <el-option label="websocket" value="websocket" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">TLS 加密</span>
            <FieldHelp :doc="DOC.clientTls" text="transport.tls.enable，是否与服务端之间启用 TLS 连接。" />
          </template>
          <el-switch v-model="form.tlsEnable" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">用户标识 user</span>
            <FieldHelp :doc="DOC.clientCommon" text="设置后代理名称会变为 {user}.{proxyName}，避免与其他用户冲突。" />
          </template>
          <el-input v-model="form.user" placeholder="user，用于限定该用户可用的端口/域名" />
        </el-form-item>
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
        <span class="sub">隧道保存后需点击节点列表的「应用配置」，由 Agent 重启 frpc 并做健康校验（失败自动回滚）。</span>
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
    </el-drawer>

    <el-dialog v-model="proxyVisible" :title="proxyEditingId ? '编辑隧道' : '新增隧道'" width="560px">
      <el-form label-width="220px">
        <el-form-item>
          <template #label>
            <span class="lb">隧道名称 name</span>
            <FieldHelp :doc="proxyDoc(proxyForm.type)" text="frp 代理标识，同一节点内不可重名，创建后不可改名。" />
          </template>
          <el-input v-model="proxyForm.name" placeholder="frp 代理标识，创建后不可改名" :disabled="!!proxyEditingId" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">类型 type</span>
            <FieldHelp :doc="DOC.proxy" text="tcp / udp 走端口映射；http / https 走虚拟主机域名；stcp / sudp / xtcp 为点对点安全隧道。" />
          </template>
          <el-select v-model="proxyForm.type" style="width: 180px">
            <el-option label="tcp" value="tcp" />
            <el-option label="udp" value="udp" />
            <el-option label="http" value="http" />
            <el-option label="https" value="https" />
            <el-option label="stcp" value="stcp" />
            <el-option label="sudp" value="sudp" />
            <el-option label="xtcp" value="xtcp" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">本地地址 localIP</span>
            <FieldHelp :doc="proxyDoc(proxyForm.type)" text="被代理的本地服务 IP，默认 127.0.0.1（容器内部署时注意改为可达地址）。" />
          </template>
          <el-input v-model="proxyForm.localIp" placeholder="127.0.0.1" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">本地端口 localPort</span>
            <FieldHelp :doc="proxyDoc(proxyForm.type)" text="被代理的本地服务端口，例如 3306、8080。" />
          </template>
          <el-input-number v-model="proxyForm.localPort" :min="1" :max="65535" />
        </el-form-item>
        <el-form-item v-if="proxyForm.type === 'tcp' || proxyForm.type === 'udp'">
          <template #label>
            <span class="lb">远端端口 remotePort</span>
            <FieldHelp :doc="proxyDoc(proxyForm.type)" text="服务端绑定的端口，访问该端口的流量会转发到本地服务。" />
          </template>
          <el-input-number v-model="proxyForm.remotePort" :min="1" :max="65535" />
          <span class="hint">frps 上对外暴露的端口</span>
        </el-form-item>
        <el-form-item v-if="proxyForm.type === 'http' || proxyForm.type === 'https'">
          <template #label>
            <span class="lb">自定义域名 customDomains</span>
            <FieldHelp :doc="DOC.featureVirtualHost" text="绑定到该代理的域名列表，多个用逗号分隔。" />
          </template>
          <el-input v-model="proxyForm.customDomains" placeholder="多个用逗号分隔，如 a.com,b.com" />
        </el-form-item>
        <el-form-item v-if="proxyForm.type === 'http' || proxyForm.type === 'https'">
          <template #label>
            <span class="lb">子域名 subdomain</span>
            <FieldHelp :doc="DOC.featureVirtualHost" text="配合服务端 subDomainHost 使用，访问域名为 {subdomain}.{subDomainHost}。" />
          </template>
          <el-input v-model="proxyForm.subdomain" placeholder="配合 frps 的 subDomainHost 使用" />
        </el-form-item>
        <el-form-item v-if="['stcp', 'sudp', 'xtcp'].includes(proxyForm.type)">
          <template #label>
            <span class="lb">分组名 group</span>
            <FieldHelp :doc="DOC.proxyStcp" text="访问端（visitor）需使用同一分组名与密钥才能访问该点对点隧道。" />
          </template>
          <el-input v-model="proxyForm.group" placeholder="访问端需使用同一分组名" />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">加密压缩</span>
            <FieldHelp :doc="DOC.featureEncryption" text="useEncryption / useCompression：对该代理与服务端之间的通信加密或压缩。" />
          </template>
          <el-checkbox v-model="proxyForm.useEncryption">加密</el-checkbox>
          <el-checkbox v-model="proxyForm.useCompression">压缩</el-checkbox>
        </el-form-item>
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
  </div>
</template>

<style scoped>
.actions {
  display: flex;
  align-items: center;
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
