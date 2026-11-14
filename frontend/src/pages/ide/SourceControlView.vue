<!-- The Source Control view: a section for each git repository of the workspace's mods, then the commit Graph. -->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { createReusableTemplate, useInfiniteScroll, useLocalStorage } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";

import type { Change, Commit } from "@services/git/models";

import { commitFilesQuery, commitsQuery, gitStatusQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useWorkspaceStore } from "../../stores/workspace";
import GitFileRow from "../../ui/GitFileRow.vue";
import OriginChip from "../../ui/OriginChip.vue";
import RepositorySection from "./RepositorySection.vue";

/** A GraphRow is one commit in the graph, placed on a lane. */
type GraphRow = {
  commit: Commit;
  lane: number;
  arrives: boolean;
  through: number[];
  joins: number[];
  merges: number[];
  below: number[];
};
/** An Entry is one row of the log: a commit, or a file the open commit changed. */
type Entry = { key: string; row: GraphRow; change?: Change; paths: { path: string; lane: number }[] };

const GIT_DOWNLOADS = "https://git-scm.com/downloads";
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
/** REF_COLORS gives a ref chip's color by ref type. A type not listed, such as a local branch, is primary. */
const REF_COLORS: Partial<Record<string, "info" | "neutral">> = { remote: "info", tag: "neutral" };

const git = useGitStore();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const [DefineLane, Lane] = createReusableTemplate<{ entry: Entry }>();
const [DefineBare, Bare] = createReusableTemplate();

const bare = computed(() =>
  workspace.listWritableMods().filter((mod) => !git.repositories.some((repo) => repo.mods?.includes(mod.id))),
);
// The repository the Graph shows: the one picked, else the active file's, else the first.
const dir = computed(
  () =>
    git.repositories.find((repo) => repo.root === git.graphRoot)?.root ||
    git.findRoot(editor.activeFile()) ||
    (git.repositories[0]?.root ?? ""),
);
const options = computed(() =>
  git.repositories.map(({ root, mods }) => ({
    label: workspace.originName(mods?.[0] ?? ""),
    value: root,
    origin: mods?.[0] ?? "",
  })),
);
const graphOrigin = computed(() => options.value.find((option) => option.value === dir.value)?.origin ?? "");
// The commit box of the first repository is focused, unless the commit command asked for another.
git.commitTarget ||= git.repositories[0]?.root ?? "";
const { data: status } = useQuery(() => ({ ...gitStatusQuery(dir.value), enabled: !!dir.value }));
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
// Another repository's log starts from its first page.
watch(dir, () => {
  limit.value = PAGE;
  opened.value = "";
});

const rows = computed(() => buildGraph(commits.value ?? []));
const graphWidth = computed(
  () =>
    LANE_WIDTH *
      (Math.max(0, ...rows.value.flatMap((row) => [row.lane, ...row.through, ...row.joins, ...row.merges])) + 1) +
    4,
);
const entries = computed<Entry[]>(() =>
  rows.value.flatMap((row) => [
    { key: row.commit.hash, row, paths: buildLines(row) },
    ...(opened.value === row.commit.hash ? (openedFiles.value ?? []) : []).map((file) => ({
      key: `${row.commit.hash}:${file.path}`,
      row,
      change: file,
      paths: row.below.map((lane) => ({ path: `M${laneX(lane)} 0V${ROW_HEIGHT}`, lane })),
    })),
  ]),
);

/** buildGraph returns a GraphRow for each commit of the log: its lane and the other lanes that touch its row. */
function buildGraph(log: readonly Commit[]): GraphRow[] {
  const lanes: string[] = [];
  const freeLane = () => (lanes.includes("") ? lanes.indexOf("") : lanes.length);
  return log.map((commit) => {
    const waiting = lanes.indexOf(commit.hash);
    const lane = waiting >= 0 ? waiting : freeLane();
    const through = lanes.flatMap((hash, i) => (hash && i !== lane && hash !== commit.hash ? [i] : []));
    const joins = lanes.flatMap((hash, i) => (hash === commit.hash && i !== lane ? [i] : []));
    joins.forEach((i) => (lanes[i] = ""));
    const [first = "", ...rest] = commit.parents ?? [];
    lanes[lane] = first;
    const merges = rest.map((parent) => {
      const merged = lanes.includes(parent) ? lanes.indexOf(parent) : freeLane();
      lanes[merged] = parent;
      return merged;
    });
    while (lanes.at(-1) === "") lanes.pop();
    const below = lanes.flatMap((hash, i) => (hash ? [i] : []));
    return { commit, lane, arrives: waiting >= 0, through, joins, merges, below };
  });
}

