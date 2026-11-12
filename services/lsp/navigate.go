// navigate.go finds where a name is defined and used and renames it. It also
// lists the symbols of a file and finds definitions by name across the
// workspace.

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

// A Symbol is one entry of a file: a definition, with the symbols nested inside
// it, or a localization key.
type Symbol struct {
	Name      string       `json:"name"`
	Title     string       `json:"title"` // the text the game shows for it in the workspace's language; "" if none
	Database  string       `json:"database"`
	What      string       `json:"what"`      // what a definition of its database is called, such as "window"
	Range     jomini.Range `json:"range"`     // the whole statement
	NameRange jomini.Range `json:"nameRange"` // the name alone
	Children  []Symbol     `json:"children,omitempty"`
}

// A WorkspaceSymbol is one definition found by the symbol search across the
// workspace.
type WorkspaceSymbol struct {
	Name     string `json:"name"`
	Database string `json:"database"`
	Site     Site   `json:"site"`
}

// Definition returns where the name at a position is defined, with the
// definition the game keeps first. For an image path it returns the image file.
// It returns an empty list if the position is on no name.
func (lsp *Service) Definition(path string, line, column int) ([]Site, error) {
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	_, reference := findReference(ws, mods, document, place, jomini.ToOffset(document.Tree.Src, document.Tree.Lines, line, column))
	if reference.Database == game.File {
		target, ok := mods.ResolveTexture(reference.Name)
		if !ok {
			return []Site{}, nil
		}
		return []Site{{Place: ws.Locate(mods, target), Line: 1, Range: jomini.ToLineRange(1)}}, nil
	}
	if reference.Name == "" {
		return []Site{}, nil
	}

	// an effect, a trigger or a link is defined by its line of the game's
	// dump, where the install has the dump
	declared := ws.Schema.LookupDefinition(reference.Database, reference.Name)
	if declared != nil && declared.Path != "" {
		return []Site{{Place: ws.Locate(mods, declared.Path), Line: declared.Line, Range: jomini.ToLineRange(declared.Line)}}, nil
	}

	sites, _ := listSites(ws, mods, place, reference)
	return locateSites(ws, mods, reference, sites, false), nil
}

// locateSites returns the given sites of a reference's name, each with its
// range and its line's text. A file the editor has open gives the sites of
// the editor's own text instead, since the index holds the saved file's: its
// definitions of the name, and its uses too where the flag says.
func locateSites(ws *workspace.Session, mods *index.Index, reference schema.Reference, sites []index.Site, uses bool) []Site {
	located, read := []Site{}, map[string]bool{}
	for _, site := range sites {
		path := mods.File(site).Path
		if _, open := ws.LookupBuffer(path); !open {
			located = append(located, LocateSite(ws, mods, site))
		} else if !read[path] {
			read[path] = true
			located = append(located, listBufferSites(ws, mods, path, reference, uses)...)
		}
	}
	return located
}

// listBufferSites returns where the document of an open file writes a
// reference's name: each definition of it, and each use where the flag says.
// A call that may be to any callable database counts as a use of a callable
// name. It returns none if the file cannot be read.
func listBufferSites(ws *workspace.Session, mods *index.Index, path string, reference schema.Reference, uses bool) []Site {
	document, err := ws.WalkDocument(path)
	if err != nil {
		return nil
	}
	name := mods.ToCaseless(reference.Database, reference.Name)
	callable := slices.Contains(game.Called, reference.Database)
	matches := func(other schema.Reference) bool {
		return (other.Database == reference.Database || callable && other.Database == game.AnyCall) &&
			mods.ToCaseless(reference.Database, other.Name) == name
	}
	var spans []jomini.Span
	for _, contexts := range [][]schema.Context{document.Contexts, document.MessageContexts} {
		for position := range contexts {
			context := &contexts[position]
			if definition := context.Definition; context.Defines && matches(schema.Reference{Database: definition.Database, Name: definition.Name}) {
				spans = append(spans, definition.Span)
			}
			for _, other := range context.References {
				if uses && matches(other) {
					spans = append(spans, other.Span)
				}
			}
		}
	}
	place, tree := ws.Locate(mods, path), document.Tree
	return lo.Map(spans, func(span jomini.Span, _ int) Site {
		line := tree.Lines.ToPosition(span.Start).Line
		row := tree.Lines.LineSpan(line)
		return Site{Place: place, Line: line + 1, Range: jomini.ToRange(tree.Src, tree.Lines, span), Text: tree.Src[row.Start:row.End]}
	})
}

