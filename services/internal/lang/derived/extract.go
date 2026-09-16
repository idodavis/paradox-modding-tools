// extract.go is the per-file catalog extractor (scan, BuildIndex, session
// reindex). One jomini.Walk turns a parsed file into FileFacts; everything a
// file contributes -- defs, refs, edges, counters, votes -- is a fold over that
// slice, and nothing below walks the CST.

package derived

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// neverAKey is the subset of the stoplist that is not a key anywhere -- a value, an operand, or a scope word -- so it is excluded from the nested table too.
var neverAKey = map[string]bool{
	"this": true, "root": true, "prev": true, "from": true,
	"yes": true, "no": true,
}

// EdgeKindCall marks a scripted-effect invocation (not drawn on the canvas).
const EdgeKindCall = "call"

// EdgeKindVia marks a hop synthesized through a chain of macro calls.
const EdgeKindVia = "via"

// CallCandidate is a possible scripted_* invocation; ApplyCallEdges keeps effects for the graph, ApplyCallRefs keeps trigger/effect/modifier refs.
type CallCandidate struct {
	From, To, Path   string
	Line, Start, End int
}

// Fact is one statement of a parsed file: an assignment (Key set) or a bare value (Key ""). Start/End address the key token, or the bare value; VStart/VEnd the scalar right-hand side. The children of a block are the facts whose Parent is its index. Field order is by width: an install is tens of millions of facts and the padding is real.
type Fact struct {
	Key    string // assignment key as written; "" for a bare value statement
	Op     string // "=", "?=", "==", "<", ...; "" for a bare value (defs need "=" or "?=")
	Value  string // scalar RHS or bare value; "" when the RHS is a block
	Parent int32  // index of the enclosing assignment fact; -1 at top level
	// Line/Start/End and VLine/VStart/VEnd are the two token spans a fold turns into a Def or a Ref.
	Line, Start, End    int32
	VLine, VStart, VEnd int32
	Block               bool // the RHS is a block, tagged (rgb/hsv) or plain
	// Quoted is the key's quoting (the bare value's, for a value statement), VQuoted the scalar RHS's: a quoted key is never a definition and a quoted value is never a citation, and the two answers differ within one statement.
	Quoted, VQuoted bool
	Depth           uint8
}

// internShards is how many maps the interner is split across: one map behind one lock turned eight workers into one, and cost more time than the strings saved.
const internShards = 64

// interner hands out one shared copy of every key and value in the corpus. A fact's strings are slices of the file's text, so one definition, one counter or one site pins that whole file until the model is dropped; interning the facts as they arrive frees a file's text with its parse tree and folds the corpus's repeats into one copy each.
type interner [internShards]struct {
	mu   sync.RWMutex
	seen map[string]string
	_    [24]byte // keeps two shards' locks off one cache line
}

func (in *interner) of(s string) string {
	if s == "" {
		return s
	}
	// A sample of the string rather than all of it: the map hashes the whole thing again anyway, and these are short.
	h := uint(len(s))*131 + uint(s[0])
	h = h*131 + uint(s[len(s)-1])
	sh := &in[h&(internShards-1)]
	sh.mu.RLock()
	c, ok := sh.seen[s]
	sh.mu.RUnlock()
	if ok {
		return c
	}
	c = strings.Clone(s)
	sh.mu.Lock()
	if was, ok := sh.seen[c]; ok {
		c = was
	} else {
		if sh.seen == nil {
			sh.seen = map[string]string{}
		}
		sh.seen[c] = c
	}
	sh.mu.Unlock()
	return c
}

// facts interns the two fields every later fold reads, which is every string a file's harvest goes on to keep.
func (in *interner) facts(ff *FileFacts) {
	for i := range ff.Facts {
		ff.Facts[i].Key = in.of(ff.Facts[i].Key)
		ff.Facts[i].Value = in.of(ff.Facts[i].Value)
	}
}

// fileID is what a definition, a reference or a vote needs to know about the file it came from. Rule is game.MatchExtract's answer for the file: the kind members are filed under, and the form this folder's definitions take.
type fileID struct {
	GameID, Path, Origin string
	Rule                 game.ExtractRule
}

// FileFacts is the one product of parsing a file. Src stays because a leading comment is not a statement and so cannot be a fold over Facts.
type FileFacts struct {
	fileID
	Src   string
	Facts []Fact
}

// FactsOf turns one parsed file into its facts, in one jomini.Walk.
func FactsOf(gameID, path, rel, origin string, res jomini.Result) FileFacts {
	ff := FileFacts{
		fileID: fileID{
			GameID: gameID, Path: filepath.Clean(path), Origin: origin,
			Rule: game.MatchExtract(gameID, rel),
		},
		Src: res.Src,
	}
	if res.Root == nil {
		return ff
	}
	// A statement is about twelve bytes of script, so the source sizes the slice and append never has to copy it.
	ff.Facts = make([]Fact, 0, len(res.Src)/12+8)
	// A pre-order walk asks for offsets in source order, so the line is a cursor over Src rather than a binary search per token. The break rule is NewLineIndex's: a newline, or a carriage return not part of one.
	src, line, at := res.Src, int32(0), 0
	lineAt := func(off int32) int32 {
		for ; at < int(off) && at < len(src); at++ {
			if c := src[at]; c == '\n' ||
				(c == '\r' && (at+1 == len(src) || src[at+1] != '\n')) {
				line++
			}
		}
		return line
	}
	// open[d] is the fact open at depth d; jomini.Walk is pre-order, so it parents everything arriving at d+1.
	var open []int32
	jomini.Walk(res.Root, func(st jomini.Statement, depth int, _ *jomini.Block) bool {
		f := Fact{Parent: -1, Depth: uint8(min(depth, 255))}
		var val jomini.Value
		if depth > 0 && depth <= len(open) {
			f.Parent = open[depth-1]
		}
		switch n := st.(type) {
		case *jomini.Assignment:
			f.Key, f.Op, f.Quoted = n.Key.Text, n.Op, n.Key.Quoted
			f.Line = lineAt(int32(n.Key.Range.Start))
			f.Start, f.End = int32(n.Key.Range.Start), int32(n.Key.Range.End)
			val = n.Value
		case *jomini.ValueStmt:
			f.Line = lineAt(int32(n.Range.Start))
			f.Start, f.End = int32(n.Range.Start), int32(n.Range.End)
			val = n.Value
		}
		switch v := val.(type) {
		case *jomini.Scalar:
			f.Value, f.VQuoted = v.Text, v.Quoted
			f.VLine = lineAt(int32(v.Range.Start))
			f.VStart, f.VEnd = int32(v.Range.Start), int32(v.Range.End)
			if f.Key == "" {
				f.Quoted, f.Line, f.Start, f.End = v.Quoted, f.VLine, f.VStart, f.VEnd
			}
		case *jomini.TaggedBlock, *jomini.Block:
			f.Block = true
		}
		for len(open) <= depth {
			open = append(open, -1)
		}
		open[depth] = int32(len(ff.Facts))
		ff.Facts = append(ff.Facts, f)
		return true
	})
	return ff
}

