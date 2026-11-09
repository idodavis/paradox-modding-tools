<!-- Save as DDS: writes a picture, or the pictures directly in a folder, as DDS files beside them. -->
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

/**
 * UNCOMPRESSED is B8G8R8A8's DXGI number: the default, and the format kept
 * for a DDS that names none.
 */
const UNCOMPRESSED = 87;
const FORMATS = [
  { value: UNCOMPRESSED, label: "B8G8R8A8", description: "Uncompressed and exact. Four bytes a pixel." },
  { value: 71, label: "BC1", description: "An eighth of the size. Each pixel is opaque or clear." },
  { value: 77, label: "BC3", description: "A quarter of the size, with full alpha." },
  { value: 98, label: "BC7", description: "A quarter of the size, and much closer to the picture than BC3." },
];

const toast = useToast();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const format = shallowRef(UNCOMPRESSED);
const mipmaps = shallowRef(false);
const likeness = shallowRef("");
const { data } = useQuery(() => ddsPicturesQuery(place.path));
const pictures = computed(() => data.value?.pictures ?? []);
const references = computed(() => pictures.value.flatMap((picture) => data.value?.references?.[picture.path] ?? []));
// A DDS in a format the rows do not offer adds a row of its own, named by
// that format.
const items = computed(() => {
  const others = new Map<number, string>();
  for (const { header } of references.value) {
    if (header.dxgi && !FORMATS.some((row) => row.value === header.dxgi)) others.set(header.dxgi, header.pixelFormat);
  }
  const rows = Array.from(others, ([value, label]) => ({
    value,
    label,
    description: "The format of the DDS it replaces.",
  }));
  return [...FORMATS, ...rows];
});
// A mod's root folder has no path inside the mod, so the mod's name labels it.
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
      format.value,
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
 * preselect sets the choices to the settings of the DDS files the pictures
 * replace, when every picture has one and they all agree. A DDS that names no
 * format keeps B8G8R8A8. Otherwise the choices keep their defaults.
 */
function preselect(): void {
  const [reference] = references.value;
  if (!reference || references.value.length < pictures.value.length) return;
  const settings = references.value.map(({ header }) => ({
    format: header.dxgi || UNCOMPRESSED,
    mipmaps: header.mipmaps > 1,
  }));
  const [first] = settings;
  if (!first || settings.some((other) => other.format !== first.format || other.mipmaps !== first.mipmaps)) return;
  format.value = first.format;
  mipmaps.value = first.mipmaps;
  if (references.value.length > 1) likeness.value = `Set like the ${references.value.length} DDS files these replace`;
  else if (reference.place.origin === VANILLA) likeness.value = `Set like the game's ${reference.place.rel}`;
  else likeness.value = `Set like ${reference.place.name} beside it`;
}

/** open shows the file at the given path in the editor. A path with no place does nothing. */
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
        <URadioGroup v-model="format" legend="Format" variant="card" :items="items" />
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
