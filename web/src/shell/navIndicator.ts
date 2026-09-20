import { panelMotion } from '../core/motion'

// The navigation indicator is positioned imperatively: only transform and size
// change during its animation, and no framework binding may touch its inline
// styles (a patch that clears cssText would also clear the motion layer hint).
export function syncNavIndicator(animate = true): void {
  const nav = document.querySelector('.nav')
  const selected = nav?.querySelector<HTMLElement>('[aria-current="page"]')
  const marker = nav?.querySelector<HTMLElement>('.nav-indicator')
  if (!nav || !selected || !marker) return
  const sidebar = document.getElementById('sidebar')
  if (sidebar?.hidden) return
  // Read all geometry before writing.
  const old = marker.getBoundingClientRect()
  const target = selected.getBoundingClientRect()
  const parent = nav.getBoundingClientRect()
  const ready = marker.dataset.ready === 'true'
  panelMotion.clear(marker)
  Object.assign(marker.style, {
    width: target.width + 'px',
    height: target.height + 'px',
    transform: `translate(${target.left - parent.left}px, ${target.top - parent.top}px)`,
  })
  marker.dataset.ready = 'true'
  if (animate && ready) {
    panelMotion.play(marker, [
      { transform: `translate(${old.left - parent.left}px, ${old.top - parent.top}px)` },
      { transform: marker.style.transform },
    ], 220)
  }
}
