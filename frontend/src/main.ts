/** The app's bootstrap: it makes the app with its one error handler, sets up its plugins and mounts it. */
import { createApp } from "vue";
import { createPinia } from "pinia";
import { PiniaColada } from "@pinia/colada";
import ui from "@nuxt/ui/vue-plugin";

import App from "./app/App.vue";
import { router } from "./app/router";
import { subscribeEvents } from "./app/events";
import { isCancelled, reportError, showError, takeToasts, writeLog } from "./lib/feedback";
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
    onError: (error: unknown) => reportError(error, "mutation"),
  },
});
app.use(ui);
app.use(router);
// A page that cannot load would leave the click that asked for it showing
// nothing.
router.onError(showError);
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
