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
	Range jomini.Range `json:"range"`
	Color jomini.RGBA  `json:"color"`
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
	for _, block := range tree.ListFolds() {
		folds = append(folds, Fold{Start: tree.Lines.ToPosition(block.Start).Line + 1, End: tree.Lines.ToPosition(block.End).Line + 1})
	}
	return folds, nil
}

// ListHints returns a file's inlay hints: each date read in the mod's calendar, and the text of each localization key
// in the workspace's language.
func (lsp *Service) ListHints(path string) ([]Hint, error) {
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	mod, _ := lsp.Workspace.LookupMod(ws.Locate(mods, path).Origin)
	hints := &hintList{ws: ws, mods: mods, calendar: mod.Calendar, hints: []Hint{}}
	for position := range document.Contexts {
		hints.addStatement(document.Tree, int32(position), &document.Contexts[position])
	}
	return hints.hints, nil
}

// A hintList collects the hints of a file.
type hintList struct {
	ws       *workspace.Session
	mods     *index.Index // where the texts of the localization keys are looked up
	calendar app.Calendar
	hints    []Hint
}

// addStatement records the hint for the statement's value if it is a date,
// and a hint after each localization key the statement names.
func (h *hintList) addStatement(tree *jomini.Tree, statement int32, context *schema.Context) {
	for _, reference := range context.References {
		if reference.Database != game.Localization {
			continue
		}
		end := reference.Span.End
		if strings.HasPrefix(tree.Src[end:], `"`) {
			end++
		}
		if text := h.describeText(reference.Name); text != "" {
			h.add(tree, end, HintText, text)
		}
	}
	if date, ok := findDate(tree, statement, context); ok {
		h.add(tree, tree.Statements[statement].Value.End, HintDate, describeDate(h.calendar, date))
	}
}

// add records a hint of the given kind and text at the given offset in the tree's source.
func (h *hintList) add(tree *jomini.Tree, offset int, kind, text string) {
	point := jomini.ToRange(tree.Src, tree.Lines, jomini.Span{Start: offset, End: offset})
	h.hints = append(h.hints, Hint{Line: point.StartLine, Column: point.StartColumn, Text: text, Kind: kind})
}

// describeText returns what a hint shows after a localization key: its text in quotes, cut at hintTextLength
// characters, or "no text yet" if no language has it. It returns "" for a key only other languages write.
func (h *hintList) describeText(key string) string {
	text, _, ok := h.ws.LookupText(h.mods, key)
	if !ok {
		if lo.ContainsBy(h.mods.ListMessages(key), func(site index.Site) bool { return h.mods.IsLoaded(site.File) }) {
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
			sites = append(sites, ColorSite{Range: jomini.ToRange(tree.Src, tree.Lines, span), Color: color})
		}
	}
	return sites, nil
}

// PresentColor returns the text that writes a color into a file, in the form of the color already written at the given
// range. If the range holds no color, it uses the form the game's coats write colors in. It returns an error if the
// form cannot be written.
func (lsp *Service) PresentColor(path string, colorRange jomini.Range, color jomini.RGBA) (string, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return "", err
	}
	offset := jomini.ToOffset(tree.Src, tree.Lines, colorRange.StartLine, colorRange.StartColumn)
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

// Signature returns the signature help for the call at a position, or nil if the position is not inside a call.
func (lsp *Service) Signature(path string, line, column int) (*Signature, error) {
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}
	tree := document.Tree
	offset := jomini.ToOffset(tree.Src, tree.Lines, line, column)
	call, callee, key, ok := findCall(ws, findCursor(document, offset))
	if !ok {
		return nil, nil
	}
	parameters := lo.Filter(slices.Sorted(maps.Keys(callee.Children)), func(key string, _ int) bool {
		return !strings.HasPrefix(key, "<")
	})
	var active *int
	for child := range tree.Children(call) {
		position := slices.Index(parameters, tree.Unquoted(tree.Statements[child].Key))
		if position >= 0 && tree.Statements[child].Span.Contains(offset) {
			active = &position
		}
	}
	return &Signature{Label: key + "(" + strings.Join(parameters, ", ") + ")", Parameters: parameters, Active: active}, nil
}

// findCall returns the call to a scripted definition that holds the visited position: its statement, the field that
// lists the definition's parameters, and the key it is called by. It returns false if the position is in no such call
// or the definition takes no parameters.
func findCall(ws *workspace.Session, cursor cursor) (int32, *schema.Field, string, bool) {
	for statement, context := cursor.statement, cursor.context; statement > 0; {
		current := cursor.tree.Statements[statement]
		if current.IsBlock() && cursor.offset > current.Block.Start && cursor.offset < current.Block.End &&
			context.Category == schema.CategoryCall && context.Field != nil {
			callees := lo.FlatMap(context.References, func(reference schema.Reference, _ int) []schema.Reference {
				return index.ListCallees(reference)
			})
			for _, reference := range callees {
				database := ws.Schema.Databases[reference.Database]
				if reference.Name == cursor.tree.Unquoted(current.Key) && database != nil && database.Declaration.Parameters {
					return statement, context.Field, reference.Name, true
				}
			}
		}
		statement, context = current.Parent, context.Parent
	}
	return 0, nil, "", false
}
