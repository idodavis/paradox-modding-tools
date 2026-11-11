# Glossary

The words Paradox Modding Tools uses for its own things: one word per thing, in the Go code, the frontend and the
docs. A file takes these words when it is next written, so older files may still use another.

The games' own words stay the games' (`datacontext`, `on_action`, `scope`), and so do a library's (Monaco's and
Nuxt UI's props, Steam's Workshop item). Each package's README lists the terms of that package alone.

## The language

| Term | What it names |
|---|---|
| **key** | A definition's identifier: `brave` in `brave = { }`. Also a localization key. The games say "database key". |
| **definition** | A named thing in a database, and the block that defines it. |
| **database** | One kind of named thing: traits, events, effects, links, types, localization. |
| **declaration** | How a database's definitions are written, named, used and combined. |
| **field** | What one key takes where it is written. |
| **shape** | What a block holds, which decides how its keys are read. |
| **keyword** | A fixed word of the script language, such as `if`, `limit` or `add`. It is recognized by the block it is written in. |
| **type** | A scope type or a data type, with the members that may follow it. |
| **member** | A link or a data function that may follow a type in a chain or a call. |
| **chain** | Links written with dots, each in the scope the one before it leads to: `root.father.culture`. |
| **pattern** | A command the dumps declare once for each definition of a database, as `has_relation_*` is for the scripted relations. |
| **copy** | One writing of a key in a file, with what decides whether the game keeps it: its entry mode, its priority and whether only its file sees it. |
| **class** | What a key, a tag or a value is written as, such as a number, a date or an identifier. |
| **segment** | One piece of a localization text: plain text, a data call, an icon or a format. |
| **channel** | One of a color's numbers. |
| **format** | The class of a file, such as script, GUI or localization. Also how a color or a date is written. |
| **output** | What a data function's return type writes in a text: a number, a text or a date. |
| **token** | A lexical piece of a text, in the formatter and the lexer. |

## What the engine finds

| Term | What it names |
|---|---|
| **context** | What the walk decided at one statement: its field, its scope, its shape, and whether it is in a trigger block or a preview. |
| **occurrence** | One key that a file writes, and whether the file defines it, refers to it or localizes it. |
| **problem** | One problem found in a file, by the parse, the walk or the scan. |
| **winner** | The copy of a key that the game keeps, why, and the copies it beats. |
| **fire** | One definition firing another, as an on_action fires an event. |
| **pass** | One of the scan's walks over the files. |
| **view** | An index narrowed to one mod and its dependencies, with its own winners and problems. |

## Files and what is drawn

| Term | What it names |
|---|---|
| **place** | Where a file sits, and whether the game reads it. |
| **hiding** | Why the game does not read a file: another file shadows it, the game drops it, or its folder is not mounted. |
| **folded** | A path compared in lower case with forward slashes. |
| **bytes** | A file's raw content. |
| **window**, **widget** | A GUI definition composed into widgets, and one of them. |
| **coat**, **layer** | A coat of arms composed, and one of its layers. |
| **title** | A text shown to people as a heading. |
| **id** | An identifier PMT gives its own records: an install, a mod, a listing. |
