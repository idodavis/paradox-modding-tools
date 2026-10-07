<script setup lang="ts">
/**
 * Asked before another workspace's files take the editors' place: save the unsaved ones, drop them, or stay.
 * Opened through useOverlay by the router's guard, it closes with true to go on.
 */
import { computed } from "vue";
import { useMutation } from "@pinia/colada";
import { plural } from "../lib/format";
import { useIdeStore } from "../stores/ide";

const emit = defineEmits<{ close: [go: boolean] }>();

const ide = useIdeStore();
const names = computed(() => [...ide.unsaved].map((path) => path.slice(path.replace(/\\/g, "/").lastIndexOf("/") + 1)));
const listed = computed(() => {
  const shown = names.value.slice(0, 4).join(", ");
  return names.value.length > 4 ? `${shown} and ${names.value.length - 4} more` : shown;
});
// the editor's code is loaded already wherever a file is unsaved
const { mutate: save, isLoading: saving } = useMutation({
  mutation: async () => (await import("../ide/monacoEditor")).saveAll(),
  onSuccess: () => emit("close", true),
});
</script>

<template>
  <UModal
    :title="`Save ${plural(names.length, 'file')}?`"
    :description="`${listed} ${names.length === 1 ? 'has' : 'have'} changes not saved. Switching workspace without saving drops them.`"
  >
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="emit('close', false)" />
      <UButton label="Don't save" color="error" variant="soft" @click="emit('close', true)" />
      <UButton label="Save all" color="primary" variant="solid" :loading="saving" @click="save()" />
    </template>
  </UModal>
</template>
