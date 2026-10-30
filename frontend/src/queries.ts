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
import type { FindingSite } from "@services/health/models";
import type { Place } from "@services/workspace/models";

/** fileUrl returns the URL that serves a file. */
export function fileUrl(path: string): string {
  return `/files?path=${encodeURIComponent(path)}`;
}

/** loadQuery reads a query outside a component, from the cache if it is fresh. It throws if Go's read fails. */
export async function loadQuery<T>(options: UseQueryOptions<T>): Promise<T | undefined> {
  const cache = useQueryCache();
  return (await cache.refresh(cache.ensure(options))).data;
}

/** keys holds the query keys that an event or a mutation refreshes. */
export const keys = {
  settings: ["app", "settings"] as const,
  windows: ["app", "windows"] as const,
  installs: ["workspace", "installs"] as const,
  workspaces: ["workspace", "workspaces"] as const,
  state: ["workspace", "state"] as const,
  file: (path: string) => ["file", path] as const,
  folder: (dir: string) => ["folder", dir] as const,
  roots: ["roots"] as const,
  draw: (path: string) => ["draw", path] as const,
  health: ["health"] as const,
  graph: ["graph"] as const,
  wiki: ["wiki"] as const,
  git: (dir: string) => ["git", dir] as const,
  release: (modId: string) => ["release", modId] as const,
  shells: ["terminal", "shells"] as const,
  agents: ["agent"] as const,
};

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
export const windowsQuery = defineQueryOptions({ key: keys.windows, query: () => app.ListWindows() });
export const crashedQuery = defineQueryOptions({ key: ["app", "crashed"], query: () => app.CrashedLastRun() });

// the workspace and its files
export const gamesQuery = defineQueryOptions({
  key: ["workspace", "games"],
  query: () => workspace.ListGames(),
});
export const installsQuery = defineQueryOptions({ key: keys.installs, query: () => workspace.ListInstalls() });
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
export const modsQuery = defineQueryOptions({ key: [...keys.workspaces, "mods"], query: () => workspace.ListMods() });
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
  key: ["place", path],
  query: () => workspace.Locate(path),
}));
export const headerQuery = defineQueryOptions((path: string) => ({
  key: [...keys.file(path), "header"],
  query: () => workspace.ReadHeader(path),
}));
/** The pictures Save as DDS writes, a picture or those directly in a folder, and the DDS whose settings each keeps. */
export const ddsPicturesQuery = defineQueryOptions((place: Place) => ({
  key: [...keys.file(place.path), "dds-pictures"],
  query: async () => {
    const pictures = place.isDir ? ((await draw.ListPictureFiles(place.path)) ?? []) : [place];
    return { pictures, references: (await draw.FindReferences(pictures.map((picture) => picture.path))) ?? {} };
  },
}));
export const copiesQuery = defineQueryOptions((path: string) => ({
  key: [...keys.file(path), "copies"],
  query: () => workspace.ListCopies(path),
}));
export const modFolderQuery = defineQueryOptions(
  ({ workspaceId, dir, name }: { workspaceId: string; dir: string; name: string }) => ({
    key: ["workspace", "mod-folder", workspaceId, dir, name],
    query: () => workspace.ResolveModFolder(workspaceId, dir, name),
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
export const colorsQuery = defineQueryOptions({ key: ["draw", "colors"], query: () => draw.ListColors() });
export const texturesQuery = defineQueryOptions((role: string) => ({
  key: ["draw", "textures", role],
  query: () => draw.ListTextures(role),
}));
export const framesQuery = defineQueryOptions({ key: ["draw", "frames"], query: () => draw.ListFrames() });
export const picturesQuery = defineQueryOptions(({ path, list }: { path: string; list: PictureList }) => ({
  key: ["draw", "pictures", path, list.database, list.name, list.type, list.field, list.argument, list.folder],
  query: () => draw.ListPictures(path, list),
}));

// Health
export const reportQuery = defineQueryOptions((order: string[]) => ({
  key: [...keys.health, "report", order],
  query: () => health.BuildReport(order),
}));
export const codesQuery = defineQueryOptions({ key: ["health", "codes"], query: () => health.ListCodes() });
export const overviewQuery = defineQueryOptions({
  key: [...keys.health, "overview"],
  query: () => health.ListOverview(),
});
export const errorLogQuery = defineQueryOptions({
  key: [...keys.health, "errorlog"],
  query: () => health.ListErrorLog(),
});
export const comparedQuery = defineQueryOptions(({ a, b }: { a: FindingSite; b: FindingSite }) => ({
  key: [...keys.health, "compared", a, b],
  query: () => health.CompareStatements(a, b),
}));
export const snippetsQuery = defineQueryOptions((sites: FindingSite[]) => ({
  key: [...keys.health, "snippets", sites],
  query: () => health.ListSnippets(sites),
}));
export const readersQuery = defineQueryOptions((key: string) => ({
  key: [...keys.health, "readers", key],
  query: () => health.ListReaders(key),
}));

// the editor
export const legendQuery = defineQueryOptions({ key: ["lsp", "legend"], query: () => lsp.ListLegend() });
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
  key: ["lsp", "symbols", query],
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
  key: ["graph", "defaults"],
  query: () => graph.Defaults(),
});

// search
export const fileSearchQuery = defineQueryOptions(({ query, limit }: { query: string; limit: number }) => ({
  key: ["search", "files", query, limit],
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
export const noteQuery = defineQueryOptions((version: string) => ({
  key: [...keys.wiki, "note", version],
  query: () => wiki.ReadNote(version),
}));
export const lexiconQuery = defineQueryOptions((cls: string) => ({
  key: [...keys.wiki, "lexicon", cls],
  query: () => wiki.BuildLexicon(cls),
}));

// git
export const repositoryQuery = defineQueryOptions((dir: string) => ({
  key: [...keys.git(dir), "repository"],
  query: () => git.FindRepository(dir),
}));
export const gitStatusQuery = defineQueryOptions((dir: string) => ({
  key: [...keys.git(dir), "status"],
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
export const accessQuery = defineQueryOptions({
  key: ["mcp", "access"],
  query: async () => ({ url: await mcp.ReadURL(), token: await mcp.ReadToken() }),
});

// Publish. A listing's key is "" for the mod's own listing.
export const listingsQuery = defineQueryOptions((modId: string) => ({
  key: [...keys.release(modId), "listings"],
  query: () => steam.ListListings(modId),
}));
export const listingQuery = defineQueryOptions(({ modId, key }: { modId: string; key: string }) => ({
  key: [...keys.release(modId), "listing", key],
  query: () => steam.ReadListing(modId, key),
}));
export const uploadQuery = defineQueryOptions(({ modId, key }: { modId: string; key: string }) => ({
  key: [...keys.release(modId), "upload", key],
  query: () => steam.ListUpload(modId, key),
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
export const dlcQuery = defineQueryOptions({ key: ["release", "dlc"], query: () => steam.ListDLC() });
export const tagsQuery = defineQueryOptions({ key: ["release", "tags"], query: () => steam.ListTags() });
export const rulesQuery = defineQueryOptions({ key: ["release", "rules"], query: () => steam.ListRules() });
export const steamStatusQuery = defineQueryOptions({ key: ["release", "steam"], query: () => steam.DescribeSteam() });
export const htmlQuery = defineQueryOptions((bbcode: string) => ({
  key: ["release", "html", bbcode],
  query: () => steam.ToHTML(bbcode),
}));
