/** The store of the uploads to Steam, one for each mod and listing. Only one runs at a time. */
import { defineStore } from "pinia";
import { useQueryCache } from "@pinia/colada";
import { ref } from "vue";
import { Events, type CancellablePromise } from "@wailsio/runtime";

import { keys } from "../queries";
import * as steam from "@services/steam/service";
import type { Listing, Progress } from "@services/steam/models";

/** An Attempt is one try at uploading a listing to Steam, running or ended. */
type Attempt = {
  status: "running" | "done" | "failed";
  progress: Progress | null;
  heard: number;
  message: string;
  listing: Listing;
};

/** STAGES holds each stage of an upload by Go's name. Stall is how many idle minutes count as stalled. */
export const STAGES: Record<string, { words: string; pip: number; stall: number }> = {
  preparing: { words: "Preparing the upload", pip: 0, stall: 15 },
  uploading: { words: "Sending files to Steam", pip: 2, stall: 5 },
  committing: { words: "Saving the page and note", pip: 3, stall: 15 },
  done: { words: "Finishing", pip: 4, stall: 15 },
};

/** useUploadStore returns the store of the uploads. */
export const useUploadStore = defineStore("upload", () => {
  const cache = useQueryCache();
  const attempts = ref<Record<string, Attempt>>({});
  // the id of the upload that runs, or "" when none does
  const active = ref("");
  let running: CancellablePromise<void> | undefined;

  Events.On("steam:progress", ({ data }) => {
    const attempt = attempts.value[active.value];
    if (!attempt) return;
    if (isAhead(data, attempt.progress)) attempt.heard = Date.now();
    attempt.progress = data;
  });

  /** find returns the upload of a mod's listing. It is undefined if there is none. */
  const find = (modId: string, key: string): Attempt | undefined => attempts.value[toId(modId, key)];

  /**
   * start writes a mod's listing and uploads it to Steam. A visibility of "" keeps the item's own. It does nothing
   * while another upload runs, and a failure is kept in the upload's state.
   */
  async function start(modId: string, key: string, listing: Listing, visibility: string): Promise<void> {
    if (active.value) return;
    const id = toId(modId, key);
    active.value = id;
    attempts.value[id] = { status: "running", progress: null, heard: Date.now(), message: "", listing: { ...listing } };
    try {
      await steam.WriteListing(modId, key, listing);
      running = steam.Publish(modId, key, visibility);
      await running;
      attempts.value[id].status = "done";
    } catch (error) {
      Object.assign(attempts.value[id], {
        status: "failed",
        message: error instanceof Error ? error.message : String(error),
      });
    } finally {
      active.value = "";
      running = undefined;
      await cache.invalidateQueries({ key: keys.release(modId) });
    }
  }

  /** stop cancels the running upload. It ends with Go's error. */
  function stop(): void {
    void running?.cancel();
  }

  /** keepWaiting restarts the stall count of a listing's upload. */
  function keepWaiting(modId: string, key: string): void {
    const attempt = find(modId, key);
    if (attempt) attempt.heard = Date.now();
  }

  /** clear forgets a listing's upload once it has ended. A running upload stays. */
  function clear(modId: string, key: string): void {
    if (find(modId, key)?.status !== "running") delete attempts.value[toId(modId, key)];
  }

  return { active, find, start, stop, keepWaiting, clear };
});

/** isAhead reports whether a progress is further than the last one. It is true if there is no last one. */
function isAhead(progress: Progress, last: Progress | null): boolean {
  if (!last) return true;
  if (progress.stage !== last.stage) return STAGES[progress.stage].pip > STAGES[last.stage].pip;
  return progress.done > last.done;
}

/** toId returns the id an upload is kept under. */
function toId(modId: string, key: string): string {
  return `${modId}:${key}`;
}
