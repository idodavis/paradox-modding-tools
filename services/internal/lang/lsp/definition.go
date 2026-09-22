// definition.go is navigation: where the word under the cursor is defined, where a name is used, and the definitions a file or a workspace holds. Every answer is the index's records as locations; nothing is parsed but the open file.
package lsp

import (
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/source"
)

// Symbol is one definition, for an outline or a workspace search.
type Symbol struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Parent   string   `json:"parent,omitempty"`
	Location Location `json:"location"`
}

// Definition is where the word at a position is defined: every site in load order, the one the game keeps first.
func Definition(s *session.Session, path string, line, col int) ([]Location, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	var card *Inspect
	if f.place.Format == source.Loc {
		card = f.locCard(f.offset(line, col))
	} else if w := f.at(f.offset(line, col)); w != nil {
		card = f.card(w)
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

// References is every place that names what the word at a position names, its definitions included, with no site twice.
func References(s *session.Session, path string, line, col int) ([]Location, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	var v schema.Value
	if f.place.Format == source.Loc {
		card := f.locCard(f.offset(line, col))
		if card == nil {
			return nil, nil
		}
		v = card.Value
	} else {
		w := f.at(f.offset(line, col))
		if w == nil {
			return nil, nil
		}
		v = w.Value
	}
	if v.Type == schema.TypeParam && f.place.Format == source.Loc {
		v.Type = schema.TypeLoc
	}

	seen := map[Location]bool{}
	var out []Location
	add := func(loc Location) {
		if !seen[loc] {
			seen[loc], out = true, append(out, loc)
		}
	}
	for _, x := range s.All() {
		for _, r := range x.Referring(v.Name) {
			if r.Type == v.Type && (v.Type != schema.TypeRef || (r.Kind == v.Kind && (v.Parent == "" || r.Parent == v.Parent))) {
				add(locate(r.At, int(r.Line), int(r.Col)))
			}
		}
	}
	switch v.Type {
	case schema.TypeRef, schema.TypeConstant, schema.TypeParam:
		for _, d := range s.Defs(v.Name) {
			if v.Type != schema.TypeRef || (d.Kind == v.Kind && (v.Parent == "" || d.Parent == v.Parent)) {
				add(locate(d.At, int(d.Line), int(d.Col)))
			}
		}
	case schema.TypeLoc:
		for _, said := range index.All(s.All()).Entries(v.Name) {
			add(locate(said.At, said.Line, 0))
		}
	}
	return out, nil
}

// DocumentSymbols is the definitions a file holds, in the file's order.
func DocumentSymbols(s *session.Session, path string) ([]Symbol, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	found, _, ok := s.Located(path)
	if !ok {
		return nil, nil
	}
	for _, x := range s.All() {
		if at, ok := x.At(path); ok {
			return symbols(x, at), nil
		}
	}
	// a file nothing has walked: its own harvest, kept nowhere
	var out []Symbol
	for _, d := range index.Harvest(found, f.text, schema.Reader{Schema: s.Schema, Vocab: s.Install.Vocab}).Defs {
		out = append(out, Symbol{Name: d.Name, Kind: d.Kind, Parent: d.Parent, Location: locate(found, int(d.Line), int(d.Col))})
	}
	return out, nil
}

// symbols is the definitions an index holds for one of its files.
func symbols(x *index.Index, at source.File) []Symbol {
	var out []Symbol
	for _, d := range x.Defs {
		if d.File >= 0 && index.PathKey(x.Files[d.File].Path) == index.PathKey(at.Path) {
			out = append(out, Symbol{Name: d.Name, Kind: d.Kind, Parent: d.Parent, Location: locate(x.Files[d.File], int(d.Line), int(d.Col))})
		}
	}
	return out
}

// WorkspaceSymbols is every definition of the mods whose name holds the query, ignoring case; nothing for an empty query.
func WorkspaceSymbols(s *session.Session, query string) []Symbol {
	s.RLock()
	defer s.RUnlock()
	query = strings.ToLower(query)
	if query == "" {
		return nil
	}
	var out []Symbol
	x := s.Workspace
	for _, d := range x.Defs {
		if d.File >= 0 && strings.Contains(strings.ToLower(d.Name), query) {
			out = append(out, Symbol{Name: d.Name, Kind: d.Kind, Parent: d.Parent, Location: locate(x.Files[d.File], int(d.Line), int(d.Col))})
		}
	}
	return out
}
