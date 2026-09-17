// lsp_test.go holds the fixtures every LSP test shares: building a live session
// over a temp mod, and the assertion helpers. The tests themselves live beside
// the request they cover — inspect_test.go, definition_test.go, complete_test.go,
// diagnostics_test.go, cursor_test.go.

package lsp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

func write(t *testing.T, root, rel, body string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// testCalendar is applied to the mod buildSession creates; package-level so the builder keeps one signature.
var testCalendar derived.ModCalendar

// withCalendar declares an era for the mod the next buildSession creates, cleared when the test ends.
func withCalendar(t *testing.T, c derived.ModCalendar) {
	t.Helper()
	testCalendar = c
	t.Cleanup(func() { testCalendar = derived.ModCalendar{} })
}

func buildSession(
	t *testing.T, game string, files map[string]string,
	cache *vanillaCache, vloc *vanillaLoc,
) (*session.Session, string) {
	t.Helper()
	if game == "" {
		game = "ck3"
	}
	root := t.TempDir()
	if game == "ck3" {
		if _, ok := files["descriptor.mod"]; !ok {
			write(t, root, "descriptor.mod", "name = \"t\"\n")
		}
	}
	for rel, body := range files {
		write(t, root, rel, body)
	}
	s := session.New("ws", game, "english", toIndex(cache, vloc), []derived.ModInput{
		{Origin: "mod", Root: root, Order: 0, Calendar: testCalendar},
	})
	for rel, body := range files {
		s.DidOpen(filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	return s, root
}

// ck3Typed declares the CK3 scope types and links these tests rely on; a nested database is harvested only where the game declares the child kind.
func ck3Typed() *vanillaCache {
	return &vanillaCache{Schema: &declared.Schema{
		Scopes: map[string]declared.ScopeType{
			"faith": {}, "culture": {}, "character": {}, "landed_title": {},
		},
		Links: map[string]declared.ScopeLink{
			"title": {Out: "landed_title", Global: true, Data: true},
			"faith": {Out: "faith", Global: true, Data: true},
		},
	}}
}

func ck3Sess(t *testing.T, body string) (*session.Session, string) {
	t.Helper()
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, nil, nil)
	return s, filepath.Join(root, "events", "x.txt")
}

func lineCol(src, needle string) (int, int) {
	i := strings.Index(src, needle)
	if i < 0 {
		return -1, -1
	}
	return strings.Count(src[:i], "\n"), i - (strings.LastIndex(src[:i], "\n") + 1)
}

func hasDiag(diags []Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func wantHover(t *testing.T, s *session.Session, f string, line, col int, subs ...string) {
	t.Helper()
	h := Hover(s, f, line, col)
	if h == nil {
		t.Fatalf("hover nil at %d:%d want %v", line, col, subs)
	}
	blob := h.Kind + "\n" + h.Key + "\n" + h.Hint + "\n" + h.Docs + "\n" + h.Body + "\n" + h.Usage + "\n" + h.Owner
	for _, v := range h.Values {
		blob += "\n" + v.Text
	}
	for _, sub := range subs {
		if !strings.Contains(blob, sub) {
			t.Fatalf("hover kind=%#v key=%#v hint=%#v docs=%#v body=%#v usage=%#v missing %q",
				h.Kind, h.Key, h.Hint, h.Docs, h.Body, h.Usage, sub)
		}
	}
}

// `key = ` parses to an Assignment with a nil Value, which is the buffer's state on every keystroke while typing one.
func TestZNilValueRequests(t *testing.T) {
	for _, body := range []string{
		"test.1 = {\n\ttype = \n}\n",
		"test.1 = {\n\timmediate = {\n\t\tadd_gold = \n\t}\n}\n",
		"test.1 = \n",
		"a = = \n",
		"test.1 = {\n\tculture_group:\n}\n",
		"test.1 = {\n\thas_culture_group = culture_group:\n}\n",
		"x = {\n\ty =",
	} {
		s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, nil, nil)
		f := filepath.Join(root, "events", "x.txt")
		for line := range 4 {
			for col := range 24 {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("panic at %d:%d in %q: %v", line, col, body, r)
						}
					}()
					_ = Hover(s, f, line, col)
					_ = Complete(s, f, line, col)
					_ = Definition(s, f, line, col)
					_ = References(s, f, line, col)
					_ = Diagnose(s, f)
					_ = FormatDocument(s, f)
				}()
			}
		}
	}
}

