/**
 * One page catalog: title, description, help, keepAlive, and toolbar pills.
 */
export type PageId =
  | "library"
  | "wizard"
  | "workspace-ide"
  | "event-graph"
  | "health"
  | "patcher"
  | "release"
  | "workspace-settings";

/** Route meta shared by the header, Help modal, toolbar, and KeepAlive. */
export type PageEntry = {
  title: string;
  description: string;
  help: string[];
  keepAlive?: boolean;
  /** beforeEach sets the active workspace from :id. */
  workspace?: boolean;
  /** Toolbar / Display visible-tools pill. */
  tool?: { label: string; icon: string };
};

declare module "vue-router" {
  interface RouteMeta extends PageEntry {}
}

/** Canonical copy and chrome for every named route. */
export const PAGE_CATALOG: Record<PageId, PageEntry> = {
  library: {
    title: "Library",
    description: "Browse and manage your modding workspaces.",
    keepAlive: true,
    help: [
      "This is your workspace library. A workspace is one game install plus the mods you are editing.",
      "Click a card to open that workspace’s default page. Edit opens Workspace Settings. Delete removes the workspace from PMT only — mod folders on disk stay.",
      "New Workspace starts the setup wizard. New Mod creates a skeleton in an existing workspace, or starts the wizard when the library is empty.",
      "Reset all data at the bottom wipes PMT workspaces, install records, leftover folders under the PMT config tree, and caches. It does not delete mods or Steam/game installs on disk. Theme, UI scale, and editor font are kept.",
    ],
  },
  wizard: {
    title: "Create Workspace",
    description: "Set up a new modding workspace.",
    help: [
      "Walk through game, install, mods, and name.",
      "Add existing mod to workspace or Create new mod (descriptor, empty common/ and events/, readmes, localization stub with UTF-8 BOM).",
      "Drag mods to set load order (SortOrder). You can change all of this later in Workspace Settings.",
      "After create you land in the IDE so you can open the files you just attached.",
    ],
  },
  "workspace-ide": {
    title: "Workspace IDE",
    description: "View and edit files in your workspace.",
    workspace: true,
    tool: { label: "IDE", icon: "i-lucide-code" },
    help: [
      "Folders come from this workspace: your mods, then the game files. There is no Open Folder — PMT owns the folder set. Back returns to the Library, not the create-workspace wizard.",
      "Game files (Crusader Kings III, Victoria 3, or Europa Universalis V) are read-only. Edit in a mod folder. Search and Ctrl+P index every root. Deleting a mod’s root folder in the explorer also detaches it from this workspace; files inside a mod delete normally.",
      "Open editor tabs persist across workspace switch and app close unless you turn that off in Workspace Settings. Themes live in the PMT header, not in VS Code settings.",
      "The Guide pull-tab on the editor’s right edge opens a resizable wiki pane (search, contents, related pages). It stays closed until you open it. Snooze or turn off the toast under Display.",
    ],
  },
  "event-graph": {
    title: "Event Graph",
    description: "Browse event and on_action links in the workspace.",
    keepAlive: true,
    workspace: true,
    tool: { label: "Event Graph", icon: "i-lucide-waypoints" },
    help: [
      "Pick a root event to see what it fires and what fires it. Origins filter which mods (and game files) feed the graph.",
      "Namespace narrows the picker; leave it empty to search every id. Double-click a node to re-root. Drag is temporary until you change layout or root.",
      "Recenter only pans to fit — it does not undo a layout change.",
      "Hover an event id in the IDE (the definition or a trigger_event) for Open in Event Graph.",
    ],
  },
  health: {
    title: "Workspace Health",
    description: "Compatibility contests and localization coverage for this workspace.",
    keepAlive: true,
    workspace: true,
    tool: { label: "Workspace Health", icon: "i-lucide-heart-pulse" },
    help: [
      "Compatibility cards: contests (FIOS / LIOS), game-file overrides, cross-mod depends, and dangling refs. Localization cards: missing, orphans, and untranslated for the language in the heading select.",
      "Drag load-order chips to preview winners without writing the workspace. Save as workspace order persists the same SortOrder as Settings.",
      "Row detail shows a colorized snippet around the hit. Default loc language is an IDE/LSP setting; coverage lists every language the mods ship.",
    ],
  },
  patcher: {
    title: "Patch Center",
    description: "Wiki patch notes for the workspace game.",
    keepAlive: true,
    workspace: true,
    tool: { label: "Patch Center", icon: "i-lucide-arrow-left-right" },
    help: [
      "Patch Notes is a cached wiki changelog reader for the game this workspace uses.",
      "Pick a version to read the notes. This page does not retarget or merge files.",
    ],
  },
  release: {
    title: "Publish",
    description: "Edit the listing and publish to Steam Workshop or Paradox Mods.",
    keepAlive: true,
    workspace: true,
    tool: { label: "Publish", icon: "i-lucide-upload" },
    help: [
      "This page edits the mod folder: descriptor, mod-description.md, mod-description.bbcode, and changelog/<version>.bbcode.",
      "Description is Markdown in a rich editor. Save writes the Markdown file and converts to BBCode. Steam always uploads BBCode. Description paths are in Workspace Settings.",
      "Steam staging skips dotfiles (except .metadata/), .gitignore, patterns in .gitignore, and Workshop ignore patterns from Settings.",
      "Steam: Steam client must be running and signed in as the Workshop owner. New items stay private until you set them Public on Workshop. Do not run a local copy and a Workshop subscribe of the same mod together.",
      "Paradox Mods has no API. Copy BBCode for the launcher, or Copy Markdown. The site often strips formatting. PMT does not write a sibling Documents/…/mod/<name>.mod pointer.",
    ],
  },
  "workspace-settings": {
    title: "Workspace Settings",
    description: "Configure this workspace.",
    workspace: true,
    help: [
      "Overview: name, default loc language, whether the IDE remembers open files, explorer hide-binaries, and which page Library should open.",
      "Game: pick or add an install; its version is read from the install at each scan. Changing install rebuilds language intelligence. Mods: attach an existing folder or create a new skeleton, then color, reorder, or detach. Detach does not delete the folder on disk. Per-mod description file paths and Workshop ignore live here.",
      "Remove this workspace deletes the PMT record only. Reset all data lives on the Library page. Appearance (scale, editor font, which tools show) lives in the header Display control, not here.",
    ],
  },
};

/** Toolbar pills in catalog order. */
export const WORKSPACE_TOOLS = (Object.entries(PAGE_CATALOG) as [PageId, PageEntry][])
  .filter((entry): entry is [PageId, PageEntry & { tool: { label: string; icon: string } }] => !!entry[1].tool)
  .map(([name, page]) => ({
    name,
    label: page.tool.label,
    icon: page.tool.icon,
  }));

/** Route name of a workspace tool page. */
export type WorkspaceToolName = (typeof WORKSPACE_TOOLS)[number]["name"];

/** All workspace tool route names, in toolbar order. */
export const WORKSPACE_TOOL_NAMES: WorkspaceToolName[] = WORKSPACE_TOOLS.map((t) => t.name);

/** Landing route for a workspace: its default page if shown, else the first shown page. */
export function workspaceHomeRoute(
  ws: { id: string; defaultTool?: string },
  visible: readonly string[],
): { name: string; params: { id: string } } {
  const want = ws.defaultTool || "workspace-ide";
  return { name: visible.includes(want) ? want : (visible[0] ?? "workspace-ide"), params: { id: ws.id } };
}
