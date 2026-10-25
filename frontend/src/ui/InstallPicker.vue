<!-- The installs of one game: the added ones with their scan, to pick, move or delete, and a way to add another. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { Dialogs } from "@wailsio/runtime";

import { describeLexicon, installLine, toSentence } from "../lib/format";
import { detectedInstallsQuery, installsQuery, readyInstallsQuery, stateQuery } from "../queries";
import ConfirmModal from "./ConfirmModal.vue";
import FileSelector from "./FileSelector.vue";
import * as workspace from "@services/workspace/service";

const installId = defineModel<string>({ required: true });
const { gameId } = defineProps<{ gameId: string }>();

const confirm = useOverlay().create(ConfirmModal);
const folder = shallowRef("");
const title = shallowRef("");
const version = shallowRef("");

const { data: allInstalls } = useQuery(installsQuery);
const { data: ready } = useQuery(readyInstallsQuery);
const { data: state } = useQuery(stateQuery);
const { data: detected } = useQuery(() => ({ ...detectedInstallsQuery(gameId), enabled: !!gameId }));
const installs = computed(() => allInstalls.value?.filter((install) => install.game === gameId) ?? []);
const items = computed(() =>
  installs.value.map((install) => ({
    value: install.id,
    label: install.name,
    description: install.path,
    ready: isReady(install.id),
  })),
);
// the picked install's version and scan; the counts are known only for the install of the open workspace
const scanLine = computed(() => {
  const install = installs.value.find((one) => one.id === installId.value);
  if (!install) return "";
  const line = `${toSentence(installLine("", install.version, install.scannedAt))}.`;
  const open = state.value?.open && state.value.install.id === install.id ? state.value : undefined;
  return open ? `${line} ${describeLexicon(open)}.` : line;
});

// Go fills an empty name with the game's and an empty version with the one the install's files say
const { mutate: addInstall, isLoading: adding } = useMutation({
  mutation: (install: { path: string; name: string; version: string }) =>
    workspace.AddInstall(gameId, install.path, install.name, install.version),
  onSuccess: (install) => {
    installId.value = install.id;
    folder.value = "";
    title.value = "";
    version.value = "";
  },
});
const { mutate: deleteInstall } = useMutation({ mutation: (id: string) => workspace.DeleteInstall(id) });
const { mutate: updateInstall } = useMutation({
  mutation: async (id: string) => {
    const path = await Dialogs.OpenFile({ CanChooseFiles: false, CanChooseDirectories: true });
    if (path) await workspace.UpdateInstall(id, path);
  },
});

/** isReady says whether an install's caches are saved, so its next open scans nothing. */
function isReady(id: string): boolean {
  return !!ready.value?.includes(id);
}

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
            <UBadge v-if="item.ready" label="Cache ready" color="success" />
            <UBadge v-else label="Not scanned" />
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
      <span v-if="scanLine" class="flex items-center gap-1.5 text-12 text-toned">
        <UIcon v-if="isReady(installId)" name="i-lucide-check" class="size-3.5 shrink-0 text-success" />{{ scanLine }}
      </span>
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
          <UButton
            v-else
            label="Add"
            :loading="adding"
            @click="addInstall({ path: install.path, name: install.name, version: install.version })"
          />
        </div>
      </div>
      <UFormField label="Another folder">
        <FileSelector v-model:path="folder" folder :filter="[]" />
      </UFormField>
      <UFormField label="Name">
        <UInput v-model="title" placeholder="Shown in the Library and the added installs" class="w-full" />
      </UFormField>
      <UFormField label="Version">
        <UInput v-model="version" placeholder="Read from the install when left empty" class="w-full" />
      </UFormField>
      <div class="flex justify-end">
        <UButton
          label="Add install"
          icon="i-lucide-plus"
          :disabled="!folder"
          :loading="adding"
          @click="addInstall({ path: folder, name: title.trim(), version: version.trim() })"
        />
      </div>
    </div>
  </div>
</template>
