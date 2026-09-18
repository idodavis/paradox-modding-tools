// index.go is the one container: the records the engine emits, the posting
// lists every query reads, the localization table, the learned sets, the
// declared schema and the shipped prose. It is also the cache file, byte for
// byte -- nothing is rebuilt on load -- and the workspace model, built by the
// same engine at session open. Records carry a PathID into Paths rather than a
// path string each.

package derived

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
)

// CacheFormatVersion is the on-disk schema of an Index, bumped when the derived CONTENT changes too and not only the field layout, since nothing else invalidates a cache harvested by a since-corrected pass.
const CacheFormatVersion = 50

// Def is one named object. OwnerKey is the enclosing scripted macro for script_param defs; Value is the harvested RHS literal at a ScriptName def site (hover body).
type Def struct {
	Kind     string `json:"t"`
	Key      string `json:"k"`
	PathID   int32  `json:"p"`
	Line     int    `json:"l"`
	Start    int    `json:"s,omitempty"`
	End      int    `json:"e,omitempty"`
	Origin   string `json:"o,omitempty"`
	OwnerKey string `json:"w,omitempty"`
	Value    string `json:"v,omitempty"`
	// Nested marks a definition harvested from inside another one rather than at a file's top level; it is real, but it is not the file's subject.
	Nested bool `json:"n,omitempty"`
	// Local marks a definition the file DECLARES inline (`scripted_trigger foo = {…}`, a GUI local_template), so a repeated key is required rather than a defect and a call must never navigate to another file's body.
	Local bool `json:"i,omitempty"`
	// Doc is the comment block written directly above the definition -- for most objects the only description that exists, since script_docs covers the engine API and not script.
	Doc string `json:"d,omitempty"`
	// Path is stored once per file in Paths and filled from PathID when a query hands the record out, so the file holds one path string per file rather than one per record.
	Path string `json:"-"`
}

// IsCitableDef reports a definition whose key names a real object, so a citation, a field value or a nested-shape join may bind to it. jomini.IsObjectKind is the stricter sibling that also rejects `namespace`.
func IsCitableDef(d Def) bool {
	k := jomini.CanonicalKind(d.Kind)
	return k != "" && k != jomini.KindLocKey && d.Key != "" && !jomini.IsEphemeral(k)
}

// Ref is a use-site. Kind is "loc", "loc-broad", "event", "on_action", or an ephemeral/script kind. OwnerKey scopes script_param refs to the enclosing scripted_* call.
type Ref struct {
	Key    string `json:"k"`
	KindID int32  `json:"t"`
	PathID int32  `json:"p"`
	Line   int    `json:"l"`
	Start  int    `json:"s"`
	// End is written only where it is not Start plus the key's length, which is every reference whose span is the name itself.
	End      int    `json:"e,omitempty"`
	OwnerKey string `json:"w,omitempty"`
	// Derived marks a reference whose KIND came from an install-derived pass rather than from something the game declares; accusing surfaces skip these.
	Derived bool `json:"d,omitempty"`
	// Kind and Path are stored once per index, in Kinds and Paths, and filled from their ids when a query hands the record out: an install writes 790,000 references and about twenty distinct kinds.
	Kind, Path string `json:"-"`
}

// Edge is a directed event-graph link. Kind "call" is a scripted-effect invocation.
type Edge struct {
	From    string `json:"f"`
	To      string `json:"o"`
	Via     string `json:"v"`
	PathID  int32  `json:"p"`
	Line    int    `json:"l"`
	Kind    string `json:"t,omitempty"`
	NameKey string `json:"n,omitempty"`
	// Rescoped marks a fire site under a block that changed scope, so the event does NOT run in the scope its source declares.
	Rescoped bool   `json:"r,omitempty"`
	Path     string `json:"-"`
}

// LocEntry is one localization value with its source file and line.
type LocEntry struct {
	Value  string `json:"v"`
	PathID int32  `json:"p"`
	Line   int    `json:"l"`
	Start  int    `json:"s,omitempty"`
	Origin string `json:"o,omitempty"`
	// Path is the spelling the loc row carried before it joined an index; it is empty on a stored entry.
	Path string `json:"-"`
}

