// diagnostics_test.go is one table test over Diagnose: one row per kept code,
// plus a subtest per rule with nuance a single row cannot carry.

package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

func codesOf(ds []Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Code
	}
	return out
}

func hasCode(ds []Diagnostic, code string) bool { return hasDiag(ds, code) }

func TestDiagnose(t *testing.T) {
	t.Run("pmt:ignore suppression", func(t *testing.T) {
		sup := scanSuppressions(jomini.Parse(
			"# pmt:ignore-next-line unclosed-brace\nbad = {\nok = 1 # pmt:ignore\n").Src)
		if !sup.Covers(1, "unclosed-brace") || sup.Covers(1, "other") || !sup.Covers(2, "anything") {
			t.Fatal(sup)
		}
		// End to end: a blanket ignore silences every code on its line.
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_triggers/t.txt": "a = {\n" +
				"\t# pmt:ignore-next-line unresolved-ref\n" +
				"\tx = culture:nonexistent\n}\n",
			"common/cultures/00.txt": "english = {\n\tcolor = { 1 2 3 }\n}\n",
		}, citeCache(), nil)
		if hasCode(Diagnose(s, root+"/common/scripted_triggers/t.txt"), "unresolved-ref") {
			t.Errorf("pmt:ignore-next-line did not suppress unresolved-ref")
		}
	})

	// One row per code that a single small file can trigger; the rest have a
	// subtest below because they need more than one row to state correctly.
	for _, tt := range []struct {
		name, game, rel, body, want string
	}{
		{"missing-value", "ck3", "common/traits/t.txt", "foo =\nbar = 1\n", "missing-value"},
		{"stray-close", "ck3", "common/traits/t.txt", "foo = { }\n}\n", "stray-close"},
		{"unclosed-brace", "ck3", "events/x.txt", "test.1 = {\n\ttitle = missing_key\n", "unclosed-brace"},
		{"unterminated-string", "ck3", "common/traits/t.txt", "name = \"hello\n", "unterminated-string"},
		{"loc-no-header", "ck3", "localization/english/a_l_english.yml", "key:0 \"value\"\n", "loc-no-header"},
		{"loc-content-before-header", "ck3", "localization/english/a_l_english.yml",
			"stray content\nl_english:\n key:0 \"v\"\n", "loc-content-before-header"},
		{"loc-tab-indent", "ck3", "localization/english/a_l_english.yml",
			"l_english:\n\tkey:0 \"v\"\n", "loc-tab-indent"},
		{"loc-unterminated-value", "ck3", "localization/english/a_l_english.yml",
			"l_english:\n key:0 \"unterminated\n", "loc-unterminated-value"},
		{"loc-bad-entry", "ck3", "localization/english/a_l_english.yml",
			"l_english:\n key no_colon\n", "loc-bad-entry"},
		{"loc-header-mismatch", "ck3", "localization/english/a_l_english.yml",
			"l_french:\n key:0 \"v\"\n", "loc-header-mismatch"},
		// ClassifyRel requires "localization" (spelled correctly) somewhere in
		// the path before a .yml even reaches locDiags, so the check that flags
		// a misspelled folder only reaches a raw path containing both spellings.
		{"wrong-localization-folder", "ck3", "localization/localisation/a_l_english.yml",
			"l_english:\n key:0 \"v\"\n", "wrong-localization-folder"},
		{"missing-descriptor", "vic3", "notes.txt", "foo = { }\n", "missing-descriptor"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, root := buildSession(t, tt.game, map[string]string{tt.rel: tt.body}, nil, nil)
			f := filepath.Join(root, filepath.FromSlash(tt.rel))
			ds := Diagnose(s, f)
			if !hasCode(ds, tt.want) {
				t.Fatalf("missing %s: %v", tt.want, codesOf(ds))
			}
		})
	}

	// TITLE names an event's tooltip title, not a localization interpolation,
	// and a definition with no learned convention is never demanded a key.
	t.Run("missing-required-loc only where the game names a fire key", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{
			"events/x.txt": "test.1 = {\n\timmediate = { TITLE = primary_title }\n}\n",
		}, nil, nil)
		if hasCode(Diagnose(s, root+"/events/x.txt"), "missing-required-loc") {
			t.Error("TITLE arg read as a loc citation")
		}
		s2, root2 := buildSession(t, "ck3", map[string]string{
			"common/traits/t.txt": "brave = { }\n",
		}, nil, nil)
		if hasCode(Diagnose(s2, root2+"/common/traits/t.txt"), "required-loc") {
			t.Error("required-loc demanded with no learned convention")
		}
	})

	t.Run("metadata skips the parser", func(t *testing.T) {
		s, root := buildSession(t, "vic3", map[string]string{".metadata/metadata.json": "{\n"}, nil, nil)
		if hasCode(Diagnose(s, root+"/.metadata/metadata.json"), "unclosed-brace") {
			t.Error("metadata.json parsed as script")
		}
	})

	t.Run("ORDER interpolation is not a loc citation", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{
			"localization/english/a_l_english.yml": "l_english:\n war:0 \"$ORDER$ fight\"\n",
		}, nil, nil)
		if hasCode(Diagnose(s, root+"/localization/english/a_l_english.yml"), "missing-required-loc") {
			t.Error("$ORDER$ read as a citation")
		}
	})

	t.Run("title loc is not unknown event, and a declared fire key is", func(t *testing.T) {
		body := "namespace = ns\nns.1 = {\n\ttitle = ns.1.t\n" +
			"\tdesc = ns.1.councillor_liege_opening\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body},
			&vanillaCache{FireKeys: map[string]string{"title": "event", "desc": "event"}}, nil)
		if hasCode(Diagnose(s, filepath.Join(root, "events", "x.txt")), "unknown-event") {
			t.Fatal("title/desc loc flagged as event")
		}

		cache := &vanillaCache{
			Schema: &declared.Schema{Triggers: map[string]declared.EngineToken{
				"trigger_event": {Target: "event"},
			}},
			LocConventions: map[string][]loc.Convention{
				"traits": {{Template: "trait_{id}", Coverage: 1}},
			},
		}
		s2, root2 := buildSession(t, "ck3", map[string]string{
			"events/x.txt":         "namespace = ns\nns.1 = {\n\timmediate = { trigger_event = ns.99 }\n}\n",
			"common/traits/00.txt": "ambitious = { }\n",
		}, cache, nil)
		if !hasCode(Diagnose(s2, filepath.Join(root2, "common", "traits", "00.txt")), "required-loc") {
			t.Error("trait required-loc missing")
		}
		if !hasCode(Diagnose(s2, filepath.Join(root2, "events", "x.txt")), "unknown-event") {
			t.Error("unknown-event missing for a declared fire key")
		}
	})

	// The four templates are the whole rule: a `_tooltip` key the script cites is not one of them.
	t.Run("decision convention loc is not required", func(t *testing.T) {
		cache := &vanillaCache{LocConventions: map[string][]loc.Convention{
			"decisions": {{Template: "{id}", Coverage: 1}, {Template: "{id}_desc", Coverage: 1}},
		}}
		s, root := buildSession(t, "ck3", map[string]string{
			"common/decisions/d.txt": "foo = {\n\tselection_tooltip = foo_tooltip\n}\n",
			"localization/english/a_l_english.yml": "l_english:\n foo:0 \"F\"\n" +
				" foo_desc:0 \"D\"\n foo_tooltip:0 \"T\"\n",
		}, cache, nil)
		df := filepath.Join(root, "common", "decisions", "d.txt")
		if hasCode(Diagnose(s, df), "required-loc") {
			t.Fatal("decision with both convention keys must not be required-loc")
		}
	})

	// required-loc asserts the game DEMANDS a key, so only a universal
	// convention (coverage 1) is worth a warning, not a mere habit.
	t.Run("required-loc demands full coverage", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			coverage float64
			want     bool
		}{
			{"a habit most of the kind shares", 0.95, false},
			{"a shape every definition has", 1, true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				cache := &vanillaCache{LocConventions: map[string][]loc.Convention{
					"decisions": {{Template: "{id}_desc", Coverage: tt.coverage}},
				}}
				s, root := buildSession(t, "ck3", map[string]string{
					"common/decisions/d.txt":               "foo = { }\n",
					"localization/english/a_l_english.yml": "l_english:\n foo:0 \"F\"\n",
				}, cache, nil)
				df := filepath.Join(root, "common", "decisions", "d.txt")
				if got := hasCode(Diagnose(s, df), "required-loc"); got != tt.want {
					t.Fatalf("required-loc = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("no diagnostic message invents a convention", func(t *testing.T) {
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
	})

	t.Run("missing-bom", func(t *testing.T) {
		const body = "l_english:\n k:0 \"v\"\n"
		rel := "localization/english/a_l_english.yml"
		s, root := buildSession(t, "ck3", map[string]string{rel: body}, nil, nil)
		f := filepath.Join(root, filepath.FromSlash(rel))
		if !hasCode(Diagnose(s, f), "missing-bom") {
			t.Fatal("want missing-bom when disk has no BOM")
		}
		raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte(body)...)
		if err := os.WriteFile(f, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		s.DidOpen(f, body)
		if hasCode(Diagnose(s, f), "missing-bom") {
			t.Fatal("disk BOM must not warn")
		}
	})

	t.Run("vanilla files are silent, but the mod they overlay is not", func(t *testing.T) {
		install := t.TempDir()
		vEvent := write(t, install, "game/events/v.txt", "test.1 = {\n\ttitle = missing_key\n")
		vLoc := write(t, install, "game/localization/english/v_l_english.yml", "l_english:\n k:0 \"v\"\n")
		modRoot := t.TempDir()
		write(t, modRoot, "descriptor.mod", "name = \"t\"\n")
		modEvent := write(t, modRoot, "events/m.txt", "mod.1 = {\n\ttitle = missing_key\n}\n")

		s := session.New("ws", "ck3", "english", toIndex(&vanillaCache{InstallPath: install}, nil),
			[]derived.ModInput{{Origin: "mod", Root: modRoot, Order: 0}})
		s.DidOpen(vEvent, "test.1 = {\n\ttitle = missing_key\n")
		s.DidOpen(vLoc, "l_english:\n k:0 \"v\"\n")
		s.DidOpen(modEvent, "mod.1 = {\n\ttitle = missing_key\n}\n")

		if diags := Diagnose(s, vEvent); len(diags) != 0 {
			t.Fatalf("vanilla event diags=%v", diags)
		}
		if diags := Diagnose(s, vLoc); len(diags) != 0 {
			t.Fatalf("vanilla loc diags=%v", diags)
		}
		if !hasCode(Diagnose(s, modEvent), "missing-required-loc") {
			t.Fatal("mod file must still get missing-required-loc")
		}
		if s.Resolve("test.1") != nil {
			t.Fatal("vanilla DidOpen must not harvest defs into workspace")
		}
	})

	// A data tier that would not load is a model missing its history, map and
	// dynasty rows, so it may not accuse one of being absent.
	t.Run("unresolved-ref is silent when the data tier failed to load", func(t *testing.T) {
		for _, tc := range []struct {
			name, install string
			wantDiag      bool
		}{
			{"tier reads", "tier-ok", true},
			{"tier missing", "tier-gone", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", dir)
				t.Setenv("AppData", dir)
				if tc.wantDiag {
					main, data := derived.NewIndex("ck3"), derived.NewIndex("ck3")
					main.InstallID, main.GameVersion = tc.install, "1.0"
					data.InstallID, data.GameVersion = tc.install, "1.0"
					if err := derived.SaveCache(main, data); err != nil {
						t.Fatal(err)
					}
				}
				cache := &vanillaCache{
					InstallID: tc.install, GameID: "ck3", GameVersion: "1.0",
					PrefixKinds: map[string]string{"title": "titles"},
				}
				s, root := buildSession(t, "ck3", map[string]string{
					"common/scripted_triggers/t.txt": "a = { x = title:k_nowhere }\n",
				}, cache, nil)
				got := hasCode(Diagnose(s, root+"/common/scripted_triggers/t.txt"), "unresolved-ref")
				if got != tc.wantDiag {
					t.Errorf("unresolved-ref = %v, want %v (dataError=%q)", got, tc.wantDiag, s.Install.DataError())
				}
			})
		}
	})

	// unresolved-ref is the citation check: a value naming a database PMT holds
	// and a key nothing defines. Every silent row is a way of having no evidence.
	t.Run("unresolved-ref", func(t *testing.T) {
		for _, tt := range []struct {
			name, body, names string
			ephemeral, want   bool
		}{
			{"a citation nothing defines", "b = { x = culture:nonexistent }", "nonexistent", false, true},
			{"a citation that resolves", "a = { x = culture:english }", "", false, false},
			// An unbound prefix means PMT does not know that database, which is
			// not evidence the key is missing from it.
			{"a prefix bound to no database", "a = { x = faith:catholic }", "", false, false},
			// Six findings on a real CK3 mod: a value the script invents as it
			// runs is not something anything defines anywhere.
			{"an ephemeral kind", "a = {\n\tset_variable = { name = where value = flag:private_chamber }\n}",
				"", true, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				c := citeCache()
				if tt.ephemeral {
					c.PrefixKinds["flag"] = "flag"
				}
				s, root := buildSession(t, "ck3", map[string]string{
					"common/cultures/00.txt":         "english = {\n\tcolor = { 1 2 3 }\n}\n",
					"common/scripted_triggers/t.txt": tt.body + "\n",
				}, c, nil)
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

	// wrong-value-type uses the declared argument type only: the install-derived
	// field types are deliberately not consulted, because being wrong there
	// costs a wrong suggestion, not a false accusation.
	t.Run("wrong-value-type", func(t *testing.T) {
		for _, tt := range []struct {
			name, body string
			schema     bool
			want       bool
		}{
			{"a value of the wrong declared type", "b = { has_culture = not_a_culture }", true, true},
			{"a value of the right type", "a = { has_culture = english }", true, false},
			{"the same value with no schema", "a = { has_culture = not_a_culture }", false, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				var c *vanillaCache
				if tt.schema {
					c = citeCache()
				}
				s, root := buildSession(t, "ck3", map[string]string{
					"common/cultures/00.txt":         "english = {\n\tcolor = { 1 2 3 }\n}\n",
					"common/scripted_triggers/t.txt": tt.body + "\n",
				}, c, nil)
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
				if !tt.schema && hasCode(ds, "unresolved-ref") {
					t.Errorf("no schema should mean no declared violations: %v", codesOf(ds))
				}
			})
		}
	})

	// unknown-key is the one check that reports an absence: a key is judged
	// against the structure or body §9 learned at that position, and nothing
	// else. Each row is one of the ways a key is known.
	t.Run("unknown-key", func(t *testing.T) {
		c := &vanillaCache{
			NestedMembers: map[string][]string{
				"triggered_desc": {"desc", "trigger", "first_valid", "random_valid",
					"triggered_effect", "modifier"},
				"cooldown": {"days"},
				// A field the corpus writes under a DIFFERENT block: send_interface_toast, never send_interface_message, the way CK3 writes message_filter_type 667 times.
				"send_interface_toast": {"message_filter_type"},
			},
			Structures: map[string][]string{"event": {"triggered_desc", "option"}},
			Schema:     &declared.Schema{Triggers: map[string]declared.EngineToken{"is_adult": {}}},
		}
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
			// A class the fold files as a position, and the words that are
			// control flow rather than names, are never members.
			{"a weight", "30", "30 = { desc = x }", false},
			{"a stop word", "nor", "NOR = { desc = x }", false},
			{"a scope word", "this", "this = yes", false},
			// A key the schema opens a block with anywhere is engine grammar,
			// whatever structure it is written in.
			{"a block key from elsewhere", "cooldown", "cooldown = { days = 1 }", false},
			// A real key in the wrong block is not this check's business: the
			// corpus writes message_filter_type under send_interface_toast, never
			// under triggered_desc, and the ruling is unknown-key names only a key
			// the corpus never writes anywhere.
			{"a field the corpus writes elsewhere", "message_filter_type",
				"message_filter_type = yes", false},
			// A name written nowhere in the corpus is the one thing this check
			// exists to catch.
			{"a key written nowhere", "trigger_evnt", "trigger_evnt = yes", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				body := "namespace = ns\nns.1 = {\n\ttriggered_desc = {\n\t\t" +
					tt.line + "\n\t}\n}\n"
				s, root := buildSession(t, "ck3", map[string]string{
					"events/x.txt": body,
					"common/scripted_triggers/t.txt": "scripted_trigger my_trigger = {\n" +
						"\talways = yes\n}\n",
				}, c, nil)
				ds := Diagnose(s, root+"/events/x.txt")
				if got := unknownKeyOn(ds, tt.key); got != tt.want {
					t.Errorf("unknown-key on %s = %v, want %v (%v)", tt.key, got, tt.want, codesOf(ds))
				}
			})
		}
		// The arguments of a call are the macro's parameters; no block holds
		// them as fields, so the structure around the call answers nothing.
		t.Run("a call is judged against its macro's parameters", func(t *testing.T) {
			s, root := buildSession(t, "ck3", map[string]string{
				"events/x.txt": "namespace = ns\nns.1 = {\n\timmediate = {\n" +
					"\t\tmy_effect = {\n\t\t\tREASON = x\n\t\t\tNOPE = y\n\t\t}\n\t}\n}\n",
				"common/scripted_effects/e.txt": "scripted_effect my_effect = {\n" +
					"\tadd_gold = $REASON$\n}\n",
			}, c, nil)
			ds := Diagnose(s, root+"/events/x.txt")
			for _, d := range ds {
				if d.Code == "unknown-key" && strings.Contains(d.Message, "reason") {
					t.Errorf("a parameter the macro spells reported: %s", d.Message)
				}
			}
			found := false
			for _, d := range ds {
				found = found || strings.Contains(d.Message, "nope is not a parameter of my_effect")
			}
			if !found {
				t.Errorf("an argument the macro never spells was not reported: %v", codesOf(ds))
			}
		})
		// A structure unifies across every position its key is written at, so a
		// member the corpus wrote only somewhere else must not be unknown here.
		t.Run("a member written at another position is not unknown", func(t *testing.T) {
			s, root := buildSession(t, "ck3", map[string]string{
				"events/x.txt": "test.1 = {\n\tcooldown = {\n\t\tdays = 10\n\t}\n}\n" +
					"test.2 = {\n\tcooldown = {\n\t\tmonths = 2\n\t}\n}\n",
			}, nil, nil)
			if unknownKeyOn(Diagnose(s, root+"/events/x.txt"), "months") {
				t.Error("months reported unknown at the position where only days was written")
			}
		})
		// A block the install never wrote is judged against the workspace's own
		// shape, which here is the mod's own file: a macro call's arguments
		// accuse nobody.
		t.Run("silent in a mod's own block", func(t *testing.T) {
			body := "d = {\n\tunwritten_block = {\n\t\tanything = yes\n\t}\n}\n"
			s, root := buildSession(t, "ck3",
				map[string]string{"common/decisions/d.txt": body}, &vanillaCache{}, nil)
			if unknownKeyOn(Diagnose(s, root+"/common/decisions/d.txt"), "anything") {
				t.Errorf("judged a key against a block only the mod wrote")
			}
		})
	})

	// wrong-scope is one row per rule the scope walk has to hold to, over the
	// full Diagnose pipeline (suppression included); ScopeMisuses and ScopeAt
	// pin the walk itself directly.
	t.Run("wrong-scope", func(t *testing.T) {
		const onAction = "common/on_action/x.txt"
		const event = "events/x.txt"
		for _, tt := range []struct {
			name, rel, body string
			noSchema, want  bool
		}{
			{"a character effect in a title block", onAction,
				"on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_gold = 100\n\t\t}\n\t}\n}\n",
				false, true},
			{"correct script in both scopes", onAction,
				"on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_title_law = x\n\t\t}\n\t\tadd_gold = 5\n\t}\n}\n",
				false, false},
			{"no declared schema", onAction,
				"on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_gold = 100\n\t\t}\n\t}\n}\n",
				true, false},
			{"under an unknown block", onAction,
				"on_birth = {\n\teffect = {\n\t\tmystery_block = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n",
				false, false},
			{"suppressed by pmt:ignore", onAction,
				"on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_gold = 100 # pmt:ignore wrong-scope\n\t\t}\n\t}\n}\n",
				false, false},
			// every_vassal declares Targets: character outside any on_action;
			// ScopeMisuses used to bail on an empty root, so this ran only in
			// common/on_action and nowhere else.
			{"inside an event body", event,
				"namespace = test\n\ntest.1 = {\n\timmediate = {\n\t\tevery_vassal = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n",
				false, true},
			{"where nothing declares a scope", event,
				"namespace = test\n\ntest.1 = {\n\timmediate = {\n\t\tadd_title_law = x\n\t}\n}\n",
				false, false},
			{"a dotted chain into a title", onAction,
				"on_birth = {\n\teffect = {\n\t\troot.capital_county = {\n\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n",
				false, true},
			{"the same chain used correctly", onAction,
				"on_birth = {\n\teffect = {\n\t\troot.capital_county = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n",
				false, false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				c := scopedCache()
				if tt.noSchema {
					c = &vanillaCache{Vocabulary: []string{"add_gold"}, NestedMembers: slotBodies}
				}
				s, root := buildSession(t, "ck3", map[string]string{tt.rel: tt.body}, c, nil)
				ds := Diagnose(s, root+"/"+tt.rel)
				if got := hasCode(ds, "wrong-scope"); got != tt.want {
					t.Errorf("wrong-scope = %v, want %v: %+v", got, tt.want, ds)
				}
			})
		}
	})

	// duplicate-definition fires only within one origin, once, at the later
	// site, and never for shared members, graphics data, or a required inline
	// redeclaration.
	t.Run("duplicate-definition", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{
			"common/traits/00_a.txt": "brave = {\n\tdiplomacy = 1\n}\n",
			"common/traits/01_b.txt": "brave = {\n\tdiplomacy = 2\n}\n",
			"common/traits/02_c.txt": "craven = {\n\tdiplomacy = 1\n}\n",
		}, nil, nil)
		if hasCode(Diagnose(s, root+"/common/traits/00_a.txt"), "duplicate-definition") {
			t.Errorf("first definition should be clean")
		}
		if !hasCode(Diagnose(s, root+"/common/traits/01_b.txt"), "duplicate-definition") {
			t.Errorf("second definition of brave not reported")
		}
		if hasCode(Diagnose(s, root+"/common/traits/02_c.txt"), "duplicate-definition") {
			t.Errorf("craven is defined once")
		}

		t.Run("ignores a member shared by two definitions", func(t *testing.T) {
			s, root := buildSession(t, "ck3", map[string]string{
				"common/activity_types/a.txt": "camp_party = {\n\tspecial_guests = { host }\n}\n",
				"common/activity_types/b.txt": "chariot_race = {\n\tspecial_guests = { host }\n}\n",
			}, &vanillaCache{}, nil)
			for _, f := range []string{"a", "b"} {
				if hasCode(Diagnose(s, root+"/common/activity_types/"+f+".txt"), "duplicate-definition") {
					t.Errorf("%s.txt: a shared member is not a duplicate", f)
				}
			}
		})

		// EU5 ships map data under dlc/<pack>/in_game/gfx/map/city_data, and
		// every file there repeats a `mesh_list` block; MatchExtract now reports
		// no extraction mode for that path at all, so it never becomes a
		// definition to collide in the first place.
		t.Run("does not extract map data as a definition", func(t *testing.T) {
			s, root := buildSession(t, "eu5", map[string]string{
				"in_game/gfx/map/city_data/one.txt": "mesh_list = {\n\ta = 1\n}\n",
				"in_game/gfx/map/city_data/two.txt": "mesh_list = {\n\ta = 2\n}\n",
			}, &vanillaCache{}, nil)
			if hasCode(Diagnose(s, root+"/in_game/gfx/map/city_data/two.txt"), "duplicate-definition") {
				t.Errorf("map data reported as duplicate definitions")
			}
		})

		// ck3-quieter-events carries one scripted_trigger in two event files,
		// resolved per file by the engine, so the repeat is required.
		t.Run("allows an inline declaration required per file", func(t *testing.T) {
			const decl = "scripted_trigger allow_naming_trigger = {\n\tis_ai = no\n}\n"
			s, root := buildSession(t, "ck3", map[string]string{
				"events/birth_events.txt":   "namespace = birth\n\n" + decl,
				"events/quieter_events.txt": "namespace = qe\n\n" + decl,
			}, nil, nil)
			for _, rel := range []string{"events/birth_events.txt", "events/quieter_events.txt"} {
				if hasCode(Diagnose(s, root+"/"+rel), "duplicate-definition") {
					t.Errorf("%s: inline declaration reported as a duplicate", rel)
				}
			}
		})
	})

	// Settings turn a code off without the checks knowing anything about
	// configuration; an unconfigured code keeps its own default.
	t.Run("settings can turn a code off", func(t *testing.T) {
		ds := []Diagnostic{
			{Code: "wrong-scope", Severity: sevWarning},
			{Code: "unresolved-ref", Severity: sevWarning},
		}
		got := ApplyEnabled(append([]Diagnostic(nil), ds...), EnabledCodes(map[string]string{
			"unresolved-ref": SettingOff,
		}))
		if hasCode(got, "unresolved-ref") {
			t.Errorf("code set to off still reported: %v", codesOf(got))
		}
		if !hasCode(got, "wrong-scope") {
			t.Errorf("unconfigured code should keep its default: %v", codesOf(got))
		}
	})
}

// unknownKeyOn reports unknown-key on one key, so a row is not answered by a
// finding somewhere else in the file.
func unknownKeyOn(ds []Diagnostic, key string) bool {
	for _, d := range ds {
		if d.Code == "unknown-key" && strings.HasPrefix(d.Message, key+" is not a key of ") {
			return true
		}
	}
	return false
}

// citeCache binds a prefix to a database so typed citations can be judged.
func citeCache() *vanillaCache {
	return &vanillaCache{
		PrefixKinds: map[string]string{"culture": "cultures"},
		// scopeKind is reversed from KindScope, and CiteFormOfScope reads it —
		// PrefixKinds alone leaves the declared target unbound to any database.
		KindScope: map[string]string{"cultures": "culture"},
		Schema: &declared.Schema{
			Scopes: map[string]declared.ScopeType{
				"character": {ChangeScopes: true, ExecuteEffects: true},
				"culture":   {},
			},
			Triggers: map[string]declared.EngineToken{
				"has_culture": {In: []string{"character"}, Target: "culture"},
			},
		},
	}
}
