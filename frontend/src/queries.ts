/**
 * Every read of Go, one query per RPC, and the one place query keys are defined. A table Go sends once is a query
 * no event refreshes; a query that an event or a mutation refreshes has a key factory here. Monaco's per-position
 * reads go through the providers, not here.
 */
import { defineQueryOptions } from "@pinia/colada";

import * as app from "@services/app/service";
import * as draw from "@services/draw/service";
import * as git from "@services/git/service";
import * as graph from "@services/graph/service";
import * as health from "@services/health/service";
import * as lsp from "@services/lsp/service";
import * as search from "@services/search/service";
import * as steam from "@services/steam/service";
import * as wiki from "@services/wiki/service";
import * as workspace from "@services/workspace/service";
import type { Site } from "@internal/index/models";
import type { Filter } from "@services/graph/models";

/** The keys an event or a mutation refreshes. */
/** The URL the files route serves a path at, for an image, a texture or a thumbnail. */
export function fileUrl(path: string): string {
  throw new Error("stub", { cause: path });
}

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
};

// the app
export const versionQuery = defineQueryOptions({ key: ["app", "version"], query: () => app.Version() });
export const settingsCatalogQuery = defineQueryOptions({
  key: ["app", "catalog"],
  query: () => app.ListSettings(),
});
export const settingsQuery = defineQueryOptions({ key: keys.settings, query: () => app.ReadSettings() });
export const updateQuery = defineQueryOptions({ key: ["app", "update"], query: () => app.CheckUpdate() });
export const windowsQuery = defineQueryOptions({ key: keys.windows, query: () => app.ListWindows() });

// the workspace and its files
export const gamesQuery = defineQueryOptions({
  key: ["workspace", "games"],
  query: () => workspace.ListGames(),
});
export const installsQuery = defineQueryOptions({ key: keys.installs, query: () => workspace.ListInstalls() });
export const detectedInstallsQuery = defineQueryOptions((gameId: string) => ({
  key: ["workspace", "detected", gameId],
  query: () => workspace.DetectInstalls(gameId),
}));
export const workspacesQuery = defineQueryOptions({
  key: keys.workspaces,
  query: () => workspace.ListWorkspaces(),
});
export const stateQuery = defineQueryOptions({ key: keys.state, query: () => workspace.State() });
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

// the Visual view
export const drawablesQuery = defineQueryOptions((path: string) => ({
  key: [...keys.draw(path), "drawables"],
  query: () => draw.ListDrawables(path),
}));
export const windowQuery = defineQueryOptions(({ path, name }: { path: string; name: string }) => ({
  key: [...keys.draw(path), "window", name],
  query: () => draw.ResolveWindow(path, name),
}));
export const statesQuery = defineQueryOptions(
  ({ path, name, picks }: { path: string; name: string; picks: Record<string, string> }) => ({
    key: [...keys.draw(path), "states", name, picks],
    query: () => draw.EvaluateStates(path, name, picks),
  }),
);
export const coatQuery = defineQueryOptions(
  ({ path, name, picks }: { path: string; name: string; picks: Record<string, string> }) => ({
    key: [...keys.draw(path), "coat", name, picks],
    query: () => draw.ResolveCoat(path, name, picks),
  }),
);
export const colorsQuery = defineQueryOptions({ key: ["draw", "colors"], query: () => draw.ListColors() });
export const texturesQuery = defineQueryOptions((role: string) => ({
  key: ["draw", "textures", role],
  query: () => draw.ListTextures(role),
}));
export const framesQuery = defineQueryOptions({ key: ["draw", "frames"], query: () => draw.ListFrames() });

// Health
export const reportQuery = defineQueryOptions((order: string[]) => ({
  key: [...keys.health, "report", order],
  query: () => health.BuildReport(order),
}));
export const sectionsQuery = defineQueryOptions({
  key: ["health", "sections"],
  query: () => health.ListSections(),
});
export const codesQuery = defineQueryOptions({ key: ["health", "codes"], query: () => health.ListCodes() });
export const overviewQuery = defineQueryOptions({
  key: [...keys.health, "overview"],
  query: () => health.ListOverview(),
});
export const comparedQuery = defineQueryOptions(({ a, b }: { a: Site; b: Site }) => ({
  key: [...keys.health, "compared", a, b],
  query: () => health.CompareStatements(a, b),
}));

// the editor
export const legendQuery = defineQueryOptions({ key: ["lsp", "legend"], query: () => lsp.ListLegend() });

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
export const graphDefaultsQuery = defineQueryOptions({
  key: ["graph", "defaults"],
  query: () => graph.Defaults(),
});

// search
export const fileSearchQuery = defineQueryOptions((query: string) => ({
  key: ["search", "files", query],
  query: () => search.SearchFiles(query, 50),
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
export const commitsQuery = defineQueryOptions(({ dir, refs }: { dir: string; refs: string[] }) => ({
  key: [...keys.git(dir), "commits", refs],
  query: () => git.ListCommits(dir, 200, refs),
}));
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

// Publish
export const listingQuery = defineQueryOptions((modId: string) => ({
  key: [...keys.release(modId), "listing"],
  query: () => steam.ReadListing(modId),
}));
export const uploadQuery = defineQueryOptions((modId: string) => ({
  key: [...keys.release(modId), "upload"],
  query: () => steam.ListUpload(modId),
}));
export const comparisonQuery = defineQueryOptions((modId: string) => ({
  key: [...keys.release(modId), "comparison"],
  query: () => steam.CompareUpload(modId),
}));
export const dlcQuery = defineQueryOptions({ key: ["release", "dlc"], query: () => steam.ListDLC() });
export const tagsQuery = defineQueryOptions({ key: ["release", "tags"], query: () => steam.ListTags() });
export const rulesQuery = defineQueryOptions({ key: ["release", "rules"], query: () => steam.ListRules() });
