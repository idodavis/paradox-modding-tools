# Observed game syntax

What CK3, Vic3 and EU5 actually do, as measured against real installs — not what
the engine assumes. **When a game update breaks PMT, start here:** re-run the
measurement in the row that broke and compare it against the recorded value.

Every claim below has a reproduction. Run them with the install env vars set:

```
PMT_TEST_INSTALL_CK3   PMT_TEST_DOCS_CK3     (…/Documents/Paradox Interactive/Crusader Kings III/logs)
PMT_TEST_INSTALL_VIC3  PMT_TEST_DOCS_VIC3    (…/Victoria 3/docs)
PMT_TEST_INSTALL_EU5   PMT_TEST_DOCS_EU5     (…/Europa Universalis V/docs)
```

Measured 2026-09 against CK3 1.19.x, Vic3 and EU5 current on Steam.

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

**Block syntax per game** — three splitters in `catalog/schema.go`:

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
target type; CK3 documents it in prose only. This is why fire keys stay derived.

---

## 2. Folder rules

| Fact | Detail |
|---|---|
| Kind comes from the parent folder | `common/cultures/` → kind `cultures`. `game.MatchExtract`. |
| EU5 nests under a stage root | Script lives in `game/in_game/...`, not `game/...`. Walking `game/events` finds nothing on EU5. |
| script_docs land in different folders | CK3 `logs/`, Vic3 and EU5 `docs/`, all under user data. |
| The console command and launch option are the **same** in all three | `-debug_mode` and `script_docs`. Verified by grepping each game binary (`ck3.exe`, `victoria3.exe`, `eu5.exe`); all three also carry the longer alias `script_documentation`. Only the output folder differs, which `ScriptDocsSubdir` already encodes — so the walkthrough needs no new per-game data. |

### Where the install's version and timestamp are (and are not)

| Source | CK3 | Vic3 | EU5 |
|---|---|---|---|
| `launcher/launcher-settings.json` | present but **11 days stale** vs the real update | accurate | **does not exist** — no launcher directory at all |
| Newest mtime in `binaries/` | 2026-06-05 02:25:14 | 2026-08-19 15:27:13 | 2026-07-16 14:08:11 |
| Steam `appmanifest_<id>.acf` `LastUpdated` | 02:25:21 | 15:27:15 | 14:08:16 |

**EU5 publishes its version nowhere in the install.** Anything that needs "which
build is this" must not depend on a version string.

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
| Loose `.txt` at the script root is not script | `checksum_manifest.txt`, `credits.txt`. Parsing the manifest as Jomini produced fire edges for `common`, `events`, `history`. |
| CK3 faiths have **no folder** | They exist only nested under `religion_types`, so `faith:` binds to nothing without a nested harvest. Verified: no `faiths/` directory exists. |
| EU5 `production_methods` **does** have a folder | `in_game/common/production_methods`. A nested shape for it is redundant. |

---

## 3. Shapes that look identical but are not

This section is the one that keeps costing us. Each row is a case where two
different things are **syntactically indistinguishable**, so the discriminator
has to come from outside the syntax.

### 3a. Setup wrapper vs. scripted macro

```
# Vic3 common/history/countries.txt — a wrapper; COUNTRIES is not an object
COUNTRIES = { c:USA ?= { ... } }

# CK3 common/scripted_triggers/… — a definition; the name IS the object
hegemon_favors_advancement_trigger = { title:h_china.holder ?= { ... } }
```

Character for character the same shape, `?=` included. **No body-shape rule can
separate them.** The discriminators:

- `game.DeclaresNamedBodies(kind)` — a `scripted_*` / `script_value*` folder has
  already declared that its top-level keys are definitions.
- `keywordDeclared` — `scripted_effect foo = { }` inside an events file is a
  declaration; the keyword states it.

Cost of getting this wrong: 33 CK3, 30 EU5 and 20 Vic3 vanilla macros deleted
from the model, breaking go-to-definition on every one.

### 3b. Nested database vs. repeated inner key

`deriveNestedShapes` looks for `prefix:id` citations whose id turns up as a
block child of some other def. Without a filter it treats **any** repeated inner
key as a database:

| Game | Shapes before filter | Phantom defs minted |
|---|---|---|
| CK3 | 133 | **342,138** under kinds `SKILL`, `TIER`, `NUMBER`, `LEVEL`, `BUILDING_LEVEL`, `$SKILL$` — script *parameter* names |
| EU5 | 183 | 35,458 under `CALL_FOR_PEACE_WARSCORE_LIMIT`, `var:chinese_expedition_opinion` |
| Vic3 | 134 | 5,231 under scripted constants like `@portrait_box_height` |

On CK3 that was **64% of the entire model**. The discriminator is
`Schema.Declares(childKind)` — the child must be a scope type or scope link the
game names. That leaves CK3 6 shapes, Vic3 4, EU5 15, including
`religion_types -> faith`.

