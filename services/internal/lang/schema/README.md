# lang/schema

Everything the sources say about one game version's script, in one tree of positions, and the
walker that reads script by the language rules in `game/lang-rules.go`.

| File | Holds |
|---|---|
| `schema.go` | the types (`Source`, `Said`, `Key`, `Command`, `Database`, `Schema`), `Speaks`, `Link`, `Named`, `Flagged`, positions (`At`, `Below`, `collapse`) and `Build` |
| `read.go` | the walker: `Read`, `Start`, `Step` |
| `dumps.go` | the reader of the console dumps |
| `vanilla.go` | the entry detector (`Entries`), vanilla's evidence (`observe`) and the value types it gives (`typeValues`) |
| `template.go` | the reader of docs, wiki examples and dump usages: the normalizer, the phrase catalogue, `join`, `usage` |

## Sources and claims

Sources write in trust order: the registry (`game`'s tables), the dumps, vanilla, the docs, the
wiki. A claim is a `Said[T]`, a value and the source that stated it; `Set` keeps the first, so a
later source only fills what an earlier one left empty. The dumps, docs and wiki **declare**
(`Source.Declared`). Vanilla gives **evidence** (`Key.Seen`: how often and in what forms it writes a
position), and a value type only where nothing declares one.

`Speaks` is the one rule for when a claim may accuse a user's code: a declared source states it,
that source is not stale, and vanilla's evidence at the position does not contradict it. A docs or
wiki claim, and any declared word list, also needs vanilla to write the position. The registry and
vanilla never accuse.

`Sources` records where each source came from: dumps `yours` (stale when older than the install's
last update) or `none`; docs `install`; wiki `yours` or `none` (never stale).

## Positions

A **position** (`Key`) is a place in a database's entry tree or a command's arguments. A written
key becomes a position only by its written form: a number is `<number>`, a date `<date>`, a
`prefix:x` word `<prefix:>`, anything else itself (`collapse`). `At(name)` finds a position, nil
when missing; `Below(name)` makes it when missing. `Written` is the first declared source that
writes a position: the dumps anywhere, the docs and wiki only where vanilla writes it too.

## Build

1. **Databases.** Each script file's folder is a database (`folder`: the longest registry row at
   or above the path, named without `common/`). Its entry block is the row's `Body` kind, else a
   fields block; each of the row's `Refs` is claimed as a reference to the database itself.
   `Callees` maps each `CalledIn` word, and `game.AnyCall`, to the databases a call by it resolves
   in.
2. **Dumps** (`readDumps`), or a shipped schema's dump part.
3. **Docs and wiki examples**, normalized and parsed once: a doc into its own folder's database; a
   database's named wiki pages' `<pre>` blocks (none for a history database).
4. **Fields**: every key name each database's docs and wiki write, for the entry detector.
5. **Vanilla**: first `Macros`, vanilla's scripted entries by name, each with the position its
   calls' blocks read at (the entry's parameters), from the scripted folders alone, so a call is
   never observed as an argument. Then each install file's entries (`Entries`), a database made for
   any new name, and its evidence (`observe`).
6. **Value types and prefixes** (`typeValues`), before any docs claim. `Prefixes` maps each link
   prefix to the database the names after it are in: the name commands' databases first, then
   vanilla's, then the database a link's own name names.
7. **Docs' and wiki's claims** (`join`), where vanilla writes each key.

## The walker (`read.go`)

`Read(reader, holder, statements, at)` reads a block's statements at a `Place` (the position, the
kind of block, its calls' word, its scope, whether it is in a trigger or an entry, the command or
call whose block it is). Each key becomes a `Step` by `Step(at, statement, name, create)`:

- **What the key is.** The block kind's steps (`game.Blocks[kind].Keys`) are tried in order: a
  vanilla scripted entry's name (a call), a declared position, a declared command, a value operator,
  a link, a position vanilla writes, the position itself, a call. In a dynamic description the
  shape decides instead (`game.DynamicDescription`): a marker holds the shape again, `trigger` holds
  triggers, the leaf reads at the position the description belongs to.
- **What its block holds.** A link's block reads where the link is written. A command's block is
  the first contents rule that fits (`game.ContentsOrder`). An operator's is a value block or
  arguments. Otherwise the position's claim decides. `game.Structurals` fix random_list's branches
  and switch's cases; a call starting with an iterator prefix iterates that scripted list
  (`game.ListIterators`).
