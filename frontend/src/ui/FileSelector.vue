<!-- A file or folder picker: the chosen path in a field, with a button that opens the system dialog. -->
<script setup lang="ts">
defineModel<string>("path", { default: "" });
defineProps<{ filter: string[]; folder?: boolean }>();
</script>

<template>
  <div />
</template>
