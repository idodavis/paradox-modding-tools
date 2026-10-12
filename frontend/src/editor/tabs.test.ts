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
  it("knows which files have two views and which are images", () => {
    expect(viewsOf("readme.md")).toEqual({ code: "Raw", visual: "Rich", first: "code" });
    expect(viewsOf("logo.SVG")).toEqual({ code: "Text", visual: "Image", first: "visual" });
    expect(viewsOf("a.txt")).toBeUndefined();
    expect(IMAGES.test("x.dds")).toBe(true);
    expect(IMAGES.test("x.txt")).toBe(false);
  });
});
