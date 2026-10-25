import { describe, expect, it } from "vitest";

import { diffTab, parseTab } from "./tabs";

describe("tab ids", () => {
  it("reads a file tab's key", () => {
    expect(parseTab("file:c:/mod/a.txt")).toEqual({ tab: "file", key: "c:/mod/a.txt" });
  });

  it("reads a diff tab's side, repository, key and commit", () => {
    expect(parseTab("diff:staged:C%3A%2Fmod:c:/mod/a.txt")).toEqual({
      tab: "diff",
      side: "staged",
      root: "C:/mod",
      key: "c:/mod/a.txt",
    });
    expect(parseTab("diff:commit@abc123:C%3A%2Fmod:c:/mod/a.txt")).toEqual({
      tab: "diff",
      side: "commit",
      root: "C:/mod",
      key: "c:/mod/a.txt",
      hash: "abc123",
    });
  });

  it("makes the id parseTab reads back", () => {
    expect(diffTab("changes", "C:/mod", "c:/mod/a.txt")).toBe("diff:changes:C%3A%2Fmod:c:/mod/a.txt");
    expect(parseTab(diffTab("commit", "C:/mod", "c:/mod/a.txt", "abc123"))).toEqual({
      tab: "diff",
      side: "commit",
      root: "C:/mod",
      key: "c:/mod/a.txt",
      hash: "abc123",
    });
  });
});
