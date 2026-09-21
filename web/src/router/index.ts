import { createRouter, createWebHistory } from 'vue-router'
import axios from 'axios'

let initChecked = false
let initialized = false

/**
 * 刷新「系统是否已初始化」的缓存，返回最新状态。
 *
 * ⚠️ 初始化接口调用成功后**必须**调用它：守卫里缓存的 initialized 仍是 false 时，
 * 会把刚跳过去的 /login 又弹回 /setup（前端表现为「初始化完成后还在初始化页」）。
 */
export async function refreshInitStatus(): Promise<boolean> {
  try {
    const res = await axios.get('/api/init-status')
    initialized = !!res.data?.initialized
  } catch {
    // 接口异常时按已初始化处理，避免卡在初始化页
    initialized = true
  }
  initChecked = true
  return initialized
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/setup',
      name: 'setup',
      component: () => import('@/views/Setup.vue'),
      meta: { public: true },
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/Login.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      component: () => import('@/layout/MainLayout.vue'),
      redirect: '/dashboard',
      children: [
        {
          path: 'dashboard',
          name: 'dashboard',
          component: () => import('@/views/Dashboard.vue'),
          meta: { title: '概览' },
        },
        {
          path: 'server',
          name: 'server',
          component: () => import('@/views/ServerConfig.vue'),
          meta: { title: 'frps 服务端' },
        },
        {
          path: 'agents',
          name: 'agents',
          component: () => import('@/views/Agents.vue'),
          meta: { title: 'Agent 管理' },
        },
        {
          path: 'nodes',
          name: 'nodes',
          component: () => import('@/views/Nodes.vue'),
          meta: { title: '客户端节点' },
        },
        {
          path: 'settings',
          name: 'settings',
          component: () => import('@/views/Settings.vue'),
          meta: { title: '设置' },
        },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
  ],
})

router.beforeEach(async (to) => {
  // 首次进入时取一次；**直接访问初始化页时强制重取** —— 只有这样才能覆盖
  //「刚初始化完」「另一个标签页已初始化」「浏览器回退到 /setup」这些情况。
  if (!initChecked || to.name === 'setup') {
    await refreshInitStatus()
  }

  if (!initialized) {
    // 未初始化：只能停在初始化页
    return to.name === 'setup' ? true : { name: 'setup' }
  }

  // 已初始化：初始化页彻底不可达，一律去登录页
  if (to.name === 'setup') {
    return { name: 'login' }
  }

  const token = localStorage.getItem('dfpanel_token')
  if (to.meta.public) {
    if (token && to.name === 'login') {
      return { name: 'dashboard' }
    }
    return true
  }
  if (!token) {
    return { name: 'login' }
  }
  return true
})

export default router
