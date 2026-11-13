// Package jomini parses Paradox script, GUI and asset files into a flat tree,
// and gives each key, tag and value its class: an identifier, a number, a
// date, a chain of links, a parameter, a constant, a data call and so on. It
// also reads colors and data calls. It answers only what the text says, such
// as where each statement starts and ends and what its parts are written as.
// What a key means is the schema's question, and this package knows no game.
//
// # The tree
//
// A Tree holds the statements of one text in one slice, in document order.
// Statement 0 is the file itself, and a block's children sit right after it.
// A statement knows its Parent and its Last, one past its last descendant, so
// its children are the statements between it and Last:
//
//	a = { b = 1 c = 2 } d = 3
//
//	0  the file    children 1, 4
//	1  a = { … }   Last 4, children 2, 3
//	2  b = 1       Last 3
//	3  c = 2       Last 4
//	4  d = 3       Last 5
//
// Every part of a statement is a Span of the text, never a copy. Every offset
// counts UTF-8 bytes, and so does a Position's column.
//
// # The syntax
//
// Parse reads the script language by these rules:
//
//   - A statement is a key, an operator and a value, or a value alone. The
//     operators are =, ==, !=, <, >, <=, >= and ?=.
//   - A value is a scalar or a block in braces. A word before a block is its
//     tag, as in color = rgb { 255 0 0 }, and so is list before a quoted
//     name, as in pattern = list "faction_patterns_list".
//   - A value alone just before a statement in its block is the statement's
//     lead: scripted_trigger in scripted_trigger x = { }, template in
//     template x { } and blockoverride in blockoverride "x" { }.
//   - A quoted string is one scalar, and a backslash escapes the character
//     after it. A string may run over several lines.
//   - A [ … ] inside a word is part of the word, spaces and all. It ends at
//     its closing ] or at the end of its line.
//   - A comma or a semicolon separates values as white space does. CK3
//     writes NOTIFICATION_MESSAGE_RELATIONS = { "friend", "rival", "nemesis" }
//     and map_objects_sunny_sun_elevation = 0.4;.
//   - A # starts a comment, which runs to the end of its line.
//   - A line ends at \n, \r\n or a lone \r.
//
// # Classes
//
// Parse gives each key, tag and value of a statement its Class, read inside
// any quotes. Classify decides it by the first of these rules that fits:
//
//   - A text that holds a parameter anywhere is a parameter, as $TRAIT$ and
//     has_$KIND$_flag are. A parameter is $NAME$ or $NAME|default$: its name
//     is ASCII letters, digits and underscores, and its default runs to the
//     next $.
//   - @[…] is an expression of numbers, constants, the four operators and
//     brackets, and @ before a name is a constant, as @size is. A constant
//     stands for the value of the last @name = value written before it in
//     the file.
//   - A text that starts with [ and ends with ] is a data call, as
//     [Root.GetName] is.
//   - A number is an optional sign, digits, an optional fraction and an
//     optional %, or a 0x hexadecimal value. A fraction may end in the f of
//     a C float, as in 3.0f.
//   - A date is year.month.day, as in 1066.9.15. A year and a month alone,
//     as in 1066.9, are a number.
//   - A text of letters, digits, characters outside ASCII and the characters
//     _ - . ' : is an event id where it is a namespace, a dot and a number,
//     as feast.1001 is, else a chain where it holds a dot or a colon, as
//     root.father and scope:x are, else an identifier, as brave is.
//   - An identifier with one argument in parentheses that end it is
//     parenthesized, as opinion(scope:ruler) is.
//   - Anything else is text, such as a quoted text with spaces.
//
// A chain splits into its links at each dot outside brackets and quotes, as
// scope:x.var:y.z does, and a link into its prefix and its name at its first
// colon. A key path is segments joined by /, each a glob, <number> for a key
// written as a number or a date, or ** for any depth.
//
// # Colors
//
// Colors lists the ways script writes a color. A part of rgb, or of a block
// with no tag, is out of 255, or out of 1 when red, green and blue are each
// at most 1. A fourth part is the alpha. A hex block holds one number of six
// hex digits, or eight with the alpha last, with or without 0x: CK3 writes
// hex{ 50779b } and EU5 hex{ 0xffbcb6b1 }. A color is written back the way
// its block wrote it: in the same scale, with an alpha only where the block
// had one, and a hex number with its digits and its 0x as they were.
//
// # Data calls
//
// A GUI file and a localization text write a data call between brackets, as
// in [Root.GetCharacter.GetName]. A call is steps joined by dots. A step may
// take arguments in parentheses, and may be cast by a type in parentheses
// before it, as in (int_bool)GetPlayer.IsAlive. An argument is a text in
// single quotes, a number or a nested call. A localization call may end in a
// format after |, as in [GetName|U].
//
// # Robustness
//
// Parse never fails. It keeps the statements it could read, and reports each
// problem as a Problem with one of these codes:
//
//   - unclosed-block: a block is never closed, and runs to the end of the
//     text.
//   - unexpected-brace: a closing brace at the top closes nothing, and is
//     skipped.
//   - unclosed-string: a quoted string is never closed, and runs to the end
//     of the text.
//   - missing-value: a key and an operator have no value after them.
//   - too-deep: a block opened deeper than 1,000 levels is skipped to its
//     matching closing brace, since the parser recurses once per level and a
//     Go stack overflow is fatal. The tree stays flat and valid.
//
// Parse reads past an operator with no key before it, which it drops, and
// past a byte order mark at the start.
package jomini

