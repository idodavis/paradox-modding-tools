---
title: Localization
description: Loc YAML rules that all three games share — BOM, l_<lang>, replace/.
weight: 3
---

# Localization

Player-facing text is YAML under `localization/` (US spelling, **z**). The games never read a `localisation/` folder; PMT flags one (`wrong-localization-folder`).

## File rules (all three games)

1. **UTF-8 with BOM.** Without the BOM the game skips the file. PMT flags it (`missing-bom`) and can add the BOM for you.
2. Filename: `anything_l_<lang>.yml` — `l_` is a lowercase L, not the digit 1.
3. First line: `l_<lang>:` matching the filename (and usually the folder).
4. Each entry: one space, key, colon, optional version int, quoted string.

```yaml
l_english:
 my_event.1.t: "A Title"
 my_event.1.d: "Body text with a $other_key$."
```

Copy English files to other language folders (rename the suffix **and** the header) or other-language players see raw keys. PMT Health's **Localization Issues** list each **untranslated** key once per language, with the file it goes in (filter the Type or Language column).

## Override a vanilla key

Do not replace a whole vanilla loc file unless you must.

- **CK3:** `localization/replace/<lang>/` or `localization/<lang>/replace/` (replace-first path wins if both exist).
- **Vic3 / EU5:** `localization/<lang>/replace/`.
- **EU5** also nests loc under the stage folders (`main_menu`, `in_game`).

Keys outside `replace/` do not reliably override vanilla and may error. Loc files otherwise load **reverse ASCII** (Z→A); `replace/` is still the right tool.

## Formatting you will see in vanilla

- Reuse another key: `$other_key$`.
- Newline: `\n` (loc files only).
- Style: `#R red text#!` `#bold …#!` — start `#code`, end `#!`.
- Text icons: `@gold!` (defined in GUI font-icon files).
- Data functions: `[GetName]`, often PascalCase and dot-chained.

Every string shown to the player should be a **loc key**. Bare quoted text in script sometimes displays (events) and sometimes errors.

## What PMT does with loc

- The workspace's **localization language** is the language you read on hover cards and the one new mods are
  written in, and Health measures every other language against it: a key a mod has in it and leaves out of another
  language the mod ships is **untranslated**.
- Health's **Localization Issues** also list files the game reads wrong: syntax, no BOM, a header naming another
  language, a misspelled folder. A key script uses that no file defines is a **Dangling Ref**.
- A new mod's localization file has the UTF-8 BOM.

See [Health]({{< relref "/pages/health" >}}) and the [FAQ]({{< relref "/faq" >}}) if loc is “ignored.”
