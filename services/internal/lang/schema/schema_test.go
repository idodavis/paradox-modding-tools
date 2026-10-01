// schema_test.go covers the docs' prose and declared types, the reader, the folder lookup, the
// name-command table and the script_docs dumps; two gates walk the real installs and their dumps.
package schema

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/source"
)

// sample is a schema whose databases list folders and a file, for the lookups.
func sample() *Schema {
	schema := &Schema{Databases: map[string]*Database{
		"traits":              {Name: "traits", Folders: []string{"common/traits"}},
		"models.pdxmesh":      {Name: "models.pdxmesh", Folders: []string{"gfx/models"}},
		"models.entity":       {Name: "models.entity", Folders: []string{"gfx/models"}},
		"map_data.definition": {Name: "map_data.definition", Folders: []string{"map_data/definition.csv"}},
	}}
	schema.BuildLookups()
	return schema
}

// docsOf writes files, by path from a root, into a fresh folder and reads them as an inventory,
// against dumps of three scope types and one event target prefix.
func docsOf(t *testing.T, files map[string]string) (Docs, []Tally) {
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
	scope := Scope{Triggers: true, Effects: true}
	return ReadDocs(inventory, ReadScripts(inventory, game.Get("ck3")), &Dumps{Scopes: map[string]Scope{"character": scope, "country": scope, "location": scope}, DataTargets: map[string]EventTarget{"s": {}}})
}

// found is a Defined over three sets: definitions as database/name, loc keys, and files.
type found struct{ defs, locs, files map[string]bool }

func (found found) Def(database, name string) bool { return found.defs[database+"/"+name] }
func (found found) Loc(key string) bool            { return found.locs[key] }
func (found found) File(name string) bool          { return found.files[name] }

func (found found) DefinedBy(name string) string {
	definedBy := ""
	for def := range found.defs {
		if database, defined, _ := strings.Cut(def, "/"); defined == name {
			if definedBy != "" {
				return ""
			}
			definedBy = database
		}
	}
	return definedBy
}

