import { panelMotion } from '../core/motion'

// The navigation indicator is a 2x16px accent bar that slides between items.
// Only transform changes during the animation, so no framework binding may touch
// its inline styles (a patch that clears cssText would also clear the motion
// layer hint).
export function syncNavIndicator(animate = true): void {
  const nav = document.querySelector('.nav')
  const selected = nav?.querySelector<HTMLElement>('[aria-current="page"]')
  const marker = nav?.querySelector<HTMLElement>('.nav-indicator')
  const sidebar = document.getElementById('sidebar')
  if (!nav || !selected || !marker || sidebar?.hidden) return
  const navBox = nav.getBoundingClientRect()
  const itemBox = selected.getBoundingClientRect()
  // Below 900px the rail becomes a horizontal bar and the marker follows it.
  const horizontal = navBox.width > navBox.height * 2
  const next = horizontal
    ? `translateX(${Math.round(itemBox.left - navBox.left + (itemBox.width - marker.offsetWidth) / 2)}px)`
    : `translateY(${Math.round(itemBox.top - navBox.top + (itemBox.height - marker.offsetHeight) / 2)}px)`
  const previous = marker.style.transform
  const ready = marker.dataset.ready === 'true'
  panelMotion.clear(marker)
  marker.style.transform = next
  marker.dataset.ready = 'true'
  if (!animate || !ready || previous === next) return
  panelMotion.play(marker, [{ transform: previous }, { transform: next }], 320, { easing: 'cubic-bezier(0.16, 1, 0.3, 1)' })
}