// A fold reads the facts under i as `for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++`: a pre-order walk makes a subtree contiguous, and the first fact past it is a sibling or an uncle, whose parent is therefore older than i.
func (id fileID) def(kind, key string, line, start, end int32) Def {
	return Def{
		Kind: jomini.CanonicalKind(kind), Key: key, Path: id.Path,
		Line: int(line), Start: int(start), End: int(end), Origin: id.Origin,
	}
}

func (id fileID) ref(kind, key string, line, start, end int32) Ref {
	return Ref{
		Key: key, Kind: kind, Path: id.Path,
		Line: int(line), Start: int(start), End: int(end),
	}
}

// FileExtract is one parsed file's catalog harvest.
type FileExtract struct {
	Defs  []Def
	Refs  []Ref
	Edges []Edge
	Cands []CallCandidate
	Loc   LocDelta
	// MemberKind is the file's kind, for both member-name tables.
	MemberKind   string
	StructCounts map[string]int
	// StructNested is block key -> member key -> occurrences, so a member is known under the block it belongs to rather than only under its file's kind.
	StructNested map[string]map[string]int
	// StructNestedKind is the kind whose files these blocks were seen in; a block name means different things under different kinds, so a check that judges an absence needs to know whether the name is ambiguous.
	StructNestedKind string
	StructBlocks     map[string]int
	Vocab            map[string]bool
	GUITypes         map[string]bool
	GUIProps         map[string]bool
	// FieldRHS and FieldListRHS are the values each field takes, for the two passes that type a field. Harvested here so they inherit the extract rule's gate: a file the game gives no kind to is not evidence about any field.
	FieldRHS     map[string]map[string]bool
	FieldListRHS map[string]map[string]bool
	// LocOrder is the file's index in the walk list, i.e. its load order; it decides which value survives when two files define one key.
	LocOrder int
}

// extractLoc turns a localization file into loc_key defs, values, and `$key$` refs.
func extractLoc(absPath, content, origin string) (defs []Def, locd LocDelta, refs []Ref) {
	r := loc.Parse(content)
	lang := loc.LanguageOf(absPath, r.Language)
	vals := map[string]LocEntry{}
	for _, e := range r.Entries {
		defs = append(defs, Def{
			Kind: "loc_key", Key: e.Key, Path: absPath, Line: e.Line,
			Start: e.KeyRange.Start, End: e.KeyRange.End, Origin: origin,
		})
		v := e.Value
		if len(v) > locValueLimit {
			v = v[:locValueLimit]
		}
		vals[e.Key] = LocEntry{Value: v, Path: absPath, Line: e.Line}
		for _, ip := range loc.Interps(e.Value, e.ValueRange.Start) {
			if loc.IsLocEngineValue(ip.Key, ip.Filter) {
				continue
			}
			refs = append(refs, Ref{
				Key: ip.Key, Kind: "loc", Path: absPath,
				Line: e.Line, Start: ip.KeyRange.Start, End: ip.KeyRange.End,
			})
		}
		// Broad: a concept name reads a key, but the engine also takes names the loc files never define.
		for _, ip := range loc.DataFnKeys(e.Value, e.ValueRange.Start) {
			refs = append(refs, Ref{
				Key: ip.Key, Kind: "loc-broad", Path: absPath,
				Line: e.Line, Start: ip.KeyRange.Start, End: ip.KeyRange.End,
			})
		}
	}
	return defs, LocDelta{Lang: lang, Vals: vals}, refs
}

// ExtractFile extracts one decoded (CR-free) file, choosing the loc or script path from the game's extract rule; loc files skip the script parse.
func ExtractFile(gameID, absPath, rel, origin, text string) FileExtract {
	text = jomini.Normalize(text)
	if game.MatchExtract(gameID, rel).Mode == game.ModeLocKey {
		var ex FileExtract
		ex.Defs, ex.Loc, ex.Refs = extractLoc(filepath.Clean(absPath), text, origin)
		return ex
	}
	return ExtractParsed(gameID, absPath, rel, origin, jomini.Parse(text), nil)
}

// ExtractParsed extracts defs, refs, edges, and call candidates from a parsed script or GUI file. overlay supplies vanilla derived for a live reindex; a nil overlay derives from this file alone. Callers turn Cands into edges via ApplyCallEdges.
func ExtractParsed(
	gameID, absPath, rel, origin string,
	res jomini.Result,
	overlay *VanillaCache,
) FileExtract {
	var schema *declared.Schema
	if overlay != nil {
		schema = overlay.Schema
	}
	return ExtractFacts(
		FactsOf(gameID, absPath, rel, origin, res), schema, derivedFromCache(overlay))
}

