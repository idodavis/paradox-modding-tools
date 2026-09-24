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
	Kind     string   `json:"kind"`
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
	var value schema.Value
	if file.place.Format == source.Loc {
		card := file.locCard(file.offset(line, col))
		if card == nil {
			return nil, nil
		}
		value = card.Value
	} else {
		word := file.at(file.offset(line, col))
		if word == nil {
			return nil, nil
		}
		value = word.Value
	}
	if value.Type == schema.TypeParam && file.place.Format == source.Loc {
		value.Type = schema.TypeLoc
	}

	seen := map[Location]bool{}
	var out []Location
	add := func(loc Location) {
		if !seen[loc] {
			seen[loc], out = true, append(out, loc)
		}
	}
	for _, idx := range sess.All() {
		for _, name := range idx.Referring(value.Name) {
			if name.Type == value.Type && (value.Type != schema.TypeRef || (name.Kind == value.Kind && (value.Parent == "" || name.Parent == value.Parent))) {
				add(locate(name.At, int(name.Line), int(name.Col)))
			}
		}
	}
	switch value.Type {
	case schema.TypeRef, schema.TypeConstant, schema.TypeParam:
		for _, name := range sess.Defs(value.Name) {
			if value.Type != schema.TypeRef || (name.Kind == value.Kind && (value.Parent == "" || name.Parent == value.Parent)) {
				add(locate(name.At, int(name.Line), int(name.Col)))
			}
		}
	case schema.TypeLoc:
		for _, said := range index.All(sess.All()).Entries(value.Name) {
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
	for _, reader := range index.Harvest(found, file.text, schema.Reader{Schema: sess.Schema, Vocab: sess.Install.Vocab}).Defs {
		out = append(out, Symbol{Name: reader.Name, Kind: reader.Kind, Parent: reader.Parent, Location: locate(found, int(reader.Line), int(reader.Col))})
	}
	return out, nil
}

// symbols is the definitions an index holds for one of its files.
func symbols(idx *index.Index, at source.File) []Symbol {
	var out []Symbol
	for _, def := range idx.Defs {
		if def.File >= 0 && index.PathKey(idx.Files[def.File].Path) == index.PathKey(at.Path) {
			out = append(out, Symbol{Name: def.Name, Kind: def.Kind, Parent: def.Parent, Location: locate(idx.Files[def.File], int(def.Line), int(def.Col))})
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
		if def.File >= 0 && strings.Contains(strings.ToLower(def.Name), query) {
			out = append(out, Symbol{Name: def.Name, Kind: def.Kind, Parent: def.Parent, Location: locate(idx.Files[def.File], int(def.Line), int(def.Col))})
		}
	}
	return out
}
