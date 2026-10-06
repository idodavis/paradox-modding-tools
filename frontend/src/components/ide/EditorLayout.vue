<script setup lang="ts">
/**
 * The editor groups where they sit (stores/ide.ts' Layout): a group, or its parts side by side or stacked, with a
 * grip between each two that moves the room between them.
 */
import { computed, useTemplateRef } from "vue";
import { useElementSize } from "@vueuse/core";
import { useIdeStore, type Layout } from "../../stores/ide";

const { node, workspaceId } = defineProps<{ node: Layout; workspaceId: string }>();

const ide = useIdeStore();
const box = useTemplateRef("box");
const { width, height } = useElementSize(box);
const room = computed(() => ("split" in node && node.split === "column" ? height.value : width.value));
// a part never shrinks under this
const MIN = 160;

/** A part's room in pixels. */
const px = (part: number) => ("split" in node ? (node.sizes[part] ?? 0) * room.value : 0);
/** A part's key: its first group's, so a part keeps its editors when the parts around it change. */
const keyOf = (part: Layout): number => ("group" in part ? part.group : keyOf(part.parts[0] ?? { group: 0 }));
</script>

<template>
  <EditorGroup v-if="'group' in node" :workspace-id="workspaceId" :group-id="node.group" />
  <div v-else ref="box" class="flex min-h-0 min-w-0 flex-1" :class="{ 'flex-col': node.split === 'column' }">
    <template v-for="(part, i) in node.parts" :key="keyOf(part)">
      <div class="flex min-h-0 min-w-0" :style="{ flex: `${node.sizes[i] ?? 1} 1 0` }">
        <EditorLayout :node="part" :workspace-id="workspaceId" />
      </div>
      <ResizeGrip
        v-if="i < node.parts.length - 1"
        :model-value="px(i)"
        :axis="node.split === 'row' ? 'x' : 'y'"
        :min="MIN"
        :max="Math.max(MIN, px(i) + px(i + 1) - MIN)"
        :default-size="(px(i) + px(i + 1)) / 2"
        @update:model-value="(size: number) => room && ide.resize(node, i, size / room)"
      />
    </template>
  </div>
</template>
