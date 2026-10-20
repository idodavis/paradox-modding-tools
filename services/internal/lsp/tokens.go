// tokens.go is what the editor colors from the engine: each key, value and name whose category only the walk knows
// (an entry defined or referred to, an operator, a scope, a variable), over the colors the grammars give by shape.

package lsp

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// Category is what a key, value or name is colored as; each is a category of the grammars (lib/grammars.ts).
type Category string

// The categories the engine colors.
const (
	CategoryEntry    Category = "entry"    // an entry's name where it is defined, or a reference that resolves to one
	CategoryOperator Category = "operator" // a key that is one of the language's operators: a keyword with a class
	CategoryScope    Category = "scope"    // a link to a scope script runs in, or a saved scope
	CategoryVariable Category = "variable" // a variable, or a scripted entry's parameter
)

// Token is one key, value or name the engine colors.
type Token struct {
	Line     int      `json:"line"`   // 0-based
	Col      int      `json:"col"`    // the byte column
	Length   int      `json:"length"` // in bytes
	Category Category `json:"category"`
}

// coloring is how the names of a database are colored.
type coloring struct {
	category Category // "" leaves a name to the grammar
	resolves bool     // a reference colors only where it resolves to a definition
}

// databaseColorings are the databases whose names are not entries that must resolve: a saved scope and a variable are
// told by their database, a parameter only where its entry declares it (a widget's properties are read as the
// parameters its type is passed), a file's path and a data function stay the grammar's (the games quote them).
var databaseColorings = map[string]coloring{
	game.SavedScopes:     {category: CategoryScope, resolves: false},
	game.Variables:       {category: CategoryVariable, resolves: false},
	game.LocalVariables:  {category: CategoryVariable, resolves: false},
	game.GlobalVariables: {category: CategoryVariable, resolves: false},
	schema.Parameter:     {category: CategoryVariable, resolves: true},
	game.File:            {category: "", resolves: false},
	game.DataFunction:    {category: "", resolves: false},
}

// tokenSpan is a token's span of the file.
type tokenSpan struct {
	span     jomini.Range
	category Category // "" leaves the span to the grammar
}

// painter is one file's walk as it colors.
type painter struct {
	file    *openFile
	keys    []tokenSpan // keys read as operators or links
	values  []tokenSpan // values that are links to a scope
	strings []tokenSpan // quoted strings, which stay the grammar's
}

// Tokens returns the keys, values and names the engine colors in a file as the editor sees it (fileTokens).
func Tokens(ws *workspace.Session, path string) ([]Token, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	return fileTokens(file), nil
}

// TextTokens returns the keys, values and names the engine colors in another text of a file, read where the file sits:
// a version a diff shows (fileTokens).
func TextTokens(ws *workspace.Session, path, text string) []Token {
	ws.RLock()
	defer ws.RUnlock()
	parsed := jomini.Parse(text)
	return fileTokens(&openFile{ws: ws, path: path, text: text, parsed: &parsed, place: ws.PlaceOf(path)})
}

// fileTokens returns the keys, values and names of a script or GUI file the engine colors, in file order, from one
// walk; a localization file has none. Layers overlap by priority: strings, keys, occurrences, then scope links.
func fileTokens(file *openFile) []Token {
	if file.place.Format != inventory.Script {
		return nil
	}
	painter := &painter{file: file}
	occurrences, _ := index.Collect(file.ws.Install.Schema, file.place.File, file.parsed, painter.visit)
	occurrenceSpans := make([]tokenSpan, 0, len(occurrences))
	for _, occurrence := range occurrences {
		if painter.oneLine(occurrence.Range) {
			occurrenceSpans = append(occurrenceSpans, tokenSpan{span: occurrence.Range, category: painter.categoryOf(occurrence)})
		}
	}
	var tokenSpans []tokenSpan
	for _, next := range [][]tokenSpan{painter.strings, painter.keys, occurrenceSpans, painter.values} {
		tokenSpans = layer(tokenSpans, next)
	}
	return painter.tokens(tokenSpans)
}

