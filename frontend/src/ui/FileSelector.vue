<script setup lang="ts">
/**
 * A path chosen with the system's picker: the path, read only, and Browse.
 */
import { pickDirectory, pickFile } from "../lib/nativeDialog";

const path = defineModel<string>({ required: true });
const {
  dialogTitle,
  mode,
  placeholder = "",
  fileFilter = "",
} = defineProps<{
  dialogTitle: string;
  mode: "file" | "folder";
  placeholder?: string;
  fileFilter?: string;
}>();

async function browse(): Promise<void> {
  const selection = mode === "folder" ? await pickDirectory(dialogTitle) : await pickFile(dialogTitle, fileFilter);
  if (selection) path.value = selection;
}
</script>

<template>
  <div class="flex min-w-0 items-center gap-2">
    <UInput
      v-model="path"
      readonly
      :placeholder="placeholder"
      class="min-w-0 flex-1 font-mono [&_input]:placeholder:font-sans"
    />
    <UButton label="Browse" @click="browse" />
  </div>
</template>
