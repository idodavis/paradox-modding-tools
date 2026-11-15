<!--
  A bar of colored segments, each as wide as its count is of the whole bar.
-->
<script setup lang="ts">
const { segments, scale } = defineProps<{
  /**
   * The segments in order, each a Nuxt UI color and its count. A count of 0
   * draws nothing.
   */
  segments: { color: string; count: number }[];
  /** The count a whole bar stands for. */
  scale: number;
}>();

/**
 * describeWidth returns a segment's width. Any count keeps a sliver, so it
 * never reads as none.
 */
function describeWidth(count: number): string {
  return `${count ? Math.max(3, (count / scale) * 100) : 0}%`;
}
</script>

<template>
  <span class="flex h-1.5 overflow-hidden rounded-full bg-accented">
    <span
      v-for="(segment, position) in segments"
      :key="position"
      class="h-full"
      :style="{ width: describeWidth(segment.count), background: `var(--ui-${segment.color})` }"
    />
  </span>
</template>
