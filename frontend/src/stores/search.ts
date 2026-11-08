/** The search store: the current search's query and its hits per file, or a name's references listed instead. */
import { defineStore } from "pinia";
import { useMutation } from "@pinia/colada";
import { useBroadcastChannel, useEventListener } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { groupBy } from "es-toolkit";
import { shallowRef, triggerRef } from "vue";

import * as searchApi from "@services/search/service";
import type { Site } from "@services/lsp/models";
import type { Hits, Query } from "@services/search/models";
import { THIS_WINDOW } from "./windows";

/** Results is the standing of a search, which the window that started it sends to the others. */
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
  const title = shallowRef("");
  const running = shallowRef(false);
  const capped = shallowRef(false);
  const files = shallowRef<Hits[]>([]);
  let arrived: Hits[] = [];
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
  const { mutate: searchFile } = useMutation({
    mutation: ({ asked, path }: { asked: Query; path: string }) => searchApi.SearchFile(asked, path),
    onSuccess: (found, { asked }) => {
      if (asked === query.value && !running.value) replaceHits(found);
    },
  });

  /** sendResults sends the search's standing to the other windows. */
  function sendResults(change: Partial<Pick<Results, "reset" | "hits">>): void {
    const plainQuery = query.value && { ...query.value, origins: [...(query.value.origins ?? [])] };
    const message = { query: plainQuery, title: title.value, running: running.value, capped: capped.value };
    post({ type: "results", ...message, reset: false, hits: [], ...change });
  }

  /**
   * replaceHits puts a file's new hits in place of its old ones and sends the results to the other windows. A file with
   * no hits left drops out, and a file with hits it did not have goes last.
   */
  function replaceHits(found: Hits): void {
    const index = files.value.findIndex((file) => file.place.key === found.place.key);
    if (index < 0 && !found.hits?.length) return;
    if (index < 0) files.value.push(found);
    else if (found.hits?.length) files.value[index] = found;
    else files.value.splice(index, 1);
    triggerRef(files);
    sendResults({ reset: true, hits: files.value });
  }

  /** takeArrived adds the hits that arrived since the last frame and sends them to the other windows. */
  function takeArrived(): void {
    if (!arrived.length) return;
    files.value.push(...arrived);
    triggerRef(files);
    sendResults({ hits: arrived });
    arrived = [];
  }

  /** current returns the results: the query or title, the search's state, and the hits. */
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

  /** refresh searches a file again with the current query, once its text has changed. A list of references stays. */
  const refresh = (path: string): void => {
    if (query.value?.text) searchFile({ asked: query.value, path });
  };

  /** showReferences lists the given sites under the given heading in place of a search's hits. */
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

  /**
   * subscribe listens for search:hits, for the results other windows send and, in the main window, for files changed on
   * disk. Call it once at startup.
   */
  const subscribe = (): void => {
    Events.On("search:hits", ({ data }) => {
      if (!started) return;
      if (!arrived.length) requestAnimationFrame(takeArrived);
      arrived.push(data);
    });
    if (THIS_WINDOW === "main") Events.On("fs:changed", ({ data }) => refresh(data.path));
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

  return { current, search, refresh, showReferences, subscribe };
});
