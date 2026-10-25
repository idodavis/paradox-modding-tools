<!--
  The Source Control view: the repository, the staged and unstaged changes, the commit box, the branch menu, and the
  log of the current branch and its upstream.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { createReusableTemplate, useInfiniteScroll, useLocalStorage } from "@vueuse/core";
import type { CommandPaletteGroup, CommandPaletteItem, DropdownMenuItem } from "@nuxt/ui";

import * as gitApi from "@services/git/service";
import type { Change, Commit } from "@services/git/models";
import type { Mod, Place } from "@services/workspace/models";

import { plural } from "../../lib/format";
import {
  commitFilesQuery,
  commitsQuery,
  gitStatusQuery,
  keys,
  loadQuery,
  refsQuery,
  repositoryQuery,
  stateQuery,
} from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useWorkspaceStore } from "../../stores/workspace";
import ConfirmModal from "../../ui/ConfirmModal.vue";
import GitFileRow from "../../ui/GitFileRow.vue";
import OriginChip from "../../ui/OriginChip.vue";
import { diffTab, parseTab } from "./editor/tabs";

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
/** What the branch menu lists. */
type Mode = "main" | "name" | "start" | "tag" | "pull" | "merge" | "delete";
/** The side of a file's diff, as a diff tab names it. */
type Side = Parameters<typeof diffTab>[0];
/** A list of changed files with the buttons that act on all of them. */
type Section = {
  title: string;
  rows: Change[];
  side?: "staged" | "changes";
  all: { label: string; icon: string; color?: "error"; run: (rows: Change[]) => void }[];
};

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
const PLACEHOLDERS: Record<Mode, string> = {
  main: "Switch branch, or type a name to create one",
  name: "Name the new branch",
  start: "The branch to make it from",
  tag: "A tag or commit",
  pull: "The remote branch to pull",
  merge: "The branch to merge",
  delete: "The branch to delete",
};
const SYNC = [
  { label: "Fetch", icon: "i-lucide-cloud-download", args: ["fetch"] },
  { label: "Pull", icon: "i-lucide-arrow-down-to-line", args: ["pull"] },
  { label: "Push", icon: "i-lucide-arrow-up-from-line", args: ["push"] },
] as const;
const SECTION = "flex h-7 shrink-0 items-center gap-1.5 ps-3 pe-1.5 text-11 font-semibold whitespace-nowrap text-toned";

const git = useGitStore();
const editor = useEditorStore();
const workspace = useWorkspaceStore();
const queryCache = useQueryCache();
const toast = useToast();
const confirm = useOverlay().create(ConfirmModal);
const [DefineLane, Lane] = createReusableTemplate<{ entry: Entry }>();

const dir = computed(() => git.repository());
const { data: state } = useQuery(stateQuery);
const mods = computed(() => state.value?.mods ?? []);
// each mod with the root of its repository, "" where it is in none
const repositories = shallowRef<{ mod: Mod; root: string }[]>([]);
const inGit = computed(() => repositories.value.filter((one) => one.root));
const bare = computed(() => repositories.value.filter((one) => !one.root));
const origin = computed(() => mods.value.find((mod) => mod.dir === dir.value)?.id ?? "");
const { data: status, error } = useQuery(() => ({ ...gitStatusQuery(dir.value), enabled: !!dir.value }));
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

const message = shallowRef("");
const branchMenu = shallowRef(false);
const mode = shallowRef<Mode>("main");
const term = shallowRef("");
const base = shallowRef("");
const remoting = shallowRef(false);
const remote = shallowRef({ label: "origin", url: "" });
const graphOpen = useLocalStorage("pmt.ide.graph", true);
const panel = useLocalStorage("pmt.ide.panel", "");
const graph = useTemplateRef("graph");
useInfiniteScroll(
  graph,
  () => {
    limit.value += PAGE;
  },
  { distance: 80, canLoadMore: () => (commits.value?.length ?? 0) >= limit.value },
);
watch(mods, listRepositories, { immediate: true });

