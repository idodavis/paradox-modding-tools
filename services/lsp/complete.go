package lsp

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// A Completion is one candidate in the completion list.
type Completion struct {
	Label    string          `json:"label"`
	Insert   string          `json:"insert"`           // the text inserted; the label where equal
	Category schema.Category `json:"category"`         // what it is, for the icon and the color
	Detail   string          `json:"detail,omitempty"` // the database's singular, the command's class
	Blurb    string          `json:"blurb,omitempty"`  // the one line shown beside it
	Rank     int             `json:"rank"`             // how often the game writes it there, for the order
	Snippet  bool            `json:"snippet"`          // Insert is a snippet with placeholders
}

// Complete returns the completion candidates at a position in a file. At a key they are the language's operators,
// then the commands and fields the block takes. At a value they are the field's choices, the names the game loads or
// the image files a path could name. It returns an empty list for a file that is not script.
func (lsp *Service) Complete(path string, line, column int) ([]Completion, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	if !place.Format.IsScript() {
		return []Completion{}, nil
	}
	offset := ToOffset(tree.Src, tree.Lines, line, column)
	if statement, ok := findValue(tree, offset); ok {
		current := tree.Statements[statement]
		visitor := &hoverVisitor{offset: current.Span.Start}
		ws.Schema.WalkTo(tree, place.File, visitor.offset, visitor)
		typed := ""
		if !current.Value.IsEmpty() && offset >= current.Value.Start {
			typed = tree.Src[current.Value.Start:offset]
		}
		return listValues(mods, ws.Game, visitor.context.Field, typed), nil
	}
	visitor := &completeVisitor{offset: offset}
	ws.Schema.WalkTo(tree, place.File, offset, visitor)
	if visitor.context.Shape == "" {
		return []Completion{}, nil
	}
	return listKeys(ws.Schema, &visitor.context), nil
}

// findValue returns the statement whose value the given offset is writing. That is a statement with an operator before
// the offset on the same line and no value yet, or a value the offset is inside or at the end of. It returns false if
// the offset is not writing a value.
func findValue(tree *jomini.Tree, offset int) (int32, bool) {
	// the last byte before the offset that is not a space decides which statement the offset follows
	prefix := strings.TrimRight(tree.Src[:offset], " \t")
	if prefix == "" {
		return 0, false
	}
	statement := tree.FindStatement(len(prefix) - 1)
	current := tree.Statements[statement]
	if current.IsBlock() || current.Op.IsEmpty() || offset < current.Op.End || strings.Contains(tree.Src[current.Op.End:offset], "\n") ||
		!current.Value.IsEmpty() && offset > current.Value.End {
		return 0, false
	}
	return statement, true
}

// listValues returns the values a field may take: its choices, yes and no, the loaded names of a database, or the
// image files under the folder typed so far. The given text is what is typed, with the quote it may begin with. It
// returns an empty list if the field states no value.
func listValues(mods *index.Index, row *game.Game, field *schema.Field, typed string) []Completion {
	if field == nil || !field.Value.IsStated() {
		return []Completion{}
	}
	expected, typed := field.Value.Value, strings.TrimPrefix(typed, `"`)
	rank := func(value string) int { return max(field.Vanilla.Values[value], field.Vanilla.Values[`"`+value+`"`]) }
	makeConstant := func(value string, _ int) Completion {
		return Completion{Label: value, Insert: value, Category: schema.CategoryConstant, Rank: rank(value)}
	}
	values := []Completion{}
	switch {
	case expected.Type == schema.ValueBool:
		values = lo.Map(game.Bools, makeConstant)
	case expected.Type == schema.ValueChoice:
		values = lo.Map(expected.Choices, makeConstant)
	case expected.Type == schema.ValueReference && expected.Database == game.File:
		for _, file := range mods.ListImages(typed[:strings.LastIndex(typed, "/")+1]) {
			// script writes a path without the stage folder it sits in
			_, rel := schema.SplitStage(row, file.Rel)
			values = append(values, Completion{Label: rel, Insert: rel, Category: schema.CategoryReference})
		}
	case expected.Type == schema.ValueReference:
		for _, site := range mods.ListDefinitions(expected.Database) {
			if key := mods.ToReference(site).Name; mods.IsLoaded(site.File) {
				values = append(values, Completion{
					Label: key, Insert: key, Category: schema.CategoryReference,
					Detail: game.DescribeDefinition(expected.Database, false), Rank: rank(key),
				})
			}
		}
	}
	return lo.UniqBy(values, func(candidate Completion) string { return candidate.Label })
}

// A completeVisitor finds the context of the innermost block that holds an offset.
type completeVisitor struct {
	offset  int
	context schema.Context
}

// Enter keeps the context of the block if it holds the offset.
func (visitor *completeVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if block := tree.Statements[statement].Block; visitor.offset > block.Start && visitor.offset < block.End {
		visitor.context = *context
	}
	return true
}

