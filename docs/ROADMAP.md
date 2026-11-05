# Roadmap

Where Paradox Modding Tools is going. This is direction, not a calendar: the order can change, and nothing has a date.
What is already built is in [FEATURES.md](FEATURES.md) and the [release notes](https://github.com/idodavis/paradox-modding-tools/releases).

To push for something, give its [roadmap issue](https://github.com/idodavis/paradox-modding-tools/issues?q=is%3Aissue+label%3Aroadmap)
a 👍, or open a feature request.

## Short Term

- **A GitHub Project for the roadmap and feature requests**, so each item here is an issue on one board you can follow,
  and this page points to it.
- **Health hints for what nothing uses:** definitions nothing refers to, and localization keys nothing reads, shown as
  hints, never errors.

## Medium Term

### Starting a mod

- **Mod Templates/Creator?** An initial screen that asks what you are setting out to do (modify existing game functionality, add something new, translate a mod, make compatability patch between mods, publish a fix for an older mod, etc) and sets up the workspace and the page for it. This is an extension of the workspace wizard where you can craft your own mod with a specific goal in mind.

### Editing

- **Game-update patching:** after a patch, open a mod's copy of a changed game file and carry your changes forward.
- **Tab completions** from Copilot in the editor.
- **Deeper EU5 support**, and more languages in hover cards without a full rescan.

### The Visual view and events

- **Play in the Visual view:** click a window as in the game, so its tabs, buttons and panels change what is shown.
- **Preview values:** a binding such as `[Character.GetName]` shows text or a number you give it, or one read from a
  save game.
- **An event simulator** that steps through an event, its options and what each one fires.
- **An event's own window in the Event Graph**, drawn the way the game draws it.

### Health and publishing

- **A field written twice where the game takes one**, such as two `title`s in one event, once a source says which
  fields take a single value.
- **More of the game's logs:** read its other log files, with the mods most likely at fault.
- **Workshop import:** bring a whole listing made outside PMT into your mod in one step.

### Beyond the app

- **A command line tool** that checks a mod from a terminal or a build script, as Health would.
- **An LSP server**, so other editors get PMT's hover, completion, definitions and checks.
- **IDE bridges** for Claude Code and Copilot CLI, sharing the editor's selection live and showing their edits as
  diffs, once those connections are documented.

## Long Term

- **Forms:** edit any entry, such as a trait, a culture or a decision, without writing script.
- **A visual editor** for GUI windows and coats of arms: add, move and resize widgets and layers on the picture.
- **A model viewer** for the games' 3D models.
- **A map editor**, starting with CK3's provinces and titles.
- **A title editor** for CK3's landed titles, as the tree they form.
- **A dynasty viewer**: CK3's characters, houses and dynasties as a graph.
