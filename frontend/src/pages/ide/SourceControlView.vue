<!--
  The Source Control view: a section for each git repository of the workspace's mods, in load order, then the Graph
  of the log of the repository picked in its header.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, useInfiniteScroll, useLocalStorage } from "@vueuse/core";
import type { DropdownMenuItem } from "@nuxt/ui";

import type { Change, Commit } from "@services/git/models";

import { commitFilesQuery, commitsQuery, gitStatusQuery, refsQuery, stateQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useWorkspaceStore } from "../../stores/workspace";
import GitFileRow from "../../ui/GitFileRow.vue";
import OriginChip from "../../ui/OriginChip.vue";
import RepositorySection from "./RepositorySection.vue";
import { diffTab } from "./editor/tabs";

/** One commit in the graph, placed on a lane. */
type GraphRow = {
  commit: Commit;
  lane: number;
  arrives: boolean;
  through: number[];
  joins: number[];
  merges: number[];
  below: number[];
};
/** One row of the log: a commit, or a file the open commit changed. */
type Entry = { key: string; row: GraphRow; change?: Change; paths: { path: string; lane: number }[] };

const LANE_WIDTH = 12;
const ROW_HEIGHT = 28;
const PAGE = 200;
const LANE_COLORS = [
  "var(--accent)",
  "var(--link)",
  "var(--purple)",
  "var(--orange)",
  "var(--blue)",
  "var(--green)",
  "var(--red)",
];

const git = useGitStore();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const [DefineLane, Lane] = createReusableTemplate<{ entry: Entry }>();

const { data: state } = useQuery(stateQuery);
// the mods in no repository, each of which can start one
const bare = computed(() =>
  (state.value?.mods ?? []).filter(
    (mod) => !mod.broken && !git.repositories.some((one) => one.mods.some((member) => member.id === mod.id)),
  ),
);
// the repository the Graph shows: the one picked in its header, else the active file's, else the first
const dir = computed(
  () =>
    git.repositories.find((one) => one.root === git.graphRoot)?.root ||
    git.findRoot(editor.activeFile()) ||
    (git.repositories[0]?.root ?? ""),
);
// each repository is offered under its first mod in load order
const options = computed(() =>
  git.repositories.map(({ root, mods: [first] }) => ({
    label: workspace.originName(first?.id ?? ""),
    value: root,
    origin: first?.id ?? "",
  })),
);
const graphOrigin = computed(() => options.value.find((one) => one.value === dir.value)?.origin ?? "");
// the view opens with the first repository's commit box focused, unless the commit command asked for another
git.commitTarget ||= git.repositories[0]?.root ?? "";
const { data: status } = useQuery(() => ({ ...gitStatusQuery(dir.value), enabled: !!dir.value }));
const { data: refs } = useQuery(() => ({ ...refsQuery(dir.value), enabled: !!dir.value && !!status.value }));
const limit = shallowRef(PAGE);
const { data: commits } = useQuery(() => ({
  ...commitsQuery({
    dir: dir.value,
    refs: [status.value?.branch ?? "", status.value?.upstream ?? ""].filter(Boolean),
    limit: limit.value,
  }),
  enabled: !!dir.value && !!status.value,
  placeholderData: (previous) => previous,
}));
const opened = shallowRef("");
const { data: openedFiles } = useQuery(() => ({
  ...commitFilesQuery({ dir: dir.value, hash: opened.value }),
  enabled: !!dir.value && !!opened.value,
}));

const graphOpen = useLocalStorage("pmt.ide.graph", true);
const graph = useTemplateRef("graph");
useInfiniteScroll(
  graph,
  () => {
    limit.value += PAGE;
  },
  { distance: 80, canLoadMore: () => (commits.value?.length ?? 0) >= limit.value },
);
// another repository's log starts from its first page, with no commit open
watch(dir, () => {
  limit.value = PAGE;
  opened.value = "";
});

const rows = computed(() => buildGraph(commits.value ?? []));
const laneCount = computed(
  () => Math.max(0, ...rows.value.flatMap((row) => [row.lane, ...row.through, ...row.joins, ...row.merges])) + 1,
);
const entries = computed<Entry[]>(() =>
  rows.value.flatMap((row) => [
    { key: row.commit.hash, row, paths: buildLines(row) },
    ...(opened.value === row.commit.hash ? (openedFiles.value ?? []) : []).map((file) => ({
      key: `${row.commit.hash}:${file.path}`,
      row,
      change: { ...file, staged: false, unstaged: false },
      paths: row.below.map((lane) => ({ path: `M${laneX(lane)} 0V${ROW_HEIGHT}`, lane })),
    })),
  ]),
);

