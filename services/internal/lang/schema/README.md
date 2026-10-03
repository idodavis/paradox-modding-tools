# lang/schema

The schema stage: one position tree (`schema.go`) that every source writes into, in trust order, and
the readers that write it.

## The stage (`schema.go`: `Build`)

`Build(ctx, Input)` is the schema stage. `Input` is the game and its version, the install's inventory
in load order, the dump folders' inventory, the wiki sidecar's path and when the install last changed
(`game.InstallUpdatedAt`). `Input.DumpsOnly` stops after steps 1 and 2: the dump part the generator
ships (D-27). `Input.Shipped` is a shipped schema whose dump part step 2 takes in place of reading
the dump files. The sources write into one `Schema` in this order, so every claim keeps the first
source that states it (spec §4.1, D-21):

1. **Databases**, made first so every later source can name them. A script file belongs to the
   database `folder` names (see Entries below); a file at the root belongs to none. Its `Naming` is
   the registry row (zero when none); its `Entry` is a new `Key` whose `Block` the registry states
   with `SourceRegistry`: effects when the row's `Body` is only `effect`, triggers when only
   `trigger`, fields when it has no `Body` (D-17). A `Body` holding `value` or several words states
   nothing, so docs may still state the entry's block. Each of the row's `Refs` (a path from the
   entry, `random_on_action/<number>`) is made and claimed with `SourceRegistry` as a reference to
   the database itself, so the walk records the words written there (an on_action's `on_actions`,
   D-35). Then `Callees` maps each `CalledIn` word to the databases whose row holds it, in name
   order, and `*` to every database with a `CalledIn` (D-24, D-29), so no call loops over the
   databases.
2. **Dumps**: `readDumps`; or, with `Input.Shipped`, its `Commands`, `Scopes`, `Saved`, `Modifiers`,
   `OnActions`, `DataTypes`, `CustomLoc` and its dumps `Sources` entry, taken as they are (nothing
   else of a shipped schema is used, and the dumps write nothing outside these).
