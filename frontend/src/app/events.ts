/**
 * Go's events, subscribed once at startup (main.ts, in the app's context): each refreshes the queries it
 * changes. The IDE's own subscriptions (fs:changed to its explorer, search:hits to its search) stay in the
 * components that draw them; the windows store answers the events about windows, and what follows here is the tabs
 * handed between them and the results one shows another.
 */
import { watch } from "vue";
import { useQueryCache } from "@pinia/colada";
import { Events, Window } from "@wailsio/runtime";
import { useDebounceFn } from "@vueuse/core";
import { scans } from "../composables/useLanguageHealth";
import { fileKeys, installKeys, languageKeys, sessionKeys, wikiKeys, workspaceKeys } from "../queries";
import { useEditorStore } from "../stores/editor";
import { useGitStore } from "../stores/git";
import { useSettingsStore } from "../stores/settings";
import { holdsFiles, isMain, useWindowsStore, windowName } from "../stores/windows";

export function subscribeEvents(): void {
  const queryCache = useQueryCache();
  const toast = useToast();
  const settings = useSettingsStore();
  Events.On("lang:scan-progress", ({ data }) => {
    scans.set(data.installId, { percent: data.percent, stage: data.stage });
  });
  // one session is built at a time, so its readiness or failure ends every scan shown; the editors holding its
  // workspace's files tell it their text again
  Events.On("lang:ready", ({ data: id }) => {
    scans.clear();
    void queryCache.invalidateQueries({ key: sessionKeys.live(id) });
    void queryCache.invalidateQueries({ key: languageKeys.one(id) });
    if (holdsFiles && useEditorStore().editingWorkspaceId === id)
      void import("../editor/monacoEditor").then((models) => models.rebuilt());
  });
  // a session closed (its inputs changed, or another workspace opened): its state is read again, which builds it anew
  Events.On("lang:dropped", ({ data: id }) => {
    void queryCache.invalidateQueries({ key: sessionKeys.live(id) });
    void queryCache.invalidateQueries({ key: languageKeys.one(id) });
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
  // a file changed on disk is read again; an editor with nothing unsaved shows the new text; source control reads
  // its repository again once a burst of changes is over
  const git = useGitStore();
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

  const editor = useEditorStore();
  const windows = useWindowsStore();

  // this window asked to close with files of its own: they go back to the main window first
  Events.On("window:closing", async ({ data: name }) => {
    if (name !== windowName || isMain || !holdsFiles) return;
    if (editor.openTabs.length)
      await (await import("../editor/monacoEditor")).handOver("main", editor.openTabs, editor.activeTab);
    void Window.Close();
  });
  Events.On("window:tabs", ({ data }) => {
    if (data.to === windowName)
      windows.receiveTabs(
        (data.tabs ?? []).map((one) => one.tab),
        async () => (await import("../editor/monacoEditor")).receive(data),
      );
  });
  // a tab this window holds, dragged into another
  Events.On("window:give", async ({ data }) => {
    if (data.to === windowName) await (await import("../editor/monacoEditor")).handOver(data.window, [data.tab]);
  });

  // the main window's sidebar shows the Search results another window finds (its references)
  if (isMain) {
    Events.On("window:results", ({ data }) => {
      editor.results = { ...data, rows: data.rows ?? [] };
      editor.sidebar = "search";
    });
  } else if (holdsFiles) {
    watch(
      () => editor.results,
      (results) => results && void Events.Emit("window:results", results),
    );
  }
}
