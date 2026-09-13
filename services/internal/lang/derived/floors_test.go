// floors_test.go holds Floors against the constants it indexes.
//
// An index that can fall out of date with the thing it indexes is the defect it
// was meant to fix, one level up. These two checks are what make floors.go worth
// having: a floor cannot be added without a row, and a row cannot drift from its
// constant.

package derived

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// floorDecl matches a package-level numeric constant that looks like a
// threshold: min*, max*, or a name ending Floor / Coverage / Share / Saturated.
var floorDecl = regexp.MustCompile(
	`(?m)^\s*(?:const\s+)?((?:[Mm]in|[Mm]ax)[A-Z][A-Za-z]*|[a-zA-Z][A-Za-z]*(?:Floor|Coverage|Share|Saturated))\s*=\s*([0-9.]+)\s*$`)

var floorDirs = map[string]string{
	"":     ".",
	"loc.": filepath.Join("..", "loc"),
}

// declaredFloors scans the covered packages' source for threshold constants.
// Reading the source rather than a hand-kept list is the point: a floor added in
// a new file is caught without anyone remembering this test exists.
func declaredFloors(t *testing.T) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for prefix, dir := range floorDirs {
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, filepath.Clean(n)))
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range floorDecl.FindAllStringSubmatch(string(raw), -1) {
				v, err := strconv.ParseFloat(m[2], 64)
				if err != nil {
					continue
				}
				out[prefix+m[1]] = v
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("found only %d threshold constants; the scan is broken and this "+
			"test is no longer checking anything", len(out))
	}
	return out
}

// Every threshold in the package must appear in Floors, or the index is a
// partial list that reads like a complete one.
func TestFloorsIndexesEveryThreshold(t *testing.T) {
	declared := declaredFloors(t)
	indexed := map[string]bool{}
	for _, f := range Floors {
		indexed[f.Name] = true
	}
	for name := range declared {
		if !indexed[name] {
			t.Errorf("%s is a threshold with no row in Floors", name)
		}
	}
	for _, f := range Floors {
		if _, ok := declared[f.Name]; !ok {
			t.Errorf("Floors has a row for %s, which is not a threshold constant "+
				"in any package this index covers", f.Name)
		}
	}
	t.Logf("%d thresholds, %d indexed", len(declared), len(Floors))
}

// The table cannot drift from the constants, because every row holds the
// identifier rather than a copy of its number — change `minFieldHits` and the
// row changes with it. So there is nothing to check there, and a test named for
// checking it would assert nothing.
//
// What this checks instead is the SCANNER: that reading the source text finds
// the same numbers the compiler does. TestFloorsIndexesEveryThreshold's
// completeness claim rests entirely on that regex finding real declarations, and
// a regex that quietly stopped matching would turn it into a test that passes
// because it found nothing.
func TestFloorSourceScanAgreesWithTheCompiler(t *testing.T) {
	declared := declaredFloors(t)
	for _, f := range Floors {
		want, ok := declared[f.Name]
		if !ok {
			continue // reported by TestFloorsIndexesEveryThreshold
		}
		if f.Value != want {
			t.Errorf("scan read %s as %v; the compiler says %v", f.Name, want, f.Value)
		}
	}
	// A spot check with a known answer, so the scan cannot pass by matching
	// nothing at all.
	if got := declared["minFieldHits"]; got != minFieldHits {
		t.Errorf("scan read minFieldHits as %v, want %v", got, minFieldHits)
	}
}

func TestFloorsSayWhatTheyGuard(t *testing.T) {
	for _, f := range Floors {
		if strings.TrimSpace(f.Guards) == "" {
			t.Errorf("%s has no Guards text", f.Name)
		}
		if f.Unit == "" {
			t.Errorf("%s has no Unit", f.Name)
		}
	}
}

func TestFloorsEvidenceCoverage(t *testing.T) {
	var bare []string
	for _, f := range Floors {
		if strings.TrimSpace(f.Evidence) == "" {
			bare = append(bare, f.Name)
		}
	}
	t.Logf("%d of %d floors record the measurement that set them; %d are reasoned only: %v",
		len(Floors)-len(bare), len(Floors), len(bare), bare)
}

