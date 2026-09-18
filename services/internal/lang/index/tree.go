// tree.go is the learned half of a game as one tree. A node is one position
// of a kind's script: what every value written there turned out to be, what
// its block holds, and its members. One classifier says what a key is, one
// table says what each class is filed under, and one descent addresses the
// tree for the scan and for the editor alike. Nothing here votes.

package index

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

// Node is one position: the kind's own body at the root, a member below it.
type Node struct {
	N     int32    `json:"n,omitempty"`
	Kinds []string `json:"k,omitempty"` // the databases every resolvable value named (the intersection), "loc" among them
	Flags uint8    `json:"f,omitempty"`
	Def   string   `json:"d,omitempty"` // with Rows: the kind the rows below define
	Class uint8    `json:"c,omitempty"`
	Body  uint8    `json:"b,omitempty"`
	// Struct names the structure this block is one of; its members are Tree.Structs[Struct].
	Struct string           `json:"s,omitempty"`
	Loc    []loc.Convention `json:"l,omitempty"` // at a root: the loc key templates this kind's definitions carry
	Kids   map[string]*Node `json:"m,omitempty"`

	labels map[string]int32
	inter  []string
	seen   bool
}

// Node.Flags.
const (
	Literal    uint8 = 1 << iota // a quantity, a date, a flag or a quoted string was written here
	Unresolved                   // a name nothing explains was written here
	IsBlock                      // the label worn most often is a block
	Names                        // block occurrences hold keyed children
	Rows                         // the block keys below are rows of a database, not fields
)

// Node.Class, in the precedence the census measured.
const (
	Structure uint8 = iota
	Database
	Body
	List
)

// Node.Body bits: the declared token classes among a body's members.
const (
	BodyTrigger uint8 = 1 << iota
	BodyEffect
	BodyModifier
)

// Labels are what one value at a node turned out to be; Def and Cite carry the kind after the colon.
const (
	LabelBlock      = "block:names"
	LabelList       = "block:list"
	LabelEmpty      = "block:empty"
	LabelLiteral    = "literal"
	LabelLoc        = "loc"
	LabelStop       = "stop"
	LabelUnresolved = "unresolved"
	LabelDef        = "def:"
	LabelCite       = "cite:"
)

// Segments a class is filed under.
const (
	SegNumber = "<n>"
	SegDate   = "<date>"
	SegCite   = "<cite>"
	SegKind   = "<kind>"
	SegBare   = "<bare>"
	SegVar    = "<var>"
)

// Bind ties a declared citation prefix to the database it cites and the scope type it holds.
type Bind struct {
	Prefix string `json:"p"`
	Kind   string `json:"k"`
	Scope  string `json:"s"`
}

// Tree is the learned half of a game; the declared half stays *declared.Schema.
type Tree struct {
	Kinds map[string]*Node `json:"kinds,omitempty"`
	// Structs is each structure's members, unified over every position its key is written at, plus the fields the game's documentation lists for it.
	Structs map[string][]string `json:"structs,omitempty"`
	// Known is every key the install writes anywhere, the declared API and the documented fields, sorted: unknown-key's whole vocabulary and completion's word list. Install-only, so a mod's own typo cannot vouch for itself.
	Known []string `json:"known,omitempty"`
	Binds []Bind   `json:"binds,omitempty"`
	// Macros is every scripted_* definition with its kind, which is what makes a keyed statement a call.
	Macros        map[string]string `json:"macros,omitempty"`
	DataFunctions []string          `json:"dataFunctions,omitempty"`
	// LocKeyPrefixes build one loc key out of another (EU5's NOT_<key>); LocEngineSlots are `$NAME$` interpolations the game's loc uses but never defines.
	LocKeyPrefixes []string        `json:"locKeyPrefixes,omitempty"`
	LocEngineSlots map[string]bool `json:"locEngineSlots,omitempty"`
	// Nested is the join as one row per (parent kind, child kind), listed once at scan time for the editor.
	Nested []Shape `json:"nested,omitempty"`

	gameID string
	decl   *declared.Schema
	// byName makes a bootstrap tree: any definition name files as a row, so the join can generalise a cited row to the position it was written at.
	byName func(string) bool
}

