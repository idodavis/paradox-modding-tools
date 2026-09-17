// views_test.go holds the fixtures every view test shares.

package views

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/loc"
)

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
