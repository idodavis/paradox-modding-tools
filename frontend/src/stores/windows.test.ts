import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApp } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { createPinia, setActivePinia } from "pinia";
import { PiniaColada } from "@pinia/colada";

vi.mock("@wailsio/runtime", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@wailsio/runtime")>();
  return {
    ...actual,
    Events: { Emit: vi.fn(() => Promise.resolve()), On: vi.fn(() => () => undefined) },
    Call: { ...actual.Call, ByID: vi.fn(() => Promise.resolve(null)), ByName: vi.fn(() => Promise.resolve(null)) },
  };
});
vi.mock("@services/windowservice", () => ({
  Open: vi.fn(() => Promise.resolve()),
  Show: vi.fn(() => Promise.resolve()),
  Close: vi.fn(() => Promise.resolve()),
  QuitAnswer: vi.fn(() => Promise.resolve()),
}));
vi.mock("@services/ideservice", () => ({ DidClose: vi.fn(() => Promise.resolve()) }));
vi.mock("@services/fileservice", () => ({ RenamePath: vi.fn(() => Promise.resolve()) }));
vi.mock("../editor/models", () => ({ saveFile: vi.fn(), resendOpenFiles: vi.fn() }));
// the store imports the unsaved-changes dialog, a component the tests have no plugin to compile
vi.mock("../ui/UnsavedChangesModal.vue", () => ({ default: {} }));

import { Events } from "@wailsio/runtime";
import { Close, Open, Show } from "@services/windowservice";
import { isMain, useWindowsStore, windowName, windowPart } from "./windows";

const state = (name: string, tabs: string[] = []) => ({
  name,
  title: name,
  roots: [],
  tabs,
  unsaved: [],
  problems: {},
});

/** The store in an app with a router, as main.ts makes it. */
function store() {
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
  return app.runWithContext(() => useWindowsStore());
}

describe("the windows", () => {
  beforeEach(() => {
    vi.mocked(Open).mockClear();
    vi.mocked(Show).mockClear();
    vi.mocked(Close).mockClear();
  });
  it("is named by its URL", () => {
    expect(windowName).toBe("main");
    expect(windowPart).toBe("main");
    expect(isMain).toBe(true);
  });
  it("opens a window once and shows it after", async () => {
    const windows = store();
    const opened = windows.openWindow("/workspace/w", "editor:1", "a.txt");
    expect(Open).toHaveBeenCalledWith("/workspace/w", "editor:1", "a.txt");
    windows.recordWindow(state("editor:1"));
    await opened;
    expect(windows.otherWindows.get("editor:1")?.title).toBe("editor:1");
    await windows.openWindow("/workspace/w", "editor:1", "a.txt");
    expect(Show).toHaveBeenCalledWith("editor:1");
    expect(Open).toHaveBeenCalledTimes(1);
  });
  it("docks the windows that match, once they are gone and their tabs here", async () => {
    const windows = store();
    windows.recordWindow(state("editor:1", ["a"]));
    windows.recordWindow(state("page:health"));
    let received = false;
    const docked = windows.dock((one) => one.name.startsWith("editor:")).then(() => (received = true));
    expect(Close).toHaveBeenCalledWith("editor:1");
    expect(Close).toHaveBeenCalledTimes(1);
    await Promise.resolve();
    expect(received).toBe(false);
    windows.otherWindows.delete("editor:1");
    windows.receiveTabs(["a"], () => Promise.resolve());
    await docked;
    expect(received).toBe(true);
    expect(windows.otherWindows.has("page:health")).toBe(true);
  });
  it("tells the others of a save only when there are others", () => {
    const windows = store();
    vi.mocked(Events.Emit).mockClear();
    windows.saved();
    expect(Events.Emit).not.toHaveBeenCalled();
    windows.recordWindow(state("panel"));
    windows.saved();
    expect(Events.Emit).toHaveBeenCalledWith("window:saved", "main");
  });
});
