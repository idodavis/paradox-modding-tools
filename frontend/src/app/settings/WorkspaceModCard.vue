<!-- One mod's card in Settings' Mods and load order, where its details are edited. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import { removeMod } from "../commands";
import { describePicture, joinParts } from "../../lib/format";
import { headerQuery, listingQuery, reportQuery } from "../../queries";
import { SLOTS, useWorkspaceStore } from "../../stores/workspace";
import FileSelector from "../../ui/FileSelector.vue";
import ColorPicker from "../../ui/ColorPicker.vue";
import ModAvatar from "../../ui/ModAvatar.vue";
import OriginChip from "../../ui/OriginChip.vue";
import * as app from "@services/app/service";
import * as steam from "@services/steam/service";
import * as workspace from "@services/workspace/service";
import type { Era, Mod as ModRecord } from "@services/app/models";
import type { Mod } from "@services/workspace/models";

/** SLOT_COLORS holds the swatches of a mod's color, one for each slot of the theme. */
const SLOT_COLORS = SLOTS.map((slot) => ({ value: slot, background: `var(--${slot})`, label: `Color ${slot}` }));

const { workspaceId, mod } = defineProps<{ workspaceId: string; mod: Mod }>();

const store = useWorkspaceStore();
const newEra = shallowRef<Era>({ name: "", year: 0 });
const item = shallowRef("");

const { data: listing } = useQuery(() => ({ ...listingQuery({ modId: mod.id, key: "" }), enabled: store.isOpen() }));
const { data: header } = useQuery(() => ({
  ...headerQuery(mod.thumbnail),
  enabled: store.isOpen() && !!mod.thumbnail,
}));
const { data: report } = useQuery(() => ({
  ...reportQuery(store.listModIds()),
  enabled: store.isOpen(),
}));

const eras = computed(() => mod.calendar.eras ?? []);
const months = computed(() => Array.from({ length: 12 }, (_, index) => mod.calendar.months?.[index] ?? ""));
const dependencies = computed(() =>
  (report.value?.compatibility.findings ?? []).filter(
    (finding) => finding.code === "missing-dependency" && finding.origin === mod.id,
  ),
);

const { mutate: update } = useMutation({
  mutation: (changes: Partial<ModRecord>) => store.updateMod(workspaceId, mod.id, changes),
});
const { mutate: rename } = useMutation({
  mutation: async (title: string) => listing.value && steam.WriteListing(mod.id, "", { ...listing.value, title }),
});
const { mutate: link, isLoading: linking } = useMutation({
  mutation: (text: string) => steam.LinkItem(mod.id, "", text),
  onSuccess: () => (item.value = ""),
});
const { mutate: setExternal } = useMutation({
  mutation: (external: boolean) => workspace.SetExternal(workspaceId, mod.id, external),
});
const { mutate: remove } = useMutation({ mutation: () => removeMod(workspaceId, mod.id, mod.descriptor.name) });
const { mutate: showFolder } = useMutation({ mutation: () => app.ShowFolder(mod.dir) });

/** updateEras saves the mod's calendar with a new list of eras. */
function updateEras(next: Era[]): void {
  update({ calendar: { ...mod.calendar, eras: next } });
}

/** updateEra changes one era of the calendar. */
function updateEra(index: number, changes: Partial<Era>): void {
  updateEras(eras.value.map((era, position) => (position === index ? { ...era, ...changes } : era)));
}

/** addEra adds the era being typed to the calendar and clears the row. */
function addEra(): void {
  updateEras([...eras.value, { name: newEra.value.name.trim(), year: newEra.value.year }]);
  newEra.value = { name: "", year: 0 };
}

/** updateMonth renames one month of the calendar. If all twelve are blank, it saves none. */
function updateMonth(index: number, text: string): void {
  const next = months.value.map((month, position) => (position === index ? text : month));
  update({ calendar: { ...mod.calendar, months: next.some(Boolean) ? next : [] } });
}
</script>

