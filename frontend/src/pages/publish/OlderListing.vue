<!--
  This dialog adds a listing for an older game version to a mod. It is made from a zip, or imported from the Workshop.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation } from "@pinia/colada";
import { Dialogs } from "@wailsio/runtime";

import { useZip, ZIP } from "./workshop";
import * as steam from "@services/steam/service";

/** MODES holds the dialog's words for each way of adding a listing. */
const MODES = {
  create: {
    title: "New listing for an older game version",
    description:
      "A separate Workshop item for players on an older game. Its files come from a zip, not the mod's folder.",
    zip: "Zip with its files",
    action: "Create listing",
  },
  import: {
    title: "Import a listing from the Workshop",
    description: "Takes an item that is on the Workshop into this mod as an older listing, with its page and files.",
    zip: "Save its files as",
    action: "Import listing",
  },
} as const;

const { modId, modTitle, mode } = defineProps<{ modId: string; modTitle: string; mode: keyof typeof MODES }>();
const open = defineModel<boolean>("open", { required: true });
const emit = defineEmits<{ added: [key: string] }>();

const gameVersion = shallowRef("");
const workshopItem = shallowRef("");
const zip = shallowRef("");
const ready = computed(() => !!zip.value && !!(mode === "create" ? gameVersion.value : workshopItem.value).trim());

const { mutate: add, isLoading: adding } = useMutation({
  mutation: () =>
    mode === "create"
      ? steam.CreateListing(modId, gameVersion.value.trim(), zip.value)
      : steam.ImportListing(modId, workshopItem.value.trim(), zip.value),
  onSuccess: (listing) => {
    open.value = false;
    gameVersion.value = workshopItem.value = zip.value = "";
    emit("added", listing.key);
  },
});
const { mutate: writeZip, isLoading: zipping } = useZip(
  () => modId,
  (path) => (zip.value = path),
);

/** submit adds the listing if its fields are filled in. */
function submit(): void {
  if (ready.value) add();
}

/** findZip asks where the zip is, or for an import, where to save it. */
async function findZip(): Promise<void> {
  const path =
    mode === "create"
      ? await Dialogs.OpenFile({ CanChooseFiles: true, Filters: ZIP })
      : await Dialogs.SaveFile({ Filename: "workshop-item.zip", Filters: ZIP });
  if (path) zip.value = path;
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="MODES[mode].title"
    :description="MODES[mode].description"
    :ui="{ content: 'h-96 sm:max-w-lg' }"
  >
    <template #body>
      <form class="flex flex-col gap-4" @submit.prevent="submit">
        <UFormField v-if="mode === 'create'" label="Game version" help="The version of the game this listing is for.">
          <UInput v-model="gameVersion" placeholder="1.19" class="w-full font-mono" autofocus />
        </UFormField>
        <UFormField v-else label="Workshop item" help="The item's id or the address of its Workshop page.">
          <UInput
            v-model="workshopItem"
            placeholder="The item's id or its address"
            class="w-full font-mono"
            autofocus
          />
        </UFormField>
        <UFormField :label="MODES[mode].zip">
          <div class="flex gap-2">
            <UInput :model-value="zip" readonly placeholder="No zip chosen yet" class="min-w-0 flex-1 font-mono" />
            <UButton label="Choose…" @click="findZip" />
          </div>
          <UButton
            v-if="mode === 'create'"
            label="Save the mod as a zip first"
            variant="link"
            class="mt-1.5 h-auto p-0 text-12"
            :loading="zipping"
            @click="writeZip(modTitle)"
          />
        </UFormField>
      </form>
    </template>
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="open = false" />
      <UButton
        :label="MODES[mode].action"
        color="primary"
        variant="solid"
        :disabled="!ready"
        :loading="adding"
        @click="add()"
      />
    </template>
  </UModal>
</template>
