import axios from 'axios'
import { ElMessage } from 'element-plus'

const request = axios.create({
  baseURL: '/api',
  timeout: 20000,
})

request.interceptors.request.use((config) => {
  const token = localStorage.getItem('dfpanel_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

request.interceptors.response.use(
  (res) => res.data,
  (err) => {
    const status = err.response?.status
    const msg = err.response?.data?.error || err.message || '请求失败'
    if (status === 401) {
      localStorage.removeItem('dfpanel_token')
      // 401 也可能是「登录时账号密码错误」：提示必须照样弹，
      // 否则登录页上点了登录什么都不发生（既不跳转、也不报错）
      ElMessage.error({ message: msg, grouping: true })
      if (window.location.pathname !== '/login') {
        window.location.href = '/login'
      }
    } else {
      ElMessage.error(msg)
    }
    return Promise.reject(err)
  },
)

export default request
