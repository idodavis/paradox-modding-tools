<script setup lang="ts">
/**
 * A titled group of fields in a detail card: its name, a hint and an action
 * at the end, then its fields; a rule below.
 */
const { title, hint = "" } = defineProps<{ title: string; hint?: string }>();
</script>

<template>
  <div class="flex flex-col gap-3 border-b border-default px-5 py-4 last:border-b-0">
    <div class="flex min-h-5 flex-wrap items-baseline gap-x-2.5 gap-y-1">
      <span class="shrink-0 text-13 font-semibold whitespace-nowrap">{{ title }}</span>
      <span v-if="hint" class="min-w-0 text-12 text-dimmed">{{ hint }}</span>
      <span class="ms-auto flex shrink-0 items-center gap-2"><slot name="end" /></span>
    </div>
    <slot />
  </div>
</template>
