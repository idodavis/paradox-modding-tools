<script setup lang="ts">
/**
 * Source control's graph (O-R60): the current branch's and its upstream's commits, each a dot on its lane with lines
 * to its parents, its subject, the branches and tags at it and its short hash. A commit opens to the files it
 * changed, each opening the commit's diff; the lanes run on past them. It scrolls on its own, and loads older
 * commits as its end comes into view.
 */
import { computed, shallowRef, useTemplateRef } from "vue";
import { useQuery } from "@pinia/colada";
import { useInfiniteScroll } from "@vueuse/core";
import type { GitCommit } from "@services/models";
import { gitCommitFilesQuery } from "../../queries";
import { useGitStore } from "../../stores/git";
import { diffTab, useIdeStore } from "../../stores/ide";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginDot from "../../ui/OriginDot.vue";

const git = useGitStore();
const ide = useIdeStore();
const settings = useSettingsStore();
const store = useWorkspaceStore();

/** A commit's row: its lane; the lanes passing by, ending in it and its later parents start; those going on below. */
type Row = {
  commit: GitCommit;
  lane: number;
  top: boolean;
  through: number[];
  joins: number[];
  merges: number[];
  below: number[];
};

// each lane waits for a commit (the parent of one drawn above it), "" when free; a commit takes the lane waiting for
// it, or a free one when it is a branch's tip
const rows = computed<Row[]>(() => {
  const lanes: string[] = [];
  const free = () => (lanes.includes("") ? lanes.indexOf("") : lanes.length);
  return (git.log ?? []).map((commit) => {
    const waiting = lanes.indexOf(commit.hash);
    const lane = waiting >= 0 ? waiting : free();
    const through = lanes.flatMap((hash, i) => (hash && hash !== commit.hash ? [i] : []));
    const joins = lanes.flatMap((hash, i) => (hash === commit.hash && i !== lane ? [i] : []));
    for (const i of joins) lanes[i] = "";
    const [first = "", ...rest] = commit.parents ?? [];
    lanes[lane] = first;
    const merges = rest.map((parent) => {
      const to = lanes.includes(parent) ? lanes.indexOf(parent) : free();
      lanes[to] = parent;
      return to;
    });
    while (lanes.at(-1) === "") lanes.pop();
    const below = lanes.flatMap((hash, i) => (hash ? [i] : []));
    return { commit, lane, top: waiting >= 0, through, joins, merges, below };
  });
});

const LANE = 12;
const width = computed(
  () =>
    LANE * (Math.max(0, ...rows.value.flatMap((row) => [row.lane, ...row.through, ...row.joins, ...row.merges])) + 1) +
    4,
);
const x = (lane: number) => 2 + LANE / 2 + lane * LANE;
const COLORS = [
  "var(--accent)",
  "var(--link)",
  "var(--purple)",
  "var(--orange)",
  "var(--blue)",
  "var(--green)",
  "var(--red)",
];
const color = (lane: number) => COLORS[lane % COLORS.length];

/** A row's lines, each in its lane's color: passing by, ending in the dot, from the dot to its parents. */
function lines(row: Row): { d: string; lane: number }[] {
  const h = settings.treeRow;
  const mid = h / 2;
  const at = x(row.lane);
  return [
    ...row.through.map((lane) => ({ d: `M${x(lane)} 0V${h}`, lane })),
    ...row.joins.map((lane) => ({ d: `M${x(lane)} 0Q${x(lane)} ${mid} ${at} ${mid}`, lane })),
    ...(row.top ? [{ d: `M${at} 0V${mid}`, lane: row.lane }] : []),
    ...(row.commit.parents?.length ? [{ d: `M${at} ${mid}V${h}`, lane: row.lane }] : []),
    ...row.merges.map((lane) => ({ d: `M${at} ${mid}Q${x(lane)} ${mid} ${x(lane)} ${h}`, lane })),
  ];
}

// a branch at a commit in the accent, a remote's in the link color, a tag plain; a remote's HEAD is left out
function labels(commit: GitCommit): { label: string; color: "primary" | "info" | "neutral" }[] {
  const remote = (ref: string) => git.branches?.remote?.some((branch) => branch.name === ref);
  return (commit.refs ?? [])
    .filter((ref) => !ref.endsWith("/HEAD"))
    .map((ref) =>
      ref.startsWith("tag: ")
        ? { label: ref.slice(5), color: "neutral" }
        : { label: ref, color: remote(ref) ? "info" : "primary" },
    );
}

// the commit opened, and the files it changed
const open = shallowRef("");
const { data: files } = useQuery(() => gitCommitFilesQuery({ root: git.root, hash: open.value }));
const origin = computed(() => store.originColor(git.repo?.origin ?? ""));

const list = useTemplateRef<HTMLElement>("list");
useInfiniteScroll(list, git.loadMore, { distance: 80, canLoadMore: () => git.hasMore });

const ROW =
  "group flex h-(--row) w-full cursor-pointer items-center gap-2 pe-2.5 text-start whitespace-nowrap text-toned hover:bg-elevated";
const LETTERS: Record<string, string> = { A: "text-success", M: "text-warning", R: "text-info", D: "text-error" };
</script>

<template>
  <div ref="list" class="min-h-0 flex-1 overflow-y-auto">
    <template v-for="row in rows" :key="row.commit.hash">
      <div
        :class="[ROW, { 'bg-accented': open === row.commit.hash }]"
        :title="row.commit.subject"
        @click="open = open === row.commit.hash ? '' : row.commit.hash"
      >
        <svg :width="width" :height="settings.treeRow" class="shrink-0" aria-hidden="true">
          <path
            v-for="(line, i) in lines(row)"
            :key="i"
            :d="line.d"
            :stroke="color(line.lane)"
            stroke-width="1.5"
            fill="none"
          />
          <circle
            :cx="x(row.lane)"
            :cy="settings.treeRow / 2"
            :r="row.commit.head ? 4.5 : 3.5"
            :fill="color(row.lane)"
          />
        </svg>
        <span class="min-w-0 grow truncate" :class="{ 'font-medium text-highlighted': row.commit.head }">{{
          row.commit.subject
        }}</span>
        <UBadge
          v-for="one in labels(row.commit)"
          :key="one.label"
          :label="one.label"
          :color="one.color"
          size="sm"
          class="max-w-30 shrink-0"
          :ui="{ label: 'truncate' }"
        />
        <span class="shrink-0 font-mono text-11 text-dimmed">{{ row.commit.short }}</span>
      </div>
      <template v-if="open === row.commit.hash">
        <div
          v-for="change in files"
          :key="change.path"
          :class="ROW"
          @click="ide.open(diffTab('commit', change.path, row.commit.hash))"
        >
          <svg :width="width" :height="settings.treeRow" class="shrink-0" aria-hidden="true">
            <path
              v-for="lane in row.below"
              :key="lane"
              :d="`M${x(lane)} 0V${settings.treeRow}`"
              :stroke="color(lane)"
              stroke-width="1.5"
            />
          </svg>
          <OriginDot :color="origin" small />
          <span class="min-w-0 grow truncate"
            ><span class="text-highlighted">{{ change.name }}</span>
            <span class="text-dimmed">{{ change.folder }}</span></span
          >
          <b class="w-3.5 shrink-0 text-center font-mono font-medium" :class="LETTERS[change.letter]">{{
            change.letter
          }}</b>
        </div>
      </template>
    </template>
    <p v-if="git.log && !git.log.length" class="px-3 py-1 text-dimmed">No commits yet.</p>
  </div>
</template>