// Leave does nothing.
func (visitor *completeVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

// listKeys returns the keys a block takes: the language's operators that fit it, then its commands, then its fields,
// each kind ranked above the next. A key listed twice is kept once, as the first kind.
func listKeys(schema *schema.Schema, holder *schema.Context) []Completion {
	fields := listFields(holder)
	commands := listCommands(schema.Commands, holder, findTopRank(fields)+1)
	operators := listOperators(schema.Commands, holder, findTopRank(commands)+1)
	candidates := slices.Concat(operators, commands, fields)
	return lo.UniqBy(candidates, func(candidate Completion) string { return candidate.Label })
}

// findTopRank returns the highest rank among some candidates, or 0 if there are none.
func findTopRank(candidates []Completion) int {
	return lo.MaxBy(candidates, func(first, second Completion) bool { return first.Rank > second.Rank }).Rank
}

// listFields returns the keys of a block's field in alphabetical order. It returns nil if the block has no field.
func listFields(holder *schema.Context) []Completion {
	if holder.Field == nil {
		return nil
	}
	var fields []Completion
	for _, key := range slices.Sorted(maps.Keys(holder.Field.Children)) {
		field := holder.Field.Children[key]
		if strings.HasPrefix(key, "<") {
			continue
		}
		fields = append(fields, makeKey(Completion{
			Label: key, Category: schema.CategoryField, Detail: string(field.Value.Value.Type), Blurb: field.Blurb.Value,
			Rank: field.Vanilla.Count,
		}, isBlockField(field)))
	}
	return fields
}

// listOperators returns the keys of the language that fit a block, ranked above the given floor. They come grouped
// by class, the classes in the order they first come in game.Keywords and each class's keys in the table's order. A
// key that is written with a block is inserted with one.
func listOperators(commands map[string]*schema.Command, holder *schema.Context, floor int) []Completion {
	fitting := lo.Filter(game.Keywords, func(keyword game.Keyword, _ int) bool {
		return !strings.ContainsAny(keyword.Key, "/*<") && isOperatorFit(keyword, commands[keyword.Key], holder)
	})
	classes := lo.Uniq(lo.Map(fitting, func(keyword game.Keyword, _ int) string { return keyword.Class }))
	slices.SortStableFunc(fitting, func(first, second game.Keyword) int {
		return cmp.Compare(slices.Index(classes, first.Class), slices.Index(classes, second.Class))
	})
	return lo.Map(fitting, func(keyword game.Keyword, position int) Completion {
		command := commands[keyword.Key]
		var claim *schema.Field
		if holder.Field != nil {
			claim = holder.Field.Children[keyword.Key]
		}
		block := isBlockShape(keyword.Shape) || isBlockField(claim) || command != nil && isBlockCommand(command)
		return makeKey(Completion{
			Label: keyword.Key, Category: schema.CategoryKeyword, Detail: keyword.Class, Blurb: keyword.Blurb,
			Rank: floor + len(fitting) - 1 - position,
		}, block)
	})
}

// isBlockShape reports whether the given shape can only be written as a block. A value or unknown shape may be plain.
func isBlockShape(shape string) bool {
	return shape != "" && shape != game.ShapeValue && shape != game.ShapeUnknown
}

// isBlockField reports whether a field is written as a block. It is false for a nil field.
func isBlockField(field *schema.Field) bool {
	return field != nil && (field.Value.Value.Type == schema.ValueBlock || len(field.Children) > 0 ||
		field.Shape.IsStated() && isBlockShape(field.Shape.Value) ||
		field.Vanilla.Blocks > 0 && field.Vanilla.Blocks == field.Vanilla.Count)
}

// isOperatorFit reports whether a keyword of the language, with the command of its key, fits the block of the given
// context.
func isOperatorFit(keyword game.Keyword, command *schema.Command, holder *schema.Context) bool {
	switch {
	case keyword.Holder != "":
		return keyword.Holder == holder.Shape
	case command != nil:
		_, ok := holder.FindDeclaration(command)
		return ok
	}
	return len(game.Shapes[holder.Shape].Allows) > 0
}

// listCommands returns the commands a block takes, in alphabetical order. Each is ranked above the given floor by how
// often the game writes it in the block's field.
func listCommands(commands map[string]*schema.Command, holder *schema.Context, floor int) []Completion {
	var candidates []Completion
	for _, key := range slices.Sorted(maps.Keys(commands)) {
		command := commands[key]
		declaration, ok := holder.FindDeclaration(command)
		if !ok || command.Wild {
			continue
		}
		rank := floor
		if holder.Field != nil && holder.Field.Children[key] != nil {
			rank += holder.Field.Children[key].Vanilla.Count
		}
		candidates = append(candidates, makeKey(Completion{
			Label: key, Category: schema.CategoryCommand, Detail: declaredLabels[declaration], Blurb: command.Blurb,
			Rank: rank,
		}, isBlockCommand(command)))
	}
	return candidates
}

// isBlockCommand reports whether a command is written with a block: it takes block arguments, leads to a scope or
// walks a list.
func isBlockCommand(command *schema.Command) bool {
	return isBlockField(command.Arguments) || command.Target != "" || command.Lists
}

// makeKey returns the given candidate with its insert text set: the key and an equals sign, followed by an empty block
// if the flag is set.
func makeKey(candidate Completion, block bool) Completion {
	candidate.Insert = candidate.Label + " = "
	if block {
		candidate.Insert, candidate.Snippet = candidate.Label+" = {\n\t$0\n}", true
	}
	return candidate
}
