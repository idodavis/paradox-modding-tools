/**
 * The active workspace (the router's guard selects it from the route), and each origin's name and color
 * in it. The workspaces and games themselves are queries (queries.ts).
 */
import { computed } from "vue";
import { defineStore } from "pinia";
import { useQuery } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import type { GameInfo } from "@services/internal/game/models";
import type { Workspace, WorkspaceMod } from "@services/models";
import { UpdateWorkspacePrefs } from "@services/workspaceservice";
import { gamesQuery, workspacesQuery } from "../queries";

/** A mod's color: its hand-picked hex, else its slot's token. */
export function modColor(mod: { color?: string; slot?: number }): string {
  return mod.color || `var(--o${mod.slot || 1})`;
}

type Prefs = Pick<Workspace, "resetIdeOnOpen" | "defaultTool" | "gameColor" | "hideExplorerBinaries">;

/** Saves some of a workspace's preferences; the rest go as the workspace has them. */
export function updatePrefs({ ws, ...set }: { ws: Workspace } & Partial<Prefs>): Promise<void> {
  const next = { ...ws, ...set };
  return UpdateWorkspacePrefs(
    ws.id,
    next.resetIdeOnOpen,
    next.defaultTool ?? "",
    next.gameColor ?? "",
    next.hideExplorerBinaries,
  );
}

export const useWorkspaceStore = defineStore("workspace", () => {
  const activeWorkspaceId = useLocalStorage("pmt.workspace.activeId", "");
  const { data: gameList } = useQuery(gamesQuery);
  const { data: workspaces } = useQuery(workspacesQuery);

  const games = computed(() => (gameList.value?.games ?? []).filter((g): g is GameInfo => !!g));
  const originVanilla = computed(() => gameList.value?.originVanilla || "vanilla");
  const activeWorkspace = computed(() => workspaces.value?.find((w) => w.id === activeWorkspaceId.value));
  const workspaceMods = computed(() => activeWorkspace.value?.mods ?? []);

  /** A game's registry row. */
  function game(id: string): GameInfo | undefined {
    return games.value.find((g) => g.id === id);
  }

  /** A mod of the active workspace. */
  function mod(id: string): WorkspaceMod | undefined {
    return workspaceMods.value.find((m) => m.id === id);
  }

  /** An origin's name: the game's for vanilla, else its mod's; the id when neither. */
  function originName(id: string): string {
    if (!id || id === originVanilla.value) return game(activeWorkspace.value?.gameId ?? "")?.name ?? "Game";
    return mod(id)?.name ?? id;
  }

  /** An origin's color: the game's own or its near-grey for vanilla, else its mod's. */
  function originColor(id: string): string {
    if (!id || id === originVanilla.value) return activeWorkspace.value?.gameColor || "var(--game)";
    const record = mod(id);
    return record ? modColor(record) : "var(--game)";
  }

  function select(id: string): void {
    activeWorkspaceId.value = id;
  }

  return {
    activeWorkspaceId,
    activeWorkspace,
    workspaceMods,
    games,
    originVanilla,
    game,
    mod,
    originName,
    originColor,
    select,
  };
});
