// Theme handling. The pre-paint decision lives in static/theme.js, which runs
// before the first paint; this module only reads and toggles what it set.
export type Theme = 'dark' | 'light'

const THEME_STORAGE_KEY = 'panel-theme'
// The browser chrome colour, matching --color-bg-0 in each theme. index.html
// ships them media-scoped; static/theme.js owns the pre-paint pass, so this is
// only for the explicit toggle.
const META_COLORS: Record<Theme, string> = { dark: '#07080b', light: '#eceff5' }

export function currentTheme(): Theme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
}

export function toggleTheme(): Theme {
  const theme: Theme = currentTheme() === 'dark' ? 'light' : 'dark'
  document.documentElement.dataset.theme = theme
  // Both tags, so the media-scoped pair agrees once a choice is stored.
  document.querySelectorAll('meta[name="theme-color"]').forEach((meta) => {
    meta.setAttribute('content', META_COLORS[theme])
  })
  try {
    localStorage.setItem(THEME_STORAGE_KEY, theme)
  } catch {
    // Storage can be unavailable; keep the current session theme.
  }
  return theme
}

// The accessible name states the theme the control switches to.
export function themeToggleLabel(theme: Theme): string {
  return theme === 'dark' ? '切换浅色主题' : '切换深色主题'
}
