/**
 * Pinia store for active game, workspace list, and selection.
 */
import { computed, ref, watch } from "vue";
import { defineStore } from "pinia";
import { useLocalStorage } from "@vueuse/core";
import {
  ListWorkspaces,
  GetWorkspace,
  ListGames,
} from "@services/workspaceservice";
import { EnsureSession } from "@services/sessionservice";
import { ReadFileBase64 } from "@services/fileservice";
import { Workspace, WorkspaceMod } from "@services/models";
import type { GameInfo } from "@services/internal/game/models";

/** Workspace default loc language choices (empty/english is the inherit lang). */
export const LOC_LANG_ITEMS: { label: string; value: string }[] = [
  { label: "English", value: "english" },
  { label: "French", value: "french" },
  { label: "German", value: "german" },
  { label: "Spanish", value: "spanish" },
  { label: "Russian", value: "russian" },
  { label: "Korean", value: "korean" },
  { label: "Simplified Chinese", value: "simp_chinese" },
  { label: "Polish", value: "polish" },
  { label: "Turkish", value: "turkish" },
  { label: "Brazilian Portuguese", value: "braz_por" },
  { label: "Japanese", value: "japanese" },
];

/** MIME type for a mod thumbnail path (png/jpeg/svg). */
export function thumbMime(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  switch (ext) {
    case "png":
      return "image/png";
    case "jpg":
    case "jpeg":
      return "image/jpeg";
    case "svg":
      return "image/svg+xml";
    case "gif":
      return "image/gif";
    case "webp":
      return "image/webp";
    default:
      return "application/octet-stream";
  }
}

/** A mod's color: its hand-picked hex, else its slot's token. */
export function modColor(mod: { color?: string; slot?: number }): string {
  return mod.color || `var(--o${mod.slot || 1})`;
}

function revokeAll(urls: Record<string, string>): void {
  for (const u of Object.values(urls)) URL.revokeObjectURL(u);
}

