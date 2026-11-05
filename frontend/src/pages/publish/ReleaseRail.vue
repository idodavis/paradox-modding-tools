<!-- The rail that edits a listing's next update, shows what it changes on Steam and uploads it. -->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { countBy } from "es-toolkit";

import {
  bytes,
  day,
  describeAge,
  describeCount,
  MARKS,
  parseTime,
  plural,
  toDifferenceId,
  toSentence,
} from "../../lib/format";
import { changeNotesQuery, comparisonQuery, stateQuery, tagsQuery, useComparison } from "../../queries";
import { useUploadStore } from "../../stores/uploads";
import { useWorkspaceStore } from "../../stores/workspace";
import DifferenceList from "./DifferenceList.vue";
import UploadAction from "./UploadAction.vue";
import type { Listing } from "@services/steam/models";

/** RELEASE matches the major, minor and hotfix numbers a version starts with. The last two are optional. */
const RELEASE = /^(\d+)(?:\.(\d+))?(?:\.(\d+))?/;

const { modId, listingKey, comparing, picked } = defineProps<{
  modId: string;
  listingKey: string;
  comparing: boolean;
  /** The id of the difference the compare shows, or "". */
  picked: string;
}>();
const emit = defineEmits<{ compare: [id: string]; link: [] }>();
const listing = defineModel<Listing>("listing", { required: true });

const workspace = useWorkspaceStore();
const uploads = useUploadStore();

const enabled = computed(() => workspace.isOpen());
const { data: state } = useQuery(stateQuery);
const { data: tags } = useQuery(() => ({ ...tagsQuery, enabled: enabled.value }));
const { data: notes } = useQuery(() => ({ ...changeNotesQuery({ modId, key: listingKey }), enabled: enabled.value }));
const { data: comparison, error: unreachable } = useQuery(() => ({
  ...comparisonQuery({ modId, key: listingKey }),
  enabled: enabled.value,
}));
const { mutate: check, isLoading: checking } = useComparison();

const older = computed(() => listingKey !== "");
// The version stays as it is for an older listing and while an upload runs.
const locked = computed(() => older.value || uploads.find(modId, listingKey)?.status === "running");
const bumps = computed(() => {
  const match = RELEASE.exec(listing.value.version);
  if (!match) return [];
  const [major = 0, minor = 0, hotfix = 0] = match.slice(1).map((part) => Number(part ?? 0));
  return [`${major}.${minor + 1}.0`, `${major}.${minor}.${hotfix + 1}`];
});
// The game's major and minor version when Steam lists no tag for it, else "".
const untaggedVersion = computed(() => {
  if (listing.value.versionTag || !tags.value?.versions?.length || !listing.value.supportedVersion) return "";
  return listing.value.supportedVersion.split(".").slice(0, 2).join(".");
});
// The draft the change note was filled from, if it is unedited.
const filledNote = computed(() =>
  listing.value.changelog ? notes.value?.find((note) => note.text === listing.value.changelog) : undefined,
);
const hint = computed(() => {
  const note = filledNote.value;
  if (!note) return "";
  if (note.source !== "git") return `Taken from ${note.source}. Edit it before you upload.`;
  const since = parseTime(note.since) ? `since your last upload, ${day(note.since)}` : "since the start";
  return `Drafted from the ${plural(note.count, "commit")} ${since}. Edit it before you upload.`;
});

const groups = computed(() => {
  const page = comparison.value?.page ?? [];
  const version = (comparison.value?.files ?? []).filter((difference) => difference.field === "version");
  return [...(page.length ? [{ label: "Page", differences: page }] : []), { label: "Files", differences: version }];
});
const fileSummary = computed(() => {
  const changes = (comparison.value?.files ?? []).filter((difference) => difference.field === "file");
  const upload = comparison.value?.upload;
  const counted = countBy(changes, (difference) => difference.status);
  const counts = Object.entries(MARKS)
    .filter(([status]) => counted[status])
    .map(([status, { count }]) => `${describeCount(counted[status] ?? 0)} ${count}`);
  return {
    // A summary with no id cannot be picked.
    id: changes[0] ? toDifferenceId(changes[0]) : "",
    lit: comparing && changes.some((difference) => toDifferenceId(difference) === picked),
    text: counts.join(", ") || "Files are the same as Steam's",
    total: upload?.files?.length ? `${describeCount(upload.files.length)} · ${bytes(upload.bytes)}` : "",
  };
});

/** change sets some fields of the listing. */
function change(changes: Partial<Listing>): void {
  listing.value = { ...listing.value, ...changes };
}
</script>

<template>
  <aside class="flex h-full w-100 shrink-0 flex-col border-s border-default bg-muted">
    <div class="flex min-h-0 grow flex-col gap-3.5 overflow-y-auto px-4.5 py-4">
      <span class="text-13 font-semibold text-highlighted">This update</span>

      <div class="grid grid-cols-2 items-start gap-5">
        <div class="flex min-w-0 flex-col gap-1.5">
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
          <span v-else class="flex flex-wrap items-center gap-1">
            <span class="text-11 text-dimmed">Next:</span>
            <template v-for="(bump, position) in bumps" :key="bump">
              <span v-if="position" class="text-11 text-dimmed">·</span>
              <UButton
                :label="bump"
                color="neutral"
                variant="subtle"
                size="xs"
                class="font-mono"
                :disabled="locked"
                @click="change({ version: bump })"
              />
            </template>
          </span>
        </div>
        <div class="flex min-w-0 flex-col gap-1.5">
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
          <div class="pb-1">
            <DifferenceList :groups="groups" :lit="comparing ? picked : ''" @pick="emit('compare', $event)" />
            <button
              type="button"
              :disabled="!fileSummary.id"
              class="flex h-7 w-full items-center gap-2 ps-3 pe-2.5 text-start text-12 enabled:hover:bg-accented"
              :class="{ 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]': fileSummary.lit }"
              @click="emit('compare', fileSummary.id)"
            >
              <UIcon name="i-lucide-file" class="size-3.5 shrink-0 text-dimmed" />
              <span class="grow truncate text-toned" :title="fileSummary.text">{{ fileSummary.text }}</span>
              <span v-if="fileSummary.total" class="shrink-0 text-11 text-dimmed">{{ fileSummary.total }}</span>
              <UIcon name="i-lucide-chevron-right" class="size-3.5 shrink-0 text-dimmed" />
            </button>
          </div>
        </template>
      </section>
    </div>

    <UploadAction :mod-id="modId" :listing-key="listingKey" :listing="listing" />
  </aside>
</template>
