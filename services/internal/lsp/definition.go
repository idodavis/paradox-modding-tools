// definition.go is navigation: where the word under the cursor is defined, where a name is used,
// and the definitions a file or the workspace holds. Every answer is the index's records as
// locations, placed by the lines they keep; nothing is parsed but the open file.

package lsp

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// Symbol is one definition, for an outline or a workspace search.
type Symbol struct {
	Name     string   `json:"name"`
	Database string   `json:"database"`
	Location Location `json:"location"`
}

// target is the reference or definition at an offset: the one the walk read in a script word, or
// what a localization file's text or line key names; nil for none.
func (file *openFile) target(offset int) *schema.Occurrence {
	switch {
	case file.place.Format == inventory.Loc:
		ref, _ := file.locRef(offset)
		return ref
	case file.place.Format == inventory.Script && file.place.Rel != "":
		_, _, ref := file.walkTo(offset).scalarAt()
		return ref
	}
	return nil
}

// inFile narrows a name no schema database holds (a saved scope, a parameter) to this file's
// definitions, where it has any: none of them replaces another.
func (file *openFile) inFile(defs []index.At) []index.At {
	if len(defs) == 0 || file.ws.Install.Schema.Databases[defs[0].Occurrence().Database] != nil {
		return defs
	}
	if local := slices.DeleteFunc(slices.Clone(defs), func(def index.At) bool { return def.File().Path != file.place.Path }); len(local) > 0 {
		return local
	}
	return defs
}

// sites is where a reference's name is defined: a loc key's entries, a file's place, or its
// definitions in load order (inFile) with the one the game keeps first.
func (file *openFile) sites(ref schema.Occurrence) []Location {
	ws, indexes := file.ws, file.ws.Indexes()
	var out []Location
	switch ref.Database {
	case schema.Localization:
		for _, entry := range index.Lookup(indexes, schema.Localizes, schema.Localization, ref.ID) {
			out = append(out, locate(ws, entry.File().Path, Position{Line: int(entry.Occurrence().Line)}))
		}
	case schema.File:
		if found, listed := index.FileAt(indexes, ref.ID); listed {
			out = []Location{locate(ws, found.Path, Position{})}
		}
	default:
		defs, _ := index.Resolve(ws.Install.Schema, indexes, ref, file.place.Path)
		if winner, _, _, _ := ws.Winner(defs); slices.Contains(defs, winner) && ws.Install.Schema.Databases[winner.Occurrence().Database] != nil {
			defs = slices.Insert(slices.DeleteFunc(slices.Clone(defs), func(def index.At) bool { return def == winner }), 0, winner)
		}
		for _, def := range file.inFile(defs) {
			out = append(out, siteOf(ws, def))
		}
	}
	return out
}

// Definition is every site defining the word at a position, in load order, the kept one first.
func Definition(ws *workspace.Session, path string, line, col int) ([]Location, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	ref := file.target(file.parsed.Lines().OffsetAt(line, col))
	if ref == nil {
		return nil, nil
	}
	return file.sites(*ref), nil
}

// References is every place naming what the word at a position names, once: its references, the
// calls whose CalledIn word resolves in its database, and its definitions.
func References(ws *workspace.Session, path string, line, col int) ([]Location, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	ref := file.target(file.parsed.Lines().OffsetAt(line, col))
	if ref == nil {
		return nil, nil
	}
	return file.uses(*ref), nil
}

// databases are the databases a reference's name is in: its own, or those a call of its kind resolves in.
func databases(ws *workspace.Session, ref schema.Occurrence) []string {
	if ref.Caller != "" {
		return ws.Install.Schema.Callees[ref.Caller]
	}
	return []string{ref.Database}
}

// uses are a reference's name everywhere it is written: its uses, and where it is defined.
func (file *openFile) uses(ref schema.Occurrence) []Location {
	ws := file.ws
	seen, out := map[Location]bool{}, []Location(nil)
	add := func(site Location) {
		if !seen[site] {
			seen[site], out = true, append(out, site)
		}
	}
	for _, database := range databases(ws, ref) {
		for _, found := range index.RefsTo(ws.Install.Schema, ws.Indexes(), database, ref.ID) {
			add(locate(ws, found.File().Path, Position{Line: int(found.Occurrence().Line), Character: int(found.Occurrence().Col)}))
		}
	}
	for _, site := range file.sites(ref) {
		add(site)
	}
	return out
}

