// schema_test.go covers the three script_docs syntaxes and, when a real game
// user-data folder is pointed at by PMT_TEST_DOCS_{CK3,EU5,VIC3}, asserts the
// counts a live install actually yields. The synthetic fixtures run everywhere;
// the install tests skip when the env var is unset.

package declared

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
)

func writeDump(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaClassicEffects(t *testing.T) {
	dir := t.TempDir()
	writeDump(t, dir, "effects.log", `Effect Documentation:

--------------------

add_gold - Adds gold to the scoped character
Supported Scopes: character
Supported Targets: dynasty

--------------------

add_title_law - add law to scoped title
add_title_law = princely_elective_succession_law
Supported Scopes: landed_title

--------------------
`)
	s := ReadSchema([]string{dir})
	if s == nil {
		t.Fatal("nil schema")
	}
	got, ok := s.Effects["add_gold"]
	if !ok {
		t.Fatalf("add_gold missing; have %v", s.Effects)
	}
	if len(got.In) != 1 || got.In[0] != "character" {
		t.Errorf("In = %v, want [character]", got.In)
	}
	if got.Target != "dynasty" {
		t.Errorf("Target = %q, want dynasty", got.Target)
	}
	if got.Doc != "Adds gold to the scoped character" {
		t.Errorf("Doc = %q", got.Doc)
	}
	// The usage line is prose here, not a "Usage:" label; it must not be lost.
	if law := s.Effects["add_title_law"]; law.In[0] != "landed_title" {
		t.Errorf("add_title_law In = %v", law.In)
	}
}

func TestSchemaMarkdownEffects(t *testing.T) {
	dir := t.TempDir()
	writeDump(t, dir, "effects.log", `# Effect Documentation
## activate_production_method
Activates the named production method
**Supported Scopes**: country, state

## add_banned_goods
Adds a total ban of a good to a country
**Supported Scopes**: country
**Supported Targets**: goods
`)
	s := ReadSchema([]string{dir})
	if s == nil {
		t.Fatal("nil schema")
	}
	got := s.Effects["activate_production_method"]
	if len(got.In) != 2 || got.In[0] != "country" || got.In[1] != "state" {
		t.Fatalf("In = %v, want [country state]", got.In)
	}
	if s.Effects["add_banned_goods"].Target != "goods" {
		t.Errorf("Target = %q, want goods", s.Effects["add_banned_goods"].Target)
	}
}

// Input/Output Scopes are the fields the previous parser dropped into prose.
func TestSchemaEventTargets(t *testing.T) {
	dir := t.TempDir()
	writeDump(t, dir, "event_targets.log", `Event Target Documentation:

--------------------

culture - Get the culture with the specified key
Requires Data: yes
Global Link: yes
Output Scopes: culture

--------------------

betrothed - Get the betrothed of the scoped character
Input Scopes: character
Output Scopes: character

--------------------
`)
	s := ReadSchema([]string{dir})
	if s == nil {
		t.Fatal("nil schema")
	}
	cul := s.Links["culture"]
	if !cul.Global || !cul.Data || cul.Out != "culture" {
		t.Fatalf("culture link = %+v", cul)
	}
	bet := s.Links["betrothed"]
	if len(bet.In) != 1 || bet.In[0] != "character" || bet.Out != "character" {
		t.Fatalf("betrothed link = %+v", bet)
	}
	if p := s.Prefixes(); p["culture"] != "culture" {
		t.Errorf("Prefixes = %v, want culture->culture", p)
	}
	// betrothed is not a global data link, so it is not a citable prefix.
	if _, ok := s.Prefixes()["betrothed"]; ok {
		t.Error("betrothed must not be a prefix")
	}
}

func TestSchemaMarkdownEventTargets(t *testing.T) {
	dir := t.TempDir()
	writeDump(t, dir, "event_targets.log", `# Event Target Documentation
### combat_width
Scope to combat width multiplier of scope province
Input Scopes: province
Output Scopes: value

### cu
Get the culture with the specified key
Requires Data: yes
Global Link: yes
Output Scopes: culture
`)
	s := ReadSchema([]string{dir})
	if s == nil {
		t.Fatal("nil schema")
	}
	if s.Links["combat_width"].Out != "value" {
		t.Errorf("combat_width = %+v", s.Links["combat_width"])
	}
	if s.Prefixes()["cu"] != "culture" {
		t.Errorf("Prefixes = %v", s.Prefixes())
	}
}

// event_scopes.log uses bare "name:" headers with no separator at all.
func TestSchemaScopeTypes(t *testing.T) {
	dir := t.TempDir()
	writeDump(t, dir, "event_scopes.log", `Scope Types:

none:
Evaluate Triggers: yes
Execute Effects: yes
Change Scopes: yes
Save Token: none
Stores Variables: no

character:
Evaluate Triggers: yes
Execute Effects: yes
Change Scopes: yes
Save Token: char
Stores Variables: yes
`)
	s := ReadSchema([]string{dir})
	if s == nil {
		t.Fatal("nil schema")
	}
	if len(s.Scopes) != 2 {
		t.Fatalf("scopes = %v", s.Scopes)
	}
	ch := s.Scopes["character"]
	if !ch.ExecuteEffects || !ch.StoresVariables || ch.SaveToken != "char" {
		t.Errorf("character = %+v", ch)
	}
	if s.Scopes["none"].StoresVariables {
		t.Error("none must not store variables")
	}
}

func TestSchemaOnActions(t *testing.T) {
	dir := t.TempDir()
	writeDump(t, dir, "on_actions.log", `On Action Documentation:

--------------------

stress_loss_confider:
From Code: No
Expected Scope: character

--------------------

on_tradition_removed:
From Code: Yes
Expected Scope: none

--------------------
`)
	s := ReadSchema([]string{dir})
	if s == nil {
		t.Fatal("nil schema")
	}
	if s.OnActions["stress_loss_confider"] != "character" {
		t.Errorf("OnActions = %v", s.OnActions)
	}
	if s.OnActions["on_tradition_removed"] != "none" {
		t.Errorf("OnActions = %v", s.OnActions)
	}
}

func TestSchemaModifiers(t *testing.T) {
	ck3 := t.TempDir()
	writeDump(t, ck3, "modifiers.log", `Printing Modifier Definitions:
Tag: dynasty_opinion
Use areas: character

Tag: monthly_income
Use areas: character
`)
	s := ReadSchema([]string{ck3})
	if s == nil || len(s.Modifiers) == 0 {
		t.Fatalf("ck3 modifiers = %+v", s)
	}

	vic3 := t.TempDir()
	writeDump(t, vic3, "modifiers.log", `--- Static modifier types ---
battle_casualties_mult:
  Mask: battle
  Name: aut!Casualties Taken
  Description: A bonus or penalty to Casualties
`)
	v := ReadSchema([]string{vic3})
	if v == nil {
		t.Fatal("nil vic3 schema")
	}
	m := v.Modifiers["battle_casualties_mult"]
	if m.Area != "battle" || m.Name != "Casualties Taken" {
		t.Errorf("modifier = %+v", m)
	}
}

func TestUsableIn(t *testing.T) {
	cases := []struct {
		in    []string
		scope string
		want  bool
	}{
		{nil, "country", true},
		{[]string{"none"}, "country", true},
		{[]string{"country"}, "country", true},
		{[]string{"country", "state"}, "state", true},
		{[]string{"country"}, "character", false},
		{[]string{"country"}, "", true},
	}
	for _, c := range cases {
		if got := UsableIn(c.in, c.scope); got != c.want {
			t.Errorf("UsableIn(%v, %q) = %v, want %v", c.in, c.scope, got, c.want)
		}
	}
}

// Prose containing a colon must not be mistaken for a label.
func TestSchemaProseWithColon(t *testing.T) {
	dir := t.TempDir()
	writeDump(t, dir, "effects.log", `Effect Documentation:

--------------------

add_to_variable_list - Adds the event target to a variable list
add_to_variable_list = { name = X target = Y }
Where X is the name of the variable
Supported Scopes: none

--------------------
`)
	s := ReadSchema([]string{dir})
	got := s.Effects["add_to_variable_list"]
	if len(got.In) != 1 || got.In[0] != "none" {
		t.Fatalf("In = %v", got.In)
	}
	if got.Doc == "" {
		t.Error("doc lost")
	}
}

// --- Real-install tier -------------------------------------------------------

// realDocs returns the dump dir for a game, or skips.
func realDocs(t *testing.T, env string) string {
	t.Helper()
	dir := os.Getenv(env)
	if dir == "" {
		corpusEnv(t, "%s not set", env)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("%s = %q: %v", env, dir, err)
	}
	return dir
}

func TestSchemaRealCK3(t *testing.T) {
	s := ReadSchema([]string{realDocs(t, "PMT_TEST_DOCS_CK3")})
	if s == nil {
		t.Fatal("nil schema")
	}
	// Counts measured against CK3 1.19.0.6.
	if len(s.Scopes) < 60 {
		t.Errorf("scopes = %d, want >= 60", len(s.Scopes))
	}
	if len(s.Effects) < 1800 {
		t.Errorf("effects = %d, want >= 1800", len(s.Effects))
	}
	if len(s.Triggers) < 1700 {
		t.Errorf("triggers = %d, want >= 1700", len(s.Triggers))
	}
	if len(s.Links) < 250 {
		t.Errorf("links = %d, want >= 250", len(s.Links))
	}
	if len(s.OnActions) < 800 {
		t.Errorf("onActions = %d, want >= 800", len(s.OnActions))
	}
	// The scope-type universe must include the common CK3 types.
	for _, want := range []string{"character", "landed_title", "culture", "faith", "province"} {
		if _, ok := s.Scopes[want]; !ok {
			t.Errorf("scope type %q missing", want)
		}
	}
	// The typed-prefix table must be declared, not guessed.
	pre := s.Prefixes()
	for name, out := range map[string]string{
		"culture": "culture", "faith": "faith", "title": "landed_title",
		"character": "character", "trait": "trait", "province": "province",
	} {
		if pre[name] != out {
			t.Errorf("prefix %q -> %q, want %q", name, pre[name], out)
		}
	}
	// Input->Output links are the transition graph.
	if l := s.Links["betrothed"]; l.Out != "character" || len(l.In) == 0 {
		t.Errorf("betrothed = %+v", l)
	}
	// Effects must carry their input scope.
	if e := s.Effects["add_gold"]; len(e.In) == 0 {
		t.Errorf("add_gold = %+v", e)
	}
	if s.ReadAt == "" {
		t.Error("ReadAt not set")
	}
}

func TestSchemaRealEU5(t *testing.T) {
	s := ReadSchema([]string{realDocs(t, "PMT_TEST_DOCS_EU5")})
	if s == nil {
		t.Fatal("nil schema")
	}
	if len(s.Effects) < 1400 {
		t.Errorf("effects = %d, want >= 1400", len(s.Effects))
	}
	if len(s.Triggers) < 1700 {
		t.Errorf("triggers = %d, want >= 1700", len(s.Triggers))
	}
	// 334 headings collapse to 289 unique names: a link may be declared twice,
	// once global and once contextual.
	if len(s.Links) < 280 {
		t.Errorf("links = %d, want >= 280", len(s.Links))
	}
	if e := s.Effects["add_accepted_culture"]; e.Target != "culture" {
		t.Errorf("add_accepted_culture = %+v", e)
	}
}

func TestSchemaRealVic3(t *testing.T) {
	s := ReadSchema([]string{realDocs(t, "PMT_TEST_DOCS_VIC3")})
	if s == nil {
		t.Fatal("nil schema")
	}
	if len(s.Effects) < 3000 {
		t.Errorf("effects = %d, want >= 3000", len(s.Effects))
	}
	if len(s.Links) < 320 {
		t.Errorf("links = %d, want >= 320", len(s.Links))
	}
	// Vic3's short prefixes are the ones the vote model handled worst.
	// `s:` scopes to a state *region*, per the game's own description — the
	// kind of fact worth reading rather than assuming.
	pre := s.Prefixes()
	for name, out := range map[string]string{
		"c": "country", "cu": "culture", "s": "state_region", "p": "province",
	} {
		if pre[name] != out {
			t.Errorf("prefix %q -> %q, want %q", name, pre[name], out)
		}

	}
	if e := s.Effects["activate_law"]; e.Target != "law_type" {
		t.Errorf("activate_law = %+v", e)
	}
}

// corpusEnv skips when the real-install tier was not requested, and FAILS when
// it was requested but the environment cannot serve it. Copied with schema_test
// when declared split out of catalog (R1); the original stays there for the
// tests that did not move.
func corpusEnv(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("PMT_TEST_TIER") == "full" {
		t.Fatalf("PMT_TEST_TIER=full but "+format, args...)
	}
	t.Skipf(format, args...)
}

// TokenParams reads an iterator's declared arguments out of the
// usage lines script_docs prints beside it.

func TestTokenParams(t *testing.T) {
	s := &Schema{Triggers: map[string]EngineToken{
		// Verbatim from CK3 triggers.log. The `any_` spelling declares the
		// argument; the `random_` one does not, and they are one iterator.
		"any_trait_in_category": {Usage: "any_trait_in_category = { category = fame/health/etc }\n" +
			"any_trait_in_category = { <count=num/all> / <percent=fixed_point> <triggers> }"},
		"any_county_in_region": {Usage: "any_county_in_region = { <count=num/all> / <percent=fixed_point> <triggers> }"},
	}, Effects: map[string]EngineToken{
		"random_trait_in_category": {Usage: "random_trait_in_category = { limit = { <triggers> } (optional) weight = { mtth } <effects> }"},
		"every_vassal":             {Usage: "every_vassal = { limit = { <triggers> } <effects> }"},
		"add_gold":                 {Usage: "add_gold = 5"},
	}}

	for _, tt := range []struct {
		key  string
		want []string
	}{
		// The family is read as one and the union is symmetric: the random_
		// spelling gains `category` from the any_ spelling that documents it,
		// and the any_ spelling gains `limit` and `weight` back. That is the
		// point -- they are one iterator the game happens to document unevenly.
		{"any_trait_in_category", []string{"category", "count", "limit", "percent", "weight"}},
		{"random_trait_in_category", []string{"category", "count", "limit", "percent", "weight"}},
		{"every_vassal", []string{"limit"}},
		{"any_county_in_region", []string{"count", "percent"}},
		// No block in the usage line means no arguments.
		{"add_gold", nil},
		{"not_a_token", nil},
	} {
		t.Run(tt.key, func(t *testing.T) {
			got := s.TokenParams(tt.key)
			var keys []string
			for k := range got {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if !slices.Equal(keys, tt.want) {
				t.Errorf("TokenParams(%q) = %v, want %v", tt.key, keys, tt.want)
			}
		})
	}
}

// A placeholder is not an argument: `<triggers>` inside `limit = { … }` sits at
// depth two and must not become a name.
func TestUsageParamsIgnoresNestedPlaceholders(t *testing.T) {
	got := usageParams("x = { limit = { <triggers> } weight = { mtth } }")
	sort.Strings(got)
	if !slices.Equal(got, []string{"limit", "weight"}) {
		t.Errorf("usageParams = %v, want [limit weight]", got)
	}
}
