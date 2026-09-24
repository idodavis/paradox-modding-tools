/**
 * Wails-backed VS Code file system provider for the embedded workbench.
 */
import { Events } from "@wailsio/runtime";
import {
  FileType,
  FileChangeType,
  FilePermission,
  FileSystemProviderCapabilities,
  FileSystemProviderError,
  FileSystemProviderErrorCode,
  type IFileSystemProviderWithFileReadWriteCapability,
  type IStat,
  type IWatchOptions,
  type IFileWriteOptions,
  type IFileDeleteOptions,
  type IFileOverwriteOptions,
  type IFileChange,
} from "@codingame/monaco-vscode-files-service-override";
import * as monaco from "monaco-editor";
import {
  StatPath,
  ListDirectory,
  ReadEditableBase64,
  WriteFileBase64,
  CreateDir,
  DeletePath,
  RenamePath,
} from "@services/fileservice";
import type { IdeRoot } from "@services/models";

type Uri = monaco.Uri;

export type { IdeRoot };

let listShowAll = false;

/** Show-all lists dotfiles; hide binaries still lists .metadata. */
export function setListShowAll(show: boolean): void {
  listShowAll = show;
}

/** Role of a multi-root IDE folder (explorer color tags). */
export type IdeRootKind = "game" | "mod";

type Disposable = { dispose(): void };

function fsPath(resource: Uri): string {
  return toOsPath(resource.fsPath || resource.path);
}