// ExtractFacts folds one file's facts into its catalog harvest. A nil derived.CitedByKind means the caller holds this file alone, so the file's own derivations are merged over the overlay first -- which is all a live single-file reindex can see, and the one branch that writes to derived.
func ExtractFacts(ff FileFacts, schema *declared.Schema, derived *Derived) FileExtract {
	if derived == nil {
		derived = derivedFromCache(nil)
	}
	ex := foldForms(ff)
	fs := foldSites(ff, schema)
	fs.defs = ex.Defs
	cited := derived.CitedByKind
	if cited == nil {
		idx := newDefIndex(fs.defs)
		derived.mergeLocal(deriveFacts(fs, idx, schema))
		derived.mergeLocal(&Derived{FieldValueKinds: deriveFieldValueKinds(ex.FieldRHS, idx)})
		cited = citedIDsByKind(derived.NestedShapes, fs.cites, derived.PrefixKinds)
	}
	resolveNests(fs, derived, cited)
	ex.Defs = fs.defs
	bodyRefs, edges, cands := resolveSites(fs, derived)
	ex.Refs = append(ex.Refs, bodyRefs...)
	ex.Edges, ex.Cands = edges, cands
	return ex
}

// foldForms harvests the definition forms the file's extract rule declares, plus the member, vocabulary and field-value tables every kind contributes.
func foldForms(ff FileFacts) FileExtract {
	var ex FileExtract
	switch ff.Rule.Mode {
	case game.ModeGUIType:
		ex.Defs, ex.GUITypes, ex.GUIProps = foldKeywordNames(ff)
		// GUI took a branch of its own and never harvested block members, so a whole file kind had no structure model. The nested harvest is generic -- key to child -- so it reads GUI as readily as script.
		_, _, _, ex.StructNested = foldStruct(ff)
		ex.StructNestedKind = "gui"
	case game.ModeTopLevelKey, game.ModeEventID:
		kind := ff.Rule.Kind
		if ff.Rule.Mode == game.ModeEventID {
			kind = "event"
			ex.Defs, ex.Refs = foldEventID(ff)
			kw, _, _ := foldKeywordNames(ff)
			ex.Defs = append(ex.Defs, kw...)
		} else {
			ex.Defs = foldTopLevel(ff)
		}
		attachLeadingDocs(ff.Src, ex.Defs)
		ex.MemberKind = kind
		ex.StructCounts, ex.StructBlocks, ex.Vocab, ex.StructNested = foldStruct(ff)
		ex.StructNestedKind = kind
	default:
		return ex
	}
	nameDefs, nameRefs := foldScriptNames(ff)
	ex.Defs = append(ex.Defs, nameDefs...)
	ex.Refs = append(ex.Refs, nameRefs...)
	paramDefs, paramRefs := foldScriptParams(ff, ex.Defs)
	ex.Defs = append(ex.Defs, paramDefs...)
	ex.Refs = append(ex.Refs, paramRefs...)
	ex.FieldRHS, ex.FieldListRHS = foldFieldValues(ff)
	return ex
}

// citedIDsByKind indexes the corpus's citations once per nested child kind, so resolution does not re-scan them for every file.
func citedIDsByKind(shapes []NestedShape, cites []typedCite, prefixKinds map[string]string) map[string]map[string]bool {
	out := make(map[string]map[string]bool, len(shapes))
	for _, s := range shapes {
		if _, done := out[s.ChildKind]; done {
			continue
		}
		out[s.ChildKind] = citedIDsForKind(cites, prefixKinds, s.ChildKind)
	}
	return out
}

// maxDefDocLines caps one definition's harvested comment: enough for a description without pulling in a whole changelog someone parked above a macro.
const maxDefDocLines = 4

// attachLeadingDocs fills Def.Doc from the comment block written directly above each definition. For a great many objects this is the only description that will ever exist: 4 of CK3's 11,708 scripted macros appear in script_docs, and none of its events, decisions or traits do. Ephemeral and localization defs are skipped -- a saved scope has no declaration site to comment -- and a blank line ends the block, without which a section banner reads as documentation of whatever definition follows it.
func attachLeadingDocs(src string, defs []Def) {
	for i := range defs {
		if !docWorthyKind(defs[i].Kind) {
			continue
		}
		defs[i].Doc = leadingComment(src, defs[i].Start)
	}
}

// docWorthyKind reports a definition whose declaration site is a line an author can comment.
func docWorthyKind(kind string) bool {
	switch jomini.CanonicalKind(kind) {
	case "loc_key", "loc_value", "mod_descriptor", "":
		return false
	}
	return !jomini.IsEphemeral(kind)
}

func leadingComment(src string, off int) string {
	if off > len(src) {
		return ""
	}
	line := strings.LastIndexByte(src[:off], '\n') + 1
	var rev []string
	for line > 0 && len(rev) < maxDefDocLines {
		prev := strings.LastIndexByte(src[:line-1], '\n') + 1
		body, hashed := jomini.PeelComment(src[prev : line-1])
		if !hashed {
			break // a blank line or real script ends the block
		}
		if body == "" || strings.Trim(body, "=-*_ ") == "" {
			break // a separator rule, not prose
		}
		rev = append(rev, body)
		line = prev
	}
	if len(rev) == 0 {
		return ""
	}
	slices.Reverse(rev)
	return jomini.AuthorProse(rev)
}

// foldTopLevel is the root `key = { ... }` definition form.
func foldTopLevel(ff FileFacts) []Def {
	var defs []Def
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Depth != 0 || f.Key == "" || f.Quoted || !f.Block ||
			(f.Op != "=" && f.Op != "?=") {
			continue
		}
		name := game.KeyIdentity(ff.GameID, f.Key)
		// A date is not a definition: the games key their history files by one, so the same date was harvested as a `struggles` object and a `cultures` object.
		if !jomini.IsDefName(name) || name == "namespace" || jomini.IsDateLiteral(name) {
			continue
		}
		defs = append(defs, ff.def(ff.Rule.Kind, name, f.Line, f.Start, f.End))
	}
	return defs
}