const changes = computed(() => status.value?.changes ?? []);
const sections = computed<Section[]>(() => [
  {
    title: "Merge conflicts",
    rows: changes.value.filter((c) => c.status === "conflict").map((c) => ({ ...c, staged: false, unstaged: true })),
    all: [],
  },
  {
    title: "Staged",
    rows: changes.value.filter((c) => c.staged && c.status !== "conflict").map((c) => ({ ...c, unstaged: false })),
    side: "staged",
    all: [{ label: "Unstage all", icon: "i-lucide-minus", run: (rows) => change(gitApi.Unstage, rows) }],
  },
  {
    title: "Changes",
    rows: changes.value.filter((c) => c.unstaged && c.status !== "conflict").map((c) => ({ ...c, staged: false })),
    side: "changes",
    all: [
      { label: "Discard all changes", icon: "i-lucide-undo-2", color: "error", run: (rows) => discard(rows) },
      { label: "Stage all changes", icon: "i-lucide-plus", run: (rows) => change(gitApi.Stage, rows) },
    ],
  },
]);
const stagedCount = computed(() => changes.value.filter((c) => c.staged).length);
const canCommit = computed(() => !!message.value.trim() && stagedCount.value > 0);

const current = computed(() => status.value?.branch || "HEAD");
const local = computed(() => (refs.value ?? []).filter((ref) => ref.type === "branch"));
const remotes = computed(() => (refs.value ?? []).filter((ref) => ref.type === "remote"));
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

const { mutate: run } = useMutation({
  mutation: (work: () => Promise<unknown>) => work(),
  onSettled: () => queryCache.invalidateQueries({ key: keys.git(dir.value) }),
});
const { mutate: listRemotes, data: remoteList } = useMutation({
  mutation: () => gitApi.Run(dir.value, "remote", "-v"),
});
const { mutate: commit } = useMutation({
  mutation: async ({ amend = false, push = false }: { amend?: boolean; push?: boolean }) => {
    if (amend) await gitApi.Run(dir.value, "commit", "--amend", "-m", message.value);
    else await gitApi.CreateCommit(dir.value, message.value);
    if (push) await gitApi.Run(dir.value, "push");
  },
  onSuccess: () => (message.value = ""),
  onSettled: () => queryCache.invalidateQueries({ key: keys.git(dir.value) }),
});
const { mutate: initialize } = useMutation({
  mutation: (folder: string) => gitApi.Run(folder, "init"),
  onSuccess: async (_output, folder) => {
    git.select(folder);
    await queryCache.invalidateQueries({ key: ["git"] });
    await listRepositories();
  },
});

