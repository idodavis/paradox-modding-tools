// cells.go is the learned half of a game: one cell per (file kind, block path,
// key), the exact record of what every value at that cell turned out to be, and
// the three questions a consumer asks of it. A type is not voted -- it is the
// union of what resolution found, so there is no floor, no coverage and no
// minimum-hits anywhere in this file. The one judgment is the reporting policy,
// which lives in lsp.

package derived

import (
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// Cell is one position in a game's script: file kind, enclosing block path, key. Path is the keys of the enclosing blocks below the definition's own key, each folded to its class unless it is a plain lower-case identifier that names nothing.
type Cell struct{ Kind, Path, Key string }

// Label is what one value at a cell turned out to be; Def and Cite carry the kind after the colon. An event is Def("event") like any other database, so the lattice's Event is not spelled separately.
type Label = string

const (
	LabelBlock      Label = "block:names" // a block holding keyed children
	LabelList       Label = "block:list"  // a block of bare members
	LabelEmpty      Label = "block:empty"
	LabelLiteral    Label = "literal"
	LabelLoc        Label = "loc"
	LabelStop       Label = "stop"
	LabelUnresolved Label = "unresolved"
	// LabelDef and LabelCite prefix the kind a value named, as a definition of it or as a `prefix:id` citation of one.
	LabelDef  Label = "def:"
	LabelCite Label = "cite:"
)

// path classes: a segment is kept verbatim only when it is a lower-case identifier that names nothing.
const (
	segNumber = "<n>"
	segDate   = "<date>"
	segCite   = "<cite>"
	segKind   = "<kind>"
	segBare   = "<bare>"
	segVar    = "<var>"
	// segTop is the definition's own level; sharing the empty path with its members made every trait a member of traits.
	segTop = "<top>"
)

// CellType is the exact record of a cell across the install; the per-label counts are the fold's working state, not the file's.
type CellType struct {
	N int32 `json:"n"`
	// Kinds are the Def kinds present in every resolvable occurrence (the intersection); empty when the corpus disagrees.
	Kinds []string `json:"k,omitempty"`
	// Flags are the labels a consumer reads as a yes or no.
	Flags uint8 `json:"f,omitempty"`
	// Names reports block occurrences holding keyed children (a database) rather than a list.
	Names bool `json:"names,omitempty"`
	// Def is the nested definition kind this cell's block keys declare, set by the join.
	Def string `json:"def,omitempty"`

	// labels, inter and seen are the fold's working state; settle turns them into Flags and Kinds.
	labels map[string]int32
	inter  []string
	seen   bool
}

// The bits of CellType.Flags: the labels a consumer reads as a yes or no.
const (
	CellLiteral    uint8 = 1 << iota // a quantity, a date, a flag or a quoted string was written here
	CellUnresolved                   // a name nothing explains was written here
	CellBlock                        // the label the cell wears most often is a block
)

// Has reports one of the three labels a settled cell keeps; every other label is a count the fold spent and did not store.
func (t *CellType) Has(l Label) bool {
	if t == nil {
		return false
	}
	switch l {
	case LabelLiteral:
		return t.Flags&CellLiteral != 0
	case LabelUnresolved:
		return t.Flags&CellUnresolved != 0
	case LabelLoc:
		return slices.Contains(t.Kinds, LabelLoc)
	}
	return false
}

// IsBlock reports a cell usually written as a block.
func (t *CellType) IsBlock() bool { return t != nil && t.Flags&CellBlock != 0 }

// KindInfo is what a whole database says about itself rather than about one of its cells.
type KindInfo struct {
	// Loc are the key templates this kind's definitions are localized under, each with its exact coverage.
	Loc []loc.Convention `json:"loc,omitempty"`
}

// Schema is the learned half of a game plus the projections the editor reads; the declared half stays *declared.Schema on the Index.
type Schema struct {
	// Cells is kind -> folded path -> key -> type. Nested rather than flat because the three levels are what every lookup walks and what keeps one copy of each kind and path string in the file.
	Cells  map[string]map[string]map[string]*CellType `json:"cells,omitempty"`
	Kinds  map[string]KindInfo                        `json:"kinds,omitempty"`
	Nested []NestedShape                              `json:"nested,omitempty"`
	// Prefix is the declared citation prefix bound to a harvested database, and KindScope the scope type each database holds.
	Prefix    map[string]string `json:"prefix,omitempty"`
	KindScope map[string]string `json:"kindScope,omitempty"`
	// KindPrefix reverses Prefix, and ScopePrefix/ScopeKind answer "a token takes a <scope type> -- how does the user write one?".
	KindPrefix  map[string]string `json:"kindPrefix,omitempty"`
	ScopePrefix map[string]string `json:"scopePrefix,omitempty"`
	ScopeKind   map[string]string `json:"scopeKind,omitempty"`
	// MacroDefs is every scripted_* definition key with its kind, which is what makes a keyed statement a call.
	MacroDefs  map[string]string `json:"macroDefs,omitempty"`
	Effects    []string          `json:"effects,omitempty"`
	Triggers   []string          `json:"triggers,omitempty"`
	EffectSet  map[string]bool   `json:"effectSet,omitempty"`
	TriggerSet map[string]bool   `json:"triggerSet,omitempty"`
	// VocabNames is the completion list: every key the corpus writes plus every name the game declares.
	VocabNames    []string `json:"vocabNames,omitempty"`
	GUITypes      []string `json:"guiTypes,omitempty"`
	GUIProps      []string `json:"guiProps,omitempty"`
	DataFunctions []string `json:"dataFunctions,omitempty"`
	// LocKeyPrefixes build one loc key out of another (EU5's NOT_<key>).
	LocKeyPrefixes []string `json:"locKeyPrefixes,omitempty"`
	// LocEngineSlots are `$NAME$` interpolations the game's loc uses but never defines; demanding one from a mod is a false "missing localization".
	LocEngineSlots map[string]bool `json:"locEngineSlots,omitempty"`
}

// TypeOf is what the corpus saw at one cell, or nil.
func (s *Schema) TypeOf(c Cell) *CellType {
	if s == nil {
		return nil
	}
	return s.Cells[c.Kind][c.Path][c.Key]
}

// Members are the keys written at one path, most-used first: the kind's structure at the empty path, a block's members below it.
func (s *Schema) Members(kind, path string) []string {
	if s == nil {
		return nil
	}
	at := s.Cells[kind][path]
	if len(at) == 0 {
		return nil
	}
	out := make([]string, 0, len(at))
	for k := range at {
		// A folded class is a position, not a name a reader can type.
		if k != "" && k[0] != '<' && at[k].N > 0 {
			out = append(out, k)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		if d := at[b].N - at[a].N; d != 0 {
			return int(d)
		}
		return strings.Compare(a, b)
	})
	return out
}

// ------------------------------------------------------------------ the fold

// foldKey folds the key half: the positional classes, and <kind> for a keyed BLOCK naming a definition -- never a scalar field, since folding `characters|name` cost 70,601 localization references.
func foldKey(gameID, key string, block, quoted bool, isDef func(string) bool) string {
	if key == "" || quoted {
		return key
	}
	switch {
	case isDigitKey(key):
		return segNumber
	case jomini.IsDateLiteral(key):
		return segDate
	case strings.ContainsAny(key, "$@"):
		return segVar
	}
	if _, _, ok := game.ParseTyped(gameID, key); ok {
		return segCite
	}
	if _, ok := jomini.ParsePrefixed(key); ok {
		return segCite
	}
	if block && !jomini.IsStop(key) && isDef(game.KeyIdentity(gameID, key)) {
		return segKind
	}
	return key
}

// foldSeg is §2's path fold: a segment is kept as written only when it is a lower-case identifier that is not a definition key of any kind.
func foldSeg(gameID, raw string, quoted bool, isDef func(string) bool) string {
	if raw == "" {
		return segBare
	}
	if quoted {
		return segVar
	}
	if strings.ContainsAny(raw, "$@") {
		return segVar
	}
	if isDigitKey(raw) {
		return segNumber
	}
	if jomini.IsDateLiteral(raw) {
		return segDate
	}
	if _, _, ok := game.ParseTyped(gameID, raw); ok {
		return segCite
	}
	if _, ok := jomini.ParsePrefixed(raw); ok {
		return segCite
	}
	name := game.KeyIdentity(gameID, raw)
	if isDef(name) {
		return segKind
	}
	if jomini.IsLowerName(name) {
		return name
	}
	// A name that is neither a lower-case token nor a class above names something the corpus has not harvested; keeping it verbatim is what made CK3 898,774 cells.
	return segKind
}

// ------------------------------------------------------------ the classifier

// lookup is everything outside a file that classifying one value consults: the def index, the localization table, the declared prefixes and the game.
type lookup struct {
	gameID     string
	kindsByKey map[string][]string
	locKeys    map[string]bool
	prefix     map[string]string
	schema     *declared.Schema
	// cells are the schemas a cell is read from, the install's before the workspace's own.
	cells []*Schema
}

// cell is the type at one position: the install's answer where it has one, the workspace's where the install never wrote there.
func (t *lookup) cell(c Cell) *CellType {
	for _, s := range t.cells {
		if at := s.TypeOf(c); at != nil {
			return at
		}
	}
	return nil
}

// isDef reports a name some definition already explains, which is what the path fold and the nested join both test.
func (t *lookup) isDef(name string) bool { return len(t.kindsByKey[name]) > 0 }

// classify is §2 by lookup: one occurrence, one label set. A literal carries only Literal -- except a number in a game.DataKinds cell, which may also name a row.
func (t *lookup) classify(v string, quoted, dataKind bool, out []string) []string {
	out = out[:0]
	if v == "" {
		return append(out, LabelLiteral)
	}
	if !quoted && jomini.IsStop(v) {
		return append(out, LabelStop)
	}
	if !quoted {
		if kind, _, ok := game.ParseTyped(t.gameID, v); ok {
			if k := t.prefix[strings.SplitN(v, ":", 2)[0]]; k != "" {
				kind = k
			}
			return append(out, LabelCite+jomini.CanonicalKind(kind))
		}
		if _, ok := jomini.ParsePrefixed(v); ok {
			return append(out, LabelCite+jomini.PrefixKind(strings.SplitN(v, ":", 2)[0]))
		}
	}
	if literalShape(v, quoted) {
		// A quoted value is still a localization key; a numeric one still names a data row.
		if t.locKeys[v] {
			out = append(out, LabelLoc)
		}
		if dataKind && isDigitKey(v) {
			for _, k := range t.kindsByKey[v] {
				out = append(out, LabelDef+k)
			}
		}
		return append(out, LabelLiteral)
	}
	for _, k := range t.kindsByKey[v] {
		out = append(out, LabelDef+k)
	}
	if t.locKeys[v] {
		out = append(out, LabelLoc)
	}
	if len(out) > 0 {
		return out
	}
	if loc.LooksLikeKey(v) || jomini.IsDefName(v) {
		return append(out, LabelUnresolved)
	}
	return append(out, LabelLiteral)
}

// literalShape is the census's rule 3: a value that is written as a quantity, a flag, a date, an interpolation or a dotted chain is never looked up in the def index, because `add_gold = 100` otherwise resolves against CK3's 10,887 numeric province keys.
func literalShape(v string, quoted bool) bool {
	if quoted || jomini.IsLiteralValue(v) {
		return true
	}
	if strings.ContainsAny(v, "$@|;") {
		return true
	}
	return strings.ContainsRune(v, '.') && !jomini.IsEventID(v)
}

// isDigitKey reports a key the games use as a weight rather than a name. The bound is deliberate: a number too large for a 64-bit count is not one of them.
func isDigitKey(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// ------------------------------------------------------------ accumulation

// add folds one occurrence's labels into a cell.
func (t *CellType) add(labels []string, n int32) {
	t.N += n
	if t.labels == nil {
		t.labels = map[string]int32{}
	}
	var kinds []string
	for _, l := range labels {
		t.labels[l] += n
		// Loc takes part in the intersection like a database: one value in three hundred also being a key made 747,513 CK3 loc references out of 451,232.
		if l == LabelLoc {
			kinds = append(kinds, LabelLoc)
			continue
		}
		if k, ok := strings.CutPrefix(l, LabelDef); ok {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return
	}
	slices.Sort(kinds)
	if !t.seen {
		t.inter, t.seen = kinds, true
		return
	}
	t.inter = intersect(t.inter, kinds)
}

// merge folds one file's cell into the corpus's.
func (t *CellType) merge(src *CellType) {
	t.N += src.N
	if t.labels == nil {
		t.labels = make(map[string]int32, len(src.labels))
	}
	for l, n := range src.labels {
		t.labels[l] += n
	}
	t.Names = t.Names || src.Names
	if !src.seen {
		return
	}
	if !t.seen {
		t.inter, t.seen = src.inter, true
		return
	}
	t.inter = intersect(t.inter, src.inter)
}

// settle publishes what the fold accumulated and drops its working state.
func (t *CellType) settle() {
	t.Kinds, t.inter = t.inter, nil
	best, at := int32(-1), ""
	for l, n := range t.labels {
		switch l {
		case LabelLiteral:
			t.Flags |= CellLiteral
		case LabelUnresolved:
			t.Flags |= CellUnresolved
		}
		if n > best || (n == best && l < at) {
			best, at = n, l
		}
	}
	if strings.HasPrefix(at, "block:") {
		t.Flags |= CellBlock
	}
	t.labels = nil
}

func intersect(a, b []string) []string {
	if len(a) == 0 {
		return nil
	}
	out := a[:0:0]
	for _, k := range a {
		if slices.Contains(b, k) {
			out = append(out, k)
		}
	}
	return out
}

// ---------------------------------------------------------- the nested join

// joinNested is the one join, by cell: a cell whose block keys hold names and one of which is cited under a declared kind is a definition cell of it, over the skeleton, which is keyed as the corpus writes it.
func joinNested(s *Schema, cited map[string]string, defined func(string) bool) {
	for kind, byPath := range s.Cells {
		for path, byKey := range byPath {
			parent, ok := parentCell(s, kind, path)
			if !ok || !parent.Names || parent.Def != "" {
				continue
			}
			child := ""
			for key := range byKey {
				// Only a citation nothing explains yet: `trait:brave` inside a `switch` made a definition of every branch of every switch.
				k := cited[key]
				if k == "" || defined(key) {
					continue
				}
				if child == "" || k < child {
					child = k
				}
			}
			if child != "" {
				parent.Def = jomini.CanonicalKind(child)
			}
		}
	}
}

// shapes publishes the join as one row per (parent kind, child kind), which a workspace applies and the editor lists.
func (s *Schema) shapes() {
	s.Nested = nil
	// The lowest key a reader could type, so two scans of one install agree and hover never prints a folded class.
	group := map[string]string{}
	for kind, byPath := range s.Cells {
		for _, byKey := range byPath {
			for key, t := range byKey {
				if t.Def == "" {
					continue
				}
				at := kind + "\x00" + t.Def
				was, seen := group[at]
				if key != "" && key[0] == '<' {
					key = ""
				}
				if !seen || was == "" || key != "" && key < was {
					group[at] = key
				}
			}
		}
	}
	for at, key := range group {
		kind, child, _ := strings.Cut(at, "\x00")
		s.Nested = append(s.Nested,
			NestedShape{ParentKind: kind, ChildKind: child, GroupKey: key})
	}
	slices.SortFunc(s.Nested, func(a, b NestedShape) int {
		if a.ParentKind != b.ParentKind {
			return strings.Compare(a.ParentKind, b.ParentKind)
		}
		return strings.Compare(a.ChildKind, b.ChildKind)
	})
}

// parentCell is the cell of the block one path hangs under.
func parentCell(s *Schema, kind, path string) (*CellType, bool) {
	// The empty path and a path ending in <kind> are one position: inside a definition's body, whose cell is the kind's.
	if inDefinitionBody(path) {
		t := s.Cells[kind][segTop][""]
		return t, t != nil
	}
	head, key := "", path
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		head, key = path[:i], path[i+1:]
	}
	t := s.Cells[kind][head][key]
	return t, t != nil
}

// inDefinitionBody reports a path whose innermost block is a definition rather than a named field, where the members are the definition's own fields as well as its nested rows.
func inDefinitionBody(path string) bool {
	return path == "" || path == segKind || strings.HasSuffix(path, "."+segKind)
}

// citedKinds is the citation half of the join, read through the DECLARED prefix table alone: admitting a contextual link harvested 153,343 CK3 legend properties as definitions.
func citedKinds(cites []typedCite, prefix map[string]string, declaredBy *declared.Schema) map[string]string {
	table := declaredBy.Prefixes()
	if len(table) == 0 {
		return nil
	}
	out := make(map[string]string, len(cites))
	for _, c := range cites {
		scope, citable := table[strings.ToLower(c.prefix)]
		if !citable || c.id == "" || jomini.IsStop(c.id) || jomini.IsEphemeral(c.prefix) {
			continue
		}
		kind := prefix[c.prefix]
		if kind == "" {
			kind = jomini.CanonicalKind(scope)
		}
		if kind == "" || kind == "type" || jomini.IsEphemeral(kind) {
			continue
		}
		out[c.id] = kind
	}
	return out
}

// typedCite is one `prefix:id` citation found in script.
type typedCite struct{ prefix, id string }

// ------------------------------------------------------------------ the bind

// bindKinds binds a declared scope to a harvested kind by exact name only.
func bindKinds(s *declared.Schema, keysByKind map[string]map[string]bool) (prefixKind, kindScope map[string]string) {
	prefixKind, kindScope = map[string]string{}, map[string]string{}
	if s == nil {
		return prefixKind, kindScope
	}
	for prefix, scope := range s.Prefixes() {
		kind := nameMatchKind(scope, keysByKind)
		if kind == "" {
			continue
		}
		prefixKind[prefix] = kind
		kindScope[kind] = scope
	}
	return prefixKind, kindScope
}

// nameMatchKind matches a scope type to a folder kind by name, tolerating the singular/plural split (scope "culture" vs folder "cultures").
func nameMatchKind(scope string, keysByKind map[string]map[string]bool) string {
	if scope == "" {
		return ""
	}
	for _, cand := range []string{scope, scope + "s", strings.TrimSuffix(scope, "s")} {
		if k := jomini.CanonicalKind(cand); k != "" && len(keysByKind[k]) > 0 {
			return k
		}
	}
	// A `_type` scope is routinely named after its stem's own folder rather than its own plural: `building_type` -> `buildings`.
	if stem := strings.TrimSuffix(scope, "_type"); stem != scope {
		for _, cand := range []string{stem, stem + "s", strings.TrimSuffix(stem, "s")} {
			if k := jomini.CanonicalKind(cand); k != "" && len(keysByKind[k]) > 0 {
				return k
			}
		}
	}
	return ""
}
