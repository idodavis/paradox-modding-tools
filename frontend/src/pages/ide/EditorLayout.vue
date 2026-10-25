<!-- The editor groups laid out in rows of columns, with the sizes remembered per workspace. -->
<script setup lang="ts">
import { computed } from "vue";

import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import EditorGroup from "./EditorGroup.vue";

const editor = useEditorStore();
const workspace = useWorkspaceStore();

const rows = computed(() =>
  editor.layout().rows.map((row, order) => ({
    id: row.columns[0]?.id ?? String(order),
    slot: "row",
    order,
    minSize: 10,
    columns: row.columns.map((group, index) => ({ id: group.id, slot: "column", order: index, minSize: 10, group })),
  })),
);
const sizes = computed(() => `pmt.ide.layout.${workspace.activeId()}`);
</script>

<template>
  <USplitter :items="rows" orientation="vertical" :auto-save-id="`${sizes}.rows`" class="min-h-0 min-w-0 flex-1">
    <template #row="{ item: row }">
      <USplitter :key="row.id" :items="row.columns" :auto-save-id="`${sizes}.${row.id}`" class="min-h-0 min-w-0 flex-1">
        <template #column="{ item: column }">
          <EditorGroup :group="column.group" :active="column.group.id === editor.activeGroup()?.id" />
        </template>
      </USplitter>
    </template>
  </USplitter>
</template>
