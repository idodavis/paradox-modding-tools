/**
 * Go's events, subscribed once at startup (main.ts, in the app's context): each refreshes the queries it changes.
 * The IDE's own subscriptions stay with the modules that draw them: the explorer's (fs:changed), the search's
 * (search:hits), the windows' (window:*).
 */
import { useQueryCache } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import { useDebounceFn } from "@vueuse/core";
import { resendOpenFiles, textEdited } from "../editor/models";
import { drawKeys, fileKeys, installKeys, symbolKeys, wikiKeys, workspaceKeys } from "../queries";
import { useEditorStore } from "../stores/editor";
import { useGitStore } from "../stores/git";
import { useSettingsStore } from "../stores/settings";
import { holdsFiles } from "../stores/windows";
import { scans } from "../stores/workspace";

export function subscribeEvents(): void {
  const queryCache = useQueryCache();
  const toast = useToast();
  const settings = useSettingsStore();
  const git = useGitStore();
  const editor = useEditorStore();
  // what an edit changes besides its file's text: the windows drawn, the properties of their parts, and the names its
  // file defines
  const drawn = [drawKeys.list, drawKeys.properties, symbolKeys.all];

  Events.On("install:scanning", ({ data }) => {
    scans.set(data.installId, { percent: data.percent, stage: data.stage });
  });
  // one workspace is open at a time, so its opening or failure ends every scan shown; the editors holding its
  // files send it their text again
  Events.On("workspace:opened", ({ data: id }) => {
    scans.clear();
    void queryCache.invalidateQueries({ key: workspaceKeys.open(id) });
    void queryCache.invalidateQueries({ key: workspaceKeys.state(id) });
    for (const key of drawn) void queryCache.invalidateQueries({ key });
    if (holdsFiles && editor.editingWorkspaceId === id) resendOpenFiles();
  });
  // the open workspace closed (its inputs changed, or another opened): its state is read again, and its pages'
  // reads open it again where one is shown
  Events.On("workspace:closed", ({ data: id }) => {
    void queryCache.invalidateQueries({ key: workspaceKeys.open(id) });
    void queryCache.invalidateQueries({ key: workspaceKeys.state(id) });
  });
  // a failure refreshes no read of the index: the build would fail again
  Events.On("workspace:failed", ({ data: id }) => {
    scans.clear();
    void queryCache.invalidateQueries({ key: workspaceKeys.state(id) });
  });
  // an install was scanned or read from its cache: its scan time and version, in every workspace's state
  Events.On("install:scanned", () => {
    void queryCache.invalidateQueries({ key: installKeys.all });
    void queryCache.invalidateQueries({ key: workspaceKeys.states });
  });
  // config.json was written, in any window: what its section feeds is read again (a reset, "", all of it). The
  // workspaces and installs feed the repositories they hold and Publish's listings; the workspace's state first
  // asks Go, which closes the open workspace where its inputs changed (workspace:closed)
  Events.On("config:changed", ({ data: section }) => {
    if (section !== "settings")
      for (const key of [workspaceKeys.all, installKeys.all, workspaceKeys.states, ["git-repos"], ["release"]])
        void queryCache.invalidateQueries({ key });
    if (section === "settings" || !section) void settings.load();
  });
  // a file changed on disk is read again; source control reads its repository and the windows drawn are drawn again
  // once a burst of changes is over, any file being a template or texture of a window
  const gitChanged = useDebounceFn(() => git.refresh(), 500);
  const drawChanged = useDebounceFn(() => {
    for (const key of drawn) void queryCache.invalidateQueries({ key });
  }, 300);
  Events.On("fs:changed", ({ data }) => {
    void queryCache.invalidateQueries({ key: fileKeys.text(data.path) });
    void queryCache.invalidateQueries({ key: fileKeys.image(data.path) });
    void drawChanged();
    void gitChanged();
  });
  // an edit the backend has now, saved or not: the windows drawn from it are drawn again
  textEdited.event(() => {
    for (const key of drawn) void queryCache.invalidateQueries({ key });
  });
  Events.On("git:output", ({ data }) => git.appendOutput(data));
  Events.On("wiki:updated", ({ data }) => {
    void queryCache.invalidateQueries({ key: wikiKeys.all });
    if (data.error) toast.add({ title: "Wiki fetch failed", description: data.error, color: "error" });
  });
}
