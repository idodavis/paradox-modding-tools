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

// FileFacts is the one product of parsing a file. Src stays because a leading comment is not a statement and so cannot be a fold over Facts.
type FileFacts struct {
	GameID, Path, Rel, Origin, Src string
	// Rule is game.MatchExtract's answer for Rel: the kind members are filed under, and the form this folder's definitions take.
	Rule  game.ExtractRule
	Facts []Fact
	Loc   LocDelta // loc files only
}

// FactsOf turns one parsed file into its facts, in one jomini.Walk.
func FactsOf(gameID, path, rel, origin string, res jomini.Result) FileFacts {
	ff := FileFacts{
		GameID: gameID, Path: filepath.Clean(path), Rel: rel, Origin: origin,
		Src: res.Src, Rule: game.MatchExtract(gameID, rel),
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
func (ff *FileFacts) def(kind, key string, line, start, end int32) Def {
	return Def{
		Kind: jomini.CanonicalKind(kind), Key: key, Path: ff.Path,
		Line: int(line), Start: int(start), End: int(end), Origin: ff.Origin,
	}
}

func (ff *FileFacts) ref(kind, key string, line, start, end int32) Ref {
	return Ref{
		Key: key, Kind: kind, Path: ff.Path,
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
	ex.Loc = ff.Loc
	if derived.CitedByKind == nil {
		idx := newDefIndex(ex.Defs)
		derived.mergeLocal(deriveFacts(ff, idx, schema))
		derived.mergeLocal(&Derived{FieldValueKinds: deriveFieldValueKinds(ex.FieldRHS, idx)})
	}
	ex.Defs = applyDerivedDefs(ff, ex.Defs, derived)
	bodyRefs, edges, cands := foldRefsAndEdges(ff, ex.Defs, derived, schema)
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

// applyDerivedDefs harvests the nested databases a file contains. derived.CitedByKind is the whole corpus's citations, keyed by child kind. A file-local set is not enough: EU5 declares its sub-continents in map_data/definitions.txt but cites them from common/advances/, so the definitions file saw no citations and produced zero defs.
func applyDerivedDefs(ff FileFacts, defs []Def, derived *Derived) []Def {
	if derived == nil {
		return defs
	}
	defs = dropWrapperDefs(defs, derived.Wrappers)
	ck := jomini.CanonicalKind(ff.Rule.Kind)
	var local []typedCite
	if derived.CitedByKind == nil {
		local, _ = foldCites(ff)
	}
	for _, shape := range derived.NestedShapes {
		if jomini.CanonicalKind(shape.ParentKind) != ck && shape.ParentKind != ff.Rule.Kind {
			continue
		}
		cited := derived.CitedByKind[shape.ChildKind]
		if derived.CitedByKind == nil {
			cited = citedIDsForKind(local, derived.PrefixKinds, shape.ChildKind)
		}
		defs = append(defs, foldNestedShape(ff, shape, cited)...)
	}
	return defs
}

// citedIDsByKind indexes the corpus's citations once per nested child kind, so the second pass does not re-scan them for every file.
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

type edgeFrame struct {
	key, nameKey string
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

// foldRefsAndEdges reads the file's use sites: localization properties, typed field values, fire sites and their graph edges, and the call candidates everything else leaves behind.
func foldRefsAndEdges(
	ff FileFacts, defs []Def, derived *Derived, schema *declared.Schema,
) ([]Ref, []Edge, []CallCandidate) {
	var refs []Ref
	var edges []Edge
	var cands []CallCandidate
	containers := defContainers(defs)
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Key == "" || f.Quoted {
			continue
		}
		from := containerAt(containers, int(f.Line))
		prop := loc.Classify(f.Key)
		// Broad, never strict: that a field usually holds localization keys is not evidence that every value of it must resolve.
		if prop == loc.PropNone && derived.locField(f.Key) {
			prop = loc.PropBroad
		}
		if prop != loc.PropNone && !f.Block && f.Value != "" && loc.LooksLikeKey(f.Value) {
			kind := "loc-broad"
			if prop == loc.PropStrict {
				kind = "loc"
			}
			refs = append(refs, ff.ref(kind, f.Value, f.VLine, f.VStart, f.VEnd))
		}
		// A list whose members are localization keys: a culture's `male_names`, its `cadet_dynasty_names`. The members are values, not assignments, so nothing else here sees them.
		if f.Block && derived.locField(f.Key) {
			for j := int32(i) + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= int32(i); j++ {
				c := ff.Facts[j]
				if c.Parent != int32(i) || c.Key != "" || c.Block ||
					c.Value == "" || !loc.LooksLikeKey(c.Value) {
					continue
				}
				refs = append(refs, ff.ref("loc-broad", c.Value, c.VLine, c.VStart, c.VEnd))
			}
		}
		if !f.Block && !f.VQuoted && f.Value != "" && prop == loc.PropNone {
			// A typed cite is already recorded in foldScriptNames.
			if _, _, typed := game.ParseTyped(ff.GameID, f.Value); !typed {
				if rk := derived.fieldKind(f.Key); rk != "" && !isMacroArgKey(f.Key) {
					if refKey, ok := objectRefKey(ff.GameID, rk, f.Value); ok {
						r := ff.ref(rk, refKey, f.VLine, f.VStart, f.VEnd)
						r.Derived = true
						refs = append(refs, r)
					}
				}
			}
		}
		fk := derived.fireKind(f.Key)
		if fk == "" {
			if from != "" && from != f.Key {
				cands = append(cands, CallCandidate{
					From: from, To: f.Key, Path: ff.Path, Line: int(f.Line),
					Start: int(f.Start), End: int(f.End),
				})
			}
			continue
		}
		frames := ff.frames(int32(i))
		kind, nameKey := fireSiteKind(frames, f.Key)
		rescoped := fireSiteRescopes(frames)
		// A numeric key is a weight, and the only place one names an event directly is a `random_events` block, where targets are always `namespace.number`. Requiring that shape there -- and only there -- stops `1 = empty` in a Vic3 genes file reading as a fire site.
		numericEvent := isDigitKey(f.Key) && fk == "event"
		ff.fireTargets(int32(i), derived, func(to string, line, start, end int32) {
			if numericEvent && !jomini.IsEventID(to) {
				return
			}
			r := ff.ref(fk, to, line, start, end)
			// Declared beats derived, and that cuts both ways: the game DECLARES trigger_event, so a fire ref at one is evidence even though FireKeys is how PMT learned it.
			r.Derived = !schema.HasName(f.Key)
			refs = append(refs, r)
			edges = append(edges, Edge{
				From: from, To: to, Via: f.Key, Path: ff.Path,
				Line: int(line), Kind: kind, NameKey: nameKey, Rescoped: rescoped,
			})
		})
	}
	return refs, edges, cands
}

// frames is the chain of enclosing block keys, outermost first -- what decides a fire site's slot and whether anything rescoped it on the way down. Bare blocks are not frames: they name nothing.
func (ff *FileFacts) frames(i int32) []edgeFrame {
	var out []edgeFrame
	for p := ff.Facts[i].Parent; p >= 0; p = ff.Facts[p].Parent {
		f := ff.Facts[p]
		if f.Key == "" {
			continue
		}
		fr := edgeFrame{key: f.Key}
		if strings.EqualFold(f.Key, "option") {
			fr.nameKey = ff.optionNameKey(p)
		}
		out = append(out, fr)
	}
	slices.Reverse(out)
	return out
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

// fireTargets visits every scalar under a fire assignment that could name an event or on_action: the bare RHS, list values, and `id = ` / numbered inners. derived supplies the fire kinds harvested from the install.
func (ff *FileFacts) fireTargets(i int32, derived *Derived, fn func(name string, line, start, end int32)) {
	f := ff.Facts[i]
	if !f.Block {
		if !f.VQuoted && fireTargetOK(f.Value) {
			fn(f.Value, f.VLine, f.VStart, f.VEnd)
		}
		return
	}
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		c := ff.Facts[j]
		if c.Parent != i {
			continue
		}
		if c.Key == "" {
			if !c.Block && !c.VQuoted && fireTargetOK(c.Value) {
				fn(c.Value, c.VLine, c.VStart, c.VEnd)
			} else if c.Block {
				ff.fireTargets(j, derived, fn)
			}
			continue
		}
		key := strings.ToLower(c.Key)
		own := derived.fireKind(c.Key)
		if !c.Block && !c.VQuoted && fireTargetOK(c.Value) &&
			(key == "id" || isDigitKey(key) || own != "") {
			fn(c.Value, c.VLine, c.VStart, c.VEnd)
			continue
		}
		if own != "" || key == "id" || isDigitKey(key) {
			ff.fireTargets(j, derived, fn)
		}
	}
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

// foldNestedShape harvests child defs from a parent file the shape describes.
func foldNestedShape(ff FileFacts, shape NestedShape, cited map[string]bool) []Def {
	var defs []Def
	add := func(i int32) {
		f := ff.Facts[i]
		name := game.KeyIdentity(ff.GameID, f.Key)
		if !jomini.IsDefName(name) || jomini.IsStop(name) || !ff.looksLikeObject(i) {
			return
		}
		d := ff.def(shape.ChildKind, name, f.Line, f.Start, f.End)
		d.Nested = true
		defs = append(defs, d)
	}
	if shape.GroupKey != "" {
		for i := range ff.Facts {
			f := ff.Facts[i]
			if f.Key == "" || !f.Block || !strings.EqualFold(f.Key, shape.GroupKey) {
				continue
			}
			for j := int32(i) + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= int32(i); j++ {
				if c := ff.Facts[j]; c.Parent == int32(i) && c.Key != "" && !c.Quoted {
					add(j)
				}
			}
		}
		return defs
	}
	// hit[i] is a cited name at fact i or below it, through assignment keys only; desc[i] is the same for its children, which is what makes a wrapper block count as the row's site.
	hit, desc := make([]bool, len(ff.Facts)), make([]bool, len(ff.Facts))
	if shape.KeyPrefix == "" {
		for i := len(ff.Facts) - 1; i >= 0; i-- {
			f := ff.Facts[i]
			if f.Key == "" {
				continue
			}
			hit[i] = desc[i] || cited[game.KeyIdentity(ff.GameID, f.Key)]
			if hit[i] && f.Parent >= 0 {
				desc[f.Parent] = true
			}
		}
	}
	for i := range ff.Facts {
		f := ff.Facts[i]
		if f.Key == "" || !f.Block || f.Depth == 0 {
			continue
		}
		if shape.KeyPrefix != "" {
			if keyPrefixOK(game.KeyIdentity(ff.GameID, f.Key), shape.KeyPrefix) {
				add(int32(i))
			}
			continue
		}
		if hit[i] {
			add(int32(i))
		}
	}
	return defs
}

// looksLikeObject reports a block that holds named members, or none at all.
func (ff *FileFacts) looksLikeObject(i int32) bool {
	if !ff.Facts[i].Block {
		return false
	}
	empty := true
	for j := i + 1; j < int32(len(ff.Facts)) && ff.Facts[j].Parent >= i; j++ {
		empty = false
		if c := ff.Facts[j]; c.Parent == i && c.Key != "" {
			return true
		}
	}
	return empty
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
