<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  frpCacheApi,
  frpVersionApi,
  settingApi,
  type FrpCachedBinary,
  type FrpVersionState,
  type SettingsValues,
} from '@/api'
import FrpVersionPicker from '@/components/FrpVersionPicker.vue'

const loading = ref(false)
const saving = ref(false)
const form = reactive<SettingsValues>({
  frpDownloadBase: '',
  // 手填版本：界面上已隐藏，但保存时要原样带回去，别把它清空
  frpManualVersions: '',
  panelFrpVersion: '',
  frpVersionApi: '',
  frpDownloadBaseDefault: '',
})

const local = ref<FrpVersionState | null>(null)
const pickerVisible = ref(false)

/** 面板已缓存的 frp 二进制，Agent 的 frp 都从这些文件下发 */
const cacheList = ref<FrpCachedBinary[]>([])
const cacheLoading = ref(false)
const downloading = ref(false)
const versionOptions = ref<string[]>([])
const latestVersion = ref('')
const dlForm = reactive({ version: '', kinds: ['frps', 'frpc'], platform: 'linux/amd64' })

const PLATFORMS = [
  { label: 'linux / amd64', value: 'linux/amd64' },
  { label: 'linux / arm64', value: 'linux/arm64' },
  { label: 'linux / arm', value: 'linux/arm' },
  { label: 'darwin / amd64', value: 'darwin/amd64' },
  { label: 'darwin / arm64', value: 'darwin/arm64' },
  { label: 'windows / amd64', value: 'windows/amd64' },
]

/**
 * 常用下载源。公共加速站都是「站址 + 原始 GitHub 地址」的拼法，
 * 随时可能失效，所以官方直连与自建示例一直留在列表里兜底。
 */
const PRESETS = [
  {
    label: '官方直连（github.com）',
    value: 'https://github.com/fatedier/frp/releases/download/v{version}/{asset}',
  },
  {
    label: 'gh-proxy.com',
    value: 'https://gh-proxy.com/https://github.com/fatedier/frp/releases/download/v{version}/{asset}',
  },
  {
    label: 'ghfast.top',
    value: 'https://ghfast.top/https://github.com/fatedier/frp/releases/download/v{version}/{asset}',
  },
  {
    label: 'ghfile.geekertao.top',
    value: 'https://ghfile.geekertao.top/https://github.com/fatedier/frp/releases/download/v{version}/{asset}',
  },
  {
    label: 'gh.xxooo.cf',
    value: 'https://gh.xxooo.cf/https://github.com/fatedier/frp/releases/download/v{version}/{asset}',
  },
  {
    label: 'gh.jasonzeng.dev',
    value: 'https://gh.jasonzeng.dev/https://github.com/fatedier/frp/releases/download/v{version}/{asset}',
  },
  {
    label: '自建镜像（示例）',
    value: 'https://mirror.example.com/frp/v{version}/{asset}',
  },
]

/** 当前模板命中的预设；手改过就为空，下拉显示占位文案 */
const currentPreset = computed(() => {
  const hit = PRESETS.find((p) => p.value === form.frpDownloadBase)
  return hit ? hit.value : ''
})

function applyPreset(value: string) {
  form.frpDownloadBase = value
}

const baseValid = computed(
  () => form.frpDownloadBase.includes('{version}') && form.frpDownloadBase.includes('{asset}'),
)

async function load() {
  loading.value = true
  try {
    const [s, l] = await Promise.all([settingApi.get(), frpVersionApi.local()])
    Object.assign(form, s)
    local.value = l
  } catch (e: any) {
    ElMessage.error(e?.message || '读取设置失败')
  } finally {
    loading.value = false
  }
  // 版本列表取不到不影响别的区块，各自兜住
  await Promise.all([loadVersions(), loadCache()])
}

/** 可选版本列表，供下载表单的下拉使用 */
async function loadVersions() {
  try {
    const v = await settingApi.frpVersions()
    versionOptions.value = v.merged || []
    latestVersion.value = v.latest || ''
    if (!dlForm.version) dlForm.version = v.latest || v.merged[0] || ''
  } catch {
    versionOptions.value = []
  }
}

