<!--
  One value edited by its kind: a switch, a choice, a number, a text, a color or a texture, with its label and help.
  The caller writes the value where it belongs.
-->
<script setup lang="ts">
defineProps<{
  kind: "switch" | "choice" | "number" | "text" | "color" | "texture";
  value: string;
  label: string;
  help?: string;
  choices?: readonly string[];
  min?: number;
  max?: number;
  role?: string;
}>();
defineEmits<{ "update:value": [value: string] }>();
</script>

<template>
  <div />
</template>
