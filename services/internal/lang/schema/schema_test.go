// schema_test.go covers the schema file's round trip and what it refuses, info prose and declared
// types, the reader, the folder lookup, the name-token table and the script_docs dumps; two gates
// walk the real installs and their dumps.
package schema

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/source"
)

// sample is a schema holding every field of every type and every spelling the writer must quote.
func sample() *Schema {
	return &Schema{
		Game:    "ck3",
		Version: "1.19.0.6",
		Kinds: map[string]*Kind{
			"game_rules": {
				Name: "game_rules", Format: source.Script, Folders: []string{"common/game_rules"}, Naming: Naming{Form: "key"},
				LocTemplates: []loc.Convention{{Template: "rule_{id}", Coverage: 1}},
				Shape: &Field{Count: 81, Class: ClassStructure, Defines: "game_rules.setting", Members: map[string]*Field{
					"categories": {Name: "categories", Count: 81, Class: ClassList, Accepts: []Accept{{Type: TypeRef, Kind: "game_rule_categories", Count: 243}}},
					"default":    {Name: "default", Count: 81, Class: ClassValue, Accepts: []Accept{{Type: TypeRef, Kind: "game_rules.setting", Count: 81}}},
				}},
			},
			"game_rules.setting": {
				Name: "game_rules.setting", Format: source.Script, Parent: "game_rules", Naming: Naming{Form: "key"},
				LocTemplates: []loc.Convention{{Template: "setting_{id}", Coverage: 1}, {Template: "setting_{id}_desc", Coverage: 38.0 / 39.0}},
				Shape: &Field{Count: 312, Class: ClassStructure, Members: map[string]*Field{
					"flag": {Name: "flag", Count: 36, Class: ClassValue, Accepts: []Accept{
						{Type: TypeEnum, Enum: []string{
							"blocks_achievements", "two words", "traits", "game_rules.setting", "history/titles", "ref:characters", "define:NGame|START", "1.19.0.6", "x-y", "Linnéa",
							"", "<date>", ">=", "?=", "trait_{id}", "a#b", "a=b", "@x", "$X$",
						}, Count: 0, Declared: true},
						{Type: TypeBool, Count: 4, Declared: true},
						{Type: TypeNumber, Count: 2},
					}},
				}},
			},
			"history/titles": {
				Name: "history/titles", Format: source.Script, Folders: []string{"history/titles"}, Naming: Naming{Form: "key"}, Subject: "landed_titles",
				Shape: &Field{Count: 5901, Class: ClassStructure, Members: map[string]*Field{
					"<date>": {Name: "<date>", Count: 92364, Class: ClassStructure, Members: map[string]*Field{
						"holder": {Name: "holder", Count: 60112, Class: ClassValue, Ops: []string{">=", "?="}, Accepts: []Accept{{Type: TypeRef, Kind: "characters", Count: 58240}, {Type: TypeNumber, Count: 1872}}},
						"effect": {Name: "effect", Count: 12, Class: ClassBody, Body: BodyEffect | BodyMath, BodyScope: "landed_title"},
					}},
				}},
			},
			"map_data.definition": {
				Name: "map_data.definition", Format: source.Table, Folders: []string{"map_data/definition.csv"}, Naming: Naming{Form: "column", Column: 0},
				Prefixes: []string{"province"}, ScopeType: "province",
				Shape: &Field{Count: 9000, Class: ClassStructure, Members: map[string]*Field{"<4>": {Name: "<4>", Count: 9000, Class: ClassValue, Accepts: []Accept{{Type: TypeString, Count: 9000}}}}},
			},
			"models.pdxmesh": {
				Name: "models.pdxmesh", Format: source.Script, Folders: []string{"gfx/models"}, Naming: Naming{Form: "member", Keyword: "pdxmesh", Member: "name"},
				Shape: &Field{Count: 2344, Class: ClassStructure, Members: map[string]*Field{"file": {Name: "file", Count: 2344, Class: ClassValue, Accepts: []Accept{{Type: TypeFile, Count: 2344}}}}},
			},
			"models.entity": {Name: "models.entity", Format: source.Script, Folders: []string{"gfx/models"}, Naming: Naming{Form: "member", Keyword: "entity", Member: "name"}},
			"saved_scopes":  {Name: "saved_scopes", Format: source.Script, Naming: Naming{Form: "token"}, Prefixes: []string{"scope"}},
			"scripted_effects": {
				Name: "scripted_effects", Format: source.Script, Folders: []string{"common/scripted_effects"}, Naming: Naming{Form: "key", Keyword: "scripted_effect"},
				Macro: BodyEffect, Params: true, Shape: &Field{Count: 900, Class: ClassBody, Body: BodyEffect},
			},
			"traits": {
				Name: "traits", Format: source.Script, Folders: []string{"common/traits"}, Naming: Naming{Form: "key"}, Prefixes: []string{"trait"}, ScopeType: "trait", LabelMember: "name",
				FileTemplates: []FileTemplate{{Pattern: "gfx/interface/icons/traits/{id}.dds", Coverage: 0.93}},
				Shape:         &Field{Count: 600, Class: ClassStructure, Members: map[string]*Field{"goods": {Name: "goods", Count: 3, Class: ClassDatabase, KeyedBy: "goods"}}},
			},
		},
		Arguments: map[string]*Field{
			"add_trait": {Name: "add_trait", Count: 4100, Class: ClassValue, Accepts: []Accept{{Type: TypeRef, Kind: "traits", Count: 4000}, {Type: TypeScopePath, Count: 100}}},
			"if":        {Name: "if", Count: 90000, Class: ClassBody, Body: BodyEffect, Members: map[string]*Field{"limit": {Name: "limit", Count: 89000, Class: ClassBody, Body: BodyTrigger}}},
		},
		Drift: Drift{
			ParseErrors: []Tally{{Name: "common/bad file.txt", Count: 2}},
			Prefixes:    []Tally{{Name: "oddity", Count: 7}, {Name: "other", Count: 1}},
			NameTokens:  []Tally{{Name: "set_thing", Count: 3}},
		},
	}
}

// infoOf writes files, by path from a root, into a fresh folder and reads them as an inventory.
func infoOf(t *testing.T, files map[string]string) Info {
	t.Helper()
	root := t.TempDir()
	var inventory []source.File
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		text := files[rel]
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		inventory = append(inventory, source.File{Path: path, Rel: rel, Origin: "vanilla", Format: source.Classify(path, rel)})
	}
	return ReadInfo(inventory)
}

// found is a Defined over three sets: definitions as kind/name, loc keys, and files.
type found struct{ defs, locs, files map[string]bool }

func (found found) Def(kind, name string) bool { return found.defs[kind+"/"+name] }
func (found found) Loc(key string) bool        { return found.locs[key] }
func (found found) File(name string) bool      { return found.files[name] }

// reader is a Reader over a small game: prefixed kinds, macro kinds and a small vocabulary.
func reader() Reader {
	kind := func(name string, prefixes ...string) *Kind { return &Kind{Name: name, Prefixes: prefixes} }
	return Reader{
		Schema: &Schema{Kinds: map[string]*Kind{
			"characters": kind("characters", "character"), "landed_titles": kind("landed_titles", "title"), "traits": kind("traits", "trait"),
			"defines.entry": kind("defines.entry", "define"), "saved_scopes": kind("saved_scopes", "scope"),
			"definitions.area": kind("definitions.area", "geo"), "definitions.region": kind("definitions.region", "geo"),
			"scripted_effects":  {Name: "scripted_effects", Macro: BodyEffect},
			"scripted_triggers": {Name: "scripted_triggers", Macro: BodyTrigger},
			"scripted_lists":    {Name: "scripted_lists", Macro: BodyTrigger | BodyEffect},
		}},
		Vocab: &Vocab{
			Effects:   map[string]EngineToken{"add_gold": {}, "if": {}, "every_vassal": {}},
			Triggers:  map[string]EngineToken{"has_trait": {}, "and": {}, "not": {}, "any_vassal": {}},
			Modifiers: map[string]Modifier{"monthly_income": {}},
			Links: map[string]Link{
				"root": {}, "liege": {Out: "character"}, "culture": {Out: "culture", Prefix: true}, "character": {Prefix: true}, "title": {Prefix: true}, "scope": {Prefix: true}, "flag": {Prefix: true},
			},
		},
		Found: found{
			defs: map[string]bool{
				"characters/186004": true, "characters/168673": true, "landed_titles/k_france": true, "traits/brave": true, "defines.entry/START_DATE": true, "definitions.region/west": true,
				"scripted_effects/my_effect": true, "scripted_triggers/my_trigger": true, "scripted_lists/adult_child": true, "game_rules.setting/on": true, "goods/wheat": true, "cultures/norse": true,
			},
			locs:  map[string]bool{"my_title": true},
			files: map[string]bool{"gfx/x.dds": true, "pattern_solid.dds": true, "music/x.ogg": true},
		},
	}
}