func TestFloorBoundaries(t *testing.T) {
	t.Run("minNestedHits", func(t *testing.T) {
		at := func(n int) []string {
			return deriveNestedMembers(map[string]map[string]int{
				"limit": {"has_trait": n},
			})["limit"]
		}
		if got := at(minNestedHits - 1); len(got) != 0 {
			t.Errorf("%d hits admitted a member: %v", minNestedHits-1, got)
		}
		if got := at(minNestedHits); len(got) != 1 {
			t.Errorf("%d hits rejected the member: %v", minNestedHits, got)
		}
	})

	t.Run("maxNestedMembers", func(t *testing.T) {
		counts := map[string]int{}
		for i := range maxNestedMembers + 10 {
			counts["m"+strconv.Itoa(i)] = minNestedHits + i
		}
		got := deriveNestedMembers(map[string]map[string]int{"limit": counts})["limit"]
		if len(got) != maxNestedMembers {
			t.Errorf("%d members, want the cap of %d", len(got), maxNestedMembers)
		}
	})

	t.Run("minAmbiguousParents", func(t *testing.T) {
		one := deriveAmbiguousFields(map[string][]string{"a": {"name"}})
		if one["name"] {
			t.Error("a field in one block is not ambiguous")
		}
		two := deriveAmbiguousFields(map[string][]string{"a": {"name"}, "b": {"name"}})
		if !two["name"] {
			t.Errorf("a field in %d blocks must be ambiguous", minAmbiguousParents)
		}
	})

	t.Run("loc.MinKeyAffixKeys", func(t *testing.T) {
		at := func(n int) []loc.KeyAffix {
			keys := map[string]bool{}
			for i := range n {
				stem := "TRIGGER_" + strconv.Itoa(i)
				keys[stem], keys["NOT_"+stem] = true, true
			}
			return loc.DeriveKeyAffixes(keys)
		}
		if got := at(loc.MinKeyAffixKeys - 1); len(got) != 0 {
			t.Errorf("%d keys made a convention: %v", loc.MinKeyAffixKeys-1, got)
		}
		if got := at(loc.MinKeyAffixKeys); len(got) == 0 {
			t.Errorf("%d keys made none", loc.MinKeyAffixKeys)
		}
	})

	t.Run("maxDefDocLines", func(t *testing.T) {
		var b strings.Builder
		for i := range maxDefDocLines + 3 {
			fmt.Fprintf(&b, "# line %d\n", i)
		}
		b.WriteString("brave = {}\n")
		src := b.String()
		got := leadingComment(src, jomini.NewLineIndex(src), maxDefDocLines+3)
		if n := strings.Count(got, "line "); n != maxDefDocLines {
			t.Errorf("kept %d comment lines, want the cap of %d: %q",
				n, maxDefDocLines, got)
		}
	})
}

