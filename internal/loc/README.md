# loc

The package reads and writes localization files. It parses a file into its messages, decides the file's language,
splits a message's text into its markup, and gives the plain text a player sees.

It shares `jomini`'s spans, line index and errors, so a localization file and a script file are read with the same
offsets. It knows no game and no index: what a key names, and which message the game keeps, are `index`'s and
`workspace`'s.

| file | holds |
|---|---|
| `loc.go` | `Message` and `File`; `Parse`, `ParseMessage`; the language rule (`FindSuffixLanguage`, `ToLanguageRel`, `ToLanguageKey`); the writer, `Encode` and `EncodeMessages` |
| `markup.go` | `Markup` and its constants, `Segment`; `Segments`, `ToPlain` |

## The file

A `File` is one parsed localization file: its `Language`, its `Messages`, its parse errors and its line index. A
`Message` is one line of it: a key, its version and its text, with the span of the whole message, of the key and of
the text without its quotes. `ParseMessage` reads one line, given where the line starts in its file, so that the spans
come out in the file's offsets.

`Parse` never fails. A line above the header is an error, `loc-content-before-header`, and is not read. A line that is
neither a header, a comment nor a message is `malformed-line`. A message with no closing quote is
`loc-unterminated-value`, and its text runs to the end of its line.

## The language

A file's language is decided once, by the first of three that says: the `l_<language>:` header, then the
`_l_<language>.yml` ending of the file's name, then the folder under `localization` that holds the file, a `replace`
folder skipped. Each is read without regard to case. The language is "" if none of them says. `ToLanguageRel` gives the
path of the file that holds the same messages in another language, setting the language in the folder and in the name
where the file has them. `ToLanguageKey` writes a language as the game does, `l_english`.

## Markup

`Segments` splits a message's text into its pieces, each a `Segment` with its `Markup` and its spans in the file. The
markups are plain text, `$key$` (another message's text, or a value the game fills in), `[call]` (a data call),
`#format … #!` (a text format and its end), `@icon!` (an icon) and `\n` (a break). A segment's `Text` is the piece
without its markers, and its `TextSpan` is where that text sits. A format's segment is followed by the segments of the
calls and keys written inside it.

`ToPlain` joins the plain pieces into the text a player reads, the markup dropped and each break a newline.

A segment's `Kind` is empty as parsed. It says what kind of value a data call shows, number or text, where the
segment's `Text` holds a sample of that value instead of the call.

## The writer

`Encode` writes the text of a localization file for one language: the header, then its messages. `EncodeMessages`
writes the messages alone, each as its key, its version and its quoted text, in the layout the game reads, with the
line ending of the file they join.

## Terms

- **File**: one parsed localization file.
- **Message**: one line of a localization file: a key, its version and its text.
- **Language**: the language a file is for, as the game names it without its `l_`.
- **Segment**: one piece of a message's text, with its spans.
- **Markup**: what a segment is: text, key, call, format, icon or break.
