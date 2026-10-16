# Observed game syntax

What CK3, Vic3 and EU5 actually do, as measured against real installs — not what
the engine assumes. **When a game update breaks PMT, start here:** re-run the
measurement in the row that broke and compare it against the recorded value.

Each claim below records what was measured. Re-measure by reading the install's
files, not by reasoning. The real-install tier (`task test:gate`) reads each
game's root folder from these variables; the dumps are read from the game's
Documents folder:

```
PMT_TEST_INSTALL_CK3   (dumps in …/Documents/Paradox Interactive/Crusader Kings III/logs)
PMT_TEST_INSTALL_VIC3  (dumps in …/Victoria 3/docs)
PMT_TEST_INSTALL_EU5   (dumps in …/Europa Universalis V/docs)
```

Measured 2026-09 against CK3 1.19.x, Vic3 and EU5 current on Steam. The engine
that reads these facts is described in `services/README.md`.

---

## 1. The type system is declared, not inferred

Each game ships its own in `script_docs`. Same semantics everywhere; only the
block syntax differs.

| Dump | Declares | CK3 | EU5 | Vic3 |
|---|---|---|---|---|
| `event_scopes.log` | the scope-type universe | 70 | ~120 | ~100 |
| `event_targets.log` | scope links, `Input Scopes` → `Output Scopes`; `Global Link`+`Requires Data` = the citable prefix table | 286 | 334 | 353 |
| `effects.log` | effects, `Supported Scopes` / `Supported Targets` | 1905 | 1534 | 3152 |
| `triggers.log` | triggers, same shape | 1785 | 1798 | 1816 |
| `on_actions.log` | `Expected Scope` per on_action | 902 | yes | yes |
| `modifiers.log` | modifier tokens, use area / mask | 742 | yes | yes |

**Block syntax per game** — the label table `dumpLabels` (`schema/dumps.go`) picks the form per file:

| Form | Used by | Looks like |
|---|---|---|
| Classic | CK3 | blocks separated by `----` |
| Markdown | EU5, Vic3 | `##` / `###` headings |
| Bare | `event_scopes.log` everywhere; Vic3 `modifiers.log` | indented `name:` / `Tag: name` / `key: name` |

**A link name is declared twice** — once global (`culture:english`), once
contextual (`character.culture`). Entries must **merge**; overwriting loses the
whole prefix table. Output types were checked across all three games and never
disagree.

**Vic3 `s:` scopes to a state _region_,** not a state. The game says so.

**`event` is not a scope type in any game.** So `trigger_event` has no declared
target type; CK3 documents it in prose only. So nothing declares what is fired:
the registry declares that events are named by event id, and a reference to an
event or an on_action is a fire unless it sits inside a trigger
(`index/README.md`, Fires).

---

## 2. Folder rules

| Fact | Detail |
|---|---|
| A database comes from the folder | `common/cultures/` → database `cultures` (`schema.folder`, `schema/README.md`). |
| EU5 nests under a stage root | Script lives in `game/in_game/...`, not `game/...`. Walking `game/events` finds nothing on EU5. |
| script_docs land in different folders | CK3 `logs/`, Vic3 and EU5 `docs/`, all under user data. |
| The console command and launch option are the **same** in all three | `-debug_mode` and `script_docs`. Verified by grepping each game binary (`ck3.exe`, `victoria3.exe`, `eu5.exe`); all three also carry the longer alias `script_documentation`. Only the output folder differs, which `GameInfo.ScriptDocsDirs` and `DataTypesDir` already encode — so the walkthrough needs no new per-game data. |
| Loose `.txt` at the script root is not script | `checksum_manifest.txt`, `credits.txt`. Parsing the manifest as Jomini produced fire edges for `common`, `events`, `history`. A file at the script root belongs to no database. |
| CK3 faiths have **no folder** | They exist only nested under `religion_types`. Verified: no `faiths/` directory exists. |
| EU5 `production_methods` **does** have a folder | `in_game/common/production_methods`. A nested `Descriptor` row for it would be redundant. |

### Where the install's version and timestamp are (and are not)

| Source | CK3 | Vic3 | EU5 |
|---|---|---|---|
| `launcher/launcher-settings.json` | present but **11 days stale** vs the real update | accurate | **does not exist** — no launcher directory at all |
| Newest mtime in `binaries/` | 2026-06-05 02:25:14 | 2026-08-19 15:27:13 | 2026-07-16 14:08:11 |
| Steam `appmanifest_<id>.acf` `LastUpdated` | 02:25:21 | 15:27:15 | 14:08:16 |

**EU5 publishes its version nowhere in the install folder.** The version the
engine reads for it is the Steam manifest's `buildid`, beside the install
(`game.LoadGameVersion`); freshness never depends on a version string.

`game.InstallUpdatedAt` therefore uses the **newest mtime among `binaries/`**,
falling back to the install root. It works for all three, needs nothing outside
the install folder (so non-Steam copies work), and answers the question that
actually matters — *did the files change since these dumps were written*.

Two properties worth keeping in mind:

- The exe mtime lands **2–7 seconds before** the appmanifest's `LastUpdated` on
  every game, which shows Steam stamps files as it writes them rather than
  carrying a build's own timestamps. So switching to an older build through
  Steam's game-version settings — a **downgrade** — still moves the mtime
  forward, and is correctly treated as invalidating dumps from the other build.
  A version comparison would miss that.
- Copying or moving an install preserves mtimes. That can only make staleness
  detection quieter, never falsely loud.

---

## 3. Shapes that look identical but are not

This section is the one that keeps costing us. Each row is a case where two
different things are **syntactically indistinguishable**, so the discriminator
has to come from outside the syntax.

### 3a. Setup wrapper vs. scripted entry

```
# Vic3 common/history/countries.txt — a wrapper; COUNTRIES is not an object
COUNTRIES = { c:USA ?= { ... } }

# CK3 common/scripted_triggers/… — a definition; the name IS the object
hegemon_favors_advancement_trigger = { title:h_china.holder ?= { ... } }
```

Character for character the same shape, `?=` included. **No body-shape rule can
separate them.** The registry's rows do, because they are declared per game and
folder:

- A wrapper key is listed (`Folder.Wrappers`: `common/genes`'
  `color_genes`, `named_colors`' `colors`); a history row with no `Of` makes the
  whole file one entry that names nothing, so `COUNTRIES` defines nothing.
- The keyword. `scripted_effect foo = { }` inside an events file is a
  declaration; the keyword states it (`Folder.Keyword`, `FileType.Keywords`).
  A definition's name is written once.

### 3b. Nested database vs. repeated inner key

Repeated inner keys are everywhere and none of them is a database. Script
*parameter* names recur as block children across many definitions, and a
repeated inner key counted as a database would mint phantom definitions:

| Game | Shapes that look like a database | Phantom defs they would mint |
|---|---|---|
| CK3 | 133 | **342,138** under `SKILL`, `TIER`, `NUMBER`, `LEVEL`, `BUILDING_LEVEL`, `$SKILL$` — script *parameter* names |
| EU5 | 183 | 35,458 under `CALL_FOR_PEACE_WARSCORE_LIMIT`, `var:chinese_expedition_opinion` |
| Vic3 | 134 | 5,231 under scripted constants like `@portrait_box_height` |

On CK3 that would be **64% of the entire model**. So nested databases are
declared, not found: a nested `Descriptor` row says where keys create entries
(`Layout.Nested`), a constant (`@x`) is never an entry, and in a nested row's
own block a key a declared source states is a field.

Root cause worth remembering: `jomini.SplitPrefixed` (`jomini/names.go`)
accepts any word shaped like `prefix:name` — a one-identifier prefix with a
lower-case letter, then a well-formed name, member or chain. The lexer is
deliberately permissive, so a prefixed word is a reference only where its prefix
is a declared link (`Schema.Link`, `index/README.md`, Resolution).

### 3c. `random_list` weight vs. fire key — *not* a bug

```
# CK3 common/on_action/army_on_actions.txt
random_events = {
    chance_of_no_event = 97
    100 = bp2_yearly.4030
    1   = epidemic_events.1120
}
```

Roughly half of each game's fire table is bare integers (CK3 37 of 71, Vic3
22/29, EU5 20/27). **These are correct.** The numeric key is a weight and the
RHS is a real event, so the on_action → event edges in the Event Graph depend on
them. Do not "clean" them.

---

## 3d. "Unreferenced in vanilla" is never evidence

Vanilla defines a great deal that vanilla itself never references: the engine
consumes it directly, or it exists for mods to reference. So the absence of a
citation says nothing about whether an object is real.

