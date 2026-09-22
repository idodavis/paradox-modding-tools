// Package index is what a scan found: the file inventory, and the definitions, references, edges and localization entries written in the files, for an install and for a workspace's mods. It infers nothing. Every kind, every field and every rule about what a written word is comes from the schema and its reader; the index records where things are and answers whether they exist.
//
// index.go is the records, the lookups over them, and load order. The five slices are what is stored; the lookups are rebuilt from them and never stored, because a cache that stores its postings decodes every one of them into a grown slice (Phase 0 measured 268 MB of a 569 MB CK3 install held that way).
package index

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

// Index is what one scan found. There is one for the install and one for the workspace's mods.
type Index struct {
	Roots []string      `json:"roots"` // the folders that mirror the game layout: the game folder and each DLC folder, or each mod
	Files []source.File `json:"files"` // the inventory: every file, with its path, rel, origin and format. It is the catalogue of binary files
	Defs  []Def         `json:"defs"`
	Refs  []Ref         `json:"refs"`
	Edges []Edge        `json:"edges"`
	Loc   []Loc         `json:"loc"`

	Replaced          []string           `json:"replaced,omitempty"` // a workspace only: the folders its mods replace (replace_path), lower-cased. The game loads no install file that sits in one, subfolders apart
	defs, locs, bases map[string][]int32 // definitions by name, loc entries by key, files by lower-cased base name
	stems             map[string]bool    // the lower-cased base names without their extension
	rels, paths       map[string]int32   // a file by its lower-cased rel (of several roots' the last, which the game loads last), and by its path
	owned             map[int32]*owns    // a live index only: what each file added, so an edit can take it back
	shared            map[string]string  // every word kept, once: a word read from a file is a piece of that file's text, and keeping it would keep the whole file
}

// Def is one definition found in a file.
type Def struct {
	Kind      string `json:"kind"`                // the kind it belongs to
	Name      string `json:"name"`                // its name, found the way Kind.Naming says
	Parent    string `json:"parent,omitempty"`    // nested kind: the name of the definition it sits inside; a parameter: its macro
	Label     string `json:"label,omitempty"`     // the value of the kind's LabelMember. "" if none
	Mode      string `json:"mode,omitempty"`      // the entry mode written before the name: REPLACE, INJECT. "" if none
	ScopeType string `json:"scopeType,omitempty"` // saved_scopes only: the scope type at the save site
	File      int32  `json:"file"`                // index into Index.Files; -1 once the file that held it was dropped
	Line      int32  `json:"line"`
	Col       int32  `json:"col"`
}

// Ref is one written word that names something: a definition, a loc key or a file.
type Ref struct {
	Type     schema.ValueType `json:"type"`           // Ref | Loc | File
	Kind     string           `json:"kind,omitempty"` // Ref: the kind of the definition named
	Name     string           `json:"name"`
	Parent   string           `json:"parent,omitempty"`
	Declared bool             `json:"declared,omitempty"` // copied from the Value it was read as
	File     int32            `json:"file"`
	Line     int32            `json:"line"`
	Col      int32            `json:"col"`
}

// Edge is one place where one definition fires or invokes another: the event graph's arrows.
type Edge struct {
	FromKind string `json:"fromKind"` // the definition the site is written in
	From     string `json:"from"`
	ToKind   string `json:"toKind"` // the definition fired or invoked
	To       string `json:"to"`
	Via      string `json:"via"` // the token at the site: trigger_event, on_actions, the macro's own name
	File     int32  `json:"file"`
	Line     int32  `json:"line"`
}

// Loc is one localization entry and the file it is written in; the file's path says its language.
type Loc struct {
	loc.Entry
	File int32 `json:"file"`
}

// owns is what one file added to a live index, as positions in its slices.
type owns struct{ defs, refs, edges, loc []int32 }

// Records is what one file holds, before it has a place in an index.
type Records struct {
	File  source.File
	Defs  []Def
	Refs  []Ref
	Edges []Edge
	Loc   []loc.Entry
}

// Found is one definition with the file it was found in, which is what load order is decided on: a Def's File means nothing outside its own index.
type Found struct {
	Def
	At source.File
}

// Contest is one name that more than one origin defines, in load order, and which of them the game keeps.
type Contest struct {
	Kind, Name string
	Found      []Found
	Winner     int  // the position in Found of the definition the game keeps
	FirstWins  bool // the kind's rule in this game
}

// Ready builds the lookups of an index that was loaded or filled without them. Every lookup calls it; a scan calls it once before its readers share the index, because building is the one write a lookup makes.
func (x *Index) Ready() {
	if x.defs == nil {
		x.defs, x.locs, x.bases = map[string][]int32{}, map[string][]int32{}, map[string][]int32{}
		x.rels, x.paths, x.stems, x.shared = map[string]int32{}, map[string]int32{}, map[string]bool{}, map[string]string{}
		x.post(0, 0, 0)
	}
}

