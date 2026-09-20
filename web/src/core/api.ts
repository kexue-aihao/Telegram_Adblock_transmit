import { activeModalRef } from './dialog'
import { state } from './stores'
import { toast } from './toast'
import type { ApiError } from './types/api'

export interface ApiOptions {
  method?: string
  body?: unknown
  signal?: AbortSignal
}

let pageController: AbortController | undefined

// Each page change cancels the previous page's GET requests. Mutations are
// never cancelled by navigation.
export function rotatePageRequests(): void {
  pageController?.abort()
  pageController = new AbortController()
}

export function abortPageRequests(): void {
  pageController?.abort()
}

// pageSignal exposes the current page's signal so a mutation that should die
// with its page (for example the built-in text tester) can opt into it.
export function pageSignal(): AbortSignal | undefined {
  return pageController?.signal
}

export async function api(path: string, options: ApiOptions = {}): Promise<any> {
  const method = options.method || 'GET'
  const pageSignal = method === 'GET' ? pageController?.signal : undefined
  const signal = options.signal && pageSignal && typeof AbortSignal.any === 'function'
    ? AbortSignal.any([options.signal, pageSignal])
    : options.signal || pageSignal
  const headers: Record<string, string> = { 'X-Requested-With': 'fetch' }
  const init: RequestInit = { method, credentials: 'same-origin', headers, signal }
  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    init.body = JSON.stringify(options.body)
  }
  let response: Response
  try {
    response = await fetch(path, init)
  } catch (err) {
    if ((err as Error).name === 'AbortError') throw err
    throw new Error('网络连接失败，请检查连接后重试。')
  }
  const data = response.status === 204 ? null : await response.json().catch(() => null)
  if (!response.ok) {
    const error: ApiError = new Error((data && data.error) || '请求失败（' + response.status + '），请稍后重试。')
    error.status = response.status
    error.code = data && data.code
    if (response.status === 401 && (!error.code || error.code === 'unauthorized') && path !== '/api/login' && state.authenticated) {
      // The shell renders the login view as soon as this flips, so the expired
      // session never leaves a half-rendered page behind.
      state.authenticated = false
      activeModalRef.current?.close(true)
      toast('登录已过期，请重新登录。', 'warn')
    }
    throw error
  }
  return data
}
