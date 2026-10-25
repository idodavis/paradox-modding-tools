<!--
  One repository's section of the Source Control view: a header with its mods, its branch and its actions, then the
  commit box and the files changed, staged and in conflict.
-->
<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef, watch } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import type { CommandPaletteGroup, CommandPaletteItem, DropdownMenuItem } from "@nuxt/ui";

import * as gitApi from "@services/git/service";
import type { Change, Ref } from "@services/git/models";
import type { Mod, Place } from "@services/workspace/models";

import { describeAge, plural } from "../../lib/format";
import { commitsQuery, gitStatusQuery, keys, refsQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useWorkspaceStore } from "../../stores/workspace";
import ConfirmModal from "../../ui/ConfirmModal.vue";
import GitFileRow from "../../ui/GitFileRow.vue";
import OriginChip from "../../ui/OriginChip.vue";
import { diffTab, parseTab } from "./editor/tabs";

const { root, mods } = defineProps<{ root: string; mods: Mod[] }>();

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

// how many of the newest commits the branch menu offers to check out
const CHECKOUT_COMMITS = 200;
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

const { data: status, error } = useQuery(() => gitStatusQuery(root));
const { data: refs } = useQuery(() => ({ ...refsQuery(root), enabled: !!status.value }));
const message = shallowRef("");
const branchMenu = shallowRef(false);
const mode = shallowRef<Mode>("main");
const term = shallowRef("");
const base = shallowRef("");
const remoting = shallowRef(false);
const remote = shallowRef({ label: "origin", url: "" });
const panel = useLocalStorage("pmt.ide.panel", "");
const box = useTemplateRef("box");
// the log is read only while the branch menu lists the commits to check out
const { data: commits } = useQuery(() => ({
  ...commitsQuery({
    dir: root,
    refs: [status.value?.branch ?? "", status.value?.upstream ?? ""].filter(Boolean),
    limit: CHECKOUT_COMMITS,
  }),
  enabled: !!status.value && mode.value === "tag",
}));
const folded = computed(() => git.folded.has(root));
// the section is named for the repository's first mod in load order; the header counts the others after it
const origin = computed(() => mods[0]?.id ?? "");
const others = computed(() => mods.slice(1).map((mod) => workspace.originName(mod.id)));

// the commit box takes the focus when it is asked for, unless the section is folded
watch(
  () => git.commitTarget,
  async (target) => {
    if (target !== root) return;
    git.commitTarget = "";
    await nextTick();
    box.value?.textareaRef?.focus();
  },
  { immediate: true },
);

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

const { mutate: run } = useMutation({
  mutation: (work: () => Promise<unknown>) => work(),
  onSettled: () => queryCache.invalidateQueries({ key: keys.git(root) }),
});
const { mutate: listRemotes, data: remoteList } = useMutation({
  mutation: () => gitApi.Run(root, "remote", "-v"),
});
const { mutate: commit } = useMutation({
  mutation: async ({ amend = false, push = false }: { amend?: boolean; push?: boolean }) => {
    if (amend) await gitApi.Run(root, "commit", "--amend", "-m", message.value);
    else await gitApi.CreateCommit(root, message.value);
    if (push) await gitApi.Run(root, "push");
  },
  onSuccess: () => (message.value = ""),
  onSettled: () => queryCache.invalidateQueries({ key: keys.git(root) }),
});

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
const moreActions = computed<DropdownMenuItem[][]>(() => [
  [
    ...SYNC.map(({ label, icon, args }) => ({ label, icon, onSelect: () => run(buildCommand(...args)) })),
    ...(status.value?.upstream ? [] : [{ label: "Publish the branch", icon: "i-lucide-cloud-upload", onSelect: sync }]),
  ],
  [
    { label: "Stage all changes", icon: "i-lucide-plus", onSelect: () => change(gitApi.Stage, changes.value) },
    { label: "Unstage all", icon: "i-lucide-minus", onSelect: () => change(gitApi.Unstage, changes.value) },
  ],
  [{ label: "Show the Git output", icon: "i-lucide-terminal", onSelect: () => (panel.value = "output") }],
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
      pick: (ref: string) => act(() => gitApi.Merge(root, ref)),
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
    start === current.value ? gitApi.CreateBranch(root, label) : gitApi.Run(root, "checkout", "-b", label, start);
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
        suffix: describeBranch(ref),
        onSelect: () => act(() => gitApi.SwitchBranch(root, ref.name)),
      })),
    },
    {
      id: "remote",
      label: "Remote branches",
      items: remotes.value.map((ref) => ({
        label: ref.name,
        icon: "i-lucide-cloud",
        suffix: describeBranch(ref),
        onSelect: () => act(buildCommand("switch", ref.name.slice(ref.name.indexOf("/") + 1))),
      })),
    },
  ];
}

/**
 * Describes a branch in the branch menu. The current branch says how far it is ahead of its upstream and behind it,
 * and the upstream how many of its commits the current branch lacks. Any other branch says when its last commit was
 * made, and a remote branch also what picking it does.
 */
function describeBranch(ref: Ref): string {
  const age = describeAge(ref.date, "long");
  const upstream = status.value?.upstream;
  const ahead = status.value?.ahead ?? 0;
  const behind = status.value?.behind ?? 0;
  if (ref.current) return upstream ? `current, ${ahead} up, ${behind} down` : "current";
  if (ref.type === "branch") return age;
  if (ref.name === upstream && behind) return `${plural(behind, "commit")} you don't have`;
  return `${age}, checks out as a new local branch`;
}

/** Builds the lists of tags and of the newest commits, each of which a checkout can detach to. */
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

