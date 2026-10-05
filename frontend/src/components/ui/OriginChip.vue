<script setup lang="ts">
/**
 * An origin by its color and name (pmt.css .chip).
 */
defineProps<{ label: string; color: string }>();
</script>

<template>
  <span
    class="inline-flex h-5 max-w-full shrink-0 items-center gap-1.5 rounded-chip border border-default bg-elevated
      ps-1.5 pe-2 text-11 font-medium text-toned"
  >
    <OriginDot :color="color" />
    <span class="truncate">{{ label }}</span>
  </span>
</template>
