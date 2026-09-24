<script lang="ts">
/**
 * Origin identity chip painted with the workspace origin hex. PILL_UI gives a UBadge its density.
 */
export const PILL_DENSITY =
  "rounded-sm px-1.5 py-0.5 text-xs leading-none";
export const PILL_UI = { base: `${PILL_DENSITY} font-normal` } as const;
</script>

<script setup lang="ts">
defineProps<{
  label: string;
  hex: string;
  thumb?: string;
}>();
</script>

<template>
  <span
    :class="['inline-flex max-w-full items-center gap-1 truncate border', PILL_DENSITY]"
    :style="{
      backgroundColor: `${hex}33`,
      borderColor: hex,
    }"
  >
    <img
      v-if="thumb"
      :src="thumb"
      alt=""
      class="size-3.5 shrink-0 rounded-sm object-cover"
    />
    {{ label }}
  </span>
</template>
