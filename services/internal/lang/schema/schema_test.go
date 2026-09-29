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
					"flag": {Name: "flag", Evidence: Declared, Doc: "Says \"which\" rule;\n\na back\\slash", Default: "none", Need: Need{Level: NeedRequiredWhen, When: "for \"letter\" events", Evidence: Declared}, Class: ClassValue,
						Accepts: []Accept{{Type: TypeRef, Kind: "common/game_rules", Evidence: Inferred}}},
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
						}, Count: 0, Evidence: Declared},
						{Type: TypeBool, Count: 4, Evidence: Declared},
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

// infoOf writes files, by path from a root, into a fresh folder and reads them as an inventory,
// against a vocabulary of three scope types and one link prefix.
func infoOf(t *testing.T, files map[string]string) (Info, []Tally) {
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
	scope := ScopeType{Triggers: true, Effects: true}
	return ReadInfo(inventory, &Vocab{ScopeTypes: map[string]ScopeType{"character": scope, "country": scope, "location": scope}, Prefixes: map[string]Link{"s": {}}})
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
			Links:     map[string]Link{"root": {}, "liege": {Out: "character"}, "culture": {Out: "culture"}},
			Prefixes:  map[string]Link{"culture": {Out: "culture"}, "character": {}, "title": {}, "scope": {}, "flag": {}},
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
	return ReadVocab(game.DumpDirs(id, install))
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

// TestReadInfo reads one real doc line, or a few, per form into the position it documents: its
// prose, class, the types it takes with their origin, default, root scope and need.
func TestReadInfo(t *testing.T) {
	declared := func(valueType ValueType) Accept { return Accept{Type: valueType, Evidence: Declared} }
	script := map[string]string{"common/traits/00_traits.txt": "brave = { }\n", "common/laws/00_laws.txt": "crown_authority = { }\n", "common/government_types/00_types.txt": "monarchy = { }\n"}
	for name, entry := range map[string]struct {
		text, file, parent, key string
		want                    Field
		absent                  bool
	}{
		"a brief's paragraphs across lone #s": {text: "### brief: lifestyle ( database key )\n# What lifestyle the focus belongs to.\n#\n# If the type is lifestyle this is required.\n#\nlifestyle = lifestyle_key\n",
			key: "lifestyle", want: Field{Doc: "What lifestyle the focus belongs to.\n\nIf the type is lifestyle this is required.", Need: Need{Level: NeedRequiredWhen, When: "if the type is lifestyle", Evidence: Declared}}},
		"a brief of a jomini trigger": {text: "\t\t### Brief: trigger ( jomini trigger )\n", key: "trigger", want: Field{Class: ClassBody, Body: BodyTrigger}},
		"<effects> alone in a block":  {text: "my_activity = {\n\ton_complete = {\n\t\t<effects>\n\t}\n}\n", key: "on_complete", want: Field{Class: ClassBody, Body: BodyEffect}},
		"prose above an inline block": {text: "my_building = {\n\t# Effects that happen when construction of the building starts/cancels/finishes\n\ton_start = { }\n}\n",
			key: "on_start", want: Field{Doc: "Effects that happen when construction of the building starts/cancels/finishes"}},
		"yes/no":               {text: "    buildable = yes/no\t# can this be built?\n", key: "buildable", want: Field{Doc: "can this be built?", Accepts: []Accept{declared(TypeBool)}}},
		"int":                  {text: "\ttruce_days = int\n", key: "truce_days", want: Field{Accepts: []Accept{declared(TypeNumber)}}},
		"string":               {text: "category = string\n", key: "category", want: Field{Accepts: []Accept{declared(TypeString)}}},
		"loc_key":              {text: "name = loc_key\t\t\t# effect display name\n", key: "name", want: Field{Doc: "effect display name", Accepts: []Accept{declared(TypeLoc)}}},
		"a/b/c":                {text: "\t### Brief: type (minor|medium|major)\n\t# How big the DLC is\n\ttype = major\n", key: "type", want: Field{Doc: "How big the DLC is", Accepts: []Accept{{Type: TypeEnum, Enum: []string{"minor", "medium", "major"}, Evidence: Declared}}}},
		"a/b/etc":              {text: "\tskill = diplomacy/martial/etc\n", key: "skill", want: Field{Accepts: []Accept{{Type: TypeEnum, Enum: []string{"diplomacy", "martial"}, Evidence: Inferred}}}},
		"law_key":              {text: "\tparent = law_key                                        # optional reference to a law\n", key: "parent", want: Field{Doc: "optional reference to a law", Need: Need{Level: NeedOptional, Evidence: Declared}, Accepts: []Accept{{Type: TypeRef, Kind: "common/laws", Evidence: Declared}}}},
		"<kind key>":           {text: "# - government: <government type key> optional government type that supports the reform\n", key: "government", want: Field{Doc: "optional government type that supports the reform", Need: Need{Level: NeedOptional, Evidence: Declared}, Accepts: []Accept{{Type: TypeRef, Kind: "common/government_types", Evidence: Declared}}}},
		"defaults":             {text: "valid_sex = all/male/female # Defaults to all\n", key: "valid_sex", want: Field{Doc: "Defaults to all", Default: "all", Need: Need{Level: NeedOptional, Evidence: Inferred}, Accepts: []Accept{{Type: TypeEnum, Enum: []string{"all", "male", "female"}, Evidence: Declared}}}},
		"root = above a block": {text: "my_decision = {\n\t# root = character\n\tis_shown = {\n\t}\n}\n", key: "is_shown", want: Field{Doc: "root = character", BodyScope: "character"}},
		"(root = x) on a bullet": {text: "# - potential: trigger for whether the action is possible (root = country)\n",
			key: "potential", want: Field{Doc: "for whether the action is possible (root = country)", Class: ClassBody, Body: BodyTrigger, BodyScope: "country"}},
		"required":            {text: "my_theme = {\n\t# This field is required.\n\tbackground = {\n\t}\n}\n", key: "background", want: Field{Doc: "This field is required.", Need: Need{Level: NeedRequired, Evidence: Declared}}},
		"required when":       {text: "\tsender = X # required for letter events\n", key: "sender", want: Field{Doc: "required for letter events", Need: Need{Level: NeedRequiredWhen, When: "for letter events", Evidence: Declared}}},
		"optional in a brief": {text: "brief: cumulative ( bool, optional )\n", key: "cumulative", want: Field{Need: Need{Level: NeedOptional, Evidence: Declared}, Accepts: []Accept{declared(TypeBool)}}},
		"all members required": {text: "my_decision = {\n\t### Brief: ai_check_interval_by_tier (int31 per tier)\n\t# '0' means it is never considered. All tiers are required to be defined\n\tai_check_interval_by_tier = {\n\t\tbarony = 0\n\t\tcounty = 0\n\t}\n}\n",
			parent: "ai_check_interval_by_tier", key: "county", want: Field{Need: Need{Level: NeedRequired, Evidence: Declared}}},
		"optional by what happens without it": {text: "\towners = pop_type_key\t# dividends and investment pool, if not set determined by building group\n",
			key: "owners", want: Field{Doc: "dividends and investment pool, if not set determined by building group", Need: Need{Level: NeedOptional, Evidence: Inferred}}},
		"## key (default: x)": {text: "## maintenance_paid_by (default: empty)\nMust be specified for directed article types, must be unspecified for mutual article types.\n", file: "common/things/things.md",
			key: "maintenance_paid_by", want: Field{Doc: "Must be specified for directed article types, must be unspecified for mutual article types.", Default: "empty", Need: Need{Level: NeedRequiredWhen, When: "for directed article types", Evidence: Declared}}},
		"(type)": {text: "\tpriority = (number) \t\t#Static number, the higher it is the higher it will appear in the list. Default is -1.\n",
			key: "priority", want: Field{Doc: "Static number, the higher it is the higher it will appear in the list. Default is -1.", Default: "-1", Need: Need{Level: NeedOptional, Evidence: Inferred}, Accepts: []Accept{declared(TypeNumber)}}},
		"...":    {text: "\ttrigger = ... # Optional, as for portraits\n", key: "trigger", want: Field{Doc: "Optional, as for portraits", Need: Need{Level: NeedOptional, Evidence: Declared}}},
		"a date": {text: "my_situation = {\n\t1.1.1 = {\n\t}\n}\n", key: "<date>"},
		"[tags] before prose": {text: "\t\t\t# [Default = always yes] Trigger to determine if entities with this tag can spawn\n\t\t\ttrigger = {}\n",
			key: "trigger", want: Field{Doc: "[Default = always yes] Trigger to determine if entities with this tag can spawn", Default: "always", Need: Need{Level: NeedOptional, Evidence: Inferred}}},
		"prose above the key":          {text: "# The trait category\ncategory = personality\n", key: "category", want: Field{Doc: "The trait category"}},
		"below does not steal above":   {text: "# Event presentation type\ntype = character_event\n# Dynamic loc key\ntitle = loc_key\n", key: "title", want: Field{Doc: "Dynamic loc key", Accepts: []Accept{declared(TypeLoc)}}},
		"a block's first key claims":   {text: "option = {\n\t# the name shown\n\tname = loc_key\n}\n", key: "name", want: Field{Doc: "the name shown", Accepts: []Accept{declared(TypeLoc)}}},
		"digits alone are no prose":    {text: "period = 55 # 55\n", key: "period"},
		"a prose bullet reaches below": {text: "# - Optional: picks random entries from that set\nfallback = yes\n", key: "fallback", want: Field{Doc: "Optional: picks random entries from that set", Need: Need{Level: NeedOptional, Evidence: Declared}}},
		"a label is no key":            {text: "# scope: character\n# root: Ruler (character)\n", key: "scope", absent: true},
		"a live colon is no key":       {text: "Valid categories: the ones listed below here\n", key: "categories", absent: true},
	} {
		files := maps.Clone(script)
		files[cmp.Or(entry.file, "common/things/_things.info")] = entry.text
		info, _ := infoOf(t, files)
		got := info.Field([]string{"common/things"}, entry.parent, entry.key, true)
		got.Members = nil
		if !entry.absent {
			entry.want.Name, entry.want.Evidence = entry.key, Declared
		}
		if !reflect.DeepEqual(got, entry.want) {
			t.Errorf("%s: %s = %+v\nwant %+v", name, entry.key, got, entry.want)
		}
	}

	// the events doc's scope default is where its definitions run; a table is drift, a parameter not
	info, drift := infoOf(t, map[string]string{
		"events/_events.info":        "\tscope = scope_type # Overrides the events root scope. Optional, defaults to character scope.\n",
		"events/00_events.txt":       "my_events.1 = { scope = character }\n",
		"common/things/_things.info": "| Flag | what it does |\nmultiplier = $SCALE$\n",
	})
	if got := info["events"].Entry.BodyScope; got != "character" {
		t.Errorf("events root = %q, want character", got)
	}
	if !reflect.DeepEqual(drift, []Tally{{Name: "common/things/_things.info:1", Count: 1}}) {
		t.Errorf("drift = %v, want the table row alone", drift)
	}

	// several files of one folder, nesting, precedence between files, and the size cap
	info, _ = infoOf(t, map[string]string{
		"common/traits/_traits.info": "\n== Loc/icon ==\nBy default, the name key is trait_<key>.\nThe default icon path is in gfx.\n\nThese can be overridden.\n\n=== Trait Properties ===\n\n" +
			"category = X # What unique category this trait is in\nValid categories:\n\t- personality: The core personality of a character\n\n" +
			"potential = { # Triggers that must be met\n\ttrigger = { # a nested trigger\n\t}\n}\ntrigger = yes # the trait's own trigger\n",
		"common/traits/zz_more.info":  "# a second file about traits\ncategory = Y # said again, and too late\nextra = 1 # only here\n",
		"Common/Events/readme.txt":    "# Events\n# What an event file holds\n#\n#<name> = {\n#\ttype = <character_event/letter_event> what it looks like\n#   option = {\n#      name = <loc key> the text on the button\n#   }\n#}\n",
		"common/laws/laws.md":         "# Reference\n\nLaws are grouped.\n\n## progressiveness\n\nHow progressive the law is.\nIt runs from 0 to 100.\n\nA second paragraph.\n\n    law_key = {\n        group = lawgroup_key    # the group it belongs to\n    }\n\n## Not A Key\nSomething else.\n",
		"gfx/cache/heightmap.info":    strings.Repeat("tile_size=128 # data, not prose\n", 40000),
		"common/traits/00_traits.txt": "# not an info file\nbrave = { potential = { } }\n",
	})
	if got := info.Kind([]string{"common/traits"}); got != "By default, the name key is trait_<key>. The default icon path is in gfx." {
		t.Errorf("Kind(traits) = %q", got)
	}
	if got := info.Kind([]string{"common/nothing", "common/events"}); got != "What an event file holds" {
		t.Errorf("Kind(events) = %q", got)
	}
	if got := info.Kind([]string{"common/laws"}); got != "Laws are grouped." {
		t.Errorf("Kind(laws) = %q", got)
	}
	enum := Accept{Type: TypeEnum, Enum: []string{"character_event", "letter_event"}, Evidence: Declared}
	for _, testCase := range []struct {
		folder, parent, key string
		want                Field
	}{
		{"common/traits", "", "category", Field{Doc: "What unique category this trait is in"}},
		{"common/traits", "", "extra", Field{Doc: "only here"}},
		{"common/traits", "", "personality", Field{Doc: "The core personality of a character"}},
		{"common/traits", "", "trigger", Field{Doc: "the trait's own trigger"}},
		{"common/traits", "potential", "trigger", Field{Doc: "a nested trigger"}},
		{"common/traits", "option", "trigger", Field{Doc: "the trait's own trigger"}},
		{"common/events", "", "type", Field{Doc: "what it looks like", Accepts: []Accept{enum}}},
		{"common/events", "option", "name", Field{Doc: "the text on the button", Accepts: []Accept{{Type: TypeLoc, Evidence: Declared}}}},
		{"COMMON/EVENTS", "", "name", Field{Doc: "the text on the button", Accepts: []Accept{{Type: TypeLoc, Evidence: Declared}}}},
		{"common/laws", "", "progressiveness", Field{Doc: "How progressive the law is. It runs from 0 to 100.\n\nA second paragraph."}},
		{"common/laws", "law_key", "group", Field{Doc: "the group it belongs to"}},
	} {
		got := info.Field([]string{testCase.folder}, testCase.parent, testCase.key, true)
		got.Members, testCase.want.Name, testCase.want.Evidence = nil, testCase.key, Declared
		if !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("Field(%s, %q, %s) = %+v\nwant %+v", testCase.folder, testCase.parent, testCase.key, got, testCase.want)
		}
	}
	if got := info.Field([]string{"common/laws"}, "", "missing", true); !reflect.DeepEqual(got, Field{}) {
		t.Errorf("Field(missing) = %+v", got)
	}
	if _, ok := info["gfx/cache"]; ok {
		t.Error("a megabyte of data was read as prose")
	}
	if info["common/traits"].Entry.Members["brave"] != nil {
		t.Error("a script file was read as info")
	}

	// a script file's header: template lines document the folder, commented-out script and notes
	// beside script do not, and a name list gives blurbs only to the names the file defines
	headers, _ := infoOf(t, map[string]string{
		"common/alert_types/00_alerts.txt": "# texture = <path>\t# relative to the game folder\n# type = alert | important_action\t# how it shows\n" +
			"# old_alert = {\n#\tscript_context = character\n# }\n\nreal_alert = {\n\tcount = 5 # use all for dynamic\n}\n",
		"common/scripted_effects/00_marriage_effects.txt": "#####\n# EFFECT LIST\n# marry_on_realm_law - Marry two characters based on the realm law\n" +
			"# positive - raises the scale by half\n#####\n\nmarry_on_realm_law = { }\n",
	})
	for _, testCase := range []struct {
		key  string
		want Field
	}{
		{"texture", Field{Name: "texture", Evidence: Declared, Doc: "relative to the game folder", Accepts: []Accept{declared(TypeFile)}}},
		{"type", Field{Name: "type", Evidence: Declared, Doc: "how it shows", Accepts: []Accept{{Type: TypeEnum, Enum: []string{"alert", "important_action"}, Evidence: Declared}}}},
		{"script_context", Field{}},
		{"count", Field{}},
	} {
		if got := headers.Field([]string{"common/alert_types"}, "", testCase.key, true); !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("header Field(%s) = %+v\nwant %+v", testCase.key, got, testCase.want)
		}
	}
	if got := headers["common/scripted_effects"].Definitions; !reflect.DeepEqual(got, map[string]string{"marry_on_realm_law": "Marry two characters based on the realm law"}) {
		t.Errorf("header list = %v, want only the name the file defines", got)
	}
}

