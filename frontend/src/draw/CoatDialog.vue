<script setup lang="ts">
/**
 * The new coat dialog: a coat written into a mod's coat file under an id, then shown beside its code. It holds a plain
 * coat on the game's solid pattern, the game designer's copy pasted, or a read-only coat's script to edit as the
 * mod's; the file is the mod's last that defines coats, else a new one in its game's coat folder.
 */
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { texturesQuery } from "../queries";
import { coatEntry, useDrawStore } from "../stores/draw";
import { useWorkspaceStore } from "../stores/workspace";

const store = useDrawStore();
const workspace = useWorkspaceStore();
const toast = useToast();

/** What each way into the dialog says. */
const TITLES = {
  new: { title: "New coat of arms", description: "A coat on the game's solid pattern, to make your own." },
  paste: { title: "Paste a coat from the game", description: "The script the game's coat designer copies." },
  copy: { title: "Edit a copy in a mod", description: "This coat's script, written into a mod under the same id." },
};

const id = shallowRef("");
const mod = shallowRef("");
const text = shallowRef("");
const isOpen = computed({
  get: () => !!store.coatDialog,
  set: (open: boolean) => {
    if (!open) store.coatDialog = null;
  },
});
const texts = computed(() => TITLES[store.coatDialog?.mode ?? "new"]);
const modItems = computed(() =>
  workspace.workspaceMods.filter((one) => !one.isBroken).map((one) => ({ label: one.name, value: one.id })),
);

// a plain coat starts on the game's solid pattern, the first pattern where it has none
const { data: patterns } = useQuery(() =>
  texturesQuery({ id: workspace.activeWorkspaceId, key: isOpen.value ? "pattern" : "" }),
);
const plainCoat = computed(() => {
  const pattern = patterns.value?.find((one) => one.id.startsWith("pattern_solid")) ?? patterns.value?.[0];
  return [`pattern = "${pattern?.id ?? ""}"`, 'color1 = "white"', 'color2 = "black"'].join("\n");
});

// each opening starts from what it was opened with
watch(
  () => store.coatDialog,
  (dialog) => {
    if (!dialog) return;
    id.value = dialog.id;
    mod.value = dialog.mod || (modItems.value[0]?.value ?? "");
    text.value = dialog.text || (dialog.mode === "new" ? plainCoat.value : "");
  },
);
// the plain coat follows the patterns once they are read, until the text is changed
watch(plainCoat, (coat, previous) => {
  if (store.coatDialog?.mode === "new" && (!text.value || text.value === previous)) text.value = coat;
});

/** Writes the coat into the mod's file and closes; a file it cannot write into is said, and the dialog stays. */
async function add(): Promise<void> {
  const name = id.value.trim();
  try {
    await store.addCoat(workspace.activeWorkspaceId, mod.value, name, coatEntry(name, text.value));
    store.coatDialog = null;
  } catch (error) {
    toast.add({ title: "The coat was not added", description: String(error), color: "error" });
  }
}
</script>

<template>
  <UModal v-model:open="isOpen" :title="texts.title" :description="texts.description" :ui="{ content: 'sm:max-w-xl' }">
    <template #body>
      <div class="flex flex-col gap-3">
        <UFormField label="Mod" description="The coat goes into its last coat of arms file, or a new one.">
          <USelect v-model="mod" :items="modItems" class="w-full" placeholder="This workspace has no mod" />
        </UFormField>
        <UFormField label="Id" description="A title's, a dynasty's or a house's key draws this coat for it.">
          <UInput v-model="id" class="w-full font-mono" placeholder="k_my_kingdom" autofocus />
        </UFormField>
        <UFormField label="Script">
          <UTextarea v-model="text" :rows="12" class="w-full font-mono" :ui="{ base: 'text-12' }" />
        </UFormField>
      </div>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton label="Cancel" color="neutral" variant="ghost" @click="isOpen = false" />
        <UButton label="Add to the mod" :disabled="!id.trim() || !mod" @click="void add()" />
      </div>
    </template>
  </UModal>
</template>
