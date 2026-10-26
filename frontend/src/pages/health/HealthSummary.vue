<!--
  The summary of Health, with the load order and three panels side by side. Each panel gives a verdict, its worst rows
  and a link into its view.
-->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { useIntervalFn, useNow } from "@vueuse/core";
import { sortBy } from "es-toolkit";
import type { Finding, GameLog } from "@services/health/models";
import type { Mod } from "@services/workspace/models";
import {
  describeAge,
  describeCheck,
  describeCount,
  describeDetail,
  describeFinding,
  describeLogLine,
  describeTime,
  LEVELS,
  listFindingRows,
  plural,
  toSiteKey,
} from "../../lib/format";
import { codesQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import LevelCounts from "../../ui/LevelCounts.vue";
import LoadOrder from "../../ui/LoadOrder.vue";
import ModAvatar from "../../ui/ModAvatar.vue";
import ProblemRow from "../../ui/ProblemRow.vue";

const {
  mods,
  compatibility,
  health,
  log = undefined,
  checkedAt,
  compatibilityLevels,
  healthLevels,
  modLevels,
  yours,
  game,
} = defineProps<{
  mods: Mod[];
  /** The findings about the mods together. */
  compatibility: Finding[];
  /** Each mod's problems, by mod id. */
  health: Record<string, Finding[]>;
  log?: GameLog;
  /** When the report was made. */
  checkedAt: Date;
  /** The level counts of the mods together. */
  compatibilityLevels: Record<string, number>;
  /** The level counts of all the mods' problems. */
  healthLevels: Record<string, number>;
  /** The level counts of each mod, by mod id. */
  modLevels: Record<string, Record<string, number>>;
  /** How many lines of the game's log are the mods'. */
  yours: number;
  /** How many lines of the game's log are the game's own. */
  game: number;
}>();
const emit = defineEmits<{ open: [view: string, mod?: string, row?: number] }>();

/** How many rows a panel lists. */
const ROWS_SHOWN = 4;
/** The plain words for the info about the mods together, one row for each kind of finding and the checks it counts. */
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
const now = useNow({ scheduler: (tick) => useIntervalFn(tick, 30_000) });
const { data: codes } = useQuery(codesQuery);

const checked = computed(() => (now.value ? describeAge(checkedAt, "long") : ""));
const order = computed(() => mods.map((mod) => mod.id));
const rows = computed(() => log?.findings ?? []);
/** The places the game's log names. */
const loggedPlaces = computed(() => new Set(rows.value.map(toSiteKey)));
const tallies = computed(() =>
  mods.map((mod) => {
    const problems = health[mod.id] ?? [];
    const row = problems.findIndex((finding) => finding.level === "serious");
    const finding = problems[row];
    return {
      mod,
      levels: modLevels[mod.id] ?? {},
      lead: finding && { finding, row, logged: loggedPlaces.value.has(toSiteKey(finding)) },
    };
  }),
);
const busiest = computed(() =>
  Math.max(1, ...tallies.value.map((tally) => (tally.levels.serious ?? 0) + (tally.levels.warning ?? 0))),
);
const brokenCount = computed(() => tallies.value.filter((tally) => tally.levels.serious).length);
/** The mods' share of the log's lines, as the meter's width. */
const logShare = computed(() => `${(yours / Math.max(1, yours + game)) * 100}%`);
const lastRun = computed(() => (log?.writtenAt ? describeTime(log.writtenAt) : ""));
/** The mods' log rows that repeat most. */
const top = computed(() =>
  sortBy(
    listFindingRows(rows.value).filter((row) => row.finding.origin),
    [(row) => -row.finding.count],
  ).slice(0, ROWS_SHOWN - 1),
);
/** Whether one of those rows repeats. Each then gets a column for its count. */
const topRepeated = computed(() => top.value.some((row) => row.finding.count > 1));
/** The conflicts and warnings among the mods, serious first. */
const lead = computed(() =>
  sortBy(
    listFindingRows(compatibility).filter((row) => row.finding.level !== "info"),
    [(row) => (row.finding.level === "serious" ? 0 : 1)],
  ).slice(0, ROWS_SHOWN),
);
/** The info about the mods together, in plain words. */
const infoChecks = computed(() =>
  INFO_WORDS.flatMap((row) => {
    const count = compatibility.filter(
      (finding) => finding.level === "info" && row.checks.includes(finding.code),
    ).length;
    return count ? [plural(count, row.one, row.many)] : [];
  }).join(" · "),
);
const panels = computed(() => [
  {
    view: "compatibility",
    kicker: "Compatibility",
    icon: "i-lucide-git-merge",
    color: resolveColor(compatibilityLevels),
    verdict: describeVerdict(compatibilityLevels, plural(compatibilityLevels.serious ?? 0, "serious problem")),
    note: `${plural(compatibilityLevels.warning ?? 0, "warning")} · ${describeCount(compatibilityLevels.info ?? 0)} info`,
    footer: "Open Compatibility",
  },
  {
    view: "mod",
    kicker: "Mods",
    icon: "i-lucide-package",
    color: resolveColor(healthLevels),
    verdict: describeVerdict(
      healthLevels,
      `${describeCount(brokenCount.value)} ${brokenCount.value === 1 ? "mod needs" : "mods need"} a fix`,
    ),
    note: `${describeCount(healthLevels.serious ?? 0)} serious · ${plural(healthLevels.warning ?? 0, "warning")}`,
    footer: "Open the mods",
    modId: mods[0]?.id,
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

/** describeVerdict returns a panel's headline. With no serious problem it is "No serious problems" or "No problems". */
function describeVerdict(levels: Record<string, number>, serious: string): string {
  if (levels.serious) return serious;
  return levels.warning ? "No serious problems" : "No problems";
}

/** resolveColor returns the color of a panel's worst level. It is success when there is no problem. */
function resolveColor(levels: Record<string, number>): string {
  if (levels.serious) return "error";
  return levels.warning ? "warning" : "success";
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

/**
 * describeMeter returns the segments of a mod's meter, one for serious problems and one for warnings. A mod with
 * neither gets one full segment.
 */
function describeMeter(levels: Record<string, number>): { color: string; width: string }[] {
  const serious = levels.serious ?? 0;
  const warning = levels.warning ?? 0;
  if (!serious && !warning) return [{ color: "success", width: "100%" }];
  // a level with any problem keeps a sliver, so it never reads as clean
  const share = (count: number): string => `${count ? Math.max(3, (count / busiest.value) * 100) : 0}%`;
  return [
    { color: "error", width: share(serious) },
    { color: "warning", width: share(warning) },
  ];
}

/** describeLogPlace returns the mod and the place a log row names. */
function describeLogPlace(finding: Finding): string {
  return [workspace.originName(finding.origin), describeDetail(finding)].filter(Boolean).join(" · ");
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
            <ProblemRow
              v-for="row in lead"
              :key="row.index"
              :level="row.finding.level"
              :message="describeFinding(row.finding, codes)"
              :detail="describeDetail(row.finding)"
              :check="describeCheck(row.finding.code, codes)"
              @open="emit('open', 'compatibility', undefined, row.index)"
            />
            <p v-if="infoChecks" class="flex items-start gap-2.5 px-3 py-2 text-12 text-dimmed">
              <UIcon :name="LEVELS.info.icon" class="mt-0.5 size-3.5 shrink-0" />
              {{ infoChecks }}
            </p>
          </template>
          <template v-else-if="panel.view === 'mod'">
            <template v-for="tally in tallies" :key="tally.mod.id">
              <button
                type="button"
                class="flex items-center gap-2.5 px-3 py-2 text-start hover:bg-accented"
                @click="emit('open', 'mod', tally.mod.id)"
              >
                <ModAvatar :mod="tally.mod" size="sm" />
                <span class="flex min-w-0 grow flex-col gap-1.25">
                  <span class="flex items-center gap-2 text-12">
                    <span class="grow truncate font-medium text-highlighted">{{ tally.mod.descriptor.name }}</span>
                    <LevelCounts :levels="tally.levels" />
                  </span>
                  <span class="flex h-1.5 overflow-hidden rounded-full bg-accented">
                    <span
                      v-for="segment in describeMeter(tally.levels)"
                      :key="segment.color"
                      class="h-full"
                      :style="{ width: segment.width, background: `var(--ui-${segment.color})` }"
                    />
                  </span>
                </span>
              </button>
              <ProblemRow
                v-if="tally.lead"
                :level="tally.lead.finding.level"
                :message="describeFinding(tally.lead.finding, codes)"
                :detail="describeDetail(tally.lead.finding)"
                :check="describeCheck(tally.lead.finding.code, codes)"
                mono
                class="ps-12"
                @open="emit('open', 'mod', tally.mod.id, tally.lead.row)"
              >
                <template v-if="tally.lead.logged" #trailing>
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
              v-for="row in top"
              :key="row.index"
              :message="describeLogLine(row.finding)"
              :detail="describeLogPlace(row.finding)"
              @open="emit('open', 'log', undefined, row.index)"
            >
              <template v-if="topRepeated" #leading>
                <span class="flex w-9 shrink-0 justify-center">
                  <UBadge v-if="row.finding.count > 1" :label="`×${describeCount(row.finding.count)}`" size="sm" />
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