Root cause worth remembering: `game.typedKindOK` accepts **any** `word:word` as
a typed cite, with only a small denylist. The lexer is deliberately permissive;
everything downstream must therefore re-check against the schema.

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
easy to violate somewhere else by accident. It has already cost twice:

- **Option databases.** `NestedShape.SkipKeys` was first computed as *"every
  inner key the field never referenced"*. That swept in every genuine row
  vanilla happens not to cite — CK3's `feudal_elective_succession_law` among
  them, which then showed up as a dangling reference on a real mod. The
  discriminator is not citation but **repetition**: an ordinary field appears in
  most instances of the parent (`categories` in 81 of 81 game rules, `flag` in
  24 of 27 law groups), a row appears in one.
- **Field typing** and **fire keys** both counted only the values that resolved,
  so a single coincidental match typed the whole field or key. See §3e.

**Rule: derive membership from how a thing is written, not from whether anything
in vanilla points at it.**

## 3e. Unanimity is not coverage

Three separate passes resolved conflicts by unanimity while silently dropping
values that resolved to nothing. One coincidental hit was therefore enough to
claim a fact, and every later use of it emitted a reference that could never
resolve. Measured on vanilla:

| Pass | Worst case | Floor added |
|---|---|---|
| `deriveFieldValueKinds` | `has_cultural_parameter` typed `ep_3` on **1 of 432** values; `remove_variable` typed `achievements` on 2 of 392 | `fieldKindCoverage` 50% |
| `resolveFireKinds` | Vic3 `alert`, `character` became on_action fire sites — **4,033** false rows on one mod | `fireKeyCoverage` 50%, but only once a key carries `minFireNames` (8) names |
| `deriveNestedShapes` | 342,138 phantom defs (§3b) | `Schema.Declares` |

`minAffixDefs` (8 definitions must share a localization naming shape) and
`minLocFieldHits` (8 values of a property must be keys, at 80% coverage) are the
same rule applied to the two localization derivations.

The fire-key floor needs the extra minimum because there a *low* hit rate is
often the defect worth reporting: a mod whose `trigger_event` targets mostly do
not exist has broken references, and vetoing the key would hide exactly what
Workspace Health is for.

Effect on real mods, dangling references reported:

| Mod | Before | After |
|---|---|---|
| More Historicity (CK3) | 1,086 | **3** |
| Fix Trade Companies (EU5) | 86 | **4** |
| Morgenroete (Vic3) | 698 | 139 |

## 3f. The same spelling means different things in different slots

The games reuse names across unrelated kinds constantly, and the engine is never
confused because it always knows which slot it is reading. A resolver that looks
a name up across every index has no such context, and breaking the tie by load
order silently lets load order decide *what kind of thing a name is*.

Measured 2026-09-06, all four found as wrong hover/peek cards:

| Written | Also spelled the same | Wrong card | What actually decides |
|---|---|---|---|
| `has_realm_law = x` | the loc key `has_realm_law` | the caption "Realm Law" | it is **left of an `=`** — `probe.inKey` |
| `culture = french` | the culture's own loc key | the caption | a value naming an object is the object — `resolveNonLoc` |
| `add_trait = conqueror` | `namespace = conqueror` in the mod | the namespace, because mod beats vanilla | a namespace is not an object — `jomini.IsObjectKind` |
| `random_list = { 95 = … }` | province `95` in `map_data/positions.txt` | the town Amiens | the **file** that defines numeric rows — `isLocalDefFile` |

The last one is worth stating plainly because it is a whole class: **a bare
number on the left of an `=` is a weight** (§3c says the same of fire tables),
and vanilla CK3 keys map positions the same way. Nothing distinguishes the two
by name; the file does.

A weight's share is computed from its siblings, never assumed to be out of 100 —
`{ 10 = {…} 30 = {…} }` is a quarter and three quarters. It is only claimed
where the branches are **bodies**: `random_events = { 100 = some.event }` (§3c)
and a Vic3 gene block's `20 = empty` both key scalars, and nothing separates a
weight from a lookup index there, so the card says nothing rather than guessing.

**The rule:** load order decides which *definition of the same kind* wins,
nothing else. Every discriminator above is a structural fact from the parse tree
— left of `=`, which file, whether the branch is a body — so none of them is a
list of names that a game update can invalidate.

`jomini.IsObjectKind` is the predicate for "a thing script can point at". Its
complement is three kinds, one per bug above: ephemerals, `loc_key`, and
`namespace`. Use it when resolving a name to a thing; not when asking whether a
name is declared at all, because a namespace genuinely is.

## 3g. A property holds a loc key — but a value is not always one

`parser/loc` splits loc-key properties into **strict** (an unresolved value is a
defect worth a diagnostic) and **broad** (a resolved value gets a hint, an
unresolved one is silent). Measured over vanilla, where nothing is missing by
definition, the split was wrong in two ways.

