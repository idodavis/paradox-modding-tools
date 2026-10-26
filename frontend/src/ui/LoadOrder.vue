<!-- Shows the load order as mod chips, which can be dragged into another order when editing is allowed. -->
<script setup lang="ts">
import { useTemplateRef } from "vue";
import { useSortable } from "@vueuse/integrations/useSortable";

import { useWorkspaceStore, VANILLA } from "../stores/workspace";

const CHIP =
  "flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-default bg-muted ps-1.5 pe-2.5 text-12 font-medium text-toned whitespace-nowrap";

const order = defineModel<string[]>("order", { required: true });
const { editable } = defineProps<{ editable: boolean }>();

const workspace = useWorkspaceStore();

useSortable(useTemplateRef<HTMLElement>("chips"), order, {
  animation: 150,
  handle: ".chip-handle",
  watchElement: true,
});
</script>

<template>
  <!-- the chips wrap, so every mod shows at any width -->
  <div class="flex min-w-0 items-start gap-2">
    <span :class="CHIP">
      <span class="w-3 text-center font-mono text-11 text-dimmed">0</span>
      <span class="size-1.5 rounded-full" :style="{ background: workspace.originColor(VANILLA) }" />
      {{ workspace.originName(VANILLA) }}
    </span>
    <ul ref="chips" class="flex min-w-0 flex-wrap items-center gap-2">
      <li v-for="(id, i) in order" :key="id" class="flex shrink-0 items-center gap-2">
        <UIcon name="i-lucide-chevron-right" class="size-3.5 text-dimmed" />
        <span :class="[CHIP, { 'chip-handle cursor-grab': editable }]">
          <UIcon v-if="editable" name="i-lucide-grip-vertical" class="size-3.5 text-dimmed" />
          <span class="w-3 text-center font-mono text-11 text-dimmed">{{ i + 1 }}</span>
          <span class="size-1.5 rounded-full" :style="{ background: workspace.originColor(id) }" />
          {{ workspace.originName(id) }}
        </span>
      </li>
    </ul>
  </div>
</template>