This is the reason IDE diagnostics are suppressed in install files, and it is
easy to violate somewhere else by accident.

What separates an ordinary field from a row is **repetition**, and it is
evidence only: an ordinary field appears in most instances of the parent
(`categories` in 81 of 81 game rules, `flag` in 24 of 27 law groups), a row
appears in one. Vanilla's counts (`Vanilla.Count`) feed completion; a declared source
decides what is a field, and vanilla alone never accuses.

**Rule: absence is never a claim. Derive membership from how a thing is written,
not from whether anything in vanilla points at it.**

## 3e. The same spelling means different things in different slots

The games reuse names across unrelated databases constantly, and the engine is
never confused because it always knows which slot it is reading. A resolver that
looks a name up across every index has no such context, and breaking the tie by
load order silently lets load order decide *what kind of thing a name is*.

Measured 2026-09-06, four cases where one spelling is two things:

| Written | Also spelled the same | What actually decides |
|---|---|---|
| `has_realm_law = x` | the loc key `has_realm_law` | a key is a declared command or a field, never a loc key (the walker's `Step`); only a value at a position whose declared value is a loc key reads as one |
| `culture = french` | the culture's own loc key | a value at a position whose declared value names a database is that database's entry; a loc key is read only where the declared value is a loc key |
| `add_trait = conqueror` | `namespace = conqueror` in the mod | a namespace is a word of its file, not an entry: only event ids are entries (`Folder.Events`) |
| `random_list = { 95 = … }` | province `95` in `map_data/positions.txt` | a number key is a number (`<number>`), and only a prefixed word or a declared value names an entry |

The last one is worth stating plainly because it is a whole class: **a bare
number on the left of an `=` is a weight** (§3c says the same of fire tables),
and vanilla CK3 keys map positions the same way. Nothing distinguishes the two
by name; the file does.

A weight's share is computed from its siblings, never assumed to be out of 100 —
`{ 10 = {…} 30 = {…} }` is a quarter and three quarters. A number key is a
weight under a declared command, but also where no command declares one:
`random_events = { 100 = some.event }` (§3c) and a Vic3 gene block's
`20 = empty` sit under no command, so nothing is claimed for them.

**The rule:** load order decides which *definition of the same database* wins,
nothing else. Every discriminator above is a structural fact — the slot,
the position's declared value, the file's folder row — so none of them is a list
of names that a game update can invalidate.

None of these is an object a name resolves to by lookup alone: a loc key is read
only at a position whose declared value is a loc key, and a namespace is a word
of its file, never a definition.

## 3f. A property holds a loc key — but a value is not always one

A property documented as a localization key does not always hold one. Measured
over vanilla, where nothing is missing by definition:

**`localization_key` is not strict.** Its value is a stem that customizable
localization completes at runtime — `localization_key = CustomLoc_BR_male_`
ends in an underscore because the engine appends to it. Unresolved values on
vanilla:

| Game | `localization_key` |
|---|---|
| CK3 | 2,607 |
| Vic3 | 11,597 |
| EU5 | **46,481** |

A value is a loc key only when a declared source says the position holds one,
and vanilla's own unresolved stems contradict that claim where they sit, so it
never accuses there. There is no `required-loc` check.

**Quoting does not tell you anything.** The obvious guess — quoted means display
text, bare means a key reference — is wrong. Of quoted values of loc-key
properties on vanilla, **86% (CK3), 99% (Vic3), 92% (EU5)** resolve to a real
key: Paradox quotes keys freely (`war_name = "INDEPENDENCE_WAR_NAME"`). Do not
use quoting as a discriminator.

**Whitespace does.** A localization file is `key:0 "value"`, so a key is a
single token and can never contain a space. `desc = "Always make coronations!"`
is display text the game shows verbatim. `loc.LooksLikeKey` rejects any value
containing whitespace.

**Keys hide in lists, not only on the right of an `=`.** A list member is a
value, not an assignment, so a reader that only reads `field = value`
structurally cannot see one — and the games put localization keys there in
quantity:

```
male_names = { Abu-Bakr Aarif Abdul-Gafur … }   →  Abbas:0 "Abbas"
cadet_dynasty_names = { "dynn_Rasulid" … }      →  dynn_Rasulid:0 "Rasulid"
```

Quoting is again no signal: vanilla quotes its cadet dynasty names and not its
given names. Most are name *equivalency* blocks
(`alexander_male = { Alexander Alexandros … }`), which are genuinely lists of
keys.

What remains is genuinely ambiguous and vanilla shares it:
`desc = "base"`, `desc = "Pyke"`, `desc = "Cowardly"` are single tokens
indistinguishable from keys, and CK3 vanilla itself has 233 of them. No
structural rule separates these; do not invent a heuristic for them.

## 4. Reference forms

An object is not always cited as `prefix:id`. Anything keyed only off prefixed
words will miss these.

| Form | Example | Where it appears |
|---|---|---|
| Prefixed word | `culture:english`, `title:k_x` | everywhere; a reference where its prefix is a declared link, whose prefix names a database (`Schema.Prefixes`) |
| Dotted chain | `character.culture`, `title:h_china.holder` | scope walks |
| Field RHS | `has_game_rule = suf_quieter` | **not a prefixed word** — a reference only where the position's declared value names a database |
| Keyword pair | `scripted_effect foo = { }` | inline scripted entries, GUI `type`/`template` |
| Saved scope | `save_scope_as = x` then `scope:x` | a definition in the `scope` database, made by the command that saves it (`game.NameCommands`) |
| Script param | `$SKILL$` inside a `scripted_*` body | a definition in the `parameter` database, named `<entry>:<NAME>` |

---

## 5. Field-referenced nested options

A recurring shape: a database whose rows live **inside** another def's block, and
which is referenced by a **field RHS** rather than a `prefix:id` word. A reader
that follows only prefixed words is structurally blind to all of them.

| Case | Defined in | Referenced as |
|---|---|---|
| Game-rule options | `common/game_rules/`, inside a rule block | `has_game_rule = suf_quieter` |
| Cultural parameters | inside culture tradition blocks | `has_cultural_parameter = has_access_to_shieldmaidens` |

A word is a reference only where a declared fact says so, so a field RHS that
nothing declares to name a database is no reference (`index/README.md`,
Resolution). CK3's nested `Descriptor` rows for `**/parameters` and `**/*_parameters`
declare the names such blocks list as entries of the `parameter` database.

### Game rules (CK3), worked through

```
# common/game_rules/zz_quieter_events_game_rules.txt
secret_unbeliever_found_rule = {
    categories = { quieter_events }
    default    = suf_quieter          # the rule names one of its own options
    suf_vanilla = { flag = GG_can_change_rule }
    suf_quieter = { flag = GG_can_change_rule }
}
```

- The **rule** is a def (database `game_rules`).
- The **options** (`suf_vanilla`, `suf_quieter`) are real named objects, written
  inside the rule's block.
- They are referenced as `has_game_rule = suf_quieter` — a field RHS, never
  `game_rule_setting:suf_quieter`. Nothing in vanilla CK3 or in any mod checked
  writes that prefix.
- Localization convention: `rule_<rule_id>` for the rule, `setting_<option_id>`
  and `setting_<option_id>_desc` for each option.

Counted on vanilla CK3: **78 rules, 77 with a `rule_<id>` key** (99%), and **380
options with 590 `setting_*` keys**.

---

## 6. Total-conversion scale is the measurement

Everything above is about what the games contain. This section is about what
that volume does to code that reads it, because the two are the same subject:
every entry here was correct on vanilla and only wrong on a mod big enough.

Whenever a loop does expensive work and *then* decides whether it was needed,
that is a bug waiting for a big enough mod.

**The rule:** filter before the limit, and decide before you materialise. Where a
budget exists, spend it on the *expensive* half — count cheaply and uncapped so
KPIs stay honest, materialise only what is kept.

### Scheduling must not decide anything

The corpus walk is parallel (`source.Ordered`), and worker completion order must
never decide a fact about the game: every file's records are added in **load
order**, on one goroutine (`index/README.md`, The scan), the same rule
that decides every other override. Parallelism may decide when work happens,
never what anything is.

## 7. Where these are asserted

The `TestInstalls*` gates run on the real installs through `task test:gate`;
each skips under `-short` and when its `PMT_*_GATE` switch or install variable
is unset. The rest run on the fixtures. A fact in this file with no row here has
no test: re-measure it by reading the install.

| Fact | Test |
|---|---|
| Every file of an install has a format or an ignore rule; every image gives up its header | `inventory/inventory_test.go` `TestInstallsAreClassified`, `texture/texture_test.go` `TestInstallImagesHaveHeaders` |
| Vanilla diagnostics under per-code ceilings | `lsp/lsp_test.go` `TestInstallsDiagnoseClean` |

**Rule for adding to this file:** record the measurement and the reproduction,
not the conclusion alone. Every wrong turn in this engine came from reasoning
about what the games *should* do instead of sampling what they *do*.

---

## 8. A key belonging to several databases is normal

A name that several databases share is not a conflict, and the games do it
constantly:

| Game | distinct definition keys | belonging to more than one database |
|---|---|---|
| CK3 | 180,434 | 16,576 (9%) |
| Vic3 | 35,328 | 757 (2%) |
| **EU5** | 81,643 | **35,019 (42%)** |

An EU5 country tag such as `BYZ` is at once a `countries`, a `coat_of_arms`, a
`flag_definitions` and a `customizable_localization` key. So a reference is never
resolved by guessing which database a name belongs to: the position says (the
slot decides, and a declared value names the database), and a name defined
in two databases is two definitions.

---

## 9. The hover signature rate is a property of the game, not of the engine

How often a hover carries a signature differs wildly between the games — CK3
15%, Victoria 3 5%, EU5 1%. That is not an engine gap. Measured over the sampled
tokens of each conversion:

| Game | hovers sampled | the game states a signature |
|---|---|---|
| CK3 | 7,278 | 1,201 (16.5%) |
| Vic3 | 3,848 | 214 (5.6%) |
| EU5 | 1,614 | 26 (1.6%) |

The "states a signature" column *is* the rate. Each game's script_docs carry
usage strings for a different share of the API, and the share that overlaps what
modders actually write differs again: EU5 documents a usage for 40% of its
declared tokens, but only 1.6% of the tokens appearing in real EU5 mod script.

So the gap is not closable by engine work. Do not chase it: the only way to raise
EU5's number is to invent signatures the game does not state, which is the
opposite of reading the type system from the dumps.

---

## 10. Primitives: the set is small, closed, and must be complete

A third of CK3's definitions are keyed by a bare number, so every integer in
script can collide with a definition:

| Game | definitions | keyed by a bare number |
|---|---|---|
| **CK3** | 215,448 | **68,109 (32%)** — characters 38,020, province_terrain 12,720, provinces 10,887, dynasties 4,223, map_data 1,465 |
| Vic3 | 41,011 | 0 |
| EU5 | 142,258 | 240 |

Left alone, `value = 5` resolves to a province. Measured on A Game of Thrones,
**4,044 of 11,411** literal values would resolve against some database — every
one a `provinces` hit on an arithmetic operand: `value`, `add`, `weight`, `days`,
`MIN`, `MAX`, `divide`, `count`.

**The first defence is a complete primitive set.** A
literal in a value position is a value, and the set of things that look like
literals is small and closed: **number, date, boolean** (`jomini.Literal` reads
numbers and dates; `yes` / `no` are the booleans). Every bug of this shape was a
hole in that set rather than a flaw in the rule: a date such as `1178.1.1` is no
name and no event id.

Assignment keys never reach the rule, so a `random_list` weight and a numeric
key in the file that defines it keep their own handling (§3e).

The slot decides next. After a word's fixed spelling (`@name`, `$NAME$`,
`[ … ]`, a prefixed word), a value is a reference only at a position whose
declared value names a database, so a number names a number-keyed entry only
there (`index/README.md`, Resolution). Hover and completion answer "what is
this" and "what can go here" from the same walk.