func TestFloorBoundariesLoc(t *testing.T) {
	// affixFixture: n definitions of one kind, each with a PRE_<id> loc key.
	affixFixture := func(n int) (*defIndex, map[string]bool) {
		var defs []Def
		locKeys := map[string]bool{}
		for i := range n {
			id := "ach_" + strconv.Itoa(i)
			defs = append(defs, Def{Kind: "achievements", Key: id})
			locKeys["ACHIEVEMENT_"+id] = true
		}
		return newDefIndex(defs), locKeys
	}

	t.Run("loc.MinAffixDefs", func(t *testing.T) {
		idx, keys := affixFixture(loc.MinAffixDefs - 1)
		if got := deriveLocAffixes(idx, keys); len(got["achievements"]) != 0 {
			t.Errorf("%d definitions made a convention: %v", loc.MinAffixDefs-1, got)
		}
		idx, keys = affixFixture(loc.MinAffixDefs)
		if got := deriveLocAffixes(idx, keys); len(got["achievements"]) == 0 {
			t.Errorf("%d definitions made none", loc.MinAffixDefs)
		}
	})

	t.Run("loc.MinAffixCoverage", func(t *testing.T) {
		// Enough definitions to clear loc.MinAffixDefs, but the shape covers too
		// few of the kind: 8 of 200 is 4%, under the floor.
		idx, keys := affixFixture(loc.MinAffixDefs)
		var defs []Def
		for i := range 200 {
			defs = append(defs, Def{Kind: "achievements", Key: "ach_" + strconv.Itoa(i)})
		}
		_ = idx
		if got := deriveLocAffixes(newDefIndex(defs), keys); len(got["achievements"]) != 0 {
			t.Errorf("a shape covering %d%% of the kind became a convention: %v",
				loc.MinAffixDefs*100/200, got)
		}
	})

	locFields := func(hits, total int) map[string]bool {
		locKeys, vals := map[string]bool{}, map[string]bool{}
		for i := range total {
			v := "val_" + strconv.Itoa(i)
			vals[v] = true
			if i < hits {
				locKeys[v] = true
			}
		}
		return deriveLocFields(map[string]map[string]bool{"desc_field": vals},
			locKeys, map[string]bool{}, &declared.Schema{})
	}

	t.Run("minLocFieldHits", func(t *testing.T) {
		if got := locFields(minLocFieldHits-1, minLocFieldHits-1); got["desc_field"] {
			t.Errorf("%d resolving values typed the field", minLocFieldHits-1)
		}
		if got := locFields(minLocFieldHits, minLocFieldHits); !got["desc_field"] {
			t.Errorf("%d resolving values did not", minLocFieldHits)
		}
	})

	t.Run("locFieldCoverage", func(t *testing.T) {
		// 10 of 20 is 50%, under the floor; 16 of 20 is 80%, exactly on it.
		if got := locFields(10, 20); got["desc_field"] {
			t.Error("50% coverage typed the field")
		}
		if got := locFields(locFieldCoverage*20/100, 20); !got["desc_field"] {
			t.Errorf("%d%% coverage did not", locFieldCoverage)
		}
	})
}

func TestFloorBoundariesMore(t *testing.T) {
	t.Run("fireKeyCoverage", func(t *testing.T) {
		// named is how many RHS values were names at all; hits how many resolved
		// to an event. Below the share the key is not a fire key.
		at := func(hits, named int) map[string]string {
			return resolveFireKinds(map[string]*fireCount{
				"trigger_event": {kinds: map[string]int{"event": hits}, named: named},
			})
		}
		lo := minFireNames * (fireKeyCoverage - 20) / 100
		if got := at(lo, minFireNames); got["trigger_event"] != "" {
			t.Errorf("%d of %d resolving elected a fire key", lo, minFireNames)
		}
		if got := at(minFireNames, minFireNames); got["trigger_event"] != "event" {
			t.Errorf("unanimous over %d names did not: %v", minFireNames, got)
		}
	})

	t.Run("minSnippetCoverage", func(t *testing.T) {
		// of definitions of the kind, how many carry the member.
		at := func(hits, of int) []SnippetMember {
			counts := map[string]int{"member": hits}
			defs := make([]Def, 0, of)
			for i := range of {
				defs = append(defs, Def{Kind: "decisions", Key: "d" + strconv.Itoa(i)})
			}
			return deriveSnippets(map[string]map[string]int{"decisions": counts},
				nil, newDefIndex(defs))["decisions"]
		}
		of := minSnippetDefs * 2
		lo := of * (minSnippetCoverage - 10) / 100
		if got := at(lo, of); len(got) != 0 {
			t.Errorf("a member on %d of %d definitions entered the snippet: %v", lo, of, got)
		}
		if got := at(of, of); len(got) == 0 {
			t.Errorf("a member on every one of %d definitions did not", of)
		}
	})

	t.Run("loc.MaxAffixesPerKind", func(t *testing.T) {
		// One shape per prefix, all well-evidenced: the cap is what bounds them.
		var defs []Def
		locKeys := map[string]bool{}
		for i := range loc.MinAffixDefs * 2 {
			id := "obj_" + strconv.Itoa(i)
			defs = append(defs, Def{Kind: "things", Key: id})
			for p := range loc.MaxAffixesPerKind + 6 {
				locKeys["P"+strconv.Itoa(p)+"_"+id] = true
			}
		}
		got := deriveLocAffixes(newDefIndex(defs), locKeys)["things"]
		if len(got) > loc.MaxAffixesPerKind {
			t.Errorf("%d conventions for one kind, cap is %d", len(got), loc.MaxAffixesPerKind)
		}
	})
}
