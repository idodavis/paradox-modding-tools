package derived

import (
	"slices"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
)

// TestKeyClass is the one classifier and the one table: a row per class, with
// what the fold writes for it and whether unknown-key may name it.
func TestKeyClass(t *testing.T) {
	s := &Schema{
		TriggerSet: map[string]bool{"is_ruler": true},
		MacroDefs:  map[string]string{"my_helper": "scripted_effects"},
	}
	for _, tt := range []struct {
		key      string
		quoted   bool
		rows     bool
		class    Class
		seg      string
		nameable bool
	}{
		{"immediate", false, false, ClassPlain, "immediate", true},
		{"", false, false, ClassBare, segBare, false},
		{"100", false, false, ClassNumber, segNumber, false},
		{"1066.1.1", false, false, ClassDate, segDate, false},
		{"culture:english", false, false, ClassCite, segCite, false},
		{"scope:actor", false, false, ClassCite, segCite, false},
		{"$PARAM$", false, false, ClassVar, segVar, false},
		{"@value", false, false, ClassVar, segVar, false},
		{"immediate", true, false, ClassVar, segVar, false},
		// Control flow is never a row, whatever the position holds.
		{"limit", false, true, ClassStop, "limit", false},
		{"is_ruler", false, false, ClassDeclared, "is_ruler", false},
		{"my_helper", false, false, ClassMacroCall, "my_helper", false},
		// A definition key at a database position is a row; the same name under a body is the effect CK3 also named a game concept.
		{"e_hre", false, true, ClassDefinition, segKind, false},
		{"add_character_flag", false, false, ClassPlain, "add_character_flag", true},
	} {
		c, _ := s.keyClass("ck3", tt.key, tt.quoted, tt.rows)
		if c != tt.class || c.Nameable() != tt.nameable {
			t.Errorf("keyClass(%q, quoted=%v, rows=%v) = %v/%v, want %v/%v",
				tt.key, tt.quoted, tt.rows, c, c.Nameable(), tt.class, tt.nameable)
		}
		if got := s.foldSeg("ck3", tt.key, tt.quoted, tt.rows); got != tt.seg {
			t.Errorf("foldSeg(%q) = %q, want %q", tt.key, got, tt.seg)
		}
	}
	// A name that is not a lower-case token names something unharvested; verbatim it is the cell explosion the census found -- but as a KEY it is the macro argument's own cell.
	if got := s.foldSeg("ck3", "ROLE", false, false); got != segKind {
		t.Errorf("foldSeg(ROLE) = %q, want %q", got, segKind)
	}
	if got := s.foldKey("ck3", "ROLE", false, false); got != "ROLE" {
		t.Errorf("foldKey(ROLE) = %q, want the name", got)
	}
	if got := s.foldKey("ck3", "e_hre", false, false); got != "e_hre" {
		t.Errorf("foldKey off a database = %q, want the name", got)
	}
}

