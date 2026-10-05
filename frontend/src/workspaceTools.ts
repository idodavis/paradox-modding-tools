/**
 * The pages: each route's name and title, its Help, and for a workspace's pages the top bar's tab. The
 * router's typed route names come from here.
 */
import type { RouteRecordInfo } from "vue-router";

type Page = { title: string; help: readonly string[]; icon?: string; workspace?: true; tab?: true };

export const PAGES = {
  library: {
    title: "Library",
    icon: "i-lucide-layout-grid",
    help: [
      "A workspace is one game install plus the mods you load with it. The active one is at the top; open another's card to make it active.",
      "More removes a workspace from PMT. Mod folders on disk stay.",
      "New mod creates a mod skeleton and adds it to a workspace. New workspace starts the setup.",
      "Reset all PMT data, in the status bar, removes every workspace, install record and cache. Mods, game files and your appearance settings stay.",
    ],
  },
  wizard: {
    title: "New workspace",
    help: [
      "Pick the game, the install PMT reads its files from, and the mods you work on, then name the workspace.",
      "Add an existing mod folder or create a new mod, and drag them into load order. All of it can be changed later in Settings.",
      "The new workspace opens in the IDE.",
    ],
  },
  "workspace-ide": {
    title: "IDE",
    icon: "i-lucide-code",
    workspace: true,
    tab: true,
    help: [
      "Folders come from this workspace: your mods, then the game files. Game files are read only; edit in a mod folder. Search and Ctrl+P index every folder.",
      "Deleting a mod's root folder in the explorer also removes it from this workspace; files inside a mod delete normally.",
      "Open tabs are kept across a workspace switch and a restart unless you turn that off in Settings › General. The editor's text sizes and checks are in the IDE settings (the gear).",
      "The Guide tab on the editor's right edge opens a wiki pane for the file you are in.",
    ],
  },
  "event-graph": {
    title: "Event Graph",
    icon: "i-lucide-git-branch",
    workspace: true,
    tab: true,
    help: [
      "Pick a root event to see what it fires and what fires it. Origins filter which mods (and game files) feed the graph.",
      "Namespace narrows the picker; leave it empty to search every id. Double-click a node to re-root. Drag is temporary until you change layout or root.",
      "Recenter only pans to fit — it does not undo a layout change.",
      "Hover an event id in the IDE (the definition or a trigger_event) for Open in Event Graph.",
    ],
  },
  health: {
    title: "Health",
    icon: "i-lucide-activity",
    workspace: true,
    tab: true,
    help: [
      "Load order cards: contests between mods (LIOS or FIOS), mods overriding the game, and mods that need or change another mod. Problems in the mods: dangling references, localization gaps, and script errors from the editor's checks.",
      "Drag the load order's chips to preview another order without saving it. Save as workspace order saves it, as Settings does.",
      "A row opens beside the table with each place it is written; Compare the two sets a contest's kept file beside the one it replaces.",
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
      "This page edits the mod folder: descriptor, mod-description.md, mod-description.bbcode, and changelog/<version>.bbcode.",
      "Description is Markdown in a rich editor. Save writes the Markdown file and converts to BBCode. Steam always uploads BBCode. Description paths are in Settings › Mods and load order.",
      "Steam staging skips dotfiles (except .metadata/), .gitignore, patterns in .gitignore, and the mod's upload exclusions from Settings.",
      "Steam: Steam client must be running and signed in as the Workshop owner. New items stay private until you set them Public on Workshop. Do not run a local copy and a Workshop subscribe of the same mod together.",
      "Paradox Mods has no API. Copy BBCode for the launcher, or Copy Markdown. The site often strips formatting. PMT does not write a sibling Documents/…/mod/<name>.mod pointer.",
    ],
  },
  "workspace-settings": {
    title: "Settings",
    icon: "i-lucide-settings",
    workspace: true,
    help: [
      "General: the workspace's name, its localization language, the page it opens on, and the IDE's tabs and explorer.",
      "Game install: the install this workspace reads, and how to make the game print its script_docs.",
      "Mods and load order: drag to reorder; for most kinds the last mod to define an object wins. Colors stay with the mod, not its position. Changes save as you make them.",
      "Appearance applies to every workspace: the theme, light and dark, the interface scale and the pages shown.",
    ],
  },
} as const satisfies Record<string, Page>;

