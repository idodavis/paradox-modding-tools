<!-- A page's facts, shown in the status bar while the page is open. -->
<script setup lang="ts">
import { onActivated, onDeactivated, shallowRef } from "vue";

defineProps<{ facts: { label: string; value: string }[] }>();

// a kept-alive page stays mounted behind the others, so its facts leave with it
const visible = shallowRef(true);
onActivated(() => (visible.value = true));
onDeactivated(() => (visible.value = false));
</script>

<template>
  <Teleport v-if="visible" defer to="#status-start">
    <span v-for="fact in facts" :key="fact.label">
      {{ fact.label }} <span class="text-toned">{{ fact.value }}</span>
    </span>
  </Teleport>
</template>
