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
  // The build injects this script at the very start of <head>, so the meta tag
  // may not be parsed yet.
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.content = theme === "dark" ? "#111619" : "#f3f6f5";
})();
