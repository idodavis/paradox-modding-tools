<script setup lang="ts">
/**
 * Release load-order rail: index, workspace thumb, origin pill.
 */
import { computed } from "vue";
import type { WorkspaceMod } from "@services/models";
import { modColor } from "../../stores/workspace";

const props = defineProps<{
  mods: WorkspaceMod[];
  selectedId: string;
}>();

const emit = defineEmits<{
  select: [id: string];
}>();

/**
 * Already in load order: Go sorts the mods before ListWorkspaces returns them. Sorting again here
 * by another rule would disagree with Go's byte order on case.
 */
const ordered = computed(() => props.mods);
</script>

<template>
  <aside
    class="flex w-48 shrink-0 flex-col gap-1 overflow-auto
      border-r border-default p-2"
  >
    <p class="px-1 text-xs font-semibold tracking-wide text-muted uppercase">
      Mods
    </p>
    <ul class="space-y-0.5">
      <li v-for="(mod, i) in ordered" :key="mod.id">
        <button
          type="button"
          class="flex w-full items-center gap-1.5 rounded-sm px-1 py-0.5
            text-left"
          :class="mod.id === selectedId ? 'bg-elevated' : ''"
          :aria-pressed="mod.id === selectedId"
          @click="emit('select', mod.id)"
        >
          <span class="w-4 shrink-0 tabular-nums text-xs text-muted">
            {{ (mod.sortOrder ?? i) + 1 }}
          </span>
          <Thumb v-if="mod.thumbnail" :path="mod.thumbnail" size="xs" />
          <OriginChip :label="mod.name" :color="modColor(mod)" class="min-w-0" />
        </button>
      </li>
    </ul>
    <p v-if="!ordered.length" class="px-1 text-xs text-muted">
      No mods attached.
    </p>
  </aside>
</template>