// dumps writes dump files into a fresh folder and reads the folder.
func dumps(t *testing.T, files map[string]string) *Vocab {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ReadVocab([]string{filepath.Join(dir, "missing"), dir})
}

// realVocab reads the dumps of one game, or reports why it cannot.
func realVocab(t *testing.T, id, env string) *Vocab {
	t.Helper()
	install := os.Getenv(env)
	if install == "" {
		t.Errorf("%s is not set: the gate cannot clear a game it never read", env)
		return nil
	}
	user := game.UserDataDir(id, install)
	return ReadVocab([]string{filepath.Join(user, "logs"), filepath.Join(user, "docs")})
}

func TestWriteReadRoundTrip(t *testing.T) {
	want := sample()
	text := Write(want)
	got, err := Read(text)
	if err != nil {
		t.Fatalf("Read: %v\n%s", err, text)
	}

	// the lookups are built on use and are no part of the value, so the exported fields are compared
	if got.Game != want.Game || got.Version != want.Version || !reflect.DeepEqual(got.Drift, want.Drift) {
		t.Errorf("header or drift changed: %+v %+v", got, got.Drift)
	}
	for name, kind := range want.Kinds {
		if !reflect.DeepEqual(got.Kinds[name], kind) {
			t.Errorf("kind %s changed:\n got %+v\nwant %+v", name, got.Kinds[name], kind)
		}
	}
	if len(got.Kinds) != len(want.Kinds) || !reflect.DeepEqual(got.Arguments, want.Arguments) {
		t.Errorf("kinds %d, want %d; arguments %+v", len(got.Kinds), len(want.Kinds), got.Arguments)
	}
	if again := Write(got); again != text {
		t.Errorf("a second write differs from the first:\n%s", again)
	}

	// each enum word is written bare where the lexer reads it back bare, quoted otherwise
	if spelled := "\twords = { blocks_achievements \"two words\" traits game_rules.setting history/titles ref:characters define:NGame|START 1.19.0.6 x-y Linnéa " +
		"\"\" \"<date>\" \">=\" \"?=\" \"trait_{id}\" \"a#b\" \"a=b\" \"@x\" \"$X$\" }\n"; !strings.Contains(text, spelled) {
		t.Errorf("the file does not hold %q\n%s", spelled, text)
	}

	// an empty schema writes its three blocks and reads back empty
	text = Write(&Schema{})
	if text != "schema = {\n\tgame = \"\"\n\tversion = \"\"\n}\ndrift = {\n}\narguments = {\n}\n" {
		t.Errorf("an empty schema wrote %q", text)
	}
	sch, err := Read(text)
	if err != nil || len(sch.Kinds) != 0 || len(sch.Arguments) != 0 {
		t.Errorf("Read of an empty schema = %+v, %v", sch, err)
	}
}

func TestReadRefuses(t *testing.T) {
	head := "schema = {\n\tgame = ck3\n\tversion = 1\n}\ndrift = {\n}\n"
	for name, text := range map[string]string{
		"an empty file":             "",
		"a file that is no schema":  "traits = {\n}\nbrave = {\n}\nx = {\n}\n",
		"no arguments block":        head + "traits = {\n\tnaming = {\n\t\tform = key\n\t}\n}\n",
		"an unclosed brace":         head + "traits = {\narguments = {\n}\n",
		"an unknown kind key":       head + "traits = {\n\tcolour = red\n}\narguments = {\n}\n",
		"an unknown naming form":    head + "traits = {\n\tnaming = {\n\t\tform = guess\n\t}\n}\narguments = {\n}\n",
		"an unknown class":          head + "traits = {\n\tshape = {\n\t\tcount = 1\n\t\tclass = heap\n\t}\n}\narguments = {\n}\n",
		"an unknown value type":     head + "traits = {\n\tshape = {\n\t\tcount = 1\n\t\taccepts = {\n\t\t\tsomething = 1\n\t\t}\n\t}\n}\narguments = {\n}\n",
		"an unknown body class":     head + "traits = {\n\tmacro = { spell }\n}\narguments = {\n}\n",
		"an unknown drift section":  "schema = {\n\tgame = ck3\n\tversion = 1\n}\ndrift = {\n\tweather = {\n\t\tx = 1\n\t}\n}\narguments = {\n}\n",
		"a count that is no number": head + "arguments = {\n\tadd_gold = {\n\t\tcount = many\n\t}\n}\n",
		"a bare word among kinds":   head + "traits\narguments = {\n}\n",
	} {
		if read, err := Read(text); err == nil {
			t.Errorf("%s was read: %+v", name, read)
		}
	}

	// the error names the line
	_, err := Read(head + "traits = {\n\tcolour = red\n}\narguments = {\n}\n")
	if err == nil || !strings.Contains(err.Error(), "line 8") {
		t.Errorf("the error does not name line 8: %v", err)
	}
}

