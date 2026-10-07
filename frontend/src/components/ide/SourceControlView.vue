<script setup lang="ts">
/**
 * The sidebar's Source control (the IdeDiff board's): the repository, always shown and picked among the workspace's
 * mods' when there are several, a mod in none made one there (O-R57, O-R60, O-R61); the branch and how far it is from its upstream; the message and
 * Commit with its options; the conflicts, the staged files and the changes, a file's row opening its diff; and the
 * graph (GitGraph). Its text and rows are the explorer's. Without git it says so (O-R9).
 */
import { computed, shallowRef, useTemplateRef } from "vue";
import { useElementSize, useLocalStorage } from "@vueuse/core";
import type { DropdownMenuItem } from "@nuxt/ui";
import { Commit, Discard, Pull, Push, Stage, Unstage } from "@services/gitservice";
import type { GitChange } from "@services/models";
import { plural } from "../../lib/format";
import { useGitStore } from "../../stores/git";
import { diffTab, tabOf, useIdeStore } from "../../stores/ide";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";

const git = useGitStore();
const ide = useIdeStore();
const settings = useSettingsStore();
const store = useWorkspaceStore();

const message = shallowRef("");
const status = computed(() => git.status);
const staged = computed(() => status.value?.staged ?? []);
const color = computed(() => store.originColor(git.repo?.origin ?? ""));
// the repositories, then the mods in none, each made one by git init
const repoMenu = computed<DropdownMenuItem[][]>(() => [
  (git.repos ?? []).map((repo) => ({ label: repo.name, origin: repo.origin, icon: repo.root === git.root ? "i-lucide-check" : undefined, onSelect: () => (git.picked = repo.root) })),
  git.bare.length
    ? [{ type: "label", label: "Not in git" }, ...git.bare.map((mod) => ({ label: `Initialize ${mod.name}`, origin: mod.id, onSelect: () => git.initialize(mod) }))]
    : [],
]);

function commit(amend = false, push = false): void {
  const text = message.value;
  git.run(async (root) => {
    await Commit(root, text, amend);
    message.value = "";
    if (push) await Push(root);
  });
}
const commitOptions = computed<DropdownMenuItem[]>(() => [
  { label: "Commit and push", icon: "i-lucide-arrow-up-from-line", disabled: !canCommit.value, onSelect: () => void commit(false, true) },
  { label: "Amend the last commit", icon: "i-lucide-pencil", disabled: !message.value.trim() || !status.value?.commit, onSelect: () => void commit(true) },
]);
const canCommit = computed(() => !!message.value.trim() && !!staged.value.length);

// the branch's distance from its upstream syncs it (pull, then push); one with none is pushed and tracks it
function sync(): void {
  const pull = !!status.value?.upstream;
  git.run(async (root) => {
    if (pull) await Pull(root, "", "");
    await Push(root);
  });
}

// discarding throws changes away, so it asks first
const discarding = shallowRef<GitChange[]>([]);
function discard(): void {
  const files = discarding.value.map((change) => change.path);
  discarding.value = [];
  git.run((root) => Discard(root, files));
}

