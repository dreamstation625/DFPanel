<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessageBox } from 'element-plus'
import { DOC_MENU } from '@/utils/docs'

const route = useRoute()
const router = useRouter()

const active = computed(() => route.path)
const username = computed(() => localStorage.getItem('dfpanel_user') || 'admin')

function onSelect(index: string) {
  // 官方文档以外部链接形式放在菜单里，直接新窗口打开
  if (index.startsWith('http')) {
    window.open(index, '_blank', 'noopener')
    return
  }
  router.push(index)
}

async function onCommand(cmd: string) {
  if (cmd !== 'logout') return
  await ElMessageBox.confirm('确认退出登录？', '提示', { type: 'warning' })
  localStorage.removeItem('dfpanel_token')
  localStorage.removeItem('dfpanel_user')
  router.push('/login')
}
</script>

<template>
  <el-container class="layout">
    <el-aside width="236px" class="aside">
      <div class="logo">DFPanel</div>
      <el-menu :default-active="active" class="menu" @select="onSelect">
        <el-menu-item index="/dashboard">
          <el-icon><DataLine /></el-icon>
          <span>概览</span>
        </el-menu-item>
        <el-menu-item index="/server">
          <el-icon><Setting /></el-icon>
          <span>frps 服务端</span>
        </el-menu-item>
        <el-menu-item index="/agents">
          <el-icon><Connection /></el-icon>
          <span>Agent 管理</span>
        </el-menu-item>
        <el-menu-item index="/nodes">
          <el-icon><Monitor /></el-icon>
          <span>客户端节点</span>
        </el-menu-item>

        <el-sub-menu index="official-docs">
          <template #title>
            <el-icon><Reading /></el-icon>
            <span>官方文档</span>
          </template>
          <el-menu-item v-for="d in DOC_MENU" :key="d.url" :index="d.url" :title="d.label">
            {{ d.label }}
          </el-menu-item>
        </el-sub-menu>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="header">
        <div class="header-title">{{ route.meta.title || 'DFPanel' }}</div>
        <el-dropdown @command="onCommand">
          <span class="user">
            <el-icon><UserFilled /></el-icon>
            {{ username }}
            <el-icon><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </el-header>
      <el-main>
        <router-view />
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.layout {
  height: 100vh;
}

.aside {
  background: #1f2d3d;
  overflow-y: auto;
}

.logo {
  height: 60px;
  line-height: 60px;
  text-align: center;
  color: #fff;
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 1px;
}

.menu {
  border-right: none;
  background: transparent;
}

.menu :deep(.el-menu-item),
.menu :deep(.el-sub-menu__title) {
  color: #c0c4cc;
}

/* 侧边栏较窄：菜单项文字允许折行，避免被截断 */
.menu :deep(.el-menu-item) {
  height: auto;
  min-height: 44px;
  line-height: 1.4;
  white-space: normal;
  word-break: break-word;
  padding-top: 10px;
  padding-bottom: 10px;
}

/* 父级标题保持单行，避免与右侧箭头错位 */
.menu :deep(.el-sub-menu__title) {
  height: 50px;
  line-height: 50px;
}

.menu :deep(.el-menu-item.is-active) {
  color: #fff;
  background: #2b3a4d;
}

.menu :deep(.el-menu-item:hover),
.menu :deep(.el-sub-menu__title:hover) {
  background: #26364a;
}

/* 子菜单（官方文档）在深色背景下的可读性 */
.menu :deep(.el-menu--inline) {
  background: #1a2533;
}

.menu :deep(.el-menu--inline .el-menu-item) {
  padding-left: 40px;
  font-size: 13px;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
}

.header-title {
  font-size: 16px;
  font-weight: 600;
}

.user {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  outline: none;
}
</style>
