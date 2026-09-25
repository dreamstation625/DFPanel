<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  agentApi,
  programVersionApi,
  settingApi,
  statusLabel,
  statusType,
  type AgentInfo,
  type CommandHistory,
  type InstallCommands,
  type ProgramVersionStatus,
} from '@/api'
import FrpVersionPicker from '@/components/FrpVersionPicker.vue'

const loading = ref(false)
const agents = ref<AgentInfo[]>([])
const versionStatuses = ref<Record<number, ProgramVersionStatus>>({})
const versionChecking = ref(false)
const versionCheckError = ref('')
let versionPoll: ReturnType<typeof setTimeout> | undefined
let mounted = false

async function checkAgentVersions(refresh = false) {
  if (versionPoll) clearTimeout(versionPoll)
  versionChecking.value = true
  try {
    const result = await programVersionApi.check(refresh)
    if (!mounted) return
    versionStatuses.value = Object.fromEntries(result.agents.map((item) => [item.id, item.status]))
    versionCheckError.value = result.agents.find((item) => item.status.error)?.status.error || ''
    versionChecking.value = result.checking
    if (result.checking) versionPoll = setTimeout(() => void checkAgentVersions(), 2000)
  } catch {
    if (!mounted) return
    versionCheckError.value = '无法连接面板版本检测接口'
    versionChecking.value = false
  }
}

// 每个 Agent 独立管理所绑定 frps 或 frpc 的 frp 版本。
const frpVisible = ref(false)
const frpTarget = ref<AgentInfo | null>(null)
/** 新建时没选 frp 版本：先弹版本选择器，关掉之后再把安装命令给出来 */
const pendingInstall = ref<AgentInfo | null>(null)

function openFrp(a: AgentInfo) {
  frpTarget.value = a
  frpVisible.value = true
}

/** frp 版本弹窗关闭后：刷新列表；新建流程里接着弹安装命令 */
function onFrpClosed() {
  load()
  const a = pendingInstall.value
  pendingInstall.value = null
  // 同样等版本弹窗收起来再弹安装命令
  if (a) window.setTimeout(() => openInstall(a), 200)
}

/** frp 版本展示：优先显示实际生效版本，面板还没下发过就是「未下发」（容器底座里没有 frp） */
function frpLabel(a: AgentInfo) {
  if (!a.frpInstalledVersion) return '未下发'
  return a.frpInstalledVersion
}

/** 期望版本与生效版本不一致，或缓存里有更新版本时提示可更新 */
function frpNeedsUpdate(a: AgentInfo) {
  if (a.frpVersion && a.frpInstalledVersion && a.frpVersion !== a.frpInstalledVersion) return true
  if (!a.frpInstalledVersion) return false
  return (a.frpCachedVersions || '')
    .split(',')
    .filter(Boolean)
    .some((v) => compareSemver(v, a.frpInstalledVersion) > 0)
}

function compareSemver(x: string, y: string) {
  const xs = x.split('.').map((n) => parseInt(n, 10) || 0)
  const ys = y.split('.').map((n) => parseInt(n, 10) || 0)
  for (let i = 0; i < 3; i += 1) {
    if ((xs[i] || 0) !== (ys[i] || 0)) return (xs[i] || 0) > (ys[i] || 0) ? 1 : -1
  }
  return 0
}

const createVisible = ref(false)
const createForm = reactive({ name: '', remark: '', roles: 'frpc', frpVersion: '' })

// 新建时可以先挑好 frp 版本：只记为期望版本，二进制要在设置页按平台下载后再来切换
const frpVersionOptions = ref<string[]>([])
const frpVersionLatest = ref('')

async function loadFrpVersions() {
  try {
    const v = await settingApi.frpVersions()
    frpVersionOptions.value = v.merged || []
    frpVersionLatest.value = v.latest || ''
  } catch {
    frpVersionOptions.value = []
    frpVersionLatest.value = ''
  }
}
const creating = ref(false)

const editVisible = ref(false)
const editForm = reactive({ id: 0, name: '', remark: '', roles: 'frpc' })
const editing = ref(false)

