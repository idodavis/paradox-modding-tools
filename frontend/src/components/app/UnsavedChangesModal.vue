<script setup lang="ts">
/**
 * Asked before unsaved files would go (a tab closes, another workspace's take their place, the window reloads): save
 * them, drop them, or stay. Opened through useOverlay, it closes with true to go on; `files` are the ones asked about
 * (every unsaved file when none are named), `going` says what drops them.
 */
import { computed } from "vue";
import { useMutation } from "@pinia/colada";
import { fileName, plural } from "../../lib/format";
import { useIdeStore } from "../../stores/ide";

const { going = "Switching workspace", files } = defineProps<{ going?: string; files?: string[] }>();
const emit = defineEmits<{ close: [go: boolean] }>();

const ide = useIdeStore();
const paths = computed(() => files ?? [...ide.unsaved]);
const names = computed(() => paths.value.map(fileName));
const listed = computed(() => {
  const shown = names.value.slice(0, 4).join(", ");
  return names.value.length > 4 ? `${shown} and ${names.value.length - 4} more` : shown;
});
// the editor's code is loaded already wherever a file is unsaved
const { mutate: save, isLoading: saving } = useMutation({
  mutation: async () => {
    const editor = await import("../../ide/monacoEditor");
    await (files ? Promise.all(files.map(editor.saveFile)) : editor.saveAll());
  },
  onSuccess: () => emit("close", true),
});
</script>

<template>
  <UModal
    :title="`Save ${plural(names.length, 'file')}?`"
    :description="`${listed} ${names.length === 1 ? 'has' : 'have'} changes not saved. ${going} without saving drops them.`"
  >
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="emit('close', false)" />
      <UButton label="Don't save" color="error" variant="soft" @click="emit('close', true)" />
      <UButton :label="names.length === 1 ? 'Save' : 'Save all'" color="primary" variant="solid" :loading="saving" @click="save()" />
    </template>
  </UModal>
</template>
