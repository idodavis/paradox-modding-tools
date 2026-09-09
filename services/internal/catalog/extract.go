// extract.go is the per-file catalog extractor (scan, BuildIndex, session reindex).

package catalog

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/parser/loc"
)

var ()

// stoplist are grammar/logic words never harvested as structure keys or vocabulary.
// neverAKey is the subset of stoplist that is not a key anywhere: a value, an
// operand, or a scope word. Unlike the rest of stoplist these cannot appear on
// the left of an `=`, so they are excluded from the nested table too.
var neverAKey = map[string]bool{
	"this": true, "root": true, "prev": true, "from": true,
	"yes": true, "no": true,
}

// EdgeKindCall marks a scripted-effect invocation (not drawn on the canvas).
const EdgeKindCall = "call"

// CallCandidate is a possible scripted_* invocation; ApplyCallEdges keeps
// effects for the graph, ApplyCallRefs keeps trigger/effect/modifier refs.
type CallCandidate struct {
	From, To, Path   string
	Line, Start, End int
}

// FileExtract is one parsed file's catalog harvest.
type FileExtract struct {
	Defs       []Def
	Refs       []Ref
	Edges      []Edge
	Cands      []CallCandidate
	Loc        LocDelta
	StructKind string
	// MemberKind is StructKind for the member-name harvest, which unlike the
	// rest runs for mods too. See the assignment site for why they are separate.
	MemberKind   string
	StructCounts map[string]int
	// StructNested is block key -> member key -> occurrences, so a member is
	// known under the block it belongs to rather than only under its file's kind.
	StructNested map[string]map[string]int
	// StructNestedKind is the kind whose files these blocks were seen in. A
	// block name means different things under different kinds — CK3 has an
	// `option` in an event and another in accolade_names — so a check that
	// judges an absence needs to know whether the name is ambiguous.
	StructNestedKind string
	StructBlocks     map[string]int
	Vocab            map[string]bool
	GUITypes         map[string]bool
	GUIProps         map[string]bool
	FieldRHS         map[string]map[string]bool
	// FieldListRHS is the bare members of a list value, keyed by the field that
	// holds the list. See extractFieldListValues.
	FieldListRHS map[string]map[string]bool
	// LocOrder is the file's index in the walk list, i.e. its load order. It
	// decides which value survives when two files define one key; see
	// accum.mergeLocOrdered.
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
	}
	return defs, LocDelta{Lang: lang, Vals: vals}, refs
}

// parsedScript is one install/mod file kept in RAM for the derive + nested pass.
type parsedScript struct {
	f   fileRef
	res jomini.Result
}