const paths = (rows: GitChange[]) => rows.map((one) => one.path);
const stage = (rows: GitChange[]) => git.run((root) => Stage(root, paths(rows)));
const unstage = (rows: GitChange[]) => git.run((root) => Unstage(root, paths(rows)));
// what throws work away is in the error color, as every delete in the app
type Action = { icon: string; label: string; run: () => void; color?: "error" };
/** A list of files: its rows, what a row opens, the diff it lights while shown, and its actions, a row's and all's. */
type Section = { title: string; rows: GitChange[]; open: (one: GitChange) => void; diff?: "changes" | "staged"; each: (one: GitChange) => Action[]; all: Action[] };
const openFile = (one: GitChange): Action => ({ icon: "i-lucide-file", label: "Open the file", run: () => ide.open(one.path) });
const sections = computed<Section[]>(() => {
  const conflicts = status.value?.conflicts ?? [];
  const changes = status.value?.changes ?? [];
  return [
    ...(conflicts.length
      ? [{ title: "Merge conflicts", rows: conflicts, open: (one: GitChange) => ide.open(one.path), each: (one: GitChange) => [{ icon: "i-lucide-plus", label: "Stage, resolved", run: () => stage([one]) }], all: [] }]
      : []),
    {
      title: "Staged",
      rows: staged.value,
      diff: "staged" as const,
      open: (one) => ide.open(diffTab("staged", one.path)),
      each: (one) => [openFile(one), { icon: "i-lucide-minus", label: "Unstage", run: () => unstage([one]) }],
      all: staged.value.length ? [{ icon: "i-lucide-minus", label: "Unstage all", run: () => unstage(staged.value) }] : [],
    },
    {
      title: "Changes",
      rows: changes,
      diff: "changes" as const,
      open: (one) => ide.open(diffTab("changes", one.path)),
      each: (one) => [
        ...(one.letter === "D" ? [] : [openFile(one)]),
        { icon: "i-lucide-undo-2", label: "Discard", run: () => (discarding.value = [one]), color: "error" },
        { icon: "i-lucide-plus", label: "Stage", run: () => stage([one]) },
      ],
      all: changes.length
        ? [
            { icon: "i-lucide-undo-2", label: "Discard all changes", run: () => (discarding.value = changes), color: "error" },
            { icon: "i-lucide-plus", label: "Stage all changes", run: () => stage(changes) },
          ]
        : [],
    },
  ];
});

// the row of the diff shown is lit
const shown = computed(() => tabOf(ide.activePath));

const LETTERS: Record<string, string> = { A: "text-success", U: "text-success", M: "text-warning", R: "text-info", D: "text-error", "!": "text-error" };
const SECTION = "flex h-7 shrink-0 items-center gap-1.5 ps-3 pe-1.5 text-11 font-semibold whitespace-nowrap text-toned";
// the graph is a pane under the changes, as tall as it was left (the changes keep room for a few rows), or folded
const pane = useTemplateRef<HTMLElement>("pane");
const { height } = useElementSize(pane);
const graphOpen = useLocalStorage("pmt.git.graphOpen", true);
const graphHeight = useLocalStorage("pmt.git.graphHeight", 240);
const graphMax = computed(() => Math.max(80, height.value - 280));

// a row as tall as the explorer's (--row, the graph's too), at its text size (the sidebar's)
const ROW = "group flex h-(--row) w-full shrink-0 cursor-pointer items-center gap-2 ps-3.5 pe-2 text-start whitespace-nowrap text-toned hover:bg-elevated";
</script>

