// wording.go is the card's words: what a block holds, what a key takes, what vanilla writes, a
// requirement, what declares a command, a scope in a sentence, a database as a heading, a position's keys
// in the order a card lists them.

package lsp

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/schema"
)

// The card's words for a number and a yes-or-no value.
const (
	aNumber = "a number"
	yesOrNo = "yes or no"
)

// blockHoldsText is what a block of each shape holds, as a card says it.
var blockHoldsText = map[game.Shape]string{
	game.ShapeTriggers: "triggers", game.ShapeEffects: "effects", game.ShapeValue: aNumber,
	game.ShapeDescription: "a description",
}

// returnsText is what a database's entries give where they are run, by what their block holds.
var returnsText = map[game.Shape]string{game.ShapeTriggers: yesOrNo, game.ShapeValue: aNumber}

// typeText returns a value type as a card says it; "" unknown.
func typeText(claim schema.Expected) string {
	return map[schema.ValueType]string{
		schema.ValueNumber: aNumber, schema.ValueBool: yesOrNo, schema.ValueDate: "a date", schema.ValueString: "text",
		schema.ValueReference: game.DescribeEntry(claim.Database, false), schema.ValueLoc: "a localization key",
		schema.ValueFile: "a file path", schema.ValueScope: "a scope", schema.ValueBlock: "a block",
	}[claim.Type]
}

// takesText returns what a key takes as declared, never vanilla's inference: its block's triggers or effects, its
// value's type or list (a long one ends with how many more), the keys of an arguments block, and its default.
func takesText(key *schema.Position) []string {
	if key == nil {
		return nil
	}
	claim, out := key.Value.Value, []string(nil)
	if key.Value.Source == game.SourceVanilla {
		claim = schema.Expected{}
	}
	texts := []string{blockHoldsText[key.Shape.Value], typeText(claim)}
	if names := slices.Sorted(maps.Keys(key.Children)); key.Shape.Value == game.ShapeArguments && len(names) > 0 {
		texts = append(texts, "a block with "+strings.Join(names, ", "))
	}
	if len(claim.Choices) > 0 {
		texts = append(texts, oneOf(claim.Choices))
	}
	if key.Default.Source != game.SourceNone {
		texts = append(texts, "defaults to "+key.Default.Value)
	}
	for _, text := range texts {
		if text != "" && !slices.Contains(out, text) {
			out = append(out, text)
		}
	}
	return out
}

// vanillaWritesText returns what vanilla writes at a key as a card says it: each form written there, its words and
// texts typed by vanilla's names, else its words listed where few.
func vanillaWritesText(key *schema.Position) (out []string) {
	words := slices.Sorted(maps.Keys(key.Vanilla.Values))
	for _, row := range []struct {
		form schema.Writes
		text string
	}{
		{schema.WritesNumber, aNumber},
		{schema.WritesBool, yesOrNo},
		{schema.WritesDate, "a date"},
		{schema.WritesIdent, "a word"},
		{schema.WritesString, "text"},
		{schema.WritesBlock, "a block"},
	} {
		form, text := row.form, row.text
		switch {
		case key.Vanilla.Writes&form == 0:
			continue
		case form&(schema.WritesIdent|schema.WritesString) == 0:
		case key.Value.Source == game.SourceVanilla:
			text = typeText(key.Value.Value)
		case form == schema.WritesIdent && len(words) <= 6:
			text = oneOf(words)
		}
		if !slices.Contains(out, text) {
			out = append(out, text)
		}
	}
	return out
}

// requirementText returns what a declared source says of a key being required: optional, required, required when.
func requirementText(key *schema.Position) string {
	if key == nil {
		return ""
	}
	switch required := key.Required.Value; required.Level {
	case schema.LevelOptional:
		return "optional"
	case schema.LevelAlways:
		return "required"
	case schema.LevelConditional:
		return "required when " + required.Condition
	}
	return ""
}

// kindText returns what a command is, in the dumps' words (effect, trigger, event target), joined with "and".
func kindText(command *schema.Command) string {
	return strings.Join(command.Declared, " and ")
}

// scopeText returns a scope type in a sentence: an article and the type's words (a landed title, an activity).
func scopeText(scope string) string {
	words := strings.ReplaceAll(scope, "_", " ")
	if strings.ContainsRune("aeiou", rune(words[0])) {
		return "an " + words
	}
	return "a " + words
}

// builtInHeading returns what a card calls a kind the game builds in, one a call by this word names (a widget kind).
func builtInHeading(caller string) string { return "built-in " + caller + " kind" }

// oneOf returns a word list as a card says it: the first six, then how many more.
func oneOf(words []string) string {
	if len(words) > 6 {
		words = append(words[:6:6], fmt.Sprintf("… and %d more", len(words)-6))
	}
	return "one of " + strings.Join(words, ", ")
}

// positionKeys returns a position's written keys, placeholders aside (those a declared source states first, then
// vanilla's, the most written first), and how many are stated.
func positionKeys(position *schema.Position) (names []string, stated int) {
	rank := map[string]int{}
	for name, key := range position.Children {
		if strings.HasPrefix(name, "<") {
			continue
		}
		names, rank[name] = append(names, name), 1
		if key.Source != game.SourceNone {
			rank[name], stated = 0, stated+1
		}
	}
	slices.SortFunc(names, func(first, second string) int {
		return cmp.Or(rank[first]-rank[second], position.Children[second].Vanilla.Count-position.Children[first].Vanilla.Count,
			strings.Compare(first, second))
	})
	return names, stated
}
