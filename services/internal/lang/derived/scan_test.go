// scan_test.go builds tiny synthetic installs and asserts what a scan makes of
// them: defs, structures, vocabulary, loc, field docs, wrappers, prefix binding,
// and the script_docs dumps that enrich all of it.

package derived

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
)

// ck3Install writes the shared corpus fixture and returns install + isolated user data.
func ck3Install(t *testing.T) (base, userData string) {
	t.Helper()
	base = t.TempDir()
	userData = filepath.Join(base, "userdata")
	pinUserData(t, base, userData)
	writeMod(t, base, "game/common/traits/00_traits.txt", `brave = {
	category = personality
	opposites = { craven }
	ai_boldness = 30
}
`)
	writeMod(t, base, "game/common/traits/_traits.info", `# The trait category
category = personality
`)
	writeMod(t, base, "game/events/_events.info", `# Event presentation type
type = character_event
# Dynamic loc key
title = loc_key
portrait = {
	# Controls visibility of this portrait
	trigger = { }
}
`)
	writeMod(t, base, "game/events/test_events.txt", `namespace = test
test.1 = {
	type = character_event
	immediate = {
		add_gold = 10
		trigger_event = test.2
		save_scope_as = duel_target
		my_trig = { TARGET = root }
	}
	option = { name = test.1.a }
}
test.2 = { type = character_event }
scripted_effect my_inline = { add_prestige = 5 }
`)
	writeMod(t, base, "game/common/scripted_triggers/t.txt",
		"my_trig = { exists = $TARGET$ }\n")
	// A local database: no prefix cites a game rule's options, but `default` names one of them.
	writeMod(t, base, "game/common/game_rules/00_rules.txt", `difficulty = {
	categories = { ai }
	default = normal_difficulty
	normal_difficulty = { }
	hard_difficulty = { flag = blocks_achievements }
}
`)
	writeMod(t, base, "game/localization/english/test_l_english.yml", `l_english:
 test.1.t:0 "Test Event"
`)
	writeMod(t, base, "game/localization/english/header_only.yml", `l_english:
 header_key:0 "From header"
`)
	writeMod(t, base, "game/localization/french/test_l_french.yml", `l_french:
 french_only:0 "Bonjour"
`)
	return base, userData
}

func pinUserData(t *testing.T, install, userData string) {
	t.Helper()
	if err := os.MkdirAll(userData, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(install, "launcher")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"gameDataPath": userData})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "launcher-settings.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func findDef(defs []Def, kind, key string) bool {
	for _, d := range defs {
		if d.Kind == kind && d.Key == key {
			return true
		}
	}
	return false
}

