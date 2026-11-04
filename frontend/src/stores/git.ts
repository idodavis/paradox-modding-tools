/**
 * The git store: whether git is installed, the git repositories of the workspace's mods, each its root with the mods
 * it holds, in load order, and the lines git printed. For the session it also keeps the repository picked for the
 * Graph, the folded sections and the commit box to focus. What a repository holds comes from the git queries.
 */
import { defineStore } from "pinia";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import { computed, reactive, shallowRef } from "vue";

import * as gitApi from "@services/git/service";
import type { Place } from "@services/workspace/models";
import { gitInstalledQuery, keys, repositoriesQuery } from "../queries";

/** OUTPUT_LINES is how many lines of git's output the store keeps. */
const OUTPUT_LINES = 300;
/** STAGE is the git command that stages the files whose paths follow it. */
export const STAGE = ["add", "--"] as const;
/** UNSTAGE is the git command that unstages the files whose paths follow it. */
export const UNSTAGE = ["reset", "-q", "--"] as const;

export const useGitStore = defineStore("git", () => {
  const cache = useQueryCache();
  const { data: listed } = useQuery(repositoriesQuery);
  const { data: installed } = useQuery(gitInstalledQuery);
  /** Whether git is on the PATH. It counts as installed until Go answers, so the views draw no flash of its absence. */
  const isInstalled = computed(() => installed.value ?? true);
  /** Each repository of the workspace's mods: its root and the mods in it, in the load order of their first mod. */
  const repositories = computed(() => listed.value ?? []);
  /** The root of the repository picked in the Graph's header, or "" until one is picked. */
  const graphRoot = shallowRef("");
  /** The roots of the repositories whose sections are folded. */
  const folded = reactive(new Set<string>());
  /** The root of the repository whose commit box takes the focus next, or "" when none is waiting. */
  const commitTarget = shallowRef("");
  const lines = shallowRef<string[]>([]);

  Events.On("git:output", ({ data }) => {
    lines.value = [...lines.value.slice(1 - OUTPUT_LINES), data];
  });

  /** findRoot returns the root of the repository that holds a file's mod, or "" when the file is in no mod with one. */
  const findRoot = (place: Pick<Place, "origin"> | null | undefined): string =>
    repositories.value.find((repository) => repository.mods?.includes(place?.origin ?? ""))?.root ?? "";

  /** output returns the lines git printed, newest last, from git:output. */
  const output = (): readonly string[] => lines.value;

  /** initialize starts a repository in the given folder, then reads the git queries and the repositories again. */
  const { mutate: initialize } = useMutation({
    mutation: (dir: string) => gitApi.Run(dir, "init"),
    onSuccess: () => cache.invalidateQueries({ key: keys.gits }),
  });

  return { isInstalled, repositories, graphRoot, folded, commitTarget, findRoot, output, initialize };
});
