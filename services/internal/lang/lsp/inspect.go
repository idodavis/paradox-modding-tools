// inspect.go answers Hover: one probe, one resolver order (resolve.go), then
// one card table. Every identity class the resolvers produce has exactly one
// row here, so what a card says is decided by the class, never by the order the
// branches happened to be written in.

package lsp

import (
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

const inspectValueCap = 8

type identity struct {
	path, name, kind, owner, locFilter, extractKind string
	// cell is the folded block path the cursor sits at, which is what the
	// learned schema files a key or a value under.
	cell string
	// block is the key of the enclosing block, when the cursor sits inside one.
	// What lets a card tell `trigger` in a triggered_desc from `trigger` in a
	// portrait, which the install documents once, by name, for both.
	block string
	// slot is the key holding the value under the cursor, for the floor card.
	slot string
	// docs is prose the identify step worked out itself, for the cards whose
	// content is computed from the cursor's surroundings rather than looked up.
	docs                               string
	def                                *derived.Def
	fieldKey, local                    bool
	spanStart, spanEnd                 int
	assignStart, assignEnd, assignLine int
}

// nestedBlock returns the block that actually governs the cursor, or empty at a
// definition's own level.
//
// Logic operators and scope switches are skipped. `OR = { … }` and
// `scope:secondary_recipient = { … }` group and redirect; neither changes which
// keys are legal, so neither is what a key belongs to. Using them produced cards
// reading "OR key" and "scope:secondary recipient key", which name a construct
// rather than a container.
func nestedBlock(at probe) string {
	if !at.inNested {
		return ""
	}
	for i := len(at.blockPath) - 1; i >= 1; i-- {
		if !transparentBlock(at.blockPath[i]) {
			return at.blockPath[i]
		}
	}
	return ""
}

// blockChain wraps a single enclosing block name as the chain FieldDoc wants, or nil outside any block.
func blockChain(block string) []string {
	if block == "" {
		return nil
	}
	return []string{block}
}

// transparentBlock reports a block that groups or redirects without changing
// which keys may be written inside it.
func transparentBlock(key string) bool {
	if strings.ContainsRune(key, ':') {
		return true // a scope switch: scope:x, culture:y
	}
	if isWeightKey(key) {
		return true // a branch of a distribution: `95 = { … }` is a chance, not a container
	}
	switch strings.ToLower(key) {
	case "or", "and", "not", "nor", "nand", "trigger_if", "trigger_else",
		"trigger_else_if", "if", "else", "else_if", "root", "prev", "this":
		return true
	}
	return false
}

// PrettyKind is the Kind line: CanonicalKind, then underscores to spaces.
func PrettyKind(kind string) string {
	k := jomini.CanonicalKind(kind)
	if k == "" {
		k = kind
	}
	return strings.ReplaceAll(k, "_", " ")
}

// Hover resolves the identifier at (line, UTF-8 column) and fills its card, or
// returns nil where there is nothing to say.
func Hover(s *session.Session, path string, line, col int) *Inspect {
	at, ok := probeAt(s, path, line, col)
	if !ok {
		return nil
	}
	id, ok := identify(s, path, at)
	if !ok {
		return nil
	}
	var ins *Inspect
	for _, c := range cards {
		if c.when(id) {
			ins = c.fill(s, id)
			break
		}
	}
	if ins == nil {
		return nil
	}
	ins.Scope = at.scope
	// A scope link is the one token whose whole purpose is to move somewhere
	// else, so the transition is the useful thing to show -- "holder" answers
	// "character" only once you know it landed you in one.
	if l, ok := s.Install.Declared.Link(id.name); ok && l.Out != "" {
		ins.ScopeOut = l.Out
	}
	if ins.Owner == "" {
		ins.Owner = id.owner
	}
	return ins
}

// card is one identity class and the card that class fills.
type card struct {
	when func(identity) bool
	fill func(*session.Session, identity) *Inspect
}

// cards is the whole card table, in order: the first row that claims the
// identity fills it, and a class no row claims has no card.
var cards = []card{
	{func(id identity) bool { return id.kind == "loc_value" }, locValueCard},
	{func(id identity) bool { return id.kind == jomini.KindScriptParam }, paramCard},
	{func(id identity) bool { return id.kind == "date" || id.kind == "weight" }, proseCard},
	{func(id identity) bool { return jomini.IsEphemeral(id.kind) }, ephemeralCard},
	{func(id identity) bool { return id.local }, localAssignCard},
	{func(id identity) bool { return id.fieldKey }, blockKeyCard},
	{func(id identity) bool { return id.def != nil && id.def.Kind == "loc_key" }, locCard},
	{func(id identity) bool { return id.def != nil }, defCard},
	{func(id identity) bool { return id.kind != "" }, tokenCard},
	{func(id identity) bool { return id.slot != "" }, slotValueCard},
}

// inspectOf fills Kind/Hint/Docs for a resolved identity.
func inspectOf(s *session.Session, kind, key, docs string) *Inspect {
	out := &Inspect{Kind: PrettyKind(kind), Key: key, Docs: docs}
	if docs == "" {
		_, _, out.Hint = declaredLines(s, kind, "", "")
	}
	return out
}

// identify runs the resolver order over the probe and takes the first answer.
// Which SOURCE gets to name a word is data, in resolve.go, so a new source
// cannot quietly outrank a declared one.
func identify(s *session.Session, path string, at probe) (identity, bool) {
	for _, r := range wordResolvers {
		if id, ok := r.resolve(s, path, at); ok {
			// An answer with no name is a stop: something is under the cursor
			// and nothing is claimed about it, so no card is built.
			return id, id.name != ""
		}
	}
	return identity{}, false
}

func ephemeralID(s *session.Session, path, name, kind string) identity {
	if name == "" || kind == "" {
		return identity{}
	}
	return identity{
		path: path, name: name, kind: kind,
		def: s.ResolveOfKind(name, kind),
	}
}

func macroCallIdentity(s *session.Session, path string, at probe) (identity, bool) {
	kind, name, a, ok := macroCallAt(at)
	if !ok || name == "" {
		return identity{}, false
	}
	d := s.ResolveOfKind(name, kind)
	if d != nil {
		ck := jomini.CanonicalKind(kind)
		for _, cand := range s.ModDefsOf(name) {
			if jomini.CanonicalKind(cand.Kind) == ck && derived.SamePath(cand.Path, path) {
				c := cand
				d = &c
				break
			}
		}
	}
	if d == nil && a != nil {
		origin, _, _ := s.Locate(path)
		d = &derived.Def{
			Kind: kind, Key: name, Path: path, Origin: origin,
			Start: a.Key.Range.Start, End: a.Key.Range.End,
			Line: at.res.Lines().PositionAt(a.Key.Range.Start).Line,
		}
	}
	return identity{path: path, name: name, kind: kind, def: d}, true
}

// ------------------------------------------------------------- the card rows

func locValueCard(s *session.Session, id identity) *Inspect {
	ins := inspectOf(s, "loc_value", "$"+id.name+"$", "")
	ins.Body = "format " + id.locFilter
	return ins
}

func paramCard(s *session.Session, id identity) *Inspect {
	ins := inspectOf(s, jomini.KindScriptParam, "$"+id.name+"$", "")
	owner := id.owner
	if owner == "" && id.def != nil {
		owner = id.def.OwnerKey
	}
	ins.Owner = owner
	ins.Values, ins.More = rankInspectValues(s.ValuesOf(jomini.KindScriptParam, owner, id.name))
	if id.def != nil {
		attachSite(s, ins, id.def.Path, id.def.Line, id.def.Origin, id.def.Start)
	}
	return ins
}

// proseCard is the pair whose whole content the resolver worked out: an era date and a weight's share of its list.
func proseCard(_ *session.Session, id identity) *Inspect {
	return &Inspect{Kind: id.kind, Key: id.name, Docs: id.docs}
}

// ephemeralCard is a var, flag, saved scope or global var: its defining site when the workspace has one, and the values the corpus writes for the name.
func ephemeralCard(s *session.Session, id identity) *Inspect {
	key := id.name
	if id.kind == "saved_scope" {
		key = "scope:" + id.name
	}
	ins := inspectOf(s, id.kind, key, "")
	ins.Values, ins.More = rankInspectValues(s.ValuesOf(id.kind, "", id.name))
	if id.def != nil {
		attachSite(s, ins, id.def.Path, id.def.Line, id.def.Origin, id.def.Start)
	}
	return ins
}

func localAssignCard(s *session.Session, id identity) *Inspect {
	docs := s.FieldDoc(id.name, id.extractKind, blockChain(id.block))
	if docs == "" && !s.IsStructureMember(id.extractKind, id.cell, id.name) {
		return nil
	}
	ins := inspectOf(s, id.extractKind, id.name, docs)
	ins.Kind += " key"
	origin, _, _ := s.Locate(id.path)
	attachSite(s, ins, id.path, id.assignLine, origin, id.assignStart)
	return ins
}

// blockKeyCard is a key written inside a block, named by the block type at that
// position: a structure shows its own (structure, field) prose, and a body holds
// the arguments of the token that opens it rather than keys of a container.
//
// The structure is the unit the game documents and the unit the schema unifies,
// so it is also the label: "event key" is true of `type` and of a `trigger` four
// levels down, and the enclosing key as written is not always a container at all
// — a random_list branch was labelling its contents "1 key".
func blockKeyCard(s *session.Session, id identity) *Inspect {
	structure := id.block
	// A folded segment names a position rather than a structure a reader could
	// type, so the written container stands in for it.
	if b := s.BlockAt(id.extractKind, id.cell); b != nil && !strings.ContainsAny(b.Key, "<.") {
		structure = b.Key
	}
	label, suffix := structure, " key"
	if label == "" {
		label = id.extractKind
	}
	// A block that is itself a declared token holds that token's arguments:
	// "set variable key" reads as though set_variable were a container.
	if s.Install.Declared.HasName(structure) {
		suffix = " argument"
	}
	ins := inspectOf(s, label, id.name, s.FieldDoc(id.name, id.extractKind, blockChain(structure)))
	ins.Kind += suffix
	// composeHint describes the KIND — its scope type, how it is cited, how its
	// localization keys are named. None of that is about the field under the
	// cursor: hovering `color2` inside a coat of arms reported the coat of arms'
	// localization convention as though it described `color2`.
	ins.Hint = ""
	// A key can be a declared token as well: `add_to_list` reaches this row
	// because it is also a structure key of the kind being edited, and the
	// signature the game states for it was being dropped on the floor.
	ins.Usage = s.TokenUsage(id.name)
	_, ins.Accepts, _ = declaredLines(s, id.extractKind, id.cell, id.name)
	return vanillaSite(s, ins)
}

func locCard(s *session.Session, id identity) *Inspect {
	text, _ := s.DefaultLoc(id.name)
	file, line, start, origin, ok := s.LocSite(id.name)
	if text == "" && !ok {
		return nil
	}
	ins := inspectOf(s, "loc_key", id.name, "")
	ins.Body = unescapeLocDisplay(text)
	if ok {
		// start, not 0. attachSite only computes Col when start > 0, and
		// languageClient gates the whole selection object on Col — so passing 0
		// shipped the right Line and then dropped it, opening every
		// localization hover at line 1.
		attachSite(s, ins, file, line, origin, start)
		attachOverlay(s, ins, &derived.Def{
			Kind: "loc_key", Key: id.name, Path: file, Line: line, Origin: origin,
		})
	}
	return ins
}

func defCard(s *session.Session, id identity) *Inspect {
	d := id.def
	docs := docsOnly(s, id.name, d.Kind)
	// A named script body has no other description: the author's comment above
	// it is the only one there will ever be.
	if docs == "" && d.Doc != "" {
		docs = d.Doc
	}
	ins := inspectOf(s, d.Kind, d.Key, docs)
	ins.Usage = s.TokenUsage(id.name)
	if ins.Usage == "" {
		ins.Usage = s.MacroUsage(d)
	}
	// First hit wins: conventions are sorted by coverage, then by the plainest shape.
	for _, key := range loc.Expand(d.Key, s.LocConventionsOf(d.Kind)) {
		if text, ok := s.DefaultLoc(key); ok && text != "" {
			ins.Body = unescapeLocDisplay(text)
			break
		}
	}
	attachSite(s, ins, d.Path, d.Line, d.Origin, d.Start)
	attachOverlay(s, ins, d)
	return ins
}

// tokenCard is an engine token, link or modifier the game declares.
func tokenCard(s *session.Session, id identity) *Inspect {
	ins := inspectOf(s, id.kind, id.name, docsOnly(s, id.name, id.kind))
	ins.Usage = s.TokenUsage(id.name)
	_, ins.Accepts, _ = declaredLines(s, id.kind, id.cell, id.name)
	return vanillaSite(s, ins)
}

// slotValueCard is the floor: a value nothing in the model names, described by
// the key that holds it. `add_character_flag = born_as_taltos` says what
// born_as_taltos is even though no database defines it, and returning nothing
// there was the commonest empty hover in the 2026-09-17 sample.
func slotValueCard(s *session.Session, id identity) *Inspect {
	ins := inspectOf(s, id.slot, id.name, s.FieldDoc(id.slot, id.extractKind, blockChain(id.block)))
	ins.Kind += " value"
	ins.Hint = ""
	_, ins.Accepts, _ = declaredLines(s, id.extractKind, id.cell, id.slot)
	return ins
}

// ----------------------------------------------------------- the one ladder

// declaredLines is the one ordered ladder the card's three declared lines share:
// the signature the game states for a name, what the value at that position may
// be, and what the name's kind is. They were three functions with three copies
// of the same precedence — declared before derived, specific before general —
// and three copies is three chances to disagree about which source outranks
// which. One pass, one order, three lines.
func declaredLines(s *session.Session, kind, cell, name string) (sig, accepts, hint string) {
	// 1. The declared signature of the name itself: token, then scope link,
	// then modifier, then the raw scopes text of a game with no type system.
	if t, role, ok := s.Install.Declared.Token(name); ok {
		parts := []string{strings.ToUpper(role[:1]) + role[1:]}
		if in := scopeList(t.In); in != "" {
			parts = append(parts, "runs on "+in)
		}
		if t.Target != "" {
			parts = append(parts, "takes a "+PrettyKind(t.Target))
		}
		sig = strings.Join(parts, " · ")
	} else if l, ok := s.Install.Declared.Link(name); ok && l.Out != "" {
		var parts []string
		if in := scopeList(l.In); in != "" {
			parts = append(parts, "from "+in)
		} else if l.Global {
			parts = append(parts, "Global link")
		}
		parts = append(parts, "yields "+PrettyKind(l.Out))
		if l.Data {
			parts = append(parts, "cite `"+name+":id`")
		}
		sig = strings.Join(parts, " · ")
	} else if m, ok := s.Install.Declared.Modifier(name); ok {
		// EU5 declares a category for all 2,436 of its modifiers and prose for
		// none, so the category list is the whole card.
		if areas := modifierAreas(m.Area); len(areas) > 0 {
			sig = "Modifier · applies to " + strings.Join(areas, " or ")
		}
	} else if sc := s.Install.Declared.ScopeText(name); sc != "" {
		sig = "Scope here: " + sc
	}

	// 2. What the value may be. The declared target names a scope type and, when
	// a database is bound to it, the citation form — one accept that produces a
	// valid value. Below it script_docs' own slot, then the game's written
	// documentation, then the learned cell, which is only ever a sample of how
	// vanilla happens to use the field.
	r, isSlot := jomini.ScriptName(name)
	dt := s.DeclaredFieldType(kind, name)
	cellKind := s.FieldValueKind(kind, cell, name)
	if target := s.TokenTarget(name); target != "" {
		cite, k := s.CiteFormOfScope(target)
		switch {
		case k != "" && cite != "":
			accepts = "accepts a " + PrettyKind(k) + ", cited `" + cite + ":<id>`"
		case k != "":
			accepts = "accepts a " + PrettyKind(k)
		default:
			accepts = "accepts a " + PrettyKind(target)
		}
	} else if isSlot && r.Kind != "" {
		accepts = "accepts a " + PrettyKind(r.Kind)
	} else if dt.Known() {
		switch {
		case len(dt.Values) > 0:
			accepts = "one of: " + strings.Join(dt.Values, ", ")
		case dt.Array:
			accepts = "accepts several " + dt.Prim + " values"
		default:
			accepts = "accepts a " + dt.Prim
		}
	} else if cellKind != "" && !jomini.IsEphemeral(cellKind) {
		accepts = "accepts a " + PrettyKind(cellKind)
	} else if loc.Classify(name) != loc.PropNone {
		// A field holding a localization key takes any key, not one of a set.
		accepts = "accepts a localization key"
	}

	// 3. What the kind is, for a card whose subject has no prose of its own: the
	// declared scope type first, then how the object is cited and nested.
	ck := jomini.CanonicalKind(kind)
	if ck == "" {
		ck = kind
	}
	if languageKind(ck) {
		return sig, accepts, ""
	}
	var parts []string
	if scope := s.ScopeOfKind(ck); scope != "" {
		parts = append(parts, PrettyKind(scope)+" scope")
	}
	if p := s.Install.Docs.KindInfo[ck]; p != "" {
		parts = append(parts, firstProse(p))
	} else if p := s.Install.Docs.KindInfo[kind]; p != "" {
		parts = append(parts, firstProse(p))
	}
	if prefix := s.Work.Schema.KindPrefix[ck]; prefix != "" {
		parts = append(parts, "cite `"+prefix+":id`")
	}
	for _, sh := range s.Work.Schema.Nested {
		if jomini.CanonicalKind(sh.ChildKind) == ck {
			msg := "nested in " + PrettyKind(sh.ParentKind)
			if sh.GroupKey != "" {
				msg += " `" + sh.GroupKey + "`"
			}
			parts = append(parts, msg)
			break
		}
	}
	// No localization-convention line: which keys an object should have, and
	// which are missing, is a list with per-key state that belongs in the
	// Inspector pane, and `required-loc` already reports the ones the game demands.
	if jomini.IsMacroKind(ck) {
		// The real signature, when the macro takes parameters, is MacroUsage and
		// shown as a code block; this is the shape note for one that takes none.
		parts = append(parts, "invoked by name")
	}
	if game.IsFIOS(s.GameID, ck) || game.IsFIOS(s.GameID, kind) {
		parts = append(parts, "FIOS")
	}
	return sig, accepts, strings.Join(parts, " · ")
}

// docsOnly is the prose a name carries plus the signature the game declares for it.
func docsOnly(s *session.Session, word, kind string) string {
	doc := s.FieldDoc(word, kind, nil)
	if doc == "" {
		// Install prose and dump prose both land in TokenDoc during a scan, but
		// the Schema carries its own description; use it rather than relying on
		// that map having been filled.
		doc = s.SchemaDoc(word)
	}
	line, _, _ := declaredLines(s, kind, "", word)
	if line == "" {
		return doc
	}
	if doc != "" {
		doc += "\n\n"
	}
	return doc + line
}

// modifierAreas splits a declared category list into the scope types it names.
//
// The three games phrase it differently — CK3 "character and province", EU5
// "location, all", Vic3 a Mask — so the separators of all three are accepted.
// "all" is dropped: EU5 stamps it on every one of its 2,436 modifiers, so it
// distinguishes nothing and would only push the useful half off the line.
func modifierAreas(area string) []string {
	if area == "" {
		return nil
	}
	fields := strings.FieldsFunc(area, func(r rune) bool {
		return r == ',' || r == ';' || r == '/' || r == '|'
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		for part := range strings.SplitSeq(strings.TrimSpace(f), " and ") {
			p := strings.ToLower(strings.TrimSpace(part))
			if p == "" || p == "all" || p == "none" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, PrettyKind(p))
		}
	}
	return out
}

// scopeList renders declared input scopes. "none" means unrestricted, so it
// carries no information worth showing.
func scopeList(in []string) string {
	keep := make([]string, 0, len(in))
	for _, sc := range in {
		if sc == "none" || sc == "" {
			continue
		}
		keep = append(keep, PrettyKind(sc))
	}
	return strings.Join(keep, " or ")
}

// ---------------------------------------------------------------- the sites

func attachOverlay(s *session.Session, ins *Inspect, d *derived.Def) {
	if ins == nil || d == nil || isVanillaOrigin(d.Origin) || d.Kind == "mod_descriptor" {
		return
	}
	var v *derived.Def
	for _, cand := range s.VanillaDefs(d.Key) {
		if cand.Kind == d.Kind {
			c := cand
			v = &c
			break
		}
	}
	if v == nil || v.Path == "" || derived.SamePath(v.Path, d.Path) {
		return
	}
	ins.VanillaPath = v.Path
	ins.VanillaRel = s.DisplayRel(v.Path)
	ins.VanillaLine = v.Line
	ins.VanillaOriginName = s.OriginName(game.OriginVanilla)
	if v.Start > 0 {
		ins.VanillaCol = s.Parsed(v.Path).Lines().PositionAt(v.Start).Character
	}
}

func isVanillaOrigin(origin string) bool {
	return origin == "" || origin == game.OriginVanilla
}

func vanillaSite(s *session.Session, ins *Inspect) *Inspect {
	if ins == nil {
		return nil
	}
	ins.Origin = game.OriginVanilla
	ins.OriginName = s.OriginName(game.OriginVanilla)
	return ins
}

func attachSite(s *session.Session, ins *Inspect, path string, line int, origin string, start int) {
	if ins == nil || path == "" {
		return
	}
	ins.Path = path
	ins.Rel = s.DisplayRel(path)
	ins.Line = line
	if origin == "" {
		origin = game.OriginVanilla
	}
	ins.Origin = origin
	ins.OriginName = s.OriginName(origin)
	if start > 0 {
		ins.Col = s.Parsed(path).Lines().PositionAt(start).Character
	}
}

func rankInspectValues(counts map[string]int) ([]InspectValue, int) {
	type pair struct {
		text string
		n    int
	}
	var ps []pair
	for t, n := range counts {
		if t != "" && n > 0 {
			ps = append(ps, pair{t, n})
		}
	}
	slices.SortFunc(ps, func(a, b pair) int {
		if a.n != b.n {
			return b.n - a.n
		}
		return strings.Compare(a.text, b.text)
	})
	more := 0
	if len(ps) > inspectValueCap {
		more = len(ps) - inspectValueCap
		ps = ps[:inspectValueCap]
	}
	out := make([]InspectValue, len(ps))
	for i, p := range ps {
		out[i] = InspectValue{Text: p.text, Count: p.n}
	}
	return out, more
}

func unescapeLocDisplay(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		default:
			b.WriteByte(s[i])
			continue
		}
		i++
	}
	return strings.TrimSpace(b.String())
}

