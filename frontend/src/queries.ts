/**
 * Every binding the pages read, as Pinia Colada query options, one per RPC, each with its key; the keys an
 * event or a mutation refreshes are exported. A workspace's session-backed reads sit under ["session", id], so
 * an event or a mutation refreshes one workspace's and never builds another's (EnsureSession and GetHealth
 * build a session, dropping the live one).
 */
import { defineQueryOptions } from "@pinia/colada";
import { GetVersion } from "@services/appservice";
import { LastRunCrashed } from "@services/logservice";
import { ReadImageFacts, ReadText } from "@services/fileservice";
import { GetEventDetail, GetEventGraph, SearchRoots } from "@services/eventgraphservice";
import { GetHealth, GetLines, LocReaders } from "@services/healthservice";
import type { EventGraph, GitCommit, Params, Roots, SourceAt } from "@services/models";
import {
  Definition,
  DiagnosticCodes,
  DocumentSymbols,
  FindLoc,
  Hover,
  References,
  ReplacedBy,
} from "@services/ideservice";
import { FileSearch } from "@services/searchservice";
import { Available, Branches, CommitFiles, Log, Remotes, Repositories, Show, Status } from "@services/gitservice";
import { CompareWithSteam, ListingSummary, LoadListing, SteamStatus, UploadFiles } from "@services/releaseservice";
import { EnsureSession, GetLanguageHealth } from "@services/sessionservice";
import { Guide, GuidePage, PatchPage, Patches } from "@services/wikiservice";
import {
  DefaultModParent,
  FindGameInstalls,
  GetIdeRoots,
  ListGameInstalls,
  ListGames,
  ListWorkspaces,
  ModFolder,
} from "@services/workspaceservice";

export const workspaceKeys = {
  /** Every workspace, and under it each one's own reads. */
  all: ["workspaces"] as const,
};

export const installKeys = { all: ["installs"] as const };

export const sessionKeys = {
  /** The live session, and under it everything read from it. */
  live: (id: string) => ["session", id] as const,
};

/** The scan and dumps state, read without building a session. */
export const languageKeys = {
  all: ["language"] as const,
  one: (id: string) => ["language", id] as const,
};

export const releaseKeys = {
  /** A mod's listing, and under it everything read of it. */
  mod: (workspaceId: string, modId: string) => ["release", workspaceId, modId] as const,
  compare: (workspaceId: string, modId: string) => ["release", workspaceId, modId, "compare"] as const,
};

export const wikiKeys = { all: ["wiki"] as const };

export const fileKeys = {
  image: (path: string) => ["file", "image", path] as const,
  text: (path: string) => ["file", "text", path] as const,
};

// the repositories' reads sit under ["git"], so a command run refreshes them all
export const gitKeys = { repos: (id: string) => ["git-repos", id] as const };

export const gitAvailableQuery = defineQueryOptions({
  key: ["git-available"],
  query: () => Available(),
  staleTime: Infinity,
});

/** The repositories of a workspace's mods. */
export const gitReposQuery = defineQueryOptions(({ id, enabled }: { id: string; enabled: boolean }) => ({
  key: gitKeys.repos(id),
  query: async () => (await Repositories(id)) ?? [],
  enabled: !!id && enabled,
}));

/** Every repository's status by its root: Source control shows one, the explorer marks them all. */
export const gitStatusesQuery = defineQueryOptions((roots: string[]) => ({
  key: ["git", "status", ...roots],
  query: async () =>
    Object.fromEntries(await Promise.all(roots.map(async (root) => [root, await Status(root)] as const))),
  enabled: roots.length > 0,
}));

/** The current branch's and its upstream's newest commits, each before its parents. */
export const gitLogQuery = defineQueryOptions(({ root, count }: { root: string; count: number }) => ({
  key: ["git", root, "log", count],
  query: async () => (await Log(root, count)) ?? [],
  enabled: !!root,
  placeholderData: (previous: GitCommit[] | undefined) => previous,
}));

/** The files a commit changed. */
export const gitCommitFilesQuery = defineQueryOptions(({ root, hash }: { root: string; hash: string }) => ({
  // a commit's files never change, so a repository's refresh leaves them be
  key: ["git-commit", root, hash],
  query: async () => (await CommitFiles(root, hash)) ?? [],
  enabled: !!root && !!hash,
  staleTime: Infinity,
}));

export const gitBranchesQuery = defineQueryOptions((root: string) => ({
  key: ["git", root, "branches"],
  query: () => Branches(root),
  enabled: !!root,
}));

export const gitRemotesQuery = defineQueryOptions(({ root, enabled }: { root: string; enabled: boolean }) => ({
  key: ["git", root, "remotes"],
  query: () => Remotes(root),
  enabled: enabled && !!root,
}));

/** A file's text at a revision, or staged when rev is "" (":2" and ":3" are a conflict's two sides). */
export const gitTextQuery = defineQueryOptions(({ root, rev, path }: { root: string; rev: string; path: string }) => ({
  key: ["git", root, "text", rev, path],
  query: () => Show(root, rev, path),
  enabled: !!root && !!path,
}));