/** Strip a monaco `/C:` prefix; use backslash on Windows drive paths. */
function toOsPath(p: string): string {
  let s = p;
  if (s.startsWith("/") && /^\/[A-Za-z]:/.test(s)) s = s.slice(1);
  if (/^[A-Za-z]:/.test(s) || s.includes("\\")) {
    return s.replace(/\//g, "\\");
  }
  return s;
}

/** Decode standard base64 to bytes without a per-char loop. */
function bytesFromB64(b64: string): Uint8Array {
  const bin = atob(b64);
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

/** Encode bytes as standard base64 in chunks (avoids call-stack limits). */
function b64FromBytes(data: Uint8Array): string {
  const chunk = 0x8000;
  const parts: string[] = [];
  for (let i = 0; i < data.length; i += chunk) {
    parts.push(String.fromCharCode(...data.subarray(i, i + chunk)));
  }
  return btoa(parts.join(""));
}

/** Normalize a filesystem path for root-identity comparison. */
export function normFs(p: string): string {
  let s = p.replace(/\\/g, "/");
  if (s.startsWith("/") && /^\/[A-Za-z]:/.test(s)) s = s.slice(1);
  return s.replace(/\/+$/, "").toLowerCase();
}

function underRoot(filePath: string, root: string): boolean {
  const a = normFs(filePath);
  const b = normFs(root);
  return a === b || a.startsWith(b + "/");
}

type ModRootDeletedFn = (originId: string) => void | Promise<void>;

let modRootDeleted: ModRootDeletedFn | undefined;

/** Called after a successful disk delete of a mounted mod root. */
export function setModRootDeletedHook(fn: ModRootDeletedFn | undefined): void {
  modRootDeleted = fn;
}

/** Minimal event emitter matching VS Code Event shape. */
class SimpleEmitter<T> {
  private listeners = new Set<(e: T) => void>();
  readonly event = (listener: (e: T) => void): Disposable => {
    this.listeners.add(listener);
    return {
      dispose: () => {
        this.listeners.delete(listener);
      },
    };
  };
  fire(e: T): void {
    for (const l of this.listeners) l(e);
  }
}

/** True only where the host filesystem really is case-sensitive. */
export function caseSensitiveFs(): boolean {
  const ua = navigator.userAgent;
  return ua.includes("Linux") && !ua.includes("Android");
}

/** File system provider bridging workbench URIs to Go FileService. */
export class WailsFileSystemProvider
  implements IFileSystemProviderWithFileReadWriteCapability
{
  // only Linux folds no case; the URI service never asks (see alignFsCaseSensitivity)
  readonly capabilities =
    FileSystemProviderCapabilities.FileReadWrite |
    (caseSensitiveFs() ? FileSystemProviderCapabilities.PathCaseSensitive : 0);

  private readonly _onDidChangeCapabilities = new SimpleEmitter<void>();
  readonly onDidChangeCapabilities = this._onDidChangeCapabilities.event;

  private readonly _onDidChangeFile = new SimpleEmitter<readonly IFileChange[]>();
  readonly onDidChangeFile = this._onDidChangeFile.event;

  private roots: IdeRoot[] = [];

  constructor() {
    Events.On("fs:changed", (ev: { data?: unknown }) => {
      this.onExternalChange(ev.data);
    });
  }

  /**
   * Apply a session-watcher event to the workbench file tree. A path still there is told as
   * ADDED: the explorer lists a new file only for ADDED, and an open editor reloads on ADDED as
   * on UPDATED.
   */
  private onExternalChange(data: unknown): void {
    if (!data || typeof data !== "object") return;
    const row = data as { path?: string; deleted?: boolean };
    if (!row.path) return;
    this._onDidChangeFile.fire([
      {
        resource: monaco.Uri.file(row.path),
        type: row.deleted ? FileChangeType.DELETED : FileChangeType.ADDED,
      },
    ]);
  }

  /** Update multi-root mount points and read-only game roots. */
  setRoots(roots: IdeRoot[]): void {
    this.roots = roots.map((r) => ({ ...r }));
    if (!this.roots.length) return;
    this._onDidChangeFile.fire(
      this.roots.map((r) => ({
        resource: monaco.Uri.file(r.path),
        type: FileChangeType.UPDATED,
      })),
    );
  }

  /** True when the path is under a read-only (game) root. */
  isReadOnly(filePath: string): boolean {
    return this.roots.some((r) => r.readOnly && underRoot(filePath, r.path));
  }

  /** True when path is under a mounted IDE root (or no roots yet). */
  private manages(path: string): boolean {
    if (!this.roots.length) return false;
    return this.roots.some((r) => underRoot(path, r.path));
  }

  private assertWritable(path: string): void {
    for (const r of this.roots) {
      if (r.readOnly && underRoot(path, r.path)) {
        throw FileSystemProviderError.create(
          "Game install files are read-only",
          FileSystemProviderErrorCode.NoPermissions,
        );
      }
    }
  }

  private notFound(): never {
    throw FileSystemProviderError.create(
      "File not found",
      FileSystemProviderErrorCode.FileNotFound,
    );
  }

  watch(_resource: Uri, _opts: IWatchOptions): Disposable {
    return { dispose() {} };
  }

  async stat(resource: Uri): Promise<IStat> {
    const path = fsPath(resource);
    // Let memory/other overlays own non-root paths (e.g. /pmt.code-workspace).
    if (!this.manages(path)) this.notFound();
    const st = await StatPath(path);
    if (!st.exists) this.notFound();
    return {
      type: st.isDir ? FileType.Directory : FileType.File,
      ctime: st.ctimeMs,
      mtime: st.mtimeMs,
      size: st.size,
      permissions: this.isReadOnly(path) ? FilePermission.Readonly : undefined,
    };
  }

  async mkdir(resource: Uri): Promise<void> {
    const path = fsPath(resource);
    if (!this.manages(path)) this.notFound();
    this.assertWritable(path);
    await CreateDir(path);
  }

  async readdir(resource: Uri): Promise<[string, FileType][]> {
    const path = fsPath(resource);
    if (!this.manages(path)) this.notFound();
    const entries = (await ListDirectory(path, listShowAll)) ?? [];
    return entries.map((e) => [
      e.name,
      e.isDir ? FileType.Directory : FileType.File,
    ]);
  }

  /** Origin id when path is exactly a kind:mod root, not a child. */
  private modRootOrigin(path: string): string | undefined {
    const key = normFs(path);
    return this.roots.find(
      (r) => r.kind === "mod" && r.origin && normFs(r.path) === key,
    )?.origin;
  }

  async delete(resource: Uri, _opts: IFileDeleteOptions): Promise<void> {
    const path = fsPath(resource);
    if (!this.manages(path)) this.notFound();
    this.assertWritable(path);
    const originId = this.modRootOrigin(path);
    await DeletePath(path);
    if (originId && modRootDeleted) await modRootDeleted(originId);
  }

  async rename(from: Uri, to: Uri, _opts: IFileOverwriteOptions): Promise<void> {
    const src = fsPath(from);
    const dst = fsPath(to);
    if (!this.manages(src) || !this.manages(dst)) this.notFound();
    this.assertWritable(src);
    this.assertWritable(dst);
    await RenamePath(src, dst);
  }

  async readFile(resource: Uri): Promise<Uint8Array> {
    const path = fsPath(resource);
    if (!this.manages(path)) this.notFound();
    const file = await ReadEditableBase64(path);
    if (!file.exists) this.notFound();
    if (file.reject) {
      throw FileSystemProviderError.create(
        file.reject,
        FileSystemProviderErrorCode.Unknown,
      );
    }
    return bytesFromB64(file.b64 ?? "");
  }

  async writeFile(
    resource: Uri,
    content: Uint8Array,
    _opts: IFileWriteOptions,
  ): Promise<void> {
    const path = fsPath(resource);
    if (!this.manages(path)) this.notFound();
    this.assertWritable(path);
    await WriteFileBase64(path, b64FromBytes(content));
  }
}
