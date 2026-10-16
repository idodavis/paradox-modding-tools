<script setup lang="ts">
/**
 * The new coat dialog: a coat written into a mod's coat file under an id, then shown beside its code. It holds a plain
 * coat on the game's solid pattern, the game designer's copy pasted, or a read-only coat's script to edit as the
 * mod's; the file is the mod's last that defines coats, else a new one in its game's coat folder, which the dialog
 * says before it writes, with its folder where that is new too.
 */
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { coatFileQuery, texturesQuery } from "../queries";
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

// the file the coat goes into, and what the dialog says of it: added to, or made (with its folder where that is new)
const { data: target } = useQuery(() =>
  coatFileQuery({ id: isOpen.value ? workspace.activeWorkspaceId : "", mod: mod.value }),
);
const targetText = computed(() => {
  if (!target.value) return "The coat goes into its last coat of arms file, or a new one.";
  const modLabel = modItems.value.find((one) => one.value === mod.value)?.label ?? "";
  if (!target.value.isNewFile) return `Added at the end of ${target.value.rel}.`;
  return `Creates ${target.value.rel} in ${modLabel}${target.value.isNewFolder ? ", its folders too" : ""}.`;
});

// a plain coat starts on the game's solid pattern, the first pattern where it has none
const { data: patterns } = useQuery(() =>
  texturesQuery({ id: workspace.activeWorkspaceId, key: isOpen.value ? "pattern" : "" }),
);
const plainCoat = computed(() => {
  const files = Object.keys(patterns.value ?? {});
  const pattern = files.find((file) => file.startsWith("pattern_solid")) ?? files[0];
  return [`pattern = "${pattern ?? ""}"`, 'color1 = "white"', 'color2 = "black"'].join("\n");
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
  const coatId = id.value.trim();
  try {
    if (target.value) await store.addCoat(target.value.path, coatId, coatEntry(coatId, text.value));
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
        <UFormField label="Mod" :description="targetText">
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
        <UButton label="Add to the mod" :disabled="!id.trim() || !target" @click="void add()" />
      </div>
    </template>
  </UModal>
</template>