func languageKind(k string) bool {
	switch k {
	case "effect", "trigger", "scope", "field", "loc_key", "loc_value",
		"script_param", "saved_scope", "data_function", "mod_descriptor":
		return true
	}
	return false
}

func firstProse(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// isWeightKey reports a bare non-negative number used as an assignment key.
// Dates (`1066.1.1`) and event ids (`ns.1`) are not bare numbers and are
// unaffected.
func isWeightKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return false
		}
	}
	return true
}

// weightShare explains a weight as its share of the list it sits in. The
// denominator is the sum of the sibling weights, which is not always 100 —
// `random_list = { 10 = {…} 30 = {…} }` is a quarter and three quarters — so it
// is computed rather than assumed, and the list is named from the file rather
// than from any list of known weighted effects.
func weightShare(parent *jomini.Assignment, listName, weight string) string {
	if parent == nil {
		return ""
	}
	b := jomini.ChildBlock(parent)
	if b == nil {
		return ""
	}
	var total, self float64
	n := 0
	for _, st := range b.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted || !isWeightKey(a.Key.Text) {
			continue
		}
		// Each branch of a weighted list is a body. Requiring that separates a
		// distribution from a numeric lookup table, whose entries are scalars.
		if jomini.ChildBlock(a) == nil {
			continue
		}
		v, err := strconv.ParseFloat(a.Key.Text, 64)
		if err != nil {
			continue
		}
		total += v
		n++
		if a.Key.Text == weight {
			self = v
		}
	}
	if n < 2 || total <= 0 {
		return ""
	}
	out := trimFloat(self) + " of " + trimFloat(total)
	if listName != "" {
		out += " in `" + listName + "`"
	}
	return out + " — a " + trimFloat(self*100/total) + "% chance"
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// dottedSegmentAt returns the segment of a dotted chain containing off, or false
// when the text is not a chain.
//
// `holder.culture` is two hops, and a reader hovering either half is asking
// about that half. Splitting is cheap and the alternative is what happened
// before: the whole string resolved to nothing and the card never appeared.
func dottedSegmentAt(text string, off int) (string, bool) {
	if off < 0 || off > len(text) || !strings.ContainsRune(text, '.') {
		return "", false
	}
	start := strings.LastIndexByte(text[:off], '.') + 1
	end := strings.IndexByte(text[off:], '.')
	if end < 0 {
		end = len(text)
	} else {
		end += off
	}
	seg := text[start:end]
	if seg == "" {
		return "", false
	}
	return seg, true
}

// segmentIdentity names one segment of a dotted chain.
//
// The two named resolvers rather than a copy of their bodies: this function's
// first half was byte-identical to resolveLanguagePrefix and its second half to
// resolveVocabulary, which made it the third place the resolution order was
// written. A copy cannot be re-ordered when the canonical list is.
func segmentIdentity(s *session.Session, path, seg string) (identity, bool) {
	at := probe{word: seg}
	for _, r := range []wordResolver{
		{"language-prefix", resolveLanguagePrefix},
		{"vocabulary", resolveVocabulary},
	} {
		if id, ok := r.resolve(s, path, at); ok {
			if id.path == "" {
				id.path = path
			}
			return id, true
		}
	}
	return identity{}, false
}
