// cells.go is the learned half of a game: one cell per (file kind, block path,
// key), the exact record of what every value at that cell turned out to be, and
// the three questions a consumer asks of it. A type is not voted -- it is the
// union of what resolution found, so there is no floor, no coverage and no
// minimum-hits anywhere in this file. The one judgment is the reporting policy,
// which lives in lsp.

package derived

import (
	"maps"
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
	// Rows reports that the block keys under this cell are rows of a database rather than fields of a structure: the join above, or a local database -- a position whose block keys a sibling scalar cell cites, as a game rule's `default` names its own options.
	Rows bool `json:"rows,omitempty"`

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

// BlockType is what the block at one (kind, path) position is: §9's class, the declared token classes a body's members belong to, the database a definition cell names, and the structure whose members it holds.
type BlockType struct {
	Class uint8 `json:"c,omitempty"`
	Body  uint8 `json:"b,omitempty"`
	// Key is the structure this position is one of, and addresses its members in Schema.Structures. Writing the members here instead cost 230MB of CK3 cache: 2,015 `limit` positions are one member list, not 2,015 copies of 811 names.
	Key      string `json:"k,omitempty"`
	Database string `json:"d,omitempty"`
}

// The four classes of §9 in the precedence the census measured: body over database over list over structure.
const (
	BlockStructure uint8 = iota
	BlockDatabase
	BlockBody
	BlockList
)

// The bits of BlockType.Body: the declared token classes among a body's members. A body routinely carries several, and 47-63% of them also carry plain fields.
const (
	BodyTrigger uint8 = 1 << iota
	BodyEffect
	BodyModifier
)

// KindInfo is what a whole database says about itself rather than about one of its cells.
type KindInfo struct {
	// Loc are the key templates this kind's definitions are localized under, each with its exact coverage.
	Loc []loc.Convention `json:"loc,omitempty"`
}

// Schema is the learned half of a game plus the projections the editor reads; the declared half stays *declared.Schema on the Index.
type Schema struct {
	// Cells is kind -> folded path -> key -> type. Nested rather than flat because the three levels are what every lookup walks and what keeps one copy of each kind and path string in the file.
	Cells map[string]map[string]map[string]*CellType `json:"cells,omitempty"`
	// Blocks is what the block at each (kind, folded path) position is, derived from the settled cells.
	Blocks map[string]map[string]*BlockType `json:"blocks,omitempty"`
	// Structures is each structure's members, unified over every position its key is written at: 64% of them are written at exactly one, so a per-position list is blind to the rest.
	Structures map[string][]string `json:"structures,omitempty"`
	// KnownKeys is every key the corpus writes anywhere, plus the declared engine
	// API and the documented structure fields -- unknown-key's whole vocabulary:
	// a real key in the wrong block is not that check's business, only a name
	// written nowhere is.
	KnownKeys map[string]bool `json:"knownKeys,omitempty"`
	Kinds      map[string]KindInfo `json:"kinds,omitempty"`
	Nested     []NestedShape       `json:"nested,omitempty"`
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

// BlockAt is what the block at one position is, or nil.
func (s *Schema) BlockAt(kind, path string) *BlockType {
	if s == nil {
		return nil
	}
	return s.Blocks[kind][path]
}

// BlockMembers are the members of the structure at one position, sorted -- the union over every position of its key, plus whatever the game's documentation lists for it.
func (s *Schema) BlockMembers(kind, path string) []string {
	if b := s.BlockAt(kind, path); b != nil {
		return s.Structures[b.Key]
	}
	return nil
}

// Known reports a key the corpus writes anywhere for this game -- as a cell of
// any kind at any position, as engine API the game declares, or as a field the
// game's own documentation lists for some structure.
func (s *Schema) Known(key string) bool {
	return s != nil && key != "" && s.KnownKeys[strings.ToLower(key)]
}

// ----------------------------------------------------------- the block types

// learnBlocks is §9 over the settled cells: one BlockType per (kind, path) position, including the third of them that no block-flagged cell sits above. Body-ness and members unify by the block's key -- a key holding declared tokens at one position holds them at all of them -- and the game's own documentation adds the fields it lists for a structure whether or not vanilla writes them.
//
// install settles KnownKeys, unknown-key's whole vocabulary, from THIS schema's
// own cells: the install's alone. A workspace relearn (install false) never
// touches it -- a key the corpus never wrote is not "known" merely because the
// file being diagnosed just wrote it, which would silence the check on its own
// finding the moment it fired.
func (s *Schema) learnBlocks(gameID string, docFields map[string]map[string]bool, declaredBy *declared.Schema, install bool) {
	s.Blocks = make(map[string]map[string]*BlockType, len(s.Cells))
	// bodyByKey is the exclusive evidence for each block key, ambByKey the both-declared members that only count where there is none.
	bodyByKey, ambByKey := map[string]uint8{}, map[string]uint8{}
	members := map[string]map[string]bool{}
	own := map[*BlockType][]string{}
	for kind, byPath := range s.Cells {
		at := make(map[string]*BlockType, len(byPath))
		s.Blocks[kind] = at
		for path, byKey := range byPath {
			b, folded := &BlockType{Key: unifyKey(kind, path)}, false
			for key, t := range byKey {
				if key == "" || t.N == 0 {
					continue
				}
				if key[0] == '<' {
					folded = folded || key == segKind || key == segCite
					continue
				}
				c, _ := s.bodyClass(key)
				b.Body |= c
				own[b] = append(own[b], key)
				// A member the game declares in exactly one class says which class this body is; one it declares in several says only that this is a body.
				if c&(c-1) == 0 {
					bodyByKey[b.Key] |= c
				} else {
					ambByKey[b.Key] |= c
				}
			}
			cell, _ := parentCell(s, kind, path)
			switch {
			case b.Body != 0:
				b.Class = BlockBody
			case cell != nil && cell.Rows:
				b.Class, b.Database = BlockDatabase, cell.Def
			case folded && len(own[b]) == 0:
				b.Class = BlockDatabase
			case cell != nil && (cell.Flags&(CellLiteral|CellUnresolved) != 0 || len(cell.Kinds) > 0):
				b.Class = BlockList
			}
			at[path] = b
		}
	}
	// A key that is a body anywhere is a body everywhere it is a block, so its class is known before any position's members are collected. Its classes are the exclusive evidence over all of them, and the ambiguous members only where there is none.
	for _, at := range s.Blocks {
		for _, b := range at {
			if b.Body = bodyByKey[b.Key]; b.Body == 0 {
				b.Body = ambByKey[b.Key]
			}
			if b.Body != 0 {
				b.Class, b.Database = BlockBody, ""
			}
			// Every block key gets its entry, a database's and a list's included: a key the schema opens a block with anywhere is engine grammar, and `unknown-key` reads that from Structures.
			if members[b.Key] == nil {
				members[b.Key] = map[string]bool{}
			}
			if b.Class != BlockBody && b.Class != BlockStructure {
				continue
			}
			for _, m := range own[b] {
				// A body's tokens are the declared API, not the structure's fields; only the rest are members.
				c, _ := s.keyClass(gameID, m, false, false)
				if b.Class == BlockStructure || classes[c].member {
					members[b.Key][m] = true
				}
			}
		}
	}
	// Declared beats derived: a field the game's own documentation lists for a structure is a member whether or not vanilla writes it.
	for structure, fields := range docFields {
		if members[structure] == nil {
			members[structure] = map[string]bool{}
		}
		for f := range fields {
			members[structure][f] = true
		}
	}
	s.Structures = make(map[string][]string, len(members))
	for key, set := range members {
		s.Structures[key] = slices.Sorted(maps.Keys(set))
	}
	if !install {
		return
	}
	known := make(map[string]bool, len(s.Cells))
	for _, byPath := range s.Cells {
		for _, byKey := range byPath {
			for key := range byKey {
				if key != "" && key[0] != '<' {
					known[strings.ToLower(key)] = true
				}
			}
		}
	}
	declaredBy.EachName(func(name string) { known[strings.ToLower(name)] = true })
	for _, fields := range docFields {
		for f := range fields {
			known[strings.ToLower(f)] = true
		}
	}
	s.KnownKeys = known
}

// unifyKey is the name a position unifies under: the block's own key, or the kind for a definition body, whose fields the documentation addresses the same way. A class-folded segment is a position and not a name, so it unifies under the key above it plus the class -- the weight entry of a random_list is `random_list.<n>` wherever it is written, and not one structure per enclosing section.
func unifyKey(kind, path string) string {
	if path == "" {
		return kind
	}
	head, key := "", path
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		head, key = path[:i], path[i+1:]
	}
	if key == "" || key[0] == '<' {
		return unifyKey(kind, head) + "." + key
	}
	return key
}

// ------------------------------------------------------- the key classifier

// Class is what one key at one position is. Exactly one answer per key, from one
// function, because the 2026-09-17 false positives were six copies of this
// question disagreeing about a key the fold and the judge each classified alone.
type Class uint8

const (
	ClassPlain      Class = iota // a name written as a field of the block it sits in
	ClassNumber                  // a weight, an index, a data-row id
	ClassDate                    // 1066.1.1
	ClassCite                    // prefix:id, through the declared prefixes
	ClassVar                     // a $PARAM$ or @value interpolation, or a quoted key
	ClassStop                    // control flow and the boolean literals
	ClassDeclared                // a token the game's own API declares
	ClassMacroCall               // a scripted_* definition invoked by name
	ClassDefinition              // a row of the database the block above holds
	ClassBare                    // a list member, which has no key at all
)

// classes is the one table every consumer of a key's class reads: what the fold
// writes for it, whether it is a member of its block's structure, whether
// unknown-key may name it, and whether completion offers it.
//
// The fifth column the design asked for -- what hover shows -- has no row here:
// a card is chosen by what the word RESOLVES to (lsp/resolve.go's wordResolvers),
// not by its class at a position, so a column would have had no reader.
var classes = [...]struct {
	// seg is the path segment and the cell key this class folds to; empty keeps the key as written.
	seg string
	// member is both columns the design asked for as one: a field of its block's structure is also exactly what completion offers under that block, and a body's declared tokens are neither.
	member, nameable bool
}{
	ClassPlain:      {"", true, true},
	ClassNumber:     {segNumber, false, false},
	ClassDate:       {segDate, false, false},
	ClassCite:       {segCite, false, false},
	ClassVar:        {segVar, false, false},
	ClassStop:       {"", true, false},
	ClassDeclared:   {"", false, false},
	ClassMacroCall:  {"", false, false},
	ClassDefinition: {segKind, false, false},
	ClassBare:       {segBare, false, false},
}

// Nameable reports a class `unknown-key` may name: only a plain field name, never a position the fold made, a stop word, a declared token or a macro call.
func (c Class) Nameable() bool { return classes[c].nameable }

// keyClass answers, for one key at one position, exactly one class, and the kind
// a citation names. rows says the block above this key holds a database, which is
// the only evidence that a name is a definition here -- CK3 names its game
// concepts `levies`, `men_at_arms` and `change_title_holder`, so classing a key
// as a definition because something SOMEWHERE defines that name made every
// declared effect body one blob.
func (s *Schema) keyClass(gameID, key string, quoted, rows bool) (Class, string) {
	if key == "" {
		return ClassBare, ""
	}
	if quoted {
		return ClassVar, ""
	}
	if isDigitKey(key) {
		return ClassNumber, ""
	}
	if jomini.IsDateLiteral(key) {
		return ClassDate, ""
	}
	if kind, _, ok := game.ParseTyped(gameID, key); ok {
		return ClassCite, jomini.CanonicalKind(kind)
	}
	if p, ok := jomini.ParsePrefixed(key); ok {
		return ClassCite, jomini.PrefixKind(p.Prefix)
	}
	if strings.ContainsAny(key, "$@") {
		return ClassVar, ""
	}
	if jomini.IsStop(key) {
		return ClassStop, ""
	}
	if rows {
		return ClassDefinition, ""
	}
	if bits, macro := s.bodyClass(key); bits != 0 {
		if macro {
			return ClassMacroCall, ""
		}
		return ClassDeclared, ""
	}
	return ClassPlain, ""
}

// bodyClass is the declared classes one name belongs to, and whether it is a
// macro the install declared for itself rather than a token the engine ships.
func (s *Schema) bodyClass(key string) (bits uint8, macro bool) {
	// Phase A classifies a value's shape before any schema exists, and a shape is settled well before the classifier reaches these two sets.
	if s == nil {
		return 0, false
	}
	if s.TriggerSet[key] {
		bits |= BodyTrigger
	}
	if s.EffectSet[key] {
		bits |= BodyEffect
	}
	if k := s.MacroDefs[key]; jomini.IsMacroKind(k) {
		macro = bits == 0
		switch {
		case strings.Contains(k, "trigger"):
			bits |= BodyTrigger
		case strings.Contains(k, "modifier"):
			bits |= BodyModifier
		default:
			bits |= BodyEffect
		}
	}
	return bits, macro
}

// ------------------------------------------------------------------ the fold

// foldKey folds one cell key by its class. rows is the position's answer, so a block key at a database folds to <kind> in the same call that folds a weight to <n>.
func (s *Schema) foldKey(gameID, key string, quoted, rows bool) string {
	// A quoted key is a name the author spelled out; only a PATH folds it to a class, because a path segment's job is to be one position and a key's is to be one name.
	if key == "" || quoted {
		return key
	}
	c, _ := s.keyClass(gameID, key, quoted, rows)
	if seg := classes[c].seg; seg != "" {
		return seg
	}
	return key
}

// foldSeg folds one path segment by the same classifier, with the one rule that
// is about paths and not about keys: a segment that is not a lower-case script
// identifier names something the corpus has not harvested, and keeping it
// verbatim is what made CK3 898,774 cells. A KEY of that shape keeps its
// spelling, because `ROLE = chemist` is the argument's own cell.
func (s *Schema) foldSeg(gameID, raw string, quoted, rows bool) string {
	c, _ := s.keyClass(gameID, raw, quoted, rows)
	if seg := classes[c].seg; seg != "" {
		return seg
	}
	name := game.KeyIdentity(gameID, raw)
	if jomini.IsLowerName(name) {
		return name
	}
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

// block is the class of the block at one position, read in the same order as a cell.
func (t *lookup) block(kind, path string) *BlockType {
	for _, s := range t.cells {
		if b := s.BlockAt(kind, path); b != nil {
			return b
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
			if !ok || !parent.Names || parent.Rows {
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
				parent.Def, parent.Rows = jomini.CanonicalKind(child), true
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

// atDatabase reports a position whose block keys are rows of a database rather than fields of a structure, and the kind they define where the join named one; every body of a definition shares the kind's own entry (the empty path), which is how a landed title five deep stays one position. Keyed by kind first because a fold asks it once per node of one file.
func atDatabase(byPath map[string]string, path string) (def string, ok bool) {
	if inDefinitionBody(path) {
		path = ""
	}
	def, ok = byPath[path]
	return def, ok
}

// markRows records that the block keys at one position are rows, on the cell of the block that opens it -- the inverse of parentCell, so the mark is found where a query looks for it.
func markRows(s *Schema, kind, path, def string) {
	head, key := segTop, ""
	if !inDefinitionBody(path) {
		head, key = "", path
		if i := strings.LastIndexByte(path, '.'); i >= 0 {
			head, key = path[:i], path[i+1:]
		}
	}
	t := s.at(kind, head, key)
	t.Rows = true
	if def != "" {
		t.Def = def
	}
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