/** laneX returns the horizontal middle of a lane. */
const laneX = (lane: number) => LANE_WIDTH / 2 + 2 + lane * LANE_WIDTH;
/** laneColor returns the color of a lane. The colors repeat after the last. */
const laneColor = (lane: number) => LANE_COLORS[lane % LANE_COLORS.length];

/** buildLines returns the SVG paths of a graph row, each with its lane. */
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
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <DefineBare>
      <div v-for="mod in bare" :key="mod.id" class="flex items-center gap-2">
        <OriginChip :origin="mod.id" dot />
        <span class="min-w-0 grow truncate">{{ workspace.originName(mod.id) }}</span>
        <UButton label="Initialize" icon="i-lucide-git-branch-plus" size="xs" @click="git.initialize(mod.dir)" />
      </div>
    </DefineBare>
    <DefineLane v-slot="{ entry }">
      <svg
        :viewBox="`0 0 ${graphWidth} ${ROW_HEIGHT}`"
        :style="{ width: `${graphWidth / 16}rem` }"
        class="h-7 shrink-0 overflow-visible"
        aria-hidden="true"
      >
        <path
          v-for="(line, index) in entry.paths"
          :key="index"
          :d="line.path"
          :stroke="laneColor(line.lane)"
          stroke-width="1.5"
          stroke-linecap="square"
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
    <div v-if="!git.isInstalled" class="p-3">
      <div class="flex flex-col gap-3 rounded-lg border border-accented bg-elevated p-3.5">
        <div class="flex items-center gap-2">
          <UIcon name="i-lucide-info" class="size-3.5 shrink-0 text-toned" />
          <span class="text-13 font-semibold text-highlighted">Git isn't installed on this computer</span>
        </div>
        <p class="text-12 text-toned">
          Source control shows your mods' git history: what you changed, and every earlier version of a file. It runs
          the git on your computer, which PMT doesn't include.
        </p>
        <UButton
          label="Get git"
          icon="i-lucide-external-link"
          class="self-start"
          @click="Browser.OpenURL(GIT_DOWNLOADS)"
        />
        <p class="text-12 text-dimmed">PMT looks for git again when it starts. Nothing else in PMT needs it.</p>
      </div>
    </div>
    <div v-else-if="!git.repositories.length" class="flex flex-col gap-1 px-3 py-2.5">
      <p class="pb-1.5 text-dimmed">None of this workspace's mods is in a git repository.</p>
      <Bare />
    </div>
    <USplitter
      v-else
      :items="[
        { id: 'repositories', slot: 'repositories', order: 1, minSize: 20 },
        graphOpen
          ? { id: 'graph', slot: 'graph', order: 2, defaultSize: 35, minSize: 10 }
          : { id: 'folded', slot: 'graph', order: 2, sizeUnit: 'px', defaultSize: 29, minSize: 29, maxSize: 29 },
      ]"
      orientation="vertical"
      :disabled="!graphOpen"
      auto-save-id="pmt.ide.git"
      class="min-h-0 flex-1"
    >
      <template #repositories>
        <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto py-1">
          <RepositorySection v-for="{ root, mods } in git.repositories" :key="root" :root="root" :mods="mods ?? []" />
          <div v-if="bare.length" class="flex flex-col gap-1 px-3 py-2.5">
            <p class="pb-1.5 text-dimmed">Not in a git repository</p>
            <Bare />
          </div>
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
                  editor.open(
                    { key: entry.change.place.key, side: 'commit', root: dir, hash: entry.row.commit.hash },
                    entry.change.place,
                    undefined,
                    'changes',
                  )
                "
              >
                <GitFileRow :change="entry.change" side="commit">
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
                  v-for="ref in entry.row.commit.refs ?? []"
                  :key="ref.name"
                  :label="ref.name"
                  :color="REF_COLORS[ref.type] ?? 'primary'"
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
