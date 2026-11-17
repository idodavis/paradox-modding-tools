// details.go answers the editor's smaller questions: folds, hints, colors and
// signatures.

package lsp

import (
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// A Fold is one foldable region, in lines counting from 1.
type Fold struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// The kinds of Hint: a date, and the text of a localization key.
const (
	HintDate = "date"
	HintText = "text"
)

// A Hint is a text drawn inline beside the code.
type Hint struct {
	// Line and Column are Monaco's, counting from 1.
	Line   int `json:"line"`
	Column int `json:"column"`

	Text string `json:"text"`

	// Kind is HintDate or HintText.
	Kind string `json:"kind"`
}

// A ColorSite is one color written in a file.
type ColorSite struct {
	Range jomini.Range `json:"range"`
	Color jomini.RGBA  `json:"color"`
}

// A Signature is the parameter list of a scripted definition at a call.
type Signature struct {
	Label      string   `json:"label"`
	Parameters []string `json:"parameters"`

	// Active is the index of the parameter the position is on, or nil.
	Active *int `json:"active,omitempty"`
}

// ListFolds returns the blocks of a file that span more than one line.
func (s *Service) ListFolds(path string) ([]Fold, error) {
	_, tree, err := s.loadTree(path)
	if err != nil {
		return nil, err
	}

	folds := []Fold{}
	for _, block := range tree.ListFolds() {
		folds = append(folds, Fold{Start: tree.Lines.ToPosition(block.Start).Line + 1, End: tree.Lines.ToPosition(block.End).Line + 1})
	}

	return folds, nil
}

// ListHints returns a file's dates and localization texts as inlay hints.
func (s *Service) ListHints(path string) ([]Hint, error) {
	ws, document, err := s.loadDocument(path)
	if err != nil {
		return nil, err
	}

	mods := ws.Mods.Load()
	mod, _ := s.Workspace.LookupMod(ws.Locate(mods, path).Origin)
	tree, hints := document.Tree, []Hint{}
	for position := range document.Contexts {
		statement, context := int32(position), &document.Contexts[position]
		for _, reference := range context.References {
			if reference.Database != lang.Localization {
				continue
			}

			end := reference.Span.End
			if strings.HasPrefix(tree.Src[end:], `"`) {
				end++
			}

			if text := describeText(ws, mods, reference.Name); text != "" {
				hints = append(hints, makeHint(tree, end, HintText, text))
			}
		}

		if date, ok := findDate(tree, statement, context); ok {
			end := tree.Statements[statement].Value.End
			hints = append(hints, makeHint(tree, end, HintDate, describeDate(mod.Calendar, date)))
		}
	}

	return hints, nil
}

// hintTextLength is how many characters of a text a hint shows.
const hintTextLength = 60

// describeText returns a key's text in quotes, cut at hintTextLength, "no text
// yet" if no language has it, or "" if only other languages write it.
func describeText(ws *workspace.Session, mods *index.Index, key string) string {
	text, _, ok := ws.LookupText(mods, key)
	if !ok {
		if slices.ContainsFunc(mods.ListMessages(key), func(site index.Site) bool { return mods.IsLoaded(site.File) }) {
			return ""
		}

		return "no text yet"
	}

	if characters := []rune(text); len(characters) > hintTextLength {
		text = string(characters[:hintTextLength]) + "…"
	}

	return `"` + text + `"`
}

func makeHint(tree *jomini.Tree, offset int, kind, text string) Hint {
	point := jomini.ToRange(tree.Src, tree.Lines, jomini.Span{Start: offset, End: offset})

	return Hint{Line: point.StartLine, Column: point.StartColumn, Text: text, Kind: kind}
}

// ListColorSites returns every color written in a file.
func (s *Service) ListColorSites(path string) ([]ColorSite, error) {
	_, tree, err := s.loadTree(path)
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

// PresentColor returns the text that writes a color in the form used in the
// range, else in the game's coat form. It returns an error if that form cannot
// be written.
func (s *Service) PresentColor(path string, colorRange jomini.Range, color jomini.RGBA) (string, error) {
	ws, tree, err := s.loadTree(path)
	if err != nil {
		return "", err
	}

	offset := jomini.ToOffset(tree.Src, tree.Lines, colorRange.StartLine, colorRange.StartColumn)
	tag, parts, ok := jomini.ColorParts(tree, tree.FindStatement(offset))
	if !ok {
		coatColor := lang.LookupDialect(ws.Game.ID).CoatColor
		tag, parts = coatColor.Tag, coatColor.Example
	}

	encoded, ok := jomini.EncodeColor(tag, parts, color)
	if !ok {
		return "", fmt.Errorf("the color form %q cannot be written", tag)
	}

	return strings.TrimSpace(tag + " { " + strings.Join(encoded, " ") + " }"), nil
}

// Signature returns the signature help for the call at a position, or nil.
func (s *Service) Signature(path string, line, column int) (*Signature, error) {
	ws, document, err := s.loadDocument(path)
	if err != nil {
		return nil, err
	}

	tree := document.Tree
	offset := jomini.ToOffset(tree.Src, tree.Lines, line, column)
	call, callee, key, ok := findCall(ws, findCursor(document, offset))
	if !ok {
		return nil, nil
	}

	parameters := listDeclared(callee)

	var active *int
	for child := range tree.Children(call) {
		position := slices.Index(parameters, tree.Unquoted(tree.Statements[child].Key))
		if position >= 0 && tree.Statements[child].Span.Contains(offset) {
			active = &position
		}
	}

	return &Signature{Label: key + "(" + strings.Join(parameters, ", ") + ")", Parameters: parameters, Active: active}, nil
}

// findCall returns the call to a scripted definition around the cursor, with
// its parameters field and the key it is called by. It returns false if none.
func findCall(ws *workspace.Session, cursor cursor) (int32, *lang.Field, string, bool) {
	for statement, context := cursor.statement, cursor.context; statement > 0; {
		current := cursor.tree.Statements[statement]
		if current.IsBlock() && cursor.offset > current.Block.Start && cursor.offset < current.Block.End &&
			context.Category == lang.CategoryCall && context.Field != nil {
			var callees []lang.Reference
			for _, reference := range context.References {
				callees = append(callees, index.ListCallees(reference)...)
			}

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