func scanCK3(t *testing.T, base string) *Index {
	t.Helper()
	t.Cleanup(func() { _ = DropVanillaFiles("inst") })
	c, _, err := Scan(context.Background(), ScanRequest{
		InstallID: "inst", GameID: "ck3", InstallPath: base,
		Version: "1.0", LocLang: "english",
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return c
}

// scanFiles writes files under a fresh install root and scans it.
func scanFiles(t *testing.T, id, gameID string, files map[string]string) *Index {
	t.Helper()
	base := t.TempDir()
	for rel, body := range files {
		writeMod(t, base, rel, body)
	}
	pinUserData(t, base, filepath.Join(t.TempDir(), "ud"))
	t.Cleanup(func() { _ = DropVanillaFiles(id) })
	c, _, err := Scan(context.Background(), ScanRequest{
		InstallID: id, GameID: gameID, InstallPath: base,
		Version: "1.0", LocLang: "english",
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return c
}

func TestScan(t *testing.T) {
	t.Run("ck3 corpus fixture", func(t *testing.T) {
		base, _ := ck3Install(t)
		c := scanCK3(t, base)

		for _, d := range [][2]string{
			{"traits", "brave"}, {"event", "test.1"}, {"scripted_effect", "my_inline"},
		} {
			if !findDef(c.Defs, d[0], d[1]) {
				t.Errorf("missing %s %s; defs=%v", d[0], d[1], c.Defs)
			}
		}
		if !hasEdge(c.Edges, "test.1", "test.2", "trigger_event") {
			t.Errorf("missing fire edge test.1→test.2; edges=%v", c.Edges)
		}

		if got := c.Loc["english"]["test.1.t"].Value; got != "Test Event" {
			t.Errorf("loc[test.1.t] = %q, want %q", got, "Test Event")
		}
		if c.Docs.FieldInfo["category"] == "" {
			t.Errorf("fieldDocs[category] empty, want prose from _traits.info")
		}
		if hk, ok := c.locAt("english", "header_key"); !ok || hk.Value != "From header" || hk.Path == "" {
			t.Errorf("header_key = %+v", hk)
		}
		if _, ok := c.Loc["english"]["french_only"]; ok {
			t.Errorf("french loc in default sidecar")
		}
		fr, err := HarvestLoc(context.Background(), "ck3", base, "french")
		if err != nil {
			t.Fatalf("lazy french harvest: %v", err)
		}
		if got := fr.Loc["french"]["french_only"].Value; got != "Bonjour" {
			t.Errorf("french loc[french_only] = %q", got)
		}
		if got := c.Docs.Fields["event"]["type"]; !strings.Contains(got, "presentation") {
			t.Errorf("fields[event][type] = %q, want events.info prose", got)
		}
		if got := c.Docs.Fields["event"]["title"]; !strings.Contains(got, "Dynamic") {
			t.Errorf("fields[event][title] = %q, want events.info prose", got)
		}
		// A field documented inside an example block is addressed by that block, not by the kind.
		if got := c.Docs.Fields["portrait"]["trigger"]; !strings.Contains(got, "visibility") {
			t.Errorf("fields[portrait][trigger] = %q, want events.info prose", got)
		}

		// The local database: the options folded to <kind>, so a mod's own option is never a key of game_rules.
		if !c.Schema.TypeOf(Cell{"game_rules", segTop, ""}).Rows {
			t.Errorf("game_rules options are not a local database: %+v",
				c.Schema.Cells["game_rules"][""])
		}
		if c.Schema.TypeOf(Cell{"game_rules", "", segKind}) == nil ||
			c.Schema.TypeOf(Cell{"game_rules", "", "normal_difficulty"}) != nil {
			t.Errorf("options did not fold: %+v", c.Schema.Cells["game_rules"][""])
		}
		for _, k := range []string{"category", "opposites", "ai_boldness"} {
			if !slices.Contains(c.Schema.Members("traits", ""), k) {
				t.Errorf("members[traits] missing %q: %v", k, c.Schema.Members("traits", ""))
			}
		}
		for _, k := range []string{"immediate", "option"} {
			if !slices.Contains(c.Schema.Members("event", ""), k) {
				t.Errorf("members[event] missing %q: %v", k, c.Schema.Members("event", ""))
			}
		}
		if got := c.Schema.Members("event", ""); len(got) == 0 || got[0] != "type" {
			t.Errorf("members[event] freq = %v, want type first", got)
		}
		for _, k := range []string{"immediate", "option"} {
			if !c.Schema.TypeOf(Cell{"event", "", k}).IsBlock() {
				t.Errorf("event.%s is not a block cell", k)
			}
		}
		if c.Schema.TypeOf(Cell{"event", "", "type"}).IsBlock() {
			t.Errorf("event.type should be scalar")
		}

		// Corpus vocabulary must be populated with NO script_docs dump, while effects and triggers -- the game's declared API -- must stay empty: inferring them from position elected every $PARAM$ site as an effect.
		if !slices.Contains(c.Schema.VocabNames, "add_gold") {
			t.Errorf("vocabulary missing corpus token add_gold: %v", c.Schema.VocabNames)
		}
		if len(c.Schema.Effects) != 0 || len(c.Schema.Triggers) != 0 {
			t.Errorf("engine tokens claimed without dumps: effects=%v triggers=%v",
				c.Schema.Effects, c.Schema.Triggers)
		}
		if c.FormatVersion != CacheFormatVersion {
			t.Errorf("formatVersion = %d, want %d", c.FormatVersion, CacheFormatVersion)
		}
		gotParam, gotCall := false, false
		for _, d := range c.Defs {
			k := jomini.CanonicalKind(d.Kind)
			if jomini.IsEphemeral(k) && k != "script_param" {
				t.Errorf("scan persisted ephemeral %s %s", d.Kind, d.Key)
			}
			if k == "script_param" && d.Key == "TARGET" {
				gotParam = true
			}
		}
		for _, r := range c.Refs {
			if r.Key == "my_trig" && jomini.CanonicalKind(c.KindOf(r.KindID)) == "scripted_triggers" {
				gotCall = true
			}
		}
		if !gotParam || !gotCall {
			t.Errorf("vanilla $NAME$/call persist param=%v call=%v refs=%v",
				gotParam, gotCall, c.Refs)
		}
	})

	// An inline dump beside the same fixture: the declared API reaches the cache, and a field documented inside a nested block is not mistaken for an effect.
	t.Run("ck3 dump enrichment", func(t *testing.T) {
		for _, tt := range []struct {
			name, body, want, drop string
		}{
			{"enrichment",
				"Effect Documentation:\n\n----\n\nadd_gold - Adds gold to the treasury.\n" +
					"Supported Scopes: character\n\n----\n\nadd_prestige - Adds prestige.\n" +
					"Supported Scopes: character\n\n----\n",
				"add_gold", ""},
			{"nested type is not effect",
				"----\nadd_gold\n\tAdds gold.\n\n\tSupported Scopes: artifact\n\ttype = enum\n----\n",
				"add_gold", "type"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				base, userData := ck3Install(t)
				logs := filepath.Join(userData, "logs")
				if err := os.MkdirAll(logs, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(logs, "effects.log"), []byte(tt.body), 0o644); err != nil {
					t.Fatal(err)
				}
				got := scanCK3(t, base)
				if _, ok := got.Declared.Effects[tt.want]; !ok {
					t.Errorf("effects missing %s: %v", tt.want, got.Declared.Effects)
				}
				if tt.name == "enrichment" && got.Declared.Doc("add_gold") == "" {
					t.Error("schema doc for add_gold empty after enrichment")
				}
				if tt.drop != "" {
					if _, ok := got.Declared.Effects[tt.drop]; ok {
						t.Fatalf("nested dump field %s leaked: %v", tt.drop, got.Declared.Effects)
					}
				}
			})
		}
	})

	// Each game's committed dump fixture end to end: the token is declared, the fixture's own section header is not mistaken for one, and the game's prose and scopes survive. One table, because the contract is identical across the three games -- only the dump syntax and the folder differ.
	t.Run("script_docs fixtures", func(t *testing.T) {
		for _, tt := range []struct {
			game, sub, script, body  string
			dumps                    map[string]string
			want, doc, scope, modDoc string
		}{
			{
				game: "ck3", sub: "logs",
				script: "game/events/x.txt",
				body:   "namespace = t\nt.1 = { type = character_event }\n",
				dumps:  map[string]string{"effects.log": "ck3-effects.log"},
				want:   "add_diplomacy_skill", doc: "diplomacy",
			},
			{
				game: "eu5", sub: "docs",
				script: "game/in_game/events/x.txt",
				body:   "namespace = t\nt.1 = { type = country_event }\n",
				dumps:  map[string]string{"effects.log": "eu5-effects.log"},
				want:   "abandon_colonial_charter", doc: "colonial", scope: "none",
			},
			{
				game: "vic3", sub: "docs",
				script: "game/common/country_definitions/x.txt",
				body:   "USA = { color = { 1 2 3 } }\n",
				dumps: map[string]string{
					"effects.log":   "vic3-effects.log",
					"modifiers.log": "vic3-modifiers.log",
				},
				want: "abandon_revolution", modDoc: "building_throughput_add",
			},
		} {
			t.Run(tt.game, func(t *testing.T) {
				base := t.TempDir()
				ud := filepath.Join(base, "userdata")
				pinUserData(t, base, ud)
				writeMod(t, base, tt.script, tt.body)
				docs := filepath.Join(ud, tt.sub)
				if err := os.MkdirAll(docs, 0o755); err != nil {
					t.Fatal(err)
				}
				for name, fixture := range tt.dumps {
					raw, err := os.ReadFile(filepath.Join("testdata", fixture))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(docs, name), raw, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				id := tt.game + "docs"
				t.Cleanup(func() { _ = DropVanillaFiles(id) })
				c, _, err := Scan(context.Background(), ScanRequest{
					InstallID: id, GameID: tt.game, InstallPath: base,
					Version: "1.0", LocLang: "english",
				})
				if err != nil {
					t.Fatalf("Scan: %v", err)
				}
				if c.Declared == nil {
					t.Fatal("scan produced no declared schema")
				}
				if _, ok := c.Declared.Effects[tt.want]; !ok {
					t.Fatalf("effects missing %s: %v", tt.want, c.Declared.Effects)
				}
				// Every fixture opens with an "Effect Documentation" style header; a splitter that reads one as a block name declares "effect" itself as an effect, which then leads the completion list.
				if _, ok := c.Declared.Effects["effect"]; ok {
					t.Error("section header parsed as an effect")
				}
				if tt.doc != "" && !strings.Contains(c.Declared.Doc(tt.want), tt.doc) {
					t.Errorf("Doc(%s) = %q, want it to mention %q",
						tt.want, c.Declared.Doc(tt.want), tt.doc)
				}
				if tt.scope != "" && !strings.Contains(c.Declared.ScopeText(tt.want), tt.scope) {
					t.Errorf("ScopeText(%s) = %q, want %q",
						tt.want, c.Declared.ScopeText(tt.want), tt.scope)
				}
				if tt.modDoc != "" && c.Declared.Doc(tt.modDoc) == "" {
					t.Errorf("modifier doc for %s is empty", tt.modDoc)
				}
			})
		}
	})

	t.Run("eu5 shipped docs", func(t *testing.T) {
		c := scanFiles(t, "eu5docs2", "eu5", map[string]string{
			"game/in_game/common/traits/00_traits.txt": "brave = {\n\tcategory = ruler\n\tallow = { always = yes }\n}\n",
			"game/in_game/common/traits/_traits.info": `trait_name = {
	# Which type of character can use this trait?
	category = ruler
	allow = {
		# What else does the character need
	}
}
`,
			"game/in_game/common/building_types/readme.txt": "# ATTRIBUTES\n" +
				"# - build_time: <integer> building time in days\n" +
				"# - allow: <trigger> can the building be built\n",
			// The cabinet_actions shape: fields indented under a key that never opens a brace, a heading between them, and a description written inside the field's own braces.
			"game/in_game/common/cabinet_actions/readme.txt": "# select_trigger = can add multiple of these\n" +
				"#\t\t\tformat:\n" +
				"#\t\t\t\tlooking_for_a = character/location\n" +
				"#\t\t\t\tshow_if = { <trigger to see if this stage is needed}\n",
		})
		if got := c.Docs.Fields["traits"]["allow"]; !strings.Contains(got, "character need") {
			t.Fatalf("traits allow doc = %q", got)
		}
		if got := c.Docs.Fields["traits"]["category"]; !strings.Contains(got, "Which type") {
			t.Fatalf("traits category doc = %q", got)
		}
		if got := c.Docs.Fields["building_types"]["build_time"]; !strings.Contains(got, "building time") {
			t.Fatalf("building_types build_time = %q", got)
		}
		// Addressed by (structure, field), with the brace prose read and the heading never a doc.
		if got := c.Docs.Fields["select_trigger"]["show_if"]; !strings.Contains(got, "this stage is needed") {
			t.Fatalf("select_trigger show_if = %q", got)
		}
		if got := c.Docs.Fields["select_trigger"]["looking_for_a"]; strings.Contains(got, "format") {
			t.Fatalf("select_trigger looking_for_a = %q, want no heading", got)
		}
	})

	t.Run("eu5 scans game/ not the install root", func(t *testing.T) {
		c := scanFiles(t, "eu5inst", "eu5", map[string]string{
			"game/in_game/events/x.txt": "namespace = t\nt.1 = { type = country_event }\n",
			"in_game/events/wrong.txt":  "namespace = w\nw.1 = { type = country_event }\n",
		})
		if !findDef(c.Defs, "event", "t.1") {
			t.Fatalf("missing install event t.1; defs=%v", c.Defs)
		}
		if findDef(c.Defs, "event", "w.1") {
			t.Fatalf("scanned install-root in_game, not game/: %v", c.Defs)
		}
	})

	t.Run("eu5 stage roots", func(t *testing.T) {
		c := scanFiles(t, "stages", "eu5", map[string]string{
			"game/in_game/common/traits/a.txt":             "brave = { category = ruler }\n",
			"game/main_menu/common/static_modifiers/b.txt": "mod_a = { diplomacy = 1 }\n",
			"game/loading_screen/common/defines/c.txt":     "NGame = { DAYS = 1 }\n",
		})
		if !findDef(c.Defs, "traits", "brave") ||
			!findDef(c.Defs, "static_modifiers", "mod_a") ||
			!findDef(c.Defs, "defines", "NGame") {
			t.Fatalf("stage defs=%v", c.Defs)
		}
	})

	// The prefix table is read from the game's DECLARED scope links, so the test supplies them the way script_docs does; bindKinds matches by name.
	t.Run("eu5 prefix binding and silent fire", func(t *testing.T) {
		base := t.TempDir()
		writeMod(t, base, "game/in_game/map_data/location_templates.txt",
			"stockholm = { province = 1 }\nuppsala = { province = 2 }\nlund = { province = 3 }\n")
		writeMod(t, base, "game/in_game/common/countries/x.txt",
			"SWE = { color = { 1 2 3 } }\nDAN = { color = { 1 2 3 } }\nNOR = { color = { 1 2 3 } }\n")
		writeMod(t, base, "game/in_game/events/e.txt", `namespace = ns
ns.1 = { type = country_event }
ns.2 = { type = country_event }
ev = {
	immediate = {
		trigger_event_silently = ns.1
		trigger_event_silently = { id = ns.2 }
		exists = location:stockholm
		exists = location:uppsala
		exists = location:lund
		c:SWE = { add_gold = 1 }
		c:DAN = { add_gold = 1 }
		c:NOR = { add_gold = 1 }
	}
}
`)
		ud := filepath.Join(t.TempDir(), "ud")
		pinUserData(t, base, ud)
		writeEventTargets(t, ud, "docs", `# Event Target Documentation
### location
Get the location with the specified key
Requires Data: yes
Global Link: yes
Output Scopes: map_data

### c
Get the country with the specified tag
Requires Data: yes
Global Link: yes
Output Scopes: countries
`)
		t.Cleanup(func() { _ = DropVanillaFiles("eu5votes") })
		c, _, err := Scan(context.Background(), ScanRequest{
			InstallID: "eu5votes", GameID: "eu5", InstallPath: base,
			Version: "1.0", LocLang: "english",
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.Schema.Prefix["location"] != "map_data" {
			t.Fatalf("location -> %q, want map_data", c.Schema.Prefix["location"])
		}
		if c.Schema.Prefix["c"] != "countries" {
			t.Fatalf("c -> %q, want countries", c.Schema.Prefix["c"])
		}
		if k := c.Schema.TypeOf(Cell{"event", "immediate", "trigger_event_silently"}).kindsOf(); !slices.Contains(k, "event") {
			t.Fatalf("trigger_event_silently cell kinds=%v", k)
		}
		var silent bool
		for _, e := range c.Edges {
			if e.Via == "trigger_event_silently" && (e.To == "ns.1" || e.To == "ns.2") {
				silent = true
			}
		}
		if !silent {
			t.Fatalf("missing silent fire edge: %v", c.Edges)
		}
	})

	// `p:` is declared but its ids are engine hex, not database rows, so it must stay unbound even though the game lists it as a link. A COUNTRIES wrapper is not a definition.
	t.Run("vic3 prefixes and wrappers", func(t *testing.T) {
		base := t.TempDir()
		writeMod(t, base, "game/common/country_definitions/x.txt",
			"USA = { }\nFRA = { }\nGBR = { }\n")
		writeMod(t, base, "game/common/cultures/x.txt", "yankee = { }\ndixie = { }\nfrench = { }\n")
		writeMod(t, base, "game/common/buildings/x.txt",
			"building_port = { }\nbuilding_farm = { }\nbuilding_mine = { }\n")
		writeMod(t, base, "game/common/journal_entries/x.txt",
			"je_example = { }\nje_second = { }\nje_third = { }\n")
		writeMod(t, base, "game/common/states/x.txt", "STATE_KENYA = { }\n")
		writeMod(t, base, "game/common/history/countries.txt",
			"COUNTRIES = { c:USA ?= { } }\n")
		writeMod(t, base, "game/events/e.txt", `namespace = ns
ns.1 = {
	immediate = {
		c:USA = { }
		c:FRA = { }
		c:GBR = { }
		cu:yankee = { }
		cu:dixie = { }
		cu:french = { }
		bt:building_port = { }
		bt:building_farm = { }
		bt:building_mine = { }
		je:je_example = { }
		je:je_second = { }
		je:je_third = { }
		exists = s:STATE_KENYA.region_state:OMA
		exists = p:x1E5261
		exists = p:x2F6372
		exists = p:x3A7483
		exists = state.b:building_port.level
	}
}
`)
		ud := filepath.Join(t.TempDir(), "ud")
		pinUserData(t, base, ud)
		writeEventTargets(t, ud, "docs", `# Event Target Documentation
### c
Requires Data: yes
Global Link: yes
Output Scopes: country_definitions

### cu
Requires Data: yes
Global Link: yes
Output Scopes: culture

### bt
Requires Data: yes
Global Link: yes
Output Scopes: buildings

### je
Requires Data: yes
Global Link: yes
Output Scopes: journal_entries

### p
Requires Data: yes
Global Link: yes
Output Scopes: province
`)
		t.Cleanup(func() { _ = DropVanillaFiles("vic3pref") })
		c, _, err := Scan(context.Background(), ScanRequest{
			InstallID: "vic3pref", GameID: "vic3", InstallPath: base,
			Version: "1.0", LocLang: "english",
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.Schema.Prefix["c"] != "country_definitions" || c.Schema.Prefix["cu"] != "cultures" ||
			c.Schema.Prefix["bt"] != "buildings" || c.Schema.Prefix["je"] != "journal_entries" {
			t.Fatalf("prefix kinds=%v", c.Schema.Prefix)
		}
		if c.Schema.Prefix["p"] != "" {
			t.Fatalf("p:hex must stay ephemeral: %v", c.Schema.Prefix)
		}
		if findDef(c.Defs, "countries", "COUNTRIES") || findDef(c.Defs, "history", "COUNTRIES") {
			t.Fatalf("wrapper harvested: %v", c.Defs)
		}
		if !findDef(c.Defs, "country_definitions", "USA") {
			t.Fatalf("missing USA: %v", c.Defs)
		}
	})

	// A `keyword name = { }` pair is a declaration, not a wrapper, however its body is shaped: matching on shape alone deleted 28 vanilla CK3 macros. And a scripted_* / script_values folder has already declared that its top-level keys are definitions, so the folder rule wins over the shape.
	t.Run("ck3 declarations are never wrappers", func(t *testing.T) {
		base, _ := ck3Install(t)
		writeMod(t, base, "game/common/culture/cultures/00_cultures.txt",
			"aragonese = { color = { 1 2 3 } }\nenglish = { color = { 4 5 6 } }\n")
		writeMod(t, base, "game/events/inline_macro_events.txt", `namespace = im
scripted_effect copy_pillars_effect = {
	culture:aragonese = { add_culture_tradition = x }
	culture:english = { add_culture_tradition = y }
}
im.1 = {
	type = character_event
	immediate = { copy_pillars_effect = yes }
}
`)
		writeMod(t, base, "game/common/landed_titles/00_landed_titles.txt",
			"h_china = { color = { 1 2 3 } }\n")
		writeMod(t, base, "game/common/scripted_triggers/00_t.txt",
			"hegemon_favors_advancement_trigger = {\n\ttitle:h_china ?= { is_ai = yes }\n}\n")
		writeMod(t, base, "game/common/script_values/00_v.txt",
			"region_counter_value = {\n\ttitle:h_china ?= { value = 1 }\n}\n")
		c := scanCK3(t, base)
		for _, want := range []struct{ kind, key string }{
			{"scripted_effect", "copy_pillars_effect"},
			{"scripted_triggers", "hegemon_favors_advancement_trigger"},
			{"script_values", "region_counter_value"},
		} {
			if !findDef(c.Defs, want.kind, want.key) {
				t.Errorf("%s %s dropped as a wrapper", want.kind, want.key)
			}
		}
	})

	// A wrong install path is a failure, not an empty game: walkClassified discards its walk error, so without the guard Scan returned a 0-def cache with a fresh ScannedAt, SaveCache wrote it over the good one, and CacheStale then read false forever.
	t.Run("an empty script root is an error", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			root func(t *testing.T) string
		}{
			{"path does not exist", func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "no-such-install")
			}},
			{"path exists but holds no script", func(t *testing.T) string {
				return t.TempDir()
			}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				_, _, err := Scan(context.Background(), ScanRequest{
					GameID: "ck3", InstallID: "scan-empty-test",
					InstallPath: tt.root(t), Version: "latest",
				})
				if err == nil {
					t.Fatal("want an error, got a cache — an empty scan would overwrite the good one")
				}
				if !strings.Contains(err.Error(), "check the install path") {
					t.Fatalf("error should name the cause, got %v", err)
				}
			})
		}
	})

	// ReadSchemaStatus backs the Workspace Health strip, so it must answer without loading the model: health is polled, and script_docs is a couple of megabytes against the model's ninety.
	t.Run("schema status is side-effect free", func(t *testing.T) {
		install := t.TempDir()
		docs := filepath.Join(install, "logs")
		if err := os.MkdirAll(docs, 0o755); err != nil {
			t.Fatal(err)
		}
		const effects = "Effect Documentation:\n\n----\n\n" +
			"add_gold - Adds gold to the scoped character\nSupported Scopes: character\n\n----\n"
		if err := os.WriteFile(filepath.Join(docs, "effects.log"), []byte(effects), 0o644); err != nil {
			t.Fatal(err)
		}
		pinUserData(t, install, install)

		st := ReadSchemaStatus("ck3", install)
		if st.Source != declared.SchemaLive {
			t.Fatalf("source = %q, want live", st.Source)
		}
		if st.Effects == 0 {
			t.Errorf("no effects counted: %+v", st)
		}
		if st.ReadAt == "" {
			t.Error("ReadAt empty; staleness cannot be judged")
		}
		if st.Archived {
			t.Error("reading status must not create an archive")
		}
		// With no dumps the strip must say so plainly rather than report a half-loaded type system.
		if err := os.RemoveAll(docs); err != nil {
			t.Fatal(err)
		}
		gone := ReadSchemaStatus("ck3", install)
		if gone.Source != declared.SchemaMissing || gone.Effects != 0 {
			t.Errorf("no dumps: %+v", gone)
		}
		if gone.Folder == "" {
			t.Error("the walkthrough needs a folder to point at even when nothing is there")
		}
	})
}

// writeEventTargets drops an event_targets.log into a synthetic install's user-data folder, so a test can exercise the declared prefix table.
func writeEventTargets(t *testing.T, userData, sub, body string) {
	t.Helper()
	dir := filepath.Join(userData, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "event_targets.log"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ClassifyRel decides what the walk does with a file. The rel path matters as much as the name: engine bookkeeping sits loose at the script root, and real script always sits in a content folder.
func TestClassifyRel(t *testing.T) {
	for _, tt := range []struct {
		rel, name, want string
	}{
		{"common/x/_traits.info", "_traits.info", "info"},
		// EU5 writes 23 of its 32 .info files with no leading underscore -- that prefix is a CK3 convention, not part of the format.
		{"common/x/00_cultures.info", "00_cultures.info", "info"},
		{"common/x/ai_diplochance.info", "ai_diplochance.info", "info"},
		{"common/x/readme.txt", "readme.txt", "info"},
		{"common/x/__readme.txt", "__readme.txt", "info"},
		{"common/x/____Info.txt", "____Info.txt", "info"},
		{"common/x/notes.md", "notes.md", "info"},
		{"common/x/00_traits.txt", "00_traits.txt", "script"},
		{"common/x/_default.txt", "_default.txt", ""},
		{"common/x/_hardcoded.txt", "_hardcoded.txt", ""},
		{"common/x/_advances_template.txt", "_advances_template.txt", ""},
		// Loose .txt at the script root is engine bookkeeping: parsing checksum_manifest.txt as Jomini produced fire edges for "common", "events" and "history".
		{"checksum_manifest.txt", "checksum_manifest.txt", ""},
		{"credits.txt", "credits.txt", ""},
		{"compound_settings.txt", "compound_settings.txt", ""},
		{`common\traits\00_traits.txt`, "00_traits.txt", "script"},
		{"events/yearly_events.txt", "yearly_events.txt", "script"},
		{"in_game/common/laws/00_laws.txt", "00_laws.txt", "script"},
	} {
		t.Run(tt.rel, func(t *testing.T) {
			if got := ClassifyRel(tt.rel, tt.name); got != tt.want {
				t.Fatalf("ClassifyRel(%q) = %q, want %q", tt.rel, got, tt.want)
			}
		})
	}
}

// A checkout linked into mod/ is the standard Linux layout; skipped where the OS refuses the link.
func TestWalkTreeEntersSymlinkedDirs(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(t.TempDir(), "checkout", "common", "traits")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "00_a.txt"), []byte("a = {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(real), filepath.Join(root, "common")); err != nil {
		t.Skipf("cannot symlink here: %v", err)
	}
	if err := os.Symlink(root, filepath.Join(real, "loop")); err != nil {
		t.Skipf("cannot symlink here: %v", err)
	}
	var got []string
	walkClassified(root, func(kind string, f fileRef) {
		got = append(got, filepath.ToSlash(f.rel))
	})
	want := []string{"common/traits/00_a.txt"}
	if !slices.Equal(got, want) {
		t.Fatalf("walk through symlink = %v, want %v", got, want)
	}
}

// writeMod writes one file under root, creating its folders.
func writeMod(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasEdge(edges []Edge, from, to, via string) bool {
	for _, e := range edges {
		if e.From == from && e.To == to && e.Via == via {
			return true
		}
	}
	return false
}

// TestWorkspaceAppliesInstallSchema is the two halves a workspace inherits: the
// install's nested shapes survive the build, and its join marks harvest a mod's
// own rows at a cell the install wrote.
func TestWorkspaceAppliesInstallSchema(t *testing.T) {
	v := NewIndex("ck3")
	v.Declared = &declared.Schema{
		Scopes: map[string]declared.ScopeType{"faith": {}},
		Links:  map[string]declared.ScopeLink{"faith": {Global: true, Data: true, Out: "faith"}},
	}
	v.Schema.Nested = []NestedShape{{ParentKind: "religions", ChildKind: "faith", GroupKey: "faiths"}}
	v.Schema.at("religions", "", "faiths").Names = true
	v.Schema.at("religions", "", "faiths").Def = "faith"

	root := t.TempDir()
	writeMod(t, root, "descriptor.mod", "name = \"m\"\n")
	writeMod(t, root, "common/religion/religions/00_mod.txt",
		"mod_religion = {\n\tfaiths = {\n\t\tmod_faith = {\n\t\t\tcolor = { 1 2 3 }\n\t\t}\n\t}\n}\n")
	w, err := BuildWorkspace(context.Background(), "ck3",
		[]ModInput{{Origin: "mod", Root: root, Name: "M"}}, v)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Schema.Nested; len(got) != 1 || got[0].ChildKind != "faith" {
		t.Errorf("a workspace must keep the install's shapes, got %+v", got)
	}
	if !findDef(w.Defs, "faith", "mod_faith") {
		t.Errorf("the install's join mark must harvest the mod's row; defs=%v", w.Defs)
	}
}
