<script setup lang="ts">
/**
 * A path chosen with the system's picker: the path, read only, and Browse.
 */
import { pickDirectory, pickFile } from "../lib/nativeDialog";

const path = defineModel<string>({ required: true });
const { dialogTitle, mode, placeholder = "", fileFilter = "" } = defineProps<{
  dialogTitle: string;
  mode: "file" | "folder";
  placeholder?: string;
  fileFilter?: string;
}>();

async function browse(): Promise<void> {
  const picked = mode === "folder" ? await pickDirectory(dialogTitle) : await pickFile(dialogTitle, fileFilter);
  if (picked) path.value = picked;
}
</script>

<template>
  <div class="flex min-w-0 items-center gap-2">
    <UInput v-model="path" readonly :placeholder="placeholder" class="min-w-0 flex-1" :ui="{ base: 'font-mono placeholder:font-sans' }" />
    <UButton label="Browse" @click="browse" />
  </div>
</template>