<template>
  <div class="@container flex min-w-0 flex-1 flex-col overflow-y-auto rounded-lg border border-default bg-muted">
    <div class="flex flex-wrap items-center gap-3.5 border-b border-default px-5 py-4">
      <ModAvatar :mod="mod" size="xl" />
      <div class="flex min-w-48 grow basis-0 flex-col gap-0.5">
        <span class="truncate text-16">{{ mod.descriptor.name }}</span>
        <span class="truncate font-mono text-12 text-dimmed">{{ mod.dir }}</span>
        <span v-if="mod.broken" class="text-12 text-error">Its folder was not found. Pick where it is now.</span>
        <span v-if="mod.workshop" class="text-12 text-dimmed">External mod · Steam's copy, read only</span>
      </div>
      <UButton label="Open folder" icon="i-lucide-folder" :disabled="mod.broken" @click="showFolder()" />
      <UButton label="Remove from workspace" variant="ghost" class="text-error" @click="remove()" />
    </div>

    <section class="border-b border-default px-5 py-4">
      <UFormField
        label="External mod"
        description="Someone else's mod, which your mods are checked against. PMT never publishes it, and Health keeps its problems out of your totals."
        orientation="horizontal"
        :ui="{ root: 'px-0' }"
      >
        <USwitch :model-value="mod.external" :disabled="mod.workshop" @update:model-value="setExternal" />
      </UFormField>
    </section>

    <div class="grid grid-cols-1 @2xl:grid-cols-2">
      <div class="flex flex-col border-b border-default @2xl:border-e @2xl:border-b-0">
        <section class="flex flex-col gap-3 border-b border-default px-5 py-4">
          <h3 class="text-13 font-semibold">Identity</h3>
          <UFormField label="Name">
            <UInput
              :model-value="listing?.title"
              :model-modifiers="{ lazy: true }"
              :disabled="!listing"
              class="w-full"
              @update:model-value="rename(`${$event}`)"
            />
          </UFormField>
          <UFormField label="Folder">
            <FileSelector :path="mod.dir" :filter="[]" folder @update:path="update({ dir: $event })" />
          </UFormField>
        </section>

        <ColorPicker
          :model-value="mod.color"
          :items="SLOT_COLORS"
          note="The mod's color in the explorer, tabs and Health."
          class="border-b border-default px-5 py-4"
          @update:model-value="update({ color: $event })"
        />

        <section class="flex flex-col gap-3 border-b border-default px-5 py-4 @2xl:border-b-0">
          <h3 class="flex items-baseline gap-2.5 text-13 font-semibold">
            Thumbnail
            <span class="text-12 font-normal text-dimmed">Shown in the launcher, on Steam and in PMT</span>
          </h3>
          <div class="flex items-end gap-3.5">
            <ModAvatar :mod="mod" size="3xl" />
            <div class="flex min-w-0 flex-col items-start gap-1.5">
              <span class="truncate font-mono text-12 text-toned">{{ mod.thumbnail || "No thumbnail" }}</span>
              <span v-if="header" class="text-12 text-dimmed">{{ describePicture(header) }}</span>
              <UButton label="Change in Publish" :to="{ name: 'release', params: { id: workspaceId } }" />
            </div>
          </div>
        </section>
      </div>

      <div class="flex flex-col">
        <section class="flex flex-col gap-3 border-b border-default px-5 py-4">
          <h3 class="flex items-baseline gap-2.5 text-13 font-semibold">
            Calendar
            <span class="text-12 font-normal text-dimmed">Shows event dates as in-game years</span>
          </h3>
          <div v-for="(era, index) in eras" :key="index" class="flex items-end gap-2.5">
            <UFormField label="Era" class="w-30">
              <UInput
                :model-value="era.name"
                :model-modifiers="{ lazy: true }"
                @update:model-value="updateEra(index, { name: `${$event}` })"
              />
            </UFormField>
            <UFormField label="Starts in year" class="w-30">
              <UInput
                type="number"
                :model-value="era.year"
                :model-modifiers="{ lazy: true }"
                @update:model-value="updateEra(index, { year: Number($event) })"
              />
            </UFormField>
            <UButton
              icon="i-lucide-x"
              color="error"
              variant="ghost"
              square
              aria-label="Remove this era"
              @click="updateEras(eras.filter((_, position) => position !== index))"
            />
          </div>
          <div class="flex items-end gap-2.5">
            <UFormField label="Era" class="w-30">
              <UInput v-model="newEra.name" placeholder="AD" />
            </UFormField>
            <UFormField label="Starts in year" class="w-30">
              <UInput v-model.number="newEra.year" type="number" />
            </UFormField>
            <UButton label="Add era" icon="i-lucide-plus" :disabled="!newEra.name.trim()" @click="addEra" />
          </div>
          <div class="grid grid-cols-3 gap-2">
            <UInput
              v-for="(month, index) in months"
              :key="index"
              :model-value="month"
              :model-modifiers="{ lazy: true }"
              :placeholder="`Month ${index + 1}`"
              @update:model-value="updateMonth(index, `${$event}`)"
            />
          </div>
        </section>

        <section class="flex flex-col gap-3 border-b border-default px-5 py-4">
          <div class="flex items-baseline gap-2.5">
            <h3 class="text-13 font-semibold">Publishing</h3>
            <UButton
              label="Open Publish"
              variant="link"
              color="secondary"
              class="ms-auto h-auto p-0 text-12"
              :to="{ name: 'release', params: { id: workspaceId } }"
            />
          </div>
          <UFormField label="Workshop item">
            <div v-if="listing?.workshopId" class="flex gap-2">
              <UInput :model-value="listing.workshopId" readonly class="grow font-mono" />
              <UButton label="Unlink" color="error" variant="ghost" :loading="linking" @click="link('')" />
            </div>
            <form v-else class="flex gap-2" @submit.prevent="link(item)">
              <UInput v-model="item" placeholder="The item's id or its address" class="grow font-mono" />
              <UButton type="submit" label="Link" :disabled="!listing || !item.trim()" :loading="linking" />
            </form>
          </UFormField>
        </section>

        <section class="flex flex-col gap-2 px-5 py-4">
          <div class="flex items-baseline gap-2.5">
            <h3 class="text-13 font-semibold">Dependencies</h3>
            <UButton
              label="Open in Health"
              variant="link"
              color="secondary"
              class="ms-auto h-auto p-0 text-12"
              :to="{ name: 'health', params: { id: workspaceId } }"
            />
          </div>
          <p
            v-for="finding in dependencies"
            :key="finding.code + finding.name"
            class="flex items-center gap-2 text-12 text-toned"
          >
            <OriginChip
              v-for="other in finding.mods?.filter((modId) => modId !== mod.id) ?? []"
              :key="other"
              :origin="other"
            />
            {{ joinParts(finding.message) }}
          </p>
          <p v-if="!dependencies.length" class="text-12 text-dimmed">No missing dependency.</p>
        </section>
      </div>
    </div>
  </div>
</template>
