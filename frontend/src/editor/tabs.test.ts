import { describe, expect, it } from "vitest";
import { diffTab, IMAGES, tabOf, viewsOf } from "./tabs";

describe("tab keys", () => {
  it("names a diff tab and reads it back", () => {
    expect(diffTab("changes", "C:\\m\\a.txt")).toBe("git:changes:C:\\m\\a.txt");
    expect(diffTab("commit", "C:/m/a.txt", "abc123")).toBe("git:commit@abc123:C:/m/a.txt");
    expect(tabOf("git:staged:C:\\m\\a.txt")).toEqual({ file: "C:\\m\\a.txt", diff: "staged", commit: undefined });
    expect(tabOf("git:commit@abc123:C:/m/a.txt")).toEqual({ file: "C:/m/a.txt", diff: "commit", commit: "abc123" });
    expect(tabOf("C:/m/a.txt")).toEqual({ file: "C:/m/a.txt" });
  });
  it("knows which files have views, a split and a panel, and which are images", () => {
    expect(viewsOf("gui/window_character.gui")).toEqual({
      ext: "gui",
      label: "GUI",
      code: "Code",
      visual: "Visual",
      first: "split",
      split: true,
      panel: "Widgets",
      drawing: "gui",
    });
    expect(viewsOf("C:\\eu5\\main_menu\\common\\coat_of_arms\\coat_of_arms\\x.txt")?.drawing).toBe("coat");
    expect(viewsOf("readme.md")?.split).toBe(false);
    expect(viewsOf("logo.SVG")?.first).toBe("visual");
    expect(viewsOf("events/x.txt")).toBeUndefined();
    expect(IMAGES.test("x.dds")).toBe(true);
    expect(IMAGES.test("x.txt")).toBe(false);
  });
});
