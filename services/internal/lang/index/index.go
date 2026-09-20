// index.go is the one container: the records a scan emits, the posting lists
// every query reads, the localization table, the learned schema, the declared
// schema and the shipped prose. It is the cache file byte for byte, and the
// workspace index too, built by the same passes at session open. Which
// definition of a key wins (LIOS, with per-game FIOS) is decided here as well.

package index

import (
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
)

// CacheFormatVersion is the on-disk schema of an Index; a mismatch means rescan.
const CacheFormatVersion = 56

// Def is one named object. OwnerKey is the enclosing scripted macro for script_param defs; Value is the harvested RHS at a script-name def site.
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
	Nested   bool   `json:"n,omitempty"` // harvested inside another definition
	Local    bool   `json:"i,omitempty"` // declared inline (`scripted_trigger foo = {…}`), so a repeat is required, not a defect
	Doc      string `json:"d,omitempty"` // the comment block written directly above it
	Path     string `json:"-"`
}

// IsCitableDef reports a definition whose key names a real object.
func IsCitableDef(d Def) bool {
	k := jomini.CanonicalKind(d.Kind)
	return k != "" && k != jomini.KindLocKey && d.Key != "" && !jomini.IsEphemeral(k)
}

// Ref is a use-site. Kind is "loc", "loc-broad", "event", "on_action", or an ephemeral or script kind.
type Ref struct {
	Key      string `json:"k"`
	KindID   int32  `json:"t"`
	PathID   int32  `json:"p"`
	Line     int    `json:"l"`
	Start    int    `json:"s"`
	End      int    `json:"e,omitempty"` // written only where it is not Start plus the key's length
	OwnerKey string `json:"w,omitempty"`
	Derived  bool   `json:"d,omitempty"` // the kind came from the learned schema, not from something the game declares
	Kind     string `json:"-"`
	Path     string `json:"-"`
}

// Edge is a directed event-graph link; Kind "call" is a scripted-effect invocation, "via" a hop synthesized through a chain of them.
type Edge struct {
	From     string `json:"f"`
	To       string `json:"o"`
	Via      string `json:"v"`
	PathID   int32  `json:"p"`
	Line     int    `json:"l"`
	Kind     string `json:"t,omitempty"`
	NameKey  string `json:"n,omitempty"`
	Rescoped bool   `json:"r,omitempty"` // fired under a block that changed scope
	Path     string `json:"-"`
}

const (
	EdgeKindCall = "call"
	EdgeKindVia  = "via"
)

// LocEntry is one localization value with its source file and line.
type LocEntry struct {
	Value  string `json:"v"`
	PathID int32  `json:"p"`
	Line   int    `json:"l"`
	Start  int    `json:"s,omitempty"`
	Origin string `json:"o,omitempty"`
	Path   string `json:"-"`
}

// LocDelta is the loc payload one file contributes.
type LocDelta struct {
	Lang string
	Vals map[string]LocEntry
}

