# lang/schema

Everything the sources say about one game version's script, in one tree of positions, and the
walker that reads script by the language rules in `game/lang-rules.go`.

| File | Holds |
|---|---|
| `schema.go` | the types, `MayAccuse`, `Link`, `DatabaseNamed`, `DatabasesWhere`, positions (`Child`, `EnsureChild`, `positionName`) |
| `build.go` | `Build` and one function per source, in trust order |
| `walk.go` | the walker: `WalkBlock`, `EntryContext`, `ReadKey` and the decisions each key and block makes |
| `entries.go` | the entry detector (`FindEntries`) |
| `evidence.go` | vanilla's evidence and the value types it gives |
| `dumps.go` | the reader of the console dumps |
| `docs.go` | the reader of docs, wiki examples and dump usages |
| `shipped.go` | the shipped backup schemas (`LoadShipped`) |

## Sources and claims

Sources write in trust order: the registry (`game`'s tables), the dumps, vanilla, the docs, the
wiki. A claim is a `Claim[T]`, a value and the source that stated it; `SetFirst` keeps the first, so a
later source only fills what an earlier one left empty. The dumps, docs and wiki **declare**
(`Source.Declared`). Vanilla gives **evidence** (`Position.Evidence`: how often and in what forms it writes a
position), and a value type only where nothing declares one.

`MayAccuse` is the one rule for when a claim may accuse a user's code: a declared source states it,
that source is not stale, and vanilla's evidence at the position does not contradict it. A docs or
wiki claim, and any declared word list, also needs vanilla to write the position. The registry and
vanilla never accuse.

`Sources` records where each source came from: dumps `yours` (stale when older than the install's
last update) or `none`; docs `install`; wiki `yours` or `none` (never stale).

## Positions

A **position** (`Position`) is a place in a database's entry tree or a command's arguments. A written
key becomes a position only by its written form: a number is `<number>`, a date `<date>`, a
`prefix:x` word `<prefix:>`, anything else itself (`positionName`). `Child(name)` finds a position, nil
when missing; `EnsureChild(name)` makes it when missing. `DeclaredBy` is the first declared source that
writes a position: the dumps anywhere, the docs and wiki only where vanilla writes it too.

## Build

1. **Databases.** Each script file's folder is a database (`folder`: the longest registry row at
   or above the path, named without `common/`). The game reads a `common/` folder whole, so a
   subfolder with no row is its highest ancestor's database (`decisions/dlc_decisions` is
   `decisions`); folders are declared shallow first so a subfolder finds it. Its entry block is the row's `Body` kind, else a
   fields block; each of the row's `Refs` is claimed as a reference to the database itself.
   `Callees` maps each `CalledIn` word, and `game.AnyCall`, to the databases a call by it resolves
   in.
2. **Dumps** (`readDumps`), or a shipped schema's dump part.
3. **Docs and wiki examples**, normalized and parsed once: a doc into its own folder's database; a
   database's script examples from the wiki, handed in by the scan (none for a history database).
4. **Fields**: every key name each database's docs and wiki write, for the entry detector.
5. **Vanilla**: first `ScriptedEntries`, vanilla's scripted entries by calling word and name, from the
   scripted folders alone, so a call is never observed as an argument. A `ScriptedEntry` names its database
   and, where that database takes parameters, holds the position its calls' blocks read at; a call
   of any other entry reads at its database's entry. No position is held twice, since the cache
   (gob) would write a shared tree once per reference. Then each install file's entries (`FindEntries`), a database made for
   any new name, and its evidence (`recordEvidence`).
6. **Value types and prefixes** (`inferValueTypes`), before any docs claim. `Prefixes` maps each link
   prefix to the database the names after it are in: the name commands' databases first, then
   vanilla's, then the database a link's own name names.
7. **Docs' and wiki's claims** (`applyDocClaims`), where vanilla writes each key.

## The walker (`walk.go`)

