/**
 * Go's events, subscribed once at startup (main.ts, in the app's context): each refreshes the queries it
 * changes. The IDE's own subscriptions (fs:changed to its explorer, search:hits to its search) stay in the
 * components that draw them.
 */
import { useQueryCache } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import { scans } from "./composables/useLanguageHealth";
import { fileKeys, installKeys, languageKeys, sessionKeys, wikiKeys } from "./queries";

export function subscribeEvents(): void {
  const queryCache = useQueryCache();
  const toast = useToast();
  Events.On("lang:scan-progress", ({ data }) => {
    scans.set(data.installId, { percent: data.percent, stage: data.stage });
  });
  // one session is built at a time, so its readiness or failure ends every scan shown
  Events.On("lang:ready", ({ data: id }) => {
    scans.clear();
    void queryCache.invalidateQueries({ key: sessionKeys.live(id) });
    void queryCache.invalidateQueries({ key: languageKeys.one(id) });
  });
  // a failure refreshes no query: the rebuild would fail again
  Events.On("lang:failed", ({ data: id }) => {
    scans.clear();
    void queryCache.invalidateQueries({ key: languageKeys.one(id) });
  });
  // an install was scanned or read from its cache: its scan time and version
  Events.On("lang:cache-updated", () => {
    void queryCache.invalidateQueries({ key: installKeys.all });
    void queryCache.invalidateQueries({ key: languageKeys.all });
  });
  // a file changed on disk is read again; an editor with nothing unsaved shows the new text
  Events.On("fs:changed", ({ data }) => void queryCache.invalidateQueries({ key: fileKeys.text(data.path) }));
  Events.On("wiki:updated", ({ data }) => {
    void queryCache.invalidateQueries({ key: wikiKeys.all });
    if (data.error) toast.add({ title: "Wiki fetch failed", description: data.error, color: "error" });
  });
}
