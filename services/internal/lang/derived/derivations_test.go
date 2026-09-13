// derivations_test.go holds the declared scopes against what the wiring does.
//
// A declaration nothing checks is a comment with a struct around it. These make
// Derivations binding: it must cover every field of Derived, and a projection
// declared to reach the session must actually arrive there.

package derived

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func derivationByName(t *testing.T) map[string]Derivation {
	t.Helper()
	out := map[string]Derivation{}
	for _, d := range Derivations {
		if _, dup := out[d.Name]; dup {
			t.Errorf("%s is declared twice", d.Name)
		}
		out[d.Name] = d
	}
	return out
}

// Every projection must be declared, and nothing may be declared that is not a
// projection. Adding a field to Derived without a scope is exactly how the
// current situation arose.
func TestDerivationsCoverDerived(t *testing.T) {
	declared := derivationByName(t)
	rt := reflect.TypeFor[Derived]()
	for field := range rt.Fields() {
		name := field.Name
		if _, ok := declared[name]; !ok {
			t.Errorf("Derived.%s has no row in Derivations: say how far it travels", name)
		}
	}
	fields := map[string]bool{}
	for field := range rt.Fields() {
		fields[field.Name] = true
	}
	for _, d := range Derivations {
		if !fields[d.Name] {
			t.Errorf("Derivations names %s, which is not a field of Derived", d.Name)
		}
	}
	for _, d := range Derivations {
		switch d.Scope {
		case ScopeInstall, ScopeInstallMod, ScopeModLocal:
		default:
			t.Errorf("%s has scope %q, which is not one of the three", d.Name, d.Scope)
		}
	}
}

// The declaration must match the wiring: a projection said to reach the session
// has to arrive in Harvest, and one said not to must not. This is what turns
// II.0's "the mod index drops seven" from an observation into a decision.
func TestDerivationScopesMatchTheModPath(t *testing.T) {
	root := writeFixture(t, coverageFixture())
	harvest, err := BuildIndex(context.Background(), "ck3",
		[]ModInput{{Origin: "mod", Root: root, Order: 0}},
		&VanillaCache{GameID: "ck3", Schema: ck3Schema()})
	if err != nil {
		t.Fatal(err)
	}
	// What the mod path actually hands the session.
	exported := map[string]bool{}
	for _, f := range populated(harvest) {
		exported[f] = true
	}
	// The fixture must reach every projection, or "not exported" is ambiguous
	// between a scope decision and a fixture that never produced one.
	_, derived, _, err := collectExtracts(context.Background(), "ck3",
		gatherFixtureFiles(t, root), true, nil, ck3Schema(), nil)
	if err != nil {
		t.Fatal(err)
	}
	produced := map[string]bool{}
	for _, f := range populated(derived) {
		produced[f] = true
	}

	for _, d := range Derivations {
		if !produced[d.Name] && d.Scope != ScopeInstall {
			t.Errorf("%s is declared %s but the fixture never produced it, so this "+
				"test cannot tell a scope decision from a fixture gap", d.Name, d.Scope)
			continue
		}
		switch d.Scope {
		case ScopeInstallMod:
			if !exported[d.Name] {
				t.Errorf("%s is declared %s but the mod index does not carry it",
					d.Name, d.Scope)
			}
		case ScopeModLocal, ScopeInstall:
			if exported[d.Name] {
				t.Errorf("%s is declared %s but the mod index does carry it — "+
					"the declaration is out of date", d.Name, d.Scope)
			}
		}
	}
}

func gatherFixtureFiles(t *testing.T, root string) []fileRef {
	t.Helper()
	inv := gather([]string{filepath.Clean(root)})
	return append(append([]fileRef{}, inv.script...), inv.gui...)
}

// A standing report, not a failure. Five of these scopes have no recorded
// reason, which means nobody can tell whether they were chosen or inherited —
// and the mod-local four are the ones where that matters, because work is being
// done and thrown away.
func TestDerivationReasonCoverage(t *testing.T) {
	var bare []string
	for _, d := range Derivations {
		if strings.TrimSpace(d.Reason) == "" {
			bare = append(bare, d.Name+" ("+string(d.Scope)+")")
		}
	}
	t.Logf("%d of %d scope decisions record why; %d do not: %v",
		len(Derivations)-len(bare), len(Derivations), len(bare), bare)
}

// The coverage golden pins WHICH derived projections each wiring path produces.
//
// Plan item II.0. There is no single declaration of what PMT derives: three code
// paths hand-wire three different subsets, so a mod file gets a strictly poorer
// model than a vanilla file and nothing says so. This records the difference as
// data, and is the regression gate for the rest of the derivation track — after
// II.1-II.5 the coverage must end equal or better, never worse.
//
// It is a SHAPE claim, not a corpus count: which projections come out populated,
// against a fixture that exercises each one. A shape survives a game patch; a
// count does not. See GAME-SYNTAX and the II.5 amendment in the plan.

var updateGolden = flag.Bool("update-coverage", false,
	"rewrite testdata/derivation-coverage.txt from this run")

