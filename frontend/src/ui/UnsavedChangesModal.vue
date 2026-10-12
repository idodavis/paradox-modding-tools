<script setup lang="ts">
/**
 * Asked before unsaved files would go (a tab closes, another workspace's take their place, the window reloads): save
 * them, drop them, or stay. Opened through useOverlay, it closes with true to go on; `files` are the ones asked about
 * (every unsaved file when none are named), `going` says what drops them.
 */
import { computed } from "vue";
import { useMutation } from "@pinia/colada";
import { fileName, plural } from "../lib/format";
import { useEditorStore } from "../stores/editor";

const { going = "Switching workspace", files } = defineProps<{ going?: string; files?: string[] }>();
const emit = defineEmits<{ close: [go: boolean] }>();

const editor = useEditorStore();
const paths = computed(() => files ?? [...editor.unsaved]);
const names = computed(() => paths.value.map(fileName));
const listed = computed(() => {
  const shown = names.value.slice(0, 4).join(", ");
  return names.value.length > 4 ? `${shown} and ${names.value.length - 4} more` : shown;
});
const { mutate: save, isLoading: saving } = useMutation({
  mutation: () => editor.saveFiles(paths.value),
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
      <UButton
        :label="names.length === 1 ? 'Save' : 'Save all'"
        color="primary"
        variant="solid"
        :loading="saving"
        @click="save()"
      />
    </template>
  </UModal>
</template>
