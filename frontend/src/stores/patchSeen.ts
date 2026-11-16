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
 * useSeenPatch returns the newest version that touches the workspace's mods,
 * the version the Patch Center last showed, and whether the first is newer.
 */
export function useSeenPatch() {
  const workspace = useWorkspaceStore();
  const { data: notes } = useQuery(() => ({ ...notesQuery, enabled: workspace.isOpen() }));
  const { data: ties } = useQuery(() => ({ ...tiesQuery, enabled: workspace.isOpen() }));
  const { data: report } = useQuery(() => ({ ...reportQuery(workspace.listModIds()), enabled: workspace.isOpen() }));
  const shown = useLocalStorage(() => `pmt.patchCenter.shown.${workspace.activeId()}`, "");

  // An external mod is not the user's to patch, so what touches it is not new.
  const newest = computed(() => {
    const own = new Set(workspace.listOwnMods().map((mod) => mod.id));
    const touching = new Set([
      ...(ties.value ?? []).filter((tie) => own.has(tie.modId)).map((tie) => tie.version),
      ...(report.value?.patch.findings ?? [])
        .filter((finding) => own.has(finding.origin))
        .map((finding) => finding.facts?.version ?? ""),
    ]);
    return notes.value?.find((note) => touching.has(note.version))?.version ?? "";
  });
  const isNew = computed(() => {
    const position = (version: string) => notes.value?.findIndex((note) => note.version === version) ?? -1;
    return !!newest.value && (!shown.value || position(newest.value) < position(shown.value));
  });

  return { newest, shown, isNew };
}
