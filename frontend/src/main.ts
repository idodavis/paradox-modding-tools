/**
 * The app's bootstrap: the stores, the query cache with its one error handler, Nuxt UI, the router, the events
 * table, the search store's subscription, and Monaco with its providers. Nothing is set up anywhere else.
 */
import { createApp } from "vue";
import { createPinia } from "pinia";
import { PiniaColada } from "@pinia/colada";
import ui from "@nuxt/ui/vue-plugin";

import { Error as logError } from "../bindings/github.com/wailsapp/wails/v3/pkg/services/log/logservice";
import App from "./app/App.vue";
import { router } from "./app/router";
import { subscribeEvents } from "./app/events";
import { useSearchStore } from "./stores/search";
import { setupMonaco } from "./pages/ide/editor/monaco";
import "./styles/nuxt-ui.css";

/** writeLog writes an error to pmt.log with the place it came from. */
function writeLog(error: unknown, source: string): void {
  const failure = error instanceof Error ? error : new Error(String(error));
  void logError(failure.message, "source", source, "stack", failure.stack ?? "");
}

/**
 * isCancelled reports whether an error is a cancelled system dialog. Its promise rejects, which only ends the action
 * that asked, so it is no error to show or log.
 */
function isCancelled(error: unknown): boolean {
  return String((error as { message?: unknown } | null | undefined)?.message).includes("cancelled by user");
}

window.addEventListener("error", (event) => writeLog(event.error ?? event.message, "window"));
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
const toast = app.runWithContext(useToast);
app.use(createPinia());
app.use(PiniaColada, {
  queryOptions: { staleTime: 30_000, refetchOnWindowFocus: false },
  mutationOptions: {
    /** The one place a failed mutation is reported: a toast and a log line. */
    onError: (error: unknown) => {
      if (isCancelled(error)) return;
      toast.add({ title: error instanceof Error ? error.message : String(error), color: "error" });
      writeLog(error, "mutation");
    },
  },
});
app.use(ui);
app.use(router);
app.runWithContext(() => {
  subscribeEvents();
  useSearchStore().subscribe();
  setupMonaco();
});
app.mount("#app");
