/** The queries that read Go, one per RPC, and the keys that events and mutations refresh. */
import { defineQueryOptions, useMutation, useQueryCache, type UseQueryOptions } from "@pinia/colada";

import * as agent from "@services/agent/service";
import * as app from "@services/app/service";
import * as draw from "@services/draw/service";
import * as git from "@services/git/service";
import * as graph from "@services/graph/service";
import * as health from "@services/health/service";
import * as lsp from "@services/lsp/service";
import * as mcp from "@services/mcp/service";
import * as search from "@services/search/service";
import * as steam from "@services/steam/service";
import * as terminal from "@services/terminal/service";
import * as wiki from "@services/wiki/service";
import * as workspace from "@services/workspace/service";
import type { Coat, PictureList, States } from "@services/draw/models";
import type { Filter } from "@services/graph/models";
import type { Site } from "@services/lsp/models";

/** fileUrl returns the URL that serves a file. */
export function fileUrl(path: string): string {
  return `/files?path=${encodeURIComponent(path)}`;
}

/** loadQuery reads a query outside a component, from the cache if it is fresh. It throws if Go's read fails. */
export async function loadQuery<T>(options: UseQueryOptions<T>): Promise<T | undefined> {
  const cache = useQueryCache();
  return (await cache.refresh(cache.ensure(options))).data;
}

/** GIT_STATUS is the last part of the key of a repository's status. */
const GIT_STATUS = "status";

// the first key part shared by every query of one kind, so one key refreshes the whole kind
const KINDS = {
  files: ["file"],
  folders: ["folder"],
  places: ["place"],
  draws: ["draw"],
  gits: ["git"],
  releases: ["release"],
} as const;

/** keys holds the query keys that events and mutations refresh. A kind's plural, such as `files`, covers its kind. */
export const keys = {
  ...KINDS,
  settings: ["app", "settings"] as const,
  installs: ["workspace", "installs"] as const,
  workspaces: ["workspace", "workspaces"] as const,
  state: ["workspace", "state"] as const,
  file: (path: string) => [...KINDS.files, path] as const,
  folder: (dir: string) => [...KINDS.folders, dir] as const,
  place: (path: string) => [...KINDS.places, path] as const,
  roots: ["roots"] as const,
  draw: (path: string) => [...KINDS.draws, path] as const,
  health: ["health"] as const,
  graph: ["graph"] as const,
  wiki: ["wiki"] as const,
  git: (dir: string) => [...KINDS.gits, dir] as const,
  repositoryWork: [...KINDS.gits, "repository-work"] as const,
  ownListings: [...KINDS.releases, "own"] as const,
  gitStatus: (dir: string) => [...KINDS.gits, dir, GIT_STATUS] as const,
  release: (modId: string) => [...KINDS.releases, modId] as const,
  lsp: ["lsp"] as const,
  search: ["search"] as const,
  shells: ["terminal", "shells"] as const,
  agents: ["agent"] as const,
};

/** isGitStatus reports whether a query key is a repository's status. */
export function isGitStatus(key: readonly unknown[]): boolean {
  return key.length === 3 && key[0] === KINDS.gits[0] && key[2] === GIT_STATUS;
}

// the app
export const versionQuery = defineQueryOptions({ key: ["app", "version"], query: () => app.Version() });
export const settingsCatalogQuery = defineQueryOptions({
  key: ["app", "catalog"],
  query: () => app.ListSettings(),
});
export const settingsQuery = defineQueryOptions((workspaceId: string) => ({
  key: [...keys.settings, workspaceId],
  query: () => app.ReadSettings(workspaceId),
}));
// a check GitHub refuses, such as a rate limit, finds no update
export const updateQuery = defineQueryOptions({
  key: ["app", "update"],
  query: () => app.CheckUpdate().catch(() => ({ version: "", available: false })),
});
export const crashedQuery = defineQueryOptions({ key: ["app", "crashed"], query: () => app.CrashedLastRun() });

