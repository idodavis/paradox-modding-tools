<!-- The summary of Health: the load order and three panels, each with a verdict and a link into its view. -->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { useTimeAgo } from "@vueuse/core";
import { keyBy, sortBy, sumBy } from "es-toolkit";
import type { Finding, GameLog, Report, Tally } from "@services/health/models";
import type { Part } from "@services/lsp/models";
import type { Mod } from "@services/workspace/models";
import {
  describeCount,
  describeTime,
  joinParts,
  splitSubject,
  list,
  listLeftOut,
  listSiteKeys,
  plural,
  toSiteKey,
} from "../../lib/format";
import { levelsQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import FactText from "../../ui/FactText.vue";
import LeftOutMark from "../../ui/LeftOutMark.vue";
import LevelCounts from "../../ui/LevelCounts.vue";
import LoadOrder from "../../ui/LoadOrder.vue";
import ModAvatar from "../../ui/ModAvatar.vue";
import ProblemRow from "../../ui/ProblemRow.vue";

const {
  mods,
  report,
  log = undefined,
  yours,
  game,
} = defineProps<{
  mods: Mod[];
  report: Report;
  log?: GameLog;
  /** How many lines of the game's log are the mods'. */
  yours: number;
  /** How many lines of the game's log are the game's own. */
  game: number;
}>();
const emit = defineEmits<{ open: [view: string, mod?: string, finding?: Finding]; check: [code: string] }>();

/** How many rows a panel lists. */
const ROWS_SHOWN = 4;
/** The plain words for compatibility's info. */
const INFO_WORDS: readonly { checks: readonly string[]; one: string; many: string }[] = [
  { checks: ["override"], one: "override of the game", many: "overrides of the game" },
  {
    checks: ["contest", "file-contest", "undecided-contest"],
    one: "allowed conflict",
    many: "allowed conflicts",
  },
  { checks: ["shared-message"], one: "allowed shared localization key", many: "allowed shared localization keys" },
  { checks: ["ignored-pair"], one: "pair of mods ignored", many: "pairs of mods ignored" },
];

const workspace = useWorkspaceStore();
const { data: levels } = useQuery(levelsQuery);
/** Each level's row, by its id. */
const marks = computed(() => keyBy(levels.value ?? [], (level) => level.id));
/** The levels that count as problems, the worst first. */
const problemLevels = computed(() => (levels.value ?? []).filter((level) => level.problem));
/** The levels a mod needs a fix for, where the game loses something. */
const fixLevels = computed(() => (levels.value ?? []).filter((level) => level.fix).map((level) => level.id));

const checked = useTimeAgo(() => report.checkedAt);
const order = computed(() => mods.map((mod) => mod.id));
const rows = computed(() => log?.findings ?? []);
/** The places the game's log names. */
const loggedPlaces = computed(() => new Set(rows.value.map(toSiteKey)));
/** The report's group of each mod, by mod id. */
const groups = computed(() => keyBy(report.mods ?? [], (group) => group.mod));
/** Each of the user's own mods with its group and its lead, the first problem of its worst level. */
const modRows = computed(() =>
  mods
    .filter((mod) => !mod.external)
    .map((mod) => {
      const group = groups.value[mod.id];
      const lead = group?.findings?.[0];
      return {
        mod,
        group,
        lead,
        logged: !!lead && listSiteKeys(lead).some((place) => loggedPlaces.value.has(place)),
      };
    }),
);
const busiest = computed(() =>
  Math.max(
    1,
    ...modRows.value.map(({ group }) => sumBy(problemLevels.value, (level) => group?.counts?.[level.id] ?? 0)),
  ),
);
const brokenCount = computed(
  () => modRows.value.filter(({ group }) => fixLevels.value.includes(group?.worst ?? "")).length,
);
/** The mods' share of the log's lines, as the meter's width. */
const logShare = computed(() => `${(yours / Math.max(1, yours + game)) * 100}%`);
const lastRun = computed(() => (log?.writtenAt ? describeTime(log.writtenAt) : ""));
/** The mods' log rows that repeat most. */
const top = computed(() =>
  sortBy(
    rows.value.filter((row) => row.origin),
    [(row) => -row.count],
  ).slice(0, ROWS_SHOWN - 1),
);
/** Whether one of those rows repeats. Each then gets a column for its count. */
const topRepeated = computed(() => top.value.some((row) => row.count > 1));
/** Each compatibility check with problems, the worst first. */
const checks = computed(() => (report.compatibility.checks ?? []).filter((check) => check.level !== "info"));
/** The mods left out of the compatibility checks, as a phrase, or "" for none. */
const leftOut = computed(() => list(listLeftOut(mods)));
/** Compatibility's info, in plain words. */
const infoChecks = computed(() =>
  INFO_WORDS.flatMap((row) => {
    const count = sumBy(
      (report.compatibility.checks ?? []).filter((check) => check.level === "info" && row.checks.includes(check.code)),
      (check) => check.count,
    );
    return count ? [plural(count, row.one, row.many)] : [];
  }).join(" · "),
);
/** Compatibility's worst level if that level needs a fix, else "". */
const worstFix = computed(() =>
  fixLevels.value.includes(report.compatibility.worst) ? report.compatibility.worst : "",
);
const panels = computed(() => [
  {
    view: "compatibility",
    kicker: "Workspace compatibility",
    icon: "i-lucide-git-merge",
    color: resolveColor(report.compatibility),
    verdict: describeVerdict(report.compatibility, report.compatibility.phrases?.[worstFix.value] ?? ""),
    note: [
      ...(levels.value ?? []).flatMap((level) =>
        level.id === worstFix.value ? [] : (report.compatibility.phrases?.[level.id] ?? []),
      ),
      leftOut.value && `${leftOut.value} left out`,
    ]
      .filter(Boolean)
      .join(" · "),
    footer: "Open workspace compatibility",
  },
  {
    view: "mod",
    kicker: "Your mods",
    icon: "i-lucide-package",
    color: resolveColor(report.own),
    verdict: describeVerdict(
      report.own,
      `${describeCount(brokenCount.value)} ${brokenCount.value === 1 ? "mod needs" : "mods need"} a fix`,
    ),
    note: problemLevels.value.flatMap((level) => report.own.phrases?.[level.id] ?? []).join(" · "),
    footer: "Open the mods",
    modId: modRows.value[0]?.mod.id,
  },
  {
    view: "log",
    kicker: "Game log",
    icon: "i-lucide-terminal",
    color: "neutral",
    verdict: yours ? `${plural(yours, "line")} from your mods` : "Nothing from your mods",
    note: [`${plural(yours + game, "line")} in all`, lastRun.value && `last run ${lastRun.value}`]
      .filter(Boolean)
      .join(" · "),
    footer: "Open the game log",
  },
]);

/** describeVerdict returns a panel's headline: the given words if something needs a fix. */
function describeVerdict(tally: Tally, fix: string): string {
  if (fixLevels.value.includes(tally.worst)) return fix;
  return tally.counts?.warning ? "Nothing needs a fix" : "No problems";
}

/** resolveColor returns the color of a panel's worst level. It is success when there is no problem. */
function resolveColor(tally: Tally): string {
  return problemLevels.value.find((level) => level.id === tally.worst)?.color ?? "success";
}

/** describeRing returns the style of a panel's ring in a Nuxt UI color. */
function describeRing(color: string): Record<string, string> {
  const base = color === "neutral" ? "var(--ui-text-muted)" : `var(--ui-${color})`;
  return {
    color: base,
    background: `color-mix(in oklab, ${base} 16%, transparent)`,
    boxShadow: `0 0 0 4px color-mix(in oklab, ${base} 8%, transparent)`,
  };
}

/** describeMeter returns the segments of a mod's meter, one for each level of problem. */
function describeMeter(counts: Tally["counts"] | undefined): { color: string; width: string }[] {
  if (!problemLevels.value.some((level) => counts?.[level.id])) return [{ color: "success", width: "100%" }];
  // A level with any problem keeps a sliver, so it never reads as clean.
  const share = (count: number): string => `${count ? Math.max(3, (count / busiest.value) * 100) : 0}%`;
  return problemLevels.value.map((level) => ({ color: level.color, width: share(counts?.[level.id] ?? 0) }));
}

/** describeLogPlace returns the mod a log row is about and the file it names, as parts. */
function describeLogPlace(finding: Finding): Part[] {
  const site = finding.sites?.[0];
  return [
    { text: workspace.originName(finding.origin), style: "mod" },
    { text: " · ", style: "" },
    site ? { text: site.place.rel, style: "path" } : { text: finding.facts?.named ?? "", style: "code" },
  ];
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-4 px-6 py-4.5">
    <div class="flex shrink-0 items-start gap-2">
      <span class="me-1.5 shrink-0 text-12 leading-7 font-medium whitespace-nowrap text-muted">Load order</span>
      <LoadOrder :order="order" :editable="false" class="min-w-0" />
      <span class="grow" />
      <span v-if="checked" class="shrink-0 text-11 leading-7 whitespace-nowrap text-dimmed">Checked {{ checked }}</span>
    </div>
    <div class="grid min-h-0 grow grid-cols-[1.1fr_1fr_1fr] gap-3.5">
      <section
        v-for="panel in panels"
        :key="panel.view"
        class="flex min-h-0 flex-col overflow-hidden rounded-lg border border-default bg-muted"
      >
        <div class="flex shrink-0 items-center gap-3 border-b border-default px-4 py-3.5">
          <span
            class="flex size-10 shrink-0 items-center justify-center rounded-full"
            :style="describeRing(panel.color)"
          >
            <UIcon :name="panel.icon" class="size-5" />
          </span>
          <span class="flex min-w-0 grow flex-col">
            <span class="text-11 font-medium tracking-wide text-dimmed uppercase">{{ panel.kicker }}</span>
            <span class="line-clamp-2 text-20 leading-tight text-highlighted">{{ panel.verdict }}</span>
            <span class="line-clamp-2 text-11 text-dimmed">{{ panel.note }}</span>
          </span>
          <span v-if="panel.view === 'log'" class="flex shrink-0 items-center gap-1.5 self-start text-11 text-dimmed">
            <span class="size-1.5 rounded-full" style="background: var(--ui-success)" />Live
          </span>
        </div>
        <div class="flex min-h-0 grow flex-col overflow-y-auto py-1">
          <template v-if="panel.view === 'compatibility'">
            <button
              v-for="check in checks"
              :key="`${check.level}:${check.code}`"
              type="button"
              class="mx-1 flex h-9 items-center gap-2.5 rounded-md px-2.5 text-start text-12 text-highlighted hover:bg-accented"
              @click="emit('check', check.code)"
            >
              <UIcon
                :name="marks[check.level]?.icon ?? 'i-lucide-info'"
                class="size-3.5 shrink-0"
                :style="{ color: `var(--ui-${marks[check.level]?.color})` }"
              />
              <span class="min-w-0 grow truncate">{{ check.label }}</span>
              <span class="shrink-0 font-semibold tabular-nums">{{ describeCount(check.count) }}</span>
            </button>
            <p v-if="infoChecks" class="flex items-start gap-2.5 px-3 py-2 text-12 text-dimmed">
              <UIcon :name="marks.info?.icon ?? 'i-lucide-info'" class="mt-0.5 size-3.5 shrink-0" />
              {{ infoChecks }}
            </p>
          </template>
          <template v-else-if="panel.view === 'mod'">
            <template v-for="modRow in modRows" :key="modRow.mod.id">
              <button
                type="button"
                class="flex items-center gap-2.5 px-3 py-2 text-start hover:bg-accented"
                @click="emit('open', 'mod', modRow.mod.id)"
              >
                <ModAvatar :mod="modRow.mod" size="sm" />
                <span class="flex min-w-0 grow flex-col gap-1.25">
                  <span class="flex items-center gap-2 text-12">
                    <span class="grow truncate font-medium text-highlighted">{{ modRow.mod.descriptor.name }}</span>
                    <LeftOutMark v-if="modRow.mod.leftOut" />
                    <LevelCounts :levels="modRow.group?.counts" />
                  </span>
                  <span class="flex h-1.5 overflow-hidden rounded-full bg-accented">
                    <span
                      v-for="segment in describeMeter(modRow.group?.counts)"
                      :key="segment.color"
                      class="h-full"
                      :style="{ width: segment.width, background: `var(--ui-${segment.color})` }"
                    />
                  </span>
                </span>
              </button>
              <ProblemRow
                v-if="modRow.lead"
                :level="modRow.lead.level"
                :message="joinParts(modRow.lead.message)"
                class="ps-12"
                @open="emit('open', 'mod', modRow.mod.id, modRow.lead)"
              >
                <template v-if="modRow.lead.name" #message>
                  <span class="font-mono text-12"><FactText :parts="splitSubject(modRow.lead)" /></span>
                </template>
                <template #detail>
                  <span class="min-w-0 truncate text-11 text-dimmed">
                    {{ modRow.lead.label }}
                    <template v-if="modRow.lead.brief?.length">· <FactText :parts="modRow.lead.brief" /></template>
                  </span>
                </template>
                <template v-if="modRow.logged" #trailing>
                  <UBadge label="in the log" color="neutral" variant="subtle" size="sm" class="shrink-0" />
                </template>
              </ProblemRow>
            </template>
          </template>
          <template v-else>
            <div class="flex flex-col gap-1.5 px-4 pt-2 pb-2.5">
              <span class="flex h-2 overflow-hidden rounded-full bg-accented">
                <span class="h-full bg-(--fg-2)" :style="{ width: logShare }" />
              </span>
              <span class="flex items-center gap-3 text-11 text-dimmed">
                <span class="flex items-center gap-1.25">
                  <span class="size-1.5 rounded-full bg-(--fg-2)" />{{ describeCount(yours) }} from your mods
                </span>
                <span class="flex items-center gap-1.25">
                  <span class="size-1.5 rounded-full bg-accented" />{{ describeCount(game) }} the game's own
                </span>
              </span>
            </div>
            <ProblemRow
              v-for="(row, position) in top"
              :key="position"
              :message="joinParts(row.message)"
              @open="emit('open', 'log', undefined, row)"
            >
              <template #detail>
                <span class="min-w-0 truncate text-11 text-dimmed"><FactText :parts="describeLogPlace(row)" /></span>
              </template>
              <template v-if="topRepeated" #leading>
                <span class="flex w-9 shrink-0 justify-center">
                  <UBadge v-if="row.count > 1" :label="`×${describeCount(row.count)}`" size="sm" />
                </span>
              </template>
            </ProblemRow>
            <p v-if="game" class="flex items-center gap-2.5 px-3 py-2 text-12 text-dimmed">
              <UIcon name="i-lucide-eye-off" class="size-3.5" />
              {{ plural(game, "line") }} about the game's own files, hidden
            </p>
          </template>
        </div>
        <button
          type="button"
          class="flex shrink-0 items-center border-t border-default px-4 py-2.5 text-12 text-toned hover:bg-accented"
          @click="emit('open', panel.view, panel.modId)"
        >
          <span class="grow text-start">{{ panel.footer }}</span>
          <UIcon name="i-lucide-chevron-right" class="size-3.5 text-dimmed" />
        </button>
      </section>
    </div>
  </div>
</template>
