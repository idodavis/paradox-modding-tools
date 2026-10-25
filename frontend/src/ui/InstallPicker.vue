<!-- The installs of one game: the added ones to pick, move or delete, and a way to add another. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { Dialogs } from "@wailsio/runtime";

import { installLine } from "../lib/format";
import { detectedInstallsQuery, gamesQuery, installsQuery } from "../queries";
import ConfirmModal from "./ConfirmModal.vue";
import FileSelector from "./FileSelector.vue";
import * as workspace from "@services/workspace/service";

const installId = defineModel<string>({ required: true });
const { gameId } = defineProps<{ gameId: string }>();

const confirm = useOverlay().create(ConfirmModal);
const folder = shallowRef("");

const { data: games } = useQuery(gamesQuery);
const { data: allInstalls } = useQuery(installsQuery);
const { data: detected } = useQuery(() => ({ ...detectedInstallsQuery(gameId), enabled: !!gameId }));
const installs = computed(() => allInstalls.value?.filter((install) => install.game === gameId) ?? []);
const items = computed(() => {
  const shortName = games.value?.find((game) => game?.id === gameId)?.shortName ?? gameId;
  return installs.value.map((install) => ({
    value: install.id,
    label: installLine(shortName, install.version, install.scannedAt),
    description: install.path,
  }));
});

const { mutate: addInstall, isLoading: adding } = useMutation({
  mutation: (path: string) => workspace.AddInstall(gameId, path),
  onSuccess: (install) => {
    installId.value = install.id;
    folder.value = "";
  },
});
const { mutate: deleteInstall } = useMutation({ mutation: (id: string) => workspace.DeleteInstall(id) });
const { mutate: updateInstall } = useMutation({
  mutation: async (id: string) => {
    const path = await Dialogs.OpenFile({ CanChooseFiles: false, CanChooseDirectories: true });
    if (path) await workspace.UpdateInstall(id, path);
  },
});

/** removeInstall asks before an install's record is deleted. Go refuses while a workspace uses the install. */
async function removeInstall(id: string): Promise<void> {
  const confirmed = await confirm.open({
    title: "Delete this install?",
    body: "PMT forgets the install and its caches. The game's files stay where they are.",
    confirm: "Delete install",
    destructive: true,
  });
  if (confirmed) deleteInstall(id);
}
</script>

<template>
  <div class="grid grid-cols-2 gap-6">
    <div class="flex min-w-0 flex-col gap-2.5">
      <span class="text-12 font-medium text-toned">Added installs</span>
      <URadioGroup
        v-if="items.length"
        v-model="installId"
        :items="items"
        variant="card"
        class="w-full"
        :ui="{ fieldset: 'w-full min-w-0', item: 'w-full min-w-0', wrapper: 'min-w-0' }"
      >
        <template #label="{ item }">
          <span class="flex items-center gap-2">
            <span class="truncate text-13 font-medium">{{ item.label }}</span>
            <span class="grow" />
            <span @click.prevent>
              <UDropdownMenu
                :items="[
                  { label: 'Change folder…', icon: 'i-lucide-folder', onSelect: () => updateInstall(item.value) },
                  {
                    label: 'Delete install',
                    icon: 'i-lucide-trash-2',
                    color: 'error',
                    onSelect: () => removeInstall(item.value),
                  },
                ]"
              >
                <UButton icon="i-lucide-ellipsis" variant="ghost" size="xs" square aria-label="More" />
              </UDropdownMenu>
            </span>
          </span>
        </template>
        <template #description="{ item }">
          <span class="block truncate font-mono text-12 text-dimmed">{{ item.description }}</span>
        </template>
      </URadioGroup>
      <p v-else class="text-12 text-dimmed">No installs added yet. Add one on the right.</p>
    </div>

    <div class="flex min-w-0 flex-col gap-2.5">
      <span class="text-12 font-medium text-toned">Add an install</span>
      <div v-if="detected?.length" class="flex flex-col gap-2 rounded-lg border border-default bg-muted px-3 py-2.5">
        <span class="text-12 text-dimmed">Found on this computer</span>
        <div v-for="install in detected" :key="install.path" class="flex items-center gap-2">
          <div class="flex min-w-0 grow flex-col gap-px">
            <span class="text-12 font-medium">{{ install.name }} {{ install.version }}</span>
            <span class="truncate font-mono text-12 text-dimmed">{{ install.path }}</span>
          </div>
          <UBadge v-if="installs.some((one) => one.path === install.path)" label="Added" />
          <UButton v-else label="Add" :loading="adding" @click="addInstall(install.path)" />
        </div>
      </div>
      <UFormField label="Another folder">
        <FileSelector v-model:path="folder" folder :filter="[]" />
      </UFormField>
      <div class="flex justify-end">
        <UButton
          label="Add install"
          icon="i-lucide-plus"
          :disabled="!folder"
          :loading="adding"
          @click="addInstall(folder)"
        />
      </div>
    </div>
  </div>
</template>
