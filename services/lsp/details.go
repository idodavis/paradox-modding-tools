// details.go answers the editor's smaller questions about a file: where it folds, the hints and color boxes it draws,
// how a color is written, and the signature of the call under the cursor.

package lsp

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// A Fold is one foldable region: its first and last lines, counted from one.
type Fold struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// The kinds of Hint.
const (
	HintDate = "date" // a date read in the mod's calendar
	HintText = "text" // the text of a localization key
)

// A Hint is a text drawn inline beside the code, such as a date read in the mod's calendar. Its line and column are
// Monaco's, counted from one, the column in UTF-16 code units.
type Hint struct {
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
	Kind   string `json:"kind"` // HintDate or HintText
}

// hintTextLength is how many characters of a localization key's text a hint shows.
const hintTextLength = 60

// A ColorSite is one color written in a file: where it is and the color it stands for.
type ColorSite struct {
	Range Range       `json:"range"`
	Color jomini.RGBA `json:"color"`
}

// A Signature is the parameter list of a scripted definition at a call, with the parameter the position is on.
type Signature struct {
	Label      string   `json:"label"`
	Parameters []string `json:"parameters"`
	Active     *int     `json:"active,omitempty"` // the parameter the position is on; absent if none
}

// ListFolds returns a file's foldable regions: every block that spans more than one line.
func (lsp *Service) ListFolds(path string) ([]Fold, error) {
	_, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	folds := []Fold{}
	for _, statement := range tree.Statements {
		if !statement.IsBlock() {
			continue
		}
		start, end := tree.Lines.ToPosition(statement.Block.Start).Line, tree.Lines.ToPosition(statement.Block.End).Line
		if end > start {
			folds = append(folds, Fold{Start: start + 1, End: end + 1})
		}
	}
	return folds, nil
}

// ListHints returns a file's inlay hints: each date in the file read in the mod's calendar, and the text of each
// localization key it names in the workspace's language. A value is a date only where its field expects one.
func (lsp *Service) ListHints(path string) ([]Hint, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	mod, _ := lsp.Workspace.LookupMod(place.Origin)
	visitor := &hintsVisitor{ws: ws, mods: mods, calendar: mod.Calendar, hints: []Hint{}}
	if place.Format.IsScript() {
		ws.Schema.Walk(tree, place.File, visitor)
	}
	return visitor.hints, nil
}

// A hintsVisitor collects the hints of a file.
type hintsVisitor struct {
	ws       *workspace.Session
	mods     *index.Index // where the texts of the localization keys are looked up
	calendar app.Calendar
	hints    []Hint
}

// Enter records the hint for the statement's value if it is a date, and a hint after each localization key the
// statement names.
func (visitor *hintsVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	for _, reference := range context.References {
		if reference.Database != game.Localization {
			continue
		}
		end := reference.Span.End
		if strings.HasPrefix(tree.Src[end:], `"`) {
			end++
		}
		if text := visitor.describeText(reference.Name); text != "" {
			visitor.add(tree, end, HintText, text)
		}
	}
	if date, ok := findDate(tree, statement, context); ok {
		card := resolveDate(visitor.calendar, date)
		parts := []string{}
		if card.Date.Day > 0 {
			parts = append(parts, fmt.Sprint(card.Date.Day))
		}
		if card.Month != "" {
			parts = append(parts, card.Month)
		}
		parts = append(parts, fmt.Sprint(card.Date.Year))
		if card.Era != "" {
			parts = append(parts, card.Era)
		}
		visitor.add(tree, tree.Statements[statement].Value.End, HintDate, strings.Join(parts, " "))
	}
	return true
}

