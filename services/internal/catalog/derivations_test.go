// derivations_test.go holds the declared scopes against what the wiring does.
//
// A declaration nothing checks is a comment with a struct around it. These make
// Derivations binding: it must cover every field of Derived, and a projection
// declared to reach the session must actually arrive there.

package catalog

import (
	"context"
	"path/filepath"
	"reflect"
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
	rt := reflect.TypeOf(Derived{})
	for i := range rt.NumField() {
		name := rt.Field(i).Name
		if _, ok := declared[name]; !ok {
			t.Errorf("Derived.%s has no row in Derivations: say how far it travels", name)
		}
	}
	fields := map[string]bool{}
	for i := range rt.NumField() {
		fields[rt.Field(i).Name] = true
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
