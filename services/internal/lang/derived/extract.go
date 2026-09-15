// extract.go is the per-file catalog extractor (scan, BuildIndex, session reindex).

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

// FileExtract is one parsed file's catalog harvest.
type FileExtract struct {
	Defs  []Def
	Refs  []Ref
	Edges []Edge
	Cands []CallCandidate
	Loc   LocDelta
	// MemberKind is the file's kind, for both member-name tables. StructBlocks and Vocab stay empty unless the caller asked for bodies, so the counter they feed simply gets nothing.
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

// parsedScript is one install/mod file kept in RAM for the derive + nested pass.
type parsedScript struct {
	f   fileRef
	res jomini.Result
}

// ExtractFile extracts one decoded (CR-free) file, choosing the loc or script path from the game's extract rule; loc files skip the script parse.
func ExtractFile(gameID, absPath, rel, origin, text string, harvestBodies bool) FileExtract {
	text = jomini.Normalize(text)
	if game.MatchExtract(gameID, rel).Mode == game.ModeLocKey {
		var ex FileExtract
		ex.Defs, ex.Loc, ex.Refs = extractLoc(filepath.Clean(absPath), text, origin)
		return ex
	}
	return ExtractParsed(gameID, absPath, rel, origin, jomini.Parse(text), harvestBodies, nil)
}

// ExtractParsed extracts defs, refs, edges, and call candidates from a parsed script or GUI file. overlay supplies vanilla derived for a live reindex; a nil overlay derives from this file alone. Callers turn Cands into edges via ApplyCallEdges.
func ExtractParsed(
	gameID, absPath, rel, origin string,
	res jomini.Result,
	harvestBodies bool,
	overlay *VanillaCache,
) FileExtract {
	ex := extractForms(gameID, absPath, rel, origin, res, harvestBodies)
	derived := derivedFromCache(overlay)
	var schema *declared.Schema
	if overlay != nil {
		schema = overlay.Schema
	}
	// One index of this file's defs for both derivations below.
	idx := newDefIndex(ex.Defs)
	derived.mergeLocal(deriveFile(gameID, rel, res, ex.Defs, idx, schema))
	derived.mergeLocal(&Derived{FieldValueKinds: deriveFieldValueKinds(ex.FieldRHS, idx)})
	ex.Defs = applyDerivedDefs(gameID, absPath, origin, rel, res, ex.Defs, derived, nil)
	bodyRefs, edges, cands := extractRefsAndEdges(
		gameID, res.Root, res.Lines(), absPath, ex.Defs, derived, schema,
	)
	ex.Refs = append(ex.Refs, bodyRefs...)
	ex.Edges, ex.Cands = edges, cands
	return ex
}

func extractForms(
	gameID, absPath, rel, origin string,
	res jomini.Result,
	harvestBodies bool,
) FileExtract {
	absPath = filepath.Clean(absPath)
	rule := game.MatchExtract(gameID, rel)
	var ex FileExtract
	switch rule.Mode {
	case game.ModeGUIType:
		ex.Defs, ex.GUITypes, ex.GUIProps = extractKeywordName(
			res.Root, res.Lines(), absPath, origin,
		)
		// GUI took a branch of its own and never harvested block members, so a whole file kind had no structure model. The nested harvest is generic -- key to child -- so it reads GUI as readily as script.
		_, _, _, ex.StructNested = harvestStructVocab(res.Root)
		ex.StructNestedKind = "gui"
	case game.ModeTopLevelKey, game.ModeEventID:
		kind := rule.Kind
		if rule.Mode == game.ModeEventID {
			kind = "event"
			var nsRefs []Ref
			ex.Defs, nsRefs = extractEventID(res.Root, res.Lines(), absPath, origin)
			ex.Refs = nsRefs
			kw, _, _ := extractKeywordName(res.Root, res.Lines(), absPath, origin)
			ex.Defs = append(ex.Defs, kw...)
		} else {
			ex.Defs = extractTopLevel(res.Root, res.Lines(), gameID, rule.Kind, absPath, origin)
		}
		attachLeadingDocs(res.Src, res.Lines(), ex.Defs)
		ex.MemberKind = kind
		counts, blocks, vocab, nested := harvestStructVocab(res.Root)
		ex.StructCounts = counts
		ex.StructNested = nested
		ex.StructNestedKind = kind
		if harvestBodies {
			ex.StructBlocks, ex.Vocab = blocks, vocab
		}
	default:
		return ex
	}
	nameDefs, nameRefs := extractScriptNames(gameID, res.Root, res.Lines(), absPath, origin)
	ex.Defs = append(ex.Defs, nameDefs...)
	ex.Refs = append(ex.Refs, nameRefs...)
	paramDefs, paramRefs := extractScriptParams(res.Root, res.Lines(), absPath, origin, ex.Defs)
	ex.Defs = append(ex.Defs, paramDefs...)
	ex.Refs = append(ex.Refs, paramRefs...)
	ex.FieldRHS, ex.FieldListRHS = extractFieldValues(res.Root)
	return ex
}

// deriveFile derives one file's facts for a live reindex. The prefix table stays empty: prefixes are bound against the whole install by bindKinds, and one edited file is not evidence enough to rebind anything.
func deriveFile(gameID, rel string, res jomini.Result, defs []Def, idx *defIndex, schema *declared.Schema) *Derived {
	fire := resolveFireKinds(deriveFireKeys(
		res.Root, idx.keysOfKind("event"), idx.keysOfKind("on_action")))
	if fire == nil {
		fire = map[string]string{}
	}
	v := &Derived{
		PrefixKinds: map[string]string{},
		FireKeys:    fire,
		Wrappers:    map[string]bool{},
	}
	defKeys := idx.citableKeys()
	for _, w := range deriveWrappers(gameID, rel, res.Root, defKeys) {
		v.Wrappers[w] = true
	}
	v.NestedShapes = deriveNestedShapes(
		gameID, oneFile(gameID, rel, res), defs, defKeys, v.PrefixKinds, schema,
		collectTypedCites(res.Root), collectFieldCites(res.Root))
	return v
}

// applyDerivedDefs harvests the nested databases a file contains. citedByKind is the whole corpus's citations, keyed by child kind. A file-local set is not enough: EU5 declares its sub-continents in map_data/definitions.txt but cites them from common/advances/, so the definitions file saw no citations and produced zero defs. Nil falls back to this file's own citations, which is all a live single-file reindex can see.
func applyDerivedDefs(
	gameID, path, origin, rel string, res jomini.Result, defs []Def, derived *Derived,
	citedByKind map[string]map[string]bool,
) []Def {
	if derived == nil {
		return defs
	}
	defs = dropWrapperDefs(defs, derived.Wrappers)
	rule := game.MatchExtract(gameID, rel)
	ck := jomini.CanonicalKind(rule.Kind)
	var local []typedCite
	if citedByKind == nil {
		local = collectTypedCites(res.Root)
	}
	for _, shape := range derived.NestedShapes {
		if jomini.CanonicalKind(shape.ParentKind) != ck && shape.ParentKind != rule.Kind {
			continue
		}
		cited := citedByKind[shape.ChildKind]
		if citedByKind == nil {
			cited = citedIDsForKind(local, derived.PrefixKinds, shape.ChildKind)
		}
		defs = append(defs, applyNestedShape(
			gameID, path, origin, res.Root, res.Lines(), shape, cited,
		)...)
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

func makeDef(kind, key, path, origin string, kr jomini.Range, li *jomini.LineIndex) Def {
	return Def{
		Kind:   jomini.CanonicalKind(kind),
		Key:    key,
		Path:   path,
		Line:   li.PositionAt(kr.Start).Line,
		Start:  kr.Start,
		End:    kr.End,
		Origin: origin,
	}
}

// maxDefDocLines caps one definition's harvested comment: enough for a description without pulling in a whole changelog someone parked above a macro.
const maxDefDocLines = 4

// attachLeadingDocs fills Def.Doc from the comment block written directly above each definition. For a great many objects this is the only description that will ever exist: 4 of CK3's 11,708 scripted macros appear in script_docs, and none of its events, decisions or traits do. Ephemeral and localization defs are skipped -- a saved scope has no declaration site to comment -- and a blank line ends the block, without which a section banner reads as documentation of whatever definition follows it.
func attachLeadingDocs(src string, li *jomini.LineIndex, defs []Def) {
	for i := range defs {
		if !docWorthyKind(defs[i].Kind) {
			continue
		}
		defs[i].Doc = leadingComment(src, li, defs[i].Line)
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

func leadingComment(src string, li *jomini.LineIndex, line int) string {
	var rev []string
	for n := line - 1; n >= 0 && len(rev) < maxDefDocLines; n-- {
		start := li.LineStart(n)
		end := len(src)
		if n+1 < li.LineCount() {
			end = li.LineStart(n + 1)
		}
		body, hashed := jomini.PeelComment(src[start:end])
		if !hashed {
			break // a blank line or real script ends the block
		}
		if body == "" || strings.Trim(body, "=-*_ ") == "" {
			break // a separator rule, not prose
		}
		rev = append(rev, body)
	}
	if len(rev) == 0 {
		return ""
	}
	slices.Reverse(rev)
	return jomini.AuthorProse(rev)
}

func extractTopLevel(root *jomini.Root, li *jomini.LineIndex, gameID, kind, path, origin string) []Def {
	var defs []Def
	for _, st := range root.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted || (a.Op != "=" && a.Op != "?=") {
			continue
		}
		if jomini.BlockOf(a.Value) == nil {
			continue
		}
		name := game.KeyIdentity(gameID, a.Key.Text)
		if !jomini.IsDefName(name) || name == "namespace" {
			continue
		}
		// A date is not a definition: the games key their history files by one, so the same date was harvested as a `struggles` object and a `cultures` object.
		if jomini.IsDateLiteral(name) {
			continue
		}
		defs = append(defs, makeDef(kind, name, path, origin, a.Key.Range, li))
	}
	return defs
}

// extractEventID harvests namespace plus `ident.digits` event ids.
func extractEventID(
	root *jomini.Root, li *jomini.LineIndex, path, origin string,
) (defs []Def, refs []Ref) {
	if root == nil {
		return nil, nil
	}
	var currentNs string
	for _, st := range root.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted || (a.Op != "=" && a.Op != "?=") {
			continue
		}
		if strings.EqualFold(a.Key.Text, "namespace") {
			sc, ok := a.Value.(*jomini.Scalar)
			if !ok || sc.Quoted || sc.Text == "" || !jomini.IsDefName(sc.Text) {
				continue
			}
			currentNs = sc.Text
			defs = append(defs, makeDef("namespace", sc.Text, path, origin, sc.Range, li))
			continue
		}
		if !jomini.IsEventID(a.Key.Text) {
			continue
		}
		defs = append(defs, makeDef("event", a.Key.Text, path, origin, a.Key.Range, li))
		if currentNs != "" {
			refs = append(refs, Ref{
				Key: currentNs, Kind: "namespace", Path: path,
				Line:  li.PositionAt(a.Key.Range.Start).Line,
				Start: a.Key.Range.Start, End: a.Key.Range.End,
			})
		}
	}
	return defs, refs
}

// extractKeywordName harvests `type X` / `template X` GUI forms and any other `keyword name = { }` pair (inline scripted_*).
func extractKeywordName(
	root *jomini.Root, li *jomini.LineIndex, path, origin string,
) ([]Def, map[string]bool, map[string]bool) {
	var defs []Def
	guiTypes := map[string]bool{}
	var scan func(stmts []jomini.Statement)
	scan = func(stmts []jomini.Statement) {
		for i := 0; i < len(stmts)-1; i++ {
			mv, ok := stmts[i].(*jomini.ValueStmt)
			if !ok {
				continue
			}
			sc, ok := mv.Value.(*jomini.Scalar)
			if !ok || sc.Quoted {
				continue
			}
			kind := jomini.DeclKeywordKind(sc.Text)
			if kind == "" {
				continue
			}
			named, ok := stmts[i+1].(*jomini.Assignment)
			if !ok || named.Key.Quoted {
				continue
			}
			if kind == "types" {
				if b := jomini.BlockOf(named.Value); b != nil {
					scan(b.Statements)
				}
				continue
			}
			if !jomini.IsDefName(named.Key.Text) {
				continue
			}
			d := makeDef(kind, named.Key.Text, path, origin, named.Key.Range, li)
			d.Local = jomini.DeclIsLocal(sc.Text)
			defs = append(defs, d)
			if kind == "gui_type" {
				guiTypes[named.Key.Text] = true
			}
		}
		for _, st := range stmts {
			switch n := st.(type) {
			case *jomini.ValueStmt:
				if b := jomini.BlockOf(n.Value); b != nil {
					scan(b.Statements)
				}
			case *jomini.Assignment:
				if b := jomini.BlockOf(n.Value); b != nil {
					scan(b.Statements)
				}
			}
		}
	}
	if root != nil {
		scan(root.Statements)
	}
	guiProps := map[string]bool{}
	jomini.Walk(root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			return true
		}
		k := strings.ToLower(a.Key.Text)
		if jomini.IsLowerName(k) && !jomini.IsStop(k) {
			guiProps[k] = true
		}
		return true
	})
	return defs, guiTypes, guiProps
}

// nestedDepth bounds how far below a definition members are recorded under their parent. Three covers `event > first_valid > triggered_desc > trigger`, the shape that sent readers to the portrait documentation.
const nestedDepth = 3

// harvestStructVocab records a member under the key of the block it sits in. The grandchildren were already being walked -- into the flat vocab bag, which is why `trigger` inside triggered_desc was documented as a portrait's trigger: every name was known, and none of them knew where it belonged.
func harvestStructVocab(root *jomini.Root) (
	counts, blocks map[string]int, vocab map[string]bool,
	nested map[string]map[string]int,
) {
	counts, blocks, vocab = map[string]int{}, map[string]int{}, map[string]bool{}
	nested = map[string]map[string]int{}
	if root == nil {
		return counts, blocks, vocab, nested
	}
	var under func(parent string, b *jomini.Block, depth int)
	under = func(parent string, b *jomini.Block, depth int) {
		if b == nil || depth > nestedDepth {
			return
		}
		for _, c := range b.Statements {
			ca, ok := c.(*jomini.Assignment)
			if !ok || ca.Key.Quoted {
				continue
			}
			k := strings.ToLower(ca.Key.Text)
			if !jomini.IsLowerName(k) || neverAKey[k] {
				continue
			}
			if !jomini.IsStop(k) {
				vocab[k] = true
			}
			if parent != "" {
				m := nested[parent]
				if m == nil {
					m = map[string]int{}
					nested[parent] = m
				}
				m[k]++
			}
			under(k, jomini.BlockOf(ca.Value), depth+1)
		}
	}
	for _, st := range root.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok {
			continue
		}
		body := jomini.BlockOf(a.Value)
		if body == nil {
			continue
		}
		for _, c := range body.Statements {
			ca, ok := c.(*jomini.Assignment)
			if !ok || ca.Key.Quoted {
				continue
			}
			k := strings.ToLower(ca.Key.Text)
			if !jomini.IsLowerName(k) || jomini.IsStop(k) {
				continue
			}
			counts[k]++
			vocab[k] = true
			if sub := jomini.BlockOf(ca.Value); sub != nil {
				blocks[k]++
				under(k, sub, 1)
			}
		}
	}
	return counts, blocks, vocab, nested
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

func extractRefsAndEdges(
	gameID string, root *jomini.Root, li *jomini.LineIndex, path string, defs []Def,
	derived *Derived, schema *declared.Schema,
) ([]Ref, []Edge, []CallCandidate) {
	if root == nil {
		return nil, nil, nil
	}
	var refs []Ref
	var edges []Edge
	var cands []CallCandidate
	containers := defContainers(defs)
	var stack []edgeFrame
	locRef := func(sc *jomini.Scalar, kind string) {
		refs = append(refs, Ref{
			Key: sc.Text, Kind: kind, Path: path,
			Line:  li.PositionAt(sc.Range.Start).Line,
			Start: sc.Range.Start, End: sc.Range.End,
		})
	}
	var walk func(stmts []jomini.Statement)
	walk = func(stmts []jomini.Statement) {
		for _, st := range stmts {
			a, ok := st.(*jomini.Assignment)
			if !ok {
				if vs, ok := st.(*jomini.ValueStmt); ok {
					if b := jomini.BlockOf(vs.Value); b != nil {
						walk(b.Statements)
					}
				}
				continue
			}
			if !a.Key.Quoted {
				key := a.Key.Text
				line := li.PositionAt(a.Key.Range.Start).Line
				from := containerAt(containers, line)
				prop := loc.Classify(key)
				// Broad, never strict: that a field usually holds localization keys is not evidence that every value of it must resolve.
				if prop == loc.PropNone && derived.locField(key) {
					prop = loc.PropBroad
				}
				if prop != loc.PropNone {
					if sc, ok := a.Value.(*jomini.Scalar); ok && sc.Text != "" && loc.LooksLikeKey(sc.Text) {
						kind := "loc-broad"
						if prop == loc.PropStrict {
							kind = "loc"
						}
						locRef(sc, kind)
					}
				}
				// A list whose members are localization keys: a culture's `male_names`, its `cadet_dynasty_names`. The members are values, not assignments, so nothing else here sees them.
				if derived.locField(key) {
					if b := jomini.BlockOf(a.Value); b != nil {
						for _, in := range b.Statements {
							vs, ok := in.(*jomini.ValueStmt)
							if !ok {
								continue
							}
							sc, ok := vs.Value.(*jomini.Scalar)
							if !ok || sc.Text == "" || !loc.LooksLikeKey(sc.Text) {
								continue
							}
							locRef(sc, "loc-broad")
						}
					}
				}
				if sc, ok := a.Value.(*jomini.Scalar); ok && !sc.Quoted && sc.Text != "" && prop == loc.PropNone {
					// A typed cite is already recorded in extractScriptNames.
					if _, _, typed := game.ParseTyped(gameID, sc.Text); !typed {
						if rk := derived.fieldKind(key); rk != "" && !isMacroArgKey(key) {
							if refKey, ok := objectRefKey(gameID, rk, sc.Text); ok {
								refs = append(refs, Ref{
									Key: refKey, Kind: rk, Path: path,
									Line:  li.PositionAt(sc.Range.Start).Line,
									Start: sc.Range.Start, End: sc.Range.End,
									Derived: true,
								})
							}
						}
					}
				}
				if fk := derived.fireKind(key); fk != "" {
					kind, nameKey := fireSiteKind(stack, key)
					// A numeric key is a weight, and the only place one names an event directly is a `random_events` block, where targets are always `namespace.number`. Requiring that shape there -- and only there -- stops `1 = empty` in a Vic3 genes file reading as a fire site.
					numericEvent := isDigitKey(key) && fk == "event"
					walkFireTargets(a.Value, derived, func(to string, start, end int) {
						if numericEvent && !jomini.IsEventID(to) {
							return
						}
						refs = append(refs, Ref{
							Key: to, Kind: fk, Path: path,
							Line: li.PositionAt(start).Line, Start: start, End: end,
							// Declared beats derived, and that cuts both ways: the game DECLARES trigger_event, so a fire ref at one is evidence even though FireKeys is how PMT learned it.
							Derived: !schema.HasName(key),
						})
						edges = append(edges, Edge{
							From: from, To: to, Via: key, Path: path,
							Line: li.PositionAt(start).Line, Kind: kind, NameKey: nameKey,
							Rescoped: fireSiteRescopes(stack),
						})
					})
				} else if from != "" && from != key {
					cands = append(cands, CallCandidate{
						From: from, To: key, Path: path, Line: line,
						Start: a.Key.Range.Start, End: a.Key.Range.End,
					})
				}
			}
			if b := jomini.BlockOf(a.Value); b != nil {
				fr := edgeFrame{key: a.Key.Text}
				if strings.EqualFold(a.Key.Text, "option") {
					fr.nameKey = optionNameKey(a.Value)
				}
				stack = append(stack, fr)
				walk(b.Statements)
				stack = stack[:len(stack)-1]
			}
		}
	}
	walk(root.Statements)
	return refs, edges, cands
}

// extractFieldValues collects the values a field takes, so deriveFieldValueKinds and deriveLocFields can type it: scalars from `field = value`, lists from the bare members of `field = { a b c }`. A list member is a ValueStmt, not an assignment, and localization keys hide there in quantity -- CK3 writes a culture's given names and its cadet dynasty names as lists, and nothing cited them: 6,978 of A Game of Thrones' 20,196 orphaned English keys, 35% of the whole residue. Quoted members count, since vanilla quotes one of those two lists and not the other.
func extractFieldValues(root *jomini.Root) (scalars, lists map[string]map[string]bool) {
	if root == nil {
		return nil, nil
	}
	scalars, lists = map[string]map[string]bool{}, map[string]map[string]bool{}
	add := func(dst map[string]map[string]bool, field, val string) {
		m := dst[field]
		if m == nil {
			m = map[string]bool{}
			dst[field] = m
		}
		m[val] = true
	}
	var walk func(stmts []jomini.Statement)
	walk = func(stmts []jomini.Statement) {
		for _, st := range stmts {
			a, ok := st.(*jomini.Assignment)
			if !ok {
				if vs, ok := st.(*jomini.ValueStmt); ok {
					if b := jomini.BlockOf(vs.Value); b != nil {
						walk(b.Statements)
					}
				}
				continue
			}
			named := !a.Key.Quoted && loc.Classify(a.Key.Text) == loc.PropNone
			if sc, ok := a.Value.(*jomini.Scalar); ok {
				if named && !sc.Quoted && sc.Text != "" && loc.LooksLikeKey(sc.Text) {
					add(scalars, a.Key.Text, sc.Text)
				}
				continue
			}
			b := jomini.BlockOf(a.Value)
			if b == nil {
				continue
			}
			if named {
				for _, in := range b.Statements {
					vs, ok := in.(*jomini.ValueStmt)
					if !ok {
						continue
					}
					sc, ok := vs.Value.(*jomini.Scalar)
					if !ok || sc.Text == "" || !loc.LooksLikeKey(sc.Text) {
						continue
					}
					add(lists, a.Key.Text, sc.Text)
				}
			}
			walk(b.Statements)
		}
	}
	walk(root.Statements)
	if len(scalars) == 0 {
		scalars = nil
	}
	if len(lists) == 0 {
		lists = nil
	}
	return scalars, lists
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
