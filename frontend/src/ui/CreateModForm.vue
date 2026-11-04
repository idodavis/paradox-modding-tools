<!-- The dialog with the form that makes a mod and adds it to a workspace. It closes when the mod is made. -->
<script setup lang="ts">
import { shallowRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import { toSentence } from "../lib/format";
import { fileUrl, languagesQuery, modFolderQuery, workspacesQuery } from "../queries";
import FileSelector, { IMAGES } from "./FileSelector.vue";
import * as steam from "@services/steam/service";
import * as workspace from "@services/workspace/service";

const { workspaceId } = defineProps<{ workspaceId: string }>();
const open = defineModel<boolean>("open", { required: true });

const title = shallowRef("");
const description = shallowRef("");
const folder = shallowRef("");
const language = shallowRef<string>();
const thumbnail = shallowRef("");

const { data: workspaces } = useQuery(workspacesQuery);
/** install returns the id of the workspace's install, or "" before the workspaces are read. */
const install = () => workspaces.value?.find((row) => row.record.id === workspaceId)?.record.install ?? "";
const { data: languages } = useQuery(() => ({ ...languagesQuery(install()), enabled: open.value && !!install() }));
const { data: parent } = useQuery(() => ({
  ...modFolderQuery({ installId: install(), dir: folder.value, name: "" }),
  enabled: open.value && !!install(),
}));
const { data: modDir, error: nameError } = useQuery(() => ({
  ...modFolderQuery({ installId: install(), dir: folder.value, name: title.value.trim() }),
  enabled: open.value && !!install() && !!title.value.trim(),
}));

const { mutate: create, isLoading: creating } = useMutation({
  mutation: async () => {
    const mod = await workspace.CreateMod(workspaceId, folder.value, title.value.trim(), language.value ?? "");
    if (description.value || thumbnail.value) {
      const listing = await steam.ReadListing(mod.id, "");
      await steam.WriteListing(mod.id, "", {
        ...listing,
        description: description.value,
        thumbnail: thumbnail.value || listing.thumbnail,
      });
    }
    return mod;
  },
  onSuccess: () => (open.value = false),
});

watch(open, (opened) => {
  if (!opened) return;
  title.value = description.value = folder.value = thumbnail.value = "";
  language.value = undefined;
});
</script>

<template>
  <UModal
    v-model:open="open"
    title="Create a mod"
    description="A descriptor, the game's mod folders, readmes and a localization file."
    :ui="{ content: 'h-128' }"
  >
    <template #body>
      <form class="flex flex-col gap-4" @submit.prevent="create()">
        <UFormField label="Name" required>
          <UInput v-model="title" placeholder="Shown in the launcher and on Steam" class="w-full" autofocus />
        </UFormField>
        <UFormField label="Description" hint="Optional">
          <UTextarea
            v-model="description"
            placeholder="Starts the listing's description"
            :rows="2"
            autoresize
            class="w-full"
          />
        </UFormField>
        <UFormField label="Folder" :help="modDir && `Creates ${modDir}`" :error="toSentence(nameError?.message ?? '')">
          <FileSelector :path="folder || parent" folder :filter="[]" @update:path="folder = $event" />
        </UFormField>
        <div class="grid grid-cols-2 gap-4">
          <UFormField label="Localization language">
            <USelect
              v-model="language"
              :items="languages ?? []"
              value-key="folder"
              placeholder="The game's base language"
              class="w-full"
            />
          </UFormField>
          <UFormField label="Thumbnail" hint="Optional">
            <div class="flex items-center gap-2">
              <UAvatar v-if="thumbnail" :src="fileUrl(thumbnail)" class="rounded-md border border-accented" />
              <FileSelector v-model:path="thumbnail" :filter="IMAGES" class="flex-1" />
            </div>
          </UFormField>
        </div>
        <div class="flex justify-end">
          <UButton
            type="submit"
            label="Create mod"
            color="primary"
            variant="solid"
            :disabled="!title.trim() || !!nameError"
            :loading="creating"
          />
        </div>
      </form>
    </template>
    <template #footer="{ close }">
      <span class="grow" />
      <UButton label="Cancel" @click="close" />
    </template>
  </UModal>
</template>
