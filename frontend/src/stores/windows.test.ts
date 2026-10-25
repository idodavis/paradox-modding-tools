import { Events } from "@wailsio/runtime";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { Format } from "@internal/files/models";
import type { Place } from "@services/workspace/models";
import { inApp } from "../../vitest.setup";
import { useEditorStore } from "./editor";
import { THIS_WINDOW, useWindowsStore, type WindowState } from "./windows";

// the broadcast channels by name, so a test can say what another window announced
const channels = vi.hoisted(() => new Map<string, EventTarget>());
vi.mock("@vueuse/core", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@vueuse/core")>();
  const { shallowRef } = await import("vue");
  return {
    ...actual,
    useBroadcastChannel: ({ name }: { name: string }) => {
      const channel = channels.get(name) ?? new EventTarget();
      channels.set(name, channel);
      return { channel: shallowRef(channel), post: vi.fn() };
    },
  };
});

/** Makes the windows store, with the editor store it reads. */
function stores() {
  vi.mocked(Events.On).mockClear();
  return inApp(() => ({ editor: useEditorStore(), windows: useWindowsStore() }));
}

/** Has a window announce what it holds on the channel every window hears. */
function announce(state: WindowState): void {
  channels.get("pmt.windows")?.dispatchEvent(new MessageEvent("message", { data: { type: "state", state } }));
}

/** The handler the store registered for a Wails event. */
function handlerOf(event: string): (message: { data: unknown }) => void {
  const call = vi.mocked(Events.On).mock.calls.find(([name]) => name === event);
  if (!call) throw new Error(`no handler for ${event}`);
  return call[1] as (message: { data: unknown }) => void;
}

describe("the windows", () => {
  beforeEach(() => channels.clear());

  it("is named by its URL", () => {
    expect(THIS_WINDOW).toBe("main");
  });

  it("finds the window that holds a file, a folder or a page", () => {
    const { windows } = stores();
    announce({ name: "editor:1", files: ["a"], folders: [], page: null, active: null });
    announce({ name: "files:/mod/sub", files: [], folders: ["/mod/sub"], page: null, active: null });
    announce({ name: "page:health", files: [], folders: [], page: "health", active: null });
    expect(windows.holderOf({ file: "a" })).toBe("editor:1");
    expect(windows.holderOf({ folder: "/mod/sub" })).toBe("files:/mod/sub");
    expect(windows.holderOf({ page: "health" })).toBe("page:health");
    expect(windows.holderOf({ file: "other" })).toBe("");
  });

  it("answers its own name for a file open in its own editor", () => {
    const { editor, windows } = stores();
    const place: Place = {
      path: "m",
      key: "m",
      rel: "m",
      name: "m",
      folder: "",
      origin: "mod",
      format: Format.FormatScript,
      root: "/mod",
      database: "",
      readOnly: false,
      loaded: true,
    };
    editor.show(place);
    expect(windows.holderOf({ file: "m" })).toBe("main");
  });

  it("forgets a window once it has closed", () => {
    const { windows } = stores();
    announce({ name: "editor:1", files: ["a"], folders: [], page: null, active: null });
    handlerOf("window:closed")({ data: "editor:1" });
    expect(windows.holderOf({ file: "a" })).toBe("");
  });
});
