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
func (file *file) target(offset int) *index.Ref {
	switch {
	case file.place.Format == inventory.Loc:
		ref, _ := file.locRef(offset)
		return ref
	case file.place.Format == inventory.Script && file.place.Rel != "":
		_, _, ref := file.walk(offset).word(file.sess)
		return ref
	}
	return nil
}

// inFile narrows a name no schema database holds (a saved scope, a parameter) to this file's
// definitions, where it has any: none of them replaces another.
func (file *file) inFile(defs []index.Def) []index.Def {
	if len(defs) == 0 || file.sess.Install.Schema.Databases[defs[0].Database] != nil {
		return defs
	}
	if local := slices.DeleteFunc(slices.Clone(defs), func(def index.Def) bool { return def.Path != file.place.Path }); len(local) > 0 {
		return local
	}
	return defs
}

// sites is where a reference's name is defined: a loc key's entries, a file's place, or its
// definitions in load order (inFile) with the one the game keeps first.
func (file *file) sites(ref index.Ref) []Location {
	sess, indexes := file.sess, file.sess.Indexes()
	var out []Location
	switch ref.Database {
	case schema.Localization:
		for _, entry := range index.LookupLocs(indexes, ref.Name) {
			out = append(out, locate(sess, entry.Path, Position{Line: entry.Line}))
		}
	case schema.File:
		if found, listed := index.FileAt(indexes, ref.Name); listed {
			out = []Location{locate(sess, found.Path, Position{})}
		}
	default:
		defs, _ := index.Resolve(sess.Install.Schema, indexes, ref)
		if winner, _, _, _ := sess.Winner(defs); slices.Contains(defs, winner) && sess.Install.Schema.Databases[winner.Database] != nil {
			defs = slices.Insert(slices.DeleteFunc(slices.Clone(defs), func(def index.Def) bool { return def == winner }), 0, winner)
		}
		for _, def := range file.inFile(defs) {
			out = append(out, defined(sess, def))
		}
	}
	return out
}

// Definition is every site defining the word at a position, in load order, the kept one first.
func Definition(sess *workspace.Session, path string, line, col int) ([]Location, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
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
func References(sess *workspace.Session, path string, line, col int) ([]Location, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
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
func databases(sess *workspace.Session, ref index.Ref) []string {
	if ref.Called != "" {
		return sess.Install.Schema.Callees[ref.Called]
	}
	return []string{ref.Database}
}

// uses are a reference's name everywhere it is written: its uses, and where it is defined.
func (file *file) uses(ref index.Ref) []Location {
	sess := file.sess
	seen, out := map[Location]bool{}, []Location(nil)
	add := func(site Location) {
		if !seen[site] {
			seen[site], out = true, append(out, site)
		}
	}
	for _, database := range databases(sess, ref) {
		for _, found := range index.RefsTo(sess.Install.Schema, sess.Indexes(), database, ref.Name) {
			add(locate(sess, found.Path, Position{Line: int(found.Line), Character: int(found.Col)}))
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
func Rename(sess *workspace.Session, path string, line, col int, name string) (WorkspaceEdit, error) {
	sess.RLock()
	defer sess.RUnlock()
	edit := WorkspaceEdit{Changes: map[string][]TextEdit{}}
	file, err := open(sess, path)
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
	for _, database := range databases(sess, *ref) {
		if len(index.Lookup(sess.Indexes(), database, name)) > 0 {
			return edit, fmt.Errorf("%s is taken", name)
		}
		for _, def := range index.Lookup(sess.Indexes(), database, ref.Name) {
			listed, _, _ := sess.Located(def.Path)
			origins[listed.Origin] = true
		}
	}
	switch {
	case len(origins) == 0:
		return edit, fmt.Errorf("nothing defines %s", ref.Name)
	case origins[game.OriginVanilla]:
		return edit, fmt.Errorf("the game defines %s, and its files never change", ref.Name)
	case len(origins) > 1:
		return edit, fmt.Errorf("more than one mod defines %s", ref.Name)
	}
	texts := map[string][]string{}
	for _, site := range file.uses(*ref) {
		if site.Origin == game.OriginVanilla {
			return edit, fmt.Errorf("the game's %s uses %s, and its files never change", site.Rel, ref.Name)
		}
		if texts[site.Path] == nil {
			text, err := sess.Text(site.Path)
			if err != nil {
				return edit, err
			}
			texts[site.Path] = strings.Split(text, "\n")
		}
		at, lines := site.Range.Start, texts[site.Path]
		if at.Line >= len(lines) || !strings.HasPrefix(lines[at.Line][min(at.Character, len(lines[at.Line])):], ref.Name) {
			return edit, fmt.Errorf("%s:%d does not read %s where it is used; rename it by hand", site.Rel, at.Line+1, ref.Name)
		}
		end := Position{Line: at.Line, Character: at.Character + len(ref.Name)}
		edit.Changes[site.Path] = append(edit.Changes[site.Path], TextEdit{Range: Range{Start: at, End: end}, NewText: name})
	}
	return edit, nil
}

// DocumentSymbols is the definitions a file holds, in the file's order.
func DocumentSymbols(sess *workspace.Session, path string) ([]Symbol, error) {
	sess.RLock()
	defer sess.RUnlock()
	listed, _, _ := sess.Located(path)
	var out []Symbol
	for _, idx := range sess.Indexes() {
		records := idx.Records[listed.Path]
		if records == nil {
			continue
		}
		for _, def := range slices.SortedFunc(slices.Values(records.Defs), func(first, second index.Def) int {
			return cmp.Or(cmp.Compare(first.Line, second.Line), cmp.Compare(first.Col, second.Col))
		}) {
			if def.Name != "" && def.Database != schema.Parameter {
				out = append(out, Symbol{Name: def.Name, Database: def.Database,
					Location: defined(sess, def)})
			}
		}
	}
	return out, nil
}

// WorkspaceSymbols is the mod definitions whose names hold a non-empty query, ignoring case, that
// the game loads.
func WorkspaceSymbols(sess *workspace.Session, query string) []Symbol {
	sess.RLock()
	defer sess.RUnlock()
	query = strings.ToLower(query)
	if query == "" {
		return nil
	}
	var out []Symbol
	for _, listed := range sess.Workspace.Files {
		records := sess.Workspace.Records[listed.Path]
		if records == nil {
			continue
		}
		for _, def := range records.Defs {
			if def.Database != schema.Parameter && strings.Contains(strings.ToLower(def.Name), query) && slices.Contains(index.Lookup(sess.Indexes(), def.Database, def.Name), def) {
				out = append(out, Symbol{Name: def.Name, Database: def.Database,
					Location: defined(sess, def)})
			}
		}
	}
	return out
}

// FindLoc is the localization keys that start with a prefix, at most limit, each where its entry in the workspace's
// language is (the english one where it has none), for the palette's #.
func FindLoc(sess *workspace.Session, prefix string, limit int) []Symbol {
	sess.RLock()
	defer sess.RUnlock()
	var out []Symbol
	for _, key := range sess.LocKeys(strings.ToLower(prefix), limit) {
		if entry, found := sess.Loc(key, sess.Language); found {
			out = append(out, Symbol{Name: key, Database: "localization", Location: locate(sess, entry.Path, Position{Line: entry.Line})})
		}
	}
	return out
}