func TestReadInfo(t *testing.T) {
	for name, entry := range map[string]struct {
		text, key, prose, typ string
	}{
		"brief header, prose, a lone #": {"### brief: create_cadet_branches ( bool )\n# Should rulers be able to create cadet branches\n#\ncreate_cadet_branches = no\n", "create_cadet_branches", "Should rulers be able to create cadet branches", "bool"},
		"attribute bullet":              {"# ATTRIBUTES\n# - build_time: <integer> building time in days\n", "build_time", "building time in days", "integer"},
		"colon bullet, no dash":         {"# custom_description: <string> key to a custom description of the disaster\n", "custom_description", "key to a custom description of the disaster", "string"},
		"colon bullet, no type":         {"# monthly_spawn_chance: script value for how likely the disease is to spawn\n", "monthly_spawn_chance", "script value for how likely the disease is to spawn", ""},
		"comment trailing the key":      {"#\ttype = country_event\t\t# Defines the scope\n", "type", "Defines the scope", ""},
		"type, then prose, no hash":     {"#<name> = {\n#\tbreak_on_war = <yes/no> ends the relationship on war\n#}\n", "break_on_war", "ends the relationship on war", "yes/no"},
		"type, then a comment":          {"x = <yes/no> # why it matters\n", "x", "why it matters", "yes/no"},
		"prose inside its own braces":   {"#\tshow_if = { <trigger to see if this stage is needed}\n", "show_if", "trigger to see if this stage is needed", ""},
		"a type inside braces":          {"gold = { <script value> }\n", "gold", "", "script value"},
		"comment above the key":         {"# The trait category\ncategory = personality\n", "category", "The trait category", ""},
		"below block comment":           {"allow = {\n\t# must also pass category\n}\n", "allow", "must also pass category", ""},
		"below does not steal above":    {"# Event presentation type\ntype = character_event\n# Dynamic loc key\ntitle = loc_key\n", "title", "Dynamic loc key", ""},
		"prose above survives":          {"# Event presentation type\ntype = character_event\n# Dynamic loc key\ntitle = loc_key\n", "type", "Event presentation type", ""},
		"inline plus below block":       {"chance = { # weight\n\t# extra lines\n}\n", "chance", "weight extra lines", ""},
		"a block's first key claims":    {"option = {\n\t# the name shown\n\tname = loc_key\n}\n", "name", "the name shown", ""},
		"and its block gets none":       {"option = {\n\t# the name shown\n\tname = loc_key\n}\n", "option", "", ""},
		"a blank line ends the block":   {"# File header prose about the whole format\n\nsome_key = 1\n", "some_key", "", ""},
		"a lone # ends it too":          {"# Disasters\n#\n# can_start: trigger to see if the disaster can start\n", "can_start", "trigger to see if the disaster can start", ""},
		"a placeholder body":            {"#\timmediate = { ... }\n", "immediate", "", ""},
		"digits alone are no prose":     {"period = 55 # 55\n", "period", "", ""},
		"the type is not the prose":     {"# - god = <god key> god that this avatar is a manifestation of\n", "god", "god that this avatar is a manifestation of", "god key"},
		"a bare type word":              {"    construction_points = int\t\t# points required\n", "construction_points", "points required", "int"},
		"bare yes/no":                   {"    buildable = yes/no\t# can this be built?\n", "buildable", "can this be built?", "yes/no"},
		"a bare example is no type":     {"valid_sex = all/male/female # Defaults to all\n", "valid_sex", "Defaults to all", ""},
		"a bare path is no type":        {"icon = gfx/interface/icons # the icon\n", "icon", "the icon", ""},
		"a capitalised brief header":    {"\t### Brief: type (minor|medium|major)\n\t# How big the DLC is\n\ttype = major\n", "type", "How big the DLC is", "minor|medium|major"},
		"a rule is no prose":            {"=== Structure ===\nprovince_id = { # the province\n}\n", "province_id", "the province", ""},
		"a dotted key":                  {"# an example event\nmy_events.1001 = {\n\ttype = character_event # Optional\n}\n", "my_events.1001", "an example event", ""},
		"a prose bullet is no key":      {"# - Optional: picks random entries from that set\nfallback = yes\n", "Optional", "", ""},
		"a label is no key":             {"# scope: character\n# root: Ruler (character)\n", "scope", "", ""},
		"a live colon is no key":        {"Valid categories: the ones listed below here\n", "categories", "", ""},
	} {
		info := infoOf(t, map[string]string{"common/things/_things.info": entry.text})
		if got := info.Field([]string{"common/things"}, "", entry.key); got.Prose != entry.prose || got.Type != entry.typ {
			t.Errorf("%s: %s = %+v, want prose %q type %q", name, entry.key, got, entry.prose, entry.typ)
		}
	}

	// several files of one folder, nesting, precedence between files, and the size cap
	info := infoOf(t, map[string]string{
		"common/traits/_traits.info": "\n== Loc/icon ==\nBy default, the name key is trait_<key>.\nThe default icon path is in gfx.\n\nThese can be overridden.\n\n=== Trait Properties ===\n\n" +
			"category = X # What unique category this trait is in\nValid categories:\n\t- personality: The core personality of a character\n\n" +
			"potential = { # Triggers that must be met\n\ttrigger = { # a nested trigger\n\t}\n}\ntrigger = yes # the trait's own trigger\n",
		"common/traits/zz_more.info":  "# a second file about traits\ncategory = Y # said again, and too late\nextra = 1 # only here\n",
		"Common/Events/readme.txt":    "# Events\n# What an event file holds\n#\n#<name> = {\n#\ttype = <character_event/letter_event> what it looks like\n#   option = {\n#      name = <loc key> the text on the button\n#   }\n#}\n",
		"common/laws/laws.md":         "# Reference\n\nLaws are grouped.\n\n## progressiveness\n\nHow progressive the law is.\nIt runs from 0 to 100.\n\nA second paragraph.\n\n    law_key = {\n        group = lawgroup_key    # the group it belongs to\n    }\n\n## Not A Key\nSomething else.\n",
		"gfx/cache/heightmap.info":    strings.Repeat("tile_size=128 # data, not prose\n", 40000),
		"common/traits/00_traits.txt": "# not an info file\nbrave = { }\n",
	})

	if got := info.Kind([]string{"common/traits"}); got != "By default, the name key is trait_<key>. The default icon path is in gfx." {
		t.Errorf("Kind(traits) = %q", got)
	}
	if got := info.Kind([]string{"common/nothing", "common/events"}); got != "Events What an event file holds" {
		t.Errorf("Kind(events) = %q", got)
	}
	if got := info.Kind([]string{"common/laws"}); got != "Laws are grouped." {
		t.Errorf("Kind(laws) = %q", got)
	}

	for _, testCase := range []struct {
		folder, parent, key string
		want                InfoField
	}{
		{"common/traits", "", "category", InfoField{Prose: "What unique category this trait is in"}},
		{"common/traits", "", "extra", InfoField{Prose: "only here"}},
		{"common/traits", "", "personality", InfoField{Prose: "The core personality of a character"}},
		{"common/traits", "", "trigger", InfoField{Prose: "the trait's own trigger"}},
		{"common/traits", "potential", "trigger", InfoField{Prose: "a nested trigger"}},
		{"common/traits", "option", "trigger", InfoField{Prose: "the trait's own trigger"}},
		{"common/events", "", "type", InfoField{Prose: "what it looks like", Type: "character_event/letter_event"}},
		{"common/events", "option", "name", InfoField{Prose: "the text on the button", Type: "loc key"}},
		{"COMMON/EVENTS", "", "name", InfoField{Prose: "the text on the button", Type: "loc key"}},
		{"common/laws", "", "progressiveness", InfoField{Prose: "How progressive the law is. It runs from 0 to 100.\n\nA second paragraph."}},
		{"common/laws", "law_key", "group", InfoField{Prose: "the group it belongs to"}},
		{"common/laws", "", "missing", InfoField{}},
		{"gfx/cache", "", "tile_size", InfoField{}},
	} {
		if got := info.Field([]string{testCase.folder}, testCase.parent, testCase.key); got != testCase.want {
			t.Errorf("Field(%s, %q, %s) = %+v, want %+v", testCase.folder, testCase.parent, testCase.key, got, testCase.want)
		}
	}
	if _, ok := info["gfx/cache"]; ok {
		t.Error("a megabyte of data was read as prose")
	}
	if len(info["common/traits"].Fields) == 0 || info["common/traits"].Fields["brave"] != (InfoField{}) {
		t.Errorf("a script file was read as info: %+v", info["common/traits"].Fields)
	}

	// a script file's header: template lines document the folder, commented-out script and notes
	// beside script do not, and a name list gives blurbs only to the names the file defines
	headers := infoOf(t, map[string]string{
		"common/alert_types/00_alerts.txt": "# texture = <path>\t# relative to the game folder\n# type = alert | important_action\t# how it shows\n" +
			"# old_alert = {\n#\tscript_context = character\n# }\n\nreal_alert = {\n\tcount = 5 # use all for dynamic\n}\n",
		"common/scripted_effects/00_marriage_effects.txt": "#####\n# EFFECT LIST\n# marry_on_realm_law - Marry two characters based on the realm law\n" +
			"# positive - raises the scale by half\n#####\n\nmarry_on_realm_law = { }\n",
	})
	for _, testCase := range []struct {
		key  string
		want InfoField
	}{
		{"texture", InfoField{Prose: "relative to the game folder", Type: "path"}},
		{"type", InfoField{Prose: "how it shows"}},
		{"script_context", InfoField{}},
		{"count", InfoField{}},
	} {
		if got := headers.Field([]string{"common/alert_types"}, "", testCase.key); got != testCase.want {
			t.Errorf("header Field(%s) = %+v, want %+v", testCase.key, got, testCase.want)
		}
	}
	if got := headers["common/scripted_effects"].Definitions; !reflect.DeepEqual(got, map[string]string{"marry_on_realm_law": "Marry two characters based on the realm law"}) {
		t.Errorf("header list = %v, want only the name the file defines", got)
	}
}