const repositoryMenu = computed<DropdownMenuItem[][]>(() => [
  inGit.value.map(({ mod, root }) => ({
    label: workspace.originName(mod.id),
    origin: mod.id,
    icon: root === dir.value ? "i-lucide-check" : undefined,
    onSelect: () => git.select(root),
  })),
  bare.value.length
    ? [
        { type: "label", label: "Not in git" },
        ...bare.value.map(({ mod }) => ({
          label: `Initialize ${workspace.originName(mod.id)}`,
          origin: mod.id,
          onSelect: () => initialize(mod.dir),
        })),
      ]
    : [],
]);
const commitOptions = computed<DropdownMenuItem[]>(() => [
  {
    label: "Commit and push",
    icon: "i-lucide-arrow-up-from-line",
    disabled: !canCommit.value,
    onSelect: () => commit({ push: true }),
  },
  {
    label: "Amend the last commit",
    icon: "i-lucide-pencil",
    disabled: !message.value.trim(),
    onSelect: () => commit({ amend: true }),
  },
]);
const moreActions = computed<DropdownMenuItem[]>(() => [
  { label: "Stage all changes", icon: "i-lucide-plus", onSelect: () => change(gitApi.Stage, changes.value) },
  { label: "Unstage all", icon: "i-lucide-minus", onSelect: () => change(gitApi.Unstage, changes.value) },
  {
    label: "Read the repository again",
    icon: "i-lucide-refresh-cw",
    onSelect: () => void queryCache.invalidateQueries({ key: keys.git(dir.value) }),
  },
  { label: "Show the Git output", icon: "i-lucide-terminal", onSelect: () => (panel.value = "output") },
]);
const remoteRows = computed(() => {
  const addresses = new Map<string, string>();
  for (const line of (remoteList.value ?? "").split("\n")) {
    const [label = "", url = ""] = line.replace(/ \((fetch|push)\)$/, "").split("\t");
    if (label) addresses.set(label, url);
  }
  return [...addresses].map(([label, url]) => ({ label, url }));
});
const footer = computed(() => {
  const [fetch, pull, push] = SYNC.map(({ label, icon, args }) => ({
    label,
    icon,
    onClick: () => act(buildCommand(...args)),
  }));
  return [
    fetch,
    pull,
    { label: "Pull from…", onClick: () => show("pull") },
    push,
    { label: `Merge into ${current.value}…`, onClick: () => show("merge") },
    { label: "Remotes…", icon: "i-lucide-server", onClick: showRemotes },
    { label: "Delete…", color: "error", variant: "ghost", onClick: () => show("delete") },
  ] as const;
});
const picks = computed(() => {
  const all = [...local.value, ...remotes.value];
  return {
    start: { label: "Make the new branch from", refs: all, pick: (ref: string) => show("name", ref) },
    pull: { label: "Pull from", refs: remotes.value, pick: (ref: string) => act(() => pullBranch(ref)) },
    merge: {
      label: `Merge into ${current.value}`,
      refs: all.filter((ref) => !ref.current),
      pick: (ref: string) => act(() => gitApi.Merge(dir.value, ref)),
    },
    delete: {
      label: "Delete a branch",
      refs: local.value.filter((ref) => !ref.current),
      pick: (ref: string) => act(() => deleteBranch(ref)),
    },
  };
});
const groups = computed<CommandPaletteGroup[]>(() => {
  const label = term.value.trim();
  if (mode.value === "main") return buildMainGroups(label);
  if (mode.value === "name") return [{ id: "create", ignoreFilter: true, items: buildCreateItems(label, base.value) }];
  if (mode.value === "tag") return buildTagGroups();
  const { label: title, refs: choices, pick } = picks.value[mode.value];
  const items = choices.map((ref) => ({
    label: ref.name,
    icon: "i-lucide-git-branch",
    onSelect: () => pick(ref.name),
  }));
  return [{ id: mode.value, label: title, items }];
});

/** Builds the item that creates a branch with the given name from a starting branch. It is none for an empty name. */
function buildCreateItems(label: string, start: string): CommandPaletteItem[] {
  if (!label || local.value.some((ref) => ref.name === label)) return [];
  const make = () =>
    start === current.value
      ? gitApi.CreateBranch(dir.value, label)
      : gitApi.Run(dir.value, "checkout", "-b", label, start);
  return [{ label: `Create branch ${label} from ${start}`, icon: "i-lucide-plus", onSelect: () => act(make) }];
}

