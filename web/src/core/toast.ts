import { activeModalRef } from './dialog'
import { el, icon } from './dom'
import { panelMotion } from './motion'

export type ToastKind = 'ok' | 'warn' | 'err'

export function toast(message: string, kind: ToastKind = 'ok'): void {
  const node = el('div', { class: 'toast ' + kind }, icon(kind === 'ok' ? 'check' : 'circle-alert'), el('span', null, message))
  const modal = activeModalRef.current
  const host = modal?.dialog.open ? modal.feedback : document.getElementById('toast-region')
  host?.append(node)
  panelMotion.reveal(node, 180)
  setTimeout(() => panelMotion.play(node, [{ opacity: 1 }, { opacity: 0, transform: 'translateY(6px)' }], 180, {}, () => node.remove()), kind === 'ok' ? 4500 : 9000)
}

// feedback renders the API's optional warning envelope before the plain message.
export function feedback(data: { warning?: string } | null | undefined, message: string): void {
  const warning = data?.warning === 'cache_refresh_failed'
    ? '规则已保存，但机器人规则缓存未刷新，可能尚未生效。请检查服务日志后重试保存。'
    : data?.warning
  toast(warning || message, warning ? 'warn' : 'ok')
}
