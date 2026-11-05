<!-- The dialog that lists the Workshop mods Steam has downloaded and hands back the ones checked. -->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";

import { matchQuery, plural } from "../lib/format";
import { workshopModsQuery } from "../queries";
import ModAvatar from "./ModAvatar.vue";
import type { Mod } from "@services/workspace/models";

const { installId, taken } = defineProps<{ installId: string; taken: string[] }>();
const open = defineModel<boolean>("open", { required: true });
const emit = defineEmits<{ add: [mods: Mod[]] }>();

/** ROW_UI lays a row out as its box, the mod's picture, and its name over its version, centered on one line. */
const ROW_UI = {
  fieldset: "gap-y-px",
  item: "items-center rounded-md px-2 py-1 hover:bg-accented",
  wrapper: "flex min-w-0 grow items-center",
  label: "flex min-w-0 grow items-center gap-2.5",
};

const search = shallowRef("");
const picked = shallowRef<string[]>([]);

const { data: mods, isLoading } = useQuery(() => ({
  ...workshopModsQuery(installId),
  enabled: open.value && !!installId,
}));

const items = computed(() =>
  (mods.value ?? [])
    .filter((mod) => matchQuery(search.value, mod.descriptor.name))
    .map((mod) => ({
      value: mod.dir,
      label: mod.descriptor.name || mod.dir,
      detail: taken.includes(mod.dir) ? "Already in the workspace" : mod.descriptor.version,
      disabled: taken.includes(mod.dir),
      mod,
    })),
);
// The mods already in the workspace show checked, and the choice holds only the others.
const empty = computed(() => {
  if (isLoading.value) return "Reading the downloads.";
  return mods.value?.length ? "No mod matches." : "Steam has downloaded no Workshop mod for this game.";
});
const checked = computed({
  get: () => [...picked.value, ...taken],
  set: (dirs) => (picked.value = dirs.filter((dir) => !taken.includes(dir))),
});

watch(open, (opened) => {
  if (!opened) return;
  search.value = "";
  picked.value = [];
});

/** add hands back the checked mods and closes the dialog. */
function add(): void {
  emit(
    "add",
    (mods.value ?? []).filter((mod) => picked.value.includes(mod.dir)),
  );
  open.value = false;
}
</script>

<template>
  <UModal
    v-model:open="open"
    title="Your downloaded Workshop mods"
    description="Each is added as an external mod. PMT never publishes it."
    :ui="{ content: 'sm:max-w-md' }"
  >
    <template #body>
      <div class="flex flex-col gap-2.5">
        <UInput
          v-model="search"
          icon="i-lucide-search"
          :placeholder="`Filter ${mods?.length ?? 0} mods`"
          :disabled="!mods?.length"
          class="w-full"
        />
        <div class="-mx-2 max-h-96 overflow-y-auto">
          <UCheckboxGroup v-model="checked" :items="items" :ui="ROW_UI">
            <template #label="{ item }">
              <ModAvatar :mod="item.mod" size="sm" />
              <span class="flex min-w-0 flex-col">
                <span class="truncate text-13 text-highlighted">{{ item.label }}</span>
                <span class="truncate text-12 text-dimmed">{{ item.detail }}</span>
              </span>
            </template>
          </UCheckboxGroup>
          <p v-if="!items.length" class="px-2 text-13 text-dimmed">{{ empty }}</p>
        </div>
      </div>
    </template>
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="open = false" />
      <UButton
        :label="picked.length ? `Add ${plural(picked.length, 'external mod')}` : 'Add'"
        color="primary"
        variant="solid"
        :disabled="!picked.length"
        @click="add"
      />
    </template>
  </UModal>
</template>
