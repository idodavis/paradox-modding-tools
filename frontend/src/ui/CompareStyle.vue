<script setup lang="ts">
/**
 * The choice between a compare's split and unified views, which every compare shares and this computer keeps.
 */
import { COMPARE_STYLES, compareStyle } from "../lib/monaco";

const { size = "md" } = defineProps<{ size?: "xs" | "md" }>();
</script>

<template>
  <UTabs
    :model-value="compareStyle"
    :items="COMPARE_STYLES"
    :content="false"
    :size="size"
    @update:model-value="compareStyle = $event === 'unified' ? 'unified' : 'split'"
  />
</template>
