/**
 * The workspace store holds the active workspace's id, why the last open failed, and what every page reads of the open
 * workspace: its mods and the names and colors of its origins. It also creates, opens and removes workspaces.
 */
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
/** SLOTS lists the theme's color slots a mod picks from, the only colors Go writes as a slot. */
export const SLOTS = Array.from({ length: 16 }, (_, index) => `o${index + 1}`);

export const useWorkspaceStore = defineStore("workspace", () => {
  const id = useLocalStorage("pmt.workspace.activeId", "");
  const toast = useToast();
  const router = useRouter();
  const { data: state } = useQuery(stateQuery);
  const { data: workspaces } = useQuery(workspacesQuery);
  const { mutate: openWorkspace } = useMutation({ mutation: (workspaceId: string) => workspace.Open(workspaceId) });
  // update writes a workspace's record whole, with the fields the caller changed
  const { mutate: update } = useMutation({ mutation: (record: Workspace) => workspace.UpdateWorkspace(record) });
  // removing the active workspace leaves none active, and no other one opens
  const { mutate: deleteWorkspace } = useMutation({
    mutation: (record: Workspace) => workspace.DeleteWorkspace(record.id),
    onSuccess: (_, record) => {
      toast.add({ title: `${record.name} removed`, color: "success" });
      if (id.value === record.id) id.value = "";
    },
  });

  // why the workspace being opened could not open, until a workspace opens
  const failure = shallowRef<{ id: string; reason: string } | null>(null);
  Events.On("workspace:failed", ({ data }) => (failure.value = { id: id.value, reason: data }));
  Events.On("workspace:opened", () => (failure.value = null));

  /** activeId returns the active workspace's id, or "" when there is none. */
  const activeId = (): string => id.value;

  /** isOpen reports whether the active workspace is open in Go, from the state query. */
  const isOpen = (): boolean => !!id.value && !!state.value?.open && state.value.id === id.value;

  /** active returns the active workspace as Go lists it, or undefined when there is none. */
  const active = () => workspaces.value?.find((row) => row.record.id === id.value);

  /** findMod returns a mod of the active workspace as Go reads it, open or not. It is undefined for an unknown mod. */
  const findMod = (modId: string) => active()?.mods?.find((mod) => mod.id === modId);

  /** originName returns the name of the active workspace's game or one of its mods. It is the origin when unknown. */
  const originName = (origin: string): string => {
    if (origin !== VANILLA) return findMod(origin)?.descriptor.name || origin;
    return active()?.game?.name ?? origin;
  };

  /** describeFolder returns a file's folder as a list shows it: its origin's name, then the folder if not the root. */
  const describeFolder = (place: Place): string => [originName(place.origin), place.folder].filter(Boolean).join(" › ");

  /**
   * originColor returns an origin's color as CSS: a slot as its variable, a custom color as it is. The game's files
   * take the active workspace's game color. An origin with no color gets the game's own. The second parameter is the
   * mod's color when the caller has it, as for a workspace that is not the active one.
   */
  const originColor = (origin: string, recorded?: string): string => {
    const color = recorded ?? (origin === VANILLA ? active()?.record.gameColor : findMod(origin)?.color);
    if (!color) return "var(--game)";
    return SLOTS.includes(color) ? `var(--${color})` : color;
  };

  /** isLeftOut reports whether the active workspace leaves a mod out of its compatibility checks. */
  const isLeftOut = (modId: string): boolean => !!findMod(modId)?.leftOut;

  /** isCustom reports whether a mod's color was picked by hand: a CSS color rather than one of the theme's slots. */
  const isCustom = (color: string | undefined): boolean => !!color && !SLOTS.includes(color);

  /**
   * listMods returns the active workspace's mods in load order, as Go reads them. It is empty when there is no active
   * workspace.
   */
  const listMods = (): Mod[] => active()?.mods ?? [];

  /**
   * listWritableMods returns the active workspace's mods that PMT writes changes into, in load order: every mod but
   * Steam's copies of Workshop mods, which are read only, and mods whose folder is missing.
   */
  const listWritableMods = (): Mod[] => listMods().filter((mod) => !mod.workshop && !mod.broken);

  /** listModIds returns the ids of the active workspace's mods in load order. */
  const listModIds = (): string[] => listMods().map((mod) => mod.id);

  /** open makes a workspace active and asks Go to open it. */
  function open(workspaceId: string): void {
    id.value = workspaceId;
    openWorkspace(workspaceId);
  }

  /**
   * create records a workspace with the given name, install and localization language, adds its mods in the given
   * order, opens it and shows the given page of it. A mod with a folder is added; one with none is made, named by its
   * title. It returns the workspace. When a mod's step fails, the workspace it recorded is deleted again and the error
   * is thrown.
   */
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

  /** updateMod writes some fields of a mod's record over its latest copy. It rejects if the mod has no record. */
  async function updateMod(workspaceId: string, modId: string, changes: Partial<ModRecord>): Promise<void> {
    const row = workspaces.value?.find((listed) => listed.record.id === workspaceId);
    const record = row?.record.mods?.find((mod) => mod.id === modId);
    if (!record) throw new Error("The mod's record was not found, so the change was not saved.");
    await workspace.UpdateMod(workspaceId, { ...record, ...changes });
  }

  /**
   * remove asks before the given workspace leaves PMT, then removes it. Its mod folders stay on disk. If it was the
   * active one, no workspace is active afterwards. It returns whether the user confirmed.
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
