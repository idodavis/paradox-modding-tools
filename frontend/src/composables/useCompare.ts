/**
 * The IDE's compares (E10), each in CompareFiles: a file with the game's copy of it, or two files (one selected for
 * compare, then the other). The tabs' menu, the explorer and the palette open them alike.
 */
import { toValue, type MaybeRefOrGetter } from "vue";
import { StatPath } from "@services/fileservice";
import CompareFiles from "../components/ui/CompareFiles.vue";
import { useWorkspaceStore } from "../stores/workspace";
import { useIdeRoots } from "./useIdeRoots";

export function useCompare(workspaceId: MaybeRefOrGetter<string>) {
  const { roots, locate, under } = useIdeRoots(() => toValue(workspaceId));
  const store = useWorkspaceStore();
  const toast = useToast();
  const compare = useOverlay().create(CompareFiles);

  /** A file as a side, opened at a 0-based line: its path from its root and its origin where it has one. */
  function side(path: string, line = 0) {
    const at = locate(path);
    return { path, rel: at?.rel ?? path, origin: at?.root.origin ?? "", line };
  }

  /** Two files, the second opened at a line. */
  function files(old: string, now: string, line = 0): void {
    void compare.open({ old: side(old), now: side(now, line) });
  }

  /** A mod's file with the game's file at the same path. */
  async function withGame(path: string, line = 0): Promise<void> {
    const at = locate(path);
    const game = roots.value.find((root) => root.readOnly);
    if (!at || !game || at.root.readOnly) return;
    const copy = under(game, at.rel);
    if (!(await StatPath(copy)).exists) return void toast.add({ title: `The game has no ${at.rel}`, color: "neutral" });
    void compare.open({
      old: { path: copy, rel: at.rel, origin: store.originVanilla, line: 0 },
      now: side(path, line),
    });
  }

  return { files, withGame };
}
