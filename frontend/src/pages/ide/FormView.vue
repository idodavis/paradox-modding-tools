<!--
  The Form view of a definition: one field per input the database offers. A change is written to the file as one edit.
-->
<script setup lang="ts">
import type { Model } from "./editor/models";

defineProps<{ model: Model; database: string }>();
</script>

<template>
  <div />
</template>
