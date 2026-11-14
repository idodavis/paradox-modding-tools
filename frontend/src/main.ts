/** The app's bootstrap: it makes the app with its one error handler, sets up its plugins and mounts it. */
import { createApp } from "vue";
import { createPinia } from "pinia";
import { PiniaColada } from "@pinia/colada";
import ui from "@nuxt/ui/vue-plugin";

import { Error as logError } from "../bindings/github.com/wailsapp/wails/v3/pkg/services/log/logservice";
import App from "./app/App.vue";
import { router } from "./app/router";
import { subscribeEvents } from "./app/events";
import { takeToasts, toast } from "./lib/feedback";
import { splitError } from "./lib/format";
import { useAgentStore } from "./stores/agent";
import { useCompletionStore } from "./stores/completion";
import { useEditorStore } from "./stores/editor";
import { useGitStore } from "./stores/git";
import { useSearchStore } from "./stores/search";
import { useSettingsStore } from "./stores/settings";
import { useTerminalStore } from "./stores/terminal";
import { useToolViewStore } from "./stores/toolView";
import { useWindowsStore } from "./stores/windows";
import { useWorkspaceStore } from "./stores/workspace";
import { setupMonaco } from "./pages/ide/editor/monaco";
import "./styles/nuxt-ui.css";

const TOAST_LINES = 3;

/** writeLog writes an error to pmt.log, tagged with the given source. */
function writeLog(error: unknown, source: string): void {
  const failure = error instanceof Error ? error : new Error(String(error));
  void logError(failure.message, "source", source, "stack", failure.stack ?? "");
}

/**
 * isCancelled reports whether an error is a cancel, which is neither shown nor logged: the user cancelling a system
 * dialog, or Monaco's "Canceled", which its tab-completion indicator leaves unhandled when a suggestion changes.
 */
function isCancelled(error: unknown): boolean {
  const failure = error as { name?: unknown; message?: unknown } | null | undefined;

  return failure?.name === "Canceled" || String(failure?.message).includes("cancelled by user");
}

window.addEventListener("error", (event) => {
  // The browser's notice that a resize observer changed the layout again within one frame is harmless.
  if (event.message.startsWith("ResizeObserver loop")) return;
  writeLog(event.error ?? event.message, "window");
});
window.addEventListener("unhandledrejection", (event) => {
  if (isCancelled(event.reason)) event.preventDefault();
  else writeLog(event.reason, "promise");
});

const app = createApp(App);
app.config.errorHandler = (error, _instance, info) => {
  if (isCancelled(error)) return;
  console.error(error);
  writeLog(error, info);
};
app.runWithContext(takeToasts);
app.use(createPinia());
app.use(PiniaColada, {
  queryOptions: { staleTime: 30_000, refetchOnWindowFocus: false },
  mutationOptions: {
    /** The one place a failed mutation is reported, as a toast and a log line. */
    onError: (error: unknown) => {
      if (isCancelled(error)) return;
      const [summary, details] = splitError(error, TOAST_LINES);
      toast?.add({ title: summary, description: details, color: "error", ui: { description: "whitespace-pre-wrap" } });
      writeLog(error, "mutation");
    },
  },
});
app.use(ui);
app.use(router);
app.runWithContext(() => {
  subscribeEvents();
  useSearchStore().subscribe();
  // The stores that hold queries are made outside any component, which would take their queries with it when it
  // closes.
  useWorkspaceStore();
  useSettingsStore();
  useWindowsStore();
  useEditorStore();
  useGitStore();
  useAgentStore();
  useCompletionStore();
  useTerminalStore();
  useToolViewStore();
  setupMonaco();
});
app.mount("#app");
