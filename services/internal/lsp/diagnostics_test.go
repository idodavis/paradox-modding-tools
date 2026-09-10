// diagnostics_test.go covers published diagnostics, signature help, and the
// # pmt:ignore suppression flow.

package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/session"
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
			&catalog.VanillaCache{FireKeys: map[string]string{
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

	s := session.NewWithLoc("ws", "ck3", "english", &catalog.VanillaCache{
		InstallPath: install,
	}, nil, []catalog.ModInput{{Origin: "mod", Root: modRoot, Order: 0}})
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
	cache := &catalog.VanillaCache{
		Schema: &catalog.Schema{Effects: map[string]catalog.EngineToken{
			"add_gold": {Usage: "add_gold = { $VALUE$ }"},
			// Declared: since T-3 unknown-event fires only where the fire key
			// is stated by the game, not merely learned from the corpus.
			"trigger_event": {Target: "event"},
		}},
		FieldInfo: map[string]string{"add_gold": "gold"},
		LocAffixes: map[string][]catalog.LocAffix{
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
