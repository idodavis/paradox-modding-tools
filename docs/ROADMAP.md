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

### The Visual view and events

- **Play in the Visual view:** click a window as in the game, so its tabs, buttons and panels change what is shown.
- **Preview values:** a binding such as `[Character.GetName]` shows text or a number you give it, or one read from a
  save game.
- **An event's own window in the Event Graph**, drawn the way the game draws it.
- **Building Chain Graph**: Using the Event Graph page as abase, showing building chains for games like EU5 visually. Showing the building chain and production methods over time all in a visual dagre graph like events.

- **Event Graph lenses**, each a toggle on the same graph: the problems in a chain (a firing that names nothing, a
  link a replaced file breaks, an event nothing fires, a loop), what your mod adds, overrides and cuts against the
  game's chains, what else hooks the same on_action or decision, and a mod's own entry points and dead ends.
- **A loop view of the Event Graph**: a force or radial layout that draws every loop of a chain whole, beside the
  left-to-right view that folds them.

### Health and publishing

- **A field written twice where the game takes one**, such as two `title`s in one event, once a source says which
  fields take a single value.
- **More of the game's logs:** read its other log files, with the mods most likely at fault.

### Beyond the app
- **An LSP server**, so other editors get PMT's hover, completion, definitions and checks.

## Long Term

- **Forms:** edit any entry, such as a trait, a culture or a decision, without writing script.
- **A visual editor** for GUI windows and coats of arms: add, move and resize widgets and layers on the picture.
- **A model viewer** for the games' 3D models.
- **A map editor**, starting with CK3's provinces and titles.
- **A title editor** for CK3's landed titles, as the tree they form.
- **A dynasty viewer**: CK3's characters, houses and dynasties as a graph.
