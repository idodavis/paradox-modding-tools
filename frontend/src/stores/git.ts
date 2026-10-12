/**
 * Source control's state: whether git is installed, the repositories of the workspace's mods and the
 * one shown, which follows the active file's; its status, history and branches; and the Git output, every command
 * GitService ran.
 */
import { computed, shallowRef, watch } from "vue";
import { defineStore } from "pinia";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import type { GitStatus } from "@pierre/trees";
import { Init } from "@services/gitservice";
import type { GitOutput, GitRepository } from "@services/models";
import { pathKey } from "../composables/useIdeRoots";
import { gitAvailableQuery, gitBranchesQuery, gitKeys, gitLogQuery, gitReposQuery, gitStatusesQuery } from "../queries";
import { useEditorStore } from "./editor";
import { useWorkspaceStore } from "./workspace";

/** Each status letter's color in the change lists and its word in the explorer tree. */
export const GIT_LETTERS: Record<string, { color: string; status: GitStatus }> = {
  A: { color: "text-success", status: "added" },
  U: { color: "text-success", status: "untracked" },
  M: { color: "text-warning", status: "modified" },
  R: { color: "text-info", status: "renamed" },
  D: { color: "text-error", status: "deleted" },
  "!": { color: "text-error", status: "modified" },
};

export const useGitStore = defineStore("git", () => {
  const editor = useEditorStore();
  const workspace = useWorkspaceStore();
  const queryCache = useQueryCache();

  const { data: available } = useQuery(gitAvailableQuery);
  const { data: repos } = useQuery(() =>
    gitReposQuery({ id: workspace.activeWorkspaceId, enabled: !!available.value }),
  );

  /** The repository a file is in. */
  const repoOf = (path: string): GitRepository | undefined =>
    repos.value?.find((repo) => pathKey(path).startsWith(`${pathKey(repo.root)}/`));

  // the repository shown: the one picked, which the active file's replaces when the tab changes (or the
  // repositories are read)
  const selectedRoot = shallowRef("");
  watch(
    () => repoOf(editor.activeFile)?.root,
    (root) => {
      if (root) selectedRoot.value = root;
    },
    { immediate: true },
  );
  const repo = computed(() => repos.value?.find((one) => one.root === selectedRoot.value) ?? repos.value?.[0]);
  const root = computed(() => repo.value?.root ?? "");

  /** The workspace's mods no repository holds, once the repositories are read. */
  const bare = computed(() => {
    if (!repos.value) return [];
    const repoMods = new Set(repos.value.flatMap((one) => one.mods ?? []));
    return workspace.workspaceMods.filter((mod) => !mod.isBroken && !repoMods.has(mod.id));
  });
  /** Make a mod's folder a repository (git init), then show it. */
  const { mutate: initialize } = useMutation({
    mutation: (mod: { id: string; path: string }) => Init(mod.path),
    onSuccess: async (_made, mod) => {
      await queryCache.invalidateQueries({ key: gitKeys.repos(workspace.activeWorkspaceId) });
      selectedRoot.value = repos.value?.find((one) => one.mods?.includes(mod.id))?.root ?? selectedRoot.value;
    },
  });

  // every repository's status, the one shown's for Source control
  const { data: statuses } = useQuery(() => gitStatusesQuery((repos.value ?? []).map((one) => one.root)));
  const status = computed(() => statuses.value?.[root.value]);
  // the graph's commits, newest first, a page more each time its end scrolls into view; a full last page says older
  // ones may follow
  const PAGE = 20;
  const count = shallowRef(PAGE);
  watch(root, () => (count.value = PAGE));
  const { data: log } = useQuery(() => gitLogQuery({ root: root.value, count: count.value }));
  const hasMore = computed(() => (log.value?.length ?? 0) >= count.value);
  function loadMore(): void {
    count.value += PAGE;
  }
  const { data: branches } = useQuery(() => gitBranchesQuery(root.value));

  // the commands run, the newest last, the last few hundred
  const output = shallowRef<GitOutput[]>([]);
  function appendOutput(line: GitOutput): void {
    output.value = [...output.value.slice(-299), line];
  }

  /** Read the repositories again. */
  function refresh(): void {
    void queryCache.invalidateQueries({ key: ["git"] });
  }

  /** Run git on the shown repository, then read the repositories again; a failure is the app's error toast. */
  const { mutate: run } = useMutation({
    mutation: async (work: (root: string) => Promise<unknown>) => {
      if (root.value) await work(root.value);
    },
    onSettled: refresh,
  });

  return {
    available,
    repos,
    repoOf,
    selectedRoot,
    repo,
    root,
    bare,
    initialize,
    statuses,
    status,
    log,
    hasMore,
    loadMore,
    branches,
    output,
    appendOutput,
    refresh,
    run,
  };
});
