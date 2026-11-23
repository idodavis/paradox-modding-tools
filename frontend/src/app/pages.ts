/** The catalog of the app's pages and the top bar's tabs. */
import { onActivated, onDeactivated, shallowRef, type ShallowRef } from "vue";
import type { RouteRecordInfo } from "vue-router";

import { useSettingsStore } from "../stores/settings";

/** A HelpSection is one topic of a page's help: a heading and the text under it. */
export type HelpSection = { heading: string; text: string };

/** A Page is one page of the catalog. */
export type Page = {
  title: string;
  help: readonly HelpSection[];
  start?: readonly string[];
  icon?: string;
  workspace?: true;
};

/** TERMS holds the words PMT uses that a new modder may not know, each with its meaning. */
export const TERMS: readonly { term: string; meaning: string }[] = [
  { term: "workspace", meaning: "One game install and the mods you load with it. PMT reads them together." },
  { term: "install", meaning: "The game's folder on this computer. PMT only reads it." },
  {
    term: "load order",
    meaning:
      "The order the game reads your mods in, after its own files. When two mods have the same file, the later one's copy is used.",
  },
  {
    term: "game files",
    meaning: "The game's own files. They are read only; a mod changes them by overriding. Modders call them vanilla.",
  },
  {
    term: "game documentation",
    meaning:
      "The lists the game writes of its own effects, triggers, scopes, modifiers and data types when you type script_docs, then dump_data_types, in its console. Wikis call them the script docs and the data type dumps. PMT checks your script against them.",
  },
  {
    term: "game readme/info",
    meaning:
      "A note the game keeps in a folder, such as _traits.info or readme.txt, on what the folder holds and how its files are written. A folder's menu in the explorer opens it, and so does the Wiki pane for the file you are in.",
  },
  {
    term: "scope",
    meaning:
      "What a line of script acts on, such as a character or a country. Each effect and trigger works only in some scopes, and a link such as root moves to another.",
  },
];

