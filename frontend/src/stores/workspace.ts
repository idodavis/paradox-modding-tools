/**
 * The active workspace (the router's guard selects it from the route), its state in Go (open or not, its install
 * and scan, whose dumps it read) and the scan while one runs, and each origin's name and color in it. The
 * workspaces and games themselves are queries (queries.ts).
 */
import { computed, reactive, watch } from "vue";
import { defineStore } from "pinia";
import { useQuery } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import { useRouter } from "vue-router";
import type { GameInfo } from "@services/internal/game/models";
import type { Workspace, WorkspaceMod, WorkspaceState } from "@services/models";
import { UpdateWorkspacePrefs } from "@services/workspaceservice";
import { isWorkspacePage } from "../app/pages";
import { gamesQuery, openQuery, stateQuery, workspacesQuery } from "../queries";
import { isMain } from "./windows";

/** Each install's scan while it runs, as install:scanning tells it (app/events.ts); a workspace opened or failed
 * ends them. */
export const scans = reactive(new Map<string, { percent: number; stage: string }>());

/** Whose script_docs an open workspace reads: the game's own and current, or not. */
export function dumpsState(state: WorkspaceState | null | undefined): { ok: boolean; label: string } | undefined {
  if (!state?.open) return undefined;
  if (state.dumpsFrom === "yours" && !state.dumpsStale) return { ok: true, label: "script_docs loaded" };
  if (state.dumpsFrom === "yours") return { ok: false, label: "script_docs older than the game" };
  if (state.dumpsFrom === "shipped") return { ok: false, label: "Using PMT's copy of script_docs" };
  return { ok: false, label: "No script_docs" };
}

/** Installs a notice was raised for, this run: a game update, and no dumps. */
const notices = { updated: new Set<string>(), noDumps: new Set<string>() };

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

  /** The active workspace's state, read without opening it; workspace:opened and workspace:closed refresh it. */
  const { data: state } = useQuery(() => stateQuery(activeWorkspaceId.value));
  // the workspace is opened on its pages, in every window showing one; the Library and the wizard open nothing
  const route = useRouter().currentRoute;
  useQuery(() => openQuery(route.value.name && isWorkspacePage(route.value.name) ? activeWorkspaceId.value : ""));
  /** The active install's scan while one runs. */
  const scan = computed(() => scans.get(state.value?.installId ?? ""));

  /** Whether a workspace is open in Go: the active one, once its index is built. */
  function isOpen(id: string): boolean {
    return !!id && id === activeWorkspaceId.value && !!state.value?.open;
  }

  // the two notices, once per install and by the main window alone: a game update rescanned, and no script_docs
  const toast = useToast();
  if (isMain)
    watch(
      state,
      (current) => {
        if (!current?.installId) return;
        // a scan that replaced another version's: the game was updated, and the new scan is done
        if (current.updatedFrom && !notices.updated.has(current.installId)) {
          notices.updated.add(current.installId);
          toast.add({
            title: "Game updated",
            description: `Game updated from ${current.updatedFrom} to ${current.gameVersion} — rescanned.`,
            color: "warning",
            icon: "i-lucide-refresh-cw",
            duration: 0,
          });
        }
        // the type system is the one part of the engine the user makes, so its absence is raised once
        if (current.open && current.dumpsFrom === "none" && !notices.noDumps.has(current.installId)) {
          notices.noDumps.add(current.installId);
          toast.add({
            title: "No script_docs for this game",
            description:
              "Definitions, references, conflicts and localization still work. Scope-aware completion, typed hover and scope diagnostics need the game to print its documentation once.",
            color: "warning",
            icon: "i-lucide-triangle-alert",
            duration: 0,
          });
        }
      },
      { immediate: true },
    );

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
    state,
    scan,
    isOpen,
    games,
    originVanilla,
    game,
    mod,
    originName,
    originColor,
    select,
  };
});
