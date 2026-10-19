// properties.go is the reader every property goes through: a statement with the file and writer it comes from, and
// the properties statements write, read by key, the last written winning.

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

// properties is the statements that apply to a widget in the order they apply, the last of each key at hand.
type properties struct {
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

// propertiesOf is what a statement's block writes, as properties.
func propertiesOf(holder statement) *properties {
	properties := &properties{}
	for _, field := range fields(holder) {
		properties.add(field)
	}
	return properties
}

// add appends statements, each the last of its key.
func (properties *properties) add(statements ...statement) {
	if properties.byKey == nil {
		properties.byKey = map[string]statement{}
	}
	for _, statement := range statements {
		properties.statements = append(properties.statements, statement)
		properties.byKey[strings.ToLower(statement.assignment.Key.Text)] = statement
	}
}

// last is the statement that last wrote a key; false for none.
func (properties *properties) last(key string) (statement, bool) {
	statement, has := properties.byKey[strings.ToLower(key)]
	return statement, has
}

// scalar is the one word a key's last statement holds, its file's constants put in; false for none or a block.
func (properties *properties) scalar(key string) (string, bool) {
	last, has := properties.last(key)
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
func (properties *properties) numbers(key string) ([]float64, bool) {
	last, has := properties.last(key)
	if !has {
		return nil, false
	}
	var numbers []float64
	for _, text := range last.scalars() {
		number, err := strconv.ParseFloat(text, 64)
		if jomini.Literal(text) != jomini.LitNumber || err != nil {
			return nil, false
		}
		numbers = append(numbers, number)
	}
	return numbers, len(numbers) > 0
}

// choices are the lower-cased words a key's last value names, split on | and space; false for none.
func (properties *properties) choices(key string) ([]string, bool) {
	last, has := properties.last(key)
	if !has {
		return nil, false
	}
	var choices []string
	for _, text := range last.scalars() {
		for choice := range strings.FieldsFuncSeq(strings.ToLower(text), func(char rune) bool { return char == '|' || char == ' ' }) {
			choices = append(choices, choice)
		}
	}
	return choices, len(choices) > 0
}

// color is a key's last color block as CSS; false for none.
func (properties *properties) color(key string) (string, bool) {
	last, has := properties.last(key)
	if !has {
		return "", false
	}
	css := cssOf(last.assignment.Value)
	return css, css != ""
}

// cssOf is a value's color as CSS, a color block read by its tag's row; "" for a value that is no color.
func cssOf(value jomini.Value) string {
	tag, parts, isColor := jomini.ColorParts(value)
	if color, isKnown := game.ColorOf(tag, parts); isColor && isKnown {
		return color.CSS()
	}
	return ""
}

// scalars are a statement's value with its file's constants put in: a scalar, a block's values, or the x and y a
// block writes as keys; a tagged value (rgb { }, list "x") is none.
func (statement statement) scalars() []string {
	var scalars []string
	switch value := statement.assignment.Value.(type) {
	case *jomini.Scalar:
		scalars = []string{value.Text}
	case *jomini.Block:
		if across, down := jomini.KeyValue(value, "x", jomini.Match{}), jomini.KeyValue(value, "y", jomini.Match{}); across != nil && down != nil {
			scalars = []string{across.Text, down.Text}
			break
		}
		for _, item := range value.Statements {
			if valued, isValue := item.(*jomini.ValueStmt); isValue {
				if scalar, isScalar := valued.Value.(*jomini.Scalar); isScalar {
					scalars = append(scalars, scalar.Text)
				}
			}
		}
	}
	for i, text := range scalars {
		scalars[i] = statement.parsed.Evaluated(text, statement.assignment.Range.Start)
	}
	return scalars
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
	return statement.rangeOf(statement.assignment.Range)
}

// valueSpan is where a statement's value is written, as the editor counts; its whole span where it has none.
func (statement statement) valueSpan() lsp.Range {
	if statement.assignment.Value == nil {
		return statement.span()
	}
	return statement.rangeOf(statement.assignment.Value.NodeRange())
}

// rangeOf is a byte range of the statement's file as the editor counts.
func (statement statement) rangeOf(span jomini.Range) lsp.Range {
	lines := statement.parsed.Lines()
	return lsp.Range{Start: lines.PositionAt(span.Start), End: lines.PositionAt(span.End)}
}