// vanillaCache and vanillaLoc are the pre-index shapes these fixtures were written against; temporary, until the task that rewrites these tests.
type vanillaCache struct {
	InstallID, GameID, InstallPath, GameVersion, ScannedAt string
	Defs                                                   []derived.Def
	LocRefs, CallRefs                                      []derived.Ref
	Edges                                                  []derived.Edge
	Schema                                                 *declared.Schema
	Structures, StructureBlocks, NestedMembers             map[string][]string
	Vocabulary, GUITypes, GUIProps, DataFunctions          []string
	LocKeyPrefixes, LocFields, Wrappers, LocEngineSlots    []string
	FieldValueKinds, FireKeys, PrefixKinds, KindScope      map[string]string
	KindInfo, FieldInfo                                    map[string]string
	FieldInfoByKind                                        map[string]map[string]string
	DeclaredFieldTypes                                     map[string]string
	DeclaredFieldTypesByKind                               map[string]map[string]string
	LocConventions                                         map[string][]loc.Convention
	NestedShapes                                           []derived.NestedShape
}

type vanillaLoc struct{ Sites map[string]derived.LocEntry }

func toIndex(c *vanillaCache, v *vanillaLoc) *derived.Index {
	if c == nil {
		c = &vanillaCache{}
	}
	x := derived.NewIndex(c.GameID)
	x.InstallID, x.InstallPath = c.InstallID, c.InstallPath
	x.GameVersion, x.ScannedAt, x.Declared = c.GameVersion, c.ScannedAt, c.Schema
	x.Schema = derived.Schema{
		GUITypes: c.GUITypes, GUIProps: c.GUIProps,
		DataFunctions: c.DataFunctions, LocKeyPrefixes: c.LocKeyPrefixes,
		VocabNames: c.Vocabulary, Prefix: c.PrefixKinds, KindScope: c.KindScope,
		Nested:         c.NestedShapes,
		LocEngineSlots: fixtureSet(c.LocEngineSlots),
		MacroDefs:      map[string]string{},
		Kinds:          map[string]derived.KindInfo{},
	}
	for kind, cs := range c.LocConventions {
		x.Schema.Kinds[kind] = derived.KindInfo{Loc: cs}
	}
	fixtureCells(&x.Schema, c)
	x.Docs = derived.Docs{
		KindInfo: c.KindInfo, FieldInfo: fixtureLower(c.FieldInfo),
		FieldInfoByKind:          fixtureLowerByKind(c.FieldInfoByKind),
		DeclaredFieldTypes:       fixtureTypes(c.DeclaredFieldTypes),
		DeclaredFieldTypesByKind: map[string]map[string]declared.DeclaredType{},
	}
	for kind, m := range c.DeclaredFieldTypesByKind {
		x.Docs.DeclaredFieldTypesByKind[kind] = fixtureTypes(m)
	}
	if c.Schema != nil {
		names := map[string]bool{}
		for _, n := range c.Vocabulary {
			names[n] = true
		}
		for n := range c.Schema.Effects {
			x.Schema.Effects = append(x.Schema.Effects, n)
			names[n] = true
		}
		for n := range c.Schema.Triggers {
			x.Schema.Triggers = append(x.Schema.Triggers, n)
			names[n] = true
		}
		c.Schema.EachName(func(n string) { names[n] = true })
		slices.Sort(x.Schema.Effects)
		slices.Sort(x.Schema.Triggers)
		x.Schema.EffectSet = fixtureSet(x.Schema.Effects)
		x.Schema.TriggerSet = fixtureSet(x.Schema.Triggers)
		x.Schema.VocabNames = nil
		for n := range names {
			x.Schema.VocabNames = append(x.Schema.VocabNames, n)
		}
		slices.Sort(x.Schema.VocabNames)
	}
	x.Schema.KindPrefix, x.Schema.ScopePrefix, x.Schema.ScopeKind =
		map[string]string{}, map[string]string{}, map[string]string{}
	for prefix, kind := range c.PrefixKinds {
		if cur, ok := x.Schema.KindPrefix[kind]; !ok || len(prefix) < len(cur) {
			x.Schema.KindPrefix[kind] = prefix
		}
	}
	for kind, scope := range c.KindScope {
		if cur, ok := x.Schema.ScopeKind[scope]; !ok || len(kind) < len(cur) {
			x.Schema.ScopeKind[scope] = kind
		}
	}
	if c.Schema != nil {
		for prefix, scope := range c.Schema.Prefixes() {
			if cur, ok := x.Schema.ScopePrefix[scope]; !ok || len(prefix) < len(cur) {
				x.Schema.ScopePrefix[scope] = prefix
			}
		}
	}
	byFile := map[string]*derived.FileHarvest{}
	at := func(p string) *derived.FileHarvest {
		h := byFile[p]
		if h == nil {
			h = &derived.FileHarvest{}
			byFile[p] = h
		}
		return h
	}
	for _, d := range c.Defs {
		h := at(d.Path)
		h.Defs = append(h.Defs, d)
	}
	for _, r := range append(append([]derived.Ref{}, c.LocRefs...), c.CallRefs...) {
		h := at(r.Path)
		h.Refs = append(h.Refs, r)
	}
	for _, e := range c.Edges {
		h := at(e.Path)
		h.Edges = append(h.Edges, e)
	}
	if v != nil {
		for k, e := range v.Sites {
			h := at(e.Path)
			if h.Loc.Vals == nil {
				h.Loc = derived.LocDelta{Lang: "english", Vals: map[string]derived.LocEntry{}}
			}
			h.Loc.Vals[k] = e
		}
	}
	for p, h := range byFile {
		x.Add(p, p, "", "script", h)
	}
	for _, d := range c.Defs {
		if strings.HasPrefix(d.Kind, "scripted_") {
			x.Schema.MacroDefs[d.Key] = d.Kind
		}
	}
	x.SortLocKeys()
	return x
}