export const versionQuery = defineQueryOptions({
  key: ["app", "version"],
  query: () => GetVersion(),
  staleTime: Infinity,
});

export const crashedQuery = defineQueryOptions({
  key: ["app", "crashed"],
  query: () => LastRunCrashed(),
  staleTime: Infinity,
});

export const gamesQuery = defineQueryOptions({ key: ["games"], query: () => ListGames(), staleTime: Infinity });

export const workspacesQuery = defineQueryOptions({
  key: workspaceKeys.all,
  query: async () => (await ListWorkspaces()) ?? [],
});

export const ideRootsQuery = defineQueryOptions((id: string) => ({
  key: ["workspaces", id, "ide-roots"],
  query: () => GetIdeRoots(id),
  enabled: !!id,
}));

export const installsQuery = defineQueryOptions((gameId: string) => ({
  key: ["installs", gameId],
  query: async () => (await ListGameInstalls(gameId)) ?? [],
  enabled: !!gameId,
}));

export const foundInstallsQuery = defineQueryOptions((gameId: string) => ({
  // apart from the installs: a Steam scan, run again only by its own page
  key: ["installs-found", gameId],
  query: async () => (await FindGameInstalls(gameId)) ?? [],
  enabled: !!gameId,
}));

export const modParentQuery = defineQueryOptions((gameId: string) => ({
  key: ["mod-parent", gameId],
  query: () => DefaultModParent(gameId),
  enabled: !!gameId,
}));

export const modFolderQuery = defineQueryOptions(({ parent, name }: { parent: string; name: string }) => ({
  key: ["mod-folder", parent, name],
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
    key: ["session", id, "health", [...order]],
    query: () => GetHealth(id, { order: [...order] }),
    enabled: !!id && live,
  }),
);

/** The event graph around a root, the one drawn kept while the next is read. */
export const graphQuery = defineQueryOptions(({ id, params, live }: { id: string; params: Params; live: boolean }) => ({
  key: ["session", id, "graph", params],
  query: () => GetEventGraph(id, params),
  enabled: !!id && live,
  placeholderData: (previous: EventGraph | null | undefined) => previous,
}));

/** One definition of the graph, opened. */
export const eventDetailQuery = defineQueryOptions(
  ({ id, event, live }: { id: string; event: string; live: boolean }) => ({
    key: ["session", id, "event-detail", event],
    query: () => GetEventDetail(id, event),
    enabled: !!id && live && !!event,
  }),
);

/** The roots a search holds, by kind. */
export const rootsQuery = defineQueryOptions(({ id, search, live }: { id: string; search: string; live: boolean }) => ({
  key: ["session", id, "roots", search],
  query: () => SearchRoots(id, search),
  enabled: !!id && live,
  placeholderData: (previous: Roots | null | undefined) => previous,
}));

/** A game's patch notes, newest first, and when its wiki was last read. */
export const patchesQuery = defineQueryOptions((gameId: string) => ({
  key: ["wiki", "patches", gameId],
  query: () => Patches(gameId),
  enabled: !!gameId,
}));

/** One patch note, by its version or title. */
export const patchPageQuery = defineQueryOptions(({ gameId, version }: { gameId: string; version: string }) => ({
  key: ["wiki", "patch", gameId, version],
  query: () => PatchPage(gameId, version),
  enabled: !!gameId && !!version,
}));

/** The wiki's pages for a file of a workspace, and the kind of file it is. */
export const guideQuery = defineQueryOptions(
  ({ id, path, enabled }: { id: string; path: string; enabled: boolean }) => ({
    key: ["wiki", "guide", id, path],
    query: () => Guide(id, path),
    enabled: enabled && !!id && !!path,
  }),
);

/** One guide page whole, by its title: the page a Wiki tab shows (a guide's tabs carry no text). */
export const guidePageQuery = defineQueryOptions(
  ({ gameId, title, enabled }: { gameId: string; title: string; enabled: boolean }) => ({
    key: ["wiki", "page", gameId, title],
    query: () => GuidePage(gameId, title),
    enabled: enabled && !!gameId && !!title,
  }),
);

/** The source lines of the places a pane shows: a definition's statement, a reference's line. */
export const linesQuery = defineQueryOptions(
  ({ id, places, live }: { id: string; places: SourceAt[]; live: boolean }) => ({
    key: ["session", id, "lines", places.map((at) => `${at.path}:${at.line}:${at.col}:${at.definition}`)],
    query: () => GetLines(id, places),
    enabled: !!id && live && places.length > 0,
  }),
);

/** What reads a localization key. */
export const locReadersQuery = defineQueryOptions(({ id, key, live }: { id: string; key: string; live: boolean }) => ({
  key: ["session", id, "loc", key],
  query: () => LocReaders(id, key),
  enabled: !!id && live && !!key,
}));

