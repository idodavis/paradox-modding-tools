/**
 * The events table: each event Go sends and the queries it makes stale. Nothing else invalidates a query. The
 * editor's own subscriptions (fs:changed to the models, search:hits to the search store) and the windows' channel
 * are handled where they are used; everything else is here.
 */
import type { keys } from "../queries";

/** One row: an event and the query keys it refreshes, computed from the event's data. */
export type EventRow = { event: string; stale: (data: unknown) => readonly (readonly unknown[])[] };

/** Every event Go sends to the pages, and what each makes stale. */
export const EVENTS: readonly EventRow[] = [];

/** Subscribes to every event in the table. Called once at startup. */
export function subscribeEvents(): void {
  throw new Error("stub");
}

export type { keys };
