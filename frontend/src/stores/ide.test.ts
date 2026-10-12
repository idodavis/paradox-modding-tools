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

import { Events } from "@wailsio/runtime";
import { DidClose } from "@services/ideservice";
import { diffTab, tabOf, useIdeStore, viewsOf, IMAGES } from "./ide";
import { others } from "./windows";

/** The store in an app with a router, as main.ts makes it. */
function store() {
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
  return app.runWithContext(() => useIdeStore());
}

describe("tab keys", () => {
  it("names a diff tab and reads it back", () => {
    expect(diffTab("changes", "C:\\m\\a.txt")).toBe("git:changes:C:\\m\\a.txt");
    expect(diffTab("commit", "C:/m/a.txt", "abc123")).toBe("git:commit@abc123:C:/m/a.txt");
    expect(tabOf("git:staged:C:\\m\\a.txt")).toEqual({ file: "C:\\m\\a.txt", diff: "staged", commit: undefined });
    expect(tabOf("git:commit@abc123:C:/m/a.txt")).toEqual({ file: "C:/m/a.txt", diff: "commit", commit: "abc123" });
    expect(tabOf("C:/m/a.txt")).toEqual({ file: "C:/m/a.txt" });
  });
  it("knows which files have two views and which are images", () => {
    expect(viewsOf("readme.md")).toEqual({ code: "Raw", visual: "Rich", first: "code" });
    expect(viewsOf("logo.SVG")).toEqual({ code: "Text", visual: "Image", first: "visual" });
    expect(viewsOf("a.txt")).toBeUndefined();
    expect(IMAGES.test("x.dds")).toBe(true);
    expect(IMAGES.test("x.txt")).toBe(false);
  });
});

