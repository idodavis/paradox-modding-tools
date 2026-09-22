// hover_test.go covers the card for every type of word the reader gives, the words that have none, and signature help.
package lsp

import (
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/schema"
)

func TestHover(t *testing.T) {
	s, dir := fixture(t, "ck3")
	for _, tt := range []struct {
		what, rel, text, needle string
		plus                    int
		typ                     schema.ValueType
		check                   func(c *Inspect) bool
	}{
		{"a definition: its kind, its name, its loc label and its site", "common/traits/m.txt", "bold = {\n\tname = x\n}\n", "bold", 0, schema.TypeRef,
			func(c *Inspect) bool {
				return c.Value.Kind == "traits" && c.Value.Name == "bold" && len(c.Sites) == 1 && c.Sites[0].Origin == "mod" && c.Sites[0].Path != ""
			}},
		{"a mod's twin of an install definition carries both sites, the mod's winning", "events/x.txt", "namespace = v\nv.1 = {\n\ttype = character_event\n}\n", "v.1 =", 0, schema.TypeRef,
			func(c *Inspect) bool {
				return c.Value.Kind == "events" && len(c.Sites) == 2 && c.Sites[0].Origin == "vanilla" && c.Sites[1].Origin == "mod" && c.Winner == 1
			}},
		{"a definition's label is its loc text", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { has_trait = brave }\n}\n", "brave", 0, schema.TypeRef,
			func(c *Inspect) bool { return c.Value.Kind == "traits" && c.Label == "Brave" && len(c.Sites) == 1 }},
		{"the comment above a definition is its docs, read from the file", "events/x.txt", "namespace = m\n# The mod's event.\n# Fires at birth.\nm.1 = { type = character_event }\n", "m.1", 0, schema.TypeRef,
			func(c *Inspect) bool { return c.Docs == "The mod's event.\nFires at birth." }},
		{"a loc key: its text and its site", "events/x.txt", "namespace = m\nm.1 = {\n\ttitle = v.1.t\n}\n", "v.1.t", 0, schema.TypeLoc,
			func(c *Inspect) bool {
				return c.Label == "Hello" && len(c.Sites) == 1 && strings.HasSuffix(c.Sites[0].Rel, "v_l_english.yml") && c.Sites[0].Range.Start.Line == 1
			}},
		{"a structure key: its class, what it accepts, and the game's own prose", "events/x.txt", "namespace = m\nm.1 = {\n\ttype = character_event\n\toption = {\n\t\tname = x\n\t}\n}\n", "option", 0, schema.TypeMember,
			func(c *Inspect) bool { return c.Class == "structure" && c.Docs == "One choice offered to the player." }},
		{"a key that accepts a loc key says so", "events/x.txt", "namespace = m\nm.1 = {\n\tdesc = m.1.desc\n}\n", "desc", 0, schema.TypeMember,
			func(c *Inspect) bool { return len(c.Accepts) > 0 && c.Accepts[0] == "loc" }},
		{"a declared token: its prose, usage, scope and what it accepts", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\tadd_gold = 1\n\t}\n}\n", "add_gold", 0, schema.TypeToken,
			func(c *Inspect) bool {
				return c.Docs == "Adds gold to the scoped character" && c.Value.Declared && c.Scope == "character"
			}},
		{"a body key under a token is an argument", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tset_variable = {\n\t\t\tname = x\n\t\t}\n\t}\n}\n", "name = x", 0, schema.TypeMember,
			func(c *Inspect) bool { return c.Key == "name" }},
		{"a link as a key: from its scope to the one it yields", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n", "capital_county", 0, schema.TypeScopePath,
			func(c *Inspect) bool { return c.Scope == "character" && c.ScopeOut == "landed_title" }},
		{"an iterator: the scope it runs in and the one it hands its block", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\tevery_vassal = {\n\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n", "every_vassal", 0, schema.TypeToken,
			func(c *Inspect) bool { return c.Scope == "character" && c.ScopeOut == "character" }},
		{"a trigger inside a logic word is a trigger, in the scope around it", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\tOR = {\n\t\t\thas_trait = brave\n\t\t}\n\t}\n}\n", "has_trait", 0, schema.TypeToken,
			func(c *Inspect) bool { return c.Docs == "does the character have this trait" && c.Scope == "character" }},
		{"a word under a token nothing defines is the token's slot value", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tadd_character_flag = born_as_taltos\n\t}\n}\n", "born_as_taltos", 0, schema.TypeUnresolved,
			func(c *Inspect) bool { return c.Key == "add_character_flag" && c.Docs == "adds a flag" }},
		{"a numbered branch of a random list is a weight", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\trandom_list = {\n\t\t\t95 = { add_gold = 1 }\n\t\t\t5 = { add_gold = 2 }\n\t\t}\n\t}\n}\n", "95 = {", 0, schema.TypeNumber,
			func(c *Inspect) bool { return c.Weight == 95 }},
		{"a date, written out, and in the mod's calendar", "common/decisions/d.txt", "m_decision = {\n\tstart_date = 8082.1.1\n}\n", "8082.1.1", 0, schema.TypeDate,
			func(c *Inspect) bool { return c.Date == "January 1st, 8082 AD; 300.1.1 AC in this mod's calendar" }},
		{"a quoted date", "common/decisions/d.txt", "m_decision = {\n\tstart_date = \"1066.9.15\"\n}\n", "1066.9.15", 0, schema.TypeDate,
			func(c *Inspect) bool { return strings.HasPrefix(c.Date, "September 15th, 1066 AD") }},
		{"a date as a history key", "history/characters/m.txt", "1000 = {\n\t830.3.22 = {\n\t\tbirth = yes\n\t}\n}\n", "830.3.22", 0, schema.TypeDate,
			func(c *Inspect) bool { return strings.HasPrefix(c.Date, "March 22nd, 830 AD") }},
		{"a character cite as a key: the character, by its label", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tcharacter:186004 = { save_scope_as = x }\n\t}\n}\n", "character:186004", 10, schema.TypeRef,
			func(c *Inspect) bool {
				return c.Value.Kind == "characters" && c.Value.Name == "186004" && c.Label == "Rognvald" && len(c.Sites) == 1 && strings.HasSuffix(c.Sites[0].Rel, "v_characters.txt")
			}},
		{"a character cite as a value", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\texists = character:186004\n\t}\n}\n", "character:186004", 0, schema.TypeRef,
			func(c *Inspect) bool { return c.Value.Kind == "characters" && c.Value.Declared }},
		{"a saved scope: where it was saved, and the scope type there", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tsave_scope_as = mine\n\t\ttitle:c_x = { save_scope_as = near }\n\t}\n\ttrigger = { exists = scope:near }\n}\n", "scope:near", 6, schema.TypeRef,
			func(c *Inspect) bool {
				return c.Value.Kind == "saved_scopes" && len(c.Sites) == 1 && c.ScopeOut == "landed_title"
			}},
		{"a variable: its site", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tset_variable = { name = foo value = 3 }\n\t}\n\ttrigger = { exists = var:foo }\n}\n", "var:foo", 4, schema.TypeRef,
			func(c *Inspect) bool {
				return c.Value.Kind == "variables" && len(c.Sites) == 1 && c.Sites[0].Range.Start.Line == 3
			}},
		{"a macro's parameter: its macro, and the words written for it where the macro is called", "common/scripted_effects/x.txt", "m_effect = {\n\tadd_gold = $AMOUNT$\n}\n", "$AMOUNT$", 1, schema.TypeParam,
			func(c *Inspect) bool { return c.Value.Name == "AMOUNT" && len(c.Sites) == 1 }},
		{"the install's parameter, with the values its callers write", "common/scripted_effects/x.txt", "m_effect = {\n\tv_effect = { AMOUNT = 7 }\n}\n", "AMOUNT", 0, schema.TypeRef,
			func(c *Inspect) bool { return c.Value.Kind == "params" && c.Value.Parent == "v_effect" }},
		{"a macro written as a key: its definition", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tv_effect = { AMOUNT = 5 }\n\t}\n}\n", "v_effect", 0, schema.TypeRef,
			func(c *Inspect) bool {
				return c.Value.Kind == "scripted_effects" && len(c.Sites) == 1 && c.Sites[0].Origin == "vanilla"
			}},
		{"a constant: its value, looked up within the file", "events/x.txt", "@gold_cost = 50\nnamespace = m\nm.1 = {\n\timmediate = {\n\t\tadd_gold = @gold_cost\n\t}\n}\n", "@gold_cost\n", 1, schema.TypeConstant,
			func(c *Inspect) bool { return c.Label == "50" && len(c.Sites) == 1 && c.Sites[0].Range.Start.Line == 0 }},
		{"a colour swatch", "common/traits/m.txt", "bold = {\n\tcolor = rgb { 255 0 0 }\n}\n", "color", 0, schema.TypeUnresolved,
			func(c *Inspect) bool { return c.Color == "#ff0000" }},
		{"a file: where it is, and its header when it is an image", "gui/m.gui", "types m_types = {\n\ttype m_icon = icon {\n\t\ttexture = \"gfx/interface/icons/traits/brave.dds\"\n\t}\n}\n", "gfx/interface", 0, schema.TypeFile,
			func(c *Inspect) bool { return len(c.Sites) == 1 && c.Image != nil && c.Image.Width > 0 }},
	} {
		path := opened(s, dir, tt.rel, tt.text)
		line, col := aim(t, tt.text, tt.needle, tt.plus)
		card, err := Hover(s, path, line, col)
		if err != nil || card == nil {
			t.Errorf("%s: no card (%v)", tt.what, err)
			continue
		}
		if card.Value.Type != tt.typ || !tt.check(card) {
			t.Errorf("%s: %+v", tt.what, *card)
		}
	}

	for _, tt := range []struct{ what, rel, text, needle string }{
		{"a literal", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\tis_adult = yes\n\t}\n}\n", "yes"},
		{"a comment", "events/x.txt", "# event should not hover\nnamespace = m\nm.1 = { }\n", "event should"},
		{"a number that is no branch of a body", "common/traits/m.txt", "bold = {\n\tgenes = {\n\t\t20 = empty\n\t\t80 = empty\n\t}\n}\n", "20 = empty"},
		{"a brace", "events/x.txt", "namespace = m\nm.1 = {\n}\n", "{"},
	} {
		path := opened(s, dir, tt.rel, tt.text)
		line, col := aim(t, tt.text, tt.needle, 0)
		if card, _ := Hover(s, path, line, col); card != nil {
			t.Errorf("%s: a card: %+v", tt.what, *card)
		}
	}

	// a value the schema reads as script is never a loc key, whatever the loc holds
	history := "1000 = {\n\tculture = norse\n}\n"
	path := opened(s, dir, "history/characters/m.txt", history)
	line, col := aim(t, history, "norse", 0)
	if card, _ := Hover(s, path, line, col); card == nil || card.Value.Type == schema.TypeLoc {
		t.Errorf("loc never wins over script: %+v", card)
	}
}

