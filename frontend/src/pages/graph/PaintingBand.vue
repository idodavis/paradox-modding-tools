<!--
  A band with a painting of the game behind its head, dimmed so that the words
  on it read. Without a painting it is a plain band.
-->
<script setup lang="ts">
import { computed } from "vue";
import { fileUrl } from "../../queries";

const { painting = "" } = defineProps<{ painting?: string }>();

const image = computed(() => (painting ? { backgroundImage: `url("${fileUrl(painting)}")` } : undefined));
</script>

<template>
  <div class="relative isolate overflow-hidden bg-cover bg-[center_45%]" :style="image">
    <div v-if="painting" class="scrim absolute inset-0 -z-10" />
    <slot />
  </div>
</template>

<style scoped>
.scrim {
  background: linear-gradient(
    90deg,
    color-mix(in srgb, var(--bg-0) 92%, transparent),
    color-mix(in srgb, var(--bg-0) 75%, transparent) 45%,
    color-mix(in srgb, var(--bg-0) 35%, transparent)
  );
}
</style>
