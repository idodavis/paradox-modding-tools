/**
 * The events table: each event Go sends and the queries it makes stale. The editor store reads fs:changed for the open
 * files and the search store reads search:hits.
 */
import { useQueryCache, type EntryKey } from "@pinia/colada";
import { Events } from "@wailsio/runtime";

import { errorLogQuery, isGitStatus, keys, updateQuery } from "../queries";

/** An EventRow is what an event makes stale, from its data. A row that forgets also empties the cached data first. */
type EventRow<E extends keyof Events.CustomEvents> = {
  stale: (data: Events.CustomEvents[E]) => readonly (readonly unknown[])[];
  forgets?: true;
};

/** WORKSPACE_KEYS holds the queries that depend on the open workspace. */
const WORKSPACE_KEYS = [
  keys.state,
  keys.roots,
  keys.files,
  keys.folders,
  keys.places,
  keys.draws,
  keys.health,
  keys.graph,
  keys.wiki,
  keys.gits,
  keys.releases,
  keys.lsp,
  keys.search,
] as const;

/** SECTIONS holds the queries each section of config.json feeds. */
const SECTIONS: Record<string, readonly (readonly unknown[])[]> = {
  settings: [keys.settings, keys.folders],
  workspaces: [keys.workspaces, keys.state, keys.health, keys.gits, keys.releases],
  installs: [keys.installs, keys.state],
  agents: [keys.agents],
};

/** EVENTS holds each event Go sends that makes queries stale. */
const EVENTS: { [E in keyof Events.CustomEvents]?: EventRow<E> } = {
  "config:changed": { stale: (section) => (section ? (SECTIONS[section] ?? []) : Object.values(SECTIONS).flat()) },
  "workspace:opened": { stale: () => [...WORKSPACE_KEYS, keys.workspaces] },
  "workspace:closed": { stale: () => WORKSPACE_KEYS, forgets: true },
  "install:scanned": { stale: () => [keys.installs, keys.state] },
  // a changed file is read again, with the listings and places that name it, the pictures drawn from files and the
  // git statuses, what the start page reads of them, and the mods' descriptors when a descriptor changed
  "fs:changed": {
    stale: ({ path, descriptor }) => [
      keys.file(path),
      keys.folders,
      keys.place(path),
      keys.draws,
      ...(descriptor ? [keys.workspaces] : []),
      ...useQueryCache()
        .getEntries({ key: keys.gits })
        .filter((entry) => isGitStatus(entry.key))
        .map((entry) => entry.key),
      keys.leftOff,
    ],
  },
  // what the start page reads of the repositories is made stale last, after the reads it is made of
  "git:changed": { stale: (dir) => [keys.git(dir), keys.leftOff] },
  "release:changed": { stale: (modId) => [keys.release(modId), keys.ownListings] },
  "wiki:updated": { stale: () => [keys.wiki] },
  "errorlog:changed": { stale: () => [errorLogQuery.key] },
  "game:exited": { stale: () => [keys.state] },
  "shell:started": { stale: () => [keys.shells] },
  "shell:exited": { stale: () => [keys.shells] },
  "agent:changed": { stale: () => [keys.agents] },
  "app:update": { stale: () => [updateQuery.key] },
};

/** subscribeEvents subscribes to every event in the table. */
export function subscribeEvents(): void {
  const cache = useQueryCache();
  for (const [event, { stale, forgets }] of Object.entries(EVENTS) as [
    keyof Events.CustomEvents,
    EventRow<keyof Events.CustomEvents>,
  ][]) {
    Events.On(event, ({ data }) => {
      const affected = stale(data);
      if (forgets) affected.forEach((key) => forget(cache, key));
      // a query that fails keeps its error for the page that shows it, so the refresh's rejection says nothing more
      for (const key of affected) cache.invalidateQueries({ key: key as EntryKey }).catch(() => undefined);
    });
  }
}

/** forget empties the data of every cached query under a key, so no page shows what a closed workspace gave it. */
function forget(cache: ReturnType<typeof useQueryCache>, key: readonly unknown[]): void {
  for (const entry of cache.getEntries({ key: key as EntryKey })) {
    cache.setEntryState(entry, { status: "pending", data: undefined, error: null });
  }
}
