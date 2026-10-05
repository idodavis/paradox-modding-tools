/**
 * Explorer root rows for the game and each mod: the origin's color and thumbnail, by injected CSS.
 */
import { watch } from "vue";
import * as vscode from "vscode";
import { ReadFileBase64 } from "@services/fileservice";
import { normFs, type IdeRoot } from "./fsBridge";
import { useWorkspaceStore } from "../stores/workspace";
import { imageMime } from "../queries";
import iconCk3 from "@assets/Icon_CK3.png?url";
import iconEu5 from "@assets/Icon_EUV.png?url";
import iconVic3 from "@assets/Icon_Vic3.png?url";

const GAME_ICONS: Record<string, string> = {
  ck3: iconCk3,
  eu5: iconEu5,
  vic3: iconVic3,
};

let thumbStyle: HTMLStyleElement | null = null;

let roots: IdeRoot[] = [];
let registered = false;
const changeEmitter = new vscode.EventEmitter<vscode.Uri | vscode.Uri[] | undefined>();

/** Tooltip for the decoration color tag. */
function tooltipFor(root: IdeRoot): string {
  switch (root.kind) {
    case "game":
      return "Game root";
    case "mod":
      return `Mod: ${root.label}`;
    default:
      return root.label;
  }
}

/** FileDecorationProvider for workspace root folders only. */
class RootDecorationProvider implements vscode.FileDecorationProvider {
  readonly onDidChangeFileDecorations = changeEmitter.event;

  provideFileDecoration(
    uri: vscode.Uri,
  ): vscode.ProviderResult<vscode.FileDecoration> {
    const path = normFs(uri.fsPath || uri.path);
    const root = roots.find((r) => normFs(r.path) === path);
    if (!root) return undefined;
    const deco = new vscode.FileDecoration(undefined, tooltipFor(root));
    deco.propagate = false;
    return deco;
  }
}

/** Register the decoration provider (once), after workbench services are ready. */
export function registerRootDecorations(): vscode.Disposable {
  if (registered) {
    return { dispose() {} };
  }
  registered = true;
  // a mod's slot or picked color changes on the Settings pages; the roots follow
  const store = useWorkspaceStore();
  watch(() => [store.workspaceMods, store.activeWorkspace], repaintDecoratedRoots);
  const provider = new RootDecorationProvider();
  return vscode.window.registerFileDecorationProvider(provider);
}

/** CSS selector for an explorer root row by aria-label prefix. */
function explorerRootSel(label: string): string {
  return (
    ".monaco-workbench .explorer-folders-view " +
    `.monaco-list-row[aria-level="1"][aria-label^="${CSS.escape(label)}"]`
  );
}

function iconRule(label: string, url: string): string {
  const sel = `${explorerRootSel(label)} .monaco-icon-label::before`;
  return `${sel} { background-image: url("${url}") !important; }`;
}

function dotRule(label: string, color: string): string {
  const row = explorerRootSel(label);
  return [
    `${row}::after { background: ${color}; }`,
    `${row} .monaco-icon-name-container { color: ${color} !important; }`,
  ].join("\n");
}

async function paintRootThumbs(next: IdeRoot[]): Promise<void> {
  if (!thumbStyle) {
    thumbStyle = document.createElement("style");
    thumbStyle.id = "pmt-root-thumbs";
    document.head.appendChild(thumbStyle);
  }
  const store = useWorkspaceStore();
  const gameUrl = GAME_ICONS[store.activeWorkspace?.gameId ?? ""];
  const rules: string[] = [];
  for (const r of next) {
    rules.push(dotRule(r.label, store.originColor(r.origin ?? "")));
    if (r.kind === "game" && gameUrl) {
      rules.push(iconRule(r.label, gameUrl));
      continue;
    }
    if (r.kind !== "mod" || !r.thumbnail) continue;
    try {
      const file = await ReadFileBase64(r.thumbnail);
      if (!file?.exists || !file.b64) continue;
      rules.push(iconRule(r.label, `data:${imageMime(r.thumbnail)};base64,${file.b64}`));
    } catch {
      /* skip */
    }
  }
  thumbStyle.textContent = rules.join("\n");
}

/** Update roots used for decorations (call when workspace folders change). */
export function setDecoratedRoots(next: IdeRoot[]): void {
  roots = next;
  const uris = next.map((r) => vscode.Uri.file(r.path));
  changeEmitter.fire(uris.length ? uris : undefined);
  void paintRootThumbs(next);
}

/** Re-paint explorer accents without remounting folders. */
export function repaintDecoratedRoots(): void {
  if (roots.length) setDecoratedRoots(roots);
}
