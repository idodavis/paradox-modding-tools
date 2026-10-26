<!--
  This rail edits the next update of a listing, shows what it changes on Steam and uploads it.
-->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { countBy } from "es-toolkit";

import {
  bytes,
  day,
  describeAge,
  describeCount,
  describeDifference,
  MARKS,
  parseTime,
  plural,
  toSentence,
  type DifferenceRow,
} from "../../lib/format";
import { changeNotesQuery, comparisonQuery, stateQuery, tagsQuery, uploadQuery, useComparison } from "../../queries";
import { useUploadStore } from "../../stores/uploads";
import { useWorkspaceStore } from "../../stores/workspace";
import UploadAction from "./UploadAction.vue";
import type { Difference, Listing } from "@services/steam/models";

/** Row is one line of the comparison box. A line with no id cannot be picked. */
type Row = Partial<DifferenceRow> & { id: string; summary: string; extra?: string; lit: boolean };

/** RELEASE matches the major, minor and hotfix numbers a version starts with. The last two are optional. */
const RELEASE = /^(\d+)(?:\.(\d+))?(?:\.(\d+))?/;

const { modId, listingKey, listing, comparing, picked } = defineProps<{
  modId: string;
  listingKey: string;
  listing: Listing;
  comparing: boolean;
  /** The id of the difference the compare shows, or "" when there is none. */
  picked: string;
}>();
const emit = defineEmits<{ "update:listing": [listing: Listing]; compare: [id: string]; link: [] }>();

const workspace = useWorkspaceStore();
const uploads = useUploadStore();

const enabled = computed(() => workspace.isOpen());
const { data: state } = useQuery(stateQuery);
const { data: tags } = useQuery(() => ({ ...tagsQuery, enabled: enabled.value }));
const { data: upload } = useQuery(() => ({ ...uploadQuery({ modId, key: listingKey }), enabled: enabled.value }));
const { data: notes } = useQuery(() => ({ ...changeNotesQuery({ modId, key: listingKey }), enabled: enabled.value }));
const { data: comparison, error: unreachable } = useQuery(() => ({
  ...comparisonQuery({ modId, key: listingKey }),
  enabled: enabled.value,
}));
const { mutate: check, isLoading: checking } = useComparison();

const older = computed(() => listingKey !== "");
// The version stays as it is for an older listing and while an upload runs.
const locked = computed(() => older.value || uploads.find(modId, listingKey)?.status === "running");
const bumps = computed(() => listBumps(listing.version));
// The game's major and minor version when Steam lists no tag for it, else "".
const untaggedVersion = computed(() => {
  if (listing.versionTag || !tags.value?.versions?.length || !listing.supportedVersion) return "";
  return listing.supportedVersion.split(".").slice(0, 2).join(".");
});
// The draft the change note was filled from, if it is unedited.
const filledNote = computed(() =>
  listing.changelog ? notes.value?.find((note) => note.text === listing.changelog) : undefined,
);
const hint = computed(() => {
  const note = filledNote.value;
  if (!note) return "";
  if (note.source !== "git") return `Taken from ${note.source}. Edit it before you upload.`;
  const since = parseTime(note.since) ? `since your last upload, ${day(note.since)}` : "since the start";
  return `Drafted from the ${plural(note.count, "commit")} ${since}. Edit it before you upload.`;
});

// The file differences fold into one summary row.
const groups = computed<{ label: string; rows: Row[] }[]>(() => {
  const files = comparison.value?.files ?? [];
  const changes = files.filter((difference) => difference.field === "file");
  const counted = countBy(changes, (difference) => difference.status);
  const summary = Object.entries(MARKS)
    .filter(([status]) => counted[status])
    .map(([status, { count }]) => `${describeCount(counted[status] ?? 0)} ${count}`);
  const total = upload.value?.files?.length
    ? `${describeCount(upload.value.files.length)} · ${bytes(upload.value.bytes)}`
    : "";
  const toRow = (difference: Difference): Row => {
    const row = describeDifference(difference);
    return { ...row, lit: comparing && row.id === picked };
  };
  return [
    { label: "Page", rows: (comparison.value?.page ?? []).map(toRow) },
    {
      label: "Files",
      rows: [
        ...files.filter((difference) => difference.field === "version").map(toRow),
        {
          id: changes[0] ? describeDifference(changes[0]).id : "",
          summary: summary.join(", ") || "Files are the same as Steam's",
          extra: total,
          lit: comparing && picked.startsWith("file:"),
        },
      ],
    },
  ].filter(({ rows }) => rows.length);
});

/** change emits the listing with some of its fields changed. */
function change(changes: Partial<Listing>): void {
  emit("update:listing", { ...listing, ...changes });
}

/** listBumps returns the next minor and the next hotfix version of a version. It returns none if it has no number. */
function listBumps(version: string): string[] {
  const match = RELEASE.exec(version);
  if (!match) return [];
  const [major = 0, minor = 0, hotfix = 0] = match.slice(1).map((part) => Number(part ?? 0));
  return [`${major}.${minor + 1}.0`, `${major}.${minor}.${hotfix + 1}`];
}
</script>

