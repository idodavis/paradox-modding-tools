// cst.go defines the concrete syntax tree node types, the LineIndex
// (offset<->position, UTF-8 byte columns), the walk/lookup helpers, and
// TopAssignments (the flattened root-assignment view used by merge/scan).

package jomini

import "sort"

// Range is a half-open span of UTF-8 byte offsets into the source (Start..End).
type Range struct {
	Start int
	End   int
}

// ErrorCode classifies a structural parse error.
type ErrorCode string

const (
	ErrUnclosedBrace      ErrorCode = "unclosed-brace"      // reported at the opening brace
	ErrStrayClose         ErrorCode = "stray-close"         // `}` with no open block
	ErrUnterminatedString ErrorCode = "unterminated-string" // recovered at end of line
	ErrMissingValue       ErrorCode = "missing-value"       // `key =` with nothing after
)

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

func (r Root) NodeRange() Range        { return r.Range }
func (s Scalar) NodeRange() Range      { return s.Range }
func (b Block) NodeRange() Range       { return b.Range }
func (t TaggedBlock) NodeRange() Range { return t.Range }
func (a Assignment) NodeRange() Range  { return a.Range }
func (v ValueStmt) NodeRange() Range   { return v.Range }

// BlockForKey returns the body of the first top-level assignment named key.
func BlockForKey(root *Root, key string) *Block {
	if root == nil || key == "" {
		return nil
	}
	for _, st := range root.Statements {
		a, ok := st.(*Assignment)
		if !ok || a.Key.Quoted || a.Key.Text != key {
			continue
		}
		return BlockOf(a.Value)
	}
	return nil
}

// BlockOf returns the Block a value owns (plain or tagged), or nil.
func BlockOf(v Value) *Block {
	switch b := v.(type) {
	case *Block:
		return b
	case *TaggedBlock:
		return &b.Block
	default:
		return nil
	}
}

func (Scalar) isValue()         {}
func (Block) isValue()          {}
func (TaggedBlock) isValue()    {}
func (Assignment) isStatement() {}
func (ValueStmt) isStatement()  {}

// TopAssignment is a flattened top-level assignment used by merge/scan: only the
// key text and byte/line spans, never bare ValueStmts. GUI `template Foo { }`
// yields one entry keyed "Foo" (the leading `template` word is a skipped ValueStmt,
// captured by callers via gap slicing between EndByte and the next StartByte).
type TopAssignment struct {
	Key       string
	StartByte int
	EndByte   int
	StartLine int
	EndLine   int
}

// TopAssignments returns the root-level Assignment nodes only, in source order.
func TopAssignments(r Result) []TopAssignment {
	li := r.Lines()
	out := make([]TopAssignment, 0, len(r.Root.Statements))
	for _, st := range r.Root.Statements {
		a, ok := st.(*Assignment)
		if !ok {
			continue
		}
		out = append(out, TopAssignment{
			Key:       a.Key.Text,
			StartByte: a.Range.Start,
			EndByte:   a.Range.End,
			StartLine: li.PositionAt(a.Range.Start).Line + 1,
			EndLine:   li.PositionAt(a.Range.End).Line + 1,
		})
	}
	return out
}

// Position is a 0-based line and UTF-8 byte column within that line.
type Position struct {
	Line      int
	Character int
}

// LineIndex maps between byte offsets and (line, byte-column) positions.
// lineStarts[i] is the byte offset of the first character of 0-based line i.
type LineIndex struct {
	lineStarts []int
	length     int
}

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
func (li *LineIndex) LineCount() int { return len(li.lineStarts) }

// LineStart returns the byte offset of the first character of 0-based line.
func (li *LineIndex) LineStart(line int) int {
	if line < 0 {
		return 0
	}
	if line >= len(li.lineStarts) {
		return li.length
	}
	return li.lineStarts[line]
}

// PositionAt converts a byte offset to a 0-based (line, byte-column) position.
func (li *LineIndex) PositionAt(offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > li.length {
		offset = li.length
	}
	// Greatest lineStart <= offset.
	line := max(sort.Search(len(li.lineStarts), func(i int) bool {
		return li.lineStarts[i] > offset
	})-1, 0)
	return Position{Line: line, Character: offset - li.lineStarts[line]}
}

// OffsetAt converts a 0-based (line, byte-column) position to a byte offset,
// clamping the column to the end of its line.
func (li *LineIndex) OffsetAt(line, col int) int {
	if line < 0 {
		line = 0
	}
	if line >= len(li.lineStarts) {
		return li.length
	}
	start := li.lineStarts[line]
	nextStart := li.length
	if line+1 < len(li.lineStarts) {
		nextStart = li.lineStarts[line+1]
	}
	if col < 0 {
		col = 0
	}
	offset := min(start+col, nextStart)
	if offset > li.length {
		offset = li.length
	}
	return offset
}

// ChildBlock returns the Block a statement owns (via block or tagged-block value),
// or nil.
func ChildBlock(st Statement) *Block {
	var v Value
	switch s := st.(type) {
	case *Assignment:
		v = s.Value
	case *ValueStmt:
		v = s.Value
	}
	return BlockOf(v)
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
			if b := ChildBlock(st); b != nil {
				visit(b.Statements, depth+1, b)
			}
		}
	}
	visit(root.Statements, 0, nil)
}

// NodeAtOffset returns the innermost-first chain of statements whose ranges
// contain offset (outermost first in the slice), or nil if offset is outside all.
func NodeAtOffset(root *Root, offset int) []Statement {
	var path []Statement
	var search func(stmts []Statement)
	search = func(stmts []Statement) {
		for _, st := range stmts {
			r := st.NodeRange()
			if offset < r.Start || offset > r.End {
				continue
			}
			path = append(path, st)
			if b := ChildBlock(st); b != nil && offset >= b.Range.Start && offset <= b.Range.End {
				search(b.Statements)
			}
			return
		}
	}
	search(root.Statements)
	return path
}
