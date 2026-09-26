<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import {
  frpVersionApi,
  settingApi,
  type FrpVersionResult,
  type FrpVersionState,
  type FrpVersionsResult,
} from '@/api'

/**
 * frp 版本选择与切换弹窗。
 *
 * 面板本机：「下载（不生效）」+「激活（重启生效）」两步，可以先把二进制预置好，切换时只换槽位。
 * Agent：只切换，没有下载按钮 —— 二进制统一在「设置 → frp 二进制」里按版本 + 类型 + 平台下载，
 * 面板缺这个版本时切换会直接被拒绝。
 */
const props = defineProps<{
  /** 目标类型：local = 面板本机 frps；agent = 单独托管一个服务端或节点的远端 Agent */
  target: 'local' | 'agent'
  /** Agent 模式下必填 */
  targetId?: number
  /** 展示用名称 */
  targetName?: string
}>()

const visible = defineModel<boolean>({ required: true })
/** 弹窗关闭后通知调用方刷新列表 */
const emit = defineEmits<{ closed: [] }>()

const loading = ref(false)
const acting = ref('')
const versions = ref<FrpVersionsResult | null>(null)
const state = ref<FrpVersionState | null>(null)
const selected = ref('')
/** 版本列表相关的说明（例如官方列表拉不到，用了内置兜底） */
const listMessage = ref('')
const customVersion = ref('')
const useCustom = ref(false)

const activeVersion = computed(() => state.value?.active || '')
const cachedVersions = computed(() => new Set(state.value?.cached || []))
/** docker 运行时：frp 以容器运行，版本靠挂载宿主机二进制实现，镜像 tag 不变 */
const isDocker = computed(() => props.target === 'agent' && state.value?.runtime === 'docker')
/** Agent 侧只切换：下载统一在设置页做 */
const isAgent = computed(() => props.target === 'agent')

const activeLabel = computed(() => {
  if (isDocker.value && !activeVersion.value) return '未下发'
  return ''
})

/** 目标下已托管的实例数：0 表示切换只是换二进制，没有任何服务会被重启 */
const instanceCount = computed(() => state.value?.instances ?? 0)

/** 主按钮文案：没有实例时就不该写「重启」 */
const switchLabel = computed(() => {
  if (instanceCount.value === 0) return '切换版本'
  return isDocker.value ? '切换并重建容器' : '切换并重启'
})

/** 三种目标的说明文案，按有没有实例、是不是容器运行时分开说 */
const switchHint = computed(() => {
  if (instanceCount.value === 0) return '还没有托管的实例，切换只替换二进制版本，不会重启任何服务。'
  if (isDocker.value) return '切换后会重建 frp 容器。请先在面板准备对应版本的二进制。'
  if (isAgent.value) return '只做切换，不下载：这个版本的二进制要先在「设置 → frp 二进制」里按平台下载好。'
  return '「下载」只预置二进制，不影响在跑的服务；「切换」才生效。'
})

/** 已缓存优先 + 官方列表 + 手填，且把当前 active 版本排到最前面便于识别 */
const options = computed(() => {
  const list = versions.value?.merged || []
  const active = activeVersion.value
  const ordered = active && list.includes(active) ? [active, ...list.filter((v) => v !== active)] : list
  return ordered
})

const finalVersion = computed(() => (useCustom.value ? customVersion.value.trim() : selected.value))

async function load(refresh = false) {
  loading.value = true
  try {
    const keep = selected.value
    const [v, s] = await Promise.all([
      settingApi.frpVersions(refresh),
      props.target === 'agent' && props.targetId
        ? frpVersionApi.agent(props.targetId)
        : frpVersionApi.local(),
    ])
    versions.value = v
    state.value = s
    // 点「重新获取」时保留用户已经选好的版本，别把它冲掉
    selected.value = keep || s.expected || s.active || v.latest || v.merged[0] || ''
    listMessage.value = v.message || ''
  } catch (e: any) {
    // 拿不到就明说，别让下拉空着还不给原因
    const msg = e?.message || '读取 frp 版本信息失败'
    listMessage.value = msg
    ElMessage.error(msg)
  } finally {
    loading.value = false
  }
}

watch(visible, (v) => {
  if (v) {
    useCustom.value = false
    customVersion.value = ''
    load()
  } else {
    emit('closed')
  }
})

// 调用方用 v-if 挂载（Agents.vue / Nodes.vue）：组件挂载那一刻 visible 就已经是 true，
// watch 看不出 false→true 的变化，不会触发 —— 于是首次打开是空列表。这里补一次。
onMounted(() => {
  if (visible.value) load()
})

/**
 * 选定版本后自动把二进制预置好，只针对面板本机。
 * Agent 侧不预置：下载统一在设置页做，这里只负责切换。
 */
async function autoPrefetch(version: string) {
  if (isAgent.value) return
  const v = (version || '').trim()
  if (!v || cachedVersions.value.has(v) || v === activeVersion.value) return
  await run('download')
}