const initializeMenu = computed<DropdownMenuItem[]>(() =>
  bare.value.map((mod) => ({
    label: `Initialize ${workspace.originName(mod.id)}`,
    origin: mod.id,
    onSelect: () => git.initialize(mod.dir),
  })),
);

/**
 * buildGraph gives each commit its lane and the lanes that touch its row. A lane waits for the commit its line leads
 * to.
 */
function buildGraph(log: readonly Commit[]): GraphRow[] {
  const lanes: string[] = [];
  const freeLane = () => (lanes.includes("") ? lanes.indexOf("") : lanes.length);
  return log.map((one) => {
    const waiting = lanes.indexOf(one.hash);
    const lane = waiting >= 0 ? waiting : freeLane();
    const through = lanes.flatMap((hash, i) => (hash && i !== lane && hash !== one.hash ? [i] : []));
    const joins = lanes.flatMap((hash, i) => (hash === one.hash && i !== lane ? [i] : []));
    joins.forEach((i) => (lanes[i] = ""));
    const [first = "", ...rest] = one.parents ?? [];
    lanes[lane] = first;
    const merges = rest.map((parent) => {
      const merged = lanes.includes(parent) ? lanes.indexOf(parent) : freeLane();
      lanes[merged] = parent;
      return merged;
    });
    while (lanes.at(-1) === "") lanes.pop();
    const below = lanes.flatMap((hash, i) => (hash ? [i] : []));
    return { commit: one, lane, arrives: waiting >= 0, through, joins, merges, below };
  });
}

/** The horizontal middle of a lane. */
const laneX = (lane: number) => LANE_WIDTH / 2 + 2 + lane * LANE_WIDTH;
/** The color of a lane. The colors repeat after the last. */
const laneColor = (lane: number) => LANE_COLORS[lane % LANE_COLORS.length];

/**
 * buildLines returns the lines of a graph row, each in its lane's color: lanes passing, joining, going on and leaving
 * for a merge.
 */
function buildLines(row: GraphRow): { path: string; lane: number }[] {
  const middle = ROW_HEIGHT / 2;
  const dot = laneX(row.lane);
  return [
    ...row.through.map((lane) => ({ path: `M${laneX(lane)} 0V${ROW_HEIGHT}`, lane })),
    ...row.joins.map((lane) => ({ path: `M${laneX(lane)} 0Q${laneX(lane)} ${middle} ${dot} ${middle}`, lane })),
    ...(row.arrives ? [{ path: `M${dot} 0V${middle}`, lane: row.lane }] : []),
    ...(row.commit.parents?.length ? [{ path: `M${dot} ${middle}V${ROW_HEIGHT}`, lane: row.lane }] : []),
    ...row.merges.map((lane) => ({
      path: `M${dot} ${middle}Q${laneX(lane)} ${middle} ${laneX(lane)} ${ROW_HEIGHT}`,
      lane,
    })),
  ];
}

/**
 * describeRefs returns the refs at a commit as chips: a branch in the accent, a remote branch in the info color, a
 * tag plain.
 */
