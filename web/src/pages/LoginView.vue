<script setup lang="ts">
import { nextTick, ref } from 'vue'

import Icon from '../components/Icon.vue'
import { api } from '../core/api'
import { busy } from '../core/dom'
import { state } from '../core/stores'

const username = ref('')
const password = ref('')
const error = ref('')
const submitButton = ref<HTMLButtonElement | null>(null)
const passwordInput = ref<HTMLInputElement | null>(null)

async function onSubmit(): Promise<void> {
  const control = submitButton.value
  if (!control) return
  error.value = ''
  await busy(control, '登录中…', async () => {
    try {
      await api('/api/login', { method: 'POST', body: { username: username.value.trim(), password: password.value } })
      state.authenticated = true
      state.username = username.value.trim()
    } catch (err) {
      error.value = (err as Error).message
      await nextTick()
      passwordInput.value?.focus()
    }
  })
}
</script>

<template>
  <div class="login-wrap">
    <form class="login-form" @submit.prevent="onSubmit">
      <div class="login-mark"><Icon name="shield-check" /></div>
      <p class="eyebrow">TELEGRAM MODERATION</p>
      <h1>登录管理面板</h1>
      <p class="login-description">让社区交流，回归内容本身。</p>
      <div class="field">
        <label for="login-user">用户名</label>
        <input id="login-user" v-model="username" name="username" autocomplete="username" required spellcheck="false">
      </div>
      <div class="field">
        <label for="login-pass">密码</label>
        <input id="login-pass" ref="passwordInput" v-model="password" name="password" type="password"
          autocomplete="current-password" required>
      </div>
      <div id="login-error">
        <div v-if="error" class="notice err" role="alert"><Icon name="circle-alert" /><span>{{ error }}</span></div>
      </div>
      <button ref="submitButton" type="submit" class="btn primary block"><span>登录</span></button>
    </form>
  </div>
</template>
