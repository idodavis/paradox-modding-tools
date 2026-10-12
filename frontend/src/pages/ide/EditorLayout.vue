<script setup lang="ts">
/**
 * The editor groups where they sit (the editor store's Layout): a group, or its parts side by side or stacked, split by a
 * handle between each two that moves the room between them; the parts' shares are the layout's own sizes.
 */
import { computed } from "vue";
import { useEditorStore, type Layout } from "../../stores/editor";
import EditorGroup from "./EditorGroup.vue";

const { node, workspaceId } = defineProps<{ node: Layout; workspaceId: string }>();
const editor = useEditorStore();

/** A part's key: its first group's, so a part keeps its editors when the parts around it change. */
const keyOf = (part: Layout): number => ("group" in part ? part.group : keyOf(part.parts[0] ?? { group: 0 }));
const panes = computed(() =>
  "split" in node
    ? node.parts.map((part, i) => ({
        id: String(keyOf(part)),
        slot: "part",
        order: i,
        part,
        defaultSize: (node.sizes[i] ?? 0) * 100,
        minSize: 10,
      }))
    : [],
);
</script>

<template>
  <EditorGroup v-if="'group' in node" :workspace-id="workspaceId" :group-id="node.group" />
  <USplitter
    v-else
    :key="panes.map((pane) => pane.id).join()"
    :items="panes"
    :orientation="node.split === 'column' ? 'vertical' : 'horizontal'"
    class="min-h-0 min-w-0 flex-1"
    @layout="
      (shares) =>
        editor.resizeSplit(
          node,
          shares.map((share) => share / 100),
        )
    "
  >
    <template #part="{ item }">
      <EditorLayout :node="item.part" :workspace-id="workspaceId" />
    </template>
  </USplitter>
</template>
