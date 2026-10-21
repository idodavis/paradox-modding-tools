/**
 * The search store: the running search's query and its hits per file as they arrive, and the results relayed from
 * another window. The search view and the panel draw from it; nothing else holds hits.
 */
import { defineStore } from "pinia";

import type { Hits, Query } from "@services/search/models";

export const useSearchStore = defineStore("search", () => {
  /** The running search's query, whether it still runs, and its hits per file. */
  const current = (): { query: Query | null; running: boolean; files: Hits[] } => {
    throw new Error("stub");
  };
  /** Starts a search, stopping the last one. The hits arrive through the search:hits event. */
  const search = (query: Query): void => {
    throw new Error("stub", { cause: query });
  };
  /** Subscribes to search:hits and to the results other windows relay. Called once at startup. */
  const subscribe = (): void => {
    throw new Error("stub");
  };
  return { current, search, subscribe };
});
