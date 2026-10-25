import { describe, expect, it } from "vitest";

import { diffTab, parseTab } from "./tabs";

describe("tab ids", () => {
  it("reads a file tab's key", () => {
    expect(parseTab("file:c:/mod/a.txt")).toEqual({ tab: "file", key: "c:/mod/a.txt" });
  });

  it("reads a diff tab's side, key and commit", () => {
    expect(parseTab("diff:staged:c:/mod/a.txt")).toEqual({ tab: "diff", side: "staged", key: "c:/mod/a.txt" });
    expect(parseTab("diff:commit@abc123:c:/mod/a.txt")).toEqual({
      tab: "diff",
      side: "commit",
      key: "c:/mod/a.txt",
      hash: "abc123",
    });
  });

  it("makes the id parseTab reads back", () => {
    expect(diffTab("changes", "c:/mod/a.txt")).toBe("diff:changes:c:/mod/a.txt");
    expect(parseTab(diffTab("commit", "c:/mod/a.txt", "abc123"))).toEqual({
      tab: "diff",
      side: "commit",
      key: "c:/mod/a.txt",
      hash: "abc123",
    });
  });
});