## 11. Loc keys are built out of other loc keys, not only out of definitions

Measured on vanilla, where the keys and the script that cites them ship from the
same build, many localization keys are no definition's name and no script's
citation: they are derived from **another key** or from a family pattern. The
engine reports no orphaned keys: a key nothing cites is not evidence.

The largest families no definition's name derives, as measured 2026-09:

- EU5 writes the negated form of a trigger tooltip as `NOT_<key>`:
  `NOT_THIRD_*` (1,259), `NOT_FIRST_*` (490), `NOT_HAS_*`, `NOT_IS_*`,
  `NOT_any_*`, and diplomatic actions as `OTHER_PERFORMS_<key>` (1,349) and
  `WE_PERFORM_<key>` (708).
- CK3: `game_concept_*` (921; concepts are *declared* in `common/game_concepts`),
  `PORTRAIT_MODIFIER_*` (1,023), `mercenary_company_*` (321), `great_project_*`
  (231), `dynn_title_*` (110), `struggle_parameter_*` (94).

**The tail is long and that shapes the fix.** CK3's keys of this kind spread over
29,647 distinct prefix clusters and the top 25 cover 9% of it. There is no set of
special cases here; only a rule that generalises would move the number.

EU5 also carries a dotted family — `caux.elysian_language`,
`br_ere_resistance_events.60.entry` — where the stem before the first `.` names a
location or an event namespace.

## 12. Where permissiveness is allowed, and where it is not

The consequences are asymmetric, and the engine should be calibrated to that
rather than to one global notion of "accurate".

**Failing to report something costs a modder a missed hint** — clutter they will
probably never notice. **Loosening a derivation costs them a hover that names the
wrong thing, a go-to-definition that opens the wrong file, and a `loc_key` type on
a field that holds no key.** The second is the failure that teaches a new modder
something false, which is the one thing this engine exists to avoid.

So the reporting side may be permissive and the derivation side may not. Two
rules keep that from collapsing back into one loose predicate:

1. **What a word is stays strict.** Vanilla evidence feeds completion and hover,
   where the worst case is a suggestion that does not belong, and it decides no
   scope or block kind and accuses of nothing. Where no source types a position's
   value, the words vanilla writes there give it one (all names of one database,
   or all loc keys): it hovers, completes and navigates,
   and never accuses. Only a declared source says what a command or a scope is.
2. **Nothing permissive runs where a claim accuses.** A claim speaks through one
   rule, `schema.MayAccuse`, and a misread doc position stays quiet.

Which positions hold a localization key is the neighbour to keep honest for the
same reason: it changes hover and go-to-definition directly. Only a declared
source (docs, wiki, dumps) says a position takes a loc key, never the vanilla
values written there.

---

## 13. A block name is not a position

`option` is a block in a CK3 event and another block in `common/accolade_names`,
and they hold nothing in common. A member table keyed by the block's own name
merges them — `triggered_desc` holds `trigger` and `desc`, `limit` holds
`exists`, `not`, `has_trait` — which is right for **offering** keys: the worst
case is a suggestion that does not belong, which costs a keystroke.

It is not enough for **judging** them. An absence check against the merged table
measured:

| Game | `unknown-key` findings on ~400 vanilla files |
|---|---|
| CK3 | **18,341** |
| Vic3 | 6 |
| EU5 | 4 |

CK3's number is one name colliding across databases, not eighteen thousand
defects.

**So `unknown-key` is not implemented** (`lsp.Codes` has no such code). It is the
one check that reports an *absence* — everything else fires on a positive
contradiction of something the game declares. Positions are keyed by database and
path (`activities/<entry>/max_guests`), so `event > option` and
`accolade_names > option` are separate, and `missing-key` speaks only for a key a
declared source calls required. Any absence check starts from this
number, measured again against the pathed positions.

## 14. A declaration keyword makes a definition file-local

`scripted_trigger foo = { … }` written inline is **not** the same object as
`foo = { … }` in `common/scripted_triggers/`. The engine resolves the inline
form within its own file.

The parser sees `scripted_trigger` as a stray bare word and `foo = {…}` as an
ordinary assignment, so nothing about the shape says "local" — the keyword does.

Measured, CK3 vanilla: **3,809 inline declarations** (2,432 `scripted_trigger`,
1,377 `scripted_effect`), and **every one sits under `events/`**, beside the
event that calls it. `common/scripted_triggers/` uses the bare form exclusively.
Vic3 and EU5 vanilla use the keyword form not at all; CK3 mods do (31 across two
in the test corpus).

Two consequences, and they pull in opposite directions from the obvious fix:

- **A repeat across files is required, not a collision.** ck3-quieter-events
  carries one `allow_naming_on_birth_of_dynasty_child_trigger` in both
  `birth_events.txt` and `quieter_events_events.txt`, commented *"Duplicating
  because CK3 is dumb and can't find it in the birth_events.txt file"*. The mod
  author found this by running the game. `duplicate-definition` must not report
  it.
- **Do not merge the databases.** The obvious tidy-up is to canonicalise the
  inline `scripted_trigger` onto the folder's `scripted_triggers`. That would let
  go-to-definition jump from a call in one file to an inline body in another —
  a body the engine cannot see from there. The accidental singular/plural split
  encodes a real distinction; go-to-definition must keep an inline definition
  to its own file instead.

