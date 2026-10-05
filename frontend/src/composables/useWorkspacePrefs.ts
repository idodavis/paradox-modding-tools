/**
 * Saving a workspace's preferences, a few at a time: the rest are sent as the workspace has them.
 */
import { defineMutation, useMutation, useQueryCache } from "@pinia/colada";
import { UpdateWorkspacePrefs } from "@services/workspaceservice";
import type { Workspace } from "@services/models";
import { workspaceKeys } from "../queries";

type Prefs = Pick<Workspace, "resetIdeOnOpen" | "defaultTool" | "gameColor" | "hideExplorerBinaries">;

export const useWorkspacePrefs = defineMutation(() => {
  const queryCache = useQueryCache();
  return useMutation({
    mutation: ({ ws, ...set }: { ws: Workspace } & Partial<Prefs>) => {
      const next = { ...ws, ...set };
      return UpdateWorkspacePrefs(ws.id, next.resetIdeOnOpen, next.defaultTool ?? "", next.gameColor ?? "", next.hideExplorerBinaries);
    },
    onSettled: () => queryCache.invalidateQueries({ key: workspaceKeys.all }),
  });
});
