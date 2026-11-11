// edit.go applies edits to a text, finds the edits that give a tree's text an
// even layout and builds the edit that adds lines to a block.

package jomini

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

// Edit is one change to a text. It replaces a span with a text, so an empty
// span inserts and an empty text deletes.
type Edit struct {
	Span Span   `json:"span"`
	Text string `json:"text"`
}

// ApplyEdits returns the text with the edits applied. It panics if two edits
// overlap.
func ApplyEdits(text string, edits []Edit) string {
	edits = slices.SortedStableFunc(slices.Values(edits), func(first, second Edit) int {
		return cmp.Or(cmp.Compare(first.Span.Start, second.Span.Start), cmp.Compare(first.Span.End, second.Span.End))
	})

	var result strings.Builder
	cursor := 0

	for _, edit := range edits {
		if edit.Span.Start < cursor {
			panic("jomini: edits overlap")
		}

		result.WriteString(text[cursor:edit.Span.Start])
		result.WriteString(edit.Text)
		cursor = edit.Span.End
	}

	result.WriteString(text[cursor:])

	return result.String()
}

// LineEnding returns the line ending a text uses: CRLF where it has one, else
// LF.
func LineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}

	return "\n"
}

// Format returns the edits that give the tree's text an even layout. Each
// line starts with the given unit of indentation once for each block it is
// inside, the tokens on a line are one space apart, and an operator is
// spaced as most of the tree's operators are. The line breaks stay where
// they are, each written with the tree's line ending. A comment loses the
// white space at its end. An edit only ever replaces white space: any other
// text between tokens, such as a comma, stays as it is.
func (t *Tree) Format(unit string) []Edit {
	gap, ending := t.operatorGap(), LineEnding(t.Src)

	var edits []Edit
	var before token
	end, depth := 0, 0

	// space makes the white space from an offset to another the given text
	space := func(start, stop int, text string) {
		if t.Src[start:stop] != text {
			edits = append(edits, Edit{Span: Span{Start: start, End: stop}, Text: text})
		}
	}

	for position, current := range t.listTokens() {
		// a closing brace is outside the block it closes
		if current.class == tokenClassClose {
			depth--
		}

		spacing := t.Src[end:current.span.Start]
		switch {
		case strings.TrimSpace(spacing) != "":
		case strings.Contains(spacing, "\n"):
			space(end, current.span.Start, strings.Repeat(ending, strings.Count(spacing, "\n"))+strings.Repeat(unit, max(depth, 0)))
		case position == 0:
			space(end, current.span.Start, "")
		case current.class != tokenClassComment && (before.class == tokenClassOperator || current.class == tokenClassOperator):
			space(end, current.span.Start, gap)
		default:
			space(end, current.span.Start, " ")
		}

		before, end = current, current.span.End
		if current.class == tokenClassComment {
			space(current.span.Start+len(strings.TrimRightFunc(t.Text(current.span), unicode.IsSpace)), end, "")
		}

		if current.class == tokenClassOpen {
			depth++
		}
	}

	if rest := t.Src[end:]; strings.TrimSpace(rest) == "" {
		space(end, len(t.Src), strings.Repeat(ending, strings.Count(rest, "\n")))
	}

	return edits
}

// operatorGap returns what an operator is spaced from its key and its value
// with: nothing where most of the tree's operators are written right after
// their key, else a space.
func (t *Tree) operatorGap() string {
	tight, operators := 0, 0

	for _, statement := range t.Statements {
		if statement.Op.IsEmpty() {
			continue
		}

		operators++
		if statement.Key.End == statement.Op.Start {
			tight++
		}
	}

	if tight > operators-tight {
		return ""
	}

	return " "
}

// listTokens returns the tokens of the tree's statements and of its comments,
// in the order they are written. A block gives its two braces. Text that no
// statement holds, such as an operator the parse dropped, gives none.
func (t *Tree) listTokens() []token {
	var tokens []token

	for _, statement := range t.Statements {
		tokens = append(tokens,
			token{class: tokenClassScalar, span: statement.Key}, token{class: tokenClassOperator, span: statement.Op},
			token{class: tokenClassScalar, span: statement.Tag}, token{class: tokenClassScalar, span: statement.Value})

		if block := statement.Block; statement.IsBlock() {
			tokens = append(tokens,
				token{class: tokenClassOpen, span: Span{Start: block.Start, End: block.Start + 1}},
				token{class: tokenClassClose, span: Span{Start: block.End - 1, End: block.End}})
		}
	}

	for _, comment := range t.Comments {
		tokens = append(tokens, token{class: tokenClassComment, span: comment})
	}

	tokens = slices.DeleteFunc(tokens, func(current token) bool { return current.span.IsEmpty() })
	slices.SortStableFunc(tokens, func(first, second token) int { return cmp.Compare(first.span.Start, second.span.Start) })

	return tokens
}

// Indentation returns the string one level of nesting is indented with in the
// tree's text. It returns "" if no statement is indented.
func (t *Tree) Indentation() string {
	for statement := int32(1); int(statement) < len(t.Statements); statement++ {
		start := t.Statements[statement].Span.Start
		indent := t.Src[t.Lines.LineSpan(t.Lines.ToPosition(start).Line).Start:start]

		depth := 0
		for parent := t.Statements[statement].Parent; parent > 0; parent = t.Statements[parent].Parent {
			depth++
		}

		if depth > 0 && indent != "" && strings.TrimSpace(indent) == "" {
			return indent[:len(indent)/depth]
		}
	}

	return ""
}

// IndentLines joins the lines with the tree's line ending, each starting with
// the given indent and one more level for each brace open above it. A level
// is the tree's own indentation, or a tab if it has none.
func (t *Tree) IndentLines(lines []string, indent string) string {
	unit, depth := cmp.Or(t.Indentation(), "\t"), 0
	indented := make([]string, len(lines))

	for position, line := range lines {
		levels := depth
		if strings.HasPrefix(line, "}") {
			levels--
		}

		depth += strings.Count(line, "{") - strings.Count(line, "}")
		indented[position] = indent + strings.Repeat(unit, max(levels, 0)) + line
	}

	return strings.Join(indented, LineEnding(t.Src))
}

// BuildInsertion returns the edit that adds the lines as the last statements
// of a block statement. If other text comes before the closing brace on its
// line, the lines go on that line instead of their own.
func (t *Tree) BuildInsertion(block int32, lines []string) Edit {
	closing := t.Statements[block].Block.End - 1
	row := t.Lines.LineSpan(t.Lines.ToPosition(closing).Line)

	if leading := t.Src[row.Start:closing]; strings.TrimSpace(leading) != "" {
		text := strings.Join(lines, " ") + " "
		if strings.TrimRight(leading, " \t") == leading {
			text = " " + text
		}

		return Edit{Span: Span{Start: closing, End: closing}, Text: text}
	}

	indent := t.Src[row.Start:closing] + cmp.Or(t.Indentation(), "\t")

	return Edit{Span: Span{Start: row.Start, End: row.Start}, Text: t.IndentLines(lines, indent) + LineEnding(t.Src)}
}
