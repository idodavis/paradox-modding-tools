/**
 * The page catalog: each route's name, title, icon and Help text, and for a workspace's pages its tab in the top
 * bar. The router's typed route names come from here, and the Help text is the user's guide.
 */
import type { RouteRecordInfo } from "vue-router";

import { useSettingsStore } from "../stores/settings";

export type Page = { title: string; help: readonly string[]; icon?: string; workspace?: true; tab?: true };

export const PAGES = {
  library: {
    title: "Library",
    icon: "i-lucide-layout-grid",
    help: [
      "A workspace is one game install plus the mods you load with it. The active one is at the top; open another's card to make it active.",
      "Remove from PMT, in a workspace's menu, takes it out of PMT. Mod folders on disk stay.",
      "New mod makes a mod skeleton and adds it to a workspace. New workspace starts the setup. Launch starts the game with its debug flag, and when the game exits having written fresher script_docs, PMT offers to scan the install again.",
      "Reset all PMT data, in the status bar, removes every workspace, install record and cache. Mods, game files and your settings stay.",
    ],
  },
  wizard: {
    title: "New workspace",
    help: [
      "Pick the game, the install PMT reads its files from, and the mods you work on, then name the workspace.",
      "Add an existing mod folder or make a new mod, and drag the mods into load order. All of it can be changed later in Settings.",
      "The new workspace opens on the page you chose to start on.",
    ],
  },
  "workspace-ide": {
    title: "IDE",
    icon: "i-lucide-code",
    workspace: true,
    tab: true,
    help: [
      "The explorer shows this workspace's mods, then the game's files, which are read only; edit in a mod folder. Ctrl P finds a file by its path, Ctrl Shift F searches the files' text and can replace it.",
      "A single click opens a file in a preview tab that the next single click replaces; a double click or an edit keeps it. Drag a tab to another group, or out to a window of its own. The open tabs come back when you reopen the workspace, unless you turn that off in Settings.",
      "Rest the pointer on a key or a value for what it is and where it is defined. Ctrl click goes to a definition, F2 renames a name in every mod file, and completion offers what fits the block you are in. Problems show as squiggles, in the Problems panel and with quick fixes.",
      "A .gui file opens as its code beside its picture; a coat of arms opens in the Visual view with its layers. The panel beside the picture lists the widgets with each property as written and the States that hide or show them.",
      "The Wiki button at the end of the editor's tabs (Ctrl Shift W) opens the wiki's pages for the file you are in, and the Lexicon of the game's triggers, effects and scopes.",
      "Source control, the sidebar's third view, runs the git installed on your computer on each mod that is a repository. Diffs open as tabs, and merge conflicts are resolved in the editor.",
      "A deleted file goes to PMT's trash, and Undo in the message that follows brings it back. Deleting a mod's root folder in the explorer also removes it from this workspace.",
    ],
  },
  "event-graph": {
    title: "Event Graph",
    icon: "i-lucide-git-branch",
    workspace: true,
    tab: true,
    help: [
      "Type an id, a namespace or a number in Root to see what that event, on_action, decision or scripted effect fires and what fires it, left to right by step.",
      "Filters picks the kinds drawn, the mods (and game files) shown, the depth and the node limit. Follow macros walks into scripted effects and triggers.",
      "Click a node for its detail: what fires it, what it fires, the places it is written and its text. Double-click a node to draw the graph from it, and click a +N group to open it.",
      "Export saves the graph as an image or as SVG, and the background picture can be changed. In the IDE, Ctrl Shift G opens the event under the cursor here.",
    ],
  },
  health: {
    title: "Health",
    icon: "i-lucide-activity",
    workspace: true,
    tab: true,
    help: [
      "The cards count what is worth a look: contests between mods for the same definition, mods overriding the game, mods that need or change another mod, dangling references, localization issues and script issues. Pick a card for its table.",
      "Drag the load order's chips to preview another order without saving it. Save as workspace order saves it, as Settings does.",
      "A row opens beside the table with each place it is written; Compare sets two statements side by side. The error log card follows the game's own log while it runs.",
      "Translation scaffold writes a language's missing localization files, with the source text as the placeholder.",
    ],
  },
  "patch-notes": {
    title: "Patch Notes",
    icon: "i-lucide-newspaper",
    workspace: true,
    tab: true,
    help: [
      "The game's patch notes from its wiki, read when PMT starts and kept, for the game this workspace uses.",
      "Pick a version to read its notes; a dot marks a note with a Modding section. Ctrl F finds in the note shown.",
    ],
  },
  release: {
    title: "Publish",
    icon: "i-lucide-upload",
    workspace: true,
    tab: true,
    help: [
      "Publish edits the mod's own files: its descriptor, and in workshop/ its description file and changelog. Nothing is written until you save, except the thumbnail, screenshots and videos, which save at once.",
      "The description comes from a Markdown file (converted to BBCode when you upload), a BBCode file (uploaded as written), or Steam's own text, which uploads leave as it is. Change source picks one. A title and description can be set for each language, and the DLC the item requires are ticked.",
      "Uploads leave out dotfiles but .metadata, .gitignore and what it names, workshop/previews (Steam takes those as the page's gallery), and the mod's upload exclusions in Settings. PMT keeps a copy of each upload, so Compare shows what the next one changes.",
      "Steam must be running and signed in as the item's owner. A new item stays private until you make it public on its Workshop page. Do not load a local copy and a Workshop subscription of the same mod together.",
      "Paradox Mods has no API: copy the description as BBCode and paste it in the Paradox Launcher's Upload Mod.",
    ],
  },
  "workspace-settings": {
    title: "Settings",
    icon: "i-lucide-settings",
    workspace: true,
    help: [
      "General: the workspace's name, its localization language, the page it opens on, and the IDE's tabs and explorer.",
      "Game install: the install this workspace reads, and how to make the game print its script_docs.",
      "Mods and load order: drag to reorder; for most kinds the last mod to define an object wins. Colors stay with the mod, not its position. Each mod's calendar, Workshop link and upload exclusions are on its card. Changes save as you make them.",
      "Appearance applies to every workspace: the theme, light and dark, matching the game's theme, the interface scale and the pages shown. The IDE's settings, such as text sizes and the Visual view's resolutions, are on their own card.",
    ],
  },
} as const satisfies Record<string, Page>;