// reader is a Reader over a small game: prefixed databases, scripted effect databases and small
// dumps.
func reader() Reader {
	database := func(name string, prefixes ...string) *Database { return &Database{Name: name, Prefixes: prefixes} }
	schema := &Schema{Databases: map[string]*Database{
		"characters": database("characters", "character"), "landed_titles": database("landed_titles", "title"), "traits": database("traits", "trait"),
		"defines.entry": database("defines.entry", "define"), "saved_scopes": database("saved_scopes", "scope"),
		"definitions.area": database("definitions.area", "geo"), "definitions.region": database("definitions.region", "geo"),
		"scripted_effects":  {Name: "scripted_effects", CalledIn: []Block{BlockEffects}},
		"scripted_triggers": {Name: "scripted_triggers", CalledIn: []Block{BlockTriggers}},
		"scripted_lists":    {Name: "scripted_lists", CalledIn: []Block{BlockTriggers, BlockEffects}},
	}, Dumps: &Dumps{
		Effects:      map[string]Command{"add_gold": {}, "if": {}, "every_vassal": {}},
		Triggers:     map[string]Command{"has_trait": {}, "and": {}, "not": {}, "any_vassal": {}},
		Modifiers:    map[string]Modifier{"monthly_income": {}},
		EventTargets: map[string]EventTarget{"root": {}, "liege": {Out: []string{"character"}}, "culture": {Out: []string{"culture"}}},
		DataTargets:  map[string]EventTarget{"culture": {Out: []string{"culture"}}, "character": {}, "title": {}, "scope": {}, "flag": {}},
	}}
	schema.BuildLookups()
	return Reader{
		Schema: schema,
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
func dumps(t *testing.T, files map[string]string) *Dumps {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ReadDumps([]string{filepath.Join(dir, "missing"), dir})
}

// realDumps reads the dumps of one game, or reports why it cannot.
func realDumps(t *testing.T, id, env string) *Dumps {
	t.Helper()
	install := os.Getenv(env)
	if install == "" {
		t.Errorf("%s is not set: the gate cannot clear a game it never read", env)
		return nil
	}
	return ReadDumps(game.DumpDirs(id, install))
}

// TestReadDocs reads one real doc line, or a few, per form into the position it documents: its
// prose, class, the types it takes with their origin, default, root scope and need.
func TestReadDocs(t *testing.T) {
	declared := func(wordType WordType) Take { return Take{Type: wordType, Source: SourceDocs} }
	script := map[string]string{"common/traits/00_traits.txt": "brave = { }\n", "common/laws/00_laws.txt": "crown_authority = { }\n", "common/government_types/00_types.txt": "monarchy = { }\n"}
	for name, entry := range map[string]struct {
		text, file, parent, key string
		want                    Key
		absent                  bool
	}{
		"a brief's paragraphs across lone #s": {text: "### brief: lifestyle ( database key )\n# What lifestyle the focus belongs to.\n#\n# If the type is lifestyle this is required.\n#\nlifestyle = lifestyle_key\n",
			key: "lifestyle", want: Key{Doc: "What lifestyle the focus belongs to.\n\nIf the type is lifestyle this is required.", Required: RequiredWhen, When: "if the type is lifestyle",
				Takes: []Take{{Type: WordReference, Database: "database key", Source: SourceDocs}, {Type: WordReference, Database: "lifestyle key", Source: SourceDocs}}}},
		"a brief of a jomini trigger": {text: "\t\t### Brief: trigger ( jomini trigger )\n", key: "trigger", want: Key{Written: WrittenBlock, Block: BlockTriggers}},
		"<effects> alone in a block":  {text: "my_activity = {\n\ton_complete = {\n\t\t<effects>\n\t}\n}\n", key: "on_complete", want: Key{Written: WrittenBlock, Block: BlockEffects}},
		"prose above an inline block": {text: "my_building = {\n\t# Effects that happen when construction of the building starts/cancels/finishes\n\ton_start = { }\n}\n",
			key: "on_start", want: Key{Doc: "Effects that happen when construction of the building starts/cancels/finishes", Written: WrittenBlock, Block: BlockEffects}},
		"yes/no":               {text: "    buildable = yes/no\t# can this be built?\n", key: "buildable", want: Key{Doc: "can this be built?", Takes: []Take{declared(WordBool)}}},
		"int":                  {text: "\ttruce_days = int\n", key: "truce_days", want: Key{Takes: []Take{declared(WordNumber)}}},
		"string":               {text: "category = string\n", key: "category", want: Key{Takes: []Take{declared(WordString)}}},
		"loc_key":              {text: "name = loc_key\t\t\t# effect display name\n", key: "name", want: Key{Doc: "effect display name", Takes: []Take{declared(WordLoc)}}},
		"a/b/c":                {text: "\t### Brief: type (minor|medium|major)\n\t# How big the DLC is\n\ttype = major\n", key: "type", want: Key{Doc: "How big the DLC is", Takes: []Take{{Type: WordEnum, Enum: []string{"minor", "medium", "major"}, Source: SourceDocs}}}},
		"a/b/etc":              {text: "\tskill = diplomacy/martial/etc\n", key: "skill", want: Key{Takes: []Take{{Type: WordEnum, Enum: []string{"diplomacy", "martial"}, Source: SourceDocs, Guessed: true}}}},
		"law_key":              {text: "\tparent = law_key                                        # optional reference to a law\n", key: "parent", want: Key{Doc: "optional reference to a law", Required: RequiredOptional, Takes: []Take{{Type: WordReference, Database: "law key", Source: SourceDocs}}}},
		"<kind key>":           {text: "# - government: <government type key> optional government type that supports the reform\n", key: "government", want: Key{Doc: "optional government type that supports the reform", Required: RequiredOptional, Takes: []Take{{Type: WordReference, Database: "government type key", Source: SourceDocs}}}},
		"defaults":             {text: "valid_sex = all/male/female # Defaults to all\n", key: "valid_sex", want: Key{Doc: "Defaults to all", Default: "all", Required: RequiredOptional, Takes: []Take{{Type: WordEnum, Enum: []string{"all", "male", "female"}, Source: SourceDocs}}}},
		"root = above a block": {text: "my_decision = {\n\t# root = character\n\tis_shown = {\n\t}\n}\n", key: "is_shown", want: Key{Doc: "root = character", Scope: "character"}},
		"(root = x) on a bullet": {text: "# - potential: trigger for whether the action is possible (root = country)\n",
			key: "potential", want: Key{Doc: "for whether the action is possible (root = country)", Written: WrittenBlock, Block: BlockTriggers, Scope: "country"}},
		"required":            {text: "my_theme = {\n\t# This field is required.\n\tbackground = {\n\t}\n}\n", key: "background", want: Key{Doc: "This field is required.", Required: RequiredAlways}},
		"required when":       {text: "\tsender = X # required for letter events\n", key: "sender", want: Key{Doc: "required for letter events", Required: RequiredWhen, When: "for letter events"}},
		"optional in a brief": {text: "brief: cumulative ( bool, optional )\n", key: "cumulative", want: Key{Required: RequiredOptional, Takes: []Take{declared(WordBool)}}},
		"all members required": {text: "my_decision = {\n\t### Brief: ai_check_interval_by_tier (int31 per tier)\n\t# '0' means it is never considered. All tiers are required to be defined\n\tai_check_interval_by_tier = {\n\t\tbarony = 0\n\t\tcounty = 0\n\t}\n}\n",
			parent: "ai_check_interval_by_tier", key: "county", want: Key{Required: RequiredAlways}},
		"optional by what happens without it": {text: "\towners = pop_type_key\t# dividends and investment pool, if not set determined by building group\n",
			key: "owners", want: Key{Doc: "dividends and investment pool, if not set determined by building group", Required: RequiredOptional, Takes: []Take{{Type: WordReference, Database: "pop type key", Source: SourceDocs}}}},
		"## key (default: x), an English word no default": {text: "## maintenance_paid_by (default: empty)\nMust be specified for directed article types, must be unspecified for mutual article types.\n", file: "common/things/things.md",
			key: "maintenance_paid_by", want: Key{Doc: "Must be specified for directed article types, must be unspecified for mutual article types.", Required: RequiredWhen, When: "for directed article types"}},
		"(type)": {text: "\tpriority = (number) \t\t#Static number, the higher it is the higher it will appear in the list. Default is -1.\n",
			key: "priority", want: Key{Doc: "Static number, the higher it is the higher it will appear in the list. Default is -1.", Default: "-1", Required: RequiredOptional, Takes: []Take{declared(WordNumber)}}},
		"...":    {text: "\ttrigger = ... # Optional, as for portraits\n", key: "trigger", want: Key{Doc: "Optional, as for portraits", Required: RequiredOptional}},
		"a date": {text: "my_situation = {\n\t1.1.1 = {\n\t}\n}\n", key: "<date>"},
		"[tags] before prose": {text: "\t\t\t# [Default = always yes] Trigger to determine if entities with this tag can spawn\n\t\t\ttrigger = {}\n",
			key: "trigger", want: Key{Doc: "[Default = always yes] Trigger to determine if entities with this tag can spawn"}},
		"prose above the key":          {text: "# The trait category\ncategory = personality\n", key: "category", want: Key{Doc: "The trait category"}},
		"below does not steal above":   {text: "# Event presentation type\ntype = character_event\n# Dynamic loc key\ntitle = loc_key\n", key: "title", want: Key{Doc: "Dynamic loc key", Takes: []Take{declared(WordLoc)}}},
		"a block's first key claims":   {text: "option = {\n\t# the name shown\n\tname = loc_key\n}\n", key: "name", want: Key{Doc: "the name shown", Takes: []Take{declared(WordLoc)}}},
		"digits alone are no prose":    {text: "period = 55 # 55\n", key: "period"},
		"a prose bullet reaches below": {text: "# - Optional: picks random entries from that set\nfallback = yes\n", key: "fallback", want: Key{Doc: "Optional: picks random entries from that set", Required: RequiredOptional}},
		"a label is no key":            {text: "# scope: character\n# root: Ruler (character)\n", key: "scope", absent: true},
		"a live colon is no key":       {text: "Valid categories: the ones listed below here\n", key: "categories", absent: true},
	} {
		files := maps.Clone(script)
		files[cmp.Or(entry.file, "common/things/_things.info")] = entry.text
		docs, _ := docsOf(t, files)
		got := docs.Key([]string{"common/things"}, entry.parent, entry.key, true)
		got.Keys = nil
		if !entry.absent {
			entry.want.Name, entry.want.Source = entry.key, SourceDocs
		}
		if !reflect.DeepEqual(got, entry.want) {
			t.Errorf("%s: %s = %+v\nwant %+v", name, entry.key, got, entry.want)
		}
	}

	// the events doc's scope default is where its definitions run; a table is unread, a parameter not
	docs, unread := docsOf(t, map[string]string{
		"events/_events.info":        "\tscope = scope_type # Overrides the events root scope. Optional, defaults to character scope.\n",
		"events/00_events.txt":       "my_events.1 = { scope = character }\n",
		"common/things/_things.info": "| Flag | what it does |\nmultiplier = $SCALE$\n",
	})
	if got := docs["events"].Entry.Scope; got != "character" {
		t.Errorf("events root = %q, want character", got)
	}
	if !reflect.DeepEqual(unread, []Tally{{Name: "common/things/_things.info:1", Count: 1}}) {
		t.Errorf("drift = %v, want the table row alone", unread)
	}

	// several files of one folder, nesting, precedence between files, and the size cap
	docs, _ = docsOf(t, map[string]string{
		"common/traits/_traits.info": "\n== Loc/icon ==\nBy default, the name key is trait_<key>.\nThe default icon path is in gfx.\n\nThese can be overridden.\n\n=== Trait Properties ===\n\n" +
			"category = X # What unique category this trait is in\nValid categories:\n\t- personality: The core personality of a character\n\n" +
			"potential = { # Triggers that must be met\n\ttrigger = { # a nested trigger\n\t}\n}\ntrigger = yes # the trait's own trigger\n",
		"common/traits/zz_more.info":  "# a second file about traits\ncategory = Y # said again, and too late\nextra = 1 # only here\n",
		"Common/Events/readme.txt":    "# Events\n# What an event file holds\n#\n#<name> = {\n#\ttype = <character_event/letter_event> what it looks like\n#   option = {\n#      name = <loc key> the text on the button\n#   }\n#}\n",
		"common/laws/laws.md":         "# Reference\n\nLaws are grouped.\n\n## progressiveness\n\nHow progressive the law is.\nIt runs from 0 to 100.\n\nA second paragraph.\n\n    law_key = {\n        group = lawgroup_key    # the group it belongs to\n    }\n\n## Not A Key\nSomething else.\n",
		"gfx/cache/heightmap.info":    strings.Repeat("tile_size=128 # data, not prose\n", 40000),
		"common/traits/00_traits.txt": "# not an info file\nbrave = { potential = { } }\n",
	})
	// no prose is dropped: each titled section is a paragraph of About headed by its title, and the
	// notes above the entry's own opener are its prose
	if got := docs["common/traits"].About; got != "Loc/icon\nBy default, the name key is trait_<key>. The default icon path is in gfx.\n\nThese can be overridden.\n\nTrait Properties\nValid categories:" {
		t.Errorf("About(traits) = %q", got)
	}
	if got := docs["common/events"].Entry.Doc; got != "What an event file holds" {
		t.Errorf("events entry = %q", got)
	}
	if got := docs["common/laws"].About; got != "Reference\nLaws are grouped.\n\nNot A Key\nSomething else." {
		t.Errorf("About(laws) = %q", got)
	}
	if got := docs["common/traits"].LocKeys; !slices.Equal(got, []string{"trait_{id}"}) {
		t.Errorf("LocKeys(traits) = %v", got)
	}
	enum := Take{Type: WordEnum, Enum: []string{"character_event", "letter_event"}, Source: SourceDocs}
	for _, testCase := range []struct {
		folder, parent, key string
		want                Key
	}{
		{"common/traits", "", "category", Key{Doc: "What unique category this trait is in\n\na second file about traits said again, and too late",
			Takes: []Take{{Type: WordEnum, Enum: []string{"personality"}, Source: SourceDocs}}}},
		{"common/traits", "", "extra", Key{Doc: "only here"}},
		{"common/traits", "", "trigger", Key{Doc: "the trait's own trigger"}},
		{"common/traits", "potential", "trigger", Key{Doc: "a nested trigger"}},
		{"common/traits", "option", "trigger", Key{Doc: "the trait's own trigger"}},
		{"common/events", "", "type", Key{Doc: "what it looks like", Takes: []Take{enum}}},
		{"common/events", "option", "name", Key{Doc: "the text on the button", Takes: []Take{{Type: WordLoc, Source: SourceDocs}}}},
		{"COMMON/EVENTS", "", "name", Key{Doc: "the text on the button", Takes: []Take{{Type: WordLoc, Source: SourceDocs}}}},
		{"common/laws", "", "progressiveness", Key{Doc: "How progressive the law is. It runs from 0 to 100.\n\nA second paragraph."}},
		{"common/laws", "law_key", "group", Key{Doc: "the group it belongs to", Takes: []Take{{Type: WordReference, Database: "lawgroup key", Source: SourceDocs}}}},
	} {
		got := docs.Key([]string{testCase.folder}, testCase.parent, testCase.key, true)
		got.Keys, testCase.want.Name, testCase.want.Source = nil, testCase.key, SourceDocs
		if !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("Field(%s, %q, %s) = %+v\nwant %+v", testCase.folder, testCase.parent, testCase.key, got, testCase.want)
		}
	}
	if got := docs.Key([]string{"common/laws"}, "", "missing", true); !reflect.DeepEqual(got, Key{}) {
		t.Errorf("Field(missing) = %+v", got)
	}
	if _, ok := docs["gfx/cache"]; ok {
		t.Error("a megabyte of data was read as prose")
	}
	if docs["common/traits"].Entry.Keys["brave"] != nil {
		t.Error("a script file was read as info")
	}
	if got := docs["common/traits"].Blurbs["personality"]; docs["common/traits"].Entry.Keys["personality"] != nil || got != "The core personality of a character" {
		t.Errorf("a value list's item is a key, or lost its blurb: %q", got)
	}

	// a script file's header of three key lines or more is a template, all of it read; notes beside
	// script are not, and a name list gives blurbs only to the names the file defines
	headers, _ := docsOf(t, map[string]string{
		"common/alert_types/00_alerts.txt": "# texture = <path>\t# relative to the game folder\n# type = alert | important_action\t# how it shows\n" +
			"# old_alert = {\n#\tscript_context = character\n# }\n\nreal_alert = {\n\tcount = 5 # use all for dynamic\n}\n",
		"common/scripted_effects/00_marriage_effects.txt": "#####\n# EFFECT LIST\n# marry_on_realm_law - Marry two characters based on the realm law\n" +
			"# positive - raises the scale by half\n#####\n\nmarry_on_realm_law = { }\n",
	})
	for _, testCase := range []struct {
		key  string
		want Key
	}{
		{"texture", Key{Name: "texture", Source: SourceDocs, Doc: "relative to the game folder", Takes: []Take{declared(WordFile)}}},
		{"type", Key{Name: "type", Source: SourceDocs, Doc: "how it shows", Takes: []Take{{Type: WordEnum, Enum: []string{"alert", "important_action"}, Source: SourceDocs}}}},
		{"script_context", Key{Name: "script_context", Source: SourceDocs}},
		{"count", Key{}},
	} {
		if got := headers.Key([]string{"common/alert_types"}, "", testCase.key, true); !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("header Field(%s) = %+v\nwant %+v", testCase.key, got, testCase.want)
		}
	}
	if got := headers["common/scripted_effects"].Blurbs; !reflect.DeepEqual(got, map[string]string{"marry_on_realm_law": "Marry two characters based on the realm law"}) {
		t.Errorf("header list = %v, want only the name the file defines", got)
	}
}

