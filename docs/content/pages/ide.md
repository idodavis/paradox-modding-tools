---
title: IDE
description: "The editor for a workspace's mods: explorer, search and source control, Monaco editors in groups, hover cards, checks, the Wiki pane."
weight: 3
---

# IDE

The IDE edits the mods of the open workspace. The game's own files sit beside them, **read only**: you read and
compare them, and edit in a mod.

{{< shot caption="IDE: explorer, an editor with a hover card, the Problems panel and the Wiki pane." >}}

The window has four parts:

- the **sidebar**: Explorer, Search and Source control, with the IDE settings (the gear) at the end of its row;
- the **editors**, in one or more groups;
- the **panel** under them: Problems and Git output (**Ctrl J** shows or hides it);
- the **Wiki** pane on the right (**Ctrl Shift W**).

Every edge between them drags to resize.

## Explorer

Two sections: **Workspace** (your mods, in load order, with **+** to add a mod folder) and **Game files** (the
install's script folders, read only, marked with a lock). A grip between them sets their sizes, and the Game files
header folds that section away.

Right-click a file or folder for: Open to the side, New file, New folder, Rename, Delete, Select for compare,
Compare with selected, Compare the two selected, Copy path and Reveal in the file manager. Deleting a mod's own
folder also takes it out of the workspace. **Ctrl Z** in the explorer undoes the last rename, move or delete, and
**Ctrl Y** (or **Ctrl Shift Z**) does it again: a deleted file waits in PMT's trash, which is emptied when PMT
starts, so a delete can be undone until you close the app. A mod's own folder, once deleted, is not brought back. On a mod folder that is not a git repository: **Initialize a git
repository**.

A mod's top folder has its color's bar, and its name in that color too (the game's folder as well) unless **Color
each top folder** is off. Where a mod is a git repository, changed files are marked: added, modified, deleted,
renamed and untracked in their colors, the folders holding them marked, and what `.gitignore` matches dimmed.

Paradox's own kinds of file have icons of their own, in the explorer and on the tabs: script, localization, GUI, a
mod's descriptor, meshes, assets, animations, textures, shaders, sprites (`.gfx`), fonts, sound banks and audio, video,
particles, map data and settings.

## Editors

Files open in tabs. A dot marks unsaved changes; a lock marks a game file. Right-click a tab for Save, Save all,
Close, Close the others, Close all, Split right (**Ctrl \\**), Split down, Compare with the game's copy, Select for
compare, Compare with selected and Copy path. Drag a tab onto another tab to reorder it, or onto an editor's edge to
split there; groups nest in rows and columns, and the same file can be open in several groups. When there are more
tabs than fit, the strip scrolls (with the mouse wheel too).

Above the editor, the path to the file and, at the caret, the blocks it is in.

A file with views switches them at the end of the tab strip, and **Ctrl Shift V** cycles through them: a Markdown file's
**Raw** and **Rich** (Raw is its text, Rich the same editor as Publish's description), and a GUI file's (**.gui**)
or a coat of arms file's (`common/coat_of_arms/coat_of_arms`, EU5's under `main_menu`) **Code**, **Split** and
**Visual**. The strip ends with the switch of the pane beside the editor: a GUI file's **Widgets** panel, a coat of
arms file's **Layers** panel and, in the group at the top right, the **Wiki** pane, the one shown tinted; click it
again to close it. The pane opens beside the editor, under the group's tabs and path; the panel is open while the file
is drawn (Visual or Split) until you close it. Both kinds open in Split; the IDE settings' Visual view tab picks what
each opens in and whether its panel opens with the picture, and the Editor tab what Markdown and SVG files open in.

The sidebar folds to a strip of its tabs when you click the open tab's icon again or drag it shut, and a strip icon
opens it again. A single click on a file in the explorer opens it in a preview tab, in italics, which the next single
click replaces; a double click on the file or its tab, an edit, or opening it from anywhere else keeps the tab.

Visual draws the file's window or widget from the game's own data: its types and templates, textures, fonts and
localization, at the resolution you pick (**1280 × 720** up to **3840 × 2160**, the ultrawide **3440 × 1440** among
them) and in the workspace's language, at the game's 100% interface scale. It is a preview, not the game: each widget is
drawn by the properties the file writes, each meaning in the browser what the game's wiki says it means (a box centers
its children and spreads them out unless an `expand` pushes them aside), with no game running, so a vanilla window is
a stress test of it, not a promise that it looks as it does in the game.

The toolbar's picker chooses what of the file to draw, grouped as windows, types, templates, text icons and formats,
each with the line it starts on; a file opens on its first window. A type, template, text icon or format is drawn by
itself, at its own size. **Bounds** outlines every widget. **States** lists the conditions the window shows its widgets
on, grouped by call: the tabs of `IsTabSelected('a')` and `IsTabSelected('b')` are one group, a flag and its `Not(...)`
the same group. Each group is drawn at one of its values, its first unless you pick another, or at every value or none,
so the window is drawn in one state instead of every tab and popup at once. Two kinds of group are drawn at none until
you pick a value: the pages of a column after its first, where each of its widgets shown on a condition has a group of
its own (EU5's government tabs), and a group that shows only widgets outside the window, a side window or tool the game
opens on a click. The resolution and
Bounds are also in the IDE settings' **Visual view** tab, with **Rows per list**: how many rows a data
model's list is drawn with, one by default, since the game fills it. A window is drawn on a screen of the resolution,
framed, with its size at the corner, where the game puts it: an EU5 lateral view in the left panel under the top bar,
a Victoria 3 panel under its top bar, beside the side bar.
Drag the floor to pan, or use the wheel alone; zoom with **Ctrl** and the wheel, or with **−** and **+** in the bar
under the floor, whose percentage goes back to **100%** when clicked. **Fit** shows the window whole, or an entry drawn
alone with whatever hangs outside its own box, and keeps it so as the pane changes size until you pan or zoom.

Click a widget to select it: it is outlined, with a chip that gives its type, key, name and size, and the widget under
the pointer is outlined the same way in the link color. A click picks the smallest widget drawn under the pointer, the
topmost of equal ones, whatever the game lets the mouse do: a text in its frame, a button in its panel; a click on a
widget's background selects the widget, and one on the floor around the picture, or **Esc**, selects nothing. A widget
that draws nothing of its own, a box or a spacer, is selected from the panel's path, with the Up key or from its code,
and a right-click lists every widget drawn under the pointer. Split shows
the code beside the picture, where the caret and the selection follow each other and the selected widget's lines are
marked. Right-click a widget for its menu: the widgets drawn under the pointer, to select one under another; select
its parent, hide or show it, show everything hidden, go to its code, open the type it is made from, and copy its name
or its path. With the picture
focused, the keys move the selection:

| Keys | Does |
|------|------|
| **↑** / **↓** | Select the parent / the first child |
| **←** / **→** | Select the sibling before / after |
| **H** / **Shift H** | Hide or show the selected widget / show everything hidden |
| **Enter** | Go to the selected widget's code |
| **F** | Fit the selected widget |
| **Esc** | Select nothing |

In Split the code and the picture point at each other. The pointer on a widget's code outlines the widget in the
picture, and the pointer on a widget tints its lines in the code. An eye before each widget's first line hides it until
the app closes, its code dimmed while it is hidden; after that line comes the size the picture lays it out at, in orange
where it is nothing, with how many rows a data model draws. With the caret on a layout word, the picture draws what it
does: a margin's bands with their widths, or an anchor's two points, the parent's and the widget's own, joined by the
offset its position adds. What the game's files did not give is squiggled on the first line of the widget that asked for it, its
sentence on the hover card, and marked on the widget with an orange badge.

The bar under the floor says how long the render took, or that it is drawing again, how many things the game's files
did not give, and how many widgets you hid. Click either count for its list, where a row selects its widget: the
templates, textures and fonts that were not found, or the hidden widgets, each with an eye that shows it again, and
**Show all**. A render that fails says why, with **Open in code** and **Try again**, and an entry with nothing to draw,
a type with no size and no texture of its own, says so on the floor.

The Widgets panel shows the way down to the selected widget from the window, each step a chip: pointing at a chip
outlines its widget in the picture, and a click selects it. Under it the widget with
**Go to code**, and a **Shown** switch that hides the widget and everything in it until the app closes. A widget whose
`visible` is a binding says **Shown when** and the binding, with each group of conditions it reads and the value that
group is drawn at, which you can change there as in **States**. Then its properties as written, grouped by who
wrote them: the widget, then each type or template it comes from, each group linked to where it is written and each
property a click away from its own line. With nothing selected, the panel says how to select a widget.

Nothing the game would decide at runtime is simulated, and nothing asks you to answer it: a widget whose `visible` is a
binding is drawn in the state **States** picks (hide it to see the window without it), and a binding in a text is a
value the game fills, drawn `[…]` in the code's binding color, so it widens nothing; the panel's properties show it as
written. A game
concept's link is drawn as the game shows it: the concept's name (`[faith|E]`), or the text a `Concept('x', 'text')`
link writes. A texture that is a binding draws the file the scan
finds for it: the entry its call or its widget's data context names (a map mode's icon), by the file the entry's icon or
texture names. Where none is found, and for a portrait, it shows a placeholder. A tooltip is not drawn in its window,
and a background lies under what its widget holds. A box takes no room for a widget that is not drawn, as in the game,
so the tab drawn takes the place the others would; in Victoria 3 a flow does the same, and elsewhere a flow or a
container keeps the room unless it writes `ignoreinvisible = yes`, as the CK3 wiki documents. A frame of a sprite sheet
is drawn alone, its slices included.

The status bar shows the open files' errors and warnings, the git branch, then the caret's line and column, the
indentation, the file's kind and its encoding (UTF-8, or UTF-8 BOM).

The editor is [Monaco](https://microsoft.github.io/monaco-editor/), the editor of VS Code: find and replace (**Ctrl F**,
**Ctrl H**), folding, the minimap, indent guides, colored bracket pairs, multiple cursors, and the line at the top
that names the block you are in. The IDE settings turn the minimap, folding, guides, bracket colors and shown
whitespace on or off, and pick the editor's font and line height.

### Coats of arms

A coat of arms file's Visual view draws each coat as the game's shaders color it: its pattern in its colors, each
emblem's instances moved, sized, turned and flipped where they write it, masked to the pattern's regions where they
write a mask, the later drawn over the earlier and a larger `depth` behind a smaller, each `sub` drawn from its parent
coat with its own colors over the parent's, and in Crusader Kings III the cloth the game shades every coat with. The
picker lists the file's coats; a coat written as another's name draws that coat. The toolbar's **Frame** cuts the coat
to a shape the game's windows use (Crusader Kings III's title shield and dynasty frame, as your mods leave them); a flag
of Victoria 3 or Europa Universalis V is its field. A value the game picks from a list is not drawn and is said under
the floor.

