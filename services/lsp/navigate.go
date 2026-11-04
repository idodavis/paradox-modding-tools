// navigate.go finds where a name is defined and used and renames it. It also lists the symbols of a file and finds
// definitions by name across the workspace.

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
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Symbol is one entry of a file: a definition, with the symbols nested inside it, or a localization key.
type Symbol struct {
	Name      string          `json:"name"`
	Title     string          `json:"title"` // the text the game shows for it in the workspace's language; "" if none
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
	Site     Site            `json:"site"`
	Category schema.Category `json:"category"`
}

// Definition returns where the name at a position is defined, with the definition the game keeps first. For an image
// path it returns the image file. It returns an empty list if the position is on no name.
func (lsp *Service) Definition(path string, line, column int) ([]Site, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	_, reference := findReference(ws, mods, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Database == game.File {
		target, ok := mods.ResolveTexture(reference.Name)
		if !ok {
			return []Site{}, nil
		}
		return []Site{{Place: ws.Locate(mods, target), Line: 1, Range: ToLineRange(1)}}, nil
	}
	if reference.Name == "" {
		return []Site{}, nil
	}
	sites, _ := listSites(ws, mods, place, reference)
	return lo.Map(sites, func(site index.Site, _ int) Site { return LocateSite(ws, mods, site) }), nil
}

// findReference returns the reference a definition or a use makes at the given offset, with the span it is written at.
// The reference has no name if the offset is on none.
func findReference(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, place workspace.Place, offset int) (jomini.Span, schema.Reference) {
	if place.Format == files.FormatLocalization {
		_, reference := describeMessage(ws, tree, place, offset)
		return reference.Span, reference
	}
	visitor := &hoverVisitor{tree: tree, offset: offset}
	ws.Schema.WalkTo(tree, place.File, offset, visitor)
	span, reference := findHit(visitor)
	return span, resolveHit(mods, place, reference, visitor.context.References)
}

// resolveHit returns the reference the name under the cursor stands for: the first of the references written at the
// given reference's span whose database defines the name, as seen from the place's file. A call that may name any
// callable database is tried against each. If none defines it, the given reference comes back.
func resolveHit(mods *index.Index, place workspace.Place, reference schema.Reference, references []schema.Reference) schema.Reference {
	candidates := lo.Filter(references, func(other schema.Reference, _ int) bool { return other.Span == reference.Span })
	if reference.Database == game.AnyCall {
		candidates = index.ListCallees(reference)
	}
	for _, candidate := range candidates {
		if len(mods.ResolveLocal(candidate, place.Position)) > 0 {
			return candidate
		}
	}
	if reference.Database == game.AnyCall {
		reference.Database = ""
	}
	return reference
}

// References returns where the name at a position is defined and where it is used. A localization key's translations
// are left out. It returns an empty list if the position is on no name.
func (lsp *Service) References(path string, line, column int) ([]Site, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	_, reference := findReference(ws, mods, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Name == "" {
		return []Site{}, nil
	}
	occurrences := dropTranslations(ws, mods, place, listOccurrences(mods, place, reference))
	return lo.Map(occurrences, func(site index.Site, _ int) Site { return LocateSite(ws, mods, site) }), nil
}

// listOccurrences returns the sites of a name, its definitions and then its uses, as seen from the given file.
func listOccurrences(mods *index.Index, place workspace.Place, reference schema.Reference) []index.Site {
	return slices.Concat(mods.ResolveLocal(reference, place.Position), mods.ListUses(reference.Database, reference.Name))
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
	span, reference := findReference(ws, mods, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
	if reference.Name == "" {
		return nil, nil
	}
	// a name the game also defines cannot be renamed, since its definition in the game's files stays
	sites := mods.ResolveLocal(reference, place.Position)
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
	_, reference := findReference(ws, mods, tree, place, ToOffset(tree.Src, tree.Lines, line, column))
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

// ListSymbols returns a file's definitions, nested as they are written, or a localization file's keys.
func (lsp *Service) ListSymbols(path string) ([]Symbol, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	return ListFileSymbols(ws, ws.Mods.Load(), path)
}

// ListFileSymbols returns the definitions of a file in the given session, nested as they are written, or the keys of
// a localization file, each with its title in the session's language. It returns none for a file that is neither, and
// an error if the file cannot be read.
func ListFileSymbols(ws *workspace.Session, mods *index.Index, path string) ([]Symbol, error) {
	document, err := ws.LoadDocument(path)
	if err != nil {
		return nil, err
	}
	place := ws.Locate(mods, path)
	if place.Format == files.FormatLocalization {
		return lo.Map(document.Messages.Messages, func(message loc.Message, _ int) Symbol {
			text, _, _ := ws.LookupText(mods, message.Key)
			reference := schema.Reference{Database: game.Localization, Name: message.Key, Span: message.KeySpan}
			return makeSymbol(document.Tree, reference, message.Span, text)
		}), nil
	}
	visitor := &symbolsVisitor{ws: ws, mods: mods, file: place.Position, symbols: []Symbol{}}
	if place.Format.IsScript() {
		ws.Schema.Walk(document.Tree, place.File, visitor)
	}
	return visitor.symbols, nil
}

// A symbolsVisitor builds the outline of a file.
type symbolsVisitor struct {
	ws         *workspace.Session
	mods       *index.Index
	file       int32 // where the index lists the file; -1 where it does not
	symbols    []Symbol
	open       []int   // the position of each open symbol among its siblings, outermost first
	statements []int32 // the statement of each open symbol
}

// Enter adds a symbol for the statement if it defines a name.
func (visitor *symbolsVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if !context.Defines || context.Definition.Database == game.Constant {
		return true
	}
	definition := *context.Definition
	symbol := makeSymbol(tree, schema.Reference{Database: definition.Database, Name: definition.Name, Span: definition.Span},
		tree.Statements[statement].Span, lookupTitle(visitor.ws, visitor.mods, visitor.file, definition))
	siblings := &visitor.symbols
	for _, position := range visitor.open {
		siblings = &(*siblings)[position].Children
	}
	*siblings = append(*siblings, symbol)
	visitor.open, visitor.statements = append(visitor.open, len(*siblings)-1), append(visitor.statements, statement)
	return true
}

// makeSymbol returns the symbol for a name the tree defines, with the given title. The reference holds the name and
// the span it is written at. The given span covers the whole definition, a statement or a message.
func makeSymbol(tree *jomini.Tree, reference schema.Reference, span jomini.Span, title string) Symbol {
	return Symbol{
		Name: reference.Name, Title: title, Database: reference.Database,
		What: game.DescribeDefinition(reference.Database, false), Category: schema.CategoryDefinition,
		Range:     ToRange(tree.Src, tree.Lines, span),
		NameRange: ToRange(tree.Src, tree.Lines, reference.Span),
	}
}

// lookupTitle returns the text the game shows for a definition, in the session's language. It returns "" if there is
// none, or if the mods index does not list the definition in the given file yet.
func lookupTitle(ws *workspace.Session, mods *index.Index, file int32, definition schema.Definition) string {
	sites := mods.ResolveLocal(schema.Reference{Database: definition.Database, Name: definition.Name}, file)
	site, ok := lo.Find(sites, func(site index.Site) bool { return site.File == file })
	if !ok {
		return ""
	}
	return ws.LookupTitle(mods, site)
}

// Leave closes the symbol of the statement, if it opened one.
func (visitor *symbolsVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {
	if last := len(visitor.statements) - 1; last >= 0 && visitor.statements[last] == statement {
		visitor.open, visitor.statements = visitor.open[:last], visitor.statements[:last]
	}
}

// FindSymbols returns up to the given limit of definitions whose name contains the query, then the localization keys
// that do. In each group the mods' files come before the game's. It returns an empty list for an empty query.
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
	checked, matches := make([]bool, len(mods.Names.Texts)), make([]bool, len(mods.Names.Texts))
	isMatch := func(name uint32) bool {
		if !checked[name] {
			checked[name], matches[name] = true, strings.Contains(strings.ToLower(mods.Names.Texts[name]), lower)
		}
		return matches[name]
	}
	// the mods' files are searched first, and a key is listed once, not once for each language
	for _, role := range []index.Role{index.RoleDefines, index.RoleLocalizes} {
		listed := map[string]bool{}
		for _, position := range slices.Concat(mods.ListOwnFiles(), ws.Install.ListOwnFiles()) {
			for number, occurrence := range mods.Occurrences[position] {
				name := mods.Names.Texts[occurrence.Name]
				if occurrence.Role != role || !isMatch(occurrence.Name) || role == index.RoleLocalizes && listed[name] {
					continue
				}
				listed[name] = true
				site := index.Site{File: position, Occurrence: int32(number)}
				reference := mods.ToReference(site)
				symbols = append(symbols, WorkspaceSymbol{
					Name: reference.Name, Database: reference.Database,
					Site: LocateSite(ws, mods, site), Category: schema.CategoryDefinition,
				})
				if len(symbols) == limit {
					return symbols, nil
				}
			}
		}
	}
	return symbols, nil
}
