<script setup lang="ts">
// Icon renders a symbol from the local Lucide sprite. The sprite stays a plain
// /assets/ URL so the bundler never inlines it (img-src allows data: URLs, but
// an inlined sprite would lose the shared cache and the licence file next to it).
withDefaults(defineProps<{ name: string; cls?: string }>(), { cls: '' })
</script>

<template>
  <svg :class="['icon', cls]" aria-hidden="true"><use :href="'/assets/icons.svg#' + name" /></svg>
</template>