- **Its scope.** The first scope rule that fits (`game.ScopeRules`); a scope that runs no triggers
  and no effects is unknown.

A `Reader` is one use of the walker: `Create` makes the positions it reads; `Entry` says which
statements start a database's entry; `Names` gives a key's alternatives; `Visit` sees each step.
Four readers use it: `observe` (vanilla, creating), `join` (docs and wiki), `usage` (the dumps'
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

A usage is read once every command is declared, into the command's `Args` (`usage`): its statement
named after the command, else its first statement when that is a block under another declared
command's name (Vic3 `set_global_variable`'s usage is written as `set_variable = { … }`). Every key
in that statement's block is an argument, whatever else its name is. A block placeholder in the
dump text gives `Args.Block` its kind when the usage gave none; a usage whose blocks write no
placeholder takes arguments only. `Lists` marks a command that iterates a list with no target.

## Entries and evidence (`vanilla.go`)

`Entries(file, parsed, found)` is the one entry detector, for the schema and the index alike. It
hands on each `Entry` (database, name, mode word, statement, `Local`) in source order:

| Where | Rule | Registry field |
|---|---|---|
| any depth | a keyword before an assignment declares it: a file type's in `<ext>.<word>`, a folder's in its database (local to its file when written in another folder) | `FileType.Keywords`, `Folder.Keyword` |
| `.gui`, `.asset`, `.font` | a block named by its name key, a `NameKeyOf` key or the widget key | `FileType` |
| a database's top | each assignment is an entry named by its key; wrapper keys hold top statements; only event ids in events; a `NameKey` folder names each top block by that key; `Unnamed`, `ByFile` and history rows as their fields say | `Folder` |
| inside an entry | keys at a nested row's path define entries of their own; a key the docs write there, or a declared command or link, is a field | `Layout.Nested` |
| tables and sound lists | each line's name | `Layout.MapTable`, `source.List` |

A mode word (`REPLACE:x`) is stripped from every name.

`observe` reads each entry with the walker, creating positions, and records only evidence: `Seen.In`
(how many blocks of the parent hold the key), `Blocks`, `Forms` (and `FormCommand` on a block that
holds a declared command) and `Values` (each word, with counts). An entry inside another is observed
only under its own database.

`typeValues` gives each position that no claim types (in entry trees, command arguments and scripted
entries' parameters), and whose words all name one script database's entries (or, for GUI trees, one
GUI database's), a reference to that database; else a loc key type when every word is a loc key.
Numbers never count, and a history database's names, which are other databases', name none. It then fills `Prefixes` from the names vanilla
writes after each link prefix. Vanilla's types never accuse.

## Docs, wiki examples and usages (`template.go`)

`normalize` turns a doc, wiki example or usage into script and keeps every other line as a comment:
lines shaped like script stay code (braces, `key =`, `key {`, field bullets); the rewrite table
quotes placeholders and moved notes (`(optional)`, `(default = x)`) into the line's comment; a
usage line is split before each statement; blocks left open close by indentation; markdown
headings and lists become comments.

`join` reads a doc into its database's tree with the walker. A key at a position vanilla writes
takes the statement's claims; a list word's claims go to the position holding it. A key vanilla
writes nowhere below where it stands (an example entry's name, `my_event.1 = { … }`) is
transparent: its block is read in its place, and at the top its claims are the entry's. A doc never
makes a position.

The phrase catalogue (`phrases`) reads a statement's value as written and its comments into claims;
the comment lines no row reads are the blurb.

| Row | Reads | Claim |
|---|---|---|
| default | `default is x`, `defaults to x`, `default = x` | `Default` |
| optional, required | `optional`; `required`, `required if/when …` | `Required` |
| root | `root = x` opening the text | `Scope` |
| types | `int`, `number`, `script value`; `bool`, `yes/no`; `date`; `string`, `text`; `scope`; `loc key` | `Value` type |
| event, on_action | `event id`; `on_action` | `Value`: a reference to the events or on_actions database |
| block kinds | `trigger(s)`; `effect(s)` | `Block` |
| file | one quoted word with a slash | `Value` file |
| words, items | `a/b/c`; `- item: blurb` lines | `Value.Words` |

A placeholder no row reads names the database `Named` gives. In a usage, an argument written as a
block placeholder word (`on_created = effect`) runs in an unknown scope.