/** Builds the branch menu's first list: the branch to create, the ways on, then the local and remote branches. */
function buildMainGroups(label: string): CommandPaletteGroup[] {
  const ways = [
    {
      label: `Create a branch from ${current.value}…`,
      icon: "i-lucide-plus",
      onSelect: () => show("name", current.value),
    },
    { label: "Create a branch from another branch…", icon: "i-lucide-plus", onSelect: () => show("start") },
    { label: "Check out a tag or commit…", icon: "i-lucide-git-commit-horizontal", onSelect: () => show("tag") },
  ];
  return [
    { id: "actions", ignoreFilter: true, items: [...buildCreateItems(label, current.value), ...(label ? [] : ways)] },
    {
      id: "local",
      label: "Local branches",
      items: local.value.map((ref) => ({
        label: ref.name,
        icon: ref.current ? "i-lucide-check" : "i-lucide-git-branch",
        suffix: ref.current ? "current" : "",
        onSelect: () => act(() => gitApi.SwitchBranch(dir.value, ref.name)),
      })),
    },
    {
      id: "remote",
      label: "Remote branches",
      items: remotes.value.map((ref) => ({
        label: ref.name,
        icon: "i-lucide-cloud",
        suffix: "checks out as a new local branch",
        onSelect: () => act(buildCommand("switch", ref.name.slice(ref.name.indexOf("/") + 1))),
      })),
    },
  ];
}

/** Builds the lists of tags and of the log's commits, each of which a checkout can detach to. */
function buildTagGroups(): CommandPaletteGroup[] {
  return [
    {
      id: "tags",
      label: "Tags",
      items: (refs.value ?? [])
        .filter((ref) => ref.type === "tag")
        .map((ref) => ({
          label: ref.name,
          icon: "i-lucide-tag",
          onSelect: () => act(buildCommand("checkout", ref.name)),
        })),
    },
    {
      id: "commits",
      label: "Commits",
      items: (commits.value ?? []).map((one) => ({
        label: one.subject,
        suffix: one.short,
        icon: "i-lucide-git-commit-horizontal",
        onSelect: () => act(buildCommand("checkout", one.hash)),
      })),
    },
  ];
}

/** Gives each commit its lane and the lanes that touch its row. A lane waits for the commit its line leads to. */
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

/** The lines of a graph row, each in its lane's color: lanes passing, joining, going on and leaving for a merge. */
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

/** Describes the refs at a commit as chips: a branch in the accent, a remote branch in the info color, a tag plain. */
function describeRefs(one: Commit): { label: string; color: "primary" | "info" | "neutral" }[] {
  const remoteNames = new Set(remotes.value.map((ref) => ref.name));
  return (one.refs ?? [])
    .filter((ref) => !ref.endsWith("/HEAD"))
    .map((ref) => ref.replace(/^HEAD -> /, ""))
    .map((ref) =>
      ref.startsWith("tag: ")
        ? { label: ref.slice(5), color: "neutral" }
        : { label: ref, color: remoteNames.has(ref) ? "info" : "primary" },
    );
}

/** Builds the git command with the given arguments, to run in the selected repository. */
function buildCommand(...args: string[]): () => Promise<string> {
  return () => gitApi.Run(dir.value, ...args);
}

/** Runs one git action and closes the branch menu. */
function act(work: () => Promise<unknown>): void {
  branchMenu.value = false;
  run(work);
}

/** Shows the branch menu in the given mode, with an empty search. A starting branch is the base of a new branch. */
function show(next: Mode, start = ""): void {
  mode.value = next;
  base.value = start;
  term.value = "";
}

/** Shows the remotes of the repository. */
function showRemotes(): void {
  branchMenu.value = false;
  remoting.value = true;
  listRemotes();
}

/** Finds the repository of each mod of the workspace. */
async function listRepositories(): Promise<void> {
  repositories.value = await Promise.all(
    mods.value.map(async (mod) => {
      const [root, found] = (await loadQuery(repositoryQuery(mod.dir))) ?? ["", false];
      return { mod, root: found ? root : "" };
    }),
  );
}

/** Opens a changed file, or its diff on the given side of a commit. */
function open(place: Place, side?: Side, hash?: string): void {
  if (side) editor.open(diffTab(side, place.key, hash), place);
  else editor.show(place);
}

/** Whether the active tab is the diff of the given side of the given changed file. */
function isLit(side: Section["side"], row: Change): boolean {
  const target = parseTab(editor.activeTab()?.id ?? "");
  return target.tab === "diff" && target.side === side && target.key === row.place.key;
}

