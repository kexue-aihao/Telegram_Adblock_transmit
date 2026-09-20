<script setup lang="ts">
import { useRoute } from 'vue-router'

import Icon from '../components/Icon.vue'
import { routeHref } from '../core/params'
import { state } from '../core/stores'

const route = useRoute()

const items = [
  { route: 'dashboard', label: '仪表盘', icon: 'layout-dashboard' },
  { route: 'rules', label: '规则管理', icon: 'list-filter' },
  { route: 'audit', label: '审计日志', icon: 'scroll-text' },
  { route: 'settings', label: '设置', icon: 'settings' },
]

function currentRoute(): string {
  return String(route.path || '/').replace(/^\//, '')
}

// The built-in library has no navigation item of its own: it is reached from a
// tab inside the rules page, so it keeps that item highlighted.
function isActive(item: string): boolean {
  const current = currentRoute()
  return item === (current === 'builtin' ? 'rules' : current)
}
</script>

<template>
  <aside id="sidebar" class="sidebar" :hidden="!state.authenticated">
    <div class="brand">
      <span class="brand-mark"><Icon name="shield-check" /></span>
      <span>
        <span class="brand-title">广告拦截</span>
        <span class="brand-sub">Telegram Adblock</span>
      </span>
    </div>
    <nav class="nav" aria-label="主导航">
      <span class="nav-indicator" aria-hidden="true"></span>
      <a v-for="item in items" :key="item.route" :href="routeHref(item.route)" class="nav-item" :data-route="item.route"
        :aria-current="isActive(item.route) ? 'page' : undefined">
        <Icon :name="item.icon" /><span>{{ item.label }}</span>
      </a>
    </nav>
    <div class="sidebar-note">
      <span class="sidebar-caption">专注社区，守护交流</span>
      <span>Telegram Moderation</span>
    </div>
  </aside>
</template>
