/**
 * Vue app bootstrap with Pinia, router, and Nuxt UI.
 */
import { createApp } from "vue";
import ui from "@nuxt/ui/vue-plugin";
import "./styles/nuxt-ui.css";
import App from "./App.vue";
import { createPinia } from "pinia";
import { PiniaColada } from "@pinia/colada";
import router from "./router";
import { applySeedCss } from "./ide/colorThemes";
import { Log } from "@services/logservice";

// what nothing caught goes to pmt.log, where a bug report finds it
window.addEventListener("error", (event) => {
  void Log("error", event.message, { at: `${event.filename}:${event.lineno}:${event.colno}`, stack: String(event.error?.stack ?? "") });
});
window.addEventListener("unhandledrejection", (event) => {
  const reason = event.reason instanceof Error ? event.reason : new Error(String(event.reason));
  void Log("error", reason.message, { kind: "unhandled rejection", stack: String(reason.stack ?? "") });
});

applySeedCss("pmt-dark");

const app = createApp(App);
// Vue catches a component's error, so the window's error event never sees it; the console still
// gets it, as without a handler
app.config.errorHandler = (err, _instance, info) => {
  const error = err instanceof Error ? err : new Error(String(err));
  console.error(error);
  void Log("error", error.message, { kind: "vue", info, stack: String(error.stack ?? "") });
};
app.use(createPinia());
app.use(PiniaColada, {
  queryOptions: {
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  },
});
app.use(router);
app.use(ui);
app.mount("#app");
