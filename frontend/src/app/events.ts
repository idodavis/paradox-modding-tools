/** The events table: each event Go sends and the queries it makes stale. */
import { useQueryCache, type EntryKey } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import { nextTick } from "vue";

import { errorLogQuery, isGitStatus, keys, reviewsQuery } from "../queries";

/**
 * An EventRow is what an event makes stale, from its data. A row that forgets empties the cached data and makes
 * nothing stale: the data belongs to a workspace that is gone, and a query that asks Go again before the next
 * workspace opens would be answered by that one.
 */
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
  keys.reviews,
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
  "fs:changed": {
    stale: ({ path, descriptor }) => [
      keys.file(path),
      keys.folders,
      keys.place(path),
      keys.draws,
      keys.health,
      ...(descriptor ? [keys.workspaces] : []),
      ...useQueryCache()
        .getEntries({ key: keys.gits })
        .filter((entry) => isGitStatus(entry.key))
        .map((entry) => entry.key),
      keys.repositoryWork,
    ],
  },
  // The start page's reads of the repositories go stale last, after the reads they are made of.
  "git:changed": { stale: (dir) => [keys.git(dir), keys.repositoryWork] },
  "release:changed": { stale: (modId) => [keys.release(modId), keys.ownListings] },
  "review:changed": { stale: (path) => [reviewsQuery.key, keys.baseline(path)] },
  "wiki:updated": { stale: () => [keys.wiki] },
  "errorlog:changed": { stale: () => [errorLogQuery.key] },
  "game:exited": { stale: () => [keys.state] },
  "shell:started": { stale: () => [keys.shells] },
  "shell:exited": { stale: () => [keys.shells] },
  "agent:changed": { stale: () => [keys.agents] },
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
      if (forgets) {
        affected.forEach((key) => forget(cache, key));
        return;
      }
      void refresh(cache, affected);
    });
  }
}

/**
 * refresh reads again the cached queries under each key in turn, once the key before it is read and drawn. So what an
 * earlier answer takes off the page, such as a removed mod's card, no longer asks Go about it. A query that fails
 * keeps its error for the page that shows it.
 */
async function refresh(
  cache: ReturnType<typeof useQueryCache>,
  affected: readonly (readonly unknown[])[],
): Promise<void> {
  for (const key of affected) {
    await cache.invalidateQueries({ key: key as EntryKey }).catch(() => undefined);
    await nextTick();
  }
}

/** forget empties the data of every cached query under a key. */
function forget(cache: ReturnType<typeof useQueryCache>, key: readonly unknown[]): void {
  for (const entry of cache.getEntries({ key: key as EntryKey })) {
    cache.setEntryState(entry, { status: "pending", data: undefined, error: null });
  }
}
