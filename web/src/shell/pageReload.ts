import { ref } from 'vue'

import { rotatePageRequests } from '../core/api'

// Bumping the token remounts the routed component, which is what the legacy
// renderers mean by "reload this page" (their refresh buttons call reloadPage).
export const pageReloadToken = ref(0)

export function requestPageReload(): void {
  rotatePageRequests()
  pageReloadToken.value++
}
