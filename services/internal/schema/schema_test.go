package schema

import (
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
)

// fixtureSchema builds the schema of a fixture install exactly as index.Scan builds it for the goldens: the
// install's files, the dump folders (empty for a fixture, so the shipped dump part stands in, marked stale),
// the english localization keys, and no wiki examples. The home is emptied, as the goldens' generator does,
// since the dump folders are found under the user's documents and not under the install.
func fixtureSchema(t *testing.T, id string) *Schema {
	t.Helper()
	home := t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, home)
	}
	root := filepath.Join("..", "testdata", id)
	install := fixtureFiles(id)
	dumps := inventory.Files("dumps", game.DumpDirs(id, root))

	// the keys of every english localization file, as the scan reads them before the schema stage
	locKeys := map[string]bool{}
	for _, file := range install {
		if file.Format != inventory.Loc {
			continue
		}
		if named := loc.LanguageFromRel(file.Rel); named != "" && named != game.BaseLanguage {
			continue
		}
		text, err := file.Text()
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range loc.Parse(text).Entries {
			locKeys[entry.Key] = true
		}
	}

	input := Input{Game: id, Version: "test", Install: install, Dumps: dumps, Updated: game.InstallUpdatedAt(root), LocKeys: locKeys}
	if len(MissingDumps(dumps)) > 0 {
		backup, exact, err := LoadShipped(id, "test")
		if err != nil {
			t.Fatal(err)
		}
		if backup != nil {
			state := backup.Sources[SourceDumps]
			state.Origin, state.Stale = "shipped", !exact
			backup.Sources[SourceDumps] = state
			input.Shipped = backup
		}
	}
	s, err := Build(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// fixtureFiles returns a fixture install's files in load order.
func fixtureFiles(id string) []inventory.File {
	return inventory.Files(game.OriginVanilla, inventory.Roots(filepath.Join("..", "testdata", id)))
}

// fixtureFile returns a fixture install's file at rel, parsed when it is script.
func fixtureFile(t *testing.T, id, rel string) (inventory.File, *jomini.Result) {
	t.Helper()
	for _, file := range fixtureFiles(id) {
		if file.Rel != rel {
			continue
		}
		if file.Format != inventory.Script {
			return file, nil
		}
		text, err := file.Text()
		if err != nil {
			t.Fatal(err)
		}
		parsed := jomini.Parse(text)
		return file, &parsed
	}
	t.Fatalf("no fixture file %s in %s", rel, id)
	return inventory.File{}, nil
}

const (
	ck3        = "ck3"
	eventsFile = "events/v_events.txt"
	v1         = "v.1"
	immediate  = "immediate"
	trigger    = "trigger"
	vEffect    = "v_effect"
)

func TestGoldenSchema(t *testing.T) {
	for _, id := range []string{ck3, "vic3", "eu5"} {
		t.Run(id, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("..", "testdata", "golden", "schema-"+id+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			got := asSaved(t, dumpSchema(fixtureSchema(t, id)))
			if got != string(want) {
				t.Fatalf("schema-%s.txt differs from the golden:\n%s", id, firstDifference(string(want), got))
			}
		})
	}
}

// asSaved writes a text as the goldens' generator saves it: the fixtures' path as <fixtures>, every
// backslash a slash.
func asSaved(t *testing.T, text string) string {
	root, err := filepath.Abs(filepath.Join("..", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	text = strings.ReplaceAll(text, strings.ReplaceAll(root, `\`, `\\`), "<fixtures>")
	text = strings.ReplaceAll(text, root, "<fixtures>")
	text = strings.ReplaceAll(text, `\\`, "/")
	return strings.ReplaceAll(text, `\`, "/")
}

// firstDifference shows the first line that differs and its line number.
func firstDifference(want, got string) string {
	wanted, gotten := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(wanted), len(gotten)) {
		line := func(lines []string) string {
			if i < len(lines) {
				return lines[i]
			}
			return "(end)"
		}
		if line(wanted) != line(gotten) {
			return fmt.Sprintf("line %d\nwant: %s\ngot:  %s", i+1, line(wanted), line(gotten))
		}
	}
	return "(none)"
}

// dumpSchema writes a schema as the goldens print it: enums as numbers, a claim as value/source.
func dumpSchema(s *Schema) string {
	var out strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&out, format+"\n", args...) }
	p("# enums are numbers; a Said prints value/source")
	p("game %s version %s", s.Game, s.Version)
	sources := make([]int, 0)
	for source := range s.Sources {
		sources = append(sources, int(source))
	}
	sort.Ints(sources)
	for _, source := range sources {
		origin := s.Sources[Source(source)]
		p("source %d from=%s", source, origin.Origin)
	}

	p("\n## databases")
	for _, name := range sortedKeys(s.Databases) {
		database := s.Databases[name]
		p("database %s fields=%v", name, sortedKeys(database.Fields))
		if database.Entry != nil {
			dumpKey(&out, "  ", "", database.Entry, 0)
		}
	}

	p("\n## commands")
	for _, name := range sortedKeys(s.Commands) {
		command := s.Commands[name]
		p("command %s kind=%d scopes=%v out=%q global=%t wild=%t lists=%t blurb=%s",
			name, command.Kinds, command.Scopes, command.Target, command.Global, command.Wild, command.Lists, short(command.Blurb))
		if command.Args != nil {
			dumpKey(&out, "  ", "", command.Args, 0)
		}
	}

	dumpRest(&out, s)
	return out.String()
}

// dumpRest writes the scopes, the dumps' other records, the calls, the prefixes and the scripted entries.
func dumpRest(out *strings.Builder, s *Schema) {
	p := func(format string, args ...any) { fmt.Fprintf(out, format+"\n", args...) }
	p("\n## the rest")
	for _, name := range sortedKeys(s.Scopes) {
		p("scope %s %v", name, s.Scopes[name])
	}
	p("saved %v", s.Saved)
	for _, name := range sortedKeys(s.Modifiers) {
		p("modifier %s %s", name, sum(s.Modifiers[name]))
	}
	for _, name := range sortedKeys(s.OnActions) {
		p("on_action %s %s", name, sum(s.OnActions[name]))
	}
	for _, name := range sortedKeys(s.DataTypes) {
		p("data_type %s %s", name, sum(s.DataTypes[name]))
	}
	for _, name := range sortedKeys(s.CustomLoc) {
		p("custom_loc %s %s", name, sum(s.CustomLoc[name]))
	}
	for _, name := range sortedKeys(s.Callees) {
		p("callee %s %v", name, s.Callees[name])
	}
	p("prefixes %v", s.Prefixes)
	p("lists %q", s.Lists)
	for _, name := range sortedKeys(s.ScriptedEntries) {
		macro := s.ScriptedEntries[name]
		p("macro %s database=%s", name, macro.Database)
		if macro.Parameters != nil {
			dumpKey(out, "  ", "", macro.Parameters, 0)
		}
	}
}

// dumpKey writes one position and, sorted by name, everything below it.
func dumpKey(out *strings.Builder, indent, path string, key *Position, depth int) {
	if depth > 40 {
		fmt.Fprintf(out, "%s%s !depth\n", indent, path)
		return
	}
	fmt.Fprintf(out, "%s%s value=%v/%d block=%d/%d scope=%q/%d required=%v/%d default=%q/%d blurb=%s/%d written=%d seen=in:%d,blocks:%d,forms:%d,values:%v\n",
		indent, cmpPath(path),
		key.Value.Value, key.Value.Source, key.Holds.Value, key.Holds.Source, key.Scope.Value, key.Scope.Source,
		key.Required.Value, key.Required.Source, key.Default.Value, key.Default.Source, short(key.Blurb.Value), key.Blurb.Source,
		key.DeclaredBy, key.Vanilla.Count, key.Vanilla.Blocks, key.Vanilla.Writes, key.Vanilla.Values)
	for _, name := range sortedKeys(key.Children) {
		dumpKey(out, indent, path+"/"+name, key.Children[name], depth+1)
	}
}

func cmpPath(path string) string {
	if path == "" {
		return "(entry)"
	}
	return path
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// short is a long text as its length, its checksum and its first words.
func short(text string) string {
	if len(text) <= 40 {
		return fmt.Sprintf("%q", text)
	}
	return fmt.Sprintf("%d:%08x:%q", len(text), crc32.ChecksumIEEE([]byte(text)), text[:40])
}

// sum is a value as the length and checksum of its printed form: a big table whose changed rows show by
// name.
func sum(value any) string {
	text := fmt.Sprintf("%v", value)
	return fmt.Sprintf("%d:%08x", len(text), crc32.ChecksumIEEE([]byte(text)))
}

// placementOf walks a fixture file with its entries and returns the reading of the first statement whose key is
// name under the given path of keys (an entry's name first); a step written key=value matches the value too.
func placementOf(t *testing.T, s *Schema, rel string, path ...string) Placement {
	t.Helper()
	file, parsed := fixtureFile(t, s.Game, rel)
	readings := map[jomini.Statement]Placement{}
	s.WalkFile(file, parsed, s.FindEntries(file, parsed), Visitor{Visit: func(placement *Placement) bool {
		readings[placement.Statement] = *placement
		return true
	}})
	return readings[statementAt(t, parsed.Root.Statements, path)]
}

// statementAt returns the first statement whose key is the path's last, following the path's keys down.
func statementAt(t *testing.T, statements []jomini.Statement, path []string) jomini.Statement {
	t.Helper()
	key, value, valued := strings.Cut(path[0], "=")
	for _, statement := range statements {
		assign, isAssign := statement.(*jomini.Assignment)
		if !isAssign || assign.Key.Text != key {
			continue
		}
		if scalar, isScalar := assign.Value.(*jomini.Scalar); valued && (!isScalar || scalar.Text != value) {
			continue
		}
		if len(path) == 1 {
			return statement
		}
		return statementAt(t, jomini.ChildBlock(statement).Statements, path[1:])
	}
	t.Fatalf("no statement %s", path[0])
	return nil
}

// snippetPlacement walks a snippet whose top statements are entries of a database and returns the reading of
// its first statement whose key is name.
func snippetPlacement(s *Schema, database, snippet, name string, create bool) Placement {
	parsed := jomini.Parse(snippet)
	var found Placement
	visitor := Visitor{Create: create, Visit: func(placement *Placement) bool {
		if placement.Name == name && found.Statement == nil {
			found = *placement
		}
		return true
	}}
	for _, statement := range parsed.Root.Statements {
		if block := jomini.ChildBlock(statement); block != nil {
			s.WalkBlock(visitor, block, block.Statements, s.EntryContext(s.Databases[database]))
		}
	}
	return found
}

func TestKeyMeaning(t *testing.T) {
	s := fixtureSchema(t, ck3)
	events, triggers, values := eventsFile, "common/scripted_triggers/v_triggers.txt", "common/script_values/v_values.txt"
	cases := []struct {
		name      string
		placement Placement
		rule      game.KeyRule
		position  *Position
		command   *Command
		calls     string
	}{
		{"a fields block reads every key as a position", placementOf(t, s, events, v1, "type"),
			game.KeyRulePosition, s.Databases["events"].Entry.Child("type"), nil, ""},
		{"a declared effect", placementOf(t, s, events, v1, immediate, "add_gold"),
			game.KeyRuleCommand, s.Commands["add_gold"].Args, s.Commands["add_gold"], ""},
		{"a scripted effect vanilla defines", placementOf(t, s, events, v1, immediate, vEffect),
			game.KeyRuleKnownCall, s.ScriptedEntries["effect:v_effect"].Parameters, nil, "effect"},
		{"a scripted trigger declared inline", placementOf(t, s, triggers, "v_rich_trigger", "OR", "trigger_if", "v_inline_trigger"),
			game.KeyRuleKnownCall, s.ScriptedEntries["trigger:v_inline_trigger"].Parameters, nil, trigger},
		{"a link", placementOf(t, s, events, v1, immediate, "capital_county"),
			game.KeyRuleLink, nil, s.Commands["capital_county"], ""},
		{"an argument the dumps declare", placementOf(t, s, events, v1, immediate, "send_interface_toast", "title"),
			game.KeyRuleDeclared, s.Commands["send_interface_toast"].Args.Child("title"), nil, ""},
		{"an argument vanilla writes", placementOf(t, s, events, v1, immediate, "send_interface_toast", "message_filter_type"),
			game.KeyRuleObserved, s.Commands["send_interface_toast"].Args.Child("message_filter_type"), nil, ""},
		{"a value operator", placementOf(t, s, values, "big_value", "add"),
			game.KeyRuleOperator, s.Databases["script_values"].Entry.Child("add"), nil, ""},
		{"a call where nothing is known", snippetPlacement(s, "scripted_triggers", "x = { nothing_declares_me = yes }", "nothing_declares_me", false),
			game.KeyRuleCall, nil, nil, trigger},
	}
	for _, c := range cases {
		placement := c.placement
		if placement.Rule != c.rule || placement.Position != c.position || placement.Command != c.command || placement.Calls != c.calls {
			t.Errorf("%s: rule %d position %p command %p calls %q; want %d %p %p %q",
				c.name, placement.Rule, placement.Position, placement.Command, placement.Calls, c.rule, c.position, c.command, c.calls)
		}
	}

	declared := placementOf(t, s, events, v1, immediate, "send_interface_toast", "title").Position
	if declared.DeclaredBy != SourceDumps {
		t.Errorf("title is declared by %d, want the dumps", declared.DeclaredBy)
	}
	observed := placementOf(t, s, events, v1, immediate, "send_interface_toast", "message_filter_type").Position
	if observed.DeclaredBy != SourceNone || observed.Vanilla.Count != 1 {
		t.Errorf("message_filter_type is declared by %d, written in %d blocks; want none, 1", observed.DeclaredBy, observed.Vanilla.Count)
	}

	// a new position with no claim and no evidence holds an unknown block, whose parent writes no command
	fresh := snippetPlacement(fixtureSchema(t, ck3), "events", "x = { fresh = { inner = 1 } }", "inner", true)
	if fresh.Rule != game.KeyRulePositionOrCall || fresh.Calls != "" {
		t.Errorf("inner: rule %d calls %q; want %d \"\"", fresh.Rule, fresh.Calls, game.KeyRulePositionOrCall)
	}
}

func TestBlockKindOf(t *testing.T) {
	s := fixtureSchema(t, ck3)
	entry := s.Databases["events"].Entry
	cases := []struct {
		name     string
		position *Position
		want     game.Shape
	}{
		{"an entry's registry claim", entry, game.ShapeFields},
		{"vanilla writes only triggers", entry.Child(trigger), game.ShapeTriggers},
		{"vanilla writes only effects", entry.Child(immediate), game.ShapeEffects},
		{"vanilla writes keys and no command", entry.Child("cooldown"), game.ShapeFields},
		{"no claim, no children", s.Commands["add_gold"].Args, game.ShapeUnknown},
		{"no position", nil, game.ShapeUnknown},
	}
	for _, c := range cases {
		if got := blockKindOf(c.position); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

func TestCommandBlockKind(t *testing.T) {
	s := fixtureSchema(t, ck3)
	cases := []struct {
		command string
		in      game.Shape
		want    game.Shape
	}{
		{"if", game.ShapeEffects, game.ShapeEffects},
		{"if", game.ShapeTriggers, game.ShapeEffects},
		{"exists", game.ShapeTriggers, game.ShapeTriggers},
		{"random_list", game.ShapeEffects, game.ShapeArguments},
	}
	for _, c := range cases {
		if got := commandBlockKind(s.Commands[c.command], c.in); got != c.want {
			t.Errorf("%s in %d: %d, want %d", c.command, c.in, got, c.want)
		}
	}
}

func TestBlockScope(t *testing.T) {
	s := fixtureSchema(t, ck3)
	events := eventsFile
	cases := []struct {
		name      string
		placement Placement
		want      string
	}{
		{"an iterator leads to its target", placementOf(t, s, events, v1, immediate, "every_vassal"), "character"},
		{"a link leads where its last link does", placementOf(t, s, events, v1, immediate, "capital_county"), "landed_title"},
		{"the events entry declares no scope", placementOf(t, s, events, v1, immediate, "add_gold"), ""},
		{"a trigger in an unscoped block", placementOf(t, s, events, v1, trigger, "exists"), ""},
		{"an on_action starts in its declared scope", placementOf(t, s, "common/on_action/v_on_actions.txt", "on_birth", "effect", "every_vassal"), "character"},
	}
	for _, c := range cases {
		if got := c.placement.Inner.Scope; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMayAccuse(t *testing.T) {
	s := &Schema{Sources: map[Source]SourceState{SourceDocs: {Origin: "install"}, SourceDumps: {Origin: "yours", Stale: true}}}
	number := func(source Source, in int, forms Writes) *Position {
		return &Position{Claims: Claims{Value: Claim[Expected]{Expected{Type: ValueNumber}, source}}, Vanilla: Vanilla{Count: in, Writes: forms}}
	}
	cases := []struct {
		name     string
		check    Check
		position *Position
		want     bool
	}{
		{"a docs claim where vanilla never writes", CheckValue, number(SourceDocs, 0, 0), false},
		{"a docs claim vanilla agrees with", CheckValue, number(SourceDocs, 1, WritesNumber), true},
		{"a docs claim vanilla contradicts", CheckValue, number(SourceDocs, 1, WritesNumber|WritesIdent), false},
		{"vanilla never accuses", CheckValue, number(SourceVanilla, 1, WritesNumber), false},
		{"stale dumps never accuse", CheckCommand, &Position{}, false},
	}
	for _, c := range cases {
		if got := s.MayAccuse(c.check, c.position, nil); got != c.want {
			t.Errorf("%s: %t, want %t", c.name, got, c.want)
		}
	}
}

func TestFindEntries(t *testing.T) {
	cases := []struct {
		game, rel string
		want      []string // database and name of each entry, in source order
	}{
		{ck3, eventsFile, []string{"events v.1", "events v.2"}},
		{ck3, "common/landed_titles/v_titles.txt", titles("e_x 1", "k_x 2", "c_x 3", "c_y 4", "e_y 5", "k_y 6")},
		{ck3, "history/characters/v_characters.txt", []string{
			"history/characters 186004", "history/characters 186009", "history/characters 186010",
		}},
		{"vic3", "common/history/countries/v_countries.txt", []string{"history "}},
		{ck3, "gui/v_window.gui", []string{
			"gui.type v_button", "gui.type v_label", "gui.template v_plain", "gui.template v_faded",
			"gui.widget v_window", "gui.widget v_other_window",
		}},
	}
	for _, c := range cases {
		s := fixtureSchema(t, c.game)
		file, parsed := fixtureFile(t, c.game, c.rel)
		var got []string
		for _, entry := range s.FindEntries(file, parsed) {
			got = append(got, entry.Database+" "+entry.ID)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%s %s:\n got %v\nwant %v", c.game, c.rel, got, c.want)
		}
	}
}

// titles lists the entries of the landed titles fixture: each title's name, its color key, and the three
// numbers of the color.
func titles(pairs ...string) (entries []string) {
	for _, pair := range pairs {
		name, color, _ := strings.Cut(pair, " ")
		entries = append(entries, "landed_titles "+name, "landed_titles color")
		entries = append(entries, slices.Repeat([]string{"landed_titles " + color}, 3)...)
	}
	return entries
}

func TestLoadShipped(t *testing.T) {
	s, exact, err := LoadShipped(ck3, "test")
	if err != nil || s == nil || exact {
		t.Errorf("ck3 test: %v, exact %t, error %v; want a schema, not exact", s != nil, exact, err)
	}
	if s, _, err := LoadShipped("nosuchgame", "1"); s != nil || err != nil {
		t.Errorf("nosuchgame: %v, %v; want nil, nil", s, err)
	}
}

func TestOccurrences(t *testing.T) {
	s := fixtureSchema(t, ck3)
	ref := func(database, name string, start, end int) Occurrence {
		return Occurrence{Role: Refers, Database: database, ID: name, Range: jomini.Range{Start: start, End: end}}
	}
	def := func(database, name string, start, end int) Occurrence {
		return Occurrence{Role: Defines, Database: database, ID: name, Range: jomini.Range{Start: start, End: end}}
	}
	call := func(called, name string, start, end int) Occurrence {
		return Occurrence{Role: Refers, Caller: called, ID: name, Range: jomini.Range{Start: start, End: end}}
	}
	const (
		effects  = "common/scripted_effects/v_effects.txt"
		triggers = "common/scripted_triggers/v_triggers.txt"
		more     = "events/v_more_events.txt"
	)
	cases := []struct {
		rel          string
		path         []string
		want         []Occurrence
		nameDatabase string
	}{
		{eventsFile, []string{v1, trigger, "exists"}, []Occurrence{ref("scope", "target", 166, 178)}, ""},
		{eventsFile, []string{v1, trigger, "exists=title:k_x"}, []Occurrence{ref("landed_titles", "k_x", 215, 224)}, ""},
		{eventsFile, []string{v1, trigger, "faith"}, nil, ""},
		{eventsFile, []string{v1, immediate, "save_scope_as"}, []Occurrence{def("scope", "target", 360, 366)}, "scope"},
		{eventsFile, []string{v1, immediate, "set_variable", "name"}, []Occurrence{def("var", "seen", 325, 329)}, "var"},
		{eventsFile, []string{v1, immediate, "trigger_event"}, []Occurrence{ref("events", "v.2", 385, 388)}, ""},
		{eventsFile, []string{v1, immediate, vEffect}, []Occurrence{call("effect", vEffect, 645, 653)}, ""},
		{eventsFile, []string{v1, immediate, vEffect, "AMOUNT"},
			[]Occurrence{ref(Parameter, "v_effect:AMOUNT", 658, 664)}, ""},
		{eventsFile, []string{v1, "title"}, []Occurrence{ref(Localization, "v.1.t", 83, 88)}, ""},
		{"common/traits/v_traits.txt", []string{"brave", "icon"},
			[]Occurrence{ref(File, "gfx/interface/icons/traits/brave.dds", 57, 95)}, ""},
		{effects, []string{vEffect, "add_gold"}, []Occurrence{def(Parameter, "v_effect:AMOUNT", 25, 33)}, ""},
		{effects, []string{"v_list_effect", "every_in_list", "variable"}, []Occurrence{ref("var", "seen_list", 183, 192)}, "var"},
		{triggers, []string{"v_rich_trigger", "OR", "gold"}, []Occurrence{ref("script_values", "big_value", 71, 80)}, ""},
		{triggers, []string{"v_rich_trigger", "big_value"}, []Occurrence{call(trigger, "big_value", 162, 171)}, ""},
		{"history/titles/v_titles.txt", []string{"k_x"}, []Occurrence{ref("landed_titles", "k_x", 0, 3)}, ""},
		{"history/provinces/v_provinces.txt", []string{"1", "culture"}, []Occurrence{ref("culture/cultures", "norse", 17, 22)}, ""},
		{more, []string{"vmore.1", trigger, "gold"}, []Occurrence{ref("defines", "NWar", 208, 228)}, ""},
	}
	for _, c := range cases {
		placement := placementOf(t, s, c.rel, c.path...)
		for i := range placement.Occurrences {
			placement.Occurrences[i].Entry, placement.Occurrences[i].InTrigger = 0, false // the entry and trigger state are the placement's
		}
		if !slices.Equal(placement.Occurrences, c.want) || placement.StoredIn != c.nameDatabase {
			t.Errorf("%s %v:\n got %+v %q\nwant %+v %q", c.rel, c.path, placement.Occurrences, placement.StoredIn, c.want, c.nameDatabase)
		}
	}
}

func TestWalkFileEntries(t *testing.T) {
	s := fixtureSchema(t, ck3)
	const more = "events/v_more_events.txt"
	if entry := placementOf(t, s, more, "vmore.1", trigger, "v_inline_trigger").Entry; entry != 1 {
		t.Errorf("vmore.1 › trigger › v_inline_trigger: entry %d, want 1", entry)
	}
	if entry := placementOf(t, s, more, "namespace").Entry; entry != -1 {
		t.Errorf("namespace: entry %d, want -1", entry)
	}
}
