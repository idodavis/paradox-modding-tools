/**
 * The newest game version whose patches name the workspace's mods, and the
 * newest one the Patch Center has shown, kept for each workspace.
 */
import { useQuery } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import { computed } from "vue";

import { notesQuery, reportQuery, tiesQuery } from "../queries";
import { useWorkspaceStore } from "./workspace";

/**
 * useSeenPatch returns the newest version that touches the workspace's mods
 * and the version the Patch Center last showed, which the page sets. It also
 * returns whether the first is newer than the second. With nothing shown yet,
 * any version that touches the mods is newer.
 */
export function useSeenPatch() {
  const workspace = useWorkspaceStore();
  const { data: notes } = useQuery(() => ({ ...notesQuery, enabled: workspace.isOpen() }));
  const { data: ties } = useQuery(() => ({ ...tiesQuery, enabled: workspace.isOpen() }));
  const { data: report } = useQuery(() => ({ ...reportQuery(workspace.listModIds()), enabled: workspace.isOpen() }));
  const shown = useLocalStorage(() => `pmt.patchCenter.shown.${workspace.activeId()}`, "");

  const newest = computed(() => {
    const touching = new Set([
      ...(ties.value ?? []).map((tie) => tie.version),
      ...(report.value?.patch.findings ?? []).map((finding) => finding.facts?.version ?? ""),
    ]);
    return notes.value?.find((note) => touching.has(note.version))?.version ?? "";
  });
  const isNew = computed(() => {
    const position = (version: string) => notes.value?.findIndex((note) => note.version === version) ?? -1;
    return !!newest.value && (!shown.value || position(newest.value) < position(shown.value));
  });

  return { newest, shown, isNew };
}
