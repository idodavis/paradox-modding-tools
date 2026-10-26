/**
 * The git store: the git repositories of the workspace's mods, each its root with the mods it holds, in load order,
 * and the lines git printed. For the session it also keeps the repository picked for the Graph, the folded sections
 * and the commit box to focus. What a repository holds comes from the git queries.
 */
import { defineStore } from "pinia";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import { groupBy } from "es-toolkit";
import { computed, reactive, shallowRef, watch } from "vue";

import * as gitApi from "@services/git/service";
import type { Mod, Place } from "@services/workspace/models";
import { loadQuery, repositoryQuery, stateQuery } from "../queries";

// the lines kept, the newest last
const OUTPUT_LINES = 300;

export const useGitStore = defineStore("git", () => {
  const { data: state } = useQuery(stateQuery);
  const cache = useQueryCache();
  /** Each repository of the workspace's mods: its root and the mods in it, in the load order of their first mod. */
  const repositories = shallowRef<{ root: string; mods: Mod[] }[]>([]);
  /** The root of the repository picked in the Graph's header, or "" until one is picked. */
  const graphRoot = shallowRef("");
  /** The roots of the repositories whose sections are folded. */
  const folded = reactive(new Set<string>());
  /** The root of the repository whose commit box takes the focus next, or "" when none is waiting. */
  const commitTarget = shallowRef("");
  const lines = shallowRef<string[]>([]);
  // a mod whose folder is missing has no repository to look for
  const mods = computed(() => (state.value?.mods ?? []).filter((mod) => !mod.broken));

  watch(mods, listRepositories, { immediate: true });

  Events.On("git:output", ({ data }) => {
    lines.value = [...lines.value.slice(1 - OUTPUT_LINES), data];
  });

  /**
   * listRepositories looks for the repository of each of the workspace's mods again. Mods in the same repository
   * share it, and a mod in none is left out.
   */
  async function listRepositories(): Promise<void> {
    const order = mods.value;
    const rows = await Promise.all(
      order.map(async (mod) => {
        const [root, isRepository] = (await loadQuery(repositoryQuery(mod.dir))) ?? ["", false];
        return isRepository ? [{ mod, root }] : [];
      }),
    );
    // the mods changed while git was asked, and the look for the new ones replaces this one
    if (order !== mods.value) return;
    const roots = groupBy(rows.flat(), (row) => row.root);
    repositories.value = Object.entries(roots).map(([root, members]) => ({
      root,
      mods: members.map((row) => row.mod),
    }));
  }

  /** The root of the repository that holds a file's mod, or "" when the file is in no mod with a repository. */
  const findRoot = (place: Pick<Place, "origin"> | null | undefined): string =>
    repositories.value.find((one) => one.mods.some((mod) => mod.id === place?.origin))?.root ?? "";

  /** The lines git printed, newest last, from git:output. */
  const output = (): readonly string[] => lines.value;

  /** initialize starts a repository in the given folder, then reads the git queries and the repositories again. */
  const { mutate: initialize } = useMutation({
    mutation: (dir: string) => gitApi.Run(dir, "init"),
    onSuccess: async () => {
      await cache.invalidateQueries({ key: ["git"] });
      await listRepositories();
    },
  });

  return { repositories, graphRoot, folded, commitTarget, listRepositories, findRoot, output, initialize };
});
