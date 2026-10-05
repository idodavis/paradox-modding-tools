/**
 * Every binding the pages read, as Pinia Colada query options: one key factory per service, one
 * defineQueryOptions per RPC. A workspace's session-backed reads sit under ["session", id], so an event
 * or a mutation refreshes one workspace's and never builds another's (EnsureSession and GetHealth build
 * a session, dropping the live one).
 */
import { defineQueryOptions } from "@pinia/colada";
import { GetVersion } from "@services/appservice";
import { LastRunCrashed } from "@services/logservice";
import { ReadEditableBase64, ReadFileBase64, StatPath } from "@services/fileservice";
import { GetEventDetail, GetEventGraph, SearchRoots } from "@services/eventgraphservice";
import { GetHealth, GetLines, LookupLoc } from "@services/healthservice";
import type { EventGraph, Params, Roots, SourceAt } from "@services/models";
import { DiagnosticCodes } from "@services/ideservice";
import { LoadListing, UploadFiles } from "@services/releaseservice";
import { EnsureSession, GetLanguageHealth } from "@services/sessionservice";
import { PatchPage, Patches } from "@services/wikiservice";
import {
  DefaultModParent,
  FindGameInstalls,
  GetIdeRoots,
  ListGameInstalls,
  ListGames,
  ListWorkspaces,
  ModFolder,
} from "@services/workspaceservice";

export const appKeys = { version: ["app", "version"], crashed: ["app", "crashed"] } as const;

export const workspaceKeys = {
  games: ["games"] as const,
  /** Every workspace, and under it each one's own reads. */
  all: ["workspaces"] as const,
  ideRoots: (id: string) => ["workspaces", id, "ide-roots"] as const,
  modParent: (gameId: string) => ["mod-parent", gameId] as const,
  modFolder: (parent: string, name: string) => ["mod-folder", parent, name] as const,
};

export const installKeys = {
  all: ["installs"] as const,
  game: (gameId: string) => ["installs", gameId] as const,
  found: (gameId: string) => ["installs", gameId, "found"] as const,
};

export const sessionKeys = {
  /** The live session, and under it everything read from it. */
  live: (id: string) => ["session", id] as const,
  health: (id: string, order: readonly string[]) => ["session", id, "health", [...order]] as const,
  graph: (id: string, params: object) => ["session", id, "graph", params] as const,
  eventDetail: (id: string, event: string) => ["session", id, "event-detail", event] as const,
  roots: (id: string, search: string) => ["session", id, "roots", search] as const,
  loc: (id: string, key: string) => ["session", id, "loc", key] as const,
  lines: (id: string, places: readonly string[]) => ["session", id, "lines", [...places]] as const,
};

/** The scan and dumps state, read without building a session. */
export const languageKeys = {
  all: ["language"] as const,
  one: (id: string) => ["language", id] as const,
};

export const ideKeys = { codes: ["ide", "codes"] as const };

export const releaseKeys = {
  listing: (workspaceId: string, modId: string) => ["release", workspaceId, modId, "listing"] as const,
  upload: (workspaceId: string, modId: string) => ["release", workspaceId, modId, "upload"] as const,
};

export const wikiKeys = {
  all: ["wiki"] as const,
  guide: (...parts: string[]) => ["wiki", "guide", ...parts] as const,
  patches: (gameId: string) => ["wiki", "patches", gameId] as const,
  patch: (gameId: string, version: string) => ["wiki", "patch", gameId, version] as const,
};

export const fileKeys = {
  image: (path: string) => ["file", "image", path] as const,
  stat: (path: string) => ["file", "stat", path] as const,
  text: (path: string) => ["file", "text", path] as const,
};

export const versionQuery = defineQueryOptions({ key: appKeys.version, query: () => GetVersion(), staleTime: Infinity });

export const crashedQuery = defineQueryOptions({
  key: appKeys.crashed,
  query: () => LastRunCrashed(),
  staleTime: Infinity,
});

export const gamesQuery = defineQueryOptions({ key: workspaceKeys.games, query: () => ListGames(), staleTime: Infinity });

export const workspacesQuery = defineQueryOptions({
  key: workspaceKeys.all,
  query: async () => (await ListWorkspaces("")) ?? [],
});

export const ideRootsQuery = defineQueryOptions((id: string) => ({
  key: workspaceKeys.ideRoots(id),
  query: () => GetIdeRoots(id),
  enabled: !!id,
}));

export const installsQuery = defineQueryOptions((gameId: string) => ({
  key: installKeys.game(gameId),
  query: async () => (await ListGameInstalls(gameId)) ?? [],
  enabled: !!gameId,
}));

export const foundInstallsQuery = defineQueryOptions((gameId: string) => ({
  key: installKeys.found(gameId),
  query: async () => (await FindGameInstalls(gameId)) ?? [],
  enabled: !!gameId,
}));

export const modParentQuery = defineQueryOptions((gameId: string) => ({
  key: workspaceKeys.modParent(gameId),
  query: () => DefaultModParent(gameId),
  enabled: !!gameId,
}));

export const modFolderQuery = defineQueryOptions(({ parent, name }: { parent: string; name: string }) => ({
  key: workspaceKeys.modFolder(parent, name),
  query: () => ModFolder(parent, name),
  enabled: !!parent && !!name.trim(),
}));

