<!-- The new mod form: a name, and the mod is created in the workspace. -->
<script setup lang="ts">
import type { Mod } from "@services/workspace/models";

defineProps<{ workspaceId: string }>();
defineEmits<{ created: [mod: Mod] }>();
</script>

<template>
  <div />
</template>