func TestDeclared(t *testing.T) {
	for raw, want := range map[string]Accept{
		"yes/no":                       {Type: TypeBool},
		"none/mutual/giving/receiving": {Type: TypeEnum, Enum: []string{"none", "mutual", "giving", "receiving"}},
		"diplomacy/subject/union":      {Type: TypeEnum, Enum: []string{"diplomacy", "subject", "union"}},
		"minor|medium|major":           {Type: TypeEnum, Enum: []string{"minor", "medium", "major"}},
		"bool":                         {Type: TypeBool},
		"bool, optional":               {Type: TypeBool},
		"float":                        {Type: TypeNumber},
		"int":                          {Type: TypeNumber},
		"integer":                      {Type: TypeNumber},
		"num":                          {Type: TypeNumber},
		"double":                       {Type: TypeNumber},
		"fixed_point":                  {Type: TypeNumber},
		"Integer":                      {Type: TypeNumber},
		"string":                       {Type: TypeString},
		"localization_key":             {Type: TypeLoc},
		"loc key":                      {Type: TypeLoc},
		"list of int":                  {Type: TypeNumber},
	} {
		want.Declared = true
		if got, ok := (InfoField{Type: raw}).Declared(); !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("Declared(%q) = %+v, %v; want %+v", raw, got, ok, want)
		}
	}

	for _, raw := range []string{
		"trigger", "effect", "script value", "mtth", "database key", "database key, optional", "enum", "dynamic desc", "dynamic description, optional",
		"name of global variable list with the list of candidates in", "locator_name_1", "the giver/receiver of the relation", "array of database keys", "a/b/c/d/e/f/g/h/i/j/k/l/m", "",
	} {
		if got, ok := (InfoField{Type: raw}).Declared(); ok {
			t.Errorf("Declared(%q) = %+v, want it declined", raw, got)
		}
	}
}

// TestInfoIsRead is the gate over the prose the three games ship: every info, readme and markdown
// file is read, a file that documents a key gives it up, and the declared types are counted by
// whether they say what a value is. Set PMT_SCHEMA_GATE=1 and the three PMT_TEST_INSTALL_
// variables, each a game's root folder.
func TestInfoIsRead(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SCHEMA_GATE") == "" {
		t.Skip("walks three installs; set PMT_SCHEMA_GATE=1")
	}

	// the check is written apart from the reader: a line that is a word, then = or a bullet's colon
	keyLine := regexp.MustCompile(`(?m)^[ \t#]*(?:-\s+)?[A-Za-z_][A-Za-z0-9_]*\s*=|^[ \t]*#+\s*-\s+[A-Za-z_][A-Za-z0-9_]*:`)
	for _, testCase := range []struct{ game, env string }{
		{"ck3", "PMT_TEST_INSTALL_CK3"}, {"vic3", "PMT_TEST_INSTALL_VIC3"}, {"eu5", "PMT_TEST_INSTALL_EU5"},
	} {
		install := os.Getenv(testCase.env)
		if install == "" {
			t.Errorf("%s is not set: the gate cannot clear a game it never walked", testCase.env)
			continue
		}

		var files, data, silent int
		var missed []string
		inventory := source.Inventory("vanilla", source.Roots(install))
		for _, file := range inventory {
			if file.Format != source.Info {
				continue
			}
			files++
			if stat, err := os.Stat(file.Path); err != nil || stat.Size() > infoMaxBytes {
				data++
				continue
			}

			// a file with a key line gives at least one field
			text, _ := file.Text()
			fields := 0
			for _, folder := range ReadInfo([]source.File{file}) {
				fields += len(folder.Fields)
			}
			if fields == 0 && keyLine.MatchString(text) {
				missed = append(missed, file.Rel)
			} else if fields == 0 {
				silent++
			}
		}

		// all of them together: what the game documents, and how much of it says what a value is
		info := ReadInfo(inventory)
		var fields, prose, typed, declared int
		declined := map[string]int{}
		for _, folder := range info {
			for name, field := range folder.Fields {
				if strings.Contains(name, ".") {
					continue
				}
				fields, prose = fields+1, prose+min(len(field.Prose), 1)
				if field.Type == "" {
					continue
				}
				typed++
				if _, ok := field.Declared(); ok {
					declared++
				} else {
					declined[strings.ToLower(field.Type)]++
				}
			}
		}
		words := make([]string, 0, len(declined))
		for word := range declined {
			words = append(words, word)
		}
		slices.SortFunc(words, func(first, second string) int { return cmp.Compare(declined[second], declined[first]) })
		t.Logf("INFO %s: %d files (%d data files over the size limit, %d with no key line), %d folders, %d fields, %d with prose, %d with a type, %d of those say what a value is; most declined: %v",
			testCase.game, files, data, silent, len(info), fields, prose, typed, declared, words[:min(len(words), 12)])

		if files < 50 || fields < 500 {
			t.Errorf("%s: %d info files and %d fields under %s: wrong folder", testCase.game, files, fields, install)
		}
		if len(missed) > 0 {
			slices.Sort(missed)
			t.Errorf("%s: %d files hold a key line and gave no field: %v", testCase.game, len(missed), missed)
		}
	}
}

