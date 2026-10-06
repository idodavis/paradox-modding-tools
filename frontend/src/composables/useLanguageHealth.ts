/**
 * A workspace's language state: its install, scan and dumps (read without building a session), the
 * install's scan while one runs, and the two notices told once per install.
 */
import { computed, reactive, toValue, watch, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@pinia/colada";
import type { LanguageHealth } from "@services/models";
import { languageQuery } from "../queries";

/** Each install's scan while it runs, as lang:scan-progress tells it (events.ts); a session built or failed ends them. */
export const scans = reactive(new Map<string, { percent: number; stage: string }>());

/** Whose script_docs a built session reads: the game's own and current, or not. */
export function dumpsState(live: LanguageHealth | null | undefined): { ok: boolean; label: string } | undefined {
  if (!live?.ready) return undefined;
  if (live.dumpsFrom === "yours" && !live.dumpsStale) return { ok: true, label: "script_docs loaded" };
  if (live.dumpsFrom === "yours") return { ok: false, label: "script_docs older than the game" };
  if (live.dumpsFrom === "shipped") return { ok: false, label: "Using PMT's copy of script_docs" };
  return { ok: false, label: "No script_docs" };
}

/** Installs already told of, this run: a game update, and no dumps. */
const told = { updated: new Set<string>(), noDumps: new Set<string>() };

export function useLanguageHealth(workspaceId: MaybeRefOrGetter<string>) {
  const { data: health } = useQuery(() => languageQuery(toValue(workspaceId)));
  const scan = computed(() => scans.get(health.value?.installId ?? ""));
  const toast = useToast();

  watch(
    health,
    (live) => {
      if (!live?.installId) return;
      // a scan that replaced another version's: the game was updated, and the new scan is done
      if (live.updatedFrom && !told.updated.has(live.installId)) {
        told.updated.add(live.installId);
        toast.add({
          title: "Game updated",
          description: `Game updated from ${live.updatedFrom} to ${live.gameVersion} — rescanned.`,
          color: "warning",
          icon: "i-lucide-refresh-cw",
          duration: 0,
        });
      }
      // the type system is the one part of the engine the user makes, so its absence is told once
      if (live.ready && live.dumpsFrom === "none" && !told.noDumps.has(live.installId)) {
        told.noDumps.add(live.installId);
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

  return { health, scan };
}
