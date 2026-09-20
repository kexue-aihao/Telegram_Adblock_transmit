"use strict";

// Runs before the stylesheet and the module bundle so the stored theme and
// palette are applied before the first paint (no flash of the wrong one).
(() => {
  // Repeated from core/theme.ts: this file has to stay a classic script ahead
  // of the bundle, so it cannot import the list.
  const ACCENTS = ["azure", "teal", "violet", "magenta", "amber", "graphite"];
  let theme, accent;
  try {
    theme = localStorage.getItem("panel-theme");
    accent = localStorage.getItem("panel-accent");
  } catch { /* Storage can be unavailable. */ }
  if (theme !== "dark" && theme !== "light") {
    theme = matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  document.documentElement.dataset.theme = theme;
  // There is no system preference for the palette, so an unknown or missing
  // value falls back to the one :root carries in tokens.css. The attribute is
  // written either way, which keeps the active choice readable from the DOM.
  document.documentElement.dataset.accent = ACCENTS.includes(accent) ? accent : "azure";
  // The colour of the browser chrome. The two theme-color tags in index.html are
  // media-scoped, which covers the system preference before any script runs; a
  // stored preference overrides the system one, so both tags then carry it.
  // This script is injected at the very start of <head>, ahead of those tags, so
  // the first call is a no-op and the second one lands as parsing finishes.
  // No palette changes it: the canvas stays graphite in all of them.
  const colors = { dark: "#07080b", light: "#eceff5" };
  const applyColor = () => {
    document.querySelectorAll('meta[name="theme-color"]').forEach((meta) => { meta.content = colors[theme]; });
  };
  applyColor();
  document.addEventListener("DOMContentLoaded", applyColor, { once: true });
})();