func TestReaderValue(t *testing.T) {
	holder := &Field{Accepts: []Accept{{Type: TypeRef, Kind: "characters"}, {Type: TypeNumber}}}
	culture := &Field{Accepts: []Accept{{Type: TypeRef, Kind: "cultures"}}}
	shown := &Field{Accepts: []Accept{{Type: TypeBool, Declared: true}}}
	style := &Field{Accepts: []Accept{{Type: TypeEnum, Enum: []string{"good", "bad"}}}}
	title := &Field{Accepts: []Accept{{Type: TypeLoc}, {Type: TypeString}}}
	texture := &Field{Accepts: []Accept{{Type: TypeFile}}}
	born := &Field{Accepts: []Accept{{Type: TypeDate}}}
	trait := &Field{Accepts: []Accept{{Type: TypeRef, Kind: "traits", Declared: true}}}

	for _, testCase := range []struct {
		name   string
		field  *Field
		text   string
		quoted bool
		want   Value
	}{
		// 1. nothing written
		{"nothing written", nil, "", false, Value{}},
		{"an empty quoted word is text", nil, "", true, Value{Type: TypeString}},

		// 2. decided by spelling alone
		{"a constant", trait, "@gold_cost", false, Value{Type: TypeConstant, Name: "gold_cost"}},
		{"an inline expression", nil, "@[ 5 + 2 ]", false, Value{Type: TypeConstant}},
		{"a parameter", trait, "$TRAIT$", false, Value{Type: TypeParam, Name: "TRAIT"}},
		{"a parameter inside a word", nil, "trait_$TRAIT$_x", true, Value{Type: TypeParam, Name: "TRAIT"}},
		{"a data call", nil, "[Character.GetName]", true, Value{Type: TypeCall}},
		{"a file by its own spelling", nil, "file:/music/x.ogg", true, Value{Type: TypeFile, Name: "music/x.ogg"}},

		// 3. a cite
		{"a cite of a declared prefix", nil, "character:186004", false, Value{Type: TypeRef, Kind: "characters", Name: "186004", Declared: true}},
		{"a cite nothing defines keeps its kind", nil, "title:k_nowhere", false, Value{Type: TypeRef, Kind: "landed_titles", Name: "k_nowhere", Declared: true}},
		{"a learned prefix the vocabulary does not declare", nil, "trait:brave", false, Value{Type: TypeRef, Kind: "traits", Name: "brave"}},
		{"a cite with a member names the member", nil, "define:NGame|START_DATE", false, Value{Type: TypeRef, Kind: "defines.entry", Name: "START_DATE", Parent: "NGame"}},
		{"a shared prefix is settled by who has it", nil, "geo:west", false, Value{Type: TypeRef, Kind: "definitions.region", Name: "west"}},
		{"a shared prefix nobody has names the first", nil, "geo:east", false, Value{Type: TypeRef, Kind: "definitions.area", Name: "east"}},
		{"a declared prefix with no kind", nil, "flag:some_flag", false, Value{Type: TypeRef, Name: "some_flag", Declared: true}},
		{"a saved scope", nil, "scope:target", false, Value{Type: TypeRef, Kind: "saved_scopes", Name: "target", Declared: true}},
		{"a sound is a name, never a cite", nil, "event:/SFX/UI/click", true, Value{Type: TypeString}},
		{"an unknown prefix is no cite", nil, "nothing:here", false, Value{Type: TypeString}},

		// 4. the field's accepts, in their order
		{"a number that is a reference", holder, "168673", false, Value{Type: TypeRef, Kind: "characters", Name: "168673"}},
		{"zero is nobody, and stays a number", holder, "0", false, Value{Type: TypeNumber}},
		{"a quoted word read as a bare one", culture, "norse", true, Value{Type: TypeRef, Kind: "cultures", Name: "norse"}},
		{"a declared type", shown, "yes", false, Value{Type: TypeBool, Declared: true}},
		{"a declared reference", trait, "brave", false, Value{Type: TypeRef, Kind: "traits", Name: "brave", Declared: true}},
		{"an enum word", style, "bad", false, Value{Type: TypeEnum}},
		{"a loc key", title, "my_title", false, Value{Type: TypeLoc, Name: "my_title"}},
		{"text where no loc key is", title, "Some Title", true, Value{Type: TypeString}},
		{"a file by path", texture, "gfx/x.dds", true, Value{Type: TypeFile, Name: "gfx/x.dds"}},
		{"a file by name alone", texture, "pattern_solid.dds", true, Value{Type: TypeFile, Name: "pattern_solid.dds"}},
		{"a date", born, "1066.9.15", false, Value{Type: TypeDate}},
		{"a quoted date", born, "1066.9.15", true, Value{Type: TypeDate}},

		// 5. a walk between scopes
		{"a link", nil, "liege", false, Value{Type: TypeScopePath, Declared: true}},
		{"a path of links", trait, "root.liege.primary_title", false, Value{Type: TypeScopePath, Declared: true}},
		{"a cite with a chain", nil, "scope:target.culture", false, Value{Type: TypeScopePath, Declared: true}},
		{"a learned cite with a chain", nil, "trait:brave.opposite", false, Value{Type: TypeScopePath}},
		{"a quoted link is text", nil, "culture", true, Value{Type: TypeString}},
		{"a reference outranks a link", culture, "norse", false, Value{Type: TypeRef, Kind: "cultures", Name: "norse"}},

		// 6. by spelling
		{"a number", nil, "-12.5", false, Value{Type: TypeNumber}},
		{"a quoted number is text", nil, "12", true, Value{Type: TypeString}},
		{"a bare date", nil, "867.1.1", false, Value{Type: TypeDate}},
		{"a bool", nil, "no", false, Value{Type: TypeBool}},
		{"a word the accepts did not hold", shown, "12", false, Value{Type: TypeNumber}},

		// 7. what is left
		{"a quoted sentence", nil, "Hello there", true, Value{Type: TypeString}},
		{"a name nothing claims", trait, "bravee", false, Value{Type: TypeUnresolved, Name: "bravee"}},
		{"an event id nothing claims", nil, "birth.1001", false, Value{Type: TypeUnresolved, Name: "birth.1001"}},
		{"an operator used as a value", nil, ">=", false, Value{Type: TypeString}},
		{"inf is a name", nil, "inf", false, Value{Type: TypeUnresolved, Name: "inf"}},
	} {
		if got := reader().Value(testCase.field, testCase.text, testCase.quoted); got != testCase.want {
			t.Errorf("%s: Value(%q) = %+v, want %+v", testCase.name, testCase.text, got, testCase.want)
		}
	}
}

func TestReaderKey(t *testing.T) {
	event := &Field{Class: ClassStructure, Members: map[string]*Field{"title": {}, "option": {}, "<date>": {}}}
	effects := &Field{Class: ClassBody, Body: BodyEffect, Members: map[string]*Field{"limit": {}}}
	triggers := &Field{Class: ClassBody, Body: BodyTrigger}
	modifiers := &Field{Class: ClassBody, Body: BodyModifier}
	rule := &Field{Class: ClassStructure, Defines: "game_rules.setting", Members: map[string]*Field{"default": {}}}
	goods := &Field{Class: ClassDatabase, KeyedBy: "goods"}

	for _, testCase := range []struct {
		name   string
		parent *Field
		text   string
		want   Value
	}{
		// 1. a member
		{"a fixed member", event, "option", Value{Type: TypeMember, Name: "option"}},
		{"a member learned under a body", effects, "limit", Value{Type: TypeMember, Name: "limit"}},

		// 2. by spelling
		{"a number", event, "1", Value{Type: TypeNumber}},
		{"a date", event, "867.1.1", Value{Type: TypeDate}},
		{"a cite", triggers, "character:186004", Value{Type: TypeRef, Kind: "characters", Name: "186004", Declared: true}},
		{"a path", nil, "gui/new_window.gui", Value{Type: TypeFile, Name: "gui/new_window.gui"}},

		// 3. in a body
		{"an effect in an effect body", effects, "add_gold", Value{Type: TypeToken, Name: "add_gold", Declared: true}},
		{"an effect in a trigger body is none", triggers, "add_gold", Value{Type: TypeUnresolved, Name: "add_gold"}},
		{"a logic word, written large", triggers, "AND", Value{Type: TypeToken, Name: "and", Declared: true}},
		{"a modifier", modifiers, "monthly_income", Value{Type: TypeToken, Name: "monthly_income", Declared: true}},
		{"a link", effects, "liege", Value{Type: TypeScopePath, Declared: true}},
		{"a path of links", triggers, "root.liege", Value{Type: TypeScopePath, Declared: true}},
		{"a scripted effect", effects, "my_effect", Value{Type: TypeRef, Kind: "scripted_effects", Name: "my_effect"}},
		{"a scripted effect in a trigger body is none", triggers, "my_effect", Value{Type: TypeUnresolved, Name: "my_effect"}},
		{"a scripted trigger", triggers, "my_trigger", Value{Type: TypeRef, Kind: "scripted_triggers", Name: "my_trigger"}},
		{"a scripted list through an iterator", effects, "every_adult_child", Value{Type: TypeRef, Kind: "scripted_lists", Name: "adult_child"}},
		{"a declared iterator is a token first", effects, "every_vassal", Value{Type: TypeToken, Name: "every_vassal", Declared: true}},
		{"a token is not one outside a body", event, "add_gold", Value{Type: TypeUnresolved, Name: "add_gold"}},

		// 4. a name of the parent's kind
		{"a definition of the nested kind", rule, "on", Value{Type: TypeRef, Kind: "game_rules.setting", Name: "on"}},
		{"a member beside them", rule, "default", Value{Type: TypeMember, Name: "default"}},
		{"a name cited as a key", goods, "wheat", Value{Type: TypeRef, Kind: "goods", Name: "wheat"}},
		{"a name nothing defines", goods, "wheet", Value{Type: TypeUnresolved, Name: "wheet"}},

		// 5. nothing claims it
		{"no parent", nil, "whatever", Value{Type: TypeUnresolved, Name: "whatever"}},
	} {
		if got := reader().Key(testCase.parent, testCase.text); got != testCase.want {
			t.Errorf("%s: Key(%q) = %+v, want %+v", testCase.name, testCase.text, got, testCase.want)
		}
	}
}

