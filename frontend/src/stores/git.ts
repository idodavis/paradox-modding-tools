/**
 * The git store: the selected repository, which defaults to the active file's mod or else the first mod that is a
 * repository, and the output lines git printed. The repository's state itself comes from the git queries.
 */
import { defineStore } from "pinia";
import { useQuery } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import { computed, shallowRef, watch } from "vue";

import { loadQuery, repositoryQuery, stateQuery } from "../queries";
import { useEditorStore } from "./editor";

// the lines kept, the newest last
const OUTPUT_LINES = 300;

export const useGitStore = defineStore("git", () => {
  const editor = useEditorStore();
  const { data: state } = useQuery(stateQuery);
  // the repository the user picked, and the one the active file or the mods lead to
  const selection = shallowRef("");
  const fallback = shallowRef("");
  const lines = shallowRef<string[]>([]);

  // the folders to look for a repository in, the best first: the active file's mod, then the workspace's mods
  const folders = computed(() =>
    [editor.activeFile()?.root, ...(state.value?.mods ?? []).filter((mod) => !mod.broken).map((mod) => mod.dir)].filter(
      (dir): dir is string => !!dir,
    ),
  );

  watch(
    folders,
    async (dirs, _previous, onCleanup) => {
      let stale = false;
      onCleanup(() => (stale = true));
      for (const dir of dirs) {
        const [root, isRepository] = (await loadQuery(repositoryQuery(dir))) ?? ["", false];
        if (stale) return;
        if (isRepository) {
          fallback.value = root;
          return;
        }
      }
      fallback.value = "";
    },
    { immediate: true },
  );
  // a file of another mod shows that mod's repository again, whichever was picked
  watch(
    () => editor.activeFile()?.root,
    () => (selection.value = ""),
  );

  Events.On("git:output", ({ data }) => {
    lines.value = [...lines.value.slice(1 - OUTPUT_LINES), data];
  });

  /** The selected repository's folder, or "" when none is selected. */
  const repository = (): string => selection.value || fallback.value;

  /** Selects a repository by its folder. */
  const select = (dir: string): void => {
    selection.value = dir;
  };

  /** The lines git printed, newest last, from git:output. */
  const output = (): readonly string[] => lines.value;

  return { repository, select, output };
});
