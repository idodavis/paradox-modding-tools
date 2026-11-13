// navigate.go finds where a name is defined and used, renames it, and lists
// symbols.

package lsp

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// Symbol is one entry of a file: a definition with its nested symbols, or a
// localization key.
type Symbol struct {
	Name string `json:"name"`

	// Title is the text the game shows for it, or "".
	Title string `json:"title"`

	Database string `json:"database"`

	// What is the word for a definition of the symbol's database, such as
	// "window".
	What string `json:"what"`

	// Range is the whole statement, and NameRange the name alone.
	Range     jomini.Range `json:"range"`
	NameRange jomini.Range `json:"nameRange"`

	Children []Symbol `json:"children,omitempty"`
}

// WorkspaceSymbol is a definition the workspace search found.
type WorkspaceSymbol struct {
	Name     string `json:"name"`
	Database string `json:"database"`
	Site     Site   `json:"site"`
}

// Definition returns where the name at a position is defined, the game's
// definition first, or the image an image path names. It returns an empty list
// for no name.
func (s *Service) Definition(path string, line, column int) ([]Site, error) {
	ws, document, err := s.loadDocument(path)
	if err != nil {
		return nil, err
	}

	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	_, reference := findReference(ws, mods, document, place, jomini.ToOffset(document.Tree.Src, document.Tree.Lines, line, column))
	if reference.Database == lang.File {
		target, ok := mods.ResolveTexture(reference.Name)
		if !ok {
			return []Site{}, nil
		}

		return []Site{{Place: ws.Locate(mods, target), Line: 1, Range: jomini.ToLineRange(1)}}, nil
	}

	if reference.Name == "" {
		return []Site{}, nil
	}

	// A dump line defines an effect, a trigger or a link.
	declared := ws.Schema.LookupDefinition(reference.Database, reference.Name)
	if declared != nil && declared.Path != "" {
		return []Site{{Place: ws.Locate(mods, declared.Path), Line: declared.Line, Range: jomini.ToLineRange(declared.Line)}}, nil
	}

	sites, _ := listSites(ws, mods, place, reference)

	return locateSites(ws, mods, reference, sites, false), nil
}

// findReference returns the reference at the offset, nameless if there is none,
// and where it is written.
func findReference(
	ws *workspace.Session, mods *index.Index, document lang.Document, place workspace.Place, offset int,
) (jomini.Span, lang.Reference) {
	if place.Format == files.FormatLocalization {
		_, reference := describeMessage(ws, document, offset)

		return reference.Span, reference
	}

	cursor := findCursor(document, offset)
	span, reference := findHit(cursor)

	return span, resolveHit(mods, place, reference, cursor.context.References)
}

// resolveHit returns the first reference at the given one's span whose database
// defines the name, or the given one. An any call tries each callable database.
func resolveHit(mods *index.Index, place workspace.Place, reference lang.Reference, references []lang.Reference) lang.Reference {
	candidates := slices.DeleteFunc(slices.Clone(references), func(other lang.Reference) bool {
		return other.Span != reference.Span
	})
	if reference.Database == lang.AnyCall {
		candidates = index.ListCallees(reference)
	}

	for _, candidate := range candidates {
		if len(mods.ResolveLocal(candidate, place.Position)) > 0 {
			return candidate
		}
	}

	if reference.Database == lang.AnyCall {
		reference.Database = ""
	}

	return reference
}