func TestReaderBelow(t *testing.T) {
	reader := reader()
	limit := &Field{Name: "limit", Class: ClassBody, Body: BodyTrigger}
	reader.Schema.Arguments = map[string]*Field{"if": {Name: "if", Class: ClassBody, Body: BodyEffect, Members: map[string]*Field{"limit": limit}}}
	setting := &Kind{Name: "game_rules.setting", Shape: &Field{Name: "setting"}}
	reader.Schema.Kinds["game_rules.setting"] = setting
	vbox := &Field{Name: "vbox"}
	window := &Kind{Name: "gui.window", Shape: &Field{Members: map[string]*Field{"vbox": vbox}}}
	body := &Field{Name: "immediate", Class: ClassBody, Body: BodyEffect, Members: map[string]*Field{"limit": limit}}
	dated, numbered, names, cites := &Field{Name: "<date>"}, &Field{Name: "<number>"}, &Field{Name: "<name>"}, &Field{Name: "<cite>"}
	entry := &Field{Name: "entry", Class: ClassStructure, Members: map[string]*Field{"<date>": dated, "<number>": numbered, "<name>": names, "<cite>": cites, "hbox": {Name: "hbox"}}}
	rule := &Field{Name: "rule", Class: ClassStructure, Defines: "game_rules.setting", Members: map[string]*Field{"default": {Name: "default"}}}

	for _, testCase := range []struct {
		what   string
		kind   *Kind
		parent *Field
		key    string
		want   *Field
	}{
		{"a member", nil, body, "limit", limit},
		{"a token takes what the game's tokens take", nil, body, "if", reader.Schema.Arguments["if"]},
		{"a scope change in a body stays in the body", nil, body, "liege", body},
		{"a cite in a body stays in the body", nil, body, "title:k_france", body},
		{"a parameter in a body stays in the body", nil, body, "$ACTOR$", body},
		{"a date selects its position", nil, entry, "867.1.1", dated},
		{"a number selects its position", nil, entry, "12", numbered},
		{"a cite in a structure selects its position", nil, entry, "title:k_france", cites},
		{"a name selects the position of names", nil, entry, "anything_else", names},
		{"a nested kind's name leads to that kind's shape", nil, rule, "on", setting.Shape},
		{"and so does one nothing has defined yet", nil, rule, "brand_new", setting.Shape},
		{"a key that can hold itself is found at the top of its kind", window, entry.Members["hbox"], "vbox", vbox},
		{"nothing learned", nil, nil, "limit", nil},
	} {
		if got := reader.Below(testCase.kind, testCase.parent, testCase.key, reader.Key(testCase.parent, testCase.key)); got != testCase.want {
			t.Errorf("%s: %+v, want %+v", testCase.what, got, testCase.want)
		}
	}
}

func TestLookups(t *testing.T) {
	sch := sample()
	names := func(kinds []*Kind) []string {
		var out []string
		for _, kind := range kinds {
			out = append(out, kind.Name)
		}
		return out
	}

	for _, testCase := range []struct {
		rel  string
		want []string
	}{
		{"common/traits/00_traits.txt", []string{"traits"}},
		{"common/traits/my_mod/more/x.txt", []string{"traits"}},
		{"Common/Traits/X.TXT", []string{"traits"}},
		{"gfx/models/units/horse/horse.asset", []string{"models.entity", "models.pdxmesh"}},
		{"map_data/definition.csv", []string{"map_data.definition"}},
		{"map_data/adjacencies.csv", nil},
		{"common/x.txt", nil},
		{"x.txt", nil},
		{"", nil},
	} {
		if got := names(sch.KindsAt(testCase.rel)); !slices.Equal(got, testCase.want) {
			t.Errorf("KindsAt(%q) = %v, want %v", testCase.rel, got, testCase.want)
		}
	}
}

func TestNameTokensOf(t *testing.T) {
	for token, want := range map[string][]NameToken{
		"has_variable":                  {{Token: "has_variable", Kind: "variables"}},
		"change_variable":               {{Token: "change_variable", Kind: "variables", Member: "name"}},
		"set_local_variable":            {{Token: "set_local_variable", Kind: "local_variables", Defines: true, Member: "name"}},
		"add_to_global_variable_list":   {{Token: "add_to_global_variable_list", Kind: "global_variables", Defines: true, Member: "name"}},
		"remove_list_local_variable":    {{Token: "remove_list_local_variable", Kind: "local_variables", Member: "name"}},
		"global_variable_list_size":     {{Token: "global_variable_list_size", Kind: "global_variables", Member: "name"}},
		"any_key_in_local_variable_map": {{Token: "any_key_in_local_variable_map", Kind: "local_variables", Member: "variable"}},
		"save_temporary_scope_value_as": {{Token: "save_temporary_scope_value_as", Kind: "saved_scopes", Defines: true, Member: "name"}},
		"clear_saved_scope":             {{Token: "clear_saved_scope", Kind: "saved_scopes"}},
		"add_to_temporary_list":         {{Token: "add_to_temporary_list", Kind: "lists", Defines: true, Member: "name"}},
		"every_in_list":                 {{Token: "every_in_list", Kind: "lists", Member: "list"}, {Token: "every_in_list", Kind: "variables", Member: "variable"}},
		"ordered_in_global_list":        {{Token: "ordered_in_global_list", Kind: "global_variables", Member: "variable"}},
		"add_gold":                      nil,
		"random_list":                   nil,
		"has_trait_flag":                nil,
		"":                              nil,
	} {
		if got := NameTokensOf(token); !reflect.DeepEqual(got, want) {
			t.Errorf("NameTokensOf(%q) = %+v, want %+v", token, got, want)
		}
	}

	// every row of the table names a script-made kind, and a token holds a member once
	seen := map[[2]string]bool{}
	for _, row := range nameTokens {
		if _, ok := ScriptKinds[row.Kind]; !ok {
			t.Errorf("%s names the kind %q, which is no script-made kind", row.Token, row.Kind)
		}
		if at := [2]string{row.Token, row.Member}; seen[at] {
			t.Errorf("%s has two rows for the member %q", row.Token, row.Member)
		} else {
			seen[at] = true
		}
	}
	if len(ScriptKinds) != 6 || ScriptKinds["saved_scopes"] != "scope" || ScriptKinds["variables"] != "var" || ScriptKinds["flags"] != "" {
		t.Errorf("ScriptKinds = %v", ScriptKinds)
	}
}

