package schema

import (
	"testing"
)

// found is a Defined over three sets: definitions as kind/name, loc keys, and files.
type found struct{ defs, locs, files map[string]bool }

func (f found) Def(kind, name string) bool { return f.defs[kind+"/"+name] }
func (f found) Loc(key string) bool        { return f.locs[key] }
func (f found) File(name string) bool      { return f.files[name] }

// reader is a Reader over a small game: a few kinds with prefixes, a macro kind, a scripted list, and a vocabulary with one of everything.
func reader() Reader {
	kind := func(name string, prefixes ...string) *Kind { return &Kind{Name: name, Prefixes: prefixes} }
	return Reader{
		Schema: &Schema{Kinds: map[string]*Kind{
			"characters": kind("characters", "character"), "landed_titles": kind("landed_titles", "title"), "traits": kind("traits", "trait"),
			"defines.entry": kind("defines.entry", "define"), "saved_scopes": kind("saved_scopes", "scope"),
			"definitions.area": kind("definitions.area", "geo"), "definitions.region": kind("definitions.region", "geo"),
			"scripted_effects":  {Name: "scripted_effects", Macro: BodyEffect},
			"scripted_triggers": {Name: "scripted_triggers", Macro: BodyTrigger},
			"scripted_lists":    {Name: "scripted_lists", Macro: BodyTrigger | BodyEffect},
		}},
		Vocab: &Vocab{
			Effects:   map[string]EngineToken{"add_gold": {}, "if": {}, "every_vassal": {}},
			Triggers:  map[string]EngineToken{"has_trait": {}, "and": {}, "not": {}, "any_vassal": {}},
			Modifiers: map[string]Modifier{"monthly_income": {}},
			Links: map[string]Link{
				"root": {}, "liege": {Out: "character"}, "culture": {Out: "culture", Prefix: true}, "character": {Prefix: true}, "title": {Prefix: true}, "scope": {Prefix: true}, "flag": {Prefix: true},
			},
		},
		Found: found{
			defs: map[string]bool{
				"characters/186004": true, "characters/168673": true, "landed_titles/k_france": true, "traits/brave": true, "defines.entry/START_DATE": true, "definitions.region/west": true,
				"scripted_effects/my_effect": true, "scripted_triggers/my_trigger": true, "scripted_lists/adult_child": true, "game_rules.setting/on": true, "goods/wheat": true, "cultures/norse": true,
			},
			locs:  map[string]bool{"my_title": true},
			files: map[string]bool{"gfx/x.dds": true, "pattern_solid.dds": true, "music/x.ogg": true},
		},
	}
}

