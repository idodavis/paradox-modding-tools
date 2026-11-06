<!-- The IDE's start page: what needs the user, ways to begin, the files opened last and a few keys worth knowing. -->
<script setup lang="ts">
import { computed, onActivated } from "vue";
import { useQuery } from "@pinia/colada";
import { keyBy, sumBy } from "es-toolkit";
import type { RouteLocationRaw } from "vue-router";

import type { Mod } from "@services/workspace/models";
import { findCommand, findKbds } from "../../app/commands";
import { describeDumps, findOwner, list, plural } from "../../lib/format";
import { levelsQuery, reportQuery, repositoryWorkQuery, stateQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { gameIcon } from "../../stores/settings";
import { useWindowsStore } from "../../stores/windows";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import SettingsSection from "../../ui/SettingsSection.vue";

/** An Action is the button on a line of Where you left off: a page to open or a thing to do. */
type Action = { label: string; to?: RouteLocationRaw; onSelect?: () => void };
/** A Line is one page, or the game's documentation, that needs the user: its dot's color, what it says, its action. */
type Line = { id: string; color: string; title: string; note: string; action: Action };

/** The most mods a line names; the rest are counted. */
const MODS_NAMED = 3;
/** The Start list. Each row runs a command of the app. */
const STARTS = [
  { id: "editor.goToFile", label: "Open a file…" },
  { id: "editor.findInFiles", label: "Search the files…" },
  { id: "palette.open", label: "Commands and settings…" },
  { id: "game.launch", label: "Launch the game" },
].map((start) => ({ ...start, command: findCommand(start.id) }));
/** Worth knowing: keys the editor owns, and commands shown with the keys the user set. */
const KNOWN: { note: string; keys?: string[]; command?: string }[] = [
  { keys: ["Hover"], note: "what a word is" },
  { keys: ["Ctrl", "Click"], note: "where it is defined" },
  { keys: ["F2"], note: "rename it everywhere" },
  { command: "editor.back", note: "back to where you were" },
  { command: "editor.toggleWiki", note: "the Wiki for this file" },
  { command: "editor.openGraph", note: "the event in the graph" },
];

const editor = useEditorStore();
const git = useGitStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const GUIDE = findCommand("docs.guide");

const row = computed(() => workspace.active());
const mods = computed(() => workspace.listOwnMods());
const roots = computed(() => [...new Set(mods.value.flatMap((mod) => git.findRoot({ origin: mod.id }) || []))]);

const { data: state } = useQuery(stateQuery);
const { data: levels } = useQuery(levelsQuery);
const { data: report, refresh: refreshReport } = useQuery(() => ({
  ...reportQuery(workspace.listModIds()),
  enabled: workspace.isOpen(),
}));
const { data: work, refresh: refreshWork } = useQuery(() => ({
  ...repositoryWorkQuery(roots.value),
  enabled: workspace.isOpen() && roots.value.length > 0,
}));

const groups = computed(() => keyBy(report.value?.mods ?? [], (group) => group.mod));
const problemLevels = computed(() => (levels.value ?? []).filter((level) => level.problem));
const recent = computed(() => editor.listRecent());
const known = computed(() =>
  KNOWN.map((hint) => ({ note: hint.note, keys: hint.command ? findKbds(hint.command) : (hint.keys ?? []) })).filter(
    (hint) => hint.keys.length,
  ),
);
const lines = computed<Line[]>(() =>
  [describePatchNotes(), describeKnowledge(), describeHealth(), describeSourceControl()].filter(
    (line) => line !== undefined,
  ),
);

// The page stays mounted behind the others, so it loads again each time it is shown.
onActivated(() => {
  if (!workspace.isOpen()) return;
  void refreshReport();
  if (roots.value.length) void refreshWork();
});

/** levelColor returns the color a level is painted in. It is the info color for an unknown level. */
function levelColor(id: string | undefined): string {
  return `var(--ui-${levels.value?.find((level) => level.id === id)?.color ?? "info"})`;
}

/** describeMods names the given mods, then counts the rest. */
function describeMods(ids: string[]): string {
  const names = ids.slice(0, MODS_NAMED).map((id) => workspace.originName(id));
  return ids.length > MODS_NAMED ? `${names.join(", ")} and ${ids.length - MODS_NAMED} more…` : list(names);
}

/** describePatchNotes returns the line for the mods made for another game version, or none when the report has none. */
function describePatchNotes(): Line | undefined {
  const findings = mods.value.flatMap(
    (mod) => groups.value[mod.id]?.findings?.filter((finding) => finding.code === "supported-version") ?? [],
  );
  const [first] = findings;
  if (!first) return undefined;
  return {
    id: "patch-notes",
    color: levelColor(first.level),
    title: first.label,
    note: describeMods(findings.map(findOwner)),
    action: { label: "Open Patch Notes", to: { name: "patch-notes", params: { id: workspace.activeId() } } },
  };
}

/** describeKnowledge returns the line for the game's documentation not being its own and current, or none. */
function describeKnowledge(): Line | undefined {
  const dumps = state.value?.open ? describeDumps(state.value.dumps) : undefined;
  if (!dumps || dumps.current) return undefined;
  return {
    id: "knowledge",
    color: "var(--ui-warning)",
    title: dumps.label,
    note: `It may be older than ${state.value?.install.version || "the game"}.`,
    action: { label: "Show me how", onSelect: () => GUIDE?.onSelect?.() },
  };
}

/** describeHealth returns the line for the mods with errors or warnings in the last scan, or none when none has. */
function describeHealth(): Line | undefined {
  const flagged = mods.value.filter((mod) =>
    problemLevels.value.some((level) => groups.value[mod.id]?.phrases?.[level.id]),
  );
  if (!flagged.length) return undefined;
  const worst = problemLevels.value.find((level) => flagged.some((mod) => groups.value[mod.id]?.worst === level.id));
  return {
    id: "health",
    color: levelColor(worst?.id),
    title: `Problems in the last scan against ${state.value?.install.version || "the game"}`,
    note: describeMods(flagged.map((mod) => mod.id)),
    action: { label: "Open Health", to: { name: "health", params: { id: workspace.activeId() } } },
  };
}

/** describeSourceControl returns the line for the mods with files changed since their last commit, or none. */
function describeSourceControl(): Line | undefined {
  const changed = mods.value.filter((mod) => countChanges(mod));
  if (!changed.length) return undefined;
  return {
    id: "source-control",
    color: "var(--fg-3)",
    title: `${plural(sumBy(changed, countChanges), "file")} changed since your last commit`,
    note: describeMods(changed.map((mod) => mod.id)),
    action: { label: "Open Source control", onSelect: () => windows.sendCommand("git.commit") },
  };
}

/** countChanges returns how many files of the given mod changed since its repository's last commit. It is 0 if none. */
function countChanges(mod: Mod): number {
  const repository = work.value?.[roots.value.indexOf(git.findRoot({ origin: mod.id }))];
  if (!repository?.commit) return 0;
  return repository.status?.changes?.filter((change) => change.place.origin === mod.id).length ?? 0;
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col items-center overflow-y-auto bg-(--bg-0) px-6 py-9">
    <div class="flex w-205 max-w-full flex-col gap-6.5">
      <div v-if="row" class="flex items-center gap-3.5">
        <img v-if="row.game" :src="gameIcon(row.game.id)" :alt="row.game.name" class="size-10 object-contain" />
        <div class="flex min-w-0 flex-col gap-0.5">
          <h1 class="truncate text-20">{{ row.record.name }}</h1>
          <span class="text-12 text-dimmed">{{ [row.game?.name, row.install.version].filter(Boolean).join(" ") }}</span>
        </div>
      </div>

      <SettingsSection section="appearance" :keys="['appearance.simple']" />

      <section v-if="lines.length" class="flex flex-col rounded-lg border border-default bg-muted">
        <div class="flex h-10 items-center gap-2 border-b border-default px-3.5">
          <UIcon name="i-lucide-flag" class="size-3.5 text-toned" />
          <h2 class="text-13 font-semibold">Where you left off</h2>
          <span class="grow" />
          <span class="text-12 text-dimmed">
            What needs you in {{ mods.length > 1 ? "these mods" : "this mod" }} now
          </span>
        </div>
        <div
          v-for="line in lines"
          :key="line.id"
          class="flex min-h-12 items-center gap-3 border-b border-default px-3.5 py-1.5 last:border-b-0"
        >
          <span class="size-2 shrink-0 rounded-full" :style="{ background: line.color }" />
          <div class="flex min-w-0 grow flex-col gap-px">
            <span class="text-13">{{ line.title }}</span>
            <span class="text-12 text-dimmed">{{ line.note }}</span>
          </div>
          <UButton
            :label="line.action.label"
            :to="line.action.to"
            size="sm"
            color="neutral"
            variant="outline"
            @click="line.action.onSelect?.()"
          />
        </div>
      </section>

      <div class="grid grid-cols-2 gap-8">
        <section class="flex flex-col gap-1">
          <h2 class="px-2 pb-1 text-12 font-semibold text-dimmed">Start</h2>
          <UButton
            v-for="start in STARTS"
            :key="start.id"
            :label="start.label"
            :icon="start.command?.icon"
            block
            variant="ghost"
            color="neutral"
            class="h-7.5 justify-start"
            @click="start.command?.onSelect?.($event)"
          >
            <template #trailing>
              <span class="ms-auto flex gap-1">
                <UKbd v-for="key in findKbds(start.id)" :key="key" :value="key" />
              </span>
            </template>
          </UButton>
        </section>
        <section class="flex flex-col gap-1">
          <h2 class="px-2 pb-1 text-12 font-semibold text-dimmed">Recent</h2>
          <UButton
            v-for="place in recent"
            :key="place.key"
            block
            variant="ghost"
            color="neutral"
            class="h-7.5 justify-start"
            @click="editor.show(place)"
          >
            <OriginChip :origin="place.origin" dot />
            <span class="truncate">{{ place.name }}</span>
            <UIcon v-if="place.readOnly" name="i-lucide-lock" class="size-3 shrink-0 text-dimmed" />
            <span class="ms-auto truncate font-mono text-12 text-dimmed">{{ place.folder }}</span>
          </UButton>
          <p v-if="!recent.length" class="px-2 text-12 text-dimmed">Files you open show here.</p>
        </section>
      </div>

      <section v-if="known.length" class="flex flex-col gap-2.5">
        <h2 class="text-12 font-semibold text-dimmed">Worth knowing</h2>
        <div class="grid grid-cols-3 gap-x-6 gap-y-2.5">
          <span v-for="hint in known" :key="hint.note" class="flex items-center gap-2 text-12 text-toned">
            <span class="flex shrink-0 gap-1"><UKbd v-for="key in hint.keys" :key="key" :value="key" /></span>
            {{ hint.note }}
          </span>
        </div>
      </section>

      <UCheckbox
        v-if="row"
        :model-value="!row.record.hideStartPage"
        label="Open this when the workspace opens"
        @update:model-value="workspace.update({ ...row.record, hideStartPage: $event !== true })"
      />
    </div>
  </div>
</template>
