<script lang="ts">
/**
 * One workspace mod row: load-order header, expandable fields, nested ignore.
 */
import type { WorkspaceMod } from "@services/models";
import type { Era } from "@services/internal/session/models";

/** Field help used by this card and the settings filter. */
export const MOD_HELP = {
  thumb: "Explorer folder icon and origin filters. Not always used on Origin/Mod Badges.",
  descMd: "Markdown listing description. Empty uses mod-description.md in the mod folder. The file must already exist.",
  descBb: "BBCode sent to Steam. Empty uses mod-description.bbcode. The file must already exist.",
  calendar:
    "Total conversions offset the year so the engine's date arithmetic still works: A Game of Thrones writes 8082.1.1 for 82 A.C. and has five eras. Add a row per era; a date reads in the era with the greatest offset below its year, as the year minus that offset. Nothing is guessed, and with no era every date reads as written. Never changes what is saved to disk.",
  workshopIgnore:
    "Gitignore syntax, stored in this workspace. Steam never uploads: dotfiles (except .metadata/), .gitignore, and patterns in a .gitignore in the mod folder. Export writes .workshop-ignore; Import reads it. Editing that file in the IDE does nothing until Import.",
} as const;

/** Fields the parent persists via UpdateWorkspaceMod. */
export type ModPatch = Partial<
  Pick<WorkspaceMod, "name" | "color" | "thumbnail" | "descMdRel" | "descBbRel" | "workshopIgnore">
>;
</script>

<script setup lang="ts">
import { computed, shallowRef } from "vue";
import FileSelector from "./FileSelector.vue";
import FieldHelpTip from "./FieldHelpTip.vue";
import OriginColorField from "./OriginColorField.vue";
import { modColor } from "../stores/workspace";

const FILE_FIELDS = [
  {
    key: "thumbnail",
    label: "Thumbnail",
    help: MOD_HELP.thumb,
    dialog: "Select thumbnail",
    filter: "*.png; *.jpg; *.jpeg; *.svg",
  },
  {
    key: "descMdRel",
    label: "Markdown file",
    help: MOD_HELP.descMd,
    dialog: "Select description Markdown",
    filter: "*.md; *.markdown; *.txt",
  },
  {
    key: "descBbRel",
    label: "BBCode file",
    help: MOD_HELP.descBb,
    dialog: "Select description BBCode",
    filter: "*.bbcode; *.txt",
  },
] as const;

const props = defineProps<{
  mod: WorkspaceMod;
  index: number;
  expanded: boolean;
  thumbUrl?: string;
}>();

const emit = defineEmits<{
  "update:expanded": [open: boolean];
  patch: [patch: ModPatch];
  /** Persisted by SetWorkspaceModSlot, which also clears a picked color. */
  slot: [slot: number];
  /** Persisted by SetWorkspaceModCalendar, not UpdateWorkspaceMod. */
  calendar: [eras: Era[]];
  importIgnore: [];
  exportIgnore: [];
  remove: [];
}>();

const ignoreOpen = shallowRef(false);
const deriveOpen = shallowRef(false);
const exWritten = shallowRef("");
const exSeen = shallowRef("");
const exEra = shallowRef("");

/** The mod's eras; each change sends the whole list. */
const eras = computed(() => props.mod.eras ?? []);
function setEra(i: number, patch: Partial<Era>): void {
  emit("calendar", eras.value.map((era, at) => (at === i ? { ...era, ...patch } : era)));
}

/**
 * The epoch offset from an example: a year the mod's script writes and the year a player sees
 * (AGoT writes 8082 for 82, so 8000). Permissive, since a calendar only changes what a hover
 * reads: a bare year serves, a negative offset is kept, and only unparseable input is null.
 */
function offsetFromExample(written: string, seen: string): number | null {
  const yearOf = (raw: string): number | null => {
    const head = raw.trim().split(".")[0];
    if (head === undefined || head === "" || head === "-") return null;
    const n = Number(head);
    return Number.isFinite(n) ? Math.trunc(n) : null;
  };
  const w = yearOf(written);
  const s = yearOf(seen);
  if (w === null || s === null) return null;
  return w - s;
}

const derivedOffset = computed(() => offsetFromExample(exWritten.value, exSeen.value));

</script>

