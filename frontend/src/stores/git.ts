/** The git store: whether git is installed, the workspace's repositories, and what the session keeps about them. */
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
  /** Whether git is on the PATH. It counts as installed until Go answers, to avoid a flash of its absence. */
  const isInstalled = computed(() => installed.value ?? true);
  /** Each repository of the workspace's mods: its root and the mods in it. */
  const repositories = computed(() => listed.value ?? []);
  /** The root of the repository picked in the Graph's header, or "" until one is picked. */
  const graphRoot = shallowRef("");
  /** The roots of the repositories whose sections are folded. */
  const folded = reactive(new Set<string>());
  /** The root of the repository whose commit box takes the focus next, or "". */
  const commitTarget = shallowRef("");
  const lines = shallowRef<string[]>([]);

  Events.On("git:output", ({ data }) => {
    lines.value = [...lines.value.slice(1 - OUTPUT_LINES), data];
  });

  /** findRoot returns the root of the repository that holds a file's mod, or "". */
  const findRoot = (place: Pick<Place, "origin"> | null | undefined): string =>
    repositories.value.find((repository) => repository.mods?.includes(place?.origin ?? ""))?.root ?? "";

  /** output returns the lines git printed, newest last, from git:output. */
  const output = (): readonly string[] => lines.value;

  /** initialize starts a repository in the given folder and reads the git queries again. */
  const { mutate: initialize } = useMutation({
    mutation: (dir: string) => gitApi.Run(dir, "init"),
    onSuccess: () => cache.invalidateQueries({ key: keys.gits }),
  });

  return { isInstalled, repositories, graphRoot, folded, commitTarget, findRoot, output, initialize };
});
