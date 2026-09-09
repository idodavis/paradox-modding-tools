// scan_test.go builds a tiny CK3-shaped install in a temp dir and asserts the
// corpus fixture (defs, structures, vocabulary, loc, field docs) populates with
// no script_docs present, then that a script_docs dump enriches classification.

package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/parser/jomini"
)

// ck3Install writes the shared corpus fixture and returns install + isolated user data.
func ck3Install(t *testing.T) (base, userData string) {
	t.Helper()
	base = t.TempDir()
	userData = filepath.Join(base, "userdata")
	if err := os.MkdirAll(userData, 0o755); err != nil {
		t.Fatal(err)
	}
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

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func findDef(defs []Def, kind, key string) bool {
	for _, d := range defs {
		if d.Kind == kind && d.Key == key {
			return true
		}
	}
	return false
}

func scanCK3(t *testing.T, base string) (*VanillaCache, *VanillaLoc) {
	t.Helper()
	t.Cleanup(func() { _ = DropVanillaFiles("inst") })
	c, vloc, err := Scan(context.Background(), ScanRequest{
		InstallID: "inst", GameID: "ck3", InstallPath: base,
		Version: "1.0", LocLang: "english",
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return c, vloc
}

func TestScanCorpusFixture(t *testing.T) {
	base, userData := ck3Install(t)
	c, vloc := scanCK3(t, base)

	for _, d := range [][2]string{
		{"traits", "brave"}, {"event", "test.1"}, {"scripted_effect", "my_inline"},
	} {
		if !findDef(c.Defs, d[0], d[1]) {
			t.Errorf("missing %s %s; defs=%v", d[0], d[1], c.Defs)
		}
	}
	var fire bool
	for _, e := range c.Edges {
		if e.From == "test.1" && e.To == "test.2" && e.Via == "trigger_event" {
			fire = true
		}
	}
	if !fire {
		t.Errorf("missing fire edge test.1→test.2; edges=%v", c.Edges)
	}

	if got := vloc.Sites["test.1.t"].Value; got != "Test Event" {
		t.Errorf("loc[test.1.t] = %q, want %q", got, "Test Event")
	}
	if c.FieldInfo["category"] == "" {
		t.Errorf("fieldDocs[category] empty, want prose from _traits.info")
	}
	hk := vloc.Sites["header_key"]
	if hk.Value != "From header" || hk.Path == "" {
		t.Errorf("header_key = %+v", hk)
	}
	if _, ok := vloc.Sites["french_only"]; ok {
		t.Errorf("french loc in default sidecar")
	}
	// A scan writes only the workspace language and english; other languages
	// are harvested on first use, which is what keeps the cache small.
	if _, err := LoadVanillaLoc("inst", "1.0", "french"); err == nil {
		t.Error("french sidecar written at scan time; only default + english should be")
	}
	fr, err := HarvestLoc(context.Background(), "ck3", base, "french")
	if err != nil {
		t.Fatalf("lazy french harvest: %v", err)
	}
	if got := fr.Sites["french_only"].Value; got != "Bonjour" {
		t.Errorf("french loc[french_only] = %q", got)
	}
	if got := c.FieldInfoByKind["event"]["type"]; !strings.Contains(got, "presentation") {
		t.Errorf("fieldDocsByKind[event][type] = %q, want events.info prose", got)
	}
	if got := c.FieldInfoByKind["event"]["title"]; !strings.Contains(got, "Dynamic") {
		t.Errorf("fieldDocsByKind[event][title] = %q, want events.info prose", got)
	}

	for _, k := range []string{"category", "opposites", "ai_boldness"} {
		if !contains(c.Structures["traits"], k) {
			t.Errorf("structures[traits] missing %q: %v", k, c.Structures["traits"])
		}
	}
	for _, k := range []string{"immediate", "option"} {
		if !contains(c.Structures["event"], k) {
			t.Errorf("structures[event] missing %q: %v", k, c.Structures["event"])
		}
	}
	if got := c.Structures["event"]; len(got) == 0 || got[0] != "type" {
		t.Errorf("structures[event] freq = %v, want type first", got)
	}
	if !contains(c.StructureBlocks["event"], "immediate") ||
		!contains(c.StructureBlocks["event"], "option") {
		t.Errorf("structureBlocks[event] = %v", c.StructureBlocks["event"])
	}
	if contains(c.StructureBlocks["event"], "type") {
		t.Errorf("type should be scalar: %v", c.StructureBlocks["event"])
	}

	// Corpus vocabulary must be populated with NO script_docs dump.
	if !contains(c.Vocabulary, "add_gold") {
		t.Errorf("vocabulary missing corpus token add_gold: %v", c.Vocabulary)
	}
	// Effects and triggers are the game's declared API, so with no script_docs
	// there is nothing to claim. Inferring them from position elected every
	// $PARAM$ site as an effect.
	if len(c.TokenNames("effect")) != 0 || len(c.TokenNames("trigger")) != 0 {
		t.Errorf("engine tokens claimed without dumps: effects=%v triggers=%v",
			c.TokenNames("effect"), c.TokenNames("trigger"))
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
	for _, r := range c.CallRefs {
		if r.Key == "my_trig" && jomini.CanonicalKind(r.Kind) == "scripted_triggers" {
			gotCall = true
		}
	}
	if !gotParam || !gotCall {
		t.Errorf("vanilla $NAME$/call persist param=%v call=%v refs=%v",
			gotParam, gotCall, c.CallRefs)
	}

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
			logs := filepath.Join(userData, "logs")
			if err := os.MkdirAll(logs, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(logs, "effects.log"), []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			got, _ := scanCK3(t, base)
			if _, ok := got.Schema.Effects[tt.want]; !ok {
				t.Errorf("effects missing %s: %v", tt.want, got.TokenNames("effect"))
			}
			if tt.name == "enrichment" && got.Schema.Doc("add_gold") == "" {
				t.Error("schema doc for add_gold empty after enrichment")
			}
			if tt.drop != "" {
				if _, ok := got.Schema.Effects[tt.drop]; ok {
					t.Fatalf("nested dump field %s leaked: %v", tt.drop, got.TokenNames("effect"))
				}
			}
		})
	}
}

// ClassifyRel decides what the walk does with a file. The rel path matters as
// much as the name: engine bookkeeping sits loose at the script root, and real
// script always sits in a content folder.
func TestClassifyRel(t *testing.T) {
	for _, tt := range []struct {
		rel, name, want string
	}{
		{"common/x/_traits.info", "_traits.info", "info"},
		// EU5 writes 23 of its 32 .info files with no leading underscore --
		// that prefix is a CK3 convention, not part of the format.
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
		// Loose .txt at the script root is engine bookkeeping, not script.
		// Parsing checksum_manifest.txt as Jomini produced fire edges for
		// "common", "events" and "history".
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

func TestScanEU5ShippedDocs(t *testing.T) {
	base := t.TempDir()
	writeMod(t, base, "game/in_game/common/traits/00_traits.txt", `brave = {
	category = ruler
	allow = { always = yes }
}
`)
	writeMod(t, base, "game/in_game/common/traits/_traits.info", `trait_name = {
	# Which type of character can use this trait?
	category = ruler
	allow = {
		# What else does the character need
	}
}
`)
	writeMod(t, base, "game/in_game/common/building_types/readme.txt", `# ATTRIBUTES
# - build_time: <integer> building time in days
# - allow: <trigger> can the building be built
`)
	pinUserData(t, base, filepath.Join(t.TempDir(), "ud"))
	t.Cleanup(func() { _ = DropVanillaFiles("eu5docs") })
	c, _, err := Scan(context.Background(), ScanRequest{
		InstallID: "eu5docs", GameID: "eu5", InstallPath: base,
		Version: "1.0", LocLang: "english",
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := c.FieldInfoByKind["traits"]["allow"]; !strings.Contains(got, "character need") {
		t.Fatalf("traits allow doc = %q", got)
	}
	if got := c.FieldInfoByKind["traits"]["category"]; !strings.Contains(got, "Which type") {
		t.Fatalf("traits category doc = %q", got)
	}
	if got := c.FieldInfoByKind["building_types"]["build_time"]; !strings.Contains(got, "building time") {
		t.Fatalf("building_types build_time = %q", got)
	}
}

func TestScanEU5InstallUsesGameFolder(t *testing.T) {
	base := t.TempDir()
	writeMod(t, base, "game/in_game/events/x.txt", `namespace = t
t.1 = { type = country_event }
`)
	writeMod(t, base, "in_game/events/wrong.txt", `namespace = w
w.1 = { type = country_event }
`)
	pinUserData(t, base, filepath.Join(t.TempDir(), "ud"))
	t.Cleanup(func() { _ = DropVanillaFiles("eu5inst") })
	c, _, err := Scan(context.Background(), ScanRequest{
		InstallID: "eu5inst", GameID: "eu5", InstallPath: base,
		Version: "1.0", LocLang: "english",
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !findDef(c.Defs, "event", "t.1") {
		t.Fatalf("missing install event t.1; defs=%v", c.Defs)
	}
	if findDef(c.Defs, "event", "w.1") {
		t.Fatalf("scanned install-root in_game, not game/: %v", c.Defs)
	}
}

// TestScanScriptDocsFixtures runs each game's committed dump fixture end to end
// through Scan and asserts it reaches the cache as a parsed Schema: the token is
// declared, the fixture's own section header is not mistaken for one, and the
// game's prose and scopes survive. One table, because the contract is identical
// across the three games — only the dump syntax and the folder differ.
func TestScanScriptDocsFixtures(t *testing.T) {
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
			if c.Schema == nil {
				t.Fatal("scan produced no schema")
			}
			if _, ok := c.Schema.Effects[tt.want]; !ok {
				t.Fatalf("effects missing %s: %v", tt.want, c.TokenNames("effect"))
			}
			// Every fixture opens with an "Effect Documentation" style header. A
			// splitter that reads one as a block name declares "effect" itself as
			// an effect, which then leads the completion list.
			if _, ok := c.Schema.Effects["effect"]; ok {
				t.Error("section header parsed as an effect")
			}
			if tt.doc != "" && !strings.Contains(c.Schema.Doc(tt.want), tt.doc) {
				t.Errorf("Doc(%s) = %q, want it to mention %q",
					tt.want, c.Schema.Doc(tt.want), tt.doc)
			}
			if tt.scope != "" && !strings.Contains(c.Schema.ScopeText(tt.want), tt.scope) {
				t.Errorf("ScopeText(%s) = %q, want %q",
					tt.want, c.Schema.ScopeText(tt.want), tt.scope)
			}
			if tt.modDoc != "" && c.Schema.Doc(tt.modDoc) == "" {
				t.Errorf("modifier doc for %s is empty", tt.modDoc)
			}
		})
	}
}

func TestScanStageRoots(t *testing.T) {
	base := t.TempDir()
	writeMod(t, base, "game/in_game/common/traits/a.txt", "brave = { category = ruler }\n")
	writeMod(t, base, "game/main_menu/common/static_modifiers/b.txt", "mod_a = { diplomacy = 1 }\n")
	writeMod(t, base, "game/loading_screen/common/defines/c.txt", "NGame = { DAYS = 1 }\n")
	pinUserData(t, base, filepath.Join(t.TempDir(), "ud"))
	t.Cleanup(func() { _ = DropVanillaFiles("stages") })
	c, _, err := Scan(context.Background(), ScanRequest{
		InstallID: "stages", GameID: "eu5", InstallPath: base,
		Version: "1.0", LocLang: "english",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !findDef(c.Defs, "traits", "brave") ||
		!findDef(c.Defs, "static_modifiers", "mod_a") ||
		!findDef(c.Defs, "defines", "NGame") {
		t.Fatalf("stage defs=%v", c.Defs)
	}
}

// writeEventTargets drops an event_targets.log into a synthetic install's
// user-data folder, so a test can exercise the declared prefix table.
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

func TestScanEU5PrefixBinding(t *testing.T) {
	base := t.TempDir()
	writeMod(t, base, "game/in_game/map_data/location_templates.txt",
		"stockholm = { province = 1 }\nuppsala = { province = 2 }\nlund = { province = 3 }\n")
	writeMod(t, base, "game/in_game/common/countries/x.txt",
		"SWE = { color = { 1 2 3 } }\nDAN = { color = { 1 2 3 } }\nNOR = { color = { 1 2 3 } }\n")
	// Each prefix needs at least minBindCites citations before it binds; one
	// coincidental key match is not evidence.
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
	// The prefix table is read from the game's declared scope links, so the
	// test must supply them the way script_docs does.
	writeEventTargets(t, ud, "docs", `# Event Target Documentation
### location
Get the location with the specified key
Requires Data: yes
Global Link: yes
Output Scopes: location

### c
Get the country with the specified tag
Requires Data: yes
Global Link: yes
Output Scopes: country
`)
	t.Cleanup(func() { _ = DropVanillaFiles("eu5votes") })
	c, _, err := Scan(context.Background(), ScanRequest{
		InstallID: "eu5votes", GameID: "eu5", InstallPath: base,
		Version: "1.0", LocLang: "english",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.PrefixKinds["location"] == "" && c.PrefixKinds["c"] == "" {
		t.Fatalf("prefix kinds empty: %v", c.PrefixKinds)
	}
	if c.FireKeys["trigger_event_silently"] != "event" {
		t.Fatalf("fire keys=%v", c.FireKeys)
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
}

func TestScanVic3Wrapper(t *testing.T) {
	base := t.TempDir()
	writeMod(t, base, "game/common/country_definitions/x.txt",
		"USA = { color = { 1 2 3 } }\n")
	writeMod(t, base, "game/common/history/countries.txt",
		"COUNTRIES = { c:USA ?= { } }\n")
	pinUserData(t, base, filepath.Join(t.TempDir(), "ud"))
	t.Cleanup(func() { _ = DropVanillaFiles("vic3wrap") })
	c, _, err := Scan(context.Background(), ScanRequest{
		InstallID: "vic3wrap", GameID: "vic3", InstallPath: base,
		Version: "1.0", LocLang: "english",
	})
	if err != nil {
		t.Fatal(err)
	}
	if findDef(c.Defs, "countries", "COUNTRIES") || findDef(c.Defs, "history", "COUNTRIES") {
		t.Fatalf("wrapper harvested: %v", c.Defs)
	}
	if !findDef(c.Defs, "country_definitions", "USA") {
		t.Fatalf("missing USA: %v", c.Defs)
	}
}

// A `keyword name = { }` pair is a declaration, not a wrapper, however its body
// is shaped. CK3 writes inline macros this way and their bodies are frequently
// nothing but typed cites — exactly the wrapper shape. Matching on shape alone
// deleted 28 vanilla CK3 macros from the model and broke go-to-definition on
// every one of them.
func TestKeywordDeclaredBlockIsNotAWrapper(t *testing.T) {
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
	c, _ := scanCK3(t, base)
	if !findDef(c.Defs, "scripted_effect", "copy_pillars_effect") {
		t.Errorf("inline macro dropped as a wrapper; defs carrying that key: %v",
			defsWithKey(c.Defs, "copy_pillars_effect"))
	}
	if contains(c.Wrappers, "copy_pillars_effect") {
		t.Errorf("keyword-declared macro elected as a wrapper: %v", c.Wrappers)
	}
}

// A scripted_* / script_values folder has already declared that its top-level
// keys are definitions, and those bodies are routinely a single typed cite —
// CK3's `hegemon_favors_advancement_trigger = { title:h_china.holder ?= {…} }`
// is exactly the Vic3 wrapper shape. The folder rule must win over the shape.
func TestNamedBodyFolderIsNeverAWrapper(t *testing.T) {
	base, _ := ck3Install(t)
	writeMod(t, base, "game/common/landed_titles/00_landed_titles.txt",
		"h_china = { color = { 1 2 3 } }\n")
	writeMod(t, base, "game/common/scripted_triggers/00_t.txt",
		"hegemon_favors_advancement_trigger = {\n\ttitle:h_china ?= { is_ai = yes }\n}\n")
	writeMod(t, base, "game/common/script_values/00_v.txt",
		"region_counter_value = {\n\ttitle:h_china ?= { value = 1 }\n}\n")
	c, _ := scanCK3(t, base)
	for _, want := range []struct{ kind, key string }{
		{"scripted_triggers", "hegemon_favors_advancement_trigger"},
		{"script_values", "region_counter_value"},
	} {
		if !findDef(c.Defs, want.kind, want.key) {
			t.Errorf("%s %s dropped as a wrapper; wrappers=%v", want.kind, want.key, c.Wrappers)
		}
	}
	if len(c.Wrappers) != 0 {
		t.Errorf("named-body folders elected wrappers: %v", c.Wrappers)
	}
}
func defsWithKey(defs []Def, key string) []Def {
	var out []Def
	for _, d := range defs {
		if d.Key == key {
			out = append(out, d)
		}
	}
	return out
}

func TestScanVic3Prefixes(t *testing.T) {
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
	// `p:` is declared but its ids are engine hex, not database rows, so it must
	// stay unbound even though the game lists it as a link.
	writeEventTargets(t, ud, "docs", `# Event Target Documentation
### c
Requires Data: yes
Global Link: yes
Output Scopes: country

### cu
Requires Data: yes
Global Link: yes
Output Scopes: culture

### bt
Requires Data: yes
Global Link: yes
Output Scopes: building_type

### je
Requires Data: yes
Global Link: yes
Output Scopes: journal_entry

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
	if c.PrefixKinds["c"] == "" || c.PrefixKinds["cu"] == "" ||
		c.PrefixKinds["bt"] == "" || c.PrefixKinds["je"] == "" {
		t.Fatalf("prefix kinds=%v", c.PrefixKinds)
	}
	if c.PrefixKinds["p"] != "" {
		t.Fatalf("p:hex must stay ephemeral: %v", c.PrefixKinds)
	}
	if findDef(c.Defs, "countries", "COUNTRIES") || findDef(c.Defs, "history", "COUNTRIES") {
		t.Fatalf("wrapper harvested: %v", c.Defs)
	}
	if !findDef(c.Defs, "country_definitions", "USA") {
		t.Fatalf("missing USA: %v", c.Defs)
	}
}

// TestParseDumpFormats covers the header shapes the three games emit, including
// the two Vic3 modifier layouts (it changed format between game versions).
func TestParseDumpFormats(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("effects.log", "### abandon_revolution\nLeaves the revolution\n")
	write("modifiers.log", "Tag: country_prestige_add, Categories: Country,\nUse areas: country\n")
	s := ReadSchema([]string{dir})
	if s == nil {
		t.Fatal("nil schema")
	}
	if _, ok := s.Effects["abandon_revolution"]; !ok {
		t.Errorf("### heading: %+v", s.Effects)
	}
	if _, ok := s.Modifiers["country_prestige_add"]; !ok {
		t.Errorf("Tag: header: %+v", s.Modifiers)
	}

	old := t.TempDir()
	if err := os.WriteFile(filepath.Join(old, "modifiers.log"),
		[]byte("--- Static modifier types ---\n    key: building_throughput_add\n"+
			"    Name: aut!Throughput\n    Description: More goods\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := ReadSchema([]string{old})
	if v == nil {
		t.Fatal("nil schema for indented key: form")
	}
	m, ok := v.Modifiers["building_throughput_add"]
	if !ok || m.Name != "Throughput" {
		t.Fatalf("indented key: form = %+v", v.Modifiers)
	}

	if kindFromDocFilename("on_actions.log") != "on_action" {
		t.Fatal("on_actions kind")
	}
}

// covers harvest-site decisions about definitions and the prose
// attached to them.
// TestLeadingCommentKeepsLineBreaks pins that an author's line breaks survive.
// The game's own dump prose is wrapped for a console and reflows correctly, but
// a comment a modder wrote above a definition chose where its lines end — a
// list or a worked example is destroyed by folding it into one run.
func TestLeadingCommentKeepsLineBreaks(t *testing.T) {
	src := "# Grants the title.\n" +
		"# Requires:\n" +
		"#   - a living holder\n" +
		"#   - an existing de jure liege\n" +
		"grant_title = {\n\tvalue = 1\n}\n"
	ex := extractCK3("common/script_values/x.txt", src, "mod")
	var doc string
	for _, d := range ex.Defs {
		if d.Key == "grant_title" {
			doc = d.Doc
		}
	}
	if doc == "" {
		t.Fatalf("no doc harvested from %d defs", len(ex.Defs))
	}
	lines := strings.Split(doc, "\n")
	if len(lines) != 4 {
		t.Fatalf("doc = %q, want 4 lines", doc)
	}
	// Each line is still normalised internally, so alignment padding does not
	// survive — only the break the author chose does.
	if lines[2] != "- a living holder" {
		t.Errorf("line 3 = %q, want the list item with its indent normalised",
			lines[2])
	}
}

// TestKeywordDeclarationLocality pins which declaration keywords produce a
// file-local definition. Vanilla CK3 declares 3,809 inline scripted_* bodies
// and every one sits under events/, beside the event that calls it; the engine
// resolves them per file, so a repeat in another file is a separate macro.
func TestKeywordDeclarationLocality(t *testing.T) {
	for _, tt := range []struct {
		src   string
		kind  string
		local bool
	}{
		{"scripted_trigger foo_trigger = {\n\tis_ai = no\n}\n", "scripted_trigger", true},
		{"scripted_effect foo_effect = {\n\tadd_gold = 1\n}\n", "scripted_effect", true},
		{"local_template Bar = {\n\tx = 1\n}\n", "gui_type", true},
		// A GUI type is visible to every .gui file; that is the point of one.
		{"type Baz = {\n\tx = 1\n}\n", "gui_type", false},
		{"template Qux = {\n\tx = 1\n}\n", "gui_type", false},
	} {
		t.Run(tt.kind+"/"+strconv.FormatBool(tt.local), func(t *testing.T) {
			res := jomini.Parse(tt.src)
			defs, _, _ := extractKeywordName(res.Root, res.Lines(), "a.txt", "")
			if len(defs) != 1 {
				t.Fatalf("got %d defs, want 1", len(defs))
			}
			if defs[0].Kind != tt.kind {
				t.Errorf("kind = %q, want %q", defs[0].Kind, tt.kind)
			}
			if defs[0].Local != tt.local {
				t.Errorf("Local = %v, want %v", defs[0].Local, tt.local)
			}
		})
	}
}

// TestScanRejectsEmptyScriptRoot pins the guard that a wrong install path is a
// failure, not an empty game.
//
// walkClassified discards its walk error, so a missing or mis-pointed install
// arrives as an empty inventory rather than an error. Without the guard, Scan
// returned a 0-def cache with a fresh ScannedAt, SaveCache wrote it over the
// good one, and CacheStale then read false forever — nothing would ever
// re-trigger. The memory note "install env is the game root" is this bug.
func TestScanRejectsEmptyScriptRoot(t *testing.T) {
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
}
