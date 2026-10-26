/**
 * The workspace store: the active workspace id, kept in local storage, and the facts of the open workspace that
 * every page reads: whether it is open, each origin's name and color, and a file's folder as lists show it.
 */
import { defineStore } from "pinia";
import { useMutation, useQuery } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import { Events } from "@wailsio/runtime";

import * as workspace from "@services/workspace/service";
import type { Mod as ModRecord, Workspace } from "@services/app/models";
import type { Place } from "@services/workspace/models";
import { gamesQuery, installsQuery, modsQuery, stateQuery, workspacesQuery } from "../queries";
import ConfirmModal from "../ui/ConfirmModal.vue";

/** VANILLA is Go's name for the game's own files as an origin. */
export const VANILLA = "vanilla";
/** SLOTS lists the theme's color slots a mod picks from, the only colors Go writes as a slot. */
export const SLOTS = Array.from({ length: 16 }, (_, index) => `o${index + 1}`);

export const useWorkspaceStore = defineStore("workspace", () => {
  const id = useLocalStorage("pmt.workspace.activeId", "");
  const toast = useToast();
  const confirm = useOverlay().create(ConfirmModal);
  const { data: state } = useQuery(stateQuery);
  const { data: workspaces } = useQuery(workspacesQuery);
  const { data: mods } = useQuery(modsQuery);
  const { data: installs } = useQuery(installsQuery);
  const { data: games } = useQuery(gamesQuery);
  const { mutate } = useMutation({ mutation: (workspaceId: string) => workspace.Open(workspaceId) });
  const { mutate: deleteWorkspace } = useMutation({
    mutation: (record: Workspace) => workspace.DeleteWorkspace(record.id),
    onSuccess: (_, record) => {
      toast.add({ title: `${record.name} removed`, color: "success" });
      const first = workspaces.value?.find((other) => other.id !== record.id);
      if (first && id.value === record.id) open(first.id);
    },
  });

  Events.On("workspace:failed", ({ data }) =>
    toast.add({ title: "The workspace could not be opened", description: data, color: "error" }),
  );

  /** The active workspace's id, or "" when there is none. The router's guard writes it. */
  const activeId = (): string => id.value;

  /** Whether the active workspace is open in Go, from the state query. */
  const isOpen = (): boolean => !!id.value && !!state.value?.open && state.value.id === id.value;

  /** The active workspace's record, or undefined when there is none. */
  const activeRecord = () => workspaces.value?.find((record) => record.id === id.value);

  /** A mod of the active workspace as Go reads it, open or not. */
  const findMod = (modId: string) => mods.value?.[id.value]?.find((mod) => mod.id === modId);

  /** An origin's name: the active workspace's game, or one of its mods. It is the origin itself when unknown. */
  const originName = (origin: string): string => {
    if (origin !== VANILLA) return findMod(origin)?.descriptor.name || origin;
    const install = installs.value?.find((one) => one.id === activeRecord()?.install);
    return games.value?.find((game) => game?.id === install?.game)?.name ?? origin;
  };

  /** The folder of a file as a list of files shows it: its origin's name, then the folder in it unless at the root. */
  const describeFolder = (place: Place): string => [originName(place.origin), place.folder].filter(Boolean).join(" › ");

  /**
   * An origin's color as CSS: a slot as its variable, a custom color as it is. The game's files take the active
   * workspace's game color. An origin with no color gets the game's own. The second parameter is the mod's color
   * when the caller has it, as for a workspace that is not the active one.
   */
  const originColor = (origin: string, recorded?: string): string => {
    const color = recorded ?? (origin === VANILLA ? activeRecord()?.gameColor : findMod(origin)?.color);
    if (!color) return "var(--game)";
    return SLOTS.includes(color) ? `var(--${color})` : color;
  };

  /** Whether a mod's color was picked by hand: a CSS color rather than one of the theme's slots. */
  const isCustom = (color: string | undefined): boolean => !!color && !SLOTS.includes(color);

  /** The ids of the open workspace's mods in load order. It is empty while no workspace is open. */
  const listModIds = (): string[] => (state.value?.mods ?? []).map((mod) => mod.id);

  /** open makes a workspace active and asks Go to open it. */
  function open(workspaceId: string): void {
    id.value = workspaceId;
    mutate(workspaceId);
  }

  /** findModRecord returns a mod's record in a workspace. It is undefined if the workspace does not record the mod. */
  const findModRecord = (workspaceId: string, modId: string): ModRecord | undefined =>
    workspaces.value?.find((one) => one.id === workspaceId)?.mods?.find((mod) => mod.id === modId);

  /** updateMod writes some fields of a mod's record over its latest copy. It rejects if the mod has no record. */
  async function updateMod(workspaceId: string, modId: string, changes: Partial<ModRecord>): Promise<void> {
    const record = findModRecord(workspaceId, modId);
    if (!record) throw new Error("The mod's record was not found, so the change was not saved.");
    await workspace.UpdateMod(workspaceId, { ...record, ...changes });
  }

  /**
   * remove asks before the given workspace leaves PMT, then removes it. Its mod folders stay on disk. If it was the
   * active one, the first workspace left becomes active. It returns whether the user confirmed.
   */
  async function remove(record: Workspace): Promise<boolean> {
    const confirmed = await confirm.open({
      title: `Remove ${record.name}?`,
      body: "The workspace leaves PMT. Its mod folders stay on disk, and you can add them to another workspace.",
      confirm: "Remove workspace",
      destructive: true,
    });
    if (confirmed) deleteWorkspace(record);
    return confirmed === true;
  }

  return {
    activeId,
    isOpen,
    originName,
    describeFolder,
    originColor,
    isCustom,
    listModIds,
    open,
    updateMod,
    remove,
  };
});
