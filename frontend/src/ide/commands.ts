/**
 * High-level IDE commands used by Nuxt pages (open a file, open Event Graph).
 */
import * as monaco from "monaco-editor";
import * as vscode from "vscode";
import router from "../router";
import { isWorkbenchReady, whenWorkbenchReady } from "./workbenchHost";

/** Open a file in the workbench at an optional 1-based line. */
export async function openFile(path: string, line?: number): Promise<void> {
  await whenWorkbenchReady();
  if (!isWorkbenchReady()) return;
  const uri = monaco.Uri.file(path);
  const doc = await vscode.workspace.openTextDocument(uri);
  const editor = await vscode.window.showTextDocument(doc, { preview: false });
  if (line && line > 0) {
    const pos = new vscode.Position(line - 1, 0);
    editor.selection = new vscode.Selection(pos, pos);
    editor.revealRange(
      new vscode.Range(pos, pos),
      vscode.TextEditorRevealType.InCenter,
    );
  }
}

/** Route to Event Graph with this definition as its root. */
export async function openEventGraph(
  workspaceId: string,
  eventId: string,
): Promise<void> {
  if (!workspaceId || !eventId) return;
  await router.push({
    name: "event-graph",
    params: { id: workspaceId },
    query: { root: eventId },
  });
}
