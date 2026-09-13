// diagnostics_test.go covers published diagnostics, signature help, and the
// # pmt:ignore suppression flow.

package lsp

import (
	"os"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
	"path/filepath"
	"strings"
	"testing"
)

func TestPmtIgnore(t *testing.T) {
	sup := scanSuppressions(jomini.Parse(
		"# pmt:ignore-next-line unclosed-brace\nbad = {\nok = 1 # pmt:ignore\n").Src)
	if !sup.Covers(1, "unclosed-brace") || sup.Covers(1, "other") || !sup.Covers(2, "anything") {
		t.Fatal(sup)
	}
}

func TestDiagnose(t *testing.T) {
	tests := []struct {
		name, game, rel, body string
		want, drop            string
	}{
		{"unclosed and missing loc", "ck3", "events/x.txt",
			"test.1 = {\n	title = missing_key\n",
			"unclosed-brace", ""},
		{"TITLE arg is not loc", "ck3", "events/x.txt",
			"test.1 = {\n\timmediate = { TITLE = primary_title }\n}\n",
			"", "missing-required-loc"},
		{"metadata skips parser", "vic3", ".metadata/metadata.json", "{\n",
			"", "unclosed-brace"},
		{"missing descriptor", "vic3", "notes.txt", "foo = { }\n",
			"missing-descriptor", ""},
		{"ORDER interp is not loc", "ck3", "localization/english/a_l_english.yml",
			"l_english:\n war:0 \"$ORDER$ fight\"\n",
			"", "missing-required-loc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, root := buildSession(t, tt.game, map[string]string{tt.rel: tt.body}, nil, nil)
			f := filepath.Join(root, filepath.FromSlash(tt.rel))
			diags := Diagnose(s, f)
			if tt.want != "" && !hasDiag(diags, tt.want) {
				t.Fatalf("missing %s: %v", tt.want, diags)
			}
			if tt.want == "unclosed-brace" && !hasDiag(diags, "missing-required-loc") {
				t.Fatalf("missing loc diag: %v", diags)
			}
			if tt.drop != "" && hasDiag(diags, tt.drop) {
				t.Fatalf("unexpected %s: %v", tt.drop, diags)
			}
		})
	}
	t.Run("title loc is not unknown event", func(t *testing.T) {
		body := "namespace = ns\nns.1 = {\n\ttitle = ns.1.t\n" +
			"\tdesc = ns.1.councillor_liege_opening\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body},
			&derived.VanillaCache{FireKeys: map[string]string{
				"title": "event", "desc": "event",
			}}, nil)
		diags := Diagnose(s, filepath.Join(root, "events", "x.txt"))
		if hasDiag(diags, "unknown-event") {
			t.Fatalf("title/desc loc flagged as event: %v", diags)
		}
	})
}

func TestDiagnoseVanillaSilent(t *testing.T) {
	install := t.TempDir()
	vEvent := write(t, install, "game/events/v.txt",
		"test.1 = {\n\ttitle = missing_key\n")
	vLoc := write(t, install, "game/localization/english/v_l_english.yml",
		"l_english:\n k:0 \"v\"\n")
	modRoot := t.TempDir()
	write(t, modRoot, "descriptor.mod", "name = \"t\"\n")
	modEvent := write(t, modRoot, "events/m.txt",
		"mod.1 = {\n\ttitle = missing_key\n}\n")

	s := session.NewWithLoc("ws", "ck3", "english", &derived.VanillaCache{
		InstallPath: install,
	}, nil, []derived.ModInput{{Origin: "mod", Root: modRoot, Order: 0}})
	s.DidOpen(vEvent, "test.1 = {\n\ttitle = missing_key\n")
	s.DidOpen(vLoc, "l_english:\n k:0 \"v\"\n")
	s.DidOpen(modEvent, "mod.1 = {\n\ttitle = missing_key\n}\n")

	if diags := Diagnose(s, vEvent); len(diags) != 0 {
		t.Fatalf("vanilla event diags=%v", diags)
	}
	if diags := Diagnose(s, vLoc); len(diags) != 0 {
		t.Fatalf("vanilla loc diags=%v", diags)
	}
	if !hasDiag(Diagnose(s, modEvent), "missing-required-loc") {
		t.Fatal("mod file must still get missing-required-loc")
	}
	if s.Resolve("test.1") != nil {
		t.Fatal("vanilla DidOpen must not harvest defs into workspace")
	}
}

