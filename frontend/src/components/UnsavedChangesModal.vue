<script setup lang="ts">
/**
 * Asked before the editors' unsaved files would go (another workspace's take their place, or the window reloads): save
 * them, drop them, or stay. Opened through useOverlay, it closes with true to go on; `going` says what drops them.
 */
import { computed } from "vue";
import { useMutation } from "@pinia/colada";
import { plural } from "../lib/format";
import { useIdeStore } from "../stores/ide";

const { going = "Switching workspace" } = defineProps<{ going?: string }>();
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
    :description="`${listed} ${names.length === 1 ? 'has' : 'have'} changes not saved. ${going} without saving drops them.`"
  >
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="emit('close', false)" />
      <UButton label="Don't save" color="error" variant="soft" @click="emit('close', true)" />
      <UButton label="Save all" color="primary" variant="solid" :loading="saving" @click="save()" />
    </template>
  </UModal>
</template>
