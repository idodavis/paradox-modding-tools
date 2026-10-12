import { beforeEach, describe, expect, it, vi } from "vitest";
import { Events } from "@wailsio/runtime";
import { Close, Open, Show } from "@services/windowservice";
import { inApp } from "../../vitest.setup";
import { showText, unsavedText } from "../editor/models";
import { useEditorStore } from "./editor";
import { isMain, useWindowsStore, windowName, windowPart } from "./windows";

const state = (name: string, tabs: string[] = []) => ({
  name,
  title: name,
  roots: [],
  tabs,
  unsaved: [],
  problems: {},
});

/** The windows store with the editor store it reads; the store says window:opened as it starts, which no test counts. */
function stores() {
  const made = inApp(() => ({ editor: useEditorStore(), windows: useWindowsStore() }));
  vi.mocked(Events.Emit).mockClear();
  return made;
}

/** An event as Wails brings it to the handler the store registered for it. */
function fire(name: string, data: unknown): void {
  const handler = vi.mocked(Events.On).mock.calls.find((call) => call[0] === name)?.[1] as (event: {
    data: unknown;
  }) => void;
  handler({ data });
}

describe("the windows", () => {
  beforeEach(() => {
    vi.mocked(Events.On).mockClear();
    vi.mocked(Open).mockClear();
    vi.mocked(Show).mockClear();
    vi.mocked(Close).mockClear();
    vi.mocked(showText).mockClear();
    vi.mocked(unsavedText).mockReset();
  });
  it("is named by its URL", () => {
    expect(windowName).toBe("main");
    expect(windowPart).toBe("main");
    expect(isMain).toBe(true);
  });
  it("opens a window once and shows it after", async () => {
    const { windows } = stores();
    const opened = windows.openWindow("/workspace/w", "editor:1", "a.txt");
    expect(Open).toHaveBeenCalledWith("/workspace/w", "editor:1", "a.txt");
    fire("window:state", state("editor:1"));
    await opened;
    expect(windows.otherWindows.get("editor:1")?.title).toBe("editor:1");
    await windows.openWindow("/workspace/w", "editor:1", "a.txt");
    expect(Show).toHaveBeenCalledWith("editor:1");
    expect(Open).toHaveBeenCalledTimes(1);
  });
  it("docks the windows that match, once they are gone and their tabs here", async () => {
    const { editor, windows } = stores();
    fire("window:state", state("editor:1", ["a"]));
    fire("window:state", state("page:health"));
    let received = false;
    const docked = windows.dock((one) => one.name.startsWith("editor:")).then(() => (received = true));
    expect(Close).toHaveBeenCalledWith("editor:1");
    expect(Close).toHaveBeenCalledTimes(1);
    await Promise.resolve();
    expect(received).toBe(false);
    windows.otherWindows.delete("editor:1");
    fire("window:tabs-handed", { to: "main", tabs: [{ tab: "a", unsaved: false, text: "" }], active: "a" });
    await docked;
    expect(received).toBe(true);
    expect(editor.openTabs).toEqual(["a"]);
    expect(windows.otherWindows.has("page:health")).toBe(true);
  });
  it("tells the others of a save only when there are others", () => {
    const { windows } = stores();
    windows.saved();
    expect(Events.Emit).not.toHaveBeenCalled();
    fire("window:state", state("panel"));
    windows.saved();
    expect(Events.Emit).toHaveBeenCalledWith("window:saved", "main");
  });
  it("answers tabs handed to it and results shown to it from the start, whatever page it shows", () => {
    stores();
    const names = vi.mocked(Events.On).mock.calls.map((call) => call[0]);
    expect(names).toContain("window:tabs-handed");
    expect(names).toContain("window:tab-requested");
    expect(names).toContain("window:results");
  });
  it("hands tabs over with their unsaved text and drops them here", async () => {
    const { editor, windows } = stores();
    editor.open("a");
    editor.open("b");
    editor.setUnsaved("b", true);
    vi.mocked(unsavedText).mockImplementation((tab) => (tab === "b" ? "B edited" : undefined));
    await windows.handOver("editor:1", ["a", "b"], "b");
    expect(editor.openTabs).toEqual([]);
    expect([...editor.unsaved]).toEqual([]);
    expect(vi.mocked(Events.Emit).mock.calls).toEqual([
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
    fire("window:tabs-handed", {
      to: "main",
      tabs: [
        { tab: "a", unsaved: false, text: "" },
        { tab: "b", unsaved: true, text: "B" },
      ],
      active: "b",
    });
    await vi.waitFor(() => expect(windows.placing.size).toBe(0));
    expect(showText).toHaveBeenCalledWith("b", "B");
    expect(editor.groups).toEqual([{ id: 1, tabs: ["a", "x", "b"], active: "a" }]);
  });
  it("requests a tab: handed from here, asked of its holder, or opened fresh", async () => {
    const { editor, windows } = stores();
    editor.open("a");
    fire("window:state", state("editor:9", ["h"]));
    await windows.requestTab("editor:2", "a");
    await windows.requestTab("editor:2", "h");
    await windows.requestTab("editor:2", "n");
    const calls = vi.mocked(Events.Emit).mock.calls as unknown[][];
    expect(calls.map((call) => call[0])).toEqual(["window:tabs-handed", "window:tab-requested", "window:tabs-handed"]);
    expect(calls[1]?.[1]).toEqual({ to: "editor:9", tab: "h", window: "editor:2" });
    expect(calls[2]?.[1]).toEqual({ to: "editor:2", tabs: [{ tab: "n", unsaved: false, text: "" }], active: "n" });
  });
  it("answers a request for a tab it holds by handing it to the asker", () => {
    const { editor } = stores();
    editor.open("a");
    fire("window:tab-requested", { to: "main", tab: "a", window: "editor:2" });
    expect(vi.mocked(Events.Emit).mock.calls).toEqual([
      ["window:tabs-handed", { to: "editor:2", tabs: [{ tab: "a", unsaved: false, text: "" }], active: "a" }],
    ]);
    expect(editor.openTabs).toEqual([]);
  });
});
