// Package index is what a scan found: the file inventory, and the definitions, references, edges
// and localization entries written in the files, for an install and for a workspace's mods. It
// infers nothing: every kind, field and rule about what a written word is comes from the schema and
// its reader; the index records where things are and answers whether they exist.
//
// index.go is the records, the lookups over them, and load order. Only the five slices are stored;
// the lookups are rebuilt, since decoded postings would each sit in an over-grown slice.
package index

import (
	"cmp"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// Index is what one scan found. There is one for the install and one for the workspace's mods.
type Index struct {
	Roots []string      // the folders that mirror the game layout: the game folder and each DLC folder, or each mod
	Files []source.File // the inventory: each file's path, rel, origin and format; the only catalogue of binary files
	Defs  []Def
	Refs  []Ref
	Edges []Edge
	Loc   []Loc

	Replaced         []string           // workspace only: lower-cased replace_path folders, where no install file loads
	defs, refs, locs map[string][]int32 // definitions by name, references by name, loc entries by key
	from, to         map[string][]int32 // edges by either end
	bases            map[string][]int32 // files by lower-cased base name
	stems            map[string]int32   // a file by its lower-cased base name without the extension
	rels, paths      map[string]int32   // a file by lower-cased rel (the last root's wins) and by PathKey
	owned            map[int32]*owns    // a live index only: what each file added, so an edit can take it back
	shared           map[string]string  // each word kept once, cloned so it does not pin its file's text
}

// Def is one definition found in a file.
type Def struct {
	Kind      string `json:"kind"`                // the kind it belongs to
	Name      string `json:"name"`                // its name, found the way Kind.Naming says
	Parent    string `json:"parent,omitempty"`    // nested kind: the definition it sits inside; a parameter: its macro
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
	File int32
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

// Found is one definition with its file, which load order is decided on; a Def's File means nothing
// outside its own index.
type Found struct {
	Def
	At source.File `json:"at"`
}

// Cited is one reference with the file it is written in.
type Cited struct {
	Ref
	At source.File `json:"at"`
}

// Firing is one edge with the file it is written in.
type Firing struct {
	Edge
	At source.File `json:"at"`
}

// Said is one localization entry with the file it is written in.
type Said struct {
	loc.Entry
	At source.File `json:"at"`
}

// Contest is a name more than one origin defines, in load order, and which one the game keeps.
type Contest struct {
	Kind, Name string
	Found      []Found
	Winner     int  // the position in Found of the definition the game keeps
	FirstWins  bool // the kind's rule in this game
}

// PathKey is the one spelling two paths of one file share: cleaned, and lower-cased on Windows,
// where the file system folds case. The editor keys its tabs by the caller's spelling, which every
// record keeps; only the lookup folds.
func PathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// At is the inventory's file at a path.
func (idx *Index) At(path string) (source.File, bool) {
	idx.Ready()
	at, ok := idx.paths[PathKey(path)]
	if !ok {
		return source.File{}, false
	}
	return idx.Files[at], true
}

// Ready builds the lookups of an index loaded or filled without them. Every lookup calls it; a scan
// calls it once before readers share the index, since building is the one write a lookup makes.
func (idx *Index) Ready() {
	if idx.defs == nil {
		idx.defs, idx.refs, idx.locs, idx.bases = map[string][]int32{}, map[string][]int32{}, map[string][]int32{}, map[string][]int32{}
		idx.from, idx.to = map[string][]int32{}, map[string][]int32{}
		idx.rels, idx.paths, idx.stems, idx.shared = map[string]int32{}, map[string]int32{}, map[string]int32{}, map[string]string{}
		idx.post(0, 0, 0, 0, 0)
	}
}

// post adds every file, definition, reference, edge and loc entry from the given positions on.
func (idx *Index) post(fromFile, fromDef, fromRef, fromEdge, fromLoc int) {
	for i := fromFile; i < len(idx.Files); i++ {
		rel := strings.ToLower(idx.Files[i].Rel)
		base := path.Base(rel)
		idx.rels[rel], idx.paths[PathKey(idx.Files[i].Path)], idx.bases[base], idx.stems[strings.TrimSuffix(base, path.Ext(base))] = int32(i), int32(i), append(idx.bases[base], int32(i)), int32(i)
	}
	for i := fromDef; i < len(idx.Defs); i++ {
		idx.defs[idx.Defs[i].Name] = append(idx.defs[idx.Defs[i].Name], int32(i))
	}
	for i := fromRef; i < len(idx.Refs); i++ {
		idx.refs[idx.Refs[i].Name] = append(idx.refs[idx.Refs[i].Name], int32(i))
	}
	for i := fromEdge; i < len(idx.Edges); i++ {
		idx.from[idx.Edges[i].From], idx.to[idx.Edges[i].To] = append(idx.from[idx.Edges[i].From], int32(i)), append(idx.to[idx.Edges[i].To], int32(i))
	}
	for i := fromLoc; i < len(idx.Loc); i++ {
		idx.locs[idx.Loc[i].Key] = append(idx.locs[idx.Loc[i].Key], int32(i))
	}
}

// own is the one kept copy of a word.
func (idx *Index) own(word string) string {
	if kept, ok := idx.shared[word]; ok || word == "" {
		return kept
	}
	if idx.shared == nil {
		idx.shared = map[string]string{}
	}
	idx.shared[strings.Clone(word)] = strings.Clone(word)
	return idx.shared[word]
}

// Add gives a file's harvest its place: the file joins the inventory if it is new, and every record
// is filed under its name.
func (idx *Index) Add(records *Records) {
	idx.Ready()
	fromFile, fromDef, fromRef, fromEdge, fromLoc := len(idx.Files), len(idx.Defs), len(idx.Refs), len(idx.Edges), len(idx.Loc)
	at, known := idx.paths[PathKey(records.File.Path)]
	if !known {
		at, idx.Files = int32(len(idx.Files)), append(idx.Files, records.File)
	} else if idx.Files[at] != records.File {
		idx.Files[at] = records.File
	}

	mine := idx.owned[at]
	if mine == nil {
		mine = &owns{}
	}
	for _, def := range records.Defs {
		def.Kind, def.Name, def.Parent, def.Label, def.Mode, def.ScopeType = idx.own(def.Kind), idx.own(def.Name), idx.own(def.Parent), idx.own(def.Label), idx.own(def.Mode), idx.own(def.ScopeType)
		def.File = at
		mine.defs, idx.Defs = append(mine.defs, int32(len(idx.Defs))), append(idx.Defs, def)
	}
	for _, ref := range records.Refs {
		ref.Kind, ref.Name, ref.Parent = idx.own(ref.Kind), idx.own(ref.Name), idx.own(ref.Parent)
		ref.File = at
		mine.refs, idx.Refs = append(mine.refs, int32(len(idx.Refs))), append(idx.Refs, ref)
	}
	for _, edge := range records.Edges {
		edge.FromKind, edge.From, edge.ToKind, edge.To, edge.Via = idx.own(edge.FromKind), idx.own(edge.From), idx.own(edge.ToKind), idx.own(edge.To), idx.own(edge.Via)
		edge.File = at
		mine.edges, idx.Edges = append(mine.edges, int32(len(idx.Edges))), append(idx.Edges, edge)
	}
	for _, entry := range records.Loc {
		entry.Key, entry.Value = idx.own(entry.Key), strings.Clone(entry.Value)
		mine.loc, idx.Loc = append(mine.loc, int32(len(idx.Loc))), append(idx.Loc, Loc{Entry: entry, File: at})
	}
	if idx.owned != nil {
		idx.owned[at] = mine
	}
	idx.post(fromFile, fromDef, fromRef, fromEdge, fromLoc)
}

// Drop takes back everything the file at path added. Each record leaves the lookups but keeps its
// slot with File -1, so the slices never move under a reader. Only a live index can drop.
func (idx *Index) Drop(filePath string) {
	idx.Ready()
	at, known := idx.paths[PathKey(filePath)]
	mine := idx.owned[at]
	if !known || mine == nil {
		return
	}
	without := func(posting map[string][]int32, name string, i int32) {
		if posting[name] = slices.DeleteFunc(posting[name], func(position int32) bool { return position == i }); len(posting[name]) == 0 {
			delete(posting, name)
		}
	}
	for _, at := range mine.defs {
		without(idx.defs, idx.Defs[at].Name, at)
		idx.Defs[at].File = -1
	}
	for _, at := range mine.refs {
		without(idx.refs, idx.Refs[at].Name, at)
		idx.Refs[at].File = -1
	}
	for _, at := range mine.edges {
		without(idx.from, idx.Edges[at].From, at)
		without(idx.to, idx.Edges[at].To, at)
		idx.Edges[at].File = -1
	}
	for _, at := range mine.loc {
		without(idx.locs, idx.Loc[at].Key, at)
		idx.Loc[at].File = -1
	}
	delete(idx.owned, at)
}

// Named is every definition of that name, of any kind, with its file, in the order found.
func (idx *Index) Named(name string) []Found {
	idx.Ready()
	var out []Found
	for _, at := range idx.defs[name] {
		out = append(out, Found{Def: idx.Defs[at], At: idx.Files[idx.Defs[at].File]})
	}
	return out
}

// Referring is every reference to a name, of any kind, with its file.
func (idx *Index) Referring(name string) []Cited {
	idx.Ready()
	var out []Cited
	for _, at := range idx.refs[name] {
		out = append(out, Cited{Ref: idx.Refs[at], At: idx.Files[idx.Refs[at].File]})
	}
	return out
}

// Fires is every edge out of a definition, and FiredBy every edge into one, each with its file.
func (idx *Index) Fires(name string) []Firing   { return idx.edges(idx.from, name) }
func (idx *Index) FiredBy(name string) []Firing { return idx.edges(idx.to, name) }

// edges is the edges a posting lists for a name.
func (idx *Index) edges(posting map[string][]int32, name string) []Firing {
	idx.Ready()
	var out []Firing
	for _, at := range posting[name] {
		out = append(out, Firing{Edge: idx.Edges[at], At: idx.Files[idx.Edges[at].File]})
	}
	return out
}

// Def reports a definition of that kind with that name.
func (idx *Index) Def(kind, name string) bool {
	idx.Ready()
	return slices.ContainsFunc(idx.defs[name], func(i int32) bool { return idx.Defs[i].Kind == kind })
}

// HasLoc reports a localization key.
func (idx *Index) HasLoc(key string) bool {
	idx.Ready()
	return len(idx.locs[key]) > 0
}

// Entries is every entry of a key, with its file, in the order found; the file says the language.
func (idx *Index) Entries(key string) []Said {
	idx.Ready()
	var out []Said
	for _, at := range idx.locs[key] {
		out = append(out, Said{Entry: idx.Loc[at].Entry, At: idx.Files[idx.Loc[at].File]})
	}
	return out
}

// Find is the file a written word names, by three lookups in turn: the path from a root, the file
// name anywhere, the same name under another extension (.tga written, .dds shipped).
func (idx *Index) Find(name string) (source.File, bool) {
	idx.Ready()
	name = strings.ToLower(strings.ReplaceAll(name, "\\", "/"))
	base := path.Base(name)
	if at, ok := idx.rels[name]; ok {
		return idx.Files[at], true
	}
	if len(idx.bases[base]) > 0 {
		return idx.Files[idx.bases[base][0]], true
	}
	if at, ok := idx.stems[strings.TrimSuffix(base, path.Ext(base))]; ok && path.Ext(base) != "" {
		return idx.Files[at], true
	}
	return source.File{}, false
}

// File reports a file by Find, for schema.Defined.
func (idx *Index) File(name string) bool {
	_, ok := idx.Find(name)
	return ok
}

// All answers from several indexes together (schema.Defined): a workspace's mods and the install
// beneath them. A file of one index in a folder another index replaces is not loaded by the game,
// and what it holds does not exist.
type All []*Index

// loaded reports a file of idx that no other index replaces.
func (all All) loaded(idx *Index, file int32) bool {
	folder := path.Dir(strings.ToLower(idx.Files[file].Rel))
	return !slices.ContainsFunc(all, func(other *Index) bool { return other != idx && slices.Contains(other.Replaced, folder) })
}

// Named is every definition of that name the game loads, of any kind, with its file.
func (all All) Named(name string) []Found {
	var out []Found
	for _, idx := range all {
		out = append(out, slices.DeleteFunc(idx.Named(name), func(found Found) bool { return !all.loaded(idx, found.File) })...)
	}
	return out
}

func (all All) Def(kind, name string) bool {
	return slices.ContainsFunc(all.Named(name), func(found Found) bool { return found.Kind == kind })
}
func (all All) Loc(key string) bool {
	return slices.ContainsFunc(all, func(idx *Index) bool { return idx.HasLoc(key) })
}

// Entries is every entry of a key the game loads, in the indexes' order.
func (all All) Entries(key string) []Said {
	var out []Said
	for _, idx := range all {
		idx.Ready()
		for _, at := range idx.locs[key] {
			if all.loaded(idx, idx.Loc[at].File) {
				out = append(out, Said{Entry: idx.Loc[at].Entry, At: idx.Files[idx.Loc[at].File]})
			}
		}
	}
	return out
}
func (all All) File(name string) bool {
	_, ok := all.Find(name)
	return ok
}

// Find is the file a written word names, the last index's first, leaving out a file in a folder
// another index replaces.
func (all All) Find(name string) (source.File, bool) {
	for _, idx := range slices.Backward(all) {
		if file, ok := idx.Find(name); ok && all.loaded(idx, idx.paths[PathKey(file.Path)]) {
			return file, true
		}
	}
	return source.File{}, false
}

// Before orders two definitions by load order: their origins' place in the playset, then file name
// in ASCII order with a folder's own files before its subfolders' (the Vic3 and EU5 wikis, Mod
// compatibility), then line.
func Before(first, second Found, order []string) int {
	folderA, folderB := path.Dir(strings.ToLower(first.At.Rel)), path.Dir(strings.ToLower(second.At.Rel))
	return cmp.Or(
		cmp.Compare(slices.Index(order, first.At.Origin), slices.Index(order, second.At.Origin)),
		cmp.Compare(strings.Count(folderA, "/"), strings.Count(folderB, "/")),
		strings.Compare(folderA, folderB),
		strings.Compare(path.Base(strings.ToLower(first.At.Rel)), path.Base(strings.ToLower(second.At.Rel))),
		cmp.Compare(first.Line, second.Line),
	)
}

// Winner is the definition the game keeps of several with one kind and name, and its position once
// Winner has sorted them into load order in place. Plain definitions come first, then each entry
// mode in the registry's order; load order decides only within one mode. Among plain definitions a
// kind is first-wins or last-wins; a replace takes the place of what was kept; an inject adds to it
// and never displaces it; a mode that creates is kept where nothing was. order is the origins in
// playset order, the install first.
func Winner(definitions []Found, firstWins bool, modes []game.EntryMode, order []string) (Found, int) {
	rank := func(definition Found) int {
		return 1 + slices.IndexFunc(modes, func(mode game.EntryMode) bool { return mode.Word == definition.Mode })
	}
	slices.SortStableFunc(definitions, func(first, second Found) int {
		return cmp.Or(cmp.Compare(rank(first), rank(second)), Before(first, second, order))
	})

	kept := -1
	for i, definition := range definitions {
		if rank(definition) == 0 {
			if kept < 0 || !firstWins {
				kept = i
			}
			continue
		}
		if mode := modes[rank(definition)-1]; (mode.Replaces && (kept >= 0 || mode.Creates || !mode.Optional)) || (kept < 0 && mode.Creates) {
			kept = i
		}
	}
	if kept < 0 {
		return Found{}, -1
	}
	return definitions[kept], kept
}

// Contests is every name of a file-defined kind that more than one origin defines, sorted by kind
// and name. Script-made names, constants, parameters and loc-named kinds never contest: nothing
// loads one over another.
func Contests(all All, sch *schema.Schema, firstWins func(kind string) bool, modes []game.EntryMode, order []string) []Contest {
	type id struct{ kind, name string }
	found := map[id][]Found{}
	for _, idx := range all {
		for _, def := range idx.Defs {
			kind := sch.Kinds[def.Kind]
			if def.File >= 0 && kind != nil && len(kind.Folders) > 0 && kind.Subject == "" && all.loaded(idx, def.File) {
				at := id{def.Kind, def.Name}
				found[at] = append(found[at], Found{Def: def, At: idx.Files[def.File]})
			}
		}
	}

	var out []Contest
	for at, defs := range found {
		origins := map[string]bool{}
		for _, def := range defs {
			origins[def.At.Origin] = true
		}
		if len(origins) > 1 {
			_, winner := Winner(defs, firstWins(at.kind), modes, order)
			out = append(out, Contest{Kind: at.kind, Name: at.name, Found: defs, Winner: winner, FirstWins: firstWins(at.kind)})
		}
	}
	slices.SortFunc(out, func(first, second Contest) int {
		return cmp.Or(strings.Compare(first.Kind, second.Kind), strings.Compare(first.Name, second.Name))
	})
	return out
}

// Body is what the cache holds of a scan besides its schema, which is kept as readable, diffable
// text. The app writes and reads it; this package defines it and how it is restored.
type Body struct {
	Index *Index
	Vocab *schema.Vocab
	Info  schema.Info
}

// Restore prepares a Body that has come back from a decoder: its slices are cut to their length (a
// decoder grows them by doubling), its words are shared again, and its lookups are rebuilt.
func (body *Body) Restore() {
	idx := body.Index
	idx.shared = map[string]string{}
	idx.Files, idx.Defs, idx.Refs, idx.Edges, idx.Loc = slices.Clone(idx.Files), slices.Clone(idx.Defs), slices.Clone(idx.Refs), slices.Clone(idx.Edges), slices.Clone(idx.Loc)
	for i := range idx.Defs {
		def := &idx.Defs[i]
		def.Kind, def.Name, def.Parent, def.Label, def.Mode, def.ScopeType = idx.own(def.Kind), idx.own(def.Name), idx.own(def.Parent), idx.own(def.Label), idx.own(def.Mode), idx.own(def.ScopeType)
	}
	for i := range idx.Refs {
		ref := &idx.Refs[i]
		ref.Kind, ref.Name, ref.Parent = idx.own(ref.Kind), idx.own(ref.Name), idx.own(ref.Parent)
	}
	for i := range idx.Edges {
		edge := &idx.Edges[i]
		edge.FromKind, edge.From, edge.ToKind, edge.To, edge.Via = idx.own(edge.FromKind), idx.own(edge.From), idx.own(edge.ToKind), idx.own(edge.To), idx.own(edge.Via)
	}
	for i := range idx.Loc {
		idx.Loc[i].Key = idx.own(idx.Loc[i].Key)
	}
	idx.shared = nil
	idx.Ready()
}
