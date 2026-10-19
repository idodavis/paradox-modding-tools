// cst.go holds the concrete syntax tree: its node types, the line index (offsets and positions in UTF-8 byte
// columns), a parse's Result with every reading of it, and the walk and lookup helpers.

package jomini

import (
	"sort"
	"strconv"
	"strings"
)

// Range is a half-open span of UTF-8 byte offsets into the source (Start..End).
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ErrorCode classifies a structural parse error.
type ErrorCode string

// ParseError is a recovered structural error; parsing always continues past it.
type ParseError struct {
	Code    ErrorCode
	Message string
	Range   Range
}

// Node is any CST node. Statement and Value are the two role sub-interfaces.
type Node interface{ NodeRange() Range }

// Statement is a top-level or in-block statement: Assignment or ValueStmt.
type Statement interface {
	Node
	isStatement()
}

// Value is the right-hand side of an assignment or a list element: Scalar,
// Block, or TaggedBlock.
type Value interface {
	Node
	isValue()
}

// Root is the whole parsed document.
type Root struct {
	Statements []Statement
	Range      Range
}

// Scalar is a bare word or a quoted string. Text excludes surrounding quotes;
// Range includes them.
type Scalar struct {
	Text   string
	Quoted bool
	Range  Range
}

// Block is a `{ ... }` group. CloseBrace is -1 when the closing brace is missing.
type Block struct {
	Statements []Statement
	Range      Range
	OpenBrace  int
	CloseBrace int
}

// TaggedBlock is a scalar tag immediately followed by a block, e.g. `rgb { 255 0 0 }`, or by one quoted text, e.g.
// `list "x"`: that one has no braces (its Block's OpenBrace is -1), holds the text as its one statement, and is no
// block to BlockOf.
type TaggedBlock struct {
	Tag   Scalar
	Block Block
	Range Range
}

// Assignment is `key op value` or GUI-style `key { ... }` (Op == "", Value a *Block).
// Value is nil when the value was missing (a ParseError is recorded).
type Assignment struct {
	Key   Scalar
	Op    string
	Value Value
	Range Range
}

// ValueStmt is a bare list element, e.g. `brave` in `traits = { brave }`.
type ValueStmt struct {
	Value Value
	Range Range
}

// Comment is a `#` comment span.
type Comment struct {
	Range Range
}

// Position is a 0-based line and UTF-8 byte column within that line.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// LineIndex maps between byte offsets and (line, byte-column) positions.
// lineStarts[i] is the byte offset of the first character of 0-based line i.
type LineIndex struct {
	lineStarts []int
	length     int
}

// Result is an immutable parse result. Src is the exact text that was parsed;
// all node offsets are byte indices into it. It keeps the parse's tokens and line
// index, so nothing reads the text again to tokenize it or to find its lines.
type Result struct {
	Root     *Root
	Errors   []ParseError
	Comments []Comment
	Src      string
	tokens   []token
	lines    *LineIndex
}

// Match is how KeyValue chooses among a block's writes of a key: Last takes the last one written rather than the
// first, and FoldCase takes keys that differ only in case for one key.
type Match struct {
	Last     bool
	FoldCase bool
}

// The structural errors the parser recovers from, each reported where it is found.
const (
	ErrUnclosedBrace      ErrorCode = "unclosed-brace"      // reported at the opening brace
	ErrStrayClose         ErrorCode = "stray-close"         // `}` with no open block
	ErrUnterminatedString ErrorCode = "unterminated-string" // runs to the end of the text
	ErrMissingValue       ErrorCode = "missing-value"       // `key =` with nothing after
)

// NewLineIndex builds a LineIndex over text. \n, \r\n and a lone \r each break a line.
func NewLineIndex(text string) *LineIndex {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			starts = append(starts, i+1)
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			starts = append(starts, i+1)
		}
	}
	return &LineIndex{lineStarts: starts, length: len(text)}
}

// LineCount returns the number of lines.
func (index *LineIndex) LineCount() int { return len(index.lineStarts) }