/** PAGES holds every page of the app by its route name. */
export const PAGES = {
  library: {
    title: "Library",
    icon: "i-lucide-layout-grid",
    start: [
      "A workspace is one game install plus the mods you load with it.",
      "The active workspace is at the top. Open another's card to make it active.",
      "Open IDE on the active card to start working in it.",
    ],
    help: [
      {
        heading: "Workspaces",
        text: "A workspace is one game install plus the mods you load with it. The active one is at the top; open another's card to make it active.",
      },
      {
        heading: "Removing a workspace",
        text: "Remove from PMT, in a workspace's menu, takes it out of PMT. Mod folders on disk stay.",
      },
      {
        heading: "New mod and New workspace",
        text: "New mod opens Make a new mod, or adds a mod to the open workspace. New workspace starts the setup.",
      },
      {
        heading: "Launching the game",
        text: "Launch, in a workspace's menu, on the IDE's start page and in Settings under Game install, starts the game with its debug flag. When the game closes, a message says what your mods logged, and offers to scan the install again if the game wrote fresher documentation.",
      },
      {
        heading: "Reset all PMT data",
        text: "Reset all PMT data, in the status bar, removes every workspace, install record and cache. Mods, game files and your settings stay.",
      },
    ],
  },
  "new-mod": {
    title: "Make a new mod",
    help: [
      {
        heading: "Making a mod",
        text: "Pick the game and the language, and name the new mod. PMT shows the folder it makes, then makes a workspace with the mod in it and opens the IDE.",
      },
      {
        heading: "Where the mod goes",
        text: "The Folder is the game's mod folder unless you choose another with Browse. PMT makes the mod's own folder inside it.",
      },
      {
        heading: "How a mod works",
        text: "With Guidance for new modders on, How a mod works explains what the mod's folders are for.",
      },
    ],
  },
  wizard: {
    title: "New workspace",
    help: [
      {
        heading: "Picking the game and the mods",
        text: "Pick the game, the install PMT reads its files from, and the mods you work on, then name the workspace.",
      },
      {
        heading: "Load order",
        text: "Add an existing mod folder or make a new mod, and drag the mods into load order. All of it can be changed later in Settings.",
      },
      { heading: "Where it opens", text: "The new workspace opens on the page you chose to start on." },
    ],
  },
  "workspace-ide": {
    title: "IDE",
    icon: "i-lucide-code",
    workspace: true,
    start: [
      "Open a file of your mod in the explorer on the left. The game files below it are read only.",
      "Rest the pointer on any word to see what it is. Ctrl click goes to where it is defined.",
      "Save with Ctrl S. Problems show as squiggles, and all of them in the Problems panel, Ctrl J.",
    ],
    help: [
      {
        heading: "Explorer",
        text: "The explorer shows this workspace's mods in load order, then its external mods, then the game's files, which are read only; edit in a mod folder. The filter button in each tree's header shows a box that filters that tree's files by path, in folders you have not opened too; pressing it again or Escape hides the box and clears the filter.",
      },
      {
        heading: "Deleting files",
        text: "A deleted file goes to PMT's trash, and Undo in the message that follows brings it back. Deleting a mod's root folder in the explorer also removes it from this workspace.",
      },
      {
        heading: "Tabs",
        text: "A single click opens a file in a preview tab that the next single click replaces; a double click or an edit keeps it. Drag a tab to another group, or out to a window of its own. A tab's right-click menu closes the others, the tabs to its right, the saved ones or all of them, asking once before it drops changes not saved; Reveal in explorer selects its file in the explorer. The open tabs come back when you reopen the workspace, unless you turn that off in Settings.",
      },
      {
        heading: "Hover, go to and rename",
        text: "Rest the pointer on a key or a value for what it is and where it is defined. Ctrl click goes to a definition, F2 renames a name in every mod file, and completion offers what fits the block you are in.",
      },
      {
        heading: "Problems",
        text: "Problems show as squiggles and with quick fixes. The Problems panel (Ctrl J) lists them for the open files, then Health's compatibility findings about those files.",
      },
      {
        heading: "Search and quick open",
        text: "Ctrl P finds a file by its path, Ctrl Shift F searches the files' text and can replace it.",
      },
      {
        heading: "The editor's right-click menu",
        text: "The editor's right-click menu also finds what fires an event, an on_action, a decision or a scripted effect, listed in the sidebar's search; looks the name up in the Wiki pane, as a lexicon entry or its file's page; and compares a mod's file with the game's copy or with another file you pick. Ctrl Shift D compares the active file with its last commit.",
      },
      {
        heading: "Going back",
        text: "Alt Left and Alt Right, or the mouse's back and forward buttons, go back and forward through the places you jumped between: a definition, a search hit, a file opened, a tab switched to, or a move of the caret of more than ten lines. Each window keeps its own last 50.",
      },
      {
        heading: "The palette",
        text: "Ctrl K opens the palette: type > for the commands and settings, the ones you ran last first. Enter flips a switch, a page shown or a check at once; a setting with a value opens where it is set. Compare this mod with Steam opens Publish's compare for the active file's mod.",
      },
      {
        heading: "Visual view",
        text: "A .gui file or a coat of arms file opens as its code, unless Open drawable files in Visual is on. The Code and Visual switch at the end of its tabs, or Ctrl Shift V, draws it in Visual, which gives the picture the IDE's screen: the sidebar's Structure lists what the file draws, a window's widgets as a tree or the coats with their pictures, and the tools over the picture are Save, the resolution or the coat's frame, Bounds, Grid, the code beside the picture and the panel. The panel lists the widgets with each property as written and the States that hide or show them, or a coat's Layers. It starts closed and opens when you click a widget or a layer. Its button opens it on States, Layers for a coat, and closes it again, and dragging its edge past its least width closes it too; the code beside the picture opens and closes the same way. A Markdown file has the switch too: its Visual shows the text as it reads, to edit there, in its own tab, and the rest of the IDE stays as it is.",
      },
      {
        heading: "Wiki",
        text: "The Wiki button at the end of the editor's tabs (Ctrl Shift W) opens the wiki's pages for the file you are in, and the Lexicon of the game's triggers, effects and scopes.",
      },
      {
        heading: "Source control",
        text: "Source control, the sidebar's third view, runs the git installed on your computer on each mod that is a repository. Each repository has a section of its own, named for its first mod, with its branch, its commit box and its changes; mods that share a repository are counted in its header, and its commit, refresh and other actions show when the pointer is on the header. The Graph at the bottom shows the log of the active file's repository, or of the one you pick in its header. Commit in the palette opens the active file's repository's commit box, and asks which repository when the file is in none. Diffs open as tabs, and merge conflicts are resolved in the editor.",
      },
      {
        heading: "Entries",
        text: "Entries, the sidebar's fourth view, lists the active file's entries grouped by kind, with their titles and their keys. Find an entry searches both, and a click opens an entry at its line.",
      },
    ],
  },
  graph: {
    title: "Graph",
    icon: "i-lucide-git-branch",
    workspace: true,
    start: [
      "Pick a good, a building, a technology or an event in the list on the left, or on the start page.",
      "Search narrows the list you are on, and a tab picks goods, buildings, technologies or events.",
      "With nothing picked, the page offers the game's goods, building groups and technologies, the definitions your mods add or change, and the ones you opened last.",
    ],
    help: [
      {
        heading: "Subject",
        text: "The list on the left holds the game's goods, buildings, technologies and events, each tab under the game's own groups. Click a row to make it the subject; Back and Forward step through what you opened. In the IDE, Ctrl Shift G opens the name under the cursor. The kind of the subject picks the view.",
      },
      {
        heading: "A good",
        text: "A good is a sheet: its head says where it is found and what it costs. How it's made lists the buildings that make it, a building whose levels take goods as a table of its levels. What uses it lists the buildings that take it, by group; click a group to open it. CK3 says of each building whether it produces the good, boosts it by a share, or gives it when a need is met. By era and From narrow both lists.",
      },
      {
        heading: "A building",
        text: "A Victoria 3 building shows its production method groups: pick one method in each, and the totals follow your picks. A CK3 building line or an EU5 building shows its levels side by side, with what changes across them as bars and what never changes said once.",
      },
      {
        heading: "A technology",
        text: "Whole tree draws every technology of the tree, with the chain of the selected one lit: what it needs and what it leads to. Only its chain draws that chain alone. A technology stands after everything it requires. The arrows at the top right turn the tree left to right or top to bottom, and the eye shows or hides the wires outside the chain. An EU5 age can also show the advances of one religion, culture or country. CK3's innovations have no prerequisites, so they are a grid of era by group.",
      },
      {
        heading: "Detail",
        text: "The button at the right edge opens the detail of what is selected: what it is, what it says in the game's words, its links both ways, and its script. Click anything it names to open that in the Graph. Open in IDE goes to its definition. If it replaces the game's copy, you can compare the two.",
      },
      {
        heading: "Event graph",
        text: "An event, an on_action, a decision, a message or a scripted effect is drawn as what fires it and what it fires. Show picks which of those are drawn, From which origins, Through scripted effects follows an effect to what it fires, and Reach sets how deep and how many nodes. Reset puts them back.",
      },
      {
        heading: "Stepping",
        text: "Click a node to light its path. Double-click it to make it the subject. A dashed node is one the depth stopped at, and an N more group holds the rest: click either to open it, and Collapse all closes what you opened.",
      },
      {
        heading: "What the graph does not work out",
        text: "A weight is the number written, marked ± where blocks change it, never worked out. A trigger is shown, never evaluated. A delay written as a value or a name is not put in order. A CK3 message sent without a type is not drawn.",
      },
    ],
  },
  health: {
    title: "Health",
    icon: "i-lucide-activity",
    workspace: true,
    start: [
      "Compatibility shows how your mods touch the same things.",
      "Mods shows whether each mod is sound on its own, and Game log what the game's last run logged.",
      "Each panel lists its worst rows first. Open a panel for its view; the rail on the left switches between them.",
    ],
    help: [
      {
        heading: "The summary",
        text: "The summary has three panels, one for each question: Compatibility, how the mods touch the same things; Mods, whether each mod is sound on its own; and Game log, what the game's last run logged. Each lists its worst rows first. Open a panel for its view; the rail on the left switches between them, and its edge drags to make it wider.",
      },
      {
        heading: "Levels",
        text: "Every row is critical, an error, a warning or info, by how much the game loses. Critical means the game loses a whole file, folder, definition or mod: it never reads it, or keeps another copy. An error means the game skips one statement, line or value and keeps running, so the mod plays while that part of it is lost. A warning may be fine, or only shows a player the wrong text. Info is only how things are, and is folded until you open it.",
      },
      {
        heading: "Checks",
        text: "Within a level, rows are grouped by the check that found them, each with its count; a level with several checks opens folded, so it reads as its list of checks. Each row's chip names its check, and the checks menu keeps only the checks you pick.",
      },
      {
        heading: "Compatibility",
        text: "Compatibility shows the load order. How the game picks a copy, at its top, says for this workspace's game which copy the game keeps and what the load order can change. Drag the order's chips to preview another order without saving it; the page then lists the names that change hands, Save as workspace order keeps it, and Back to the workspace order drops it. A row opens beside the list with each copy, whether the game uses it and why, and Compare the two puts them side by side. Where the load order decides a conflict and another order would fix it, Try this order previews it. A row's menu can ignore the conflicts between two mods; Stop ignoring brings them back.",
      },
      {
        heading: "A mod's view",
        text: "A mod's view says which of the workspace's mods it is judged with, counts what it holds, filters its problems by topic, and shows each language's share of its localization keys. Write the missing keys adds the keys a language lacks to its files, with the base language's text as the placeholder. A problem the game also logged says so.",
      },
      {
        heading: "Game log",
        text: "The game log lists the lines from your mods apart from the game's own, by mod or by message, with repeats folded. Each line says why it is a mod's. Open error.log opens the log in the IDE as it was when you opened it; the tab does not follow the game's later writes. Live means the list follows the log while the workspace is open.",
      },
      {
        heading: "External mods",
        text: "External mods: add subscribed Workshop mods, or copies of other people's mods, as external mods to check your mods against them. PMT keeps an external mod read only, never publishes it, and keeps it out of your totals.",
      },
    ],
  },
  "patch-center": {
    title: "Patch Center",
    icon: "i-lucide-newspaper",
    workspace: true,
    start: [
      "Your mods shows what the game's patches changed under each mod, from the version it is made for to the game's.",
      "Pick a version in the list to read what it changed. A dot marks a note with a Modding section.",
      "Mark as made for sets a mod's version once you have carried the changes over.",
    ],
    help: [
      {
        heading: "Your mods",
        text: "Each mod's line says which versions its changes run from and to, and counts the names the game renamed or removed that the mod uses, the game files it replaces that the game changed, and the breaking changes and new features that name what it holds. Open a mod for the rows. Health lists the same findings.",
      },
      {
        heading: "Compare from and Mark as made for",
        text: "A mod that gives no supported version that PMT can read is compared from the version you pick in Compare from. Mark as made for writes the game's release as the mod's supported version, in its descriptor and its Publish listing.",
      },
      {
        heading: "A version",
        text: "Pick a version to read the wiki's patch notes, the Modding Co-op's digest and the game files it changed. A mod's name under a digest's article says the article names a folder the mod writes files in, or a name the mod uses. Compare with the game's opens a file you replace next to the game's copy.",
      },
      {
        heading: "Changes not known yet",
        text: "Where no digest or comparison covers a version yet, the page says so. The comparison PMT makes itself needs the game's documentation to be current: type script_docs in the game's console after an update.",
      },
    ],
  },
  release: {
    title: "Publish",
    icon: "i-lucide-upload",
    workspace: true,
    start: [
      "Publish shows the mod's Workshop page as players see it.",
      "Click the name, tags, requirements, pictures or description to change them.",
      "Every change is saved in the mod's files as soon as you stop typing, so there is nothing to save.",
    ],
    help: [
      {
        heading: "The Workshop page",
        text: "Publish shows the mod's Workshop page as players see it. Click the name, tags, requirements, pictures or description to change them. Every change is saved in the mod's files as soon as you stop typing, so there is nothing to save.",
      },
      {
        heading: "The description",
        text: "The description is written as it looks on Steam, or as BBCode with the BBCode switch. It is kept as BBCode in the mod's .workshop folder, and Steam gets it as written. A description the visual editor cannot hold without losing markup opens as BBCode.",
      },
      {
        heading: "This update",
        text: "The rail on the right is this update: its version, the game version it is for, and its change note. It lists what the upload changes on Steam. Click a change to see it beside Steam's copy. Files never uploaded in the menu holds the files an upload leaves out.",
      },
      {
        heading: "Uploading",
        text: "Steam must be running and signed in as the item's owner. A new item stays private until you make it public on its Workshop page. Do not load a local copy and a Workshop subscription of the same mod together.",
      },
      {
        heading: "Older game versions",
        text: "A mod can have a separate Workshop item for an older game version. Pick it from the mod's name at the top, or add one from the same menu.",
      },
      {
        heading: "Paradox Mods",
        text: "Paradox Mods has no API: use Copy for Paradox Mods in the description's BBCode view and paste it in the Paradox Launcher's Upload Mod.",
      },
      {
        heading: "External mods",
        text: "External mods: add subscribed Workshop mods, or copies of other people's mods, as external mods to check your mods against them. PMT keeps an external mod read only, never publishes it, and keeps it out of your totals.",
      },
    ],
  },
} as const satisfies Record<string, Page>;

