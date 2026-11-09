import { beforeEach, describe, expect, it, vi } from "vitest";

import { Format } from "@internal/files/models";
import type { Place } from "@services/workspace/models";
import { inApp } from "../../vitest.setup";
import { closeModel } from "../pages/ide/editor/models";
import { useEditorStore } from "./editor";
import { useWindowsStore } from "./windows";

/** place returns a place whose key, path and rel are the same word, of the given format. */
function place(key: string, format = Format.FormatScript): Place {
  return {
    path: key,
    key,
    rel: key,
    name: key,
    folder: "",
    origin: "mod",
    stage: "",
    format,
    root: "/mod",
    database: "",
    readOnly: false,
    isDir: false,
    drawable: false,
    readme: "",
  };
}

beforeEach(() => vi.mocked(closeModel).mockClear());

/** stores makes the editor store and the windows store it asks, together. */
function stores() {
  return inApp(() => ({ editor: useEditorStore(), windows: useWindowsStore() }));
}

/** tabsOf returns the ids of the tabs of the active group, with whether each is a preview. */
function tabsOf(editor: ReturnType<typeof useEditorStore>) {
  return (editor.activeGroup()?.tabs ?? []).map((tab) => [tab.id, tab.preview]);
}

describe("showing a place", () => {
  it("opens an explorer click as a preview tab that the next explorer click replaces", () => {
    const { editor } = stores();
    editor.show(place("a"), undefined, "explorer");
    editor.show(place("b"), undefined, "explorer");
    expect(tabsOf(editor)).toEqual([["file:b", true]]);
    editor.show(place("c"));
    expect(tabsOf(editor)).toEqual([
      ["file:b", true],
      ["file:c", false],
    ]);
  });

  it("pins a preview tab when the file is shown any other way", () => {
    const { editor } = stores();
    editor.show(place("a"), undefined, "explorer");
    editor.show(place("a"), undefined, "navigate");
    expect(tabsOf(editor)).toEqual([["file:a", false]]);
    editor.show(place("b"), undefined, "explorer");
    editor.pin("file:b");
    expect(tabsOf(editor)).toEqual([
      ["file:a", false],
      ["file:b", false],
    ]);
  });

  it("shows a file another window holds in that window and opens no tab here", () => {
    const { editor, windows } = stores();
    vi.spyOn(windows, "holderOf").mockReturnValue("editor:1");
    const showElsewhere = vi.spyOn(windows, "showElsewhere").mockImplementation(() => undefined);
    editor.show(place("a"));
    expect(showElsewhere).toHaveBeenCalledWith("editor:1", place("a"), undefined, "navigate");
    expect(tabsOf(editor)).toEqual([]);
  });
});

describe("groups", () => {
  it("splits the active group beside it and below it, each holding a copy of the active tab", () => {
    const { editor } = stores();
    editor.show(place("a"));
    editor.split("row");
    editor.split("column");
    expect(editor.layout().rows.map((row) => row.columns.length)).toEqual([2, 1]);
    expect(
      editor
        .layout()
        .rows.flatMap((row) => row.columns)
        .map((group) => group.active),
    ).toEqual(["file:a", "file:a", "file:a"]);
  });

  it("moves a tab to another group, or into a new one beside the group it came from", () => {
    const { editor } = stores();
    editor.show(place("a"));
    editor.show(place("b"));
    editor.show(place("c"));
    editor.split("row");
    const [first, second] = editor.layout().rows[0]?.columns ?? [];
    editor.move("file:a", second?.id ?? "", 0);
    expect(first?.tabs.map((tab) => tab.id)).toEqual(["file:b", "file:c"]);
    expect(second?.tabs.map((tab) => tab.id)).toEqual(["file:a", "file:c"]);
    editor.activate(first?.id ?? "");
    editor.move("file:b", first?.id ?? "", "right");
    expect(editor.layout().rows[0]?.columns.map((group) => group.tabs.map((tab) => tab.id))).toEqual([
      ["file:c"],
      ["file:b"],
      ["file:a", "file:c"],
    ]);
  });

  it("closes a file's model only when its last tab closes", () => {
    const { editor } = stores();
    editor.show(place("a"));
    editor.split("row");
    editor.close("file:a");
    expect(closeModel).not.toHaveBeenCalled();
    expect(editor.layout().rows[0]?.columns).toHaveLength(1);
    editor.close("file:a");
    expect(closeModel).toHaveBeenCalledWith("a");
    expect(editor.activeTab()).toBeNull();
  });
});