export type PageName = keyof typeof PAGES;
export type WorkspacePage = { [N in PageName]: (typeof PAGES)[N] extends { workspace: true } ? N : never }[PageName];
export type WorkspaceToolName = { [N in PageName]: (typeof PAGES)[N] extends { tab: true } ? N : never }[PageName];

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

/** The top bar's page tabs, in order. */
export const WORKSPACE_TOOLS = (["workspace-ide", "event-graph", "health", "patch-notes", "release"] as const).map(
  (name: WorkspaceToolName) => ({ name, label: PAGES[name].title, icon: PAGES[name].icon }),
);

export const WORKSPACE_TOOL_NAMES: WorkspaceToolName[] = WORKSPACE_TOOLS.map((tool) => tool.name);

/** Where a workspace opens: its default page if shown, else the first shown page. */
export function workspaceHomeRoute(ws: { id: string; defaultTool?: string }, visible: readonly WorkspaceToolName[]) {
  const want = ws.defaultTool || "workspace-ide";
  const name = visible.find((tool) => tool === want) ?? visible[0] ?? "workspace-ide";
  return { name, params: { id: ws.id } };
}

/** Settings' sections: a workspace's, then the app's. */
export const SETTINGS_SECTIONS = {
  general: { label: "General", scope: "Workspace" },
  install: { label: "Game install", scope: "Workspace" },
  mods: { label: "Mods and load order", scope: "Workspace" },
  appearance: { label: "Appearance", scope: "App" },
} as const;
export type SettingsSection = keyof typeof SETTINGS_SECTIONS;

/** A setting the palette flips where you are. */
export type SettingSwitch = "reopenTabs" | "hideBinaries" | "matchGame" | "followSystem";

/**
 * Every setting, once: Settings' search keeps the sections holding a match, and the palette lists each,
 * flipping a switch in place and opening the section, or the IDE's settings card, for one with a value.
 */
export const SETTINGS: readonly { label: string; section: SettingsSection | "ide"; words?: string; flips?: SettingSwitch }[] = [
  { label: "Workspace name", section: "general" },
  { label: "Localization language", section: "general", words: "loc english french german spanish russian" },
  { label: "Page a workspace opens on", section: "general", words: "default start" },
  { label: "Reopen the IDE's tabs", section: "general", words: "remember open files", flips: "reopenTabs" },
  { label: "Hide binaries in the explorer", section: "general", words: "dll exe files", flips: "hideBinaries" },
  { label: "Game install", section: "install", words: "folder path version steam delete" },
  { label: "script_docs", section: "install", words: "dumps documentation type system" },
  { label: "Load order", section: "mods", words: "reorder drag add create remove" },
  { label: "Mod color", section: "mods", words: "slot custom" },
  { label: "Mod folder", section: "mods", words: "locate path" },
  { label: "Thumbnail", section: "mods", words: "image picture" },
  { label: "Calendar", section: "mods", words: "era year offset date" },
  { label: "Workshop item", section: "mods", words: "steam publishing description markdown bbcode unlink" },
  { label: "Upload exclusions", section: "mods", words: "workshop ignore gitignore" },
  { label: "Theme", section: "appearance", words: "dark light catppuccin one crusader kings europa universalis victoria" },
  { label: "Match the workspace's game theme", section: "appearance", flips: "matchGame" },
  { label: "Follow Windows light and dark", section: "appearance", words: "system mode", flips: "followSystem" },
  { label: "Interface scale", section: "appearance", words: "text size zoom" },
  { label: "Pages a workspace shows", section: "appearance", words: "tabs ide event graph health patch notes publish" },
  { label: "Mod colors", section: "appearance", words: "reset automatic" },
  { label: "Editor text size", section: "ide", words: "font" },
  { label: "Explorer and panel text size", section: "ide", words: "font problems search wiki" },
  { label: "Checks", section: "ide", words: "diagnostics problems" },
  { label: "Wiki suggestion toast", section: "ide", words: "guide" },
];

/** The localization languages a workspace may default to. */
export const LOC_LANGUAGES = [
  { label: "English", value: "english" },
  { label: "French", value: "french" },
  { label: "German", value: "german" },
  { label: "Spanish", value: "spanish" },
  { label: "Russian", value: "russian" },
  { label: "Korean", value: "korean" },
  { label: "Simplified Chinese", value: "simp_chinese" },
  { label: "Polish", value: "polish" },
  { label: "Turkish", value: "turkish" },
  { label: "Brazilian Portuguese", value: "braz_por" },
  { label: "Japanese", value: "japanese" },
];
