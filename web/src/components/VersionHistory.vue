<script setup lang="ts">
import { statusLabel, statusType, type ConfigVersionItem } from '@/api'

const props = defineProps<{
  versions: ConfigVersionItem[]
  loading?: boolean
  /** 历史快照是否来自 Agent 本地（否则只有面板记录） */
  fromAgent?: boolean
}>()

const emit = defineEmits<{ (e: 'rollback', version: number): void }>()

function fmtTime(v: ConfigVersionItem) {
  const t = v.time ? v.time * 1000 : v.createdAt ? Date.parse(v.createdAt) : 0
  return t ? new Date(t).toLocaleString() : '—'
}

function fmtSize(bytes?: number) {
  if (!bytes) return '—'
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KB`
}

/** Agent 模式下只有本地还保留快照的版本能回滚；本机托管模式走面板记录，可回滚 */
function canRollback(row: ConfigVersionItem) {
  if (row.current) return false
  return props.fromAgent ? !!row.onAgent : true
}
</script>

<template>
  <div v-loading="loading">
    <el-alert
      v-if="!fromAgent"
      type="info"
      show-icon
      :closable="false"
      title="当前显示面板侧的版本记录"
      description="Agent 离线，无法读取本地快照；仍可回滚（面板会重新下发）。"
      style="margin-bottom: 12px"
    />

    <el-table v-if="versions.length" :data="versions" size="small" max-height="440" style="width: 100%">
      <el-table-column label="版本" width="110">
        <template #default="{ row }">
          <span class="mono">v{{ row.version }}</span>
          <el-tag v-if="row.current" size="small" type="success" style="margin-left: 4px">当前</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="时间" width="170">
        <template #default="{ row }">{{ fmtTime(row) }}</template>
      </el-table-column>
      <el-table-column label="大小" width="90">
        <template #default="{ row }">{{ fmtSize(row.size) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="140">
        <template #default="{ row }">
          <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="说明" min-width="240">
        <template #default="{ row }">
          <span class="sub">{{ row.message || '—' }}</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="110" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" :disabled="!canRollback(row)" @click="emit('rollback', row.version)">
            回滚
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-empty v-else description="暂无历史版本（每次下发配置都会生成一份快照）" />
  </div>
</template>

<style scoped>
.mono {
  font-family: Consolas, Monaco, monospace;
}

.sub {
  color: #909399;
  font-size: 12px;
}
</style>