async function loadCache() {
  cacheLoading.value = true
  try {
    cacheList.value = await frpCacheApi.list()
  } catch (e: any) {
    ElMessage.error(e?.message || '读取已缓存的 frp 二进制失败')
  } finally {
    cacheLoading.value = false
  }
}

async function downloadBinary() {
  const version = dlForm.version.trim()
  if (!version) {
    ElMessage.warning('请选择或填写要下载的 frp 版本')
    return
  }
  if (!dlForm.kinds.length) {
    ElMessage.warning('至少勾选 frps / frpc 中的一种')
    return
  }
  const [os, arch] = dlForm.platform.split('/')
  downloading.value = true
  try {
    const res = await frpCacheApi.download({ version, kinds: dlForm.kinds, os, arch })
    ElMessage.success(res.message)
    cacheList.value = res.cached || []
  } catch (e: any) {
    ElMessage.error(e?.message || '下载失败')
  } finally {
    downloading.value = false
  }
}

async function removeBinary(b: FrpCachedBinary) {
  // 删掉的就是 Agent 切换时要用的那份，确认一下免得手滑
  try {
    await ElMessageBox.confirm(
      `确认删除 ${b.kind} ${b.version}（${b.os}/${b.arch}）？Agent 之后要切到这个版本得重新下载。`,
      '提示',
      { type: 'warning' },
    )
  } catch {
    return
  }
  try {
    const res = await frpCacheApi.remove(b)
    ElMessage.success(res.message)
    cacheList.value = res.cached || []
  } catch (e: any) {
    ElMessage.error(e?.message || '删除失败')
  }
}

function fmtSize(n: number) {
  if (!n) return '-'
  const mb = n / 1024 / 1024
  return mb >= 1 ? mb.toFixed(1) + ' MB' : Math.round(n / 1024) + ' KB'
}

function fmtWhen(s: string) {
  if (!s) return '-'
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
}

