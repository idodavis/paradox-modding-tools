/**
 * Shared language-health query: one fetch per workspace, not per mounted strip.
 */
import { computed, reactive, toValue, watch, type MaybeRefOrGetter } from "vue";
import { useQuery, useQueryCache } from "@pinia/colada";
import { useEventListener } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { useToast } from "@nuxt/ui/composables/useToast";
import { EnsureSession, GetLanguageHealth, RebuildInstallSemantics } from "@services/sessionservice";

type ScanState = {
  scanning: boolean;
  pct: number;
  msg: string;
};

const scanByInstall = new Map<string, ScanState>();
/** Installs already toasted for CacheStale this process (no per-navigation spam). */
const staleToasted = new Set<string>();
/** Same, for an install with no script_docs: told once, not on every navigation. */
const docsToasted = new Set<string>();
let scanListening = false;
let sessionQueryCache: ReturnType<typeof useQueryCache> | null = null;

/** Bust health + IDE boot so a failed first open remounts after rescan. */
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
    if (typeof row.percent === "number") s.pct = row.percent;
    if (typeof row.stage === "string") s.msg = row.stage;
  });
  Events.On("lang:ready", () => {
    void bustSessionQueries(sessionQueryCache);
  });
  // a game's wiki pages were refreshed: the Guide pane reads them again, and a failure is told
  Events.On("wiki:updated", (ev: { data?: { gameId?: string; error?: string } }) => {
    void sessionQueryCache?.invalidateQueries({ key: ["guide"] });
    if (ev.data?.error) useToast().add({ title: "Wiki fetch failed", description: ev.data.error, color: "error" });
  });
}

/** Health payload, rescan action, and shared scan progress for a workspace. */
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

  const scanLabel = computed(() => (health.value?.scannedAt ? "Rescan" : "Scan"));

  /** Auto-rescan install semantics then reindex; bust session queries. */
  async function rescan(): Promise<void> {
    const wsId = id.value;
    const installId = health.value?.installId;
    if (!wsId || !installId || scan.value.scanning) return;
    scan.value.scanning = true;
    scan.value.pct = 0;
    scan.value.msg = "starting";
    try {
      await RebuildInstallSemantics(installId);
      staleToasted.delete(installId);
      await EnsureSession(wsId);
      await bustSessionQueries(queryCache);
    } finally {
      scan.value.scanning = false;
      scan.value.msg = "";
      scan.value.pct = 0;
    }
  }

  watch(
    () => ({
      stale: health.value?.cacheStale,
      installId: health.value?.installId,
      scannedAt: health.value?.scannedAt,
      scanning: scan.value.scanning,
    }),
    (s) => {
      if (!s.stale || !s.installId || !s.scannedAt || s.scanning) return;
      if (staleToasted.has(s.installId)) return;
      staleToasted.add(s.installId);
      useToast().add({
        title: "Game updated",
        description: "The installed version changed since the last scan. Rescan so the semantic cache matches.",
        color: "warning",
        icon: "i-lucide-refresh-cw",
        duration: 0,
        actions: [
          {
            label: scanLabel.value,
            color: "warning",
            onClick: (e?: Event) => {
              e?.stopPropagation();
              void rescan();
            },
          },
        ],
      });
    },
    { immediate: true },
  );

  // the type system is the one part of the engine the user makes, so its absence is told once
  watch(
    () => ({
      installId: health.value?.installId,
      dumped: (health.value?.dumps ?? []).some((dump) => dump.effects > 0),
    }),
    (s) => {
      if (!s.installId || s.dumped || docsToasted.has(s.installId)) return;
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
    scanLabel,
    rescan,
  };
}