/** Stages, unstages or discards files, as the given call of the git service does. */
function change(call: typeof gitApi.Stage, files: Change[]): void {
  run(() =>
    call(
      dir.value,
      files.map((file) => file.path),
    ),
  );
}

/** Throws the working changes of files away, once the user has agreed. */
function discard(files: Change[]): void {
  const body = `The changes to ${files.length === 1 ? files[0]?.path : plural(files.length, "file")} go, and a file git does not track yet is deleted. This cannot be undone.`;
  run(async () => {
    if (await confirm.open({ title: "Discard the changes?", body, confirm: "Discard", destructive: true }))
      await gitApi.Discard(
        dir.value,
        files.map((file) => file.path),
      );
  });
}

/** Pulls and pushes the branch, or publishes it where it has no upstream. A repository with no remote asks for one. */
function sync(): void {
  run(async () => {
    const existing = (await gitApi.Run(dir.value, "remote")).split("\n")[0]?.trim();
    if (!existing) remoting.value = true;
    else if (status.value?.upstream) {
      await gitApi.Run(dir.value, "pull");
      await gitApi.Run(dir.value, "push");
    } else await gitApi.Run(dir.value, "push", "-u", existing, status.value?.branch ?? "");
  });
}

/** Adds a remote, or changes the address of one with the same label, and pushes to it if it is the first. */
function saveRemote(): void {
  const { label, url } = remote.value;
  const exists = remoteRows.value.some((row) => row.label === label);
  run(async () => {
    await gitApi.Run(dir.value, "remote", exists ? "set-url" : "add", label, url.trim());
    if (!remoteRows.value.length) await gitApi.Run(dir.value, "push", "-u", label, status.value?.branch ?? "");
    listRemotes();
  });
  remote.value = { label: "origin", url: "" };
}

/** Removes a remote and lists the rest. */
function removeRemote(label: string): void {
  run(async () => {
    await gitApi.Run(dir.value, "remote", "remove", label);
    listRemotes();
  });
}

/** Pulls one remote branch into the current one. */
function pullBranch(ref: string): Promise<string> {
  const slash = ref.indexOf("/");
  return gitApi.Run(dir.value, "pull", ref.slice(0, slash), ref.slice(slash + 1));
}

