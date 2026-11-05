/**
 * Before every test file: this window's name in the page's URL (the tests are the main window), Nuxt UI's
 * auto-imported composables as stubs, the Wails runtime, the IDE's editor modules and the settings store as mocks,
 * and `inApp`, which makes a store as main.ts does.
 */
import { vi } from "vitest";
import { createApp } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { createPinia, setActivePinia } from "pinia";
import { PiniaColada } from "@pinia/colada";

vi.stubGlobal("location", new URL("http://localhost/?window=main"));
vi.stubGlobal("useToast", () => ({ add: vi.fn() }));
vi.stubGlobal("useOverlay", () => ({ create: () => ({ open: vi.fn() }) }));

// the runtime's events and calls go nowhere; the bindings call Create.* at load, so the rest of the module is real
vi.mock("@wailsio/runtime", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@wailsio/runtime")>();
  return {
    ...actual,
    Events: { Emit: vi.fn(() => Promise.resolve()), On: vi.fn(() => () => undefined) },
    Call: { ...actual.Call, ByID: vi.fn(() => Promise.resolve(null)), ByName: vi.fn(() => Promise.resolve(null)) },
  };
});
// the models need Monaco and the page; the stores open a file's model and close it, and read which are unsaved
vi.mock("./src/pages/ide/editor/models", () => ({
  openModel: vi.fn(() => Promise.resolve({ caret: { line: 1, column: 1 }, model: { setValue: vi.fn() } })),
  findModel: vi.fn(() => null),
  closeModel: vi.fn(),
  unsavedModels: vi.fn(() => []),
  applyEdits: vi.fn(),
  renameModel: vi.fn(),
  saveUnsaved: vi.fn(() => Promise.resolve(true)),
  syncText: vi.fn(() => Promise.resolve([])),
  save: vi.fn(() => Promise.resolve()),
}));
// a component the tests have no plugin to compile
vi.mock("./src/ui/ConfirmModal.vue", () => ({ default: {} }));
// the real store puts the theme on the page and loads the games' icons; the tests have neither
vi.mock("./src/stores/settings", () => ({
  useSettingsStore: () => ({
    value: () => "",
    write: vi.fn(),
    theme: () => ({ family: "pmt", dark: true }),
  }),
}));

/** A store made in an app with a router, Pinia and Pinia Colada, as main.ts makes them. */
export function inApp<T>(make: () => T): T {
  const app = createApp({});
  app.use(
    createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/:all(.*)*", name: "workspace-ide", component: { render: () => null } }],
    }),
  );
  const pinia = createPinia();
  app.use(pinia);
  app.use(PiniaColada);
  setActivePinia(pinia);
  return app.runWithContext(make);
}