<template>
  <div class="flex overflow-hidden rounded border border-default">
    <div
      class="flex shrink-0 flex-col items-center gap-1 self-stretch border-e border-default bg-elevated/50 px-1 py-1.5"
    >
      <UBadge
        :label="String((mod.sortOrder ?? index) + 1)"
        color="neutral"
        variant="subtle"
        size="xs"
        class="w-6 justify-center tabular-nums"
      />
      <button
        type="button"
        class="mod-handle inline-flex size-8 cursor-grab items-center justify-center text-muted hover:text-default active:cursor-grabbing"
        aria-label="Drag to reorder"
        title="Drag to reorder"
      >
        <UIcon name="i-lucide-grip-vertical" class="size-4" />
      </button>
    </div>

    <div class="min-w-0 flex-1">
      <div class="flex items-center gap-2 px-2 py-1.5">
        <span
          class="size-5 shrink-0 rounded border border-default"
          :style="{ backgroundColor: modColor(mod) }"
        />
        <div class="min-w-0 flex-1">
          <UInput
            :model-value="mod.name"
            size="sm"
            class="w-full"
            @update:model-value="emit('patch', { name: String($event ?? '') })"
          />
          <p class="truncate text-xs text-muted">{{ mod.path }}</p>
        </div>
        <UBadge v-if="mod.isBroken" color="error" variant="subtle" size="xs"> Missing </UBadge>
        <UTooltip text="Remove mod">
          <UButton icon="i-lucide-trash-2" color="error" variant="ghost" size="xs" square @click="emit('remove')" />
        </UTooltip>
        <UTooltip :text="expanded ? 'Collapse settings' : 'Expand settings'">
          <UButton
            :icon="expanded ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
            color="neutral"
            variant="ghost"
            size="xs"
            square
            @click="emit('update:expanded', !expanded)"
          />
        </UTooltip>
      </div>

      <div v-show="expanded" class="space-y-3 border-t border-default px-2 py-2">
        <div class="grid grid-cols-1 gap-x-6 gap-y-3 md:grid-cols-2">
          <OriginColorField
            :model-value="mod.color ?? ''"
            :slot="mod.slot"
            compact
            @update:model-value="emit('patch', { color: $event })"
            @slot="emit('slot', $event)"
          />
          <UFormField v-for="f in FILE_FIELDS" :key="f.key" size="sm">
            <template #label>
              <FieldHelpTip :label="f.label" :text="f.help" />
            </template>
            <div class="flex items-center gap-2">
              <img
                v-if="f.key === 'thumbnail' && mod.thumbnail && thumbUrl"
                :src="thumbUrl"
                alt=""
                class="size-6 rounded-sm object-cover"
              />
              <FileSelector
                :model-value="mod[f.key] ?? ''"
                mode="file"
                :dialog-title="f.dialog"
                :file-filter="f.filter"
                class="min-w-0 flex-1"
                @update:model-value="emit('patch', { [f.key]: $event })"
              />
              <UButton
                v-if="mod[f.key]"
                label="Clear"
                size="xs"
                variant="ghost"
                @click="emit('patch', { [f.key]: '' })"
              />
            </div>
          </UFormField>
        </div>

        <div class="space-y-2 border-t border-default pt-3">
          <div class="flex items-center gap-1">
            <span class="text-sm font-medium">Calendar</span>
            <FieldHelpTip :text="MOD_HELP.calendar" />
          </div>
          <div v-for="(era, i) in eras" :key="i" class="flex flex-wrap items-end gap-3">
            <UFormField :label="i === 0 ? 'Era' : undefined" class="w-24">
              <UInput :model-value="era.name" placeholder="A.C." size="sm" @update:model-value="setEra(i, { name: String($event ?? '') })" />
            </UFormField>
            <UFormField :label="i === 0 ? 'Year offset' : undefined" class="w-32">
              <UInput type="number" :model-value="era.offset" size="sm" @update:model-value="setEra(i, { offset: Number($event) || 0 })" />
            </UFormField>
            <UButton
              icon="i-lucide-trash-2"
              color="neutral"
              variant="ghost"
              size="xs"
              @click="emit('calendar', eras.filter((_, at) => at !== i))"
            />
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <UButton label="Add era" icon="i-lucide-plus" color="neutral" variant="outline" size="xs" @click="emit('calendar', [...eras, { name: '', offset: 0 }])" />
            <UButton
              :label="deriveOpen ? 'Hide helper' : 'Work it out'"
              color="neutral"
              variant="outline"
              size="xs"
              @click="deriveOpen = !deriveOpen"
            />
            <UButton v-if="eras.length" label="Clear" color="neutral" variant="ghost" size="xs" @click="emit('calendar', [])" />
          </div>
          <div v-if="deriveOpen" class="flex flex-wrap items-end gap-3 rounded-md bg-elevated/50 p-3">
            <UFormField label="Date as written" class="w-36">
              <UInput v-model="exWritten" placeholder="8082.1.1" size="sm" />
            </UFormField>
            <UFormField label="Year players see" class="w-32">
              <UInput v-model="exSeen" placeholder="82" size="sm" />
            </UFormField>
            <UFormField label="Era" class="w-24">
              <UInput v-model="exEra" placeholder="A.C." size="sm" />
            </UFormField>
            <UButton
              label="Add this era"
              size="xs"
              :disabled="derivedOffset === null || !exEra.trim()"
              @click="emit('calendar', [...eras, { name: exEra.trim(), offset: derivedOffset ?? 0 }])"
            />
            <p v-if="exWritten && exSeen && derivedOffset === null" class="text-xs text-error">
              Enter a date like 8082.1.1 and the year it should read.
            </p>
          </div>
        </div>

        <div class="space-y-2">
          <div class="flex flex-wrap items-center gap-1">
            <UButton
              label="Workshop ignore"
              color="neutral"
              variant="ghost"
              size="xs"
              :trailing-icon="ignoreOpen ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
              @click="ignoreOpen = !ignoreOpen"
            />
            <FieldHelpTip :text="MOD_HELP.workshopIgnore" />
            <div class="ms-auto flex flex-wrap gap-2">
              <UButton
                label="Import .workshop-ignore"
                icon="i-lucide-download"
                size="xs"
                variant="outline"
                @click="emit('importIgnore')"
              />
              <UButton
                label="Export .workshop-ignore"
                icon="i-lucide-upload"
                size="xs"
                variant="outline"
                @click="emit('exportIgnore')"
              />
            </div>
          </div>
          <UTextarea
            v-show="ignoreOpen"
            :model-value="mod.workshopIgnore ?? ''"
            :rows="3"
            class="w-full font-mono text-sm"
            placeholder="# e.g. docs/&#10;*.psd"
            @update:model-value="emit('patch', { workshopIgnore: String($event ?? '') })"
          />
        </div>
      </div>
    </div>
  </div>
</template>