// TestDocsAreRead is the gate over the documentation the three games ship: every info, readme and
// markdown file is read, a file with a key line gives a position, and the positions are counted by
// what they say. The lines no form reads are logged. Set PMT_SCHEMA_GATE=1 and the three
// PMT_TEST_INSTALL_ variables, each a game's root folder.
func TestDocsAreRead(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SCHEMA_GATE") == "" {
		t.Skip("walks three installs; set PMT_SCHEMA_GATE=1")
	}

	// the check is written apart from the reader: a line that is a word, then = or a bullet's colon
	keyLine := regexp.MustCompile(`(?m)^[ \t#]*(?:-\s+)?[A-Za-z_][A-Za-z0-9_]*\s*=|^[ \t]*#+\s*-\s+[A-Za-z_][A-Za-z0-9_]*:`)
	for _, testCase := range []struct{ game, env string }{
		{"ck3", "PMT_TEST_INSTALL_CK3"}, {"vic3", "PMT_TEST_INSTALL_VIC3"}, {"eu5", "PMT_TEST_INSTALL_EU5"},
	} {
		dumps := realDumps(t, testCase.game, testCase.env)
		if dumps == nil {
			continue
		}
		var files, data, silent int
		var missed []string
		inventory := source.Inventory("vanilla", source.Roots(os.Getenv(testCase.env)))
		scripts := ReadScripts(inventory, game.Get(testCase.game))
		alone := *scripts
		alone.Headers = nil
		for _, file := range inventory {
			if file.Format != source.Info {
				continue
			}
			files++
			if stat, err := os.Stat(file.Path); err != nil || stat.Size() > docsMaxBytes {
				data++
				continue
			}

			// a file with a key line gives at least one position, a key or the definition's own
			// prose, or reports the lines no form reads
			text, _ := file.Text()
			positions := 0
			docs, unread := ReadDocs([]source.File{file}, &alone, dumps)
			for _, folder := range docs {
				positions += len(folder.Entry.Keys) + min(len(folder.Entry.Doc), 1)
			}
			if positions == 0 && len(unread) == 0 && keyLine.MatchString(text) {
				missed = append(missed, file.Rel)
			} else if positions == 0 {
				silent++
			}
		}

		// all of them together: the positions, and what they say
		docs, unread := ReadDocs(inventory, scripts, dumps)
		counts := map[string]int{}
		var walk func(key *Key)
		walk = func(key *Key) {
			for _, key := range key.Keys {
				counts["positions"]++
				counts["with prose"] += min(len(key.Doc), 1)
				counts["with a written form"] += min(int(key.Written), 1)
				counts["with a declared type"] += min(len(slices.DeleteFunc(slices.Clone(key.Takes), func(take Take) bool { return !take.Source.Declared() || take.Guessed })), 1)
				if key.Required != RequiredUnstated {
					counts[fmt.Sprintf("required %d", key.Required)]++
				}
				walk(key)
			}
		}
		for _, folder := range docs {
			walk(folder.Entry)
		}
		t.Logf("INFO %s: %d files (%d data files over the size limit, %d with no key line), %d folders, %v; %d lines no form reads: %v",
			testCase.game, files, data, silent, len(docs), counts, len(unread), unread)

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
	holder := &Key{Takes: []Take{{Type: WordReference, Database: "characters"}, {Type: WordNumber}}}
	culture := &Key{Takes: []Take{{Type: WordReference, Database: "cultures"}}}
	shown := &Key{Takes: []Take{{Type: WordBool, Source: SourceDocs}}}
	style := &Key{Takes: []Take{{Type: WordEnum, Enum: []string{"good", "bad"}}}}
	title := &Key{Takes: []Take{{Type: WordLoc}, {Type: WordString}}}
	texture := &Key{Takes: []Take{{Type: WordFile}}}
	born := &Key{Takes: []Take{{Type: WordDate}}}
	trait := &Key{Takes: []Take{{Type: WordReference, Database: "traits", Source: SourceDocs}}}
	province := &Key{Takes: []Take{{Type: WordNumber, Source: SourceDocs}, {Type: WordReference, Database: "characters"}}}

	for _, testCase := range []struct {
		name   string
		field  *Key
		text   string
		quoted bool
		want   Word
	}{
		// 1. nothing written
		{"nothing written", nil, "", false, Word{}},
		{"an empty quoted word is text", nil, "", true, Word{Type: WordString}},

		// 2. decided by spelling alone
		{"a constant", trait, "@gold_cost", false, Word{Type: WordConstant, Name: "gold_cost"}},
		{"an inline expression", nil, "@[ 5 + 2 ]", false, Word{Type: WordConstant}},
		{"a parameter", trait, "$TRAIT$", false, Word{Type: WordParameter, Name: "TRAIT"}},
		{"a parameter inside a word", nil, "trait_$TRAIT$_x", true, Word{Type: WordParameter, Name: "TRAIT"}},
		{"a data call", nil, "[Character.GetName]", true, Word{Type: WordDataCall}},
		{"a file by its own spelling", nil, "file:/music/x.ogg", true, Word{Type: WordFile, Name: "music/x.ogg"}},

		// 3. a prefixed word
		{"a cite of a declared prefix", nil, "character:186004", false, Word{Type: WordReference, Database: "characters", Name: "186004", Declared: true}},
		{"a cite nothing defines keeps its kind", nil, "title:k_nowhere", false, Word{Type: WordReference, Database: "landed_titles", Name: "k_nowhere", Declared: true}},
		{"a learned prefix the vocabulary does not declare", nil, "trait:brave", false, Word{Type: WordReference, Database: "traits", Name: "brave"}},
		{"a cite with a member names the member", nil, "define:NGame|START_DATE", false, Word{Type: WordReference, Database: "defines.entry", Name: "START_DATE", Parent: "NGame"}},
		{"a shared prefix is settled by who has it", nil, "geo:west", false, Word{Type: WordReference, Database: "definitions.region", Name: "west"}},
		{"a shared prefix nobody has names the first", nil, "geo:east", false, Word{Type: WordReference, Database: "definitions.area", Name: "east"}},
		{"a declared prefix with no kind", nil, "flag:some_flag", false, Word{Type: WordReference, Name: "some_flag", Declared: true}},
		{"a saved scope", nil, "scope:target", false, Word{Type: WordReference, Database: "saved_scopes", Name: "target", Declared: true}},
		{"a sound is a name, never a cite", nil, "event:/SFX/UI/click", true, Word{Type: WordString}},
		{"an unknown prefix is no cite", nil, "nothing:here", false, Word{Type: WordString}},

		// 4. the key's takes, in their order
		{"a number that is a reference", holder, "168673", false, Word{Type: WordReference, Database: "characters", Name: "168673"}},
		{"zero is nobody, and stays a number", holder, "0", false, Word{Type: WordNumber}},
		{"a quoted word read as a bare one", culture, "norse", true, Word{Type: WordReference, Database: "cultures", Name: "norse"}},
		{"a declared type", shown, "yes", false, Word{Type: WordBool, Declared: true}},
		{"a declared reference", trait, "brave", false, Word{Type: WordReference, Database: "traits", Name: "brave", Declared: true}},
		{"a number that names a definition is its link, though a number is declared", province, "186004", false, Word{Type: WordReference, Database: "characters", Name: "186004"}},
		{"a number that names nothing stays the declared number", province, "5", false, Word{Type: WordNumber, Declared: true}},
		{"an enum word", style, "bad", false, Word{Type: WordEnum}},
		{"a loc key", title, "my_title", false, Word{Type: WordLoc, Name: "my_title"}},
		{"text where no loc key is", title, "Some Title", true, Word{Type: WordString}},
		{"a file by path", texture, "gfx/x.dds", true, Word{Type: WordFile, Name: "gfx/x.dds"}},
		{"a file by name alone", texture, "pattern_solid.dds", true, Word{Type: WordFile, Name: "pattern_solid.dds"}},
		{"a date", born, "1066.9.15", false, Word{Type: WordDate}},
		{"a quoted date", born, "1066.9.15", true, Word{Type: WordDate}},

		// 5. a walk between scopes
		{"a link", nil, "liege", false, Word{Type: WordScopePath, Declared: true}},
		{"a path of links", trait, "root.liege.primary_title", false, Word{Type: WordScopePath, Declared: true}},
		{"a cite with a chain", nil, "scope:target.culture", false, Word{Type: WordScopePath, Declared: true}},
		{"a learned cite with a chain", nil, "trait:brave.opposite", false, Word{Type: WordScopePath}},
		{"a quoted link is text", nil, "culture", true, Word{Type: WordString}},
		{"a reference outranks a link", culture, "norse", false, Word{Type: WordReference, Database: "cultures", Name: "norse"}},

		// 6. by spelling
		{"a number", nil, "-12.5", false, Word{Type: WordNumber}},
		{"a quoted number is text", nil, "12", true, Word{Type: WordString}},
		{"a bare date", nil, "867.1.1", false, Word{Type: WordDate}},
		{"a bool", nil, "no", false, Word{Type: WordBool}},
		{"a word the accepts did not hold", shown, "12", false, Word{Type: WordNumber}},

		// 7. what is left
		{"a quoted sentence", nil, "Hello there", true, Word{Type: WordString}},
		{"a name nothing claims", trait, "bravee", false, Word{Type: WordUnknown, Name: "bravee"}},
		{"an event id nothing claims", nil, "birth.1001", false, Word{Type: WordUnknown, Name: "birth.1001"}},
		{"an operator used as a value", nil, ">=", false, Word{Type: WordString}},
		{"inf is a name", nil, "inf", false, Word{Type: WordUnknown, Name: "inf"}},
	} {
		if got := reader().Value(testCase.field, testCase.text, testCase.quoted); got != testCase.want {
			t.Errorf("%s: Value(%q) = %+v, want %+v", testCase.name, testCase.text, got, testCase.want)
		}
	}
}

