<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

import { loading } from '../core/dom'
import { panelMotion } from '../core/motion'
import { legacyPages, type LegacyRoute } from '../legacy/legacy'
import { markRouteRendered } from '../router'

// The adapter keeps the not-yet-migrated renderers working unchanged: it owns
// the <section class="page"> wrapper, paints the loading skeleton (some
// renderers stay silent until their first request resolves) and hands the
// section to the imperative renderer, which fills it exactly as before.
const props = defineProps<{ route: LegacyRoute }>()
const host = ref<HTMLElement | null>(null)

onMounted(async () => {
  const view = host.value
  if (!view) return
  view.append(loading())
  markRouteRendered(props.route)
  panelMotion.reveal(view, 240)
  await legacyPages[props.route](view)
  if (view.isConnected && view.contains(document.activeElement)) {
    (view.querySelector('h1') as HTMLElement | null)?.focus({ preventScroll: true })
  }
})

onBeforeUnmount(() => {
  if (host.value) panelMotion.clear(host.value)
})
</script>

<template>
  <section ref="host" class="page" tabindex="-1"></section>
</template>
