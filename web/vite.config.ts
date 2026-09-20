import { copyFileSync, mkdirSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig, type Plugin } from 'vite'

const webRoot = fileURLToPath(new URL('.', import.meta.url))
const staticDir = join(webRoot, 'static')

// publishStatic copies the hand-written files that must stay outside the bundle:
// the anti-FOUC theme script, the Lucide sprite, the favicon and its licence.
// They live in web/static because the build empties its output directory, which
// is the embedded panel assets directory.
function publishStatic(): Plugin {
  let outDir = ''
  return {
    name: 'panel-static',
    configResolved(config) {
      outDir = config.build.outDir
    },
    closeBundle() {
      mkdirSync(outDir, { recursive: true })
      for (const name of readdirSync(staticDir)) {
        copyFileSync(join(staticDir, name), join(outDir, name))
      }
    },
  }
}

// themeScript keeps web/static/theme.js a classic script in <head>: it has to
// run before the first paint and before the module bundle, and Vite must not
// treat the hand-written /assets/ URL as an asset import.
function themeScript(): Plugin {
  return {
    name: 'panel-theme-script',
    transformIndexHtml: {
      order: 'post',
      handler: () => [{ tag: 'script', attrs: { src: '/assets/theme.js' }, injectTo: 'head-prepend' as const }],
    },
  }
}

export default defineConfig({
  root: webRoot,
  // The panel always loads its assets from /assets/, which is the route the Go
  // server registers for the embedded file system.
  base: '/assets/',
  // Static files are published by publishStatic instead, so their URLs stay
  // root-absolute and are never rewritten as bundler imports.
  publicDir: false,
  plugins: [vue(), publishStatic(), themeScript()],
  define: {
    __VUE_OPTIONS_API__: false,
    __VUE_PROD_DEVTOOLS__: false,
    __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: false,
  },
  build: {
    outDir: '../internal/webui/assets',
    // The output directory lives outside the project root, so Vite would skip
    // emptying it by default and stale bundles would accumulate.
    emptyOutDir: true,
    // One stylesheet keeps the fixed app.css name the Go tests pin.
    cssCodeSplit: false,
    // Never inline assets as data: URLs. font-src falls back to 'self', so an
    // inlined font would be blocked by the production CSP.
    assetsInlineLimit: 0,
    sourcemap: false,
    // The modulepreload polyfill is the only inline <script> Vite emits by
    // default, and the production CSP forbids inline scripts.
    modulePreload: { polyfill: false },
    rollupOptions: {
      input: { app: join(webRoot, 'index.html') },
      output: {
        // Fixed names: internal/webui/handlers_test.go pins /assets/app.js and
        // /assets/app.css, and Cache-Control: no-store makes hashing useless.
        entryFileNames: 'app.js',
        chunkFileNames: 'chunks/[name]-[hash].js',
        assetFileNames: (info) => {
          const names = (info as { names?: string[] }).names
          const name = names?.[0] ?? info.name ?? ''
          if (name.endsWith('.css')) {
            return 'app.css'
          }
          if (/\.(woff2?|ttf|otf|eot)$/i.test(name)) {
            return 'fonts/[name][extname]'
          }
          return '[name][extname]'
        },
      },
    },
  },
})