func TestReadVocab(t *testing.T) {
	vocab := dumps(t, map[string]string{
		"effects.log": "Effect Documentation:\n\n--------------------\n\nadd_gold - Adds gold to the scoped character\nSupported Scopes: character\nSupported Targets: dynasty\n\n--------------------\n\n" +
			"add_title_law - add law to scoped title\nadd_title_law = princely_elective_succession_law\nSupported Scopes: landed_title\n\n--------------------\n\n" +
			"create_character - Creates a character\nsave_event_target_as = flag - save the character as an event target\nfaith, culture and dynasty are set from the template\nfaith = faith tag OR a faith scope\nTraits: yes\nSupported Scopes: character\n\n--------------------\n\n" +
			"add_to_variable_list - Adds the event target to a variable list\nadd_to_variable_list = { name = X target = Y }\nWhere X is the name of the variable\nNote: the list is made when it is first added to\nSupported Scopes: none\n\n--------------------\n\n" +
			"debug_log - Log a string\nThe message can be a localization string.\n\nUsage:\n\tdebug_log = \"This is a test message\"\n\nSupported Scopes: none\nOutput Type: text\n\n--------------------\n\n" +
			"switch - Switch on a trigger\nswitch = {\n\ttrigger = simple_assign_trigger\n\tcase_1 = { <effects> }\nSupported Scopes: none\n\n--------------------\n\n" +
			"add_dead_character_flag - add_dead_character_flag = {\n\tflag = <name> - name of the flag\n\tyears/weeks/days = { <min_number> <max_number> } - how long it lasts\n}\nAdds a flag to a dead character.\nSupported Scopes: character\n\n--------------------\n",
		"event_targets.log": "Event Target Documentation:\n\n--------------------\n\nculture - Get the culture with the specified key\nRequires Data: yes\nGlobal Link: yes\nOutput Scopes: culture\n\n--------------------\n\n" +
			"betrothed - Get the betrothed of the scoped character\nInput Scopes: character\nOutput Scopes: character\n\n--------------------\n\n" +
			"faith - Get the faith with the specified key\nRequires Data: yes\nGlobal Link: yes\nOutput Scopes: faith\n\n--------------------\n\n" +
			"faith - Get the faith of the scoped object\nInput Scopes: character, province\nOutput Scopes: faith\n\n--------------------\n\n" +
			"Event Targets Saved from Code:\n\ncontext\nactor\nrecipient\n",
		"on_actions.log": "On Action Documentation:\n\n--------------------\n\nstress_loss_confider:\nFrom Code: No\nExpected Scope: character\n\n--------------------\n\non_tradition_removed:\nFrom Code: Yes\nExpected Scope: none\n\n--------------------\n",
	})

	gold := vocab.Effects["add_gold"]
	if !slices.Equal(gold.In, []string{"character"}) || gold.Out != "dynasty" || gold.Doc != "Adds gold to the scoped character" || gold.Usage != "" || gold.Name != "add_gold" {
		t.Errorf("add_gold = %+v", gold)
	}
	if law := vocab.Effects["add_title_law"]; !slices.Equal(law.In, []string{"landed_title"}) || law.Usage != "add_title_law = princely_elective_succession_law" || law.Doc != "add law to scoped title" {
		t.Errorf("add_title_law = %+v", law)
	}
	create := vocab.Effects["create_character"]
	if create.Doc != "Creates a character\n\nfaith, culture and dynasty are set from the template" || !strings.Contains(create.Usage, "faith = faith tag") || strings.Contains(create.Usage, "Traits") || !slices.Equal(create.In, []string{"character"}) {
		t.Errorf("create_character = %+v", create)
	}
	if effect := vocab.Effects["add_to_variable_list"]; !slices.Equal(effect.In, []string{"none"}) || !strings.Contains(effect.Doc, "Where X is the name") || !strings.Contains(effect.Doc, "Note: the list") {
		t.Errorf("add_to_variable_list = %+v", effect)
	}
	if log := vocab.Effects["debug_log"]; log.Usage != "\tdebug_log = \"This is a test message\"" || log.Doc != "Log a string\nThe message can be a localization string." {
		t.Errorf("debug_log = %+v", log)
	}
	if switchEffect := vocab.Effects["switch"]; !slices.Equal(switchEffect.In, []string{"none"}) || !strings.Contains(switchEffect.Usage, "case_1") {
		t.Errorf("an unclosed signature swallowed its label: %+v", switchEffect)
	}
	if flag := vocab.Effects["add_dead_character_flag"]; flag.Doc != "Adds a flag to a dead character." || !slices.Equal(flag.In, []string{"character"}) ||
		flag.Usage != "add_dead_character_flag = {\n\tflag = <name> - name of the flag\n\tyears/weeks/days = { <min_number> <max_number> } - how long it lasts\n}" {
		t.Errorf("a signature that opens on the header line: %+v", flag)
	}
	if len(vocab.Effects) != 7 {
		t.Errorf("%d effects, want 7: the banner is no entry", len(vocab.Effects))
	}

	// an unknown label is counted when written as the games write theirs; a prose opener is not
	if !slices.Equal(vocab.Unknown, []Tally{{Name: "output type", Count: 1}}) {
		t.Errorf("Unknown = %+v", vocab.Unknown)
	}

	if link := vocab.Links["culture"]; !link.Prefix || link.Out != "culture" || len(link.In) != 0 || link.Doc != "Get the culture with the specified key" {
		t.Errorf("culture = %+v", link)
	}
	if link := vocab.Links["betrothed"]; link.Prefix || link.Out != "character" || !slices.Equal(link.In, []string{"character"}) {
		t.Errorf("betrothed = %+v", link)
	}
	if link := vocab.Links["faith"]; !link.Prefix || link.Out != "faith" || !slices.Equal(link.In, []string{"character", "province"}) {
		t.Errorf("a link declared twice is not one link: %+v", link)
	}
	if !slices.Equal(vocab.SavedScopes, []string{"context", "actor", "recipient"}) || len(vocab.Links) != 3 {
		t.Errorf("SavedScopes = %v, %d links", vocab.SavedScopes, len(vocab.Links))
	}

	if vocab.OnActions["stress_loss_confider"] != "character" || vocab.OnActions["on_tradition_removed"] != "none" || len(vocab.OnActions) != 2 {
		t.Errorf("OnActions = %v", vocab.OnActions)
	}

	// the markdown layout of Vic3 and EU5, with a BOM and CRLF, and files that are no dumps
	vocab = dumps(t, map[string]string{
		"EFFECTS.LOG": "\xef\xbb\xbf# Effect Documentation\r\n## activate_production_method\r\nActivates the named production method\r\n**Supported Scopes**: country, state  \r\n\r\n" +
			"## add_banned_goods\r\nAdds a total ban of a good to a country  \r\n**Supported Scopes**: country  \r\n**Supported Targets**: goods  \r\n\r\n" +
			"## ordered_hostile_country\nIterate through all hostile countries  \nordered_hostile_country = {  \nlimit = { <triggers> }  \norder_by = script_value  \n<effects> }\n**Supported Scopes**: country  \n**Supported Targets**: country  \n\n" +
			"## some_scripted_list\nmy_trigger = {  \n\texists = scope:pop  \n\t## a comment inside the quoted body\n}\n**Supported Scopes**: state  \n",
		"triggers.log": "# Trigger Documentation\n## has_region_stance\nChecks the stance  \nUsage:  \nregion_stance = {  \n\tregion = sr:some_region  \n}  \nReads gamestate for all scopes.\n**Supported Scopes**: country  \n",
		"event_targets.log": "# Event Target Documentation\n### combat_width\nScope to combat width multiplier of scope province\nInput Scopes: province\nOutput Scopes: value\n\n" +
			"### cu\nGet the culture with the specified key\nRequires Data: yes\nGlobal Link: yes\nOutput Scopes: culture\n\n### area\nUnknown, add something in code registration\nRequires Data: yes\nGlobal Link: yes\nOutput Scopes: area\n\n\n--------------------\n\nEvent Targets Saved from Code:\n\ncontext\nattacker\n",
		"error.log":  "## not_a_dump\nSupported Scopes: country\n",
		"readme.md":  "## not_a_dump_either\n",
		"debug.log":  "add_gold - nothing\n",
		"effects.md": "## wrong_name\n",
	})

	if effect := vocab.Effects["activate_production_method"]; !slices.Equal(effect.In, []string{"country", "state"}) || effect.Doc != "Activates the named production method" {
		t.Errorf("activate_production_method = %+v", effect)
	}
	if effect := vocab.Effects["add_banned_goods"]; effect.Out != "goods" || effect.Doc != "Adds a total ban of a good to a country" {
		t.Errorf("add_banned_goods = %+v", effect)
	}
	if effect := vocab.Effects["ordered_hostile_country"]; effect.Usage != "ordered_hostile_country = {\nlimit = { <triggers> }\norder_by = script_value\n<effects> }" || effect.Out != "country" || effect.Doc != "Iterate through all hostile countries" {
		t.Errorf("ordered_hostile_country = %+v", effect)
	}
	if effect := vocab.Effects["some_scripted_list"]; !slices.Equal(effect.In, []string{"state"}) || len(vocab.Effects) != 4 {
		t.Errorf("an indented ## opened an entry: %d effects, %+v", len(vocab.Effects), effect)
	}
	if trigger := vocab.Triggers["has_region_stance"]; trigger.Usage != "region_stance = {\n\tregion = sr:some_region\n}" || trigger.Doc != "Checks the stance\n\nReads gamestate for all scopes." {
		t.Errorf("has_region_stance = %+v", trigger)
	}

	if link := vocab.Links["combat_width"]; link.Out != "value" || link.Prefix || !slices.Equal(link.In, []string{"province"}) {
		t.Errorf("combat_width = %+v", link)
	}
	if link := vocab.Links["cu"]; link.Out != "culture" || !link.Prefix {
		t.Errorf("cu = %+v", link)
	}
	if link := vocab.Links["area"]; link.Doc != "" || link.Out != "area" {
		t.Errorf("the placeholder is no description: %+v", link)
	}
	if !slices.Equal(vocab.SavedScopes, []string{"context", "attacker"}) || len(vocab.Links) != 3 {
		t.Errorf("SavedScopes = %v, %d links", vocab.SavedScopes, len(vocab.Links))
	}

	// the bare layout: scope types, and modifiers in both of their formats
	vocab = dumps(t, map[string]string{
		"event_scopes.log": "Scope Types:\n\nnone:\nEvaluate Triggers: yes\nExecute Effects: yes\nChange Scopes: yes\nSave Token: none\nStores Variables: no\n\ncharacter:\nEvaluate Triggers: yes\nExecute Effects: yes\nChange Scopes: yes\nSave Token: char\nStores Variables: yes\n",
		"modifiers.log": "Printing Modifier Definitions:\nTag: dynasty_opinion\nUse areas: character\n\nTag: $CULTURE$_opinion\nUse areas: character\n\n" +
			"Tag: ai_opinion_bias, Categories: Country, , All, \nTag: army_logistics_distance, Categories: Unit, , All, \n",
	})
	if !slices.Equal(vocab.ScopeTypes, []string{"none", "character"}) {
		t.Errorf("ScopeTypes = %v: a labelled body line opened an entry", vocab.ScopeTypes)
	}
	for name, want := range map[string][]string{"dynasty_opinion": {"character"}, "$CULTURE$_opinion": {"character"}, "ai_opinion_bias": {"country", "all"}, "army_logistics_distance": {"unit", "all"}} {
		if modifier := vocab.Modifiers[name]; !slices.Equal(modifier.Categories, want) || modifier.Name != name {
			t.Errorf("modifier %s = %+v, want %v", name, modifier, want)
		}
	}

	vocab = dumps(t, map[string]string{"modifiers.log": "--- Static modifier types ---\nbattle_casualties_mult:\n  Mask: battle\n  Name: aut!Casualties Taken\n  Description: A bonus or penalty to Casualties\nunit_offense_add:\n  Mask: unit\n  Name: Offense\n  Description: A Military Unit's Offense\n"})
	if modifier := vocab.Modifiers["battle_casualties_mult"]; !slices.Equal(modifier.Categories, []string{"battle"}) || modifier.Doc != "A bonus or penalty to Casualties" || len(vocab.Modifiers) != 2 {
		t.Errorf("battle_casualties_mult = %+v of %d", modifier, len(vocab.Modifiers))
	}

	// no dump folder gives an empty vocabulary
	vocab = ReadVocab([]string{filepath.Join(t.TempDir(), "nothing")})
	if vocab == nil || len(vocab.Effects)+len(vocab.Triggers)+len(vocab.Links)+len(vocab.Modifiers)+len(vocab.OnActions)+len(vocab.ScopeTypes) != 0 {
		t.Errorf("a game with no dumps gave %+v", vocab)
	}
}

