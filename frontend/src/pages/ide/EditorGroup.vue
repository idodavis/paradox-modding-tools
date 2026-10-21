<!--
  One editor group: its tab strip, with tabs dragged between groups and windows, and the active tab's view: code,
  image, visual, form, split or a diff.
-->
<script setup lang="ts">
import type { Group } from "../../stores/editor";

defineProps<{ group: Group; active: boolean }>();
</script>

<template>
  <div />
</template>