// post files the definitions from position d on and the loc entries from l on in the lookups, and every file from f on.
func (x *Index) post(f, d, l int) {
	for i := f; i < len(x.Files); i++ {
		rel := strings.ToLower(x.Files[i].Rel)
		base := path.Base(rel)
		x.rels[rel], x.paths[x.Files[i].Path], x.bases[base], x.stems[strings.TrimSuffix(base, path.Ext(base))] = int32(i), int32(i), append(x.bases[base], int32(i)), true
	}
	for i := d; i < len(x.Defs); i++ {
		x.defs[x.Defs[i].Name] = append(x.defs[x.Defs[i].Name], int32(i))
	}
	for i := l; i < len(x.Loc); i++ {
		x.locs[x.Loc[i].Key] = append(x.locs[x.Loc[i].Key], int32(i))
	}
}

// own is the one kept copy of a word.
func (x *Index) own(word string) string {
	if kept, ok := x.shared[word]; ok || word == "" {
		return kept
	}
	if x.shared == nil {
		x.shared = map[string]string{}
	}
	x.shared[strings.Clone(word)] = strings.Clone(word)
	return x.shared[word]
}

// Add gives a file's harvest its place: the file joins the inventory if it is new to it, and every record is filed under its name.
func (x *Index) Add(h *Records) {
	x.Ready()
	f, d, l := len(x.Files), len(x.Defs), len(x.Loc)
	at, known := x.paths[h.File.Path]
	if !known {
		at, x.Files = int32(len(x.Files)), append(x.Files, h.File)
	}

	mine := x.owned[at]
	if mine == nil {
		mine = &owns{}
	}
	for _, def := range h.Defs {
		def.Kind, def.Name, def.Parent, def.Label, def.Mode, def.ScopeType = x.own(def.Kind), x.own(def.Name), x.own(def.Parent), x.own(def.Label), x.own(def.Mode), x.own(def.ScopeType)
		def.File = at
		mine.defs, x.Defs = append(mine.defs, int32(len(x.Defs))), append(x.Defs, def)
	}
	for _, ref := range h.Refs {
		ref.Kind, ref.Name, ref.Parent = x.own(ref.Kind), x.own(ref.Name), x.own(ref.Parent)
		ref.File = at
		mine.refs, x.Refs = append(mine.refs, int32(len(x.Refs))), append(x.Refs, ref)
	}
	for _, edge := range h.Edges {
		edge.FromKind, edge.From, edge.ToKind, edge.To, edge.Via = x.own(edge.FromKind), x.own(edge.From), x.own(edge.ToKind), x.own(edge.To), x.own(edge.Via)
		edge.File = at
		mine.edges, x.Edges = append(mine.edges, int32(len(x.Edges))), append(x.Edges, edge)
	}
	for _, entry := range h.Loc {
		entry.Key, entry.Value = x.own(entry.Key), strings.Clone(entry.Value)
		mine.loc, x.Loc = append(mine.loc, int32(len(x.Loc))), append(x.Loc, Loc{Entry: entry, File: at})
	}
	if x.owned != nil {
		x.owned[at] = mine
	}
	x.post(f, d, l)
}

// Drop takes back everything the file at path added. A record keeps its slot with File -1, so the slices never move under a reader, and leaves the lookups. Only a live index can drop.
func (x *Index) Drop(filePath string) {
	x.Ready()
	at, known := x.paths[filePath]
	mine := x.owned[at]
	if !known || mine == nil {
		return
	}
	without := func(posting map[string][]int32, name string, i int32) {
		if posting[name] = slices.DeleteFunc(posting[name], func(o int32) bool { return o == i }); len(posting[name]) == 0 {
			delete(posting, name)
		}
	}
	for _, i := range mine.defs {
		without(x.defs, x.Defs[i].Name, i)
		x.Defs[i].File = -1
	}
	for _, i := range mine.refs {
		x.Refs[i].File = -1
	}
	for _, i := range mine.edges {
		x.Edges[i].File = -1
	}
	for _, i := range mine.loc {
		without(x.locs, x.Loc[i].Key, i)
		x.Loc[i].File = -1
	}
	delete(x.owned, at)
}

// Named is every definition of that name, of any kind, with its file, in the order found.
func (x *Index) Named(name string) []Found {
	x.Ready()
	var out []Found
	for _, i := range x.defs[name] {
		out = append(out, Found{Def: x.Defs[i], At: x.Files[x.Defs[i].File]})
	}
	return out
}

// Def reports a definition of that kind with that name.
func (x *Index) Def(kind, name string) bool {
	x.Ready()
	return slices.ContainsFunc(x.defs[name], func(i int32) bool { return x.Defs[i].Kind == kind })
}

// HasLoc reports a localization key.
func (x *Index) HasLoc(key string) bool {
	x.Ready()
	return len(x.locs[key]) > 0
}

// File reports a file by the three lookups measured in the plan's §10: the path from a root, the file name anywhere in the inventory, the same name under another extension (.tga written, .dds shipped).
func (x *Index) File(name string) bool {
	x.Ready()
	name = strings.ToLower(strings.ReplaceAll(name, "\\", "/"))
	base := path.Base(name)
	if _, ok := x.rels[name]; ok || len(x.bases[base]) > 0 {
		return true
	}
	return path.Ext(base) != "" && x.stems[strings.TrimSuffix(base, path.Ext(base))]
}

