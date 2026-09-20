// Theme handling. The pre-paint decision lives in static/theme.js, which runs
// before the first paint; this module only reads and changes what it set.
export type Theme = 'dark' | 'light'

// A palette moves the brand hue and nothing else: surfaces, ink and the
// semantic colours are identical in all of them, so the choice never changes
// what a page means. The ids are the values of `data-accent` in tokens.css and
// the keys of the list in static/theme.js — a palette has to be added to all
// three, and the label is the only part that lives here alone.
export type Accent = 'azure' | 'teal' | 'violet' | 'magenta' | 'amber' | 'graphite'

export const ACCENTS: { id: Accent; label: string }[] = [
  { id: 'azure', label: '湛蓝' },
  { id: 'teal', label: '青碧' },
  { id: 'violet', label: '紫罗兰' },
  { id: 'magenta', label: '品红' },
  { id: 'amber', label: '琥珀' },
  { id: 'graphite', label: '石墨' },
]

// The palette the panel falls back to when nothing is stored. It matches the
// values :root carries in tokens.css.
const DEFAULT_ACCENT: Accent = 'azure'

const THEME_STORAGE_KEY = 'panel-theme'
const ACCENT_STORAGE_KEY = 'panel-accent'
// The browser chrome colour, matching --color-bg-0 in each theme. index.html
// ships them media-scoped; static/theme.js owns the pre-paint pass, so this is
// only for the explicit toggle. No palette changes it: the canvas is graphite
// in all of them.
const META_COLORS: Record<Theme, string> = { dark: '#07080b', light: '#eceff5' }

function isAccent(value: string | null | undefined): value is Accent {
  return ACCENTS.some((palette) => palette.id === value)
}

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

// The attribute is the single source of truth: static/theme.js always writes
// one before the first paint, so reading it back here cannot disagree with what
// is on screen.
export function currentAccent(): Accent {
  const active = document.documentElement.dataset.accent
  return isAccent(active) ? active : DEFAULT_ACCENT
}

export function setAccent(accent: Accent): Accent {
  document.documentElement.dataset.accent = accent
  try {
    localStorage.setItem(ACCENT_STORAGE_KEY, accent)
  } catch {
    // Storage can be unavailable; keep the current session palette.
  }
  return accent
}
