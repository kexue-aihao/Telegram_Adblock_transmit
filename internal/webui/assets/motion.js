"use strict";

// A single, cancellable animation owner. No timers, frame loops or layout polling.
const panelMotion = (() => {
  const preference = matchMedia("(prefers-reduced-motion: reduce)");
  const running = new Map();
  const easing = "cubic-bezier(0.22, 1, 0.36, 1)";
  function stop(node) { running.get(node)?.cleanup(); }
  function play(node, frames, duration = 160, options = {}, done = () => {}) {
    stop(node);
    if (preference.matches || !node.animate || !node.isConnected) {
      done();
      return;
    }
    const previous = node.style.willChange;
    node.style.willChange = "transform, opacity";
    const animation = node.animate(frames, { duration, easing, fill: "backwards", ...options });
    let finished = false;
    const cleanup = () => {
      if (finished) return;
      finished = true;
      if (running.get(node)?.animation === animation) {
        running.delete(node);
        node.style.willChange = previous;
      }
      animation.cancel();
      done();
    };
    running.set(node, { animation, cleanup });
    // Cancellation is normal during rapid navigation, never an unhandled rejection.
    animation.finished.then(cleanup, cleanup);
  }
  function clear(root) {
    for (const [node, entry] of running) {
      if (node === root || root.contains(node)) entry.cleanup();
    }
  }
  function reveal(node, duration = 160, delay = 0) {
    play(node, [{ opacity: 0, transform: "translateY(8px)" }, { opacity: 1, transform: "translateY(0)" }], duration, { delay });
  }
  preference.addEventListener("change", () => {
    if (preference.matches) for (const entry of running.values()) entry.cleanup();
  });
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) for (const entry of running.values()) entry.cleanup();
  });
  return { play, reveal, clear, reduced: () => preference.matches };
})();
