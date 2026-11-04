# Roadmap

Where Paradox Modding Tools is going. This is direction, not a calendar: the order can change, and nothing has a date.
What is already built is in [FEATURES.md](FEATURES.md) and the [release notes](https://github.com/idodavis/paradox-modding-tools/releases).

To push for something, give its [roadmap issue](https://github.com/idodavis/paradox-modding-tools/issues?q=is%3Aissue+label%3Aroadmap)
a 👍, or open a feature request.

## Now

- **Health hints for what nothing uses:** definitions nothing refers to, and localization keys nothing reads, shown as
  hints, never errors.
- **A Workshop title and description per language** in Publish.

## Next

- **Play in the Visual view:** click a window as in the game, so its tabs, buttons and panels change what is shown.
- **Preview values:** a binding such as `[Character.GetName]` shows text or a number you give it, or one read from a
  save game.
- **An event simulator** that steps through an event, its options and what each one fires.
- **An event's own window in the Event Graph**, drawn the way the game draws it.
- **Game-update patching:** after a patch, open a mod's copy of a changed game file and carry your changes forward.
- **Two workspaces open at once.**
- **Workshop import:** bring a whole listing made outside PMT into your mod in one step.
- **More of the game's logs:** read its other log files, with the mods most likely at fault.
- **Deeper EU5 support**, and more languages in hover cards without a full rescan.
- **A command line tool** that checks a mod from a terminal or a build script, as Health would.
- **An LSP server**, so other editors get PMT's hover, completion, definitions and checks.
- **Tab completions** from Copilot in the editor.
- **IDE bridges** for Claude Code and Copilot CLI, sharing the editor's selection live and showing their edits as
  diffs, once those connections are documented.

## Later

- **Forms:** edit any entry, such as a trait, a culture or a decision, without writing script.
- **A visual editor** for GUI windows and coats of arms: add, move and resize widgets and layers on the picture.
- **A model viewer** for the games' 3D models.
- **A map editor**, starting with CK3's provinces and titles.
- **A title editor** for CK3's landed titles, as the tree they form.
- **A dynasty viewer**: CK3's characters, houses and dynasties as a graph.