func TestLocBOMDiag(t *testing.T) {
	const body = "l_english:\n k:0 \"v\"\n"
	rel := "localization/english/a_l_english.yml"
	s, root := buildSession(t, "ck3", map[string]string{rel: body}, nil, nil)
	f := filepath.Join(root, filepath.FromSlash(rel))
	if !hasDiag(Diagnose(s, f), "missing-bom") {
		t.Fatal("want missing-bom when disk has no BOM")
	}
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte(body)...)
	if err := os.WriteFile(f, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s.DidOpen(f, body)
	if hasDiag(Diagnose(s, f), "missing-bom") {
		t.Fatal("disk BOM must not warn")
	}
}

func TestSignatureHelpAndSchemaDiags(t *testing.T) {
	cache := &derived.VanillaCache{
		Schema: &declared.Schema{Effects: map[string]declared.EngineToken{
			"add_gold": {Usage: "add_gold = { $VALUE$ }"},
			// Declared: since T-3 unknown-event fires only where the fire key
			// is stated by the game, not merely learned from the corpus.
			"trigger_event": {Target: "event"},
		}},
		FieldInfo: map[string]string{"add_gold": "gold"},
		LocAffixes: map[string][]loc.Affix{
			"traits": {{Pre: "trait_", Defs: 10, Of: 10}},
		},
	}
	s, root := buildSession(t, "ck3", map[string]string{
		"events/x.txt":         "namespace = ns\nns.1 = {\n\timmediate = { add_gold = {  trigger_event = ns.99 }\n}\n",
		"common/traits/00.txt": "ambitious = { }\n",
	}, cache, nil)
	f := filepath.Join(root, "events", "x.txt")
	line, col := lineCol(
		"namespace = ns\nns.1 = {\n\timmediate = { add_gold = {  trigger_event = ns.99 }\n}\n",
		"add_gold = {",
	)
	h := SignatureHelp(s, f, line, col+len("add_gold = {"))
	if h == nil || !strings.Contains(h.Label, "add_gold") {
		t.Fatalf("signature: %#v", h)
	}

	tf := filepath.Join(root, "common", "traits", "00.txt")
	if !hasDiag(Diagnose(s, tf), "required-loc") {
		t.Fatal("trait required-loc missing")
	}
	if !hasDiag(Diagnose(s, f), "unknown-event") {
		t.Fatal("unknown-event missing")
	}
}

func TestDecisionConventionLocIsNotRequired(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/decisions/d.txt":               "foo = {\n\tselection_tooltip = foo_tooltip\n}\n",
		"localization/english/a_l_english.yml": "\ufeffl_english:\n foo:0 \"F\"\n foo_tooltip:0 \"T\"\n",
	}, nil, nil)
	df := filepath.Join(root, "common", "decisions", "d.txt")
	if hasDiag(Diagnose(s, df), "required-loc") {
		t.Fatal("decision suffix loc must not be required-loc")
	}
}

func labels(items []CompletionItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

func TestDiagnoseMessageConventionNotMissing(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/messages/x.txt": "quieter_events_neutral = {\n" +
			"	title = event_message_title\n}\n",
		"localization/english/a_l_english.yml": "l_english:\n event_message_title:0 \"T\"\n",
	}, nil, nil)
	f := filepath.Join(root, "common", "messages", "x.txt")
	for _, d := range Diagnose(s, f) {
		if strings.Contains(d.Message, "quieter_events_neutral") ||
			strings.Contains(d.Message, "rule_quieter") ||
			strings.Contains(d.Message, "setting_quieter") {
			t.Fatalf("invented loc diag: %+v", d)
		}
	}
}
func TestWrongScopeDiagnostic(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_gold = 100
		}
	}
}
`
	s, p := scopedSession(t, body)
	diags := Diagnose(s, p)
	if !hasDiag(diags, "wrong-scope") {
		t.Fatalf("want wrong-scope for a character effect in a title block: %+v", diags)
	}
}

func TestWrongScopeSilentWhenCorrect(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_title_law = x
		}
		add_gold = 5
	}
}
`
	s, p := scopedSession(t, body)
	if diags := Diagnose(s, p); hasDiag(diags, "wrong-scope") {
		t.Errorf("false positive on correct script: %+v", diags)
	}
}