// TestInfoIsRead is the gate over the documentation the three games ship: every info, readme and
// markdown file is read, a file with a key line gives a position, and the positions are counted by
// what they say. The lines no form reads are logged. Set PMT_SCHEMA_GATE=1 and the three
// PMT_TEST_INSTALL_ variables, each a game's root folder.
func TestInfoIsRead(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SCHEMA_GATE") == "" {
		t.Skip("walks three installs; set PMT_SCHEMA_GATE=1")
	}

	// the check is written apart from the reader: a line that is a word, then = or a bullet's colon
	keyLine := regexp.MustCompile(`(?m)^[ \t#]*(?:-\s+)?[A-Za-z_][A-Za-z0-9_]*\s*=|^[ \t]*#+\s*-\s+[A-Za-z_][A-Za-z0-9_]*:`)
	for _, testCase := range []struct{ game, env string }{
		{"ck3", "PMT_TEST_INSTALL_CK3"}, {"vic3", "PMT_TEST_INSTALL_VIC3"}, {"eu5", "PMT_TEST_INSTALL_EU5"},
	} {
		vocab := realVocab(t, testCase.game, testCase.env)
		if vocab == nil {
			continue
		}
		var files, data, silent int
		var missed []string
		inventory := source.Inventory("vanilla", source.Roots(os.Getenv(testCase.env)))
		for _, file := range inventory {
			if file.Format != source.Info {
				continue
			}
			files++
			if stat, err := os.Stat(file.Path); err != nil || stat.Size() > infoMaxBytes {
				data++
				continue
			}

			// a file with a key line gives at least one position, a member or the definition's own
			// prose, or reports the lines no form reads
			text, _ := file.Text()
			positions := 0
			docs, unread := ReadInfo([]source.File{file}, vocab)
			for _, folder := range docs {
				positions += len(folder.Entry.Members) + min(len(folder.Entry.Doc), 1)
			}
			if positions == 0 && len(unread) == 0 && keyLine.MatchString(text) {
				missed = append(missed, file.Rel)
			} else if positions == 0 {
				silent++
			}
		}

		// all of them together: the positions, and what they say
		info, drift := ReadInfo(inventory, vocab)
		counts := map[string]int{}
		var walk func(field *Field)
		walk = func(field *Field) {
			for _, member := range field.Members {
				counts["positions"]++
				counts["with prose"] += min(len(member.Doc), 1)
				counts["with a class"] += min(int(member.Class), 1)
				counts["with a declared type"] += min(len(slices.DeleteFunc(slices.Clone(member.Accepts), func(accept Accept) bool { return accept.Evidence != Declared })), 1)
				if member.Need.Level != NeedUnstated {
					counts["need "+member.Need.Level.String()+" "+member.Need.Evidence.String()]++
				}
				walk(member)
			}
		}
		for _, folder := range info {
			walk(folder.Entry)
		}
		t.Logf("INFO %s: %d files (%d data files over the size limit, %d with no key line), %d folders, %v; %d lines no form reads: %v",
			testCase.game, files, data, silent, len(info), counts, len(drift), drift)

		if files < 50 || counts["positions"] < 500 {
			t.Errorf("%s: %d info files and %d positions: wrong folder", testCase.game, files, counts["positions"])
		}
		if len(missed) > 0 {
			slices.Sort(missed)
			t.Errorf("%s: %d files hold a key line and gave no position: %v", testCase.game, len(missed), missed)
		}
	}
}