import (
	"iter"
	"slices"
	"sort"
	"strings"
	"unicode/utf16"
)

// Span is a half-open range of bytes in a text.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// IsEmpty reports whether the span covers no bytes.
func (s Span) IsEmpty() bool { return s.Start == s.End }

// Contains reports whether a byte offset lies inside the span.
func (s Span) Contains(offset int) bool { return s.Start <= offset && offset < s.End }

// Position is a line and a column in a text, both from 0. The column counts
// UTF-8 bytes.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Problem is one problem found in a text.
type Problem struct {
	// Code is one of the package doc's codes, or the loc package's.
	Code string `json:"code"`

	Message string `json:"message"`
	Span    Span   `json:"span"`
}

// Lines is the line index of a text. A line ends at \n, \r\n or a lone \r.
type Lines struct {
	starts []int
	ends   []int
	length int
}

// NewLines builds the line index of a text.
func NewLines(text string) Lines {
	lines := Lines{starts: []int{0}, length: len(text)}

	for i := 0; i < len(text); i++ {
		if text[i] != '\n' && text[i] != '\r' {
			continue
		}

		lines.ends = append(lines.ends, i)

		if text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n' {
			i++
		}

		lines.starts = append(lines.starts, i+1)
	}

	lines.ends = append(lines.ends, len(text))

	return lines
}

// CountLines returns the number of lines, at least one.
func (l Lines) CountLines() int { return len(l.starts) }

// ToPosition returns the line and column of a byte offset, clamped to the
// text.
func (l Lines) ToPosition(offset int) Position {
	offset = min(max(offset, 0), l.length)
	line := sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > offset }) - 1

	return Position{Line: line, Column: offset - l.starts[line]}
}

// LineSpan returns the span of a line from 0, without its line ending.
func (l Lines) LineSpan(line int) Span {
	return Span{Start: l.starts[line], End: l.ends[line]}
}

// Range is a span as an editor counts it: from one, UTF-16 columns, end
// exclusive.
type Range struct {
	StartLine   int `json:"startLineNumber"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLineNumber"`
	EndColumn   int `json:"endColumn"`
}

