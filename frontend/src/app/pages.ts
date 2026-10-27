/** The catalog of the app's pages and the top bar's tabs. */
import { onActivated, onDeactivated, shallowRef, type ShallowRef } from "vue";
import type { RouteRecordInfo } from "vue-router";

import { useSettingsStore } from "../stores/settings";

/** A Page is one page of the catalog. */
export type Page = { title: string; help: readonly string[]; icon?: string; workspace?: true };

/** PAGES holds every page of the app by its route name. */
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
    help: [
      "The explorer shows this workspace's mods in load order, then the game's files, which are read only; edit in a mod folder. The filter button in each tree's header shows a box that filters that tree's files by path, in folders you have not opened too; pressing it again or Escape hides the box and clears the filter. Ctrl P finds a file by its path, Ctrl Shift F searches the files' text and can replace it.",
      "A single click opens a file in a preview tab that the next single click replaces; a double click or an edit keeps it. Drag a tab to another group, or out to a window of its own. A tab's right-click menu closes the others, the tabs to its right, the saved ones or all of them, asking once before it drops changes not saved; Reveal in explorer selects its file in the explorer. The open tabs come back when you reopen the workspace, unless you turn that off in Settings.",
      "Rest the pointer on a key or a value for what it is and where it is defined. Ctrl click goes to a definition, F2 renames a name in every mod file, and completion offers what fits the block you are in. Problems show as squiggles and with quick fixes. The Problems panel (Ctrl J) lists them for the open files, then Health's compatibility findings about those files.",
      "The editor's right-click menu also finds what fires an event, an on_action, a decision or a scripted effect, listed in the sidebar's search; looks the name up in the Wiki pane, as a lexicon entry or its file's page; and compares a mod's file with the game's copy or with another file you pick. Ctrl Shift D compares the active file with its last commit.",
      "Alt Left and Alt Right, or the mouse's back and forward buttons, go back and forward through the places you jumped between: a definition, a search hit, a file opened, a tab switched to, or a move of the caret of more than ten lines. Each window keeps its own last 50.",
      "Ctrl K opens the palette: type > for the commands and settings, the ones you ran last first. Enter flips a switch, a page shown or a check at once; a setting with a value opens where it is set. Compare this mod with Steam opens Publish's compare for the active file's mod.",
      "A .gui file opens as its code beside its picture; a coat of arms opens in the Visual view with its layers. The panel beside the picture lists the widgets with each property as written and the States that hide or show them. It starts closed and opens when you click a widget or a layer. The States button, Layers for a coat, opens it on that tab and closes it again, and dragging its edge past its least width closes it too.",
      "The Wiki button at the end of the editor's tabs (Ctrl Shift W) opens the wiki's pages for the file you are in, and the Lexicon of the game's triggers, effects and scopes.",
      "Source control, the sidebar's third view, runs the git installed on your computer on each mod that is a repository. Each repository has a section of its own, named for its first mod, with its branch, its commit box and its changes; mods that share a repository are counted in its header, and its commit, refresh and other actions show when the pointer is on the header. The Graph at the bottom shows the log of the active file's repository, or of the one you pick in its header. Commit in the palette opens the active file's repository's commit box, and asks which repository when the file is in none. Diffs open as tabs, and merge conflicts are resolved in the editor.",
      "Entries, the sidebar's fourth view, lists the active file's entries grouped by kind, with their titles and their keys. Find an entry searches both, and a click opens an entry at its line.",
      "A deleted file goes to PMT's trash, and Undo in the message that follows brings it back. Deleting a mod's root folder in the explorer also removes it from this workspace.",
    ],
  },
  "event-graph": {
    title: "Event Graph",
    icon: "i-lucide-git-branch",
    workspace: true,
    help: [
      "Type an id, a namespace or a number in Root to see what that event, on_action, decision or scripted effect fires and what fires it, left to right by step.",
      "Any entry that fires an event, such as a character interaction, shows before it with the rest of what fires it, whatever kinds Filters picks.",
      "Filters picks the kinds drawn, the mods (and game files) shown, the depth and the node limit. Follow macros walks into scripted effects to find the events they fire.",
      "Click a node for its detail: its texts, each marked where the workspace's language lacks it, what it fires and what fires it with the place each is written, what it reads, and its script. Double-click a node to draw the graph from it, and click an N more group to open it.",
      "A dashed node is one the depth stopped at that links further: click it to add its next step. Collapse all closes every group and dashed node you opened.",
      "Export saves the graph as a PNG image or as SVG. In the IDE, Ctrl Shift G opens the event under the cursor here.",
    ],
  },
  health: {
    title: "Health",
    icon: "i-lucide-activity",
    workspace: true,
    help: [
      "The summary has three panels, one for each question: Compatibility, how the mods touch the same things; Mods, whether each mod is sound on its own; and Game log, what the game's last run logged. Each lists its serious rows first. Open a panel for its view; the rail on the left switches between them, and its edge drags to make it wider.",
      "Every row is serious, a warning or info. Serious means a mod's change is lost, a dependency is missing, or the game's choice is not documented. A warning is something the game copes with. Info is only how things are, and is folded until you open it.",
      "Within a level, rows are grouped by the check that found them, each with its count; a level with several checks opens folded, so it reads as its list of checks. Each row's chip names its check, and the checks menu keeps only the checks you pick.",
      "Compatibility shows the load order. How the game picks a copy, at its top, says for this workspace's game which copy the game keeps and what the load order can change. Drag the order's chips to preview another order without saving it; the page then lists the names that change hands, Save as workspace order keeps it, and Back to the workspace order drops it. A row opens beside the list with each copy, whether the game uses it and why, and Compare the two puts them side by side. Where the load order decides a conflict and another order would fix it, Try this order previews it. A row's menu can ignore the conflicts between two mods; Stop ignoring brings them back.",
      "A mod's view counts what it holds, filters its problems by topic, and shows each language's share of its localization keys. Write the missing files makes the localization files a language lacks, with the source text as the placeholder. A problem the game also logged says so.",
      "The game log lists the lines from your mods apart from the game's own, by mod or by message, with repeats folded. Each line says why it is a mod's. Open error.log opens the log in the IDE as it was when you opened it; the tab does not follow the game's later writes. Live means the list follows the log while the workspace is open.",
    ],
  },
  "patch-notes": {
    title: "Patch Notes",
    icon: "i-lucide-newspaper",
    workspace: true,
    help: [
      "The game's patch notes from its wiki, read when PMT starts and kept, for the game this workspace uses.",
      "Pick a version to read its notes; a dot marks a note with a Modding section. Ctrl F finds in the note shown.",
    ],
  },
  release: {
    title: "Publish",
    icon: "i-lucide-upload",
    workspace: true,
    help: [
      "Publish shows the mod's Workshop page as players see it. Click the name, tags, requirements, pictures or description to change them. Every change is saved in the mod's files as soon as you stop typing, so there is nothing to save.",
      "The description is written as it looks on Steam, or as BBCode with the BBCode switch. It is kept as BBCode in the mod's .workshop folder, and Steam gets it as written. A description the visual editor cannot hold without losing markup opens as BBCode.",
      "The rail on the right is this update: its version, the game version it is for, and its change note. It lists what the upload changes on Steam. Click a change to see it beside Steam's copy. Files never uploaded in the menu holds the files an upload leaves out.",
      "Steam must be running and signed in as the item's owner. A new item stays private until you make it public on its Workshop page. Do not load a local copy and a Workshop subscription of the same mod together.",
      "A mod can have a separate Workshop item for an older game version. Pick it from the mod's name at the top, or add one from the same menu.",
      "Paradox Mods has no API: use Copy for Paradox Mods in the description's BBCode view and paste it in the Paradox Launcher's Upload Mod.",
    ],
  },
} as const satisfies Record<string, Page>;

