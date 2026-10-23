package schema

import (
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A visit is one statement the recorder was offered, with the context the walk gave it.
type visit struct {
	key     string
	value   string
	context Context
}

// A recorder is a visitor that keeps every statement it is offered.
type recorder struct {
	visits []visit
}

// Enter keeps the statement and its context, its references copied, and goes on into its block.
func (recorder *recorder) Enter(tree *jomini.Tree, statement int32, context *Context) bool {
	kept := *context
	kept.References = slices.Clone(context.References)
	kept.Saved = slices.Clone(context.Saved)
	current := tree.Statements[statement]
	recorder.visits = append(recorder.visits, visit{key: tree.Text(current.Key), value: tree.Text(current.Value), context: kept})
	return true
}

// Leave does nothing.
func (recorder *recorder) Leave(tree *jomini.Tree, statement int32, context *Context) {}

// find returns the first statement kept with the given key and value; "" matches any value.
func (recorder *recorder) find(t *testing.T, key, value string) visit {
	t.Helper()
	for _, seen := range recorder.visits {
		if seen.key == key && (value == "" || seen.value == value) {
			return seen
		}
	}
	t.Fatalf("no statement %s = %s", key, value)
	return visit{}
}

// parseFile parses a file from disk, as the workspace does.
func parseFile(path string) (*jomini.Tree, error) {
	text, err := files.ReadText(path)
	return jomini.Parse(text.Text), err
}

// listInstall returns every file under an install's roots.
func listInstall(t *testing.T, install string) []string {
	t.Helper()
	var paths []string
	for _, root := range game.InstallRoots {
		dir := filepath.Join(install, root)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		listing, err := files.Walk(files.Root{Dir: dir, Origin: game.OriginVanilla}, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range listing {
			paths = append(paths, file.Path)
		}
	}
	return paths
}

// buildFixture builds the schema of the CK3 fixture install.
func buildFixture(t *testing.T) (*Schema, string) {
	t.Helper()
	install, err := filepath.Abs(filepath.Join("..", "testdata", "ck3"))
	if err != nil {
		t.Fatal(err)
	}
	built, err := Build(game.LookupGame("ck3"), install, listInstall(t, install), parseFile)
	if err != nil {
		t.Fatal(err)
	}
	return built, install
}

// openFixture parses one file of the CK3 fixture install.
func openFixture(t *testing.T, install, rel string) (*jomini.Tree, files.File) {
	t.Helper()
	path := filepath.Join(install, game.ScriptRoot, filepath.FromSlash(rel))
	tree, err := parseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return tree, files.File{Path: path, Rel: rel, Origin: game.OriginVanilla, Format: files.Classify(rel)}
}

// walkFixture walks one file of the CK3 fixture install and returns what the walk offered.
func walkFixture(t *testing.T, built *Schema, install, rel string) *recorder {
	t.Helper()
	tree, file := openFixture(t, install, rel)
	seen := &recorder{}
	built.Walk(tree, file, seen)
	return seen
}

// walkFixtureTo walks one file of the CK3 fixture install to a byte offset and returns what the walk offered.
func walkFixtureTo(t *testing.T, built *Schema, install, rel string, offset int) *recorder {
	t.Helper()
	tree, file := openFixture(t, install, rel)
	seen := &recorder{}
	built.WalkTo(tree, file, offset, seen)
	return seen
}

// TestReadDumpsFixtures checks that the dumps of each game's fixture are read in the form that game writes them.
func TestReadDumpsFixtures(t *testing.T) {
	for _, gameID := range []string{"ck3", "vic3", "eu5"} {
		dumps, err := ReadDumps([]string{filepath.Join("..", "testdata", gameID, "docs")})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(dumps.Commands, func(command DumpCommand) bool { return command.Declared == game.DeclaredEffect }) {
			t.Errorf("%s: no effect read", gameID)
		}
	}
	dumps, _ := ReadDumps([]string{filepath.Join("..", "testdata", "ck3", "docs")})
	lookup := func(name string) DumpCommand {
		i := slices.IndexFunc(dumps.Commands, func(command DumpCommand) bool { return command.Name == name })
		if i < 0 {
			t.Fatalf("ck3: no command %s", name)
		}
		return dumps.Commands[i]
	}
	if root := lookup("root"); !root.Global || strings.Contains(root.Description, "Global Link") {
		t.Errorf("root: global %v, description %q", root.Global, root.Description)
	}
	if scope := lookup("scope"); !scope.Prefixed {
		t.Error("scope: not read as requiring data")
	}
	if len(dumps.Scopes) == 0 {
		t.Error("ck3: no scope types read")
	}
}

// The paths the doc tests read from: a .info file and an EU5-style readme.
const (
	briefDoc  = "x/_x.info"
	readmeDoc = "x/readme.txt"
)

// newDocSchema returns a schema that knows the scope types the doc tests resolve the docs' scope phrases to.
func newDocSchema() *Schema {
	docs := &Schema{Scopes: map[string]ScopeType{}}
	for _, scope := range []string{"character", "location", "country", "interest_group", "story_cycle"} {
		docs.Scopes[scope] = ScopeType{Name: scope}
	}
	return docs
}

// TestParseDoc checks that each way the games' doc files are written gives its facts as claims.
func TestParseDoc(t *testing.T) {
	docs := newDocSchema()
	cases := []struct {
		name, rel, text, key string
		check                func(*Field) bool
	}{
		{"brief header", briefDoc, "# ### Brief: cooldown ( duration, optional )\n# How long until it can be taken again.\n# Default: 0\ncooldown = { days = 10 }\n",
			"cooldown", func(field *Field) bool {
				return field.Required.Value.Level == "optional" && field.Default.Value == "0" && strings.Contains(field.Blurb.Value, "How long")
			}},
		{"commented-out template", readmeDoc, "#<advance ID> = {\n#\tage = <age>\t\t# Determines in which age this advance is available\n#}\n",
			"age", func(field *Field) bool { return strings.HasPrefix(field.Blurb.Value, "Determines") }},
		{"markdown with alternatives", "x/x.md", "# Reference\nA building of a state.\n\nexample = {\n\tbuildable = yes/no # can this building be built? default yes\n}\n",
			"buildable", func(field *Field) bool { return field.Value.Value.Type == ValueBool && field.Default.Value == "yes" }},
		{"value list", briefDoc, "key = {\n\tcategory = common # the category\n}\n# Possible values:\n# - common (default)\n# - skilled\n",
			"category", func(field *Field) bool {
				return slices.Equal(field.Value.Value.Choices, []string{"common", "skilled"}) && field.Default.Value == "common"
			}},
		{"key and text", readmeDoc, "# fire_only_once: yes/no - whether the disaster can occur again\n",
			"fire_only_once", func(field *Field) bool { return strings.Contains(field.Blurb.Value, "occur again") }},
		{"inline root", readmeDoc, "#trait = {\n#\tallow = { } # trigger to decide if it is allowed (root = character)\n#}\n",
			"allow", func(field *Field) bool { return field.Scope.Value == "character" }},
		{"comment run above", briefDoc, "key = {\n\t# full path to the icon. Required, an error is logged if missing\n\ticon = \"gfx/x.dds\"\n}\n",
			"icon", func(field *Field) bool {
				return field.Required.Value.Level == "always" && field.Value.Value.Type == ValueText
			}},
		{"bullet writing a statement", readmeDoc, "# - always_add_demands = <yes/no> when set to yes the building needs full demand\n",
			"always_add_demands", func(field *Field) bool {
				return field.Value.Value.Type == ValueBool && strings.HasPrefix(field.Blurb.Value, "when set to yes")
			}},
		{"number key", briefDoc, "key = {\n\tranks = {\n\t\t1 = {\n\t\t\tmen_at_arms = x # the men at arms\n\t\t}\n\t}\n}\n",
			"ranks/<number>/men_at_arms", func(field *Field) bool { return field.Blurb.Value == "the men at arms" }},
		{"header type with a required term", briefDoc, "# ### Brief: can_only_start_via_script ( optional bool )\ncan_only_start_via_script = yes\n",
			"can_only_start_via_script", func(field *Field) bool {
				return field.Value.Value.Type == ValueBool && field.Required.Value.Level == "optional"
			}},
		{"type term with underscores", readmeDoc, "#\ttraded_quantity = fixed_point # how many units are moved\n",
			"traded_quantity", func(field *Field) bool { return field.Value.Value.Type == ValueNumber }},
		{"placeholder opening a bullet", readmeDoc, "# - allow: <trigger> can the building be built (root = location)\n",
			"allow", func(field *Field) bool { return field.Shape.Value == shapeTriggers && field.Scope.Value == "location" }},
		{"shape opening the text above", briefDoc, "key = {\n\t# Effect is executed when the owner dies\n\t# root - active story cycle\n\ton_owner_death = {}\n}\n",
			"on_owner_death", func(field *Field) bool {
				return field.Shape.Value == shapeEffects && field.Scope.Value == "story_cycle"
			}},
		{"nested at its path", briefDoc, "key = {\n\tranks = {\n\t\tliege_modifier = { } # what the liege gets\n\t}\n}\n",
			"ranks/liege_modifier", func(field *Field) bool { return field.Blurb.Value == "what the liege gets" }},
		{"header heads a nested statement", briefDoc, "key = {\n\toption = {\n\t\t# ### Brief: weight ( int )\n\t\tweight = 1\n\t}\n}\n",
			"option/weight", func(field *Field) bool { return field.Value.Value.Type == ValueNumber }},
	}
	for _, each := range cases {
		reader, ok := docs.parseDoc(each.text, each.rel)
		if !ok {
			t.Errorf("%s: not read as a doc", each.name)
			continue
		}
		if field := reader.fields[each.key]; field == nil || !each.check(field) {
			t.Errorf("%s: %+v", each.name, field)
		}
	}
}

// TestParseDocDatabase checks what a doc file says of its database as a whole: its blurb and root scope, and that a
// data file named like a doc is not read as one.
func TestParseDocDatabase(t *testing.T) {
	docs := newDocSchema()
	text := "# Reference\nA building of a state.\n\nexample = {\n\t# root = owning country or no scope\n\tgroup = x\n}\n"
	reader, ok := docs.parseDoc(text, "x/x.md")
	if !ok || strings.Join(reader.blurb, " ") != "A building of a state." || reader.scope != "country" {
		t.Fatalf("database blurb and root: %v", reader)
	}
	if _, ok := docs.parseDoc("a = 1\nb = 2\nc = { d = 3 }\n", "x/00_example_templates.txt"); ok {
		t.Error("a data file named like a doc is read as one")
	}
	if scope := docs.resolveScopePhrase("interest group"); scope != "interest_group" {
		t.Errorf("a scope of two terms: %q", scope)
	}
}

// TestBuildFixtureLearns checks what Build learns from the fixture install: the events database and the docs of a
// database.
func TestBuildFixtureLearns(t *testing.T) {
	built, _ := buildFixture(t)
	if built.Events != "events" {
		t.Errorf("events database: %q", built.Events)
	}
	if traits := built.Databases["traits"]; traits == nil || len(traits.Documented) == 0 {
		t.Error("traits: no documented keys")
	}
}

// TestWalkEventsFixture checks the walk over the fixture's events: a definition, an event reference, a link, a
// saved scope and a keyword reached through the number form.
func TestWalkEventsFixture(t *testing.T) {
	built, install := buildFixture(t)
	seen := walkFixture(t, built, install, "events/v_events.txt")
	if event := seen.find(t, "v.1", ""); !event.context.Defines || event.context.Definition.Database != "events" {
		t.Errorf("v.1: %+v", event.context)
	}
	trigger := seen.find(t, "trigger_event", "v.2")
	if !slices.ContainsFunc(trigger.context.References, func(reference Reference) bool {
		return reference.Database == "events" && reference.Name == "v.2"
	}) {
		t.Errorf("trigger_event = v.2: %+v", trigger.context.References)
	}
	if county := seen.find(t, "capital_county", ""); county.context.Category != CategoryLink {
		t.Errorf("capital_county: %q", county.context.Category)
	}
	if saved := seen.find(t, "save_scope_as", "target"); !saved.context.Defines || saved.context.Definition.Database != game.SavedScopes {
		t.Errorf("save_scope_as = target: %+v", saved.context)
	}
	if branch := seen.find(t, "90", ""); branch.context.Category != CategoryKeyword {
		t.Errorf("random_list's 90: %q", branch.context.Category)
	}
	if shape := resolveWriteShape(map[string]int{shapeValue: 2, "triggers": 1}); shape != shapeValue {
		t.Errorf("a block of value keys and triggers: %q", shape)
	}
	if shape := resolveWriteShape(map[string]int{"fields": 2, "triggers": 1}); shape != shapeUnknown {
		t.Errorf("a block of fields and triggers: %q", shape)
	}
}

// TestWalkToKeepsSaved checks that a walk to an offset knows the scopes saved before it, as the whole walk does.
func TestWalkToKeepsSaved(t *testing.T) {
	built, install := buildFixture(t)
	text, err := os.ReadFile(filepath.Join(install, game.ScriptRoot, "events", "v_events.txt"))
	if err != nil {
		t.Fatal(err)
	}
	offset := strings.Index(string(text), "add_character_flag = seen_one")
	whole := walkFixture(t, built, install, "events/v_events.txt").find(t, "add_character_flag", "seen_one")
	part := walkFixtureTo(t, built, install, "events/v_events.txt", offset).find(t, "add_character_flag", "seen_one")
	if len(whole.context.Saved) == 0 || !slices.Equal(whole.context.Saved, part.context.Saved) {
		t.Errorf("Walk %v, WalkTo %v", whole.context.Saved, part.context.Saved)
	}
}

// TestResolveMacroReplacesParameters checks that a call to a macro stands for its text, the parameter replaced by
// the argument written for it.
func TestResolveMacroReplacesParameters(t *testing.T) {
	macros := &Schema{DataTypes: map[string]DataType{game.GlobalType: {Functions: map[string]DataFunction{
		"IsZero": {Name: "IsZero", Arguments: []string{"Value"}, Expansion: "EqualTo_int32(Value, '(int32)0')"},
	}}}}
	call, ok := jomini.ParseCall("IsZero(GetPlayer.GetGold)", 0)
	if !ok {
		t.Fatal("call not parsed")
	}
	expanded := macros.ResolveMacro(call, "")
	if len(expanded.Steps) != 1 || expanded.Steps[0].Text != "EqualTo_int32" {
		t.Fatalf("steps: %+v", expanded.Steps)
	}
	argument := expanded.Steps[0].Arguments[0]
	if argument.Call == nil || argument.Call.Steps[0].Text != "GetPlayer" || argument.Span.IsEmpty() {
		t.Errorf("first argument: %+v", argument)
	}
}

// TestListInputsStatedOnly checks that the Form view offers the keys a source states, never a form.
func TestListInputsStatedOnly(t *testing.T) {
	definition := &Field{}
	definition.buildChild("stated").Value.resolve(Expected{Type: ValueBool}, game.SourceDocs)
	definition.buildChild("unstated")
	definition.buildChild("<number>").Value.resolve(Expected{Type: ValueNumber}, game.SourceVanilla)
	inputs := (&Schema{Databases: map[string]*Database{"x": {Definition: definition}}}).ListInputs("x")
	if len(inputs) != 1 || inputs[0].Key != "stated" || inputs[0].Type != ValueBool {
		t.Errorf("inputs: %+v", inputs)
	}
	if (&Schema{Databases: map[string]*Database{}}).ListInputs("missing") != nil {
		t.Error("a missing database has inputs")
	}
}

// TestBuildInstalls builds the schema of each real install that PMT_TEST_INSTALL_<GAME> names, and prints what
// each source gave. It asserts that the build succeeds and the dumps declare commands; the counts are read.
func TestBuildInstalls(t *testing.T) {
	for _, gameID := range []string{"ck3", "vic3", "eu5"} {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}
			paths := listInstall(t, install)
			start := time.Now()
			built, err := Build(game.LookupGame(gameID), install, paths, parseFile)
			if err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(start)
			if len(built.Commands) == 0 {
				t.Fatal("no commands")
			}
			sources, facts := map[game.Source]int{}, map[string]int{}
			built.walkFields(func(field *Field) {
				sources[field.Source]++
				for name, claim := range map[string]game.Source{
					"blurb": field.Blurb.Source, "value": field.Value.Source, "default": field.Default.Source,
					"required": field.Required.Source, "scope": field.Scope.Source,
				} {
					if claim == game.SourceDocs {
						facts[name]++
					}
				}
			})
			t.Logf("%d files in %s: %d databases, %d commands, %d saved from code, %d scripted definitions",
				len(paths), elapsed.Round(time.Millisecond), len(built.Databases), len(built.Commands),
				len(built.Saved), len(built.ScriptedDefinitions))
			t.Logf("fields by source: %d none, %d dumps, %d docs, %d vanilla; docs facts: %v",
				sources[game.SourceNone], sources[game.SourceDumps], sources[game.SourceDocs], sources[game.SourceVanilla], facts)
			var cached bytes.Buffer
			if err := gob.NewEncoder(&cached).Encode(built); err != nil {
				t.Fatal(err)
			}
			t.Logf("cached: %d bytes", cached.Len())
		})
	}
}
