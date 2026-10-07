/**
 * The workspace's IDE folders (GetIdeRoots): its mods' and the game's, and where a path sits among them, its root
 * and its slash-separated path from it, which the explorer's tree is written in.
 */
import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@pinia/colada";
import type { IdeRoot } from "@services/models";
import { ideRootsQuery } from "../queries";

// Windows folds a path's case; Linux does not
const folds = !navigator.userAgent.includes("Linux");
/** A path as the system compares it: slashes one way, and on Windows one case. */
export function pathKey(path: string): string {
  const slashed = path.replaceAll("\\", "/").replace(/\/$/, "");
  return folds ? slashed.toLowerCase() : slashed;
}

export function useIdeRoots(workspaceId: MaybeRefOrGetter<string>) {
  const { data } = useQuery(() => ideRootsQuery(toValue(workspaceId)));
  const roots = computed(() => data.value?.roots ?? []);

  /** The root a path is under (of the workspace's, or of the roots given), and its path from it. */
  function locate(path: string, among = roots.value): { root: IdeRoot; rel: string } | undefined {
    const at = pathKey(path);
    for (const root of among) {
      const base = pathKey(root.path);
      if (at.startsWith(`${base}/`)) return { root, rel: path.slice(base.length + 1).replaceAll("\\", "/") };
    }
  }

  /** A path under a root, written as the root's own path is. */
  function under(root: IdeRoot, rel: string): string {
    const sep = root.path.includes("\\") ? "\\" : "/";
    return `${root.path.replace(/[\\/]$/, "")}${sep}${rel.replaceAll("/", sep)}`;
  }

  return { data, roots, locate, under };
}
