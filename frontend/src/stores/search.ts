/**
 * The search store: the running search's query and its hits per file, taken in once a frame as they arrive, a name's
 * references when those are listed instead, and the results sent from another window. The search view and the panel
 * draw from it; nothing else holds hits.
 */
import { defineStore } from "pinia";
import { useMutation } from "@pinia/colada";
import { useBroadcastChannel, useEventListener } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { groupBy } from "es-toolkit";
import { shallowRef, triggerRef } from "vue";

import * as searchApi from "@services/search/service";
import type { Location } from "@services/lsp/models";
import type { Hits, Query } from "@services/search/models";

/** How the search of the window that started it stands, sent on the channel every window hears. */
type Results = {
  type: "results";
  query: Query | null;
  title: string;
  running: boolean;
  capped: boolean;
  reset: boolean;
  hits: Hits[];
};

export const useSearchStore = defineStore("search", () => {
  const query = shallowRef<Query | null>(null);
  // what the results are, when they are not a search's, such as a name's references
  const title = shallowRef("");
  const running = shallowRef(false);
  // whether the search stopped at the most hits Go sends
  const capped = shallowRef(false);
  const files = shallowRef<Hits[]>([]);
  // the hits that arrived since the last frame, which the next frame takes in
  let arrived: Hits[] = [];
  // whether the hits that arrive belong to this window's own search
  let started = false;
  const { channel, post } = useBroadcastChannel<Results, Results>({ name: "pmt.windows" });
  const { mutate } = useMutation({
    mutation: async (next: Query) => {
      await searchApi.StopSearch();
      files.value = [];
      arrived = [];
      started = true;
      return next.text ? searchApi.SearchText(next) : false;
    },
    onSettled: (stopped, _error, next) => {
      if (next !== query.value) return;
      running.value = false;
      capped.value = !!stopped;
      sendResults({});
    },
  });

  /** Sends the search's standing to the other windows. A message carries no new hits unless it is given some. */
  function sendResults(change: Partial<Pick<Results, "reset" | "hits">>): void {
    const current = query.value && { ...query.value, origins: [...(query.value.origins ?? [])] };
    const message = { query: current, title: title.value, running: running.value, capped: capped.value };
    post({ type: "results", ...message, reset: false, hits: [], ...change });
  }

  /** Takes in the hits that arrived since the last frame, and sends them to the other windows. */
  function takeArrived(): void {
    if (!arrived.length) return;
    files.value.push(...arrived);
    triggerRef(files);
    sendResults({ hits: arrived });
    arrived = [];
  }

  /**
   * The results: the search's query or what else they are, whether the search still runs or stopped at the most hits
   * Go sends, and the hits per file.
   */
  const current = (): { query: Query | null; title: string; running: boolean; capped: boolean; files: Hits[] } => ({
    query: query.value,
    title: title.value,
    running: running.value,
    capped: capped.value,
    files: files.value,
  });

  /** Starts a search, stopping the last one. The hits arrive through the search:hits event. */
  const search = (next: Query): void => {
    started = false;
    query.value = next;
    title.value = "";
    running.value = !!next.text;
    capped.value = false;
    sendResults({ reset: true });
    mutate(next);
  };

  /** Lists places as the results under the given title, as a name's references, in place of a search's hits. */
  const showReferences = (heading: string, locations: Location[]): void => {
    started = false;
    void searchApi.StopSearch();
    query.value = null;
    title.value = heading;
    running.value = false;
    capped.value = false;
    files.value = Object.values(groupBy(locations, (location) => location.place.key)).flatMap(([first, ...rest]) =>
      first ? [{ place: first.place, hits: [first, ...rest] }] : [],
    );
    sendResults({ reset: true, hits: files.value });
  };

  /** Subscribes to search:hits and to the results other windows send. Called once at startup. */
  const subscribe = (): void => {
    Events.On("search:hits", ({ data }) => {
      if (!started) return;
      if (!arrived.length) requestAnimationFrame(takeArrived);
      arrived.push(data);
    });
    useEventListener(channel, "message", ({ data }: MessageEvent<Results>) => {
      if (data.type !== "results") return;
      started = false;
      query.value = data.query;
      title.value = data.title;
      running.value = data.running;
      capped.value = data.capped;
      files.value = data.reset ? data.hits : [...files.value, ...data.hits];
    });
  };

  return { current, search, showReferences, subscribe };
});
