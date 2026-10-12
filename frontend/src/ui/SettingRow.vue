<script setup lang="ts">
/**
 * One setting (pmt.css .srow): its name and a hint, the control at the end. Rows in a list are split
 * by the list's `divide-y divide-default`.
 */
const { label, hint = "", mono = false } = defineProps<{ label: string; hint?: string; mono?: boolean }>();
</script>

<template>
  <div class="flex min-h-11 items-center gap-4 px-3.5 py-1.75">
    <div class="flex min-w-0 flex-1 flex-col gap-px">
      <span class="text-13 leading-4.5 font-medium text-highlighted">{{ label }}</span>
      <span v-if="hint" class="text-11 leading-3.75 text-dimmed" :class="{ 'font-mono': mono }">{{ hint }}</span>
    </div>
    <slot />
  </div>
</template>
