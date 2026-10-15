// block.go is the reader every property goes through: a statement with the file and writer it comes from, and a
// block of statements read by key, the last written winning.

package draw

import (
	"iter"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/lsp"
)

// statement is one assignment that applies to a widget, with its file's parse and path, and the type or template
// that wrote it ("" the widget's own block).
type statement struct {
	assignment *jomini.Assignment
	parsed     *jomini.Result
	path       string
	writer     string
}

// block is the statements that apply to a widget in the order they apply, the last of each key at hand.
type block struct {
	statements []statement
	byKey      map[string]statement
}

// fields yields each assignment of a statement's block with the word written before it (block, blockoverride, "").
func fields(holder statement) iter.Seq2[string, statement] {
	return func(yield func(string, statement) bool) {
		block := jomini.ChildBlock(holder.assignment)
		if block == nil {
			return
		}
		previous := ""
		for _, item := range block.Statements {
			if assignment, isAssignment := item.(*jomini.Assignment); isAssignment {
				field := holder
				field.assignment = assignment
				if !yield(previous, field) {
					return
				}
			}
			previous = ""
			if value, isValue := item.(*jomini.ValueStmt); isValue {
				if scalar, isScalar := value.Value.(*jomini.Scalar); isScalar {
					previous = strings.ToLower(scalar.Text)
				}
			}
		}
	}
}

// blockOf is a statement's block as a block.
func blockOf(holder statement) *block {
	block := &block{}
	for _, field := range fields(holder) {
		block.add(field)
	}
	return block
}

// add appends statements, each the last of its key.
func (block *block) add(statements ...statement) {
	if block.byKey == nil {
		block.byKey = map[string]statement{}
	}
	for _, statement := range statements {
		block.statements = append(block.statements, statement)
		block.byKey[strings.ToLower(statement.assignment.Key.Text)] = statement
	}
}

// last is the statement that last wrote a key; false for none.
func (block *block) last(key string) (statement, bool) {
	statement, has := block.byKey[strings.ToLower(key)]
	return statement, has
}

// scalar is the one word a key's last statement holds, its file's constants put in; false for none or a block.
func (block *block) scalar(key string) (string, bool) {
	last, has := block.last(key)
	if !has {
		return "", false
	}
	value, isScalar := last.assignment.Value.(*jomini.Scalar)
	if !isScalar {
		return "", false
	}
	return last.parsed.Evaluated(value.Text, value.Range.Start), true
}

// numbers are the numbers a key's last value holds; false for none, or a value that is not numbers alone.
func (block *block) numbers(key string) ([]float64, bool) {
	last, has := block.last(key)
	if !has {
		return nil, false
	}
	var numbers []float64
	for _, text := range last.words() {
		number, err := strconv.ParseFloat(text, 64)
		if jomini.Literal(text) != jomini.LitNumber || err != nil {
			return nil, false
		}
		numbers = append(numbers, number)
	}
	return numbers, len(numbers) > 0
}

// choices are the lower-cased words a key's last value names, split on | and space; false for none.
func (block *block) choices(key string) ([]string, bool) {
	last, has := block.last(key)
	if !has {
		return nil, false
	}
	var choices []string
	for _, text := range last.words() {
		for choice := range strings.FieldsFuncSeq(strings.ToLower(text), func(char rune) bool { return char == '|' || char == ' ' }) {
			choices = append(choices, choice)
		}
	}
	return choices, len(choices) > 0
}

// color is a key's last color block as CSS; false for none.
func (block *block) color(key string) (string, bool) {
	last, has := block.last(key)
	if !has {
		return "", false
	}
	tag, parts, isColor := jomini.ColorParts(last.assignment.Value)
	if color, isKnown := game.ColorOf(tag, parts); isColor && isKnown {
		return color.CSS(), true
	}
	return "", false
}

// words are a statement's value as words with its file's constants put in: a scalar, a block's values, or the x and
// y a block writes as keys.
func (statement statement) words() []string {
	var words []string
	switch value := statement.assignment.Value.(type) {
	case *jomini.Scalar:
		words = []string{value.Text}
	case *jomini.Block:
		if x, y := jomini.ScalarOf(value, "x"), jomini.ScalarOf(value, "y"); x != nil && y != nil {
			words = []string{x.Text, y.Text}
			break
		}
		for _, item := range value.Statements {
			if valued, isValue := item.(*jomini.ValueStmt); isValue {
				if scalar, isScalar := valued.Value.(*jomini.Scalar); isScalar {
					words = append(words, scalar.Text)
				}
			}
		}
	}
	for i, text := range words {
		words[i] = statement.parsed.Evaluated(text, statement.assignment.Range.Start)
	}
	return words
}

// text is a statement's value as written.
func (statement statement) text() string {
	if scalar, isScalar := statement.assignment.Value.(*jomini.Scalar); isScalar {
		return scalar.Text
	}
	if statement.assignment.Value == nil {
		return ""
	}
	return statement.parsed.Flat(statement.assignment.Value.NodeRange())
}

// span is where a statement is written, as the editor counts.
func (statement statement) span() lsp.Range {
	lines := statement.parsed.Lines()
	return lsp.Range{
		Start: lines.PositionAt(statement.assignment.Range.Start), End: lines.PositionAt(statement.assignment.Range.End),
	}
}