// the workspace and its files
export const gamesQuery = defineQueryOptions({
  key: ["workspace", "games"],
  query: () => workspace.ListGames(),
});
export const installsQuery = defineQueryOptions((gameId: string) => ({
  key: [...keys.installs, gameId],
  query: () => workspace.ListInstalls(gameId),
}));
export const readyInstallsQuery = defineQueryOptions({
  key: [...keys.installs, "ready"],
  query: () => workspace.ListReadyInstalls(),
});
export const detectedInstallsQuery = defineQueryOptions((gameId: string) => ({
  key: ["workspace", "detected", gameId],
  query: () => workspace.DetectInstalls(gameId),
}));
export const workspacesQuery = defineQueryOptions({
  key: keys.workspaces,
  query: () => workspace.ListWorkspaces(),
});
export const releasesQuery = defineQueryOptions({ key: ["app", "releases"], query: () => app.ListReleases() });
export const stateQuery = defineQueryOptions({ key: keys.state, query: () => workspace.State() });
export const languagesQuery = defineQueryOptions((installId: string) => ({
  key: [...keys.installs, installId, "languages"],
  query: () => workspace.ListLanguages(installId),
}));
export const fileQuery = defineQueryOptions((path: string) => ({
  key: keys.file(path),
  query: () => workspace.Read(path),
}));
export const folderQuery = defineQueryOptions((dir: string) => ({
  key: keys.folder(dir),
  query: () => workspace.ListFolder(dir),
}));
export const rootsQuery = defineQueryOptions({ key: keys.roots, query: () => workspace.ListRoots() });
export const placeQuery = defineQueryOptions((path: string) => ({
  key: keys.place(path),
  query: () => workspace.Locate(path),
}));
export const headerQuery = defineQueryOptions((path: string) => ({
  key: [...keys.file(path), "header"],
  query: () => workspace.ReadHeader(path),
}));
/** ddsPicturesQuery lists the pictures Save as DDS saves for a picture or a folder, with the DDS each reuses. */
export const ddsPicturesQuery = defineQueryOptions((path: string) => ({
  key: [...keys.file(path), "dds-pictures"],
  query: () => draw.FindPictureFiles(path),
}));
export const copiesQuery = defineQueryOptions((path: string) => ({
  key: [...keys.file(path), "copies"],
  query: () => workspace.ListCopies(path),
}));
export const modFolderQuery = defineQueryOptions(
  ({ installId, dir, name }: { installId: string; dir: string; name: string }) => ({
    key: ["workspace", "mod-folder", installId, dir, name],
    query: () => workspace.ResolveModFolder(installId, dir, name),
  }),
);
export const modCheckQuery = defineQueryOptions(({ installId, dir }: { installId: string; dir: string }) => ({
  key: ["workspace", "mod-check", installId, dir],
  query: () => workspace.CheckModFolder(installId, dir),
}));
export const precedenceQuery = defineQueryOptions(
  ({ gameId, database, folder }: { gameId: string; database: string; folder: string }) => ({
    key: ["workspace", "precedence", gameId, database, folder],
    query: () => workspace.DescribePrecedence(gameId, database, folder),
  }),
);

// the Visual view
export const drawablesQuery = defineQueryOptions((path: string) => ({
  key: [...keys.draw(path), "drawables"],
  query: () => draw.ListDrawables(path),
}));
export const windowQuery = defineQueryOptions(({ path, name }: { path: string; name: string }) => ({
  key: [...keys.draw(path), "window", name],
  query: () => draw.ResolveWindow(path, name),
}));
// a new pick shows the last answer until its own arrives, so the view never blanks
export const statesQuery = defineQueryOptions(
  ({ path, name, picks }: { path: string; name: string; picks: Record<string, string> }) => ({
    key: [...keys.draw(path), "states", name, picks],
    query: () => draw.EvaluateStates(path, name, picks),
    placeholderData: (previous: States | undefined) => previous,
  }),
);
export const coatQuery = defineQueryOptions(
  ({ path, name, picks }: { path: string; name: string; picks: Record<string, string> }) => ({
    key: [...keys.draw(path), "coat", name, picks],
    query: () => draw.ResolveCoat(path, name, picks),
    placeholderData: (previous: Coat | undefined) => previous,
  }),
);
export const colorsQuery = defineQueryOptions({ key: [...keys.draws, "colors"], query: () => draw.ListColors() });
export const texturesQuery = defineQueryOptions((role: string) => ({
  key: [...keys.draws, "textures", role],
  query: () => draw.ListTextures(role),
}));
export const framesQuery = defineQueryOptions({ key: [...keys.draws, "frames"], query: () => draw.ListFrames() });
export const picturesQuery = defineQueryOptions(({ path, list }: { path: string; list: PictureList }) => ({
  key: [...keys.draws, "pictures", path, list.database, list.name, list.type, list.field, list.argument, list.folder],
  query: () => draw.ListPictures(path, list),
}));

