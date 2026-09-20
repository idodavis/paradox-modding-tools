// definition_test.go covers go-to-definition, references, and symbols.
package lsp

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/session"
)

func hasURI(locs []Location, sub string) bool {
	for _, loc := range locs {
		if strings.Contains(loc.URI, sub) {
			return true
		}
	}
	return false
}

func hasRefSite(locs []Location, line, col int) bool {
	for _, loc := range locs {
		if loc.Range.Start.Line == line && loc.Range.Start.Character == col {
			return true
		}
	}
	return false
}

func TestDefinitionAndReferences(t *testing.T) {
	t.Run("macro name", func(t *testing.T) {
		trig := "scripted_trigger my_name = { always = yes }\n"
		call := "ns.1 = {\n\tmy_name = yes\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"common/scripted_triggers/x.txt": trig, "events/e.txt": call})
		tf := filepath.Join(root, "common", "scripted_triggers", "x.txt")
		ef := filepath.Join(root, "events", "e.txt")
		for _, needle := range []string{"scripted_trigger", "my_name"} {
			line, col := lineCol(trig, needle)
			locs := Definition(s, tf, line, col)
			if len(locs) == 0 || locs[0].TargetRange == nil || locs[0].TargetRange.End.Line < locs[0].Range.Start.Line {
				t.Fatalf("F12 on %s: %#v", needle, locs)
			}
		}
		line, col := lineCol(call, "my_name")
		if locs := Definition(s, ef, line, col); len(locs) == 0 || !index.SamePath(locs[0].URI, tf) {
			t.Fatalf("F12 from call=%v want %s", locs, tf)
		}
		if refs := References(s, tf, 0, strings.Index(trig, "my_name")); !hasLocURI(refs, ef) {
			t.Fatalf("refs=%v want call site %s", refs, ef)
		}
		inline := "scripted_trigger inline_trig = { always = yes }\nns.1 = { inline_trig = yes }\n"
		s2, root2 := buildSession(t, "ck3", map[string]string{"events/birth.txt": inline})
		bf := filepath.Join(root2, "events", "birth.txt")
		for _, needle := range []string{"scripted_trigger", "inline_trig"} {
			line, col := lineCol(inline, needle)
			if locs := Definition(s2, bf, line, col); len(locs) == 0 {
				t.Fatalf("events-file F12 on %s: empty", needle)
			}
		}
		plain := "my_trig = { always = yes }\n"
		s3, root3 := buildSession(t, "ck3", map[string]string{"common/scripted_triggers/t.txt": plain})
		line, col = lineCol(plain, "my_trig")
		if locs := Definition(s3, filepath.Join(root3, "common", "scripted_triggers", "t.txt"), line, col); len(locs) == 0 || locs[0].TargetRange == nil {
			t.Fatalf("folder-form F12: %#v", locs)
		}
	})

	t.Run("dotted event", func(t *testing.T) {
		src := "ns.3002 = { type = character_event }\nns.1 = {\n\ttrigger_event = ns.3002\n}\n"
		s, f := ck3Sess(t, src)
		line, col := lineCol(src, "trigger_event = ns.3002")
		col += len("trigger_event = ")
		if locs := Definition(s, f, line, col); len(locs) == 0 {
			t.Fatal("Definition on ns of ns.3002")
		}
		if locs := Definition(s, f, line, col+len("ns.")); len(locs) == 0 {
			t.Fatal("Definition on 3002 of ns.3002")
		}
	})

	t.Run("loc file references", func(t *testing.T) {
		locBody := "l_english:\n used_key:0 \"Hello\"\n other_key:0 \"see $used_key$\"\n only_loc:0 \"x\"\n ref_loc:0 \"see $only_loc|U$\"\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"events/x.txt":                         "ns.1 = {\n\ttitle = used_key\n}\n",
			"localization/english/a_l_english.yml": locBody,
		})
		locFile := filepath.Join(root, "localization", "english", "a_l_english.yml")
		line, col := lineCol(locBody, "used_key")
		if !hasURI(References(s, locFile, line, col), "events") {
			t.Fatal("loc key refs miss the script site")
		}
		for _, c := range []int{col, col + len("used_key")} {
			if len(Definition(s, locFile, line, c)) == 0 || len(References(s, locFile, line, c)) == 0 {
				t.Fatalf("loc-file Definition/References at col %d", c)
			}
		}
		line, col = lineCol(locBody, "Hello")
		if locs := References(s, locFile, line, col); len(locs) != 0 {
			t.Fatalf("value should not steal key refs: %v", locs)
		}
		line, col = lineCol(locBody, "$used_key$")
		if len(References(s, locFile, line, col+1)) == 0 || len(Definition(s, locFile, line, col+1)) == 0 {
			t.Fatal("$used_key$ should resolve as a reference")
		}
		line, col = lineCol(locBody, "only_loc")
		interpLine, interpCol := lineCol(locBody, "$only_loc|U$")
		if !hasRefSite(References(s, locFile, line, col), interpLine, interpCol+1) {
			t.Fatal("loc-only key should find $only_loc$ reuse")
		}
		bomBody := string(rune(0xFEFF)) + locBody
		s.DidOpen(locFile, bomBody)
		line, col = lineCol(bomBody, "used_key")
		if len(References(s, locFile, line, col)) == 0 || len(Definition(s, locFile, line, col+len("used_key"))) == 0 {
			t.Fatal("BOM loc buffer should still resolve")
		}
		if interpLine, interpCol := lineCol(bomBody, "$only_loc|U$"); len(References(s, locFile, interpLine, interpCol)) == 0 {
			t.Fatal("cursor on $ of $only_loc|U$ should resolve")
		}
	})

	t.Run("trigger localization references", func(t *testing.T) {
		locBody := "l_english:\n IS_ADULT_TRIGGER:0 \"Is an adult\"\n"
		trig := "is_adult = {\n\tglobal = IS_ADULT_TRIGGER\n\tfirst = I_AM_ADULT_TRIGGER\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/trigger_localization/x.txt":    trig,
			"localization/english/a_l_english.yml": locBody,
		})
		locFile := filepath.Join(root, "localization", "english", "a_l_english.yml")
		line, col := lineCol(locBody, "IS_ADULT_TRIGGER")
		if locs := References(s, locFile, line, col); !hasURI(locs, "trigger_localization") {
			t.Fatalf("trigger loc key refs=%v", locs)
		}
		tLine, tCol := lineCol(trig, "IS_ADULT_TRIGGER")
		if locs := Definition(s, filepath.Join(root, "common", "trigger_localization", "x.txt"), tLine, tCol); !hasURI(locs, "a_l_english.yml") {
			t.Fatalf("script-side Definition=%v", locs)
		}
	})

	t.Run("script names navigate", func(t *testing.T) {
		a := "test.1 = {\n\timmediate = {\n\t\tset_variable = { name = foo value = 3 }\n\t\tsave_scope_as = duel_target\n\t}\n}\n"
		b := "test.2 = {\n\timmediate = {\n\t\thas_variable = foo\n\t\texists = scope:duel_target\n\t\texists = var:foo\n\t\texists = flag:used_life\n\t}\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"events/a.txt": a, "events/b.txt": b})
		fa, fb := filepath.Join(root, "events", "a.txt"), filepath.Join(root, "events", "b.txt")
		line, col := lineCol(b, "var:foo")
		if locs := Definition(s, fb, line, col+len("var:")); len(locs) != 1 {
			t.Fatalf("var: F12=%v", locs)
		}
		line, col = lineCol(b, "has_variable = ")
		if items := Complete(s, fb, line, col+len("has_variable = ")).Items; !hasComplete(items, "foo") {
			t.Fatalf("complete has_variable: %v", labels(items))
		}
		line, col = lineCol(b, "scope:duel_target")
		if locs := Definition(s, fb, line, col+len("scope:")); len(locs) != 1 || !index.SamePath(locs[0].URI, fa) {
			t.Fatalf("scope F12=%v", locs)
		}
	})

	t.Run("typed prefix navigate", func(t *testing.T) {
		body := "e.1 = {\n\timmediate = {\n\t\tculture = english\n\t\tculture = culture:english\n\t\texists = scope:foo\n\t}\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body})
		f := filepath.Join(root, "events", "x.txt")
		line, col := lineCol(body, "culture = english\n")
		if locs := Definition(s, f, line, col+len("culture = ")); len(locs) != 1 || !hasURI(locs, "v_cultures.txt") {
			t.Fatalf("bare culture F12=%v", locs)
		}
		line, col = lineCol(body, "culture:english")
		if locs := Definition(s, f, line, col+len("culture:")); len(locs) != 1 || !hasURI(locs, "v_cultures.txt") {
			t.Fatalf("typed culture F12=%v", locs)
		}
		line, col = lineCol(body, "scope:foo")
		if locs := Definition(s, f, line, col+len("scope:")); len(locs) != 0 {
			t.Fatalf("scope:foo must stay ephemeral, F12=%v", locs)
		}
	})

	// A nested title is a title wherever it sits, and hovers as one rather than as a loc key.
	t.Run("typed title prefix navigate", func(t *testing.T) {
		body := "e.1 = { immediate = { exists = title:c_y } }\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"events/x.txt":                         body,
			"common/landed_titles/t.txt":           "e_y = { k_y = { c_y = { color = { 1 2 3 } } } }\n",
			"localization/english/a_l_english.yml": "l_english:\n c_y:0 \"County Y\"\n",
		})
		f := filepath.Join(root, "events", "x.txt")
		line, col := lineCol(body, "title:c_y")
		if locs := Definition(s, f, line, col+len("title:")); !hasURI(locs, "landed_titles") {
			t.Fatalf("title:c_y F12=%v", locs)
		}
		if h := Hover(s, f, line, col+len("title:")); h == nil || h.Kind == "loc key" || h.Key != "c_y" {
			t.Fatalf("title hover=%#v", h)
		}
	})

	// A character is a data row keyed by number; F12 reaches the history file and the history file's references reach the cite.
	t.Run("numeric character cite", func(t *testing.T) {
		body := "e.1 = {\n\timmediate = {\n\t\tcharacter:186004 = { save_scope_as = x }\n\t}\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body})
		f := filepath.Join(root, "events", "x.txt")
		line, col := lineCol(body, "character:186004")
		locs := Definition(s, f, line, col+len("character:"))
		if len(locs) != 1 || !hasURI(locs, "v_characters.txt") {
			t.Fatalf("character F12=%v", locs)
		}
		if refs := References(s, locs[0].URI, locs[0].Range.Start.Line, locs[0].Range.Start.Character); !hasLocURI(refs, f) {
			t.Fatalf("character refs=%v want %s", refs, f)
		}
	})

	t.Run("script param F12 and owner", func(t *testing.T) {
		a := "trig_a = { exists = $TARGET$ }\n"
		call := "ev.1 = { trig_a = { TARGET = title:k_france.holder } }\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/a.txt": a,
			"common/scripted_triggers/b.txt": "trig_b = { exists = $TARGET$ }\n",
			"events/e.txt":                   call,
		})
		af := filepath.Join(root, "common", "scripted_triggers", "a.txt")
		line, col := lineCol(a, "$TARGET$")
		if locs := Definition(s, af, line, col+1); len(locs) != 1 {
			t.Fatalf("def F12=%v", locs)
		}
		line, col = lineCol(call, "TARGET =")
		if locs := Definition(s, filepath.Join(root, "events", "e.txt"), line, col); len(locs) != 1 || !index.SamePath(locs[0].URI, af) {
			t.Fatalf("owner F12=%v want %s", locs, af)
		}
		dotted := "my_trig = {\n\t$TARGET$.holder = { is_ai = yes }\n}\n"
		s2, root2 := buildSession(t, "ck3", map[string]string{"common/scripted_triggers/t.txt": dotted})
		line, col = lineCol(dotted, "$TARGET$")
		if locs := Definition(s2, filepath.Join(root2, "common", "scripted_triggers", "t.txt"), line, col+1); len(locs) == 0 {
			t.Fatalf("dotted $TARGET$ F12 empty")
		}
	})

	// A macro call in the install resolves to the install's own body, live in the editor and from the cache alike.
	t.Run("vanilla trigger call refs", func(t *testing.T) {
		trig := "is_wrong_gender_in_faith_trigger = { always = yes }\n"
		call := "mpo.1 = {\n\ttrigger = {\n\t\tis_wrong_gender_in_faith_trigger = { FAITH = root.faith }\n\t}\n}\n"
		key := "is_wrong_gender_in_faith_trigger"
		install := t.TempDir()
		tf := write(t, install, "game/common/scripted_triggers/00_religious_triggers.txt", trig)
		ef := write(t, install, "game/events/mpo.txt", call)
		cache := scanInstall(t, install)
		s := session.New("ws", "ck3", "english", cache, nil)
		line, col := lineCol(trig, key)
		if refs := References(s, tf, line, col); !hasLocURI(refs, ef) {
			t.Fatalf("persisted refs=%v want %s", refs, ef)
		}
		s.DidOpen(tf, trig)
		s.DidOpen(ef, call)
		if refs := References(s, tf, line, col); !hasLocURI(refs, ef) {
			t.Fatalf("live refs=%v want %s", refs, ef)
		}
		line, col = lineCol(call, key)
		if locs := Definition(s, ef, line, col); !hasLocURI(locs, tf) {
			t.Fatalf("F12=%v want %s", locs, tf)
		}
	})

	t.Run("script value ref", func(t *testing.T) {
		use := "cb = { value = hre_conquest_ai_score_value }\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/script_values/v.txt":      "hre_conquest_ai_score_value = { value = 100 }\n",
			"common/casus_belli_types/cb.txt": use,
		})
		line, col := lineCol(use, "hre_conquest_ai_score_value")
		if locs := Definition(s, filepath.Join(root, "common", "casus_belli_types", "cb.txt"), line, col); len(locs) != 1 || !hasURI(locs, "script_values") {
			t.Fatalf("F12=%v", locs)
		}
	})

	// One location per physical site, whatever spelling each source used for the path.
	t.Run("no duplicate sites", func(t *testing.T) {
		trig := "scripted_trigger my_name = { always = yes }\n"
		call := "ns.1 = {\n\tmy_name = yes\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"common/scripted_triggers/x.txt": trig, "events/e.txt": call})
		for _, c := range []struct{ file, src string }{
			{filepath.Join(root, "events", "e.txt"), call},
			{filepath.Join(root, "common", "scripted_triggers", "x.txt"), trig},
		} {
			line, col := lineCol(c.src, "my_name")
			seen := map[string]bool{}
			for _, l := range Definition(s, c.file, line, col) {
				k := index.CanonPath(l.URI) + ":" + strconv.Itoa(l.Range.Start.Line) + ":" + strconv.Itoa(l.Range.Start.Character)
				if seen[k] {
					t.Errorf("F12 in %s returned %s twice", filepath.Base(c.file), k)
				}
				seen[k] = true
			}
		}
	})

	// F12 on a call offers only the inline body this file can reach.
	t.Run("confines inline declarations", func(t *testing.T) {
		const decl = "scripted_trigger allow_naming_trigger = {\n\tis_ai = no\n}\n"
		body := "namespace = birth\n\n" + decl + "\nbirth.1 = {\n\ttrigger = {\n\t\tallow_naming_trigger = yes\n\t}\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"events/birth_events.txt":   body,
			"events/quieter_events.txt": "namespace = qe\n\n" + decl,
		})
		path := filepath.Join(root, "events", "birth_events.txt")
		line, col := lineCol(body, "allow_naming_trigger = yes")
		locs := Definition(s, path, line, col+3)
		if len(locs) == 0 {
			t.Fatalf("no definition for the inline trigger")
		}
		for _, l := range locs {
			if !index.SamePath(l.URI, path) {
				t.Errorf("jumped to %s; an inline declaration in another file is not visible here", l.URI)
			}
		}
	})
}

// scanInstall scans a temp install laid out like the game's.
func scanInstall(t *testing.T, install string) *index.Index {
	t.Helper()
	x, _, err := index.Scan(t.Context(), index.ScanRequest{GameID: "ck3", InstallPath: install, LocLang: "english", Declared: fixture(t, "ck3").Declared})
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// TestSymbols covers the document and workspace outlines: a definition per name, ephemeral kinds skipped.
func TestSymbols(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"events/a.txt": "test.1 = {\n\timmediate = {\n\t\tset_variable = { name = foo value = 3 }\n\t}\n}\n",
		"events/b.txt": "test.2 = { }\n",
	})
	if syms := DocumentSymbols(s, filepath.Join(root, "events", "a.txt")); len(syms) != 1 || syms[0].Name != "test.1" {
		t.Fatalf("outline must skip the harvested variable: %v", syms)
	}
	syms := WorkspaceSymbols(s, "test.")
	names := map[string]bool{}
	for _, sym := range syms {
		names[sym.Name] = true
	}
	if len(syms) != 2 || !names["test.1"] || !names["test.2"] {
		t.Fatalf("workspace symbols = %v, want test.1 and test.2", syms)
	}
}