// findReference returns the reference a definition or a use makes at the given
// offset, with the span it is written at. The reference has no name if the
// offset is on none.
func findReference(
	ws *workspace.Session, mods *index.Index, document schema.Document, place workspace.Place, offset int,
) (jomini.Span, schema.Reference) {
	if place.Format == files.FormatLocalization {
		_, reference := describeMessage(ws, document, offset)
		return reference.Span, reference
	}
	cursor := findCursor(document, offset)
	span, reference := findHit(cursor)
	return span, resolveHit(mods, place, reference, cursor.context.References)
}

// resolveHit returns the reference the name under the cursor stands for: the
// first of the references written at the given reference's span whose database
// defines the name, as seen from the place's file. A call that may name any
// callable database is tried against each. If none defines it, the given
// reference comes back.
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

// References returns where the name at a position is defined and where it is
// used. A localization key's translations are left out. It returns an empty
// list if the position is on no name.
func (lsp *Service) References(path string, line, column int) ([]Site, error) {
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	_, reference := findReference(ws, mods, document, place, jomini.ToOffset(document.Tree.Src, document.Tree.Lines, line, column))
	if reference.Name == "" {
		return []Site{}, nil
	}
	occurrences := dropTranslations(ws, mods, place, listOccurrences(mods, place, reference))
	return locateSites(ws, mods, reference, occurrences, true), nil
}

// listOccurrences returns the sites of a name, its definitions and then its
// uses, as seen from the given file.
func listOccurrences(mods *index.Index, place workspace.Place, reference schema.Reference) []index.Site {
	return slices.Concat(mods.ResolveLocal(reference, place.Position), mods.ListUses(reference.Database, reference.Name))
}

// PrepareRename returns the range of the name at a position. It returns nil
// if the position is on no name, if nothing defines it, or if a file that
// cannot be changed defines it too: one of the game's or of a Workshop mod.
func (lsp *Service) PrepareRename(path string, line, column int) (*jomini.Range, error) {
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	span, reference := findReference(ws, mods, document, place, jomini.ToOffset(document.Tree.Src, document.Tree.Lines, line, column))
	if reference.Name == "" {
		return nil, nil
	}
	// a name a read-only file also defines cannot be renamed, since that
	// definition stays
	sites := mods.ResolveLocal(reference, place.Position)
	if len(sites) == 0 || lo.SomeBy(sites, func(site index.Site) bool { return ws.Locate(mods, mods.File(site).Path).ReadOnly }) {
		return nil, nil
	}
	renamable := jomini.ToRange(document.Tree.Src, document.Tree.Lines, span)
	return &renamable, nil
}

// Rename returns the edits that give the name at a position a new name,
// grouped by the file they change. A file that cannot be changed, the game's
// or a Workshop mod's, gets none. It returns an error if the new name is
// empty, or if the name has no database and more than one database defines
// it.
func (lsp *Service) Rename(path string, line, column int, name string) (map[string][]Edit, error) {
	if name == "" {
		return nil, errors.New("a name cannot be empty")
	}
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	_, reference := findReference(ws, mods, document, place, jomini.ToOffset(document.Tree.Src, document.Tree.Lines, line, column))
	if reference.Database == "" {
		if databases := mods.ListDatabases(reference.Name); len(databases) > 1 {
			slices.Sort(databases)
			return nil, fmt.Errorf("%s is written with no database, and more than one database defines it: %s",
				reference.Name, strings.Join(databases, ", "))
		}
	}
	edits := map[string][]Edit{}
	for _, site := range locateSites(ws, mods, reference, listOccurrences(mods, place, reference), true) {
		if !site.Place.ReadOnly && site.Range != (jomini.Range{}) {
			edits[site.Place.Path] = append(edits[site.Place.Path], Edit{Range: site.Range, Text: name})
		}
	}
	return edits, nil
}

