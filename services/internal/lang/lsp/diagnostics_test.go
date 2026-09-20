// diagnostics_test.go is one table test over Diagnose: one row per kept code,
// plus a subtest per rule with nuance a single row cannot carry.

package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

func codesOf(ds []Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Code
	}
	return out
}

func TestDiagnose(t *testing.T) {
	t.Run("pmt:ignore suppression", func(t *testing.T) {
		sup := scanSuppressions(jomini.Parse("# pmt:ignore-next-line unclosed-brace\nbad = {\nok = 1 # pmt:ignore\n").Src)
		if !sup.Covers(1, "unclosed-brace") || sup.Covers(1, "other") || !sup.Covers(2, "anything") {
			t.Fatal(sup)
		}
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/t.txt": "a = {\n\t# pmt:ignore-next-line unresolved-ref\n\tx = culture:nonexistent\n}\n",
		})
		if hasDiag(Diagnose(s, root+"/common/scripted_triggers/t.txt"), "unresolved-ref") {
			t.Errorf("pmt:ignore-next-line did not suppress unresolved-ref")
		}
	})

	// One row per code that a single small file can trigger.
	for _, tt := range []struct{ name, game, rel, body, want string }{
		{"missing-value", "ck3", "common/traits/t.txt", "foo =\nbar = 1\n", "missing-value"},
		{"stray-close", "ck3", "common/traits/t.txt", "foo = { }\n}\n", "stray-close"},
		{"unclosed-brace", "ck3", "events/x.txt", "test.1 = {\n\ttitle = missing_key\n", "unclosed-brace"},
		{"unterminated-string", "ck3", "common/traits/t.txt", "name = \"hello\n", "unterminated-string"},
		{"loc-no-header", "ck3", "localization/english/a_l_english.yml", "key:0 \"value\"\n", "loc-no-header"},
		{"loc-content-before-header", "ck3", "localization/english/a_l_english.yml", "stray content\nl_english:\n key:0 \"v\"\n", "loc-content-before-header"},
		{"loc-tab-indent", "ck3", "localization/english/a_l_english.yml", "l_english:\n\tkey:0 \"v\"\n", "loc-tab-indent"},
		{"loc-unterminated-value", "ck3", "localization/english/a_l_english.yml", "l_english:\n key:0 \"unterminated\n", "loc-unterminated-value"},
		{"loc-bad-entry", "ck3", "localization/english/a_l_english.yml", "l_english:\n key no_colon\n", "loc-bad-entry"},
		{"loc-header-mismatch", "ck3", "localization/english/a_l_english.yml", "l_french:\n key:0 \"v\"\n", "loc-header-mismatch"},
		{"wrong-localization-folder", "ck3", "localization/localisation/a_l_english.yml", "l_english:\n key:0 \"v\"\n", "wrong-localization-folder"},
		{"missing-required-loc", "ck3", "events/x.txt", "test.1 = {\n\ttitle = missing_key\n}\n", "missing-required-loc"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, root := buildSession(t, tt.game, map[string]string{tt.rel: tt.body})
			if ds := Diagnose(s, filepath.Join(root, filepath.FromSlash(tt.rel))); !hasDiag(ds, tt.want) {
				t.Errorf("want %s, got %v", tt.want, codesOf(ds))
			}
		})
	}

	// A fire key the game declares reports an event nothing defines; a localization field never does. A kind whose every vanilla definition carries a key demands it.
	t.Run("unknown-event and required-loc", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{
			"events/x.txt":         "namespace = ns\nns.1 = {\n\ttitle = ns.1.t\n\tdesc = ns.1.desc\n\timmediate = { trigger_event = ns.99 }\n}\n",
			"common/traits/00.txt": "ambitious_mod = { category = personality }\n",
		})
		ds := Diagnose(s, root+"/events/x.txt")
		n := 0
		for _, d := range ds {
			if d.Code == "unknown-event" {
				n++
				if !strings.Contains(d.Message, "ns.99") {
					t.Errorf("unknown-event names %q", d.Message)
				}
			}
		}
		if n != 1 {
			t.Errorf("unknown-event = %d, want one for ns.99: %v", n, codesOf(ds))
		}
		if !hasDiag(Diagnose(s, root+"/common/traits/00.txt"), "required-loc") {
			t.Error("trait required-loc missing")
		}
	})

	// The templates the kind's own definitions carry are the whole rule.
	t.Run("required-loc reads the convention", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{
			"common/decisions/d.txt":               "foo = {\n\tselection_tooltip = foo_tooltip\n}\n",
			"localization/english/a_l_english.yml": "l_english:\n foo:0 \"F\"\n foo_desc:0 \"D\"\n foo_tooltip:0 \"T\"\n",
		})
		if hasDiag(Diagnose(s, root+"/common/decisions/d.txt"), "required-loc") {
			t.Fatal("decision with its convention keys must not be required-loc")
		}
	})

	t.Run("no diagnostic message invents a convention", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{
			"common/messages/x.txt":                "quieter_events_neutral = {\n\ttitle = event_message_title\n}\n",
			"localization/english/a_l_english.yml": "l_english:\n event_message_title:0 \"T\"\n",
		})
		for _, d := range Diagnose(s, root+"/common/messages/x.txt") {
			if strings.Contains(d.Message, "quieter_events_neutral") {
				t.Fatalf("invented loc diag: %+v", d)
			}
		}
	})

	t.Run("missing-bom", func(t *testing.T) {
		const body = "l_english:\n k:0 \"v\"\n"
		rel := "localization/english/a_l_english.yml"
		s, root := buildSession(t, "ck3", map[string]string{rel: body})
		f := filepath.Join(root, filepath.FromSlash(rel))
		if !hasDiag(Diagnose(s, f), "missing-bom") {
			t.Fatal("want missing-bom when disk has no BOM")
		}
		if err := os.WriteFile(f, append([]byte{0xEF, 0xBB, 0xBF}, []byte(body)...), 0o644); err != nil {
			t.Fatal(err)
		}
		s.DidOpen(f, body)
		if hasDiag(Diagnose(s, f), "missing-bom") {
			t.Fatal("disk BOM must not warn")
		}
	})

	t.Run("vanilla files are silent, but the mod they overlay is not", func(t *testing.T) {
		install := t.TempDir()
		vEvent := write(t, install, "game/events/v.txt", "test.1 = {\n\ttitle = missing_key\n}\n")
		vLoc := write(t, install, "game/localization/english/v_l_english.yml", "l_english:\n k:0 \"v\"\n")
		s, root := sessionOver(t, "ck3", scanInstall(t, install), map[string]string{"events/m.txt": "mod.1 = {\n\ttitle = missing_key\n}\n"})
		s.DidOpen(vEvent, "test.1 = {\n\ttitle = missing_key\n}\n")
		s.DidOpen(vLoc, "l_english:\n k:0 \"v\"\n")
		if diags := Diagnose(s, vEvent); len(diags) != 0 {
			t.Fatalf("vanilla event diags=%v", diags)
		}
		if diags := Diagnose(s, vLoc); len(diags) != 0 {
			t.Fatalf("vanilla loc diags=%v", diags)
		}
		if !hasDiag(Diagnose(s, root+"/events/m.txt"), "missing-required-loc") {
			t.Fatal("mod file must still get missing-required-loc")
		}
		if len(s.ModDefs("test.1")) != 0 {
			t.Fatal("vanilla DidOpen must not harvest defs into the workspace")
		}
	})

	// A data tier that would not load is a model missing its rows, so it may not accuse one of being absent.
	t.Run("unresolved-ref is silent when the data tier failed to load", func(t *testing.T) {
		for _, tc := range []struct {
			name, install string
			wantDiag      bool
		}{{"tier reads", "tier-ok", true}, {"tier missing", "tier-gone", false}} {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", dir)
				t.Setenv("AppData", dir)
				// A loaded cache reads its tier from disk, which is where a tier goes missing; a fresh scan carries its own.
				scanned := scanFixture(t, "ck3")
				scanned.InstallID, scanned.GameVersion = tc.install, "1.0"
				var data *index.Index
				if tc.wantDiag {
					data = index.NewIndex("ck3", nil)
					data.InstallID, data.GameVersion = tc.install, "1.0"
				}
				if err := index.SaveCache(scanned, data); err != nil {
					t.Fatal(err)
				}
				cache, err := index.LoadCache(tc.install, "1.0")
				if err != nil {
					t.Fatal(err)
				}
				s, root := sessionOver(t, "ck3", cache, map[string]string{"common/scripted_triggers/t.txt": "a = { x = dynasty:nowhere }\n"})
				if got := hasDiag(Diagnose(s, root+"/common/scripted_triggers/t.txt"), "unresolved-ref"); got != tc.wantDiag {
					t.Errorf("unresolved-ref = %v, want %v (dataError=%q)", got, tc.wantDiag, s.Install.DataError())
				}
			})
		}
	})

	// unresolved-ref is the citation check: a value naming a database PMT holds and a key nothing defines.
	t.Run("unresolved-ref", func(t *testing.T) {
		for _, tt := range []struct {
			name, body, names string
			want              bool
		}{
			{"a citation nothing defines", "b = { x = culture:nonexistent }", "nonexistent", true},
			{"a citation that resolves", "a = { x = culture:english }", "", false},
			{"a numeric citation nothing defines", "b = { x = character:999999 }", "999999", true},
			{"a numeric citation that resolves", "a = { x = character:186004 }", "", false},
			// An unbound prefix means PMT does not know that database.
			{"a prefix bound to no database", "a = { x = province:9 }", "", false},
			// A value the script invents as it runs is not something anything defines.
			{"an ephemeral kind", "a = {\n\tset_variable = { name = where value = flag:private_chamber }\n}", "", false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s, root := buildSession(t, "ck3", map[string]string{"common/scripted_triggers/t.txt": tt.body + "\n"})
				var msgs []string
				for _, d := range Diagnose(s, root+"/common/scripted_triggers/t.txt") {
					if d.Code == "unresolved-ref" {
						msgs = append(msgs, d.Message)
					}
				}
				if got := len(msgs) > 0; got != tt.want {
					t.Fatalf("unresolved-ref = %v, want %v: %v", got, tt.want, msgs)
				}
				if tt.names != "" && !strings.Contains(msgs[0], tt.names) {
					t.Errorf("message = %q, want it to name %s", msgs[0], tt.names)
				}
			})
		}
	})

	// wrong-value-type uses the declared argument type only.
	t.Run("wrong-value-type", func(t *testing.T) {
		for _, tt := range []struct {
			name, body string
			bare, want bool
		}{
			{"a value of the wrong declared type", "b = { has_culture = not_a_culture }", false, true},
			{"a value of the right type", "a = { has_culture = english }", false, false},
			{"the same value with no schema", "a = { has_culture = not_a_culture }", true, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string]string{"common/scripted_triggers/t.txt": tt.body + "\n"}
				var s *session.Session
				var root string
				if tt.bare {
					s, root = bareSession(t, files)
				} else {
					s, root = buildSession(t, "ck3", files)
				}
				ds := Diagnose(s, root+"/common/scripted_triggers/t.txt")
				var msgs []string
				for _, d := range ds {
					if d.Code == "wrong-value-type" {
						msgs = append(msgs, d.Message)
					}
				}
				if got := len(msgs) > 0; got != tt.want {
					t.Fatalf("wrong-value-type = %v, want %v (all: %v)", got, tt.want, codesOf(ds))
				}
				if tt.want && !strings.Contains(msgs[0], "not_a_culture") {
					t.Errorf("message = %q, want it to name the offending value", msgs[0])
				}
			})
		}
	})

	// unknown-key is the one check that reports an absence: a key is judged against the structure or body learned at that position. Each row is one of the ways a key is known.
	t.Run("unknown-key", func(t *testing.T) {
		for _, tt := range []struct {
			name, key, line string
			want            bool
		}{
			{"a key the structure never holds", "not_a_real_key", "not_a_real_key = yes", true},
			{"a member of the unified structure", "desc", "desc = x", false},
			{"a declared trigger", "is_adult", "is_adult = yes", false},
			{"a scripted macro", "my_trigger", "my_trigger = yes", false},
			{"script grammar", "$param$", "$PARAM$ = yes", false},
			{"a scope path", "root.culture", "root.culture = x", false},
			{"a weight", "30", "30 = { desc = x }", false},
			{"a stop word", "nor", "NOR = { desc = x }", false},
			{"a scope word", "this", "this = yes", false},
			// A key the corpus opens a block with anywhere is engine grammar, whatever structure it is written in.
			{"a block key from elsewhere", "cooldown", "cooldown = { days = 1 }", false},
			// A real key in the wrong block is not this check's business: only a name written nowhere is.
			{"a field the corpus writes elsewhere", "message_filter_type", "message_filter_type = yes", false},
			{"a key written nowhere", "trigger_evnt", "trigger_evnt = yes", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s, root := buildSession(t, "ck3", map[string]string{
					"events/x.txt":                   "namespace = ns\nns.1 = {\n\ttriggered_desc = {\n\t\t" + tt.line + "\n\t}\n}\n",
					"common/scripted_triggers/t.txt": "scripted_trigger my_trigger = {\n\talways = yes\n}\n",
				})
				ds := Diagnose(s, root+"/events/x.txt")
				if got := unknownKeyOn(ds, tt.key); got != tt.want {
					t.Errorf("unknown-key on %s = %v, want %v (%v)", tt.key, got, tt.want, ds)
				}
			})
		}
		// The arguments of a call are the macro's parameters, not a structure's members.
		t.Run("a call is judged against its macro's parameters", func(t *testing.T) {
			s, root := buildSession(t, "ck3", map[string]string{
				"events/x.txt":                  "namespace = ns\nns.1 = {\n\timmediate = {\n\t\tmy_effect = {\n\t\t\tREASON = x\n\t\t\tNOPE = y\n\t\t}\n\t}\n}\n",
				"common/scripted_effects/e.txt": "scripted_effect my_effect = {\n\tadd_gold = $REASON$\n}\n",
			})
			ds := Diagnose(s, root+"/events/x.txt")
			found := false
			for _, d := range ds {
				if d.Code == "unknown-key" && strings.Contains(d.Message, "reason") {
					t.Errorf("a parameter the macro spells reported: %s", d.Message)
				}
				found = found || strings.Contains(d.Message, "nope is not a parameter of my_effect")
			}
			if !found {
				t.Errorf("an argument the macro never spells was not reported: %v", codesOf(ds))
			}
		})
		// A block the install never wrote accuses nobody.
		t.Run("silent in a mod's own block", func(t *testing.T) {
			s, root := buildSession(t, "ck3", map[string]string{"common/decisions/d.txt": "d = {\n\tunwritten_block = {\n\t\tanything = yes\n\t}\n}\n"})
			if unknownKeyOn(Diagnose(s, root+"/common/decisions/d.txt"), "anything") {
				t.Errorf("judged a key against a block only the mod wrote")
			}
		})
	})

	// wrong-scope is one row per rule the scope walk has to hold to, over the full Diagnose pipeline.
	t.Run("wrong-scope", func(t *testing.T) {
		const onAction = "common/on_action/x.txt"
		const event = "events/x.txt"
		for _, tt := range []struct {
			name, rel, body string
			bare, want      bool
		}{
			{"a character effect in a title block", onAction, "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_gold = 100\n\t\t}\n\t}\n}\n", false, true},
			{"correct script in both scopes", onAction, "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_title_law = x\n\t\t}\n\t\tadd_gold = 5\n\t}\n}\n", false, false},
			{"no declared schema", onAction, "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_gold = 100\n\t\t}\n\t}\n}\n", true, false},
			{"under an unknown block", onAction, "on_birth = {\n\teffect = {\n\t\tmystery_block = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n", false, false},
			{"suppressed by pmt:ignore", onAction, "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_gold = 100 # pmt:ignore wrong-scope\n\t\t}\n\t}\n}\n", false, false},
			{"inside an event body", event, "namespace = test\n\ntest.1 = {\n\timmediate = {\n\t\tevery_vassal = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n", false, true},
			{"where nothing declares a scope", event, "namespace = test\n\ntest.1 = {\n\timmediate = {\n\t\tadd_title_law = x\n\t}\n}\n", false, false},
			{"a dotted chain into a title", onAction, "on_birth = {\n\teffect = {\n\t\troot.capital_county = {\n\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n", false, true},
			{"the same chain used correctly", onAction, "on_birth = {\n\teffect = {\n\t\troot.capital_county = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n", false, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string]string{tt.rel: tt.body}
				var s *session.Session
				var root string
				if tt.bare {
					s, root = bareSession(t, files)
				} else {
					s, root = buildSession(t, "ck3", files)
				}
				ds := Diagnose(s, root+"/"+tt.rel)
				if hasDiag(ds, "wrong-scope") != tt.want {
					t.Errorf("wrong-scope = %v, want %v: %+v", !tt.want, tt.want, ds)
				}
				// The message names the token, the scopes it runs on and the scope it sits in.
				for _, d := range ds {
					if d.Code == "wrong-scope" && !strings.Contains(d.Message, " runs on ") {
						t.Errorf("wrong-scope message = %q", d.Message)
					}
				}
			})
		}
	})

	// duplicate-definition fires only within one origin, once, at the later site, and never for shared members, graphics data, or a required inline redeclaration.
	t.Run("duplicate-definition", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{
			"common/traits/00_a.txt": "brave_m = {\n\tdiplomacy = 1\n}\n",
			"common/traits/01_b.txt": "brave_m = {\n\tdiplomacy = 2\n}\n",
			"common/traits/02_c.txt": "craven_m = {\n\tdiplomacy = 1\n}\n",
		})
		if hasDiag(Diagnose(s, root+"/common/traits/00_a.txt"), "duplicate-definition") {
			t.Errorf("first definition should be clean")
		}
		if !hasDiag(Diagnose(s, root+"/common/traits/01_b.txt"), "duplicate-definition") {
			t.Errorf("second definition of brave_m not reported")
		}
		if hasDiag(Diagnose(s, root+"/common/traits/02_c.txt"), "duplicate-definition") {
			t.Errorf("craven_m is defined once")
		}
		s, root = buildSession(t, "ck3", map[string]string{
			"common/activity_types/a.txt": "camp_party = {\n\tspecial_guests = { host }\n}\n",
			"common/activity_types/b.txt": "chariot_race = {\n\tspecial_guests = { host }\n}\n",
		})
		for _, f := range []string{"a", "b"} {
			if hasDiag(Diagnose(s, root+"/common/activity_types/"+f+".txt"), "duplicate-definition") {
				t.Errorf("%s.txt: a shared member is not a duplicate", f)
			}
		}
		s, root = buildSession(t, "eu5", map[string]string{
			"in_game/gfx/map/city_data/one.txt": "mesh_list = {\n\ta = 1\n}\n",
			"in_game/gfx/map/city_data/two.txt": "mesh_list = {\n\ta = 2\n}\n",
		})
		if hasDiag(Diagnose(s, root+"/in_game/gfx/map/city_data/two.txt"), "duplicate-definition") {
			t.Errorf("map data reported as duplicate definitions")
		}
		const decl = "scripted_trigger allow_naming_trigger = {\n\tis_ai = no\n}\n"
		s, root = buildSession(t, "ck3", map[string]string{
			"events/birth_events.txt":   "namespace = birth\n\n" + decl,
			"events/quieter_events.txt": "namespace = qe\n\n" + decl,
		})
		for _, rel := range []string{"events/birth_events.txt", "events/quieter_events.txt"} {
			if hasDiag(Diagnose(s, root+"/"+rel), "duplicate-definition") {
				t.Errorf("%s: inline declaration reported as a duplicate", rel)
			}
		}
	})

	t.Run("settings can turn a code off", func(t *testing.T) {
		ds := []Diagnostic{{Code: "wrong-scope", Severity: sevWarning}, {Code: "unresolved-ref", Severity: sevWarning}}
		got := ApplyEnabled(ds, EnabledCodes(map[string]string{"unresolved-ref": SettingOff}))
		if hasDiag(got, "unresolved-ref") || !hasDiag(got, "wrong-scope") {
			t.Errorf("codes = %v", codesOf(got))
		}
	})
}

// unknownKeyOn reports unknown-key on one key, so a row is not answered by a finding somewhere else in the file.
func unknownKeyOn(ds []Diagnostic, key string) bool {
	for _, d := range ds {
		if d.Code == "unknown-key" && strings.HasPrefix(d.Message, key+" is not a key of ") {
			return true
		}
	}
	return false
}