func TestReaderValue(t *testing.T) {
	holder := &Field{Accepts: []Accept{{Type: TypeRef, Kind: "characters"}, {Type: TypeNumber}}}
	culture := &Field{Accepts: []Accept{{Type: TypeRef, Kind: "cultures"}}}
	shown := &Field{Accepts: []Accept{{Type: TypeBool, Declared: true}}}
	style := &Field{Accepts: []Accept{{Type: TypeEnum, Enum: []string{"good", "bad"}}}}
	title := &Field{Accepts: []Accept{{Type: TypeLoc}, {Type: TypeString}}}
	texture := &Field{Accepts: []Accept{{Type: TypeFile}}}
	born := &Field{Accepts: []Accept{{Type: TypeDate}}}
	trait := &Field{Accepts: []Accept{{Type: TypeRef, Kind: "traits", Declared: true}}}

	for _, c := range []struct {
		name   string
		f      *Field
		text   string
		quoted bool
		want   Value
	}{
		// 1. nothing written
		{"nothing written", nil, "", false, Value{}},
		{"an empty quoted word is text", nil, "", true, Value{Type: TypeString}},

		// 2. decided by spelling alone
		{"a constant", trait, "@gold_cost", false, Value{Type: TypeConstant, Name: "gold_cost"}},
		{"an inline expression", nil, "@[ 5 + 2 ]", false, Value{Type: TypeConstant}},
		{"a parameter", trait, "$TRAIT$", false, Value{Type: TypeParam, Name: "TRAIT"}},
		{"a parameter inside a word", nil, "trait_$TRAIT$_x", true, Value{Type: TypeParam, Name: "TRAIT"}},
		{"a data call", nil, "[Character.GetName]", true, Value{Type: TypeCall}},
		{"a file by its own spelling", nil, "file:/music/x.ogg", true, Value{Type: TypeFile, Name: "music/x.ogg"}},

		// 3. a cite
		{"a cite of a declared prefix", nil, "character:186004", false, Value{Type: TypeRef, Kind: "characters", Name: "186004", Declared: true}},
		{"a cite nothing defines keeps its kind", nil, "title:k_nowhere", false, Value{Type: TypeRef, Kind: "landed_titles", Name: "k_nowhere", Declared: true}},
		{"a learned prefix the vocabulary does not declare", nil, "trait:brave", false, Value{Type: TypeRef, Kind: "traits", Name: "brave"}},
		{"a cite with a member names the member", nil, "define:NGame|START_DATE", false, Value{Type: TypeRef, Kind: "defines.entry", Name: "START_DATE", Parent: "NGame"}},
		{"a shared prefix is settled by who has it", nil, "geo:west", false, Value{Type: TypeRef, Kind: "definitions.region", Name: "west"}},
		{"a shared prefix nobody has names the first", nil, "geo:east", false, Value{Type: TypeRef, Kind: "definitions.area", Name: "east"}},
		{"a declared prefix with no kind", nil, "flag:some_flag", false, Value{Type: TypeRef, Name: "some_flag", Declared: true}},
		{"a saved scope", nil, "scope:target", false, Value{Type: TypeRef, Kind: "saved_scopes", Name: "target", Declared: true}},
		{"a sound is a name, never a cite", nil, "event:/SFX/UI/click", true, Value{Type: TypeString}},
		{"an unknown prefix is no cite", nil, "nothing:here", false, Value{Type: TypeString}},

		// 4. the field's accepts, in their order
		{"a number that is a reference", holder, "168673", false, Value{Type: TypeRef, Kind: "characters", Name: "168673"}},
		{"zero is nobody, and stays a number", holder, "0", false, Value{Type: TypeNumber}},
		{"a quoted word read as a bare one", culture, "norse", true, Value{Type: TypeRef, Kind: "cultures", Name: "norse"}},
		{"a declared type", shown, "yes", false, Value{Type: TypeBool, Declared: true}},
		{"a declared reference", trait, "brave", false, Value{Type: TypeRef, Kind: "traits", Name: "brave", Declared: true}},
		{"an enum word", style, "bad", false, Value{Type: TypeEnum}},
		{"a loc key", title, "my_title", false, Value{Type: TypeLoc, Name: "my_title"}},
		{"text where no loc key is", title, "Some Title", true, Value{Type: TypeString}},
		{"a file by path", texture, "gfx/x.dds", true, Value{Type: TypeFile, Name: "gfx/x.dds"}},
		{"a file by name alone", texture, "pattern_solid.dds", true, Value{Type: TypeFile, Name: "pattern_solid.dds"}},
		{"a date", born, "1066.9.15", false, Value{Type: TypeDate}},
		{"a quoted date", born, "1066.9.15", true, Value{Type: TypeDate}},

		// 5. a walk between scopes
		{"a link", nil, "liege", false, Value{Type: TypeScopePath, Declared: true}},
		{"a path of links", trait, "root.liege.primary_title", false, Value{Type: TypeScopePath, Declared: true}},
		{"a cite with a chain", nil, "scope:target.culture", false, Value{Type: TypeScopePath, Declared: true}},
		{"a learned cite with a chain", nil, "trait:brave.opposite", false, Value{Type: TypeScopePath}},
		{"a quoted link is text", nil, "culture", true, Value{Type: TypeString}},
		{"a reference outranks a link", culture, "norse", false, Value{Type: TypeRef, Kind: "cultures", Name: "norse"}},

		// 6. by spelling
		{"a number", nil, "-12.5", false, Value{Type: TypeNumber}},
		{"a quoted number is text", nil, "12", true, Value{Type: TypeString}},
		{"a bare date", nil, "867.1.1", false, Value{Type: TypeDate}},
		{"a bool", nil, "no", false, Value{Type: TypeBool}},
		{"a word the accepts did not hold", shown, "12", false, Value{Type: TypeNumber}},

		// 7. what is left
		{"a quoted sentence", nil, "Hello there", true, Value{Type: TypeString}},
		{"a name nothing claims", trait, "bravee", false, Value{Type: TypeUnresolved, Name: "bravee"}},
		{"an event id nothing claims", nil, "birth.1001", false, Value{Type: TypeUnresolved, Name: "birth.1001"}},
		{"an operator used as a value", nil, ">=", false, Value{Type: TypeString}},
		{"inf is a name", nil, "inf", false, Value{Type: TypeUnresolved, Name: "inf"}},
	} {
		if got := reader().Value(c.f, c.text, c.quoted); got != c.want {
			t.Errorf("%s: Value(%q) = %+v, want %+v", c.name, c.text, got, c.want)
		}
	}
}