PMT does not do this yet: a folder row's `Keyword` declares an inline entry in
that row's own database (`entryAt`, `schema/README.md`), so both of
the above can happen.

Vanilla repeats names by design too: Vic3 defines 14 coat-of-arms keys twice,
verified by scanning the files directly, and the game keeps the last.

## 15. Each game documents itself in a different shape, and one of them is not prose

Five formats, and every doc line goes through the one normalizer
(`schema/README.md`, Docscript), so a format it does not recognize is prose that stays
a comment, not a file it skips; the health strip counts none of it.

| Shape | Game | Example |
|---|---|---|
| comment **above** the key | CK3 `_*.info` | `# localization key` ⏎ `adjective = "adjective_1"` |
| comment **trailing** the key | Vic3 `.md` | `building_group = bg_key   # *reference to building group*` |
| `# - key: <type> desc` bullets | EU5 | `# - build_time: <integer> building time in days` |
| `key = <type> desc` — **no hash between type and prose** | EU5 | `# type = <diplomacy/subject/union> defines if the relation …` |
| `### brief: name ( type )` header, prose, lone `#`, then the key | CK3 | `_governments.info` |

The last two are why EU5 looks undocumented next to CK3 when they are not read:

- The angle-bracket form documents **297 EU5 keys** and almost nothing else
  (CK3 1, Vic3 2). A matcher that finds the key and then looks for a `#` that is
  not there records an *empty* doc.
- The `brief` form's convention is to close the prose with a lone `#` before the
  key it describes. Treating that `#` as the end of the comment block drops the
  description from nearly every one: `_governments.info` documents ~100 keys and
  a reader that stops at the lone `#` yields **6**.

`.info` files do not all start with `_`: that is a CK3 convention (162 of its
166). EU5 writes **23 of its 32** without it.

**Vic3 barely moves, and that is the real finding about Vic3.** Its `.md` files
are *narrative*, not field annotation: `treaty_articles.md` is 468 lines
explaining what source and target mean for directed versus mutual articles, and
`ship_name_definitions.md` is 198 lines of ship names, which document no field.

Those brackets and parentheses also carry a **declared type**, and all three
games write them — the angle form is not EU5-only. A **closed value set**
(`yes/no`, `none/mutual/giving/receiving`) is the only half a diagnostic can
judge, and EU5 carries 91 of them (CK3 18, Vic3 15) — the game states the set,
so a value outside it is wrong without any inference. `wrong-declared-value`
checks it, and §20 says why it must stay quiet where vanilla writes a value
outside the set.

`dynamic desc` also accepts a `first_valid` block, so reading it as
"localization key" would teach a rule that is too narrow. Whole and decimal
numbers differ — `= 2.5` in an integer field is a real mistake and only a
distinction can say so — but the type words map both to one `number`, a known
gap.