/** A PageName is the route name of a page. */
export type PageName = keyof typeof PAGES;
/** A WorkspacePage is the route name of a page that takes a workspace's id. */
export type WorkspacePage = { [N in PageName]: (typeof PAGES)[N] extends { workspace: true } ? N : never }[PageName];
declare module "vue-router" {
  interface TypesConfig {
    RouteNamedMap: {
      [N in PageName]: N extends WorkspacePage
        ? RouteRecordInfo<N, string, { id: string }, { id: string }>
        : RouteRecordInfo<N, string, Record<never, never>, Record<never, never>>;
    };
  }
}

/** isWorkspacePage reports whether a route is one of a workspace's pages. */
export function isWorkspacePage(name: PageName): name is WorkspacePage {
  return "workspace" in PAGES[name];
}

/** WORKSPACE_TOOLS holds the workspace pages that have a tab in the top bar, in order. */
export const WORKSPACE_TOOLS = [
  "workspace-ide",
  "event-graph",
  "health",
  "patch-notes",
  "release",
] as const satisfies readonly WorkspacePage[];

/** A WorkspaceTool is the route name of a workspace page that has a tab in the top bar. */
export type WorkspaceTool = (typeof WORKSPACE_TOOLS)[number];

/** TOOL_CHOICES holds the top bar's tabs as the choices of a select. */
export const TOOL_CHOICES: { label: string; value: string }[] = WORKSPACE_TOOLS.map((tool) => ({
  label: PAGES[tool].title,
  value: tool,
}));

/** listShownTools returns the top bar's tabs that the settings show, in order. */
export function listShownTools(): WorkspaceTool[] {
  const pages = useSettingsStore().value("appearance.pages").split(",");
  return WORKSPACE_TOOLS.filter((tool) => pages.includes(tool));
}

/**
 * useShown returns whether the kept-alive page that calls it is the one shown. A page stays mounted behind the others,
 * so the flag is false from the time it is deactivated until it is activated again.
 */
export function useShown(): Readonly<ShallowRef<boolean>> {
  const shown = shallowRef(true);
  onActivated(() => (shown.value = true));
  onDeactivated(() => (shown.value = false));
  return shown;
}

/**
 * homeRoute returns the route a workspace opens on. It is the home page if shown, else the first shown page, else the
 * IDE when no page is shown.
 */
export function homeRoute(workspaceId: string, homePage: string) {
  const visible = listShownTools();
  const name = visible.find((tool) => tool === homePage) ?? visible[0] ?? "workspace-ide";
  return { name, params: { id: workspaceId } };
}