// foldEventID harvests namespace plus `ident.digits` event ids.
func foldEventID(ff FileFacts) (defs []Def, refs []Ref) {
	var currentNs string
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Depth != 0 || f.Key == "" || f.Quoted || (f.Op != "=" && f.Op != "?=") {
			continue
		}
		if strings.EqualFold(f.Key, "namespace") {
			if f.VQuoted || f.Value == "" || !jomini.IsDefName(f.Value) {
				continue
			}
			currentNs = f.Value
			defs = append(defs, ff.def("namespace", f.Value, f.VLine, f.VStart, f.VEnd))
			continue
		}
		if !jomini.IsEventID(f.Key) {
			continue
		}
		defs = append(defs, ff.def("event", f.Key, f.Line, f.Start, f.End))
		if currentNs != "" {
			refs = append(refs, ff.ref("namespace", currentNs, f.Line, f.Start, f.End))
		}
	}
	return defs, refs
}

// foldKeywordNames harvests `type X` / `template X` GUI forms and any other `keyword name = { }` pair (inline scripted_*), plus every GUI property name. Only a .gui file has GUI properties, and only that caller reads them.
func foldKeywordNames(ff FileFacts) (defs []Def, guiTypes, guiProps map[string]bool) {
	guiTypes, guiProps = map[string]bool{}, map[string]bool{}
	gui := ff.Rule.Mode == game.ModeGUIType
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Key != "" {
			if k := strings.ToLower(f.Key); gui && !f.Quoted &&
				jomini.IsLowerName(k) && !jomini.IsStop(k) {
				guiProps[k] = true
			}
			continue
		}
		// The keyword is a bare word and the thing it names is the statement after it, in the same block.
		if f.Quoted || f.Value == "" || i+1 >= len(ff.Facts) {
			continue
		}
		kind := jomini.DeclKeywordKind(f.Value)
		if kind == "" || kind == "types" {
			continue
		}
		named := ff.Facts[i+1]
		if named.Parent != f.Parent || named.Key == "" || named.Quoted ||
			!jomini.IsDefName(named.Key) {
			continue
		}
		d := ff.def(kind, named.Key, named.Line, named.Start, named.End)
		d.Local = jomini.DeclIsLocal(f.Value)
		defs = append(defs, d)
		if kind == "gui_type" {
			guiTypes[named.Key] = true
		}
	}
	return defs, guiTypes, guiProps
}

// nestedDepth bounds how far below a definition members are recorded under their parent. Three covers `event > first_valid > triggered_desc > trigger`, the shape that sent readers to the portrait documentation.
const nestedDepth = 3

// foldStruct records a member under the key of the block it sits in. The grandchildren were already being walked -- into the flat vocab bag, which is why `trigger` inside triggered_desc was documented as a portrait's trigger: every name was known, and none of them knew where it belonged.
func foldStruct(ff FileFacts) (
	counts, blocks map[string]int, vocab map[string]bool,
	nested map[string]map[string]int,
) {
	counts, blocks, vocab = map[string]int{}, map[string]int{}, map[string]bool{}
	nested = map[string]map[string]int{}
	// under[i] is the fact naming the block that fact i files its children under, or -1 when its children belong to no block's shape. A top-level definition body names itself and is never read back, because its members are counted per kind instead.
	under := make([]int32, len(ff.Facts))
	for i := range under {
		under[i] = -1
	}
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Depth == 0 {
			if f.Key != "" && f.Block {
				under[i] = int32(i)
			}
			continue
		}
		if f.Parent < 0 || under[f.Parent] < 0 || f.Key == "" || f.Quoted {
			continue
		}
		k := strings.ToLower(f.Key)
		if !jomini.IsLowerName(k) {
			continue
		}
		if f.Depth == 1 {
			if jomini.IsStop(k) {
				continue
			}
			counts[k]++
			vocab[k] = true
			if f.Block {
				blocks[k]++
				under[i] = int32(i)
			}
			continue
		}
		if neverAKey[k] {
			continue
		}
		if !jomini.IsStop(k) {
			vocab[k] = true
		}
		parent := strings.ToLower(ff.Facts[under[f.Parent]].Key)
		m := nested[parent]
		if m == nil {
			m = map[string]int{}
			nested[parent] = m
		}
		m[k]++
		if f.Depth <= nestedDepth {
			under[i] = int32(i)
		}
	}
	return counts, blocks, vocab, nested
}

// foldScriptNames harvests ScriptName defs/refs (flags, variables, saved scopes) and the `prefix:id` citations on a scalar right-hand side.
func foldScriptNames(ff FileFacts) (defs []Def, refs []Ref) {
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Key == "" || f.Quoted {
			continue
		}
		if r, ok := jomini.ScriptName(f.Key); ok {
			name, line, start, end, found := ff.scriptNameSpan(int32(i), r.InnerKey)
			switch {
			case !found || name == "":
			case r.IsDef:
				// Ephemeral and script-name definitions live inside another definition, never at a file's top level.
				d := ff.def(r.Kind, name, line, start, end)
				d.Value = ff.scriptNameValue(int32(i), r)
				d.Nested = true
				defs = append(defs, d)
			default:
				refs = append(refs, ff.ref(r.Kind, name, line, start, end))
			}
		}
		if f.Block || f.VQuoted || f.Value == "" {
			continue
		}
		if p, ok := jomini.ParsePrefixed(f.Value); ok {
			kind := jomini.PrefixKind(p.Prefix)
			if kind == "" {
				kind = p.Prefix
			}
			at := f.VStart + int32(p.NameOff)
			refs = append(refs, ff.ref(kind, p.Name, f.VLine, at, at+int32(len(p.Name))))
		} else if kind, id, ok := game.ParseTyped(ff.GameID, f.Value); ok {
			refs = append(refs, ff.ref(kind, id, f.VLine, f.VStart, f.VEnd))
		}
	}
	return defs, refs
}