func TestReaderKey(t *testing.T) {
	event := &Key{Written: WrittenBlock, Keys: map[string]*Key{"title": {}, "option": {}, "<date>": {}}}
	effects := &Key{Written: WrittenBlock, Block: BlockEffects, Keys: map[string]*Key{"limit": {}}}
	triggers := &Key{Written: WrittenBlock, Block: BlockTriggers}
	modifiers := &Key{Written: WrittenBlock, Block: BlockModifiers}
	rule := &Key{Written: WrittenBlock, Defines: "game_rules.setting", Keys: map[string]*Key{"default": {}}}
	goods := &Key{Written: WrittenBlock, Names: "goods"}

	for _, testCase := range []struct {
		name   string
		parent *Key
		text   string
		want   Word
	}{
		// 1. a key
		{"a fixed member", event, "option", Word{Type: WordKey, Name: "option"}},
		{"a member learned under a body", effects, "limit", Word{Type: WordKey, Name: "limit"}},

		// 2. by spelling
		{"a number", event, "1", Word{Type: WordNumber}},
		{"a date", event, "867.1.1", Word{Type: WordDate}},
		{"a cite", triggers, "character:186004", Word{Type: WordReference, Database: "characters", Name: "186004", Declared: true}},
		{"a path", nil, "gui/new_window.gui", Word{Type: WordFile, Name: "gui/new_window.gui"}},

		// 3. in a body
		{"an effect in an effect body", effects, "add_gold", Word{Type: WordCommand, Name: "add_gold", Declared: true}},
		{"an effect in a trigger body is none", triggers, "add_gold", Word{Type: WordUnknown, Name: "add_gold"}},
		{"a logic word, written large", triggers, "AND", Word{Type: WordCommand, Name: "and", Declared: true}},
		{"a modifier", modifiers, "monthly_income", Word{Type: WordCommand, Name: "monthly_income", Declared: true}},
		{"a link", effects, "liege", Word{Type: WordScopePath, Declared: true}},
		{"a path of links", triggers, "root.liege", Word{Type: WordScopePath, Declared: true}},
		{"a scripted effect", effects, "my_effect", Word{Type: WordReference, Database: "scripted_effects", Name: "my_effect"}},
		{"a scripted effect in a trigger body is none", triggers, "my_effect", Word{Type: WordUnknown, Name: "my_effect"}},
		{"a scripted trigger", triggers, "my_trigger", Word{Type: WordReference, Database: "scripted_triggers", Name: "my_trigger"}},
		{"a scripted list through an iterator", effects, "every_adult_child", Word{Type: WordReference, Database: "scripted_lists", Name: "adult_child"}},
		{"a declared iterator is a token first", effects, "every_vassal", Word{Type: WordCommand, Name: "every_vassal", Declared: true}},
		{"a token is not one outside a body", event, "add_gold", Word{Type: WordUnknown, Name: "add_gold"}},

		// 4. a name of the parent's database
		{"a definition of the nested kind", rule, "on", Word{Type: WordReference, Database: "game_rules.setting", Name: "on"}},
		{"a member beside them", rule, "default", Word{Type: WordKey, Name: "default"}},
		{"a name cited as a key", goods, "wheat", Word{Type: WordReference, Database: "goods", Name: "wheat"}},
		{"a name nothing defines", goods, "wheet", Word{Type: WordUnknown, Name: "wheet"}},

		// 5. nothing claims it
		{"no parent", nil, "whatever", Word{Type: WordUnknown, Name: "whatever"}},
	} {
		if got := reader().Key(testCase.parent, testCase.text); got != testCase.want {
			t.Errorf("%s: Key(%q) = %+v, want %+v", testCase.name, testCase.text, got, testCase.want)
		}
	}
}

