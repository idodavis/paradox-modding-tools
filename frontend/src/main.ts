/**
 * The app's start: Pinia, Pinia Colada with the one toast for a failed save, the router, Nuxt UI, Go's
 * events and the settings.
 */
import { createApp } from "vue";
import ui from "@nuxt/ui/vue-plugin";
import "./styles/nuxt-ui.css";
import App from "./app/App.vue";
import { createPinia } from "pinia";
import { PiniaColada } from "@pinia/colada";
import router from "./app/router";
import { subscribeEvents } from "./app/events";
import { useSettingsStore } from "./stores/settings";
import { Log } from "@services/logservice";
import { changed } from "./stores/windows";

// what nothing caught goes to pmt.log, where a bug report finds it
window.addEventListener("error", (event) => {
  void Log("error", event.message, {
    at: `${event.filename}:${event.lineno}:${event.colno}`,
    stack: String(event.error?.stack ?? ""),
  });
});
window.addEventListener("unhandledrejection", (event) => {
  const reason = event.reason instanceof Error ? event.reason : new Error(String(event.reason));
  void Log("error", reason.message, { kind: "unhandled rejection", stack: String(reason.stack ?? "") });
});

const app = createApp(App);
// Vue catches a component's error, so the window's error event never sees it; the console still
// gets it, as without a handler
app.config.errorHandler = (err, _instance, info) => {
  const error = err instanceof Error ? err : new Error(String(err));
  console.error(error);
  void Log("error", error.message, { kind: "vue", info, stack: String(error.stack ?? "") });
};
app.use(createPinia());
const toast = app.runWithContext(useToast);
app.use(PiniaColada, {
  queryOptions: {
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  },
  // every failed save is told here, once (Go's error says what went wrong), and every save to the other windows,
  // which read again what they show
  mutationOptions: {
    onError: (error) => toast.add({ title: error instanceof Error ? error.message : String(error), color: "error" }),
    onSuccess: changed,
  },
});
app.use(router);
app.use(ui);
app.runWithContext(() => {
  subscribeEvents(router);
  // the settings put the theme and text sizes on the page as they load
  void useSettingsStore().load();
});
app.mount("#app");
