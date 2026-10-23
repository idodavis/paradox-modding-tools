package lsp

import (
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// A Completion is one candidate the completion list shows: the text to insert, how it ranks, and what the item
// shows.
type Completion struct {
	Label    string          `json:"label"`
	Insert   string          `json:"insert"`           // the text inserted; the label where equal
	Category schema.Category `json:"category"`         // what it is, for the icon and the color
	Detail   string          `json:"detail,omitempty"` // the database's singular, the command's class
	Blurb    string          `json:"blurb,omitempty"`  // the one line shown beside it
	Rank     int             `json:"rank"`             // how often vanilla writes it in this holder, for the order
	Snippet  bool            `json:"snippet"`          // Insert is a snippet with placeholders
}

// blockClasses are the operator classes whose keys always hold a block.
var blockClasses = []string{"logical", "conditional", "flow", "display"}

// fixedShapes are the shapes of blocks that take their own keys and none of the language's operators.
var fixedShapes = []string{"fields", "widget", "setup", "description"}

// Complete returns the completion candidates at a position in a file. In a key position they are the operators
// first, then the commands and fields the block takes, ranked by how often the game's own files write each one
// there. In a value position they come from the field's expected type: its choices, the names the game loads, or
// the texture files a path could name. Every candidate is sent; Monaco filters by what was typed.
func (lsp *Service) Complete(path string, line, column int) ([]Completion, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	place := ws.Locate(path)
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
		return listValues(ws.Mods.Load(), visitor.context.Field, typed), nil
	}
	visitor := &completeVisitor{offset: offset}
	ws.Schema.WalkTo(tree, place.File, offset, visitor)
	if visitor.context.Shape == "" {
		return []Completion{}, nil
	}
	return listKeys(ws.Schema, &visitor.context), nil
}

// findValue returns the statement that an offset writes the value of: one with an operator before the offset on the
// same line, and either no value or a value that the offset is in or at the end of. It returns false if the offset
// writes no value.
func findValue(tree *jomini.Tree, offset int) (int32, bool) {
	// the last written byte before the offset decides which statement the offset follows
	written := strings.TrimRight(tree.Src[:offset], " \t")
	if written == "" {
		return 0, false
	}
	statement := tree.FindStatement(len(written) - 1)
	current := tree.Statements[statement]
	if current.IsBlock() || current.Op.IsEmpty() || offset < current.Op.End || strings.Contains(tree.Src[current.Op.End:offset], "\n") ||
		!current.Value.IsEmpty() && offset > current.Value.End {
		return 0, false
	}
	return statement, true
}

// listValues returns the values a field's value may be: its choices, yes and no, the names of a database that a file
// the game loads defines, or the image files under the folder written so far. The text typed so far is given as it
// stands, with a quote it may begin with. It returns none where the field states no value.
func listValues(mods *index.Index, field *schema.Field, typed string) []Completion {
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
			_, written := schema.SplitStage(game.LookupGame(mods.Game), file.Rel)
			values = append(values, Completion{Label: written, Insert: written, Category: schema.CategoryReference})
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

// A completeVisitor is the visitor behind Complete. It keeps the context of the block that holds the offset.
type completeVisitor struct {
	offset  int
	context schema.Context
}

// Enter keeps the context of the innermost block covering the offset.
func (visitor *completeVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if block := tree.Statements[statement].Block; visitor.offset > block.Start && visitor.offset < block.End {
		visitor.context = *context
	}
	return true
}

// Leave does nothing: completion needs only the holding block's context.
func (visitor *completeVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

// listKeys returns the keys a block takes: the operators of the language that fit its shape, then the commands its
// shape admits, then the keys of its field. Where one key comes up twice, the first kind keeps it.
func listKeys(schema *schema.Schema, holder *schema.Context) []Completion {
	fields := listFields(holder)
	top := lo.MaxBy(fields, func(first, second Completion) bool { return first.Rank > second.Rank }).Rank
	candidates := slices.Concat(listOperators(holder.Shape, top+1), listCommands(schema.Commands, holder), fields)
	return lo.UniqBy(candidates, func(candidate Completion) string { return candidate.Label })
}

// listFields returns the keys of a holder's field, in alphabetical order, each ranked by how often the game's own
// files write it there. It returns none where the walk knows no field.
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
		block := field.Value.Value.Type == schema.ValueBlock || len(field.Children) > 0 ||
			field.Vanilla.Blocks > 0 && field.Vanilla.Blocks == field.Vanilla.Count
		fields = append(fields, makeKey(Completion{
			Label: key, Category: schema.CategoryField, Detail: string(field.Value.Value.Type), Blurb: field.Blurb.Value,
			Rank: field.Vanilla.Count,
		}, block))
	}
	return fields
}

// listOperators returns the keys of the language that fit a block of a shape, each with the given rank. A block of a
// fixed shape takes none, and effects have no place in a block of triggers.
func listOperators(shape string, rank int) []Completion {
	operators := []Completion{}
	for _, keyword := range game.Keywords {
		if (keyword.Holder != shape && (keyword.Holder != "" || slices.Contains(fixedShapes, shape))) ||
			strings.ContainsAny(keyword.Key, "/*<") || shape == "triggers" && (keyword.Class == "flow" || keyword.Class == "saving") {
			continue
		}
		block := keyword.Shape != "" || slices.Contains(blockClasses, keyword.Class) && keyword.Key != "always"
		operators = append(operators, makeKey(Completion{
			Label: keyword.Key, Category: schema.CategoryKeyword, Detail: keyword.Class, Blurb: keyword.Blurb, Rank: rank,
		}, block))
	}
	return operators
}

// listCommands returns the commands that a block's shape admits, in alphabetical order. A block inside triggers takes
// triggers only.
func listCommands(commands map[string]*schema.Command, holder *schema.Context) []Completion {
	var candidates []Completion
	allowed := game.Shapes[holder.Shape].Allows
	for _, key := range slices.Sorted(maps.Keys(commands)) {
		command := commands[key]
		position := slices.IndexFunc(command.Declared, func(declared string) bool {
			return slices.Contains(allowed, declared) && (declared != game.DeclaredEffect || !holder.Conditional)
		})
		if position < 0 || command.Wild {
			continue
		}
		block := command.Arguments != nil || command.Target != "" || command.Lists
		candidates = append(candidates, makeKey(Completion{
			Label: key, Category: schema.CategoryCommand, Detail: declaredLabels[command.Declared[position]], Blurb: command.Blurb,
		}, block))
	}
	return candidates
}

// makeKey returns the given candidate for a key with its insert text: the key with an equals sign after it, or with
// a block to fill in where the key holds one.
func makeKey(candidate Completion, block bool) Completion {
	candidate.Insert = candidate.Label + " = "
	if block {
		candidate.Insert, candidate.Snippet = candidate.Label+" = {\n\t$0\n}", true
	}
	return candidate
}
