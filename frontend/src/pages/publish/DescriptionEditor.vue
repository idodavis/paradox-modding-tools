<!-- The description editor: its source format, the text, and the titles and descriptions per language. -->
<script setup lang="ts">
import type { Listing } from "@services/steam/models";

defineModel<Listing>("listing", { required: true });
</script>

<template>
  <div />
</template>