The **Layers** panel lists the coat, its pattern, each emblem with its instances and each sub, an eye on each to hide it
until the app closes; pointing at a row outlines its layer, a click selects it, as clicking it in the picture does.
Under it the selected layer's properties, edited where they are written: a texture from tiles of its folder drawn in
the layer's colors, a color from the game's named colors with their swatches, a position, scale or offset as two
numbers, a rotation or depth as one, a mask as its three regions. The buttons beside the layer add an instance to an
emblem or an emblem to the coat, after the last of its kind and written as the file writes its others, remove it, or
draw an instance in front of the next or behind the one before. Drag the selected instance in the picture to move it.
Every edit is a change of the file's text, as typing is: **Ctrl Z** undoes it, **Ctrl S** saves it, and source control
sees it. A game's own file is drawn but never changed; **Edit a copy** writes its coat into one of your mods under the
same name, where it replaces the game's.

**Copy** puts the coat's script on the clipboard for the game's coat of arms designer, and **Paste** replaces the coat
with the script the designer copies. From anywhere, the command palette's **New coat of arms** and **Paste a coat from
the game**, or **New coat of arms** on a coat of arms folder in the explorer, ask for a mod and a name and write the
coat into that mod's last coat of arms file, or a new one in the folder its game keeps them in, then show it.

