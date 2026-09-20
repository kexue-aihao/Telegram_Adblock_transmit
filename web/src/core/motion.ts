// A single, cancellable animation owner. No timers, frame loops or layout
// polling. Every animation clears the inline will-change hint it set, which the
// motion regression suite asserts after every navigation.
const preference = matchMedia("(prefers-reduced-motion: reduce)")
const running = new Map<Element, { animation: Animation; cleanup: () => void }>()
const easing = "cubic-bezier(0.22, 1, 0.36, 1)"

function stop(node: Element): void {
  running.get(node)?.cleanup()
}

function play(
  node: Element,
  frames: Keyframe[],
  duration = 160,
  options: KeyframeAnimationOptions = {},
  done: () => void = () => {},
): void {
  stop(node)
  if (preference.matches || !node.animate || !node.isConnected) {
    done()
    return
  }
  const previous = (node as HTMLElement).style.willChange
  ;(node as HTMLElement).style.willChange = "transform, opacity"
  const animation = node.animate(frames, { duration, easing, fill: "backwards", ...options })
  let finished = false
  const cleanup = (): void => {
    if (finished) return
    finished = true
    if (running.get(node)?.animation === animation) {
      running.delete(node)
      ;(node as HTMLElement).style.willChange = previous
    }
    animation.cancel()
    done()
  }
  running.set(node, { animation, cleanup })
  // Cancellation is normal during rapid navigation, never an unhandled rejection.
  animation.finished.then(cleanup, cleanup)
}

function clear(root: Element): void {
  for (const [node, entry] of running) {
    if (node === root || root.contains(node)) entry.cleanup()
  }
}

function reveal(node: Element | null, duration = 160, delay = 0): void {
  if (!node) return
  play(node, [
    { opacity: 0, transform: "translateY(8px)" },
    { opacity: 1, transform: "translateY(0)" },
  ], duration, { delay })
}

preference.addEventListener("change", () => {
  if (preference.matches) for (const entry of running.values()) entry.cleanup()
})
document.addEventListener("visibilitychange", () => {
  if (document.hidden) for (const entry of running.values()) entry.cleanup()
})

export const panelMotion = { play, reveal, clear, reduced: (): boolean => preference.matches }
