"use strict";

// Runs before the stylesheet and the module bundle so the stored theme is
// applied before the first paint (no flash of the wrong theme).
(() => {
  let theme;
  try { theme = localStorage.getItem("panel-theme"); } catch { /* Storage can be unavailable. */ }
  if (theme !== "dark" && theme !== "light") {
    theme = matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  document.documentElement.dataset.theme = theme;
  // The colour of the browser chrome. The two theme-color tags in index.html are
  // media-scoped, which covers the system preference before any script runs; a
  // stored preference overrides the system one, so both tags then carry it.
  // This script is injected at the very start of <head>, ahead of those tags, so
  // the first call is a no-op and the second one lands as parsing finishes.
  const colors = { dark: "#07080b", light: "#eceff5" };
  const applyColor = () => {
    document.querySelectorAll('meta[name="theme-color"]').forEach((meta) => { meta.content = colors[theme]; });
  };
  applyColor();
  document.addEventListener("DOMContentLoaded", applyColor, { once: true });
})();