**`localization_key` is not strict.** Its value is a stem that customizable
localization completes at runtime — `localization_key = CustomLoc_BR_male_`
ends in an underscore because the engine appends to it. Unresolved values on
vanilla:

| Game | `localization_key` | All other strict props | `localization_key` share |
|---|---|---|---|
| CK3 | 2,607 | 730 | 78% |
| Vic3 | 11,597 | 271 | 98% |
| EU5 | **46,481** | 139 | **99.7%** |

Now broad: still harvested, so the key counts as used and hovers with its text;
it just no longer asserts a key the game builds itself is missing.

**Quoting does not tell you anything.** The obvious guess — quoted means display
text, bare means a key reference — is wrong. Of quoted strict-property values on
vanilla, **86% (CK3), 99% (Vic3), 92% (EU5)** resolve to a real key: Paradox
quotes keys freely (`war_name = "INDEPENDENCE_WAR_NAME"`). Do not use quoting as
a discriminator.

**Whitespace does.** A localization file is `key:0 "value"`, so a key is a
single token and can never contain a space. `desc = "Always make coronations!"`
is display text the game shows verbatim, and demanding a key by that name was
537 of A Game of Thrones' 618 `missing-required-loc` findings. `LooksLikeKey`
now rejects any value containing whitespace.

**Keys hide in lists, not only on the right of an `=`.** A list member is a
value, not an assignment, so a harvest that reads `field = value` structurally
cannot see one — and the games put localization keys there in quantity:

```
male_names = { Abu-Bakr Aarif Abdul-Gafur … }   →  Abbas:0 "Abbas"
cadet_dynasty_names = { "dynn_Rasulid" … }      →  dynn_Rasulid:0 "Rasulid"
```

Quoting is again no signal: vanilla quotes its cadet dynasty names and not its
given names. Which list-valued properties hold keys is derived exactly as for
assignment values — same floors, same declared-token exclusion, different value
source — giving **11 fields on Victoria 3, 19 on EU5, 168 on CK3** (mostly name
*equivalency* blocks, `alexander_male = { Alexander Alexandros … }`, which are
genuinely lists of keys).

Measured: name-list members were **6,978 of A Game of Thrones' 20,196** orphaned
English keys, 35%. Corpus-wide the fix took orphaned **75,686 → 50,438** on CK3
and **99,560 → 92,770** on Victoria 3, EU5 unchanged. It costs references —
`locRefs` +23% on CK3, +51% on EU5 — and EU5's Workspace Health live heap rose
from 2.1 GB to 3.9 GB, which Phase 9 has to budget against.

**A convention nine definitions in ten share is a habit, not a requirement.**
Vanilla is complete by construction, so for any convention `Of - Defs` is exactly
how many vanilla definitions a "missing required key" check would warn about —
which makes the floor measurable rather than a matter of taste:

| floor | CK3 conventions / vanilla warnings | Vic3 | EU5 |
|---|---|---|---|
| 90 | 197 / 475 | 164 / 119 | 301 / 270 |
| 95 | 175 / 230 | 160 / 55 | 287 / 224 |
| 99 | 137 / 16 | 143 / 13 | 253 / 71 |
| **100** | **126 / 0** | **135 / 0** | **244 / 0** |

Only 100 clears the gate, at a cost of 19–36% of the conventions. EU5 gives 90%
of its diplomatic actions a `WE_PERFORM_<id>_ACTION_BTN3` key, and an action with
two buttons is not defective. On the workshop corpus this took `required-loc`
from 12/185/401 to **5/103/61**.

Three floors, three questions, deliberately not one constant: `LocConventionUsed`
(20 — might the engine read this key?), `LocConventionStored` (90 — is it worth a
stored, navigable reference?), `LocConventionRequired` (100 — does the game demand
it?). While the last two shared a value, tightening the diagnostic silently
shrank the set of keys counted as used and pushed EU5's orphan count up 143.

What remains after both fixes is genuinely ambiguous and vanilla shares it:
`desc = "base"`, `desc = "Pyke"`, `desc = "Cowardly"` are single tokens
indistinguishable from keys, and CK3 vanilla itself has 233 of them. No
structural rule separates these; do not invent a heuristic for them.

## 4. Reference forms

An object is not always cited as `prefix:id`. Anything keyed only off typed
cites will miss these.

| Form | Example | Where it appears |
|---|---|---|
| Typed cite | `culture:english`, `title:k_x` | everywhere; drives `bindKinds` and nested shapes |
| Dotted chain | `character.culture`, `title:h_china.holder` | scope walks |
| Field RHS | `has_game_rule = suf_quieter` | **not a typed cite** — handled by `deriveFieldValueKinds` |
| Keyword pair | `scripted_effect foo = { }` | inline macros, GUI `type`/`template` |
| Saved scope | `save_scope_as = x` then `scope:x` | per-file, ephemeral |
| Script param | `$SKILL$` inside a `scripted_*` body | ephemeral, owner-scoped |