function describeRefs(one: Commit): { label: string; color: "primary" | "info" | "neutral" }[] {
  const remoteNames = new Set((refs.value ?? []).filter((ref) => ref.type === "remote").map((ref) => ref.name));
  return (one.refs ?? [])
    .filter((ref) => !ref.endsWith("/HEAD"))
    .map((ref) => ref.replace(/^HEAD -> /, ""))
    .map((ref) =>
      ref.startsWith("tag: ")
        ? { label: ref.slice(5), color: "neutral" }
        : { label: ref, color: remoteNames.has(ref) ? "info" : "primary" },
    );
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <DefineLane v-slot="{ entry }">
      <svg :width="LANE_WIDTH * laneCount + 4" :height="ROW_HEIGHT" class="shrink-0" aria-hidden="true">
        <path
          v-for="(line, index) in entry.paths"
          :key="index"
          :d="line.path"
          :stroke="laneColor(line.lane)"
          stroke-width="1.5"
          fill="none"
        />
        <circle
          v-if="!entry.change"
          :cx="laneX(entry.row.lane)"
          :cy="ROW_HEIGHT / 2"
          r="3.5"
          :fill="laneColor(entry.row.lane)"
        />
      </svg>
    </DefineLane>
    <Teleport v-if="git.repositories.length && bare.length" defer to="#sidebar-actions">
      <UDropdownMenu :items="initializeMenu" :content="{ align: 'end' }">
        <UButton icon="i-lucide-git-branch-plus" variant="ghost" square aria-label="Initialize a repository" />
        <template #item-leading="{ item }">
          <OriginChip v-if="'origin' in item" :origin="String(item.origin)" dot />
        </template>
      </UDropdownMenu>
      <USeparator orientation="vertical" class="mx-0.5 h-4" />
    </Teleport>
    <div v-if="!git.repositories.length" class="flex flex-col gap-1 px-3 py-2.5">
      <p class="pb-1.5 text-dimmed">None of this workspace's mods is in a git repository.</p>
      <div v-for="mod in bare" :key="mod.id" class="flex items-center gap-2">
        <OriginChip :origin="mod.id" dot />
        <span class="min-w-0 grow truncate">{{ workspace.originName(mod.id) }}</span>
        <UButton label="Initialize" icon="i-lucide-git-branch-plus" size="xs" @click="git.initialize(mod.dir)" />
      </div>
    </div>
    <USplitter
      v-else
      :items="[
        { id: 'repositories', slot: 'repositories', order: 1, sizeUnit: 'px', minSize: 200 },
        graphOpen
          ? { id: 'graph', slot: 'graph', order: 2, sizeUnit: 'px', defaultSize: 240, minSize: 80 }
          : { id: 'folded', slot: 'graph', order: 2, sizeUnit: 'px', defaultSize: 29, minSize: 29, maxSize: 29 },
      ]"
      orientation="vertical"
      :disabled="!graphOpen"
      auto-save-id="pmt.ide.git"
      class="min-h-0 flex-1"
    >
      <template #repositories>
        <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto py-1">
          <RepositorySection v-for="{ root, mods } in git.repositories" :key="root" :root="root" :mods="mods" />
        </div>
      </template>
      <template #graph>
        <div class="flex min-h-0 min-w-0 flex-1 flex-col border-t border-default">
          <div class="flex h-7 shrink-0 items-center pe-1.5">
            <button
              type="button"
              class="flex h-7 grow cursor-pointer items-center gap-1 px-1.5 text-11 font-semibold whitespace-nowrap text-toned hover:text-highlighted"
              :aria-expanded="graphOpen"
              @click="graphOpen = !graphOpen"
            >
              <UIcon :name="graphOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-3.5" />Graph
            </button>
            <USelect
              :model-value="dir"
              :items="options"
              variant="ghost"
              size="xs"
              class="max-w-40 min-w-0"
              aria-label="The repository the Graph shows"
              @update:model-value="(root) => (git.graphRoot = root)"
            >
              <template #leading>
                <OriginChip :origin="graphOrigin" dot />
              </template>
              <template #item-leading="{ item }">
                <OriginChip :origin="item.origin" dot />
              </template>
            </USelect>
          </div>
          <div v-if="graphOpen" ref="graph" class="min-h-0 flex-1 overflow-y-auto">
            <template v-for="entry in entries" :key="entry.key">
              <div
                v-if="entry.change"
                class="contents"
                @click="
                  editor.open(diffTab('commit', dir, entry.change.place.key, entry.row.commit.hash), entry.change.place)
                "
              >
                <GitFileRow :change="entry.change">
                  <template #lane><Lane :entry="entry" /></template>
                </GitFileRow>
              </div>
              <div
                v-else
                class="flex h-7 w-full cursor-pointer items-center gap-2 pe-2.5 whitespace-nowrap text-toned hover:bg-elevated"
                :class="{ 'bg-accented': opened === entry.row.commit.hash }"
                @click="opened = opened === entry.row.commit.hash ? '' : entry.row.commit.hash"
              >
                <Lane :entry="entry" />
                <span class="min-w-0 grow truncate" :title="entry.row.commit.subject">{{
                  entry.row.commit.subject
                }}</span>
                <UBadge
                  v-for="chip in describeRefs(entry.row.commit)"
                  :key="chip.label"
                  :label="chip.label"
                  :color="chip.color"
                  size="sm"
                  class="max-w-30 shrink-0"
                  :ui="{ label: 'truncate' }"
                />
                <span class="shrink-0 font-mono text-11 text-dimmed">{{ entry.row.commit.short }}</span>
              </div>
            </template>
            <p v-if="commits && !commits.length" class="px-3 py-1 text-dimmed">No commits yet.</p>
          </div>
        </div>
      </template>
    </USplitter>
  </div>
</template>
