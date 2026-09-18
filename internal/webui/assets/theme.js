"use strict";

(() => {
  let theme;
  try { theme = localStorage.getItem("panel-theme"); } catch { /* Storage can be unavailable. */ }
  if (theme !== "dark" && theme !== "light") {
    theme = matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  document.documentElement.dataset.theme = theme;
  document.querySelector('meta[name="theme-color"]').content = theme === "dark" ? "#191d20" : "#f5f7f7";
})();