// locateSites returns the sites with their range and line text, taking an open
// file's from the editor's text, since the index holds the saved file's.
func locateSites(ws *workspace.Session, mods *index.Index, reference lang.Reference, sites []index.Site, uses bool) []Site {
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

// listBufferSites returns where the editor's text writes a reference's name. A
// call to any callable database uses a callable name. It returns none if the
// file cannot be read.
func listBufferSites(ws *workspace.Session, mods *index.Index, path string, reference lang.Reference, uses bool) []Site {
	document, err := ws.WalkDocument(path)
	if err != nil {
		return nil
	}

	name := mods.ToCaseless(reference.Database, reference.Name)
	callable := slices.Contains(lang.Called, reference.Database)
	matches := func(other lang.Reference) bool {
		return (other.Database == reference.Database || callable && other.Database == lang.AnyCall) &&
			mods.ToCaseless(reference.Database, other.Name) == name
	}

	var spans []jomini.Span
	for _, contexts := range [][]lang.Context{document.Contexts, document.MessageContexts} {
		for position := range contexts {
			context := &contexts[position]
			if definition := context.Definition; context.Defines && matches(lang.Reference{Database: definition.Database, Name: definition.Name}) {
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
	located := make([]Site, len(spans))
	for position, span := range spans {
		line := tree.Lines.ToPosition(span.Start).Line
		row := tree.Lines.LineSpan(line)
		located[position] = Site{Place: place, Line: line + 1, Range: jomini.ToRange(tree.Src, tree.Lines, span), Text: tree.Src[row.Start:row.End]}
	}

	return located
}

// References returns where the name at a position is defined and used, minus a
// key's translations. It returns an empty list for no name.
func (s *Service) References(path string, line, column int) ([]Site, error) {
	ws, document, err := s.loadDocument(path)
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

func listOccurrences(mods *index.Index, place workspace.Place, reference lang.Reference) []index.Site {
	return slices.Concat(mods.ResolveLocal(reference, place.Position), mods.ListUses(reference.Database, reference.Name))
}

// PrepareRename returns the range of the name at a position. It returns nil for
// no name, if nothing defines it, or if a file that cannot be changed does too.
func (s *Service) PrepareRename(path string, line, column int) (*jomini.Range, error) {
	ws, document, err := s.loadDocument(path)
	if err != nil {
		return nil, err
	}

	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	span, reference := findReference(ws, mods, document, place, jomini.ToOffset(document.Tree.Src, document.Tree.Lines, line, column))
	if reference.Name == "" {
		return nil, nil
	}

	// A read-only file's definition stays, so no rename.
	sites := mods.ResolveLocal(reference, place.Position)
	if len(sites) == 0 || slices.ContainsFunc(sites, func(site index.Site) bool { return ws.Locate(mods, mods.File(site).Path).ReadOnly }) {
		return nil, nil
	}

	renamable := jomini.ToRange(document.Tree.Src, document.Tree.Lines, span)

	return &renamable, nil
}

// Rename returns the edits that rename the name at a position, by file, none
// for a file that cannot be changed. It returns an error for an empty new name,
// or for a name with no database that several databases define.
func (s *Service) Rename(path string, line, column int, name string) (map[string][]Edit, error) {
	if name == "" {
		return nil, errors.New("a name cannot be empty")
	}

	ws, document, err := s.loadDocument(path)
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

// ListSymbols returns a file's definitions, nested, or a localization file's
// keys.
func (s *Service) ListSymbols(path string) ([]Symbol, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	return ListFileSymbols(ws, ws.Mods.Load(), path)
}

// ListFileSymbols is ListSymbols for a session's file. It returns none for
// other files, and an error if the file cannot be read.
func ListFileSymbols(ws *workspace.Session, mods *index.Index, path string) ([]Symbol, error) {
	document, err := ws.WalkDocument(path)
	if err != nil {
		return nil, err
	}

	place := ws.Locate(mods, path)
	if place.Format == files.FormatLocalization {
		symbols := make([]Symbol, len(document.Messages.Messages))
		for position, message := range document.Messages.Messages {
			text, _, _ := ws.LookupText(mods, message.Key)
			reference := lang.Reference{Database: lang.Localization, Name: message.Key, Span: message.KeySpan}
			symbols[position] = makeSymbol(document.Tree, reference, message.Span, text)
		}

		return symbols, nil
	}

	return listSymbols(ws, mods, document), nil
}

// listSymbols returns the definitions of a walked document, nested, each titled
// by its own statement. A constant is left out.
func listSymbols(ws *workspace.Session, mods *index.Index, document lang.Document) []Symbol {
	tree, symbols := document.Tree, []Symbol{}
	// path and ends track the symbols still open.
	var path []int
	var ends []int32

	for position := range document.Contexts {
		statement, context := int32(position), &document.Contexts[position]
		for len(ends) > 0 && ends[len(ends)-1] <= statement {
			path, ends = path[:len(path)-1], ends[:len(ends)-1]
		}

		if !context.Defines || context.Definition.Database == lang.Constant {
			continue
		}

		reference := lang.Reference{
			Database: context.Definition.Database, Name: context.Definition.Name, Span: context.Definition.Span,
		}
		title := ws.LookupStatementText(mods, tree, statement, reference, lang.TitleKey)

		siblings := &symbols
		for _, position := range path {
			siblings = &(*siblings)[position].Children
		}

		*siblings = append(*siblings, makeSymbol(tree, reference, tree.Statements[statement].Span, title))
		path, ends = append(path, len(*siblings)-1), append(ends, tree.Statements[statement].Last)
	}

	return symbols
}

// makeSymbol returns the symbol of a defined name, over its whole definition.
func makeSymbol(tree *jomini.Tree, reference lang.Reference, span jomini.Span, title string) Symbol {
	return Symbol{
		Name: reference.Name, Title: title, Database: reference.Database,
		What:      lang.DescribeDefinition(reference.Database, false),
		Range:     jomini.ToRange(tree.Src, tree.Lines, span),
		NameRange: jomini.ToRange(tree.Src, tree.Lines, reference.Span),
	}
}

// FindSymbols returns up to the given number of definitions, or localization
// keys if the second argument is true, whose names contain the query: the query
// itself first, then names starting with it, then the rest, the mods' files
// before the game's. It returns an empty list for no query.
func (s *Service) FindSymbols(query string, localization bool, limit int) ([]WorkspaceSymbol, error) {
	ws, err := s.Workspace.Session()
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

	// A key is listed once, not once per language.
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
