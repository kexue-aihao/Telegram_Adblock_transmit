import { panelMotion } from '../core/motion'

// Progressive enhancement for imperatively rendered pages. The page renderers
// (Vue or legacy) only emit plain markup; this module adds the animated
// selection thumb behind segmented controls and rule tabs, and staggers the
// entrance of repeated cards. Nothing here is required: without it every
// control still works and simply looks static.

const THUMB = 'segmented-thumb'
const STAGGERED = ['stats-grid .stat', 'inventory']
const MAX_STAGGER_ITEMS = 12

// positionThumb keeps the sliding pill on the active option.
function positionThumb(container: HTMLElement, animate: boolean): void {
  const active = container.querySelector<HTMLElement>('[aria-current="page"], [aria-pressed="true"]')
  let thumb = container.querySelector<HTMLElement>('.' + THUMB)
  if (!active) {
    thumb?.remove()
    return
  }
  const created = !thumb
  if (!thumb) {
    thumb = document.createElement('span')
    thumb.className = THUMB
    thumb.setAttribute('aria-hidden', 'true')
    container.prepend(thumb)
  }
  const pad = 3
  const box = container.getBoundingClientRect()
  const target = active.getBoundingClientRect()
  const next = {
    transform: `translateX(${Math.round(target.left - box.left - pad)}px)`,
    width: `${Math.round(target.width)}px`,
  }
  const settled = thumb.dataset.ready === 'true'
  const from = { transform: thumb.style.transform, width: thumb.style.width }
  thumb.style.transform = next.transform
  thumb.style.width = next.width
  thumb.dataset.ready = 'true'
  if (!animate || created || !settled || from.transform === next.transform) return
  panelMotion.play(thumb, [
    { transform: from.transform, width: from.width },
    { transform: next.transform, width: next.width },
  ], 240)
}

function decorateSelections(root: ParentNode): void {
  root.querySelectorAll<HTMLElement>('.segmented, .rule-tabs, .quick-dates').forEach((container) => {
    positionThumb(container, true)
  })
}

// staggerReveal plays the entrance of repeated cards once per element.
function staggerReveal(root: ParentNode): void {
  if (panelMotion.reduced()) return
  let index = 0
  for (const selector of STAGGERED) {
    root.querySelectorAll<HTMLElement>(selector).forEach((node) => {
      if (node.dataset.entered === 'true') return
      node.dataset.entered = 'true'
      if (index >= MAX_STAGGER_ITEMS) return
      panelMotion.reveal(node, 260, index * 28)
      index++
    })
  }
}

// animateTrend plays the chart entrance: the day columns rise in sequence.
// The chart enters as one surface: animating every column separately starts
// dozens of animations per navigation and costs far more than it adds.
function animateTrend(root: ParentNode): void {
  if (panelMotion.reduced()) return
  const chart = root.querySelector<HTMLElement>('.trend-region .chart-scroll')
  if (!chart || chart.dataset.entered === 'true') return
  chart.dataset.entered = 'true'
  panelMotion.reveal(chart, 320)
}

let scheduled = false

function decorate(root: ParentNode): void {
  if (scheduled) return
  scheduled = true
  requestAnimationFrame(() => {
    scheduled = false
    decorateSelections(root)
    staggerReveal(root)
    animateTrend(root)
  })
}

// observeDecorations watches the page container so newly rendered controls get
// their thumb without the renderer knowing anything about it.
export function observeDecorations(container: HTMLElement): () => void {
  const run = (): void => decorate(container)
  run()
  const observer = new MutationObserver(run)
  // childList catches new controls, attributes catches a renderer flipping
  // aria-pressed/aria-current on an existing one.
  observer.observe(container, {
    childList: true,
    subtree: true,
    attributes: true,
    attributeFilter: ['aria-pressed', 'aria-current'],
  })
  const onResize = (): void => {
    container.querySelectorAll<HTMLElement>('.segmented, .rule-tabs, .quick-dates').forEach((node) => positionThumb(node, false))
  }
  window.addEventListener('resize', onResize)
  return () => {
    observer.disconnect()
    window.removeEventListener('resize', onResize)
  }
}