`WalkBlock(visitor, holder, statements, in)` reads a block's statements at a `BlockContext` (the position, the
kind of block, its calls' word, its scope, whether it is in a trigger or an entry, the command or
call whose block it is). Each key becomes a `Reading` by `ReadKey(in, statement, name, create)`:

- **What the key is.** The block kind's steps (`game.Blocks[kind].Keys`) are tried in order: a
  vanilla scripted entry's name (a call), a declared position, a declared command, a value operator,
  a link, a position vanilla writes, the position itself, a call. In a dynamic description the
  shape decides instead (`game.DynamicDescription`): a marker holds the shape again, `trigger` holds
  triggers, the leaf reads at the position the description belongs to.
- **What its block holds.** A link's block reads where the link is written. A command's block is
  `commandBlockKind`: a value block, an arguments block, the enclosing kind, or its own. An operator's is a value block or
  arguments. Otherwise the position's claim decides, else vanilla's evidence (`blockKindOf`): every key a
  value operator (its `if`s aside) is a value block, only triggers a triggers block, only effects an
  effects block, keys and no command a fields block. `game.Structurals` fix random_list's branches
  and switch's cases; a call starting with an iterator prefix iterates that scripted list
  (`game.ListIterators`). An arguments block reads no links: a key it neither declares nor sees in
  vanilla is a plain position, never a scope change.
- **Its scope.** `blockScope` decides, in order: a declared scope, a link or iterator's target, none for a list or bare target, the written scope for value, description and command blocks; a scope that runs no triggers
  and no effects is unknown.

