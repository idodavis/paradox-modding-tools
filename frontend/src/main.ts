/**
 * The app's bootstrap: the stores, the query cache with its one error handler, Nuxt UI, the router, the events
 * table, the search store's subscription, and Monaco with its providers. Nothing is set up anywhere else.
 */
import { createApp } from "vue";
import { createPinia } from "pinia";
import { PiniaColada } from "@pinia/colada";
import ui from "@nuxt/ui/vue-plugin";

import App from "./app/App.vue";
import { router } from "./app/router";
import { subscribeEvents } from "./app/events";
import { useSearchStore } from "./stores/search";
import { setupMonaco } from "./pages/ide/editor/monaco";
import "./styles/nuxt-ui.css";

const app = createApp(App);
app.use(createPinia());
app.use(PiniaColada, {
  mutationOptions: {
    /** The one place a failed mutation is reported: a toast and a log line. */
    onError: (error: unknown) => {
      throw new Error("stub", { cause: error });
    },
  },
});
app.use(ui);
app.use(router);
subscribeEvents();
useSearchStore().subscribe();
setupMonaco();
app.mount("#app");
