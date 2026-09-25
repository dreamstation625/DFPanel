<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  agentApi,
  emptyServer,
  frpVersionApi,
  serverApi,
  statusLabel as statusText,
  statusType as statusKind,
  type AgentInfo,
  type ConfigVersionItem,
  type FrpsServer,
  type FrpVersionState,
} from '@/api'
import FieldHelp from '@/components/FieldHelp.vue'
import VersionHistory from '@/components/VersionHistory.vue'
import { DOC } from '@/utils/docs'
import { genPassword } from '@/utils/password'

const loading = ref(false)
const saving = ref(false)
const list = ref<FrpsServer[]>([])
const agents = ref<AgentInfo[]>([])
const form = ref<FrpsServer>(emptyServer())
const previewText = ref('')
const previewVisible = ref(false)
const logText = ref('')
const logVisible = ref(false)
const logLoading = ref(false)
/** 日志内容容器：打开弹窗时滚到最底部，直接看到最新一行 */
const logBox = ref<HTMLElement | null>(null)
/** 正在执行的进程操作：start / stop / restart，用于按钮 loading */
const acting = ref('')
const activeTab = ref('basic')

// 历史版本（每次下发都会在 Agent 侧生成快照，可在此按版本回滚）
const versionVisible = ref(false)
const versionLoading = ref(false)
const versions = ref<ConfigVersionItem[]>([])
const versionsFromAgent = ref(false)

// frp 版本（本机托管看面板 bin 目录；Agent 托管看该 Agent 上报的版本）
const localFrp = ref<FrpVersionState | null>(null)

const frpVersionText = computed(() => {
  if (form.value.deployMode === 'agent') {
    const a = agents.value.find((item) => item.id === form.value.agentId)
    if (!a) return '未绑定 Agent'
    if (!a.frpInstalledVersion) return '未下发'
    return a.frpInstalledVersion
  }
  return localFrp.value?.active || '未安装'
})

/** 版本提示：本地/Agent 是否有可用的更新，或期望版本尚未生效 */
const frpVersionHint = computed(() => {
  if (form.value.deployMode === 'agent') {
    const a = agents.value.find((item) => item.id === form.value.agentId)
    if (!a) return ''
    if (a.frpVersion && a.frpInstalledVersion && a.frpVersion !== a.frpInstalledVersion) {
      return '期望版本与生效版本不一致'
    }
    return ''
  }
  if (localFrp.value?.updatable) return '期望版本与生效版本不一致'
  return ''
})

const current = computed(() => list.value.find((s) => s.id === form.value.id) ?? null)
/** 用户是否主动点了「新建服务端」：用于区分初始空白态与新建草稿 */
const draftMode = ref(false)
/** 新建中（尚未落库） */
const isNew = computed(() => !form.value.id)
const statusType = computed(() => (isNew.value ? 'info' : statusKind(current.value?.status)))
const statusLabel = computed(() => (isNew.value ? '未保存' : statusText(current.value?.status)))
const agentRoleOk = computed(() => {
  const a = agents.value.find((item) => item.id === form.value.agentId)
  return !!a && a.roles === 'frps'
})
const availableFrpsAgents = computed(() => agents.value.filter((a) =>
  a.roles === 'frps' && !list.value.some((s) => s.deployMode === 'agent' && s.agentId === a.id && s.id !== form.value.id),
))

async function load() {
  loading.value = true
  try {
    const [servers, agentList] = await Promise.all([serverApi.list(), agentApi.list()])
    list.value = servers
    agents.value = agentList
    // frp 版本状态：失败不影响页面其它信息
    try {
      localFrp.value = await frpVersionApi.local()
    } catch {
      localFrp.value = null
    }
    if (list.value.length > 0) {
      const stillExists = list.value.some((s) => s.id === form.value.id)
      // 新建草稿时保留用户填写的内容；其余情况默认选中第一个服务端
      if (!stillExists && !draftMode.value) {
        form.value = { ...list.value[0] }
      }
    } else if (!draftMode.value) {
      form.value = emptyServer()
    }
  } finally {
    loading.value = false
  }
}

function selectServer(id: number | undefined) {
  draftMode.value = false
  const found = list.value.find((s) => s.id === id)
  if (found) form.value = { ...found }
}

async function save(): Promise<boolean> {
  saving.value = true
  try {
    if (form.value.id) {
      const updated = await serverApi.update(form.value.id, form.value)
      form.value = { ...updated }
      ElMessage.success('配置已保存')
    } else {
      const created = await serverApi.create(form.value)
      form.value = { ...created }
      draftMode.value = false
      ElMessage.success(`服务端「${created.name}」已创建`)
    }
    await load()
    return true
  } finally {
    saving.value = false
  }
}