Prose wears the same brackets — EU5's `<name of global variable list with the
list of candidates in>`, Vic3's `<locator_name_1>` — so a bracketed word is a
type only where the type table lists it (the phrase catalogue,
`schema/README.md`), and a placeholder names a database only by its name (`Schema.DatabaseNamed`).

## 16. What declares an event's scope, and what only looks like it does

An event body runs in a scope, but the event states it nowhere. Two candidates
look like they declare it. One does not.

**`type` does not.** `type = character_event` is a presentation window, not a
scope: CK3's `activity_event` and `letter_event` bodies are *both*
character-scoped. Reading the prefix as a scope reported thousands of correct
vanilla lines as `wrong-scope`, and the schema cannot save it — `activity` is a
real declared scope type, so a "the name proposes, the schema disposes" check
confirms the wrong answer confidently. It has been re-derived twice before.

**The on_action does.** `on_actions.log` states the scope each on_action runs in,
and an event it fires runs in that same scope. Measured over the fire graph:

| | events | reached from a scoped on_action | with 1 root scope | with 2+ |
|---|---|---|---|---|
| CK3 | 9,791 | 4,376 | 4,376 | **0** |
| Vic3 | 2,264 | 932 | 932 | **0** |
| EU5 | 7,528 | 2,027 | 2,027 | **0** |

Not one event in any of the three installs is fired by two on_actions of
different scopes. That is what made **scope sets unnecessary** for this: the
obvious next step after tiger's bitflag `Scopes` was to make the walk carry a set
so an event could be "character or country", and the corpus says no such event
exists. A disagreement therefore means something PMT does not understand, and
the walk must give no root rather than pick one.

PMT does not read this yet: the walk does not carry an on_action's scope into the
events it fires, and an event body runs in its database's `Scope` claim, else
unknown (`index/README.md`, Scopes).

Edges are read from the open workspace rather than the cache, so a mod's own on_action
firing a mod's own event counts exactly as vanilla's does.

## 17. `root` is only meaningful where a root was declared

Only an on_action declares its own starting scope. Everywhere else nothing
declares one: PMT starts an entry's body in its database `Entry`'s `Scope` claim
(`index/README.md`, Scopes); declared links and iterators move
it from there.

`root` is the exception. It means "the bottom of the stack", and with no declared
root the bottom is merely the first hop the walk happened to take. CK3's
`camp_party_actions.txt` writes

```
effect = { random_attending_character = { limit = { root = { any_attending_character = … } } } }
```

where `root` is the *activity* the pulse action runs on. Reading it as `stack[0]`
answers `character` and reports nine pieces of correct vanilla script. PMT
follows `root` as an ordinary link to its declared output; CK3's dump declares
none, so the scope under `root = { }` is unknown and `wrong-scope` stays silent
there. ck3-tiger reaches the same conclusion independently, with
`is_unrooted` / `new_unrooted()` for contexts where "the relationship to root is
not known".

## 18. A dotted key is a scope chain, and skipping it is not neutral

`root.liege.primary_title = { … }` is three hops. A walk that matches keys whole
resolves a dotted key to nothing and stops — silent rather than wrong, and
useless: **5,135 dotted block keys in vanilla CK3 alone** are dead ends
(`domicile_location.culture` 437, `root.faith` 355, `root.location` 277,
`root.culture` 230).

Splitting must happen in one place, not at the callers, so hover and
`wrong-scope` cannot disagree about where a chain lands, and any segment failing
must fail the whole chain, so a half-followed chain never leaves a scope behind
to be judged against. A date is not a chain — `867.1.1` would otherwise step
through "867", "1", "1".

The walk reads a chain's first part as the link, and the scope leads where the
chain's last link does (`schema/README.md`, Positions and the walker, and
`index/README.md`, Scopes).

Saved scopes (`scope:foo`) and prefixed words (`title:k_england`) are followed: a
prefixed word's block runs in its prefix link's output, and `scope:x = { }` is
entered the same way. tiger models both.

## 19. The engine coerces scopes, and nothing declares which

`wrong-scope` judges a token against `Supported Scopes`. That list says where an
effect is *documented*, not where it *works*, and the gap is real script:

| game | written in | declared for | token |
|---|---|---|---|
| CK3 | `province` | `landed_title` | `set_county_faith` |
| Vic3 | `state` | country / front / state_region / … | `random_scope_state` |
| EU5 | `location` | `country` | `add_stability`, `add_country_modifier` |
| EU5 | `location` | `market` | `every_merchant_in_market` |
| EU5 | `location` | `building` | `building_category` |
| EU5 | `province_definition` | `country` | `random_owned_location` |

Every one is vanilla, and every one is the same shape: a contained thing used
where its container or owner is expected, which the engine resolves silently.
`add_stability` appears 1,927 times in EU5's events alone.

### Why it is not yet fixable, measured

- **Not in script_docs.** `Supported Scopes` is the only scope data the game
  prints, and these uses contradict it.
- **Not derivable from declared links.** Tested against all six pairs: links
  explain three (EU5 `location` reaches country, market and building) and miss
  three — CK3 `province → landed_title` and Vic3 `state → country` have no such
  link, and EU5's `province_definition` declares *no* outgoing links at all. A
  one-hop-link rule would also be far too permissive: `location` alone produces
  ten distinct output types, so it would accept ten scopes' worth of tokens.
- **ck3-tiger does not derive it either.** Its `scopes.rs` enforces strict
  matching with no coercion mechanism; where the game's docs are wrong, tiger's
  hand-maintained schema is simply corrected by a person. PMT cannot take that
  route without giving up the property that a game update is a rescan.

`wrong-scope` has no coercion model, so the six pairs above are where it can
accuse correct script.

### What this blocks

Saved scopes (`scope:x = { … }`) — **69,823 block keys in vanilla CK3 alone**,
fourteen times the dotted chains — and prefixed words (`title:k_england = { … }`,
~4,300). Resolving saved scopes was measured at eight false positives over 750
vanilla files, every one a coercion case and none a resolution error: a stricter
re-save rule changed nothing. Entering these blocks without a coercion model
turns correct script into an accusation.

## 20. script_docs is not always right either

The whole engine rests on declared beating derived, and §19 already showed
`Supported Scopes` describing where an effect is *documented* rather than where
it works. The documentation can also be plainly stale.

Victoria 3 ships `common/company_charter_types/company_charter_types.md`:

```
type = <investment/monopoly/new_industry/trade/colonization>
```

and `00_company_charter_types.txt:122` writes `type = industry`. All five other
charter types use a documented value; this one does not, and the game ships it,
so the script is right and the doc is stale.

That is one finding in one file, and it is the reason a declared word list is
contradicted by vanilla's own values. A check for a value
outside a declared set would be correct — a value outside the set the game
states — and the set is wrong. Nothing in the install says which of the two to
believe.

**What this costs.** A closed value set is the strongest evidence PMT has for an
accusing check: the game names the values. If even that can be stale, then every
declared-source check needs vanilla's evidence before it speaks, not just the
derived ones. `MayAccuse`'s vanilla condition is that gate, and the vanilla gate
(`TestInstallsDiagnoseClean`) is the only place PMT compares its judgment
against script the game itself ships.

## 21. A mod's thumbnail location is not implied by its descriptor format

Measured over the 31 Steam workshop mods on this machine, 2026-09-08:

| Game | Descriptor | Convention | root only | `.metadata` only | both | none |
|---|---|---|---|---|---|---|
| CK3 | `descriptor.mod` | root | 3 | 0 | 0 | 1 |
| Victoria 3 | `.metadata/metadata.json` | **root** | 10 | **0** | 4 | 0 |
| EU5 | `.metadata/metadata.json` | **`.metadata/`** | 0 | 11 | 2 | 0 |

Victoria 3 is the case that separates the two facts: it uses the metadata
descriptor and puts its thumbnail at the mod root, and **not one Vic3 mod in the
corpus keeps it in `.metadata` alone**. Deriving the location from the descriptor
format — which PMT did — is wrong for a whole game.

Three further facts from the same corpus:

- **The declaration is a bare file name, always.** Every mod that declares one
  writes `"picture": "thumbnail.png"`, including the EU5 mods whose file sits in
  `.metadata/`. The field is read relative to the descriptor's own directory, so
  PMT must never write a path into it.
- **The declaration is usually absent, and unreliable when present.** Of 14 Vic3
  mods only 2 declare a thumbnail; of 13 EU5 mods, 4 — under two spellings, one
  of them `"picture": null`. The file is discoverable, so detection beats the
  declaration and the declaration is the fallback.
- **Extensions in use: png and gif, no svg.** EU5's Basileia Romaion declares
  `"picture": "thumbnail.gif"`. PMT accepted `.svg`, which nobody ships, and
  rejected `.gif`, which is in use.

**The rule that follows:** adopt what the mod already has — for reading, for
replacing and for the listing — and consult the per-game convention
(`game.GameInfo.ThumbnailDir`) only when the mod has none. This is the same
"don't fight what mods already do" rule the mod calendar follows.

## 22. The GUI, measured

Measured over the three installs on this machine, 2026-10-12, by reading every `.gui` file through the engine's own
parse and index. `<game>` is an install's `game` folder (EU5's `in_game`); the commands run in bash at the Steam
`common` folder.

| fact | CK3 | Vic3 | EU5 |
|---|---|---|---|
| `.gui` files; their bytes; every file parsed once | 558; 8.2 MB; 68 ms | 362; 7.1 MB; 48 ms | 661; 11.8 MB; 75 ms |
| types defined; built-in kinds seen (a base no type defines, or a type based on its own name) | 2,235; 35 | 2,245; 39 | 1,819; 45 |
| types based on their own name (`type hbox = hbox { }`: the kind's defaults) | 18 | 22 | 17 |
| windows (a top-level widget, or a type named as its file) | 735 | 672 | 1,527 |
| `visible` bindings over every window: literal; decided by the opening state; computed; unknown | 9,266; 371; 873; 64,619 | 19,149; 1,201; 4,634; 25,302 | 31,703; 443; 11,110; 87,657 |
| windows with siblings sharing a box (anchor, position, size as written) that both carry a `visible` binding | 368 of 735 | 402 of 672 | 1,093 of 1,527 |

**A type based on its own name is a built-in kind's defaults.** Each game ships `type hbox = hbox { … }` for the kinds
it gives defaults to (CK3 18, Vic3 22, EU5 17 such types); a base no type defines (CK3 17 of 35) is a kind too. A few
kinds are written with no type based on them or named for them, so no file states that they are kinds. Over each whole
install (`game`, `jomini`, `clausewitz`; the debug and tool windows' editor widgets are types `clausewitz` defines)
they are CK3's `zoomarea` and `text_occluder`, Vic3's `overlappingitembox` (39), `treemapchart`, `axis` and
`portrait_button`, and EU5's `axis`, `plotline`, `screen_grab` and `newsfeed_carousel`: `game.UndeclaredKinds`. No
rule finds them in the files without false kinds. A block written `key = { }` among a widget's children that writes a
key sizing or placing its own box (`size`, `position`, the anchors, the layout policies, the margins) finds them in the
game's own folder, a `state` left out (CK3 2,552, Vic3 1,296, EU5 201), whose box keys are its widget's at the state's
end; but over the engine's shared folders, which every Jomini game ships alike, it also takes `attachto` (all three
games), a block that attaches its window to another widget by id with the anchors it attaches by, and EU5's
`header_pattern` and `button_standard`, CK3 and Vic3 types that a shared `jomini` window writes and EU5 does not
define. Learning from a block that holds widgets, or from any key the page lays out (`alpha`, `alwaystransparent`),
would also take a holder's parts (a scroll bar's `slider`, `track`, `inc_button`, `dec_button`, a button's
`buttontext`, a progress bar's `marker`, a dropdown's `active_item` and `list`), EU5's `contextmenu_widget` (72), a
menu shown on a right click, and `glow` (CK3 102, EU5 1,124), an effect drawn around its holder.

```
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hoE '^\s*type\s+(\w+)\s*=\s*\1\b'
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hoE '^\s*(zoomarea|overlappingitembox|contextmenu_widget|glow|state)\s*=\s*\{' | tr -d ' \t={' | sort | uniq -c
```

**The words a widget's layout is written in.** Five layout policy words, case-insensitively: `expanding` (CK3 10,935
uses), `growing`, `preferred`, `fixed`, `shrinking`; three EU5 values are bindings. Anchors are `left right top bottom
center hcenter vcenter` joined by `|`; fit types `centercrop end start center fill`; sprite types `corneredtiled
corneredstretched stretched` (quoted in EU5, and written as `Corneredtiled` and `corneredtiled`); blend modes
`alphamultiply overlay colordodge multiply add normal mask`; directions `vertical horizontal`.

```
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hoE '^\s*layoutpolicy_(horizontal|vertical)\s*=\s*\S+' | sed -E 's/.*=\s*//' | sort | uniq -c | sort -rn
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hoE '^\s*(fittype|spriteType|spritetype|blend_mode|direction|align|parentanchor|widgetanchor)\s*=\s*[^#]*' | sed -E 's/^\s*//; s/\s+$//' | sort | uniq -c | sort -rn
```

**A size written negative is unset on its axis, never a percent.** `size = { -1 32 }` is written under
`layoutpolicy_horizontal = expanding` and `maximumsize = { 400 -1 }` for no maximum: CK3 45 negative sizes and 155
negative maximums, Vic3 35 and 666, EU5 1,916 and 724. A percent is always written with `%`. Reading a negative as a
fraction made a mod's `size = { -10 0 }` spacer ten times its row.

```
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hE '^\s*size = \{ *(-[0-9]+ +[0-9%]+|[0-9%]+ +-[0-9]+) *\}' | wc -l
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hE '^\s*maximumsize = \{.*-1' | wc -l
```

**A row or column under a plain widget takes the room its children expand into.** Of the `hbox` and `vbox` written
directly under a `window`, `widget`, `container` or `margin_widget` in CK3 (549), 4 declare a `size`; 366 hold a
child with `layoutpolicy_horizontal = expanding` or `growing` across the column (or `_vertical` across the row) or a
percent size, and 5 of those carry a `parentanchor` or `position`; 18 with no such child carry one (a footer row of
buttons at `bottom|hcenter`). So a window's main column fills the window and a footer row keeps its content size.
Measured by a walk of every file through the engine's parse, each box's direct children read for the policy across
the box and for a `%` in `size`.

**Siblings shown on a binding and its negation.** Among the children of one block, a `visible = "[X]"` beside a
`visible = "[Not(X)]"` (spaces ignored) is a pair the game shows one of: CK3 780 pairs in 680 blocks of 8,632 bound
`visible`s, Vic3 1,279 in 986 of 7,487, EU5 (`game/in_game/gui`) 1,132 in 984 of 7,902, The Fallen Eagle 257 in 213 of
2,658. Measured by a walk of every file through the engine's parse, each block's direct children's `visible` read.

**A `background` fills its parent and takes no place among its siblings.** It is written directly under a row,
column, flow or grid (CK3 17 times on the line after the box opens, more further in) and over the whole box; laid out as
an item it pushed every sibling by the box's width. **A `multiline` text takes the width the layout gives it** and
wraps there (`multiline = yes` CK3 148, Vic3 519, EU5 79; `text_multi` is `textbox { multiline = yes }`, CK3 640 uses),
where `text_single` is `autoresize = yes` and sized by its words.

```
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hB1 -E '^\s*background = \{' | grep -cE '^\s*(hbox|vbox|flowcontainer|uberhbox|ubervbox|fixedgridbox|dynamicgridbox) = \{'
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hE '^\s*multiline = yes' | wc -l
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hoE '^\s*(scrollwidget|zoomwidget|zoomarea)\s*=\s*\{' | tr -d ' \t={' | sort | uniq -c
grep -rlE --include=*.gui '=\s*zoomarea\s*\{' "<install>"
```

**The blocks a widget writes that are not its children.** A `background = { }` is the widget's skin, filling it and
drawn behind its children (CK3 2,305, Vic3 1,294, EU5 1,455); a `tooltipwidget = { }` is shown on hover and never in the
window (679, 682, 3,811); a `scrollbar_vertical` or `scrollbar_horizontal = { }` is a `scrollbar` at the area's edge
(CK3 59 and 12); a `scrollwidget = { }` is the widget a scroll area scrolls and a `zoomwidget = { }` the one a zoom area
zooms, each a plain widget though no type is named after it (`scrollwidget` CK3 65, Vic3 52, EU5 183; `zoomwidget` 3, 3,
4). A `zoomarea` is a type's base in Vic3 (`modifier_debugger.gui`, `tech_tree.gui`, `ticktask_debugger.gui`) and in
EU5's `jomini` folder, never in CK3, which writes it 3 times. Every other block whose key names a widget is a child,
laid out among its siblings in written order. The properties are the same in the three games (`hbox` CK3 3,545, Vic3
715, EU5 3,267; `layoutpolicy_horizontal` 8,754, 1,939, 4,546; `parentanchor` 4,427, 6,801, 2,837), and the preview
hands them to the browser as written, each mapped to the CSS that means the same (`frontend/src/draw/picture.ts`);
Go measures nothing.

```
for k in background tooltipwidget scrollbar_vertical scrollbar_horizontal hbox layoutpolicy_horizontal parentanchor; do printf '%-24s %s\n' "$k" "$(grep -rhoE "^\s*$k\s*=" "<game>/gui" | wc -l)"; done
```

**What the words mean, measured over every window.** Each whole install's windows were resolved (CK3 687, Vic3 594, EU5
1,346, with the workspaces' mods) and drawn in the app, every text measured against its widget's box; a rule below is
the one under which the game's own windows fit their literal text. GUI files declare constants (`@panel_width = 540`:
CK3 31 at the top of a file and 18 inside blocks, Vic3 806 and 10, EU5 159 and 37) and write them in place of a number
(CK3 382, Vic3 1,091, EU5 397, Vic3's mostly sizes: `size` 388, `minimumsize` 302, `maximumsize` 175), or inline
arithmetic over them, `@[ panel_width - 20 ]` (CK3 2, Vic3 15, EU5 186) with `+ - * /`, brackets and a leading minus;
each file declares the constants it uses, a block's own before the file's. A `textbox` that writes `autoresize = yes` is
its text's size whatever size it writes (`text_multi` writes a default 45 by 45 that windows autoresize up to
`max_width`); the wiki says a textbox is fixed size or autoresized. The layout policies are the wiki's: `expanding`
grows and never shrinks below its own size, `growing` the same at lower priority, `preferred` grows or shrinks,
`shrinking` only shrinks, `fixed` neither; the priority is common, as 1,718 of CK3's 17,141 rows and columns (in 343 of
its 687 windows) mix an expanding child with growing or preferred ones. `ignore_layout = yes` takes a widget out of its box's layout (EU5 271, an
overlay at `size = { 100% 100% }`), and a position may be written in percent of what holds the widget (`position = { 82%
52% }`: CK3 163, Vic3 23, EU5 39). `raw_text` names a loc entry where one exists (57 of vanilla Vic3's 64 key-like raw
texts are entries; the rest are words), a text's `#format` is matched without regard to case (the files define `V`, `R`,
`N`, `I`, `P`, `high`, `weak`; the texts write `#v`, `#r`, `#HIGH`, `#Weak`), and `#underline` is an engine word no file
defines. A texture path may begin with a slash or double one (Vic3 `/gfx/interface/map_markers/small_nib.dds`, CK3
`gfx/interface/icons//icon_barter.dds`), which the game reads as the plain path; a `portrait_texture` is a binding in
every one written (CK3 306, Vic3 209, EU5 176). Two Vic3 templates are written `template = name { }`
(`block_windows.gui`; `map_modes_button_position` places the map-mode button of ten panels): the entry detector reads
a keyword before an equals sign as declaring the tag's name, and `AssignmentAt` finds the statement by that tag.