/** The mods' copies of a game file, which the game loads in its place. */
export const replacedQuery = defineQueryOptions(({ id, path }: { id: string; path: string }) => ({
  key: ["ide", "replaced", path],
  query: () => ReplacedBy(id, path).then((copies) => copies ?? []),
}));

/** What a word is, at a place of a file (a 0-based line and UTF-8 byte column). */
export const hoverQuery = defineQueryOptions(
  ({ id, path, line, col }: { id: string; path: string; line: number; col: number }) => ({
    key: ["ide", "hover", id, path, line, col],
    query: () => Hover(id, path, line, col),
  }),
);

/** The files whose path holds a text, in the IDE's folders. */
export const filesQuery = defineQueryOptions(
  ({ id, pattern, folders }: { id: string; pattern: string; folders: string[] }) => ({
    key: ["ide", "files", id, pattern],
    query: () => FileSearch({ filePattern: pattern, maxResults: 50, folders: folders.map((path) => ({ path })) }),
    enabled: !!id && !!pattern && folders.length > 0,
  }),
);

/** The names a file defines. */
/** The localization keys starting with a prefix, each at its entry in the workspace's language. */
export const locQuery = defineQueryOptions(({ id, prefix }: { id: string; prefix: string }) => ({
  key: ["ide", "loc", id, prefix],
  query: async () => (await FindLoc(id, prefix, 30)) ?? [],
  enabled: !!id && !!prefix,
}));

export const symbolsQuery = defineQueryOptions(({ id, path }: { id: string; path: string }) => ({
  key: ["ide", "symbols", path],
  query: async () => (await DocumentSymbols(id, path)) ?? [],
  enabled: !!id && !!path,
}));

/** A word's definitions, or every place that names it, from a place of a file. */
export const sitesQuery = defineQueryOptions(
  ({
    id,
    path,
    line,
    col,
    mode,
  }: {
    id: string;
    path: string;
    line: number;
    col: number;
    mode: "definition" | "references";
  }) => ({
    key: ["ide", "sites", mode, path, line, col],
    query: async () => (await (mode === "definition" ? Definition : References)(id, path, line, col)) ?? [],
  }),
);

export const codesQuery = defineQueryOptions({
  key: ["ide", "codes"],
  query: async () => (await DiagnosticCodes()) ?? [],
  staleTime: Infinity,
});

export const listingQuery = defineQueryOptions(({ workspaceId, modId }: { workspaceId: string; modId: string }) => ({
  key: [...releaseKeys.mod(workspaceId, modId), "listing"],
  query: () => LoadListing(workspaceId, modId),
  enabled: !!workspaceId && !!modId,
}));

/** A mod's Workshop id, listing image and description source, read without writing. */
export const summaryQuery = defineQueryOptions(({ workspaceId, modId }: { workspaceId: string; modId: string }) => ({
  key: [...releaseKeys.mod(workspaceId, modId), "summary"],
  query: () => ListingSummary(workspaceId, modId),
  enabled: !!workspaceId && !!modId,
}));

/** A mod against the copy Steam has, by the snapshot PMT kept; never a download. */
export const compareQuery = defineQueryOptions(({ workspaceId, modId }: { workspaceId: string; modId: string }) => ({
  key: releaseKeys.compare(workspaceId, modId),
  query: () => CompareWithSteam(workspaceId, modId),
  enabled: !!workspaceId && !!modId,
}));

/** Why Steam cannot take an upload for a workspace's game now ("" when it can), asked of the publish's child. */
export const steamStatusQuery = defineQueryOptions(({ id, enabled }: { id: string; enabled: boolean }) => ({
  key: ["steam-status", id] as const,
  query: () =>
    SteamStatus(id).then(
      () => "",
      (error: unknown) => (error instanceof Error ? error.message : String(error)),
    ),
  enabled: !!id && enabled,
  staleTime: 0,
}));

export const uploadFilesQuery = defineQueryOptions(
  ({ workspaceId, modId }: { workspaceId: string; modId: string }) => ({
    key: [...releaseKeys.mod(workspaceId, modId), "upload"],
    query: () => UploadFiles(workspaceId, modId),
    enabled: !!workspaceId && !!modId,
  }),
);

/** An image file: its URL on Go's image route (a DDS or TGA decoded there), its size in pixels (the header's) and in
 * bytes, and its facts; none when it cannot be read. */
export const imageQuery = defineQueryOptions((path: string) => ({
  key: fileKeys.image(path),
  query: async () => {
    const facts = await ReadImageFacts(path).catch(() => undefined);
    return facts && { src: facts.url, width: facts.info.width, height: facts.info.height, bytes: facts.bytes, facts };
  },
  enabled: !!path,
}));

/** A text file as the editor reads it, decoded in Go as the engine decodes it; `reject` says why there is no text. */
export const textQuery = defineQueryOptions((path: string) => ({
  key: fileKeys.text(path),
  query: () => ReadText(path),
  enabled: !!path,
}));
