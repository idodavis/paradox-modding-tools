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
	var out []Diagnostic
	for _, d := range f.s.DefsInFile(f.path) {
		if !jomini.IsObjectKind(d.Kind) {
			continue
		}
		// A key that is also a member of its own kind's block structure is a
		// field name, not the name of a thing. Several kinds harvest nested keys
		// as definitions — every CK3 activity type carries `special_guests`, so
		// the check reported 1,614 duplicates across 398 vanilla files, all of
		// them one member appearing where it is supposed to.
		if members, _, _ := f.s.MemberSets(d.Kind); members[strings.ToLower(d.Key)] {
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