```
grep -rhE --include=*.gui '^@[A-Za-z_]\w*\s*=' "<install>" | wc -l; grep -rhE --include=*.gui '^\s+@[A-Za-z_]\w*\s*=' "<install>" | wc -l
grep -rhoE --include=*.gui '@\[[^]]*\]' "<install>" | grep -oE '[^][@A-Za-z0-9_. ]' | sort | uniq -c
grep -rhoE --include=*.gui 'position\s*=\s*\{[^}]*%[^}]*\}' "<install>" | wc -l
grep -rhoiE --include=*.gui '^\s*(ignore_layout|portrait_texture)\s*=\s*\S+' "<install>" | sort | uniq -c
grep -rhoE --include=*.gui 'raw_text\s*=\s*"[A-Z][A-Z0-9_]+"' "<game>/gui" | sort -u | wc -l
grep -rhoE --include=*.gui '^\s*(template|local_template)\s*=\s*[A-Za-z_]\w*\s*\{' "<install>"
```

**How a widget's textures blend, read from the game's shader.** The installs ship the GUI shader's source:
`clausewitz/gfx/FX/cw/pdxgui_sprite_base.fxh` in CK3 and Vic3 (EU5: `clausewitz/loading_screen/gfx/FX/cw/`) holds `Blend`
and `ApplyModifyTextures`; the coat of arms headers are under `jomini/gfx/FX/coat_of_arms/` (EU5:
`jomini/main_menu/gfx/FX/coat_of_arms/`). Each `modify_texture` is laid on the widget's texture in order, at its own
`alpha` (CK3 writes one 341 times, Vic3 95, EU5 310), in its `blend_mode`: add, overlay, multiply, colordodge, lighten,
darken and normal all keep the widget's texture's alpha, so an overlay never reaches outside the texture's shape;
`alphamultiply` (CK3 920, Vic3 260, EU5 398) scales that alpha by its own; `mask` (CK3 22, all shimmer effects) draws
nothing and scales every overlay after it. The sprite's overlay (`pdxgui_sprite_base.fxh`, `Overlay(Base, Blend)`)
chooses its formula by the base color, which is CSS's `overlay`. A coat of arms is colored as its headers say: a pattern
blends from a fallback color toward color 1 by its red, color 2 by its green and color 3 by its blue; a colored emblem
starts at color 1, blends to color 2 by its green and color 3 by its red, then takes a full overlay of its own blue. The
coats' overlay is `GetOverlay` (`clausewitz/gfx/FX/cw/utility.fxh`, the same in all three games), which flips the two
and so chooses by the overlay's value: CSS's `hard-light` with the overlay on top. CK3 alone shades both afterwards with
a separate cloth texture (section 24). Neither is a CSS filter: the products of channels and the overlay need the pixels
computed.