async function save() {
  if (!baseValid.value) {
    ElMessage.warning('下载地址模板必须同时包含 {version} 与 {asset} 占位符')
    return
  }
  saving.value = true
  try {
    const res = await settingApi.save({
      frpDownloadBase: form.frpDownloadBase,
      frpManualVersions: form.frpManualVersions,
      panelFrpVersion: form.panelFrpVersion,
    })
    Object.assign(form, res)
    ElMessage.success('设置已保存')
  } catch (e: any) {
    ElMessage.error(e?.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function reloadLocal() {
  try {
    local.value = await frpVersionApi.local()
  } catch {
    /* 拉取失败时保留原状态，不打断用户 */
  }
}

onMounted(load)
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="page-header">
      <div class="page-title">设置</div>
      <div class="actions">
        <el-button @click="load">刷新</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存</el-button>
      </div>
    </div>

    <el-card shadow="never" class="card">
      <template #header>
        <div class="card-title">frp 二进制下载源</div>
      </template>
      <el-form label-width="150px" style="max-width: 900px">
        <el-form-item label="下载地址模板" required>
          <el-input v-model="form.frpDownloadBase" placeholder="https://.../v{version}/{asset}" />
          <div class="hint">
            占位符：<code>{version}</code> <code>{asset}</code> <code>{os}</code> <code>{arch}</code>
          </div>
          <div class="presets">
            <el-select
              size="small"
              :model-value="currentPreset"
              placeholder="选择下载源（自动填入模板）"
              style="width: 300px"
              @update:model-value="applyPreset"
            >
              <el-option v-for="p in PRESETS" :key="p.value" :label="p.label" :value="p.value" />
            </el-select>
            <el-button size="small" text @click="form.frpDownloadBase = form.frpDownloadBaseDefault">恢复默认</el-button>
          </div>
          <el-text v-if="!baseValid" type="danger" size="small">
            模板必须同时包含 {version} 与 {asset} 占位符
          </el-text>
        </el-form-item>

        <el-form-item label="版本列表接口">
          <el-input :model-value="form.frpVersionApi || 'https://api.github.com/repos/fatedier/frp/releases'" disabled />
          <div class="hint">固定使用 GitHub 官方接口，不提供镜像。</div>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never" class="card">
      <template #header>
        <div class="card-title">面板本机 frps 版本</div>
      </template>
      <el-descriptions :column="3" border size="small">
        <el-descriptions-item label="当前生效版本">
          <span v-if="local?.active">{{ local.active }}</span>
          <el-text v-else type="info">未安装</el-text>
        </el-descriptions-item>
        <el-descriptions-item label="期望版本">
          <span v-if="local?.expected">{{ local.expected }}</span>
          <el-text v-else type="info">未设置</el-text>
        </el-descriptions-item>
        <el-descriptions-item label="已缓存版本">
          <template v-if="local?.cached?.length">
            <el-tag v-for="v in local.cached" :key="v" size="small" style="margin-right: 6px">{{ v }}</el-tag>
          </template>
          <el-text v-else type="info">无</el-text>
        </el-descriptions-item>
      </el-descriptions>

      <div class="hint" style="margin: 12px 0">
        本机托管的 frps 落在面板 <code>bin</code> 目录；Docker 运行时不受此项影响。
      </div>

      <el-button type="primary" @click="pickerVisible = true">管理本机 frps 版本</el-button>
      <el-tag v-if="local?.updatable" type="warning" size="small" style="margin-left: 10px">
        期望版本与当前生效版本不一致，需切换后生效
      </el-tag>
    </el-card>

    <el-card shadow="never" class="card">
      <template #header>
        <div class="card-head">
          <span class="card-title">frp 二进制（供 Agent 下载）</span>
          <el-text type="info" size="small">共 {{ cacheList.length }} 份</el-text>
          <el-button size="small" :loading="cacheLoading" @click="loadCache">刷新</el-button>
        </div>
      </template>
      <div class="hint" style="margin-bottom: 12px">
        Agent 用的 frp 都从面板下发。按目标机器的平台先下载好，Agent 切换版本时直接命中，不用现抓上游。
      </div>

      <div class="dl-bar">
        <el-select
          v-model="dlForm.version"
          filterable
          allow-create
          default-first-option
          placeholder="版本"
          style="width: 170px"
        >
          <el-option
            v-for="v in versionOptions"
            :key="v"
            :value="v"
            :label="v === latestVersion ? `${v}（最新）` : v"
          />
        </el-select>
        <el-checkbox-group v-model="dlForm.kinds" size="small">
          <el-checkbox-button value="frps">frps</el-checkbox-button>
          <el-checkbox-button value="frpc">frpc</el-checkbox-button>
        </el-checkbox-group>
        <el-select v-model="dlForm.platform" placeholder="平台" style="width: 170px">
          <el-option v-for="p in PLATFORMS" :key="p.value" :label="p.label" :value="p.value" />
        </el-select>
        <el-button type="primary" :loading="downloading" @click="downloadBinary">下载</el-button>
      </div>

      <el-table :data="cacheList" v-loading="cacheLoading" size="small" max-height="320" style="margin-top: 12px">
        <el-table-column label="类型" width="80" prop="kind" />
        <el-table-column label="版本" width="110" prop="version" />
        <el-table-column label="平台" width="150">
          <template #default="{ row }">{{ row.os }}/{{ row.arch }}</template>
        </el-table-column>
        <el-table-column label="大小" width="100">
          <template #default="{ row }">{{ fmtSize(row.size) }}</template>
        </el-table-column>
        <el-table-column label="下载时间">
          <template #default="{ row }">{{ fmtWhen(row.modTime) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button link type="danger" @click="removeBinary(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="还没有缓存任何 frp 二进制" :image-size="60" />
        </template>
      </el-table>
    </el-card>

    <FrpVersionPicker v-model="pickerVisible" target="local" target-name="面板本机" @closed="reloadLocal" />
  </div>
</template>

<style scoped>
.card {
  max-width: 1000px;
  margin-bottom: 16px;
}

.card-title {
  font-size: 15px;
  font-weight: 600;
}

.card-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.hint {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.7;
}

.presets {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
}

.dl-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

code {
  background: #f5f7fa;
  padding: 1px 4px;
  border-radius: 3px;
  font-size: 12px;
}
</style>
