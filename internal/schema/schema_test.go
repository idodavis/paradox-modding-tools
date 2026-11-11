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

// visit is one statement of a walked file, with the context the walk gave
// it.
type visit struct {
	key     string
	value   string
	context Context
}

// recorder holds every statement of a walked file.
type recorder struct {
	visits []visit
}

// find returns the first statement kept with the given key and value; ""
// matches any value.
func (r *recorder) find(t *testing.T, key, value string) visit {
	t.Helper()

	for _, seen := range r.visits {
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

// listInstall returns every file under an install's roots, its DLC folders
// included. A DLC's files are listed under the game's root too, and Build
// leaves those copies out.
func listInstall(t *testing.T, row *game.Game, install string) []files.File {
	t.Helper()

	roots, err := row.ListInstallRoots(install)
	if err != nil {
		t.Fatal(err)
	}

	var listing []files.File
	for _, root := range roots {
		list, err := files.Walk(root)
		if err != nil {
			t.Fatal(err)
		}

		listing = append(listing, list...)
	}

	return listing
}

// buildFixture builds the schema of the CK3 fixture install.
func buildFixture(t *testing.T) (*Schema, string) {
	t.Helper()

	install, err := filepath.Abs(filepath.Join("..", "testdata", "ck3"))
	if err != nil {
		t.Fatal(err)
	}

	row := game.LookupGame("ck3")
	built, err := Build(row, install, listInstall(t, row, install), parseFile)
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

	return tree, files.File{Path: path, Rel: rel, Origin: files.OriginVanilla, Format: files.Classify(rel)}
}

// walkFixture walks one file of the CK3 fixture install and returns its
// statements with their contexts. The file's own comes first only where the
// file is one definition.
func walkFixture(t *testing.T, built *Schema, install, rel string) *recorder {
	t.Helper()

	tree, file := openFixture(t, install, rel)
	seen := &recorder{}
	for statement, context := range built.Walk(tree, file, nil) {
		if statement == 0 && context.Definition == nil {
			continue
		}

		current := tree.Statements[statement]
		seen.visits = append(seen.visits, visit{key: tree.Text(current.Key), value: tree.Text(current.Value), context: context})
	}

	return seen
}

// TestReadDumpsFixtures checks that the dumps of each game's fixture are read.
func TestReadDumpsFixtures(t *testing.T) {
	for _, gameID := range []string{"ck3", "vic3", "eu5"} {
		dumps, err := ReadDumps([]string{filepath.Join("..", "testdata", gameID, "docs")})
		if err != nil {
			t.Fatal(err)
		}

		if !slices.ContainsFunc(dumps.Commands, func(command DumpCommand) bool { return command.Declared == declaredEffect }) {
			t.Errorf("%s: no effect read", gameID)
		}
	}

	dumps, _ := ReadDumps([]string{filepath.Join("..", "testdata", "ck3", "docs")})
	lookup := func(name string) DumpCommand {
		position := slices.IndexFunc(dumps.Commands, func(command DumpCommand) bool { return command.Name == name })
		if position < 0 {
			t.Fatalf("ck3: no command %s", name)
		}

		return dumps.Commands[position]
	}

	if root := lookup("root"); strings.Contains(root.Description, "Global Link") {
		t.Errorf("root: description %q", root.Description)
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

// TestParseDoc checks that each way the games write doc files gives its
// facts as claims.
func TestParseDoc(t *testing.T) {
	docs := &Schema{}
	cases := []struct {
		name, rel, text, key string
		check                func(*Field) bool
	}{
		{"brief header", briefDoc, "# ### Brief: cooldown ( duration, optional )\n# How long until it can be taken again.\n# Default: 0\ncooldown = { days = 10 }\n",
			"cooldown", func(field *Field) bool {
				return field.Required.Value == "optional" && field.Default.Value == "0" && strings.Contains(field.Blurb.Value, "How long")
			}},
		{"commented-out template", readmeDoc, "#<advance ID> = {\n#\tage = <age>\t\t# Determines in which age this advance is available\n#}\n",
			"age", func(field *Field) bool { return strings.HasPrefix(field.Blurb.Value, "Determines") }},
		{"markdown with alternatives", "x/x.md", "# Reference\nA building of a state.\n\nexample = {\n\tbuildable = yes/no # can this building be built? default yes\n}\n",
			"buildable", func(field *Field) bool {
				return field.Value.Value.Type == ValueTypeBool && field.Default.Value == "yes"
			}},
		{"value list", briefDoc, "key = {\n\tcategory = common # the category\n}\n# Possible values:\n# - common (default)\n# - skilled\n",
			"category", func(field *Field) bool {
				return slices.Equal(field.Value.Value.Choices, []string{"common", "skilled"}) && field.Default.Value == "common"
			}},
		{"key and text", readmeDoc, "# fire_only_once: yes/no - whether the disaster can occur again\n",
			"fire_only_once", func(field *Field) bool { return strings.Contains(field.Blurb.Value, "occur again") }},
		{"comment run above", briefDoc, "key = {\n\t# full path to the icon. Required, an error is logged if missing\n\ticon = \"gfx/x.dds\"\n}\n",
			"icon", func(field *Field) bool {
				return field.Required.Value == "always" && field.Value.Value.Type == ValueTypeText
			}},
		{"bullet writing a statement", readmeDoc, "# - always_add_demands = <yes/no> when set to yes the building needs full demand\n",
			"always_add_demands", func(field *Field) bool {
				return field.Value.Value.Type == ValueTypeBool && strings.HasPrefix(field.Blurb.Value, "when set to yes")
			}},
		{"number key", briefDoc, "key = {\n\tranks = {\n\t\t1 = {\n\t\t\tmen_at_arms = x # the men at arms\n\t\t}\n\t}\n}\n",
			"ranks/<number>/men_at_arms", func(field *Field) bool { return field.Blurb.Value == "the men at arms" }},
		{"header type with a required term", briefDoc, "# ### Brief: can_only_start_via_script ( optional bool )\ncan_only_start_via_script = yes\n",
			"can_only_start_via_script", func(field *Field) bool {
				return field.Value.Value.Type == ValueTypeBool && field.Required.Value == "optional"
			}},
		{"type term with underscores", readmeDoc, "#\ttraded_quantity = fixed_point # how many units are moved\n",
			"traded_quantity", func(field *Field) bool { return field.Value.Value.Type == ValueTypeNumber }},
		{"placeholder opening a bullet", readmeDoc, "# - allow: <trigger> can the building be built (root = location)\n",
			"allow", func(field *Field) bool { return field.Shape.Value == game.ShapeTriggers }},
		{"shape opening the text above", briefDoc, "key = {\n\t# Effect is executed when the owner dies\n\t# root - active story cycle\n\ton_owner_death = {}\n}\n",
			"on_owner_death", func(field *Field) bool { return field.Shape.Value == game.ShapeEffects }},
		{"nested at its path", briefDoc, "key = {\n\tranks = {\n\t\tliege_modifier = { } # what the liege gets\n\t}\n}\n",
			"ranks/liege_modifier", func(field *Field) bool { return field.Blurb.Value == "what the liege gets" }},
		{"header heads a nested statement", briefDoc, "key = {\n\toption = {\n\t\t# ### Brief: weight ( int )\n\t\tweight = 1\n\t}\n}\n",
			"option/weight", func(field *Field) bool { return field.Value.Value.Type == ValueTypeNumber }},
	}

	for _, each := range cases {
		reader, ok := docs.parseDoc(each.text, each.rel, game.Database{})
		if !ok {
			t.Errorf("%s: not read as a doc", each.name)

			continue
		}

		if field := findField(reader.root, each.key); field == nil || !each.check(field) {
			t.Errorf("%s: %+v", each.name, field)
		}
	}
}

// findField returns the field written at a key path below the given field,
// its keys joined by slashes, or nil.
func findField(field *Field, path string) *Field {
	for key := range strings.SplitSeq(path, "/") {
		field = field.findChild(key)
	}

	return field
}

// TestParseDocDatabase checks a doc file's blurb, and that a data file named
// like a doc is not read as one.
func TestParseDocDatabase(t *testing.T) {
	docs := &Schema{}
	text := "# Reference\nA building of a state.\n\nexample = {\n\tgroup = x\n}\n"
	reader, ok := docs.parseDoc(text, "x/x.md", game.Database{})
	if !ok || strings.Join(reader.blurb, " ") != "A building of a state." {
		t.Fatalf("database blurb: %v", reader)
	}

	if _, ok := docs.parseDoc("a = 1\nb = 2\nc = { d = 3 }\n", "x/00_example_templates.txt", game.Database{}); ok {
		t.Error("a data file named like a doc is read as one")
	}
}

// TestBuildFixtureLearns checks the events database and the docs that Build
// learns from the fixture install.
func TestBuildFixtureLearns(t *testing.T) {
	built, _ := buildFixture(t)
	if built.Events != "events" {
		t.Errorf("events database: %q", built.Events)
	}

	if traits := built.Databases["traits"]; traits == nil || len(traits.Documented) == 0 {
		t.Error("traits: no documented keys")
	}
}

// TestWalkEventsFixture checks the walk over the fixture's events: a
// definition, an event reference, a link, a saved scope and a keyword reached
// through the number form.
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

	if shape := resolveShapeClaim(Vanilla{Shapes: map[game.Shape]int{game.ShapeValue: 2, game.ShapeTriggers: 1}}); shape != game.ShapeValue {
		t.Errorf("a block of value keys and triggers: %q", shape)
	}

	if shape := resolveShapeClaim(Vanilla{Shapes: map[game.Shape]int{game.ShapeFields: 2, game.ShapeTriggers: 1}}); shape != game.ShapeUnknown {
		t.Errorf("a block of fields and triggers: %q", shape)
	}
}

// TestResolveMacroReplacesParameters checks that a call to a macro expands
// with its parameters replaced.
func TestResolveMacroReplacesParameters(t *testing.T) {
	macros := &Schema{DataTypes: map[string]DataType{globalType: {Functions: map[string]DataFunction{
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

// TestBuildInstalls builds the schema of each real install that
// PMT_TEST_INSTALL_<GAME> names, and prints what each source gave. It asserts
// that the build succeeds and the dumps declare commands.
func TestBuildInstalls(t *testing.T) {
	for _, gameID := range []string{"ck3", "vic3", "eu5"} {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}

			row := game.LookupGame(gameID)
			paths := listInstall(t, row, install)
			start := time.Now()
			built, err := Build(row, install, paths, parseFile)
			if err != nil {
				t.Fatal(err)
			}

			elapsed := time.Since(start)
			commands := len(built.Databases[game.DumpEffects].Keys) + len(built.Databases[game.DumpTriggers].Keys) +
				len(built.Databases[game.DumpLinks].Keys)
			if commands == 0 {
				t.Fatal("no commands")
			}

			sources, facts := map[game.Source]int{}, map[string]int{}
			built.walkFields(func(field *Field, _ bool) {
				sources[field.Source]++

				for name, claim := range map[string]game.Source{
					"blurb": field.Blurb.Source, "value": field.Value.Source, "default": field.Default.Source,
					"required": field.Required.Source,
				} {
					if claim == game.SourceDocs {
						facts[name]++
					}
				}
			})

			t.Logf("%d files in %s: %d databases, %d commands, %d scripted definitions",
				len(paths), elapsed.Round(time.Millisecond), len(built.Databases), commands,
				len(built.ScriptedCopies))
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
