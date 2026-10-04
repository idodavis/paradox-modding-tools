// definition.go is navigation: where the word under the cursor is defined, where a name is used,
// and the definitions a file or the workspace holds. Every answer is the index's records as
// locations, placed by the lines they keep; nothing is parsed but the open file.
package lsp

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
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
	case file.place.Format == source.Loc:
		ref, _ := file.locRef(offset)
		return ref
	case file.place.Format == source.Script && file.place.Rel != "":
		_, _, ref := file.walk(offset).word(file.sess)
		return ref
	}
	return nil
}

// inFile is a name's definitions; for a name the walk makes (saved, a parameter), none replaces
// another, so only those in this file where there are any.
func (file *file) inFile(defs []index.Def) []index.Def {
	if len(defs) == 0 || file.sess.Install.Schema.Databases[defs[0].Database] != nil {
		return defs
	}
	here, _, _ := file.sess.Located(file.path)
	if local := slices.DeleteFunc(slices.Clone(defs), func(def index.Def) bool { return def.Path != here.Path }); len(local) > 0 {
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
	case index.Localization:
		for _, entry := range index.LookupLocs(indexes, ref.Name) {
			out = append(out, locate(sess, entry.Path, Position{Line: entry.Line}))
		}
	case index.File:
		if found, listed := index.FileAt(indexes, ref.Name); listed {
			out = []Location{locate(sess, found.Path, Position{})}
		}
	default:
		defs, _ := index.Resolve(sess.Install.Schema, indexes, ref)
		if winner, _, _, _ := sess.Winner(defs); slices.Contains(defs, winner) && sess.Install.Schema.Databases[winner.Database] != nil {
			defs = slices.Insert(slices.DeleteFunc(slices.Clone(defs), func(def index.Def) bool { return def == winner }), 0, winner)
		}
		for _, def := range file.inFile(defs) {
			out = append(out, locate(sess, def.Path, Position{Line: int(def.Line), Character: int(def.Col)}))
		}
	}
	return out
}

// Definition is every site defining the word at a position, in load order, the kept one first.
func Definition(sess *session.Session, path string, line, col int) ([]Location, error) {
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
func References(sess *session.Session, path string, line, col int) ([]Location, error) {
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
	sch, indexes := sess.Install.Schema, sess.Indexes()

	// the databases the name is in: its own, or those a call of its kind resolves in
	databases := []string{ref.Database}
	if ref.Called != "" {
		databases = sch.Callees[ref.Called]
	}
	seen, out, calls := map[Location]bool{}, []Location(nil), index.LookupRefs(indexes, "", ref.Name)
	add := func(site Location) {
		if !seen[site] {
			seen[site], out = true, append(out, site)
		}
	}
	for _, database := range databases {
		refs := index.LookupRefs(indexes, database, ref.Name)
		for _, call := range calls {
			if slices.Contains(sch.Callees[call.Called], database) {
				refs = append(refs, call)
			}
		}
		for _, found := range refs {
			add(locate(sess, found.Path, Position{Line: int(found.Line), Character: int(found.Col)}))
		}
	}
	for _, site := range file.sites(*ref) {
		add(site)
	}
	return out, nil
}

// DocumentSymbols is the definitions a file holds, in the file's order.
func DocumentSymbols(sess *session.Session, path string) ([]Symbol, error) {
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
			if def.Name != "" && def.Database != index.Parameter {
				out = append(out, Symbol{Name: def.Name, Database: def.Database,
					Location: locate(sess, def.Path, Position{Line: int(def.Line), Character: int(def.Col)})})
			}
		}
	}
	return out, nil
}

// WorkspaceSymbols is the mod definitions whose names hold a non-empty query, ignoring case, that
// the game loads.
func WorkspaceSymbols(sess *session.Session, query string) []Symbol {
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
			if strings.Contains(strings.ToLower(def.Name), query) && slices.Contains(index.Lookup(sess.Indexes(), def.Database, def.Name), def) {
				out = append(out, Symbol{Name: def.Name, Database: def.Database,
					Location: locate(sess, def.Path, Position{Line: int(def.Line), Character: int(def.Col)})})
			}
		}
	}
	return out
}
