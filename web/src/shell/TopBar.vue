<script setup lang="ts">
import { computed, ref } from 'vue'

import Icon from '../components/Icon.vue'
import { api } from '../core/api'
import { activeModalRef } from '../core/dialog'
import { busy } from '../core/dom'
import { resetSessionState, state } from '../core/stores'
import { currentTheme, themeToggleLabel, toggleTheme } from '../core/theme'
import { toast } from '../core/toast'

const theme = ref(currentTheme())
const themeLabel = computed(() => themeToggleLabel(theme.value))

function onToggleTheme(): void {
  theme.value = toggleTheme()
}

async function onLogout(event: MouseEvent): Promise<void> {
  const control = event.currentTarget as HTMLButtonElement
  await busy(control, '退出中…', async () => {
    try {
      await api('/api/logout', { method: 'POST' })
      state.authenticated = false
      resetSessionState()
      activeModalRef.current?.close(true)
    } catch (err) {
      toast((err as Error).message, 'err')
    }
  })
}
</script>

<template>
  <header class="topbar">
    <div class="topbar-actions">
      <span id="account-name" class="account-name">{{ state.authenticated ? state.username : '' }}</span>
      <button id="theme-toggle" class="icon-btn" type="button" :title="themeLabel" :aria-label="themeLabel" @click="onToggleTheme">
        <Icon name="sun" cls="theme-sun" />
        <Icon name="moon" cls="theme-moon" />
      </button>
      <span class="topbar-divider" aria-hidden="true" />
      <button id="logout-btn" class="icon-btn" type="button" title="退出登录" aria-label="退出登录"
        :hidden="!state.authenticated" @click="onLogout">
        <Icon name="log-out" />
      </button>
    </div>
  </header>
</template>
