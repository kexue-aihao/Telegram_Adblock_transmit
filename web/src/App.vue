<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterView, useRoute } from 'vue-router'

import Icon from './components/Icon.vue'
import { api } from './core/api'
import { state } from './core/stores'
import LoginView from './pages/LoginView.vue'
import SideBar from './shell/SideBar.vue'
import { syncNavIndicator } from './shell/navIndicator'
import { pageReloadToken } from './shell/pageReload'
import TopBar from './shell/TopBar.vue'

const route = useRoute()
const booting = ref(true)
const bootError = ref('')
let observer: ResizeObserver | undefined

function onResize(): void {
  syncNavIndicator(false)
}

function onSkipLink(event: MouseEvent): void {
  event.preventDefault()
  document.getElementById('view')?.focus()
}

function onReload(): void {
  window.location.reload()
}

onMounted(async () => {
  const nav = document.querySelector('.nav')
  if (nav && typeof ResizeObserver === 'function') {
    observer = new ResizeObserver(() => syncNavIndicator(false))
    observer.observe(nav)
  } else {
    window.addEventListener('resize', onResize)
  }
  try {
    const session = await api('/api/session')
    state.authenticated = !!session.authenticated
    state.username = session.username || ''
  } catch (err) {
    bootError.value = (err as Error).message
  } finally {
    booting.value = false
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  window.removeEventListener('resize', onResize)
})
</script>

<template>
  <div class="ambient" aria-hidden="true">
    <span class="ambient-orb orb-a" />
    <span class="ambient-orb orb-b" />
    <span class="ambient-orb orb-c" />
    <span class="ambient-orb orb-d" />
  </div>
  <div class="shell">
  <a class="skip-link" href="#view" @click="onSkipLink">跳到主要内容</a>
  <TopBar />
  <div class="layout">
    <SideBar />
    <main id="view" class="view" tabindex="-1">
      <div v-if="booting" class="loading skeleton-rows" role="status">
        <div class="skeleton-shapes" aria-hidden="true">
          <span class="skeleton-block" /><span class="skeleton-block" /><span class="skeleton-block" />
        </div>
        <span class="loading-caption">正在连接面板…</span>
      </div>
      <div v-else-if="bootError" class="request-error">
        <div class="notice err" role="alert"><Icon name="circle-alert" /><span>{{ bootError }}</span></div>
        <button class="btn" type="button" @click="onReload"><Icon name="refresh-cw" /><span>重试</span></button>
      </div>
      <LoginView v-else-if="!state.authenticated" />
      <RouterView v-else v-slot="{ Component }">
        <component :is="Component" :key="route.path + ':' + pageReloadToken" />
      </RouterView>
    </main>
  </div>
  </div>
  <div id="modal-root"></div>
  <div id="toast-region" class="toast-region" role="status" aria-live="polite" aria-relevant="additions"></div>
</template>
