package lsp

import (
	"errors"
	"fmt"
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
	What      string          `json:"what"`      // what a definition of its database is called, such as "window"
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

// Definition returns where the name at a position is defined, with the definition the game keeps first. For an image
// path it returns the image file. It returns an empty list if the position is on no name.
func (lsp *Service) Definition(path string, line, column int) ([]Location, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	_, reference := findReference(ws, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Database == game.File {
		target, ok := mods.ResolveTexture(reference.Name)
		if !ok {
			return []Location{}, nil
		}
		return []Location{{Place: ws.Locate(mods, target), Range: ToLineRange(1)}}, nil
	}
	if reference.Name == "" {
		return []Location{}, nil
	}
	return lo.Map(listSites(ws, mods, place, reference), func(site CardSite, _ int) Location {
		return locate(ws, site.Place, site.Line)
	}), nil
}

// findReference returns the reference a definition or a use makes at the given offset, with the span it is written at.
// The reference has no name if the offset is on none.
func findReference(ws *workspace.Session, tree *jomini.Tree, place workspace.Place, offset int) (jomini.Span, schema.Reference) {
	if place.Format == files.FormatLocalization {
		_, reference := describeMessage(ws, tree, place, offset)
		return reference.Span, reference
	}
	visitor := &hoverVisitor{offset: offset}
	ws.Schema.WalkTo(tree, place.File, offset, visitor)
	span, reference := findHit(tree, visitor)
	return span, resolveHit(ws.Mods.Load(), place, reference, visitor.context.References)
}

// resolveHit returns the reference that the name under the cursor makes, of the given references written at its span:
// the first whose database defines the name, as seen from the given file, as a value that may be a language or a
// dialect is either. A call that may name any callable database stands for one reference per callable database. If
// none defines the name, the given reference is returned, with no database for a call.
func resolveHit(mods *index.Index, place workspace.Place, reference schema.Reference, references []schema.Reference) schema.Reference {
	candidates := lo.Filter(references, func(other schema.Reference, _ int) bool { return other.Span == reference.Span })
	if reference.Database == game.AnyCall {
		candidates = index.ListCallees(reference)
	}
	if position := slices.IndexFunc(candidates, func(candidate schema.Reference) bool {
		return len(mods.ResolveLocal(candidate, place.Path)) > 0
	}); position >= 0 {
		return candidates[position]
	}
	if reference.Database == game.AnyCall {
		reference.Database = ""
	}
	return reference
}

// References returns where the name at a position is defined and where it is used. A localization key's translations
// are left out. It returns an empty list if the position is on no name.
func (lsp *Service) References(path string, line, column int) ([]Location, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	_, reference := findReference(ws, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Name == "" {
		return []Location{}, nil
	}
	occurrences := dropTranslations(ws, mods, place, listOccurrences(mods, place, reference))
	return lo.Map(occurrences, func(site index.Site, _ int) Location {
		return locateOccurrence(ws, mods, site)
	}), nil
}

// listOccurrences returns the sites of a name, its definitions and then its uses, as seen from the given file.
func listOccurrences(mods *index.Index, place workspace.Place, reference schema.Reference) []index.Site {
	return slices.Concat(mods.ResolveLocal(reference, place.Path), mods.ListUses(reference.Database, reference.Name))
}

// PrepareRename returns the range of the name at a position. It returns nil if the position is on no name, if the
// game defines the name too, or if no mod defines it.
func (lsp *Service) PrepareRename(path string, line, column int) (*Range, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	span, reference := findReference(ws, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Name == "" {
		return nil, nil
	}
	// a name the game also defines cannot be renamed, since its definition in the game's files stays
	sites := mods.ResolveLocal(reference, place.Path)
	if len(sites) == 0 || lo.SomeBy(sites, func(site index.Site) bool { return mods.File(site).Origin == game.OriginVanilla }) {
		return nil, nil
	}
	renamable := ToRange(tree.Src, tree.Lines, span)
	return &renamable, nil
}

// Rename returns the edits that give the name at a position a new name, grouped by the mod file they change. It returns
// an error if the new name is empty, or if the name has no database and more than one database defines it.
func (lsp *Service) Rename(path string, line, column int, name string) (map[string][]Edit, error) {
	if name == "" {
		return nil, errors.New("a name cannot be empty")
	}
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	_, reference := findReference(ws, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Database == "" {
		if databases := mods.ListDatabases(reference.Name); len(databases) > 1 {
			slices.Sort(databases)
			return nil, fmt.Errorf("%s is written with no database, and more than one database defines it: %s",
				reference.Name, strings.Join(databases, ", "))
		}
	}
	edits := map[string][]Edit{}
	for _, site := range listOccurrences(mods, place, reference) {
		target := mods.File(site).Path
		if mods.File(site).Origin == game.OriginVanilla {
			continue
		}
		modTree, err := ws.LoadTree(target)
		if err != nil {
			continue
		}
		edits[target] = append(edits[target], Edit{Range: ToRange(modTree.Src, modTree.Lines, mods.Occurrence(site).Span), Text: name})
	}
	return edits, nil
}

// ListSymbols returns a file's definitions for the outline, nested as they are written.
func (lsp *Service) ListSymbols(path string) ([]Symbol, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	return ListFileSymbols(ws, path)
}

// ListFileSymbols returns the definitions of a file in the given session, nested as they are written. It returns none
// for a file that is not script, and an error if the file cannot be read.
func ListFileSymbols(ws *workspace.Session, path string) ([]Symbol, error) {
	tree, err := ws.LoadTree(path)
	if err != nil {
		return nil, err
	}
	visitor, place := &symbolsVisitor{symbols: []Symbol{}}, ws.Locate(ws.Mods.Load(), path)
	if place.Format.IsScript() {
		ws.Schema.Walk(tree, place.File, visitor)
	}
	return visitor.symbols, nil
}

// A symbolsVisitor builds the outline of a file.
type symbolsVisitor struct {
	symbols    []Symbol
	open       []int   // the position of each open symbol among its siblings, outermost first
	statements []int32 // the statement of each open symbol
}

// Enter adds a symbol for the statement if it defines a name.
func (visitor *symbolsVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if !context.Defines || context.Definition.Database == game.Constant {
		return true
	}
	definition := context.Definition
	symbol := Symbol{
		Name: definition.Name, Database: definition.Database, What: game.DescribeDefinition(definition.Database, false),
		Category:  schema.CategoryDefinition,
		Range:     ToRange(tree.Src, tree.Lines, tree.Statements[statement].Span),
		NameRange: ToRange(tree.Src, tree.Lines, definition.Span),
	}
	siblings := &visitor.symbols
	for _, position := range visitor.open {
		siblings = &(*siblings)[position].Children
	}
	*siblings = append(*siblings, symbol)
	visitor.open, visitor.statements = append(visitor.open, len(*siblings)-1), append(visitor.statements, statement)
	return true
}

// Leave closes the symbol of the statement, if it opened one.
func (visitor *symbolsVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {
	if last := len(visitor.statements) - 1; last >= 0 && visitor.statements[last] == statement {
		visitor.open, visitor.statements = visitor.open[:last], visitor.statements[:last]
	}
}

// FindSymbols returns the definitions whose name contains the query, the mods' first, and then the localization keys
// that do, up to the given limit. It returns an empty list for an empty query.
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
	// the mods' files are searched first, and a key is listed once, not once for each language
	for _, role := range []index.Role{index.RoleDefines, index.RoleLocalizes} {
		listed := map[string]bool{}
		for _, position := range slices.Concat(mods.ListOwnFiles(), ws.Install.ListOwnFiles()) {
			for number, occurrence := range mods.Occurrences[position] {
				name := mods.Names.Texts[occurrence.Name]
				if occurrence.Role != role || !strings.Contains(strings.ToLower(name), lower) || role == index.RoleLocalizes && listed[name] {
					continue
				}
				listed[name] = true
				site := index.Site{File: position, Occurrence: int32(number)}
				reference := mods.ToReference(site)
				symbols = append(symbols, WorkspaceSymbol{
					Name: reference.Name, Database: reference.Database,
					Location: locateOccurrence(ws, mods, site), Category: schema.CategoryDefinition,
				})
				if len(symbols) == limit {
					return symbols, nil
				}
			}
		}
	}
	return symbols, nil
}

// locateOccurrence returns the location of an occurrence: the range of its name, with the text of its line. The range
// is empty if the file cannot be read.
func locateOccurrence(ws *workspace.Session, mods *index.Index, site index.Site) Location {
	occurrence := mods.Occurrence(site)
	location := locate(ws, ws.Locate(mods, mods.File(site).Path), int(occurrence.Line)+1)
	if tree, err := ws.LoadTree(location.Place.Path); err == nil {
		location.Range = ToRange(tree.Src, tree.Lines, occurrence.Span)
	}
	return location
}

// locate returns the location of a line in a file. The range is empty if the file cannot be read or has no such line.
func locate(ws *workspace.Session, place workspace.Place, line int) Location {
	tree, err := ws.LoadTree(place.Path)
	if err != nil || line < 1 || line > tree.Lines.CountLines() {
		return Location{Place: place}
	}
	span := tree.Lines.LineSpan(line - 1)
	return Location{Place: place, Range: ToRange(tree.Src, tree.Lines, span), Text: tree.Src[span.Start:span.End]}
}
