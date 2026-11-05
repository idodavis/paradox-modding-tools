<!--
  The dialog with the form that makes a mod and adds it to a workspace. With Guidance on, it first asks what to make.
  It closes when the mod is made.
-->
<script setup lang="ts">
import { computed, ref, shallowRef, watch } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";

import { toSentence } from "../lib/format";
import { GOALS, isPicked } from "../lib/goals";
import { fileUrl, languagesQuery, modFolderQuery, workspacesQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";
import FileSelector, { IMAGES } from "./FileSelector.vue";
import * as steam from "@services/steam/service";
import * as workspace from "@services/workspace/service";

const { workspaceId } = defineProps<{ workspaceId: string }>();
const open = defineModel<boolean>("open", { required: true });

/** OFFERED holds the goals that make a mod, the ones the dialog asks about. */
const OFFERED = GOALS.filter((goal) => goal.makes);

const router = useRouter();
const settings = useSettingsStore();
const title = shallowRef("");
const description = shallowRef("");
const folder = shallowRef("");
const language = shallowRef<string>();
const thumbnail = shallowRef("");
const goalId = shallowRef("add");
const chosen = shallowRef(false);
const picks = ref(["", ""]);

const { data: workspaces } = useQuery(workspacesQuery);
const row = computed(() => workspaces.value?.find((listed) => listed.record.id === workspaceId));
/** install returns the id of the workspace's install, or "" before the workspaces are read. */
const install = () => row.value?.record.install ?? "";
/** The goal being made, or undefined when Guidance is off. */
const goal = computed(() => (settings.isGuided() ? OFFERED.find((offered) => offered.id === goalId.value) : undefined));
const choosing = computed(() => settings.isGuided() && !chosen.value);
const existing = computed(() => goal.value?.existing ?? 0);
const picked = computed(() => picks.value.slice(0, existing.value));
const modItems = computed(() =>
  (row.value?.mods ?? [])
    .filter((mod) => !mod.broken)
    .map((mod) => ({ label: mod.descriptor.name || mod.id, value: mod.id })),
);
const { data: languages } = useQuery(() => ({ ...languagesQuery(install()), enabled: open.value && !!install() }));
const { data: parent } = useQuery(() => ({
  ...modFolderQuery({ installId: install(), dir: folder.value, name: "" }),
  enabled: open.value && !!install(),
}));
const { data: modDir, error: nameError } = useQuery(() => ({
  ...modFolderQuery({ installId: install(), dir: folder.value, name: title.value.trim() }),
  enabled: open.value && !!install() && !!title.value.trim(),
}));
const blocked = computed(
  () =>
    !title.value.trim() || !!nameError.value || (!!goal.value && !isPicked(goal.value, picked.value, language.value)),
);

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
    if (goal.value?.id === "translate" && row.value) {
      await workspace.UpdateWorkspace({ ...row.value.record, language: language.value ?? "" });
    }
    return mod;
  },
  onSuccess: () => {
    open.value = false;
    if (!goal.value) return;
    const mod = goal.value.id === "translate" ? picks.value[0] : undefined;
    void router.push({ ...goal.value.opens, params: { id: workspaceId }, query: { ...goal.value.opens.query, mod } });
  },
});

watch(open, (opened) => {
  if (!opened) return;
  title.value = description.value = folder.value = thumbnail.value = "";
  language.value = undefined;
  goalId.value = "add";
  chosen.value = false;
  picks.value = ["", ""];
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
      <URadioGroup
        v-if="choosing"
        v-model="goalId"
        legend="What do you want to make?"
        :items="OFFERED"
        value-key="id"
        label-key="label"
        description-key="blurb"
        variant="card"
      />
      <form v-else class="flex flex-col gap-4" @submit.prevent="create()">
        <UFormField v-for="n in existing" :key="n" :label="existing > 1 ? `Mod ${n}` : 'Mod'" required>
          <USelect v-model="picks[n - 1]" :items="modItems" placeholder="Pick a mod" class="w-full" />
        </UFormField>
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
          <UFormField
            :label="goal?.id === 'translate' ? 'Language of the translation' : 'Localization language'"
            :required="goal?.id === 'translate'"
          >
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
            :disabled="blocked"
            :loading="creating"
          />
        </div>
      </form>
    </template>
    <template #footer="{ close }">
      <UButton v-if="settings.isGuided() && chosen" label="Back" variant="ghost" @click="chosen = false" />
      <span class="grow" />
      <UButton label="Cancel" @click="close" />
      <UButton v-if="choosing" label="Continue" color="primary" variant="solid" @click="chosen = true" />
    </template>
  </UModal>
</template>