// All answers from several indexes together (schema.Defined): a workspace's mods and the install beneath them. A file of one index that sits in a folder another index replaces is not loaded by the game, and what it holds does not exist.
type All []*Index

// loaded reports a file of x that no other index replaces.
func (a All) loaded(x *Index, file int32) bool {
	folder := path.Dir(strings.ToLower(x.Files[file].Rel))
	return !slices.ContainsFunc(a, func(o *Index) bool { return o != x && slices.Contains(o.Replaced, folder) })
}

// Named is every definition of that name the game loads, of any kind, with its file.
func (a All) Named(name string) []Found {
	var out []Found
	for _, x := range a {
		out = append(out, slices.DeleteFunc(x.Named(name), func(f Found) bool { return !a.loaded(x, f.File) })...)
	}
	return out
}

func (a All) Def(kind, name string) bool {
	return slices.ContainsFunc(a.Named(name), func(f Found) bool { return f.Kind == kind })
}
func (a All) Loc(key string) bool {
	return slices.ContainsFunc(a, func(x *Index) bool { return x.HasLoc(key) })
}
func (a All) File(name string) bool {
	return slices.ContainsFunc(a, func(x *Index) bool {
		x.Ready()
		at, isRel := x.rels[strings.ToLower(strings.ReplaceAll(name, "\\", "/"))]
		return x.File(name) && (!isRel || a.loaded(x, at))
	})
}

// before reports that a loads before b: by its origin's place in the playset's order, then by file name in ASCII order with a folder's own files before its subfolders' (the Victoria 3 and EU5 wikis, Mod compatibility), then by line.
func before(a, b Found, order []string) int {
	folderA, folderB := path.Dir(strings.ToLower(a.At.Rel)), path.Dir(strings.ToLower(b.At.Rel))
	return cmp.Or(
		cmp.Compare(slices.Index(order, a.At.Origin), slices.Index(order, b.At.Origin)),
		cmp.Compare(strings.Count(folderA, "/"), strings.Count(folderB, "/")),
		strings.Compare(folderA, folderB),
		strings.Compare(path.Base(strings.ToLower(a.At.Rel)), path.Base(strings.ToLower(b.At.Rel))),
		cmp.Compare(a.Line, b.Line),
	)
}

// Winner is the definition the game keeps of several with one kind and name, and its position among them once they are put in load order (which Winner does, in place). Definitions with no entry mode come first, then each mode in the order the registry gives, and load order decides only within one mode. A kind is first-wins or last-wins among plain definitions; a replace takes the place of what was kept; an inject adds to it and never displaces it; a mode that creates is kept where nothing was. order is the origins in the playset's order, the install first.
func Winner(found []Found, firstWins bool, modes []game.EntryMode, order []string) (Found, int) {
	rank := func(f Found) int {
		return 1 + slices.IndexFunc(modes, func(m game.EntryMode) bool { return m.Word == f.Mode })
	}
	slices.SortStableFunc(found, func(a, b Found) int { return cmp.Or(cmp.Compare(rank(a), rank(b)), before(a, b, order)) })

	kept := -1
	for i, f := range found {
		if rank(f) == 0 {
			if kept < 0 || !firstWins {
				kept = i
			}
			continue
		}
		if mode := modes[rank(f)-1]; (mode.Replaces && (kept >= 0 || mode.Creates || !mode.Optional)) || (kept < 0 && mode.Creates) {
			kept = i
		}
	}
	if kept < 0 {
		return Found{}, -1
	}
	return found[kept], kept
}

// Contests is every name of a kind that files define which more than one origin defines, by kind and name. Script-made names, constants, parameters and loc-named kinds never contest: nothing loads one over another.
func Contests(all All, s *schema.Schema, firstWins func(kind string) bool, modes []game.EntryMode, order []string) []Contest {
	type id struct{ kind, name string }
	found := map[id][]Found{}
	for _, x := range all {
		for _, def := range x.Defs {
			k := s.Kinds[def.Kind]
			if def.File >= 0 && k != nil && len(k.Folders) > 0 && k.Subject == "" && all.loaded(x, def.File) {
				at := id{def.Kind, def.Name}
				found[at] = append(found[at], Found{Def: def, At: x.Files[def.File]})
			}
		}
	}

	var out []Contest
	for at, defs := range found {
		origins := map[string]bool{}
		for _, f := range defs {
			origins[f.At.Origin] = true
		}
		if len(origins) > 1 {
			_, winner := Winner(defs, firstWins(at.kind), modes, order)
			out = append(out, Contest{Kind: at.kind, Name: at.name, Found: defs, Winner: winner, FirstWins: firstWins(at.kind)})
		}
	}
	slices.SortFunc(out, func(a, b Contest) int {
		return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Name, b.Name))
	})
	return out
}