// scriptNameSpan is the name token span of a ScriptName assignment. A scalar RHS is the name; a block uses InnerKey (flag/name) when the rule sets one.
func (ff *FileFacts) scriptNameSpan(i int32, innerKey string) (name string, line, start, end int32, ok bool) {
	f := ff.Facts[i]
	if !f.Block && !f.VQuoted && f.Value != "" {
		return f.Value, f.VLine, f.VStart, f.VEnd, true
	}
	if innerKey == "" || !f.Block {
		return "", 0, 0, 0, false
	}
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		c := ff.Facts[j]
		if c.Parent != i || c.Quoted || c.Key != innerKey {
			continue
		}
		if c.Block || c.VQuoted || c.Value == "" {
			return "", 0, 0, 0, false
		}
		return c.Value, c.VLine, c.VStart, c.VEnd, true
	}
	return "", 0, 0, 0, false
}

// scriptNameValue is the hover RHS for a ScriptName def. Scalar saves (save_scope_as) have no value; their hover body is the ancestor scope expr.
func (ff *FileFacts) scriptNameValue(i int32, r jomini.ScriptNameRule) string {
	f := ff.Facts[i]
	if r.Kind == "saved_scope" && r.InnerKey == "" {
		return ""
	}
	if !f.Block {
		return "yes"
	}
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		c := ff.Facts[j]
		if c.Parent == i && !c.Quoted && c.Key == "value" &&
			!c.Block && !c.VQuoted && c.Value != "" {
			return c.Value
		}
	}
	return "yes"
}

// foldScriptParams harvests `$NAME$` macro defs/refs and the call-site `TARGET =` refs that fill them.
func foldScriptParams(ff FileFacts, existing []Def) (defs []Def, refs []Ref) {
	owners := scriptedMacroKeys(existing)
	if len(owners) == 0 {
		return nil, nil
	}
	ownerParams := map[string]map[string]bool{}
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Depth != 0 || f.Key == "" || f.Quoted || !f.Block || !owners[f.Key] {
			continue
		}
		pdefs, prefs, names := ff.dollarParams(int32(i), f.Key)
		defs = append(defs, pdefs...)
		refs = append(refs, prefs...)
		ownerParams[f.Key] = names
	}
	// call[i] is the fact naming the macro whose parameters an argument key inside fact i would fill, or -1 outside any call.
	call := make([]int32, len(ff.Facts))
	for i := range ff.Facts {
		f := ff.Facts[i]
		ck := int32(-1)
		if f.Parent >= 0 {
			ck = call[f.Parent]
		}
		if f.Key != "" && !f.Quoted {
			if ck >= 0 {
				if owner := ff.Facts[ck].Key; ownerParams[owner][f.Key] {
					r := ff.ref("script_param", f.Key, f.Line, f.Start, f.End)
					r.OwnerKey = owner
					refs = append(refs, r)
				}
			}
			if owners[f.Key] {
				ck = int32(i)
			}
		}
		call[i] = ck
	}
	return defs, refs
}

// scriptedMacroKeys is IsCitableDef plus two exclusions of its own, and they are deliberate: a `$PARAM$` site resolves against names a macro body could call, and neither an event namespace nor a GUI template is callable from script.
func scriptedMacroKeys(defs []Def) map[string]bool {
	out := map[string]bool{}
	for _, d := range defs {
		k := jomini.CanonicalKind(d.Kind)
		if !IsCitableDef(d) || k == jomini.KindNamespace || k == jomini.KindGUIType {
			continue
		}
		out[d.Key] = true
	}
	return out
}

// dollarParams records every `$NAME$` inside one macro body: the first sighting declares the parameter, the rest use it.
func (ff *FileFacts) dollarParams(owner int32, ownerKey string) (defs []Def, refs []Ref, names map[string]bool) {
	names = map[string]bool{}
	seen := map[string]bool{}
	record := func(name string, line, start, end int32) {
		names[name] = true
		if !seen[name] {
			seen[name] = true
			d := ff.def("script_param", name, line, start, end)
			d.OwnerKey = ownerKey
			defs = append(defs, d)
			return
		}
		r := ff.ref("script_param", name, line, start, end)
		r.OwnerKey = ownerKey
		refs = append(refs, r)
	}
	for j := owner + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= owner; j++ {
		c := ff.Facts[j]
		if c.Key == "" || c.Quoted {
			continue
		}
		for _, sp := range dollarParamsIn(c.Key, c.Start) {
			record(sp.name, c.Line, sp.start, sp.end)
		}
		if c.Block || c.VQuoted {
			continue
		}
		for _, sp := range dollarParamsIn(c.Value, c.VStart) {
			record(sp.name, c.VLine, sp.start, sp.end)
		}
	}
	return defs, refs, names
}

type dollarSpan struct {
	name       string
	start, end int32
}

func dollarParamsIn(text string, base int32) []dollarSpan {
	var out []dollarSpan
	for i := 0; i < len(text); {
		if text[i] != '$' {
			i++
			continue
		}
		j := i + 1
		nameStart := j
		for j < len(text) && jomini.IsScriptParamByte(text[j], j == nameStart) {
			j++
		}
		if j == nameStart || j >= len(text) || text[j] != '$' {
			i++
			continue
		}
		out = append(out, dollarSpan{
			name:  text[nameStart:j],
			start: base + int32(i),
			end:   base + int32(j) + 1,
		})
		i = j + 1
	}
	return out
}

// dropEphemeralDefs drops flags/variables/scopes from the install cache. script_param stays so $NAME$ in vanilla scripted_* files can resolve.
func dropEphemeralDefs(defs []Def) []Def {
	out := make([]Def, 0, len(defs))
	for _, d := range defs {
		k := jomini.CanonicalKind(d.Kind)
		if jomini.IsEphemeral(k) && k != "script_param" {
			continue
		}
		out = append(out, d)
	}
	return out
}

type defAtLine struct {
	line int
	key  string
}

// defContainers lists the objects a line can belong to, for containerAt. IsObjectKind rather than a hand-written exclusion list: without it a `saved_scope` def, created at the line of a save_scope_as RHS inside an event body, outranked the event as a container -- containerAt takes the LAST container at or before a line -- which pointed 5,604 of CK3's 17,073 non-call edges at a source node that does not exist.
func defContainers(defs []Def) []defAtLine {
	var out []defAtLine
	for _, d := range defs {
		if jomini.IsObjectKind(d.Kind) {
			out = append(out, defAtLine{d.Line, d.Key})
		}
	}
	slices.SortFunc(out, func(a, b defAtLine) int { return cmp.Compare(a.line, b.line) })
	return out
}

