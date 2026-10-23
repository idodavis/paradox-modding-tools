package lsp

import (
	"errors"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Symbol is one definition in a file as the outline shows it, with the symbols nested inside it.
type Symbol struct {
	Name      string          `json:"name"`
	Database  string          `json:"database"`
	Range     Range           `json:"range"`     // the whole statement
	NameRange Range           `json:"nameRange"` // the name alone
	Children  []Symbol        `json:"children,omitempty"`
	Category  schema.Category `json:"category"` // for the icon
}

// A WorkspaceSymbol is one definition found by the symbol search across the workspace.
type WorkspaceSymbol struct {
	Name     string          `json:"name"`
	Database string          `json:"database"`
	Location Location        `json:"location"`
	Category schema.Category `json:"category"`
}

// Definition returns where the name at a position is defined, the definition the game keeps first. For a written
// file path it returns the file itself.
func (lsp *Service) Definition(path string, line, column int) ([]Location, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods, place := ws.Mods.Load(), ws.Locate(path)
	_, reference := findReference(ws, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Database == game.File {
		target, ok := mods.ResolveTexture(reference.Name)
		if !ok {
			return []Location{}, nil
		}
		return []Location{{Place: ws.Locate(target), Range: Range{StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 1}}}, nil
	}
	if reference.Name == "" {
		return []Location{}, nil
	}
	return lo.Map(listSites(ws, mods, place, reference), func(site CardSite, _ int) Location {
		return locate(ws, site.Place, site.Line)
	}), nil
}

// findReference returns the name that a definition or a reference makes at an offset, with the span it is written at.
// The name is empty where the offset is on none.
func findReference(ws *workspace.Session, tree *jomini.Tree, place workspace.Place, offset int) (jomini.Span, schema.Reference) {
	if place.Format == files.FormatLocalization {
		_, reference := describeMessage(ws, tree, place, offset)
		return reference.Span, reference
	}
	visitor := &hoverVisitor{offset: offset}
	ws.Schema.WalkTo(tree, place.File, offset, visitor)
	return findHit(tree, visitor)
}

// References returns every use of the name at a position.
func (lsp *Service) References(path string, line, column int) ([]Location, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods, place := ws.Mods.Load(), ws.Locate(path)
	_, reference := findReference(ws, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Name == "" {
		return []Location{}, nil
	}
	return lo.Map(listOccurrences(ws, mods, place, reference), func(site index.Site, _ int) Location {
		return locate(ws, ws.Locate(mods.File(site).Path), int(mods.Occurrence(site).Line)+1)
	}), nil
}

// listOccurrences returns the sites of a name: its definitions and then its uses, as the file the name is written in
// sees them.
func listOccurrences(ws *workspace.Session, mods *index.Index, place workspace.Place, reference schema.Reference) []index.Site {
	return slices.Concat(resolve(ws, mods, reference, place.Path), mods.ListUses(reference.Database, reference.Name))
}

// PrepareRename returns the range of the name at a position, or nil if the position is not on a name that a mod
// defines.
func (lsp *Service) PrepareRename(path string, line, column int) (*Range, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods, place := ws.Mods.Load(), ws.Locate(path)
	span, reference := findReference(ws, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Name == "" {
		return nil, nil
	}
	// a name the game also defines cannot be renamed, since its definition in the game's files stays
	sites := resolve(ws, mods, reference, place.Path)
	if len(sites) == 0 || lo.SomeBy(sites, func(site index.Site) bool { return mods.File(site).Origin == game.OriginVanilla }) {
		return nil, nil
	}
	renamable := ToRange(tree.Src, tree.Lines, findKeySpan(tree.Src, span, reference.Name))
	return &renamable, nil
}

// findKeySpan returns the part of a span that a key takes: a definition's span may begin with an entry mode, and a
// key may be written in another case than the index keeps it. It is the span itself where the key is not in it.
func findKeySpan(text string, span jomini.Span, key string) jomini.Span {
	start := strings.LastIndex(strings.ToLower(text[span.Start:span.End]), strings.ToLower(key))
	if start < 0 {
		return span
	}
	return jomini.Span{Start: span.Start + start, End: span.Start + start + len(key)}
}

// Rename returns the edits that give the name at a position a new name, grouped by the path of each mod file that
// defines or uses it.
func (lsp *Service) Rename(path string, line, column int, name string) (map[string][]Edit, error) {
	if name == "" {
		return nil, errors.New("a name cannot be empty")
	}
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods, place := ws.Mods.Load(), ws.Locate(path)
	_, reference := findReference(ws, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	edits := map[string][]Edit{}
	for _, site := range listOccurrences(ws, mods, place, reference) {
		target := mods.File(site).Path
		if mods.File(site).Origin == game.OriginVanilla {
			continue
		}
		modTree, err := ws.LoadTree(target)
		if err != nil {
			continue
		}
		span := findKeySpan(modTree.Src, mods.Occurrence(site).Span, reference.Name)
		edits[target] = append(edits[target], Edit{Range: ToRange(modTree.Src, modTree.Lines, span), Text: name})
	}
	return edits, nil
}

// ListSymbols returns a file's definitions for the outline, nested as they are written.
func (lsp *Service) ListSymbols(path string) ([]Symbol, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	visitor, place := &symbolsVisitor{symbols: []Symbol{}}, ws.Locate(path)
	if place.Format.IsScript() {
		ws.Schema.Walk(tree, place.File, visitor)
	}
	return visitor.symbols, nil
}

// A symbolsVisitor is the visitor behind ListSymbols. It builds the outline as the walk goes in and out of the
// definitions' blocks.
type symbolsVisitor struct {
	symbols    []Symbol
	open       []int   // the position of each open symbol among its siblings, from the outermost in
	statements []int32 // the statement of each open symbol
}

// Enter adds a symbol for the statement if it makes a definition, inside the symbol it is nested in.
func (visitor *symbolsVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if !context.Defines || context.Definition.Database == game.Constant {
		return true
	}
	definition := context.Definition
	symbol := Symbol{
		Name: definition.Name, Database: definition.Database, Category: schema.CategoryDefinition,
		Range:     ToRange(tree.Src, tree.Lines, tree.Statements[statement].Span),
		NameRange: ToRange(tree.Src, tree.Lines, findKeySpan(tree.Src, definition.Span, definition.Name)),
	}
	siblings := &visitor.symbols
	for _, position := range visitor.open {
		siblings = &(*siblings)[position].Children
	}
	*siblings = append(*siblings, symbol)
	visitor.open, visitor.statements = append(visitor.open, len(*siblings)-1), append(visitor.statements, statement)
	return true
}

// Leave closes the symbol the statement opened, if it did.
func (visitor *symbolsVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {
	if last := len(visitor.statements) - 1; last >= 0 && visitor.statements[last] == statement {
		visitor.open, visitor.statements = visitor.open[:last], visitor.statements[:last]
	}
}

// FindSymbols returns the definitions whose name contains the query, the mods' first, up to the given limit.
func (lsp *Service) FindSymbols(query string, limit int) ([]WorkspaceSymbol, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	symbols := []WorkspaceSymbol{}
	lower := strings.ToLower(query)
	if lower == "" || limit <= 0 {
		return symbols, nil
	}
	matching := make([]bool, len(mods.Names.Texts))
	for ordinal, text := range mods.Names.Texts {
		matching[ordinal] = strings.Contains(strings.ToLower(text), lower)
	}
	// the mods' files are searched first
	for _, position := range slices.Concat(mods.ListOwnFiles(), ws.Install.ListOwnFiles()) {
		for number, occurrence := range mods.Occurrences[position] {
			if occurrence.Role != index.RoleDefines || !matching[occurrence.Name] {
				continue
			}
			site := index.Site{File: position, Occurrence: int32(number)}
			reference := mods.ToReference(site)
			symbols = append(symbols, WorkspaceSymbol{
				Name: reference.Name, Database: reference.Database,
				Location: locate(ws, ws.Locate(mods.File(site).Path), int(occurrence.Line)+1), Category: schema.CategoryDefinition,
			})
			if len(symbols) == limit {
				return symbols, nil
			}
		}
	}
	return symbols, nil
}

// locate returns the location of a line in a file.
func locate(ws *workspace.Session, place workspace.Place, line int) Location {
	tree, err := ws.LoadTree(place.Path)
	if err != nil || line < 1 || line > tree.Lines.CountLines() {
		return Location{Place: place}
	}
	return Location{Place: place, Range: ToRange(tree.Src, tree.Lines, tree.Lines.LineSpan(line-1))}
}
