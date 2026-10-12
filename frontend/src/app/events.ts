/**
 * Go's events, subscribed once at startup (main.ts, in the app's context): each refreshes the queries it changes.
 * The IDE's own subscriptions stay with the modules that draw them: the explorer's (fs:changed), the search's
 * (search:hits), the windows' (window:*).
 */
import { useQueryCache } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import { useDebounceFn } from "@vueuse/core";
import { scans } from "../composables/useLanguageHealth";
import { fileKeys, installKeys, languageKeys, sessionKeys, wikiKeys, workspaceKeys } from "../queries";
import { useEditorStore } from "../stores/editor";
import { useGitStore } from "../stores/git";
import { useSettingsStore } from "../stores/settings";
import { holdsFiles } from "../stores/windows";

export function subscribeEvents(): void {
  const queryCache = useQueryCache();
  const toast = useToast();
  const settings = useSettingsStore();
  const git = useGitStore();
  const editor = useEditorStore();

  Events.On("lang:scan-progress", ({ data }) => {
    scans.set(data.installId, { percent: data.percent, stage: data.stage });
  });
  // one session is built at a time, so its readiness or failure ends every scan shown; the editors holding its
  // workspace's files send it their text again
  Events.On("lang:ready", ({ data: id }) => {
    scans.clear();
    void queryCache.invalidateQueries({ key: sessionKeys.live(id) });
    void queryCache.invalidateQueries({ key: languageKeys.one(id) });
    if (holdsFiles && editor.editingWorkspaceId === id) editor.sessionRebuilt();
  });
  // a session closed (its inputs changed, or another workspace opened): its state is read again, which builds it anew
  Events.On("lang:dropped", ({ data: id }) => {
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
  // config.json was written, in any window: what its section feeds is read again (a reset, "", all of it). The
  // workspaces and installs feed the repositories they hold and Publish's listings; the language health first asks
  // the session, which closes itself where its inputs changed (lang:dropped)
  Events.On("config:changed", ({ data: section }) => {
    if (section !== "settings")
      for (const key of [workspaceKeys.all, installKeys.all, languageKeys.all, ["git-repos"], ["release"]])
        void queryCache.invalidateQueries({ key });
    if (section === "settings" || !section) void settings.load();
  });
  // a file changed on disk is read again; source control reads its repository again once a burst of changes is over
  const gitChanged = useDebounceFn(() => git.refresh(), 500);
  Events.On("fs:changed", ({ data }) => {
    void queryCache.invalidateQueries({ key: fileKeys.text(data.path) });
    void queryCache.invalidateQueries({ key: fileKeys.image(data.path) });
    void gitChanged();
  });
  Events.On("git:output", ({ data }) => git.appendOutput(data));
  Events.On("wiki:updated", ({ data }) => {
    void queryCache.invalidateQueries({ key: wikiKeys.all });
    if (data.error) toast.add({ title: "Wiki fetch failed", description: data.error, color: "error" });
  });
}
