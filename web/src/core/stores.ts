import { markRaw, reactive } from 'vue'

import type { BuiltinRuleInfo, ChatSummary } from './types/api'

// The panel state is shared by the Vue shell and the not-yet-migrated page
// renderers, so it stays a single reactive object instead of a store library.
export interface PanelState {
  authenticated: boolean
  username: string
  route: string
  hash: string
  chats: ChatSummary[]
  builtinCatalog: Map<string, BuiltinRuleInfo>
  builtinCatalogLoaded: boolean
}

export const state = reactive<PanelState>({
  authenticated: false,
  username: '',
  route: 'dashboard',
  hash: '',
  chats: [],
  // Kept out of deep reactivity: it is read while rendering, never mutated
  // incrementally.
  builtinCatalog: markRaw(new Map<string, BuiltinRuleInfo>()),
  builtinCatalogLoaded: false,
})

export function resetSessionState(): void {
  state.chats = []
  state.builtinCatalog.clear()
  state.builtinCatalogLoaded = false
}
