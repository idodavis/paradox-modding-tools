<!--
  Save as DDS: writes a picture, or the pictures directly in a folder, as DDS
  files beside them.
-->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";

import * as draw from "@services/draw/service";
import type { Place } from "@services/workspace/models";
import { plural } from "../../lib/format";
import { ddsPicturesQuery, loadQuery, placeQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore, VANILLA } from "../../stores/workspace";

const { place } = defineProps<{ place: Place }>();
const emit = defineEmits<{ close: [] }>();

const toast = useToast();
const cache = useQueryCache();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
// The format's DXGI number, 0 until the pictures are listed.
const format = shallowRef(0);
const mipmaps = shallowRef(false);
const { data } = useQuery(() => ddsPicturesQuery(place.path));
const pictures = computed(() => data.value?.pictures ?? []);
// A mod's root folder has no path inside the mod, so the mod's name labels it.
const folder = computed(() => (place.isDir ? place.rel : place.folder) || workspace.originName(place.origin));
const summary = computed(() => {
  const [first] = pictures.value;
  if (!first) return "No PNG or TGA pictures";
  if (pictures.value.length === 1) return `${first.name} → ${data.value?.names?.[0]}`;
  return `${pictures.value.length} pictures → a .dds beside each`;
});
// Says which DDS files the settings were taken from, or nothing if from none.
const likeness = computed(() => {
  const matched = data.value?.matched ?? 0;
  const reference = data.value?.references?.[pictures.value[0]?.path ?? ""];
  if (matched > 1) return `Set like the ${matched} DDS files these replace`;
  if (!matched || !reference) return "";
  if (reference.place.origin === VANILLA) return `Set like the game's ${reference.place.rel}`;
  return `Set like ${reference.place.name} beside it`;
});
// The choices start as the settings Go gives, and follow the replaced DDS
// files' settings when the pictures are listed again.
watch(
  data,
  (next) => {
    if (!next || (!next.matched && format.value)) return;
    format.value = next.format;
    mipmaps.value = next.mipmaps;
  },
  { immediate: true },
);

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
    // The pictures now have a DDS beside them, whose settings the next open
    // starts with.
    void cache.invalidateQueries({ key: ddsPicturesQuery(place.path).key });
    const [name] = data.value?.names ?? [];
    const title = pictures.value.length === 1 && name ? name : plural(pictures.value.length, "DDS file");
    toast.add({
      title: `Saved ${title}`,
      color: "success",
      actions: [{ label: "Open", onClick: () => void open(paths?.[0] ?? "") }],
    });
    emit("close");
  },
});

/**
 * open shows the file at the given path in the editor. A path with no place
 * does nothing.
 */
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
        <URadioGroup v-model="format" legend="Format" variant="card" value-key="dxgi" :items="data?.formats ?? []" />
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