/** PANE_HELP is the agent pane's help, shown after every workspace page's own help. */
export const PANE_HELP: readonly HelpSection[] = [
  {
    heading: "The agent pane",
    text: "The agent pane chats with coding agents: Claude, GitHub Copilot and Codex, and agents of your own that Settings adds. Each runs on your computer and signs in to its own account; PMT never sees it. The button beside search (Ctrl Alt I) shows it beside any workspace page; ⤢ shows the session over the whole window, and the window button moves it into a window of its own.",
  },
  {
    heading: "Setting up an agent",
    text: "Settings, Agents and tools, sets an agent up. PMT installs it from the Agent Client Protocol's registry into its own folder, with Node when your system has none new enough. An agent signs in its own way: in PMT's Terminal, or in your browser.",
  },
  {
    heading: "Tools and permissions",
    text: "Every session is given PMT's tools, so the agent finds names, problems, Health's findings and the Graph as PMT does, with your unsaved text. What the agent asks permission for shows in the session, and a link it asks to open shows as a card; nothing opens before you choose.",
  },
  {
    heading: "Attachments and sessions",
    text: "What the page has in front of you goes with your prompt as an attachment, and ✕ leaves it out. Follow the agent opens the files it reads in the preview tab and keeps those it edits open. The session menu lists every session, with Rename, Archive and Delete. An edit can be undone from its row, or all of a session's at once from the files button.",
  },
];

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

