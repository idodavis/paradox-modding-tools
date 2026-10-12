import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@wailsio/runtime")>();
  return { ...actual, Events: { Emit: vi.fn(() => Promise.resolve()), On: vi.fn(() => () => undefined) } };
});
vi.mock("@services/windowservice", () => ({
  Open: vi.fn(() => Promise.resolve()),
  Show: vi.fn(() => Promise.resolve()),
  Close: vi.fn(() => Promise.resolve()),
}));

import { Events } from "@wailsio/runtime";
import { Close, Open, Show } from "@services/windowservice";
import { changed, dock, held, isMain, openWindow, others, receiveTabs, windowName, windowPart } from "./windows";

const state = (name: string, tabs: string[] = []) => ({
  name,
  title: name,
  roots: [],
  tabs,
  unsaved: [],
  problems: {},
});

describe("the windows", () => {
  beforeEach(() => {
    others.clear();
    vi.mocked(Events.Emit).mockClear();
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
    const opened = openWindow("/workspace/w", "editor:1", "a.txt");
    expect(Open).toHaveBeenCalledWith("/workspace/w", "editor:1", "a.txt");
    held(state("editor:1"));
    await opened;
    expect(others.get("editor:1")?.title).toBe("editor:1");
    await openWindow("/workspace/w", "editor:1", "a.txt");
    expect(Show).toHaveBeenCalledWith("editor:1");
    expect(Open).toHaveBeenCalledTimes(1);
  });
  it("docks the windows that match, once they are gone and their tabs here", async () => {
    held(state("editor:1", ["a"]));
    held(state("page:health"));
    let received = false;
    const docked = dock((one) => one.name.startsWith("editor:")).then(() => (received = true));
    expect(Close).toHaveBeenCalledWith("editor:1");
    expect(Close).toHaveBeenCalledTimes(1);
    await Promise.resolve();
    expect(received).toBe(false);
    others.delete("editor:1");
    receiveTabs(["a"], () => Promise.resolve());
    await docked;
    expect(received).toBe(true);
    expect(others.has("page:health")).toBe(true);
  });
  it("tells the others of a save only when there are others", () => {
    changed();
    expect(Events.Emit).not.toHaveBeenCalled();
    held(state("panel"));
    changed();
    expect(Events.Emit).toHaveBeenCalledWith("window:changed", "main");
  });
});
