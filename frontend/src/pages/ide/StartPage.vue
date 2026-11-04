<!--
  The IDE's start page: the workspace, the simple interface's switch, what needs the user in its mods now, ways to
  begin, the files shown last and a few keys worth knowing. A group with no tab shows it too.
-->
<script setup lang="ts">
import { computed, onActivated, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import { keyBy } from "es-toolkit";
import type { RouteLocationRaw } from "vue-router";

import type { Mod } from "@services/workspace/models";
import { findCommand, findKbds } from "../../app/commands";
import { day, describeDumps, joinParts, list, plural } from "../../lib/format";
import { levelsQuery, reportQuery, repositoryWorkQuery, stateQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { gameIcon } from "../../stores/settings";
import { useWindowsStore } from "../../stores/windows";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptDocsGuide from "../../ui/ScriptDocsGuide.vue";
import SettingsSection from "../../ui/SettingsSection.vue";

/** An Action is a button on a line of Where you left off: a page to open or a thing to do, with the keys that do it. */
type Action = { label: string; kbds?: string[]; to?: RouteLocationRaw; onSelect?: () => void };
/** A Line is one thing that needs the user: the color of its dot, what it says and what can be done about it. */
type Line = { id: string; color: string; title: string; note: string; actions: Action[] };

/** The Start list: each row runs a command of the app. */
const STARTS = [
  { id: "editor.goToFile", label: "Open a file…" },
  { id: "editor.findInFiles", label: "Search the files…" },
  { id: "palette.open", label: "Commands and settings…" },
  { id: "game.launch", label: "Launch the game" },
].map((row) => ({ ...row, command: findCommand(row.id) }));
/** Worth knowing: the keys the editor owns, and the commands whose keys are shown as the user set them. */
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
const guiding = shallowRef(false);

const row = computed(() => workspace.active());
const mods = computed(() => workspace.listWritableMods());
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
  KNOWN.map((one) => ({ note: one.note, keys: one.command ? findKbds(one.command) : (one.keys ?? []) })).filter(
    (one) => one.keys.length,
  ),
);
const lines = computed<Line[]>(() => [
  ...mods.value.flatMap(describeVersion),
  ...describeKnowledge(),
  ...mods.value.flatMap(describeProblems),
  ...mods.value.flatMap(describeChanges),
]);

// the page stays mounted behind the others, so what it reads is read again when it is shown
onActivated(() => {
  if (!workspace.isOpen()) return;
  void refreshReport();
  if (roots.value.length) void refreshWork();
});

/** levelColor returns the color a level is painted in. */
function levelColor(id: string): string {
  return `var(--ui-${levels.value?.find((level) => level.id === id)?.color ?? "info"})`;
}

/** describeVersion returns the line for a mod made for another game version, or none when the report has no finding. */
function describeVersion(mod: Mod): Line[] {
  const finding = groups.value[mod.id]?.findings?.find((one) => one.code === "supported-version");
  if (!finding) return [];
  const to = { name: "patch-notes", params: { id: workspace.activeId() } } as const;
  const actions = [{ label: "Open Patch Notes", to }];
  return [
    {
      id: `version:${mod.id}`,
      color: levelColor(finding.level),
      title: finding.label,
      note: joinParts(finding.message),
      actions,
    },
  ];
}

/** describeKnowledge returns the line for the game's documentation not being its own and current, or none. */
function describeKnowledge(): Line[] {
  const dumps = state.value?.open ? describeDumps(state.value.dumps) : undefined;
  if (!dumps || dumps.current) return [];
  const version = state.value?.install.version || "the game";
  const actions = [{ label: "Show me how", onSelect: () => (guiding.value = true) }];
  return [
    {
      id: "knowledge",
      color: "var(--ui-warning)",
      title: dumps.label,
      note: `It may be older than ${version}.`,
      actions,
    },
  ];
}

/** describeProblems returns the line for a mod's errors and warnings, or none when the last scan found no problem. */
function describeProblems(mod: Mod): Line[] {
  const group = groups.value[mod.id];
  const phrases = problemLevels.value.flatMap((level) => group?.phrases?.[level.id] ?? []);
  if (!group || !phrases.length) return [];
  const to = { name: "health", params: { id: workspace.activeId() } } as const;
  const actions = [
    { label: "Problems", kbds: ["meta", "J"], onSelect: () => editor.showPanel("problems") },
    { label: "Open Health", to },
  ];
  const scan = `From the last scan of ${workspace.originName(mod.id)} against ${state.value?.install.version || "the game"}.`;
  return [{ id: `problems:${mod.id}`, color: levelColor(group.worst), title: list(phrases), note: scan, actions }];
}

/** describeChanges returns the line for a mod's files changed since its last commit, or none when it has none. */
function describeChanges(mod: Mod): Line[] {
  const repository = work.value?.[roots.value.indexOf(git.findRoot({ origin: mod.id }))];
  const changed = repository?.status?.changes?.filter((change) => change.place.origin === mod.id) ?? [];
  const commit = repository?.commit;
  if (!changed.length || !commit) return [];
  const named = mods.value.length > 1 ? ` in ${workspace.originName(mod.id)}` : "";
  const actions = [{ label: "Open Source control", onSelect: () => windows.sendCommand("git.commit") }];
  return [
    {
      id: `changes:${mod.id}`,
      color: "var(--fg-3)",
      title: `${plural(changed.length, "file")} changed${named} since your last commit`,
      note: `Last commit ${day(commit.date)}: ${commit.subject}`,
      actions,
    },
  ];
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
            v-for="action in line.actions"
            :key="action.label"
            :label="action.label"
            :to="action.to"
            size="sm"
            color="neutral"
            variant="outline"
            @click="action.onSelect?.()"
          >
            <template v-if="action.kbds" #trailing>
              <UKbd v-for="key in action.kbds" :key="key" :value="key" />
            </template>
          </UButton>
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
          <span v-for="one in known" :key="one.note" class="flex items-center gap-2 text-12 text-toned">
            <span class="flex shrink-0 gap-1"><UKbd v-for="key in one.keys" :key="key" :value="key" /></span>
            {{ one.note }}
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
    <ScriptDocsGuide v-model:open="guiding" :game="state?.game?.id ?? ''" />
  </div>
</template>