```
grep -n -A45 'float4 Blend( float4 Base' "<install>/clausewitz/gfx/FX/cw/pdxgui_sprite_base.fxh"
grep -n -A8 'float4 Pattern( VS_OUTPUT_COA_ATLAS Input, float4 PatternTex )' "<install>/jomini/gfx/FX/coat_of_arms/coat_of_arms_pattern.fxh"
grep -n -B2 -A8 'ifdef COLORED_EMBLEM' "<install>/jomini/gfx/FX/coat_of_arms/coat_of_arms_textured_emblem.fxh"
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hoE '^\s*blend_mode\s*=\s*\w+' | sed -E 's/.*=\s*//' | sort | uniq -c
```

**What a window is as the game opens it.** A window's state named `_show` (CK3 455, Vic3 180, EU5 53) and every state
with `trigger_on_create = yes` (305, 117, 5) run as it opens, writing their end values (`position_x`, `alpha`) as the
widget's own; `_hide` does not.

```
find "<game>" -name '*.gui' -print0 | xargs -0 grep -hoE '^\s*name\s*=\s*_[a-z_]+' | sed -E 's/.*=\s*//' | sort | uniq -c | sort -rn
```

**What no file answers.** A binding's function names tell what it reads. `Exists`, `HasValue`, `IsTabSelected` and
`Get` belong only to the variable systems, the tab holders and `Context`; `IsTooltipLocked` only to `PdxGuiWidget`; the
pointer's `IsHovered`, `IsMouseOver`, `IsHighlighted` and `IsPressed` to widgets. All of them read what is unset as a
window opens. The rest is game data the files do not hold (`ScriptedRelation.HasRelationBetween`, CK3 6,900 uses;
`ConditionLine.HasFailed`, EU5 4,874). The preview answers none of them: a bound `visible` draws its widget and a
bound text is drawn as written.

```
cat "<Documents>/<game>/logs/data_types/"*.txt | grep -oE '^[A-Za-z0-9_]+\.(Exists|HasValue|IsTabSelected|Get|IsTooltipLocked|IsHovered|IsMouseOver|IsHighlighted|IsPressed)\(' | sort | uniq -c
```

**The functions whose meaning is the engine's.** The dumps declare `And`, `Or`, `Not`, the typed comparison and
arithmetic families (`GreaterThan_int32`, `Select_CString`, …), `And3` to `And8`, `IsDataModelEmpty`, `Localize`,
`Concatenate` and the conversions, each with its return type, and no file states what they compute; a data model's row
index is known to the widget it is drawn for (`PdxGuiWidget.GetIndexInDataModel`), which `IsOdd_*` and `IsEven_*`
(EU5 15,741 uses, Vic3 913) read. A function's return type of `[unregistered]` is the dumps' word for a type they do
not name (CK3 7,741 functions): the property it is read for says what it is.

**A data binding macro** is a `data_binding` entry: a `definition = "Name(A,B)"` and a `replace_with` text over its
parameters. The dumps do not declare macros as global functions.

```
head -12 "<game>/data_binding/00_script_value_bindings.txt"
```

**Text.** A `font` entry names a `fontstyle` per style (CK3 regular, bold and italic; EU5 regular only), each a
`fontfiles` entry of language groups whose files are tried in order: a file lacks glyphs the next one has (Noto Sans SC
has no `ż`, Open Sans no CJK), so a glyph is measured in the first file of the group that has it. Formats are
`gui.format` entries of `;`-joined words (`color:{r,g,b}`, `bold`, `italic`, another format's name); text icons are
`texticon` entries. In localization, `#format text#!`, `@icon!`, `$key$` and `[call]` are the markup (CK3 `#EMP` 5,890
uses, Vic3 `#v` 3,147, EU5 `#T` 1,275; `[…]` 195,886, 61,161 and 114,128). The default font and size of a text are the
types' own: `preload/defaults.gui` declares `type textbox = textbox { font = "StandardGameFont" fontsize = 15 … }` (lines
141 to 146) and the same for `type editbox`, and no other type of the file declares a font; a widget that is no text
box (a `game_button` with a `text`) writes no size of its own and is drawn in this one.

```
grep -oE 'style\s*=\s*\w+' "<game>/fonts/fonts.font" | sort | uniq -c
grep -rn -A3 '^\s*format = {' "<game>/gui/preload/textformatting.gui"
cat "<game>/localization/english/"*.yml | grep -oE '#[A-Za-z_]+' | sort | uniq -c | sort -rn | head
```

**Anchors.** A widget names the point of its parent it sits at with `parentanchor` (CK3 4,407 lines) and the point of
itself that goes there with `widgetanchor` (328 lines): the second is left out almost always, and then the game puts the
widget's own `parentanchor` point on it. `window_council.gui` writes `parentanchor = top|right` with no `widgetanchor`
and the game docks the window at the right edge; `window_character.gui` writes a 25 by 25 button with `parentanchor =
center` and no `widgetanchor`, which the game centers.

```
grep -rhoi parentanchor "<ck3>/game/gui" | wc -l
grep -rhoi widgetanchor "<ck3>/game/gui" | wc -l
```

**Where a widget's size comes from.** The box types are written without a size and the plain types with one. Every
block-valued key of every vanilla CK3 `.gui`, with and without a direct `size`: `hbox` 3,507 blocks, 20 sized; `vbox`
3,149, 12; `container` 1,009, 32; `flowcontainer` 517, 9; `fixedgridbox` 305, 10; `dynamicgridbox` 52, 0;
`background` 2,321, 19; `text_single` 3,418, 72 (1,106 with a `minimumsize` or `maximumsize`, 399 with `autoresize`);
against `icon` 2,431, 2,205; `widget` 1,955, 1,633; `margin_widget` 97, 89; `scrollarea` 63, 59; `overlappingitembox`
21, 18; `button_group` 42, 5. So the first group takes its size from what it holds or from its parent, the second
declares it. The counts come from a parse; this reproduces them to a few percent, a line at a time:

```
find "<ck3>/game/gui" -name '*.gui' -print0 | xargs -0 cat | awk '{ l=$0; sub(/#.*/,"",l); if (d>0 && l~/^[ \t]*size[ \t]*=/) s[d]=1; k=""; if (l~/^[ \t]*[A-Za-z_0-9]+[ \t]*=[ \t]*\{/) { k=l; sub(/^[ \t]*/,"",k); sub(/[ \t]*=.*/,"",k) }; f=1; for (i=1;i<=length(l);i++) { c=substr(l,i,1); if (c=="{") { d++; n[d]=f?k:""; f=0; s[d]=0 } else if (c=="}" && d>0) { if (n[d]!="") { t[n[d]]++; if (s[d]) w[n[d]]++ } d-- } } } END { for (k in t) print k, t[k], w[k]+0 }' | sort -k2 -rn | head -30
```

**A percent size and the box that holds it.** `Window_Size_MainTab` is `size = { 655 100% }` and `Window_Margins_Sidebar` is
`margin_right = 18`, with no size: `shared/windows.gui` gives the character window's main column a width and its sidebar
none. Five `button_tab`s at `size = { 100% 42 }` under one `hbox` share the box's width (610 there, 122 each); they do
not make it five windows wide.

```
grep -n -A3 'template Window_Size_MainTab\|template Window_Margins_Sidebar' "<ck3>/game/gui/shared/windows.gui"
```

**A concept's loc key.** `common/game_concepts` holds 1,126 entries in CK3, 611 in Vic3 and 695 in EU5 (under
`main_menu/common/game_concepts`). The title of one is the loc key of its name in the game's english folder, with a
prefix in two games: CK3 `abandon_celibacy`, `abandon_hook_interaction` and `absolute_control` are `game_concept_<name>`;
EU5 `ability`, `accepted_culture`, `action` and `societal_value` are `game_concept_<name>`
(`main_menu/localization/english`); Vic3 `concept_acceptance`, `concept_acceptance_status` and
`concept_accepted_culture` are their own keys. A concept block holds `alias` and `texture` and no loc field, so its
title is found by the key and by nothing in the block. In a text the link is `[name|E]`; The Fallen Eagle's
`bottom_bar` writes seven of them (`dux`, `caliph_autho`, `great_house_favor`, `great_house_strength`,
`magister_militum`, `zakah_bank`, `augustus`).

```
grep -c . "<ck3>/game/common/game_concepts/"*.txt | head
grep -rn '^game_concept_abandon_celibacy\|^ game_concept_abandon_celibacy' "<ck3>/game/localization/english" | head -3
```

