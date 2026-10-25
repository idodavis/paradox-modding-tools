<!--
  A file or folder picker: the chosen path in a field, with a button that opens the system dialog. A caller can
  replace both with a button of its own.
-->
<script setup lang="ts">
import { Dialogs } from "@wailsio/runtime";

const path = defineModel<string>("path", { default: "" });
const { filter, folder = false } = defineProps<{ filter: string[]; folder?: boolean }>();

/** findPath opens the system dialog and keeps the path it returns. A cancelled dialog leaves the path as it was. */
async function findPath(): Promise<void> {
  const selection = await Dialogs.OpenFile({
    CanChooseFiles: !folder,
    CanChooseDirectories: folder,
    Filters: folder ? [] : [{ DisplayName: "Files", Pattern: filter.join(";") }],
  });
  if (selection) path.value = selection;
}
</script>

<template>
  <div class="flex min-w-0 items-center gap-2">
    <slot :browse="findPath">
      <UInput :model-value="path" readonly class="min-w-0 flex-1 font-mono" />
      <UButton label="Browse" @click="findPath" />
    </slot>
  </div>
</template>