func TestReaderKey(t *testing.T) {
	event := &Field{Class: ClassStructure, Members: map[string]*Field{"title": {}, "option": {}, "<date>": {}}}
	effects := &Field{Class: ClassBody, Body: BodyEffect, Members: map[string]*Field{"limit": {}}}
	triggers := &Field{Class: ClassBody, Body: BodyTrigger}
	modifiers := &Field{Class: ClassBody, Body: BodyModifier}
	rule := &Field{Class: ClassStructure, Defines: "game_rules.setting", Members: map[string]*Field{"default": {}}}
	goods := &Field{Class: ClassDatabase, KeyedBy: "goods"}

	for _, c := range []struct {
		name   string
		parent *Field
		text   string
		want   Value
	}{
		// 1. a member
		{"a fixed member", event, "option", Value{Type: TypeMember, Name: "option"}},
		{"a member learned under a body", effects, "limit", Value{Type: TypeMember, Name: "limit"}},

		// 2. by spelling
		{"a number", event, "1", Value{Type: TypeNumber}},
		{"a date", event, "867.1.1", Value{Type: TypeDate}},
		{"a cite", triggers, "character:186004", Value{Type: TypeRef, Kind: "characters", Name: "186004", Declared: true}},
		{"a path", nil, "gui/new_window.gui", Value{Type: TypeFile, Name: "gui/new_window.gui"}},

		// 3. in a body
		{"an effect in an effect body", effects, "add_gold", Value{Type: TypeToken, Name: "add_gold", Declared: true}},
		{"an effect in a trigger body is none", triggers, "add_gold", Value{Type: TypeUnresolved, Name: "add_gold"}},
		{"a logic word, written large", triggers, "AND", Value{Type: TypeToken, Name: "and", Declared: true}},
		{"a modifier", modifiers, "monthly_income", Value{Type: TypeToken, Name: "monthly_income", Declared: true}},
		{"a link", effects, "liege", Value{Type: TypeScopePath, Declared: true}},
		{"a path of links", triggers, "root.liege", Value{Type: TypeScopePath, Declared: true}},
		{"a scripted effect", effects, "my_effect", Value{Type: TypeRef, Kind: "scripted_effects", Name: "my_effect"}},
		{"a scripted effect in a trigger body is none", triggers, "my_effect", Value{Type: TypeUnresolved, Name: "my_effect"}},
		{"a scripted trigger", triggers, "my_trigger", Value{Type: TypeRef, Kind: "scripted_triggers", Name: "my_trigger"}},
		{"a scripted list through an iterator", effects, "every_adult_child", Value{Type: TypeRef, Kind: "scripted_lists", Name: "adult_child"}},
		{"a declared iterator is a token first", effects, "every_vassal", Value{Type: TypeToken, Name: "every_vassal", Declared: true}},
		{"a token is not one outside a body", event, "add_gold", Value{Type: TypeUnresolved, Name: "add_gold"}},

		// 4. a name of the parent's kind
		{"a definition of the nested kind", rule, "on", Value{Type: TypeRef, Kind: "game_rules.setting", Name: "on"}},
		{"a member beside them", rule, "default", Value{Type: TypeMember, Name: "default"}},
		{"a name cited as a key", goods, "wheat", Value{Type: TypeRef, Kind: "goods", Name: "wheat"}},
		{"a name nothing defines", goods, "wheet", Value{Type: TypeUnresolved, Name: "wheet"}},

		// 5. nothing claims it
		{"no parent", nil, "whatever", Value{Type: TypeUnresolved, Name: "whatever"}},
	} {
		if got := reader().Key(c.parent, c.text); got != c.want {
			t.Errorf("%s: Key(%q) = %+v, want %+v", c.name, c.text, got, c.want)
		}
	}
}
