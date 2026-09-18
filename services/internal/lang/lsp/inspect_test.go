// inspect_test.go is one table over the hover card: one row per identity class
// the resolvers produce, plus the floors where the right card is no card.

package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/index"
)

type hoverCase struct {
	name, game string
	files      map[string]string
	cal        index.ModCalendar
	rel, at    string // the file to hover in, and the needle in its text
	skip       int    // columns past the needle's start
	want       []string
	none       bool
	check      func(t *testing.T, h *Inspect)
}

func TestHover(t *testing.T) {
	for _, tc := range hoverCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cal.Declared() {
				withCalendar(t, tc.cal)
			}
			s, root := buildSession(t, tc.game, tc.files)
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
			blob := strings.Join([]string{h.Kind, h.Key, h.Hint, h.Docs, h.Body, h.Usage, h.Owner, h.Accepts, h.Scope, h.ScopeOut}, "\n")
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
	// A definition: the object's own kind, its site, and the localization the kind's convention names for it.
	name: "def",
	files: map[string]string{
		"common/traits/m.txt":                  "bold = {\n\tcategory = personality\n}\n",
		"localization/english/m_l_english.yml": "l_english:\n bold:0 \"Bold\"\n",
	},
	rel: "common/traits/m.txt", at: "bold = {",
	want: []string{"traits", "bold", "Bold"},
	check: func(t *testing.T, h *Inspect) {
		if h.Origin != "mod" || h.Path == "" {
			t.Fatalf("def card has no mod site: %#v", h)
		}
	},
}, {
	// A localization key: the player-facing string, and the site with a column.
	name: "loc key",
	files: map[string]string{
		"events/x.txt":                         "ns.1 = {\n\ttitle = wanted_key\n}\n",
		"localization/english/a_l_english.yml": "l_english:\n first:0 \"one\"\n wanted_key:0 \"\\n\\nHello\"\n",
	},
	rel: "events/x.txt", at: "wanted_key",
	want: []string{"loc key", "Hello"},
	check: func(t *testing.T, h *Inspect) {
		if h.Body != "Hello" || h.Hint != "" || h.Line == 0 || h.Col == 0 {
			t.Fatalf("loc card: %#v", h)
		}
	},
}, {
	// A key inside a structure takes the game's own (structure, field) prose, which EU5 writes for show_if under select_trigger and vanilla writes nowhere.
	name:  "field doc by structure",
	game:  "eu5",
	files: map[string]string{"in_game/events/x.txt": "ns.1 = {\n\tselect_trigger = {\n\t\tshow_if = { }\n\t}\n}\n"},
	rel:   "in_game/events/x.txt", at: "show_if",
	want: []string{"select trigger key", "show_if", "trigger to see if this stage is needed"},
}, {
	// A scripted macro: the $PARAM$ names inside the body are the only signature that will ever exist.
	name: "macro",
	files: map[string]string{
		"common/scripted_effects/x.txt": "scripted_effect give_gift_effect = {\n\tadd_gold = $AMOUNT$\n\tsave_scope_as = $RECIPIENT$\n}\n",
	},
	rel: "common/scripted_effects/x.txt", at: "give_gift_effect",
	want: []string{"scripted effect", "give_gift_effect = {", "AMOUNT", "RECIPIENT"},
}, {
	// An ephemeral: its defining site when the workspace has one, and the values the corpus writes for the name.
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
	// A saved scope has no right-hand side, so its value is what it was saved FROM.
	name: "saved scope source",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\ttitle:c_rouen = { save_scope_as = target }\n" +
			"\trandom_in_list = {\n\t\tlist = my_list\n\t\tsave_scope_as = target\n\t}\n\texists = scope:target\n}\n",
	},
	rel: "events/x.txt", at: "scope:target", skip: len("scope:"),
	want: []string{"saved scope", "title:c_rouen", "random_in_list - list=my_list"},
}, {
	// A mod definition that overrides a vanilla one names both sites.
	name:  "vanilla twin",
	files: map[string]string{"events/x.txt": "v.1 = {\n\ttype = character_event\n}\n"},
	rel:   "events/x.txt", at: "v.1",
	want: []string{"event", "v.1"},
	check: func(t *testing.T, h *Inspect) {
		if h.Origin != "mod" || h.VanillaPath == "" || h.VanillaOriginName != "Crusader Kings III" {
			t.Fatalf("overlay not reported: %#v", h)
		}
	},
}, {
	// A declared token: the signature the game states for it.
	name:  "token signature",
	files: map[string]string{"common/activity_types/a.txt": "act = {\n\tadd_to_list = my_list\n}\n"},
	rel:   "common/activity_types/a.txt", at: "add_to_list",
	want: []string{"effect", "Effect", "runs on character", "add_to_list = <list name>"},
}, {
	// The scope in force at the cursor, and where a link moves.
	name:  "scope",
	files: map[string]string{"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"},
	rel:   "common/on_action/x.txt", at: "capital_county",
	want: []string{"from character", "yields landed title"},
	check: func(t *testing.T, h *Inspect) {
		if h.Scope != "character" || h.ScopeOut != "landed_title" {
			t.Fatalf("scope=%q scopeOut=%q", h.Scope, h.ScopeOut)
		}
	},
}, {
	// An iterator states the scope it takes and the target it hands to the blocks inside it.
	name:  "iterator target",
	files: map[string]string{"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\tevery_vassal = {\n\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n"},
	rel:   "common/on_action/x.txt", at: "every_vassal",
	want: []string{"runs on character", "takes a character"},
}, {
	// A structure key at the definition's own level is named after the kind, and the install is the origin that documents it.
	name:  "structure key",
	files: map[string]string{"events/x.txt": "ns.1 = {\n\ttype = character_event\n\toption = {\n\t\tname = x\n\t}\n}\n"},
	rel:   "events/x.txt", at: "option",
	want: []string{"event key", "option", "One choice offered to the player."},
	check: func(t *testing.T, h *Inspect) {
		if h.OriginName != "Crusader Kings III" {
			t.Fatalf("structure key origin=%q", h.OriginName)
		}
	},
}, {
	// A block that is itself a declared effect holds its arguments, not the keys of a container.
	name:  "body key",
	files: map[string]string{"events/x.txt": "ns.1 = {\n\timmediate = {\n\t\tset_variable = {\n\t\t\tname = x\n\t\t}\n\t}\n}\n"},
	rel:   "events/x.txt", at: "name = x",
	want: []string{"argument"},
}, {
	// A key names what it belongs to, never whatever block encloses it: `OR` groups and `scope:` redirects.
	name: "container is not a grouping block",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\ttrigger = {\n\t\tOR = {\n\t\t\tscope:target = {\n\t\t\t\thas_trait = brave\n\t\t\t}\n\t\t}\n\t}\n}\n",
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
	// A value nothing in the model defines is still the value of a key that is documented.
	name:  "slot value",
	files: map[string]string{"events/x.txt": "ns.1 = {\n\timmediate = {\n\t\tadd_character_flag = born_as_taltos\n\t}\n}\n"},
	rel:   "events/x.txt", at: "born_as_taltos",
	want: []string{"add character flag value", "born_as_taltos", "Adds a flag to the character."},
}, {
	// What a key's value may be, declared before derived.
	name:  "accepts declared target",
	files: map[string]string{"common/on_action/x.txt": "on_birth = {\n\ttrigger = {\n\t\thas_culture_group = x\n\t}\n}\n"},
	rel:   "common/on_action/x.txt", at: "has_culture_group",
	want: []string{"accepts a culture group"},
}, {
	// A field holding a localization key takes ANY key, not one of a set.
	name:  "accepts a loc key",
	files: map[string]string{"events/x.txt": "namespace = ns\nns.1 = {\n\ttype = character_event\n\tdesc = ns.1.desc\n}\n"},
	rel:   "events/x.txt", at: "desc = ns",
	want: []string{"accepts a localization key"},
}, {
	// EU5 declares a category for every modifier and prose for none; "all" is on every one and says nothing.
	name:  "modifier area",
	game:  "eu5",
	files: map[string]string{"in_game/common/town_rights/x.txt": "aga_tr = {\n\tlocal_trades_per_burgher = 0.25\n}\n"},
	rel:   "in_game/common/town_rights/x.txt", at: "local_trades_per_burgher",
	want: []string{"applies to location"},
	check: func(t *testing.T, h *Inspect) {
		if strings.Contains(strings.ToLower(h.Docs), " all") {
			t.Fatalf("docs = %q keeps the all-modifiers filler", h.Docs)
		}
	},
}, {
	// A bare number on the left of an `=` is a branch of a distribution, and the denominator is the sum of its siblings.
	name: "weight",
	files: map[string]string{
		"events/x.txt": "ns.1 = {\n\timmediate = {\n\t\trandom_list = {\n\t\t\t95 = { trigger_event = birth.1001 }\n\t\t\t5 = { trigger_event = birth.1002 }\n\t\t}\n\t}\n}\n",
	},
	rel: "events/x.txt", at: "95 = { trigger_event",
	want: []string{"weight", "95%", "random_list"},
}, {
	// A Vic3 gene block writes `20 = empty`, an index whose entries are scalars, so there is no share.
	name:  "numeric lookup is not a weight",
	game:  "vic3",
	files: map[string]string{"common/genes/g.txt": "portrait = {\n\tgenes = {\n\t\thair = {\n\t\t\t20 = empty\n\t\t\t80 = empty\n\t\t}\n\t}\n}\n"},
	rel:   "common/genes/g.txt", at: "20 = empty", none: true,
}, {
	name:  "date in a declared era",
	cal:   index.ModCalendar{Era: "AC", EpochOffset: 7782},
	files: map[string]string{"common/bookmarks/bookmarks/00.txt": "bm = {\n\tstart_date = 8082.1.1\n}\n"},
	rel:   "common/bookmarks/bookmarks/00.txt", at: "8082.1.1",
	want: []string{"300.1.1 AC"},
}, {
	name:  "date without a calendar",
	files: map[string]string{"common/bookmarks/bookmarks/00.txt": "bm = {\n\tstart_date = 8082.1.1\n}\n"},
	rel:   "common/bookmarks/bookmarks/00.txt", at: "8082.1.1", none: true,
}, {
	// A literal is not a name, even where vanilla localizes the word.
	name:  "literal value",
	files: map[string]string{"events/x.txt": "ns.1 = {\n\ttrigger = {\n\t\tis_ai = yes\n\t}\n}\n"},
	rel:   "events/x.txt", at: "yes\n", none: true,
}, {
	// Paradox localizes a great many words that are also script tokens.
	name:  "loc never wins over script",
	files: map[string]string{"events/x.txt": "ns.1 = {\n\tculture = french\n}\n"},
	rel:   "events/x.txt", at: "culture = french", skip: len("culture = "),
	check: func(t *testing.T, h *Inspect) {
		if h == nil || h.Kind == "loc key" {
			t.Fatalf("culture value came back as a localization key: %#v", h)
		}
	},
}, {
	name:  "loc engine value",
	files: map[string]string{"localization/english/a_l_english.yml": "l_english:\n r:0 \"Cost: $VALUE|=+0$\"\n"},
	rel:   "localization/english/a_l_english.yml", at: "VALUE",
	want: []string{"loc value", "$VALUE$"},
}, {
	name:  "comment",
	files: map[string]string{"events/x.txt": "# event should not hover\nns.1 = { }\n"},
	rel:   "events/x.txt", at: "event should", none: true,
}, {
	// `option = { name = x }` is the option's own name, not the macro parameter $name$ some other file declares.
	name: "option name is not a macro parameter",
	files: map[string]string{
		"events/x.txt":                  "namespace = t\nt.1 = {\n\ttype = character_event\n\toption = { name = t.1.a }\n}\n",
		"common/scripted_effects/e.txt": "do_it = { add_gold = $name$ }\n",
	},
	rel: "events/x.txt", at: "name = t.1.a",
	check: func(t *testing.T, h *Inspect) {
		if h != nil && h.Kind == "script param" {
			t.Fatalf("option name stolen as script_param: %#v", h)
		}
	},
}, {
	// A macro parameter's card names its owner and every value callers pass it.
	name: "script param",
	files: map[string]string{
		"common/scripted_triggers/t.txt": "my_trig = { exists = $TARGET$ }\n",
		"events/e.txt": "ev.1 = {\n\tmy_trig = { TARGET = title:c_rouen }\n" +
			"\tmy_trig = { TARGET = scope:adventurer_target }\n\tmy_trig = { TARGET = scope:adventurer_target }\n}\n",
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