func containerAt(containers []defAtLine, line int) string {
	lo, hi, best := 0, len(containers)-1, ""
	for lo <= hi {
		mid := (lo + hi) / 2
		if containers[mid].line <= line {
			best = containers[mid].key
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return best
}

// objectRefKey is the stored key for a typed field RHS, or false to skip.
func objectRefKey(gameID, kind, text string) (string, bool) {
	// A data row is named by its number, so the rule that bare numbers are quantities has to stop short of `holder = 1234`.
	if isDigitKey(text) && game.IsDataKind(gameID, kind) {
		return text, true
	}
	if jomini.SkipFieldRHS(text) {
		return "", false
	}
	// Grammar words are never object names: `c:FRA ?= this` recorded `this` as a reference 253 times on one Vic3 mod.
	if jomini.IsStop(text) {
		return "", false
	}
	if _, ok := jomini.ParsePrefixed(text); ok {
		return "", false
	}
	if _, id, ok := game.ParseTyped(gameID, text); ok {
		return id, true
	}
	return text, true
}

// fieldSite is one `key = value`, or one bare member of `key = { … }`. Whether it names a localization key or an object of some kind waits on the LocFields and FieldValueKinds votes.
type fieldSite struct {
	key, value       string
	line, start, end int32
	list, quoted     bool
}

// fireTarget is one name a fire site holds directly. vote marks the statement's own right-hand side, which is the evidence FireKeys is counted from.
type fireTarget struct {
	name             string
	line, start, end int32
	vote             bool
}

// fireSite is one statement naming something that could be an event or an on_action. The slot, the option name and the rescoping come from the enclosing blocks and need no vote. Each target is held once, by the innermost statement that names it: up is the statement it also reaches, and ownN splits the names this statement holds itself from the ones handed outward to it -- which is how a fire key nested inside another one carries its names out without every ancestor keeping a copy.
type fireSite struct {
	key, kind, nameKey         string
	line, at, up, ownN         int32
	targets                    []fireTarget
	rescoped, declared, quoted bool
}

// nestSite is one keyed statement with a link to its keyed parent: a nested definition when a shape claims it, and otherwise a link in the citation chain that decides which of its ancestors are.
type nestSite struct {
	key                        string
	parent                     int32
	line, start, end           int32
	block, object, quoted, top bool
}

// wrapSite is a top-level block whose every member is a typed cite; it is a wrapper if the corpus defines all of them.
type wrapSite struct {
	key string
	ids []string
}

// fileSites is one file's deferred half: the statements whose meaning a whole-corpus vote decides, and the per-file evidence those votes are counted from. It holds no facts, and every string in it is an interned copy rather than a slice of the file, so the parse tree, the fact table and the file's text are all released as soon as the file has been read.
type fileSites struct {
	fileID
	defs              []Def
	fields            []fieldSite
	fires             []fireSite
	nests             []nestSite
	wraps             []wrapSite
	cites, fieldCites []typedCite
	// nested is every nested block name the file holds, under the depth-1 block above it: the per-file half of the NestedShapes vote.
	nested map[string]nestedObs
}

// frame is what the enclosing blocks decide about a statement, carried forward as the facts are read so nothing walks back up its parents. optionAt is the option block itself rather than its name key, which is a walk of that block and is only worth making for a statement that turns out to name an event.
type frame struct {
	kind            string
	optionAt, depth int32
	rescoped        bool
}

// foldSites reads every use site a corpus vote decides: field values and list members, call sites, fire targets, nested blocks and wrapper candidates. Nothing below this reads a Fact again.
func foldSites(ff FileFacts, schema *declared.Schema) *fileSites {
	fs := &fileSites{fileID: ff.fileID, nested: map[string]nestedObs{}}
	// fr[i] is what the children of fact i inherit; nestAt[i] is fact i's place in nests, -1 for a bare statement, which breaks the citation chain exactly where the recursion it replaces did; wrapAt[i] is the fact naming the depth-1 block fact i sits under.
	fr := make([]frame, len(ff.Facts))
	nestAt := make([]int32, len(ff.Facts))
	wrapAt := make([]int32, len(ff.Facts))
	// recvAt[i] is the nearest enclosing statement that names something, past the bare blocks that name nothing: where fact i's fire targets are handed outward to.
	recvAt := make([]int32, len(ff.Facts))
	bodies := jomini.DeclaresNamedBodies(ff.Rule.Kind)
	for i := range ff.Facts {
		f := ff.Facts[i]
		own, at, nestParent := frame{optionAt: -1}, int32(-1), int32(-1)
		recvAt[i] = -1
		if f.Parent >= 0 {
			own, at, nestParent = fr[f.Parent], wrapAt[f.Parent], nestAt[f.Parent]
			if ff.Facts[f.Parent].Key != "" {
				recvAt[i] = f.Parent
			} else {
				recvAt[i] = recvAt[f.Parent]
			}
		}
		wrapAt[i], nestAt[i], fr[i] = at, -1, own
		if f.Key != "" {
			// The outermost frame is the definition's own key, which is where its declared scope applies, so rescoping is only counted below it.
			if own.depth >= 1 && !jomini.IsScopeNeutral(f.Key) {
				fr[i].rescoped = true
			}
			fr[i].depth = own.depth + 1
			if strings.EqualFold(f.Key, "option") {
				fr[i].kind, fr[i].optionAt = "option", int32(i)
			} else if slot := jomini.ScriptSlot(f.Key); slot != "" {
				fr[i].kind, fr[i].optionAt = slot, -1
			}
		}
		if f.Key == "" {
			continue
		}
		nestAt[i] = int32(len(fs.nests))
		fs.nests = append(fs.nests, nestSite{
			key: f.Key, parent: nestParent, line: f.Line, start: f.Start, end: f.End,
			block: f.Block, object: f.Block && ff.holdsNames(int32(i)),
			quoted: f.Quoted, top: f.Depth == 0,
		})
		if f.Block && f.Depth == 0 {
			wrapAt[i] = -1
		} else if f.Block {
			if at < 0 {
				wrapAt[i] = int32(i)
			}
			group := ""
			if at >= 0 {
				group = game.KeyIdentity(ff.GameID, ff.Facts[at].Key)
			}
			fs.observe(game.KeyIdentity(ff.GameID, f.Key), group)
		}
		if !jomini.IsStop(f.Key) && loc.Classify(f.Key) == loc.PropNone &&
			!strings.EqualFold(f.Key, "id") {
			if targets := ff.fireTargets(int32(i), true, nil); len(targets) > 0 {
				s := ff.fireSiteAt(int32(i), fr, recvAt, schema)
				s.targets, s.ownN = targets, int32(len(targets))
				fs.fires = append(fs.fires, s)
			}
		}
		if f.Quoted {
			continue
		}
		if f.Depth == 0 && f.Block && !bodies {
			if w, ok := ff.wrapCandidate(int32(i)); ok {
				fs.wraps = append(fs.wraps, w)
			}
		}
		fs.foldFieldSites(ff, int32(i))
	}
	fs.addFireRelays(ff, fr, recvAt, schema)
	fs.cites, fs.fieldCites = foldCites(ff)
	fs.clip()
	return fs
}

// fireSiteAt describes the statement at i as a fire site, without its targets.
func (ff *FileFacts) fireSiteAt(
	i int32, fr []frame, recvAt []int32, schema *declared.Schema,
) fireSite {
	f := ff.Facts[i]
	own := frame{optionAt: -1}
	if f.Parent >= 0 {
		own = fr[f.Parent]
	}
	kind, nameKey := own.kind, ""
	if kind == "" {
		if kind = jomini.ScriptSlot(f.Key); kind == "" {
			kind = "effect"
		}
	} else if own.optionAt >= 0 {
		nameKey = ff.optionNameKey(own.optionAt)
	}
	// Declared beats derived, and that cuts both ways: the game DECLARES trigger_event, so a fire ref at one is evidence even though FireKeys is how PMT learned it.
	return fireSite{
		key: f.Key, kind: kind, nameKey: nameKey, line: f.Line,
		at: i, up: recvAt[i], rescoped: own.rescoped,
		declared: schema.HasName(f.Key), quoted: f.Quoted,
	}
}

// addFireRelays gives every statement that will be handed another's targets a site of its own, so the outward walk in resolveSites has somewhere to put them. A statement passes something on when it holds names of its own and is not id-like -- an id-like key is already read as part of the statement around it -- or when something was handed to it. Sites stay ordered by statement, which is what lets one walk carry a whole chain outward.
func (fs *fileSites) addFireRelays(
	ff FileFacts, fr []frame, recvAt []int32, schema *declared.Schema,
) {
	if len(fs.fires) == 0 {
		return
	}
	held, passes := make([]bool, len(ff.Facts)), make([]bool, len(ff.Facts))
	for _, s := range fs.fires {
		held[s.at] = true
		passes[s.at] = !inlineFireKey(s.key)
	}
	added := false
	for i := len(ff.Facts) - 1; i >= 0; i-- {
		u := recvAt[i]
		if !passes[i] || u < 0 {
			continue
		}
		passes[u] = true
		if !held[u] {
			held[u] = true
			added = true
			fs.fires = append(fs.fires, ff.fireSiteAt(u, fr, recvAt, schema))
		}
	}
	if added {
		slices.SortFunc(fs.fires, func(a, b fireSite) int { return cmp.Compare(a.at, b.at) })
	}
}

// clip trims the slack append leaves on the three site slices big enough for it to matter: across an install that slack is hundreds of megabytes, and the sites are the pass's heap peak.
func (fs *fileSites) clip() {
	fs.fields = slices.Clone(fs.fields)
	fs.fires = slices.Clone(fs.fires)
	fs.nests = slices.Clone(fs.nests)
}

// foldFieldSites records the values fact i offers a field vote: its scalar right-hand side, or the bare members of its block. A member that is a bare number or a date is dropped here rather than carried: it is never a localization key, and keeping every one of them cost 7 to 13 million candidates per install to produce none.
func (fs *fileSites) foldFieldSites(ff FileFacts, i int32) {
	f := ff.Facts[i]
	if !f.Block {
		// A value that is neither a plausible localization key nor a plausible object name resolves to nothing whichever way the votes go, and `x = yes` alone is a third of the install.
		if f.Value != "" && (loc.LooksLikeKey(f.Value) ||
			!jomini.SkipFieldRHS(f.Value) && !jomini.IsStop(f.Value)) {
			fs.fields = append(fs.fields, fieldSite{
				key: f.Key, value: f.Value, line: f.VLine,
				start: f.VStart, end: f.VEnd, quoted: f.VQuoted,
			})
		}
		return
	}
	// A list whose members are localization keys: a culture's `male_names`, its `cadet_dynasty_names`. The members are values, not assignments, so nothing else here sees them.
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		c := ff.Facts[j]
		if c.Parent != i || c.Key != "" || c.Block || c.Value == "" ||
			!loc.LooksLikeKey(c.Value) || jomini.IsLiteralValue(c.Value) {
			continue
		}
		fs.fields = append(fs.fields, fieldSite{
			key: f.Key, value: c.Value, line: c.VLine,
			start: c.VStart, end: c.VEnd, list: true,
		})
	}
}

// holdsNames reports a block that holds named members, or none at all.
func (ff *FileFacts) holdsNames(i int32) bool {
	empty := true
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		empty = false
		if c := ff.Facts[j]; c.Parent == i && c.Key != "" {
			return true
		}
	}
	return empty
}