// FileMeta is what a path is, recorded when the file was indexed.
type FileMeta struct {
	Rel    string `json:"rel,omitempty"`
	Origin string `json:"origin,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

// Docs is the prose and declared types the install ships beside its script, addressed by (structure, field).
type Docs struct {
	FieldInfo                map[string]string                           `json:"fieldInfo,omitempty"`
	Fields                   map[string]map[string]string                `json:"fields,omitempty"`
	KindInfo                 map[string]string                           `json:"kindInfo,omitempty"`
	DeclaredFieldTypes       map[string]declared.DeclaredType            `json:"declaredFieldTypes,omitempty"`
	DeclaredFieldTypesByKind map[string]map[string]declared.DeclaredType `json:"declaredFieldTypesByKind,omitempty"`
}

// ModInput is one workspace mod: Origin id, Root folder, load Order, display Name.
type ModInput struct {
	Origin   string
	Root     string
	Order    int
	Name     string
	Calendar ModCalendar
}

// ModCalendar is how a mod's date literals map to the era its players see; stated by a person, never guessed.
type ModCalendar struct {
	Era         string `json:"era,omitempty"`
	EpochOffset int    `json:"epochOffset,omitempty"`
}

func (c ModCalendar) Declared() bool { return c.Era != "" || c.EpochOffset != 0 }

// EraDate renders a Clausewitz date in the mod's era; empty for an undeclared calendar, a non-date, or a year the offset pushes to zero or below.
func EraDate(date string, cal ModCalendar) string {
	if !cal.Declared() {
		return ""
	}
	parts := strings.Split(date, ".")
	if len(parts) < 3 || len(parts) > 4 {
		return ""
	}
	year, err := strconv.Atoi(parts[0])
	era := year - cal.EpochOffset
	if err != nil || era <= 0 {
		return ""
	}
	out := strconv.Itoa(era) + "." + strings.Join(parts[1:], ".")
	if cal.Era != "" {
		out += " " + cal.Era
	}
	return out
}

// Harvest is everything one file emits.
type Harvest struct {
	Path, Rel, Origin, Kind string
	Order                   int
	Defs                    []Def
	Refs                    []Ref
	Edges                   []Edge
	Loc                     LocDelta
	// Values is this file's slice of the ephemeral-value histogram, keyed `kind\x00owner\x00name`.
	Values map[string]map[string]int32
}

// note folds one observed value for an ephemeral name into the histogram, under its owner and again under no owner.
func (h *Harvest) note(kind, owner, name, value string) {
	if name == "" || value == "" {
		return
	}
	if h.Values == nil {
		h.Values = map[string]map[string]int32{}
	}
	for _, at := range []string{owner, ""} {
		k := kind + "\x00" + at + "\x00" + name
		if h.Values[k] == nil {
			h.Values[k] = map[string]int32{}
		}
		h.Values[k][value]++
		if owner == "" {
			return
		}
	}
}

// Index is what the passes emit into, what the cache file is, and what a query reads.
type Index struct {
	FormatVersion int    `json:"formatVersion"`
	InstallID     string `json:"installId,omitempty"`
	GameID        string `json:"gameId,omitempty"`
	InstallPath   string `json:"installPath,omitempty"`
	GameVersion   string `json:"gameVersion,omitempty"`
	ScannedAt     string `json:"scannedAt,omitempty"`

	Paths []string   `json:"paths,omitempty"`
	Files []FileMeta `json:"files,omitempty"`
	Kinds []string   `json:"kinds,omitempty"` // the reference kind table a Ref indexes
	Defs  []Def      `json:"defs,omitempty"`
	Refs  []Ref      `json:"refs,omitempty"`
	Edges []Edge     `json:"edges,omitempty"`

	DefsByKey   map[string][]int32 `json:"defsByKey,omitempty"`
	DefsByKind  map[string][]int32 `json:"defsByKind,omitempty"`
	DefsByFile  map[int32][]int32  `json:"defsByFile,omitempty"`
	RefsByKey   map[string][]int32 `json:"refsByKey,omitempty"`
	RefsByFile  map[int32][]int32  `json:"refsByFile,omitempty"`
	EdgesFrom   map[string][]int32 `json:"edgesFrom,omitempty"`
	EdgesTo     map[string][]int32 `json:"edgesTo,omitempty"`
	EdgesByFile map[int32][]int32  `json:"edgesByFile,omitempty"`

	Loc           map[string]map[string]LocEntry `json:"loc,omitempty"`
	LocKeysSorted map[string][]string            `json:"locKeys,omitempty"` // per language, so completion binary-searches a prefix
	// Values is how often the corpus wrote each value for one ephemeral name; it survives compact, which drops the ephemeral records themselves.
	Values map[string]map[string]int32 `json:"values,omitempty"`

	Tree     Tree             `json:"tree"`
	Declared *declared.Schema `json:"declared,omitempty"`
	Docs     Docs             `json:"docs,omitempty"`
	Order    []string         `json:"order,omitempty"` // origins in load order; empty for an install

	// cites is every `prefix:id` a workspace's files wrote, so a file re-indexed alone still joins the rows the rest of the mod cites.
	cites map[string]string
	// The by-file retractions only a workspace takes: an install never drops a file.
	locByFile    map[int32][]string
	valuesByFile map[int32]map[string]map[string]int32
	nodesByFile  map[int32][]kid
	pathID       map[string]int32
	kindID       map[string]int32
	locOrder     map[string]map[string]int
	data         *Index
	dataOnce     sync.Once
	dataErr      atomic.Pointer[string]
}

// kid is one learned node a workspace file added, for Drop.
type kid struct {
	p    *Node
	name string
}

// NewIndex is an empty index ready to Add into.
func NewIndex(gameID string, decl *declared.Schema) *Index {
	x := &Index{
		FormatVersion: CacheFormatVersion, GameID: gameID, Declared: decl,
		DefsByKey: map[string][]int32{}, DefsByKind: map[string][]int32{},
		DefsByFile: map[int32][]int32{}, RefsByKey: map[string][]int32{},
		RefsByFile: map[int32][]int32{}, EdgesFrom: map[string][]int32{},
		EdgesTo: map[string][]int32{}, EdgesByFile: map[int32][]int32{},
		Loc: map[string]map[string]LocEntry{}, LocKeysSorted: map[string][]string{},
	}
	x.wake()
	return x
}

// wake rebuilds the lookups a loaded index does not store.
func (x *Index) wake() {
	x.pathID = make(map[string]int32, len(x.Paths))
	for i, p := range x.Paths {
		x.pathID[CanonPath(p)] = int32(i)
	}
	x.kindID = make(map[string]int32, len(x.Kinds))
	for i, k := range x.Kinds {
		x.kindID[k] = int32(i)
	}
	x.locOrder = map[string]map[string]int{}
	x.locByFile = map[int32][]string{}
	x.valuesByFile = map[int32]map[string]map[string]int32{}
	x.nodesByFile = map[int32][]kid{}
	x.cites = map[string]string{}
	if x.Loc == nil {
		x.Loc = map[string]map[string]LocEntry{}
	}
	if x.LocKeysSorted == nil {
		x.LocKeysSorted = map[string][]string{}
	}

	// A loaded index without a dump still answers every declared question: empty maps, never a nil pointer.
	if x.Declared == nil {
		x.Declared = &declared.Schema{}
	}
	x.Tree.gameID, x.Tree.decl = x.GameID, x.Declared
	x.SortLocKeys()
}

func (x *Index) PathOf(id int32) string {
	if x == nil || id < 0 || int(id) >= len(x.Paths) {
		return ""
	}
	return x.Paths[id]
}

func (x *Index) FileOf(id int32) FileMeta {
	if x == nil || id < 0 || int(id) >= len(x.Files) {
		return FileMeta{}
	}
	return x.Files[id]
}

func (x *Index) KindOf(id int32) string {
	if x == nil || id < 0 || int(id) >= len(x.Kinds) {
		return ""
	}
	return x.Kinds[id]
}

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
func (x *Index) Add(h *Harvest) {
	at := x.intern(h.Path, h.Rel, h.Origin, h.Kind)
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
			x.Values = map[string]map[string]int32{}
		}
		if x.Values[k] == nil {
			x.Values[k] = map[string]int32{}
		}
		for v, n := range vals {
			x.Values[k][v] += n
		}
	}
	if len(h.Values) > 0 {
		x.valuesByFile[at] = h.Values
	}
	x.addLoc(at, h.Order, h.Loc)
}

// addLoc merges one file's localization under load order, since the corpus walk is parallel and completion order is arbitrary.
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
		x.locByFile[at] = append(x.locByFile[at], k)
	}
	x.LocKeysSorted[d.Lang] = nil
}

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
		x.LocKeysSorted[lang] = slices.Sorted(mapKeys(byKey))
	}
}

func mapKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// Drop retracts one file's records; a retracted record keeps its slot with PathID -1 so the flat slices never move under a live reader.
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
	for _, k := range x.nodesByFile[at] {
		delete(k.p.Kids, k.name)
	}
	delete(x.nodesByFile, at)
	for k, vals := range x.valuesByFile[at] {
		for v, n := range vals {
			if x.Values[k][v] -= n; x.Values[k][v] <= 0 {
				delete(x.Values[k], v)
			}
		}
		if len(x.Values[k]) == 0 {
			delete(x.Values, k)
		}
	}
	delete(x.valuesByFile, at)
	for _, k := range x.locByFile[at] {
		for lang, byKey := range x.Loc {
			if v, ok := byKey[k]; ok && v.PathID == at {
				delete(byKey, k)
				delete(x.locOrder[lang], k)
			}
		}
	}
	delete(x.locByFile, at)
	for lang := range x.LocKeysSorted {
		x.LocKeysSorted[lang] = nil
	}
}

// retract marks every definition a rule deletes; compact removes them.
func (x *Index) retract(drop func(Def) bool) {
	hit := false
	for i := range x.Defs {
		if d := &x.Defs[i]; d.PathID >= 0 && drop(*d) {
			d.PathID = -1
			hit = true
		}
	}
	if hit {
		x.reindex()
	}
}

// compact removes every retracted record and every reference an install cache does not keep: of everything the passes emit, a cache keeps the localization references and the macro invocations; the rest is re-derived against the open file.
func (x *Index) compact() {
	for i := range x.Defs {
		if k := jomini.CanonicalKind(x.Defs[i].Kind); jomini.IsEphemeral(k) && k != jomini.KindScriptParam {
			x.Defs[i].PathID = -1
		}
	}
	for i := range x.Refs {
		if k := x.KindOf(x.Refs[i].KindID); !isLocKind(k) && !jomini.IsMacroKind(k) {
			x.Refs[i].PathID = -1
		}
	}
	x.Defs = slices.DeleteFunc(x.Defs, func(d Def) bool { return d.PathID < 0 })
	x.Refs = slices.DeleteFunc(x.Refs, func(r Ref) bool { return r.PathID < 0 })
	x.Edges = slices.DeleteFunc(x.Edges, func(e Edge) bool { return e.PathID < 0 })
	x.reindex()
}

// reindex rebuilds every posting list from the flat records.
func (x *Index) reindex() {
	x.DefsByKey, x.DefsByKind, x.DefsByFile = make(map[string][]int32, len(x.Defs)/2), map[string][]int32{}, map[int32][]int32{}
	for i := range x.Defs {
		if d := &x.Defs[i]; d.PathID >= 0 {
			ck := jomini.CanonicalKind(d.Kind)
			x.DefsByKey[d.Key] = append(x.DefsByKey[d.Key], int32(i))
			x.DefsByKind[ck] = append(x.DefsByKind[ck], int32(i))
			x.DefsByFile[d.PathID] = append(x.DefsByFile[d.PathID], int32(i))
		}
	}
	x.RefsByKey, x.RefsByFile = make(map[string][]int32, len(x.Refs)/4), map[int32][]int32{}
	for i := range x.Refs {
		if r := &x.Refs[i]; r.PathID >= 0 {
			x.RefsByKey[r.Key] = append(x.RefsByKey[r.Key], int32(i))
			x.RefsByFile[r.PathID] = append(x.RefsByFile[r.PathID], int32(i))
		}
	}
	x.EdgesFrom, x.EdgesTo, x.EdgesByFile = map[string][]int32{}, map[string][]int32{}, map[int32][]int32{}
	for i := range x.Edges {
		if e := &x.Edges[i]; e.PathID >= 0 {
			x.EdgesFrom[e.From] = append(x.EdgesFrom[e.From], int32(i))
			x.EdgesTo[e.To] = append(x.EdgesTo[e.To], int32(i))
			x.EdgesByFile[e.PathID] = append(x.EdgesByFile[e.PathID], int32(i))
		}
	}
}

func without(list []int32, drop int32) []int32 {
	out := slices.DeleteFunc(list, func(i int32) bool { return i == drop })
	if len(out) == 0 {
		return nil
	}
	return out
}

// ----------------------------------------------------------------- the reads

// DefsAt materialises a posting list, skipping retracted records.
func (x *Index) DefsAt(list []int32) []Def {
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

func (x *Index) RefsAt(list []int32) []Ref {
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

func (x *Index) EdgesAt(list []int32) []Edge {
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

// Live is how many definitions have not been retracted.
func (x *Index) Live() int {
	n := 0
	for _, d := range x.Defs {
		if d.PathID >= 0 {
			n++
		}
	}
	return n
}

// EachDef walks every live definition; fn false stops.
func (x *Index) EachDef(fn func(Def) bool) {
	if x == nil {
		return
	}
	for _, d := range x.Defs {
		if d.PathID >= 0 {
			d.Path = x.Paths[d.PathID]
			if !fn(d) {
				return
			}
		}
	}
}

// LocAt is one localization entry with its file filled in.
func (x *Index) LocAt(lang, key string) (LocEntry, bool) {
	if x == nil {
		return LocEntry{}, false
	}
	e, ok := x.Loc[lang][key]
	if ok {
		e.Path = x.PathOf(e.PathID)
	}
	return e, ok
}

// LocKeys are up to limit keys starting with prefix, by binary search over each language's order.
func (x *Index) LocKeys(prefix string, limit int, seen map[string]bool, out []string) []string {
	lp := strings.ToLower(prefix)
	for _, keys := range x.LocKeysSorted {
		for i := sort.SearchStrings(keys, prefix); i < len(keys) && len(out) < limit; i++ {
			k := keys[i]
			if !strings.HasPrefix(strings.ToLower(k), lp) {
				if !strings.HasPrefix(k, prefix) {
					break
				}
				continue
			}
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}

// citable groups the live definitions the fold and the join resolve against: kinds by key, keys by kind.
func (x *Index) citable(into map[string][]string, byKind map[string]map[string]bool) {
	for _, d := range x.Defs {
		if d.PathID < 0 || !IsCitableDef(d) {
			continue
		}
		k := jomini.CanonicalKind(d.Kind)
		set := byKind[k]
		if set == nil {
			set = map[string]bool{}
			byKind[k] = set
		}
		if !set[d.Key] {
			set[d.Key] = true
			into[d.Key] = append(into[d.Key], k)
		}
	}
}

// macros is every scripted_* definition key with its kind; a keyed statement naming one is a call.
func (x *Index) macros(into map[string]string) {
	x.EachDef(func(d Def) bool {
		if k := jomini.CanonicalKind(d.Kind); jomini.IsMacroKind(k) {
			into[d.Key] = k
		}
		return true
	})
}

func isLocKind(kind string) bool { return kind == "loc" || kind == "loc-broad" }

// ------------------------------------------------------------- the data tier

// splitData moves the databases a game defines once per instance -- history, map, dynasty and portrait tables -- into an index of their own: on CK3 they are 60% of the model by def count and almost none of what an editor asks about.
func splitData(x *Index) *Index {
	g := game.Get(x.GameID)
	if g == nil || len(g.DataKinds) == 0 {
		return nil
	}
	data := NewIndex(x.GameID, nil)
	data.InstallID, data.GameVersion = x.InstallID, x.GameVersion
	data.Paths, data.Files, data.pathID = x.Paths, x.Files, x.pathID
	kept := x.Defs[:0]
	for _, d := range x.Defs {
		if !game.IsDataKind(x.GameID, d.Kind) {
			kept = append(kept, d)
			continue
		}
		d.Doc, d.OwnerKey, d.Nested = "", "", false
		data.Defs = append(data.Defs, d)
	}
	x.Defs = kept
	x.reindex()
	data.reindex()
	return data
}

// DataTier indexes the data rows once, from disk; sync.Once serialises the first load and orders every later reader after it.
func (x *Index) DataTier() *Index {
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
	if x != nil {
		if msg := x.dataErr.Load(); msg != nil {
			return *msg
		}
	}
	return ""
}

// ------------------------------------------------------------------- paths

// CanonPath is a cleaned path for index and buffer identity, lowercased on Windows; never for a value anything displays or opens.
func CanonPath(p string) string {
	if p == "" {
		return p
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// SamePath reports whether a and b name the same file.
func SamePath(a, b string) bool { return CanonPath(a) == CanonPath(b) }

// RelPath reports whether file is inside root and returns the relative path.
func RelPath(root, file string) (rel string, ok bool) {
	r, err := filepath.Rel(CanonPath(root), CanonPath(file))
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	if r == "." {
		return r, true
	}

	// The caller's spelling, not the canonical one: the relative path is displayed.
	return filepath.Clean(file)[len(filepath.Clean(root))+1:], true
}

// ---------------------------------------------------------------- overrides

// Contest is one contested (kind, key).
type Contest struct {
	Kind     string
	Name     string
	Rule     string // "FIOS" | "LIOS"
	Winner   string
	Overlay  bool
	ModCount int
	Defs     []Def
}

// Winner picks the effective definition; order is origin → rank, and vanilla always loses. FIOS/LIOS is a property of the kind, so each kind is reduced under its own rule and the per-kind winners are compared by load order.
func Winner(gameID string, defs []Def, order map[string]int) *Def {
	var vanilla, best *Def
	byKind := map[string][]Def{}
	for i := range defs {
		if defs[i].Origin == "" {
			vanilla = &defs[i]
			continue
		}
		byKind[defs[i].Kind] = append(byKind[defs[i].Kind], defs[i])
	}
	for _, kind := range slices.Sorted(mapKeys(byKind)) {
		mods, fios := byKind[kind], game.IsFIOS(gameID, kind)
		b := 0
		for i := 1; i < len(mods); i++ {
			ri, rb := order[mods[i].Origin], order[mods[b].Origin]
			if fios && ri < rb || !fios && ri >= rb {
				b = i
			}
		}
		if best == nil || order[mods[b].Origin] > order[best.Origin] {
			w := mods[b]
			best = &w
		}
	}
	if best == nil {
		return vanilla
	}
	return best
}

func skipOverrideKind(t string) bool {
	return t == "loc_key" || t == "mod_descriptor" || t == "namespace" || jomini.IsEphemeral(t)
}

// Contests returns override rows from workspace and vanilla defs: mod-vs-mod, and a single mod overlaying vanilla.
func Contests(gameID string, workspace, vanilla []Def, order []string) []Contest {
	orderMap := OrderMap(order)
	by := map[string][]Def{}
	for _, d := range workspace {
		if !skipOverrideKind(d.Kind) {
			by[d.Kind+"\x00"+d.Key] = append(by[d.Kind+"\x00"+d.Key], d)
		}
	}
	for _, d := range vanilla {
		if k := d.Kind + "\x00" + d.Key; by[k] != nil && !skipOverrideKind(d.Kind) {
			by[k] = append(by[k], d)
		}
	}
	var rows []Contest
	for _, all := range by {
		modOrigins, hasVanilla := map[string]bool{}, false
		for _, d := range all {
			if d.Origin == "" {
				hasVanilla = true
			} else {
				modOrigins[d.Origin] = true
			}
		}
		overlay := len(modOrigins) == 1 && hasVanilla
		w := Winner(gameID, all, orderMap)
		if len(modOrigins) < 2 && !overlay || w == nil {
			continue
		}
		rule := "LIOS"
		if game.IsFIOS(gameID, w.Kind) {
			rule = "FIOS"
		}
		rows = append(rows, Contest{Kind: w.Kind, Name: w.Key, Rule: rule, Winner: w.Origin,
			Overlay: overlay, ModCount: len(modOrigins), Defs: all})
	}
	return rows
}

// OrderMap turns a load-order slice into origin → rank.
func OrderMap(order []string) map[string]int {
	m := make(map[string]int, len(order))
	for i, o := range order {
		m[o] = i
	}
	return m
}
