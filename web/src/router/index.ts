import { createRouter, createWebHistory } from 'vue-router'
import axios from 'axios'

let initChecked = false
let initialized = false

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
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
  ],
})

router.beforeEach(async (to) => {
  if (!initChecked) {
    try {
      const res = await axios.get('/api/init-status')
      initialized = !!res.data?.initialized
    } catch {
      // 接口异常时按已初始化处理，避免卡在初始化页
      initialized = true
    }
    initChecked = true
  }

  if (!initialized) {
    return to.name === 'setup' ? true : { name: 'setup' }
  }
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