// wrapCandidate is a top-level block whose every member is a typed cite (Vic3 COUNTRIES = { c:USA ?= { } }). A `keyword name = { }` pair is never a wrapper however its body is shaped: CK3 declares inline macros that way, and matching them on shape alone deleted 28 vanilla macros from the model. The keyword is a childless bare word, so the fact before this one is the statement before it.
func (ff *FileFacts) wrapCandidate(i int32) (wrapSite, bool) {
	if i > 0 {
		if p := ff.Facts[i-1]; p.Parent == ff.Facts[i].Parent && p.Key == "" &&
			!p.Quoted && jomini.DeclKeywordKind(p.Value) != "" {
			return wrapSite{}, false
		}
	}
	w, any := wrapSite{key: ff.Facts[i].Key}, false
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		c := ff.Facts[j]
		if c.Parent != i || c.Key == "" || c.Quoted {
			continue
		}
		any = true
		kind, id, ok := game.ParseTyped(ff.GameID, c.Key)
		if !ok {
			if sp, sok := jomini.TypedSpanAt(c.Key, 0); sok {
				kind, id, ok = game.ParseTyped(ff.GameID, sp.Prefix+":"+sp.ID)
			}
		}
		if !ok || kind == "" {
			return wrapSite{}, false
		}
		w.ids = append(w.ids, id)
	}
	return w, any
}

