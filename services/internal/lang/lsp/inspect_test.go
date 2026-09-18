// inspect_test.go is one table over the hover card: one row per identity class
// the resolvers produce, plus the floors where the right card is no card.

package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/loc"
)

type hoverCase struct {
	name  string
	game  string
	files map[string]string
	cache *vanillaCache
	vloc  *vanillaLoc
	cal   derived.ModCalendar
	// rel names the file to hover in, at is the needle in that file's text and
	// skip the columns past its start.
	rel, at string
	skip    int
	want    []string
	none    bool
	check   func(t *testing.T, h *Inspect)
}

func TestHover(t *testing.T) {
	for _, tc := range hoverCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cal.Declared() {
				withCalendar(t, tc.cal)
			}
			game := tc.game
			if game == "" {
				game = "ck3"
			}
			s, root := buildSession(t, game, tc.files, tc.cache, tc.vloc)
			f := filepath.Join(root, filepath.FromSlash(tc.rel))
			line, col := lineCol(tc.files[tc.rel], tc.at)
			if line < 0 {
				t.Fatalf("needle %q not in %s", tc.at, tc.rel)
			}
			h := Hover(s, f, line, col+tc.skip)
			if tc.none {
				if h != nil {
					t.Fatalf("want no card, got %#v", h)
				}
				return
			}
			if h == nil {
				t.Fatalf("no card at %s:%d:%d", tc.rel, line, col+tc.skip)
			}
			blob := strings.Join([]string{
				h.Kind, h.Key, h.Hint, h.Docs, h.Body, h.Usage, h.Owner,
				h.Accepts, h.Scope, h.ScopeOut,
			}, "\n")
			for _, v := range h.Values {
				blob += "\n" + v.Text
			}
			for _, sub := range tc.want {
				if !strings.Contains(blob, sub) {
					t.Fatalf("card %#v missing %q", h, sub)
				}
			}
			if tc.check != nil {
				tc.check(t, h)
			}
		})
	}
}

