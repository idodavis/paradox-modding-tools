package graph

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// The ids of the fixture mods.
const (
	modA = "mod-a"
	modB = "mod-b"
	modC = "mod-c"
)

// The ids of the fixture mods' nodes and the delays written beside their firings.
const (
	eventD1     = "events:d.1"
	eventD2     = "events:d.2"
	eventD3     = "events:d.3"
	eventD4     = "events:d.4"
	eventD5     = "events:d.5"
	eventB1     = "events:b.1"
	effectFire  = "scripted_effects:d_fire_effect"
	delayDays   = "3 days"
	delayMonths = "1–3 months"
)

// modAFiles are the files the first mod adds: events and on_actions that fire each other, with and without delays, an
// event that fires through a scripted effect, and a second definition of a vanilla on_action.
var modAFiles = map[string]string{
	filepath.Join("events", "d_events.txt"): `namespace = d

d.1 = {
	type = character_event
	left_portrait = { character = root animation = happy }
	right_portrait = scope:target
	immediate = {
		trigger_event = { id = d.2 days = 3 }
		trigger_event = b.1
		trigger_event = { id = d.4 months = { 1 3 } }
	}
}
d.2 = { type = character_event }
d.3 = { type = character_event }
d.4 = { type = character_event }
d.5 = {
	type = character_event
	immediate = { d_fire_effect = yes }
}
`,
	filepath.Join("common", "scripted_effects", "d_effects.txt"): `d_fire_effect = {
	trigger_event = { id = d.2 days = 5 }
}
`,
	filepath.Join("common", "on_action", "d_on_actions.txt"): `d_root = {
	on_actions = { d_mid }
}
d_mid = {
	events = { d.3 }
}
d_delayed = {
	delay = { days = 7 }
	events = { d.4 }
}
on_birth = {
	events = { d.2 }
}
`,
}

// modBFiles are the files the second mod adds: an event that fires one of the first mod's.
var modBFiles = map[string]string{
	filepath.Join("events", "b_events.txt"): `namespace = b

b.1 = {
	type = character_event
	immediate = { trigger_event = d.3 }
}
`,
}

// modCFiles are the files the third mod adds: a file that replaces a vanilla file and defines v.2 again, with an event
// that fires it.
var modCFiles = map[string]string{
	filepath.Join("events", "v_events.txt"): `namespace = v
namespace = vc

vc.1 = {
	type = character_event
	immediate = { trigger_event = v.2 }
}
v.2 = { type = character_event }
`,
}

// createApp makes the application the workspace service sends its events through, once for the whole test run.
var createApp = sync.OnceFunc(func() { application.New(application.Options{}) })