const installVisible = ref(false)
const installTarget = ref<AgentInfo | null>(null)
const installOS = ref('linux')
const installRuntime = ref<'process' | 'docker'>('process')
const installMode = ref<'binary' | 'docker' | 'compose'>('binary')
const installCmds = ref<InstallCommands | null>(null)
const installLoading = ref(false)

const historyVisible = ref(false)
const historyTitle = ref('')
const history = ref<CommandHistory[]>([])

const onlineCount = computed(() => agents.value.filter((a) => a.online).length)

const currentCommand = computed(() => {
  const c = installCmds.value
  if (!c) return ''
  if (installMode.value === 'binary') return c.binary
  if (installMode.value === 'docker') return c.docker
  return c.compose
})

function fmtTime(v?: string | null) {
  if (!v) return '—'
  return new Date(v).toLocaleString()
}

async function load() {
  loading.value = true
  try {
    agents.value = await agentApi.list()
    void checkAgentVersions()
  } finally {
    loading.value = false
  }
}

function openCreate() {
  createForm.name = ''
  createForm.remark = ''
  createForm.roles = 'frpc'
  createForm.frpVersion = ''
  createVisible.value = true
  loadFrpVersions()
}

async function submitCreate() {
  creating.value = true
  try {
    const pickedVersion = createForm.frpVersion
    const created = await agentApi.create({
      name: createForm.name || `Agent-${agents.value.length + 1}`,
      remark: createForm.remark,
      roles: createForm.roles,
      frpVersion: pickedVersion || undefined,
    })
    createVisible.value = false
    await load()
    if (pickedVersion) {
      ElMessage.success(`Agent 已创建，期望 frp ${pickedVersion}（该版本要先在设置页下载，再到这里切换）`)
      openInstall(created)
      return
    }
    // 没选版本：先把 frp 版本定下来，关掉弹窗再给安装命令
    ElMessage.info('Agent 已创建，先选一个 frp 版本')
    pendingInstall.value = created
    // 等创建弹窗的收起动画走完再开版本弹窗，避免两层叠在一起
    window.setTimeout(() => openFrp(created), 200)
  } finally {
    creating.value = false
  }
}

function openEdit(a: AgentInfo) {
  editForm.id = a.id
  editForm.name = a.name
  editForm.remark = a.remark
  editForm.roles = a.roles || 'frpc'
  editVisible.value = true
}

async function submitEdit() {
  editing.value = true
  try {
    await agentApi.update(editForm.id, {
      name: editForm.name,
      remark: editForm.remark,
      roles: editForm.roles,
    })
    editVisible.value = false
    ElMessage.success('已保存')
    await load()
  } finally {
    editing.value = false
  }
}

async function openInstall(a: AgentInfo) {
  installTarget.value = a
  installMode.value = 'binary'
  installRuntime.value = 'process'
  installVisible.value = true
  await loadInstall()
}

/** 进程模式只给一键脚本：Agent 装成宿主机二进制，frp 也是子进程，没有容器什么事 */
function onRuntimeChange() {
  if (installRuntime.value === 'process') installMode.value = 'binary'
  loadInstall()
}

async function loadInstall() {
  const a = installTarget.value
  if (!a) return
  installLoading.value = true
  try {
    installCmds.value = await agentApi.installCommand(a.id, installOS.value, installRuntime.value)
  } finally {
    installLoading.value = false
  }
}