// Root is a kind's own body, made when learning.
func (s *Tree) Root(kind string, create bool) *Node {
	if s == nil {
		return nil
	}
	n := s.Kinds[kind]
	if n == nil && create {
		if s.Kinds == nil {
			s.Kinds = map[string]*Node{}
		}
		n = &Node{}
		s.Kinds[kind] = n
	}
	return n
}

// At descends from a kind's root by a chain of block keys, folding each as the scan did.
func (s *Tree) At(kind string, chain ...string) *Node {
	n := s.Root(kind, false)
	for _, key := range chain {
		if n == nil {
			return nil
		}
		n = s.Child(n, key, false, true, false)
	}
	return n
}

// Child is the node a key addresses below p; create makes it.
func (s *Tree) Child(p *Node, key string, quoted, block, create bool) *Node {
	if p == nil {
		return nil
	}
	name := s.fold(key, quoted, block && p.Flags&Rows != 0)
	c := p.Kids[name]
	if c == nil && create {
		if p.Kids == nil {
			p.Kids = map[string]*Node{}
		}
		c = &Node{}
		p.Kids[strings.Clone(name)] = c
	}
	return c
}

// Step is the one descent the editor takes: what a block key is filed under below p, its node, and the position its members hang from -- the body of the kind a database defines when the key is a row.
func (s *Tree) Step(p *Node, key string, quoted bool) (name string, cell, hang *Node) {
	if p == nil {
		return "", nil, nil
	}
	name = s.fold(key, quoted, p.Flags&Rows != 0)
	cell = p.Kids[name]
	hang = cell
	if name == SegKind && cell != nil {
		hang = s.body(p, cell, false)
	}
	return name, cell, hang
}

// Members are the keys written at one position, most-used first; a folded class is a position, not a name a reader can type.
func (n *Node) Members() []string {
	if n == nil {
		return nil
	}
	out := make([]string, 0, len(n.Kids))
	for k, c := range n.Kids {
		if k != "" && k[0] != '<' && c.N > 0 {
			out = append(out, k)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		if d := n.Kids[b].N - n.Kids[a].N; d != 0 {
			return int(d)
		}
		return strings.Compare(a, b)
	})
	return out
}

// Has reports one of the labels a settled node keeps.
func (n *Node) Has(label string) bool {
	if n == nil {
		return false
	}
	switch label {
	case LabelLiteral:
		return n.Flags&Literal != 0
	case LabelUnresolved:
		return n.Flags&Unresolved != 0
	case LabelLoc:
		return slices.Contains(n.Kinds, LabelLoc)
	}
	return false
}

// Kind is the one database a node's values all named, or empty where the corpus disagreed; a localization key of the same name beside it is noise, since every culture has one.
func (n *Node) Kind() string {
	out := ""
	if n == nil {
		return out
	}
	for _, k := range n.Kinds {
		if k == LabelLoc {
			continue
		}
		if out != "" {
			return ""
		}
		out = k
	}
	return out
}

// Fire is the event or on_action kind a node's values name, which is what makes an assignment a fire site.
func (n *Node) Fire() string {
	if n == nil {
		return ""
	}
	for _, k := range n.Kinds {
		if k == jomini.KindEvent || k == "on_action" {
			return k
		}
	}
	return ""
}

// Prefix is the declared prefix bound to a database, the shortest when several are.
func (s *Tree) Prefix(kind string) string {
	out := ""
	for _, b := range s.Binds {
		if b.Kind == kind && (out == "" || len(b.Prefix) < len(out)) {
			out = b.Prefix
		}
	}
	return out
}

// PrefixKind is the database a declared prefix cites.
func (s *Tree) PrefixKind(prefix string) string {
	for _, b := range s.Binds {
		if strings.EqualFold(b.Prefix, prefix) {
			return b.Kind
		}
	}
	return ""
}