async function apply() {
  const ok = await save()
  if (!ok) return
  const res = await serverApi.apply(form.value.id)
  if (res.queued) {
    ElMessage.warning(res.message)
  } else if (res.rolledBack) {
    ElMessage.warning(`新配置未生效，已自动回滚上一版：${res.message}`)
  } else if (res.unverified) {
    ElMessage.warning(res.message)
  } else if (res.ok === false) {
    ElMessage.error(res.message)
  } else {
    ElMessage.success(res.message)
  }
  await load()
}

async function preview() {
  if (!form.value.id) {
    ElMessage.warning('请先保存配置')
    return
  }
  const res = await serverApi.preview(form.value.id)
  previewText.value = res.content
  previewVisible.value = true
}

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/**
 * 操作后重新读状态：Agent 上报有延迟，多试几次，避免刚启动就被判成「没起来」。
 * 返回最终读到的状态。
 */
async function refreshStatus(id: number, expect: string) {
  let status: string | undefined
  for (let i = 0; i < 3; i += 1) {
    await load()
    status = list.value.find((s) => s.id === id)?.status
    if (status === expect || status === 'agent_offline') return status
    if (i < 2) await sleep(1500)
  }
  return status
}

/**
 * 启停重启：无论如何都刷新一次状态；失败（含启动后进程没起来）会提示并直接把日志打开。
 */
async function doAction(action: 'start' | 'stop' | 'restart') {
  const id = form.value.id
  if (!id) {
    ElMessage.warning('请先保存配置')
    return
  }

  acting.value = action
  let failure = ''
  let notified = false // 请求拦截器已经弹过错误提示，避免重复弹
  try {
    const res = await serverApi[action](id)
    if (res.queued) {
      ElMessage.warning(res.message || 'Agent 离线，指令已排队，上线后自动执行')
      await load()
      return
    }
    if (res.ok === false) {
      failure = res.message || '操作失败'
    } else {
      ElMessage.success(res.message ?? '操作完成')
    }
  } catch (e: any) {
    failure = e?.response?.data?.error || e?.message || '操作失败'
    notified = true
  } finally {
    acting.value = ''
  }

  const expect = action === 'stop' ? 'stopped' : 'running'
  if (!failure) {
    const status = await refreshStatus(id, expect)
    if (status && status !== expect) {
      failure = `${action === 'stop' ? '停止' : '启动'}后状态是「${statusText(status)}」，进程可能没跑起来`
    }
  } else {
    await load()
  }

  if (failure) {
    if (!notified) ElMessage.error(failure)
    await openLog(id)
  }
}

/** 打开日志弹窗；进程操作失败时会被自动调用，直接看原因 */
async function openLog(id = form.value.id) {
  if (!id) return
  logText.value = ''
  logLoading.value = true
  logVisible.value = true
  try {
    const res = await serverApi.log(id)
    logText.value = res.content || '（暂无日志，请先启动 frps）'
    if (res.message) ElMessage.warning(res.message)
  } catch (e: any) {
    logText.value = '读取日志失败：' + (e?.response?.data?.error || e?.message || '未知错误')
  } finally {
    logLoading.value = false
  }
  // 最新一行在最后：打开就滚到底，等弹窗动画结束再滚一次（否则容器高度还是 0）
  await scrollLogToBottom()
  window.setTimeout(scrollLogToBottom, 260)
}

/** 把日志窗口滚到底部 */
async function scrollLogToBottom() {
  await nextTick()
  const el = logBox.value
  if (el) el.scrollTop = el.scrollHeight
}

async function openVersions() {
  if (!form.value.id) return
  versionVisible.value = true
  versionLoading.value = true
  try {
    const res = await serverApi.versions(form.value.id)
    versions.value = res.versions || []
    versionsFromAgent.value = !!res.fromAgent
    if (res.message) ElMessage.warning(res.message)
  } finally {
    versionLoading.value = false
  }
}

async function rollbackVersion(version: number) {
  if (!form.value.id) return
  await ElMessageBox.confirm(
    `确认回滚到版本 v${version}？Agent 会应用该版本配置并做健康校验，若出现明确的 frp 报错会自动退回当前配置。`,
    '回滚确认',
    { type: 'warning' },
  )
  const res = await serverApi.rollback(form.value.id, version)
  if (res.queued || res.unverified || res.rolledBack) {
    ElMessage.warning(res.message)
  } else if (res.ok === false) {
    ElMessage.error(res.message)
  } else {
    ElMessage.success(res.message)
  }
  await load()
  await openVersions()
}

