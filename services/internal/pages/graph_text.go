// graph_text.go is a block's text as the page shows it: one line, its source lines, a block's own trigger, a dynamic
// description's parts and an option's names.

package pages

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/workspace"
)

// eventText reads the parts of an event's block that the page shows as text: a trigger, a description's versions,
// an option's names; each key's text is its localization key's entry in the workspace's language.
type eventText struct {
	ws     *workspace.Session
	parsed *jomini.Result
}

// oneLine is a block's statements on one line: each as its source folded to single spaces, an inner block as its
// head and { … }.
func oneLine(parsed *jomini.Result, statements []jomini.Statement) string {
	parts := []string{"{"}
	for _, statement := range statements {
		span := statement.NodeRange()
		if inner := jomini.ChildBlock(statement); inner != nil {
			parts = append(parts, parsed.Flat(jomini.Range{Start: span.Start, End: inner.OpenBrace})+" { … }")
			continue
		}
		parts = append(parts, parsed.Flat(span))
	}
	return strings.Join(append(parts, "}"), " ")
}

// spanLines is the source lines a span covers, each without its line end; with a limit, the first limit lines and a
// "…" where the span runs on.
func spanLines(parsed *jomini.Result, span jomini.Range, limit int) []string {
	lines := parsed.Lines()
	first := lines.PositionAt(span.Start).Line
	last := lines.PositionAt(max(span.End-1, span.Start)).Line
	var out []string
	for line := first; line <= last; line++ {
		if limit > 0 && len(out) == limit {
			return append(out, "…")
		}
		start, end := lines.OffsetAt(line, 0), len(parsed.Src)
		if line+1 < lines.LineCount() {
			end = lines.OffsetAt(line+1, 0)
		}
		out = append(out, strings.TrimRight(parsed.Src[start:end], "\r\n"))
	}
	return out
}

// trigger is the block's own trigger on one line, and its statement: the first trigger key whose value is a block.
func (text eventText) trigger(block *jomini.Block) (string, jomini.Statement) {
	for _, statement := range block.Statements {
		assign, isAssign := statement.(*jomini.Assignment)
		if !isAssign || assign.Key.Text != game.DynamicDescription.Trigger {
			continue
		}
		if inner := jomini.BlockOf(assign.Value); inner != nil {
			return text.parsed.Flat(jomini.Range{Start: inner.OpenBrace + 1, End: inner.CloseBrace}), assign
		}
	}
	return "", nil
}

// scalar is the one-word value the block writes for a key; nil where it writes none.
func (text eventText) scalar(block *jomini.Block, key string) *jomini.Scalar {
	for _, field := range blockFieldsOf(block) {
		if scalar, isScalar := field.Value.(*jomini.Scalar); isScalar && field.Key.Text == key {
			return scalar
		}
	}
	return nil
}

// version is one text a key shows: the key at its line, and its text in the workspace's language where it has one.
func (text eventText) version(scalar *jomini.Scalar, when string) Version {
	version := Version{When: when, Key: scalar.Text, Line: text.parsed.Lines().PositionAt(scalar.Range.Start).Line}
	if entry, known := text.ws.Loc(scalar.Text, ""); known {
		version.Text = entry.Text()
	}
	return version
}

// parts is a dynamic description as the game reads it, each statement a part in order: a leaf's block is read in
// its place, a leaf or a triggered leaf is one part, and a first-valid or random-valid block is one part of the
// versions of each statement in it.
func (text eventText) parts(block *jomini.Block) []TextPart {
	shape := game.DynamicDescription
	var parts []TextPart
	for _, statement := range block.Statements {
		assign, isAssign := statement.(*jomini.Assignment)
		if !isAssign {
			continue
		}
		key, inner := assign.Key.Text, jomini.BlockOf(assign.Value)
		switch {
		case key == shape.Leaf && inner != nil:
			parts = append(parts, text.parts(inner)...)
		case key == shape.Leaf || key == shape.Triggered:
			parts = append(parts, TextPart{Versions: text.versions(statement)})
		case slices.Contains(shape.Markers, key) && inner != nil:
			part := TextPart{Random: key == shape.Random}
			for _, entry := range inner.Statements {
				part.Versions = append(part.Versions, text.versions(entry)...)
			}
			parts = append(parts, part)
		}
	}
	return parts
}

// versions is the texts one statement of a description can show: a leaf's one, or a triggered leaf's with its
// trigger; none for any other statement.
func (text eventText) versions(statement jomini.Statement) []Version {
	assign, isAssign := statement.(*jomini.Assignment)
	if !isAssign {
		return nil
	}
	shape := game.DynamicDescription
	switch assign.Key.Text {
	case shape.Leaf:
		if scalar, isScalar := assign.Value.(*jomini.Scalar); isScalar {
			return []Version{text.version(scalar, "")}
		}
	case shape.Triggered:
		if block := jomini.BlockOf(assign.Value); block != nil {
			if scalar := text.scalar(block, shape.Leaf); scalar != nil {
				when, _ := text.trigger(block)
				return []Version{text.version(scalar, when)}
			}
		}
	}
	return nil
}

// option is one option block: its trigger, every name it is written with (a plain one, or a block holding the text
// and its trigger) and the flags it is marked with.
func (text eventText) option(block *jomini.Block, line int) Option {
	option := Option{Line: line}
	option.When, _ = text.trigger(block)
	for _, field := range blockFieldsOf(block) {
		switch scalar, isScalar := field.Value.(*jomini.Scalar); {
		case field.Key.Text == "name" && isScalar:
			option.Names = append(option.Names, text.version(scalar, ""))
		case field.Key.Text == "name":
			if name, named := text.blockName(field); named {
				option.Names = append(option.Names, name)
			}
		case isScalar && scalar.Text == game.Bools[0] && slices.Contains(game.OptionFlags, field.Key.Text):
			option.Flags = append(option.Flags, field.Key.Text)
		}
	}
	return option
}

// blockName is the name an option writes as a block: the text key's word with the block's own trigger.
func (text eventText) blockName(field *jomini.Assignment) (Version, bool) {
	inner := jomini.BlockOf(field.Value)
	if inner == nil {
		return Version{}, false
	}
	label := text.scalar(inner, "text")
	if label == nil {
		return Version{}, false
	}
	when, _ := text.trigger(inner)
	return text.version(label, when), true
}

// blockFieldsOf is a block's assignments; none for no block.
func blockFieldsOf(block *jomini.Block) []*jomini.Assignment {
	if block == nil {
		return nil
	}
	var fields []*jomini.Assignment
	for _, statement := range block.Statements {
		if assign, isAssign := statement.(*jomini.Assignment); isAssign {
			fields = append(fields, assign)
		}
	}
	return fields
}
