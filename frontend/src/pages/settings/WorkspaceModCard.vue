<!-- One mod's card in Settings: its folder, thumbnail, color, calendar, Workshop item and upload exclusions. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import { diagnosticMessage } from "../../lib/format";
import { codesQuery, listingQuery, reportQuery, rulesQuery, stateQuery, workspacesQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
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

/** The color slots a mod picks from, in the order the theme defines them. */
const SLOTS = Array.from({ length: 16 }, (_, index) => `o${index + 1}`);

const { workspaceId, mod } = defineProps<{ workspaceId: string; mod: Mod }>();

const store = useWorkspaceStore();
const confirm = useOverlay().create(ConfirmModal);
const newEra = shallowRef<Era>({ name: "", year: 0 });
let lastUpdate: Promise<void> | undefined;

const { data: state } = useQuery(stateQuery);
const { data: workspaces } = useQuery(workspacesQuery);
const { data: codes } = useQuery(codesQuery);
const { data: rules } = useQuery(rulesQuery);
const { data: listing } = useQuery(() => ({ ...listingQuery(mod.id), enabled: store.isOpen() }));
const { data: report } = useQuery(() => ({
  ...reportQuery((state.value?.mods ?? []).map((one) => one.id)),
  enabled: store.isOpen(),
}));

const eras = computed(() => mod.calendar.eras ?? []);
const months = computed(() => Array.from({ length: 12 }, (_, index) => mod.calendar.months?.[index] ?? ""));
const dependencies = computed(() =>
  (report.value?.findings ?? []).filter((finding) => finding.section === "dependencies" && finding.origin === mod.id),
);

const { mutate: update } = useMutation({
  mutation: (changes: Partial<ModRecord>) => {
    const latest = workspaces.value?.find((row) => row.id === workspaceId)?.mods?.find((row) => row.id === mod.id);
    lastUpdate = workspace.UpdateMod(workspaceId, { ...(latest ?? mod), ...changes });
    return lastUpdate;
  },
});
const { mutate: removeMod } = useMutation({ mutation: () => workspace.RemoveMod(workspaceId, mod.id) });
const { mutate: showFolder } = useMutation({ mutation: () => app.ShowFolder(mod.dir) });
const { mutate: importIgnore } = useMutation({
  mutation: () => steam.ReadIgnore(mod.id),
  onSuccess: (text) => text && update({ ignore: text }),
});
const { mutate: exportIgnore } = useMutation({
  mutation: async () => {
    await lastUpdate?.catch(() => undefined);
    await steam.WriteIgnore(mod.id);
  },
});

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

/** updateEras saves the given eras as the mod's calendar. */
function updateEras(next: Era[]): void {
  update({ calendar: { ...mod.calendar, eras: next } });
}

/** updateEra saves the given changes to one era of the calendar. */
function updateEra(index: number, changes: Partial<Era>): void {
  updateEras(eras.value.map((era, position) => (position === index ? { ...era, ...changes } : era)));
}

/** addEra saves the era being typed as a new one and clears the row. */
function addEra(): void {
  updateEras([...eras.value, { name: newEra.value.name.trim(), year: newEra.value.year }]);
  newEra.value = { name: "", year: 0 };
}

/** updateMonth saves the given name for one month. Twelve blank names save as none, so the game's names show. */
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
          <h3 class="text-13 font-semibold">Folder</h3>
          <FileSelector :path="mod.dir" :filter="[]" folder @update:path="update({ dir: $event })" />
        </section>

        <section class="flex flex-col gap-3 border-b border-default px-5 py-4">
          <h3 class="flex items-baseline gap-2.5 text-13 font-semibold">
            Color
            <span class="text-12 font-normal text-dimmed">
              {{ store.isCustom(mod.id) ? "Picked by hand" : "A slot of the theme" }}
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
              :value="store.isCustom(mod.id) ? mod.color : ''"
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
          <div class="flex gap-2">
            <UInput :model-value="mod.workshopId" readonly placeholder="Not published" class="grow font-mono" />
            <UButton label="Unlink" variant="ghost" :disabled="!mod.workshopId" @click="update({ workshopId: '' })" />
          </div>
          <p v-if="listing?.descriptionPath" class="text-12 text-dimmed">
            The description comes from {{ listing.descriptionPath }}.
          </p>
        </section>

        <section class="flex flex-col gap-2 border-b border-default px-5 py-4">
          <div class="flex items-baseline gap-2.5">
            <h3 class="text-13 font-semibold">Used by and uses</h3>
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
            <OriginChip v-if="finding.sites?.[1]" :origin="finding.sites[1].place.origin" />
            {{ diagnosticMessage(codes?.find((code) => code.id === finding.code)?.message ?? "", finding.facts ?? {}) }}
          </p>
          <p v-if="!dependencies.length" class="text-12 text-dimmed">No other mod reads this one.</p>
        </section>

        <section class="flex flex-col gap-3 px-5 py-4">
          <h3 class="flex items-baseline gap-2.5 text-13 font-semibold">
            Upload exclusions
            <span class="text-12 font-normal text-dimmed">Gitignore patterns, one per line</span>
          </h3>
          <UTextarea
            :model-value="mod.ignore"
            :model-modifiers="{ lazy: true }"
            :rows="6"
            placeholder="gfx/source/**"
            class="w-full font-mono"
            @update:model-value="update({ ignore: `${$event}` })"
          />
          <div class="flex flex-col gap-1">
            <span class="text-12 font-medium text-toned">Every upload leaves out</span>
            <p v-for="rule in rules ?? []" :key="rule.pattern" class="text-12 text-dimmed">
              <span class="font-mono text-toned">{{ rule.pattern }}</span> {{ rule.reason }}
            </p>
          </div>
          <div class="flex gap-1">
            <UButton
              label="Import .workshop-ignore"
              icon="i-lucide-file-down"
              variant="ghost"
              size="sm"
              @click="importIgnore()"
            />
            <UButton
              label="Export .workshop-ignore"
              icon="i-lucide-file-up"
              variant="ghost"
              size="sm"
              @click="exportIgnore()"
            />
          </div>
        </section>
      </div>
    </div>
  </div>
</template>