function toggleSshGateway(val: boolean) {
  form.value.sshGatewayBindPort = val ? 2201 : 0
}

function regenDashboardPwd() {
  form.value.dashboardPwd = genPassword(8)
  ElMessage.success('已生成新的 8 位随机密码，保存后生效')
}

async function copyDashboardPwd() {
  const text = form.value.dashboardPwd
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
  ElMessage.success('密码已复制')
}

/** 同一台机器（本机托管 / 同一个 Agent）的服务端端口不能重复 */
function hostKeyOf(s: { deployMode?: string; agentId?: number }) {
  if (s.deployMode !== 'agent') return 'local'
  const agent = agents.value.find((a) => a.id === s.agentId)
  return agent?.hostId ? `host:${agent.hostId}` : `agent:${s.agentId || 0}`
}

/** 自动挑选同机上第一个空闲的监听端口 */
function nextFreePort(hostKey: string): number {
  const used = new Set(list.value.filter((s) => hostKeyOf(s) === hostKey).map((s) => s.bindPort))
  for (let p = 7000; p < 7600; p++) {
    if (!used.has(p)) return p
  }
  return 7000
}

/** 切换到新建配置（不落库，保存时才创建） */
function createNew() {
  const draft = emptyServer()
  draft.name = `服务端-${list.value.length + 1}`
  // 沿用当前选择的部署位置，方便在同一台机器上批量添加
  draft.deployMode = form.value.deployMode === 'agent' ? 'agent' : 'local'
  draft.agentId = draft.deployMode === 'agent'
    ? (agents.value.find((a) => a.roles === 'frps' && !list.value.some((s) => s.deployMode === 'agent' && s.agentId === a.id))?.id ?? 0)
    : 0
  draft.bindPort = nextFreePort(hostKeyOf(draft))
  form.value = draft
  draftMode.value = true
  activeTab.value = 'basic'
  ElMessage.info('已切换到新建配置，填写后点击「保存」即可创建')
}

async function removeServer() {
  const id = form.value.id
  if (!id) return
  await ElMessageBox.confirm(`确认删除服务端「${form.value.name}」？删除后无法恢复。`, '提示', { type: 'warning' })
  const result = await serverApi.remove(id)
  if (result.cleanupQueued) ElMessage.warning('服务端已删除；Agent 离线，旧配置将在其上线后清理')
  else ElMessage.success('已删除')
  draftMode.value = false
  const next = list.value.find((s) => s.id !== id)
  form.value = next ? { ...next } : emptyServer()
  await load()
}

