<!-- A band with a game's painting behind what it holds, dimmed toward the start so the words stay readable. -->
<script setup lang="ts">
import { computed } from "vue";

import { fileUrl } from "../../queries";

const { painting = "" } = defineProps<{
  /** The painting's file, "" for a band with none. */
  painting?: string;
}>();

const image = computed(() => (painting ? { backgroundImage: `url('${fileUrl(painting)}')` } : undefined));
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
