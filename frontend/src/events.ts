/**
 * Go's events, subscribed once at startup (main.ts, in the app's context): each refreshes the queries it
 * changes. The IDE's own subscriptions (fs:changed to its explorer, search:hits to its search) stay in the
 * components that draw them. Then the windows' (windows.ts): what each holds, its title, the tabs handed between
 * them, a file or a link shown in another, and what one saved, which the others read again.
 */
import { computed, watch } from "vue";
import type { Router } from "vue-router";
import { useQueryCache } from "@pinia/colada";
import { Events, Window } from "@wailsio/runtime";
import { useDebounceFn, watchDebounced } from "@vueuse/core";
import type { WindowHeld } from "@services/models";
import { scans } from "./composables/useLanguageHealth";
import { fileKeys, installKeys, languageKeys, sessionKeys, wikiKeys, workspaceKeys } from "./queries";
import { useGitStore } from "./stores/git";
import { useIdeStore } from "./stores/ide";
import { useSettingsStore } from "./stores/settings";
import { useWorkspaceStore } from "./stores/workspace";
import { QuitAnswer, Show } from "@services/windowservice";
import UnsavedChangesModal from "./components/app/UnsavedChangesModal.vue";
import { dock, held, holdsFiles, isMain, others, receiveTabs, roots, windowName, windowPart } from "./windows";
import { fileName } from "./lib/format";
import { PAGES } from "./workspaceTools";

export function subscribeEvents(router: Router): void {
  const queryCache = useQueryCache();
  const toast = useToast();
  Events.On("lang:scan-progress", ({ data }) => {
    scans.set(data.installId, { percent: data.percent, stage: data.stage });
  });
  // one session is built at a time, so its readiness or failure ends every scan shown; the editors holding its
  // workspace's files tell it their text again
  Events.On("lang:ready", ({ data: id }) => {
    scans.clear();
    void queryCache.invalidateQueries({ key: sessionKeys.live(id) });
    void queryCache.invalidateQueries({ key: languageKeys.one(id) });
    if (holdsFiles && useIdeStore().editing === id) void import("./ide/monacoEditor").then((editor) => editor.rebuilt());
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
    if (section !== "settings") for (const key of [workspaceKeys.all, installKeys.all, languageKeys.all, ["git-repos"], ["release"]]) void queryCache.invalidateQueries({ key });
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
  Events.On("git:output", ({ data }) => git.told(data));
  Events.On("wiki:updated", ({ data }) => {
    void queryCache.invalidateQueries({ key: wikiKeys.all });
    if (data.error) toast.add({ title: "Wiki fetch failed", description: data.error, color: "error" });
  });

  const ide = useIdeStore();
  const store = useWorkspaceStore();
  const unsavedModal = useOverlay().create(UnsavedChangesModal);
  const settings = useSettingsStore();
  const route = router.currentRoute;
  // a window's title: what it shows, then its workspace
  const title = computed(() => {
    const workspace = store.activeWorkspace;
    const of = (what: string) => (workspace ? `${what}, ${workspace.name}` : what);
    const file = fileName(ide.activeFile);
    if (windowPart === "game") return `Game files, ${store.game(workspace?.gameId ?? "")?.name ?? ""}`;
    if (windowPart === "files") return of(fileName(windowName));
    if (windowPart === "panel") return of("Panel");
    if (windowPart === "editor") return of(file || "Editor");
    if (windowPart === "page") return of(route.value.name ? PAGES[route.value.name].title : "");
    return "Paradox Modding Tools";
  });
  if (!isMain) watch(title, (text) => void Window.SetTitle(text), { immediate: true });

  // what this window holds, told once ready (a window holding files when its editors are set up), and again when it
  // changes while another window is open
  const holding = computed<WindowHeld>(() => ({
    name: windowName,
    title: title.value,
    roots: roots.value,
    tabs: ide.openFiles,
    unsaved: [...ide.unsaved],
    problems: Object.fromEntries(ide.problems),
  }));
  const ready = computed(() => !holdsFiles || isMain || !!ide.editing);
  const tell = () => ready.value && void Events.Emit("window:held", holding.value);
  watchDebounced([holding, ready, () => others.size], () => others.size && tell(), { debounce: 100 });
  // a window opened asks what the others hold
  Events.On("window:hello", ({ data: name }) => name !== windowName && tell());
  Events.On("window:held", ({ data }) => data.name !== windowName && held(data));
  void Events.Emit("window:hello", windowName);
  // a window closed: its tabs came back before; the panel shows here again
  Events.On("window:closed", ({ data: name }) => {
    others.delete(name);
    if (name === "panel" && isMain) ide.panel ||= "problems";
  });
  // this window asked to close: its tabs go back to the main window first
  Events.On("window:closing", async ({ data: name }) => {
    if (name !== windowName) return;
    // the main window's close: the windows with unsaved files hand them here, then they are saved, dropped, or PMT stays
    if (isMain) {
      await dock((one) => !!one.unsaved?.length);
      return void QuitAnswer(!ide.unsaved.size || (await unsavedModal.open({ going: "Closing PMT" })));
    }
    if (ide.openFiles.length) await (await import("./ide/monacoEditor")).handOver("main", ide.openFiles, ide.activePath);
    void Window.Close();
  });
  Events.On("window:tabs", ({ data }) => {
    if (data.to === windowName) receiveTabs((data.tabs ?? []).map((one) => one.tab), async () => (await import("./ide/monacoEditor")).receive(data));
  });
  // a tab this window holds, dragged into another
  Events.On("window:give", async ({ data }) => {
    if (data.to === windowName) await (await import("./ide/monacoEditor")).handOver(data.window, [data.tab]);
  });
  // an editor group's window whose last tab went (closed, or moved to another window) closes
  if (windowPart === "editor") watch(() => ide.openFiles.length, (count, was) => !count && was && void Window.Close());
  // this window asked to the front: at a link, or showing a file (in the IDE, in the main window)
  Events.On("window:show", async ({ data }) => {
    if (data.to !== windowName) return;
    if (data.route) await router.push(data.route);
    if (data.path) ide.open(data.path, data.line < 0 ? undefined : { line: data.line, col: data.col });
    await Show(windowName);
  });

  // the panel's window shows every window's problems
  if (windowPart === "panel") {
    watch(others, () => {
      ide.problems = new Map([...others.values()].flatMap((one) => Object.entries(one.problems ?? {}).map(([path, found]) => [path, found ?? []])));
    });
  }
  // the main window's sidebar shows the Search results another window finds (its references)
  if (isMain) {
    Events.On("window:results", ({ data }) => {
      ide.results = { ...data, rows: data.rows ?? [] };
      ide.sidebar = "search";
    });
  } else if (holdsFiles) {
    watch(() => ide.results, (results) => results && void Events.Emit("window:results", results));
  }

  // another window saved something config.json does not hold (config:changed tells of that): its repositories' state
  // and Publish's files are read again
  Events.On("window:changed", ({ data: name }) => {
    if (name === windowName) return;
    for (const key of [["git"], ["release"]]) void queryCache.invalidateQueries({ key });
  });

  // a window of one thing follows the main window to another workspace
  if (!isMain) {
    watch(
      () => store.activeWorkspaceId,
      (id) => {
        const now = route.value;
        if ("id" in now.params && now.params.id !== id) void router.replace({ path: now.fullPath.replace(`/workspace/${now.params.id}`, `/workspace/${id}`) });
      },
    );
  }
}