---

## 5. Field-referenced nested options — an unindexed class

A recurring shape: a database whose rows live **inside** another def's block, and
which is referenced by a **field RHS** rather than a `prefix:id` cite. The
nested-database pass is cite-driven, so it is structurally blind to all of them.

| Case | Defined in | Referenced as |
|---|---|---|
| Game-rule options | `common/game_rules/`, inside a rule block | `has_game_rule = suf_quieter` |
| Cultural parameters | inside culture tradition blocks | `has_cultural_parameter = has_access_to_shieldmaidens` |

Measured on real mods, these are the largest remaining source of false
"dangling reference" rows in Workspace Health — `events_karling_decline_rule`
×17 and `has_access_to_shieldmaidens` ×10 on *More Historicity - CK3*, both of
which are defined, just not indexed.

**Solved by `catalog/options.go`.** `deriveNestedOptions` joins each field
against the nested blocks of the install: when nearly every value a field takes
is a child of one particular block, that block holds the database. It is a
deterministic join over many citations, like `bindKinds` — not a shape guess.
Three filters, tuned against all three games:

| Filter | Value | Why |
|---|---|---|
| `minOptionRefs` | 8 distinct names | below this, coincidence dominates |
| `optionCoverage` | 95% of the field's values | real cases 96–100%, noise 80–92% |
| `optionOwnerShare` | 50% of the block's inner keys | EU5 `start` matched 6,009 of 38,182 keys — not an option database |

What it finds, per game:

| Game | Option databases |
|---|---|
| CK3 (12) | `game_rule`, `realm_law`, `cultural_parameter`, `doctrine_parameter`, `domicile_parameter`, `religion` (faiths), `current_phase`, `participant_group_type`, `task_contract`, `track` |
| EU5 (4) | `game_rule`, `language` (dialects), `country_color`, `target` |
| Vic3 (3) | `game_rule`, `secret_goal`, `target` |

**The shape stores the complement.** `NestedShape.SkipKeys` lists the block's
*ordinary fields* (`categories`, `default`), not its rows — so anything else
under the parent is a row, and a mod's own new option is picked up without
re-deriving. Storing the row names instead froze mods out, which is what left
`events_karling_decline_rule` dangling on *More Historicity - CK3*.

`applyOptionShape` also ignores the value's shape: a game-rule option is a
block, a cultural parameter is `name = yes`, and both are equally real.

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

- The **rule** is a def (folder kind `game_rules`). PMT indexes it.
- The **options** (`suf_vanilla`, `suf_quieter`) are real named objects. PMT
  does **not** index them — 7 rules, 0 options on a real mod.
- They are referenced as `has_game_rule = suf_quieter` — a field RHS, never
  `game_rule_setting:suf_quieter`. Nothing in vanilla CK3 or in any mod checked
  writes that prefix, which is why the cite-driven nested pass cannot see them.
- Localization convention: `rule_<rule_id>` for the rule, `setting_<option_id>`
  and `setting_<option_id>_desc` for each option.

Counted on vanilla CK3: **78 rules, 77 with a `rule_<id>` key** (99%), and **380
options with 590 `setting_*` keys**.

The loc side is **done**, and it did not need the options to be definitions.
`deriveLocAffixes` covers `rule_<id>`, because a rule is a def. `setting_<id>`
is covered by `deriveLocMemberAffixes`, which runs the same derivation over
**block-member key names** instead of def keys — the option names were already
harvested into `Structures` and then discarded. `BuildIndex` now carries them
too, so a mod's *new* option is recognised even though it exists in no install.

The two maps are kept apart on purpose. Member names include ordinary field
names (`default`, `categories`), and letting a def-keyed convention such as
`<id>_desc` match one of those would explain away real orphans.

Still open, and unaffected by the above: harvest the options as **defs**, and
let `has_game_rule` resolve to them via field-value kinds. That is what
go-to-definition and completion on an option need. Tracked in the plan.

---

## 6. Total-conversion scale is the measurement

Everything above is about what the games contain. This section is about what
that volume does to code that reads it, because the two are the same subject:
every entry here was correct on vanilla and only wrong on a mod big enough.

Whenever a loop does expensive work and *then* decides whether it was needed,
that is a bug waiting for a big enough mod. Five instances so far, one shape:

| Where | What it did | Cost at total-conversion scale |
|---|---|---|
| `scriptParamItems` | took the first 80 script params, then kept this macro's | returned nothing, across 2,536 CK3 macros |
| `defsOfType` | `FindDefs` with no kind, then filtered by kind | returned nothing with an empty prefix |
| `views.refRows` | built a site (file read + line split) per reference, then called `Resolve` | hundreds of thousands of discarded reads |
| `views.Coverage` | built a site per loc issue per language, then applied `healthLocCap` | ~99% of the work discarded |
| `catalog.ConventionOwner` | built its visitor closure per *span* of a key rather than per key | 22 allocations and 448ns to answer one key; 4 and 155ns after |