func TestReaderValue(t *testing.T) {
	holder := &Field{Accepts: []Accept{{Type: TypeRef, Kind: "characters"}, {Type: TypeNumber}}}
	culture := &Field{Accepts: []Accept{{Type: TypeRef, Kind: "cultures"}}}
	shown := &Field{Accepts: []Accept{{Type: TypeBool, Evidence: Declared}}}
	style := &Field{Accepts: []Accept{{Type: TypeEnum, Enum: []string{"good", "bad"}}}}
	title := &Field{Accepts: []Accept{{Type: TypeLoc}, {Type: TypeString}}}
	texture := &Field{Accepts: []Accept{{Type: TypeFile}}}
	born := &Field{Accepts: []Accept{{Type: TypeDate}}}
	trait := &Field{Accepts: []Accept{{Type: TypeRef, Kind: "traits", Evidence: Declared}}}
	province := &Field{Accepts: []Accept{{Type: TypeNumber, Evidence: Declared}, {Type: TypeRef, Kind: "characters"}}}

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
		{"a number that names a definition is its link, though a number is declared", province, "186004", false, Value{Type: TypeRef, Kind: "characters", Name: "186004"}},
		{"a number that names nothing stays the declared number", province, "5", false, Value{Type: TypeNumber, Declared: true}},
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

	if link := vocab.Prefixes["culture"]; !link.Global || link.Out != "culture" || len(link.In) != 0 || link.Doc != "Get the culture with the specified key" {
		t.Errorf("culture = %+v", link)
	}
	if link := vocab.Links["betrothed"]; link.Global || link.Out != "character" || !slices.Equal(link.In, []string{"character"}) {
		t.Errorf("betrothed = %+v", link)
	}
	if bare, prefix := vocab.Links["faith"], vocab.Prefixes["faith"]; bare.Doc != "Get the faith of the scoped object" || !slices.Equal(bare.In, []string{"character", "province"}) || prefix.Doc != "Get the faith with the specified key" || len(prefix.In) != 0 {
		t.Errorf("a word declared twice is not its two readings: %+v, %+v", bare, prefix)
	}
	if !slices.Equal(vocab.SavedScopes, []string{"context", "actor", "recipient"}) || len(vocab.Links) != 2 || len(vocab.Prefixes) != 2 {
		t.Errorf("SavedScopes = %v, %d links, %d prefixes", vocab.SavedScopes, len(vocab.Links), len(vocab.Prefixes))
	}

	if vocab.OnActions["stress_loss_confider"] != (OnAction{Scope: "character"}) || vocab.OnActions["on_tradition_removed"] != (OnAction{Scope: "none", FromCode: true}) || len(vocab.OnActions) != 2 {
		t.Errorf("OnActions = %v", vocab.OnActions)
	}

	// what a trigger's value may be, the members a usage declares, custom localization and data types
	rule := "\n--------------------\n\n"
	vocab = dumps(t, map[string]string{
		"triggers.log": "Trigger Documentation:" + rule + "is_adult - Is the character adult?\nTraits: yes/no \nSupported Scopes: character" + rule +
			"birth_date - Compares the birth date\nTraits: <, <=, =, >, >= valid date\nSupported Scopes: character" + rule +
			"trait_is_sin - Does the faith consider the trait a sin?\ntrait_is_sin = lustful\nTraits: class CTraitDatabase key\nSupported Scopes: faith\nSupported Targets: trait" + rule +
			"is_at_location - Is the character in the province?\nTraits: province target province scope\nSupported Scopes: character" + rule +
			"adjacent_to_owned_by - Is the location next to the country's?\nTraits: country tag/country scope\nSupported Scopes: location" + rule +
			"has_dlc - Does the host have this DLC\nTraits: Valid DLCs: The Royal Court, Legacy of Persia, and Arctic Attire\nSupported Scopes: none" + rule +
			"outbreak_intensity - The outbreak's intensity\nTraits: minor, major, apocalyptic\nSupported Scopes: epidemic" + rule +
			"odd_trigger - A form no game writes\nTraits: something new\nSupported Scopes: none" + rule +
			"any_active_accolade - Iterates the accolades\nany_active_accolade = { accolade_parameter = accolade_organize_army_bonus }\nany_active_accolade = { <count=num/all> / <percent=fixed_point> <triggers> }\nSupported Scopes: character" + rule,
		"effects.log": "Effect Documentation:" + rule + "add_gold - Adds gold\nSupported Scopes: character" + rule +
			"every_active_accolade - Iterates the accolades\nany_active_accolade = { accolade_parameter = accolade_organize_army_bonus }\nevery_active_accolade = { limit = { <triggers> } <effects> }\nSupported Scopes: character" + rule +
			"send_interface_message - Sends a message\nsend_interface_message = {\n\ttype = message_type # default: send_interface_message\n\t# optional effects...\n\tadd_gold = 5\n}\nSupported Scopes: character" + rule +
			"add_to_variable_list - Adds to a list\nadd_to_variable_list = { name = X target = Y days/weeks/months/years = Z }\nSupported Scopes: none" + rule +
			"add_to_global_variable_list - Adds to a global list\nadd_to_variable_list = { name = X target = Y days/weeks/months/years = Z }\nSupported Scopes: none" + rule +
			"clear_variable_list - Empties a list\nclear_variable_list = X\nSupported Scopes: none" + rule +
			"clear_global_variable_list - Empties a global list\nclear_variable_list = X\nSupported Scopes: none" + rule +
			"try_start_diarchy - Starts a diarchy\nstart_diarchy = regency\nSupported Scopes: character" + rule +
			"create_character - Creates a character\nsave_event_target_as = flag - save the character as an event target\n" +
			"random_faith = { catholic = { some triggers } cathar = { some triggers }. Random one where the triggers are met will be picked\n" +
			"real_father = (should only be set if the real father is not the same as father=)\nafter_creation = { some effects } run after character is created\nSupported Scopes: none" + rule,
		"custom_localization.log": "Custom Localization Documentation:" + rule + "GetAnimalType:\n\nScope: all\nRandom Valid: yes\n\nEntries:\nanimal_type_bear\nanimal_type_wolf" + rule +
			"GetAnimalPlural:\n\nScope: all\nRandom Valid: no\nParent: GetAnimalType\nSuffix: _plural\n\nEntries:" + rule,
		"data_types_common.txt": strings.ReplaceAll("Application\nDefinition type: Global promote\nReturn type: Application|GetVariableSystem\nDescription: Access the global variable system\nDefinition type: Global promote\nReturn type: VariableSystem|"+
			"Character\nDefinition type: Type|Character.GetName\nDefinition type: Function\nReturn type: CString|Character.GetTrait( Arg0 )\nDefinition type: Promote\nReturn type: Trait|"+
			"CourtPosition.GetMaxAvailableHires,\nDefinition type: Function\nReturn type: int32|brave\nDefinition type: Global function\nReturn type: [unregistered]|brave\nDefinition type: Global promote\nReturn type: Trait|"+
			"AddTextIf( Arg0, Arg1 )\nDefinition type: Global function\nReturn type: CString|EmptyIf( Arg0 )\nDefinition type: Global macro\nMacro replacement: AddTextIf(Arg0, '')|Odd\nDefinition type: Something new\n", "|", "\n\n-----------------------\n\n"),
	})

	for name, want := range map[string]Traits{
		"is_adult": {Bool: true}, "birth_date": {Compare: []string{"<", "<=", "=", ">", ">="}, Date: true}, "trait_is_sin": {Keys: []string{"CTraitDatabase"}},
		"is_at_location": {Scopes: []string{"province"}}, "adjacent_to_owned_by": {Scopes: []string{"country"}, Keys: []string{"country tag"}},
		"has_dlc": {Words: []string{"The Royal Court", "Legacy of Persia", "Arctic Attire"}}, "outbreak_intensity": {Words: []string{"minor", "major", "apocalyptic"}}, "odd_trigger": {},
	} {
		if got := vocab.Triggers[name].Traits; !reflect.DeepEqual(got, want) {
			t.Errorf("%s traits = %+v, want %+v", name, got, want)
		}
	}
	if !slices.Equal(vocab.Unknown, []Tally{{Name: "definition type: Something new", Count: 1}, {Name: "traits: something new", Count: 1}}) {
		t.Errorf("a form the reader does not know is drift: %+v", vocab.Unknown)
	}

	// members, each with the dump's words for it, and what a block holds
	docs := func(members map[string]*Member) map[string]string {
		out := map[string]string{}
		for name, member := range members {
			out[name] = member.Doc
		}
		return out
	}
	for name, want := range map[string]struct {
		body    BodyClass
		members map[string]string
	}{
		"any_active_accolade":         {BodyTrigger, map[string]string{"accolade_parameter": "accolade_organize_army_bonus", "count": "num/all", "percent": "fixed_point"}},
		"every_active_accolade":       {BodyEffect, map[string]string{"accolade_parameter": "accolade_organize_army_bonus", "limit": ""}},
		"send_interface_message":      {BodyEffect, map[string]string{"type": "message_type # default: send_interface_message"}},
		"add_to_global_variable_list": {0, map[string]string{"name": "X", "target": "Y", "days": "Z", "weeks": "Z", "months": "Z", "years": "Z"}},
		"clear_global_variable_list":  {0, map[string]string{}},
		"try_start_diarchy":           {0, map[string]string{}},
		"create_character": {0, map[string]string{"save_event_target_as": "flag - save the character as an event target", "random_faith": "",
			"real_father": "(should only be set if the real father is not the same as father=)", "after_creation": "run after character is created"}},
	} {
		token, ok := vocab.Effects[name]
		if !ok {
			token = vocab.Triggers[name]
		}
		if got := docs(token.Block.Members); token.Block.Body != want.body || !maps.Equal(got, want.members) {
			t.Errorf("%s = body %d, members %v; want body %d, members %v", name, token.Block.Body, got, want.body, want.members)
		}
	}
	if limit := vocab.Effects["every_active_accolade"].Block.Members["limit"]; limit == nil || limit.Body != BodyTrigger {
		t.Errorf("limit = { <triggers> } holds triggers: %+v", limit)
	}
	members := vocab.Effects["create_character"].Block.Members
	if faith := members["random_faith"]; faith == nil || len(faith.Members) != 2 || faith.Members["cathar"].Body != BodyTrigger || members["after_creation"].Body != BodyEffect {
		t.Errorf("a prose brace left open does not run on, and { some triggers } says what a block holds: %+v", members)
	}

	if loc := vocab.CustomLoc["GetAnimalType"]; loc.Scope != "all" || !loc.Random || !slices.Equal(loc.Entries, []string{"animal_type_bear", "animal_type_wolf"}) {
		t.Errorf("GetAnimalType = %+v", loc)
	}
	if loc := vocab.CustomLoc["GetAnimalPlural"]; loc.Parent != "GetAnimalType" || loc.Suffix != "_plural" || loc.Random || len(loc.Entries) != 0 {
		t.Errorf("GetAnimalPlural = %+v", loc)
	}

	// the data-type dump: no rule before its first entry, and a promote outranks its function twin
	character := vocab.DataTypes["Character"]
	if name, trait := character["GetName"], character["GetTrait"]; len(character) != 2 || name.Returns != "CString" || name.Promote || !trait.Promote || !slices.Equal(trait.Args, []string{"Arg0"}) || trait.Returns != "Trait" {
		t.Errorf("Character = %+v", character)
	}
	if _, ok := vocab.DataTypes["CourtPosition"]["GetMaxAvailableHires"]; !ok || len(vocab.DataTypes) != 2 {
		t.Errorf("a header with a stray comma is read: %+v", vocab.DataTypes)
	}
	globals := vocab.Globals
	if !globals["Application"].Promote || globals["GetVariableSystem"].Doc != "Access the global variable system" || !globals["brave"].Promote || globals["brave"].Returns != "Trait" ||
		!slices.Equal(globals["AddTextIf"].Args, []string{"Arg0", "Arg1"}) || globals["EmptyIf"].Macro != "AddTextIf(Arg0, '')" || len(globals) != 5 {
		t.Errorf("Globals = %+v", globals)
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

	if link := vocab.Links["combat_width"]; link.Out != "value" || !slices.Equal(link.In, []string{"province"}) {
		t.Errorf("combat_width = %+v", link)
	}
	if link := vocab.Prefixes["cu"]; link.Out != "culture" || !link.Global {
		t.Errorf("cu = %+v", link)
	}
	if link := vocab.Prefixes["area"]; link.Doc != "" || link.Out != "area" {
		t.Errorf("the placeholder is no description: %+v", link)
	}
	if !slices.Equal(vocab.SavedScopes, []string{"context", "attacker"}) || len(vocab.Links) != 1 || len(vocab.Prefixes) != 2 {
		t.Errorf("SavedScopes = %v, %d links, %d prefixes", vocab.SavedScopes, len(vocab.Links), len(vocab.Prefixes))
	}

	// the bare layout: scope types, and modifiers in both of their formats
	vocab = dumps(t, map[string]string{
		"event_scopes.log": "Scope Types:\n\nnone:\nEvaluate Triggers: yes\nExecute Effects: yes\nChange Scopes: yes\nSave Token: none\nStores Variables: no\n\ncharacter:\nEvaluate Triggers: yes\nExecute Effects: yes\nChange Scopes: yes\nSave Token: char\nStores Variables: yes\n",
		"modifiers.log": "Printing Modifier Definitions:\nTag: dynasty_opinion\nUse areas: character\n\nTag: $CULTURE$_opinion\nUse areas: character\n\n" +
			"Tag: ai_opinion_bias, Categories: Country, , All, \nTag: army_logistics_distance, Categories: Unit, , All, \n",
	})
	if character := vocab.ScopeTypes["character"]; len(vocab.ScopeTypes) != 2 || character != (ScopeType{Triggers: true, Effects: true, Hops: true, SaveToken: "char", Variables: true}) || vocab.ScopeTypes["none"].Variables {
		t.Errorf("ScopeTypes = %+v: a labelled body line opened an entry", vocab.ScopeTypes)
	}
	for name, want := range map[string][]string{"dynasty_opinion": {"character"}, "$CULTURE$_opinion": {"character"}, "ai_opinion_bias": {"country", "all"}, "army_logistics_distance": {"unit", "all"}} {
		if modifier := vocab.Modifiers[name]; !slices.Equal(modifier.Categories, want) || modifier.Name != name {
			t.Errorf("modifier %s = %+v, want %v", name, modifier, want)
		}
	}

	vocab = dumps(t, map[string]string{"modifiers.log": "--- Static modifier types ---\nbattle_casualties_mult:\n  Mask: battle\n  Name: aut!Casualties Taken\n  Description: A bonus or penalty to Casualties\nunit_offense_add:\n  Mask: unit\n  Name: Offense\n  Description: A Military Unit's Offense\n"})
	if modifier := vocab.Modifiers["battle_casualties_mult"]; !slices.Equal(modifier.Categories, []string{"battle"}) || modifier.Doc != "A bonus or penalty to Casualties" || modifier.Label != "aut!Casualties Taken" || modifier.Section != "static" || len(vocab.Modifiers) != 2 {
		t.Errorf("battle_casualties_mult = %+v of %d", modifier, len(vocab.Modifiers))
	}

	// no dump folder gives an empty vocabulary
	vocab = ReadVocab([]string{filepath.Join(t.TempDir(), "nothing")})
	if vocab == nil || len(vocab.Effects)+len(vocab.Triggers)+len(vocab.Links)+len(vocab.Modifiers)+len(vocab.OnActions)+len(vocab.ScopeTypes) != 0 {
		t.Errorf("a game with no dumps gave %+v", vocab)
	}
}

// TestDumpsAreRead is the gate over the real dumps of all three games: every field of the
// vocabulary types is filled by one of them, every trigger with a Traits line carries its traits,
// the declared prefixes are there, and every token of the name-token table is one some game
// declares. It needs each game's dumps (run script_docs and dump_data_types in the console once);
// set PMT_SCHEMA_GATE=1 and the three PMT_TEST_INSTALL_ variables.
func TestDumpsAreRead(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SCHEMA_GATE") == "" {
		t.Skip("reads the three games' dumps; set PMT_SCHEMA_GATE=1")
	}

	filled := map[string]int{}
	for _, testCase := range []struct {
		id, env                                                  string
		effects, triggers, links, scopes, onActions, mod, traits int
		customLoc, dataTypes, globals                            int
		prefixes                                                 map[string]string
	}{
		{"ck3", "PMT_TEST_INSTALL_CK3", 1800, 1700, 250, 60, 800, 1, 1134, 2000, 1200, 3000, map[string]string{"culture": "culture", "faith": "faith", "title": "landed_title", "character": "character", "trait": "trait", "province": "province"}},
		{"vic3", "PMT_TEST_INSTALL_VIC3", 3000, 1500, 320, 60, 100, 1, 692, 400, 850, 2000, map[string]string{"c": "country", "cu": "culture", "s": "state_region", "p": "province"}},
		{"eu5", "PMT_TEST_INSTALL_EU5", 1400, 1700, 280, 60, 100, 1, 1300, 400, 1400, 5000, map[string]string{"c": "country", "culture": "culture", "location": "location"}},
	} {
		vocab := realVocab(t, testCase.id, testCase.env)
		if vocab == nil {
			continue
		}
		traits := 0
		for _, trigger := range vocab.Triggers {
			if !reflect.DeepEqual(trigger.Traits, Traits{}) {
				traits++
			}
		}
		t.Logf("VOCAB %s: %d effects, %d triggers (%d with traits), %d links, %d prefixes, %d scope types, %d on_actions, %d modifiers, %d custom localizations, %d data types, %d globals, %d engine-saved scopes, unknown %v",
			testCase.id, len(vocab.Effects), len(vocab.Triggers), traits, len(vocab.Links), len(vocab.Prefixes), len(vocab.ScopeTypes), len(vocab.OnActions), len(vocab.Modifiers), len(vocab.CustomLoc), len(vocab.DataTypes), len(vocab.Globals), len(vocab.SavedScopes), vocab.Unknown)
		if len(vocab.Effects) < testCase.effects || len(vocab.Triggers) < testCase.triggers || len(vocab.Links)+len(vocab.Prefixes) < testCase.links || len(vocab.ScopeTypes) < testCase.scopes || len(vocab.OnActions) < testCase.onActions ||
			len(vocab.Modifiers) < testCase.mod || traits < testCase.traits || len(vocab.CustomLoc) < testCase.customLoc || len(vocab.DataTypes) < testCase.dataTypes || len(vocab.Globals) < testCase.globals || len(vocab.SavedScopes) == 0 {
			t.Errorf("%s: fewer entries than the game is known to declare", testCase.id)
		}
		if len(vocab.Unknown) > 0 {
			t.Errorf("%s: labels the reader does not know: %v", testCase.id, vocab.Unknown)
		}

		// the prefixes the game declares, and what each leads to
		for prefix, out := range testCase.prefixes {
			if link := vocab.Prefixes[prefix]; link.Out != out {
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
		var members, bodies int
		for _, bag := range []map[string]EngineToken{vocab.Effects, vocab.Triggers} {
			for _, tok := range bag {
				members, bodies = members+min(len(tok.Block.Members), 1), bodies+min(int(tok.Block.Body), 1)
			}
		}
		count("EngineToken.Doc", doc)
		count("EngineToken.Usage", usage)
		count("EngineToken.In", in)
		count("EngineToken.Out", out)
		count("EngineToken.Traits", traits)
		count("EngineToken.Block.Members", members)
		count("EngineToken.Block.Body", bodies)
		t.Logf("  %s tokens with several targets: %d", testCase.id, several)

		var linkDoc, linkIn, linkOut, linkGlobal, linkSeveral int
		for _, links := range []map[string]Link{vocab.Links, vocab.Prefixes} {
			for _, link := range links {
				linkDoc, linkIn, linkOut = linkDoc+min(len(link.Doc), 1), linkIn+min(len(link.In), 1), linkOut+min(len(link.Out), 1)
				if link.Global {
					linkGlobal++
				}
				if len(commaList(link.Out)) > 1 {
					linkSeveral++
				}
			}
		}
		count("Link.Doc", linkDoc)
		count("Link.In", linkIn)
		count("Link.Out", linkOut)
		count("Link.Global", linkGlobal)
		t.Logf("  %s links with several outputs: %d", testCase.id, linkSeveral)

		var modDoc, modCategories int
		for _, modifier := range vocab.Modifiers {
			modDoc, modCategories = modDoc+min(len(modifier.Doc), 1), modCategories+min(len(modifier.Categories), 1)
		}
		count("Modifier.Doc", modDoc)
		count("Modifier.Categories", modCategories)
		var modLabel, modSection, variables, fromCode, entries int
		for _, modifier := range vocab.Modifiers {
			modLabel, modSection = modLabel+min(len(modifier.Label), 1), modSection+min(len(modifier.Section), 1)
		}
		for _, scope := range vocab.ScopeTypes {
			if scope.Variables {
				variables++
			}
		}
		for _, action := range vocab.OnActions {
			if action.FromCode {
				fromCode++
			}
		}
		for _, loc := range vocab.CustomLoc {
			entries += min(len(loc.Entries), 1)
		}
		count("Modifier.Label", modLabel)
		count("Modifier.Section", modSection)
		count("ScopeType.Variables", variables)
		count("OnAction.FromCode", fromCode)
		count("CustomLoc.Entries", entries)
		count("DataTypes", len(vocab.DataTypes))
		count("Globals", len(vocab.Globals))

		// CK3's create_character declares its members as a bare list, each with the dump's words
		if create := vocab.Effects["create_character"]; testCase.id == "ck3" && (create.Block.Members["employer"] == nil || create.Block.Members["mother"] == nil || create.Block.Members["random_traits_list"] == nil) {
			t.Errorf("ck3: create_character's members are not declared: %v", slices.Sorted(maps.Keys(create.Block.Members)))
		}

		// the prefix of every script-made kind is one the game declares
		for kind, prefix := range ScriptKinds {
			if _, declared := vocab.Prefixes[prefix]; prefix != "" && !declared {
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

	for _, field := range []string{"EngineToken.Doc", "EngineToken.Usage", "EngineToken.In", "EngineToken.Out", "EngineToken.Traits", "EngineToken.Block.Members", "EngineToken.Block.Body", "Link.Doc", "Link.In", "Link.Out", "Link.Global",
		"Modifier.Doc", "Modifier.Categories", "Modifier.Label", "Modifier.Section", "ScopeType.Variables", "OnAction.FromCode", "CustomLoc.Entries", "DataTypes", "Globals"} {
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
