import { beforeEach, describe, expect, it, vi } from "vitest";
import { Events } from "@wailsio/runtime";
import { DidClose } from "@services/ideservice";
import { inApp } from "../../vitest.setup";
import { useEditorStore } from "./editor";
import { useWindowsStore } from "./windows";

describe("groups and layout", () => {
  beforeEach(() => {
    vi.mocked(Events.Emit).mockClear();
    vi.mocked(DidClose).mockClear();
  });
  it("opens after the shown tab, once", () => {
    const editor = inApp(useEditorStore);
    editor.open("a");
    editor.open("b");
    editor.open("a");
    editor.open("c", { line: 3 });
    expect(editor.groups).toEqual([{ id: 1, tabs: ["a", "c", "b"], active: "c" }]);
    expect(editor.cursor).toEqual({ path: "c", line: 3, col: 0 });
    expect(editor.activeTab).toBe("c");
    expect(editor.openTabs).toEqual(["a", "c", "b"]);
  });
  it("splits beside, sharing a sibling's room in the same direction, and nests across it", () => {
    const editor = inApp(useEditorStore);
    editor.open("a");
    editor.split("right");
    expect(editor.layout).toEqual({ split: "row", parts: [{ group: 1 }, { group: 2 }], sizes: [0.5, 0.5] });
    expect(editor.focused).toBe(2);
    editor.split("right");
    expect(editor.layout).toEqual({
      split: "row",
      parts: [{ group: 1 }, { group: 2 }, { group: 3 }],
      sizes: [0.5, 0.25, 0.25],
    });
    editor.split("down");
    expect(editor.layout).toEqual({
      split: "row",
      parts: [{ group: 1 }, { group: 2 }, { split: "column", parts: [{ group: 3 }, { group: 4 }], sizes: [0.5, 0.5] }],
      sizes: [0.5, 0.25, 0.25],
    });
    expect(editor.cornerGroup).toBe(3);
    expect(editor.groups.map((group) => group.tabs)).toEqual([["a"], ["a"], ["a"], ["a"]]);
  });
  it("closes a group's last tab and folds the split around it; the focus moves only when its group goes", async () => {
    const editor = inApp(useEditorStore);
    editor.open("a");
    editor.split("right");
    editor.split("down");
    editor.closeTab(2, "a");
    expect(editor.layout).toEqual({ split: "row", parts: [{ group: 1 }, { group: 3 }], sizes: [0.5, 0.5] });
    expect(editor.focused).toBe(3);
    editor.closeTab(3, "a");
    expect(editor.layout).toEqual({ group: 1 });
    expect(editor.focused).toBe(1);
    editor.closeTab(1, "a");
    expect(editor.groups).toEqual([{ id: 1, tabs: [], active: "" }]);
    // the backend is notified through a mutation, a tick later
    await vi.waitFor(() => expect(DidClose).toHaveBeenCalled());
    expect(DidClose).toHaveBeenCalledTimes(1);
  });
  it("drops a tab before another, into a group, or onto an edge", () => {
    const editor = inApp(useEditorStore);
    editor.open("a");
    editor.open("b");
    editor.split("right", "a");
    editor.drop("b", 1, 2, "middle", "a");
    expect(editor.groups).toEqual([
      { id: 1, tabs: ["a"], active: "a" },
      { id: 2, tabs: ["b", "a"], active: "b" },
    ]);
    editor.drop("a", 2, 2, "down");
    expect(editor.groups.map((group) => [group.id, group.tabs, group.active])).toEqual([
      [1, ["a"], "a"],
      [2, ["b"], "b"],
      [3, ["a"], "a"],
    ]);
    expect(editor.layout).toEqual({
      split: "row",
      parts: [{ group: 1 }, { split: "column", parts: [{ group: 2 }, { group: 3 }], sizes: [0.5, 0.5] }],
      sizes: [0.5, 0.5],
    });
    // a group's only tab splits nothing off it
    editor.drop("a", 1, 1, "right");
    expect(editor.groups.length).toBe(3);
  });
  it("moves the shown tab to the next group, making one when there is none", () => {
    const editor = inApp(useEditorStore);
    editor.open("a");
    editor.open("b");
    editor.moveTabToNextGroup();
    expect(editor.groups).toEqual([
      { id: 1, tabs: ["a"], active: "a" },
      { id: 2, tabs: ["b"], active: "b" },
    ]);
    editor.focused = 1;
    editor.moveTabToNextGroup();
    expect(editor.groups).toEqual([{ id: 2, tabs: ["b", "a"], active: "a" }]);
    expect(editor.layout).toEqual({ group: 2 });
  });
  it("remembers and restores a workspace's tabs: others' into the first group, each once, the shown file found", () => {
    const editor = inApp(useEditorStore);
    editor.open("a");
    editor.open("b");
    editor.split("right", "b");
    editor.remember("w", ["c", "A", "c"]);
    editor.restore("w", true);
    expect(editor.groups).toEqual([{ id: 1, tabs: [], active: "" }]);
    editor.restore("w", false);
    expect(editor.groups).toEqual([
      { id: 1, tabs: ["a", "b", "c"], active: "b" },
      { id: 2, tabs: ["b"], active: "b" },
    ]);
    expect(editor.focused).toBe(1);
    expect(editor.editingWorkspaceId).toBe("w");
  });
  it("follows a file moved on disk and closes one deleted", () => {
    const editor = inApp(useEditorStore);
    editor.open("C:/m/x/a.txt");
    editor.open("C:/m/b.txt");
    editor.setUnsaved("C:/m/b.txt", true);
    editor.renameTabs("C:/m/x", "C:/m/y");
    expect(editor.openTabs).toEqual(["C:/m/y/a.txt", "C:/m/b.txt"]);
    editor.closeTabs("C:/m/b.txt");
    expect(editor.openTabs).toEqual(["C:/m/y/a.txt"]);
    expect([...editor.unsaved]).toEqual([]);
  });
  it("lets tabs go to another window and adopts one edited in a peek", () => {
    const editor = inApp(useEditorStore);
    editor.open("a");
    editor.open("b");
    editor.setUnsaved("a", true);
    editor.dropTabs(["a"]);
    expect(editor.openTabs).toEqual(["b"]);
    expect([...editor.unsaved]).toEqual([]);
    expect(DidClose).not.toHaveBeenCalled();
    editor.addTabHidden("c");
    expect(editor.groups).toEqual([{ id: 1, tabs: ["b", "c"], active: "b" }]);
  });
  it("shows a file in the window holding it, else in the one whose explorer shows its folder", () => {
    const editor = inApp(useEditorStore);
    const windows = useWindowsStore();
    windows.otherWindows.set("editor:x", {
      name: "editor:x",
      title: "",
      roots: [],
      tabs: ["C:/m/a.txt"],
      unsaved: [],
      problems: {},
    });
    windows.otherWindows.set("files:C:/m/sub", {
      name: "files:C:/m/sub",
      title: "",
      roots: ["C:/m/sub"],
      tabs: [],
      unsaved: [],
      problems: {},
    });
    // the windows store announced that it opened
    vi.mocked(Events.Emit).mockClear();
    editor.open("c:/M/a.txt");
    editor.open("C:/m/sub/b.txt", { line: 4, col: 2 });
    editor.open("C:/m/c.txt");
    expect(vi.mocked(Events.Emit).mock.calls).toEqual([
      ["window:show", { to: "editor:x", route: "", path: "c:/M/a.txt", line: -1, col: 0 }],
      ["window:show", { to: "files:C:/m/sub", route: "", path: "C:/m/sub/b.txt", line: 4, col: 2 }],
    ]);
    expect(editor.openTabs).toEqual(["C:/m/c.txt"]);
  });
  it("shows a file in the window holding only a diff tab of it", () => {
    const editor = inApp(useEditorStore);
    const windows = useWindowsStore();
    windows.otherWindows.set("editor:x", {
      name: "editor:x",
      title: "",
      roots: [],
      tabs: ["git:changes:C:/m/a.txt"],
      unsaved: [],
      problems: {},
    });
    vi.mocked(Events.Emit).mockClear();
    editor.open("C:/m/a.txt");
    expect(vi.mocked(Events.Emit).mock.calls).toEqual([
      ["window:show", { to: "editor:x", route: "", path: "C:/m/a.txt", line: -1, col: 0 }],
    ]);
    expect(editor.openTabs).toEqual([]);
  });
  it("keeps the open tab's spelling for the file and its cursor", () => {
    const editor = inApp(useEditorStore);
    editor.open("C:/m/a.txt");
    editor.open("c:/M/a.txt", { line: 7, col: 1 });
    expect(editor.openTabs).toEqual(["C:/m/a.txt"]);
    expect(editor.cursor).toEqual({ path: "C:/m/a.txt", line: 7, col: 1 });
  });
  it("opens a file's panel with Visual and never with Split or Code", () => {
    const editor = inApp(useEditorStore);
    editor.open("gui/a.gui");
    editor.setView("gui/a.gui", "visual");
    expect(editor.panels.get("gui/a.gui")).toBe(true);
    editor.setView("gui/a.gui", "split");
    expect(editor.panels.get("gui/a.gui")).toBe(true);
    editor.togglePanel("gui/a.gui");
    expect(editor.panels.get("gui/a.gui")).toBe(false);
    editor.setView("gui/a.gui", "code");
    expect(editor.panels.get("gui/a.gui")).toBe(false);
    editor.setView("readme.md", "visual");
    expect(editor.panels.get("readme.md")).toBeUndefined();
  });
  it("starts with the problems panel open", () => {
    expect(inApp(useEditorStore).panel).toBe("problems");
  });
});
