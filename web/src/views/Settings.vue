<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import {
  frpVersionApi,
  settingApi,
  type FrpVersionState,
  type SettingsValues,
} from '@/api'
import FrpVersionPicker from '@/components/FrpVersionPicker.vue'

const loading = ref(false)
const saving = ref(false)
const form = reactive<SettingsValues>({
  frpDownloadBase: '',
  frpManualVersions: '',
  panelFrpVersion: '',
  frpVersionApi: '',
  frpDownloadBaseDefault: '',
})

const local = ref<FrpVersionState | null>(null)
const pickerVisible = ref(false)

/** 常用镜像源模板，点一下填进去；不硬编码为默认值，避免第三方代理失效后被动 */
const PRESETS = [
  {
    label: '官方直连',
    value: 'https://github.com/fatedier/frp/releases/download/v{version}/{asset}',
  },
  {
    label: 'ghproxy 代理前缀',
    value: 'https://ghproxy.net/https://github.com/fatedier/frp/releases/download/v{version}/{asset}',
  },
  {
    label: '自建镜像（示例）',
    value: 'https://mirror.example.com/frp/v{version}/{asset}',
  },
]

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
            占位符：<code>{version}</code> <code>{asset}</code> <code>{os}</code> <code>{arch}</code>。Agent 从面板拉取，无需访问 GitHub。
          </div>
          <div class="presets">
            <span class="presets-label">快速填入：</span>
            <el-button v-for="p in PRESETS" :key="p.label" size="small" text type="primary" @click="form.frpDownloadBase = p.value">
              {{ p.label }}
            </el-button>
            <el-button size="small" text @click="form.frpDownloadBase = form.frpDownloadBaseDefault">恢复默认</el-button>
          </div>
          <el-text v-if="!baseValid" type="danger" size="small">
            模板必须同时包含 {version} 与 {asset} 占位符
          </el-text>
        </el-form-item>

        <el-form-item label="手填版本">
          <el-input
            v-model="form.frpManualVersions"
            type="textarea"
            :rows="2"
            placeholder="0.62.1, 0.61.1"
          />
          <div class="hint">
            逗号或换行分隔，作为版本列表取不到时的兜底。
          </div>
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

.hint {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.7;
}

.presets {
  margin-top: 6px;
}

.presets-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-right: 4px;
}

code {
  background: #f5f7fa;
  padding: 1px 4px;
  border-radius: 3px;
  font-size: 12px;
}
</style>
