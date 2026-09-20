<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { authApi } from '@/api'

const router = useRouter()
const loading = ref(false)
const form = ref({ username: 'admin', password: '', confirm: '' })

async function submit() {
  if (form.value.username.length < 3) {
    ElMessage.warning('用户名至少 3 个字符')
    return
  }
  if (form.value.password.length < 6) {
    ElMessage.warning('密码至少 6 个字符')
    return
  }
  if (form.value.password !== form.value.confirm) {
    ElMessage.warning('两次输入的密码不一致')
    return
  }
  loading.value = true
  try {
    await authApi.init({ username: form.value.username, password: form.value.password })
    ElMessage.success('初始化成功，请登录')
    router.push('/login')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="auth-page">
    <el-card class="auth-card">
      <div class="brand">
        <h1>初始化 DFPanel</h1>
        <p>首次使用，请创建管理员账号</p>
      </div>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="管理员用户名">
          <el-input v-model="form.username" size="large" placeholder="至少 3 个字符" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input v-model="form.password" type="password" show-password size="large" placeholder="至少 6 个字符" />
        </el-form-item>
        <el-form-item label="确认密码">
          <el-input v-model="form.confirm" type="password" show-password size="large" @keyup.enter="submit" />
        </el-form-item>
        <el-button type="primary" size="large" style="width: 100%" :loading="loading" @click="submit">
          创建并进入
        </el-button>
      </el-form>
    </el-card>
  </div>
</template>

<style scoped>
.auth-page {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1f2d3d 0%, #3a5169 100%);
}

.auth-card {
  width: 400px;
  padding: 12px 8px;
}

.brand {
  text-align: center;
  margin-bottom: 20px;
}

.brand h1 {
  margin: 0;
  font-size: 22px;
}

.brand p {
  margin: 6px 0 0;
  color: #909399;
  font-size: 13px;
}
</style>
