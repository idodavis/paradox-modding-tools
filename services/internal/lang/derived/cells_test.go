package derived

import (
	"slices"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
)

// TestFoldSeg is the path fold of the spec's §2: a segment is kept verbatim only
// when it is a lower-case identifier that names nothing.
func TestFoldSeg(t *testing.T) {
	defs := map[string]bool{"catholic": true, "e_hre": true}
	isDef := func(n string) bool { return defs[n] }
	for _, tt := range []struct{ raw, want string }{
		{"immediate", "immediate"},
		{"option", "option"},
		{"", segBare},
		{"100", segNumber},
		{"1066.1.1", segDate},
		{"culture:english", segCite},
		{"scope:actor", segCite},
		{"$PARAM$", segVar},
		{"@value", segVar},
		{"catholic", segKind},
		{"e_hre", segKind},
		// A name that is not a lower-case token names something unharvested; verbatim it is the cell explosion the census found.
		{"MyWindow", segKind},
	} {
		if got := foldSeg("ck3", tt.raw, false, isDef); got != tt.want {
			t.Errorf("foldSeg(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
	if got := foldSeg("ck3", "immediate", true, isDef); got != segVar {
		t.Errorf("a quoted segment is not an identifier: %q", got)
	}
	// The key half folds by the same rule, so a barony id and a GUI widget name are one cell, not one each.
}

// TestClassify is the value rule: a literal carries only Literal, a citation only its kind, the rest whatever the indexes hold.
func TestClassify(t *testing.T) {
	lk := &lookup{
		gameID:     "ck3",
		kindsByKey: map[string][]string{"brave": {"traits"}, "1234": {"provinces"}},
		locKeys:    map[string]bool{"brave": true, "trait_brave": true},
		prefix:     map[string]string{"culture": "cultures"},
	}
	for _, tt := range []struct {
		value           string
		quoted, dataKnd bool
		want            []string
	}{
		{value: "yes", want: []string{LabelStop}},
		{value: "10", want: []string{LabelLiteral}},
		{value: "1066.1.1", want: []string{LabelLiteral}},
		{value: "$AMOUNT$", want: []string{LabelLiteral}},
		{value: "culture:english", want: []string{LabelCite + "cultures"}},
		{value: "brave", want: []string{LabelDef + "traits", LabelLoc}},
		{value: "trait_brave", want: []string{LabelLoc}},
		{value: "nothing_holds_this", want: []string{LabelUnresolved}},
		{value: "trait_brave", quoted: true, want: []string{LabelLoc, LabelLiteral}},
		// A number is a quantity -- except in a data-tier file, where a row is named by one.
		{value: "1234", want: []string{LabelLiteral}},
		{value: "1234", dataKnd: true, want: []string{LabelDef + "provinces", LabelLiteral}},
	} {
		got := lk.classify(tt.value, tt.quoted, tt.dataKnd, nil)
		if !slices.Equal(got, tt.want) {
			t.Errorf("classify(%q quoted=%v data=%v) = %v, want %v",
				tt.value, tt.quoted, tt.dataKnd, got, tt.want)
		}
	}
}

// TestCellTypeFold folds occurrences into a cell: labels the union, kinds the intersection, a disagreeing corpus typing nothing.
func TestCellTypeFold(t *testing.T) {
	agree := &CellType{}
	agree.add([]string{LabelDef + "traits", LabelLoc}, 1)
	agree.add([]string{LabelDef + "traits"}, 1)
	agree.add([]string{LabelLiteral}, 3)
	agree.settle()
	if !slices.Equal(agree.Kinds, []string{"traits"}) {
		t.Errorf("kinds = %v, want [traits]", agree.Kinds)
	}
	if agree.N != 5 || !agree.Has(LabelLiteral) || agree.Has(LabelUnresolved) {
		t.Errorf("flags = %d n=%d", agree.Flags, agree.N)
	}
	mixed := &CellType{}
	mixed.add([]string{LabelDef + "traits"}, 1)
	mixed.add([]string{LabelDef + "cultures"}, 1)
	mixed.settle()
	if len(mixed.Kinds) != 0 {
		t.Errorf("a mixed cell types nothing, got %v", mixed.Kinds)
	}
	block := &CellType{}
	block.add([]string{LabelBlock}, 4)
	block.add([]string{LabelLiteral}, 1)
	block.settle()
	if !block.IsBlock() {
		t.Errorf("flags = %d, want the block bit", block.Flags)
	}
}

// TestJoinNested is the one join, by cell: block keys that hold names, one of them cited under a declared kind.
func TestJoinNested(t *testing.T) {
	s := &Schema{}
	// religion_types: `<religion> = { faiths = { catholic = {…} orthodox = {…} } }`
	s.at("religion_types", "", "faiths").add([]string{LabelBlock}, 2)
	s.at("religion_types", "", "faiths").Names = true
	// A block of bare members is not a database, however it is cited.
	s.at("religion_types", "", "colors").add([]string{LabelList}, 1)

	sc := &declared.Schema{
		Scopes: map[string]declared.ScopeType{"faith": {}},
		Links:  map[string]declared.ScopeLink{"faith": {Global: true, Data: true, Out: "faith"}},
	}
	cited := citedKinds([]typedCite{
		{prefix: "faith", id: "catholic"},
		// Only the declared prefix table counts: admitting a contextual link harvested 153,343 CK3 legend properties.
		{prefix: "legend_property", id: "orthodox"},
		{prefix: "flag", id: "catholic"},
	}, nil, sc)
	if cited["catholic"] != "faith" || cited["orthodox"] != "" {
		t.Fatalf("cited = %v", cited)
	}
	// The block keys as the corpus writes them, which is how the skeleton is keyed.
	for _, faith := range []string{"catholic", "orthodox"} {
		s.at("religion_types", "faiths", faith).add([]string{LabelBlock}, 1)
	}
	s.at("religion_types", "colors", "catholic").add([]string{LabelList}, 1)
	joinNested(s, cited, func(string) bool { return false })
	s.shapes()
	if got := s.TypeOf(Cell{"religion_types", "", "faiths"}).Def; got != "faith" {
		t.Errorf("faiths declares %q, want faith", got)
	}
	if got := s.TypeOf(Cell{"religion_types", "", "colors"}).Def; got != "" {
		t.Errorf("a list declares %q", got)
	}
	if len(s.Nested) != 1 || s.Nested[0].ChildKind != "faith" {
		t.Errorf("shapes = %+v", s.Nested)
	}
}

// TestMembersOrder is the count order completion reads, without the folded classes: those are positions, not names.
func TestMembersOrder(t *testing.T) {
	s := &Schema{}
	s.at("event", "", "option").add([]string{LabelBlock}, 3)
	s.at("event", "", "type").add([]string{LabelLiteral}, 9)
	s.at("event", "", "desc").add([]string{LabelLoc}, 5)
	s.at("event", "", segNumber).add([]string{LabelLiteral}, 99)
	if got := s.Members("event", ""); !slices.Equal(got, []string{"type", "desc", "option"}) {
		t.Errorf("members = %v", got)
	}
	if got := s.Members("event", "nothing"); got != nil {
		t.Errorf("unknown path = %v", got)
	}
}

// TestBindKinds is the one join against declared types: a scope binds to a folder by name, singular or plural.
func TestBindKinds(t *testing.T) {
	sc := &declared.Schema{Links: map[string]declared.ScopeLink{
		"culture":       {Global: true, Data: true, Out: "culture"},
		"building_type": {Global: true, Data: true, Out: "building_type"},
		"nowhere":       {Global: true, Data: true, Out: "nowhere"},
	}}
	keysByKind := map[string]map[string]bool{
		"cultures": {"english": true}, "buildings": {"castle": true},
	}
	prefixKind, kindScope := bindKinds(sc, keysByKind)
	if prefixKind["culture"] != "cultures" || prefixKind["building_type"] != "buildings" {
		t.Errorf("prefixKind = %v", prefixKind)
	}
	if _, bound := prefixKind["nowhere"]; bound {
		t.Errorf("a scope with no database must stay unbound: %v", prefixKind)
	}
	if kindScope["cultures"] != "culture" {
		t.Errorf("kindScope = %v", kindScope)
	}
}
