<script setup lang="ts">
import { nextTick, reactive, ref, watch } from 'vue'
import { type FormInstance, type FormRules } from 'element-plus'
import { authApi } from '@/api'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'changed'): void
}>()

const formRef = ref<FormInstance>()
const submitting = ref(false)
const form = reactive({ currentPassword: '', newPassword: '', confirmPassword: '' })
const rules: FormRules = {
  currentPassword: [{ required: true, message: '请输入当前密码', trigger: 'blur' }],
  newPassword: [{
    validator: (_rule, value: string, callback) => {
      const length = Array.from(value).length
      if (length < 6 || length > 64) {
        callback(new Error('新密码应为 6–64 个字符'))
      } else if (new TextEncoder().encode(value).length > 72) {
        callback(new Error('新密码不能超过 72 字节，请减少中文或其他多字节字符'))
      } else if (value === form.currentPassword) {
        callback(new Error('新密码不能与当前密码相同'))
      } else {
        callback()
      }
    },
    trigger: 'blur',
  }],
  confirmPassword: [{
    validator: (_rule, value: string, callback) => {
      if (!value) callback(new Error('请再次输入新密码'))
      else if (value !== form.newPassword) callback(new Error('两次输入的新密码不一致'))
      else callback()
    },
    trigger: 'blur',
  }],
}

function resetForm() {
  form.currentPassword = ''
  form.newPassword = ''
  form.confirmPassword = ''
  formRef.value?.clearValidate()
}

watch(() => props.modelValue, async (visible) => {
  if (visible) {
    resetForm()
    await nextTick()
    formRef.value?.clearValidate()
  }
})

function beforeClose(done: () => void) {
  if (!submitting.value) done()
}

async function submit() {
  if (submitting.value || !formRef.value) return
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid || submitting.value) return
  submitting.value = true
  try {
    await authApi.changePassword({ currentPassword: form.currentPassword, newPassword: form.newPassword })
    resetForm()
    emit('update:modelValue', false)
    emit('changed')
  } catch {
    // 接口失败由统一拦截器提示；保留表单，允许修正当前密码后重试。
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <el-dialog
    :model-value="modelValue"
    title="修改密码"
    width="min(440px, calc(100vw - 32px))"
    :close-on-click-modal="false"
    :close-on-press-escape="!submitting"
    :show-close="!submitting"
    :before-close="beforeClose"
    @update:model-value="emit('update:modelValue', $event)"
    @closed="resetForm"
  >
    <p class="notice">修改成功后，所有设备会退出登录，请使用新密码重新登录。</p>
    <el-form ref="formRef" :model="form" :rules="rules" label-position="top" :disabled="submitting" @submit.prevent="submit">
      <el-form-item label="当前密码" prop="currentPassword">
        <el-input v-model="form.currentPassword" type="password" show-password autocomplete="current-password" placeholder="请输入当前密码" />
      </el-form-item>
      <el-form-item label="新密码" prop="newPassword">
        <el-input v-model="form.newPassword" type="password" show-password autocomplete="new-password" placeholder="6–64 个字符" />
      </el-form-item>
      <el-form-item label="确认新密码" prop="confirmPassword">
        <el-input v-model="form.confirmPassword" type="password" show-password autocomplete="new-password" placeholder="请再次输入新密码" />
      </el-form-item>
      <div class="actions">
        <el-button :disabled="submitting" @click="emit('update:modelValue', false)">取消</el-button>
        <el-button type="primary" native-type="submit" :loading="submitting">修改密码</el-button>
      </div>
    </el-form>
  </el-dialog>
</template>

<style scoped>
.notice {
  margin: 0 0 20px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.actions .el-button + .el-button {
  margin-left: 0;
}
</style>
