// validate.go holds the diagnostics that judge script against what the game
// declares, rather than against how it parses.
//
// Every check here is silent unless the game states the thing being violated.
// That is the whole design: PMT derives its type system from script_docs plus
// the install walk, and the derived passes carry coverage floors, so a check
// that fires on derived evidence alone would tell a modder their correct script
// is wrong wherever PMT simply has not seen enough. A missing or partial dump
// must produce silence, never noise.

package lsp

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/session"
)

// duplicateDefDiags flags a key defined twice inside one origin.
//
// One origin, not the workspace: a mod redefining a vanilla object is an
// override, which is the point of modding and is already reported as such by
// Workspace Health. Two definitions of one key inside a single mod is a defect —
// load order decides which survives, and the author almost never means that.
func (f diagFile) duplicateDefDiags() []Diagnostic {
	origin, _, ok := f.s.Locate(f.path)
	if !ok {
		return nil
	}
	if f.isGraphicsData() {
		return nil
	}
	var out []Diagnostic
	for _, d := range f.s.DefsInFile(f.path) {
		if !jomini.IsObjectKind(d.Kind) {
			continue
		}
		// Only a file's own subject. A definition harvested from inside another
		// one — a game rule's options, a script parameter, the members of a
		// nested shape — is a real definition but not the file's subject, and
		// every activity type carries `special_guests`. Judging those reported
		// 1,614 duplicates across 398 vanilla CK3 files, none of them defects.
		if d.Nested {
			continue
		}
		// A key the file declares inline is resolved per file by the engine, so
		// the same one in two files is two separate macros -- required, not a
		// collision. ck3-quieter-events repeats a scripted_trigger across two
		// event files and says why in a comment above it. See Def.Local.
		if d.Local {
			continue
		}
		for _, other := range f.s.ModDefsOf(d.Key) {
			if other.Origin != origin || other.Kind != d.Kind {
				continue
			}
			// Different files only. A key repeated inside ONE file is an
			// authoring idiom, not a defect: CK3's achievement_groups.txt opens
			// `group = { … }` eleven times, and name lists and history files do
			// the same. The load-order hazard this check exists for is two
			// FILES in one mod defining the same key. Counting repetition
			// within a file produced 1,743 findings on 398 vanilla files.
			if session.CanonPath(other.Path) == session.CanonPath(d.Path) {
				continue
			}
			// Report once, at the later site, so the first definition is left
			// clean and the message can point back at it. Ordered by path then
			// offset: line numbers are per-file, so comparing them across two
			// files is meaningless and reported both copies.
			op, dp := session.CanonPath(other.Path), session.CanonPath(d.Path)
			if op > dp || (op == dp && other.Start > d.Start) {
				continue
			}
			where := f.s.DisplayRel(other.Path)
			out = append(out, diag(byteRange(f.li, d.Start, d.End), sevWarning,
				d.Key+" is already defined in "+where+
					"; load order decides which one the game uses.",
				"duplicate-definition"))
			break
		}
	}
	return out
}

// unresolvedRefDiags flags `culture:nonexistent` — a citation whose database the
// game names in the text and whose key nothing defines.
//
// Deliberately narrower than Workspace Health's dangling check. A typed cite is
// the one reference form where the game itself states both the database and the
// key, so a miss is unambiguous; everything else relies on PMT having inferred
// the field's type, and an inference below its coverage floor would produce a
// confident error about correct script.
func (f diagFile) unresolvedRefDiags() []Diagnostic {
	if f.res.Root == nil {
		return nil
	}
	var out []Diagnostic
	jomini.Walk(f.res.Root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Value == nil {
			return true
		}
		text, rng, ok := scalarValue(a)
		if !ok {
			return true
		}
		// A citation followed by a chain is a scope expression, not a plain
		// reference: Victoria 3 writes `owner ?= p:xB0D0E0.state.owner`, where
		// the head is a province keyed by its map colour -- a row that lives in
		// the map data, not in script, so nothing defines it by name. Judging
		// only the head of a chain is a weaker claim than this check makes, and
		// SkipObjectRHS rejects dotted values everywhere else for the same
		// reason.
		if strings.ContainsRune(text, '.') {
			return true
		}
		kind, id, ok := game.ParseTyped(f.s.GameID, text)
		if !ok {
			return true
		}
		if pk := f.s.PrefixKind(strings.SplitN(text, ":", 2)[0]); pk != "" {
			kind = pk
		}
		// Only where the prefix is bound to a database PMT actually holds. An
		// unbound prefix means PMT does not know the database, which is not
		// evidence that the key is missing from it.
		//
		// And never for an ephemeral kind. `flag:private_chamber` names a value
		// the script invents as it runs, the same as a saved scope or a
		// variable; nothing defines it anywhere, so an absence proves nothing.
		if kind == "" || jomini.IsEphemeral(kind) || !f.s.KindHasDefs(kind) {
			return true
		}
		if len(f.s.FindDefsOf(id, kind, 1, false, true)) > 0 {
			return true
		}
		// Nothing of that kind — but the kind PMT bound to the prefix is not
		// always the kind the definition was harvested under, and a nested
		// definition may be filed differently again: CK3's k_austria sits inside
		// another title in 00_landed_titles.txt. Reporting a citation whose name
		// something in the model does define would be a confident error about
		// correct script, so the miss has to be total.
		if len(f.s.FindDefs(id, 1, false, true)) > 0 {
			return true
		}
		out = append(out, diag(byteRange(f.li, rng.Start, rng.End), sevWarning,
			"nothing named "+id+" is defined.", "unresolved-ref"))
		return true
	})
	return out
}

