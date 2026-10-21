<!--
  A find bar over a list of texts: the term, the count, the current hit, next, previous and close. It tells its holder
  which text is current.
-->
<script setup lang="ts">
defineModel<string>("term", { default: "" });
defineProps<{ texts: readonly string[] }>();
defineEmits<{ hit: [index: number]; close: [] }>();
</script>

<template>
  <div />
</template>