/** isPage reports whether a name is the route name of a page. */
export function isPage(name: string): name is PageName {
  return name in PAGES;
}

/** isWorkspacePage reports whether a page is one of a workspace's pages. */
export function isWorkspacePage(name: PageName): name is WorkspacePage {
  return "workspace" in PAGES[name];
}

/** WORKSPACE_TOOLS holds the workspace pages that have a tab in the top bar. */
export const WORKSPACE_TOOLS = (Object.keys(PAGES) as PageName[]).filter(isWorkspacePage);

/** TOOL_CHOICES holds the top bar's tabs as the choices of a select. */
export const TOOL_CHOICES: { label: string; value: string }[] = WORKSPACE_TOOLS.map((tool) => ({
  label: PAGES[tool].title,
  value: tool,
}));

/** listShownTools returns the top bar's tabs that the settings show, in order. */
export function listShownTools(): WorkspacePage[] {
  const pages = useSettingsStore().value("appearance.pages").split(",");
  return WORKSPACE_TOOLS.filter((tool) => pages.includes(tool));
}

/** useShown returns whether the kept-alive page that calls it is the one shown. */
export function useShown(): Readonly<ShallowRef<boolean>> {
  const shown = shallowRef(true);
  onActivated(() => (shown.value = true));
  onDeactivated(() => (shown.value = false));
  return shown;
}

/** homeRoute returns the route a workspace opens on: its home page if shown, else the first shown page. */
export function homeRoute(workspaceId: string, homePage: string) {
  const visible = listShownTools();
  const name = visible.find((tool) => tool === homePage) ?? visible[0] ?? "workspace-ide";
  return { name, params: { id: workspaceId } };
}