// Leave does nothing.
func (visitor *hintsVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

// add records a hint of the given kind and text at the given offset in the tree's source.
func (visitor *hintsVisitor) add(tree *jomini.Tree, offset int, kind, text string) {
	point := ToRange(tree.Src, tree.Lines, jomini.Span{Start: offset, End: offset})
	visitor.hints = append(visitor.hints, Hint{Line: point.StartLine, Column: point.StartColumn, Text: text, Kind: kind})
}

// describeText returns what a hint shows after a localization key: the key's text in quotes, cut at hintTextLength
// characters with "…", or "no text yet" where no language has a message with the key. It returns "" for a key that
// only other languages than the workspace's write.
func (visitor *hintsVisitor) describeText(key string) string {
	text, _, ok := visitor.ws.LookupText(visitor.mods, key)
	if !ok {
		if lo.ContainsBy(visitor.mods.ListMessages(key), func(site index.Site) bool { return visitor.mods.IsLoaded(site.File) }) {
			return ""
		}
		return "no text yet"
	}
	if characters := []rune(text); len(characters) > hintTextLength {
		text = string(characters[:hintTextLength]) + "…"
	}
	return `"` + text + `"`
}

// ListColorSites returns every color written in a file, with the color it makes.
func (lsp *Service) ListColorSites(path string) ([]ColorSite, error) {
	_, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	sites := []ColorSite{}
	for statement := int32(1); int(statement) < len(tree.Statements); statement++ {
		if color, ok := jomini.ToColor(tree, statement); ok {
			span := tree.Statements[statement].ResolveValueSpan()
			sites = append(sites, ColorSite{Range: ToRange(tree.Src, tree.Lines, span), Color: color})
		}
	}
	return sites, nil
}

// PresentColor returns the text that writes a color into a file, in the same form as the color written in the given
// range: the same tag and scale, with an alpha only if that color has one. A range that holds no color, such as a
// coat's layer that does not write the color yet, gets it in the form the game's own coats write their colors in
// (game.Game.Coat.ColorForm). It returns an error if the form cannot be written.
func (lsp *Service) PresentColor(path string, colorRange Range, color jomini.RGBA) (string, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return "", err
	}
	offset := ToOffset(tree.Src, tree.Lines, colorRange.StartLine, colorRange.StartColumn)
	tag, parts, ok := jomini.ColorParts(tree, tree.FindStatement(offset))
	if !ok {
		tag, parts = ws.Game.Coat.ColorForm.Tag, ws.Game.Coat.ColorForm.Example
	}
	encoded, ok := jomini.EncodeColor(tag, parts, color)
	if !ok {
		return "", fmt.Errorf("the color form %q cannot be written", tag)
	}
	return strings.TrimSpace(tag + " { " + strings.Join(encoded, " ") + " }"), nil
}

// Signature returns the signature help for a call at a position in a file, or nil if the position is not inside
// a call.
func (lsp *Service) Signature(path string, line, column int) (*Signature, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	offset := ToOffset(tree.Src, tree.Lines, line, column)
	visitor := &hoverVisitor{tree: tree, offset: offset}
	ws.Schema.WalkTo(tree, ws.Locate(ws.Mods.Load(), path).File, offset, visitor)
	call, callee, key, ok := findCall(ws, visitor)
	if !ok {
		return nil, nil
	}
	parameters := lo.Filter(slices.Sorted(maps.Keys(callee.Children)), func(key string, _ int) bool {
		return !strings.HasPrefix(key, "<")
	})
	var active *int
	for child := range tree.Children(call) {
		if position := slices.Index(parameters, tree.Unquoted(tree.Statements[child].Key)); position >= 0 &&
			tree.Statements[child].Span.Contains(offset) {
			active = &position
		}
	}
	return &Signature{Label: key + "(" + strings.Join(parameters, ", ") + ")", Parameters: parameters, Active: active}, nil
}

// findCall returns the call to a scripted definition that holds the visited position: its statement, the field that
// lists the parameters the definition takes, and the key it is called by. It returns false if the position is in no
// such call, or if the definition takes no parameters.
func findCall(ws *workspace.Session, visitor *hoverVisitor) (int32, *schema.Field, string, bool) {
	for statement, context := visitor.statement, &visitor.context; statement > 0; {
		current := visitor.tree.Statements[statement]
		if current.IsBlock() && visitor.offset > current.Block.Start && visitor.offset < current.Block.End &&
			context.Category == schema.CategoryCall && context.Field != nil {
			for _, reference := range lo.FlatMap(context.References, func(reference schema.Reference, _ int) []schema.Reference {
				return index.ListCallees(reference)
			}) {
				if database := ws.Schema.Databases[reference.Database]; reference.Name == visitor.tree.Unquoted(current.Key) &&
					database != nil && database.Callable.Parameters {
					return statement, context.Field, reference.Name, true
				}
			}
		}
		statement, context = current.Parent, context.Parent
	}
	return 0, nil, "", false
}
