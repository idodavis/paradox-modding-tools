/**
 * Saving a workspace: its preferences, a few at a time (the rest sent as the workspace has them), and any
 * other change to it or its mods, which may rebuild its session or change what Publish shows of a mod.
 */
import { toValue, type MaybeRefOrGetter } from "vue";
import { defineMutation, useMutation, useQueryCache } from "@pinia/colada";
import { UpdateWorkspacePrefs } from "@services/workspaceservice";
import type { Workspace } from "@services/models";
import { releaseKeys, sessionKeys, workspaceKeys } from "../queries";

type Prefs = Pick<Workspace, "resetIdeOnOpen" | "defaultTool" | "gameColor" | "hideExplorerBinaries">;

/** Saves some of a workspace's preferences; the rest go as the workspace has them. */
export function updatePrefs(ws: Workspace, set: Partial<Prefs>): Promise<void> {
  const next = { ...ws, ...set };
  return UpdateWorkspacePrefs(ws.id, next.resetIdeOnOpen, next.defaultTool ?? "", next.gameColor ?? "", next.hideExplorerBinaries);
}

export const useWorkspacePrefs = defineMutation(() => {
  const queryCache = useQueryCache();
  return useMutation({
    mutation: ({ ws, ...set }: { ws: Workspace } & Partial<Prefs>) => updatePrefs(ws, set),
    onSettled: () => queryCache.invalidateQueries({ key: workspaceKeys.all }),
  });
});

/** A change to the workspace or one of its mods, saved; one that rebuilds the session, or changes what
 * Publish shows of the mod it names, says so. */
export function useWorkspaceSave(workspaceId: MaybeRefOrGetter<string>) {
  const queryCache = useQueryCache();
  return useMutation({
    mutation: ({ save }: { save: () => Promise<unknown>; rebuilds?: boolean; release?: string }) => save(),
    onSettled: (_data, _error, { rebuilds, release }) => {
      const id = toValue(workspaceId);
      void queryCache.invalidateQueries({ key: workspaceKeys.all });
      if (rebuilds) void queryCache.invalidateQueries({ key: sessionKeys.live(id) });
      if (release) void queryCache.invalidateQueries({ key: releaseKeys.mod(id, release) });
    },
  });
}