// resolveFixture returns the absolute path of a folder under internal/testdata.
func resolveFixture(t *testing.T, parts ...string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "internal", "testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// createMod copies the CK3 fixture mod into a temporary folder, writes the given files into it and returns its
// record.
func createMod(t *testing.T, id string, added map[string]string) app.Mod {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(resolveFixture(t, "mods", "ck3"))); err != nil {
		t.Fatal(err)
	}
	for path, text := range added {
		path = filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return app.Mod{ID: id, Dir: dir}
}

// openService opens a workspace on an install with the given mods and returns the graph service over it. The workspace
// closes when the test ends.
func openService(t *testing.T, gameID, installPath string, mods ...app.Mod) *Service {
	t.Helper()
	createApp()
	for _, variable := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(variable, t.TempDir())
	}
	config, err := app.OpenConfig()
	if err != nil {
		t.Fatal(err)
	}
	err = config.Update("", func(records *app.Config) error {
		records.Installs = []app.Install{{ID: "i1", Game: gameID, Path: installPath}}
		records.Workspaces = []app.Workspace{{ID: "w1", Name: "Test", Install: "i1", Mods: mods, Language: "english"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wsp := &workspace.Service{Config: config, Build: "test"}
	t.Cleanup(wsp.Close)
	wsp.Open("w1")
	if _, err := wsp.Session(); err != nil {
		t.Fatal(err)
	}
	return &Service{Workspace: wsp}
}

// openFixture opens the CK3 fixture install with the first two fixture mods.
func openFixture(t *testing.T) *Service {
	t.Helper()
	return openService(t, "ck3", resolveFixture(t, "ck3"), createMod(t, modA, modAFiles), createMod(t, modB, modBFiles))
}

// TestBuildGraphSplicesHiddenNodes checks that a graph goes forward and backward from its root, with signed steps and
// the delay of each firing, and that a node hidden by origin or by database becomes one edge that passes through it.
func TestBuildGraphSplicesHiddenNodes(t *testing.T) {
	gph := openFixture(t)
	filter := gph.Defaults()
	if !slices.Contains(filter.Databases, "events") || !slices.Contains(filter.Databases, "on_action") ||
		!slices.Equal(filter.Origins, []string{game.OriginVanilla, modA, modB}) || filter.Depth != 3 ||
		filter.Limit != 200 || !filter.Follow {
		t.Fatalf("defaults %+v", filter)
	}
	whole, err := gph.BuildGraph(eventD1, filter)
	if err != nil {
		t.Fatal(err)
	}
	wantEdges := []Edge{
		{From: eventD1, To: eventD2, Delay: delayDays},
		{From: eventD1, To: eventB1},
		{From: eventD1, To: eventD4, Delay: delayMonths},
		{From: eventB1, To: eventD3},
	}
	if whole.Truncated || len(whole.Nodes) != 5 || !hasEdges(whole.Edges, wantEdges) {
		t.Errorf("graph with every origin drawn: %d nodes, edges %+v", len(whole.Nodes), whole.Edges)
	}
	backward, err := gph.BuildGraph(eventD3, filter)
	if err != nil {
		t.Fatal(err)
	}
	if position := slices.IndexFunc(backward.Nodes, func(node Node) bool { return node.ID == eventD1 }); position < 0 ||
		backward.Nodes[position].Step != -2 {
		t.Errorf("graph from the fired event: %+v", backward.Nodes)
	}
	filter.Origins = []string{game.OriginVanilla, modA}
	short, err := gph.BuildGraph(eventD1, filter)
	if err != nil {
		t.Fatal(err)
	}
	wantEdges = []Edge{
		{From: eventD1, To: eventD2, Delay: delayDays},
		{From: eventD1, To: eventD3, Via: []string{"b.1"}},
		{From: eventD1, To: eventD4, Delay: delayMonths},
	}
	if len(short.Nodes) != 4 || !hasEdges(short.Edges, wantEdges) {
		t.Errorf("graph with the second mod hidden: %d nodes, edges %+v", len(short.Nodes), short.Edges)
	}
	filter = Filter{Databases: []string{"events"}, Origins: filter.Origins, Depth: 3, Limit: 200}
	chain, err := gph.BuildGraph("on_action:d_root", filter)
	if err != nil {
		t.Fatal(err)
	}
	wantEdges = []Edge{{From: "on_action:d_root", To: eventD3, Via: []string{"d_mid"}}}
	if len(chain.Nodes) != 2 || !hasEdges(chain.Edges, wantEdges) {
		t.Errorf("graph with the on_actions hidden: %d nodes, edges %+v", len(chain.Nodes), chain.Edges)
	}
	filter.Databases = []string{"on_action", "events"}
	delay, err := gph.BuildGraph("on_action:d_delayed", filter)
	if err != nil {
		t.Fatal(err)
	}
	if wantEdges = []Edge{{From: "on_action:d_delayed", To: eventD4, Delay: "7 days"}}; !hasEdges(delay.Edges, wantEdges) {
		t.Errorf("graph of the on_action with a delay block: %+v", delay.Edges)
	}
}

// TestBuildGraphDrawsEffects checks that a scripted effect between two nodes is a node if the filter draws effects, and
// is the label of the edge if it does not.
func TestBuildGraphDrawsEffects(t *testing.T) {
	gph := openFixture(t)
	filter := gph.Defaults()
	asNodes, err := gph.BuildGraph(eventD5, filter)
	if err != nil {
		t.Fatal(err)
	}
	wantEdges := []Edge{{From: eventD5, To: effectFire}, {From: effectFire, To: eventD2, Delay: "5 days"}}
	if len(asNodes.Nodes) != 3 || !hasEdges(asNodes.Edges, wantEdges) {
		t.Errorf("graph with the effects as nodes: %d nodes, edges %+v", len(asNodes.Nodes), asNodes.Edges)
	}
	filter.Databases = []string{"events"}
	asLabels, err := gph.BuildGraph(eventD5, filter)
	if err != nil {
		t.Fatal(err)
	}
	wantEdges = []Edge{{From: eventD5, To: eventD2, Delay: "5 days", Via: []string{"d_fire_effect"}}}
	if len(asLabels.Nodes) != 2 || !hasEdges(asLabels.Edges, wantEdges) {
		t.Errorf("graph with the effects left out: %d nodes, edges %+v", len(asLabels.Nodes), asLabels.Edges)
	}
}

// TestBuildGraphMergesDefinitions checks that a node whose database merges definitions fires what each fires: an
// on_action that a mod defines again fires the events of both.
func TestBuildGraphMergesDefinitions(t *testing.T) {
	gph := openFixture(t)
	graph, err := gph.BuildGraph("on_action:on_birth", gph.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	targets := lo.FilterMap(graph.Edges, func(edge Edge, _ int) (string, bool) {
		return edge.To, edge.From == "on_action:on_birth"
	})
	slices.Sort(targets)
	if want := []string{eventD2, "events:v.1", "events:v.2"}; !slices.Equal(targets, want) {
		t.Errorf("on_birth fires %v, want %v", targets, want)
	}
}

// TestBuildGraphSkipsReplacedFirer checks that an event only a replaced vanilla file defines is not drawn as a firer,
// and that an event the mod defines again carries the game's copy it hides, where a new one carries none.
func TestBuildGraphSkipsReplacedFirer(t *testing.T) {
	gph := openService(t, "ck3", resolveFixture(t, "ck3"), createMod(t, modC, modCFiles))
	graph := buildGraph(t, gph, "events:v.2", gph.Defaults())
	ids := lo.Map(graph.Nodes, func(node Node, _ int) string { return node.ID })
	if !slices.Contains(ids, "events:vc.1") || slices.Contains(ids, "events:v.1") {
		t.Errorf("the nodes around v.2: %v", ids)
	}
	replacing, err := gph.BuildDetail("events:v.2")
	if err != nil {
		t.Fatal(err)
	}
	added, err := gph.BuildDetail("events:vc.1")
	if err != nil {
		t.Fatal(err)
	}
	if replacing.Node.CopyOrigin != game.OriginVanilla || replacing.Copy == nil ||
		replacing.Copy.Place.Origin != game.OriginVanilla || added.Node.CopyOrigin != "" || added.Copy != nil {
		t.Errorf("v.2 hides %q (%v); vc.1 hides %q (%v)", replacing.Node.CopyOrigin, replacing.Copy != nil,
			added.Node.CopyOrigin, added.Copy != nil)
	}
}

// TestBuildDetailReadsDefinition checks the detail of an event: its text, what it fires and what fires it, its
// references, and that an id nothing defines is an error.
func TestBuildDetailReadsDefinition(t *testing.T) {
	gph := openFixture(t)
	detail, err := gph.BuildDetail(eventD1)
	if err != nil {
		t.Fatal(err)
	}
	wantFires := []Edge{
		{From: eventD1, To: eventD2, Delay: delayDays},
		{From: eventD1, To: eventB1},
		{From: eventD1, To: eventD4, Delay: delayMonths},
	}
	if detail.Node.ID != eventD1 || !hasEdges(detail.Node.Fires, wantFires) || len(detail.FiredBy) != 0 {
		t.Errorf("detail of d.1: %+v", detail)
	}
	if !slices.ContainsFunc(detail.References, func(reference Node) bool {
		return reference.Database == "events" && reference.Name == "b.1" && reference.Place.Origin == modB
	}) {
		t.Errorf("references of d.1: %+v", detail.References)
	}
	vanilla, err := gph.BuildDetail("events:v.1")
	if err != nil {
		t.Fatal(err)
	}
	if findDescription(vanilla) != "Desc" || vanilla.Node.Title != "Hello" || vanilla.Node.Line != 4 ||
		!slices.ContainsFunc(vanilla.FiredBy, func(edge Edge) bool { return edge.From == "on_action:on_birth" }) {
		t.Errorf("detail of v.1: text %q, title %q, line %d, fired by %+v",
			findDescription(vanilla), vanilla.Node.Title, vanilla.Node.Line, vanilla.FiredBy)
	}
	if _, err := gph.BuildDetail("events:nowhere.1"); err == nil {
		t.Error("the detail of a node nothing defines was built")
	}
}

// TestBuildGraphOnInstalls builds the graph of each real install that PMT_TEST_INSTALL_<GAME> names, with no mods and
// the default filter, from the on_actions that start a game and that fire each year. It fails if a node has no detail,
// if an event that picks one branch of a weighted list fires with no weighted step, and if CK3's birth chain is not
// read as its script writes it.
func TestBuildGraphOnInstalls(t *testing.T) {
	installs := []struct {
		game        string
		roots       []string // the on_actions drawn from
		weighted    string   // an event that fires in a weighted branch
		checksBirth bool     // whether the birth chain is checked
	}{
		{"ck3", []string{"on_game_start", "random_yearly_playable_pulse"}, "events:birth.0001", true},
		{"vic3", []string{"on_yearly_pulse_country"}, "events:ai_expedition_simulator.1", false},
		{"eu5", []string{"on_game_start", "yearly_country_pulse"}, "events:court_flavor_events.1", false},
	}
	for _, row := range installs {
		t.Run(row.game, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(row.game)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}
			gph := openService(t, row.game, install)
			filter := gph.Defaults()
			for _, root := range row.roots {
				matches := gph.FindRoots(root, 1)
				if len(matches) != 1 {
					t.Fatalf("no root %s", root)
				}
				started := time.Now()
				graph, err := gph.BuildGraph(matches[0].ID, filter)
				if err != nil {
					t.Fatal(err)
				}
				delays := lo.CountBy(graph.Edges, func(edge Edge) bool { return edge.Delay != "" })
				titles := lo.CountBy(graph.Nodes, func(node Node) bool { return node.Title != "" })
				t.Logf("%s: %d nodes, %d edges, %d with a delay, %d with a title, truncated %v, %s", root,
					len(graph.Nodes), len(graph.Edges), delays, titles, graph.Truncated, time.Since(started).Round(time.Millisecond))
				logDetails(t, gph, root, graph)
			}
			branches := findNode(buildGraph(t, gph, row.weighted, filter), row.weighted)
			if !slices.ContainsFunc(branches.Fires, func(edge Edge) bool {
				return slices.ContainsFunc(edge.Steps, func(step Step) bool { return step.Kind == kindWeight })
			}) {
				t.Errorf("%s fires nothing in a weighted branch: %+v", row.weighted, branches.Fires)
			}
			if row.checksBirth {
				checkBirth(t, gph, filter)
			}
		})
	}
}

// checkBirth checks CK3's birth chain: the blocks birth.0001's firings sit in, which events show a window and with
// what picture, the on_action the game calls from its code, and a message sent with its title.
func checkBirth(t *testing.T, gph *Service, filter Filter) {
	t.Helper()
	graph := buildGraph(t, gph, "events:birth.0001", filter)
	root := findNode(graph, "events:birth.0001")
	findFire := func(node Node, to string) Edge {
		edge, _ := lo.Find(node.Fires, func(edge Edge) bool { return edge.To == to })
		return edge
	}
	if steps := findFire(root, "events:birth.1001").Steps; !slices.ContainsFunc(steps, func(step Step) bool {
		return step.Kind == kindWeight && step.Weight == "95"
	}) {
		t.Errorf("the steps to birth.1001: %+v", steps)
	}
	steps := lo.Map(findFire(root, "events:birth.1010").Steps, func(step Step, _ int) string {
		return step.Key + " " + step.Kind
	})
	want := []string{"scope:child.dynasty scope", "if branch", "random_player list"}
	if !slices.Equal(steps[max(len(steps)-3, 0):], want) {
		t.Errorf("the steps to birth.1010: %v, want them to end in %v", steps, want)
	}
	window := findNode(graph, "events:birth.1001")
	if root.Display != displayHidden || window.Display != displayWindow ||
		!strings.HasSuffix(filepath.ToSlash(window.Background), "event_scenes/bedchamber.dds") {
		t.Errorf("birth.0001 shows as %q; birth.1001 as %q with %q", root.Display, window.Display, window.Background)
	}
	if actions := gph.FindRoots("on_birth_mother", 1); len(actions) != 1 || !actions[0].Hardcoded {
		t.Errorf("on_birth_mother: %+v", actions)
	}
	message := buildGraph(t, gph, "events:birth.1100", filter)
	sends := slices.ContainsFunc(message.Nodes, func(node Node) bool { return node.Display == displayMessage })
	titled := slices.ContainsFunc(findNode(message, "events:birth.1100").Fires, func(edge Edge) bool {
		return edge.Title != ""
	})
	if !sends || !titled {
		t.Errorf("birth.1100 sends no titled message: %+v", message.Nodes)
	}
}

// logDetails builds the detail of every node of a graph but its folds and logs what they hold, with each window that
// shows no background. It fails the test if a detail is an error.
func logDetails(t *testing.T, gph *Service, root string, graph Graph) {
	t.Helper()
	started := time.Now()
	texts, references, backgrounds := 0, 0, 0
	for _, node := range graph.Nodes {
		if node.Count > 0 {
			continue
		}
		detail, err := gph.BuildDetail(node.ID)
		if err != nil {
			t.Fatalf("detail of %s: %v", node.ID, err)
		}
		texts += lo.Ternary(findDescription(detail) != "", 1, 0)
		references += len(detail.References)
		backgrounds += lo.Ternary(detail.Node.Background != "", 1, 0)
		if node.Display == displayWindow && detail.Node.Background == "" {
			t.Logf("%s shows no background", node.ID)
		}
	}
	t.Logf("%s: details of %d nodes, %d with a text, %d references, %d with a background, %s", root, len(graph.Nodes),
		texts, references, backgrounds, time.Since(started).Round(time.Millisecond))
}

// buildGraph builds the graph from a root under a filter. It fails the test if the graph is an error.
func buildGraph(t *testing.T, gph *Service, root string, filter Filter) Graph {
	t.Helper()
	graph, err := gph.BuildGraph(root, filter)
	if err != nil {
		t.Fatal(err)
	}
	return graph
}

// findDescription returns the text of a detail's first description, or "" if it writes none.
func findDescription(detail Detail) string {
	section, _ := lo.Find(detail.Sections, func(section Section) bool { return section.Key == game.DescriptionKey })
	return lo.FirstOr(section.Texts, Version{}).Text
}

// findNode returns the node of a graph with the given id, or an empty node if the graph has none.
func findNode(graph Graph, id string) Node {
	node, _ := lo.Find(graph.Nodes, func(node Node) bool { return node.ID == id })
	return node
}

// hasEdges reports whether a list holds exactly the wanted edges, in any order.
func hasEdges(got, want []Edge) bool {
	keys := func(edges []Edge) []string {
		list := lo.Map(edges, func(edge Edge, _ int) string { return toKey(edge) })
		slices.Sort(list)
		return list
	}
	return slices.Equal(keys(got), keys(want))
}
