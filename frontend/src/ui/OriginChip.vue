<!-- An origin's colored dot and name. It shows the dot alone when asked, with the name as its tooltip. -->
<script setup lang="ts">
import { useWorkspaceStore } from "../stores/workspace";

const { origin, dot = false } = defineProps<{ origin: string; dot?: boolean }>();

const workspace = useWorkspaceStore();
</script>

<template>
  <span
    :title="workspace.originName(origin)"
    class="inline-flex shrink-0 items-center"
    :class="
      !dot &&
      'h-5 max-w-full gap-1.5 rounded-chip border border-default bg-elevated ps-1.5 pe-2 text-11 font-medium text-toned'
    "
  >
    <span class="size-1.5 shrink-0 rounded-full" :style="{ background: workspace.originColor(origin) }" />
    <span v-if="!dot" class="truncate">{{ workspace.originName(origin) }}</span>
  </span>
</template>