// PositionAt converts a byte offset to a 0-based (line, byte-column) position.
func (index *LineIndex) PositionAt(offset int) Position {
	offset = min(max(offset, 0), index.length)
	// Greatest lineStart <= offset.
	line := max(sort.Search(len(index.lineStarts), func(i int) bool {
		return index.lineStarts[i] > offset
	})-1, 0)
	return Position{Line: line, Character: offset - index.lineStarts[line]}
}

// OffsetAt converts a 0-based (line, byte-column) position to a byte offset,
// clamping the column to the end of its line.
func (index *LineIndex) OffsetAt(line, col int) int {
	if line < 0 {
		line = 0
	}
	if line >= len(index.lineStarts) {
		return index.length
	}
	return min(index.lineStarts[line]+max(col, 0), index.nextLineStart(line))
}

// LineText returns the text of a 0-based line of text, the one this index was built over, without its line ending;
// "" for a line the text does not have.
func (index *LineIndex) LineText(text string, line int) string {
	if line < 0 || line >= len(index.lineStarts) {
		return ""
	}
	return strings.TrimRight(text[index.lineStarts[line]:index.nextLineStart(line)], "\r\n")
}

// nextLineStart is the offset where the line after a 0-based line starts: the end of the text for the last line.
func (index *LineIndex) nextLineStart(line int) int {
	if line+1 < len(index.lineStarts) {
		return index.lineStarts[line+1]
	}
	return index.length
}

// Lines returns the parse's LineIndex, built once by Parse.
func (result Result) Lines() *LineIndex {
	return result.lines
}

// Flat is the source a range covers on one line: its runs of space folded to one, its comments left out.
func (result Result) Flat(span Range) string {
	text := []byte(result.Src[span.Start:span.End])
	for _, comment := range result.Comments {
		for offset := max(comment.Range.Start, span.Start); offset < min(comment.Range.End, span.End); offset++ {
			text[offset-span.Start] = ' '
		}
	}
	return strings.Join(strings.Fields(string(text)), " ")
}

// LineIndents is each line's brace depth, from the parse's tokens: the depth before the line, less the closing braces
// that lead it.
func (result Result) LineIndents() []int {
	lines := result.Lines()
	indents := make([]int, lines.LineCount())
	depth, next := 0, 0
	for line := range indents {
		lineEnd := lines.nextLineStart(line)
		lineDepth, leading, isLeading := depth, 0, true
		for ; next < len(result.tokens) && result.tokens[next].start < lineEnd; next++ {
			switch result.tokens[next].kind {
			case tokLBrace:
				depth++
				isLeading = false
			case tokRBrace:
				depth = max(depth-1, 0)
				if isLeading {
					leading++
				}
			default:
				isLeading = false
			}
		}
		indents[line] = max(lineDepth-leading, 0)
	}
	return indents
}

// TokenAt is the start and end of the word an offset touches: the bare word, or the run of word characters of a
// quoted text, that holds the offset or ends at it. Both are the offset where only a comment, an operator, a brace, a
// quote or a blank is there.
func (result Result) TokenAt(offset int) (start, end int) {
	offset = min(max(offset, 0), len(result.Src))
	tokens := result.tokens[:len(result.tokens)-1]
	first := sort.Search(len(tokens), func(i int) bool { return tokens[i].end > offset })
	for _, i := range []int{first, first - 1} {
		if i < 0 || i >= len(tokens) || offset < tokens[i].start || offset > tokens[i].end {
			continue
		}
		switch tok := tokens[i]; tok.kind {
		case tokWord:
			return tok.start, tok.end
		case tokString:
			textStart, textEnd := tok.start+1, tok.end-1
			if tok.unterminated {
				textEnd = tok.end
			}
			if offset < textStart || offset > textEnd {
				continue
			}
			start, end = offset, offset
			for start > textStart && !isWordTerminator(result.Src[start-1]) {
				start--
			}
			for end < textEnd && !isWordTerminator(result.Src[end]) {
				end++
			}
			return start, end
		}
	}
	return offset, offset
}

