import { nextTick } from 'vue'
import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'

import { rotatePageRequests } from './core/api'
import { activeModalRef } from './core/dialog'
import { panelMotion } from './core/motion'
import { rememberRouteQuery, setRouteAdapter } from './core/params'
import { state } from './core/stores'
import type { LegacyRoute as LegacyRouteName } from './legacy/legacy'
import LegacyRoute from './pages/LegacyRoute.vue'
import { syncNavIndicator } from './shell/navIndicator'
import { requestPageReload } from './shell/pageReload'

export const PAGE_TITLES: Record<string, string> = {
  dashboard: '仪表盘',
  rules: '规则管理',
  builtin: '内置广告库',
  audit: '审计日志',
  settings: '设置',
}

// Pages still rendered by the imperative legacy module. Each migration phase
// replaces one entry with a Vue component.
const LEGACY_PAGES: LegacyRouteName[] = ['dashboard', 'rules', 'builtin', 'audit', 'settings']

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/dashboard' },
  ...LEGACY_PAGES.map((name) => ({
    path: `/${name}`,
    name,
    component: LegacyRoute,
    props: { route: name },
  })),
]

export const router = createRouter({ history: createWebHashHistory(), routes })

let exiting: HTMLElement | null = null
// The route whose page is currently on screen. A burst of hash changes can
// return to it before the intermediate route ever rendered, and cloning the
// page that is about to be rendered again would duplicate its content.
let renderedRoute = ''

// Snapshot the outgoing page before Vue swaps it. The clone carries no ids and
// is inert, so nothing observes a duplicate element while it fades away.
function captureExitingPage(): void {
  // Rapid navigation cancels the previous effect: at most one snapshot exists,
  // so a page's contents can never be in the document twice during a burst.
  // (A snapshot is handed to the DOM in afterEach, so it is tracked here as a
  // document element rather than through the pending capture variable.)
  exiting = null
  document.querySelectorAll('#view > .page-exit').forEach((node) => {
    panelMotion.clear(node)
    node.remove()
  })
  if (panelMotion.reduced()) return
  const current = document.querySelector<HTMLElement>('#view .page:not(.page-exit)')
  if (!current) return
  const clone = current.cloneNode(true) as HTMLElement
  clone.classList.add('page-exit')
  clone.inert = true
  clone.setAttribute('aria-hidden', 'true')
  clone.removeAttribute('id')
  clone.querySelectorAll('[id]').forEach((node) => node.removeAttribute('id'))
  exiting = clone
}

router.beforeEach((to, from) => {
  // Navigation is refused while a dialog needs an answer; Vue Router restores
  // the previous hash, exactly like the pre-Vue shell did.
  const modal = activeModalRef.current
  if (modal && !modal.close(false, true)) return false
  // Only a page change cancels requests: pages rewrite their own query string
  // (filters) through the adapter, and that must not abort the request they
  // are waiting for.
  if (to.path !== from.path) {
    rotatePageRequests()
    if (to.path !== '/' + renderedRoute) captureExitingPage()
  }
  return true
})

// markRouteRendered is called by the page adapter once its component is on
// screen, so the transition logic knows which route the DOM currently shows.
export function markRouteRendered(route: string): void {
  renderedRoute = route
}

router.afterEach((to, from) => {
  const route = String(to.path).replace(/^\//, '')
  state.route = route
  state.hash = location.hash
  // The navigation item for this route keeps the filters it was left with.
  rememberRouteQuery(route, location.hash.split('?')[1] || '')
  document.title = (PAGE_TITLES[route] || '页面不存在') + ' · 广告拦截管理面板'
  if (exiting) {
    const clone = exiting
    exiting = null
    const root = document.getElementById('view')
    if (root) {
      root.append(clone)
      panelMotion.play(clone, [{ opacity: .65 }, { opacity: 0 }], 70, {}, () => clone.remove())
    }
  }
  if (to.path !== from.path) window.scrollTo({ top: 0, behavior: 'instant' })
  // The marker moves once Vue has re-rendered the navigation items: reading
  // aria-current here directly would still see the previous route.
  void nextTick(() => syncNavIndicator(to.path !== from.path))
})

// The legacy renderers read and rewrite query parameters through this adapter,
// which keeps filter state in the hash without importing the router.
setRouteAdapter({
  route: () => String(router.currentRoute.value.path).replace(/^\//, ''),
  query: () => new URLSearchParams(router.currentRoute.value.query as Record<string, string>),
  replaceQuery: (values) => {
    const query: Record<string, string> = {}
    for (const [key, value] of Object.entries(values)) {
      if (value !== '' && value !== null && value !== undefined) query[key] = String(value)
    }
    void router.replace({ path: router.currentRoute.value.path, query })
  },
  reload: requestPageReload,
})
