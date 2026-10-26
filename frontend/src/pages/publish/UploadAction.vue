<!--
  This shows the upload button, the mod's problems before an upload, and its progress until it ends.
-->
<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { useIntervalFn, useNow } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import { countBy, groupBy, isEqual, omit } from "es-toolkit";
import { useRouter } from "vue-router";

import {
  describeCount,
  describeDetail,
  describeFinding,
  type FindingRow,
  LEVELS,
  list,
  listFindingRows,
  listProblems,
  plural,
  toRepeatKey,
  toSentence,
} from "../../lib/format";
import { codesQuery, comparisonQuery, reportQuery, steamStatusQuery } from "../../queries";
import { STAGES, useUploadStore } from "../../stores/uploads";
import { useWorkspaceStore } from "../../stores/workspace";
import LevelCounts from "../../ui/LevelCounts.vue";
import ProblemRow from "../../ui/ProblemRow.vue";
import { AGREEMENT_URL, toPageURL } from "./workshop";
import type { Listing } from "@services/steam/models";

const VISIBILITIES = [
  { label: "Private", value: "private" },
  { label: "Friends only", value: "friends" },
  { label: "Public", value: "public" },
  { label: "Unlisted", value: "unlisted" },
];
const PIPS = ["Packed", "Item ready", "Files", "Page and note"];

const { modId, listingKey, listing } = defineProps<{ modId: string; listingKey: string; listing: Listing }>();

const router = useRouter();
const workspace = useWorkspaceStore();
const uploads = useUploadStore();
const now = useNow({ scheduler: (tick) => useIntervalFn(tick, 15_000) });
// "" keeps the item's own visibility. A new item starts private.
const picked = shallowRef("");
// Whether the upload waits for the modder to confirm the mod's problems.
const confirming = shallowRef(false);

const enabled = computed(() => workspace.isOpen());
const {
  data: reason,
  refetch: checkSteam,
  isLoading: checkingSteam,
} = useQuery(() => ({ ...steamStatusQuery, enabled: enabled.value }));
const { data: codes } = useQuery(codesQuery);
const { data: report } = useQuery(() => ({ ...reportQuery(workspace.listModIds()), enabled: enabled.value }));
const { data: comparison } = useQuery(() => ({
  ...comparisonQuery({ modId, key: listingKey }),
  enabled: enabled.value,
}));

// The mod's own problems as Health lists them, each with the row Health opens it at. An older listing has none.
const problems = computed(() =>
  listingKey ? [] : listFindingRows(listProblems(report.value?.findings ?? [])[modId] ?? []),
);

const attempt = computed(() => uploads.find(modId, listingKey));
// The route that opens Health at the mod.
const healthRoute = computed(() => ({
  name: "health" as const,
  params: { id: workspace.activeId() },
  query: { view: "mod", mod: modId },
}));
const visibility = computed({
  get: () => picked.value || comparison.value?.steam?.visibility || (listing.workshopId ? "" : "private"),
  set: (next: string) => (picked.value = next),
});
const counts = computed(() => countBy(problems.value, ({ finding }) => finding.level));
const sentence = computed(() =>
  list(
    [
      counts.value.serious ? plural(counts.value.serious, "serious problem") : "",
      counts.value.warning ? plural(counts.value.warning, "warning") : "",
    ].filter(Boolean),
  ),
);
// Each serious finding once, with its repeats, as Health lists them.
const serious = computed(() =>
  Object.values(
    groupBy(
      problems.value.filter(({ finding }) => finding.level === "serious"),
      ({ finding }) => toRepeatKey(finding, codes.value),
    ),
  ),
);
// The worst level among the problems, or none if the mod has none.
const worst = computed(() =>
  counts.value.serious ? LEVELS.serious : counts.value.warning ? LEVELS.warning : undefined,
);
const label = computed(() => {
  if (reason.value) return "Start Steam to upload";
  return listing.workshopId ? "Upload to Steam" : "Create on Steam";
});
// Why the upload button is off, when Steam is not the reason. Steam's reason has its own line.
const obstacle = computed(() => {
  if (!listing.title) return "Give the listing a title to upload it.";
  return uploads.active ? "Another listing is uploading. This one can start when it ends." : "";
});
const stage = computed(() => STAGES[attempt.value?.progress?.stage ?? "preparing"]);
const percent = computed(() => {
  const progress = attempt.value?.progress;
  return progress?.total ? Math.round((progress.done * 100) / progress.total) : null;
});
const quiet = computed(() => Math.floor((now.value.getTime() - (attempt.value?.heard ?? 0)) / 60_000));
const stalled = computed(() => quiet.value >= stage.value.stall);