// wrongValueTypeDiags flags a field value that is not of the type the game
// declares for that field.
//
// Declared only. `Supported Targets` states the argument type of 1,694 CK3 /
// 3,834 Vic3 / 1,742 EU5 tokens, and a value contradicting it is wrong however
// little else PMT knows. The install-derived field types are not consulted here
// at all: they exist to offer completions and to name a hover, where being wrong
// costs a wrong suggestion, not a false accusation.
func (f diagFile) wrongValueTypeDiags() []Diagnostic {
	if f.res.Root == nil || !f.s.HasSchema() {
		return nil
	}
	var out []Diagnostic
	jomini.Walk(f.res.Root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			return true
		}
		text, rng, ok := scalarValue(a)
		if !ok || jomini.SkipObjectRHS(text) || isScopeWord(text) {
			return true
		}
		target := f.s.TokenTarget(a.Key.Text)
		if target == "" {
			return true
		}
		cite, kind := f.s.CiteFormOfScope(target)
		if kind == "" || jomini.IsEphemeral(kind) || !f.s.KindHasDefs(kind) {
			return true
		}
		// A citation of the right type is correct however it is spelled, and so
		// is a bare key of the bound database.
		// Any citation at all is left alone. Stripping only the prefix PMT
		// bound to this scope meant `has_title = title:k_austria` -- a correct
		// line -- was read whole and reported, because "title" is not the
		// prefix the binding happened to record. Judging a citation of the
		// wrong database is a different check with a different message.
		bare := text
		if i := strings.IndexByte(text, ':'); i > 0 {
			if cite != "" && text[:i] == cite {
				bare = text[i+1:]
			} else {
				return true
			}
		}
		if len(f.s.FindDefsOf(bare, kind, 1, false, true)) > 0 {
			return true
		}
		// Same total-miss rule unresolved-ref uses, and for the same reason: a
		// nested definition is filed under a kind the binding does not name, so
		// `has_title = title:k_austria` looked unresolved while k_austria sits
		// inside another title in 00_landed_titles.txt.
		if len(f.s.FindDefs(bare, 1, false, true)) > 0 {
			return true
		}
		out = append(out, diag(byteRange(f.li, rng.Start, rng.End), sevWarning,
			a.Key.Text+" takes a "+session.PrettyKind(kind)+"; "+text+
				" is not one.", "wrong-value-type"))
		return true
	})
	return out
}

// scalarValue returns an assignment's right-hand side when it is a bare scalar.
func scalarValue(a *jomini.Assignment) (string, jomini.Range, bool) {
	tok, ok := a.Value.(*jomini.Scalar)
	if !ok || tok.Quoted || tok.Text == "" {
		return "", jomini.Range{}, false
	}
	return tok.Text, tok.Range, true
}

// isScopeWord reports a value that names the current scope rather than a row in
// a database: the built-in keywords, and anything carrying a language prefix.
//
// `has_same_court_language = root` takes a character, and root IS a character —
// it just is not the id of one. Without this the check called 38 correct lines
// wrong on 398 vanilla files.
func isScopeWord(text string) bool {
	if _, ok := jomini.ParsePrefixed(text); ok {
		return true
	}
	switch strings.ToLower(text) {
	case "root", "prev", "this", "from", "fromfrom", "prevprev":
		return true
	}
	return false
}

// minUnknownKeyEvidence is how many members a block must have before its shape
// is complete enough to judge an absence against. A block seen twice tells you
// what it can hold, never what it cannot.
const minUnknownKeyEvidence = 6

