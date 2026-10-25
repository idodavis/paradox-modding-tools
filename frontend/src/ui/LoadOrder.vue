<!-- The load order as a row of mod chips, reordered by dragging where editing is allowed. -->
<script setup lang="ts">
import { computed, useTemplateRef } from "vue";
import { useSortable } from "@vueuse/integrations/useSortable";

import { useWorkspaceStore } from "../stores/workspace";
import type { Mod } from "@services/workspace/models";

/** The origin of the game's own files, which always load first. */
const GAME = "vanilla";
const CHIP =
  "flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-default bg-muted ps-1.5 pe-2.5 text-12 font-medium text-toned whitespace-nowrap";

const order = defineModel<string[]>("order", { required: true });
const { mods, editable } = defineProps<{ mods: Mod[]; editable: boolean }>();

const workspace = useWorkspaceStore();
const titles = computed(() => new Map(mods.map((mod) => [mod.id, mod.descriptor.name])));

useSortable(useTemplateRef<HTMLElement>("chips"), order, {
  animation: 150,
  handle: ".chip-handle",
  watchElement: true,
});
</script>

<template>
  <div class="flex min-w-0 items-center gap-2">
    <span :class="CHIP">
      <span class="w-3 text-center font-mono text-11 text-dimmed">0</span>
      <span class="size-1.5 rounded-full" :style="{ background: workspace.originColor(GAME) }" />
      {{ workspace.originName(GAME) }}
    </span>
    <ul ref="chips" class="flex min-w-0 items-center gap-2 overflow-x-auto">
      <li v-for="(id, i) in order" :key="id" class="flex shrink-0 items-center gap-2">
        <UIcon name="i-lucide-chevron-right" class="size-3.5 text-dimmed" />
        <span :class="[CHIP, { 'chip-handle cursor-grab': editable }]">
          <UIcon v-if="editable" name="i-lucide-grip-vertical" class="size-3.5 text-dimmed" />
          <span class="w-3 text-center font-mono text-11 text-dimmed">{{ i + 1 }}</span>
          <span class="size-1.5 rounded-full" :style="{ background: workspace.originColor(id) }" />
          {{ titles.get(id) }}
        </span>
      </li>
    </ul>
  </div>
</template>
