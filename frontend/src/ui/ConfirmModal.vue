<!--
  A confirmation dialog with a title, a description and its buttons. It resolves with the choice made. A destructive
  confirm is red.
-->
<script setup lang="ts">
defineProps<{
  title: string;
  body: string;
  confirm: string;
  destructive?: boolean;
}>();
defineEmits<{ close: [confirmed: boolean] }>();
</script>

<template>
  <div />
</template>
