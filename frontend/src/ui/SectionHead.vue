<!-- A section head: its title, a subtitle if it has one, and a count if it has one. Its slot ends the row. -->
<script setup lang="ts">
defineProps<{ title: string; subtitle?: string; count?: number }>();
</script>

<template>
  <div class="flex h-11 shrink-0 items-center gap-2.5 border-b border-default px-3">
    <span class="shrink-0 text-13 font-semibold">{{ title }}</span>
    <span v-if="subtitle" class="truncate text-12 text-dimmed">{{ subtitle }}</span>
    <span class="grow" />
    <span v-if="count !== undefined" class="shrink-0 text-11 text-dimmed tabular-nums">{{ count }}</span>
    <slot />
  </div>
</template>
