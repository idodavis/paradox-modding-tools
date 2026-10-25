/**
 * The events table: each event Go sends and the queries it makes stale. Nothing else invalidates a query but a
 * mutation and the text the models send to Go. The editor's own subscriptions (fs:changed to the models,
 * search:hits to the search store) and the windows' channel are handled where they are used; everything else is
 * here.
 */
import { useQueryCache, type EntryKey } from "@pinia/colada";
import { Events } from "@wailsio/runtime";

import { errorLogQuery, keys, updateQuery } from "../queries";

/** One row: an event and the query keys it refreshes, computed from the event's data. */
export type EventRow = { event: string; stale: (data: unknown) => readonly (readonly unknown[])[] };

// the reads that hang on the open workspace, so any change of it makes them stale
const FOLDERS = ["folder"] as const;
const WORKSPACE_KEYS = [
  keys.state,
  keys.roots,
  ["file"],
  FOLDERS,
  ["place"],
  ["draw"],
  keys.health,
  keys.graph,
  keys.wiki,
  ["git"],
  ["release"],
  ["lsp"],
  ["search"],
] as const;

// what each section of config.json feeds
const SECTIONS: Record<string, readonly (readonly unknown[])[]> = {
  settings: [keys.settings, FOLDERS],
  workspaces: [keys.workspaces, keys.state, ["git"], ["release"]],
  installs: [keys.installs, keys.state],
};

/** Every event Go sends to the pages, and what each makes stale. */
export const EVENTS: readonly EventRow[] = [
  {
    event: "config:changed",
    stale: (section) => (toText(section) ? (SECTIONS[toText(section)] ?? []) : Object.values(SECTIONS).flat()),
  },
  { event: "workspace:opened", stale: () => WORKSPACE_KEYS },
  { event: "workspace:closed", stale: () => WORKSPACE_KEYS },
  { event: "install:scanned", stale: () => [keys.installs, keys.state] },
  // a changed file is read again, with the listings and places that name it, the pictures drawn from files and the
  // git statuses
  {
    event: "fs:changed",
    stale: (change) => [keys.file(toPath(change)), FOLDERS, ["place", toPath(change)], ["draw"], ...listStatusKeys()],
  },
  { event: "git:changed", stale: (dir) => [keys.git(toText(dir))] },
  { event: "release:changed", stale: (modId) => [keys.release(toText(modId))] },
  { event: "wiki:updated", stale: () => [keys.wiki] },
  { event: "errorlog:changed", stale: () => [errorLogQuery.key] },
  { event: "game:exited", stale: () => [keys.state] },
  { event: "app:update", stale: () => [updateQuery.key] },
];

/** Subscribes to every event in the table. Called once at startup. */
export function subscribeEvents(): void {
  const cache = useQueryCache();
  for (const { event, stale } of EVENTS) {
    Events.On(event, ({ data }) => {
      for (const key of stale(data)) void cache.invalidateQueries({ key: key as EntryKey });
    });
  }
}

/** Lists the keys of the git statuses in the cache, one per repository. */
function listStatusKeys(): (readonly unknown[])[] {
  return useQueryCache()
    .getEntries({ key: ["git"] })
    .filter((entry) => entry.key[2] === "status")
    .map((entry) => entry.key);
}

/** Converts the text an event carries, or "" when it carries none. */
function toText(data: unknown): string {
  return typeof data === "string" ? data : "";
}

/** Converts an fs:changed event to the path it names. */
function toPath(change: unknown): string {
  return typeof change === "object" && change !== null && "path" in change ? toText(change.path) : "";
}

export type { keys };
