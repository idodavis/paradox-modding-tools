<!--
  The new coat dialog: a mod, an id and a script, the game designer's copy pasted or left empty for a plain coat on the
  game's solid pattern. The coat file is created and shown.
-->
<script setup lang="ts">
import { useMutation, useQuery } from "@pinia/colada";
import { computed, shallowRef } from "vue";

import * as draw from "@services/draw/service";
import * as workspace from "@services/workspace/service";

import { stateQuery } from "../../../queries";
import { useEditorStore } from "../../../stores/editor";
import { listMods } from "./coat";

const emit = defineEmits<{ close: [result: { modId: string; name: string } | null] }>();

const editor = useEditorStore();
const { data: state } = useQuery(stateQuery);
const mods = computed(() => listMods(state.value?.mods));
const choice = shallowRef("");
const id = shallowRef("");
const script = shallowRef("");
const modId = computed({
  get: () => choice.value || (mods.value[0]?.value ?? ""),
  set: (mod: string) => (choice.value = mod),
});

const { mutate: create } = useMutation({
  mutation: async () => workspace.Locate(await draw.CreateCoatFile(modId.value, id.value.trim(), script.value)),
  onSuccess: (place) => {
    editor.show(place);
    emit("close", { modId: modId.value, name: id.value.trim() });
  },
});
</script>

<template>
  <UModal
    title="New coat of arms"
    description="A coat on the game's solid pattern to make your own, or one the game's coat designer copied."
    :ui="{ content: 'sm:max-w-xl' }"
    :close="{ onClick: () => emit('close', null) }"
  >
    <template #body>
      <div class="flex flex-col gap-3">
        <UFormField label="Mod" description="The coat goes into a new file in the mod's coat of arms folder.">
          <USelect v-model="modId" :items="mods" placeholder="This workspace has no mod" class="w-full" />
        </UFormField>
        <UFormField label="Id" description="A title's, a dynasty's or a house's key draws this coat for it.">
          <UInput v-model="id" class="w-full font-mono" placeholder="k_my_kingdom" autofocus />
        </UFormField>
        <UFormField label="Script" description="Paste what the game's coat designer copies, or leave it empty.">
          <UTextarea v-model="script" :rows="10" class="w-full font-mono" :ui="{ base: 'text-12' }" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton label="Cancel" color="neutral" variant="ghost" @click="emit('close', null)" />
        <UButton label="Add to the mod" :disabled="!id.trim() || !modId" @click="create()" />
      </div>
    </template>
  </UModal>
</template>
