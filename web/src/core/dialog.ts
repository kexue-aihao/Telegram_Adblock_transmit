import { el, iconButton } from './dom'
import { panelMotion } from './motion'

export interface PanelModal {
  content: HTMLElement
  actions: HTMLElement
  dialog: HTMLDialogElement
  feedback: HTMLElement
  dirty: () => boolean
  saving: boolean
  closing: boolean
  /** force skips the guards, immediate skips the exit animation. */
  close: (force?: boolean, immediate?: boolean) => boolean
}

// Shared as a holder rather than a bare binding so the toast and request layers
// can read the open dialog without importing this module's internals.
export const activeModalRef: { current: PanelModal | null } = { current: null }

export function openModal(title: string, subtitle = ''): PanelModal | null {
  const current = activeModalRef.current
  if (current && !current.close(false, true)) return null
  const previousFocus = document.activeElement as HTMLElement | null
  const content = el('div', { class: 'modal-content' })
  const actions = el('div', { class: 'modal-actions' })
  const feedbackRegion = el('div', { class: 'modal-feedback', role: 'status', 'aria-live': 'polite' })
  const dialog: HTMLDialogElement = el('dialog', { class: 'modal', 'aria-labelledby': 'modal-title', tabindex: '-1' })
  const modal: PanelModal = {
    content,
    actions,
    dialog,
    feedback: feedbackRegion,
    dirty: () => false,
    saving: false,
    closing: false,
    close(force = false, immediate = false): boolean {
      if (modal.closing && !force && !immediate) return true
      if (!modal.closing && !force && (modal.saving || (modal.dirty() && !window.confirm('有未保存的修改，确定放弃吗？')))) return false
      modal.closing = true
      dialog.inert = true
      const finish = (): void => {
        if (!dialog.isConnected) return
        document.getElementById('toast-region')!.append(...feedbackRegion.childNodes)
        dialog.close()
        dialog.remove()
        if (activeModalRef.current === modal) activeModalRef.current = null
        if (previousFocus && previousFocus.isConnected) (previousFocus as HTMLElement).focus({ preventScroll: true })
        else (document.querySelector('.page:not(.page-exit) h1') as HTMLElement | null)?.focus({ preventScroll: true })
      }
      panelMotion.clear(dialog)
      if (force || immediate) finish()
      else panelMotion.play(dialog, [{ opacity: 1, transform: 'scale(1)' }, { opacity: 0, transform: 'translateY(8px) scale(.98)' }], 160, {}, finish)
      return true
    },
  }
  dialog.append(el('header', { class: 'modal-header' },
    el('div', null, el('h2', { id: 'modal-title' }, title), subtitle ? el('p', { class: 'hint' }, subtitle) : null),
    iconButton('关闭弹窗', 'x', () => modal.close())), content, feedbackRegion, actions)
  dialog.addEventListener('cancel', (event) => { event.preventDefault(); modal.close() })
  dialog.addEventListener('keydown', (event: KeyboardEvent) => {
    if (event.key !== 'Tab') return
    const controls = [...dialog.querySelectorAll('button, input, textarea, select, a[href], [tabindex]')]
      .filter((node) => !(node as HTMLButtonElement).disabled && (node as HTMLElement).tabIndex >= 0 && node.getClientRects().length) as HTMLElement[]
    const first = controls[0]
    const last = controls[controls.length - 1]
    if (!first || document.activeElement === dialog ||
      (event.shiftKey ? document.activeElement === first : document.activeElement === last)) {
      event.preventDefault()
      ;(event.shiftKey ? last : first)?.focus()
    }
  })
  dialog.addEventListener('click', (event: MouseEvent) => {
    if (event.target !== dialog) return
    const bounds = dialog.getBoundingClientRect()
    if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) modal.close()
  })
  document.getElementById('modal-root')!.append(dialog)
  activeModalRef.current = modal
  queueMicrotask(() => {
    if (!dialog.isConnected) return
    dialog.showModal()
    panelMotion.play(dialog, [{ opacity: 0, transform: 'translateY(12px) scale(.97)' }, { opacity: 1, transform: 'translateY(0) scale(1)' }], 240, { easing: 'cubic-bezier(0.16, 1.12, 0.3, 1)' })
    const first = matchMedia('(min-width: 720px)').matches ? content.querySelector('input, textarea, button') : null
    ;((first as HTMLElement) || dialog).focus()
  })
  return modal
}

window.addEventListener('beforeunload', (event) => {
  const modal = activeModalRef.current
  if (modal && (modal.dirty() || modal.saving)) {
    event.preventDefault()
    event.returnValue = ''
  }
})