describe("groups and layout", () => {
  beforeEach(() => {
    others.clear();
    vi.mocked(Events.Emit).mockClear();
    vi.mocked(DidClose).mockClear();
  });
  it("opens after the shown tab, once", () => {
    const ide = store();
    ide.open("a");
    ide.open("b");
    ide.open("a");
    ide.open("c", { line: 3 });
    expect(ide.groups).toEqual([{ id: 1, files: ["a", "c", "b"], active: "c" }]);
    expect(ide.place).toEqual({ path: "c", line: 3, col: 0 });
    expect(ide.activePath).toBe("c");
    expect(ide.openFiles).toEqual(["a", "c", "b"]);
  });
  it("splits beside, sharing a sibling's room in the same direction, and nests across it", () => {
    const ide = store();
    ide.open("a");
    ide.split("right");
    expect(ide.layout).toEqual({ split: "row", parts: [{ group: 1 }, { group: 2 }], sizes: [0.5, 0.5] });
    expect(ide.focused).toBe(2);
    ide.split("right");
    expect(ide.layout).toEqual({
      split: "row",
      parts: [{ group: 1 }, { group: 2 }, { group: 3 }],
      sizes: [0.5, 0.25, 0.25],
    });
    ide.split("down");
    expect(ide.layout).toEqual({
      split: "row",
      parts: [{ group: 1 }, { group: 2 }, { split: "column", parts: [{ group: 3 }, { group: 4 }], sizes: [0.5, 0.5] }],
      sizes: [0.5, 0.25, 0.25],
    });
    expect(ide.cornerGroup).toBe(3);
    expect(ide.groups.map((group) => group.files)).toEqual([["a"], ["a"], ["a"], ["a"]]);
  });
  it("closes a group's last tab and folds the split around it; the focus moves only when its group goes", () => {
    const ide = store();
    ide.open("a");
    ide.split("right");
    ide.split("down");
    ide.closeIn(2, "a");
    expect(ide.layout).toEqual({ split: "row", parts: [{ group: 1 }, { group: 3 }], sizes: [0.5, 0.5] });
    expect(ide.focused).toBe(3);
    ide.closeIn(3, "a");
    expect(ide.layout).toEqual({ group: 1 });
    expect(ide.focused).toBe(1);
    ide.closeIn(1, "a");
    expect(ide.groups).toEqual([{ id: 1, files: [], active: "" }]);
    expect(DidClose).toHaveBeenCalledTimes(1);
  });
  it("drops a tab before another, into a group, or onto an edge", () => {
    const ide = store();
    ide.open("a");
    ide.open("b");
    ide.split("right", "a");
    ide.drop("b", 1, 2, "middle", "a");
    expect(ide.groups).toEqual([
      { id: 1, files: ["a"], active: "a" },
      { id: 2, files: ["b", "a"], active: "b" },
    ]);
    ide.drop("a", 2, 2, "down");
    expect(ide.groups.map((group) => [group.id, group.files, group.active])).toEqual([
      [1, ["a"], "a"],
      [2, ["b"], "b"],
      [3, ["a"], "a"],
    ]);
    expect(ide.layout).toEqual({
      split: "row",
      parts: [{ group: 1 }, { split: "column", parts: [{ group: 2 }, { group: 3 }], sizes: [0.5, 0.5] }],
      sizes: [0.5, 0.5],
    });
    // a group's only tab splits nothing off it
    ide.drop("a", 1, 1, "right");
    expect(ide.groups.length).toBe(3);
  });
  it("moves the shown tab to the next group, making one when there is none", () => {
    const ide = store();
    ide.open("a");
    ide.open("b");
    ide.moveToNext();
    expect(ide.groups).toEqual([
      { id: 1, files: ["a"], active: "a" },
      { id: 2, files: ["b"], active: "b" },
    ]);
    ide.focused = 1;
    ide.moveToNext();
    expect(ide.groups).toEqual([{ id: 2, files: ["b", "a"], active: "a" }]);
    expect(ide.layout).toEqual({ group: 2 });
  });
  it("remembers and restores a workspace's tabs: others' into the first group, each once, the shown file found", () => {
    const ide = store();
    ide.open("a");
    ide.open("b");
    ide.split("right", "b");
    ide.remember("w", ["c", "A", "c"]);
    ide.restore("w", true);
    expect(ide.groups).toEqual([{ id: 1, files: [], active: "" }]);
    ide.restore("w", false);
    expect(ide.groups).toEqual([
      { id: 1, files: ["a", "b", "c"], active: "b" },
      { id: 2, files: ["b"], active: "b" },
    ]);
    expect(ide.focused).toBe(1);
    expect(ide.editing).toBe("w");
  });
  it("follows a file moved on disk and closes one deleted", () => {
    const ide = store();
    ide.open("C:/m/x/a.txt");
    ide.open("C:/m/b.txt");
    ide.setUnsaved("C:/m/b.txt", true);
    ide.moved("C:/m/x", "C:/m/y");
    expect(ide.openFiles).toEqual(["C:/m/y/a.txt", "C:/m/b.txt"]);
    ide.moved("C:/m/b.txt");
    expect(ide.openFiles).toEqual(["C:/m/y/a.txt"]);
    expect([...ide.unsaved]).toEqual([]);
  });
  it("lets tabs go to another window and adopts one edited in a peek", () => {
    const ide = store();
    ide.open("a");
    ide.open("b");
    ide.setUnsaved("a", true);
    ide.release(["a"]);
    expect(ide.openFiles).toEqual(["b"]);
    expect([...ide.unsaved]).toEqual([]);
    expect(DidClose).not.toHaveBeenCalled();
    ide.adopt("c");
    expect(ide.groups).toEqual([{ id: 1, files: ["b", "c"], active: "b" }]);
  });
  it("shows a file in the window holding it, else in the one whose explorer shows its folder", () => {
    const ide = store();
    others.set("editor:x", { name: "editor:x", title: "", roots: [], tabs: ["C:/m/a.txt"], unsaved: [], problems: {} });
    others.set("files:C:/m/sub", {
      name: "files:C:/m/sub",
      title: "",
      roots: ["C:/m/sub"],
      tabs: [],
      unsaved: [],
      problems: {},
    });
    ide.open("c:/M/a.txt");
    ide.open("C:/m/sub/b.txt", { line: 4, col: 2 });
    ide.open("C:/m/c.txt");
    expect(vi.mocked(Events.Emit).mock.calls).toEqual([
      ["window:show", { to: "editor:x", route: "", path: "c:/M/a.txt", line: -1, col: 0 }],
      ["window:show", { to: "files:C:/m/sub", route: "", path: "C:/m/sub/b.txt", line: 4, col: 2 }],
    ]);
    expect(ide.openFiles).toEqual(["C:/m/c.txt"]);
  });
});