// LocDelta is the loc payload one file contributes.
type LocDelta struct {
	Lang string
	Vals map[string]LocEntry
}

// NestedShape is a derived parent→child harvest pattern (faiths under religion, nested titles). GroupKey is the wrapper when every hit sat under one assignment.
type NestedShape struct {
	ParentKind string `json:"parentKind"`
	ChildKind  string `json:"childKind"`
	GroupKey   string `json:"groupKey,omitempty"`
}

// FileMeta is what a path is, recorded when the file was indexed so Locate and its dependents never scan the mod list.
type FileMeta struct {
	Rel    string `json:"rel,omitempty"`
	Origin string `json:"origin,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

// Docs is the prose and declared types the install ships beside its script.
type Docs struct {
	FieldInfo map[string]string `json:"fieldInfo,omitempty"`
	// Fields is prose addressed by (structure, field), which is how a game writes it: under the block key a field sits in, or under the file kind for one at the definition's own level.
	Fields                   map[string]map[string]string                `json:"fields,omitempty"`
	KindInfo                 map[string]string                           `json:"kindInfo,omitempty"`
	DeclaredFieldTypes       map[string]declared.DeclaredType            `json:"declaredFieldTypes,omitempty"`
	DeclaredFieldTypesByKind map[string]map[string]declared.DeclaredType `json:"declaredFieldTypesByKind,omitempty"`
}

// ModInput is one workspace mod: Origin id, Root folder, load Order, display Name.
type ModInput struct {
	Origin string
	Root   string
	Order  int
	Name   string
	// Calendar is the era this mod's dates are written in, when it declares one.
	Calendar ModCalendar
}

// ModCalendar is how a mod's date literals map to the era its players see: A Game of Thrones writes 8082.1.1 for what the game shows as 300 AC. Not derivable and deliberately not guessed -- it is metadata a person states once.
type ModCalendar struct {
	// Era is the suffix shown after a converted year, e.g. "AC".
	Era string `json:"era,omitempty"`
	// EpochOffset is subtracted from the script year to get the era year.
	EpochOffset int `json:"epochOffset,omitempty"`
}

// Declared reports whether this calendar says anything; an undeclared one must leave every date exactly as written.
func (c ModCalendar) Declared() bool { return c.Era != "" || c.EpochOffset != 0 }

// EraDate renders a Clausewitz date in the mod's era. Empty for an undeclared calendar, a non-date, or a year the offset would push to zero or below.
func EraDate(date string, cal ModCalendar) string {
	if !cal.Declared() {
		return ""
	}
	parts := strings.Split(date, ".")
	if len(parts) < 3 || len(parts) > 4 {
		return ""
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return ""
	}
	era := year - cal.EpochOffset
	if era <= 0 {
		return ""
	}
	out := strconv.Itoa(era) + "." + strings.Join(parts[1:], ".")
	if cal.Era != "" {
		out += " " + cal.Era
	}
	return out
}

// Index is what the engine emits into, what the cache file is, and what a query reads.
type Index struct {
	FormatVersion int    `json:"formatVersion"`
	InstallID     string `json:"installId,omitempty"`
	GameID        string `json:"gameId,omitempty"`
	InstallPath   string `json:"installPath,omitempty"`
	GameVersion   string `json:"gameVersion,omitempty"`
	ScannedAt     string `json:"scannedAt,omitempty"`

	Paths []string   `json:"paths,omitempty"`
	Files []FileMeta `json:"files,omitempty"`
	// Kinds is the reference kind table: a Ref stores an index into it.
	Kinds []string `json:"kinds,omitempty"`
	Defs  []Def    `json:"defs,omitempty"`
	Refs  []Ref    `json:"refs,omitempty"`
	Edges []Edge   `json:"edges,omitempty"`

	DefsByKey   map[string][]int32 `json:"defsByKey,omitempty"`
	DefsByKind  map[string][]int32 `json:"defsByKind,omitempty"`
	DefsByFile  map[int32][]int32  `json:"defsByFile,omitempty"`
	RefsByKey   map[string][]int32 `json:"refsByKey,omitempty"`
	RefsByFile  map[int32][]int32  `json:"refsByFile,omitempty"`
	EdgesFrom   map[string][]int32 `json:"edgesFrom,omitempty"`
	EdgesTo     map[string][]int32 `json:"edgesTo,omitempty"`
	EdgesByFile map[int32][]int32  `json:"edgesByFile,omitempty"`

	Loc map[string]map[string]LocEntry `json:"loc,omitempty"`
	// LocByFile is what Drop retracts, and only a workspace index is ever dropped from; an install cache would carry every key twice for a call it never takes.
	LocByFile map[int32][]string `json:"-"`
	// CellsByFile is the same for the learned cells a workspace file contributed; an install never drops a file, so it is never filled there.
	CellsByFile map[int32][]Cell `json:"-"`
	// LocKeysSorted is each language's keys in order, so completion binary-searches a prefix instead of filtering 300,000 keys per keystroke.
	LocKeysSorted map[string][]string `json:"locKeys,omitempty"`

	// Values is how often the corpus wrote each value for one ephemeral name, keyed `kind\x00owner\x00name`. It is folded as each file is indexed and survives compact, which drops the ephemeral records themselves.
	Values map[string]map[string]int32 `json:"values,omitempty"`
	// ValuesByFile is what Drop subtracts, and only a workspace index is ever dropped from.
	ValuesByFile map[int32]map[string]map[string]int32 `json:"-"`

	Schema   Schema           `json:"schema"`
	Declared *declared.Schema `json:"declared,omitempty"`
	Docs     Docs             `json:"docs,omitempty"`
	// Order is the origins this index holds, in load order; empty for an install.
	Order []string `json:"order,omitempty"`

	pathID   map[string]int32
	kindID   map[string]int32
	locOrder map[string]map[string]int
	data     *Index
	dataOnce sync.Once
	dataErr  atomic.Pointer[string]
}

// NewIndex is an empty index ready to Add into.
func NewIndex(gameID string) *Index {
	return &Index{
		FormatVersion: CacheFormatVersion, GameID: gameID,
		DefsByKey: map[string][]int32{}, DefsByKind: map[string][]int32{},
		DefsByFile: map[int32][]int32{}, RefsByKey: map[string][]int32{},
		RefsByFile: map[int32][]int32{}, EdgesFrom: map[string][]int32{},
		EdgesTo: map[string][]int32{}, EdgesByFile: map[int32][]int32{},
		Loc: map[string]map[string]LocEntry{}, LocByFile: map[int32][]string{},
		LocKeysSorted: map[string][]string{},
		pathID:        map[string]int32{}, kindID: map[string]int32{},
		locOrder: map[string]map[string]int{},
	}
}

// PathOf is the spelling a record's file was indexed under.
func (x *Index) PathOf(id int32) string {
	if x == nil || id < 0 || int(id) >= len(x.Paths) {
		return ""
	}
	return x.Paths[id]
}

// FileOf is what the walk knew about a record's file.
func (x *Index) FileOf(id int32) FileMeta {
	if x == nil || id < 0 || int(id) >= len(x.Files) {
		return FileMeta{}
	}
	return x.Files[id]
}

// KindOf is the kind a reference's id names.
func (x *Index) KindOf(id int32) string {
	if x == nil || id < 0 || int(id) >= len(x.Kinds) {
		return ""
	}
	return x.Kinds[id]
}

// kindOf gives a reference kind an id, adding it to the table the first time.
func (x *Index) internKind(kind string) int32 {
	if at, ok := x.kindID[kind]; ok {
		return at
	}
	at := int32(len(x.Kinds))
	x.Kinds = append(x.Kinds, kind)
	x.kindID[kind] = at
	return at
}

// PathID is the id of a path already indexed, or -1.
func (x *Index) PathID(path string) int32 {
	if x == nil {
		return -1
	}
	if id, ok := x.pathID[CanonPath(path)]; ok {
		return id
	}
	return -1
}

// intern gives a path an id, recording what the walk knew about it.
func (x *Index) intern(path, rel, origin, kind string) int32 {
	key := CanonPath(path)
	if at, ok := x.pathID[key]; ok {
		return at
	}
	at := int32(len(x.Paths))
	x.Paths = append(x.Paths, path)
	x.Files = append(x.Files, FileMeta{Rel: rel, Origin: origin, Kind: kind})
	x.pathID[key] = at
	return at
}

// Add appends one file's emissions and their posting-list entries.
func (x *Index) Add(path, rel, origin, kind string, h *FileHarvest) {
	at := x.intern(path, rel, origin, kind)
	for _, d := range h.Defs {
		d.PathID = at
		i := int32(len(x.Defs))
		x.Defs = append(x.Defs, d)
		x.DefsByKey[d.Key] = append(x.DefsByKey[d.Key], i)
		ck := jomini.CanonicalKind(d.Kind)
		x.DefsByKind[ck] = append(x.DefsByKind[ck], i)
		x.DefsByFile[at] = append(x.DefsByFile[at], i)
	}
	for _, r := range h.Refs {
		r.PathID, r.KindID = at, x.internKind(r.Kind)
		r.Kind = ""
		if r.End == r.Start+len(r.Key) {
			r.End = 0
		}
		i := int32(len(x.Refs))
		x.Refs = append(x.Refs, r)
		x.RefsByKey[r.Key] = append(x.RefsByKey[r.Key], i)
		x.RefsByFile[at] = append(x.RefsByFile[at], i)
	}
	for _, e := range h.Edges {
		e.PathID = at
		i := int32(len(x.Edges))
		x.Edges = append(x.Edges, e)
		x.EdgesFrom[e.From] = append(x.EdgesFrom[e.From], i)
		x.EdgesTo[e.To] = append(x.EdgesTo[e.To], i)
		x.EdgesByFile[at] = append(x.EdgesByFile[at], i)
	}
	for k, vals := range h.Values {
		if x.Values == nil {
			x.Values, x.ValuesByFile = map[string]map[string]int32{}, map[int32]map[string]map[string]int32{}
		}
		if x.Values[k] == nil {
			x.Values[k] = map[string]int32{}
		}
		for v, n := range vals {
			x.Values[k][v] += n
		}
	}
	if len(h.Values) > 0 {
		x.ValuesByFile[at] = h.Values
	}
	x.addLoc(at, h.Order, h.Loc)
}

// addLoc merges one file's localization under LOAD order rather than completion order: the corpus walk is parallel, so when two files of one language define the same key, whichever worker finished last used to decide which value survived.
func (x *Index) addLoc(at int32, order int, d LocDelta) {
	if d.Lang == "" || len(d.Vals) == 0 {
		return
	}
	m := x.Loc[d.Lang]
	if m == nil {
		m = map[string]LocEntry{}
		x.Loc[d.Lang] = m
	}
	ord := x.locOrder[d.Lang]
	if ord == nil {
		ord = map[string]int{}
		x.locOrder[d.Lang] = ord
	}
	for k, v := range d.Vals {
		if prev, seen := ord[k]; seen && prev > order {
			continue
		}
		v.PathID, v.Path = at, ""
		m[k] = v
		ord[k] = order
		x.LocByFile[at] = append(x.LocByFile[at], k)
	}
	x.LocKeysSorted[d.Lang] = nil
}

// HasLoc reports that this index already holds a language's localization.
func (x *Index) HasLoc(lang string) bool { return x != nil && len(x.Loc[lang]) > 0 }

// MergeLoc folds another index's localization into this one, which is how a language the scan did not read joins without a rescan.
func (x *Index) MergeLoc(src *Index) {
	if x == nil || src == nil {
		return
	}
	for lang, byKey := range src.Loc {
		dst := x.Loc[lang]
		if dst == nil {
			dst = map[string]LocEntry{}
			x.Loc[lang] = dst
		}
		for k, e := range byKey {
			e.PathID = x.intern(src.PathOf(e.PathID), "", e.Origin, "loc")
			dst[k] = e
		}
		x.LocKeysSorted[lang] = nil
	}
	x.SortLocKeys()
}

// SortLocKeys rebuilds the per-language key order completion binary-searches.
func (x *Index) SortLocKeys() {
	for lang, byKey := range x.Loc {
		if len(x.LocKeysSorted[lang]) == len(byKey) {
			continue
		}
		keys := make([]string, 0, len(byKey))
		for k := range byKey {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		x.LocKeysSorted[lang] = keys
	}
}

// Drop retracts one file's records by walking its by-file lists; a retracted record keeps its slot with PathID -1 so the flat slices never move under a live reader.
func (x *Index) Drop(at int32) {
	if at < 0 {
		return
	}
	for _, i := range x.DefsByFile[at] {
		d := &x.Defs[i]
		x.DefsByKey[d.Key] = without(x.DefsByKey[d.Key], i)
		ck := jomini.CanonicalKind(d.Kind)
		x.DefsByKind[ck] = without(x.DefsByKind[ck], i)
		d.PathID = -1
	}
	delete(x.DefsByFile, at)
	for _, i := range x.RefsByFile[at] {
		r := &x.Refs[i]
		x.RefsByKey[r.Key] = without(x.RefsByKey[r.Key], i)
		r.PathID = -1
	}
	delete(x.RefsByFile, at)
	for _, i := range x.EdgesByFile[at] {
		e := &x.Edges[i]
		x.EdgesFrom[e.From] = without(x.EdgesFrom[e.From], i)
		x.EdgesTo[e.To] = without(x.EdgesTo[e.To], i)
		e.PathID = -1
	}
	delete(x.EdgesByFile, at)
	for _, c := range x.CellsByFile[at] {
		if byKey := x.Schema.Cells[c.Kind][c.Path]; byKey != nil {
			delete(byKey, c.Key)
		}
	}
	delete(x.CellsByFile, at)
	for k, vals := range x.ValuesByFile[at] {
		for v, n := range vals {
			if x.Values[k][v] -= n; x.Values[k][v] <= 0 {
				delete(x.Values[k], v)
			}
		}
		if len(x.Values[k]) == 0 {
			delete(x.Values, k)
		}
	}
	delete(x.ValuesByFile, at)
	for _, k := range x.LocByFile[at] {
		for lang, byKey := range x.Loc {
			if v, ok := byKey[k]; ok && v.PathID == at {
				delete(byKey, k)
				delete(x.locOrder[lang], k)
			}
		}
	}
	delete(x.LocByFile, at)
	for lang := range x.LocKeysSorted {
		x.LocKeysSorted[lang] = nil
	}
}

// retract marks every definition a fixed row deletes; the compaction at the end of the scan is what removes them.
func (x *Index) retract(drop func(Def) bool) {
	hit := false
	for i := range x.Defs {
		d := &x.Defs[i]
		if d.PathID >= 0 && drop(*d) {
			d.PathID = -1
			hit = true
		}
	}
	if hit {
		x.reindexDefs()
	}
}

// compact removes every retracted record and every reference an install cache does not keep, then rebuilds each posting list. It runs once, at the end of a scan, because the file it writes should hold no holes. Of everything the rows emit, a cache keeps only the localization references and the macro invocations; the rest is re-derived against the open file.
func (x *Index) compact() {
	// An ephemeral definition is script-local; script_param stays so `$NAME$` in a vanilla macro resolves.
	for i := range x.Defs {
		if d := &x.Defs[i]; jomini.IsEphemeral(jomini.CanonicalKind(d.Kind)) &&
			jomini.CanonicalKind(d.Kind) != jomini.KindScriptParam {
			d.PathID = -1
		}
	}
	for i := range x.Refs {
		k := x.KindOf(x.Refs[i].KindID)
		if r := &x.Refs[i]; !isLocKind(k) && !jomini.IsMacroKind(k) {
			r.PathID = -1
		}
	}
	defs := x.Defs[:0]
	for _, d := range x.Defs {
		if d.PathID >= 0 {
			defs = append(defs, d)
		}
	}
	x.Defs = defs
	refs := x.Refs[:0]
	for _, r := range x.Refs {
		if r.PathID >= 0 {
			refs = append(refs, r)
		}
	}
	x.Refs = refs
	edges := x.Edges[:0]
	for _, e := range x.Edges {
		if e.PathID >= 0 {
			edges = append(edges, e)
		}
	}
	x.Edges = edges
	x.reindexDefs()
	x.reindexRefs()
	x.reindexEdges()
}

func (x *Index) reindexRefs() {
	x.RefsByKey = make(map[string][]int32, len(x.Refs)/4)
	x.RefsByFile = map[int32][]int32{}
	for i := range x.Refs {
		r := &x.Refs[i]
		if r.PathID < 0 {
			continue
		}
		x.RefsByKey[r.Key] = append(x.RefsByKey[r.Key], int32(i))
		x.RefsByFile[r.PathID] = append(x.RefsByFile[r.PathID], int32(i))
	}
}

func (x *Index) reindexEdges() {
	x.EdgesFrom = map[string][]int32{}
	x.EdgesTo = map[string][]int32{}
	x.EdgesByFile = map[int32][]int32{}
	for i := range x.Edges {
		e := &x.Edges[i]
		if e.PathID < 0 {
			continue
		}
		x.EdgesFrom[e.From] = append(x.EdgesFrom[e.From], int32(i))
		x.EdgesTo[e.To] = append(x.EdgesTo[e.To], int32(i))
		x.EdgesByFile[e.PathID] = append(x.EdgesByFile[e.PathID], int32(i))
	}
}

func without(list []int32, drop int32) []int32 {
	out := list[:0]
	for _, i := range list {
		if i != drop {
			out = append(out, i)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// defsAt materialises a posting list, skipping records a Drop retracted.
func (x *Index) defsAt(list []int32) []Def {
	if x == nil || len(list) == 0 {
		return nil
	}
	out := make([]Def, 0, len(list))
	for _, i := range list {
		if d := x.Defs[i]; d.PathID >= 0 {
			d.Path = x.Paths[d.PathID]
			out = append(out, d)
		}
	}
	return out
}

func (x *Index) refsAt(list []int32) []Ref {
	if x == nil || len(list) == 0 {
		return nil
	}
	out := make([]Ref, 0, len(list))
	for _, i := range list {
		if r := x.Refs[i]; r.PathID >= 0 {
			r.Path, r.Kind = x.Paths[r.PathID], x.KindOf(r.KindID)
			if r.End == 0 {
				r.End = r.Start + len(r.Key)
			}
			out = append(out, r)
		}
	}
	return out
}

func (x *Index) edgesAt(list []int32) []Edge {
	if x == nil || len(list) == 0 {
		return nil
	}
	out := make([]Edge, 0, len(list))
	for _, i := range list {
		if e := x.Edges[i]; e.PathID >= 0 {
			e.Path = x.Paths[e.PathID]
			out = append(out, e)
		}
	}
	return out
}

// eachDef walks every live definition; fn false stops.
func (x *Index) eachDef(fn func(Def) bool) {
	if x == nil {
		return
	}
	for _, d := range x.Defs {
		if d.PathID < 0 {
			continue
		}
		d.Path = x.Paths[d.PathID]
		if !fn(d) {
			return
		}
	}
}

// locAt is one localization entry with the file it came from filled in.
func (x *Index) locAt(lang, key string) (LocEntry, bool) {
	if x == nil {
		return LocEntry{}, false
	}
	e, ok := x.Loc[lang][key]
	if !ok {
		return LocEntry{}, false
	}
	e.Path = x.PathOf(e.PathID)
	return e, true
}

// keysOfKind is the distinct citable keys defined under one canonical kind.
func (x *Index) keysOfKind(kind string) map[string]bool {
	if x == nil {
		return nil
	}
	out := map[string]bool{}
	for _, i := range x.DefsByKind[jomini.CanonicalKind(kind)] {
		if d := x.Defs[i]; d.PathID >= 0 && IsCitableDef(d) {
			out[d.Key] = true
		}
	}
	return out
}

// citable groups the live definitions the fold and the join resolve against: kind by key, keys by kind.
func (x *Index) citable() (kindsByKey map[string][]string, keysByKind map[string]map[string]bool) {
	kindsByKey = make(map[string][]string, len(x.Defs)/2)
	keysByKind = make(map[string]map[string]bool, 64)
	for _, d := range x.Defs {
		if d.PathID < 0 || !IsCitableDef(d) {
			continue
		}
		k := jomini.CanonicalKind(d.Kind)
		set := keysByKind[k]
		if set == nil {
			set = map[string]bool{}
			keysByKind[k] = set
		}
		// One membership test dedupes both maps: the key is new for this kind exactly when the kind is new for this key.
		if !set[d.Key] {
			set[d.Key] = true
			kindsByKey[d.Key] = append(kindsByKey[d.Key], k)
		}
	}
	return kindsByKey, keysByKind
}

// macroDefs is every scripted_* definition key with its kind; a keyed statement naming one is a call.
func (x *Index) macroDefs() map[string]string {
	out := map[string]string{}
	x.eachDef(func(d Def) bool {
		// A call is the invocation of a scripted_* macro, which the folder name states outright. Deriving it from observed usage elected `subtract` and the GUI value `right|top`.
		if k := jomini.CanonicalKind(d.Kind); jomini.IsMacroKind(k) {
			out[d.Key] = k
		}
		return true
	})
	return out
}

// ------------------------------------------------------------- the data tier

// splitData moves the databases a game defines once per instance -- history, map, dynasty and portrait tables -- into an index of their own. On CK3 they are 60% of the model by def count and almost none of what an editor ever asks about.
func splitData(x *Index) *Index {
	if !hasDataKinds(x.GameID) {
		return nil
	}
	data := NewIndex(x.GameID)
	data.FormatVersion, data.InstallID, data.GameVersion = CacheFormatVersion, x.InstallID, x.GameVersion
	data.Paths, data.Files, data.pathID = x.Paths, x.Files, x.pathID
	kept := x.Defs[:0]
	for _, d := range x.Defs {
		if !game.IsDataKind(x.GameID, d.Kind) {
			kept = append(kept, d)
			continue
		}
		// A row keeps its identity and its site; the rest describes script.
		d.Doc, d.Value, d.OwnerKey, d.Nested = "", "", "", false
		i := int32(len(data.Defs))
		data.Defs = append(data.Defs, d)
		data.DefsByKey[d.Key] = append(data.DefsByKey[d.Key], i)
		ck := jomini.CanonicalKind(d.Kind)
		data.DefsByKind[ck] = append(data.DefsByKind[ck], i)
		data.DefsByFile[d.PathID] = append(data.DefsByFile[d.PathID], i)
	}
	x.Defs = kept
	x.reindexDefs()
	return data
}

func hasDataKinds(gameID string) bool {
	g := game.Get(gameID)
	return g != nil && len(g.DataKinds) > 0
}

// reindexDefs rebuilds the definition posting lists after the data tier is split out; it runs once per scan, never on load.
func (x *Index) reindexDefs() {
	x.DefsByKey = make(map[string][]int32, len(x.Defs)/2)
	x.DefsByKind = map[string][]int32{}
	x.DefsByFile = map[int32][]int32{}
	for i := range x.Defs {
		d := &x.Defs[i]
		if d.PathID < 0 {
			continue
		}
		ck := jomini.CanonicalKind(d.Kind)
		x.DefsByKey[d.Key] = append(x.DefsByKey[d.Key], int32(i))
		x.DefsByKind[ck] = append(x.DefsByKind[ck], int32(i))
		x.DefsByFile[d.PathID] = append(x.DefsByFile[d.PathID], int32(i))
	}
}

// dataTier indexes the data rows once, from the scan that made them or from disk; sync.Once serialises the first load and orders every later reader after the finished index, so it needs no lock of its own under the session's read lock.
func (x *Index) dataTier() *Index {
	if x == nil {
		return nil
	}
	x.dataOnce.Do(func() {
		if x.data != nil || x.InstallID == "" {
			return
		}
		loaded, err := LoadData(x.InstallID, x.GameVersion)
		if err != nil {
			msg := err.Error()
			x.dataErr.Store(&msg)
			return
		}
		x.data = loaded
	})
	return x.data
}

// DataError reports why the data tier could not be read, empty when it could.
func (x *Index) DataError() string {
	if x == nil {
		return ""
	}
	if msg := x.dataErr.Load(); msg != nil {
		return *msg
	}
	return ""
}
