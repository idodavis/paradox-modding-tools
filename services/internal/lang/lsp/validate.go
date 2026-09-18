package lsp

import (
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// defDiags is the one pass over the file's own definitions: a key defined twice
// inside one origin, and the localization a kind's universal conventions name.
//
// One origin, not the workspace: a mod redefining a vanilla object is an
// override, which is the point of modding and is already reported as such by
// Workspace Health. Two definitions of one key inside a single mod is a defect —
// load order decides which survives, and the author almost never means that.
func (f diagFile) defDiags() []Diagnostic {
	origin, _, ok := f.s.Locate(f.path)
	if !ok || f.src == "" {
		return nil
	}
	var out []Diagnostic
	for _, d := range f.s.DefsInFile(f.path) {
		out = append(out, f.requiredLoc(d)...)
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
		for _, other := range f.s.ModDefs(d.Key) {
			if other.Origin != origin || other.Kind != d.Kind {
				continue
			}
			// Different files only. A key repeated inside ONE file is an
			// authoring idiom, not a defect: CK3's achievement_groups.txt opens
			// `group = { … }` eleven times, and name lists and history files do
			// the same. The load-order hazard this check exists for is two
			// FILES in one mod defining the same key. Counting repetition
			// within a file produced 1,743 findings on 398 vanilla files.
			if index.CanonPath(other.Path) == index.CanonPath(d.Path) {
				continue
			}
			// Report once, at the later site, so the first definition is left
			// clean and the message can point back at it. Ordered by path then
			// offset: line numbers are per-file, so comparing them across two
			// files is meaningless and reported both copies.
			op, dp := index.CanonPath(other.Path), index.CanonPath(d.Path)
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

// requiredLoc demands the keys a kind's universal conventions name; a coverage short of 1 is a habit.
func (f diagFile) requiredLoc(d index.Def) []Diagnostic {
	var required []loc.Convention
	for _, c := range f.s.LocConventions(d.Kind) {
		if c.Coverage >= 1 {
			required = append(required, c)
		}
	}
	var out []Diagnostic
	for _, key := range loc.Expand(d.Key, required) {
		if f.s.LocDefined(key) {
			continue
		}
		out = append(out, diag(byteRange(f.li, d.Start, d.End), sevWarning,
			"Missing localization key \""+key+"\".", "required-loc"))
	}
	return out
}

// rule is one diagnostic that reads the parse tree: judge sees every assignment
// with the cell position it sits at and returns the span and message it reports.
type rule struct {
	code  string
	judge func(f diagFile, a *jomini.Assignment, hang *index.Node, block string, top bool) (jomini.Range, string, bool)
}

// fileRules is every kept code that reads the tree, in one table; fileChecks is the single walk that feeds them.
var fileRules = []rule{
	{"unresolved-ref", unresolvedRefAt},
	{"wrong-value-type", wrongValueTypeAt},
	{"unknown-key", unknownKeyAt},
}

func (f diagFile) fileChecks() []Diagnostic {
	if f.res.Root == nil {
		return nil
	}
	var out []Diagnostic
	f.eachCell(func(a *jomini.Assignment, hang *index.Node, block string, top bool) {
		for _, r := range fileRules {
			if rng, msg, ok := r.judge(f, a, hang, block, top); ok {
				out = append(out, diag(byteRange(f.li, rng.Start, rng.End),
					sevWarning, msg, r.code))
			}
		}
	})
	return out
}

// unresolvedRefAt flags `culture:nonexistent` — a citation whose database the
// game names in the text and whose key nothing defines.
//
// Deliberately narrower than Workspace Health's dangling check: a citation states its own database, and a bare name is judged only where the install's cell names one.
func unresolvedRefAt(f diagFile, a *jomini.Assignment, hang *index.Node, _ string, _ bool) (jomini.Range, string, bool) {
	none := jomini.Range{}
	text, rng, ok := scalarValue(a)
	if !ok {
		return none, "", false
	}
	// A dotted value is a scope chain, not a name: `p:xB0D0E0.state.owner` cites a province and then walks two links off it. A value at a script-name key (`save_scope_as = x`) is a name the script invents, never a reference to judge.
	if _, named := jomini.ScriptName(a.Key.Text); named || strings.ContainsRune(text, '.') {
		return none, "", false
	}
	id, kind := text, ""
	if k, rest, typed := game.ParseTyped(f.s.GameID, text); typed {
		// A citation needs no cell: it is the one reference form where the game itself states both the database and the key.
		id, kind = rest, k
		if pk := f.s.Install.Tree.PrefixKind(strings.SplitN(text, ":", 2)[0]); pk != "" {
			kind = pk
		}
		// Only a prefix bound to a database PMT holds, and never an ephemeral kind: `flag:private_chamber` names a value the script invents as it runs.
		if kind == "" || jomini.IsEphemeral(kind) || !f.s.KindHasDefs(kind) {
			return none, "", false
		}
	} else {
		// The reporting policy, at the one value it is about: a bare name is judged only where the install's cell names a database for it and never wrote a literal or an unresolved name there itself.
		cell := f.s.Install.Tree.Child(hang, a.Key.Text, a.Key.Quoted, false, false)
		if cell == nil || len(cell.Kinds) == 0 ||
			cell.Has(index.LabelUnresolved) || cell.Has(index.LabelLiteral) ||
			jomini.SkipObjectRHS(text) || isScopeWord(text) {
			return none, "", false
		}
		kind = cell.Kinds[0]
		if jomini.IsEphemeral(kind) || !f.s.KindHasDefs(kind) {
			return none, "", false
		}
	}
	if len(f.s.FindDefsOf(id, kind, 1, false, true)) > 0 {
		return none, "", false
	}
	// The miss has to be total: a nested definition is filed under a kind the binding does not name, and CK3's k_austria sits inside another title.
	if len(f.s.FindDefsOf(id, "", 1, false, true)) > 0 {
		return none, "", false
	}
	// A tier that would not load is a model missing its data rows, which is no evidence that this one is absent.
	if f.s.Install.DataError() != "" && game.IsDataKind(f.s.GameID, kind) {
		return none, "", false
	}
	return rng, "nothing named " + id + " is defined.", true
}

// wrongValueTypeDiags flags a field value that is not of the type the game
// declares for that field.
//
// Declared only. `Supported Targets` states the argument type of 1,694 CK3 /
// 3,834 Vic3 / 1,742 EU5 tokens, and a value contradicting it is wrong however
// little else PMT knows. The install-derived field types are not consulted here
// at all: they exist to offer completions and to name a hover, where being wrong
// costs a wrong suggestion, not a false accusation.
func wrongValueTypeAt(f diagFile, a *jomini.Assignment, hang *index.Node, _ string, _ bool) (jomini.Range, string, bool) {
	none := jomini.Range{}
	if a.Key.Quoted || f.s.Install.Declared.Empty() {
		return none, "", false
	}
	text, rng, ok := scalarValue(a)
	if !ok || jomini.SkipObjectRHS(text) || isScopeWord(text) {
		return none, "", false
	}
	target := tokenTarget(f.s, a.Key.Text)
	if target == "" {
		return none, "", false
	}
	// The same reporting policy unresolved-ref follows: a cell the install
	// itself wrote a literal or an unresolved name at accuses nothing.
	cell := f.s.Install.Tree.Child(hang, a.Key.Text, a.Key.Quoted, false, false)
	if cell != nil &&
		(cell.Has(index.LabelUnresolved) || cell.Has(index.LabelLiteral)) {
		return none, "", false
	}
	cite, kind := f.s.Install.Tree.CiteForm(target)
	if kind == "" || jomini.IsEphemeral(kind) || !f.s.KindHasDefs(kind) {
		return none, "", false
	}
	// Any citation at all is left alone: stripping only the bound prefix read `has_title = title:k_austria` whole and reported a correct line.
	bare := text
	if i := strings.IndexByte(text, ':'); i > 0 {
		if cite != "" && text[:i] == cite {
			bare = text[i+1:]
		} else {
			return none, "", false
		}
	}
	if len(f.s.FindDefsOf(bare, kind, 1, false, true)) > 0 {
		return none, "", false
	}
	// Same total-miss rule unresolved-ref uses, and for the same reason: a
	// nested definition is filed under a kind the binding does not name, so
	// `has_title = title:k_austria` looked unresolved while k_austria sits
	// inside another title in 00_landed_titles.txt.
	if len(f.s.FindDefsOf(bare, "", 1, false, true)) > 0 {
		return none, "", false
	}
	// A tier that would not load is a model missing its data rows, which is no evidence that this one is absent.
	if f.s.Install.DataError() != "" && game.IsDataKind(f.s.GameID, kind) {
		return none, "", false
	}
	return rng, a.Key.Text + " takes a " + PrettyKind(kind) + "; " + text + " is not one.", true
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

// unknownKeyAt flags a key the block it sits in does not hold -- the one check
// that reports an absence, so it judges only where the schema learned a shape
// (§9's structure or body) and never a name the game or a macro declares.
func unknownKeyAt(f diagFile, a *jomini.Assignment, hang *index.Node, block string, top bool) (jomini.Range, string, bool) {
	none := jomini.Range{}
	// The definition's own key sits in no block; its fields are judged one level down, against the kind's own structure.
	if top || a.Key.Quoted {
		return none, "", false
	}
	key := strings.ToLower(a.Key.Text)
	// The arguments of a call are the macro's parameters, not a structure's
	// members: a scripted effect reads whatever `$NAME$` its own body spells,
	// and nothing about the block it is written in says which. A macro whose
	// parameters PMT did not record answers nothing at all.
	if block != "" && f.s.Work.Tree.Macros[block] != "" {
		params := f.s.Params(block)
		if len(params) == 0 || paramNamed(params, key) {
			return none, "", false
		}
		return a.Key.Range, key + " is not a parameter of " + block + ".", true
	}
	if block == "" {
		block = f.fileKind
	}
	if hang == nil || (hang.Class != index.Structure && hang.Class != index.Body) {
		return none, "", false
	}
	// The one classifier decides what the key IS: only a plain field name is a
	// key this check may name. A weight, a date or a citation is a POSITION, a
	// stop word is control flow, a declared token or a macro call is the engine's
	// API, and a row of a database is a name the author is entitled to invent --
	// a mod's own game rule options were 68 of the 69 findings on 2026-09-17. A
	// dot or an unbound colon is a scope path, which is grammar and not a name.
	//
	// InstallTypeOf asks as the corpus folded it. Asking the structure alone
	// reported 22,149 findings on 250 vanilla CK3 event files. Known asks the
	// corpus, the declared API and the game's own docs whether the key exists
	// AT ALL: a real key in the wrong block is not this check's business, and
	// only a name written nowhere is -- CK3's send_interface_toast writes
	// message_filter_type 667 times, never inside send_interface_message.
	cls, _ := f.s.Install.Tree.KeyClass(a.Key.Text, false, hang.Flags&index.Rows != 0)
	if !cls.Nameable() || strings.ContainsAny(key, ".:") || isScopeWord(key) ||
		f.s.Install.Tree.Child(hang, a.Key.Text, false, false, false) != nil ||
		isMember(f.s, hang, a.Key.Text) || f.s.Install.Tree.KnownKey(key) || jomini.IsMacroKind(key) {
		return none, "", false
	}
	// A definition the engine invokes by name is a call wherever it is written: `num_unaligned_vassals > 5` compares a script value the mod wrote, and no block holds it as a field.
	if d := f.s.ResolveMatching(a.Key.Text, func(d index.Def) bool {
		return jomini.DeclaresNamedBodies(d.Kind)
	}); d != nil {
		return none, "", false
	}
	return a.Key.Range, key + " is not a key of " + block + ".", true
}

func paramNamed(params []string, key string) bool {
	for _, p := range params {
		if strings.EqualFold(p, key) {
			return true
		}
	}
	return false
}

