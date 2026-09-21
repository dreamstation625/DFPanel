<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  agentApi,
  statusLabel,
  statusType,
  type AgentInfo,
  type CommandHistory,
  type InstallCommands,
} from '@/api'
import FrpVersionPicker from '@/components/FrpVersionPicker.vue'

const loading = ref(false)
const agents = ref<AgentInfo[]>([])

// frp 版本管理（版本按 Agent 统一：该 Agent 上 frps 与 frpc 共用一个版本）
const frpVisible = ref(false)
const frpTarget = ref<AgentInfo | null>(null)

function openFrp(a: AgentInfo) {
  frpTarget.value = a
  frpVisible.value = true
}

/** frp 版本展示：优先显示实际生效版本，未接管时按运行时给出说明 */
function frpLabel(a: AgentInfo) {
  if (!a.frpInstalledVersion) return a.runtime === 'docker' ? '镜像自带' : '—'
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
const createForm = reactive({ name: '', remark: '', roles: ['frpc'] as string[] })
const creating = ref(false)

const editVisible = ref(false)
const editForm = reactive({ id: 0, name: '', remark: '', roles: [] as string[] })
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
  } finally {
    loading.value = false
  }
}

function openCreate() {
  createForm.name = ''
  createForm.remark = ''
  createForm.roles = ['frpc']
  createVisible.value = true
}

async function submitCreate() {
  if (!createForm.roles.length) {
    ElMessage.warning('请至少选择一个角色')
    return
  }
  creating.value = true
  try {
    const created = await agentApi.create({
      name: createForm.name || `Agent-${agents.value.length + 1}`,
      remark: createForm.remark,
      roles: createForm.roles.join(','),
    })
    createVisible.value = false
    ElMessage.success('Agent 已创建，请复制安装命令到目标服务器执行')
    await load()
    openInstall(created)
  } finally {
    creating.value = false
  }
}

function openEdit(a: AgentInfo) {
  editForm.id = a.id
  editForm.name = a.name
  editForm.remark = a.remark
  editForm.roles = (a.roles || 'frpc').split(',').filter(Boolean)
  editVisible.value = true
}

async function submitEdit() {
  editing.value = true
  try {
    await agentApi.update(editForm.id, {
      name: editForm.name,
      remark: editForm.remark,
      roles: editForm.roles.join(','),
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
  await ElMessageBox.confirm(`确认删除 Agent「${a.name}」？该机器上已托管的 frps / frpc 不会被自动停止。`, '提示', {
    type: 'warning',
  })
  await agentApi.remove(a.id)
  ElMessage.success('已删除')
  await load()
}

onMounted(load)
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="page-header">
      <div class="page-title">Agent 管理</div>
      <div class="actions">
        <el-tag type="success" effect="plain" style="margin-right: 8px">在线 {{ onlineCount }} / {{ agents.length }}</el-tag>
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
        <el-table-column label="角色" width="150">
          <template #default="{ row }">
            <el-tag v-for="r in (row.roles || 'frpc').split(',')" :key="r" size="small" style="margin-right: 4px">
              {{ r }}
            </el-tag>
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
        <el-table-column prop="version" label="Agent 版本" width="100">
          <template #default="{ row }">{{ row.version || '—' }}</template>
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
            <div>{{ frpLabel(row) }}</div>
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
        <el-form-item label="角色">
          <el-checkbox-group v-model="createForm.roles">
            <el-checkbox value="frps">frps（服务端）</el-checkbox>
            <el-checkbox value="frpc">frpc（客户端）</el-checkbox>
          </el-checkbox-group>
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
        <el-form-item label="角色">
          <el-checkbox-group v-model="editForm.roles">
            <el-checkbox value="frps">frps（服务端）</el-checkbox>
            <el-checkbox value="frpc">frpc（客户端）</el-checkbox>
          </el-checkbox-group>
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

        <pre class="code-block">{{ currentCommand }}</pre>
        <div class="hint-line">
          <span v-if="installRuntime === 'docker'">
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
      @closed="load"
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