func TestReaderBelow(t *testing.T) {
	reader := reader()
	limit := &Key{Name: "limit", Written: WrittenBlock, Block: BlockTriggers}
	reader.Schema.Arguments = map[string]*Key{"if": {Name: "if", Written: WrittenBlock, Block: BlockEffects, Keys: map[string]*Key{"limit": limit}}}
	setting := &Database{Name: "game_rules.setting", Entry: &Key{Name: "setting"}}
	reader.Schema.Databases["game_rules.setting"] = setting
	vbox := &Key{Name: "vbox"}
	window := &Database{Name: "gui.window", Entry: &Key{Keys: map[string]*Key{"vbox": vbox}}}
	body := &Key{Name: "immediate", Written: WrittenBlock, Block: BlockEffects, Keys: map[string]*Key{"limit": limit}}
	dated, numbered, names, prefixed := &Key{Name: "<date>"}, &Key{Name: "<number>"}, &Key{Name: "<name>"}, &Key{Name: "<prefixed>"}
	entry := &Key{Name: "entry", Written: WrittenBlock, Keys: map[string]*Key{"<date>": dated, "<number>": numbered, "<name>": names, "<prefixed>": prefixed, "hbox": {Name: "hbox"}}}
	rule := &Key{Name: "rule", Written: WrittenBlock, Defines: "game_rules.setting", Keys: map[string]*Key{"default": {Name: "default"}}}

	for _, testCase := range []struct {
		what   string
		kind   *Database
		parent *Key
		key    string
		want   *Key
	}{
		{"a member", nil, body, "limit", limit},
		{"a token takes what the game's tokens take", nil, body, "if", reader.Schema.Arguments["if"]},
		{"a scope change in a body stays in the body", nil, body, "liege", body},
		{"a cite in a body stays in the body", nil, body, "title:k_france", body},
		{"a parameter in a body stays in the body", nil, body, "$ACTOR$", body},
		{"a date selects its position", nil, entry, "867.1.1", dated},
		{"a number selects its position", nil, entry, "12", numbered},
		{"a cite in a structure selects its position", nil, entry, "title:k_france", prefixed},
		{"a name selects the position of names", nil, entry, "anything_else", names},
		{"a nested kind's name leads to that kind's shape", nil, rule, "on", setting.Entry},
		{"and so does one nothing has defined yet", nil, rule, "brand_new", setting.Entry},
		{"a key that can hold itself is found at the top of its kind", window, entry.Keys["hbox"], "vbox", vbox},
		{"nothing learned", nil, nil, "limit", nil},
	} {
		if got := reader.Below(testCase.kind, testCase.parent, testCase.key, reader.Key(testCase.parent, testCase.key)); got != testCase.want {
			t.Errorf("%s: %+v, want %+v", testCase.what, got, testCase.want)
		}
	}
}