// An ended upload is cleared once the listing is edited. A new item's id from Go is not an edit.
watch(
  () => omit(listing, ["workshopId"]),
  (edited) => {
    const ended = attempt.value;
    if (ended && ended.status !== "running" && !isEqual(edited, omit(ended.listing, ["workshopId"]))) {
      uploads.clear(modId, listingKey);
    }
  },
);

/** start uploads the listing. It asks about the mod's problems first if the mod has any. */
function start(): void {
  if (worst.value) confirming.value = true;
  else submit();
}

/** open shows a problem in Health. */
function open(problem: FindingRow): void {
  void router.push({ ...healthRoute.value, query: { ...healthRoute.value.query, row: String(problem.index) } });
}

/** submit writes the listing and uploads it, with the picked visibility or the item's own. */
function submit(): void {
  confirming.value = false;
  void uploads.start(modId, listingKey, listing, picked.value || (listing.workshopId ? "" : "private"));
}
</script>

<template>
  <div class="flex shrink-0 flex-col gap-2 border-t border-default px-4.5 py-3.5">
    <template v-if="attempt?.status === 'running'">
      <template v-if="stalled">
        <div class="flex items-center gap-2 text-12">
          <span class="size-2 shrink-0 rounded-full bg-warning" />
          No progress from Steam for {{ plural(quiet, "minute") }}
        </div>
        <div class="flex gap-2">
          <UButton
            label="Keep waiting"
            color="neutral"
            variant="subtle"
            class="grow justify-center"
            @click="uploads.keepWaiting(modId, listingKey)"
          />
          <UButton label="Stop" color="error" variant="soft" class="grow justify-center" @click="uploads.stop()" />
        </div>
      </template>
      <template v-else>
        <div class="flex items-center gap-2 text-12">
          <span class="grow">{{ stage.words }}</span>
          <span v-if="percent !== null" class="font-mono text-toned">{{ percent }}%</span>
        </div>
        <UProgress :model-value="percent" size="xs" />
        <div class="flex gap-1" aria-hidden="true">
          <i
            v-for="(pip, index) in PIPS"
            :key="pip"
            class="h-1 grow rounded-full"
            :class="index < stage.pip ? 'bg-primary' : index === stage.pip ? 'bg-primary/50' : 'bg-accented'"
          />
        </div>
        <span class="flex flex-wrap gap-x-1.5 text-11 text-dimmed">
          <template v-for="(pip, index) in PIPS" :key="pip">
            <span :class="{ 'font-medium text-highlighted': index === stage.pip }">{{ pip }}</span>
            <span v-if="index < PIPS.length - 1">·</span>
          </template>
        </span>
      </template>
    </template>

    <template v-else-if="attempt?.status === 'done'">
      <div class="flex items-center gap-2 text-12">
        <UIcon name="i-lucide-check" class="size-3.5 shrink-0 text-success" />
        <span class="grow">{{ attempt.listing.version }} is on Steam</span>
        <UButton
          label="View"
          icon="i-lucide-external-link"
          variant="ghost"
          color="neutral"
          size="xs"
          :disabled="!listing.workshopId"
          @click="Browser.OpenURL(toPageURL(listing.workshopId))"
        />
        <UTooltip text="Dismiss">
          <UButton
            icon="i-lucide-x"
            variant="ghost"
            color="neutral"
            size="xs"
            square
            aria-label="Dismiss"
            @click="uploads.clear(modId, listingKey)"
          />
        </UTooltip>
      </div>
      <span class="text-11 text-dimmed">Steam confirmed the files, the page and the change note.</span>
    </template>

    <template v-else-if="attempt?.status === 'failed'">
      <div class="flex items-start gap-2 text-12">
        <UIcon name="i-lucide-circle-x" class="mt-0.5 size-3.5 shrink-0 text-error" />
        <span class="grow wrap-anywhere">{{ attempt.message }}</span>
      </div>
      <UButton
        label="Try again"
        color="neutral"
        variant="subtle"
        class="justify-center"
        @click="uploads.clear(modId, listingKey)"
      />
    </template>

    <template v-else-if="confirming && worst">
      <div class="flex items-start gap-2 text-12">
        <UIcon :name="worst.icon" class="mt-0.5 size-3.5 shrink-0" :style="{ color: `var(--ui-${worst.color})` }" />
        <span class="grow">This mod has {{ sentence }}. Steam will still take the upload.</span>
      </div>
      <div
        v-if="serious.length"
        class="max-h-36 divide-y divide-default overflow-y-auto rounded-lg border border-default bg-default"
      >
        <ProblemRow
          v-for="[problem, ...repeats] in serious"
          :key="problem?.index"
          :level="problem?.finding.level"
          :message="problem ? describeFinding(problem.finding, codes) : ''"
          :detail="problem ? describeDetail(problem.finding) : ''"
          mono
          @open="problem && open(problem)"
        >
          <template #trailing>
            <UBadge v-if="repeats.length" :label="`×${describeCount(repeats.length + 1)}`" size="sm" class="shrink-0" />
          </template>
        </ProblemRow>
      </div>
      <div class="flex gap-2">
        <UButton
          label="Show in Health"
          :to="healthRoute"
          color="neutral"
          variant="subtle"
          class="grow justify-center"
        />
        <UButton label="Upload anyway" color="primary" variant="solid" class="grow justify-center" @click="submit" />
      </div>
    </template>

    <template v-else>
      <UButton
        v-if="!listingKey"
        :to="healthRoute"
        color="neutral"
        variant="subtle"
        block
        class="h-9.5 justify-start gap-2.5 text-12"
      >
        <UIcon name="i-lucide-activity" class="size-4 shrink-0 text-dimmed" />
        <span class="grow text-start">Health</span>
        <LevelCounts :levels="counts" />
        <UIcon name="i-lucide-chevron-right" class="size-3.5 shrink-0 text-dimmed" />
      </UButton>
      <span v-if="reason" class="flex items-center gap-2 text-11 text-dimmed">
        <span class="min-w-0 truncate" :title="toSentence(reason)">{{ toSentence(reason) }}</span>
        <UButton
          label="Check again"
          variant="link"
          size="xs"
          class="shrink-0 p-0 text-11"
          :loading="checkingSteam"
          @click="checkSteam()"
        />
      </span>
      <div class="flex gap-2">
        <USelect v-model="visibility" :items="VISIBILITIES" placeholder="Unchanged" size="lg" class="w-34 shrink-0" />
        <UButton
          :label="label"
          icon="i-lucide-upload"
          color="primary"
          variant="solid"
          size="lg"
          class="grow justify-center"
          :disabled="!!reason || !!obstacle"
          @click="start"
        />
      </div>
      <span v-if="obstacle" class="text-center text-11 text-dimmed">{{ obstacle }}</span>
      <span v-if="listing.workshopId" class="text-center text-11 text-dimmed">
        Updates Workshop item {{ listing.workshopId }}
      </span>
    </template>

    <span class="text-center text-11 text-dimmed">
      Uploading accepts the
      <UButton
        label="Steam Workshop agreement"
        variant="link"
        class="p-0 text-11"
        @click="Browser.OpenURL(AGREEMENT_URL)"
      />
    </span>
  </div>
</template>
