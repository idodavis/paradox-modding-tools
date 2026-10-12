<script setup lang="ts">
/**
 * A game's installs: those added, to pick one or delete one, and
 * adding another, found on this computer or browsed to. The wizard and Settings › Game install use it.
 */
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { AddGameInstall, DeleteGameInstall, UpdateGameInstall } from "@services/workspaceservice";
import { pickDirectory } from "../lib/nativeDialog";
import { foundInstallsQuery, installsQuery } from "../queries";
import FileSelector from "./FileSelector.vue";

const selected = defineModel<string>({ required: true });
const { gameId } = defineProps<{ gameId: string }>();

const { data: installs } = useQuery(() => installsQuery(gameId));
const { data: found } = useQuery(() => foundInstallsQuery(gameId));
const path = shallowRef("");
const name = shallowRef("");

const items = computed(() =>
  (installs.value ?? []).map((install) => ({
    value: install.id,
    label: install.name,
    description: install.path,
    scanned: !!install.scannedAt,
  })),
);
// the same folder as Go wrote it both times
const added = computed(() => new Set((installs.value ?? []).map((install) => install.path)));

const { mutate: add, isLoading: adding } = useMutation({
  mutation: (install: { name: string; path: string }) => AddGameInstall(gameId, install.name, install.path),
  onSuccess: (install) => {
    if (install) selected.value = install.id;
    path.value = "";
    name.value = "";
  },
});

const { mutate: move } = useMutation({
  mutation: ({ id, path }: { id: string; path: string }) => UpdateGameInstall(id, path),
});

async function changeFolder(id: string): Promise<void> {
  const path = await pickDirectory("The game's install folder");
  if (path) move({ id, path });
}

// Go refuses an install a workspace uses, and says which
const { mutate: remove } = useMutation({
  mutation: (id: string) => DeleteGameInstall(id),
  onSuccess: (_, id) => {
    if (selected.value === id) selected.value = "";
  },
});
</script>

<template>
  <div class="grid grid-cols-2 gap-6">
    <div class="flex min-w-0 flex-col gap-2.5">
      <span class="text-12 font-medium text-toned">Added installs</span>
      <URadioGroup
        v-if="items.length"
        v-model="selected"
        :items="items"
        variant="card"
        class="w-full"
        :ui="{ fieldset: 'w-full min-w-0', item: 'w-full min-w-0', wrapper: 'min-w-0' }"
      >
        <template #label="{ item }">
          <span class="flex items-center gap-2">
            <span class="truncate text-13 font-medium">{{ item.label }}</span>
            <UBadge v-if="item.scanned" label="Cache ready" color="success" />
            <UBadge v-else label="Not scanned" />
            <span class="grow" />
            <span @click.prevent>
              <UDropdownMenu
                :items="[
                  { label: 'Change folder…', icon: 'i-lucide-folder', onSelect: () => changeFolder(item.value) },
                  {
                    label: 'Delete install',
                    icon: 'i-lucide-trash-2',
                    color: 'error',
                    onSelect: () => remove(item.value),
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
      <div v-if="found?.length" class="flex flex-col gap-2 rounded-lg border border-default bg-muted px-3 py-2.5">
        <span class="text-12 text-dimmed">Found on this computer</span>
        <div v-for="install in found" :key="install.path" class="flex items-center gap-2">
          <div class="flex min-w-0 grow flex-col gap-px">
            <span class="text-12 font-medium">{{ install.name }} {{ install.version }}</span>
            <span class="truncate font-mono text-12 text-dimmed">{{ install.path }}</span>
          </div>
          <UBadge v-if="added.has(install.path)" label="Added" />
          <UButton v-else label="Add" :loading="adding" @click="add({ name: install.name, path: install.path })" />
        </div>
      </div>
      <UFormField label="Install folder">
        <FileSelector
          v-model="path"
          mode="folder"
          dialog-title="The game's install folder"
          placeholder="The folder that holds the game"
        />
      </UFormField>
      <UFormField label="Name">
        <UInput v-model="name" placeholder="Shown in the Library and the workspace switcher" class="w-full" />
      </UFormField>
      <div class="flex justify-end">
        <UButton
          label="Add install"
          icon="i-lucide-plus"
          :disabled="!path || !name.trim()"
          :loading="adding"
          @click="add({ name: name.trim(), path })"
        />
      </div>
    </div>
  </div>
</template>