// ToRange converts a byte span to an editor's range.
func ToRange(text string, lines Lines, span Span) Range {
	start, end := lines.ToPosition(span.Start), lines.ToPosition(span.End)

	return Range{
		StartLine: start.Line + 1, StartColumn: countColumn(text, lines, start) + 1,
		EndLine: end.Line + 1, EndColumn: countColumn(text, lines, end) + 1,
	}
}

// ToSpan converts an editor's range to a byte span.
func ToSpan(text string, lines Lines, bounds Range) Span {
	return Span{
		Start: ToOffset(text, lines, bounds.StartLine, bounds.StartColumn),
		End:   ToOffset(text, lines, bounds.EndLine, bounds.EndColumn),
	}
}

// ToLineRange returns the empty range at the start of a line.
func ToLineRange(line int) Range {
	return Range{StartLine: line, StartColumn: 1, EndLine: line, EndColumn: 1}
}

// ToOffset converts an editor's line and column to a byte offset, clamped to
// the text.
func ToOffset(text string, lines Lines, line, column int) int {
	span := lines.LineSpan(min(max(line-1, 0), lines.CountLines()-1))
	units := 0
	for offset, character := range text[span.Start:span.End] {
		if units >= column-1 {
			return span.Start + offset
		}

		units += utf16.RuneLen(character)
	}

	return span.End
}

// CountUnits returns the length of a text in UTF-16 code units. A character
// above U+FFFF counts two.
func CountUnits(text string) int {
	units := 0
	for _, character := range text {
		units += utf16.RuneLen(character)
	}

	return units
}

func countColumn(text string, lines Lines, position Position) int {
	start := lines.LineSpan(position.Line).Start

	return CountUnits(text[start : start+position.Column])
}

// Statement is one statement of a text: key op value, a value alone, or a
// block with or without a tag. A part it does not write is empty.
type Statement struct {
	// Parent is the block holding the statement. It is 0 for a top statement
	// and for the file itself.
	Parent int32

	// Last is one past the statement's last descendant, so its children are
	// Statements[i+1:Last].
	Last int32

	Span Span

	// Key is empty for a value alone.
	Key Span

	Op Span

	Tag Span

	// Value is a scalar's text, quotes included. Block runs from the opening
	// brace to the closing one. Each is empty for the other kind.
	Value Span
	Block Span

	// KeyClass, TagClass and ValueClass are what each part is written as,
	// ClassNone for a part not written.
	KeyClass, TagClass, ValueClass Class
}

// IsBlock reports whether the statement's value is a block.
func (s Statement) IsBlock() bool { return !s.Block.IsEmpty() }

// IsListed reports whether the statement is a value alone, not a block.
func (s Statement) IsListed() bool { return s.Key.IsEmpty() && !s.IsBlock() }

// ResolveValueSpan returns the span from the statement's tag, value or block
// to its end. It is empty if the statement writes none.
func (s Statement) ResolveValueSpan() Span {
	for _, part := range []Span{s.Tag, s.Value, s.Block} {
		if !part.IsEmpty() {
			return Span{Start: part.Start, End: max(s.Value.End, s.Block.End)}
		}
	}

	return Span{}
}

// Tree is the parse of one text, with its comments, problems and line index.
type Tree struct {
	Src        string
	Statements []Statement
	Comments   []Span
	Problems   []Problem
	Lines      Lines
}

// Text returns the source text a span covers.
func (t *Tree) Text(span Span) string { return t.Src[span.Start:span.End] }

// Unquoted returns the text a span covers, without any surrounding quotes.
func (t *Tree) Unquoted(span Span) string { return t.Text(t.UnquotedSpan(span)) }

// UnquotedSpan returns a span without its surrounding quotes. A span of one
// quote gives an empty span.
func (t *Tree) UnquotedSpan(span Span) Span {
	text := t.Text(span)
	if strings.HasPrefix(text, `"`) {
		span.Start++
	}

	if strings.HasSuffix(text, `"`) && span.End > span.Start {
		span.End--
	}

	return span
}

