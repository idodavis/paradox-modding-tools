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

// TaggedBlock is a scalar tag immediately followed by a block, e.g. `rgb { 255 0 0 }`.
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
// all node offsets are byte indices into it.
type Result struct {
	Root     *Root
	Errors   []ParseError
	Comments []Comment
	Src      string
	lines    *LineIndex
}

// The structural errors the parser recovers from, each reported where it is found.
const (
	ErrUnclosedBrace      ErrorCode = "unclosed-brace"      // reported at the opening brace
	ErrStrayClose         ErrorCode = "stray-close"         // `}` with no open block
	ErrUnterminatedString ErrorCode = "unterminated-string" // runs to the end of the text
	ErrMissingValue       ErrorCode = "missing-value"       // `key =` with nothing after
)

// NewLineIndex builds a LineIndex over text. \r\n and lone \r both break a line.
func NewLineIndex(text string) *LineIndex {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			starts = append(starts, i+1)
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				starts = append(starts, i+2)
				i++
			} else {
				starts = append(starts, i+1)
			}
		}
	}
	return &LineIndex{lineStarts: starts, length: len(text)}
}

// LineCount returns the number of lines.
func (index *LineIndex) LineCount() int { return len(index.lineStarts) }

// PositionAt converts a byte offset to a 0-based (line, byte-column) position.
func (index *LineIndex) PositionAt(offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > index.length {
		offset = index.length
	}
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
	start := index.lineStarts[line]
	nextStart := index.length
	if line+1 < len(index.lineStarts) {
		nextStart = index.lineStarts[line+1]
	}
	if col < 0 {
		col = 0
	}
	offset := min(min(start+col, nextStart), index.length)
	return offset
}

// Lines returns the LineIndex for Src (built at parse time).
func (result Result) Lines() *LineIndex {
	if result.lines != nil {
		return result.lines
	}
	return NewLineIndex(result.Src)
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

// LineIndents is brace-depth indent per line. It tokenizes Src: a Result does
// not keep its tokens, because only the two editor-time readers here ever want
// them and a scan would otherwise carry one buffer per open file.
func (result Result) LineIndents() []int {
	toks := tokenize(result.Src)
	defer putTokens(toks)
	return lineIndentsFrom(result.Src, toks)
}

// TokenAt returns the identifier covering off (UTF-8 bytes). Comments yield
// an empty word. Src is tokenized on the fly; see LineIndents.
func (result Result) TokenAt(off int) (word string, start, end int) {
	src := result.Src
	if off < 0 {
		off = 0
	}
	if off > len(src) {
		off = len(src)
	}
	start, end = off, off
	var toks []token
	if src != "" {
		toks = tokenize(src)
		defer putTokens(toks)
	}
	for _, tok := range toks {
		if tok.kind == tokEOF || off < tok.start || off > tok.end {
			continue
		}
		if tok.kind == tokComment {
			return "", off, off
		}
		if off == tok.end && tok.start != tok.end {
			continue
		}
		switch tok.kind {
		case tokWord:
			return src[tok.start:tok.end], tok.start, tok.end
		case tokString:
			return identAt(src, off, tok.start, tok.end)
		}
	}
	return identAt(src, off, 0, len(src))
}

// identAt returns the identifier touching off within src[lo:hi], with its start and end; the
// identifier is empty where none touches off.
func identAt(src string, off, lo, hi int) (string, int, int) {
	if off < lo {
		off = lo
	}
	if off > hi {
		off = hi
	}
	start, end := off, off
	for start > lo && isIdentByte(src[start-1]) {
		start--
	}
	for end < hi && end < len(src) && isIdentByte(src[end]) {
		end++
	}
	if start == end {
		return "", start, end
	}
	return src[start:end], start, end
}

// isIdentByte reports the bytes an identifier is made of: letters, digits, `_`, `.` and `-`.
func isIdentByte(char byte) bool {
	return char == '_' || char == '.' || char == '-' ||
		(char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')
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
// Loose comments keep source order.
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

// BlockOf returns the Block a value owns (plain or tagged), or nil.
func BlockOf(value Value) *Block {
	switch block := value.(type) {
	case *Block:
		return block
	case *TaggedBlock:
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

// ScalarOf is the one-word value the first assignment of a key in a block writes; nil where the block writes none,
// or a block.
func ScalarOf(block *Block, key string) *Scalar {
	for _, field := range Fields(block) {
		if scalar, isScalar := field.Value.(*Scalar); isScalar && field.Key.Text == key {
			return scalar
		}
	}
	return nil
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