func TestHoverLoc(t *testing.T) {
	s, dir := fixture(t, "ck3")
	text := "l_english:\n m.1.t:0 \"Cost: $VALUE|=+0$ and $v.1.t$\"\n"
	path := opened(s, dir, "localization/english/m_l_english.yml", text)
	for _, tt := range []struct {
		what, needle string
		typ          schema.ValueType
		label        string
	}{
		{"a key at the line's start", "m.1.t", schema.TypeLoc, "Cost: $VALUE|=+0$ and $v.1.t$"},
		{"an engine value the text interpolates", "VALUE", schema.TypeParam, ""},
		{"a loc key the text interpolates", "v.1.t", schema.TypeLoc, "Hello"},
	} {
		line, col := aim(t, text, tt.needle, 0)
		card, _ := Hover(s, path, line, col)
		if card == nil || card.Value.Type != tt.typ || card.Label != tt.label {
			t.Errorf("%s: %+v", tt.what, card)
		}
	}
	if card, _ := Hover(s, path, 0, 2); card != nil {
		t.Error("the language header is no key")
	}
}

func TestSignatureHelp(t *testing.T) {
	s, dir := fixture(t, "ck3")
	text := "namespace = m\nm.1 = {\n\timmediate = {\n\t\ttrigger_event = { id = v.2 }\n\t\thidden_effect = { add_gold = 1 }\n\t}\n}\n"
	path := opened(s, dir, "events/x.txt", text)
	line, col := aim(t, text, "id = v.2", 0)
	if help, _ := SignatureHelp(s, path, line, col); help == nil || help.Label != "trigger_event = { id = X days = Y }" {
		t.Errorf("inside a declared token's block, its usage: %+v", help)
	}
	line, col = aim(t, text, "add_gold = 1", 0)
	if help, _ := SignatureHelp(s, path, line, col); help != nil {
		t.Errorf("a token with no usage: %+v", help)
	}
}