onMounted(load)
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="page-header">
      <div class="page-title">frps 服务端配置</div>
      <div class="actions">
        <el-select
          v-if="list.length"
          :model-value="form.id || undefined"
          placeholder="选择服务端"
          style="width: 220px"
          @update:model-value="selectServer"
        >
          <el-option v-for="s in list" :key="s.id" :value="s.id" :label="s.name" />
        </el-select>
        <el-tag :type="statusType" effect="dark">{{ statusLabel }}</el-tag>
        <el-tooltip
          :content="frpVersionHint || 'frp 版本（可在「设置」或「Agent 管理」中切换）'"
          placement="bottom"
        >
          <el-tag effect="plain" :type="frpVersionHint ? 'warning' : 'info'">
            frp {{ frpVersionText }}
          </el-tag>
        </el-tooltip>
        <el-button type="primary" plain @click="createNew">新建服务端</el-button>
        <el-button :disabled="isNew" @click="preview">预览 frps.json</el-button>
        <el-button :disabled="isNew" @click="openLog()">日志</el-button>
        <el-button :disabled="isNew" @click="openVersions">历史版本</el-button>
        <el-button :loading="saving" @click="save">保存</el-button>
        <el-button type="primary" @click="apply">保存并应用</el-button>
        <el-button v-if="!isNew" type="danger" plain @click="removeServer">删除</el-button>
      </div>
    </div>

    <el-alert
      v-if="list.length && current?.status === 'not_installed'"
      type="warning"
      show-icon
      :closable="false"
      title="未检测到 frps 二进制"
      description="可在「设置」中下载，或手动放入 ./data/bin。"
      style="margin-bottom: 16px"
    />

    <el-alert
      v-if="list.length && form.deployMode === 'agent' && (!form.agentId || !agentRoleOk)"
      type="warning"
      show-icon
      :closable="false"
      title="Agent 托管模式配置不完整"
      description="请选择具备 frps 角色的 Agent；Agent 离线时配置会排队，上线后自动下发。"
      style="margin-bottom: 16px"
    />

    <el-alert
      v-if="isNew"
      type="info"
      show-icon
      :closable="false"
      title="正在新建服务端配置"
      description="「保存」创建，「保存并应用」立即生效；同一台机器上监听端口不可重复。"
      style="margin-bottom: 16px"
    />

    <el-card>
      <div class="proc-bar">
        <el-button
          type="success"
          :disabled="isNew"
          :loading="acting === 'start'"
          @click="doAction('start')"
        >
          启动 frps
        </el-button>
        <el-button
          type="warning"
          :disabled="isNew"
          :loading="acting === 'restart'"
          @click="doAction('restart')"
        >
          重启
        </el-button>
        <el-button
          type="danger"
          :disabled="isNew"
          :loading="acting === 'stop'"
          @click="doAction('stop')"
        >
          停止
        </el-button>
        <span v-if="isNew" class="hint">请先保存配置后再操作进程</span>
        <span v-else-if="form.deployMode === 'agent'" class="hint">将通过托管 Agent 在远端执行</span>
      </div>

      <el-form label-width="250px">
        <el-tabs v-model="activeTab">
          <el-tab-pane label="基础监听" name="basic">
            <el-form-item label="服务端名称">
              <el-input v-model="form.name" placeholder="用于面板内区分" />
            </el-form-item>
            <el-form-item label="部署模式">
              <el-radio-group v-model="form.deployMode" :disabled="!!form.id && current?.deployMode === 'agent'">
                <el-radio-button value="local">面板本机托管</el-radio-button>
                <el-radio-button value="agent">远端 Agent 托管</el-radio-button>
              </el-radio-group>
              <span class="hint">本机由面板启动 frps，Agent 托管则下发到目标服务器执行</span>
            </el-form-item>
            <el-form-item label="自动启动">
              <el-switch v-model="form.autoStart" />
              <span class="hint">面板重启后自动拉起；手动停止过的不再自动拉起</span>
            </el-form-item>
            <el-form-item v-if="form.deployMode === 'agent'" label="托管 Agent">
              <el-select v-model="form.agentId" placeholder="选择空闲的服务端 Agent" style="width: 280px" :disabled="!!form.id && current?.deployMode === 'agent'">
                <el-option
                  v-for="a in availableFrpsAgents"
                  :key="a.id"
                  :value="a.id"
                  :label="`${a.name}（${a.online ? '在线' : '离线'}）`"
                />
              </el-select>
              <span class="hint">每个服务端 Agent 只绑定一个 frps；已创建的 Agent 绑定不能更换</span>
            </el-form-item>
            <el-form-item v-if="form.deployMode === 'agent'" label="公网地址 publicAddr">
              <el-input v-model="form.publicAddr" placeholder="如 1.2.3.4 或 frp.example.com" />
              <span class="hint">供 frpc 生成 serverAddr，留空用面板地址</span>
            </el-form-item>

            <el-form-item>
              <template #label>
                <span class="lb">监听地址 bindAddr</span>
                <FieldHelp :doc="DOC.serverConfig" text="默认 0.0.0.0" />
              </template>
              <el-input v-model="form.bindAddr" placeholder="0.0.0.0" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">监听端口 bindPort</span>
                <FieldHelp :doc="DOC.serverConfig" text="默认 7000，frpc 连接此端口" />
              </template>
              <el-input-number v-model="form.bindPort" :min="1" :max="65535" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">KCP 端口 kcpBindPort</span>
                <FieldHelp :doc="DOC.serverConfig" text="接收 KCP 协议连接" />
              </template>
              <el-input-number v-model="form.kcpBindPort" :min="0" :max="65535" controls-position="right" />
              <span class="hint">0 表示不启用</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">QUIC 端口 quicBindPort</span>
                <FieldHelp :doc="DOC.serverConfig" text="接收 QUIC 协议连接" />
              </template>
              <el-input-number v-model="form.quicBindPort" :min="0" :max="65535" controls-position="right" />
              <span class="hint">0 表示不启用</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">代理出口地址 proxyBindAddr</span>
                <FieldHelp :doc="DOC.serverConfig" text="留空同 bindAddr" />
              </template>
              <el-input v-model="form.proxyBindAddr" placeholder="留空使用 bindAddr" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">HTTP 虚拟主机端口</span>
                <FieldHelp :doc="DOC.featureVirtualHost" text="vhostHTTPPort，0 表示不启用" />
              </template>
              <el-input-number v-model="form.vhostHttpPort" :min="0" :max="65535" />
              <span class="hint">0 表示不启用</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">HTTP 空闲超时(秒)</span>
                <FieldHelp :doc="DOC.serverConfig" text="vhostHTTPTimeout，默认 60" />
              </template>
              <el-input-number v-model="form.vhostHttpTimeout" :min="0" controls-position="right" />
              <span class="hint">0 使用默认 60</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">HTTPS 虚拟主机端口</span>
                <FieldHelp :doc="DOC.featureVirtualHost" text="vhostHTTPSPort，0 表示不启用" />
              </template>
              <el-input-number v-model="form.vhostHttpsPort" :min="0" :max="65535" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">子域名后缀 subDomainHost</span>
                <FieldHelp :doc="DOC.featureSubdomain" text="配置后 frpc 可用 subdomain 生成域名" />
              </template>
              <el-input v-model="form.subdomainHost" placeholder="如 example.com" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">自定义 404 页面</span>
                <FieldHelp :doc="DOC.serverConfig" text="custom404Page，404 页面地址" />
              </template>
              <el-input v-model="form.custom404Page" placeholder="如 /etc/frp/404.html" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">tcpmux HTTP 端口</span>
                <FieldHelp :doc="DOC.serverConfig" text="tcpmuxHTTPConnectPort，0 表示不启用" />
              </template>
              <el-input-number v-model="form.tcpmuxHttpConnectPort" :min="0" :max="65535" controls-position="right" />
              <span class="hint">vhost 端口不可用时可用此端口</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">tcpmux 透传</span>
                <FieldHelp :doc="DOC.serverConfig" text="tcpmuxPassthrough，透传 CONNECT 请求" />
              </template>
              <el-switch v-model="form.tcpmuxPassthrough" />
            </el-form-item>
          </el-tab-pane>

          <el-tab-pane label="认证与 Dashboard" name="auth">
            <el-form-item>
              <template #label>
                <span class="lb">认证方式 auth.method</span>
                <FieldHelp :doc="DOC.serverAuth" text="token 或 oidc，默认 token" />
              </template>
              <el-select v-model="form.authMethod">
                <el-option label="token" value="token" />
                <el-option label="oidc" value="oidc" />
              </el-select>
            </el-form-item>
        <el-form-item v-if="form.authMethod === 'token'">
          <template #label>
            <span class="lb">认证 Token</span>
            <FieldHelp :doc="DOC.serverAuth" text="auth.token，客户端须填相同值" />
          </template>
          <el-input v-model="form.authToken" show-password placeholder="留空自动生成" />
        </el-form-item>
        <el-form-item v-if="form.authMethod === 'token'">
          <template #label>
            <span class="lb">Token 来源 tokenSource</span>
            <FieldHelp :doc="DOC.serverAuth" text="tokenSource.type，与上面的 token 互斥" />
          </template>
          <el-select v-model="form.authTokenSourceType" clearable placeholder="直接写在上方" style="width: 140px">
            <el-option label="file" value="file" />
          </el-select>
          <el-input
            v-if="form.authTokenSourceType"
            v-model="form.authTokenSourcePath"
            placeholder="token 文件路径，如 /etc/frp/token"
            style="width: 280px; margin-left: 8px"
          />
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="lb">服务端插件 httpPlugins</span>
            <FieldHelp :doc="DOC.serverConfig" text="JSON 数组，留空不使用插件" />
          </template>
          <el-input
            v-model="form.authHttpPlugins"
            type="textarea"
            :rows="3"
            placeholder='[{"name":"user-manager","addr":"127.0.0.1:9000","path":"/handler","ops":["Login"]}]'
          />
        </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">授权范围 additionalScopes</span>
                <FieldHelp :doc="DOC.serverAuth" text="可选 HeartBeats / NewWorkConns" />
              </template>
              <el-input v-model="form.authAdditionalScopes" placeholder="HeartBeats,NewWorkConns" />
            </el-form-item>

            <template v-if="form.authMethod === 'oidc'">
              <el-form-item>
                <template #label>
                  <span class="lb">OIDC Issuer</span>
                  <FieldHelp :doc="DOC.serverOidc" text="oidc.issuer，签发者地址" />
                </template>
                <el-input v-model="form.authOidcIssuer" placeholder="https://example.com:8443/dex" />
              </el-form-item>
              <el-form-item>
                <template #label>
                  <span class="lb">OIDC Audience</span>
                  <FieldHelp :doc="DOC.serverOidc" text="oidc.audience，受众标识" />
                </template>
                <el-input v-model="form.authOidcAudience" />
              </el-form-item>
              <el-form-item>
                <template #label>
                  <span class="lb">跳过过期校验</span>
                  <FieldHelp :doc="DOC.serverOidc" text="oidc.skipExpiryCheck" />
                </template>
                <el-switch v-model="form.authOidcSkipExpiryCheck" />
              </el-form-item>
              <el-form-item>
                <template #label>
                  <span class="lb">跳过签发者校验</span>
                  <FieldHelp :doc="DOC.serverOidc" text="oidc.skipIssuerCheck" />
                </template>
                <el-switch v-model="form.authOidcSkipIssuerCheck" />
              </el-form-item>
            </template>

            <el-divider content-position="left">Dashboard（默认不启用）</el-divider>

            <el-form-item>
              <template #label>
                <span class="lb">启用 Dashboard</span>
                <FieldHelp :doc="DOC.commonWebServer" text="关闭时不写入 webServer 段" />
              </template>
              <el-switch v-model="form.dashboardEnabled" />
              <span class="hint">默认关闭，开启后用下方随机密码登录</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">Dashboard 监听地址</span>
                <FieldHelp :doc="DOC.commonWebServer" text="webServer.addr，默认 127.0.0.1" />
              </template>
              <el-input v-model="form.dashboardAddr" placeholder="留空为 0.0.0.0" :disabled="!form.dashboardEnabled" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">Dashboard 端口</span>
                <FieldHelp :doc="DOC.commonWebServer" text="webServer.port，常用 7500" />
              </template>
              <el-input-number
                v-model="form.dashboardPort"
                :min="0"
                :max="65535"
                :disabled="!form.dashboardEnabled"
              />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">Dashboard 用户名</span>
                <FieldHelp :doc="DOC.commonWebServer" text="webServer.user，BasicAuth 用户名" />
              </template>
              <el-input v-model="form.dashboardUser" :disabled="!form.dashboardEnabled" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">Dashboard 密码</span>
                <FieldHelp :doc="DOC.commonWebServer" text="webServer.password，已预置随机密码" />
              </template>
              <el-input v-model="form.dashboardPwd" show-password :disabled="!form.dashboardEnabled" style="width: 240px" />
              <el-button link type="primary" @click="regenDashboardPwd">随机生成</el-button>
              <el-button link type="primary" @click="copyDashboardPwd">复制</el-button>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">静态资源目录</span>
                <FieldHelp :doc="DOC.commonWebServer" text="webServer.assetsDir，静态资源目录" />
              </template>
              <el-input
                v-model="form.dashboardAssetsDir"
                placeholder="自定义 Dashboard 前端资源"
                :disabled="!form.dashboardEnabled"
              />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">pprof 调试接口</span>
                <FieldHelp :doc="DOC.commonWebServer" text="webServer.pprofEnable，启用 pprof" />
              </template>
              <el-switch v-model="form.dashboardPprofEnable" :disabled="!form.dashboardEnabled" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">Dashboard 证书</span>
                <FieldHelp :doc="DOC.commonTls" text="webServer.tls.certFile，HTTPS 证书" />
              </template>
              <el-input v-model="form.dashboardTlsCertFile" placeholder="certFile" :disabled="!form.dashboardEnabled" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">Dashboard 私钥</span>
                <FieldHelp :doc="DOC.commonTls" text="webServer.tls.keyFile，填后走 HTTPS" />
              </template>
              <el-input v-model="form.dashboardTlsKeyFile" placeholder="keyFile" :disabled="!form.dashboardEnabled" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">Prometheus 指标</span>
                <FieldHelp :doc="DOC.serverConfig" text="enablePrometheus，需启用 Dashboard" />
              </template>
              <el-switch v-model="form.enablePrometheus" />
            </el-form-item>
          </el-tab-pane>

          <el-tab-pane label="传输层" name="transport">
            <el-form-item>
              <template #label>
                <span class="lb">TCP 多路复用 tcpMux</span>
                <FieldHelp :doc="DOC.serverTransport" text="transport.tcpMux，单端口多连接，建议开启" />
              </template>
              <el-switch v-model="form.tcpMux" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">多路复用保活间隔(秒)</span>
                <FieldHelp :doc="DOC.serverTransport" text="tcpMuxKeepaliveInterval，秒" />
              </template>
              <el-input-number v-model="form.tcpMuxKeepaliveInterval" :min="0" controls-position="right" />
              <span class="hint">0 使用默认 30</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">TCP 保活时间(秒)</span>
                <FieldHelp :doc="DOC.serverTransport" text="tcpKeepalive，负数表示不启用" />
              </template>
              <el-input-number v-model="form.tcpKeepalive" :min="0" controls-position="right" />
              <span class="hint">0 使用默认 7200</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">连接池上限 maxPoolCount</span>
                <FieldHelp :doc="DOC.serverTransport" text="maxPoolCount，默认 5" />
              </template>
              <el-input-number v-model="form.maxPoolCount" :min="0" controls-position="right" />
              <span class="hint">0 使用默认 5</span>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">心跳超时(秒)</span>
                <FieldHelp :doc="DOC.serverTransport" text="heartbeatTimeout，默认 90" />
              </template>
              <el-input-number v-model="form.heartbeatTimeout" :min="0" controls-position="right" />
              <span class="hint">0 使用默认（tcpMux 开启时为 -1）</span>
            </el-form-item>

            <el-form-item>
              <template #label>
                <span class="lb">强制 TLS transport.tls</span>
                <FieldHelp :doc="DOC.serverTls" text="transport.tls.force，只接受 TLS 连接" />
              </template>
              <el-switch v-model="form.tlsForce" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">服务端证书</span>
                <FieldHelp :doc="DOC.commonTls" text="tls.certFile，证书路径" />
              </template>
              <el-input v-model="form.tlsCertFile" placeholder="transport.tls.certFile" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">服务端私钥</span>
                <FieldHelp :doc="DOC.commonTls" text="tls.keyFile，私钥路径" />
              </template>
              <el-input v-model="form.tlsKeyFile" placeholder="transport.tls.keyFile" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">可信 CA</span>
                <FieldHelp :doc="DOC.commonTls" text="tls.trustedCaFile，CA 证书" />
              </template>
              <el-input v-model="form.tlsTrustedCaFile" placeholder="transport.tls.trustedCaFile" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">TLS ServerName</span>
                <FieldHelp :doc="DOC.commonTls" text="tls.serverName，留空则不校验" />
              </template>
              <el-input v-model="form.tlsServerName" placeholder="为空则不校验证书 hostname" />
            </el-form-item>

            <el-form-item>
              <template #label>
                <span class="lb">QUIC 保活周期(秒)</span>
                <FieldHelp :doc="DOC.commonQuic" text="quic.keepalivePeriod，默认 10" />
              </template>
              <el-input-number v-model="form.quicKeepalivePeriod" :min="0" controls-position="right" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">QUIC 空闲超时(秒)</span>
                <FieldHelp :doc="DOC.commonQuic" text="quic.maxIdleTimeout，默认 30" />
              </template>
              <el-input-number v-model="form.quicMaxIdleTimeout" :min="0" controls-position="right" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">QUIC 最大流数</span>
                <FieldHelp :doc="DOC.commonQuic" text="quic.maxIncomingStreams，默认 10 万" />
              </template>
              <el-input-number v-model="form.quicMaxIncomingStreams" :min="0" controls-position="right" />
            </el-form-item>
          </el-tab-pane>

          <el-tab-pane label="高级与日志" name="advanced">
            <el-form-item>
              <template #label>
                <span class="lb">允许端口范围 allowPorts</span>
                <FieldHelp :doc="DOC.serverConfig" text="allowPorts，如 20000-30000" />
              </template>
              <el-input v-model="form.allowPorts" placeholder="如 20000-30000,30001" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">单客户端端口上限</span>
                <FieldHelp :doc="DOC.serverConfig" text="maxPortsPerClient，0 不限制" />
              </template>
              <el-input-number v-model="form.maxPortsPerClient" :min="0" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">返回详细错误给客户端</span>
                <FieldHelp :doc="DOC.serverConfig" text="detailedErrorsToClient，默认开启" />
              </template>
              <el-switch v-model="form.detailedErrorsToClient" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">用户连接超时(秒)</span>
                <FieldHelp :doc="DOC.serverConfig" text="userConnTimeout，默认 10" />
              </template>
              <el-input-number v-model="form.userConnTimeout" :min="0" controls-position="right" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">UDP 包大小</span>
                <FieldHelp :doc="DOC.serverConfig" text="udpPacketSize，默认 1500，需与客户端一致" />
              </template>
              <el-input-number v-model="form.udpPacketSize" :min="0" controls-position="right" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">打洞数据保留(小时)</span>
                <FieldHelp :doc="DOC.serverConfig" text="natholeAnalysisDataReserveHours，默认 168" />
              </template>
              <el-input-number v-model="form.natholeAnalysisDataReserveHours" :min="0" controls-position="right" />
            </el-form-item>

            <el-form-item>
              <template #label>
                <span class="lb">日志级别</span>
                <FieldHelp :doc="DOC.commonLog" text="log.level，默认 info" />
              </template>
              <el-select v-model="form.logLevel">
                <el-option label="trace" value="trace" />
                <el-option label="debug" value="debug" />
                <el-option label="info" value="info" />
                <el-option label="warn" value="warn" />
                <el-option label="error" value="error" />
              </el-select>
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">日志保留天数</span>
                <FieldHelp :doc="DOC.commonLog" text="log.maxDays，默认 3 天" />
              </template>
              <el-input-number v-model="form.logMaxDays" :min="0" :max="365" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">禁用日志颜色</span>
                <FieldHelp :doc="DOC.commonLog" text="log.disablePrintColor，禁用颜色" />
              </template>
              <el-switch v-model="form.logDisablePrintColor" />
            </el-form-item>

            <el-form-item>
              <template #label>
                <span class="lb">额外 JSON 配置</span>
              </template>
              <el-input
                v-model="form.extraJson"
                type="textarea"
                :rows="5"
                placeholder='例如：{ "bindPort": 7100 }'
              />
              <span class="hint">顶层字段会覆盖上方表单生成的值</span>
            </el-form-item>
          </el-tab-pane>

          <el-tab-pane label="SSH 网关" name="ssh">
            <el-form-item>
              <template #label>
                <span class="lb">启用 SSH 隧道网关</span>
                <FieldHelp :doc="DOC.serverSshGateway" text="sshTunnelGateway，需 frp v0.68+" />
              </template>
              <el-switch :model-value="form.sshGatewayBindPort > 0" @update:model-value="toggleSshGateway" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">SSH 监听端口</span>
                <FieldHelp :doc="DOC.serverSshGateway" text="sshTunnelGateway.bindPort，必填" />
              </template>
              <el-input-number
                v-model="form.sshGatewayBindPort"
                :min="0"
                :max="65535"
                :disabled="form.sshGatewayBindPort === 0"
              />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">SSH 私钥文件</span>
                <FieldHelp :doc="DOC.serverSshGateway" text="privateKeyFile，SSH 私钥" />
              </template>
              <el-input v-model="form.sshGatewayPrivateKeyFile" placeholder="/etc/frp/ssh_host_ed25519_key" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">自动生成私钥路径</span>
                <FieldHelp :doc="DOC.serverSshGateway" text="autoGenPrivateKeyPath，自动生成私钥" />
              </template>
              <el-input v-model="form.sshGatewayAutoGenPrivateKeyPath" placeholder="/var/lib/frp/ssh_tunnel_gateway" />
            </el-form-item>
            <el-form-item>
              <template #label>
                <span class="lb">公钥认证文件</span>
                <FieldHelp :doc="DOC.serverSshGateway" text="authorizedKeysFile，留空则不鉴权" />
              </template>
              <el-input v-model="form.sshGatewayAuthorizedKeysFile" placeholder="/etc/frp/authorized_keys" />
            </el-form-item>
          </el-tab-pane>
        </el-tabs>
      </el-form>

    </el-card>

    <el-dialog v-model="previewVisible" title="frps.json 预览" width="720px">
      <pre class="code-block">{{ previewText }}</pre>
    </el-dialog>

    <el-dialog v-model="logVisible" title="frps 日志" width="760px">
      <pre ref="logBox" v-loading="logLoading" class="code-block">{{ logText || '（暂无日志）' }}</pre>
    </el-dialog>

    <el-dialog v-model="versionVisible" title="历史版本（可回滚）" width="920px">
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
/* 进程操作栏：挪到表单上方，打开页面就能点到 */
.proc-bar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.proc-bar .el-button + .el-button,
.proc-bar .hint {
  margin-left: 0;
}

.actions {
  display: flex;
  align-items: center;
}

.hint {
  margin-left: 10px;
  color: #909399;
  font-size: 12px;
}

.lb {
  white-space: nowrap;
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