// No type system means no scope claims at all.
func TestWrongScopeSilentWithoutSchema(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_gold = 100
		}
	}
}
`
	c := &derived.VanillaCache{Vocabulary: []string{"add_gold"}}
	derived.PrepareCache(c)
	s, root := buildSession(t, "ck3", map[string]string{"common/on_action/x.txt": body}, c, nil)
	if diags := Diagnose(s, root+"/common/on_action/x.txt"); hasDiag(diags, "wrong-scope") {
		t.Errorf("claimed a scope error with no schema: %+v", diags)
	}
}

// An unknown block stops the walk, so nothing below it is judged.
func TestWrongScopeSilentAfterUnknownBlock(t *testing.T) {
	body := `on_birth = {
	effect = {
		mystery_block = {
			add_title_law = x
		}
	}
}
`
	s, p := scopedSession(t, body)
	if diags := Diagnose(s, p); hasDiag(diags, "wrong-scope") {
		t.Errorf("judged script under an unknown transition: %+v", diags)
	}
}

// The suppression comment applies to the new code like any other.
func TestWrongScopeSuppressed(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_gold = 100 # pmt:ignore wrong-scope
		}
	}
}
`
	s, p := scopedSession(t, body)
	if diags := Diagnose(s, p); hasDiag(diags, "wrong-scope") {
		t.Errorf("suppression ignored: %+v", diags)
	}
}

// Scripted macros defined in the mod stay available alongside engine tokens.
func TestWrongScopeOutsideOnActions(t *testing.T) {
	// every_vassal declares Targets: character, so it establishes a character
	// scope with nothing before it. add_title_law is landed_title only.
	const body = "namespace = test\n\ntest.1 = {\n" +
		"\timmediate = {\n\t\tevery_vassal = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"events/x.txt": body}, scopedCache(), nil)
	diags := Diagnose(s, root+"/events/x.txt")
	if !hasDiag(diags, "wrong-scope") {
		t.Fatalf("no wrong-scope inside an event body: %+v", diags)
	}
}

// TestWrongScopeSilentWithoutDeclaredScope: continuing past an empty root must
// not start guessing. Where nothing declared establishes a scope there is
// nothing to contradict, so the check stays quiet.
func TestWrongScopeSilentWithoutDeclaredScope(t *testing.T) {
	const body = "namespace = test\n\ntest.1 = {\n" +
		"\timmediate = {\n\t\tadd_title_law = x\n\t}\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"events/x.txt": body}, scopedCache(), nil)
	if diags := Diagnose(s, root+"/events/x.txt"); hasDiag(diags, "wrong-scope") {
		t.Errorf("judged a scope nothing declared: %+v", diags)
	}
}

// TestWrongScopeRootWithoutDeclaredRoot is nine vanilla CK3 findings, all one
// shape. camp_party_actions.txt writes
//
//	effect = { random_attending_character = { limit = { root = { any_… } } } }
//
// where root is the ACTIVITY the pulse action runs on. Only an on_action
// declares its root scope, so the walk's stack bottom here is just the first hop
// it happened to take -- "character" -- and reading root as stack[0] reported
// correct vanilla script as wrong-scope. An unrooted walk must treat root as a
// scope it cannot name.
func TestWrongScopeRootWithoutDeclaredRoot(t *testing.T) {
	const body = "namespace = test\n\ntest.1 = {\n\timmediate = {\n" +
		"\t\tevery_vassal = {\n\t\t\troot = {\n\t\t\t\tadd_title_law = x\n" +
		"\t\t\t}\n\t\t}\n\t}\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"events/x.txt": body}, scopedCache(), nil)
	if diags := Diagnose(s, root+"/events/x.txt"); hasDiag(diags, "wrong-scope") {
		t.Errorf("judged `root` against a scope nothing declared: %+v", diags)
	}
}

// And where the root IS declared, root still resolves and is still judged.
func TestWrongScopeRootWhenDeclared(t *testing.T) {
	const body = "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n" +
		"\t\t\troot = {\n\t\t\t\tadd_title_law = x\n\t\t\t}\n\t\t}\n\t}\n}\n"
	s, p := scopedSession(t, body)
	if !hasDiag(Diagnose(s, p), "wrong-scope") {
		t.Errorf("on_birth declares a character root; add_title_law is landed_title only")
	}
}

