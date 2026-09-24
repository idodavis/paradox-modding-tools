/**
 * Workbench Find-in-Files through Go's search.
 */
import { CancelError, Events, type CancellablePromise } from "@wailsio/runtime";
import { getService } from "@codingame/monaco-vscode-api";
import { Schemas } from "@codingame/monaco-vscode-api/vscode/vs/base/common/network";
import { URI } from "@codingame/monaco-vscode-api/vscode/vs/base/common/uri";
import { Range } from "@codingame/monaco-vscode-api/vscode/vs/editor/common/core/range";
import {
  FileMatch,
  SearchCompletionExitCode,
  SearchProviderType,
  TextSearchMatch,
  type IFileQuery,
  type IFolderQuery,
  type ISearchComplete,
  type ISearchProgressItem,
  type ISearchResultProvider,
  type ITextQuery,
} from "@codingame/monaco-vscode-api/vscode/vs/workbench/services/search/common/search";
import { ISearchService } from "@codingame/monaco-vscode-api/vscode/vs/workbench/services/search/common/search.service";
import { FileSearch, TextSearch } from "@services/searchservice";
import type { TextSearchFolder } from "@services/models";
import type { CancellationToken } from "vscode";

/** True-keys from a VS Code glob expression (same as resolvePatternsForProvider). */
function resolvePatternsForProvider(
  global: Record<string, unknown> | undefined,
  folder: Record<string, unknown> | undefined,
): string[] {
  const merged = { ...(global ?? {}), ...(folder ?? {}) };
  return Object.keys(merged).filter((key) => merged[key] !== false);
}

function folderDto(
  fq: IFolderQuery,
  extraInc: Record<string, unknown> | undefined,
  extraExc: Record<string, unknown> | undefined,
): TextSearchFolder {
  const folderExc: Record<string, unknown> = {};
  for (const ex of fq.excludePattern ?? []) {
    Object.assign(folderExc, ex.pattern ?? {});
  }
  return {
    path: fq.folder.fsPath,
    includes: resolvePatternsForProvider(
      extraInc,
      fq.includePattern as Record<string, unknown> | undefined,
    ),
    excludes: resolvePatternsForProvider(extraExc, folderExc),
    disregardIgnoreFiles: !!fq.disregardIgnoreFiles,
  };
}

function foldersFrom(
  q: {
    folderQueries: IFolderQuery[];
    includePattern?: Record<string, unknown>;
    excludePattern?: Record<string, unknown>;
  },
): TextSearchFolder[] {
  return (q.folderQueries ?? []).map((fq) =>
    folderDto(fq, q.includePattern, q.excludePattern),
  );
}

function emptyComplete(): ISearchComplete {
  return {
    results: [],
    limitHit: false,
    messages: [],
    stats: { type: "textSearchProvider" },
    exit: SearchCompletionExitCode.Normal,
  };
}

function fileMatchesFromHits(
  hits: { path: string; line: number; col: number; endCol: number; preview: string }[],
): FileMatch[] {
  const byFile = new Map<string, FileMatch>();
  const results: FileMatch[] = [];
  for (const hit of hits) {
    let fm = byFile.get(hit.path);
    if (!fm) {
      fm = new FileMatch(URI.file(hit.path));
      fm.results = [];
      byFile.set(hit.path, fm);
      results.push(fm);
    }
    const range = new Range(hit.line, hit.col, hit.line, hit.endCol);
    fm.results!.push(new TextSearchMatch(hit.preview, range));
  }
  return results;
}

/** Forward Monaco cancellation to a Wails CancellablePromise. */
async function awaitCancellable<T>(
  promise: CancellablePromise<T>,
  token?: CancellationToken,
): Promise<T | undefined> {
  if (token?.isCancellationRequested) {
    void promise.cancel();
    return undefined;
  }
  const sub = token?.onCancellationRequested(() => {
    void promise.cancel();
  });
  try {
    return await promise;
  } catch (err) {
    if (err instanceof CancelError || token?.isCancellationRequested) {
      return undefined;
    }
    throw err;
  } finally {
    sub?.dispose();
  }
}

/** Each text search's id, which its hits come back under. */
let searches = 0;

const provider: ISearchResultProvider = {
  async getAIName() {
    return undefined;
  },
  async clearCache() {
    return;
  },
  async textSearch(query: ITextQuery, onProgress, token?: CancellationToken) {
    if (token?.isCancellationRequested) return emptyComplete();
    const pattern = query.contentPattern?.pattern ?? "";
    if (!pattern) return emptyComplete();
    // each file's hits arrive as Go finds them and show at once; the reply says how many were sent,
    // and since events can land after it, the search ends when that many are in. A replaced
    // search's late hits are dropped: the view adds progress to whichever search is current
    const id = String(++searches);
    const results: FileMatch[] = [];
    let received = 0;
    let expected = Infinity;
    let arrived = () => {};
    const off = Events.On("search:hits", (event) => {
      if (event.data.id !== id || token?.isCancellationRequested) return;
      const part = fileMatchesFromHits(event.data.hits ?? []);
      for (const fm of part) onProgress?.(fm as ISearchProgressItem);
      results.push(...part);
      received += event.data.hits?.length ?? 0;
      if (received >= expected) arrived();
    });
    try {
      const res = await awaitCancellable(
        TextSearch({
          id,
          pattern,
          isRegexp: !!query.contentPattern.isRegExp,
          isCaseSensitive: !!query.contentPattern.isCaseSensitive,
          isWordMatch: !!query.contentPattern.isWordMatch,
          maxResults: query.maxResults ?? 0,
          folders: foldersFrom(query),
        }),
        token,
      );
      if (!res || token?.isCancellationRequested) return emptyComplete();
      if (received < res.found) {
        await new Promise<void>((resolve) => {
          expected = res.found;
          arrived = resolve;
          token?.onCancellationRequested(() => resolve());
        });
      }
      return { ...emptyComplete(), results, limitHit: res.limitHit };
    } finally {
      off();
    }
  },
  async fileSearch(query: IFileQuery, token?: CancellationToken) {
    if (token?.isCancellationRequested) return emptyComplete();
    const res = await awaitCancellable(
      FileSearch({
        filePattern: query.filePattern ?? "",
        maxResults: query.maxResults ?? 0,
        folders: foldersFrom(query),
      }),
      token,
    );
    if (!res || token?.isCancellationRequested) return emptyComplete();
    const results = (res.hits ?? []).map((h) => new FileMatch(URI.file(h.path)));
    return { ...emptyComplete(), results, limitHit: !!res.limitHit };
  },
};

let registered = false;

/** Replace the JS WorkspaceSearchProvider with Go workspace search for scheme `file`. */
export async function registerWorkspaceSearch(): Promise<void> {
  if (registered) return;
  registered = true;
  const search = await getService(ISearchService);
  search.registerSearchResultProvider(Schemas.file, SearchProviderType.file, provider);
  search.registerSearchResultProvider(Schemas.file, SearchProviderType.text, provider);
}