func (ff *FileFacts) optionNameKey(i int32) string {
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		c := ff.Facts[j]
		if c.Parent == i && c.Key != "" && strings.EqualFold(c.Key, "name") && !c.Block {
			return c.Value
		}
	}
	return ""
}

// fireTargets collects every scalar this statement names itself: the bare RHS, list values, and `id =` / numbered inners, following bare and id-like blocks because those name nothing of their own. Any other keyed inner is a statement in its own right and keeps its targets; they reach this one through its up link. top marks the statement's own right-hand side, the half of this that FireKeys is voted from.
func (ff *FileFacts) fireTargets(i int32, top bool, out []fireTarget) []fireTarget {
	f := ff.Facts[i]
	if !f.Block {
		if !f.VQuoted && fireTargetOK(f.Value) {
			out = append(out, fireTarget{name: f.Value, line: f.VLine,
				start: f.VStart, end: f.VEnd, vote: top})
		}
		return out
	}
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		c := ff.Facts[j]
		if c.Parent != i || (c.Key != "" && !inlineFireKey(c.Key)) {
			continue
		}
		if !c.Block && !c.VQuoted && fireTargetOK(c.Value) {
			out = append(out, fireTarget{name: c.Value, line: c.VLine,
				start: c.VStart, end: c.VEnd, vote: top})
		} else if c.Block {
			out = ff.fireTargets(j, false, out)
		}
	}
	return out
}

// inlineFireKey reports a key that names no fire site of its own, so its targets belong to the statement around it: `id = ` and the numbered weights of a random_events block.
func inlineFireKey(key string) bool {
	k := strings.ToLower(key)
	return k == "id" || isDigitKey(k)
}

// foldFieldValues collects the values a field takes, so deriveFieldValueKinds and deriveLocFields can type it: scalars from `field = value`, lists from the bare members of `field = { a b c }`. A list member is a ValueStmt, not an assignment, and localization keys hide there in quantity -- CK3 writes a culture's given names and its cadet dynasty names as lists, and nothing cited them: 6,978 of A Game of Thrones' 20,196 orphaned English keys, 35% of the whole residue. Quoted members count, since vanilla quotes one of those two lists and not the other.
func foldFieldValues(ff FileFacts) (scalars, lists map[string]map[string]bool) {
	scalars, lists = map[string]map[string]bool{}, map[string]map[string]bool{}
	add := func(dst map[string]map[string]bool, field, val string) {
		m := dst[field]
		if m == nil {
			m = map[string]bool{}
			dst[field] = m
		}
		m[val] = true
	}
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Key == "" || f.Quoted || loc.Classify(f.Key) != loc.PropNone {
			continue
		}
		if !f.Block {
			if !f.VQuoted && f.Value != "" && loc.LooksLikeKey(f.Value) {
				add(scalars, f.Key, f.Value)
			}
			continue
		}
		for j := int32(i) + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= int32(i); j++ {
			c := ff.Facts[j]
			if c.Parent != int32(i) || c.Key != "" || c.Block ||
				c.Value == "" || !loc.LooksLikeKey(c.Value) {
				continue
			}
			add(lists, f.Key, c.Value)
		}
	}
	if len(scalars) == 0 {
		scalars = nil
	}
	if len(lists) == 0 {
		lists = nil
	}
	return scalars, lists
}

// foldCites reads both halves of the nested join in one pass: `typed` is every `prefix:id` citation, minus the language prefixes (scope:, var:, …) that name locals rather than database rows; `field` is every `field = name` pair whose RHS could be an undefined row.
func foldCites(ff FileFacts) (typed, field []typedCite) {
	add := func(text string) {
		for _, sp := range jomini.TypedSpans(text) {
			if jomini.PrefixKind(sp.Prefix) != "" {
				continue
			}
			typed = append(typed, typedCite{prefix: sp.Prefix, id: sp.ID})
		}
	}
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Key != "" && !f.Quoted {
			add(f.Key)
		}
		if f.Block || f.VQuoted || f.Value == "" {
			continue
		}
		add(f.Value)
		if f.Key == "" || f.Quoted || jomini.SkipObjectRHS(f.Value) {
			continue
		}
		if _, _, ok := game.ParseTyped("", f.Value); ok {
			continue
		}
		if _, ok := jomini.ParsePrefixed(f.Value); ok {
			continue
		}
		if !jomini.IsDefName(f.Value) || jomini.IsStop(f.Value) {
			continue
		}
		field = append(field, typedCite{prefix: f.Key, id: f.Value})
	}
	return typed, field
}

// isMacroArgKey reports the all-caps convention Paradox uses for the arguments of a scripted macro: `some_effect = { ROLE = chemist }`. The value is a fragment the macro pastes into a name, not a reference to an object of the field's type.
func isMacroArgKey(key string) bool {
	upper := false
	for i := range len(key) {
		c := key[i]
		switch {
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= 'a' && c <= 'z':
			return false
		}
	}
	return upper
}