3. **Docs**: each `source.Info` file is read only into its own folder's database: `folder` must
   stop at the doc's folder (no registry row above it) and the database must exist. It is decoded
   by the caller (`File.Text`) and read by `template` into that database's `Entry` (markdown by its
   `.md` extension). A doc in a folder with no database of its own is not read, so a data file in a
   subfolder (EU5's `terrain_cache/*.info`) never reaches the database above it.
4. **Wiki**: for each database in name order, the pages `wiki.NamedPages` gives (named for the
   database or its first file's folder); each page's `<pre>` blocks are read by `template` as a
   doc's example is. A history database (registry `History` row) reads no page: its pages describe
   the database it is the start state of. The scripting-language pages (Effects, Triggers, Scopes,
   On actions) that `wiki.PagesFor` adds to Guide tabs describe commands, not databases, and are
   not read.
5. **Fields**: each database's `Fields` is the key names a declared source (dumps, docs, wiki)
   states a claim at anywhere in its `Entry` tree (D-21), gathered once now that those sources are
   read; vanilla states no claim, so the sets are final and only read afterwards.
6. **Vanilla**: `source.Ordered` parses the install's script files ahead on a worker per CPU; on
   one goroutine, in file order, `Entries` finds each file's entries (a table or a sound list
   goes unparsed), a database is made for every name it yields that does not exist yet (file types,
   nested rows, the map table, sounds: a fields block, `Naming` zero), and `observe` records the
   file's evidence. Vanilla sets no claim, so reading it last leaves the trust order of claims alone.

The prose lines `template` leaves unread, from docs and the wiki, go to `Schema.Unread`; the dumps'
description text is not listed. `Sources` says where each source came from and whether it is out of
date (spec §4.3):

| Source | `From` | `Written` | `Stale` |
|---|---|---|---|
| dumps | `yours`; `none` when no dump file was read | the oldest dump file read (dump_data_types runs apart from script_docs) | `Written` before `Updated` |
| docs | `install` | `Updated` | never |
| wiki | `yours`; `none` when the sidecar is missing | the sidecar's modification time | never: §4.3 rule 2 marks a page stale only when it is labeled for an older version, and a sanitized page keeps no label (the sanitizer drops the infobox and metadata boxes) |
| registry, vanilla | no entry | | the registry is always fresh; vanilla never accuses |

`Schema` is `Game`, `Version`, `Sources`, `Databases`, `Callees`, `Commands`, `Scopes`, `Saved`, the
dumps' flat tables (`Modifiers`, `OnActions`, `DataTypes`, `CustomLoc`) and `Unread` (spec §5). A
`Database` is its `Name`, `Entry`, `Naming` and `Fields` (step 5).

A claim may accuse a user's code only through `Speaks` (spec §4.3): dumps, docs or wiki state it
(rule 1: only they have a `Sources` entry), that entry is not stale, no vanilla evidence at the
position contradicts it, and a docs or wiki claim also needs vanilla to have written the position
(D-20). A registry claim never speaks: its block kinds classify keys, which `Speaks` never judges,
and its `Refs` references feed go-to and the event graph only, so `random_on_action = { 100 = 0 }`
stays quiet. A value claim is contradicted by a form vanilla wrote outside its type; `FormCommand`
marks the block, not a value, and is ignored.

## Positions and links (`schema.go`)

A written name becomes a position only by its written form, read with jomini's own readers
(`collapse`): what `jomini.Literal` reads as a number is `<number>`, as a date (`867.1.1`,
`-221.1.1`, `1444.11.11.12`, `867.1.1.`) `<date>`, and what `jomini.SplitPrefixed` splits as
`prefix:x` is `<prefix:>` (not `REPLACE:x`, an entry mode, nor `event:/SFX/x`, a path).
`Key.At(name)` is the position for a written name, nil when missing, and never makes one (the
index stage reads the schema only through it); `Key.Below(name)` is `At`, made when missing.
Every source meets the same positions. There is no other collapse (D-22): a doc's placeholder key
(`<culture> = { … }`) is a position of its own, and vanilla's `french = { … }` another.

`Schema.Slot(under, name, create)` is the one place the slot decides a key's kind (D-17), for the
docs, vanilla's observation and the classify walk alike. It returns the position the key reads at
and the command when the key is one:

| Where | Position | Command |
|---|---|---|
| in a fields block (`under.Block` is `BlockFields`) | the field `under.At(name)` | nil |
| elsewhere, a field a declared source states (`Key.Stated`): a command usage's own argument (D-34) | that field | nil |
| elsewhere, a declared command that is no link | its `Args` | the command |
| elsewhere, a link (`Schema.Link`) | nil: its block reads at `under` | the link |
| elsewhere, any other key | the field `under.At(name)` | nil |

So in `create_pop = { culture = <culture> }` the usage's `culture` is create_pop's argument, not
the `culture` link, wherever it is read. Only a usage creates such a field (see the walk below):
any other source reaches a command's or link's name through its command first.

A field `under` does not hold is made by `Below` when `create` is true, else the position is nil
(unknown). Without `create` (the classify walk), a triggers or effects block holds only the fields a
declared source states, a claim from dumps, docs or wiki at that position (`Key.Stated`, which
completion also reads to put such keys first) (D-24): a key vanilla alone
wrote there reads as nil, so the walk can read it as a call. Vanilla's positions stay in the tree
as evidence for completion. `Slot` matches command and link names ignoring case (D-26): the dumps
declare `and` and script writes `AND`. Field keys match exactly, and so does the entry detector's
D-21 test, so a define named `CHARACTER` stays an entry.

`Schema.Link(key)` is the declared link a key reads through: the first link of a chain (`root` in
`root.liege`), by its name or by its prefix before `:` (`c:FRA`, `scope:x`). Names are held
lower-case, so a caller matching ignoring case passes the key lower-cased. `Schema.Scopes` holds
the scope types the dumps declare.

`Schema.Named(word)` is D-15, the one place a word names a database, for doc placeholders and the
classify walk's link prefixes alike: in two tiers, history databases (registry `History` rows) left
out, the databases called by the word or the word + `s`; only when there is none, the databases
whose last folder is either. Exactly one in the deciding tier is the database; several or none
give "", so no map order decides.

`Schema.Flagged(flag)` is the databases whose registry row passes a test, in name order: the one
place a caller finds "the database whose row has X" (`Events`, `Concepts`, `Functions`,
`Describes`). The databases a call resolves in are `Schema.Callees`, built once in step 1.

Sources, in trust order: `SourceRegistry` (game.go's rows, such as an entry's block kind or a
`Refs` position's reference: declared and always fresh, it classifies and never accuses), then
dumps, vanilla, docs, wiki. A
declared word list with no word type is a word value: `Speaks` allows words and text there.

## Dumps (`dumps.go`)

`schema.readDumps(files)` reads the games' console dumps into the schema, stated by `SourceDumps`:
the script_docs `.log` files and the five dump_data_types `.txt` files (`DumpFiles`), found by base
name in any folder; a missing or unreadable file is skipped. It returns the oldest file's time
(zero when none was read), and it runs first, so the trust order holds: later sources only fill
what the dumps left empty.

`MissingDumps(files)` is the one test of which `DumpFiles` names an inventory lacks: the scan takes
the shipped dump part when it gives any, and `index.BuildSchema` (the generator) refuses to build.

**The label table** (`dumpLabels`) is the one place that knows a dump's forms: per kind of file, its
files, a pattern for an entry's header, and each label its entries write with the field it fills (a
label mapped to `""` is read and kept nowhere). The label-line and dashes patterns are compiled
once, at package level. Entry headers by form: `-----` then `name - text`
(CK3), `## name` and `### name` (Vic3, EU5), `name:` (scopes, on_actions, custom loc, Vic3
modifiers), `Tag: name` (CK3, EU5 modifiers) and a data type's `Type.Function( Args )`.

| File | Fills | Labels read |
|---|---|---|
| `event_scopes.log` | `Scope` | `Stores Variables`, `Evaluate Triggers`, `Execute Effects` (`Variables`, `Triggers`, `Effects`, each false only for `no`); `Save Token`, `Change Scopes` kept nowhere |
| `event_targets.log` | `Command` (link), `Saved` | `Input Scopes`, `Output Scopes`, `Global Link` (`Global` when `yes`); `Requires Data`, `Wild Card` kept nowhere; the names under `Event Targets Saved from Code:` |
| `effects.log`, `triggers.log` | `Command` | `Supported Scopes`, `Supported Targets`, `Traits`, `Usage` (`usage`, `Usages`) |
| `modifiers.log` | `Modifier` | `Description`, `Extra info`; `Use areas`, `Mask`, `Name` kept nowhere; a `--- section ---` heading is no modifier |
| `on_actions.log` | `OnAction` | `Expected Scope`; `From Code` kept nowhere |
| `custom_localization.log` | `CustomLoc` | `Scope`, `Parent`, `Suffix`; the lines after `Entries:`; `Random Valid` kept nowhere |
| `data_types_*.txt` | `DataType` | `Definition type`, `Return type`; `Macro replacement`, `Description` kept nowhere |

**An entry** runs from its header to the next header or a dashes line. A line that opens with a
label of its kind's table (exact case, maybe bold) fills that label's field; every other line is
text. Text is description until the usage starts, at a `Usage` label or the first line the island
test (`islands.code`) reads as script; description lines are the `Blurb`, and the lines from the
usage on go through `template` with the command's name, after every command is declared; only
the usage's top-level statements named after the command are read (Vic3's generated iterators
carry another trigger's body, which stays prose). A usage written inside a description sentence
is description.

**Commands.** A command or link name is held lower-case (D-26), and usages match it ignoring case.
An effect, a trigger and a link of one name are one `Command` with every `Kind` bit;
it runs where any reading runs, and a reading naming no scope runs anywhere (`none` and `all` name
no scope). `Supported Targets` and `Output Scopes` fill `Out` as written. A link the dumps label
`Global Link: yes` is `Global`: it reads the same from any scope (`scope`, `local_var`,
`global_var`; not `var`). A trigger's `Traits` claim the value of its `Args`: `yes/no` a bool,
`... valid date` a date, `X scope` and `X target` a scope, a comma list its words; comparison
operators claim no type. `class CXDatabase key` is read
at once, before any usage, as the usage `command = <x>` through `template`, x being the class word
X turned CamelCase → snake_case (`TenetType` → `tenet_type`, D-23), so it names a database by D-15
exactly as a doc's placeholder does (`tenet_type` → `religion/tenet_types`); no match claims
nothing. Every `Args` is made (`&Key{}`) before any usage is read.

**Saved scopes.** `Saved` holds, as true, each scope name the link dump lists under `Event Targets
Saved from Code:` (`actor`, `target`, ...): scopes the game saves itself, never defined in script.

**Flat tables.** `Modifiers` (`$TYPE$` families keep the `$`), `OnActions`, `CustomLoc` and
`DataTypes` hold what the files write by name. A data type holds its
functions by name; functions written without a type (global promotes, functions and macros) are
the functions of the type `Global`.

## Entries and vanilla's evidence (`vanilla.go`)

One entry detector serves both stages: in the schema stage the entries it finds are where vanilla is
observed; in the index stage they are the definitions. Every rule reads a field of `game.go`'s rows;
none names a game or a folder.

**Databases.** `schema.folder(rel)` is the one place a path names its database: it walks up to the
longest `GameInfo.Folders` row at or above the path (a row's whole tree is one database, as
`events` is; a `Rel` holds no stage folder, since EU5's inventory roots are the stage folders),
else takes the path's folder, and names the database by that path with a leading `common/`
dropped, slashes kept (`landed_titles`,
`struggle/catalysts`, `events`); a file row is named by the file's path (`achievement_groups.txt`).
`Build` makes the databases by it, and `DatabaseAt(rel)` and `Entries` find them by it, so a file's
database is always the one `Build` made for its folder; a folder no install file has names none.
Its `Naming` is the folder row, read directly.

**Entries.** `schema.Entries(file, parsed, found)` hands on each `Entry` (database, name, mode, the
defining statement) in source order:

| Where | Rule | Registry field |
|---|---|---|
| any script, any depth | a keyword word before an assignment declares that assignment: a file type's keyword in `<ext>.<word>`, a folder row's in that row's database | `FileType.Keywords`, `Folder.Keyword` |
| `.gui`, `.asset`, `.font` | a top block is named by its name key's word, in `<ext>.<key>` (without it, nothing); a block of a `NameKeyOf` key, at any depth, by its row's key, in `<ext>.<key>`; any other block below the top writing the widget key, in `<ext>.widget` | `FileType.NameKey`, `NameKeyOf`, `WidgetKey` |
| an extension with no row | nothing | `GameInfo.FileTypes` |
| a database's top statements | each assignment is one entry named by its key; a constant (`@x`) is none | — |
| | a wrapper key's block holds top statements | `Folder.Wrappers` |
| | only event ids are entries (`namespace` is none) | `Folder.Events` |
| | each top block, an anonymous `{ }` too, is named by its name key's word, up to a `(`; a block without its name key defines nothing | `Folder.NameKey` |
| | each top statement is an entry that names nothing (`Name` "") | `Folder.Unnamed` |
| | the whole file is one entry named by its stem | `Folder.ByFile` |
| | a history row with no `Of` makes the whole file one entry that names nothing; with `Of`, the top keys are its entries | `Folder.History`, `Of` |
| inside an entry | the keys at a row's path (`""` the entry's own block, `*` one key, `**` any depth, segments as globs), constants aside, are entries of `<Parent>.<Database>`, and so is each word listed there that `jomini.IsDefName` reads as a name (a game concept's `alias = { … }` with `Path: "alias"`; CK3's `accolade_parameters = { … }`; not a stray `;`); `self` keeps the parent's database and nests again; a row with no `Database` creates none. In the entry's own block (`""`) a key whose written form (`collapse`) dumps, docs or wiki state a claim at anywhere in the parent database's `Entry` tree is a field, not an entry, and so is a key the dumps declare as a command or link (`Commands`, `Link`): nested entries are new names, never game commands (D-21; `Build` reads docs and wiki before vanilla). Those names are the parent database's `Fields` | `GameInfo.Nested` |
| the map table (parsed nil) | each line's first column, in the row's database; a `#` line is a comment | `GameInfo.MapTable` |
| a sound list (parsed nil) | each line's text after its GUID, in the folder's database | `source.List` |

A whole file's entry is a `ValueStmt` holding the file's statements as one block; a table row or a
sound is a `ValueStmt` holding its name as a word at its place in the text. A declared mode word
(`GameInfo.EntryModes`) is stripped from every name, standard, nested, history, GUI or declared:
`REPLACE:x` is `x` with `Mode` `REPLACE`. A history row's `Of` is read only for whether it is
present (none: the whole file is one entry that names nothing); no code reads the names in it.

**Evidence.** `schema.observe(entries)` takes a file's entries together and walks each entry's value
under its database's `Entry`, recording `Seen` only; nothing sets a claim (spec §4.2). A statement
that is itself an entry (a nested title, a policy, a character in a setup file) is observed only on
its own, under its own database, never as a field of the entry holding it. A listed word that is an
entry (an alias) is a list value, not a block: it is recorded as a value of the position holding
it, and on its own records nothing. An entry whose database is
not in `Databases` records nothing (`Build` makes one for every database `Entries` names). Each key
reads where `Schema.Slot` says, made when missing (D-17): in a fields block (a `BlockFields` claim:
the entry's own block from the registry, or one a declared source calls fields) every key is a
field; elsewhere a stated field (a command usage's argument, D-34) is that field, so vanilla's
values there are observed, a declared command (not a link) continues in its `Args`, a link's block
reads at the link's parent position, and any other key is a field. A list element is written at
the position holding it.
A declared command (not a link) written directly in a block sets `FormCommand` on the block's
position (a link's block counts as its parent's): the classify walk records calls in an undeclared
block only where vanilla writes commands (D-29).

| `Seen` | Counts |
|---|---|
| `In` | the written blocks of the parent that hold the key, once per block |
| `Blocks` | the times the position is written as a block |
| `Forms` | quoted → text; a number, a date; `yes`/`no` → bool; any other word; a block; `FormCommand`: a declared command written directly in its block |
| `Values` | each word, text or bool written, with counts, also where a declared value names a database (`unresolved-ref` checks they all resolve) |

## Docs, wiki examples and dump usages (`template.go`)

`schema.template(text, markdown, at, from, command)` reads a doc (`.info`, readme, `.md`), a wiki
`<pre>` example or a dump usage into the positions under `at`, each claim stated by `from`; `command`
is the documented command's name for a usage, "" otherwise. Callers pass decoded text (a file's
`File.Text`). It is one pass to plain script (`normalize`), one `jomini.Parse`, comment attachment
(`Result.Attach`) and one walk.

### The normalizer

`normalize(text, markdown, usage)` turns the text into script and drops no line:

1. **Island lines.** A line shaped like script stays code: a brace, a lone `<placeholder>`, `key op`
   or `key {` (the key a word, a quoted string or a `<placeholder>`; `a | b | c` names
   alternatives), or a field bullet `- key: <placeholder> text`. A word ends in a letter, digit, `_`
   or `$`, so `(skills`, `->` and `Note:` are prose. Inside an open block a bare word is code (a list
   element), but only on a line that was never a comment. A `#` line is tested without its first
   `#`, so commented-out examples are read; it stays a comment when blank, not shaped like script, or
   opened by a phrase the catalogue reads (`root =`, `scope:x =`, `Brief:`, `Default …`); `# key =
   prose` becomes a field whose prose is its blurb. Every other line becomes `# ` + the line,
   verbatim. The shapes (`islands`) and both tables are compiled once, at package level.
2. **Rewrite catalogue** (`rewrites`), one table applied in order to a code line before its own
   comment; each row has a replacement and the part it moves into the trailing comment:

   | Written | Becomes |
   |---|---|
   | `(… optional …)`, `(… default = x …)`, `[x = y]` | moved into the trailing comment |
   | `a \| b \| c` | `a\|b\|c`, one key of alternatives |
   | `<placeholder>` | `"<placeholder>"`, one quoted word |
   | `- key: <placeholder> text` | `key = "<placeholder>"`, the text moved into the comment |
   | `key = words with spaces` | `key = "words with spaces"` |

   Any other `key: text` stays prose. `a/b/c` stays one key; the walk reads it as siblings.
3. **Usages.** A usage line that starts with a statement is first split before each statement on
   its own level or its first brace level (placeholders and `(notes)` skipped), so a note trails the
   argument it follows: `limit = { <triggers> } (optional) weight = …` gives `limit` its note.
4. **Offside rule.** A block still open when a later code line returns to its opening line's
   indentation (a tab runs to the next multiple of 4) is closed before that line; blocks still open
   at the end of the text close there.
5. **Markdown.** goldmark marks headings and lists: a heading becomes `# ## heading`, a list line is
   a comment unless it is a field bullet, and every other line goes through 1–4.

### The walk

Each name of a key (`a/b/c`, `a|b|c`) is classified as the classify walk does: the slot decides the
kind (D-17), from the `Block` claim of the position the key sits in (`at` carries the registry's
entry block; a usage's `Args` has none unless the usage declares one):

1. at the top level of a usage, the statement named after the command reads at `at` (the command's
   `Args`): its block contents, or its value as a claim on `Args` itself; any other top-level
   statement of a usage is unread;
2. in that statement's block, each key is a field of `Args`, made at once whatever else its name
   is (`culture`, `flag`, `terrain` are the command's arguments, not links or commands; D-34), so
   `Slot` reads it as that field wherever it has a claim;
3. in a doc, the one top-level block that is no declared command (a link counts) wraps the example
   entry: its contents read at `at`. Without exactly one, the top-level statements are `at`'s
   fields (D-18);
4. every other name reads where `Schema.Slot` says, made when missing: in a fields block a field;
   elsewhere (a triggers or effects block, or one nothing declares) a declared command (not a link)
   is an example of it, read into that command's `Args`; a link's block reads at the parent
   position and the link states no claim; otherwise a field.

A key written `<word>` is a position of its own (D-22). The contents of a block are never top-level.

A list value (`<triggers>`, a bare word) states its claims for the position holding it; a value
outside any block is unread. A statement's value as written, then its attached comments, go through
the phrase catalogue. The blurb is the attached comment lines no row reads, heading lines aside,
joined with spaces; a quoted value of several words that no row reads is the blurb too. A placeholder
no type row reads names the database `Schema.Named` gives (D-15), claimed as a reference; none
claims nothing. A statement holding a jomini error that none of its children
holds, or a top-level statement of a usage not named after its command, is left out: its lines and
those of its comments are prose, with the loose comments; each prose line appears once. `Build`
lists the prose as unread.

### The phrase catalogue

`phrases` is one table over a comment's text (leading `#` marks and blanks off) or a value as
written (a placeholder's word without `"<` and `>"`). Each row sets one claim with `Set`, so the first
statement and comment to state a claim win. A row marked alone reads a text only when no other does.

| Row | Reads | Claim |
|---|---|---|
| default | `default is x`, `defaults to x`, `default = x`; `default x` where x is yes, no, none, a number or a dotted or underscored name; x one word that ends its clause | `Default` |
| optional | the word `optional` | `Required` optional |
| required when | `required if …` or `required when …` opening the text | `Required` when, with the text |
| required | `Required`, `Required!`, `Required field` opening the text; `this field is required` | `Required` always |
| root | `root = x` or `root - x` opening the text | `Scope`: the first word of x that is a declared scope |
| saved scope | `scope:x = …` opening the text | none: a note, never the blurb |
| brief | `Brief …` opening the text | none; a `(type)` in it is read by the type rows |
| number | `int`, `integer`, `int32`, `int31`, `num`, `number`, `fixed_point`, `script value`, `script_value`, `0.0-1.0` | `Value` number |
| bool, date, text | `bool`, `yes/no`; `date`; `string`, `name`, `text` | `Value` bool, date, text |
| scope, loc | `scope`; `loc key`, `localization key`, `localization_key` | `Value` scope, loc |
| event, on_action | `event ID`, `event_id`; `on action`, `on_action`, then optionally ` name` (CK3 writes `<on_action name>`) | `Value` a reference to the database whose registry row is `Events`, or `OnActions` (D-25) |
| block kinds | `trigger(s)`; `effect(s)` | `Block` triggers, effects |
| file | `"path/to/file"`: one quoted word with a slash | `Value` file |
| words (alone) | a value `a/b/c` | `Value.Words` |
| items (alone) | `- item: blurb`, `* item: blurb`, `item - blurb` | `Value.Words`, one per line |

Type words count alone (quoted or not) or alone in parentheses.

### Example

CK3 `common/activities/activity_types/_activity_type.info`:

```
### Character state
- Travel: The character is travelling to the location of the next phase
...
activity_type = {
	# Should the player see this activity type
	# root = character trying to host this activity
	is_shown = {
		<triggers>
	}
	...
	# Defaults to 0.
	sort_order = integer
	...
	# optional - if left empty this check will be taken from can_start_showing_failures_only instead.
	can_plan = {
		<triggers>
	}
```

`activity_type = {` is the doc's one top-level block that is no declared command, so it wraps the
example entry and its contents read at the database's entry position, a fields block. `is_shown` is
a field there; it gets a triggers block, root scope `character` and the blurb "Should the player see
this activity type". `sort_order` gets a number defaulting to `0`;
`can_plan` is optional. The `### Character state` list is loose: it comes back as prose, three items
and their blurbs.
