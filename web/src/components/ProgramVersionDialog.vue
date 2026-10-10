<script setup lang="ts">
import { computed } from 'vue'
import type { ProgramVersionStatus } from '@/api'
import { renderReleaseMarkdown } from '@/utils/releaseMarkdown'

const visible = defineModel<boolean>({ default: false })
const props = defineProps<{
  status: ProgramVersionStatus | null
  checking: boolean
  checkedAt: string
}>()
defineEmits<{ refresh: [] }>()

function dateLabel(value?: string) {
  if (!value || value.startsWith('0001-')) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleString('zh-CN', { hour12: false })
}

const release = computed(() => props.status?.release)
const releaseHtml = computed(() => renderReleaseMarkdown(release.value?.body || ''))
const resultLabel = computed(() => {
  if (props.checking) return '正在检测版本…'
  if (props.status?.state === 'error') return props.status.error || '版本检测失败'
  switch (props.status?.state) {
    case 'update': return '有新版本可更新'
    case 'current': return '已是最新版本'
    case 'ahead': return '当前版本高于最新发布版本'
    default: return props.status?.latest ? '无法比较当前版本，请查看发布说明' : '暂无当前发布通道的版本信息'
  }
})
const resultType = computed(() => {
  if (props.checking) return 'info'
  if (props.status?.state === 'error') return 'error'
  if (props.status?.state === 'update') return 'warning'
  return props.status?.state === 'current' ? 'success' : 'info'
})
</script>

<template>
  <el-dialog v-model="visible" title="面板版本" width="680px" align-center class="program-version-dialog">
    <el-alert :title="resultLabel" :type="resultType" :closable="false" show-icon />
    <div class="version-summary">
      <div><span>当前版本</span><strong>{{ status?.current || '未知' }}</strong></div>
      <div><span>最新发布</span><strong>{{ status?.latest || '暂无' }}</strong></div>
    </div>
    <p class="check-time" v-if="dateLabel(checkedAt)">最近成功检测：{{ dateLabel(checkedAt) }}</p>
    <template v-if="release">
      <div class="release-heading">
        <h3>{{ release.name }}</h3>
        <el-tag size="small" :type="release.prerelease ? 'warning' : 'success'">{{ release.prerelease ? '预发布' : '正式版' }}</el-tag>
      </div>
      <p class="release-meta">{{ release.tagName }}<span v-if="dateLabel(release.publishedAt)"> · 发布于 {{ dateLabel(release.publishedAt) }}</span></p>
      <p v-if="checking || status?.state === 'error'" class="cached-notice">以下为最近成功获取的发布说明。</p>
      <!-- 仅渲染禁用原始 HTML、过滤危险链接后的 Markdown。 -->
      <div v-if="release.body.trim()" class="release-notes" v-html="releaseHtml" />
      <el-empty v-else description="此 Release 未填写发布说明" :image-size="64" />
    </template>
    <el-skeleton v-else-if="checking" :rows="4" animated class="release-loading" />
    <el-empty v-else :description="status?.state === 'error' ? '未能获取发布说明，可稍后重试' : '暂无对应的 Release'" :image-size="64" />
    <template #footer>
      <div class="dialog-actions">
        <el-button @click="$emit('refresh')" :loading="checking">重新检测</el-button>
        <a v-if="status?.releaseUrl" :href="status.releaseUrl" target="_blank" rel="noopener noreferrer" class="release-link">在 GitHub 查看</a>
        <el-button type="primary" @click="visible = false">关闭</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.version-summary { display: flex; gap: 24px; flex-wrap: wrap; margin-top: 18px; }
.version-summary div { display: flex; gap: 10px; align-items: baseline; min-width: 0; }
.version-summary span, .check-time, .release-meta { color: var(--el-text-color-secondary); }
.version-summary strong { overflow-wrap: anywhere; color: var(--el-text-color-primary); }
.check-time, .release-meta, .cached-notice { font-size: 12px; }
.release-heading { display: flex; align-items: center; gap: 10px; margin-top: 20px; }
.release-heading h3 { margin: 0; overflow-wrap: anywhere; font-size: 16px; }
.release-heading .el-tag { flex-shrink: 0; }
.release-meta { margin: 8px 0 16px; overflow-wrap: anywhere; }
.cached-notice { color: var(--el-color-warning-dark-2); }
.release-loading { margin-top: 24px; }
.release-notes { max-height: 45vh; overflow: auto; overflow-wrap: anywhere; padding-right: 8px; line-height: 1.7; color: var(--el-text-color-primary); }
.release-notes :deep(h1), .release-notes :deep(h2), .release-notes :deep(h3) { font-size: 16px; margin: 18px 0 8px; }
.release-notes :deep(p) { margin: 8px 0; }
.release-notes :deep(ul), .release-notes :deep(ol) { padding-left: 24px; }
.release-notes :deep(a), .release-link { color: var(--el-color-primary); }
.release-notes :deep(blockquote) { margin: 12px 0; padding: 8px 16px; border-left: 4px solid var(--el-border-color); background: var(--el-fill-color-light); }
.release-notes :deep(.release-alert--warning), .release-notes :deep(.release-alert--caution) { border-color: var(--el-color-warning); background: var(--el-color-warning-light-9); }
.release-notes :deep(.release-alert-title) { font-weight: 600; margin-bottom: 4px; }
.release-notes :deep(code) { background: var(--el-fill-color-light); border-radius: 3px; padding: 2px 4px; }
.release-notes :deep(pre) { overflow: auto; padding: 12px; background: var(--el-fill-color-light); }
.release-notes :deep(pre code) { padding: 0; }
.release-notes :deep(table) { border-collapse: collapse; }
.release-notes :deep(td), .release-notes :deep(th) { border: 1px solid var(--el-border-color); padding: 6px 10px; }
.dialog-actions { display: flex; justify-content: flex-end; align-items: center; gap: 12px; flex-wrap: wrap; }
.release-link { font-size: 14px; text-decoration: none; }
.release-link:hover { text-decoration: underline; }
</style>

<style>
.program-version-dialog { max-width: calc(100vw - 32px); }
.program-version-dialog .el-dialog__body { max-height: calc(100dvh - 160px); overflow-y: auto; }
</style>