// TestWrongScopeFollowsDottedChain: `root.capital_county = { … }` is two hops,
// and the walk stopped dead at the dot. ck3-tiger models these explicitly
// (open_builder / finalize_builder); PMT was silent rather than wrong, which is
// safe and useless -- a chain is one of the commonest ways to change scope.
func TestWrongScopeFollowsDottedChain(t *testing.T) {
	// on_birth roots at character; .capital_county hops to landed_title, where
	// add_gold does not belong.
	const bad = "on_birth = {\n\teffect = {\n\t\troot.capital_county = {\n" +
		"\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n"
	s, p := scopedSession(t, bad)
	if !hasDiag(Diagnose(s, p), "wrong-scope") {
		t.Errorf("chain not followed: add_gold is character-only, root.capital_county is a title")
	}

	// The same chain with a title effect is correct and must stay silent.
	const good = "on_birth = {\n\teffect = {\n\t\troot.capital_county = {\n" +
		"\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
	s2, p2 := scopedSession(t, good)
	if hasDiag(Diagnose(s2, p2), "wrong-scope") {
		t.Errorf("false positive on a correct chain")
	}

	// Three hops, back to character: add_gold belongs again.
	const back = "on_birth = {\n\teffect = {\n\t\troot.capital_county.holder = {\n" +
		"\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n"
	s3, p3 := scopedSession(t, back)
	if hasDiag(Diagnose(s3, p3), "wrong-scope") {
		t.Errorf("false positive after hopping back to character")
	}
}

// TestScopeChainStopsOnUnknownSegment: half a chain is not a scope. A segment
// PMT cannot resolve ends the walk rather than leaving the previous hop's scope
// in place to be judged against.

func benchSession(b *testing.B) (*session.Session, string) {
	b.Helper()
	install := os.Getenv("PMT_TEST_INSTALL_CK3")
	docs := os.Getenv("PMT_TEST_DOCS_CK3")
	if install == "" || docs == "" {
		b.Skip("env")
	}
	schema := declared.ReadSchema([]string{docs})
	// A real, large CK3 event file, copied into a mod so diagnostics run on it.
	srcPath := filepath.Join(install, "game", "events", "global_religion_events.txt")
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		b.Skip(err)
	}
	mod := b.TempDir()
	if err := os.WriteFile(filepath.Join(mod, "descriptor.mod"),
		[]byte("name = \"t\"\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mod, "events"), 0o755); err != nil {
		b.Fatal(err)
	}
	p := filepath.Join(mod, "events", "big.txt")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		b.Fatal(err)
	}
	cache := &derived.VanillaCache{GameID: "ck3", InstallPath: install, Schema: schema}
	derived.PrepareCache(cache)
	s := session.NewWithLoc("ws", "ck3", "english", cache, nil,
		[]derived.ModInput{{Origin: "mod", Root: mod, Order: 0}})
	s.DidOpen(p, string(raw))
	b.Logf("file is %d bytes", len(raw))
	return s, p
}

func BenchmarkDiagnoseAll(b *testing.B) {
	s, p := benchSession(b)
	b.ResetTimer()
	for b.Loop() {
		_ = Diagnose(s, p)
	}
}

func BenchmarkDiagnoseUnknownTokenOnly(b *testing.B) {
	s, p := benchSession(b)
	f := diagFile{s: s, path: p, kind: s.KindFor(p), src: s.FileText(p)}
	f.refs = s.RefsInFile(p)
	f.res = s.Parsed(p)
	f.li = f.res.Lines()
	b.ResetTimer()
	for b.Loop() {
		_ = f.unknownTokenDiags()
	}
}

func BenchmarkDiagnoseDeclaredValueOnly(b *testing.B) {
	s, p := benchSession(b)
	f := diagFile{s: s, path: p, kind: s.KindFor(p), src: s.FileText(p)}
	f.refs = s.RefsInFile(p)
	f.res = s.Parsed(p)
	f.li = f.res.Lines()
	b.ResetTimer()
	for b.Loop() {
		_ = f.declaredValueDiags()
	}
}

func BenchmarkDiagnoseDefaultsEnabled(b *testing.B) {
	s, p := benchSession(b)
	on := EnabledCodes(nil) // what a user with no settings actually gets
	b.ResetTimer()
	for b.Loop() {
		_ = DiagnoseEnabled(s, p, on)
	}
}