// ExtractParsed extracts defs, refs, edges, and call candidates from a parsed
// script or GUI file. overlay supplies vanilla derived for live reindex; nil
// overlay derived from this file only (tests). Callers turn Cands into edges
// via ApplyCallEdges.
func ExtractParsed(
	gameID, absPath, rel, origin string,
	res jomini.Result,
	harvestBodies bool,
	overlay *VanillaCache,
) FileExtract {
	ex := extractForms(gameID, absPath, rel, origin, res, harvestBodies)
	derived := derivedFromCache(overlay)
	var schema *Schema
	if overlay != nil {
		schema = overlay.Schema
	}
	derived.mergeLocal(deriveFile(gameID, rel, res, ex.Defs, schema))
	derived.mergeLocal(&Derived{FieldValueKinds: deriveFieldValueKinds(ex.FieldRHS, ex.Defs)})
	ex.Defs = applyDerivedDefs(gameID, absPath, origin, rel, res, ex.Defs, derived, nil)
	ex.Refs = append(ex.Refs, conventionLocRefs(res.Lines(), absPath, ex.Defs, derived)...)
	bodyRefs, edges, cands := extractRefsAndEdges(
		gameID, res.Root, res.Lines(), absPath, ex.Defs, derived,
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
		// GUI took a branch of its own and never harvested block members, so a
		// whole file kind had no structure model: hovering `offset` inside a
		// texticon produced no card at all. The nested harvest is generic —
		// key to child — so it reads GUI as readily as script.
		_, _, _, ex.StructNested = harvestStructVocab(res.Root)
		ex.StructNestedKind = "gui"
	case game.ModeTopLevelKey, game.ModeEventID:
		if rule.Mode == game.ModeEventID {
			var nsRefs []Ref
			ex.Defs, nsRefs = extractEventID(res.Root, res.Lines(), absPath, origin)
			ex.Refs = nsRefs
		} else {
			ex.Defs = extractTopLevel(res.Root, res.Lines(), gameID, rule.Kind, absPath, origin)
		}
		if rule.Mode == game.ModeEventID {
			kw, _, _ := extractKeywordName(res.Root, res.Lines(), absPath, origin)
			ex.Defs = append(ex.Defs, kw...)
		}
		attachLeadingDocs(res.Src, res.Lines(), ex.Defs)
		// MemberKind is set for mods as well as the install; StructKind stays
		// install-only. They name the same kind, and the split exists because
		// StructKind drives more than the member names: per-kind field values,
		// the block-shape table and the completion vocabulary are all derived
		// from vanilla on purpose, and letting a mod contribute to them changed
		// which references counted as dangling.
		//
		// The member names alone are needed everywhere, because that is what a
		// localization member convention resolves against — `setting_<option>`
		// names an option nested inside a game rule, and a mod's own new option
		// exists in no install.
		kind := rule.Kind
		if rule.Mode == game.ModeEventID {
			kind = "event"
		}
		ex.MemberKind = kind
		counts, blocks, vocab, nested := harvestStructVocab(res.Root)
		ex.StructCounts = counts
		ex.StructNested = nested
		ex.StructNestedKind = kind
		if harvestBodies {
			ex.StructKind = kind
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
	ex.FieldRHS = extractFieldRHS(res.Root)
	ex.FieldListRHS = extractFieldListValues(res.Root)
	return ex
}

// deriveFile derives one file's facts for a live reindex. The prefix table stays
// empty here: prefixes are bound against the whole install by bindKinds, and one
// edited file is not evidence enough to rebind anything.
func deriveFile(gameID, rel string, res jomini.Result, defs []Def, schema *Schema) *Derived {
	ev, oa := eventOnActionIDs(defs)
	fire := resolveFireKinds(deriveFireKeys(res.Root, ev, oa))
	if fire == nil {
		fire = map[string]string{}
	}
	v := &Derived{
		PrefixKinds: map[string]string{},
		FireKeys:    fire,
		Wrappers:    map[string]bool{},
	}
	for _, w := range deriveWrappers(gameID, rel, res.Root, defKeySet(defs)) {
		v.Wrappers[w] = true
	}
	v.NestedShapes = deriveNestedShapes(
		gameID, oneFile(gameID, rel, res), defs, v.PrefixKinds, schema,
		collectTypedCites(res.Root), collectFieldCites(res.Root))
	return v
}

// applyDerivedDefs harvests the nested databases a file contains.
//
// citedByKind is the whole corpus's citations, keyed by child kind. A file-local
// set is not enough and quietly harvested nothing: EU5 declares its
// sub-continents in `map_data/definitions.txt` but cites them from
// `common/advances/`, so the definitions file saw no citations and produced
// zero defs. CK3 faiths only worked because `faith:` happens to be cited inside
// the religion files themselves. Nil falls back to this file's own citations,
// which is all a live single-file reindex can see.
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

// citedIDsByKind indexes the corpus's citations once per nested child kind, so
// the second pass does not re-scan them for every file.
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

// ExtractFile extracts one decoded (CR-free) file, choosing the loc or script
// path from the game's extract rule; loc files skip the script parse.
func ExtractFile(gameID, absPath, rel, origin, text string, harvestBodies bool) FileExtract {
	text = jomini.Normalize(text)
	if game.MatchExtract(gameID, rel).Mode == game.ModeLocKey {
		var ex FileExtract
		ex.Defs, ex.Loc, ex.Refs = extractLoc(filepath.Clean(absPath), text, origin)
		return ex
	}
	return ExtractParsed(gameID, absPath, rel, origin, jomini.Parse(text), harvestBodies, nil)
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

// maxDefDocLines caps one definition's harvested comment. Four lines is what
// the install-prose reader already keeps, and it is enough for a description
// without pulling in a whole changelog someone parked above a macro.
const maxDefDocLines = 4

// attachLeadingDocs fills Def.Doc from the comment block written directly above
// each definition.
//
// For a great many objects this is the only description that will ever exist.
// script_docs covers the engine API, not script: 4 of CK3's 11,708 scripted
// macros appear in it, and none of its events, decisions or traits do. Authors
// document them in a comment instead — vanilla comments 29% of its scripted
// effects and events are commented more often still, and mods far more than
// vanilla.
//
// Ephemeral and localization defs are skipped: a saved scope or a loc key has no
// declaration site to comment, and keeping their neighbours' comments would both
// mislead and dominate the cache. Everything conventionally declared — a root
// key, an event id, a nested row — is kept.
//
// A blank line ends the block. Without that, the banner above a section
// ("#####  COURT EVENTS  #####") would be read as documentation of whichever
// definition happened to follow it.
func attachLeadingDocs(src string, li *jomini.LineIndex, defs []Def) {
	for i := range defs {
		if !docWorthyKind(defs[i].Kind) {
			continue
		}
		defs[i].Doc = leadingComment(src, li, defs[i].Line)
	}
}

// docWorthyKind reports a definition whose declaration site is a line an author
// can comment.
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
	return authorProse(rev)
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
		// A date is not a definition. The games key their history files by one —
		// `history/struggles/…` opens `718.1.1 = { … }` and
		// `history/cultures/afghan.txt` opens `867.1.1 = { … }` — so the same
		// date was harvested as a `struggles` object and a `cultures` object,
		// and hovering `game_start_date < 1178.1.1` produced a struggle card
		// while go-to-definition jumped into a culture's history.
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

// extractKeywordName harvests `type X` / `template X` GUI forms and any other
// `keyword name = { }` pair (inline scripted_*).
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
			kind := game.DeclKeywordKind(sc.Text)
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
			d.Local = game.DeclIsLocal(sc.Text)
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

// nestedDepth bounds how far below a definition members are recorded under their
// parent. Three covers `event > first_valid > triggered_desc > trigger`, which is
// the shape that sent readers to the portrait documentation; going deeper grows
// the table for blocks nobody edits by hand.
const nestedDepth = 3

func harvestStructVocab(root *jomini.Root) (
	counts, blocks map[string]int, vocab map[string]bool,
	nested map[string]map[string]int,
) {
	counts, blocks, vocab = map[string]int{}, map[string]int{}, map[string]bool{}
	nested = map[string]map[string]int{}
	if root == nil {
		return counts, blocks, vocab, nested
	}
	// Record a member under the key of the block it sits in. The grandchildren
	// were already being walked -- they went into the flat vocab bag, which is
	// why `trigger` inside triggered_desc was documented as a portrait's
	// trigger: every name was known, and none of them knew where it belonged.
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
			// Recorded and recursed even when the kind vocabulary stoplists it.
			// That list keeps control flow out of a KIND's member names, which
			// is right, but `first_valid`, `triggered_desc`, `limit` and `if`
			// are exactly the blocks worth knowing the inside of — and they are
			// legitimate keys in the positions they appear, which is what this
			// table is for.
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

// defContainers lists the objects a line can belong to, for containerAt.
//
// IsObjectKind rather than a hand-written exclusion list: this was the only one
// of six "what counts as a definition" predicates that did not exclude ephemeral
// kinds, and jomini's own comment names itself the canonical one — "anything
// resolving a name to a thing wants this predicate".
//
// The cost of the omission was that a `saved_scope` def, created at the line of
// a save_scope_as RHS *inside* an event body, outranked the event as a
// container — containerAt takes the LAST container at or before a line. On the
// real installs that made 5,604 of CK3's 17,073 non-call edges and 1,501 of
// Vic3's 3,847 point at a source node that does not exist.
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
	if jomini.SkipFieldRHS(text) {
		return "", false
	}
	// Grammar words are never object names. `c:FRA ?= this` recorded `this` as a
	// reference 253 times on one Vic3 mod; the stoplist already knew better, but
	// nothing on the reference path consulted it.
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
	derived *Derived,
) ([]Ref, []Edge, []CallCandidate) {
	var refs []Ref
	var edges []Edge
	var cands []CallCandidate
	if root == nil {
		return nil, nil, nil
	}
	containers := defContainers(defs)
	var stack []edgeFrame
	var walk func(stmts []jomini.Statement)
	walk = func(stmts []jomini.Statement) {
		for _, st := range stmts {
			a, ok := st.(*jomini.Assignment)
			if ok {
				if !a.Key.Quoted {
					key := a.Key.Text
					line := li.PositionAt(a.Key.Range.Start).Line
					from := containerAt(containers, line)
					prop := loc.Classify(key)
					if prop == loc.PropNone && derived.locField(key) {
						// Broad, never strict: that a field usually holds
						// localization keys is not evidence that every value of
						// it must resolve. It marks a key used; it never demands
						// one exist.
						prop = loc.PropBroad
					}
					if prop != loc.PropNone {
						if sc, ok := a.Value.(*jomini.Scalar); ok && sc.Text != "" && loc.LooksLikeKey(sc.Text) {
							kind := "loc-broad"
							if prop == loc.PropStrict {
								kind = "loc"
							}
							refs = append(refs, Ref{
								Key: sc.Text, Kind: kind, Path: path,
								Line:  li.PositionAt(sc.Range.Start).Line,
								Start: sc.Range.Start, End: sc.Range.End,
							})
						}
					}
					// A list whose members are localization keys: a culture's
					// `male_names`, its `cadet_dynasty_names`. The members are
					// values, not assignments, so nothing else here sees them.
					if derived.locListField(key) {
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
								refs = append(refs, Ref{
									Key: sc.Text, Kind: "loc-broad", Path: path,
									Line:  li.PositionAt(sc.Range.Start).Line,
									Start: sc.Range.Start, End: sc.Range.End,
								})
							}
						}
					}
					if sc, ok := a.Value.(*jomini.Scalar); ok && !sc.Quoted && sc.Text != "" {
						if prop == loc.PropNone {
							if _, _, ok := game.ParseTyped(gameID, sc.Text); ok {
								// typed cite already recorded in extractScriptNames
							} else if rk := derived.fieldKind(key); rk != "" && !isMacroArgKey(key) {
								if refKey, ok := objectRefKey(gameID, rk, sc.Text); ok {
									refs = append(refs, Ref{
										Key: refKey, Kind: rk, Path: path,
										Line:  li.PositionAt(sc.Range.Start).Line,
										Start: sc.Range.Start, End: sc.Range.End,
									})
								}
							}
						}
					}
					if fk := derived.fireKind(key); fk != "" {
						kind, nameKey := fireSiteKind(stack, key)
						// A numeric key is a weight, and the only place one
						// names an event directly is a `random_events` block,
						// where targets are always `namespace.number`. Requiring
						// that shape there — and only there — stops `1 = empty`
						// in a Vic3 genes file reading as a fire site, without
						// silencing a real broken target on a named key like
						// trigger_event.
						numericEvent := isDigitKey(key) && fk == "event"
						walkFireTargets(a.Value, derived, func(to string, start, end int) {
							if numericEvent && !jomini.IsEventID(to) {
								return
							}
							refs = append(refs, Ref{
								Key: to, Kind: fk, Path: path,
								Line: li.PositionAt(start).Line, Start: start, End: end,
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
				continue
			}
			if vs, ok := st.(*jomini.ValueStmt); ok {
				if b := jomini.BlockOf(vs.Value); b != nil {
					walk(b.Statements)
				}
			}
		}
	}
	walk(root.Statements)
	return refs, edges, cands
}

// extractFieldRHS collects `field = value` pairs whose RHS could name a def, so
// deriveFieldValueKinds can later type the field. Fire keys are not excluded
// here: this runs before any fire key is known, and the fire pass filters its
// own keys anyway.
func extractFieldRHS(root *jomini.Root) map[string]map[string]bool {
	if root == nil {
		return nil
	}
	out := map[string]map[string]bool{}
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
			if !a.Key.Quoted && loc.Classify(a.Key.Text) == loc.PropNone {
				if sc, ok := a.Value.(*jomini.Scalar); ok && !sc.Quoted &&
					sc.Text != "" && loc.LooksLikeKey(sc.Text) {
					m := out[a.Key.Text]
					if m == nil {
						m = map[string]bool{}
						out[a.Key.Text] = m
					}
					m[sc.Text] = true
				}
			}
			if b := jomini.BlockOf(a.Value); b != nil {
				walk(b.Statements)
			}
		}
	}
	walk(root.Statements)
	if len(out) == 0 {
		return nil
	}
	return out
}

// extractFieldListValues collects the bare members of a list — `male_names = {
// Abu-Bakr Aarif … }` — keyed by the field that holds the list.
//
// A list member is a `ValueStmt`, not an assignment, so extractFieldRHS never
// saw one: it reads the right-hand side of `field = value`. Localization keys
// hide there in quantity. CK3 writes a culture's given names and its cadet
// dynasty names as lists, both are keys (`Abbas:0 "Abbas"`,
// `dynn_Rasulid:0 "Rasulid"`), and nothing cited them — 6,978 of A Game of
// Thrones' 20,196 orphaned English keys, 35% of the whole residue.
//
// Quoted members count. Vanilla quotes its cadet dynasty names and not its
// given names, and the quoting says nothing about whether the value is a key.
func extractFieldListValues(root *jomini.Root) map[string]map[string]bool {
	if root == nil {
		return nil
	}
	out := map[string]map[string]bool{}
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
			b := jomini.BlockOf(a.Value)
			if b == nil {
				continue
			}
			if !a.Key.Quoted && loc.Classify(a.Key.Text) == loc.PropNone {
				for _, in := range b.Statements {
					vs, ok := in.(*jomini.ValueStmt)
					if !ok {
						continue
					}
					sc, ok := vs.Value.(*jomini.Scalar)
					if !ok || sc.Text == "" || !loc.LooksLikeKey(sc.Text) {
						continue
					}
					m := out[a.Key.Text]
					if m == nil {
						m = map[string]bool{}
						out[a.Key.Text] = m
					}
					m[sc.Text] = true
				}
			}
			walk(b.Statements)
		}
	}
	walk(root.Statements)
	if len(out) == 0 {
		return nil
	}
	return out
}

// isMacroArgKey reports the all-caps convention Paradox uses for the arguments
// of a scripted macro: `some_effect = { ROLE = chemist }`. The value is a
// fragment the macro pastes into a name, not a reference to an object of the
// field's type — Morgenroete builds `character_role_mendelejew_chemist` that
// way, and reading `chemist` as a character_roles reference reported it missing.
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