var hoverCases = []hoverCase{{
	// A definition: the object's own kind, its site, and the localization the
	// kind's convention names for it.
	name: "def",
	files: map[string]string{
		"events/x.txt":                         "namespace = ns\nns.1 = {\n\ttype = character_event\n}\n",
		"localization/english/e_l_english.yml": "l_english:\n ns.1:0 \"Hello\"\n",
	},
	cache: &vanillaCache{LocConventions: map[string][]loc.Convention{
		"event": {{Template: "{id}", Coverage: 1}},
	}},
	rel: "events/x.txt", at: "ns.1 = {",
	want: []string{"event", "ns.1", "Hello"},
	check: func(t *testing.T, h *Inspect) {
		if h.Origin != "mod" || h.Path == "" {
			t.Fatalf("def card has no mod site: %#v", h)
		}
	},
}, {
	// A localization key: the player-facing string, and the site with a column —
	// languageClient gates the whole selection on Col, so a card with a line and
	// no column opens the file at line 1.
	name: "loc key",
	files: map[string]string{
		"events/x.txt":                         "ns.1 = {\n\ttitle = wanted_key\n}\n",
		"localization/english/a_l_english.yml": "l_english:\n first:0 \"one\"\n wanted_key:0 \"\\n\\nHello\"\n",
	},
	rel: "events/x.txt", at: "wanted_key",
	want: []string{"loc key", "Hello"},
	check: func(t *testing.T, h *Inspect) {
		if h.Body != "Hello" {
			t.Fatalf("escapes not unescaped: %q", h.Body)
		}
		if h.Hint != "" {
			t.Fatalf("loc card described a kind: hint=%q", h.Hint)
		}
		if h.Line == 0 || h.Col == 0 {
			t.Fatalf("loc site line=%d col=%d", h.Line, h.Col)
		}
	},
}, {
	// A key inside a structure takes the game's own (structure, field) prose.
	// EU5 documents `show_if` under `select_trigger` and vanilla writes it
	// nowhere, so a card keyed by the file's kind, or gated on what vanilla
	// happens to have written, has nothing to say about it.
	name: "field doc by structure",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\tselect_trigger = {\n\t\tshow_if = { }\n\t}\n}\n",
	},
	cache: &vanillaCache{FieldInfoByKind: map[string]map[string]string{
		"select_trigger": {"show_if": "trigger to see if this stage is needed"},
	}},
	rel: "events/x.txt", at: "show_if",
	want: []string{"select trigger key", "show_if", "trigger to see if this stage is needed"},
}, {
	// A scripted macro: script_docs covers the engine API, so the $PARAM$ names
	// inside the body are the only signature that will ever exist.
	name: "macro",
	files: map[string]string{
		"common/scripted_effects/x.txt": "scripted_effect give_gift_effect = {\n" +
			"\tadd_gold = $AMOUNT$\n\tsave_scope_as = $RECIPIENT$\n}\n",
	},
	rel: "common/scripted_effects/x.txt", at: "give_gift_effect",
	want: []string{"scripted effect", "give_gift_effect = {", "AMOUNT", "RECIPIENT"},
}, {
	// An ephemeral: its defining site when the workspace has one, and the values
	// the corpus writes for the name — folded when the file was indexed, not by
	// re-parsing every site on each hover.
	name: "ephemeral with site",
	files: map[string]string{
		"events/a.txt": "ns.1 = {\n\timmediate = {\n\t\tset_variable = { name = foo value = 3 }\n\t}\n}\n",
		"events/b.txt": "ns.2 = {\n\timmediate = {\n\t\texists = var:foo\n\t}\n}\n",
	},
	rel: "events/b.txt", at: "var:foo", skip: len("var:"),
	want: []string{"var", "foo", "3"},
	check: func(t *testing.T, h *Inspect) {
		if h.Path == "" || h.Line == 0 {
			t.Fatalf("ephemeral card carries no defining site: %#v", h)
		}
	},
}, {
	// A saved scope has no right-hand side, so its value is what it was saved
	// FROM; the iterator names the list, which is the half a reader cannot infer.
	name: "saved scope source",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\ttitle:c_rouen = { save_scope_as = target }\n" +
			"\trandom_in_list = {\n\t\tlist = my_list\n\t\tsave_scope_as = target\n\t}\n" +
			"\texists = scope:target\n}\n",
	},
	rel: "events/x.txt", at: "scope:target", skip: len("scope:"),
	want: []string{"saved scope", "title:c_rouen", "random_in_list - list=my_list"},
}, {
	// A mod definition that overrides a vanilla one names both sites.
	name: "vanilla twin",
	files: map[string]string{
		"events/x.txt": "overlay.1 = {\n\ttype = character_event\n}\n",
	},
	cache: &vanillaCache{Defs: []derived.Def{{
		Kind: "event", Key: "overlay.1", Path: "vanilla/events/v.txt", Line: 0, End: 9,
	}}},
	rel: "events/x.txt", at: "overlay.1",
	want: []string{"event", "overlay.1"},
	check: func(t *testing.T, h *Inspect) {
		if h.Origin != "mod" || h.VanillaPath == "" ||
			h.VanillaOriginName != "Crusader Kings III" {
			t.Fatalf("overlay not reported: %#v", h)
		}
	},
}, {
	// A declared token: the signature the game states for it, which a key that
	// is also a structure key of its kind used to drop on the floor.
	name: "token signature",
	files: map[string]string{
		"common/activity_types/a.txt": "act = {\n\tadd_to_list = my_list\n}\n",
	},
	cache: &vanillaCache{
		Structures: map[string][]string{"activity_types": {"add_to_list"}},
		Schema: &declared.Schema{Effects: map[string]declared.EngineToken{
			"add_to_list": {In: []string{"character"}, Usage: "add_to_list = <list name>"},
		}},
	},
	rel: "common/activity_types/a.txt", at: "add_to_list",
	want: []string{"effect", "Effect", "runs on character", "add_to_list = <list name>"},
}, {
	// A name in both the corpus vocabulary and the game's declared engine API
	// cards as the declared kind; the vocabulary resolver used to win first,
	// which showed a declared effect's card as though nothing named it.
	name: "vocabulary does not outrank a declared effect",
	files: map[string]string{
		"events/x.txt": "namespace = t\n\nt.1 = {\n\timmediate = {\n\t\tadd_gold = 5\n\t}\n}\n",
	},
	cache: &vanillaCache{
		Vocabulary: []string{"add_gold"},
		Schema:     &declared.Schema{Effects: map[string]declared.EngineToken{"add_gold": {}}},
	},
	rel: "events/x.txt", at: "add_gold",
	want: []string{"effect"},
}, {
	// The scope in force at the cursor, and where a link moves: both declared,
	// both already computed for completion, neither ever shown before.
	name: "scope",
	files: map[string]string{
		"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n" +
			"\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n",
	},
	cache: scopedCache(),
	rel:   "common/on_action/x.txt", at: "capital_county",
	want:  []string{"from character", "yields landed title"},
	check: func(t *testing.T, h *Inspect) {
		if h.Scope != "character" || h.ScopeOut != "landed_title" {
			t.Fatalf("scope=%q scopeOut=%q", h.Scope, h.ScopeOut)
		}
	},
}, {
	// An iterator states the scope it takes, not only the one it runs on: it
	// names the target it hands to the blocks inside it.
	name: "iterator target",
	files: map[string]string{
		"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\tevery_vassal = {\n" +
			"\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n",
	},
	cache: scopedCache(),
	rel:   "common/on_action/x.txt", at: "every_vassal",
	want:  []string{"runs on character", "takes a character"},
}, {
	// A structure key at the definition's own level is named after the kind, and
	// the install is the origin that documents it. `option` came back nil in the
	// 2026-09-17 sample.
	name: "structure key",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\ttype = character_event\n\toption = {\n\t\tname = x\n\t}\n}\n",
	},
	cache: &vanillaCache{
		Structures: map[string][]string{"event": {"option", "type"}},
		FieldInfoByKind: map[string]map[string]string{
			"event": {"option": "One choice offered to the player."},
		},
	},
	rel: "events/x.txt", at: "option",
	want: []string{"event key", "option", "One choice offered to the player."},
	check: func(t *testing.T, h *Inspect) {
		if h.OriginName != "Crusader Kings III" {
			t.Fatalf("structure key origin=%q", h.OriginName)
		}
	},
}, {
	// A block that is itself a declared effect holds its arguments, not the keys
	// of a container: "set variable key" reads as though set_variable were one.
	name: "body key",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\timmediate = {\n\t\tset_variable = {\n\t\t\tname = x\n\t\t}\n\t}\n}\n",
	},
	cache: &vanillaCache{
		Structures:    map[string][]string{"event": {"immediate"}},
		NestedMembers: map[string][]string{"set_variable": {"name", "value"}},
		Schema: &declared.Schema{Effects: map[string]declared.EngineToken{
			"set_variable": {Doc: "Sets a variable."},
		}},
	},
	rel: "events/x.txt", at: "name = x",
	want: []string{"argument"},
}, {
	// A key names what it belongs to, never whatever block encloses it: `OR`
	// groups and `scope:` redirects, and neither decides which keys are legal.
	name: "container is not a grouping block",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\ttrigger = {\n\t\tOR = {\n\t\t\tscope:target = {\n" +
			"\t\t\t\thas_trait = brave\n\t\t\t}\n\t\t}\n\t}\n}\n",
	},
	cache: &vanillaCache{
		Structures:    map[string][]string{"event": {"trigger", "type"}},
		NestedMembers: map[string][]string{"trigger": {"or", "has_trait", "exists"}},
		FieldInfoByKind: map[string]map[string]string{
			"event": {"has_trait": "Checks a trait."},
		},
	},
	rel: "events/x.txt", at: "has_trait",
	want: []string{"trigger", "Checks a trait."},
	check: func(t *testing.T, h *Inspect) {
		for _, wrong := range []string{"OR", "scope"} {
			if strings.Contains(h.Kind, wrong) {
				t.Fatalf("Kind = %q names a construct, not a container", h.Kind)
			}
		}
	},
}, {
	// A value nothing in the model defines is still the value of a key that is
	// documented. `add_character_flag = born_as_taltos` came back nil.
	name: "slot value",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\timmediate = {\n\t\tadd_character_flag = born_as_taltos\n\t}\n}\n",
	},
	cache: &vanillaCache{FieldInfo: map[string]string{
		"add_character_flag": "Adds a flag to the character.",
	}},
	rel: "events/x.txt", at: "born_as_taltos",
	want: []string{"add character flag value", "born_as_taltos", "Adds a flag to the character."},
}, {
	// What a key's value may be, declared before derived: the fixture's derived
	// field type deliberately disagrees with the declared target.
	name: "accepts declared target",
	files: map[string]string{
		"common/on_action/x.txt": "on_birth = {\n\ttrigger = {\n\t\thas_culture_group = x\n\t}\n}\n",
	},
	cache: &vanillaCache{
		Schema: &declared.Schema{
			Scopes: map[string]declared.ScopeType{
				"character": {ChangeScopes: true, ExecuteEffects: true}, "culture_group": {},
			},
			Triggers: map[string]declared.EngineToken{
				"has_culture_group": {In: []string{"character"}, Target: "culture_group"},
			},
			OnActions: map[string]string{"on_birth": "character"},
		},
		FieldValueKinds: map[string]string{"has_culture_group": "traits"},
	},
	rel: "common/on_action/x.txt", at: "has_culture_group",
	want: []string{"accepts a culture group"},
	check: func(t *testing.T, h *Inspect) {
		if strings.Contains(h.Accepts, "trait") {
			t.Fatalf("Accepts = %q used the derived type over the declared one", h.Accepts)
		}
	},
}, {
	// A field holding a localization key takes ANY key, not one of a set: the
	// enumeration listed forty-one unrelated event descriptions under `desc`.
	name: "accepts a loc key",
	files: map[string]string{
		"events/x.txt": "namespace = ns\nns.1 = {\n\ttype = character_event\n\tdesc = ns.1.desc\n}\n",
	},
	cache: &vanillaCache{FieldInfo: map[string]string{"desc": "Dynamic Description syntax."}},
	rel:   "events/x.txt", at: "desc = ns",
	want: []string{"accepts a localization key"},
}, {
	// EU5 declares a category for all 2,436 of its modifiers and prose for none,
	// so the category is the whole card; "all" is on every one and says nothing.
	name: "modifier area",
	game: "eu5",
	files: map[string]string{
		"in_game/common/town_rights/x.txt": "aga_tr = {\n\tlocal_trades_per_burgher = 0.25\n}\n",
	},
	cache: &vanillaCache{Schema: &declared.Schema{
		Modifiers: map[string]declared.Modifier{
			"local_trades_per_burgher": {Area: "location, all"},
		},
	}},
	rel: "in_game/common/town_rights/x.txt", at: "local_trades_per_burgher",
	want: []string{"applies to location"},
	check: func(t *testing.T, h *Inspect) {
		if strings.Contains(strings.ToLower(h.Docs), " all") {
			t.Fatalf("docs = %q keeps the all-modifiers filler", h.Docs)
		}
	},
}, {
	// A bare number on the left of an `=` is a branch of a distribution, and the
	// denominator is the sum of its siblings, not an assumed 100. CK3 also keys
	// map positions by province id, so `95` resolved to a French town.
	name: "weight",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\timmediate = {\n\t\trandom_list = {\n" +
			"\t\t\t95 = { trigger_event = birth.1001 }\n" +
			"\t\t\t5 = { trigger_event = birth.1002 }\n\t\t}\n\t}\n}\n",
	},
	cache: &vanillaCache{Defs: []derived.Def{
		{Kind: "map_data", Key: "95", Path: "positions.txt"},
	}},
	rel: "events/x.txt", at: "95 = { trigger_event",
	want: []string{"weight", "95%", "random_list"},
}, {
	// The same numeric key is not always a probability: a Vic3 gene block writes
	// `20 = empty`, an index whose entries are scalars, so there is no share.
	name: "numeric lookup is not a weight",
	game: "vic3",
	files: map[string]string{
		"common/genes/g.txt": "portrait = {\n\tgenes = {\n\t\thair = {\n" +
			"\t\t\t20 = empty\n\t\t\t80 = empty\n\t\t}\n\t}\n}\n",
	},
	rel: "common/genes/g.txt", at: "20 = empty", none: true,
}, {
	// A total conversion offsets its years so the engine's arithmetic works, and
	// the reader would otherwise subtract 7,782 in their head.
	name: "date in a declared era",
	cal:  derived.ModCalendar{Era: "AC", EpochOffset: 7782},
	files: map[string]string{
		"common/bookmarks/bookmarks/00.txt": "bm = {\n\tstart_date = 8082.1.1\n}\n",
	},
	rel: "common/bookmarks/bookmarks/00.txt", at: "8082.1.1",
	want: []string{"300.1.1 AC"},
}, {
	// No calendar, no card. A date is a number, and saying anything about one is
	// what made 1178.1.1 hover as a struggle.
	name: "date without a calendar",
	files: map[string]string{
		"common/bookmarks/bookmarks/00.txt": "bm = {\n\tstart_date = 8082.1.1\n}\n",
	},
	rel: "common/bookmarks/bookmarks/00.txt", at: "8082.1.1", none: true,
}, {
	// A literal is not a name. Vanilla localization defines a key spelled `yes`,
	// so `is_ai = yes` showed that key's player-facing string as the meaning.
	name: "literal value",
	files: map[string]string{
		"events/x.txt":                "ns.1 = {\n\ttrigger = {\n\t\tis_ai = yes\n\t}\n}\n",
		"localization/english/ui.yml": "l_english:\n yes:0 \"Yes\"\n",
	},
	rel: "events/x.txt", at: "yes\n", none: true,
}, {
	// Paradox localizes a great many words that are also script tokens. Not
	// knowing what a key is is the honest answer; calling it localization is not.
	name: "loc never wins over script",
	files: map[string]string{
		"events/x.txt":                         "ns.1 = {\n\tculture = french\n}\n",
		"common/cultures/c.txt":                "french = { }\n",
		"localization/english/a_l_english.yml": "l_english:\n french:0 \"French\"\n",
	},
	rel: "events/x.txt", at: "culture = french", skip: len("culture = "),
	check: func(t *testing.T, h *Inspect) {
		if h.Kind == "loc key" {
			t.Fatalf("culture value came back as a localization key: %#v", h)
		}
	},
}, {
	// An interpolation the engine fills in names no key anyone can define.
	name: "loc engine value",
	files: map[string]string{
		"localization/english/a_l_english.yml": "l_english:\n r:0 \"Cost: $VALUE|=+0$\"\n",
	},
	rel: "localization/english/a_l_english.yml", at: "VALUE",
	want: []string{"loc value", "$VALUE$"},
}, {
	// A comment is not script.
	name: "comment",
	files: map[string]string{
		"events/x.txt": "# event should not hover\nns.1 = { }\n",
	},
	rel: "events/x.txt", at: "event should", none: true,
}, {
	// A key that shares a name with a script parameter belongs to its own
	// container first: `option = { name = x }` is the option's own name, not the
	// macro parameter $name$ some other file declares.
	name: "option name is not a macro parameter",
	files: map[string]string{
		"events/x.txt":                  "namespace = t\nt.1 = {\n\ttype = character_event\n\toption = { name = t.1.a }\n}\n",
		"common/scripted_effects/e.txt": "do_it = { add_gold = $name$ }\n",
	},
	rel: "events/x.txt", at: "name = t.1.a",
	check: func(t *testing.T, h *Inspect) {
		if h.Kind == "script param" {
			t.Fatalf("option name stolen as script_param: %#v", h)
		}
	},
}, {
	// A macro parameter's card names its owner and every value callers pass it,
	// with how many times each one was seen.
	name: "script param",
	files: map[string]string{
		"common/scripted_triggers/t.txt": "my_trig = { exists = $TARGET$ }\n",
		"events/e.txt": "ev.1 = {\n\tmy_trig = { TARGET = title:c_rouen }\n" +
			"\tmy_trig = { TARGET = scope:adventurer_target }\n" +
			"\tmy_trig = { TARGET = scope:adventurer_target }\n}\n",
	},
	rel: "common/scripted_triggers/t.txt", at: "$TARGET$", skip: 1,
	want: []string{"title:c_rouen", "scope:adventurer_target"},
	check: func(t *testing.T, h *Inspect) {
		if h.Owner != "my_trig" {
			t.Fatalf("param owner=%q, want my_trig", h.Owner)
		}
		var rouen, adv int
		for _, v := range h.Values {
			switch v.Text {
			case "title:c_rouen":
				rouen = v.Count
			case "scope:adventurer_target":
				adv = v.Count
			}
		}
		if rouen != 1 || adv != 2 {
			t.Fatalf("counts rouen=%d adv=%d values=%#v", rouen, adv, h.Values)
		}
	},
}}