<template>
  <div ref="pane" class="flex min-h-0 flex-1 flex-col" :style="{ '--row': `${settings.treeRow}px` }">
    <p v-if="git.available === false" class="px-3 py-2.5 text-dimmed">
      Source control runs the git installed on this computer, and there is none on the PATH. Install Git, then start
      PMT again.
    </p>
    <div v-else-if="git.repos && !git.repos.length" class="flex flex-col gap-1 px-3 py-2.5">
      <p class="pb-1.5 text-dimmed">None of this workspace's mods is in a git repository.</p>
      <div v-for="mod in git.bare" :key="mod.id" class="flex items-center gap-2">
        <OriginDot :color="store.originColor(mod.id)" small />
        <span class="min-w-0 grow truncate">{{ mod.name }}</span>
        <UButton label="Initialize" icon="i-lucide-git-branch-plus" size="xs" @click="git.initialize(mod)" />
      </div>
    </div>
    <template v-else-if="git.repo">
      <div class="flex shrink-0 flex-col gap-2.5 px-3 pt-3" @keydown.ctrl.enter="canCommit && commit()">
        <UDropdownMenu v-if="(git.repos?.length ?? 0) > 1 || git.bare.length" :items="repoMenu" :content="{ align: 'start' }">
          <UButton color="neutral" variant="ghost" trailing-icon="i-lucide-chevron-down" class="-mx-2.5 font-semibold" :ui="{ trailingIcon: 'ms-auto' }">
            <OriginDot :color="color" small /><span class="truncate">{{ git.repo.name }}</span>
          </UButton>
          <template #item-leading="{ item }">
            <OriginDot v-if="'origin' in item" :color="store.originColor(String(item.origin))" small />
          </template>
        </UDropdownMenu>
        <div v-else class="flex h-6 items-center gap-2 font-semibold text-highlighted">
          <OriginDot :color="color" small /><span class="truncate">{{ git.repo.name }}</span>
        </div>
        <div class="flex items-center gap-1.5">
          <BranchMenu />
          <UTooltip :text="status?.upstream ? `${status.ahead} to push, ${status.behind} to pull` : 'Push the branch, and track it'">
            <UButton icon="i-lucide-arrow-down-up" :label="status?.upstream ? `${status.ahead} up, ${status.behind} down` : 'Publish'" @click="sync" />
          </UTooltip>
        </div>
        <UTextarea v-model="message" :placeholder="`Message for the commit on ${status?.branch || 'HEAD'}`" :rows="3" :ui="{ base: 'resize-none' }" />
        <div class="flex gap-1">
          <UButton
            icon="i-lucide-check"
            color="primary"
            variant="solid"
            class="grow justify-center"
            :label="`Commit ${plural(staged.length, 'staged file')}`"
            :disabled="!canCommit"
            @click="commit()"
          />
          <UDropdownMenu :items="commitOptions">
            <UButton icon="i-lucide-chevron-down" square aria-label="Commit options" />
          </UDropdownMenu>
        </div>
      </div>
      <div class="flex min-h-0 flex-1 flex-col overflow-y-auto pt-3 pb-2">
        <template v-for="section in sections" :key="section.title">
          <div :class="SECTION">
            {{ section.title }}<span class="font-normal text-dimmed">{{ section.rows.length }}</span>
            <span class="grow" />
            <UButton v-for="act in section.all" :key="act.label" :icon="act.icon" :color="act.color ?? 'neutral'" variant="ghost" size="xs" square :aria-label="act.label" @click="act.run" />
          </div>
          <div
            v-for="one in section.rows"
            :key="one.path"
            :class="[ROW, { 'bg-accented': section.diff && shown.diff === section.diff && shown.file === one.path }]"
            @click="section.open(one)"
          >
            <OriginDot :color="color" small />
            <span class="min-w-0 grow truncate"><span class="text-highlighted">{{ one.name }}</span> <span class="text-dimmed">{{ one.folder }}</span></span>
            <UButton
              v-for="act in section.each(one)"
              :key="act.label"
              class="hidden group-hover:flex"
              :icon="act.icon"
              :color="act.color ?? 'neutral'"
              variant="ghost"
              size="xs"
              square
              :aria-label="act.label"
              @click.stop="act.run"
            />
            <b class="w-3.5 shrink-0 text-center font-mono font-medium" :class="LETTERS[one.letter]">{{ one.letter }}</b>
          </div>
        </template>
      </div>
      <ResizeGrip v-if="graphOpen" v-model="graphHeight" axis="y" reverse :min="80" :max="graphMax" :default-size="240" />
      <div class="flex shrink-0 flex-col border-t border-default" :style="graphOpen ? { height: `${Math.min(graphHeight, graphMax)}px` } : undefined">
        <button
          type="button"
          class="flex h-7 shrink-0 cursor-pointer items-center gap-1 ps-1.5 pe-1.5 text-11 font-semibold whitespace-nowrap text-toned hover:text-highlighted"
          :aria-expanded="graphOpen"
          @click="graphOpen = !graphOpen"
        >
          <UIcon :name="graphOpen ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'" class="size-3.5" />Graph
        </button>
        <GitGraph v-if="graphOpen" />
      </div>
    </template>
    <UModal :open="!!discarding.length" title="Discard the changes?" @update:open="(on) => !on && (discarding = [])">
      <template #body>
        <p class="text-13 text-toned">
          The changes to {{ discarding.length === 1 ? discarding[0]?.name : plural(discarding.length, "file") }} go, and a
          file git does not track yet is deleted. This cannot be undone.
        </p>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton label="Cancel" @click="discarding = []" />
          <UButton label="Discard" color="error" variant="soft" @click="discard" />
        </div>
      </template>
    </UModal>
  </div>
</template>