**The rule:** filter before the limit, and decide before you materialise. Where a
budget exists, spend it on the *expensive* half — count cheaply and uncapped so
KPIs stay honest, materialise only what is kept. `FindMacroDefs` and
`Session.FindParamsOf` document the filter-before-limit half; `views.Coverage`
documents the count-then-materialise half.

The same rule decides where a derived fact is *stored*. A localization naming
convention could be expanded forwards — one reference per definition per
convention, hundreds of thousands of rows on CK3 — or applied backwards by
`ConventionOwner` only to the few keys that still look orphaned. It is applied
backwards.

### Scheduling must not decide anything

The corpus walk is parallel, and twice that let worker completion order decide a
fact about the game. Both were found by scanning one install twice and diffing
the caches, which is now `TestHealthIsDeterministic`:

| Where | What order decided | Symptom |
|---|---|---|
| `accum.merge` → `MergeLoc` | which value survives when two files of one language define the same key | `untranslated` moved by 1–2 between identical runs |
| `deriveNestedOptions` | which field names an option database, because the greedy loop consumes values as it assigns owners | two scans disagreed on whether CK3's `genes` database was `template` or `positive_mirror` — **352 defs under a different kind**, and `orphaned` differed by 30 |

Both are now ordered: localization merges under **load order** (the file's index
in the walk list, which is mod order then walk order — the same rule that
decides every other override), and the option pass iterates fields, owners and
values sorted.

The second one is the one to remember. It was not a display bug: the *kind* of
352 definitions depended on thread scheduling, so `FieldValueKinds`,
go-to-definition and every reference to them changed between runs of the same
workspace. Parallelism may decide when work happens, never what anything is.

**Reproduction:** `task test:sweep`. Every Workspace Health check against every
workshop mod on the machine, reported per game with hover and completion answer
rates, diagnostics per code, and live heap. A Game of Thrones (21,499 files) is
the acceptance case, and the run includes the determinism guard — a count that
moves between runs cannot be read as a regression either way.

## 7. Where these are asserted

Real-install tests, skipped when the env var is absent:

| Fact | Test |
|---|---|
| Schema counts, prefix binding coverage | `catalog/bind_test.go`, `schema_test.go` |
| Keyword pair is not a wrapper | `TestKeywordDeclaredBlockIsNotAWrapper` |
| Named-body folder is not a wrapper | `TestNamedBodyFolderIsNeverAWrapper` |
| Nested child must be declared | `TestDeriveNestedShapesNeedsDeclaredChild` |
| Cache size ceiling | `catalog/scansize_test.go` |
| Scope walk, zero false positives on vanilla | `session/scopereal_test.go` |

**Rule for adding to this file:** record the measurement and the reproduction,
not the conclusion alone. Every wrong turn in this engine came from reasoning
about what the games *should* do instead of sampling what they *do*.

---

## 8. Structure keys are recorded one level deep, on purpose

`harvestStructVocab` records the direct children of a definition, not its
grandchildren. That is why hovering Victoria 3's `instance` — nested inside
`colored_emblem = { … }` inside a coat of arms — produces no card, and it is
**740 of the 884 unnamed hovers** on the Hail conversion, which is the whole of
that game's 77%-vs-96% hover-naming gap.

Recording depth 2 was measured and rejected:

| Game | depth-1 names | depth-2 names | new names added |
|---|---|---|---|
| CK3 | 9,652 | 15,893 | **+14,895 (+154%)** |
| Vic3 | 10,993 | 6,183 | +6,004 (+54%) |
| EU5 | 4,321 | 6,775 | **+6,606 (+152%)** |

`Structures` is not only a hover lookup: it feeds the structure keys completion
offers inside a block, and it is what a future `unknown-key` diagnostic would
judge against. Trebling it with names that are only valid two levels down would
offer them where they are wrong and blunt that check — a bad trade for naming one
key.

The number is also a sampling artifact worth remembering when reading the
scoreboard: one mod's coat-of-arms files repeat `instance` hundreds of times, and
the sample takes every seventh assignment, so a single deeply-nested key in a
single file family moved a whole-game metric by nineteen points.

---

## 9. A key belonging to several kinds is normal, and typing on it is not solved

`deriveFieldValueKinds` refuses to type a field whose values disagree about their
kind. That veto also fires on a value that is merely *ambiguous* — one key that
several databases share — and the games do that constantly:

| Game | distinct definition keys | belonging to more than one kind |
|---|---|---|
| CK3 | 180,434 | 16,576 (9%) |
| Vic3 | 35,328 | 757 (2%) |
| **EU5** | 81,643 | **35,019 (42%)** |

