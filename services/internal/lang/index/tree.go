// tree.go is the learned half of a game as one tree. A node is one position
// of a kind's script: what every value written there turned out to be, what
// its block holds, and its members. One classifier says what a key is, one
// table says what each class is filed under, and one descent addresses the
// tree for the scan and for the editor alike. Nothing here votes.

package index

import (
	"cmp"
	"maps"
	"slices"
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
	// Kind is set at a root: the type the tree below describes.
	Kind *Kind `json:"K,omitempty"`

	counts map[Class]int32
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

// LabelLoc marks, among a node's Kinds, that its values were localization keys.
const LabelLoc = "loc"

// Segments a class is filed under.
const (
	SegNumber = "<n>"
	SegDate   = "<date>"
	SegCite   = "<cite>"
	SegKind   = "<kind>"
	SegBare   = "<bare>"
	SegVar    = "<var>"
)

// Kind is one type the install defines, kept on the root node of its tree: where its rows live, the declared prefixes that cite it, and the scope type a row holds.
type Kind struct {
	Data     bool     `json:"data,omitempty"`
	Prefixes []string `json:"prefixes,omitempty"`
	Scope    string   `json:"scope,omitempty"`
}

// Tree is the learned half of a game; the declared half stays *declared.Schema.
type Tree struct {
	Kinds map[string]*Node `json:"kinds,omitempty"`
	// Structs is each structure's members, unified over every position its key is written at, plus the fields the game's documentation lists for it.
	Structs map[string][]string `json:"structs,omitempty"`
	// Known is every key the install writes anywhere, the declared API and the documented fields, sorted: unknown-key's whole vocabulary and completion's word list. Install-only, so a mod's own typo cannot vouch for itself.
	Known []string `json:"known,omitempty"`
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
	base   *Tree // the install's tree under a workspace's: a kind the workspace never learned is read there
	// byName makes a bootstrap tree: any definition name files as a row, so the join can generalise a cited row to the position it was written at.
	byName func(string) bool
}

// kind is the entry on a root, made on first use.
func (n *Node) kind() *Kind {
	if n.Kind == nil {
		n.Kind = &Kind{}
	}
	return n.Kind
}

// eachKind visits every kind entry, the workspace's own then the install's beneath it.
func (s *Tree) eachKind(fn func(kind string, k *Kind) bool) {
	for t := s; t != nil; t = t.base {
		for kind, root := range t.Kinds {
			if root.Kind != nil && !fn(kind, root.Kind) {
				return
			}
		}
	}
}

// Of is the kind entry of a type, nil where nothing learned it.
func (s *Tree) Of(kind string) *Kind {
	for t := s; t != nil; t = t.base {
		if r := t.Kinds[kind]; r != nil && r.Kind != nil {
			return r.Kind
		}
	}
	return nil
}

// IsData reports a kind whose rows live in the data tier.
func (s *Tree) IsData(kind string) bool {
	k := s.Of(kind)
	return k != nil && k.Data
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

// Database is the one database a node's values all named, or empty where the corpus disagreed; a localization key of the same name beside it is noise, since every culture has one.
func (n *Node) Database() string {
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
func (s *Tree) Prefix(kind string) (out string) {
	if k := s.Of(kind); k != nil {
		for _, p := range k.Prefixes {
			if out == "" || len(p) < len(out) {
				out = p
			}
		}
	}
	return out
}

// PrefixKind is the database a declared prefix cites.
func (s *Tree) PrefixKind(prefix string) (out string) {
	s.eachKind(func(kind string, k *Kind) bool {
		if slices.ContainsFunc(k.Prefixes, func(p string) bool { return strings.EqualFold(p, prefix) }) {
			out = kind
		}
		return out == ""
	})
	return out
}

// ScopeOf is the scope type a database holds.
func (s *Tree) ScopeOf(kind string) string {
	k := s.Of(kind)
	if k == nil {
		k = s.Of(jomini.CanonicalKind(kind))
	}
	if k == nil {
		return ""
	}
	return k.Scope
}

// CiteForm is how a user writes a member of a scope type: the shortest prefix and the database.
func (s *Tree) CiteForm(scope string) (prefix, kind string) {
	s.eachKind(func(k string, e *Kind) bool {
		if !strings.EqualFold(e.Scope, scope) {
			return true
		}
		if kind == "" || len(k) < len(kind) {
			kind = k
		}
		for _, p := range e.Prefixes {
			if prefix == "" || len(p) < len(prefix) {
				prefix = p
			}
		}
		return true
	})
	return prefix, kind
}

// ------------------------------------------------------------- the classifier

// Class is what one written word is, at one position: exactly one answer from one function, for a key and for a value alike.
type Class uint8

const (
	ClassPlain      Class = iota // a field name, or a value that is neither a name nor a literal
	ClassNumber                  // a weight, an index, a data-row id
	ClassDate                    // 1066.1.1
	ClassCite                    // prefix:id, through the declared prefixes
	ClassEphemeral               // scope:x, var:x, flag:x: a value that lives for one script run
	ClassVar                     // a $PARAM$ or @value interpolation, or a quoted word
	ClassStop                    // control flow, the scope words and the boolean literals
	ClassDeclared                // a token the game's own API declares
	ClassMacroCall               // a scripted_* definition invoked by name
	ClassDefinition              // a row of the database the block above holds
	ClassBare                    // a list member, which has no key at all
	ClassPath                    // a dotted scope chain, `root.culture`
	ClassName                    // a name the corpus defines, in Kinds
	ClassLoc                     // a localization key and nothing else
	ClassUnresolved              // name-shaped, and nothing explains it
	ClassBlockNames              // a block holding keyed children
	ClassBlockList               // a block holding bare members
	ClassBlockEmpty              // `{ }`
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
	ClassEphemeral:  {SegCite, false, false},
	ClassVar:        {SegVar, false, false},
	ClassStop:       {"", true, false},
	ClassDeclared:   {"", false, false},
	ClassMacroCall:  {"", false, false},
	ClassDefinition: {SegKind, false, false},
	ClassBare:       {SegBare, false, false},
	ClassPath:       {"", false, false},
	ClassName:       {"", false, false},
	ClassLoc:        {"", false, false},
	ClassUnresolved: {"", false, false},
	ClassBlockNames: {"", false, false},
	ClassBlockList:  {"", false, false},
	ClassBlockEmpty: {"", false, false},
}

// Nameable reports a class unknown-key may name.
func (c Class) Nameable() bool { return classes[c].nameable }

// Token is one written word as the classifier read it: its class, the databases a name or cite may belong to (LabelLoc among them when the word is also a localization key), and the id inside a cite.
type Token struct {
	Class Class
	Kinds []string
	ID    string
}

// At is the one kind a token has at a cell: what the cell agrees on, a cite's own, or the single database a name belongs to.
func (t Token) At(cell *Node) string {
	if cell != nil {
		for _, k := range cell.Kinds {
			if k != LabelLoc && slices.Contains(t.Kinds, k) {
				return k
			}
		}
	}
	if t.Class == ClassCite || t.Class == ClassEphemeral {
		return t.Kinds[0]
	}
	if len(t.Kinds) == 1 && t.Kinds[0] != LabelLoc || len(t.Kinds) == 2 && t.Kinds[1] == LabelLoc {
		return t.Kinds[0]
	}
	return ""
}

// Defined answers what the corpus defines, for the value half of the classifier: the databases a name is a row of, and whether it is a localization key.
type Defined interface {
	Kinds(name string) []string
	IsLoc(name string) bool
}

// Classify is the one reading of a word. A key passes rows, which says the block above holds a database, and no names; a value passes the names it may resolve against. The cell it sits at is not consulted here: Token.At applies it.
func (s *Tree) Classify(text string, quoted, rows bool, names Defined) Token {
	switch {
	case text == "":
		return Token{Class: ClassBare}
	case quoted:
		return Token{Class: ClassVar, Kinds: s.locOnly(text, names)}
	case jomini.IsDigits(text):
		return Token{Class: ClassNumber, ID: text, Kinds: s.named(text, names)}
	}
	switch jomini.Literal(text) {
	case jomini.LitDate:
		return Token{Class: ClassDate}
	case jomini.LitNumber:
		return Token{Class: ClassNumber, Kinds: s.locOnly(text, names)}
	}
	if kind, id, ok := game.ParseTyped(s.gameID, text); ok {
		if k := s.PrefixKind(strings.SplitN(text, ":", 2)[0]); k != "" {
			kind = k
		}
		return Token{Class: ClassCite, Kinds: []string{jomini.CanonicalKind(kind)}, ID: id}
	}
	if p, ok := jomini.ParsePrefixed(text); ok {
		return Token{Class: ClassEphemeral, Kinds: []string{cmp.Or(jomini.PrefixKind(p.Prefix), p.Prefix)}, ID: p.Name}
	}
	switch {
	case strings.ContainsAny(text, "$@"):
		return Token{Class: ClassVar}
	case jomini.IsStop(text):
		return Token{Class: ClassStop}
	}
	if names == nil {
		// A key: a row where the block above holds a database, else the engine's API, else a field.
		if rows {
			return Token{Class: ClassDefinition}
		}
		if bits, macro := s.bodyClass(text); bits != 0 {
			if macro {
				return Token{Class: ClassMacroCall}
			}
			return Token{Class: ClassDeclared}
		}

		// A link or a modifier the game declares keeps its own card rather than becoming a field of the block.
		if s.decl != nil && s.decl.HasName(text) {
			return Token{Class: ClassDeclared}
		}
		return Token{Class: ClassPlain}
	}

	// A value: an interpolation or a chain is grammar; a name is what the corpus defines it as; a localization key; a name nothing explains.
	if strings.ContainsAny(text, "|;") || strings.ContainsRune(text, '.') && !jomini.IsEventID(text) {
		return Token{Class: ClassPath, Kinds: s.locOnly(text, names)}
	}
	kinds := s.named(text, names)
	switch {
	case len(kinds) > 1 || len(kinds) == 1 && kinds[0] != LabelLoc:
		return Token{Class: ClassName, Kinds: kinds, ID: text}
	case len(kinds) == 1:
		return Token{Class: ClassLoc, Kinds: kinds}
	case loc.LooksLikeKey(text) || jomini.IsDefName(text):
		return Token{Class: ClassUnresolved}
	}
	return Token{Class: ClassPlain}
}

// named is the databases defining a name, the loc label last.
func (s *Tree) named(text string, names Defined) []string {
	if names == nil {
		return nil
	}
	out := slices.Clone(names.Kinds(text))
	if names.IsLoc(text) {
		out = append(out, LabelLoc)
	}
	return out
}

func (s *Tree) locOnly(text string, names Defined) []string {
	if names != nil && names.IsLoc(text) {
		return []string{LabelLoc}
	}
	return nil
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
	if seg := classes[s.Classify(key, quoted, rows, nil).Class].seg; seg != "" {
		return seg
	}
	return key
}

// ---------------------------------------------------------------- the values

// resolver is everything outside a file that typing one value consults: the tree, the definitions and the localization.
type resolver struct {
	tree       *Tree
	kindsByKey map[string][]string
	locKeys    map[string]bool
}

func (r *resolver) Kinds(name string) []string { return r.kindsByKey[name] }
func (r *resolver) IsLoc(name string) bool     { return r.locKeys[name] }

// add folds one occurrence into a node: its class counted, and the databases it named intersected with what came before. A cite names its own database and says nothing about the position; a number names rows only in a data file. Loc takes part in the intersection like a database: one value in three hundred also being a key made 747,513 CK3 loc references out of 451,232.
func (n *Node) add(t Token, data bool, count int32) {
	n.N += count
	if n.counts == nil {
		n.counts = map[Class]int32{}
	}
	n.counts[t.Class] += count
	kinds := t.Kinds
	switch t.Class {
	case ClassCite, ClassEphemeral, ClassStop, ClassUnresolved, ClassBlockNames, ClassBlockList, ClassBlockEmpty:
		return
	case ClassNumber:
		if !data && slices.Contains(kinds, LabelLoc) {
			kinds = []string{LabelLoc}
		} else if !data {
			return
		}
	}
	if len(kinds) == 0 {
		return
	}
	kinds = slices.Clone(kinds)
	slices.Sort(kinds)
	if !n.seen {
		n.inter, n.seen = kinds, true
		return
	}
	n.inter = intersect(n.inter, kinds)
}

// settle publishes what the fold accumulated and drops its working state, over the whole tree: the kinds, whether a literal or an unexplained name was ever written here, and whether the position is a block more often than a value.
func (n *Node) settle() {
	if n.seen || len(n.counts) > 0 {
		n.Kinds, n.inter = n.inter, nil
		best, at := int32(-1), ClassPlain
		for c, k := range n.counts {
			switch c {
			case ClassNumber, ClassDate, ClassVar, ClassPath, ClassPlain:
				n.Flags |= Literal
			case ClassUnresolved:
				n.Flags |= Unresolved
			}
			if k > best || k == best && c.block() && !at.block() {
				best, at = k, c
			}
		}
		if at.block() {
			n.Flags |= IsBlock
		}
		n.counts, n.seen = nil, false
	}
	for _, c := range n.Kids {
		c.settle()
	}
}

func (c Class) block() bool { return c >= ClassBlockNames }

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
	if len(src.counts) > 0 && n.counts == nil {
		n.counts = make(map[Class]int32, len(src.counts))
	}
	for c, k := range src.counts {
		n.counts[c] += k
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
func (s *Tree) bind(decl *declared.Schema, keysByKind map[string]map[string]bool) {
	for prefix, scope := range decl.Prefixes() {
		if kind := nameMatchKind(scope, keysByKind); kind != "" {
			k := s.Root(kind, true).kind()
			k.Scope = scope
			if !slices.Contains(k.Prefixes, prefix) {
				k.Prefixes = append(k.Prefixes, prefix)
				slices.Sort(k.Prefixes)
			}
		}
	}
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
	out := map[string]string{}
	s.eachKind(func(kind string, k *Kind) bool {
		for _, p := range k.Prefixes {
			out[strings.ToLower(p)] = kind
		}
		return true
	})
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
			if n.Class == Structure || classes[s.Classify(m, false, false, nil).Class].member {
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