/** Deletes a branch. One with commits no other branch has is deleted only when the user asks again. */
async function deleteBranch(branch: string): Promise<void> {
  try {
    await gitApi.Run(dir.value, "branch", "-d", branch);
  } catch (err) {
    if (!String(err).includes("not fully merged")) throw err;
    const force = {
      label: "Delete anyway",
      color: "error",
      variant: "soft",
      onClick: () => run(buildCommand("branch", "-D", branch)),
    };
    toast.add({ title: `${branch} has commits no other branch has`, actions: [force] });
  }
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
    <Teleport v-if="dir" defer to="#sidebar-actions">
      <UTooltip v-for="step in SYNC" :key="step.label" :text="step.label">
        <UButton
          :icon="step.icon"
          variant="ghost"
          square
          :aria-label="step.label"
          @click="run(buildCommand(...step.args))"
        />
      </UTooltip>
      <UDropdownMenu :items="moreActions">
        <UButton icon="i-lucide-ellipsis" variant="ghost" square aria-label="More source control actions" />
      </UDropdownMenu>
      <USeparator orientation="vertical" class="mx-0.5 h-4" />
    </Teleport>
    <div v-if="!dir || error" class="flex flex-col gap-1 px-3 py-2.5">
      <p class="pb-1.5 text-dimmed">
        {{ error ? error.message : "None of this workspace's mods is in a git repository." }}
      </p>
      <div v-for="mod in mods" :key="mod.id" class="flex items-center gap-2">
        <OriginChip :origin="mod.id" dot />
        <span class="min-w-0 grow truncate">{{ workspace.originName(mod.id) }}</span>
        <UButton label="Initialize" icon="i-lucide-git-branch-plus" size="xs" @click="initialize(mod.dir)" />
      </div>
    </div>
    <template v-else>
      <div class="flex shrink-0 flex-col gap-2.5 px-3 pt-3" @keydown.ctrl.enter="canCommit && commit({})">
        <UDropdownMenu v-if="inGit.length > 1 || bare.length" :items="repositoryMenu" :content="{ align: 'start' }">
          <UButton
            color="neutral"
            variant="ghost"
            trailing-icon="i-lucide-chevron-down"
            size="sm"
            class="-mx-2.5 font-semibold"
            :ui="{ trailingIcon: 'ms-auto' }"
          >
            <OriginChip :origin="origin" dot /><span class="truncate">{{ workspace.originName(origin) }}</span>
          </UButton>
          <template #item-leading="{ item }">
            <OriginChip v-if="'origin' in item" :origin="String(item.origin)" dot />
          </template>
        </UDropdownMenu>
        <div v-else class="flex h-6 items-center gap-2 font-semibold text-highlighted">
          <OriginChip :origin="origin" dot /><span class="truncate">{{ workspace.originName(origin) }}</span>
        </div>
        <div class="flex items-center gap-1.5">
          <UPopover v-model:open="branchMenu" :content="{ align: 'start' }" @update:open="show('main')">
            <UButton
              icon="i-lucide-git-branch"
              trailing-icon="i-lucide-chevron-down"
              color="neutral"
              variant="soft"
              size="sm"
              class="min-w-0 grow"
              :ui="{ label: 'truncate font-mono', trailingIcon: 'ms-auto text-dimmed' }"
              :label="status?.branch || 'detached HEAD'"
            />
            <template #content>
              <UCommandPalette
                v-model:search-term="term"
                :groups="groups"
                :placeholder="PLACEHOLDERS[mode]"
                class="h-100 w-110"
                :ui="{ itemLabelSuffix: 'text-11 text-dimmed' }"
              >
                <template #footer>
                  <div v-if="mode === 'main'" class="flex flex-wrap gap-1.5">
                    <UButton v-for="button in footer" :key="button.label" v-bind="button" size="sm" />
                  </div>
                  <UButton
                    v-else
                    label="Back"
                    icon="i-lucide-arrow-left"
                    variant="ghost"
                    size="sm"
                    @click="show('main')"
                  />
                </template>
              </UCommandPalette>
            </template>
          </UPopover>
          <UTooltip
            :text="
              status?.upstream ? `${status.ahead} to push, ${status.behind} to pull` : 'Push the branch, and track it'
            "
          >
            <UButton
              icon="i-lucide-arrow-down-up"
              size="sm"
              :label="status?.upstream ? `${status.ahead} up, ${status.behind} down` : 'Publish'"
              @click="sync"
            />
          </UTooltip>
        </div>
        <UTextarea
          v-model="message"
          :placeholder="`Message for the commit on ${status?.branch || 'HEAD'}`"
          :rows="3"
          size="sm"
          autofocus
          class="[&_textarea]:resize-none"
        />
        <div class="flex gap-1">
          <UButton
            icon="i-lucide-check"
            color="primary"
            variant="solid"
            size="sm"
            class="grow justify-center"
            :label="`Commit ${plural(stagedCount, 'staged file')}`"
            :disabled="!canCommit"
            @click="commit({})"
          />
          <UDropdownMenu :items="commitOptions">
            <UButton icon="i-lucide-chevron-down" size="sm" square aria-label="Commit options" />
          </UDropdownMenu>
        </div>
      </div>
      <USplitter
        :items="[
          { id: 'changes', slot: 'changes', order: 1, sizeUnit: 'px', minSize: 200 },
          graphOpen
            ? { id: 'graph', slot: 'graph', order: 2, sizeUnit: 'px', defaultSize: 240, minSize: 80 }
            : { id: 'folded', slot: 'graph', order: 2, sizeUnit: 'px', defaultSize: 29, minSize: 29, maxSize: 29 },
        ]"
        orientation="vertical"
        :disabled="!graphOpen"
        auto-save-id="pmt.ide.git"
        class="min-h-0 flex-1"
      >
        <template #changes>
          <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto pt-3 pb-2">
            <template v-for="section in sections" :key="section.title">
              <template v-if="section.rows.length || section.side">
                <div :class="SECTION">
                  {{ section.title }}<span class="font-normal text-dimmed">{{ section.rows.length }}</span>
                  <span class="grow" />
                  <UButton
                    v-for="action in section.rows.length ? section.all : []"
                    :key="action.label"
                    :icon="action.icon"
                    :color="action.color ?? 'neutral'"
                    variant="ghost"
                    size="xs"
                    square
                    :aria-label="action.label"
                    @click="action.run(section.rows)"
                  />
                </div>
                <div
                  v-for="row in section.rows"
                  :key="row.path"
                  class="contents"
                  @click="open(row.place, section.side)"
                >
                  <GitFileRow
                    :class="{ 'bg-accented': isLit(section.side, row) }"
                    :change="row"
                    @open="open(row.place)"
                    @stage="change(gitApi.Stage, [row])"
                    @unstage="change(gitApi.Unstage, [row])"
                    @discard="discard([row])"
                  />
                </div>
              </template>
            </template>
          </div>
        </template>
        <template #graph>
          <div class="flex min-h-0 min-w-0 flex-1 flex-col border-t border-default">
            <button
              type="button"
              class="flex h-7 shrink-0 cursor-pointer items-center gap-1 px-1.5 text-11 font-semibold whitespace-nowrap text-toned hover:text-highlighted"
              :aria-expanded="graphOpen"
              @click="graphOpen = !graphOpen"
            >
              <UIcon :name="graphOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-3.5" />Graph
            </button>
            <div v-if="graphOpen" ref="graph" class="min-h-0 flex-1 overflow-y-auto">
              <template v-for="entry in entries" :key="entry.key">
                <div
                  v-if="entry.change"
                  class="contents"
                  @click="open(entry.change.place, 'commit', entry.row.commit.hash)"
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
    </template>
    <UModal
      v-model:open="remoting"
      title="Remotes"
      description="Where this repository fetches from and pushes to."
      :ui="{ content: 'h-90 sm:max-w-xl', body: 'overflow-y-auto' }"
    >
      <template #body>
        <div class="flex flex-col gap-2 text-12">
          <p v-if="!remoteRows.length" class="text-dimmed">This repository has no remote yet.</p>
          <div v-for="row in remoteRows" :key="row.label" class="flex h-7 items-center gap-1.5">
            <span class="w-16 shrink-0 truncate font-mono text-highlighted">{{ row.label }}</span>
            <span class="min-w-0 grow truncate font-mono text-toned">{{ row.url }}</span>
            <UButton
              icon="i-lucide-pencil"
              variant="ghost"
              size="xs"
              square
              :aria-label="`Change the address of ${row.label}`"
              @click="remote = { label: row.label, url: row.url }"
            />
            <UButton
              icon="i-lucide-trash-2"
              color="error"
              variant="ghost"
              size="xs"
              square
              :aria-label="`Remove ${row.label}`"
              @click="removeRemote(row.label)"
            />
          </div>
          <form class="flex items-center gap-1.5 border-t border-default pt-3" @submit.prevent="saveRemote">
            <UInput v-model="remote.label" placeholder="origin" size="sm" class="w-16 shrink-0 font-mono" />
            <UInput
              v-model="remote.url"
              placeholder="https://github.com/you/my-mod.git"
              size="sm"
              class="min-w-0 grow font-mono"
            />
            <UButton type="submit" label="Save" icon="i-lucide-plus" size="sm" :disabled="!remote.url.trim()" />
          </form>
        </div>
      </template>
    </UModal>
  </div>
</template>
