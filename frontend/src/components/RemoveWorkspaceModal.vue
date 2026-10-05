<script setup lang="ts">
/**
 * Asks before removing a workspace from PMT, then removes it; mod folders on disk stay. Opened through
 * useOverlay, it closes with true once the workspace is gone.
 */
import { useMutation, useQueryCache } from "@pinia/colada";
import { DeleteWorkspace } from "@services/workspaceservice";
import { workspaceKeys } from "../queries";

const { id, name } = defineProps<{ id: string; name: string }>();
const emit = defineEmits<{ close: [removed: boolean] }>();

const queryCache = useQueryCache();
const toast = useToast();
const { mutate: remove, isLoading: removing } = useMutation({
  mutation: () => DeleteWorkspace(id),
  onSuccess: () => {
    toast.add({ title: `${name} removed`, color: "success" });
    emit("close", true);
  },
  onSettled: () => queryCache.invalidateQueries({ key: workspaceKeys.all }),
});
</script>

<template>
  <UModal
    :title="`Remove ${name}?`"
    description="The workspace leaves PMT. Its mod folders stay on disk, and you can add them to another workspace."
  >
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="emit('close', false)" />
      <UButton label="Remove workspace" color="error" variant="soft" :loading="removing" @click="remove()" />
    </template>
  </UModal>
</template>
