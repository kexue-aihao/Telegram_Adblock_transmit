import { num } from './format'
import { panelMotion } from './motion'

// DOM primitives shared by the not-yet-migrated page renderers. New Vue
// components emit the same class names directly instead of calling these, but
// anything that builds DOM imperatively (dialogs, legacy pages) uses this one
// implementation. Values are typed loosely on purpose: the renderers are still
// the original dynamic JavaScript and are tightened as they migrate.
/* eslint-disable @typescript-eslint/no-explicit-any */

// All API values are inserted as text, including patterns and Telegram messages.
export function el(tag: string, props?: Record<string, any> | null, ...children: any[]): any {
  const node = document.createElement(tag)
  for (const [key, value] of Object.entries(props || {})) {
    if (key === 'class') node.className = value as string
    else if (key === 'value') continue
    else if (['checked', 'disabled', 'hidden', 'required'].includes(key)) (node as any)[key] = value
    else if (key === 'dataset') Object.assign(node.dataset, value as object)
    else if (key.startsWith('on') && typeof value === 'function') node.addEventListener(key.slice(2), value as EventListener)
    else if (value !== null && value !== undefined) node.setAttribute(key, value as string)
  }
  for (const child of children.flat(Infinity)) {
    if (child !== null && child !== undefined) node.append(child instanceof Node ? child : String(child))
  }
  if (props && props.value !== undefined) (node as any).value = props.value
  return node
}

export function empty(node: Element): Element {
  node.replaceChildren()
  return node
}

export function icon(name: string): SVGElement {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  svg.setAttribute('class', 'icon')
  svg.setAttribute('aria-hidden', 'true')
  const use = document.createElementNS(svg.namespaceURI, 'use')
  use.setAttribute('href', '/assets/icons.svg#' + name)
  svg.append(use)
  return svg
}

export function button(
  label: string,
  name: string | null,
  handler?: ((event: Event) => void) | null,
  props: Record<string, any> = {},
): HTMLButtonElement {
  return el('button', {
    type: 'button', class: 'btn', onclick: (event: Event) => {
      // WebKit does not focus pointer-clicked buttons by default. Keep a reliable
      // invoker for dialog focus restoration across all supported browsers.
      ;(event.currentTarget as HTMLElement).focus({ preventScroll: true })
      handler?.(event)
    }, ...props,
  }, name ? icon(name) : null, el('span', null, label))
}

export function iconButton(
  label: string,
  name: string,
  handler: ((event: Event) => void) | null,
  props: Record<string, any> = {},
): HTMLButtonElement {
  const control = button(label, null, handler, { class: 'icon-btn', 'aria-label': label, title: label, ...props })
  control.replaceChildren(icon(name))
  return control
}

export function field(label: string, control: any, hint?: string): HTMLElement {
  return el('div', { class: 'field' }, el('label', { for: control.id }, label), control,
    hint ? el('p', { class: 'hint' }, hint) : null)
}

export function notice(text: string, kind: 'err' | 'warn' = 'err'): HTMLElement {
  return el('div', { class: 'notice ' + kind, role: kind === 'err' ? 'alert' : 'status' }, icon('circle-alert'), el('span', null, text))
}

export function loading(text = '正在加载…', shape = 'rows'): HTMLElement {
  return el('div', { class: 'loading skeleton-' + shape, role: 'status' },
    el('div', { class: 'skeleton-shapes', 'aria-hidden': 'true' }, Array.from({ length: shape === 'stats' ? 4 : 3 }, () => el('span', { class: 'skeleton-block' }))),
    el('span', { class: 'loading-caption' }, text))
}

export async function busy<T>(btn: any, label: string, work: () => Promise<T>): Promise<T | undefined> {
  if (btn.disabled) return
  const children = [...btn.childNodes]
  const previousWidth = btn.style.width
  const previousLabel = btn.getAttribute('aria-label')
  const width = btn.offsetWidth
  btn.style.width = width + 'px'
  btn.disabled = true
  btn.setAttribute('aria-busy', 'true')
  btn.setAttribute('aria-label', label)
  btn.replaceChildren(el('span', { class: 'spinner', 'aria-hidden': 'true' }),
    btn.classList.contains('icon-btn') ? '' : el('span', { class: 'busy-label' }, label))
  try {
    return await work()
  } finally {
    btn.disabled = false
    btn.removeAttribute('aria-busy')
    btn.replaceChildren(...children)
    btn.style.width = previousWidth
    if (previousLabel === null) btn.removeAttribute('aria-label')
    else btn.setAttribute('aria-label', previousLabel)
  }
}

export function renderError(region: any, err: any, retry?: (() => void) | null): void {
  if (!region.isConnected || err?.name === 'AbortError') return
  region.querySelectorAll(':scope > .request-error').forEach((node: Element) => node.remove())
  if (region.querySelector('.loading')) empty(region)
  region.append(el('div', { class: 'request-error' }, notice(err.message), retry ? button('重试', 'refresh-cw', retry) : null))
}

export function pageHeader(title: string, meta?: string, actions: any[] = []): HTMLElement {
  return el('header', { class: 'page-header' },
    el('div', null, el('h1', { tabindex: '-1' }, title), meta ? el('p', { class: 'page-meta' }, meta) : null),
    el('div', { class: 'actions' }, actions))
}

export function table(headers: string[], className = ''): { body: HTMLElement; wrap: HTMLElement } {
  const body = el('tbody')
  const element = el('table', { class: 'data ' + className },
    el('thead', null, el('tr', null, headers.map((h) => el('th', { scope: 'col' }, h)))), body)
  return { body, wrap: el('div', { class: 'table-wrap' }, element) }
}

export function cell(label: string, content: any, className = ''): HTMLElement {
  return el('td', { class: className, 'data-label': label }, content)
}

export function emptyState(title: string, action?: any): HTMLElement {
  return el('div', { class: 'empty-state' }, icon('search'), el('p', null, title), action)
}

export function pagination(page: number, totalPages: number, total: number, onPage: (page: number) => void): HTMLElement {
  return el('nav', { class: 'pagination', 'aria-label': '分页' },
    el('span', { class: 'pagination-info', role: 'status' }, '共 ' + num(total) + ' 条，第 ' + page + ' / ' + Math.max(1, totalPages) + ' 页'),
    iconButton('上一页', 'chevron-left', () => onPage(page - 1), { disabled: page <= 1 }),
    iconButton('下一页', 'chevron-right', () => onPage(page + 1), { disabled: page >= totalPages }))
}

// Re-exported so legacy renderers keep a single import site.
export { panelMotion }