// declaredValueDiags flags a value the game's own documentation excludes:
// `uses_diplo_capacity = sometimes` where EU5 writes
// `# uses_diplo_capacity = <none/mutual/giving/receiving>` beside the field.
//
// Experimental, default off. The SOURCE is strong -- a declared closed set
// outranks anything the install walk infers -- but what has not been
// demonstrated is the harvest's precision at scale. These sets come out of prose
// files written by hand, and one mis-read line becomes a confident error about
// correct script. It ships enabled when the vanilla gate is clean on all three
// installs, which is the same condition unknown-key had to meet.
func (f diagFile) declaredValueDiags() []Diagnostic {
	if f.res.Root == nil || f.isGraphicsData() {
		return nil
	}
	_, rel, ok := f.s.Locate(f.path)
	if !ok {
		return nil
	}
	kind := game.MatchExtract(f.s.GameID, rel).Kind
	if kind == "" {
		return nil
	}
	var out []Diagnostic
	for _, st := range f.res.Root.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		b := jomini.BlockOf(a.Value)
		if b == nil {
			continue
		}
		// The definition's own fields only. Documentation describes a kind's
		// shape at that level, and a nested block reusing a name is a different
		// slot -- the same reason the per-kind map exists rather than a global
		// one, one level further in.
		for _, c := range b.Statements {
			ca, ok := c.(*jomini.Assignment)
			if !ok || ca.Key.Quoted {
				continue
			}
			text, rng, ok := scalarValue(ca)
			if !ok || jomini.SkipObjectRHS(text) || isScopeWord(text) {
				continue
			}
			// A value the script computes cannot be matched against a literal
			// set: a macro parameter is unknown until the macro is called, and
			// a citation, a scope path or a script value names something rather
			// than being one.
			if strings.ContainsAny(text, "$.:@") {
				continue
			}
			values := f.s.DeclaredValueSet(kind, ca.Key.Text)
			if len(values) == 0 {
				continue
			}
			if slices.ContainsFunc(values, func(v string) bool {
				return strings.EqualFold(v, text)
			}) {
				continue
			}
			out = append(out, diag(byteRange(f.li, rng.Start, rng.End), sevWarning,
				ca.Key.Text+" is documented as "+strings.Join(values, "/")+"; "+
					text+" is not one of those.", "wrong-declared-value"))
		}
	}
	return out
}

// unknownKeyDiags flags a key that is not a member of the block it sits in.
//
// The riskiest check here, and the only one that reports an ABSENCE: every other
// fires on a positive contradiction of something the game declares. So it is
// gated four ways — the block name must belong to exactly one kind, the block
// must be well-evidenced, the key must be unknown to the model everywhere, and
// the cursor must be inside a block rather than at a definition's top level.
//
// Keyed by name alone this fired 18,341 times on 400 vanilla CK3 files, because
// `option` is a block in an event and another in accolade_names. See
// GAME-SYNTAX §14.
func (f diagFile) unknownKeyDiags() []Diagnostic {
	if f.res.Root == nil || f.isGraphicsData() {
		return nil
	}
	var out []Diagnostic
	var walk func(block string, b *jomini.Block, depth int)
	walk = func(block string, b *jomini.Block, depth int) {
		if b == nil || depth > 4 {
			return
		}
		members := f.s.NestedMembers(block)
		judge := len(members) >= minUnknownKeyEvidence && f.s.BlockIsJudgeable(block)
		known := make(map[string]bool, len(members))
		for _, m := range members {
			known[m] = true
		}
		for _, st := range b.Statements {
			a, ok := st.(*jomini.Assignment)
			if !ok || a.Key.Quoted {
				continue
			}
			key := strings.ToLower(a.Key.Text)
			if judge && !known[key] && !f.knownAnywhere(key) {
				out = append(out, diag(
					byteRange(f.li, a.Key.Range.Start, a.Key.Range.End), sevWarning,
					key+" is not a key of "+block+".", "unknown-key"))
			}
			walk(key, jomini.BlockOf(a.Value), depth+1)
		}
	}
	for _, st := range f.res.Root.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		// Top level is the definition itself; its members are the kind's own
		// structure, which this check does not judge.
		if b := jomini.BlockOf(a.Value); b != nil {
			for _, c := range b.Statements {
				ca, ok := c.(*jomini.Assignment)
				if !ok || ca.Key.Quoted {
					continue
				}
				walk(strings.ToLower(ca.Key.Text), jomini.BlockOf(ca.Value), 1)
			}
		}
	}
	return out
}