// Health
export const reportQuery = defineQueryOptions((order: string[]) => ({
  key: [...keys.health, "report", order],
  query: () => health.BuildReport(order),
}));
export const codesQuery = defineQueryOptions({ key: [...keys.health, "codes"], query: () => health.ListCodes() });
export const levelsQuery = defineQueryOptions({ key: [...keys.health, "levels"], query: () => health.ListLevels() });
export const topicsQuery = defineQueryOptions({ key: [...keys.health, "topics"], query: () => health.ListTopics() });
export const overviewQuery = defineQueryOptions({
  key: [...keys.health, "overview"],
  query: () => health.ListOverview(),
});
export const errorLogQuery = defineQueryOptions({
  key: [...keys.health, "errorlog"],
  query: () => health.ListErrorLog(),
});
export const comparedQuery = defineQueryOptions(({ left, right }: { left: Site; right: Site }) => ({
  key: [...keys.health, "compared", left, right],
  query: () => health.CompareStatements(left, right),
}));
export const snippetsQuery = defineQueryOptions((sites: Site[]) => ({
  key: [...keys.health, "snippets", sites],
  query: () => health.ListSnippets(sites),
}));
export const readersQuery = defineQueryOptions((key: string) => ({
  key: [...keys.health, "readers", key],
  query: () => health.ListReaders(key),
}));

// the editor
export const legendQuery = defineQueryOptions({ key: [...keys.lsp, "legend"], query: () => lsp.ListLegend() });
export const indentationQuery = defineQueryOptions((path: string) => ({
  key: [...keys.file(path), "indentation"],
  query: () => lsp.DescribeIndentation(path),
}));
// the outline shares the Visual view's key, so it is read again each time the editor sends Go the text
export const outlineQuery = defineQueryOptions((path: string) => ({
  key: [...keys.draw(path), "outline"],
  query: () => lsp.ListSymbols(path),
}));
export const symbolsQuery = defineQueryOptions((query: string) => ({
  key: [...keys.lsp, "symbols", query],
  query: () => lsp.FindSymbols(query, 50),
}));

// the Event Graph
export const graphQuery = defineQueryOptions(({ root, filter }: { root: string; filter: Filter }) => ({
  key: [...keys.graph, root, filter],
  query: () => graph.BuildGraph(root, filter),
}));
export const detailQuery = defineQueryOptions((id: string) => ({
  key: [...keys.graph, "detail", id],
  query: () => graph.BuildDetail(id),
}));
export const graphRootsQuery = defineQueryOptions((query: string) => ({
  key: [...keys.graph, "roots", query],
  query: () => graph.FindRoots(query, 50),
}));
export const ownRootsQuery = defineQueryOptions({
  key: [...keys.graph, "own"],
  query: () => graph.ListOwnRoots(),
});
export const graphDefaultsQuery = defineQueryOptions({
  key: [...keys.graph, "defaults"],
  query: () => graph.Defaults(),
});

// search
export const fileSearchQuery = defineQueryOptions(({ query, limit }: { query: string; limit: number }) => ({
  key: [...keys.search, "files", query, limit],
  query: () => search.SearchFiles(query, limit),
}));

// the wiki
export const wikiPagesQuery = defineQueryOptions((path: string) => ({
  key: [...keys.wiki, "pages", path],
  query: () => wiki.FindPages(path),
}));
export const wikiPageQuery = defineQueryOptions((title: string) => ({
  key: [...keys.wiki, "page", title],
  query: () => wiki.ReadPage(title),
}));
export const wikiSearchQuery = defineQueryOptions((term: string) => ({
  key: [...keys.wiki, "search", term],
  query: () => wiki.SearchPages(term),
}));
export const notesQuery = defineQueryOptions({ key: [...keys.wiki, "notes"], query: () => wiki.ListNotes() });
export const lexiconQuery = defineQueryOptions((cls: string) => ({
  key: [...keys.wiki, "lexicon", cls],
  query: () => wiki.BuildLexicon(cls),
}));
export const classesQuery = defineQueryOptions({ key: [...keys.wiki, "classes"], query: () => wiki.ListClasses() });

// git
export const gitInstalledQuery = defineQueryOptions({
  key: [...keys.gits, "installed"],
  query: () => git.IsInstalled(),
});
export const repositoriesQuery = defineQueryOptions({
  key: [...keys.gits, "repositories"],
  query: () => git.ListRepositories(),
});
export const gitStatusQuery = defineQueryOptions((dir: string) => ({
  key: keys.gitStatus(dir),
  query: () => git.ReadStatus(dir),
}));
export const refsQuery = defineQueryOptions((dir: string) => ({
  key: [...keys.git(dir), "refs"],
  query: () => git.ListRefs(dir),
}));
export const commitsQuery = defineQueryOptions(
  ({ dir, refs, limit }: { dir: string; refs: string[]; limit: number }) => ({
    key: [...keys.git(dir), "commits", refs, limit],
    query: () => git.ListCommits(dir, limit, refs),
  }),
);
export const commitFilesQuery = defineQueryOptions(({ dir, hash }: { dir: string; hash: string }) => ({
  key: [...keys.git(dir), "commit", hash],
  query: () => git.ListCommitFiles(dir, hash),
}));
/** repositoryWorkQuery reads the status and the last commit of each of the given repositories, in their order. */
export const repositoryWorkQuery = defineQueryOptions((roots: string[]) => ({
  key: [...keys.repositoryWork, roots],
  query: () =>
    Promise.all(
      roots.map(async (dir) => ({
        status: await loadQuery(gitStatusQuery(dir)),
        commit: (await loadQuery(commitsQuery({ dir, refs: [], limit: 1 })))?.[0],
      })),
    ),
}));
export const gitFileQuery = defineQueryOptions(
  ({ dir, revision, path }: { dir: string; revision: string; path: string }) => ({
    key: [...keys.git(dir), "file", revision, path],
    query: () => git.ReadFile(dir, revision, path),
  }),
);