// ListSymbols returns a file's definitions, nested as they are written, or a
// localization file's keys.
func (lsp *Service) ListSymbols(path string) ([]Symbol, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	return ListFileSymbols(ws, ws.Mods.Load(), path)
}

// ListFileSymbols returns the definitions of a file in the given session,
// nested as they are written, or the keys of a localization file, each with its
// title in the session's language. It returns none for a file that is neither,
// and an error if the file cannot be read.
func ListFileSymbols(ws *workspace.Session, mods *index.Index, path string) ([]Symbol, error) {
	document, err := ws.WalkDocument(path)
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
	return listSymbols(ws, mods, document), nil
}

// listSymbols returns the definitions of a walked document, nested as they
// are written, each with the title its own statement gives it. A constant is
// left out.
func listSymbols(ws *workspace.Session, mods *index.Index, document schema.Document) []Symbol {
	tree, symbols := document.Tree, []Symbol{}
	// path holds the position of each symbol around the statement among its
	// siblings, the outermost first, and ends the statement that follows each
	// one's last
	var path []int
	var ends []int32

	for position := range document.Contexts {
		statement, context := int32(position), &document.Contexts[position]
		for len(ends) > 0 && ends[len(ends)-1] <= statement {
			path, ends = path[:len(path)-1], ends[:len(ends)-1]
		}

		if !context.Defines || context.Definition.Database == game.Constant {
			continue
		}

		reference := schema.Reference{
			Database: context.Definition.Database, Name: context.Definition.Name, Span: context.Definition.Span,
		}
		title := ws.LookupStatementText(mods, tree, statement, reference, game.TitleKey)

		siblings := &symbols
		for _, position := range path {
			siblings = &(*siblings)[position].Children
		}

		*siblings = append(*siblings, makeSymbol(tree, reference, tree.Statements[statement].Span, title))
		path, ends = append(path, len(*siblings)-1), append(ends, tree.Statements[statement].Last)
	}

	return symbols
}

// makeSymbol returns the symbol for a name the tree defines, with the given
// title. The reference holds the name and the span it is written at. The given
// span covers the whole definition, a statement or a message.
func makeSymbol(tree *jomini.Tree, reference schema.Reference, span jomini.Span, title string) Symbol {
	return Symbol{
		Name: reference.Name, Title: title, Database: reference.Database,
		What:      game.DescribeDefinition(reference.Database, false),
		Range:     jomini.ToRange(tree.Src, tree.Lines, span),
		NameRange: jomini.ToRange(tree.Src, tree.Lines, reference.Span),
	}
}

// FindSymbols returns up to the given limit of names that contain the query:
// the definitions, or the localization keys when the second argument is true.
// The best match comes first: the name that is the query, then the names that
// start with it, then the rest. Within each of these the mods' files come
// before the game's. It returns an empty list for an empty query.
func (lsp *Service) FindSymbols(query string, localization bool, limit int) ([]WorkspaceSymbol, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	query = strings.ToLower(query)
	if query == "" || limit <= 0 {
		return []WorkspaceSymbol{}, nil
	}
	role := index.RoleDefines
	if localization {
		role = index.RoleLocalizes
	}
	sites := mods.ListNamed(role, func(name string) int {
		lower := strings.ToLower(name)
		if !strings.Contains(lower, query) {
			return -1
		}
		return files.ClassifyMatch(lower, query)
	})
	// a key is listed once, not once for each language
	symbols, listed := []WorkspaceSymbol{}, map[string]bool{}
	for _, site := range sites {
		reference := mods.ToReference(site)
		if len(symbols) == limit {
			break
		}
		if localization && listed[reference.Name] {
			continue
		}
		listed[reference.Name] = true
		symbols = append(symbols, WorkspaceSymbol{Name: reference.Name, Database: reference.Database, Site: LocateSite(ws, mods, site)})
	}
	return symbols, nil
}
