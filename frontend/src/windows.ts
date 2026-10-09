/**
 * The app's windows (O-R24, the MultiWindow board). The main window has the top bar; each other shows one thing: a
 * workspace page (`page:<route name>`), an editor group (`editor:<n>`), the game's files (`game`), a folder of the
 * workspace (`files:<path>`) or the panel (`panel`), opened through WindowService and named in its URL. The windows
 * tell each other what they hold (window:held, events.ts), so a file is open in one window at a time, and one under
 * a folder a window shows opens there; closing one hands its tabs back to the main window, and only the main
 * window's close ends the app.
 */
import { reactive, shallowRef } from "vue";
import { until } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import type { WindowHeld } from "@services/models";
import type { Edge } from "./stores/ide";
import { Close, Open, Show } from "@services/windowservice";

/** This window's name, and what it shows: the main window, a page, an editor group, the game's files or the panel. */
export const windowName = new URLSearchParams(location.search).get("window") ?? "main";
export const windowPart = windowName.split(":")[0] as "main" | "page" | "editor" | "game" | "files" | "panel";
export const isMain = windowPart === "main";
/** Whether this window holds files: the others ask the main window, or the window holding one, to show it. */
export const holdsFiles = isMain || windowPart === "editor" || windowPart === "game" || windowPart === "files";

/** What the other windows hold, by name, as each last told. */
export const others = reactive(new Map<string, WindowHeld>());
/** The folders this window's explorer shows, whose files open here (the game's files, or a folder's). */
export const roots = shallowRef<string[]>([]);
/** Where a tab dragged in from another window goes once it is here: its group, the edge, the tab it goes before. */
export const placing = new Map<string, { group: number; edge: Edge; before?: string }>();

// a window opened, waiting for it to say what it holds (it is ready for tabs then)
const opening = new Map<string, () => void>();
// the tabs handed to this window, received one hand at a time, and those that have arrived
let receiving = Promise.resolve();
const arrived = reactive(new Set<string>());

/** Receive tabs handed to this window, after those handed before. */
export function receiveTabs(tabs: string[], work: () => Promise<void>): void {
  tabs.forEach((tab) => arrived.add(tab));
  receiving = receiving.then(work);
}

/** A window told what it holds. */
export function held(data: WindowHeld): void {
  others.set(data.name, data);
  opening.get(data.name)?.();
  opening.delete(data.name);
}

/** Open a window at a route, or show it; resolved once it is ready for tabs. */
export function openWindow(route: string, name: string, title: string): Promise<void> {
  if (others.has(name)) return Show(name);
  return new Promise((resolve) => {
    opening.set(name, resolve);
    void Open(route, name, title);
  });
}

/** Close the windows that match, which hand their tabs back; resolved once they are gone and their tabs here, in
 * whichever order Wails brings the two (it keeps no order between events). */
export async function dock(which: (one: WindowHeld) => boolean): Promise<void> {
  const closing = [...others.values()].filter(which);
  const tabs = closing.flatMap((one) => one.tabs ?? []);
  tabs.forEach((tab) => arrived.delete(tab));
  closing.forEach((one) => void Close(one.name));
  await until(() => closing.some((one) => others.has(one.name)) || tabs.some((tab) => !arrived.has(tab))).toBe(false, {
    timeout: 5000,
  });
  await receiving;
}

/** Tell the other windows, if any, that this one saved something they read, so they read it again. */
export function changed(): void {
  if (others.size) void Events.Emit("window:changed", windowName);
}
