<!-- The new mod form: its name, description, folder, localization language and thumbnail. It creates the mod. -->
<script setup lang="ts">
import { shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import { describeLanguages } from "../lib/format";
import { fileUrl, languagesQuery, modFolderQuery, workspacesQuery } from "../queries";
import FileSelector from "./FileSelector.vue";
import * as steam from "@services/steam/service";
import * as workspace from "@services/workspace/service";
import type { Mod } from "@services/workspace/models";

const IMAGES = ["*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp"];

const { workspaceId } = defineProps<{ workspaceId: string }>();
const emit = defineEmits<{ created: [mod: Mod] }>();

const title = shallowRef("");
const description = shallowRef("");
const folder = shallowRef("");
const language = shallowRef<string>();
const thumbnail = shallowRef("");

const { data: workspaces } = useQuery(workspacesQuery);
const install = () => workspaces.value?.find((record) => record.id === workspaceId)?.install ?? "";
const { data: languages } = useQuery(() => ({ ...languagesQuery(install()), enabled: !!install() }));
const { data: parent } = useQuery(() => modFolderQuery({ workspaceId, dir: folder.value, name: "" }));
const { data: modDir } = useQuery(() => ({
  ...modFolderQuery({ workspaceId, dir: folder.value, name: title.value.trim() }),
  enabled: !!title.value.trim(),
}));

const { mutate: create, isLoading: creating } = useMutation({
  mutation: async () => {
    const mod = await workspace.CreateMod(workspaceId, folder.value, title.value.trim(), language.value ?? "");
    if (description.value) await steam.SetDescriptionSource(mod.id, "markdown", "");
    if (description.value || thumbnail.value) {
      const listing = await steam.ReadListing(mod.id);
      await steam.WriteListing(mod.id, {
        ...listing,
        description: description.value,
        thumbnail: thumbnail.value || listing.thumbnail,
      });
    }
    return mod;
  },
  onSuccess: (mod) => emit("created", mod),
});
</script>

<template>
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
    <UFormField label="Folder" :help="modDir && `Creates ${modDir}`">
      <FileSelector :path="folder || parent" folder :filter="[]" @update:path="folder = $event" />
    </UFormField>
    <div class="grid grid-cols-2 gap-4">
      <UFormField label="Localization language">
        <USelect
          v-model="language"
          :items="describeLanguages(languages)"
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
        :disabled="!title.trim()"
        :loading="creating"
      />
    </div>
  </form>
</template>