// ScopeOf is the scope type a database holds.
func (s *Tree) ScopeOf(kind string) string {
	for _, b := range s.Binds {
		if b.Kind == kind || b.Kind == jomini.CanonicalKind(kind) {
			return b.Scope
		}
	}
	return ""
}

// CiteForm is how a user writes a member of a scope type: the shortest prefix and the database.
func (s *Tree) CiteForm(scope string) (prefix, kind string) {
	for _, b := range s.Binds {
		if !strings.EqualFold(b.Scope, scope) {
			continue
		}
		if prefix == "" || len(b.Prefix) < len(prefix) {
			prefix = b.Prefix
		}
		if kind == "" || len(b.Kind) < len(kind) {
			kind = b.Kind
		}
	}
	return prefix, kind
}

// ------------------------------------------------------------- the classifier

// Class is what one key at one position is: exactly one answer from one function.
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

// classes is the one table: what a class is filed under (empty keeps the key), whether it is a member of its block's structure (which is also what completion offers), and whether unknown-key may name it.
var classes = [...]struct {
	seg              string
	member, nameable bool
}{
	ClassPlain:      {"", true, true},
	ClassNumber:     {SegNumber, false, false},
	ClassDate:       {SegDate, false, false},
	ClassCite:       {SegCite, false, false},
	ClassVar:        {SegVar, false, false},
	ClassStop:       {"", true, false},
	ClassDeclared:   {"", false, false},
	ClassMacroCall:  {"", false, false},
	ClassDefinition: {SegKind, false, false},
	ClassBare:       {SegBare, false, false},
}

// Nameable reports a class unknown-key may name.
func (c Class) Nameable() bool { return classes[c].nameable }

