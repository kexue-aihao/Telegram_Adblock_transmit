/// <reference types="vite/client" />

// vue-tsc understands .vue files natively; this shim keeps plain tsc and editors
// working when they resolve an SFC import without the Vue language service.
declare module '*.vue' {
  import type { DefineComponent } from 'vue'

  const component: DefineComponent<Record<string, unknown>, Record<string, unknown>, unknown>
  export default component
}
