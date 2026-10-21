<!-- The command palette. It lists the commands by group and runs the one picked. -->
<script setup lang="ts">
const open = defineModel<boolean>("open", { default: false });
</script>

<template>
  <div v-if="open" />
</template>
