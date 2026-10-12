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
vi.mock("@services/ideservice", () => ({ DidClose: vi.fn(() => Promise.resolve()) }));
vi.mock("@services/windowservice", () => ({
  Open: vi.fn(() => Promise.resolve()),
  Show: vi.fn(() => Promise.resolve()),
  Close: vi.fn(() => Promise.resolve()),
}));
// the windows store imports the unsaved-changes dialog, a component the tests have no plugin to compile
vi.mock("../ui/UnsavedChangesModal.vue", () => ({ default: {} }));
vi.mock("@services/fileservice", () => ({ RenamePath: vi.fn(() => Promise.resolve()) }));
// the models: only tab b has unsaved text
vi.mock("./models", () => ({
  unsavedText: vi.fn((tab: string) => (tab === "b" ? "B edited" : undefined)),
  showText: vi.fn(),
}));

import { Events } from "@wailsio/runtime";
import { showText } from "./models";
import { handOver, installHandover, receive, requestTab } from "./handover";
import { useEditorStore } from "../stores/editor";
import { useWindowsStore } from "../stores/windows";

function stores() {
  const app = createApp({});
  const page = { render: () => null };
  app.use(
    createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/:all(.*)*", name: "workspace-ide", component: page }],
    }),
  );
  const pinia = createPinia();
  app.use(pinia);
  app.use(PiniaColada);
  setActivePinia(pinia);
  const made = app.runWithContext(() => ({ editor: useEditorStore(), windows: useWindowsStore() }));
  // the windows store says window:opened when it starts; the tests count what the module sends
  vi.mocked(Events.Emit).mockClear();
  return made;
}

const emitted = () => vi.mocked(Events.Emit).mock.calls as unknown[][];

describe("tabs between windows", () => {
  beforeEach(() => {
    vi.mocked(Events.Emit).mockClear();
    vi.mocked(Events.On).mockClear();
    vi.mocked(showText).mockClear();
  });
  it("hands tabs over with their unsaved text and drops them here", async () => {
    const { editor } = stores();
    editor.open("a");
    editor.open("b");
    editor.setUnsaved("b", true);
    await handOver("editor:1", ["a", "b"], "b");
    expect(editor.openTabs).toEqual([]);
    expect([...editor.unsaved]).toEqual([]);
    expect(emitted()).toEqual([
      [
        "window:tabs-handed",
        {
          to: "editor:1",
          tabs: [
            { tab: "a", unsaved: false, text: "" },
            { tab: "b", unsaved: true, text: "B edited" },
          ],
          active: "b",
        },
      ],
    ]);
  });
  it("receives tabs: hidden, the shown one shown, unsaved text shown, a dragged one placed", async () => {
    const { editor, windows } = stores();
    editor.open("x");
    windows.placing.set("a", { group: 1, edge: "middle", nextTab: "x" });
    await receive({
      to: "main",
      tabs: [
        { tab: "a", unsaved: false, text: "" },
        { tab: "b", unsaved: true, text: "B" },
      ],
      active: "b",
    });
    expect(showText).toHaveBeenCalledWith("b", "B");
    expect(editor.groups).toEqual([{ id: 1, tabs: ["a", "x", "b"], active: "a" }]);
    expect(windows.placing.size).toBe(0);
  });
  it("requests a tab: handed from here, asked of its holder, or opened fresh", async () => {
    const { editor, windows } = stores();
    editor.open("a");
    windows.recordWindow({ name: "editor:9", title: "", roots: [], tabs: ["h"], unsaved: [], problems: {} });
    await requestTab("editor:2", "a");
    await requestTab("editor:2", "h");
    await requestTab("editor:2", "n");
    expect(emitted().map((call) => call[0])).toEqual([
      "window:tabs-handed",
      "window:tab-requested",
      "window:tabs-handed",
    ]);
    expect(emitted()[1]?.[1]).toEqual({ to: "editor:9", tab: "h", window: "editor:2" });
    expect(emitted()[2]?.[1]).toEqual({ to: "editor:2", tabs: [{ tab: "n", unsaved: false, text: "" }], active: "n" });
  });
  it("answers a request for a tab it holds by handing it to the asker", async () => {
    const { editor } = stores();
    editor.open("a");
    installHandover();
    const requested = vi
      .mocked(Events.On)
      .mock.calls.find((call) => call[0] === "window:tab-requested")?.[1] as (event: {
      data: { to: string; tab: string; window: string };
    }) => Promise<void>;
    await requested({ data: { to: "main", tab: "a", window: "editor:2" } });
    expect(emitted()).toEqual([
      ["window:tabs-handed", { to: "editor:2", tabs: [{ tab: "a", unsaved: false, text: "" }], active: "a" }],
    ]);
    expect(editor.openTabs).toEqual([]);
  });
});
