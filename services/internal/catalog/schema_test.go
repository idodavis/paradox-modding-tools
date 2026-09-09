// schema_test.go covers the three script_docs syntaxes and, when a real game
// user-data folder is pointed at by PMT_TEST_DOCS_{CK3,EU5,VIC3}, asserts the
// counts a live install actually yields. The synthetic fixtures run everywhere;
// the install tests skip when the env var is unset.

package catalog

import (
	"os"
	"path/filepath"
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

const archiveEffects = `Effect Documentation:

--------------------

add_gold - Adds gold to the scoped character
Supported Scopes: character

--------------------
`

// The archive exists so a user runs script_docs once per game version, not once
// per accidental cleanup of their Documents folder.
func TestSchemaArchiveFallback(t *testing.T) {
	const installID = "archive-test"
	const version = "1.0"
	t.Cleanup(func() { _ = DropVanillaFiles(installID) })

	live := t.TempDir()
	if err := os.WriteFile(filepath.Join(live, "effects.log"),
		[]byte(archiveEffects), 0o644); err != nil {
		t.Fatal(err)
	}

	// First read: live dumps win and seed the archive.
	s, src := loadSchema(installID, version, []string{live})
	if s == nil || src != SchemaLive {
		t.Fatalf("source = %q, schema = %v; want live", src, s)
	}
	if _, ok := s.Effects["add_gold"]; !ok {
		t.Fatalf("effects = %v", s.Effects)
	}
	if !hasSchemaArchive(installID, version) {
		t.Fatal("archive not written after a live read")
	}

	// The user deletes their dumps. The archive must stand in.
	if err := os.RemoveAll(live); err != nil {
		t.Fatal(err)
	}
	s2, src2 := loadSchema(installID, version, []string{live})
	if src2 != SchemaArchive {
		t.Fatalf("source = %q, want archive", src2)
	}
	if s2 == nil {
		t.Fatal("nil schema from archive")
	}
	if _, ok := s2.Effects["add_gold"]; !ok {
		t.Errorf("archive lost the effect: %v", s2.Effects)
	}
	if s2.Source != SchemaArchive {
		t.Errorf("Source = %q, want archive", s2.Source)
	}

	// Forgetting the install drops the archive with it.
	if err := DropVanillaFiles(installID); err != nil {
		t.Fatal(err)
	}
	if hasSchemaArchive(installID, version) {
		t.Error("archive survived DropVanillaFiles")
	}
	if _, src3 := loadSchema(installID, version, []string{live}); src3 != SchemaMissing {
		t.Errorf("source = %q, want missing", src3)
	}
}

// A newer live dump must replace the archived copy, not be shadowed by it.
func TestSchemaArchiveRefreshes(t *testing.T) {
	const installID = "archive-refresh"
	const version = "1.0"
	t.Cleanup(func() { _ = DropVanillaFiles(installID) })

	live := t.TempDir()
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(live, "effects.log"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(archiveEffects)
	if _, src := loadSchema(installID, version, []string{live}); src != SchemaLive {
		t.Fatalf("source = %q", src)
	}

	write(archiveEffects + `
add_prestige - Adds prestige
Supported Scopes: character

--------------------
`)
	s, src := loadSchema(installID, version, []string{live})
	if src != SchemaLive {
		t.Fatalf("source = %q", src)
	}
	if _, ok := s.Effects["add_prestige"]; !ok {
		t.Fatalf("live re-read missed the new effect: %v", s.Effects)
	}

	if err := os.RemoveAll(live); err != nil {
		t.Fatal(err)
	}
	s2, src2 := loadSchema(installID, version, []string{live})
	if src2 != SchemaArchive {
		t.Fatalf("source = %q", src2)
	}
	if _, ok := s2.Effects["add_prestige"]; !ok {
		t.Errorf("archive was not refreshed: %v", s2.Effects)
	}
}

// Files the schema reader does not recognize must not be copied into the cache.
func TestSchemaArchiveSkipsUnrelatedFiles(t *testing.T) {
	const installID = "archive-filter"
	const version = "1.0"
	t.Cleanup(func() { _ = DropVanillaFiles(installID) })

	live := t.TempDir()
	for name, body := range map[string]string{
		"effects.log": archiveEffects,
		"error.log":   "some error spam\n",
		"game.log":    "startup noise\n",
	} {
		if err := os.WriteFile(filepath.Join(live, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := archiveScriptDocs(installID, version, []string{live})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("archived %d files, want only effects.log", n)
	}
	dir, err := schemaArchiveDir(installID, version)
	if err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() != "effects.log" {
			t.Errorf("unexpected archived file %q", e.Name())
		}
	}
}

// With no dumps anywhere the scan proceeds without a type system.
func TestSchemaArchiveMissing(t *testing.T) {
	s, src := loadSchema("archive-none", "1.0", []string{t.TempDir()})
	if s != nil || src != SchemaMissing {
		t.Fatalf("schema = %v, source = %q; want nil/missing", s, src)
	}
}

// ReadSchemaStatus backs the Workspace Health strip, so it must answer without
// loading the model and without the side effect loadSchema has: health is
// polled, and a read that re-copies the dumps every few seconds is a bug.
func TestReadSchemaStatusIsSideEffectFree(t *testing.T) {
	const installID = "status-test"
	t.Cleanup(func() { _ = DropVanillaFiles(installID) })

	install := t.TempDir()
	docs := filepath.Join(install, "logs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "effects.log"),
		[]byte(archiveEffects), 0o644); err != nil {
		t.Fatal(err)
	}
	pinUserData(t, install, install)

	st := ReadSchemaStatus("ck3", installID, install, "1.0")
	if st.Source != SchemaLive {
		t.Fatalf("source = %q, want live", st.Source)
	}
	if st.Effects == 0 {
		t.Errorf("no effects counted: %+v", st)
	}
	if st.ReadAt == "" {
		t.Error("ReadAt empty; staleness cannot be judged")
	}
	if st.Archived {
		t.Error("reading status must not create an archive")
	}

	// With no dumps and no archive the strip must say so plainly rather than
	// report a half-loaded type system.
	if err := os.RemoveAll(docs); err != nil {
		t.Fatal(err)
	}
	gone := ReadSchemaStatus("ck3", installID, install, "1.0")
	if gone.Source != SchemaMissing || gone.Effects != 0 {
		t.Errorf("no dumps and no archive: %+v", gone)
	}
	if gone.Folder == "" {
		t.Error("the walkthrough needs a folder to point at even when nothing is there")
	}
}