An EU5 country tag such as `BYZ` is at once a `countries`, a `coat_of_arms`, a
`flag_definitions` and a `customizable_localization` key. So `has_or_had_tag`
types as nothing, completion offers `yes`/`no` for a field whose values are
perfectly well known, and it looks like an EU5-specific weakness when it is a
general rule meeting an unusual corpus.

**Two replacements were measured and both rejected.** The guard is `dangling`,
which sits at 82 / 97 / 7 and had not moved for any other change:

| Rule | CK3 dangling | Vic3 | EU5 | completion won |
|---|---|---|---|---|
| veto (shipped) | 82 | 97 | 7 | — |
| vote per kind, 80% winner | **1,031** | 174 | 51 | CK3 +7, EU5 +17 |
| intersect the kind sets | **679** | 111 | 20 | EU5 +20 only |

Voting let two coincidental matches carry a whole field — `cultures/bad`,
`map_data/b`, `dynasties/generate`. Intersection is the right *shape*, because it
separates ambiguity (a big candidate set that constrains little) from
disagreement (an empty set, which must veto), and it recovers `has_or_had_tag`
correctly once the schema picks `countries` from the survivors on the grounds
that it is the only one with a scope type. But it still types a field on as few
as two resolving values, and where keys belong to four kinds apiece a spurious
intersection is cheap.

What a future attempt needs is not another rule but a far higher evidence floor
on this path specifically. And note what the table says about value: on CK3 and
Victoria 3 both replacements bought **no completion at all** and cost 597 and 14
false dangling rows. Only EU5 gained, because only EU5 has the 42%.

---

## 10. The hover signature rate is a property of the game, not of the engine

The scoreboard reports how often a hover carries a signature, and the three games
look wildly unequal — CK3 15%, Victoria 3 5%, EU5 1%. That is not an engine gap.
Measured over the sampled tokens of each conversion:

| Game | hovers sampled | the game states a signature | the card showed it |
|---|---|---|---|
| CK3 | 7,278 | 1,201 (16.5%) | **100%** |
| Vic3 | 3,848 | 214 (5.6%) | **100%** |
| EU5 | 1,614 | 26 (1.6%) | **100%** |

The "states a signature" column *is* the reported rate. Each game's script_docs
carry usage strings for a different share of the API, and the share that overlaps
what modders actually write differs again: EU5 documents a usage for 40% of its
declared tokens, but only 1.6% of the tokens appearing in real EU5 mod script.

So the metric is honest and the gap is not closable by engine work. Do not chase
it: the only way to raise EU5's number is to invent signatures the game does not
state, which is the opposite of reading the type system from the dumps.

The one thing that *was* an engine fault here was small and is fixed: a token
that is both a declared effect and a structure key of the kind being edited
reached the structure-key card, which never attached the signature. That lost 58
of CK3's 1,201 and 8 of Victoria 3's 214. All three games now show 100% of what
their dumps state.

---

## 11. Primitives: the set is small, closed, and must be complete

A third of CK3's model is keyed by a bare number, so every integer in script can
collide with a definition:

| Game | definitions | keyed by a bare number |
|---|---|---|
| **CK3** | 215,448 | **68,109 (32%)** — characters 38,020, province_terrain 12,720, provinces 10,887, dynasties 4,223, map_data 1,465 |
| Vic3 | 41,011 | 0 |
| EU5 | 142,258 | 240 |

Left alone, `value = 5` resolves to a province. Measured on A Game of Thrones,
**4,044 of 11,411** literal values would resolve against some database — every
one a `provinces` hit on an arithmetic operand: `value`, `add`, `weight`, `days`,
`MIN`, `MAX`, `divide`, `count`.

**The defence is not context-tracking, it is a complete primitive set.** A
literal in a value position is a value, and the set of things that look like
literals is small and closed: **number, date, boolean**. Every bug of this shape
so far was a hole in that set rather than a flaw in the rule — dates were
missing, which is why `game_start_date < 1178.1.1` hovered as a struggle.

Two measurements say the rule is not too blunt:

- literal values sitting in a slot whose type the engine already knows: **zero**,
  across a 21,499-file conversion. Suppressing them costs nothing today.
- assignment keys never reach the rule, so a `random_list` weight and a numeric
  key in the file that defines it keep their own handling (§3f).

If a future game does put a literal in a typed slot, the fix is already designed
and should not be built before then: resolve it through the slot's declared type
using the same chain completion uses in `valueItems` — fire key, script name,
`Supported Targets`, then field value kind. Hover and completion would then
answer "what is this" and "what can go here" from one source. Today that would
change nothing, and speculative machinery here is how an engine accumulates the
hardcoded edge cases this one was rebuilt to remove.

## 12. Loc keys are built out of other loc keys, not only out of definitions

Measured on vanilla, where the keys and the script that cites them ship from the
same build, so a key the orphan rule flags is nearly always a mechanism PMT does
not model rather than dead weight Paradox shipped:

| Game | english keys | cited | convention-owned | flagged orphan |
|---|---|---|---|---|
| CK3 | 287,896 | 63.9% | 19.9% | **46,526 (16.2%)** |
| Vic3 | 101,713 | 65.8% | 19.4% | **15,021 (14.8%)** |
| EU5 | 234,673 | 38.5% | 31.9% | **69,565 (29.6%)** |

**Total conversions do not inflate the rate**, only the count — they are large,
so the same rate produces a much bigger number:

| Corpus | own english keys | flagged | rate |
|---|---|---|---|
| CK3 vanilla | 287,896 | 46,526 | 16.2% |
| CK3 · A Game of Thrones | 93,914 | 11,813 | **12.6%** |
| CK3 · The Fallen Eagle | 26,417 | 5,047 | 19.1% |
| Vic3 vanilla | 101,713 | 15,021 | 14.8% |
| Vic3 · Hail | 3,487 | 205 | **5.9%** |
| EU5 vanilla | 234,673 | 69,565 | 29.6% |
| EU5 · Basileia Romaion | 7,675 | 5,762 | **75.1%** |

AGoT and Hail sit *below* their game's vanilla rate, so the residue there is the
same engine gap being applied to more keys. Fallen Eagle's 19.1% against CK3's
16.2% is the only CK3 excess worth treating as candidate real cruft.

The largest unmodelled mechanism is a key derived from **another key** rather
than from a definition. EU5 writes the negated form of a trigger tooltip as
`NOT_<key>`: `NOT_THIRD_*` (1,259), `NOT_FIRST_*` (490), `NOT_HAS_*`, `NOT_IS_*`,
`NOT_any_*`, and diplomatic actions as `OTHER_PERFORMS_<key>` (1,349) and
`WE_PERFORM_<key>` (708). `ConventionOwner` only resolves affixes around a
**definition id**, so none of these have an owner and all of them read as
orphaned.

CK3's residue is the same rule failing on shapes it should already reach —
`game_concept_*` (921, and concepts are *declared*, so this is declared-beats-
derived again), `PORTRAIT_MODIFIER_*` (1,023), `mercenary_company_*` (321),
`great_project_*` (231), `dynn_title_*` (110), `struggle_parameter_*` (94).

**The tail is long and that shapes the fix.** CK3's residue spreads over 29,647
distinct prefix clusters and the top 25 cover 9% of it. There is no set of
special cases here; only a rule that generalises will move the number.

EU5 also carries a dotted family — `caux.elysian_language`,
`br_ere_resistance_events.60.entry` — where the stem before the first `.` names a
location or an event namespace. §3g recorded 592 of these on AGoT as not worth a
tokeniser change; at EU5's 75% mod rate that judgement no longer holds.

Reproduce with `PMT_LOC_RESIDUE=1` and `internal/views/locresidue_test.go`.

## 13. Where permissiveness is allowed, and where it is not

The consequences are asymmetric, and the engine should be calibrated to that
rather than to one global notion of "accurate".

**Failing to report an orphan costs a modder some dead localization lines** —
clutter they will probably never notice. **Loosening a derivation costs them a
hover that names the wrong thing, a go-to-definition that opens the wrong file,
and a `loc_key` type on a field that holds no key.** The second is the failure
that teaches a new modder something false, which is the one thing this engine
exists to avoid.

So the reporting side may be permissive and the derivation side may not. Two
rules keep that from collapsing back into one loose predicate:

1. **The permissive side returns a boolean and nothing else.** `LocKeyExplained`
   answers "does anything account for this key?" and cannot answer "what?". A
   predicate that yields no identity cannot produce a wrong link, so widening it
   has no reachable effect on hover, definition or typing. `LocConventionOwner`
   keeps the `(id, kind, ok)` signature and its floors, and stays the only thing
   the LSP asks.
2. **The permissive side is reverse-only.** `ConventionOwner` runs backwards,
   from a key to whatever explains it. `ConventionLocKeys` runs forwards, from a
   definition to the keys it is expected to have, and feeds both the
   `required-loc` diagnostics and the stored loc references that `UsedLocKeys`
   reads. A permissive shape in the forward direction would *demand* keys that
   were never meant to exist and fabricate references to them — false positives
   in the other direction, and in a diagnostic. `LocKeyAffixes` must never be
   wired into it.

`deriveLocFields` is the neighbour to keep honest for the same reason: it decides
which script fields hold a localization key, so it changes typing, hover and
go-to-definition directly. It keeps strict floors (`minLocFieldHits = 8`,
`locFieldCoverage = 80`) and skips values that name a definition or a schema
token. Electing 323 CK3 fields there put `has_trait` and `capital` on the
loc_key path; 179 is the honest number.

---

## 11. Primitives: the set is small, closed, and must be complete

A third of CK3's model is keyed by a bare number, so every integer in script can
collide with a definition:

| Game | definitions | keyed by a bare number |
|---|---|---|
| **CK3** | 215,448 | **68,109 (32%)** — characters 38,020, province_terrain 12,720, provinces 10,887, dynasties 4,223, map_data 1,465 |
| Vic3 | 41,011 | 0 |
| EU5 | 142,258 | 240 |

Left alone, `value = 5` resolves to a province. Measured on A Game of Thrones,
**4,044 of 11,411** literal values would resolve against some database — every
one of them a `provinces` hit on an arithmetic operand: `value`, `add`, `weight`,
`days`, `MIN`, `MAX`, `divide`, `count`.

**The defence is not context-tracking, it is a complete primitive set.** A
literal in a value position is a value, full stop, and the set of things that
look like literals is small and closed: **number, date, boolean**. Every bug of
this shape so far was a hole in that set rather than a flaw in the rule — dates
were missing, which is why `game_start_date < 1178.1.1` hovered as a struggle.

Two measurements say the rule is not too blunt:

- literal values sitting in a slot whose type the engine already knows: **zero**,
  across a 21,499-file conversion. Suppressing them costs nothing today.
- assignment keys never reach the rule, so a `random_list` weight and a numeric
  key in the file that defines it keep their own handling (§3f).

If a future game does put a literal in a typed slot, the fix is already designed
and should not be built before then: resolve it through the slot's declared type
using the same chain completion uses in `valueItems` — fire key, script name,
`Supported Targets`, then field value kind. Hover and completion would then
answer "what is this" and "what can go here" from one source. Today that would
change nothing, and speculative machinery here is how the engine got its
hardcoded edge cases in the first place.

## 14. A block name is not a position

`NestedMembers` keys block members by the block's own name — `triggered_desc`
holds `trigger` and `desc`, `limit` holds `exists`, `not`, `has_trait`. Merging
the evidence by name is what makes an occurrence floor mean anything, and it is
right for **offering** keys: the worst case is a suggestion that does not belong,
which costs a keystroke.

It is not enough for **judging** them. `option` is a block in a CK3 event and a
block in `common/accolade_names`, and they hold nothing in common. Measured:

| Game | `unknown-key` findings on ~400 vanilla files |
|---|---|
| CK3 | **18,341** |
| Vic3 | 6 |
| EU5 | 4 |

CK3's number is one name colliding across kinds, not eighteen thousand defects.
The check also took the vanilla gate from 20s to 244s on CK3 alone, because it
walks every block of every file rather than answering about one cursor.

**So `unknown-key` is not implemented.** It is the one check that reports an
*absence* — everything else fires on a positive contradiction of something the
game declares — and an absence is only meaningful once the model can tell
`event > option` from `accolade_names > option`. That needs the member table
keyed by **kind plus path**, not by block name, and the same measurement rerun
against it.

Recording this so the next attempt starts from the number rather than from the
idea: the pathed model made completion correct at every depth, and that was not
sufficient to make the diagnostic safe.

## 15. LSP quality, measured against real mods

Measured through the running app against the user's own CK3 and EU5 workspaces —
not fixtures — at real identifier positions rather than fixed columns. Blind
column sampling reports whitespace as a failure and gave 25% where the same file
measures 98%.

**Hover**, at 277 identifier positions across all three games:

| | named | with docs |
|---|---|---|
| CK3 | 118/120 (98%) | 88 (73%) |
| Vic3 | 119/120 (99%) | 75 (63%) |
| EU5 | 36/37 (97%) | 14 (38%) |

The unnamed remainder is `this` — a scope keyword that correctly produces no
card — and one EU5 portrait key. §8 recorded 77% naming with the depth gap as
the cause; pathed block members closed it.

EU5's 38% documentation rate against CK3's 73% is a content difference, not an
engine one: EU5 documents fewer of its fields.

**Completion**, key position, same 157 places: no `yes`/`no` fallback anywhere,
15 positions offering nothing, and most of those sit on a definition's own name
where there is nothing to suggest.

**Completion**, value position, 121 places:

| | answered | `yes`/`no` only | empty |
|---|---|---|---|
| CK3 | 55/100 | 16 | 29 |
| Vic3 | 54/100 | 39 | 7 |
| EU5 | 10/21 | 4 | 7 |

Victoria 3 found a fault the other two hid: its mods lean far harder on scripted
triggers invoked as `x = yes`, and a parameterless macro was claiming that slot
and returning nothing. Worth 3 positions on CK3 and 17 on Vic3. **A third corpus
is worth more than more tests against an existing one.**

Every `yes`/`no` result was a genuinely boolean field (`is_landed`, `is_ai`,
`decaying`), so that is the right answer rather than a fallback. The remaining
empties are flags — author-invented, nothing to offer — and definition names.

**Go-to-definition and references**: of every hover card that knows a site,
26 of 26 had a working jump and working references. No failures.

Re-measure with the MCP (`WAILS_MCP=1 wails3 dev`) rather than by reasoning:
five of the bugs behind these numbers were invisible in the source and obvious
in one call.