/** Builds the git command with the given arguments, to run in this repository. */
function buildCommand(...args: string[]): () => Promise<string> {
  return () => gitApi.Run(root, ...args);
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

/** Folds the section, or unfolds it when it is folded. */
function toggle(): void {
  if (folded.value) git.folded.delete(root);
  else git.folded.add(root);
}

/** Opens a changed file, or its diff on the given side. */
function open(place: Place, side?: Side): void {
  if (side) editor.open(diffTab(side, root, place.key), place);
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
      root,
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
        root,
        files.map((file) => file.path),
      );
  });
}

/** Pulls and pushes the branch, or publishes it where it has no upstream. A repository with no remote asks for one. */
function sync(): void {
  run(async () => {
    const existing = (await gitApi.Run(root, "remote")).split("\n")[0]?.trim();
    if (!existing) remoting.value = true;
    else if (status.value?.upstream) {
      await gitApi.Run(root, "pull");
      await gitApi.Run(root, "push");
    } else await gitApi.Run(root, "push", "-u", existing, status.value?.branch ?? "");
  });
}

/** Adds a remote, or changes the address of one with the same label, and pushes to it if it is the first. */
function saveRemote(): void {
  const { label, url } = remote.value;
  const exists = remoteRows.value.some((row) => row.label === label);
  run(async () => {
    await gitApi.Run(root, "remote", exists ? "set-url" : "add", label, url.trim());
    if (!remoteRows.value.length) await gitApi.Run(root, "push", "-u", label, status.value?.branch ?? "");
    listRemotes();
  });
  remote.value = { label: "origin", url: "" };
}

/** Removes a remote and lists the rest. */
function removeRemote(label: string): void {
  run(async () => {
    await gitApi.Run(root, "remote", "remove", label);
    listRemotes();
  });
}

/** Pulls one remote branch into the current one. */
function pullBranch(ref: string): Promise<string> {
  const slash = ref.indexOf("/");
  return gitApi.Run(root, "pull", ref.slice(0, slash), ref.slice(slash + 1));
}

/** Deletes a branch. One with commits no other branch has is deleted only when the user asks again. */
async function deleteBranch(branch: string): Promise<void> {
  try {
    await gitApi.Run(root, "branch", "-d", branch);
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
  <div class="flex shrink-0 flex-col">
    <div class="group flex h-7 shrink-0 items-center gap-1 ps-1 pe-1.5 whitespace-nowrap">
      <UButton
        :icon="folded ? 'i-lucide-chevron-right' : 'i-lucide-chevron-down'"
        variant="ghost"
        size="xs"
        square
        :aria-label="folded ? 'Unfold' : 'Fold'"
        :aria-expanded="!folded"
        @click="toggle"
      />
      <OriginChip :origin="origin" dot />
      <span class="min-w-0 truncate text-11 font-semibold text-highlighted">{{ workspace.originName(origin) }}</span>
      <UTooltip v-if="others.length" :text="others.join(', ')">
        <span class="shrink-0 text-11 text-dimmed">+ {{ others.length }}</span>
      </UTooltip>
      <span class="grow" />
      <UPopover v-model:open="branchMenu" :content="{ align: 'end' }" @update:open="show('main')">
        <UButton
          icon="i-lucide-git-branch"
          variant="ghost"
          size="xs"
          class="min-w-0 shrink"
          :ui="{ label: 'truncate font-mono' }"
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
              <UButton v-else label="Back" icon="i-lucide-arrow-left" variant="ghost" size="sm" @click="show('main')" />
            </template>
          </UCommandPalette>
        </template>
      </UPopover>
      <UTooltip v-if="status?.upstream" :text="`${status.ahead} to push, ${status.behind} to pull`">
        <UButton :label="`${status.ahead}↑ ${status.behind}↓`" variant="ghost" size="xs" @click="sync" />
      </UTooltip>
      <!-- the actions show while the pointer is on the header, and stay while their menu is open -->
      <span class="hidden group-hover:flex has-[[data-state=open]]:flex">
        <UTooltip text="Commit">
          <UButton
            icon="i-lucide-check"
            variant="ghost"
            size="xs"
            square
            aria-label="Commit"
            :disabled="!canCommit"
            @click="commit({})"
          />
        </UTooltip>
        <UTooltip text="Refresh">
          <UButton
            icon="i-lucide-refresh-cw"
            variant="ghost"
            size="xs"
            square
            aria-label="Refresh"
            @click="queryCache.invalidateQueries({ key: keys.git(root) })"
          />
        </UTooltip>
        <UDropdownMenu :items="moreActions" :content="{ align: 'end' }">
          <UButton icon="i-lucide-ellipsis" variant="ghost" size="xs" square aria-label="More source control actions" />
        </UDropdownMenu>
      </span>
    </div>
    <UCollapsible :open="!folded">
      <template #content>
        <p v-if="error" class="px-3 py-1.5 text-dimmed">{{ error.message }}</p>
        <template v-else>
          <div class="flex flex-col gap-2 px-3 pt-1 pb-2" @keydown.ctrl.enter="canCommit && commit({})">
            <UTextarea
              ref="box"
              v-model="message"
              :placeholder="`Message for the commit on ${status?.branch || 'HEAD'}`"
              :rows="3"
              size="sm"
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
              <div v-for="row in section.rows" :key="row.path" class="contents" @click="open(row.place, section.side)">
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
        </template>
      </template>
    </UCollapsible>
    <UModal
      v-model:open="remoting"
      :title="`Remotes of ${workspace.originName(origin)}`"
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
