<script setup lang="ts">
/**
 * One setting on a Settings page: its name and what it does on the left, its
 * controls on the right, a rule below.
 */
const { title, hint = "" } = defineProps<{ title: string; hint?: string }>();
</script>

<template>
  <section class="grid grid-cols-[260px_minmax(0,1fr)] gap-8 border-b border-default py-5 last:border-b-0">
    <div class="min-w-0">
      <h2 class="text-13 font-semibold">{{ title }}</h2>
      <p v-if="hint || $slots.hint" class="mt-0.75 text-12 leading-4.5 text-dimmed">
        <slot name="hint">{{ hint }}</slot>
      </p>
    </div>
    <div class="flex min-w-0 flex-col gap-3">
      <slot />
    </div>
  </section>
</template>
