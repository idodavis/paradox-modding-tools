/**
 * The IDE's palette commands, registered into the commands store when the editor installs: the compares, Open the
 * event under the cursor, Format, Save, the splits, Find in files and the Wiki pane.
 */
import type { RouteLocationRaw, Router } from "vue-router";
import { Hover } from "@services/ideservice";
import { useCompare } from "../composables/useCompare";
import { useIdeRoots } from "../composables/useIdeRoots";
import { useCommandsStore, type CommandItem } from "../stores/commands";
import { useEditorStore } from "../stores/editor";
import { useGitStore } from "../stores/git";
import { useWorkspaceStore } from "../stores/workspace";
import { runAction } from "./editor";
import { diffTab } from "./tabs";

/** Register the IDE's commands: those that need a file are listed only while one is shown. */
export function registerIdeCommands(router: Router): void {
  const palette = useCommandsStore();
  const editor = useEditorStore();
  const workspace = useWorkspaceStore();
  const git = useGitStore();
  const { locate } = useIdeRoots(() => workspace.activeWorkspaceId);
  const compare = useCompare(() => workspace.activeWorkspaceId);

  // every command closes the palette before it runs
  const run = (fn: () => unknown) => () => {
    palette.paletteOpen = false;
    void fn();
  };
  const go = (to: RouteLocationRaw) => {
    palette.paletteOpen = false;
    void router.push(to);
  };

  /** The event under the caret, in the Event Graph. */
  async function graphAtCaret(): Promise<void> {
    const card = await Hover(
      workspace.activeWorkspaceId,
      editor.activeFile,
      editor.caret?.line ?? 0,
      editor.caret?.col ?? 0,
    );
    if (card?.graphed)
      go({ name: "event-graph", params: { id: workspace.activeWorkspaceId }, query: { root: card.word } });
  }

  function compares(): CommandItem[] {
    const active = editor.activeFile;
    const [first, second] = editor.selected;
    const picked = editor.comparing;
    const item = (label: string, onSelect: () => void, icon = "i-lucide-diff", disabled = false): CommandItem => ({
      label,
      prefix: "Compare:",
      icon,
      disabled,
      onSelect,
    });
    return [
      ...(active
        ? [
            item(
              "Select the active file for compare",
              run(() => (editor.comparing = active)),
            ),
            ...(picked && picked !== active
              ? [
                  item(
                    "Active file with the file selected",
                    run(() => compare.files(picked, active)),
                  ),
                ]
              : []),
            ...(git.repoOf(active)
              ? [
                  item(
                    "Active file with the last commit",
                    run(() => editor.open(diffTab("changes", active))),
                    "i-lucide-git-compare",
                  ),
                ]
              : []),
            item(
              "Active file with the game's copy",
              run(() => compare.withGame(active)),
              "i-lucide-diff",
              !!locate(active)?.root.readOnly,
            ),
          ]
        : []),
      ...(first && second
        ? [
            item(
              "Two files selected in the explorer",
              run(() => compare.files(first, second)),
            ),
          ]
        : []),
    ];
  }

  function fileCommands(): CommandItem[] {
    const active = editor.activeFile;
    if (!active) return [];
    const item = (label: string, icon: string, onSelect: () => void, kbds?: string[]): CommandItem => ({
      label,
      prefix: "IDE:",
      icon,
      kbds,
      onSelect,
    });
    return [
      {
        label: "Open the event under the cursor",
        prefix: "Event Graph:",
        icon: "i-lucide-git-branch",
        kbds: ["Ctrl Shift G"],
        onSelect: run(graphAtCaret),
      },
      item(
        "Format document",
        "i-lucide-align-left",
        run(() => runAction("editor.action.formatDocument")),
        ["Shift Alt F"],
      ),
      item(
        "Save",
        "i-lucide-save",
        run(() => editor.save([active])),
        ["Ctrl S"],
      ),
      item(
        "Save all",
        "i-lucide-save-all",
        run(() => editor.save()),
      ),
      item(
        "Split right",
        "i-lucide-columns-2",
        run(() => editor.split("right")),
        ["Ctrl \\"],
      ),
      item(
        "Split down",
        "i-lucide-rows-2",
        run(() => editor.split("down")),
      ),
      item(
        "Move the editor into the next group",
        "i-lucide-arrow-right-to-line",
        run(() => editor.moveTabToNextGroup()),
      ),
    ];
  }

  palette.register("ide", () => {
    const id = workspace.activeWorkspaceId;
    if (!id) return [];
    return [
      ...compares(),
      ...fileCommands(),
      {
        label: "Find in files",
        prefix: "IDE:",
        icon: "i-lucide-search",
        kbds: ["Ctrl Shift F"],
        onSelect: () => {
          editor.sidebar = "search";
          go({ name: "workspace-ide", params: { id } });
        },
      },
      {
        label: editor.wikiOpen ? "Hide the Wiki pane" : "Show the Wiki pane",
        prefix: "IDE:",
        icon: "i-lucide-book-open",
        kbds: ["Ctrl Shift W"],
        onSelect: run(() => (editor.wikiOpen = !editor.wikiOpen)),
      },
    ];
  });
}