// knownAnywhere reports a key the model recognises in some capacity — a declared
// token, a scripted macro, a definition, or a name carrying script grammar. A
// name the engine knows is not evidence of a typo wherever it appears.
func (f diagFile) knownAnywhere(key string) bool {
	if f.s.DeclaresEngineName(key) || f.s.IsMacroDef(key) || jomini.IsMacroKind(key) {
		return true
	}
	if strings.ContainsAny(key, "$.:@") {
		return true // a macro parameter, a scope path, or a script value
	}
	return len(f.s.FindDefs(key, 1, false, true)) > 0
}

// isGraphicsData reports a file under gfx/, which is asset data rather than
// script.
//
// EU5's gfx/map/city_data files are written in a different syntax entirely --
// `template unique_buildings { … }` -- and every one of them carries the same
// blocks. The keys inside them are not definitions competing for a name and not
// members of a script block, so the two checks that judge structure have nothing
// to say about them. Matched anywhere in the path, not as a prefix: EU5 ships
// them under dlc/<pack>/in_game/gfx/map/city_data.
func (f diagFile) isGraphicsData() bool {
	rel := strings.ToLower(strings.ReplaceAll(f.s.DisplayRel(f.path), "\\", "/"))
	return strings.Contains("/"+rel, "/gfx/")
}

// unknownTokenDiags flags an effect or trigger nothing in the model recognises:
// not declared by script_docs, not a harvested scripted_* macro, not a
// definition, not script grammar.
//
// The riskiest check in the file, and the last to ship, because it judges
// against PMT's VOCABULARY rather than against a declaration -- the shape that
// cries wolf. It fires only inside a script slot, where a key is a statement
// rather than a field of a data structure, and only with a schema loaded: with
// none, every token is unknown and the honest output is silence.
func (f diagFile) unknownTokenDiags() []Diagnostic {
	if f.res.Root == nil || f.isGraphicsData() || !f.s.HasSchema() {
		return nil
	}
	var out []Diagnostic
	// inData marks a block that holds a token's ARGUMENTS rather than
	// statements: `add_modifier = { name = … days = … is_decaying = yes }`,
	// `create_character_memory = { participants = { coronator = … } }`. The
	// keys there belong to the token, and judging them as engine API reported
	// 442 pieces of correct vanilla script.
	// args is what the enclosing token DOCUMENTS as its arguments, which matters
	// for the tokens that both open a slot and take them: every iterator accepts
	// `limit`, and an ordered_ one accepts `order_by` beside real statements.
	var walk func(b *jomini.Block, slot string, args map[string]bool, inData bool, depth int)
	walk = func(b *jomini.Block, slot string, args map[string]bool, inData bool, depth int) {
		if b == nil || depth > 8 {
			return
		}
		for _, st := range b.Statements {
			a, ok := st.(*jomini.Assignment)
			if !ok || a.Key.Quoted {
				continue
			}
			key := a.Key.Text
			lk := strings.ToLower(game.KeyIdentity("", key))
			opens := jomini.ScriptSlot(lk)
			if slot != "" && opens == "" && !inData && !args[lk] &&
				f.judgeableToken(lk) && !f.knownAnywhere(key) {
				out = append(out, diag(
					byteRange(f.li, a.Key.Range.Start, a.Key.Range.End), sevWarning,
					key+" is not an effect, trigger or scripted macro the game declares.",
					"unknown-token"))
			}
			next := slot
			if opens != "" {
				next = opens
			}
			// A declared token's block is its argument list, and a scripted
			// macro's is its parameter list -- both data, all the way down.
			//
			// Two exceptions keep real script judgeable: a token that opens a
			// slot (every iterator, `limit`, `trigger`) holds statements, and so
			// does script grammar like `if` and `random_list`, which IsStop
			// names. Without them, `random_list = { 20 = { … } }` would swallow
			// every effect inside it.
			holdsArgs := opens == "" && !jomini.IsStop(lk) &&
				(f.s.DeclaresEngineName(key) || f.s.IsMacroDef(key))
			walk(jomini.ChildBlock(a), next, f.s.TokenParams(lk), inData || holdsArgs, depth+1)
		}
	}
	for _, st := range f.res.Root.Statements {
		if a, ok := st.(*jomini.Assignment); ok {
			walk(jomini.ChildBlock(a), "", nil, false, 1)
		}
	}
	return out
}

// judgeableToken reports a key shaped like a token PMT could recognise. A
// number, a date and script grammar are not claims about the engine API.
func (f diagFile) judgeableToken(lk string) bool {
	if lk == "" || jomini.IsStop(lk) || jomini.IsDateLiteral(lk) {
		return false
	}
	for i := 0; i < len(lk); i++ {
		if c := lk[i]; c != '_' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return lk[0] >= 'a' && lk[0] <= 'z'
}
