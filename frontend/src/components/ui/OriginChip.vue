<script setup lang="ts">
/**
 * An origin by its color and name (pmt.css .chip); small, its dot is 6px.
 */
const { label, color, small = false } = defineProps<{ label: string; color: string; small?: boolean }>();
</script>

<template>
  <span
    class="inline-flex h-5 max-w-full shrink-0 items-center gap-1.5 rounded-chip border border-default bg-elevated
      ps-1.5 pe-2 text-11 font-medium text-toned"
  >
    <OriginDot :color="color" :small="small" />
    <span class="truncate">{{ label }}</span>
  </span>
</template>
