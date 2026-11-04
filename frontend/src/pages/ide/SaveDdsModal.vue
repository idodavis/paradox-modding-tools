<!--
  Save as DDS: writes a picture, or the pictures directly in a folder, as DDS files beside them. The pictures start
  with the settings of the DDS files they replace when those agree.
-->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import * as draw from "@services/draw/service";
import type { Place } from "@services/workspace/models";
import { plural } from "../../lib/format";
import { ddsPicturesQuery, loadQuery, placeQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";

const { place } = defineProps<{ place: Place }>();
const emit = defineEmits<{ close: [] }>();

const FORMATS = [
  { value: "uncompressed", label: "Uncompressed", description: "Exact. Four bytes a pixel." },
  { value: "BC3", label: "BC3", description: "A quarter of the size. Colors blur slightly within each 4×4 block." },
];

const toast = useToast();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const format = shallowRef("uncompressed");
const mipmaps = shallowRef(false);
const likeness = shallowRef("");
const { data } = useQuery(() => ddsPicturesQuery(place.path));
const pictures = computed(() => data.value?.pictures ?? []);
// a mod's own folder, or a picture at its top, has no path in the mod, so it goes by the mod's name
const folder = computed(() => (place.isDir ? place.rel : place.folder) || workspace.originName(place.origin));
const summary = computed(() => {
  const [first] = pictures.value;
  if (!first) return "No PNG or TGA pictures";
  if (pictures.value.length === 1) return `${first.name} → ${toDDS(first.name)}`;
  return `${pictures.value.length} pictures → a .dds beside each`;
});
watch(data, preselect, { immediate: true });

const {
  mutate: save,
  isLoading,
  error,
} = useMutation({
  mutation: () =>
    draw.WriteDDS(
      pictures.value.map((picture) => picture.path),
      format.value === "BC3",
      mipmaps.value,
    ),
  onSuccess: (paths) => {
    const [first] = pictures.value;
    const title = pictures.value.length === 1 && first ? toDDS(first.name) : plural(pictures.value.length, "DDS file");
    toast.add({
      title: `Saved ${title}`,
      color: "success",
      actions: [{ label: "Open", onClick: () => void open(paths?.[0] ?? "") }],
    });
    emit("close");
  },
});

/** toDDS returns the name of the DDS a picture of the given name is saved as. */
function toDDS(name: string): string {
  return name.replace(/\.[^.]+$/, ".dds");
}

/**
 * preselect sets the dialog's choices to the settings of the DDS files the pictures replace, when every picture has
 * one and they agree, and says so. Otherwise the choices keep their default, uncompressed without mipmaps.
 */
function preselect(): void {
  const references = pictures.value.flatMap((picture) => data.value?.references?.[picture.path] ?? []);
  const [reference] = references;
  if (!reference || references.length < pictures.value.length) return;
  const settings = references.map(({ header }) => ({
    format: header.compression ? "BC3" : "uncompressed",
    mipmaps: header.mipmaps > 1,
  }));
  const [first] = settings;
  if (!first || settings.some((one) => one.format !== first.format || one.mipmaps !== first.mipmaps)) return;
  format.value = first.format;
  mipmaps.value = first.mipmaps;
  if (references.length > 1) likeness.value = `Set like the ${references.length} DDS files these replace`;
  else if (reference.place.origin === VANILLA) likeness.value = `Set like the game's ${reference.place.rel}`;
  else likeness.value = `Set like ${reference.place.name} beside it`;
}

/** open shows a file in the editor. */
async function open(path: string): Promise<void> {
  const file = await loadQuery(placeQuery(path));
  if (file) editor.show(file);
}
</script>

<template>
  <UModal title="Save as DDS" :ui="{ content: 'max-w-115' }" @update:open="(open) => !open && emit('close')">
    <template #body>
      <div class="flex flex-col gap-3.5">
        <div class="flex flex-col gap-1">
          <span class="font-mono text-12 text-highlighted">{{ summary }}</span>
          <span class="text-11 text-dimmed">in {{ folder }}</span>
        </div>
        <URadioGroup v-model="format" legend="Format" variant="card" :items="FORMATS" />
        <UCheckbox
          :model-value="mipmaps"
          label="Mipmaps"
          description="Smaller copies the game draws when the picture is shown small."
          @update:model-value="(checked) => (mipmaps = checked === true)"
        />
        <span v-if="likeness" class="text-11 text-dimmed">{{ likeness }}</span>
        <UAlert v-if="error" color="error" variant="subtle" :description="error.message" />
      </div>
    </template>
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="emit('close')" />
      <UButton label="Save" color="primary" :disabled="!pictures.length" :loading="isLoading" @click="save()" />
    </template>
  </UModal>
</template>
