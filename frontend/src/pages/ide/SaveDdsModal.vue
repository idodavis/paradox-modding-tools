<!--
  Save as DDS: writes a picture, or the pictures directly in a folder, as DDS files beside them. The pictures start
  with the settings of the DDS files they replace when those agree.
-->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import * as draw from "@services/draw/service";
import type { Header } from "@internal/texture/models";
import type { Place } from "@services/workspace/models";
import { plural } from "../../lib/format";
import { ddsPicturesQuery, headerQuery, loadQuery, placeQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";

const { place } = defineProps<{ place: Place }>();
const emit = defineEmits<{ close: [] }>();

const FORMATS = [
  { value: "uncompressed", label: "Uncompressed", description: "Exact. Four bytes a pixel." },
  { value: "BC3", label: "BC3", description: "A quarter of the size. Colors blur slightly within each 4×4 block." },
];

const toast = useToast();
const editor = useEditorStore();
const format = shallowRef("uncompressed");
const mipmaps = shallowRef(false);
const likeness = shallowRef("");
const { data } = useQuery(() => ddsPicturesQuery(place));
const pictures = computed(() => data.value?.pictures ?? []);
const folder = computed(() => (place.isDir ? place.rel : place.folder));
const summary = computed(() => {
  const [first] = pictures.value;
  if (!first) return `No PNG or TGA pictures in ${folder.value}.`;
  if (pictures.value.length === 1) return `${first.name} → ${toDDS(first.name)}`;
  return `${pictures.value.length} pictures → a .dds beside each`;
});
watch(data, () => void preselect(), { immediate: true });

const { mutate: save, isLoading } = useMutation({
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

/** toSettings returns a DDS's format and whether it has mipmaps, as the dialog's choices hold them. */
function toSettings(header: Header | undefined): { format: string; mipmaps: boolean } {
  return { format: header?.compression ? "BC3" : "uncompressed", mipmaps: (header?.mipmaps ?? 1) > 1 };
}

/**
 * preselect sets the dialog's choices to the settings of the DDS files the pictures replace, when every picture has
 * one and they agree, and says so. Otherwise the choices keep their default, uncompressed without mipmaps.
 */
async function preselect(): Promise<void> {
  const references = pictures.value.flatMap((picture) => data.value?.references[picture.path] ?? []);
  const [reference] = references;
  if (!reference || references.length < pictures.value.length) return;
  const settings = await Promise.all(references.map(async (one) => toSettings(await loadQuery(headerQuery(one.path)))));
  const [first] = settings;
  if (!first || settings.some((one) => one.format !== first.format || one.mipmaps !== first.mipmaps)) return;
  format.value = first.format;
  mipmaps.value = first.mipmaps;
  if (references.length > 1) likeness.value = `Set like the ${references.length} DDS files these replace`;
  else if (reference.readOnly) likeness.value = `Set like the game's ${reference.rel}`;
  else likeness.value = `Set like ${reference.name} beside it`;
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
      </div>
    </template>
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="emit('close')" />
      <UButton label="Save" color="primary" :disabled="!pictures.length" :loading="isLoading" @click="save()" />
    </template>
  </UModal>
</template>
