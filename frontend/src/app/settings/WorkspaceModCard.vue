<!--
  This is one mod's card in a workspace's settings, where the mod's details are edited.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import { describeFinding, describePicture } from "../../lib/format";
import { codesQuery, headerQuery, listingQuery, reportQuery } from "../../queries";
import { SLOTS, useWorkspaceStore } from "../../stores/workspace";
import ConfirmModal from "../../ui/ConfirmModal.vue";
import FileSelector from "../../ui/FileSelector.vue";
import ModAvatar from "../../ui/ModAvatar.vue";
import OriginChip from "../../ui/OriginChip.vue";
import ValueField from "../../ui/ValueField.vue";
import * as app from "@services/app/service";
import * as steam from "@services/steam/service";
import * as workspace from "@services/workspace/service";
import type { Era, Mod as ModRecord } from "@services/app/models";
import type { Mod } from "@services/workspace/models";

/** RELATIONS lists the finding codes about a mod's dependencies. */
const RELATIONS = ["missing-dependency", "undeclared-dependency"];

const { workspaceId, mod } = defineProps<{ workspaceId: string; mod: Mod }>();

const store = useWorkspaceStore();
const confirm = useOverlay().create(ConfirmModal);
const newEra = shallowRef<Era>({ name: "", year: 0 });
// The Workshop item being typed, as its id or its page's address.
const item = shallowRef("");

const { data: codes } = useQuery(codesQuery);
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
  (report.value?.findings ?? []).filter((finding) => RELATIONS.includes(finding.code) && finding.origin === mod.id),
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
const { mutate: removeMod } = useMutation({ mutation: () => workspace.RemoveMod(workspaceId, mod.id) });
const { mutate: showFolder } = useMutation({ mutation: () => app.ShowFolder(mod.dir) });

/** remove asks before taking the mod out of the workspace. Its folder stays on disk. */
async function remove(): Promise<void> {
  const confirmed = await confirm.open({
    title: `Remove ${mod.descriptor.name || "this mod"}?`,
    body: "The mod leaves this workspace. Its folder stays on disk.",
    confirm: "Remove mod",
    destructive: true,
  });
  if (confirmed) removeMod();
}

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
      </div>
      <UButton label="Open folder" icon="i-lucide-folder" :disabled="mod.broken" @click="showFolder()" />
      <UButton label="Remove from workspace" variant="ghost" class="text-error" @click="remove" />
    </div>

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

        <section class="flex flex-col gap-3 border-b border-default px-5 py-4">
          <h3 class="flex items-baseline gap-2.5 text-13 font-semibold">
            Color
            <span class="text-12 font-normal text-dimmed">
              {{ store.isCustom(mod.color) ? "Picked by hand" : "A slot of the theme" }}
            </span>
          </h3>
          <div class="flex flex-wrap items-center gap-2">
            <button
              v-for="slot in SLOTS"
              :key="slot"
              type="button"
              class="size-5.5 shrink-0 rounded-md border border-accented"
              :class="{ 'ring-1 ring-primary ring-offset-2 ring-offset-(--bg-3)': mod.color === slot }"
              :style="{ background: `var(--${slot})` }"
              :aria-label="`Color ${slot}`"
              @click="update({ color: slot })"
            />
            <span class="mx-1 h-4.5 w-px shrink-0 bg-accented" />
            <ValueField
              kind="color"
              :value="store.isCustom(mod.color) ? mod.color : ''"
              label="Custom…"
              @update:value="update({ color: $event })"
            />
          </div>
        </section>

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
              @click="updateEras(eras.filter((_, at) => at !== index))"
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
              <UInput v-model="item" placeholder="The item's id or its Workshop address" class="grow font-mono" />
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
              v-for="other in finding.mods?.filter((one) => one !== mod.id) ?? []"
              :key="other"
              :origin="other"
            />
            {{ describeFinding(finding, codes) }}
          </p>
          <p v-if="!dependencies.length" class="text-12 text-dimmed">No missing or undeclared dependency.</p>
        </section>
      </div>
    </div>
  </div>
</template>
