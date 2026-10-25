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

/** Writes an error to pmt.log with the place it came from. */
function writeLog(error: unknown, source: string): void {
  const failure = error instanceof Error ? error : new Error(String(error));
  void logError(failure.message, "source", source, "stack", failure.stack ?? "");
}

window.addEventListener("error", (event) => writeLog(event.error ?? event.message, "window"));
window.addEventListener("unhandledrejection", (event) => writeLog(event.reason, "promise"));

const app = createApp(App);
app.config.errorHandler = (error, _instance, info) => {
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