// TestDumpsAreRead is the gate over the real script_docs dumps of all three games: every field of
// the vocabulary types is filled by one of them, the declared prefixes are there, and every token
// of the name-token table is one some game declares. It needs each game's dumps (run script_docs
// in the console once); set PMT_SCHEMA_GATE=1 and the three PMT_TEST_INSTALL_ variables.
func TestDumpsAreRead(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SCHEMA_GATE") == "" {
		t.Skip("reads the three games' dumps; set PMT_SCHEMA_GATE=1")
	}

	filled := map[string]int{}
	for _, testCase := range []struct {
		id, env                                          string
		effects, triggers, links, scopes, onActions, mod int
		prefixes                                         map[string]string
	}{
		{"ck3", "PMT_TEST_INSTALL_CK3", 1800, 1700, 250, 60, 800, 1, map[string]string{"culture": "culture", "faith": "faith", "title": "landed_title", "character": "character", "trait": "trait", "province": "province"}},
		{"vic3", "PMT_TEST_INSTALL_VIC3", 3000, 1500, 320, 60, 100, 1, map[string]string{"c": "country", "cu": "culture", "s": "state_region", "p": "province"}},
		{"eu5", "PMT_TEST_INSTALL_EU5", 1400, 1700, 280, 60, 100, 1, map[string]string{"c": "country", "culture": "culture", "location": "location"}},
	} {
		vocab := realVocab(t, testCase.id, testCase.env)
		if vocab == nil {
			continue
		}
		t.Logf("VOCAB %s: %d effects, %d triggers, %d links, %d scope types, %d on_actions, %d modifiers, %d engine-saved scopes, unknown labels %v",
			testCase.id, len(vocab.Effects), len(vocab.Triggers), len(vocab.Links), len(vocab.ScopeTypes), len(vocab.OnActions), len(vocab.Modifiers), len(vocab.SavedScopes), vocab.Unknown)
		if len(vocab.Effects) < testCase.effects || len(vocab.Triggers) < testCase.triggers || len(vocab.Links) < testCase.links || len(vocab.ScopeTypes) < testCase.scopes || len(vocab.OnActions) < testCase.onActions || len(vocab.Modifiers) < testCase.mod || len(vocab.SavedScopes) == 0 {
			t.Errorf("%s: fewer entries than the game is known to declare", testCase.id)
		}
		if len(vocab.Unknown) > 0 {
			t.Errorf("%s: labels the reader does not know: %v", testCase.id, vocab.Unknown)
		}

		// the prefixes the game declares, and what each leads to
		for prefix, out := range testCase.prefixes {
			if link := vocab.Links[prefix]; !link.Prefix || link.Out != out {
				t.Errorf("%s: link %s = %+v, want a prefix that leads to %s", testCase.id, prefix, link, out)
			}
		}

		// which fields this game's dumps fill
		count := func(field string, entries int) {
			filled[field] += entries
			t.Logf("  %s %-22s %d", testCase.id, field, entries)
		}
		var doc, usage, in, out, several int
		for _, bag := range []map[string]EngineToken{vocab.Effects, vocab.Triggers} {
			for _, tok := range bag {
				doc, usage, in = doc+min(len(tok.Doc), 1), usage+min(len(tok.Usage), 1), in+min(len(tok.In), 1)
				out += min(len(tok.Out), 1)
				if len(commaList(tok.Out)) > 1 {
					several++
				}
			}
		}
		count("EngineToken.Doc", doc)
		count("EngineToken.Usage", usage)
		count("EngineToken.In", in)
		count("EngineToken.Out", out)
		t.Logf("  %s tokens with several targets: %d", testCase.id, several)

		var linkDoc, linkIn, linkOut, linkPrefix, linkSeveral int
		for _, link := range vocab.Links {
			linkDoc, linkIn, linkOut = linkDoc+min(len(link.Doc), 1), linkIn+min(len(link.In), 1), linkOut+min(len(link.Out), 1)
			if link.Prefix {
				linkPrefix++
			}
			if len(commaList(link.Out)) > 1 {
				linkSeveral++
			}
		}
		count("Link.Doc", linkDoc)
		count("Link.In", linkIn)
		count("Link.Out", linkOut)
		count("Link.Prefix", linkPrefix)
		t.Logf("  %s links with several outputs: %d", testCase.id, linkSeveral)

		var modDoc, modCategories int
		for _, modifier := range vocab.Modifiers {
			modDoc, modCategories = modDoc+min(len(modifier.Doc), 1), modCategories+min(len(modifier.Categories), 1)
		}
		count("Modifier.Doc", modDoc)
		count("Modifier.Categories", modCategories)

		// the prefix of every script-made kind is one the game declares
		for kind, prefix := range ScriptKinds {
			if prefix != "" && !vocab.Links[prefix].Prefix {
				t.Errorf("%s: %s are cited by %s:, which the game does not declare as a prefix", testCase.id, kind, prefix)
			}
		}

		// every token of the hand table is declared by some game, checked over all three below
		for _, row := range nameTokens {
			if _, ok := vocab.Effects[row.Token]; ok {
				filled["token "+row.Token]++
			} else if _, ok := vocab.Triggers[row.Token]; ok {
				filled["token "+row.Token]++
			}
		}
	}

	for _, field := range []string{"EngineToken.Doc", "EngineToken.Usage", "EngineToken.In", "EngineToken.Out", "Link.Doc", "Link.In", "Link.Out", "Link.Prefix", "Modifier.Doc", "Modifier.Categories"} {
		if filled[field] == 0 {
			t.Errorf("no dump of any game fills %s", field)
		}
	}
	for _, row := range nameTokens {
		if filled["token "+row.Token] == 0 {
			t.Errorf("no game declares the name token %s", row.Token)
		}
	}
}
