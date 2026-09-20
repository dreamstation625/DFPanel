<script setup lang="ts">
// 字段说明角标：悬停展示功能说明，点击跳转到 frp 官方文档对应小节
defineProps<{
  /** 官方文档地址（含锚点） */
  doc: string
  /** 功能说明文本 */
  text?: string
}>()
</script>

<template>
  <el-tooltip placement="top" :show-after="120" effect="dark">
    <template #content>
      <div class="tip">
        <div v-if="text" class="tip-text">{{ text }}</div>
        <div class="tip-link">点击问号查看官方文档</div>
      </div>
    </template>
    <a :href="doc" target="_blank" rel="noopener noreferrer" class="help" @click.stop>
      <el-icon><QuestionFilled /></el-icon>
    </a>
  </el-tooltip>
</template>

<style scoped>
.help {
  display: inline-flex;
  align-items: center;
  margin-left: 4px;
  color: #c0c4cc;
  cursor: pointer;
  vertical-align: middle;
  outline: none;
  transition: color 0.2s;
}

.help:hover {
  color: #409eff;
}

.tip {
  max-width: 260px;
  line-height: 1.6;
}

.tip-link {
  margin-top: 4px;
  color: #a3c7f7;
  font-size: 12px;
}
</style>