// Rename is the edits that rename the name at a place everywhere it is written, in the mods' files. It refuses, with
// the reason, a name the game defines, one more than one origin defines, a site in the game's files or one that
// does not read as the name there, and a new name that is no name or is taken.
func Rename(ws *workspace.Session, path string, line, col int, name string) (WorkspaceEdit, error) {
	ws.RLock()
	defer ws.RUnlock()
	edit := WorkspaceEdit{Changes: map[string][]TextEdit{}}
	file, err := open(ws, path)
	if err != nil {
		return edit, err
	}
	ref := file.target(file.parsed.Lines().OffsetAt(line, col))
	if ref == nil {
		return edit, fmt.Errorf("nothing here to rename")
	}
	if !jomini.IsDefName(name) {
		return edit, fmt.Errorf("%q is not a name the game reads", name)
	}
	origins := map[string]bool{}
	for _, database := range databases(ws, *ref) {
		if len(index.Lookup(ws.Indexes(), schema.Defines, database, name)) > 0 {
			return edit, fmt.Errorf("%s is taken", name)
		}
		for _, def := range index.Lookup(ws.Indexes(), schema.Defines, database, ref.ID) {
			origins[def.File().Origin] = true
		}
	}
	switch {
	case len(origins) == 0:
		return edit, fmt.Errorf("nothing defines %s", ref.ID)
	case origins[game.OriginVanilla]:
		return edit, fmt.Errorf("the game defines %s, and its files never change", ref.ID)
	case len(origins) > 1:
		return edit, fmt.Errorf("more than one mod defines %s", ref.ID)
	}
	texts := map[string][]string{}
	for _, site := range file.uses(*ref) {
		if site.Origin == game.OriginVanilla {
			return edit, fmt.Errorf("the game's %s uses %s, and its files never change", site.Rel, ref.ID)
		}
		if texts[site.Path] == nil {
			text, err := ws.Text(site.Path)
			if err != nil {
				return edit, err
			}
			texts[site.Path] = strings.Split(text, "\n")
		}
		at, lines := site.Range.Start, texts[site.Path]
		if at.Line >= len(lines) || !strings.HasPrefix(lines[at.Line][min(at.Character, len(lines[at.Line])):], ref.ID) {
			return edit, fmt.Errorf("%s:%d does not read %s where it is used; rename it by hand", site.Rel, at.Line+1, ref.ID)
		}
		end := Position{Line: at.Line, Character: at.Character + len(ref.ID)}
		edit.Changes[site.Path] = append(edit.Changes[site.Path], TextEdit{Range: Range{Start: at, End: end}, NewText: name})
	}
	return edit, nil
}

// DocumentSymbols is the definitions a file holds, in the file's order.
func DocumentSymbols(ws *workspace.Session, path string) ([]Symbol, error) {
	ws.RLock()
	defer ws.RUnlock()
	listed, _, _ := ws.Located(path)
	var out []Symbol
	for _, idx := range ws.Indexes() {
		for _, def := range slices.SortedFunc(slices.Values(idx.InFile(listed.Path)), func(first, second index.At) int {
			return cmp.Or(cmp.Compare(first.Occurrence().Line, second.Occurrence().Line), cmp.Compare(first.Occurrence().Col, second.Occurrence().Col))
		}) {
			if found := def.Occurrence(); found.Role == schema.Defines && found.ID != "" && found.Database != schema.Parameter {
				out = append(out, Symbol{Name: found.ID, Database: found.Database, Location: siteOf(ws, def)})
			}
		}
	}
	return out, nil
}

// WorkspaceSymbols is the mod definitions whose names hold a non-empty query, ignoring case, that
// the game loads.
func WorkspaceSymbols(ws *workspace.Session, query string) []Symbol {
	ws.RLock()
	defer ws.RUnlock()
	query = strings.ToLower(query)
	if query == "" {
		return nil
	}
	var out []Symbol
	for _, listed := range ws.Workspace.Files {
		for _, def := range ws.Workspace.InFile(listed.Path) {
			found := def.Occurrence()
			if found.Role == schema.Defines && found.Database != schema.Parameter && strings.Contains(strings.ToLower(found.ID), query) &&
				slices.Contains(index.Lookup(ws.Indexes(), schema.Defines, found.Database, found.ID), def) {
				out = append(out, Symbol{Name: found.ID, Database: found.Database, Location: siteOf(ws, def)})
			}
		}
	}
	return out
}

// FindLoc is the localization keys that start with a prefix, at most limit, each where its entry in the workspace's
// language is (the english one where it has none), for the palette's #.
func FindLoc(ws *workspace.Session, prefix string, limit int) []Symbol {
	ws.RLock()
	defer ws.RUnlock()
	var out []Symbol
	for _, key := range ws.LocKeys(strings.ToLower(prefix), limit) {
		if entry, found := ws.Loc(key, ws.Language); found {
			out = append(out, Symbol{Name: key, Database: "localization", Location: locate(ws, entry.File().Path, Position{Line: int(entry.Occurrence().Line)})})
		}
	}
	return out
}
