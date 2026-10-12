/**
 * Before every test file: this window's name in the page's URL (the tests are the main window), Nuxt UI's auto-imported
 * composables, the Wails runtime and the bound services as mocks, and `inApp`, which makes a store as main.ts does.
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
vi.mock("@services/ideservice", () => ({ DidClose: vi.fn(() => Promise.resolve()) }));
vi.mock("@services/fileservice", () => ({ RenamePath: vi.fn(() => Promise.resolve()) }));
vi.mock("@services/windowservice", () => ({
  Open: vi.fn(() => Promise.resolve()),
  Show: vi.fn(() => Promise.resolve()),
  Close: vi.fn(() => Promise.resolve()),
  QuitAnswer: vi.fn(() => Promise.resolve()),
}));
// the models need Monaco and the page; the stores reach them for a save and for a tab's unsaved text
vi.mock("./src/editor/models", () => ({
  saveFile: vi.fn(),
  resendOpenFiles: vi.fn(),
  unsavedText: vi.fn(() => undefined),
  showText: vi.fn(() => Promise.resolve()),
}));
// a component the tests have no plugin to compile
vi.mock("./src/ui/UnsavedChangesModal.vue", () => ({ default: {} }));

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