/** The workspace's live session: built once, then its health. */
export const sessionQuery = defineQueryOptions((id: string) => ({
  key: sessionKeys.live(id),
  query: () => EnsureSession(id),
  enabled: !!id,
}));

export const languageQuery = defineQueryOptions((id: string) => ({
  key: languageKeys.one(id),
  query: () => GetLanguageHealth(id),
  enabled: !!id,
}));

/** The Health report for a load order (empty: the workspace's own); only while the session is live. */
export const healthQuery = defineQueryOptions(
  ({ id, order = [], live }: { id: string; order?: readonly string[]; live: boolean }) => ({
    key: sessionKeys.health(id, order),
    query: () => GetHealth(id, { order: [...order] }),
    enabled: !!id && live,
  }),
);

/** The event graph around a root, the one drawn kept while the next is read. */
export const graphQuery = defineQueryOptions(({ id, params, live }: { id: string; params: Params; live: boolean }) => ({
  key: sessionKeys.graph(id, params),
  query: () => GetEventGraph(id, params),
  enabled: !!id && live,
  placeholderData: (previous: EventGraph | null | undefined) => previous,
}));

/** One definition of the graph, opened. */
export const eventDetailQuery = defineQueryOptions(({ id, event, live }: { id: string; event: string; live: boolean }) => ({
  key: sessionKeys.eventDetail(id, event),
  query: () => GetEventDetail(id, event),
  enabled: !!id && live && !!event,
}));

/** The roots a search holds, by kind. */
export const rootsQuery = defineQueryOptions(({ id, search, live }: { id: string; search: string; live: boolean }) => ({
  key: sessionKeys.roots(id, search),
  query: () => SearchRoots(id, search, ""),
  enabled: !!id && live,
  placeholderData: (previous: Roots | null | undefined) => previous,
}));

/** A game's patch notes, newest first, and when its wiki was last read. */
export const patchesQuery = defineQueryOptions((gameId: string) => ({
  key: wikiKeys.patches(gameId),
  query: () => Patches(gameId),
  enabled: !!gameId,
}));

/** One patch note, by its version or title. */
export const patchPageQuery = defineQueryOptions(({ gameId, version }: { gameId: string; version: string }) => ({
  key: wikiKeys.patch(gameId, version),
  query: () => PatchPage(gameId, version),
  enabled: !!gameId && !!version,
}));

/** The source lines of the places a pane shows: a definition's statement, a reference's line. */
export const linesQuery = defineQueryOptions(
  ({ id, places, live }: { id: string; places: SourceAt[]; live: boolean }) => ({
    key: sessionKeys.lines(id, places.map((at) => `${at.path}:${at.line}:${at.col}:${at.definition}`)),
    query: () => GetLines(id, places),
    enabled: !!id && live && places.length > 0,
  }),
);

/** A localization key's entries in every language, and what reads it. */
export const locQuery = defineQueryOptions(({ id, key, live }: { id: string; key: string; live: boolean }) => ({
  key: sessionKeys.loc(id, key),
  query: () => LookupLoc(id, key),
  enabled: !!id && live && !!key,
}));

export const codesQuery = defineQueryOptions({
  key: ideKeys.codes,
  query: async () => (await DiagnosticCodes()) ?? [],
  staleTime: Infinity,
});

export const listingQuery = defineQueryOptions(({ workspaceId, modId }: { workspaceId: string; modId: string }) => ({
  key: releaseKeys.listing(workspaceId, modId),
  query: () => LoadListing(workspaceId, modId),
  enabled: !!workspaceId && !!modId,
}));

export const uploadFilesQuery = defineQueryOptions(
  ({ workspaceId, modId }: { workspaceId: string; modId: string }) => ({
    key: releaseKeys.upload(workspaceId, modId),
    query: () => UploadFiles(workspaceId, modId),
    enabled: !!workspaceId && !!modId,
  }),
);

/** An image's MIME type by its extension, for a data URL. */
export function imageMime(path: string): string {
  const ext = path.split(".").pop()?.toLowerCase() ?? "";
  const known: Record<string, string> = {
    png: "image/png",
    jpg: "image/jpeg",
    jpeg: "image/jpeg",
    svg: "image/svg+xml",
    gif: "image/gif",
    webp: "image/webp",
  };
  return known[ext] ?? "application/octet-stream";
}

/** An image file as a data URL with its size in pixels; none when it cannot be read. */
export const imageQuery = defineQueryOptions((path: string) => ({
  key: fileKeys.image(path),
  query: async () => {
    const file = await ReadFileBase64(path);
    if (!file.exists || !file.b64) return undefined;
    const image = new Image();
    image.src = `data:${imageMime(path)};base64,${file.b64}`;
    await image.decode();
    return { src: image.src, width: image.naturalWidth, height: image.naturalHeight };
  },
  enabled: !!path,
}));

/** A text file's contents, read as the editor reads them; none when it cannot be read. */
export const textQuery = defineQueryOptions((path: string) => ({
  key: fileKeys.text(path),
  query: async () => {
    const file = await ReadEditableBase64(path);
    if (!file.exists || file.reject) return undefined;
    return new TextDecoder().decode(Uint8Array.from(atob(file.b64 ?? ""), (c) => c.charCodeAt(0)));
  },
  enabled: !!path,
}));

export const statQuery = defineQueryOptions((path: string) => ({
  key: fileKeys.stat(path),
  query: () => StatPath(path),
  enabled: !!path,
}));
