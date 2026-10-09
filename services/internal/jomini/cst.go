// cst.go defines the concrete syntax tree node types, the LineIndex
// (offset<->position, UTF-8 byte columns), the walk/lookup helpers, and comment attachment.

package jomini

import (
	"sort"
	"strings"
)

// Range is a half-open span of UTF-8 byte offsets into the source (Start..End).
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ErrorCode classifies a structural parse error.
type ErrorCode string

const (
	ErrUnclosedBrace      ErrorCode = "unclosed-brace"      // reported at the opening brace
	ErrStrayClose         ErrorCode = "stray-close"         // `}` with no open block
	ErrUnterminatedString ErrorCode = "unterminated-string" // runs to the end of the text
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

func (root Root) NodeRange() Range               { return root.Range }
func (scalar Scalar) NodeRange() Range           { return scalar.Range }
func (block Block) NodeRange() Range             { return block.Range }
func (taggedBlock TaggedBlock) NodeRange() Range { return taggedBlock.Range }
func (assign Assignment) NodeRange() Range       { return assign.Range }
func (valueStmt ValueStmt) NodeRange() Range     { return valueStmt.Range }

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

func (Scalar) isValue()         {}
func (Block) isValue()          {}
func (TaggedBlock) isValue()    {}
func (Assignment) isStatement() {}
func (ValueStmt) isStatement()  {}

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
	offset := min(min(start+col, nextStart), li.length)
	return offset
}

// ValueOf is the value a statement writes: an assignment's right side, or a listed value.
func ValueOf(statement Statement) Value {
	switch written := statement.(type) {
	case *Assignment:
		return written.Value
	case *ValueStmt:
		return written.Value
	}
	return nil
}

// ChildBlock returns the Block a statement owns (via block or tagged-block value), or nil.
func ChildBlock(statement Statement) *Block {
	return BlockOf(ValueOf(statement))
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

// AssignmentAt is the assignment whose key starts at a line and column of Src; nil where none does.
func (result Result) AssignmentAt(line, col int) Statement {
	start, found := result.Lines().OffsetAt(line, col), Statement(nil)
	Walk(result.Root, func(statement Statement, _ int, _ *Block) bool {
		if assign, isAssign := statement.(*Assignment); isAssign && assign.Key.Range.Start == start {
			found = statement
		}
		return found == nil && statement.NodeRange().Start <= start && start < statement.NodeRange().End
	})
	return found
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
		before := result.Src[comment.Range.Start-pos.Character : comment.Range.Start]
		wholeLine := strings.TrimSpace(before) == ""

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