async function openHistory(a: AgentInfo) {
  historyTitle.value = `${a.name} · 指令历史`
  history.value = await agentApi.commands(a.id)
  historyVisible.value = true
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

async function resetToken(a: AgentInfo) {
  await ElMessageBox.confirm(
    `重置后旧令牌立即失效，${a.name} 上的 Agent 需重新安装或更新配置才能连上面板。确认重置？`,
    '重置安装令牌',
    { type: 'warning' },
  )
  await agentApi.resetToken(a.id)
  ElMessage.success('已重置令牌，请重新复制安装命令')
  await load()
}

async function remove(a: AgentInfo) {
  await ElMessageBox.confirm(`确认删除 Agent「${a.name}」？请先删除其绑定的服务端或节点。`, '提示', {
    type: 'warning',
  })
  await agentApi.remove(a.id)
  ElMessage.success('已删除')
  await load()
}

onMounted(() => {
  mounted = true
  void load()
})
onUnmounted(() => {
  mounted = false
  if (versionPoll) clearTimeout(versionPoll)
})
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="page-header">
      <div class="page-title">Agent 管理</div>
      <div class="actions">
        <el-tag type="success" effect="plain" style="margin-right: 8px">在线 {{ onlineCount }} / {{ agents.length }}</el-tag>
        <el-tooltip v-if="versionCheckError" :content="versionCheckError">
          <span class="version-error">版本检测失败</span>
        </el-tooltip>
        <el-button :loading="versionChecking" @click="checkAgentVersions(true)">检测 Agent 版本</el-button>
        <el-button @click="load">刷新</el-button>
        <el-button type="primary" @click="openCreate">新建 Agent</el-button>
      </div>
    </div>

    <el-card>
      <el-table v-if="agents.length" :data="agents" style="width: 100%">
        <el-table-column prop="name" label="名称" min-width="140">
          <template #default="{ row }">
            <div class="name-cell">{{ row.name }}</div>
            <div v-if="row.remark" class="sub">{{ row.remark }}</div>
          </template>
        </el-table-column>
        <el-table-column label="类型" width="150">
          <template #default="{ row }">
            <el-tag size="small">{{ row.roles === 'frps' ? '服务端' : '客户端' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.online ? 'success' : 'info'" effect="dark">
              {{ row.online ? '在线' : '离线' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="主机" min-width="160">
          <template #default="{ row }">
            <div>{{ row.hostname || '—' }}</div>
            <div class="sub">{{ row.os }} {{ row.arch }} {{ row.remoteAddr }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="version" label="Agent 版本" width="135">
          <template #default="{ row }">
            <div>{{ row.version || '—' }}</div>
            <el-tooltip v-if="versionStatuses[row.id]?.updateAvailable" :content="`最新发布版 ${versionStatuses[row.id].latest}`">
              <a :href="versionStatuses[row.id].releaseUrl" target="_blank" rel="noopener noreferrer" class="agent-update">可更新</a>
            </el-tooltip>
            <div v-else-if="versionStatuses[row.id]?.state === 'current'" class="sub">已是最新</div>
            <div v-else-if="versionStatuses[row.id]?.state === 'ahead'" class="sub">高于发布版</div>
            <div v-else-if="versionChecking && versionStatuses[row.id]?.state === 'unknown'" class="sub">正在检测…</div>
            <div v-else-if="versionStatuses[row.id]?.state === 'unknown'" class="sub">版本未知</div>
          </template>
        </el-table-column>
        <el-table-column label="运行时" width="96">
          <template #default="{ row }">
            <el-tag size="small" :type="row.runtime === 'docker' ? 'warning' : 'info'" effect="plain">
              {{ row.runtime === 'docker' ? 'docker' : 'process' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="frp 版本" width="150">
          <template #default="{ row }">
            <el-tooltip
              :disabled="!!row.frpInstalledVersion"
              content="面板还没给这台 Agent 下发 frp 二进制（容器底座镜像里不含 frp）：先去「设置 → frp 二进制」按平台下载，再到「frp 版本」里切换"
              placement="top"
            >
              <div>{{ frpLabel(row) }}</div>
            </el-tooltip>
            <div v-if="row.frpVersion" class="sub">期望 {{ row.frpVersion }}</div>
            <el-tag v-if="frpNeedsUpdate(row)" size="small" type="warning" style="margin-top: 2px">可更新</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最后心跳" width="180">
          <template #default="{ row }">{{ fmtTime(row.lastSeen) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="320" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openInstall(row)">安装命令</el-button>
            <el-button link type="primary" @click="openFrp(row)">frp 版本</el-button>
            <el-button link @click="openEdit(row)">编辑</el-button>
            <el-button link @click="openHistory(row)">指令</el-button>
            <el-button link type="warning" @click="resetToken(row)">重置</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else description="还没有 Agent，点击右上角新建后复制安装命令到目标服务器" />
    </el-card>

    <el-dialog v-model="createVisible" title="新建 Agent" width="520px">
      <el-form label-width="90px">
        <el-form-item label="名称">
          <el-input v-model="createForm.name" placeholder="如 广州-01" />
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="createForm.roles">
            <el-radio value="frps">frps（服务端）</el-radio>
            <el-radio value="frpc">frpc（客户端）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="frp 版本">
          <el-select
            v-model="createForm.frpVersion"
            filterable
            allow-create
            clearable
            default-first-option
            placeholder="选填，建议现在就选好"
            style="width: 100%"
          >
            <el-option
              v-for="v in frpVersionOptions"
              :key="v"
              :value="v"
              :label="v === frpVersionLatest ? `${v}（最新）` : v"
            />
          </el-select>
          <div class="hint-line">
            只记为期望版本，不切换、不重启。该版本要先在设置页下载好，再到 Agent 上切换。
          </div>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="createForm.remark" placeholder="选填" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="submitCreate">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="editVisible" title="编辑 Agent" width="520px">
      <el-form label-width="90px">
        <el-form-item label="名称">
          <el-input v-model="editForm.name" />
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="editForm.roles">
            <el-radio value="frps">frps（服务端）</el-radio>
            <el-radio value="frpc">frpc（客户端）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="editForm.remark" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :loading="editing" @click="submitEdit">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="installVisible" title="安装 Agent" width="860px">
      <div v-if="installTarget" v-loading="installLoading">
        <el-descriptions :column="3" border size="small" style="margin-bottom: 12px">
          <el-descriptions-item label="名称">{{ installTarget.name }}</el-descriptions-item>
          <el-descriptions-item label="角色">{{ installTarget.roles }}</el-descriptions-item>
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
          <el-radio-group v-model="installRuntime" size="small" style="margin-left: 12px" @change="onRuntimeChange">
            <el-radio-button value="process">进程运行</el-radio-button>
            <el-radio-button value="docker">Docker 容器运行</el-radio-button>
          </el-radio-group>
        </div>

        <el-tabs v-if="installRuntime === 'docker'" v-model="installMode">
          <el-tab-pane label="一键脚本" name="binary" />
          <el-tab-pane label="docker run" name="docker" />
          <el-tab-pane label="docker compose" name="compose" />
        </el-tabs>

        <pre class="code-block">{{ currentCommand }}</pre>
        <div class="hint-line">
          <span v-if="installRuntime === 'docker' && installMode !== 'binary'">
            容器内已内置 frps / frpc；需挂载 /var/run/docker.sock。
          </span>
          <span v-else>自动注册 systemd / launchd / 计划任务并开机自启。</span>
        </div>
      </div>
      <template #footer>
        <el-button @click="installVisible = false">关闭</el-button>
        <el-button type="primary" @click="copy(currentCommand)">复制命令</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="historyVisible" :title="historyTitle" width="820px">
      <el-table :data="history" max-height="420" size="small">
        <el-table-column prop="id" label="#" width="60" />
        <el-table-column label="类型" width="90">
          <template #default="{ row }">{{ row.type }}</template>
        </el-table-column>
        <el-table-column label="目标" width="120">
          <template #default="{ row }">{{ row.targetType }}#{{ row.targetId }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="结果" min-width="260">
          <template #default="{ row }">
            <span class="sub">{{ row.result || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ fmtTime(row.doneAt || row.sentAt || row.createdAt) }}</template>
        </el-table-column>
      </el-table>
    </el-dialog>
    <FrpVersionPicker
      v-if="frpTarget"
      v-model="frpVisible"
      target="agent"
      :target-id="frpTarget.id"
      :target-name="frpTarget.name"
      @closed="onFrpClosed"
    />
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

.version-error {
  color: #f56c6c;
  font-size: 12px;
  margin-right: 8px;
}

.agent-update {
  color: #e6a23c;
  font-size: 12px;
  text-decoration: none;
}

.agent-update:hover {
  text-decoration: underline;
}

.mono {
  font-family: Consolas, Monaco, monospace;
}

.install-bar {
  display: flex;
  align-items: center;
  margin-bottom: 4px;
}

.hint-line {
  color: #909399;
  font-size: 12px;
  line-height: 1.7;
}

.code-block {
  margin: 0;
  max-height: 320px;
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