// KeyClass is what one key is at a position; rows says the block above holds a database, the only evidence that a name is a definition here.
func (s *Tree) KeyClass(key string, quoted, rows bool) (Class, string) {
	switch {
	case key == "":
		return ClassBare, ""
	case quoted:
		return ClassVar, ""
	case isDigits(key):
		return ClassNumber, ""
	case jomini.IsDateLiteral(key):
		return ClassDate, ""
	}
	if kind, _, ok := game.ParseTyped(s.gameID, key); ok {
		return ClassCite, jomini.CanonicalKind(kind)
	}
	if p, ok := jomini.ParsePrefixed(key); ok {
		return ClassCite, jomini.PrefixKind(p.Prefix)
	}
	switch {
	case strings.ContainsAny(key, "$@"):
		return ClassVar, ""
	case jomini.IsStop(key):
		return ClassStop, ""
	case rows:
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

// bodyClass is the declared classes one name belongs to, and whether it is a macro the install declared for itself rather than a token the engine ships.
func (s *Tree) bodyClass(key string) (bits uint8, macro bool) {
	if s == nil {
		return 0, false
	}
	lk := strings.ToLower(key)
	if s.decl != nil {
		if _, ok := s.decl.Triggers[lk]; ok {
			bits |= BodyTrigger
		}
		if _, ok := s.decl.Effects[lk]; ok {
			bits |= BodyEffect
		}
	}
	if k := s.Macros[key]; jomini.IsMacroKind(k) {
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

// fold is the name a key is filed under at a position.
func (s *Tree) fold(key string, quoted, rows bool) string {
	c, _ := s.KeyClass(key, quoted, rows)
	if seg := classes[c].seg; seg != "" {
		return seg
	}
	return key
}

func isDigits(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// ---------------------------------------------------------------- the values

// resolver is everything outside a file that typing one value consults: the definitions, the localization, the prefixes and the game.
type resolver struct {
	gameID     string
	kindsByKey map[string][]string
	locKeys    map[string]bool
	prefix     map[string]string
	macros     map[string]string
}

// classify is one value, one label set, by lookup. A literal carries only Literal, except a number in a data kind, which may also name a row.
func (r *resolver) classify(v string, quoted, dataKind bool, out []string) []string {
	out = out[:0]
	if v == "" {
		return append(out, LabelLiteral)
	}
	if !quoted {
		if jomini.IsStop(v) {
			return append(out, LabelStop)
		}
		if kind, _, ok := game.ParseTyped(r.gameID, v); ok {
			if k := r.prefix[strings.ToLower(strings.SplitN(v, ":", 2)[0])]; k != "" {
				kind = k
			}
			return append(out, LabelCite+jomini.CanonicalKind(kind))
		}
		if _, ok := jomini.ParsePrefixed(v); ok {
			return append(out, LabelCite+jomini.PrefixKind(strings.SplitN(v, ":", 2)[0]))
		}
	}
	if literalShape(v, quoted) {
		if r.locKeys[v] {
			out = append(out, LabelLoc)
		}
		if dataKind && isDigits(v) {
			for _, k := range r.kindsByKey[v] {
				out = append(out, LabelDef+k)
			}
		}
		return append(out, LabelLiteral)
	}
	for _, k := range r.kindsByKey[v] {
		out = append(out, LabelDef+k)
	}
	if r.locKeys[v] {
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

// literalShape is a value written as a quantity, a flag, a date, an interpolation or a dotted chain, which is never looked up: `add_gold = 100` otherwise resolves against CK3's 10,887 numeric province keys.
func literalShape(v string, quoted bool) bool {
	if quoted || jomini.IsLiteralValue(v) || strings.ContainsAny(v, "$@|;") {
		return true
	}
	return strings.ContainsRune(v, '.') && !jomini.IsEventID(v)
}

// add folds one occurrence's labels into a node. Loc takes part in the intersection like a database: one value in three hundred also being a key made 747,513 CK3 loc references out of 451,232.
func (n *Node) add(labels []string, count int32) {
	n.N += count
	if n.labels == nil {
		n.labels = map[string]int32{}
	}
	var kinds []string
	for _, l := range labels {
		n.labels[l] += count
		if l == LabelLoc {
			kinds = append(kinds, LabelLoc)
		} else if k, ok := strings.CutPrefix(l, LabelDef); ok {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return
	}
	slices.Sort(kinds)
	if !n.seen {
		n.inter, n.seen = kinds, true
		return
	}
	n.inter = intersect(n.inter, kinds)
}

// merge folds one file's tree into the corpus's, and reports the members it added or wrote to, which is what a workspace retracts with the file.
func (s *Tree) merge(src *Tree, into []kid) []kid {
	for kind, root := range src.Kinds {
		into = s.Root(kind, true).merge(root, into)
	}
	return into
}

func (n *Node) merge(src *Node, into []kid) []kid {
	n.N += src.N
	n.Flags |= src.Flags & (Names | Rows)
	if src.Def != "" {
		n.Def = src.Def
	}
	if len(src.labels) > 0 && n.labels == nil {
		n.labels = make(map[string]int32, len(src.labels))
	}
	for l, c := range src.labels {
		n.labels[l] += c
	}
	if src.seen {
		if !n.seen {
			n.inter, n.seen = src.inter, true
		} else {
			n.inter = intersect(n.inter, src.inter)
		}
	}
	for k, c := range src.Kids {
		if at := n.Kids[k]; at != nil {
			into = at.merge(c, into)
			continue
		}
		if n.Kids == nil {
			n.Kids = map[string]*Node{}
		}
		n.Kids[k] = c
		into = append(into, kid{n, k})
	}
	return into
}

// settle publishes what the fold accumulated and drops its working state, over the whole tree.
func (n *Node) settle() {
	if n.seen || len(n.labels) > 0 {
		n.Kinds, n.inter = n.inter, nil
		best, at := int32(-1), ""
		for l, c := range n.labels {
			switch l {
			case LabelLiteral:
				n.Flags |= Literal
			case LabelUnresolved:
				n.Flags |= Unresolved
			}
			if c > best || (c == best && l < at) {
				best, at = c, l
			}
		}
		if strings.HasPrefix(at, "block:") {
			n.Flags |= IsBlock
		}
		n.labels, n.seen = nil, false
	}
	for _, c := range n.Kids {
		c.settle()
	}
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

// ------------------------------------------------------------------ the join

// join marks every node whose block keys hold names and one of which is cited under a declared kind as a database of that kind, over a tree keyed as the corpus writes it.
func (s *Tree) join(cited map[string]string, defined func(string) bool) {
	var walk func(n *Node)
	walk = func(n *Node) {
		if n.Flags&(Names|Rows) == Names {
			child := ""
			for key := range n.Kids {
				// Only a citation nothing explains yet: `trait:brave` inside a `switch` made a definition of every branch of every switch.
				if k := cited[key]; k != "" && !defined(key) && (child == "" || k < child) {
					child = k
				}
			}
			if child != "" {
				n.Def, n.Flags = jomini.CanonicalKind(child), n.Flags|Rows
			}
		}
		for _, c := range n.Kids {
			walk(c)
		}
	}
	for _, root := range s.Kinds {
		walk(root)
	}
}

// Shapes is the join as one row per (parent kind, child kind), which a workspace applies and the editor lists.
type Shape struct {
	ParentKind string `json:"parentKind"`
	ChildKind  string `json:"childKind"`
	GroupKey   string `json:"groupKey,omitempty"`
}

// Shapes lists the nested databases the join found, grouped under the lowest key a reader could type.
func (s *Tree) Shapes() []Shape {
	group := map[[2]string]string{}
	for kind, root := range s.Kinds {
		var visit func(n *Node, key string)
		visit = func(n *Node, key string) {
			if n.Def != "" {
				at := [2]string{kind, n.Def}
				if key != "" && key[0] == '<' {
					key = ""
				}
				if was, seen := group[at]; !seen || was == "" || key != "" && key < was {
					group[at] = key
				}
			}
			for k, c := range n.Kids {
				visit(c, k)
			}
		}
		visit(root, "")
	}
	out := make([]Shape, 0, len(group))
	for at, key := range group {
		out = append(out, Shape{ParentKind: at[0], ChildKind: at[1], GroupKey: key})
	}
	slices.SortFunc(out, func(a, b Shape) int {
		if a.ParentKind != b.ParentKind {
			return strings.Compare(a.ParentKind, b.ParentKind)
		}
		return strings.Compare(a.ChildKind, b.ChildKind)
	})
	return out
}

// citedKinds is the citation half of the join, read through the declared prefix table alone: admitting a contextual link harvested 153,343 CK3 legend properties as definitions.
func citedKinds(cites map[string]string, prefix map[string]string, decl *declared.Schema) map[string]string {
	table := decl.Prefixes()
	out := make(map[string]string, len(cites))
	for id, p := range cites {
		scope, citable := table[strings.ToLower(p)]
		if !citable || id == "" || jomini.IsStop(id) || jomini.IsEphemeral(p) {
			continue
		}
		kind := prefix[strings.ToLower(p)]
		if kind == "" {
			kind = jomini.CanonicalKind(scope)
		}
		if kind == "" || kind == "type" || jomini.IsEphemeral(kind) {
			continue
		}
		out[id] = kind
	}
	return out
}

// bind ties each declared prefix to the harvested database of the same name, tolerating the singular/plural split and the `_type` stem.
func bind(decl *declared.Schema, keysByKind map[string]map[string]bool) []Bind {
	if decl == nil {
		return nil
	}
	var out []Bind
	for prefix, scope := range decl.Prefixes() {
		if kind := nameMatchKind(scope, keysByKind); kind != "" {
			out = append(out, Bind{Prefix: prefix, Kind: kind, Scope: scope})
		}
	}
	slices.SortFunc(out, func(a, b Bind) int { return strings.Compare(a.Prefix, b.Prefix) })
	return out
}

func nameMatchKind(scope string, keysByKind map[string]map[string]bool) string {
	stem := strings.TrimSuffix(scope, "_type")
	for _, base := range []string{scope, stem} {
		for _, cand := range []string{base, base + "s", strings.TrimSuffix(base, "s"), strings.TrimSuffix(base, "y") + "ies"} {
			if k := jomini.CanonicalKind(cand); k != "" && len(keysByKind[k]) > 0 {
				return k
			}
		}
	}
	return ""
}

func (s *Tree) prefixMap() map[string]string {
	out := make(map[string]string, len(s.Binds))
	for _, b := range s.Binds {
		out[strings.ToLower(b.Prefix)] = b.Kind
	}
	return out
}

// ---------------------------------------------------------------- the blocks

// blocks is one class per position over the settled tree. Body-ness and members unify by the block's key -- a key holding declared tokens at one position holds them at all of them -- and the game's own documentation adds the fields it lists for a structure whether or not vanilla writes them. Only an install settles Known.
func (s *Tree) blocks(docFields map[string]map[string]bool, install bool) {
	bodyByKey, ambByKey := map[string]uint8{}, map[string]uint8{}
	members := map[string]map[string]bool{}
	type pos struct {
		n   *Node
		own []string
	}
	var all []pos
	var walk func(n *Node, name string)
	walk = func(n *Node, name string) {
		p := pos{n: n}
		n.Struct, n.Class, n.Body = name, Structure, 0
		folded := false
		for key, c := range n.Kids {
			if c.N == 0 && c.Flags&Rows == 0 {
				continue
			}
			if key[0] == '<' {
				folded = folded || key == SegKind || key == SegCite
			} else {
				bits, _ := s.bodyClass(key)
				n.Body |= bits
				p.own = append(p.own, key)
				// A member the game declares in exactly one class says which class this body is; one it declares in several says only that this is a body.
				if bits&(bits-1) == 0 {
					bodyByKey[name] |= bits
				} else {
					ambByKey[name] |= bits
				}
			}
		}
		switch {
		case n.Body != 0:
			n.Class = Body
		case n.Flags&Rows != 0:
			n.Class = Database
		case folded && len(p.own) == 0:
			n.Class = Database
		case n.Flags&(Literal|Unresolved) != 0 || len(n.Kinds) > 0:
			n.Class = List
		}
		all = append(all, p)
		for key, c := range n.Kids {
			child := key
			if key[0] == '<' {
				child = name + "." + key
			}
			walk(c, child)
		}
	}
	for kind, root := range s.Kinds {
		walk(root, kind)
	}
	for _, p := range all {
		n := p.n
		if n.Body = bodyByKey[n.Struct]; n.Body == 0 {
			n.Body = ambByKey[n.Struct]
		}
		if n.Body != 0 {
			n.Class = Body
		}
		if members[n.Struct] == nil {
			members[n.Struct] = map[string]bool{}
		}
		if n.Class != Body && n.Class != Structure {
			continue
		}
		for _, m := range p.own {
			c, _ := s.KeyClass(m, false, false)
			if n.Class == Structure || classes[c].member {
				members[n.Struct][m] = true
			}
		}
	}
	for structure, fields := range docFields {
		if members[structure] == nil {
			members[structure] = map[string]bool{}
		}
		maps.Copy(members[structure], fields)
	}
	s.Structs = make(map[string][]string, len(members))
	for key, set := range members {
		s.Structs[key] = slices.Sorted(maps.Keys(set))
	}
	if !install {
		return
	}
	known := map[string]bool{}
	for _, p := range all {
		for _, m := range p.own {
			known[strings.ToLower(m)] = true
		}
	}
	s.decl.EachName(func(name string) { known[strings.ToLower(name)] = true })
	for _, fields := range docFields {
		for f := range fields {
			known[strings.ToLower(f)] = true
		}
	}
	s.Known = slices.Sorted(maps.Keys(known))
}

// KnownKey reports a key the install writes anywhere, declares or documents.
func (s *Tree) KnownKey(key string) bool {
	if s == nil || key == "" {
		return false
	}
	_, ok := slices.BinarySearch(s.Known, strings.ToLower(key))
	return ok
}

// StructMembers are the members of the structure at one position, sorted.
func (s *Tree) StructMembers(n *Node) []string {
	if s == nil || n == nil {
		return nil
	}
	return s.Structs[n.Struct]
}
