<!-- The load order as mod chips. When editing is allowed, chips can be dragged and each mod can be left out. -->
<script setup lang="ts">
import { computed, useTemplateRef } from "vue";
import { useSortable } from "@vueuse/integrations/useSortable";

import { useWorkspaceStore, VANILLA } from "../stores/workspace";
import LeftOutMark from "./LeftOutMark.vue";
import ModAvatar from "./ModAvatar.vue";

const CHIP =
  "flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-default bg-muted ps-1.5 pe-2.5 text-12 font-medium text-toned whitespace-nowrap";

const order = defineModel<string[]>("order", { required: true });
const { editable } = defineProps<{ editable: boolean }>();
const emit = defineEmits<{ toggle: [mod: string] }>();

const workspace = useWorkspaceStore();

useSortable(useTemplateRef<HTMLElement>("chips"), order, {
  animation: 150,
  handle: ".chip-handle",
  watchElement: true,
});

/** Each chip's mod, whether it is left out, and what its switch does in words. */
const chips = computed(() =>
  order.value.map((id) => {
    const out = workspace.isLeftOut(id);
    const title = workspace.originName(id);
    return {
      id,
      mod: workspace.listMods().find((mod) => mod.id === id),
      out,
      action: out ? `Check ${title} with the other mods again` : `Leave ${title} out of the compatibility checks`,
    };
  }),
);
</script>

<template>
  <div class="flex min-w-0 items-start gap-2">
    <span :class="CHIP">
      <span class="w-3 text-center font-mono text-11 text-dimmed">0</span>
      <span class="size-1.5 rounded-full" :style="{ background: workspace.originColor(VANILLA) }" />
      {{ workspace.originName(VANILLA) }}
    </span>
    <ul ref="chips" class="flex min-w-0 flex-wrap items-center gap-2">
      <li v-for="(chip, i) in chips" :key="chip.id" class="flex shrink-0 items-center gap-2">
        <UIcon name="i-lucide-chevron-right" class="size-3.5 text-dimmed" />
        <span
          :class="[
            CHIP,
            { 'chip-handle cursor-grab pe-1': editable, 'border-dashed bg-transparent text-dimmed': chip.out },
          ]"
        >
          <UIcon v-if="editable" name="i-lucide-grip-vertical" class="size-3.5 text-dimmed" />
          <span class="w-3 text-center font-mono text-11 text-dimmed">{{ i + 1 }}</span>
          <ModAvatar v-if="chip.mod" :mod="chip.mod" size="3xs" :class="{ 'opacity-45': chip.out }" />
          {{ workspace.originName(chip.id) }}
          <UTooltip v-if="editable" :text="chip.action">
            <UButton
              :icon="chip.out ? 'i-lucide-eye-off' : 'i-lucide-eye'"
              :aria-label="chip.action"
              variant="ghost"
              color="neutral"
              size="xs"
              square
              :class="chip.out && 'bg-accented text-toned'"
              @click="emit('toggle', chip.id)"
            />
          </UTooltip>
          <LeftOutMark v-else-if="chip.out" />
        </span>
      </li>
    </ul>
  </div>
</template>