func TestLookups(t *testing.T) {
	sch := sample()
	names := func(databases []*Database) []string {
		var out []string
		for _, database := range databases {
			out = append(out, database.Name)
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
		if got := names(sch.DatabasesAt(testCase.rel)); !slices.Equal(got, testCase.want) {
			t.Errorf("KindsAt(%q) = %v, want %v", testCase.rel, got, testCase.want)
		}
	}
}

func TestNameCommandsOf(t *testing.T) {
	for command, want := range map[string][]NameCommand{
		"has_variable":                  {{Command: "has_variable", Database: "variables"}},
		"change_variable":               {{Command: "change_variable", Database: "variables", NameKey: "name"}},
		"set_local_variable":            {{Command: "set_local_variable", Database: "local_variables", Defines: true, NameKey: "name"}},
		"add_to_global_variable_list":   {{Command: "add_to_global_variable_list", Database: "global_variables", Defines: true, NameKey: "name"}},
		"remove_list_local_variable":    {{Command: "remove_list_local_variable", Database: "local_variables", NameKey: "name"}},
		"global_variable_list_size":     {{Command: "global_variable_list_size", Database: "global_variables", NameKey: "name"}},
		"any_key_in_local_variable_map": {{Command: "any_key_in_local_variable_map", Database: "local_variables", NameKey: "variable"}},
		"save_temporary_scope_value_as": {{Command: "save_temporary_scope_value_as", Database: "saved_scopes", Defines: true, NameKey: "name"}},
		"clear_saved_scope":             {{Command: "clear_saved_scope", Database: "saved_scopes"}},
		"add_to_temporary_list":         {{Command: "add_to_temporary_list", Database: "lists", Defines: true, NameKey: "name"}},
		"every_in_list":                 {{Command: "every_in_list", Database: "lists", NameKey: "list"}, {Command: "every_in_list", Database: "variables", NameKey: "variable"}},
		"ordered_in_global_list":        {{Command: "ordered_in_global_list", Database: "global_variables", NameKey: "variable"}},
		"add_gold":                      nil,
		"random_list":                   nil,
		"has_trait_flag":                nil,
		"":                              nil,
	} {
		if got := NameCommandsOf(command); !reflect.DeepEqual(got, want) {
			t.Errorf("NameTokensOf(%q) = %+v, want %+v", command, got, want)
		}
	}

	// every row of the table names a script-made database, and a command holds a key once
	seen := map[[2]string]bool{}
	for _, row := range nameCommands {
		if _, ok := ScriptMade[row.Database]; !ok {
			t.Errorf("%s names the kind %q, which is no script-made kind", row.Command, row.Database)
		}
		if at := [2]string{row.Command, row.NameKey}; seen[at] {
			t.Errorf("%s has two rows for the member %q", row.Command, row.NameKey)
		} else {
			seen[at] = true
		}
	}
	if len(ScriptMade) != 6 || ScriptMade["saved_scopes"] != "scope" || ScriptMade["variables"] != "var" || ScriptMade["flags"] != "" {
		t.Errorf("ScriptKinds = %v", ScriptMade)
	}
}

func TestReadDumps(t *testing.T) {
	read := dumps(t, map[string]string{
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

	gold := read.Effects["add_gold"]
	if !slices.Equal(gold.In, []string{"character"}) || gold.Out != "dynasty" || gold.Doc != "Adds gold to the scoped character" || gold.Usage != "" || gold.Name != "add_gold" {
		t.Errorf("add_gold = %+v", gold)
	}
	if law := read.Effects["add_title_law"]; !slices.Equal(law.In, []string{"landed_title"}) || law.Usage != "add_title_law = princely_elective_succession_law" || law.Doc != "add law to scoped title" {
		t.Errorf("add_title_law = %+v", law)
	}
	create := read.Effects["create_character"]
	if create.Doc != "Creates a character\n\nfaith, culture and dynasty are set from the template" || !strings.Contains(create.Usage, "faith = faith tag") || strings.Contains(create.Usage, "Traits") || !slices.Equal(create.In, []string{"character"}) {
		t.Errorf("create_character = %+v", create)
	}
	if effect := read.Effects["add_to_variable_list"]; !slices.Equal(effect.In, []string{"none"}) || !strings.Contains(effect.Doc, "Where X is the name") || !strings.Contains(effect.Doc, "Note: the list") {
		t.Errorf("add_to_variable_list = %+v", effect)
	}
	if log := read.Effects["debug_log"]; log.Usage != "\tdebug_log = \"This is a test message\"" || log.Doc != "Log a string\nThe message can be a localization string.\n\nOutput Type: text" {
		t.Errorf("debug_log = %+v", log)
	}
	if switchEffect := read.Effects["switch"]; !slices.Equal(switchEffect.In, []string{"none"}) || !strings.Contains(switchEffect.Usage, "case_1") {
		t.Errorf("an unclosed signature swallowed its label: %+v", switchEffect)
	}
	if flag := read.Effects["add_dead_character_flag"]; flag.Doc != "Adds a flag to a dead character." || !slices.Equal(flag.In, []string{"character"}) ||
		flag.Usage != "add_dead_character_flag = {\n\tflag = <name> - name of the flag\n\tyears/weeks/days = { <min_number> <max_number> } - how long it lasts\n}" {
		t.Errorf("a signature that opens on the header line: %+v", flag)
	}
	if len(read.Effects) != 7 {
		t.Errorf("%d effects, want 7: the banner is no entry", len(read.Effects))
	}

	// a line shaped like a label the file does not have is counted, and stays prose
	if !slices.Equal(read.Unknown, []Tally{{Name: "note", Count: 1}, {Name: "output type", Count: 1}}) {
		t.Errorf("Unknown = %+v", read.Unknown)
	}

	if target := read.DataTargets["culture"]; !target.Global || !slices.Equal(target.Out, []string{"culture"}) || len(target.In) != 0 || target.Doc != "Get the culture with the specified key" {
		t.Errorf("culture = %+v", target)
	}
	if target := read.EventTargets["betrothed"]; target.Global || !slices.Equal(target.Out, []string{"character"}) || !slices.Equal(target.In, []string{"character"}) {
		t.Errorf("betrothed = %+v", target)
	}
	if bare, prefix := read.EventTargets["faith"], read.DataTargets["faith"]; bare.Doc != "Get the faith of the scoped object" || !slices.Equal(bare.In, []string{"character", "province"}) || prefix.Doc != "Get the faith with the specified key" || len(prefix.In) != 0 {
		t.Errorf("a word declared twice is not its two readings: %+v, %+v", bare, prefix)
	}
	if !slices.Equal(read.SavedScopes, []string{"context", "actor", "recipient"}) || len(read.EventTargets) != 2 || len(read.DataTargets) != 2 {
		t.Errorf("SavedScopes = %v, %d links, %d prefixes", read.SavedScopes, len(read.EventTargets), len(read.DataTargets))
	}

	if read.OnActions["stress_loss_confider"] != (OnAction{Scope: "character"}) || read.OnActions["on_tradition_removed"] != (OnAction{Scope: "none", FromCode: true}) || len(read.OnActions) != 2 {
		t.Errorf("OnActions = %v", read.OnActions)
	}

	// what a trigger's value may be, the keys a usage declares, custom localization and data types
	rule := "\n--------------------\n\n"
	read = dumps(t, map[string]string{
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

	take := func(wordType WordType, database string, enum ...string) Take {
		return Take{Type: wordType, Database: database, Enum: enum, Source: SourceDumps}
	}
	for name, want := range map[string][]Take{
		"is_adult": {take(WordBool, "")}, "birth_date": {take(WordDate, "")}, "trait_is_sin": {take(WordReference, "CTraitDatabase")},
		"is_at_location": {take(WordScopePath, "province")}, "adjacent_to_owned_by": {take(WordReference, "country tag"), take(WordScopePath, "country")},
		"has_dlc": {take(WordEnum, "", "The Royal Court", "Legacy of Persia", "Arctic Attire")}, "outbreak_intensity": {take(WordEnum, "", "minor", "major", "apocalyptic")}, "odd_trigger": nil,
	} {
		var got []Take
		if block := read.Triggers[name].Block; block != nil {
			got = block.Takes
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s takes = %+v, want %+v", name, got, want)
		}
	}
	if !slices.Equal(read.Unknown, []Tally{{Name: "definition type: Something new", Count: 1}, {Name: "traits: something new", Count: 1}}) {
		t.Errorf("a form the reader does not know is drift: %+v", read.Unknown)
	}

	// keys, each with the dump's words for it, and what a block holds
	docs := func(key *Key) map[string]string {
		out := map[string]string{}
		if key != nil {
			for name, child := range key.Keys {
				out[name] = child.Doc
			}
		}
		return out
	}
	blockOf := func(key *Key) Block {
		if key == nil {
			return BlockNone
		}
		return key.Block
	}
	for name, want := range map[string]struct {
		body    Block
		members map[string]string
	}{
		"any_active_accolade":         {BlockTriggers, map[string]string{"accolade_parameter": "accolade_organize_army_bonus", "count": "num/all", "percent": "fixed_point"}},
		"every_active_accolade":       {BlockEffects, map[string]string{"accolade_parameter": "accolade_organize_army_bonus", "limit": ""}},
		"send_interface_message":      {BlockEffects, map[string]string{"type": "message_type # default: send_interface_message"}},
		"add_to_global_variable_list": {0, map[string]string{"name": "X", "target": "Y", "days": "Z", "weeks": "Z", "months": "Z", "years": "Z"}},
		"clear_global_variable_list":  {0, map[string]string{}},
		"try_start_diarchy":           {0, map[string]string{}},
		"create_character": {0, map[string]string{"save_event_target_as": "flag - save the character as an event target", "random_faith": "",
			"real_father": "(should only be set if the real father is not the same as father=)", "after_creation": "run after character is created"}},
	} {
		command, ok := read.Effects[name]
		if !ok {
			command = read.Triggers[name]
		}
		if got := docs(command.Block); blockOf(command.Block) != want.body || !maps.Equal(got, want.members) {
			t.Errorf("%s = block %d, keys %v; want block %d, keys %v", name, blockOf(command.Block), got, want.body, want.members)
		}
	}
	if limit := read.Effects["every_active_accolade"].Block.Keys["limit"]; limit == nil || limit.Block != BlockTriggers {
		t.Errorf("limit = { <triggers> } holds triggers: %+v", limit)
	}
	members := read.Effects["create_character"].Block.Keys
	if faith := members["random_faith"]; faith == nil || len(faith.Keys) != 2 || faith.Keys["cathar"].Block != BlockTriggers || members["after_creation"].Block != BlockEffects {
		t.Errorf("a prose brace left open does not run on, and { some triggers } says what a block holds: %+v", members)
	}

	if loc := read.CustomLocalization["GetAnimalType"]; loc.Scope != "all" || !loc.Random || !slices.Equal(loc.Entries, []string{"animal_type_bear", "animal_type_wolf"}) {
		t.Errorf("GetAnimalType = %+v", loc)
	}
	if loc := read.CustomLocalization["GetAnimalPlural"]; loc.Parent != "GetAnimalType" || loc.Suffix != "_plural" || loc.Random || len(loc.Entries) != 0 {
		t.Errorf("GetAnimalPlural = %+v", loc)
	}

	// the data-type dump: no rule before its first entry, and a promote outranks its function twin
	character := read.DataTypes["Character"].Functions
	if name, trait := character["GetName"], character["GetTrait"]; len(character) != 2 || name.Returns != "CString" || name.Promote || !trait.Promote || !slices.Equal(trait.Args, []string{"Arg0"}) || trait.Returns != "Trait" {
		t.Errorf("Character = %+v", character)
	}
	if _, ok := read.DataTypes["CourtPosition"].Functions["GetMaxAvailableHires"]; !ok || len(read.DataTypes) != 2 {
		t.Errorf("a header with a stray comma is read: %+v", read.DataTypes)
	}
	globals := read.Globals
	if !globals["Application"].Promote || globals["GetVariableSystem"].Doc != "Access the global variable system" || !globals["brave"].Promote || globals["brave"].Returns != "Trait" ||
		!slices.Equal(globals["AddTextIf"].Args, []string{"Arg0", "Arg1"}) || globals["EmptyIf"].Macro != "AddTextIf(Arg0, '')" || len(globals) != 5 {
		t.Errorf("Globals = %+v", globals)
	}

	// the markdown layout of Vic3 and EU5, with a BOM and CRLF, and files that are no dumps
	read = dumps(t, map[string]string{
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

	if effect := read.Effects["activate_production_method"]; !slices.Equal(effect.In, []string{"country", "state"}) || effect.Doc != "Activates the named production method" {
		t.Errorf("activate_production_method = %+v", effect)
	}
	if effect := read.Effects["add_banned_goods"]; effect.Out != "goods" || effect.Doc != "Adds a total ban of a good to a country" {
		t.Errorf("add_banned_goods = %+v", effect)
	}
	if effect := read.Effects["ordered_hostile_country"]; effect.Usage != "ordered_hostile_country = {\nlimit = { <triggers> }\norder_by = script_value\n<effects> }" || effect.Out != "country" || effect.Doc != "Iterate through all hostile countries" {
		t.Errorf("ordered_hostile_country = %+v", effect)
	}
	if effect := read.Effects["some_scripted_list"]; !slices.Equal(effect.In, []string{"state"}) || len(read.Effects) != 4 {
		t.Errorf("an indented ## opened an entry: %d effects, %+v", len(read.Effects), effect)
	}
	if trigger := read.Triggers["has_region_stance"]; trigger.Usage != "region_stance = {\n\tregion = sr:some_region\n}" || trigger.Doc != "Checks the stance" {
		t.Errorf("has_region_stance = %+v", trigger)
	}

	if target := read.EventTargets["combat_width"]; !slices.Equal(target.Out, []string{"value"}) || !slices.Equal(target.In, []string{"province"}) {
		t.Errorf("combat_width = %+v", target)
	}
	if target := read.DataTargets["cu"]; !slices.Equal(target.Out, []string{"culture"}) || !target.Global {
		t.Errorf("cu = %+v", target)
	}
	if target := read.DataTargets["area"]; target.Doc != "" || !slices.Equal(target.Out, []string{"area"}) {
		t.Errorf("the placeholder is no description: %+v", target)
	}
	if !slices.Equal(read.SavedScopes, []string{"context", "attacker"}) || len(read.EventTargets) != 1 || len(read.DataTargets) != 2 {
		t.Errorf("SavedScopes = %v, %d links, %d prefixes", read.SavedScopes, len(read.EventTargets), len(read.DataTargets))
	}

	// the bare layout: scope types, and modifiers in both of their formats
	read = dumps(t, map[string]string{
		"event_scopes.log": "Scope Types:\n\nnone:\nEvaluate Triggers: yes\nExecute Effects: yes\nChange Scopes: yes\nSave Token: none\nStores Variables: no\n\ncharacter:\nEvaluate Triggers: yes\nExecute Effects: yes\nChange Scopes: yes\nSave Token: char\nStores Variables: yes\n",
		"modifiers.log": "Printing Modifier Definitions:\nTag: dynasty_opinion\nUse areas: character\n\nTag: $CULTURE$_opinion\nUse areas: character\n\n" +
			"Tag: ai_opinion_bias, Categories: Country, , All, \nTag: army_logistics_distance, Categories: Unit, , All, \n",
	})
	if character := read.Scopes["character"]; len(read.Scopes) != 2 || character != (Scope{Triggers: true, Effects: true, Hops: true, SaveToken: "char", Variables: true}) || read.Scopes["none"].Variables {
		t.Errorf("ScopeTypes = %+v: a labelled body line opened an entry", read.Scopes)
	}
	for name, want := range map[string][]string{"dynasty_opinion": {"character"}, "$CULTURE$_opinion": {"character"}, "ai_opinion_bias": {"country"}, "army_logistics_distance": {"unit"}} {
		if modifier := read.Modifiers[name]; !slices.Equal(modifier.Categories, want) || modifier.Name != name {
			t.Errorf("modifier %s = %+v, want %v", name, modifier, want)
		}
	}

	read = dumps(t, map[string]string{"modifiers.log": "--- Static modifier types ---\nbattle_casualties_mult:\n  Mask: battle\n  Name: aut!Casualties Taken\n  Description: A bonus or penalty to Casualties\nunit_offense_add:\n  Mask: unit\n  Name: Offense\n  Description: A Military Unit's Offense\n"})
	if modifier := read.Modifiers["battle_casualties_mult"]; !slices.Equal(modifier.Categories, []string{"battle"}) || modifier.Doc != "A bonus or penalty to Casualties" || modifier.Label != "aut!Casualties Taken" || modifier.Section != "static" || len(read.Modifiers) != 2 {
		t.Errorf("battle_casualties_mult = %+v of %d", modifier, len(read.Modifiers))
	}

	// no dump folder gives an empty vocabulary
	read = ReadDumps([]string{filepath.Join(t.TempDir(), "nothing")})
	if read == nil || len(read.Effects)+len(read.Triggers)+len(read.EventTargets)+len(read.Modifiers)+len(read.OnActions)+len(read.Scopes) != 0 {
		t.Errorf("a game with no dumps gave %+v", read)
	}
}

// TestDumpsAreRead is the gate over the real dumps of all three games: every field of the
// dumps types is filled by one of them, every trigger with a Traits line carries its traits,
// the declared prefixes are there, and every command of the name-command table is one some game
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
		dumps := realDumps(t, testCase.id, testCase.env)
		if dumps == nil {
			continue
		}
		traits := 0
		for _, trigger := range dumps.Triggers {
			if trigger.Block != nil && len(trigger.Block.Takes) > 0 {
				traits++
			}
		}
		t.Logf("VOCAB %s: %d effects, %d triggers (%d with traits), %d links, %d prefixes, %d scope types, %d on_actions, %d modifiers, %d custom localizations, %d data types, %d globals, %d engine-saved scopes, unknown %v",
			testCase.id, len(dumps.Effects), len(dumps.Triggers), traits, len(dumps.EventTargets), len(dumps.DataTargets), len(dumps.Scopes), len(dumps.OnActions), len(dumps.Modifiers), len(dumps.CustomLocalization), len(dumps.DataTypes), len(dumps.Globals), len(dumps.SavedScopes), dumps.Unknown)
		if len(dumps.Effects) < testCase.effects || len(dumps.Triggers) < testCase.triggers || len(dumps.EventTargets)+len(dumps.DataTargets) < testCase.links || len(dumps.Scopes) < testCase.scopes || len(dumps.OnActions) < testCase.onActions ||
			len(dumps.Modifiers) < testCase.mod || traits < testCase.traits || len(dumps.CustomLocalization) < testCase.customLoc || len(dumps.DataTypes) < testCase.dataTypes || len(dumps.Globals) < testCase.globals || len(dumps.SavedScopes) == 0 {
			t.Errorf("%s: fewer entries than the game is known to declare", testCase.id)
		}
		// prose shaped like a label is reported; a Traits form, header or layout the reader cannot read fails
		for _, tally := range dumps.Unknown {
			if prefix, _, _ := strings.Cut(tally.Name, ":"); slices.Contains([]string{"traits", "definition type", "header", "layout"}, prefix) {
				t.Errorf("%s: a form the reader does not know: %v", testCase.id, tally)
			}
		}

		// the prefixes the game declares, and what each leads to
		for prefix, out := range testCase.prefixes {
			if target := dumps.DataTargets[prefix]; !slices.Equal(target.Out, []string{out}) {
				t.Errorf("%s: link %s = %+v, want a prefix that leads to %s", testCase.id, prefix, target, out)
			}
		}

		// which fields this game's dumps fill
		count := func(field string, entries int) {
			filled[field] += entries
			t.Logf("  %s %-22s %d", testCase.id, field, entries)
		}
		var doc, usage, in, out, several int
		for _, bag := range []map[string]Command{dumps.Effects, dumps.Triggers} {
			for _, command := range bag {
				doc, usage, in = doc+min(len(command.Doc), 1), usage+min(len(command.Usage), 1), in+min(len(command.In), 1)
				out += min(len(command.Out), 1)
				if len(commaList(command.Out)) > 1 {
					several++
				}
			}
		}
		var members, bodies int
		for _, bag := range []map[string]Command{dumps.Effects, dumps.Triggers} {
			for _, command := range bag {
				if command.Block != nil {
					members, bodies = members+min(len(command.Block.Keys), 1), bodies+min(int(command.Block.Block), 1)
				}
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
		for _, targets := range []map[string]EventTarget{dumps.EventTargets, dumps.DataTargets} {
			for _, target := range targets {
				linkDoc, linkIn, linkOut = linkDoc+min(len(target.Doc), 1), linkIn+min(len(target.In), 1), linkOut+min(len(target.Out), 1)
				if target.Global {
					linkGlobal++
				}
				if len(target.Out) > 1 {
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
		for _, modifier := range dumps.Modifiers {
			modDoc, modCategories = modDoc+min(len(modifier.Doc), 1), modCategories+min(len(modifier.Categories), 1)
		}
		count("Modifier.Doc", modDoc)
		count("Modifier.Categories", modCategories)
		var modLabel, modSection, variables, fromCode, entries int
		for _, modifier := range dumps.Modifiers {
			modLabel, modSection = modLabel+min(len(modifier.Label), 1), modSection+min(len(modifier.Section), 1)
		}
		for _, scope := range dumps.Scopes {
			if scope.Variables {
				variables++
			}
		}
		for _, action := range dumps.OnActions {
			if action.FromCode {
				fromCode++
			}
		}
		for _, loc := range dumps.CustomLocalization {
			entries += min(len(loc.Entries), 1)
		}
		count("Modifier.Label", modLabel)
		count("Modifier.Section", modSection)
		count("ScopeType.Variables", variables)
		count("OnAction.FromCode", fromCode)
		count("CustomLoc.Entries", entries)
		count("DataTypes", len(dumps.DataTypes))
		count("Globals", len(dumps.Globals))

		// CK3's create_character declares its keys as a bare list, each with the dump's words
		if create := dumps.Effects["create_character"]; testCase.id == "ck3" && (create.Block == nil || create.Block.Keys["employer"] == nil || create.Block.Keys["mother"] == nil || create.Block.Keys["random_traits_list"] == nil) {
			t.Errorf("ck3: create_character's keys are not declared: %+v", create.Block)
		}

		// the prefix of every script-made database is one the game declares
		for database, prefix := range ScriptMade {
			if _, declared := dumps.DataTargets[prefix]; prefix != "" && !declared {
				t.Errorf("%s: %s are cited by %s:, which the game does not declare as a prefix", testCase.id, database, prefix)
			}
		}

		// every command of the hand table is declared by some game, checked over all three below
		for _, row := range nameCommands {
			if _, ok := dumps.Effects[row.Command]; ok {
				filled["token "+row.Command]++
			} else if _, ok := dumps.Triggers[row.Command]; ok {
				filled["token "+row.Command]++
			}
		}
	}

	for _, field := range []string{"EngineToken.Doc", "EngineToken.Usage", "EngineToken.In", "EngineToken.Out", "EngineToken.Traits", "EngineToken.Block.Members", "EngineToken.Block.Body", "Link.Doc", "Link.In", "Link.Out", "Link.Global",
		"Modifier.Doc", "Modifier.Categories", "Modifier.Label", "Modifier.Section", "ScopeType.Variables", "OnAction.FromCode", "CustomLoc.Entries", "DataTypes", "Globals"} {
		if filled[field] == 0 {
			t.Errorf("no dump of any game fills %s", field)
		}
	}
	for _, row := range nameCommands {
		if filled["token "+row.Command] == 0 {
			t.Errorf("no game declares the name token %s", row.Command)
		}
	}
}
