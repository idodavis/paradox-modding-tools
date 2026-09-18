// definition_test.go covers go-to-definition, references, and symbols.
package lsp

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/derived"
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
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/x.txt": trig,
			"events/e.txt":                   call,
		}, nil, nil)
		tf := filepath.Join(root, "common", "scripted_triggers", "x.txt")
		ef := filepath.Join(root, "events", "e.txt")
		for _, needle := range []string{"scripted_trigger", "my_name"} {
			line, col := lineCol(trig, needle)
			locs := Definition(s, tf, line, col)
			if len(locs) == 0 {
				t.Fatalf("F12 on %s: empty", needle)
			}
			if locs[0].TargetRange == nil ||
				locs[0].TargetRange.End.Line < locs[0].Range.Start.Line {
				t.Fatalf("F12 on %s: want block targetRange, got %#v", needle, locs[0])
			}
		}
		line, col := lineCol(call, "my_name")
		locs := Definition(s, ef, line, col)
		if len(locs) == 0 {
			t.Fatalf("F12 from call: empty")
		}
		if !derived.SamePath(locs[0].URI, tf) {
			t.Fatalf("F12 from call=%v want %s", locs, tf)
		}
		refs := References(s, tf, 0, strings.Index(trig, "my_name"))
		if !hasLocURI(refs, ef) {
			t.Fatalf("refs=%v want call site %s", refs, ef)
		}

		inline := "scripted_trigger inline_trig = { always = yes }\nns.1 = { inline_trig = yes }\n"
		s2, root2 := buildSession(t, "ck3", map[string]string{
			"events/birth.txt": inline,
		}, nil, nil)
		bf := filepath.Join(root2, "events", "birth.txt")
		for _, needle := range []string{"scripted_trigger", "inline_trig"} {
			line, col := lineCol(inline, needle)
			if locs := Definition(s2, bf, line, col); len(locs) == 0 {
				t.Fatalf("events-file F12 on %s: empty", needle)
			}
		}

		plain := "my_trig = { always = yes }\n"
		s3, root3 := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/t.txt": plain,
		}, nil, nil)
		pf := filepath.Join(root3, "common", "scripted_triggers", "t.txt")
		line, col = lineCol(plain, "my_trig")
		locs = Definition(s3, pf, line, col)
		if len(locs) == 0 {
			t.Fatalf("folder-form F12 on my_trig: empty")
		}
		if locs[0].TargetRange == nil {
			t.Fatalf("folder-form F12: want targetRange, got %#v", locs[0])
		}
	})

	t.Run("dotted event", func(t *testing.T) {
		src := "ns.3002 = { type = character_event }\n" +
			"ns.1 = {\n\ttrigger_event = ns.3002\n}\n"
		s, f := ck3Sess(t, src)
		line, col := lineCol(src, "trigger_event = ns.3002")
		col += len("trigger_event = ")
		locs := Definition(s, f, line, col)
		if len(locs) == 0 {
			t.Fatal("Definition on ns of ns.3002")
		}
		col += len("ns.")
		if locs := Definition(s, f, line, col); len(locs) == 0 {
			t.Fatal("Definition on 3002 of ns.3002")
		}
	})

	t.Run("loc file references", func(t *testing.T) {
		locBody := "l_english:\n used_key:0 \"Hello\"\n other_key:0 \"see $used_key$\"\n" +
			" only_loc:0 \"x\"\n ref_loc:0 \"see $only_loc|U$\"\n"
		script := "ns.1 = {\n\ttitle = used_key\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"events/x.txt":                         script,
			"localization/english/a_l_english.yml": locBody,
		}, nil, nil)
		locFile := filepath.Join(root, "localization", "english", "a_l_english.yml")
		line, col := lineCol(locBody, "used_key")
		locs := References(s, locFile, line, col)
		found := false
		for _, loc := range locs {
			if strings.Contains(loc.URI, "events") {
				found = true
			}
		}
		if !found {
			t.Fatalf("loc key refs=%v", locs)
		}
		if locs := Definition(s, locFile, line, col); len(locs) == 0 {
			t.Fatal("loc-file Definition at key")
		}
		endCol := col + len("used_key")
		if locs := References(s, locFile, line, endCol); len(locs) == 0 {
			t.Fatal("cursor at end of loc key should still find usages")
		}
		if locs := Definition(s, locFile, line, endCol); len(locs) == 0 {
			t.Fatal("loc-file Definition at end of key")
		}
		line, col = lineCol(locBody, "Hello")
		if locs := References(s, locFile, line, col); len(locs) != 0 {
			t.Fatalf("value should not steal key refs: %v", locs)
		}
		line, col = lineCol(locBody, "$used_key$")
		col++ // inner key
		if locs := References(s, locFile, line, col); len(locs) == 0 {
			t.Fatal("$used_key$ should resolve as a reference")
		}
		if locs := Definition(s, locFile, line, col); len(locs) == 0 {
			t.Fatal("$used_key$ Definition")
		}
		line, col = lineCol(locBody, "only_loc")
		interpLine, interpCol := lineCol(locBody, "$only_loc|U$")
		if !hasRefSite(References(s, locFile, line, col), interpLine, interpCol+1) {
			t.Fatal("loc-only key should find $only_loc$ reuse")
		}

		bomBody := string(rune(0xFEFF)) + locBody
		s.DidOpen(locFile, bomBody)
		line, col = lineCol(bomBody, "used_key")
		if locs := References(s, locFile, line, col); len(locs) == 0 {
			t.Fatal("BOM loc buffer should still find usages")
		}
		if locs := Definition(s, locFile, line, col); len(locs) == 0 {
			t.Fatal("loc-file Definition at key")
		}
		endCol = col + len("used_key")
		if locs := Definition(s, locFile, line, endCol); len(locs) == 0 {
			t.Fatal("loc-file Definition at end of key")
		}
		line, col = lineCol(bomBody, "only_loc")
		interpLine, interpCol = lineCol(bomBody, "$only_loc|U$")
		if !hasRefSite(References(s, locFile, line, col), interpLine, interpCol+1) {
			t.Fatal("BOM loc-only key should keep $only_loc$ reuse")
		}
		if locs := References(s, locFile, interpLine, interpCol); len(locs) == 0 {
			t.Fatal("cursor on $ of $only_loc|U$ should resolve")
		}
	})

	t.Run("trigger localization references", func(t *testing.T) {
		locBody := "l_english:\n IS_ADULT_TRIGGER:0 \"Is an adult\"\n"
		trig := "is_adult = {\n\tglobal = IS_ADULT_TRIGGER\n\tfirst = I_AM_ADULT_TRIGGER\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/trigger_localization/x.txt":    trig,
			"localization/english/a_l_english.yml": locBody,
		}, nil, nil)
		locFile := filepath.Join(root, "localization", "english", "a_l_english.yml")
		line, col := lineCol(locBody, "IS_ADULT_TRIGGER")
		locs := References(s, locFile, line, col)
		if !hasURI(locs, "trigger_localization") {
			t.Fatalf("trigger loc key refs=%v", locs)
		}
		trigFile := filepath.Join(root, "common", "trigger_localization", "x.txt")
		tLine, tCol := lineCol(trig, "IS_ADULT_TRIGGER")
		if locs := Definition(s, trigFile, tLine, tCol); !hasURI(locs, "a_l_english.yml") {
			t.Fatalf("script-side Definition=%v", locs)
		}
	})

	t.Run("script names navigate", func(t *testing.T) {
		a := `test.1 = {
	immediate = {
		set_variable = { name = foo value = 3 }
		save_scope_as = duel_target
	}
}
`
		b := `test.2 = {
	immediate = {
		has_variable = foo
		exists = scope:duel_target
		exists = var:foo
		exists = flag:used_life
	}
}
`
		s, root := buildSession(t, "ck3", map[string]string{
			"events/a.txt": a,
			"events/b.txt": b,
		}, nil, nil)
		fb := filepath.Join(root, "events", "b.txt")

		t.Run("variable", func(t *testing.T) {
			line, col := lineCol(b, "var:foo")
			if locs := Definition(s, fb, line, col+len("var:")); len(locs) != 1 {
				t.Fatalf("var: F12=%v", locs)
			}
			line, col = lineCol(b, "has_variable = ")
			items := Complete(s, fb, line, col+len("has_variable = ")).Items
			if !hasComplete(items, "foo") {
				t.Fatalf("complete has_variable: %v", labels(items))
			}
		})
		t.Run("saved scope cross-file", func(t *testing.T) {
			fa := filepath.Join(root, "events", "a.txt")
			line, col := lineCol(b, "scope:duel_target")
			if locs := Definition(s, fb, line, col+len("scope:")); len(locs) != 1 ||
				!derived.SamePath(locs[0].URI, fa) {
				t.Fatalf("scope F12=%v", locs)
			}
		})
	})

	t.Run("typed prefix navigate", func(t *testing.T) {
		body := `e.1 = {
	immediate = {
		culture = english
		culture = culture:english
		exists = scope:foo
	}
}
`
		s, root := buildSession(t, "ck3", map[string]string{
			"events/x.txt":                  body,
			"common/culture/cultures/c.txt": "english = { }\n",
		}, nil, nil)
		f := filepath.Join(root, "events", "x.txt")
		cultures := filepath.Join(root, "common", "culture", "cultures", "c.txt")

		line, col := lineCol(body, "culture = english\n")
		valCol := col + len("culture = ")
		if locs := Definition(s, f, line, valCol); len(locs) != 1 ||
			!derived.SamePath(locs[0].URI, cultures) {
			t.Fatalf("bare culture F12=%v want %s", locs, cultures)
		}

		line, col = lineCol(body, "culture:english")
		if locs := Definition(s, f, line, col+len("culture:")); len(locs) != 1 ||
			!derived.SamePath(locs[0].URI, cultures) {
			t.Fatalf("typed culture F12=%v want %s", locs, cultures)
		}

		line, col = lineCol(body, "scope:foo")
		if locs := Definition(s, f, line, col+len("scope:")); len(locs) != 0 {
			t.Fatalf("scope:foo must stay ephemeral, F12=%v", locs)
		}
	})

	t.Run("typed title prefix navigate", func(t *testing.T) {
		body := "e.1 = { immediate = { exists = titles:c_x } }\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"events/x.txt":                         body,
			"common/landed_titles/t.txt":           "e_x = { exists = title:k_x k_x = { c_x = { exists = title:c_x } } }\n",
			"localization/english/a_l_english.yml": "l_english:\n c_x:0 \"County X\"\n",
		}, ck3Typed(), nil)
		f := filepath.Join(root, "events", "x.txt")
		titles := filepath.Join(root, "common", "landed_titles", "t.txt")
		line, col := lineCol(body, "titles:c_x")
		locs := Definition(s, f, line, col+len("titles:"))
		found := false
		for _, loc := range locs {
			if derived.SamePath(loc.URI, titles) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("titles:c_x F12=%v want %s", locs, titles)
		}
		// The same position hovers as the title it names, not a loc key.
		h := Hover(s, f, line, col+len("titles:"))
		if h == nil || h.Kind == "loc key" || h.Key != "c_x" {
			t.Fatalf("title hover=%#v", h)
		}
	})

	t.Run("script param F12", func(t *testing.T) {
		trig := "my_trig = { exists = $TARGET$ }\n"
		call := "ev.1 = { my_trig = { TARGET = title:k_france.holder } }\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/t.txt": trig,
			"events/e.txt":                   call,
		}, nil, nil)
		tf := filepath.Join(root, "common", "scripted_triggers", "t.txt")
		line, col := lineCol(trig, "$TARGET$")
		if locs := Definition(s, tf, line, col+1); len(locs) != 1 {
			t.Fatalf("def F12=%v", locs)
		}
		ef := filepath.Join(root, "events", "e.txt")
		line, col = lineCol(call, "TARGET =")
		if locs := Definition(s, ef, line, col); len(locs) < 1 {
			t.Fatalf("call F12=%v", locs)
		}
	})

	t.Run("script param owner scoped", func(t *testing.T) {
		a := "trig_a = { exists = $TARGET$ }\n"
		b := "trig_b = { exists = $TARGET$ }\n"
		call := "ev.1 = { trig_a = { TARGET = title:k_france.holder } }\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/a.txt": a,
			"common/scripted_triggers/b.txt": b,
			"events/e.txt":                   call,
		}, nil, nil)
		af := filepath.Join(root, "common", "scripted_triggers", "a.txt")
		bf := filepath.Join(root, "common", "scripted_triggers", "b.txt")
		ef := filepath.Join(root, "events", "e.txt")
		line, col := lineCol(call, "TARGET =")
		locs := Definition(s, ef, line, col)
		if len(locs) != 1 || !derived.SamePath(locs[0].URI, af) {
			t.Fatalf("owner F12=%v want %s not %s", locs, af, bf)
		}
	})

	t.Run("script param multi value", func(t *testing.T) {
		trig := "my_trig = { exists = $TARGET$ }\n"
		call := "ev.1 = {\n\tmy_trig = { TARGET = title:c_rouen }\n" +
			"\tmy_trig = { TARGET = scope:adventurer_target }\n" +
			"\tmy_trig = { TARGET = scope:adventurer_target }\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/t.txt": trig,
			"events/e.txt":                   call,
		}, nil, nil)
		tf := filepath.Join(root, "common", "scripted_triggers", "t.txt")
		line, col := lineCol(trig, "$TARGET$")
		if locs := Definition(s, tf, line, col+1); len(locs) != 1 {
			t.Fatalf("multi-value def F12=%v", locs)
		}
	})

	t.Run("dotted script param F12", func(t *testing.T) {
		trig := "my_trig = {\n\t$TARGET$.holder = { is_ai = yes }\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/t.txt": trig,
		}, nil, nil)
		tf := filepath.Join(root, "common", "scripted_triggers", "t.txt")
		line, col := lineCol(trig, "$TARGET$")
		if locs := Definition(s, tf, line, col+1); len(locs) == 0 {
			t.Fatalf("dotted $TARGET$ F12 empty")
		}
	})

	t.Run("vanilla trigger call refs", func(t *testing.T) {
		trig := "is_wrong_gender_in_faith_trigger = { always = yes }\n"
		call := "mpo_events_ariana.0100 = {\n\ttrigger = {\n" +
			"\t\tis_wrong_gender_in_faith_trigger = { FAITH = root.faith }\n\t}\n}\n"
		trigRel := "game/common/scripted_triggers/00_religious_triggers.txt"
		callRel := "game/events/dlc/mpo/mpo_events_ariana.txt"
		key := "is_wrong_gender_in_faith_trigger"
		t.Run("live open", func(t *testing.T) {
			install := t.TempDir()
			tf := write(t, install, trigRel, trig)
			ef := write(t, install, callRel, call)
			s := ck3VanillaSess(t, install, &vanillaCache{
				Defs: []derived.Def{{
					Kind: "scripted_trigger", Key: key, Path: tf,
					Start: 0, End: len(key),
				}},
			})
			s.DidOpen(tf, trig)
			s.DidOpen(ef, call)
			line, col := lineCol(trig, key)
			if refs := References(s, tf, line, col); !hasLocURI(refs, ef) {
				t.Fatalf("refs=%v want %s", refs, ef)
			}
			line, col = lineCol(call, key)
			if locs := Definition(s, ef, line, col); !hasLocURI(locs, tf) {
				t.Fatalf("F12=%v want %s", locs, tf)
			}
		})
		t.Run("persisted call refs", func(t *testing.T) {
			install := t.TempDir()
			tf := write(t, install, trigRel, trig)
			ef := write(t, install, callRel, call)
			i := strings.Index(call, key)
			s := ck3VanillaSess(t, install, &vanillaCache{
				Defs: []derived.Def{{
					Kind: "scripted_trigger", Key: key, Path: tf,
					Start: 0, End: len(key),
				}},
				CallRefs: []derived.Ref{{
					Key: key, Kind: "scripted_trigger", Path: ef,
					Line:  strings.Count(call[:i], "\n"),
					Start: i, End: i + len(key),
				}},
			})
			s.DidOpen(tf, trig)
			line, col := lineCol(trig, key)
			if refs := References(s, tf, line, col); !hasLocURI(refs, ef) {
				t.Fatalf("refs=%v want persisted %s", refs, ef)
			}
		})
	})

	t.Run("script value ref", func(t *testing.T) {
		vals := "hre_conquest_ai_score_value = { value = 100 }\n"
		use := "cb = { value = hre_conquest_ai_score_value }\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/script_values/v.txt":      vals,
			"common/casus_belli_types/cb.txt": use,
		}, nil, nil)
		vf := filepath.Join(root, "common", "script_values", "v.txt")
		uf := filepath.Join(root, "common", "casus_belli_types", "cb.txt")
		line, col := lineCol(use, "hre_conquest_ai_score_value")
		if locs := Definition(s, uf, line, col); len(locs) != 1 ||
			!derived.SamePath(locs[0].URI, vf) {
			t.Fatalf("F12=%v want %s", locs, vf)
		}
	})

	// Go-to-definition must return one location per physical site. The same
	// definition reaches siteLocations from several sources, and when they spell
	// the path differently — which happens on Windows, where CanonPath exists
	// precisely because case varies — an undeduped duplicate makes the editor open
	// a peek listing the file you are already in, instead of jumping.
	t.Run("no duplicate sites", func(t *testing.T) {
		trig := "scripted_trigger my_name = { always = yes }\n"
		call := "ns.1 = {\n\tmy_name = yes\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/x.txt": trig,
			"events/e.txt":                   call,
		}, nil, nil)
		for _, c := range []struct {
			file, src, needle string
		}{
			{filepath.Join(root, "events", "e.txt"), call, "my_name"},
			{filepath.Join(root, "common", "scripted_triggers", "x.txt"), trig, "my_name"},
		} {
			line, col := lineCol(c.src, c.needle)
			seen := map[string]bool{}
			for _, l := range Definition(s, c.file, line, col) {
				k := derived.CanonPath(l.URI) + ":" +
					strconv.Itoa(l.Range.Start.Line) + ":" +
					strconv.Itoa(l.Range.Start.Character)
				if seen[k] {
					t.Errorf("F12 in %s returned %s twice", filepath.Base(c.file), k)
				}
				seen[k] = true
			}
		}
	})

	// F12 on a call must offer only the body this file can actually reach.
	// Listing the other file's copy made the editor open a peek with two
	// bodies, one of them invisible to the engine here.
	t.Run("confines inline declarations", func(t *testing.T) {
		const decl = "scripted_trigger allow_naming_trigger = {\n\tis_ai = no\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"events/birth_events.txt": "namespace = birth\n\n" + decl +
				"\nbirth.1 = {\n\ttrigger = {\n\t\tallow_naming_trigger = yes\n\t}\n}\n",
			"events/quieter_events.txt": "namespace = qe\n\n" + decl,
		}, nil, nil)

		path := root + "/events/birth_events.txt"
		src := s.FileText(path)
		line, col := -1, 0
		for i, l := range strings.Split(src, "\n") {
			if c := strings.Index(l, "allow_naming_trigger = yes"); c >= 0 {
				line, col = i, c+3
				break
			}
		}
		if line < 0 {
			t.Fatalf("call site not found in\n%s", src)
		}
		locs := Definition(s, path, line, col)
		if len(locs) == 0 {
			t.Fatalf("no definition for the inline trigger")
		}
		for _, l := range locs {
			if derived.CanonPath(l.URI) != derived.CanonPath(path) {
				t.Errorf("jumped to %s; an inline declaration in another file is not visible here", l.URI)
			}
		}
	})
}

// TestSymbols covers the document and workspace outlines: a definition per
// name, ephemeral kinds skipped.
func TestSymbols(t *testing.T) {
	body := "test.1 = {\n\timmediate = {\n\t\tset_variable = { name = foo value = 3 }\n\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"events/a.txt": body,
		"events/b.txt": "test.2 = { }\n",
	}, nil, nil)
	fa := filepath.Join(root, "events", "a.txt")

	t.Run("document symbols skip ephemeral kinds", func(t *testing.T) {
		syms := DocumentSymbols(s, fa)
		if len(syms) != 1 || syms[0].Name != "test.1" {
			t.Fatalf("outline must skip the harvested variable: %v", syms)
		}
	})

	t.Run("workspace symbols search by key", func(t *testing.T) {
		syms := WorkspaceSymbols(s, "test.")
		if len(syms) != 2 {
			t.Fatalf("workspace symbols = %v, want test.1 and test.2", syms)
		}
		for _, want := range []string{"test.1", "test.2"} {
			found := false
			for _, sym := range syms {
				if sym.Name == want {
					found = true
				}
			}
			if !found {
				t.Errorf("missing %s in %v", want, syms)
			}
		}
	})
}
