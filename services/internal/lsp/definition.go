// definition.go is navigation: where the word under the cursor is defined, where a name is used,
// and the definitions a file or the workspace holds. Every answer is the index's records as
// locations; nothing is parsed but the open file.
package lsp

import (
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
)

// Symbol is one definition, for an outline or a workspace search.
type Symbol struct {
	Name     string   `json:"name"`
	Database string   `json:"database"`
	Parent   string   `json:"parent,omitempty"`
	Location Location `json:"location"`
}

// Definition is every site defining the word at a position, in load order, the kept one first.
func Definition(sess *session.Session, path string, line, col int) ([]Location, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	var card *Inspect
	if file.place.Format == source.Loc {
		card = file.locCard(file.offset(line, col))
	} else if site := file.at(file.offset(line, col)); site != nil {
		card = file.card(site)
	}
	if card == nil || len(card.Sites) == 0 {
		return nil, nil
	}
	if card.Winner > 0 {
		sites := append([]Location{card.Sites[card.Winner]}, card.Sites[:card.Winner]...)
		return append(sites, card.Sites[card.Winner+1:]...), nil
	}
	return card.Sites, nil
}

// References is every place naming what the word at a position names, definitions included, once.
func References(sess *session.Session, path string, line, col int) ([]Location, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	var target schema.Word
	if file.place.Format == source.Loc {
		card := file.locCard(file.offset(line, col))
		if card == nil {
			return nil, nil
		}
		target = card.Word
	} else {
		word := file.at(file.offset(line, col))
		if word == nil {
			return nil, nil
		}
		target = word.Value
	}
	if target.Type == schema.WordParameter && file.place.Format == source.Loc {
		target.Type = schema.WordLoc
	}

	seen := map[Location]bool{}
	var out []Location
	add := func(loc Location) {
		if !seen[loc] {
			seen[loc], out = true, append(out, loc)
		}
	}
	all := sess.All()
	for _, idx := range all {
		for _, name := range idx.Referring(target.Name) {
			if !all.Loaded(idx, name.File) {
				continue
			}
			if name.Type == target.Type && (target.Type != schema.WordReference || (name.Database == target.Database && (target.Parent == "" || name.Parent == target.Parent))) {
				add(locate(name.At, int(name.Line), int(name.Col)))
			}
		}
	}
	switch target.Type {
	case schema.WordReference, schema.WordConstant, schema.WordParameter:
		for _, name := range sess.Defs(target.Name) {
			if target.Type != schema.WordReference || (name.Database == target.Database && (target.Parent == "" || name.Parent == target.Parent)) {
				add(locate(name.At, int(name.Line), int(name.Col)))
			}
		}
	case schema.WordLoc:
		for _, said := range all.Entries(target.Name) {
			add(locate(said.At, said.Line, 0))
		}
	}
	return out, nil
}

// DocumentSymbols is the definitions a file holds, in the file's order.
func DocumentSymbols(sess *session.Session, path string) ([]Symbol, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	found, _, ok := sess.Located(path)
	if !ok {
		return nil, nil
	}
	for _, idx := range sess.All() {
		if at, ok := idx.At(path); ok {
			return symbols(idx, at), nil
		}
	}
	// a file nothing has walked: its own harvest, kept nowhere
	var out []Symbol
	for _, reader := range index.Harvest(found, file.text, schema.Reader{Schema: sess.Schema}).Defs {
		out = append(out, Symbol{Name: reader.Name, Database: reader.Database, Parent: reader.Parent, Location: locate(found, int(reader.Line), int(reader.Col))})
	}
	return out, nil
}

// symbols is the definitions an index holds for one of its files.
func symbols(idx *index.Index, at source.File) []Symbol {
	var out []Symbol
	for _, def := range idx.Defs {
		if def.File >= 0 && index.PathKey(idx.Files[def.File].Path) == index.PathKey(at.Path) {
			out = append(out, Symbol{Name: def.Name, Database: def.Database, Parent: def.Parent, Location: locate(idx.Files[def.File], int(def.Line), int(def.Col))})
		}
	}
	return out
}

// WorkspaceSymbols is the mod definitions whose names hold a non-empty query, ignoring case.
func WorkspaceSymbols(sess *session.Session, query string) []Symbol {
	sess.RLock()
	defer sess.RUnlock()
	query = strings.ToLower(query)
	if query == "" {
		return nil
	}
	var out []Symbol
	idx := sess.Workspace
	for _, def := range idx.Defs {
		if strings.Contains(strings.ToLower(def.Name), query) && sess.All().Loaded(idx, def.File) {
			out = append(out, Symbol{Name: def.Name, Database: def.Database, Parent: def.Parent, Location: locate(idx.Files[def.File], int(def.Line), int(def.Col))})
		}
	}
	return out
}