// visit keeps a statement's key where it is one of the language's operators or a link to a scope script runs in, its
// value where it is such a link, and its key or value where it is a quoted string.
func (painter *painter) visit(statement jomini.Statement, context schema.Context, inner *schema.Context,
	_ []schema.Occurrence) bool {
	if assign, isAssign := statement.(*jomini.Assignment); isAssign {
		key := assign.Key.Text
		switch {
		case assign.Key.Quoted:
			painter.strings = append(painter.strings, tokenSpan{span: assign.Key.Range})
		case game.ClassifyKey(context.Shape, context.Command, key) != "" || listClass(*inner, key) != "":
			painter.keys = append(painter.keys, tokenSpan{span: assign.Key.Range, category: CategoryOperator})
		case inner.KeyRule == game.KeyRuleLink && painter.leadsToScope(key):
			painter.keys = append(painter.keys, tokenSpan{span: assign.Key.Range, category: CategoryScope})
		}
	}
	if scalar, isScalar := jomini.ValueOf(statement).(*jomini.Scalar); isScalar {
		switch {
		case scalar.Quoted:
			painter.strings = append(painter.strings, tokenSpan{span: scalar.Range})
		case painter.leadsToScope(scalar.Text):
			painter.values = append(painter.values, tokenSpan{span: scalar.Range, category: CategoryScope})
		}
	}
	return true
}

// listClass returns the class of the list iterator a key is where the walker reads it as one (every_vassal,
// any_in_list); a scripted trigger or a field named with the prefix (random_events) iterates nothing.
func listClass(inner schema.Context, key string) string {
	keyword, isList := game.MatchListIterator(strings.ToLower(key))
	if !isList || inner.KeyRule != game.KeyRuleCommand && inner.List == "" {
		return ""
	}
	return keyword.Class
}

// oneLine reports whether a span starts and ends on one line, as every key, value and name does; a whole-file entry's
// definition does not, and would keep every other name in the file from being colored.
func (painter *painter) oneLine(span jomini.Range) bool {
	lines := painter.file.parsed.Lines()
	return lines.PositionAt(span.Start).Line == lines.PositionAt(span.End).Line
}

// categoryOf returns what an occurrence is colored as: its database's coloring, else an entry that must resolve; a
// reference that resolves to nothing stays the grammar's.
func (painter *painter) categoryOf(occurrence schema.Occurrence) Category {
	ws := painter.file.ws
	databaseColoring, isListed := databaseColorings[occurrence.Database]
	if !isListed {
		databaseColoring = coloring{category: CategoryEntry, resolves: true}
	}
	if databaseColoring.resolves && occurrence.Role == schema.Refers {
		if _, known := index.Resolve(ws.Install.Schema, ws.Indexes(), occurrence, painter.file.path); !known {
			return ""
		}
	}
	return databaseColoring.category
}

// leadsToScope reports whether a text is a link to a scope script runs in, whatever its case (ROOT): root, prev, this,
// or a scope type that runs script. yes and no are literals, though the dumps declare them as links; flag:x runs none.
func (painter *painter) leadsToScope(text string) bool {
	install := painter.file.ws.Install
	lower := strings.ToLower(text)
	link := install.Schema.Link(lower)
	return link != nil && !slices.Contains(game.Bools, lower) && (link.Target == "" || install.Schema.RunsScript(link.Target))
}

// layer returns the token spans of a base sorted by where they start, with each of the next layer that overlaps neither
// a span of the base nor an earlier one of its own.
func layer(base, next []tokenSpan) []tokenSpan {
	compareStarts := func(first, second tokenSpan) int { return cmp.Compare(first.span.Start, second.span.Start) }
	slices.SortFunc(next, compareStarts)
	tokenSpans := slices.Clone(base)
	end := -1
	for _, candidate := range next {
		successor, _ := slices.BinarySearchFunc(base, candidate, compareStarts)
		overlaps := candidate.span.Start < end ||
			successor > 0 && base[successor-1].span.End > candidate.span.Start ||
			successor < len(base) && base[successor].span.Start < candidate.span.End
		if !overlaps {
			tokenSpans = append(tokenSpans, candidate)
			end = candidate.span.End
		}
	}
	slices.SortFunc(tokenSpans, compareStarts)
	return tokenSpans
}

// tokens returns the token spans that have a category, each on the line it starts on.
func (painter *painter) tokens(tokenSpans []tokenSpan) []Token {
	lines := painter.file.parsed.Lines()
	tokens := make([]Token, 0, len(tokenSpans))
	for _, tokenSpan := range tokenSpans {
		if tokenSpan.category != "" {
			start := lines.PositionAt(tokenSpan.span.Start)
			tokens = append(tokens, Token{Line: start.Line, Col: start.Character, Length: tokenSpan.span.End - tokenSpan.span.Start,
				Category: tokenSpan.category})
		}
	}
	return tokens
}
