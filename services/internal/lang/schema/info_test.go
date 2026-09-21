package schema

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/source"
)

// infoOf writes files, by their path from a root, into a fresh folder and reads them as an inventory.
func infoOf(t *testing.T, files map[string]string) Info {
	t.Helper()
	root := t.TempDir()
	var inventory []source.File
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		text := files[rel]
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		inventory = append(inventory, source.File{Path: p, Rel: rel, Origin: "vanilla", Format: source.Classify(rel)})
	}
	return ReadInfo(inventory)
}

func TestReadInfoLines(t *testing.T) {
	for name, c := range map[string]struct {
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
		info := infoOf(t, map[string]string{"common/things/_things.info": c.text})
		if got := info.Field([]string{"common/things"}, "", c.key); got.Prose != c.prose || got.Type != c.typ {
			t.Errorf("%s: %s = %+v, want prose %q type %q", name, c.key, got, c.prose, c.typ)
		}
	}
}

func TestReadInfoStructure(t *testing.T) {
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

	for _, c := range []struct {
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
		if got := info.Field([]string{c.folder}, c.parent, c.key); got != c.want {
			t.Errorf("Field(%s, %q, %s) = %+v, want %+v", c.folder, c.parent, c.key, got, c.want)
		}
	}
	if _, ok := info["gfx/cache"]; ok {
		t.Error("a megabyte of data was read as prose")
	}
	if len(info["common/traits"].Fields) == 0 || info["common/traits"].Fields["brave"] != (InfoField{}) {
		t.Errorf("a script file was read as info: %+v", info["common/traits"].Fields)
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

// TestInfoIsRead is the gate over the prose the three games ship: every info, readme and markdown file of every install is read, a file that documents a key gives that key up, and the types the games declare are counted by whether they say what a value is. It walks three installs, so it runs only when asked: set PMT_SCHEMA_GATE=1 and the three PMT_TEST_INSTALL_ variables, each a game's root folder.
func TestInfoIsRead(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SCHEMA_GATE") == "" {
		t.Skip("walks three installs; set PMT_SCHEMA_GATE=1")
	}

	// the check is written apart from the reader: a line that is a word, then = or a bullet's colon
	keyLine := regexp.MustCompile(`(?m)^[ \t#]*(?:-\s+)?[A-Za-z_][A-Za-z0-9_]*\s*=|^[ \t]*#+\s*-\s+[A-Za-z_][A-Za-z0-9_]*:`)
	for _, g := range []struct{ game, env string }{
		{"ck3", "PMT_TEST_INSTALL_CK3"}, {"vic3", "PMT_TEST_INSTALL_VIC3"}, {"eu5", "PMT_TEST_INSTALL_EU5"},
	} {
		install := os.Getenv(g.env)
		if install == "" {
			t.Errorf("%s is not set: the gate cannot clear a game it never walked", g.env)
			continue
		}

		var files, data, silent int
		var missed []string
		inventory := source.Inventory("vanilla", source.Roots(install))
		for _, f := range inventory {
			if f.Format != source.Info {
				continue
			}
			files++
			if stat, err := os.Stat(f.Path); err != nil || stat.Size() > infoMaxBytes {
				data++
				continue
			}

			// a file with a key line gives at least one field
			text, _ := f.Text()
			fields := 0
			for _, fi := range ReadInfo([]source.File{f}) {
				fields += len(fi.Fields)
			}
			if fields == 0 && keyLine.MatchString(text) {
				missed = append(missed, f.Rel)
			} else if fields == 0 {
				silent++
			}
		}

		// all of them together: what the game documents, and how much of it says what a value is
		info := ReadInfo(inventory)
		var fields, prose, typed, declared int
		declined := map[string]int{}
		for _, fi := range info {
			for name, f := range fi.Fields {
				if strings.Contains(name, ".") {
					continue
				}
				fields, prose = fields+1, prose+min(len(f.Prose), 1)
				if f.Type == "" {
					continue
				}
				typed++
				if _, ok := f.Declared(); ok {
					declared++
				} else {
					declined[strings.ToLower(f.Type)]++
				}
			}
		}
		words := make([]string, 0, len(declined))
		for w := range declined {
			words = append(words, w)
		}
		sort.Slice(words, func(i, j int) bool { return declined[words[i]] > declined[words[j]] })
		t.Logf("INFO %s: %d files (%d data files over the size limit, %d with no key line), %d folders, %d fields, %d with prose, %d with a type, %d of those say what a value is; most declined: %v",
			g.game, files, data, silent, len(info), fields, prose, typed, declared, words[:min(len(words), 12)])

		if files < 50 || fields < 500 {
			t.Errorf("%s: %d info files and %d fields under %s: wrong folder", g.game, files, fields, install)
		}
		if len(missed) > 0 {
			slices.Sort(missed)
			t.Errorf("%s: %d files hold a key line and gave no field: %v", g.game, len(missed), missed)
		}
	}
}