**A block named in a type is overridden below it.** `blockoverride "n" { … }` written in a widget replaces the `block
"n"` of the type the widget is, and of every type it instantiates below: a window's content is usually its own
`blockoverride` of a base type's `block "entire_content_area"`, written inside a child of the base's body. An override
written in the same type as the block it names is read as a block.

## 23. A color is told by its place, not by its numbers

A block of three or four numbers is a color only where its place says so: script writes far more of them that are no
color (`position = { x y z }`, CK3 90,376 times, Vic3 4,858, EU5 107,518; `rotation`, `scale`, a camera's `look_at`,
a light's `sun_direction`) than colors. A tagged block (`rgb { }`, `hsv { }`) is a color anywhere. An untagged one is a
color under a key ending in `color` (a culture's `color`, a GUI file's `fontcolor`), but a key naming a portrait gene
(`hair_color` holds two pairs into a color map), and where vanilla writes nothing at its position but colors, one of
them tagged, and words, which name colors there: a named color's entry, which CK3 writes untagged 82 times of 112 (30
tagged), Vic3 3 of 185 and EU5 0 of 4,103, and a coat of arms' `color1` to `color5`, written as numbers, as a named
color (`"red"`), as `list "x"` or as another slot (`color1 = color2`). A mod writes them as its game does (TFE's
`culture_colors.txt` 82, its coats of arms 26). Over the three installs and their workspaces' mods, that last rule
colors those and one EU5 `color2`, nothing else. Two rules that look the same fail: "the position of a tagged color"
colors the map's lighting files (`gfx/map/environment`, whose entries mix `hsv { }` colors, plain numbers and
`sun_direction` vectors at one position; the numbers keep the rule above off them), and "a block beside a tagged
color" colors 2,067 portrait camera `look_at`s in CK3.

```
for g in "Crusader Kings III/game/common" "Victoria 3/game/common" "Europa Universalis V/game/main_menu/common"; do
  cat "$g/named_colors"/*.txt | grep -cE '^\s*\w+\s*=\s*(hsv|rgb|hsv360|hex)\s*\{'   # tagged
  cat "$g/named_colors"/*.txt | grep -cE '^\s*\w+\s*=\s*\{\s*-?[0-9.]'                # untagged
done
grep -rhoE --include=*.txt '\bposition\s*=\s*\{\s*-?[0-9.]+\s+-?[0-9.]+\s+-?[0-9.]+' "<game>" | wc -l
```

## 24. A coat of arms, measured

The three games write one coat language: `pattern`, `color1` to `color5`, `colored_emblem` and `textured_emblem` with a
`texture`, colors, a `mask` and `instance`s of `position`, `scale`, `rotation` and `depth`, and `sub` with a `parent`,
its `instance`s an `offset` and a `scale`. Counted in each folder's files (the key at any depth; CK3, Vic3, EU5):
`instance` 10,236, 9,280, 17,303; a `rotation` other than 0 1,067, 127, 94; `mask` 161, 42, 499; `depth` 468, 0, 0;
`sub` 0, 295, 1,330. EU5 keeps its coats under `main_menu/` (`game.CoatFolders`).

**Which way depth draws.** A larger `depth` draws behind. CK3's designer writes depths into its exports, and they read
only that way: `h_eastern_roman_empire`'s eagle is 6.01, two shields at one place 5.01 and 4.01, its letters 3.01 down
to unwritten, and only larger-behind shows the letters on the shields; the game draws `k_bosporan_kingdom`'s red eagle
(1.01) over its white escutcheon (2.01). Equal depths keep the written order, the later in front (the game's
`k_papal_state`: the silver key, written second, over the gold). Of the 262 CK3 coats that write a depth, 178 paint in
another order than written, and 7 interleave one emblem's instances with another's (`d_samarra`: a sabre at 2.01, the
frame at 1.01, the other sabre unwritten).

**Which way rotation turns.** A positive `rotation` turns clockwise. `ce_key_st_peter.dds` is an upright key with its
bit on the left; `k_papal_state` writes the gold key at `-40`, and the game draws it leaning left, its bit outward. The
shader turns the unit quad in the field's own units before it scales it (`coat_of_arms_textured_emblem.fxh`'s vertex
shader), so a turned instance with an uneven scale, or any on a 3:2 field (Vic3, EU5), is sheared as a box turned after
sizing is not.

**Where an instance is.** `position` is its center as fractions of the field, `scale` its size, a negative one a flip of
that axis; unwritten, an instance is at the field's middle at full size (CK3 569, Vic3 43, EU5 4 instances write no
position, all centered frames: the wiki's `{ 0 0 }` would put them on the corner). A `sub`'s `offset` is its top-left
corner and `scale` its size (Vic3's `AUS_absolute_monarchy`: `scale { 0.45 0.45 }` at `offset { 0.275 0.05 }`,
centered).

**What a mask keeps.** `mask = { n }` keeps an emblem where the pattern's region n is: region 1, 2, 3 is red, green,
blue, each less the later channels (red less green and blue, green less blue), the emblem's alpha times their sum,
sampled at the field's place under each pixel. Patterns are cumulative, so the subtraction matters:
`pattern_vertical_split_01` is red everywhere and green on its right half, and `mask = { 1 }` keeps its left half.

**What a pattern starts from.** Every pattern is red everywhere with alpha 255, so the shader's fallback color never
shows, but in Vic3's `pattern_shield_heater`, black outside its shield and used by subs: the fallback is transparent,
the blend carrying the slots' alpha, and the pattern's own alpha is unused.

**What a sub inherits.** Every vanilla sub writes a `parent` (Vic3 295, EU5 1,330) and beside it only colors (Vic3 13
subs, EU5 168) and its `instance`s; no parent writes a parent or subs of its own, and no coat writes a top-level
`parent`. An emblem's `color1 = color2` names its coat's slot (Vic3 1,601 times, EU5 14,276), so a sub's colors reach
its parent's emblems (EU5's `ABW`). The wiki's further rules (a coat-level `parent`, emblems inherited all or nothing)
are never exercised by vanilla.

**Color 3.** The colored emblem shader blends to color 3 by red, and vanilla writes `color3` under a `colored_emblem`
1,031, 464 and 2,209 times: the wiki's "currently unavailable" is out of date.

**CK3's cloth.** CK3 replaces jomini's coat shaders with its own (`game/gfx/FX/coat_of_arms/`): the pattern and every
emblem take a 20% overlay (`GetOverlay`, the flipped one) of `gfx/coat_of_arms/coa_mask_texture.dds`'s blue, and each
emblem's alpha is multiplied by twice its green. Vic3 and EU5 draw their coats flat.

**The field and its frames.** The field is the first atlas' `tile_size`: CK3 230 × 230, Vic3 384 × 256, EU5 192 × 128.
CK3's windows cut a coat with a mask its GUI names, `coat_of_arms/title_mask.dds` (a shield, 13 uses) or
`frames/dynasty_mask.dds`; others are bindings the game picks (a realm's by government, a house's by culture). A mod can
replace them (The Fallen Eagle's are plain squares).

**Lists and aliases.** A value can be `list "x"`, a template list's pick (CK3 2,397 such values in 4,129 coats, Vic3 667
in 1,817, EU5 2,528 in 5,076); CK3 writes 527 coats as another coat's id (`b_vaticano = k_papal_state`).

```
D="<install>/game/common/coat_of_arms/coat_of_arms"   # EU5: <install>/game/main_menu/common/coat_of_arms/coat_of_arms
cat "$D"/*.txt | grep -o 'rotation\s*=\s*-\?[0-9.]*' | grep -vc '= *0$\|= *0\.0$'
for k in instance mask depth sub; do cat "$D"/*.txt | grep -o "$k\s*=" | wc -l; done
grep -n -B12 -A40 '^h_eastern_roman_empire' "$D"/01_landed_titles.txt
grep -rn -A10 'Pattern( VS_OUTPUT_COA_ATLAS Input, float4 PatternTex )' "<install>/jomini/gfx/FX/coat_of_arms/"
grep -rn -B3 -A12 'USE_PATTERN_MASK' "<install>/jomini/gfx/FX/coat_of_arms/coat_of_arms_textured_emblem.fxh"
grep -rn 'GetOverlay\|MaskTex' "<CK3>/game/gfx/FX/coat_of_arms/"
grep -n -A2 '^atlas' "<install>/game/common/coat_of_arms/options/atlases.txt"
grep -rhoE 'coat_of_arms_mask\s*=\s*"[^"]+"' "<CK3>/game/gui" | sort | uniq -c | sort -rn
```
