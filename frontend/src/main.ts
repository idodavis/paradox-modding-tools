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
import { useSearchStore } from "./stores/search";
import { useTerminalStore } from "./stores/terminal";
import { useToolViewStore } from "./stores/toolView";
import { setupMonaco } from "./pages/ide/editor/monaco";
import "./styles/nuxt-ui.css";

const TOAST_LINES = 3;

/** writeLog writes an error to pmt.log, tagged with the given source. */
function writeLog(error: unknown, source: string): void {
  const failure = error instanceof Error ? error : new Error(String(error));
  void logError(failure.message, "source", source, "stack", failure.stack ?? "");
}

/** isCancelled reports whether an error is the user cancelling a system dialog, which is neither shown nor logged. */
function isCancelled(error: unknown): boolean {
  return String((error as { message?: unknown } | null | undefined)?.message).includes("cancelled by user");
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
  // The window that shows the panel holds the shells from its start, whichever page it shows.
  useTerminalStore();
  useToolViewStore();
  setupMonaco();
});
app.mount("#app");