export type PageName = keyof typeof PAGES;
export type WorkspacePage = { [N in PageName]: (typeof PAGES)[N] extends { workspace: true } ? N : never }[PageName];
export type WorkspaceTool = { [N in PageName]: (typeof PAGES)[N] extends { tab: true } ? N : never }[PageName];

declare module "vue-router" {
  interface TypesConfig {
    RouteNamedMap: {
      [N in PageName]: N extends WorkspacePage
        ? RouteRecordInfo<N, string, { id: string }, { id: string }>
        : RouteRecordInfo<N, string, Record<never, never>, Record<never, never>>;
    };
  }
}

/** Whether a route is one of a workspace's pages, which take its id. */
export function isWorkspacePage(name: PageName): name is WorkspacePage {
  return "workspace" in PAGES[name];
}

/** The top bar's tabs, in order. */
export const WORKSPACE_TOOLS: readonly WorkspaceTool[] = [
  "workspace-ide",
  "event-graph",
  "health",
  "patch-notes",
  "release",
];

/** The top bar's tabs as the choices of a select. */
export const TOOL_CHOICES: { label: string; value: string }[] = WORKSPACE_TOOLS.map((tool) => ({
  label: PAGES[tool].title,
  value: tool,
}));

/** Lists the top bar's tabs the settings show, in order. */
export function listShownTools(): WorkspaceTool[] {
  const pages = useSettingsStore().value("appearance.pages").split(",");
  return WORKSPACE_TOOLS.filter((tool) => pages.includes(tool));
}

/** The page a workspace opens on: its home page if that is shown, else the first page shown. */
export function homeRoute(workspaceId: string, homePage: string, visible: readonly WorkspaceTool[]) {
  const name = visible.find((tool) => tool === homePage) ?? visible[0] ?? "workspace-ide";
  return { name, params: { id: workspaceId } };
}