func fixtureSet(in []string) map[string]bool {
	out := map[string]bool{}
	for _, k := range in {
		out[k] = true
		out[strings.ToLower(k)] = true
	}
	return out
}

func fixtureSets(in map[string][]string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for k, v := range in {
		out[k] = fixtureSet(v)
	}
	return out
}

func fixtureLower(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		if out[strings.ToLower(k)] == "" {
			out[strings.ToLower(k)] = v
		}
	}
	return out
}

func fixtureLowerByKind(in map[string]map[string]string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for k, v := range in {
		out[k] = fixtureLower(v)
	}
	return out
}

func fixtureTypes(in map[string]string) map[string]declared.DeclaredType {
	out := map[string]declared.DeclaredType{}
	for k, v := range in {
		out[strings.ToLower(k)] = declared.ClassifyDeclaredType(v)
	}
	return out
}

// fixtureCells writes the pre-cell fixture fields onto the learned schema, temporary with the shim above it.
func fixtureCells(s *derived.Schema, c *vanillaCache) {
	s.Cells = map[string]map[string]map[string]*derived.CellType{}
	at := func(kind, path, key string) *derived.CellType {
		byPath := s.Cells[kind]
		if byPath == nil {
			byPath = map[string]map[string]*derived.CellType{}
			s.Cells[kind] = byPath
		}
		byKey := byPath[path]
		if byKey == nil {
			byKey = map[string]*derived.CellType{}
			byPath[path] = byKey
		}
		t := byKey[key]
		if t == nil {
			t = &derived.CellType{}
			byKey[key] = t
		}
		return t
	}
	// The kinds a fixture's files can sit under, since a cell is addressed by its file's kind.
	kinds := map[string]bool{}
	for _, k := range []string{
		"event", "on_action", "scripted_effect", "scripted_effects",
		"scripted_trigger", "scripted_triggers", "script_values", "decisions",
		"traits", "character_interactions", "gui_type", "landed_titles",
		"coat_of_arms", "cultures", "religions", "religion_types", "titles",
		"activity_types", "game_rules", "modifiers", "customizable_localization",
		"provinces", "buildings", "laws", "countries", "journal_entries",
		"map_data", "named_colors", "council_positions", "story_cycles",
	} {
		kinds[k] = true
	}
	for kind := range c.Structures {
		kinds[kind] = true
	}
	for _, d := range c.Defs {
		kinds[d.Kind] = true
	}
	// The block names a fixture mentions plus the ones every fixture writes without saying so, because a cell is addressed by its whole enclosing path.
	blocks := map[string]bool{
		"immediate": true, "option": true, "after": true, "trigger": true,
		"limit": true, "is_shown": true, "effect": true, "desc": true,
		"first_valid": true, "triggered_desc": true, "modifier": true,
		"if": true, "else": true, "random_list": true, "ai_chance": true,
	}
	for block, members := range c.NestedMembers {
		blocks[block] = true
		for _, m := range members {
			blocks[m] = true
		}
	}
	for _, keys := range c.Structures {
		for _, k := range keys {
			blocks[k] = true
		}
	}
	for _, keys := range c.StructureBlocks {
		for _, k := range keys {
			blocks[k] = true
		}
	}
	for k := range fixtureFireKeys(c) {
		blocks[k] = true
	}
	deep := map[string]bool{}
	for k := range blocks {
		deep[k] = true
	}
	if c.Schema != nil {
		for n := range c.Schema.Effects {
			blocks[n] = true
		}
		for n := range c.Schema.Triggers {
			blocks[n] = true
		}
	}
	paths := []string{""}
	for a := range blocks {
		paths = append(paths, a)
	}
	// Two levels only for the blocks a fixture names itself; pairing the whole vocabulary is thousands of paths nothing reads.
	for a := range deep {
		for b := range blocks {
			paths = append(paths, a+"."+b)
		}
	}
	for kind, keys := range c.Structures {
		for i, k := range keys {
			t := at(kind, "", k)
			t.N = int32(len(keys) - i)
		}
		for _, k := range c.StructureBlocks[kind] {
			t := at(kind, "", k)
			t.Flags = derived.CellBlock
			t.Names = true
		}
	}
	for kind := range kinds {
		for block, members := range c.NestedMembers {
			for _, prefix := range paths {
				path := block
				if prefix != "" {
					path = prefix + "." + block
				}
				for i, m := range members {
					at(kind, path, m).N = int32(len(members) - i)
				}
			}
		}
		for _, path := range paths {
			for key, k := range c.FieldValueKinds {
				at(kind, path, key).Kinds = []string{k}
			}
			for key, k := range fixtureFireKeys(c) {
				for _, cellKey := range []string{key, "id", "<n>"} {
					cellPath := path
					if cellKey != key {
						cellPath = key
						if path != "" {
							cellPath = path + "." + key
						}
					}
					at(kind, cellPath, cellKey).Kinds = []string{k}
				}
			}
			for _, key := range c.LocFields {
				t := at(kind, path, key)
				t.Kinds = append(t.Kinds, derived.LabelLoc)
			}
		}
	}
}

// fixtureFireKeys stands in for what a scanned install teaches -- which keys hold event and on_action ids -- unless the fixture states its own.
func fixtureFireKeys(c *vanillaCache) map[string]string {
	if len(c.FireKeys) > 0 {
		return c.FireKeys
	}
	return map[string]string{
		"trigger_event": "event", "trigger_event_silently": "event",
		"on_action": "on_action", "events": "event", "random_events": "event",
	}
}