### Language features

On Paradox script, localization and GUI files:

| Feature | How |
|---------|-----|
| Hover card | Rest the pointer on a word (see below) |
| Completion | While you type, or **Ctrl Space**; the likeliest completion shows in grey, **Tab** takes it |
| Signature help | What an effect or macro takes, while you fill it in |
| Go to definition | **F12**, or **Ctrl+click**; on a definition, its references |
| Peek | **Alt F12** shows the definition in place; each file in it says which mod (or the game) it is in, with that origin's color |
| References | **Shift F12** in place; **Shift Alt F12** lists them all in the sidebar's Search |
| Rename | **F2** renames a name everywhere the mods write it, each file left unsaved to check; refused, with the reason, where the game defines or uses it, two mods define it, or the new name is taken |
| Format | **Shift Alt F** |
| Folding | By the file's blocks |
| Quick fixes | Add a missing UTF-8 BOM; create a missing localization key |
| Inlay hints | A date in the mod's own calendar era, where the mod has one |
| Colors | A swatch before every color a file writes: a tagged block (`rgb { }`, `hsv { }`), an untagged one under a key ending in `color` (`fontcolor`, a culture's `color`; not a portrait gene such as `hair_color`), a named color's entry and a coat of arms' color (`color1 = { 82 10 10 }`); click it for a picker that writes the color back as the block wrote it |
| Event Graph | **Ctrl Shift G** draws the graph from the event at the caret |

Right-click in the editor for the same actions, Show in Health, Format document, Cut, Copy, Paste and the command
palette.

### The hover card

Rest the pointer on a word for half a second. The card says what the word is, its scope and what it takes and
returns, its localized name and the game's description of it, and last where it is defined and what it replaces:
each mod or the game once, with each of its files under it (a file holding many places says how many), each a link
to the place. A long value wraps, and a card taller than the room beside the word scrolls. A problem at the word shows at the top of the card. The card stays open while the pointer
is on it, and any click closes it. When your `script_docs` are older than the game, the card says
**outdated script_docs**; see [three kinds of docs]({{< relref "/concepts/docs" >}}).

### Checks

Problems show as squiggles in the editor and as rows in the **Problems** panel (filter them as you type, or open them
in [Health]({{< relref "/pages/health" >}})). The checks, each on or off in the IDE settings' **Checks** tab:

| Check | Finds |
|-------|-------|
| `unresolved-ref` | a word naming an entry that nothing defines |
| `wrong-value-type` | a value of another type than the key takes |
| `wrong-scope` | a command written in a scope it does not run in, or a variable read where none is stored; the message says how the scope was found (a link, `root`, `prev`, a saved `scope:x`) |
| `duplicate-definition` | a name the workspace defines twice |
| `inject-without-target` | an INJECT or REPLACE with nothing loaded before it |
| `missing-bom` | a localization file without a UTF-8 byte order mark |
| `loc-header-mismatch` | a localization file whose header names another language than its name |
| `wrong-localization-folder` | a folder spelled `localisation`, which the game never reads |
| `unknown-event` | an event id no event defines |
| `wrong-declared-value` | a word outside the words the key takes |
| `missing-file` | a path that exists nowhere in the install or the mods (off by default) |
| `missing-key` | a key the game's docs say every entry writes, left out (off by default) |

Game files are never checked. To silence a line, write a comment:

```
# pmt:ignore
# pmt:ignore unresolved-ref, missing-bom
```

It turns the checks off on its own line and the next one; with codes, only those.

## Search

**Ctrl Shift F** opens Search in the sidebar: text or a regular expression, with Match case and Whole word, files to
include and exclude (`common/**, *.txt`), and the folders to search. It searches as you type, once you pause
(**Enter** searches at once). Hits list under the search as they are found, by file, each named with its mod (or the
game) and folder as a peek names it; once 500 hits show, the files after are listed folded. Click a file to fold or
unfold its hits, and a hit to open its line.

**Replace all** asks first, then writes the replacement into the mods' files on disk; a file open in an editor is
changed there instead, unsaved. Game files are never changed.

## Compare

Compare a mod's file with the game's copy at the same path, or any two files: **Select for compare** on one, then
**Compare with selected** on the other (from a tab or the explorer). The two open side by side or in one column,
the differences marked, at the first change.

## Source control

The sidebar's third view runs the **git installed on your computer**; without git on the PATH it says so. It
works on every mod of the workspace that is in a git repository.

- **The repository** shows at the top. With several, it is a menu that follows the file you are editing, and lists
  the mods in no repository: **Initialize** makes one (`git init`; your first commit is yours to write).
- **The branch** button opens the branch menu: switch branches, or type a name to create one; create one from
  another branch; check out a tag or commit; the local and remote branches; Fetch, Pull, Pull from, Push, Merge
  into the current branch, **Remotes** (each remote's address, changed or removed, and a new one added) and
  Delete. Beside it, how many commits are to push and to pull; clicking it pulls then pushes (a branch with no
  upstream shows **Publish**; a repository with no remote, such as one made here, asks first for the address of an
  empty repository to push to, and adds it as `origin`).
- **The message** and **Commit**; its menu has Commit and push, and Amend the last commit. **Ctrl Enter** commits.
- **Merge conflicts**, **Staged** and **Changes**, each file with its letter: A added, M modified, D deleted,
  R renamed, U untracked, ! in conflict. A row opens the file's diff; its buttons open the file, stage, unstage or
  discard it. Discarding asks first, and deletes a file git does not track yet.
- **The graph**: the current branch and its upstream, branches and merges drawn as lanes, with the branches and
  tags at each commit. Click a commit to see the files it changed; click a file to open what the commit changed in
  it. The graph is a section of its own under the changes: drag its top edge to resize it, click its header to fold
  it; older commits load as you scroll to its end.

**Diffs** open as editor tabs, side by side or in one column. A file's changes compare its staged copy with the file
itself, which you can edit right there. The **+** in the gutter stages one block, the arrow between the sides
discards it (an unsaved edit you can undo); **Stage file** and **Discard** act on the whole file. A staged file's
diff has a **−** to unstage a block.

**Merge conflicts** are resolved in the editor: each conflict's two sides are shaded, and above it **Accept
current**, **Accept incoming**, **Accept both** and **Compare** (both sides of the file side by side). Save, then
stage the file to mark it resolved.

Every git command, and what git said, is listed in the panel's **Git output**. Git never asks for a password here:
a pull or push that needs one fails with git's message, so set up your credential helper or SSH key first.

## Wiki

The Wiki pane opens beside the editor of the main window's top-right group, under its tabs and path, from the
**Wiki** button that ends that group's tab strip; a drawn file's panel (Widgets, Layers) takes its place there while
the panel is open. It shows the community wiki's pages for the file you are editing, as tabs ("Suggested for event
files"), and the page's contents beside it (drag their edge to resize them; the button beside them hides them). Its
search keeps the pages it finds and marks its words in the page, as Patch Notes' find does: **Enter** and **Shift
Enter** for the next and previous match, and **Ctrl F**, outside the editor, puts the cursor in it. It follows the
open file unless you turn that off. When it is closed and the wiki has pages for a new kind of file, a toast says so;
the IDE settings' Wiki tab can hide that toast for an hour, until restart, or for good.

This is the community wiki, not `script_docs` and not the install's `_*.info` files. See
[three kinds of docs]({{< relref "/concepts/docs" >}}).

## Windows

Parts of PMT open in windows of their own, to put on another screen:

- **A page.** Right-click the Event Graph's, Health's, Patch Notes' or Publish's tab in the top bar: **Open in a new
  window**. The tab then shows a window mark, and clicking it brings that window to the front; its menu has **Show
  window** and **Back into the main window**. Anything that would open the page (Open in Event Graph, Show in
  Health, a link) goes to its window.
- **An editor.** A tab's menu, **Move into a new window**, or the explorer's menu on a file, **Open in a new window**.
  The tab moves with its unsaved changes. The window closes when its last tab does; **Dock back into the main
  window** brings its tabs back.
- **Game files.** The window button on the explorer's Game files header. The game's open files go with it, and any
  game file you open later (a definition, a reference, a search hit) opens there. The explorer shows **Bring back**
  and **Show window** in its place.
- **A folder or a mod.** The explorer's menu on a folder, **Open in a new window**: its own tree and editors, which
  take the folder's open files and every file of it you open later. In the main window's explorer the folder shows a
  window mark; click it to bring that window to the front, or use the folder's menu, **Close the window**.
- **The panel.** The window button in the panel's header. It shows the problems of every window's files, and a row
  opens its file where it is open.
Drag a tab onto another window's tabs or editor to move it there: a line between the tabs, or a highlighted half
of the editor, shows where it lands. Drop it outside every window for a window of its own. The Game files window
takes only game files.

A game file a mod replaces says so on its path's row, **Replaced by** the mod, with **Compare**.

A file is open in one window at a time: opening it from another brings up the window that has it. Every window shows
the main window's workspace and follows it to another; editor windows first come back into the main window, so you
are asked about their unsaved changes. Closing any window but the main one moves what it shows back into the main
window; closing the main window closes PMT, after asking about any window's unsaved files. The status bar counts the windows, and its menu brings any of them to the
front. The tabs of every window come back in the main window the next time the workspace opens.

## IDE settings

The gear at the end of the sidebar's row. Its tabs:

- **Editor**: font (any font installed, by name), line height (Compact or Roomy), editor text size, word wrap,
  closing brackets and quotes, the minimap, indent guides, bracket pair colors, shown spaces and tabs, indentation;
- **Explorer**: explorer and panel text size, row density (Compact, Default or Relaxed), sticky folders, the Game
  files section, coloring each top folder, following the open file;
- **Language**: folding, hover cards, the completion list while typing, predicted text, signature help;
- **Checks**: each check on or off;
- **Wiki**: following the open file, and the suggestion toast.

They apply to every workspace. Each tab can be reset to its defaults.

## Keyboard

| Keys | Does |
|------|------|
| **Ctrl K**, **Ctrl Shift P** (and **F1** in the editor) | The command palette: files; `>` commands (Reload the window among them); `@` names in this file; `#` events, on_actions and localization keys |
| **Ctrl P** | Open a file by name |
| **Ctrl S** | Save |
| **Ctrl Shift E** / **Ctrl Shift F** | Explorer / Search |
| **Ctrl J** | The panel |
| **Ctrl Shift W** | The Wiki pane |
| **Ctrl \\** | Split the editor right |
| **Ctrl Shift V** | Cycle a file's views: Code, Split, Visual (Raw, Rich for Markdown) |
| **Ctrl F** / **Ctrl H** | Find / replace in the file |
| **Ctrl Z** / **Ctrl Y** in the explorer | Undo / redo a rename, move or delete |
| **F12**, **Alt F12**, **Shift F12**, **Shift Alt F12** | Definition, peek, references, all references |
| **Ctrl Shift G** | The Event Graph from the event at the caret |
| **Shift Alt F** | Format the file |

Open tabs come back when the workspace opens again, in the groups they were in, unless **Reopen the IDE's tabs** is
off in [Settings]({{< relref "/pages/settings" >}}).