/** Shared workspace library and active-workspace selection. */
export const useWorkspaceStore = defineStore("workspace", () => {
  const currentGameId = useLocalStorage("workspace.gameId", "ck3");
  const activeWorkspaceId = useLocalStorage("workspace.activeId", "");
  const workspaces = ref<Workspace[]>([]);
  const activeWorkspace = ref<Workspace | null>(null);
  const workspaceMods = ref<WorkspaceMod[]>([]);
  const games = ref<GameInfo[]>([]);
  const originVanilla = ref("vanilla");
  const thumbUrls = ref<Record<string, string>>({});
  const loading = ref(false);
  const error = ref<string | null>(null);

  let loadToken = 0;
  let inflight: Promise<void> | null = null;
  let inflightId = "";

  const hasWorkspaces = computed(() => workspaces.value.length > 0);
  const activeWorkspaceName = computed(
    () => activeWorkspace.value?.name ?? "No workspace",
  );
  const gameOptions = computed(() =>
    games.value.map((g) => ({ label: g.shortName, value: g.id })),
  );
  const workspacesByGame = computed(() =>
    games.value.map((g) => ({
      gameId: g.id,
      label: g.shortName,
      workspaces: workspaces.value.filter((w) => w.gameId === g.id),
    })).filter((s) => s.workspaces.length > 0),
  );

  /** Short name from the registry, or the raw id. */
  function shortName(id: string): string {
    return games.value.find((g) => g.id === id)?.shortName ?? id;
  }

  /** Full title from the registry, or "Game". */
  function gameName(id: string): string {
    return games.value.find((g) => g.id === id)?.name ?? "Game";
  }

  /** An origin's name: the game's for vanilla, else its mod's; the id when neither. */
  function originName(id: string): string {
    if (!id || id === originVanilla.value) return gameName(activeWorkspace.value?.gameId ?? "");
    return workspaceMods.value.find((mod) => mod.id === id)?.name ?? id;
  }

  /** An origin's color: the game's own or its near-grey for vanilla, else its mod's. */
  function originColor(id: string): string {
    if (!id || id === originVanilla.value) return activeWorkspace.value?.gameColor || "var(--game)";
    const mod = workspaceMods.value.find((m) => m.id === id);
    return mod ? modColor(mod) : "var(--game)";
  }

  /** FIOS sentence from GameInfo.firstWins. */
  function firstWins(id: string): string {
    return games.value.find((g) => g.id === id)?.firstWins ?? "";
  }

  function applyGameList(list: {
    originVanilla?: string;
    games?: (GameInfo | null)[] | null;
  } | null): void {
    games.value = (list?.games ?? []).filter((g): g is GameInfo => !!g?.id);
    originVanilla.value = list?.originVanilla || "vanilla";
    if (
      games.value.length &&
      !games.value.some((g) => g.id === currentGameId.value)
    ) {
      currentGameId.value = games.value[0]!.id;
    }
  }

  /** Refresh all workspaces (every game). */
  async function refresh(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      applyGameList(await ListGames());
      workspaces.value = (await ListWorkspaces("")) ?? [];
      const id = activeWorkspaceId.value;
      const listed = id
        ? workspaces.value.find((w) => w.id === id)
        : undefined;
      if (listed) {
        activeWorkspace.value = listed;
        workspaceMods.value = listed.mods ?? [];
        if (listed.gameId) currentGameId.value = listed.gameId;
      } else {
        if (id) activeWorkspaceId.value = "";
        activeWorkspace.value = null;
        workspaceMods.value = [];
        if (workspaces.value.length) {
          const match =
            workspaces.value.find((w) => w.gameId === currentGameId.value) ??
            workspaces.value[0];
          if (match?.gameId) currentGameId.value = match.gameId;
        }
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
    }
  }

  /** Load active workspace details and mods (in-flight deduped per id). */
  async function loadActiveWorkspace(): Promise<void> {
    const id = activeWorkspaceId.value;
    if (!id) {
      activeWorkspace.value = null;
      workspaceMods.value = [];
      return;
    }
    if (inflight && inflightId === id) return inflight;
    const token = ++loadToken;
    inflightId = id;
    inflight = (async () => {
      const listed = workspaces.value.find((w) => w.id === id);
      if (listed) {
        if (token !== loadToken) return;
        activeWorkspace.value = listed;
        workspaceMods.value = listed.mods ?? [];
        return;
      }
      try {
        const rec = await GetWorkspace(id);
        if (token !== loadToken) return;
        activeWorkspace.value = rec;
        workspaceMods.value = rec?.mods ?? [];
      } catch {
        if (token !== loadToken) return;
        if (activeWorkspaceId.value === id) activeWorkspaceId.value = "";
        activeWorkspace.value = null;
        workspaceMods.value = [];
      }
    })().finally(() => {
      if (token === loadToken) inflight = null;
    });
    return inflight;
  }

  /** Switch to a different workspace by ID. No-op when already selected. */
  function setActiveWorkspace(id: string | null): void {
    const next = id ?? "";
    if (next === activeWorkspaceId.value) {
      if (next && !activeWorkspace.value) void loadActiveWorkspace();
      return;
    }
    activeWorkspaceId.value = next;
    if (next) {
      const found = workspaces.value.find((w) => w.id === next);
      if (found?.gameId) currentGameId.value = found.gameId;
    }
    void loadActiveWorkspace();
  }

  /** True when this workspace has a live language session (not merely defs on disk). */
  async function ensureReady(): Promise<boolean> {
    const id = activeWorkspaceId.value;
    if (!id) return false;
    try {
      const health = await EnsureSession(id);
      return Boolean(health?.ready);
    } catch {
      return false;
    }
  }

  watch(
    [workspaces, workspaceMods],
    async ([list, active]) => {
      const prev = thumbUrls.value;
      const next: Record<string, string> = {};
      const seen = new Set<string>();
      const mods: WorkspaceMod[] = [];
      for (const m of [...active, ...list.flatMap((w) => w.mods ?? [])]) {
        if (seen.has(m.id)) continue;
        seen.add(m.id);
        mods.push(m);
      }
      for (const m of mods) {
        if (!m.thumbnail) continue;
        try {
          const file = await ReadFileBase64(m.thumbnail);
          if (!file?.exists || !file.b64) continue;
          const bin = Uint8Array.from(atob(file.b64), (c) => c.charCodeAt(0));
          next[m.id] = URL.createObjectURL(new Blob([bin], {
            type: thumbMime(m.thumbnail),
          }));
        } catch {
          /* skip unreadable thumbs */
        }
      }
      thumbUrls.value = next;
      revokeAll(prev);
    },
    { deep: true },
  );

  return {
    currentGameId,
    activeWorkspaceId,
    workspaces,
    activeWorkspace,
    workspaceMods,
    games,
    originVanilla,
    thumbUrls,
    loading,
    error,
    hasWorkspaces,
    activeWorkspaceName,
    gameOptions,
    workspacesByGame,
    shortName,
    gameName,
    originName,
    originColor,
    firstWins,
    refresh,
    loadActiveWorkspace,
    setActiveWorkspace,
    ensureReady,
  };
});