// ParseBracketed parses a span's text as a bracketed data call, such as
// [Root.GetName], or returns false.
func (t *Tree) ParseBracketed(span Span) (Call, bool) {
	return ParseBracketed(t.Unquoted(span), t.UnquotedSpan(span).Start)
}

// Children yields the direct children of a statement in document order.
func (t *Tree) Children(statement int32) iter.Seq[int32] {
	return func(yield func(int32) bool) {
		// A child's Last is where its next sibling starts.
		for child, last := statement+1, t.Statements[statement].Last; child < last; child = t.Statements[child].Last {
			if !yield(child) {
				return
			}
		}
	}
}

// FindLead returns the value alone before a statement, as scripted_trigger is
// in scripted_trigger x = { }. It returns false if there is none.
func (t *Tree) FindLead(statement int32) (int32, bool) {
	lead := statement - 1
	if lead < 1 || t.Statements[lead].Parent != t.Statements[statement].Parent || !t.Statements[lead].IsListed() {
		return 0, false
	}

	return lead, true
}

// FindStatement returns the innermost statement that covers a byte offset. It
// returns 0, the file, if no other statement does.
func (t *Tree) FindStatement(offset int) int32 {
	// The last statement starting at or before the offset is the deepest
	// candidate.
	statement := int32(sort.Search(len(t.Statements)-1, func(i int) bool {
		return t.Statements[i+1].Span.Start > offset
	}))

	for statement > 0 && !t.Statements[statement].Span.Contains(offset) {
		statement = t.Statements[statement].Parent
	}

	return statement
}

// LookupStatement returns the statement starting at a byte offset, or false.
func (t *Tree) LookupStatement(start int) (int32, bool) {
	statement := t.FindStatement(start)

	return statement, statement > 0 && t.Statements[statement].Span.Start == start
}

// FindChild returns the child of a statement with the given key. The last one
// wins if several have it. It returns false if none does.
func (t *Tree) FindChild(statement int32, key string) (int32, bool) {
	match, ok := int32(0), false
	for child := range t.Children(statement) {
		if t.Text(t.Statements[child].Key) == key {
			match, ok = child, true
		}
	}

	return match, ok
}

// FindChildValue returns the unquoted value of the child FindChild finds. It
// returns false if there is none or the key is empty.
func (t *Tree) FindChildValue(statement int32, key string) (string, bool) {
	if key == "" {
		return "", false
	}

	child, ok := t.FindChild(statement, key)
	if !ok {
		return "", false
	}

	return t.Unquoted(t.Statements[child].Value), true
}

// FindComment returns the comment lines just above a statement, joined by
// newlines, or "". A comment after code on its line is not one.
func (t *Tree) FindComment(statement int32) string {
	start := t.Statements[statement].Span.Start
	line := t.Lines.ToPosition(start).Line
	after := sort.Search(len(t.Comments), func(i int) bool { return t.Comments[i].Start >= start })

	// Read upwards, each on the line above the last.
	var lines []string
	for position := after - 1; position >= 0; position-- {
		comment := t.Comments[position]
		above := line - len(lines) - 1
		if t.Lines.ToPosition(comment.Start).Line != above ||
			strings.TrimSpace(t.Src[t.Lines.LineSpan(above).Start:comment.Start]) != "" {
			break
		}

		lines = append(lines, strings.TrimSpace(t.Text(comment)))
	}

	slices.Reverse(lines)

	return strings.Join(lines, "\n")
}

// ListFolds returns the span of each block over more than one line, in order
// of opening.
func (t *Tree) ListFolds() []Span {
	var folds []Span

	for _, statement := range t.Statements {
		block := statement.Block
		if statement.IsBlock() && t.Lines.ToPosition(block.End).Line > t.Lines.ToPosition(block.Start).Line {
			folds = append(folds, block)
		}
	}

	return folds
}
