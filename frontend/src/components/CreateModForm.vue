<script setup lang="ts">
/**
 * Creates a mod: its descriptor, an empty folder for each of the game's mod folders, readmes and a
 * localization stub with a UTF-8 BOM, then adds it to the workspace when given one. Opened through
 * useOverlay, it closes with the new mod's folder.
 */
import { computed, shallowRef } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { AddWorkspaceMod, CreateMod } from "@services/workspaceservice";
import { IMAGE_FILTER } from "../lib/nativeDialog";
import { modFolderQuery, modParentQuery, workspaceKeys } from "../queries";
import { LOC_LANGUAGES } from "../workspaceTools";

const { gameId, workspaceId = "", locLang = "english", supportedVersion = "" } = defineProps<{
  gameId: string;
  /** The workspace the mod joins; none for the wizard, which adds it when it makes the workspace. */
  workspaceId?: string;
  locLang?: string;
  supportedVersion?: string;
}>();
const emit = defineEmits<{ close: [path?: string] }>();

const queryCache = useQueryCache();
const name = shallowRef("");
const description = shallowRef("");
const chosenParent = shallowRef("");
const lang = shallowRef(locLang);
const thumbnail = shallowRef("");

const { data: defaultParent } = useQuery(() => modParentQuery(gameId));
const parent = computed({
  get: () => chosenParent.value || defaultParent.value || "",
  set: (path: string) => (chosenParent.value = path),
});
const { data: folder } = useQuery(() => modFolderQuery({ parent: parent.value, name: name.value }));

const { mutate: create, isLoading: creating } = useMutation({
  mutation: async () => {
    const path = await CreateMod(gameId, parent.value, name.value.trim(), lang.value, supportedVersion, description.value, thumbnail.value);
    // Go names the mod from its descriptor, and finds the thumbnail where the game keeps it
    if (workspaceId) await AddWorkspaceMod(workspaceId, path);
    return path;
  },
  onSuccess: (path) => emit("close", path),
  onSettled: () => queryCache.invalidateQueries({ key: workspaceKeys.all }),
});
</script>

<template>
  <UModal title="Create a mod" description="A descriptor, the game's mod folders, readmes and a localization file.">
    <template #body>
      <div class="flex flex-col gap-4">
        <UFormField label="Name" required>
          <UInput v-model="name" placeholder="Shown in the launcher and on Steam" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Description" hint="Optional">
          <UTextarea v-model="description" placeholder="Starts the listing's description" :rows="2" autoresize class="w-full" />
        </UFormField>
        <UFormField label="Folder" :help="folder ? `Creates ${folder}` : undefined">
          <FileSelector v-model="parent" mode="folder" dialog-title="Folder for the new mod" />
        </UFormField>
        <div class="grid grid-cols-2 gap-4">
          <UFormField label="Localization language">
            <USelect v-model="lang" :items="LOC_LANGUAGES" class="w-full" />
          </UFormField>
          <UFormField label="Thumbnail" hint="Optional">
            <div class="flex items-center gap-2">
              <Thumb v-if="thumbnail" :path="thumbnail" />
              <FileSelector v-model="thumbnail" mode="file" dialog-title="Thumbnail" :file-filter="IMAGE_FILTER" class="flex-1" />
            </div>
          </UFormField>
        </div>
      </div>
    </template>
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="emit('close')" />
      <UButton
        label="Create mod"
        color="primary"
        variant="solid"
        :disabled="!name.trim() || !parent"
        :loading="creating"
        @click="create()"
      />
    </template>
  </UModal>
</template>
