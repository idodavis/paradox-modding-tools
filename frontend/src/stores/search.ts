/**
 * The search store: the current search's query and its hits per file, or a name's references listed instead. Hits are
 * added once per frame as they arrive. It also takes in the results another window sends. The search view and the
 * panel read from it; nothing else holds hits.
 */
import { defineStore } from "pinia";
import { useMutation } from "@pinia/colada";
import { useBroadcastChannel, useEventListener } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { groupBy } from "es-toolkit";
import { shallowRef, triggerRef } from "vue";

import * as searchApi from "@services/search/service";
import type { Site } from "@services/lsp/models";
import type { Hits, Query } from "@services/search/models";

/** Results is the standing of a search, which the window that started it sends on the channel every window hears. */
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
  // the heading of the results when they are a name's references, not a search's
  const title = shallowRef("");
  const running = shallowRef(false);
  // whether the search stopped at the most hits Go sends
  const capped = shallowRef(false);
  const files = shallowRef<Hits[]>([]);
  // the hits that arrived since the last frame
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

  /** sendResults sends the search's standing to the other windows. It carries no hits unless the change gives some. */
  function sendResults(change: Partial<Pick<Results, "reset" | "hits">>): void {
    const plainQuery = query.value && { ...query.value, origins: [...(query.value.origins ?? [])] };
    const message = { query: plainQuery, title: title.value, running: running.value, capped: capped.value };
    post({ type: "results", ...message, reset: false, hits: [], ...change });
  }

  /** takeArrived adds the hits that arrived since the last frame to the files, and sends them to the other windows. */
  function takeArrived(): void {
    if (!arrived.length) return;
    files.value.push(...arrived);
    triggerRef(files);
    sendResults({ hits: arrived });
    arrived = [];
  }

  /** current returns the results: the query or title, whether the search runs or hit Go's limit, and the hits. */
  const current = (): { query: Query | null; title: string; running: boolean; capped: boolean; files: Hits[] } => ({
    query: query.value,
    title: title.value,
    running: running.value,
    capped: capped.value,
    files: files.value,
  });

  /** search starts a search and stops the last one. The hits arrive through the search:hits event. */
  const search = (next: Query): void => {
    started = false;
    query.value = next;
    title.value = "";
    running.value = !!next.text;
    capped.value = false;
    sendResults({ reset: true });
    mutate(next);
  };

  /** showReferences lists the given sites as the results under the given heading, in place of a search's hits. */
  const showReferences = (heading: string, sites: Site[]): void => {
    started = false;
    void searchApi.StopSearch();
    query.value = null;
    title.value = heading;
    running.value = false;
    capped.value = false;
    files.value = Object.values(groupBy(sites, (site) => site.place.key)).flatMap(([first, ...rest]) =>
      first ? [{ place: first.place, hits: [first, ...rest] }] : [],
    );
    sendResults({ reset: true, hits: files.value });
  };

  /** subscribe listens for search:hits and for the results other windows send. Call it once at startup. */
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
