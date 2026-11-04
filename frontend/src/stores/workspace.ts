/** The workspace store holds the active workspace's id and what every page reads of the open workspace. */
import { defineStore } from "pinia";
import { useMutation, useQuery } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { shallowRef } from "vue";
import { useRouter, type LocationQueryRaw } from "vue-router";

import * as workspace from "@services/workspace/service";
import type { Mod as ModRecord, Workspace } from "@services/app/models";
import type { Mod, Place } from "@services/workspace/models";
import type { WorkspacePage } from "../app/pages";
import { confirm } from "../lib/feedback";
import { stateQuery, workspacesQuery } from "../queries";

/** VANILLA is Go's name for the game's own files as an origin. */
export const VANILLA = "vanilla";
/** SLOTS lists the theme's color slots a mod picks from. */
export const SLOTS = Array.from({ length: 16 }, (_, index) => `o${index + 1}`);

export const useWorkspaceStore = defineStore("workspace", () => {
  const id = useLocalStorage("pmt.workspace.activeId", "");
  const toast = useToast();
  const router = useRouter();
  const { data: state } = useQuery(stateQuery);
  const { data: workspaces } = useQuery(workspacesQuery);
  const { mutate: openWorkspace } = useMutation({ mutation: (workspaceId: string) => workspace.Open(workspaceId) });
  const { mutate: update } = useMutation({ mutation: (record: Workspace) => workspace.UpdateWorkspace(record) });
  const { mutate: deleteWorkspace } = useMutation({
    mutation: (record: Workspace) => workspace.DeleteWorkspace(record.id),
    onSuccess: (_, record) => {
      toast.add({ title: `${record.name} removed`, color: "success" });
      if (id.value === record.id) id.value = "";
    },
  });

  const failure = shallowRef<{ id: string; reason: string } | null>(null);
  Events.On("workspace:failed", ({ data }) => (failure.value = { id: id.value, reason: data }));
  Events.On("workspace:opened", () => (failure.value = null));

  /** activeId returns the active workspace's id, or "" if there is none. */
  const activeId = (): string => id.value;

  /** isOpen reports whether the active workspace is open in Go. */
  const isOpen = (): boolean => !!id.value && !!state.value?.open && state.value.id === id.value;

  /** active returns the active workspace as Go lists it, or undefined. */
  const active = () => workspaces.value?.find((row) => row.record.id === id.value);

  /** findMod returns a mod of the active workspace, or undefined for an unknown mod. */
  const findMod = (modId: string) => active()?.mods?.find((mod) => mod.id === modId);

  /** originName returns the name of the active workspace's game or one of its mods, or the origin if unknown. */
  const originName = (origin: string): string => {
    if (origin !== VANILLA) return findMod(origin)?.descriptor.name || origin;
    return active()?.game?.name ?? origin;
  };

  /** describeFolder returns a file's folder as a list shows it, after its origin's name. */
  const describeFolder = (place: Place): string => [originName(place.origin), place.folder].filter(Boolean).join(" › ");

  /** originColor returns an origin's color as CSS. The given color is the mod's own, for another workspace. */
  const originColor = (origin: string, recorded?: string): string => {
    const color = recorded ?? (origin === VANILLA ? active()?.record.gameColor : findMod(origin)?.color);
    if (!color) return "var(--game)";
    return SLOTS.includes(color) ? `var(--${color})` : color;
  };

  /** isLeftOut reports whether the active workspace leaves a mod out of its compatibility checks. */
  const isLeftOut = (modId: string): boolean => !!findMod(modId)?.leftOut;

  /** isCustom reports whether a mod's color is a CSS color rather than one of the theme's slots. */
  const isCustom = (color: string | undefined): boolean => !!color && !SLOTS.includes(color);

  /** listMods returns the active workspace's mods in load order. */
  const listMods = (): Mod[] => active()?.mods ?? [];

  /** listWritableMods returns the mods PMT writes into: all but Steam's read-only Workshop copies and broken mods. */
  const listWritableMods = (): Mod[] => listMods().filter((mod) => !mod.workshop && !mod.broken);

  /** listModIds returns the ids of the active workspace's mods in load order. */
  const listModIds = (): string[] => listMods().map((mod) => mod.id);

  /** open makes a workspace active and asks Go to open it. */
  function open(workspaceId: string): void {
    id.value = workspaceId;
    openWorkspace(workspaceId);
  }

  /** create records a workspace with its mods, opens it and shows a page. If a mod fails, it deletes the workspace. */
  async function create(
    record: Pick<Workspace, "name" | "install" | "language">,
    mods: { dir?: string; title: string }[],
    page: { name: WorkspacePage; query?: LocationQueryRaw },
  ): Promise<Workspace> {
    const row = await workspace.CreateWorkspace(record.name, record.install, record.language);
    try {
      for (const mod of mods) {
        if (mod.dir) await workspace.AddMod(row.id, mod.dir);
        else await workspace.CreateMod(row.id, "", mod.title, record.language);
      }
    } catch (error) {
      await workspace.DeleteWorkspace(row.id);
      throw error;
    }
    open(row.id);
    void router.replace({ ...page, params: { id: row.id } });
    return row;
  }

  /** updateMod writes some fields of a mod's record. It rejects if the mod has no record. */
  async function updateMod(workspaceId: string, modId: string, changes: Partial<ModRecord>): Promise<void> {
    const row = workspaces.value?.find((listed) => listed.record.id === workspaceId);
    const record = row?.record.mods?.find((mod) => mod.id === modId);
    if (!record) throw new Error("The mod's record was not found, so the change was not saved.");
    await workspace.UpdateMod(workspaceId, { ...record, ...changes });
  }

  /** remove asks, then removes a workspace but not its mod folders. It returns the user's answer. */
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
    failure,
    activeId,
    isOpen,
    active,
    update,
    originName,
    describeFolder,
    originColor,
    isLeftOut,
    isCustom,
    listMods,
    listWritableMods,
    listModIds,
    open,
    create,
    updateMod,
    remove,
  };
});