// TestUnifyKey is §9's unification: a block key is one structure everywhere, and a class-folded position one structure under the key above it.
func TestUnifyKey(t *testing.T) {
	for _, tt := range []struct{ path, want string }{
		{"", "event"},
		{"immediate", "immediate"},
		{"immediate.random_list", "random_list"},
		{"immediate.random_list." + segNumber, "random_list." + segNumber},
		{"option.random_list." + segNumber, "random_list." + segNumber},
		{"trigger." + segCite, "trigger." + segCite},
	} {
		if got := unifyKey("event", tt.path); got != tt.want {
			t.Errorf("unifyKey(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
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

// TestBlockTypes is §9's four classes over the cells, and the two things a key unifies: its body-ness and its members.
func TestBlockTypes(t *testing.T) {
	s := &Schema{
		// save_temporary_scope_as is declared in both dumps, as CK3 declares it.
		TriggerSet: map[string]bool{"is_ruler": true, "save_temporary_scope_as": true},
		EffectSet:  map[string]bool{"add_gold": true, "save_temporary_scope_as": true},
		MacroDefs:  map[string]string{"my_scaling": "scripted_modifiers"},
	}
	cell := func(kind, path, key string, n int32, flags uint8) {
		c := s.at(kind, path, key)
		c.N, c.Flags = n, flags
	}
	// event: a definition body of fields, a trigger body under it, and a `limit` inside that body.
	cell("event", segTop, "", 9, CellBlock)
	cell("event", "", "desc", 9, 0)
	cell("event", "", "trigger", 4, CellBlock)
	cell("event", "trigger", "is_ruler", 4, 0)
	cell("event", "trigger", "limit", 2, CellBlock)
	cell("event", "trigger.limit", "is_ruler", 2, 0)
	// One both-declared member does not make a trigger body an effect body too; a block with nothing else keeps both classes.
	cell("event", "trigger", "save_temporary_scope_as", 1, 0)
	cell("decisions", "", "on_pick", 1, CellBlock)
	cell("decisions", "on_pick", "save_temporary_scope_as", 1, 0)
	// decisions writes `limit` with plain fields only; it is still a trigger body, because its key is one in event.
	cell("decisions", "", "limit", 1, CellBlock)
	cell("decisions", "limit", "count", 1, 0)
	// A database: the §3 join marked the cell, and every block key under it folded to <kind>.
	s.at("religions", "", "faiths").Def, s.at("religions", "", "faiths").Rows = "faith", true
	cell("religions", "", "faiths", 3, CellBlock)
	cell("religions", "faiths", segKind, 3, CellBlock)
	// A list: the block's own cell also carries a value type, because its bare members folded into it.
	cell("religions", "", "colors", 2, CellBlock|CellLiteral)
	cell("religions", "colors", "add", 1, 0)
	// A macro call names its class: a scripted_modifiers call makes a modifier body.
	cell("religions", "", "scaling", 1, CellBlock)
	cell("religions", "scaling", "my_scaling", 1, 0)
	// select_trigger is one structure at two kinds; show_if is written at one position only, allow_null at none.
	cell("cabinet_actions", "", "select_trigger", 1, CellBlock)
	cell("cabinet_actions", "select_trigger", "looking_for_a", 1, 0)
	cell("cabinet_actions", "select_trigger", "target_flag", 1, 0)
	cell("country_interactions", "", "select_trigger", 1, CellBlock)
	cell("country_interactions", "select_trigger", "show_if", 1, 0)

	s.learnBlocks("ck3", map[string]map[string]bool{"select_trigger": {"allow_null": true}}, nil, true)

	for _, tt := range []struct {
		kind, path  string
		class, body uint8
		database    string
		members     []string
	}{
		{kind: "event", path: "", class: BlockStructure, members: []string{"desc", "trigger"}},
		{kind: "event", path: "trigger", class: BlockBody, body: BodyTrigger, members: []string{"limit"}},
		{kind: "event", path: "trigger.limit", class: BlockBody, body: BodyTrigger},
		{kind: "decisions", path: "limit", class: BlockBody, body: BodyTrigger, members: []string{"count"}},
		{kind: "decisions", path: "on_pick", class: BlockBody, body: BodyTrigger | BodyEffect},
		{kind: "religions", path: "faiths", class: BlockDatabase, database: "faith"},
		{kind: "religions", path: "colors", class: BlockList},
		{kind: "religions", path: "scaling", class: BlockBody, body: BodyModifier},
		{kind: "cabinet_actions", path: "select_trigger", class: BlockStructure,
			members: []string{"allow_null", "looking_for_a", "show_if", "target_flag"}},
		{kind: "country_interactions", path: "select_trigger", class: BlockStructure,
			members: []string{"allow_null", "looking_for_a", "show_if", "target_flag"}},
	} {
		b := s.BlockAt(tt.kind, tt.path)
		if b == nil {
			t.Errorf("%s/%s: no block type", tt.kind, tt.path)
			continue
		}
		if b.Class != tt.class || b.Body != tt.body || b.Database != tt.database {
			t.Errorf("%s/%s = class %d body %d db %q, want %d %d %q",
				tt.kind, tt.path, b.Class, b.Body, b.Database, tt.class, tt.body, tt.database)
		}
		if got := s.BlockMembers(tt.kind, tt.path); tt.members != nil && !slices.Equal(got, tt.members) {
			t.Errorf("%s/%s members = %v, want %v", tt.kind, tt.path, got, tt.members)
		}
	}
}

// TestKnown is unknown-key's whole vocabulary: a corpus key, a declared name and a documented field are all known; a name written nowhere is not.
func TestKnown(t *testing.T) {
	s := &Schema{}
	s.at("event", "", "desc").add([]string{LabelLiteral}, 1)
	sc := &declared.Schema{Effects: map[string]declared.EngineToken{"add_gold": {}}}
	s.learnBlocks("ck3", map[string]map[string]bool{"event": {"trigger": true}}, sc, true)
	for _, key := range []string{"desc", "add_gold", "trigger"} {
		if !s.Known(key) {
			t.Errorf("Known(%q) = false, want true", key)
		}
	}
	if s.Known("nothing_writes_this") {
		t.Error("Known(nothing_writes_this) = true, want false")
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