// the terminal
export const shellsQuery = defineQueryOptions({ key: keys.shells, query: () => terminal.ListShells() });
export const programsQuery = defineQueryOptions({
  key: ["terminal", "programs"],
  query: () => terminal.ListPrograms(),
});
export const environmentQuery = defineQueryOptions({
  key: ["terminal", "environment"],
  query: () => terminal.ListEnvironment(),
});

// the agents and PMT's tools for them; the access is the server's address and token
export const agentsQuery = defineQueryOptions({ key: [...keys.agents, "list"], query: () => agent.ListAgents() });
export const nodeQuery = defineQueryOptions({ key: [...keys.agents, "node"], query: () => agent.DescribeNode() });
export const tasksQuery = defineQueryOptions({ key: ["mcp", "tasks"], query: () => mcp.ListTasks() });
export const accessQuery = defineQueryOptions({ key: ["mcp", "access"], query: () => mcp.ResolveAccess() });

// Publish. A listing's key is "" for the mod's own listing.
export const listingsQuery = defineQueryOptions((modId: string) => ({
  key: [...keys.release(modId), "listings"],
  query: () => steam.ListListings(modId),
}));
export const listingQuery = defineQueryOptions(({ modId, key }: { modId: string; key: string }) => ({
  key: [...keys.release(modId), "listing", key],
  query: () => steam.ReadListing(modId, key),
}));
/** ownListingsQuery reads the mod's own listing of each of the given mods, in their order. */
export const ownListingsQuery = defineQueryOptions((modIds: string[]) => ({
  key: [...keys.ownListings, modIds],
  query: () => Promise.all(modIds.map((modId) => steam.ReadListing(modId, ""))),
}));
export const uploadQuery = defineQueryOptions(({ modId, key }: { modId: string; key: string }) => ({
  key: [...keys.release(modId), "upload", key],
  query: () => steam.ListUpload(modId, key),
}));
export const ignoreQuery = defineQueryOptions((modId: string) => ({
  key: [...keys.release(modId), "ignore"],
  query: () => steam.ReadIgnore(modId),
}));
export const comparisonQuery = defineQueryOptions(({ modId, key }: { modId: string; key: string }) => ({
  key: [...keys.release(modId), "comparison", key],
  query: () => steam.CompareUpload(modId, key),
}));
/** useComparison returns a mutation that asks Steam again about a listing's item and caches the answer. */
export function useComparison() {
  const cache = useQueryCache();
  return useMutation({
    mutation: ({ modId, key }: { modId: string; key: string }) => steam.FetchItem(modId, key),
    onSuccess: (comparison, variables) => cache.setQueryData(comparisonQuery(variables).key, comparison),
  });
}
export const comparedFileQuery = defineQueryOptions(
  ({ modId, key, rel }: { modId: string; key: string; rel: string }) => ({
    key: [...keys.release(modId), "file", key, rel],
    query: () => steam.CompareFile(modId, key, rel),
  }),
);
export const changeNotesQuery = defineQueryOptions(({ modId, key }: { modId: string; key: string }) => ({
  key: [...keys.release(modId), "change-notes", key],
  query: () => steam.ListChangeNotes(modId, key),
}));
export const dlcQuery = defineQueryOptions({ key: [...keys.releases, "dlc"], query: () => steam.ListDLC() });
export const tagsQuery = defineQueryOptions({ key: [...keys.releases, "tags"], query: () => steam.ListTags() });
export const rulesQuery = defineQueryOptions({ key: [...keys.releases, "rules"], query: () => steam.ListRules() });
export const steamStatusQuery = defineQueryOptions({
  key: [...keys.releases, "steam"],
  query: () => steam.DescribeSteam(),
});
export const htmlQuery = defineQueryOptions((bbcode: string) => ({
  key: [...keys.releases, "html", bbcode],
  query: () => steam.ToHTML(bbcode),
}));
export const bbcodeQuery = defineQueryOptions((html: string) => ({
  key: [...keys.releases, "bbcode", html],
  query: () => steam.ToBBCode(html),
}));