// populated lists the exported fields of v that carry anything, so the golden
// says what a path produced rather than what its struct could hold.
func populated(v any) []string {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	var out []string
	rt := rv.Type()
	for i := range rv.NumField() {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		if !rv.Field(i).IsZero() {
			out = append(out, f.Name)
		}
	}
	sort.Strings(out)
	return out
}

// coverageFixture is one small corpus built to CLEAR the floors, not merely to
// mention each shape: minFieldHits is 2, so a field needs two resolving values;
// minBindCites is 3 with bindFloor 0.5, so a prefix needs three citations its
// folder explains. A fixture that stops below a floor produces a golden that
// cannot detect a regression in whatever it failed to fire — the first draft of
// this one left FieldValueKinds and PrefixKinds empty for exactly that reason.
func coverageFixture() map[string]string {
	return map[string]string{
		"common/cultures/00_cultures.txt": "english = { color = { 1 1 1 } }\n" +
			"french = { color = { 2 2 2 } }\n" +
			"german = { color = { 3 3 3 } }\n",
		"common/religion/religions/00_test.txt": `test_religion = {
	faiths = {
		faith_one = { icon = one color = { 1 2 3 } }
		faith_two = { icon = two color = { 4 5 6 } }
		faith_three = { icon = three color = { 7 8 9 } }
	}
}
`,
		"common/traits/00_traits.txt": "brave = { category = personality }\n" +
			"craven = { category = personality }\n",
		"common/game_rules/00_rules.txt": `test_rule = {
	categories = { a }
	default = setting_low
	setting_low = { }
	setting_high = { }
}
`,
		"common/scripted_effects/00_effects.txt": "give_gold_effect = { add_gold = 10 }\n",
		"events/test_events.txt": `namespace = tst
tst.1 = {
	type = character_event
	title = tst.1.t
	desc = tst.1.d
	immediate = {
		trigger_event = tst.2
		add_trait = brave
		give_gold_effect = yes
		save_scope_as = target
		set_culture = culture:english
		has_faith = faith:faith_one
	}
}
tst.2 = {
	type = character_event
	title = tst.2.t
	immediate = {
		add_trait = craven
		set_culture = culture:french
		has_faith = faith:faith_two
	}
}
tst.3 = {
	type = character_event
	title = tst.3.t
	immediate = {
		set_culture = culture:german
		has_faith = faith:faith_three
	}
}
`,
		// Built from the code point rather than typed: a literal BOM in Go
		// source is a byte-order mark the compiler rejects.
		"localization/english/test_l_english.yml": string(rune(0xFEFF)) + "l_english:\n" +
			" tst.1.t:0 \"One\"\n tst.1.d:0 \"Desc\"\n tst.2.t:0 \"Two\"\n" +
			" brave:0 \"Brave\"\n craven:0 \"Craven\"\n",
		"descriptor.mod": "name=\"cov\"\n",
	}
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDerivationCoverageGolden(t *testing.T) {
	root := writeFixture(t, coverageFixture())
	schema := ck3Schema()

	// --- the mod-index path
	harvest, err := BuildIndex(context.Background(), "ck3",
		[]ModInput{{Origin: "mod", Root: root, Order: 0}},
		&VanillaCache{GameID: "ck3", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}

	// --- the per-file save path
	body := coverageFixture()["events/test_events.txt"]
	file := ExtractFile("ck3", filepath.Join(root, "events", "test_events.txt"),
		"events/test_events.txt", "mod", body, true)

	// --- the shared derivation stage both of the above run through, which is
	// where the projections are actually computed.
	inv := gather([]string{root})
	files := append(append([]fileRef{}, inv.script...), inv.gui...)
	_, derived, _, err := collectExtracts(
		context.Background(), "ck3", files, true, nil, schema, nil)
	if err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	b.WriteString("# Which projections each wiring path produces (plan II.0).\n")
	b.WriteString("# Regenerate: go test ./internal/lang/derived/ -run Coverage -update-coverage\n")
	for _, sec := range []struct {
		name string
		v    any
	}{
		{"derived (collectExtracts, shared by every path)", derived},
		{"mod index (BuildIndex -> Harvest)", harvest},
		{"per-file save (ExtractFile -> FileExtract)", file},
	} {
		b.WriteString("\n[" + sec.name + "]\n")
		for _, f := range populated(sec.v) {
			b.WriteString("  " + f + "\n")
		}
	}

	// The opening defect, stated rather than left to be diffed by eye: what the
	// shared stage worked out and the mod path then threw away. II.3's Scope
	// field is what turns this from an accident into a decision, so this list
	// shrinking is the visible sign that the track is working.
	kept := map[string]bool{}
	for _, f := range populated(harvest) {
		kept[f] = true
	}
	b.WriteString("\n[derived, then dropped by the mod index]\n")
	for _, f := range populated(derived) {
		if !kept[f] {
			b.WriteString("  " + f + "\n")
		}
	}
	got := b.String()

	path := filepath.Join("testdata", "derivation-coverage.txt")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden rewritten")
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v\nrun with -update-coverage to create it", err)
	}
	if got != string(want) {
		t.Errorf("derivation coverage changed.\n--- want\n%s\n--- got\n%s", want, got)
	}
}