async function run(action: 'download' | 'activate') {
  const version = finalVersion.value
  if (!version) {
    ElMessage.warning('请选择或填写要使用的 frp 版本')
    return
  }
  acting.value = action
  try {
    let res: FrpVersionResult
    if (props.target === 'agent' && props.targetId) {
      // Agent 只切换：下载统一在设置页做，这里不接受 download
      res = await frpVersionApi.agentActivate(props.targetId, version)
    } else {
      res = action === 'download'
        ? await frpVersionApi.localDownload(version)
        : await frpVersionApi.localActivate(version)
    }

    if (res.queued) {
      ElMessage.warning(res.message)
      return
    }
    if (action === 'activate' && res.rolledBack) {
      ElMessage.error(res.message)
    } else if (res.ok === false) {
      ElMessage.error(res.message)
    } else {
      ElMessage.success(res.message)
    }
    await load()
    if (action === 'activate' && res.ok !== false) {
      visible.value = false
    }
  } catch (e: any) {
    ElMessage.error(e?.message || (action === 'download' ? '下载失败' : '切换失败'))
  } finally {
    acting.value = ''
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="`frp 版本 — ${targetName || (target === 'local' ? '面板本机' : 'Agent')}`" width="620px">
    <div v-loading="loading">
      <el-alert
        v-if="target === 'agent' && !isDocker"
        type="info"
        :closable="false"
        show-icon
        title="此版本只应用于当前 Agent"
        :description="switchHint"
        style="margin-bottom: 14px"
      />
      <el-alert
        v-else-if="isDocker"
        type="info"
        :closable="false"
        show-icon
        title="Docker：镜像只作运行时底座"
        :description="switchHint"
        style="margin-bottom: 14px"
      />
      <el-alert
        v-else
        type="info"
        :closable="false"
        show-icon
        title="面板本机 frps 版本"
        :description="switchHint"
        style="margin-bottom: 14px"
      />

      <el-alert
        v-if="listMessage"
        type="warning"
        :closable="false"
        show-icon
        :title="listMessage"
        style="margin-bottom: 14px"
      />

      <el-alert
        v-if="state?.message"
        type="warning"
        :closable="false"
        show-icon
        :title="state.message"
        style="margin-bottom: 14px"
      />

      <el-descriptions :column="2" border size="small" style="margin-bottom: 14px">
        <el-descriptions-item label="当前生效版本">
          <span v-if="activeVersion">{{ activeVersion }}</span>
          <el-text v-else-if="activeLabel" type="warning">{{ activeLabel }}</el-text>
          <el-text v-else type="info">未下载</el-text>
        </el-descriptions-item>
        <el-descriptions-item label="期望版本">
          <span v-if="state?.expected">{{ state.expected }}</span>
          <el-text v-else type="info">未设置</el-text>
        </el-descriptions-item>
        <el-descriptions-item label="已缓存版本" :span="2">
          <template v-if="state?.cached?.length">
            <el-tag v-for="v in state.cached" :key="v" size="small" style="margin-right: 6px">{{ v }}</el-tag>
          </template>
          <el-text v-else type="info">无</el-text>
        </el-descriptions-item>
      </el-descriptions>

      <el-form label-width="92px">
        <el-form-item label="目标版本">
          <el-radio-group v-model="useCustom" size="small" style="margin-bottom: 8px">
            <el-radio-button :value="false">从列表选择</el-radio-button>
            <el-radio-button :value="true">手填版本号</el-radio-button>
          </el-radio-group>
          <el-select
            v-if="!useCustom"
            v-model="selected"
            placeholder="选择版本"
            filterable
            allow-create
            default-first-option
            style="width: 100%"
            @change="autoPrefetch"
          >
            <el-option
              v-for="v in options"
              :key="v"
              :value="v"
              :label="v === versions?.latest ? `${v}（最新）` : v"
            >
              <span>{{ v }}</span>
              <el-tag v-if="v === versions?.latest" size="small" type="success" style="margin-left: 8px">最新</el-tag>
              <el-tag v-if="cachedVersions.has(v)" size="small" type="info" style="margin-left: 6px">已缓存</el-tag>
              <el-tag v-if="v === activeVersion" size="small" type="warning" style="margin-left: 6px">当前</el-tag>
            </el-option>
          </el-select>
          <el-input
            v-else
            v-model="customVersion"
            placeholder="例如 0.62.1（不带 v 前缀）"
            clearable
            @change="autoPrefetch"
          />
          <div class="hint">
            版本列表来自 GitHub 官方接口，取不到可直接手填。<template v-if="isAgent">Agent 侧只切换，二进制请先在设置页下载。</template><template v-else>选定后会自动下载该版本。</template>
          </div>
          <div class="list-actions">
            <el-button size="small" :loading="loading" @click="load(true)">重新获取版本列表</el-button>
          </div>
        </el-form-item>
      </el-form>
    </div>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button v-if="!isAgent" :loading="acting === 'download'" @click="run('download')">仅下载</el-button>
      <el-button type="primary" :loading="acting === 'activate'" @click="run('activate')">
        {{ switchLabel }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.list-actions {
  margin-top: 8px;
}

.hint {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}
</style>