A `Visitor` is one use of the walker: `Create` makes the positions it reads; `Entry` says which
statements start a database's entry; `Names` gives a key's alternatives; `Visit` sees each reading.
Four readers use it: `recordEvidence` (vanilla, creating), `applyDocClaims` (docs and wiki), `applyUsage` (the dumps'
usages, creating) and `index.Walk` (a file's records).

## Dumps (`dumps.go`)

`readDumps` reads the script_docs `.log` files and the dump_data_types `.txt` files, by base name,
stated by `SourceDumps`. `MissingDumps` lists the dump files an inventory lacks.

| File | Fills |
|---|---|
| `event_scopes.log` | `Scopes`: whether each stores variables, runs triggers, runs effects |
| `event_targets.log` | link `Commands` (output scope, `Global`, `Wild`), `Saved` scopes |
| `triggers.log`, `effects.log` | `Commands`: scopes, target, traits, usage |
| `modifiers.log` | `Modifiers` |
| `on_actions.log` | `OnActions` |
| `custom_localization.log` | `CustomLoc` |
| `data_types_*.txt` | `DataTypes`; functions with no type belong to `Global` |

A name read from several logs is one `Command` with every kind: it runs where any reading runs, and
keeps a trigger or effect reading's blurb over a link's, a plain link's over a global link's. A
`Wild` link is a pattern (`value`, `compare_value`) that no written name reads as. Names are held
lower-case.

A usage is read once every command is declared, into the command's `Args` (`applyUsage`): every
statement named after the command, at any depth (CK3 `any_vassal` writes two), else its one
statement under another name (Vic3 `set_global_variable`'s is `set_variable = { … }`), else a bare
list of the arguments. Every key in that statement's block is an argument, whatever else its name
is; `<count=num/all>` is the argument `count`. A block that lists a block placeholder takes the kind
its last word names (`game.Placeholders`: `<triggers>`, `<display_triggers>`, `<interface effects>`),
at any level; a usage whose blocks write no placeholder takes arguments only. `Lists` marks a
command whose blurb opens with `game.DumpWords.Iterates` and that has no target. A modifier named
with `$TYPE$` parts keeps them as a glob (`Modifier.Pattern`).

## Entries and evidence (`entries.go`, `evidence.go`)

`FindEntries(file, parsed, found)` is the one entry detector, for the schema and the index alike. It
hands on each `Entry` (database, name, mode word, statement, `Local`) in source order:

| Where | Rule | Registry field |
|---|---|---|
| any depth | a keyword before an assignment declares it: a file type's in `<ext>.<word>`, a folder's in its database (local to its file when written in another folder) | `FileType.Keywords`, `Folder.Keyword` |
| `.gui`, `.asset`, `.font` | a block named by its name key, a `NameKeyOf` key or the widget key | `FileType` |
| a database's top | each assignment is an entry named by its key; wrapper keys hold top statements; only event ids in events; a `NameKey` folder names each top block by that key; `Unnamed`, `ByFile` and history rows as their fields say | `Folder` |
| inside an entry | keys at a nested row's path define entries of their own; a key the docs write there, or a declared command or link, is a field | `Layout.Nested` |
| tables and sound lists | each line's name (`tableEntries`) | `Layout.MapTable`, `inventory.List` |

A mode word (`REPLACE:x`) is stripped from every name (`GameInfo.Mode`).

`recordEvidence` reads each entry with the walker, creating positions, and records only evidence: `Evidence.In`
(how many blocks of the parent hold the key), `Blocks`, `Forms` (and `FormCommand` on a block that
holds a declared command) and `Values` (each word, with counts). An entry inside another is observed
only under its own database.

`inferValueTypes` gives each position that no claim types (in entry trees, command arguments and scripted
entries' parameters), and whose words all name one script database's entries (or, for GUI trees, one
GUI database's), a reference to that database; else a loc key type when every word is a loc key.
Numbers never count, and a history database's names, which are other databases', name none. It then fills `Prefixes` from the names vanilla
writes after each link prefix. Vanilla's types never accuse.

## Docs, wiki examples and usages (`docs.go`)

`normalize` turns the lines of a doc, wiki example or usage into script and keeps every other line
as a comment: lines shaped like script stay code (braces, `key =`, `key {`, field bullets); the
rewrite table quotes placeholders, reads `<key = value>` as that argument, and moves a `(note)` after
a value into the line's comment; blocks a text leaves open close by indentation. An empty comment
line ends a paragraph, so the comments above it lead no statement; an upper-case line is a heading.
`usageLines` splits a usage line before each statement; `markdown` blanks headings and comments list
lines that are no field.

`applyDocClaims` reads a doc into its database's tree with the walker. A key at a position vanilla writes
takes the statement's claims; a list word's claims go to the position holding it. A key vanilla
writes nowhere below where it stands is a **stand-in** (an example entry's name, `<key>`,
`my_phase_key`): its block reads in its place where vanilla writes its keys there, else at each
position one level below that writes them (each real phase, each real guest), else nowhere; at the
top its claims are the entry's, unless it is a value where vanilla writes every entry as a block.
A doc never makes a position.

The phrase catalog (`phraseRules`) reads a statement's value as written and its comments into claims.
A comment line is the blurb unless the rows read all of it, or a row that claims nothing reads it.

| Row | Reads | Claim |
|---|---|---|
| default | `default is x`, `defaults to x`, `default = x`, `default: x` | `Default` |
| optional, required | `optional`; `required`, `required if/when …` | `Required` |
| root | `root = x` opening the text | `Scope` |
| no claim | `scope:x = …`, `Brief:`, a line ending in `:` | — |
| types | a type word opening the text or a parenthesis: `int`, `float`, `number`, `scripted value`; `bool`, `yes/no`; `date`; `loc key`; `string`, `text`, `scope` only alone | `Value` type |
| event, on_action | `event id`; `on_action` | `Value`: a reference to the events or on_actions database |
| block kinds | `trigger(s)`; `effect(s)` | `Block` |
| file | one quoted word with a slash | `Value` file |
| items | `- item: blurb` lines | `Value.Words` |

A value written `a/b/c` that no row reads whole is read word by word: a type word gives its type,
the rest are its words (`num/all`: a number or `all`). A placeholder no row reads names the database
`DatabaseNamed` gives. A doc's literal states its form (`expiration_days = 3650` a number), and a doc's
block whose kind nothing names states only that the key takes a block (`WordBlock`). In a usage,
an argument written as a block placeholder word (`on_created = effect`) runs in an unknown scope.
