/**
 * Shared language-health query: one fetch per workspace, not per mounted strip.
 */
import { computed, reactive, toValue, watch, type MaybeRefOrGetter } from "vue";
import { useQuery, useQueryCache } from "@pinia/colada";
import { useEventListener } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { useToast } from "@nuxt/ui/composables/useToast";
import { GetLanguageHealth } from "@services/sessionservice";

type ScanState = {
  scanning: boolean;
  pct: number;
  msg: string;
};

const scanByInstall = new Map<string, ScanState>();
/** Installs whose game update was already told this process: once, not on every navigation. */
const updatedToasted = new Set<string>();
/** Same, for an install with no dumps. */
const docsToasted = new Set<string>();
let scanListening = false;
let sessionQueryCache: ReturnType<typeof useQueryCache> | null = null;

/** Bust health + IDE boot so a failed first open remounts once the session is ready. */
async function bustSessionQueries(cache: ReturnType<typeof useQueryCache> | null): Promise<void> {
  if (!cache) return;
  await Promise.all([cache.invalidateQueries({ key: ["session"] }), cache.invalidateQueries({ key: ["ide-boot"] })]);
}

function scanState(id: string): ScanState {
  let s = scanByInstall.get(id);
  if (!s) {
    s = reactive({ scanning: false, pct: 0, msg: "" });
    scanByInstall.set(id, s);
  }
  return s;
}

function ensureScanListener(): void {
  if (scanListening) return;
  scanListening = true;
  Events.On("lang:scan-progress", (ev: { data?: unknown }) => {
    const data = ev.data;
    if (!data || typeof data !== "object") return;
    const row = data as { installId?: string; percent?: number; stage?: string };
    if (!row.installId) return;
    const s = scanState(row.installId);
    s.scanning = true;
    if (typeof row.percent === "number") s.pct = row.percent;
    if (typeof row.stage === "string") s.msg = row.stage;
  });
  // one session is built at a time, so its readiness or failure ends every scan shown; a failure
  // busts no query, since the rebuild would fail again
  Events.On("lang:ready", () => {
    for (const s of scanByInstall.values()) s.scanning = false;
    void bustSessionQueries(sessionQueryCache);
  });
  Events.On("lang:failed", () => {
    for (const s of scanByInstall.values()) s.scanning = false;
  });
  // a game's wiki pages were refreshed: the Guide pane reads them again, and a failure is told
  Events.On("wiki:updated", (ev: { data?: { gameId?: string; error?: string } }) => {
    void sessionQueryCache?.invalidateQueries({ key: ["guide"] });
    if (ev.data?.error) useToast().add({ title: "Wiki fetch failed", description: ev.data.error, color: "error" });
  });
}

/** Health payload and shared scan progress for a workspace. */
export function useLanguageHealth(workspaceId: MaybeRefOrGetter<string>) {
  ensureScanListener();
  const queryCache = useQueryCache();
  sessionQueryCache = queryCache;
  const id = computed(() => toValue(workspaceId));
  const { data: health, refetch } = useQuery({
    key: () => ["session", "health", id.value],
    query: async () => {
      const wsId = id.value;
      if (!wsId) return null;
      return GetLanguageHealth(wsId);
    },
    enabled: () => !!id.value,
    refetchOnWindowFocus: false,
  });
  const scan = computed(() => scanState(health.value?.installId || id.value || "_"));

  useEventListener(window, "focus", () => {
    if (id.value) void refetch();
  });

  const lastScanned = computed(() => {
    const raw = health.value?.scannedAt;
    if (!raw) return "";
    const ms = Date.parse(raw);
    if (Number.isNaN(ms)) return raw;
    return new Date(ms).toLocaleString();
  });

  // a scan that replaced another version's: the game was updated, and the new scan is already done
  watch(
    () => ({ installId: health.value?.installId, from: health.value?.updatedFrom, to: health.value?.gameVersion }),
    (s) => {
      if (!s.installId || !s.from || updatedToasted.has(s.installId)) return;
      updatedToasted.add(s.installId);
      useToast().add({
        title: "Game updated",
        description: `Game updated from ${s.from} to ${s.to} — rescanned.`,
        color: "warning",
        icon: "i-lucide-refresh-cw",
        duration: 0,
      });
    },
    { immediate: true },
  );

  // the type system is the one part of the engine the user makes, so its absence is told once
  watch(
    () => ({
      installId: health.value?.installId,
      missing: !!health.value?.ready && health.value.dumpsFrom === "none",
    }),
    (s) => {
      if (!s.installId || !s.missing || docsToasted.has(s.installId)) return;
      docsToasted.add(s.installId);
      useToast().add({
        title: "No script_docs for this game",
        description:
          "Definitions, references, conflicts and localization still work. Scope-aware completion, typed hover and scope diagnostics need the game to print its documentation once.",
        color: "warning",
        icon: "i-lucide-triangle-alert",
        duration: 0,
      });
    },
    { immediate: true },
  );

  return {
    health,
    scanning: computed(() => scan.value.scanning),
    scanPct: computed(() => scan.value.pct),
    scanMsg: computed(() => scan.value.msg),
    lastScanned,
  };
}