// AssignmentAt is the assignment whose key, or the tag of whose value, starts at a line and column of Src (a name a
// keyword declares with an equals sign: template = name { }); nil where none does. A block's statements are in
// source order, so the one that holds the place is found by halving them, block by block down, never by a walk of
// the file.
func (result Result) AssignmentAt(line, col int) Statement {
	start := result.Lines().OffsetAt(line, col)
	statements := result.Root.Statements
	for {
		i := sort.Search(len(statements), func(i int) bool { return statements[i].NodeRange().End > start })
		if i == len(statements) || statements[i].NodeRange().Start > start {
			return nil
		}
		if assign, isAssign := statements[i].(*Assignment); isAssign {
			tagged, isTagged := assign.Value.(*TaggedBlock)
			if assign.Key.Range.Start == start || isTagged && tagged.Tag.Range.Start == start {
				return assign
			}
		}
		block := ChildBlock(statements[i])
		if block == nil {
			return nil
		}
		statements = block.Statements
	}
}

// Attach gives each statement the comments that belong to it, and the comments that belong to none.
// Loose comments keep source order. The docs reader attaches its own, differently: it gives a trailing comment to the
// first statement on its line where this gives it to the outermost, and it drops headings.
func (result Result) Attach() (attached map[Statement][]Comment, loose []Comment) {
	lines := result.Lines()

	// the outermost statement starting on each line is the shallowest, the first Walk visits at
	// that depth
	startsOn, depthOn := map[int]Statement{}, map[int]int{}
	Walk(result.Root, func(stmt Statement, depth int, _ *Block) bool {
		line := lines.PositionAt(stmt.NodeRange().Start).Line
		if seen, ok := depthOn[line]; !ok || depth < seen {
			startsOn[line], depthOn[line] = stmt, depth
		}
		return true
	})

	// the last trailing comment (or its continuation), and the run of whole-line comments
	// that may still turn out to lead a statement; its comments wait at the tail of loose
	attached = map[Statement][]Comment{}
	var trailing Statement
	trailAt := Position{Line: -2}
	runStart, runLine := 0, -2
	for _, comment := range result.Comments {
		pos := lines.PositionAt(comment.Range.Start)
		linePrefix := result.Src[comment.Range.Start-pos.Character : comment.Range.Start]
		wholeLine := strings.TrimSpace(linePrefix) == ""

		// a comment after code on a line where a statement starts trails the outermost one
		if stmt, ok := startsOn[pos.Line]; ok {
			attached[stmt] = append(attached[stmt], comment)
			trailing, trailAt = stmt, pos
			continue
		}

		// a whole-line comment under a trailing comment, in its column, continues it
		if wholeLine && pos.Line == trailAt.Line+1 && pos.Character == trailAt.Character {
			attached[trailing] = append(attached[trailing], comment)
			trailAt = pos
			continue
		}

		// a comment after code where no statement starts, such as `} # end`, belongs to none
		if !wholeLine {
			loose = append(loose, comment)
			continue
		}

		// a run of whole-line comments on consecutive lines leads the statement on the next line
		if pos.Line != runLine+1 {
			runStart = len(loose)
		}
		runLine = pos.Line
		loose = append(loose, comment)
		if stmt, ok := startsOn[pos.Line+1]; ok {
			attached[stmt] = append(attached[stmt], loose[runStart:]...)
			loose = loose[:runStart]
		}
	}
	return attached, loose
}

// ValueOf is the value a statement writes: an assignment's right side, or a listed value.
func ValueOf(statement Statement) Value {
	switch node := statement.(type) {
	case *Assignment:
		return node.Value
	case *ValueStmt:
		return node.Value
	}
	return nil
}

// BlockOf returns the Block a value owns (plain or tagged), or nil: a scalar owns none, nor does a tag written with
// a text (`list "x"`) rather than a block.
func BlockOf(value Value) *Block {
	switch block := value.(type) {
	case *Block:
		return block
	case *TaggedBlock:
		if block.Block.OpenBrace < 0 {
			return nil
		}
		return &block.Block
	default:
		return nil
	}
}

// ChildBlock returns the Block a statement owns (via block or tagged-block value), or nil.
func ChildBlock(statement Statement) *Block {
	return BlockOf(ValueOf(statement))
}

// Fields is a block's assignments, in order; none for no block.
func Fields(block *Block) []*Assignment {
	if block == nil {
		return nil
	}
	var fields []*Assignment
	for _, statement := range block.Statements {
		if assign, isAssign := statement.(*Assignment); isAssign {
			fields = append(fields, assign)
		}
	}
	return fields
}

