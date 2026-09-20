// Hash-route helpers. The router registers an adapter at startup so the legacy
// renderers can read and rewrite the query without importing the router (which
// would create an import cycle).
import { reactive } from 'vue'

import { state } from './stores'

export interface RouteAdapter {
  route: () => string
  query: () => URLSearchParams
  replaceQuery: (values: Record<string, unknown>) => void
  reload: () => void
}

let adapter: RouteAdapter | undefined

export function setRouteAdapter(next: RouteAdapter): void {
  adapter = next
}

export function routeParams(): URLSearchParams {
  if (adapter) return adapter.query()
  return new URLSearchParams(location.hash.split('?')[1] || '')
}

// syncParams replaces the current history entry, so filters survive a reload
// without polluting the back button.
export function syncParams(values: Record<string, unknown>): void {
  adapter?.replaceQuery(values)
}

export function reloadPage(): void {
  adapter?.reload()
}

export function currentRoute(): string {
  return adapter ? adapter.route() : state.route
}

// Each navigation item keeps the last query string used on its own route, so
// returning to a page re-enters the filters the operator left behind.
export const routeQueries = reactive<Record<string, string>>({})

export function rememberRouteQuery(route: string, search: string): void {
  routeQueries[route] = search
}

export function routeHref(route: string): string {
  const search = routeQueries[route]
  return '#/' + route + (search ? '?' + search : '')
}

export function hashURL(route: string, values: Record<string, unknown> = {}): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(values)) {
    if (value !== '' && value !== null && value !== undefined) params.set(key, String(value))
  }
  return '#/' + route + (params.size ? '?' + params.toString() : '')
}