<template>
  <aside class="flex h-full w-86 shrink-0 flex-col border-s border-default bg-muted">
    <div class="flex min-h-0 grow flex-col gap-3.5 overflow-y-auto px-4.5 py-4">
      <span class="text-13 font-semibold text-highlighted">This update</span>

      <div class="flex items-start gap-3">
        <div class="flex w-37 shrink-0 flex-col gap-1.5">
          <UFormField label="Version" :ui="{ label: 'text-12 text-toned' }">
            <UInput
              :model-value="listing.version"
              :readonly="locked"
              size="sm"
              class="w-full"
              :ui="{ base: 'font-mono' }"
              @update:model-value="change({ version: `${$event}` })"
            />
          </UFormField>
          <span v-if="older" class="text-11 text-dimmed">From the zip</span>
          <span v-else class="flex flex-wrap gap-1">
            <UButton
              v-for="bump in bumps"
              :key="bump"
              :label="bump"
              color="neutral"
              variant="subtle"
              size="xs"
              class="font-mono"
              :disabled="locked"
              @click="change({ version: bump })"
            />
          </span>
        </div>
        <div class="flex min-w-0 grow flex-col gap-1.5">
          <UFormField
            label="For game version"
            :help="state?.install.version ? `Installed game is ${state.install.version}` : undefined"
            :ui="{ label: 'text-12 text-toned', help: 'text-11 text-dimmed' }"
          >
            <UInput
              :model-value="listing.supportedVersion"
              size="sm"
              class="w-full"
              :ui="{ base: 'font-mono' }"
              @update:model-value="change({ supportedVersion: `${$event}` })"
            />
          </UFormField>
          <span v-if="listing.versionTag" class="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
            <UBadge :label="listing.versionTag" size="sm" class="font-mono" />
            <span class="text-11 whitespace-nowrap text-dimmed">Steam's version tag</span>
          </span>
          <span v-else-if="untaggedVersion" class="text-11 text-dimmed">
            Steam lists no version tag for {{ untaggedVersion }}
          </span>
        </div>
      </div>

      <UFormField label="Change note" :ui="{ label: 'text-12 text-toned', hint: 'flex gap-2' }">
        <template #hint>
          <UButton
            v-for="note in notes ?? []"
            :key="note.source"
            :label="`Fill from ${note.source}`"
            variant="link"
            size="xs"
            class="p-0 text-11"
            @click="change({ changelog: note.text })"
          />
        </template>
        <UTextarea
          :model-value="listing.changelog"
          :rows="3"
          :maxrows="10"
          autoresize
          size="sm"
          placeholder="What changed in this version. Steam shows it under Change Notes."
          class="w-full"
          @update:model-value="change({ changelog: `${$event}` })"
        />
        <p v-if="hint" class="mt-1.5 text-11 text-dimmed">{{ hint }}</p>
      </UFormField>

      <section
        v-if="!listing.workshopId"
        class="flex shrink-0 flex-col gap-1.5 rounded-lg border border-dashed border-accented p-3.5"
      >
        <span class="flex items-center gap-2 text-12">
          <UIcon name="i-lucide-cloud" class="size-3.5 text-dimmed" />
          Not on the Workshop yet
        </span>
        <span class="text-11 text-dimmed">Uploading creates the Workshop item and fills its page from the left.</span>
        <UButton
          label="Already on Steam? Link it"
          variant="link"
          size="xs"
          class="self-start p-0 text-12"
          @click="emit('link')"
        />
      </section>

      <section v-else class="shrink-0 overflow-hidden rounded-lg border border-default bg-default">
        <div class="flex h-9 items-center gap-2 border-b border-default ps-3 pe-2.5">
          <span class="grow text-12 font-semibold">What changes on Steam</span>
          <span v-if="comparison?.checkedAt" class="text-11 text-dimmed">
            checked {{ describeAge(comparison.checkedAt, "short") }}
          </span>
          <UTooltip text="Check again">
            <UButton
              icon="i-lucide-refresh-cw"
              variant="ghost"
              color="neutral"
              size="xs"
              square
              aria-label="Check again"
              :loading="checking"
              @click="check({ modId, key: listingKey })"
            />
          </UTooltip>
        </div>
        <p v-if="!comparison && !unreachable" class="px-3 py-2.5 text-12 text-dimmed">Asking Steam…</p>
        <div
          v-else-if="!comparison || comparison.status === 'unknown'"
          class="flex flex-col items-start gap-2 px-3 py-2.5"
        >
          <span class="text-12 text-toned">
            {{ toSentence(comparison ? comparison.reason : (unreachable?.message ?? "")) }}
          </span>
          <UButton
            label="Check again"
            icon="i-lucide-refresh-cw"
            variant="subtle"
            size="xs"
            :loading="checking"
            @click="check({ modId, key: listingKey })"
          />
        </div>
        <template v-else>
          <div v-for="group in groups" :key="group.label" class="pb-1">
            <div class="px-3 pt-2 pb-0.5 text-11 font-semibold text-dimmed">{{ group.label }}</div>
            <button
              v-for="row in group.rows"
              :key="row.id + row.summary"
              type="button"
              :disabled="!row.id"
              class="flex h-7 w-full items-center gap-2 ps-3 pe-2.5 text-start text-12 enabled:hover:bg-elevated"
              :class="{ 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]': row.lit }"
              @click="emit('compare', row.id)"
            >
              <b v-if="row.mark" class="w-3.5 shrink-0 text-center font-mono font-medium" :class="row.color">
                {{ row.mark }}
              </b>
              <UIcon v-else name="i-lucide-file" class="size-3.5 shrink-0 text-dimmed" />
              <span v-if="row.label" class="w-30 shrink-0 truncate text-highlighted">{{ row.label }}</span>
              <span
                class="grow truncate text-toned"
                :class="{ 'font-mono text-11.5': row.id.startsWith('version:') }"
                :title="row.summary"
              >
                {{ row.summary }}
              </span>
              <span v-if="row.extra" class="shrink-0 text-11 text-dimmed">{{ row.extra }}</span>
              <UIcon v-if="!row.label" name="i-lucide-chevron-right" class="size-3.5 shrink-0 text-dimmed" />
            </button>
          </div>
        </template>
      </section>
    </div>

    <UploadAction :mod-id="modId" :listing-key="listingKey" :listing="listing" />
  </aside>
</template>