// KeyValue is the one-word value a block gives a key: the first of the key's assignments that writes a word, or the
// last when match.Last, a key that differs only in case counting as the key when match.FoldCase; nil where the block
// writes none, or only blocks.
func KeyValue(block *Block, key string, match Match) *Scalar {
	var value *Scalar
	for _, field := range Fields(block) {
		scalar, isScalar := field.Value.(*Scalar)
		isKey := field.Key.Text == key || match.FoldCase && strings.EqualFold(field.Key.Text, key)
		if !isScalar || !isKey {
			continue
		}
		value = scalar
		if !match.Last {
			break
		}
	}
	return value
}

// ColorParts reads a color block: the tag written before it ("" for none) and the numbers it holds. A block holding
// one hex word (hex { 0xff8000ff }) gives that word's bytes, so its digit count is kept. False for a value that is no
// block, an empty block, and a block holding anything but numbers.
func ColorParts(value Value) (tag string, parts []float64, ok bool) {
	block := BlockOf(value)
	if block == nil {
		return "", nil, false
	}
	if tagged, isTagged := value.(*TaggedBlock); isTagged {
		tag = tagged.Tag.Text
	}
	for _, statement := range block.Statements {
		item, isValue := statement.(*ValueStmt)
		if !isValue {
			return "", nil, false
		}
		scalar, isScalar := item.Value.(*Scalar)
		if !isScalar {
			return "", nil, false
		}
		bytes := hexBytes(scalar.Text)
		switch {
		case Literal(scalar.Text) == LitNumber:
			number, _ := strconv.ParseFloat(scalar.Text, 64)
			parts = append(parts, number)
		case bytes != nil:
			parts = append(parts, bytes...)
		default:
			return "", nil, false
		}
	}
	return tag, parts, len(parts) > 0
}

// hexBytes is the bytes a 0x word writes, at most four, from its first digits; nil for a word that is no hex number
// of at least six digits.
func hexBytes(text string) []float64 {
	digits, isHex := strings.CutPrefix(strings.ToLower(text), "0x")
	if !isHex || len(digits) < 6 {
		return nil
	}
	var bytes []float64
	for i := 0; i+2 <= len(digits) && len(bytes) < 4; i += 2 {
		value, err := strconv.ParseUint(digits[i:i+2], 16, 8)
		if err != nil {
			return nil
		}
		bytes = append(bytes, float64(value))
	}
	return bytes
}

// Walk visits every statement depth-first. depth is 0 at root; parent is the
// enclosing block (nil at root). Returning false skips the subtree.
func Walk(root *Root, fn func(st Statement, depth int, parent *Block) bool) {
	var visit func(stmts []Statement, depth int, parent *Block)
	visit = func(stmts []Statement, depth int, parent *Block) {
		for _, st := range stmts {
			if !fn(st, depth, parent) {
				continue
			}
			if block := ChildBlock(st); block != nil {
				visit(block.Statements, depth+1, block)
			}
		}
	}
	visit(root.Statements, 0, nil)
}

// NodeRange is the root's span.
func (root Root) NodeRange() Range { return root.Range }

// NodeRange is the scalar's span.
func (scalar Scalar) NodeRange() Range { return scalar.Range }

// TextStart is the offset the scalar's text starts at: past the opening quote of a quoted one.
func (scalar Scalar) TextStart() int {
	if scalar.Quoted {
		return scalar.Range.Start + 1
	}
	return scalar.Range.Start
}

// NodeRange is the block's span.
func (block Block) NodeRange() Range { return block.Range }

// NodeRange is the tagged block's span.
func (taggedBlock TaggedBlock) NodeRange() Range { return taggedBlock.Range }

// NodeRange is the assignment's span.
func (assign Assignment) NodeRange() Range { return assign.Range }

// NodeRange is the value statement's span.
func (valueStmt ValueStmt) NodeRange() Range { return valueStmt.Range }

// the role markers: a Scalar, a Block and a TaggedBlock are values, an Assignment and a ValueStmt statements
func (Scalar) isValue()         {}
func (Block) isValue()          {}
func (TaggedBlock) isValue()    {}
func (Assignment) isStatement() {}
func (ValueStmt) isStatement()  {}
