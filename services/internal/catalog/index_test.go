// index_test.go covers ExtractFile harvest and BuildIndex across mods.

package catalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/parser/jomini"
)

func writeMod(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func extractCK3(rel, content, origin string) FileExtract {
	return ExtractFile("ck3", filepath.FromSlash(rel), rel, origin, content, false)
}

func extractCK3Overlay(rel, content, origin string, overlay *VanillaCache) FileExtract {
	return ExtractParsed("ck3", filepath.FromSlash(rel), rel, origin,
		jomini.Parse(jomini.Normalize(content)), false, overlay)
}

// ck3Schema declares the scope types and links these tests rely on, matching
// what CK3's event_scopes.log / event_targets.log actually state. Nested
// databases are only harvested for a declared child kind, so a fixture without
// a schema harvests none.
func ck3Schema() *Schema {
	return &Schema{
		Scopes: map[string]ScopeType{
			"faith": {}, "culture": {}, "trait": {}, "character": {},
		},
		Links: map[string]ScopeLink{
			"title": {Out: "landed_title", Global: true, Data: true},
			"faith": {Out: "faith", Global: true, Data: true},
		},
	}
}

func extractCK3Typed(rel, content, origin string) FileExtract {
	return extractCK3Overlay(rel, content, origin, &VanillaCache{Schema: ck3Schema()})
}

func TestEventTitleSingleLocRef(t *testing.T) {
	body := "namespace = test\n\ntest.1 = {\n\ttitle = k.t\n}\n"
	ex := extractCK3("events/x.txt", body, "mod")
	var kt []Ref
	for _, r := range ex.Refs {
		if r.Key == "k.t" {
			kt = append(kt, r)
		}
	}
	if len(kt) != 1 {
		t.Fatalf("k.t refs=%v", kt)
	}
}

func TestDeriveFireKeysSkipsLocProps(t *testing.T) {
	src := `namespace = ns
ns.1 = {
	title = ns.1
	desc = ns.1.t
	immediate = { trigger_event = ns.2 }
}
ns.2 = { title = ns.2.t }
`
	res := jomini.Parse(jomini.Normalize(src))
	got := resolveFireKinds(deriveFireKeys(res.Root, map[string]bool{
		"ns.1": true, "ns.2": true,
	}, nil))
	if got["title"] != "" || got["desc"] != "" {
		t.Fatalf("loc props voted as fire: %v", got)
	}
	if got["trigger_event"] != "event" {
		t.Fatalf("trigger_event: %v", got)
	}
}

func TestTitleIsNotAnEventRefDespiteFireKey(t *testing.T) {
	ex := extractCK3Overlay("events/x.txt",
		"namespace = ns\nns.1 = {\n\ttitle = ns.1.t\n"+
			"\tdesc = ns.1.councillor_liege_opening\n"+
			"\timmediate = { trigger_event = ns.2 }\n}\n",
		"mod", &VanillaCache{FireKeys: map[string]string{
			"title": "event", "desc": "event", "trigger_event": "event",
		}})
	if hasRef(ex.Refs, "event", "ns.1.t") ||
		hasRef(ex.Refs, "event", "ns.1.councillor_liege_opening") {
		t.Fatalf("loc harvested as event: %v", ex.Refs)
	}
	if !hasRef(ex.Refs, "loc", "ns.1.t") ||
		!hasRef(ex.Refs, "loc", "ns.1.councillor_liege_opening") {
		t.Fatalf("missing loc refs: %v", ex.Refs)
	}
	if !hasRef(ex.Refs, "event", "ns.2") {
		t.Fatalf("missing fire: %v", ex.Refs)
	}
}

func hasRef(refs []Ref, kind, key string) bool {
	for _, r := range refs {
		if r.Kind == kind && r.Key == key {
			return true
		}
	}
	return false
}

func hasEdge(edges []Edge, from, to, kind string) bool {
	for _, e := range edges {
		if e.From == from && e.To == to && (kind == "" || e.Kind == kind || e.Via == kind) {
			return true
		}
	}
	return false
}

func TestExtractFile(t *testing.T) {
	eventSrc := `namespace = test
test.1 = {
	title = test.1.t
	immediate = { trigger_event = test.2 }
	option = { name = test.1.a
		trigger_event = test.3
	}
	events = { test.4 }
}
`
	titleArg := "test.1 = {\n\timmediate = { TITLE = primary_title }\n\ttitle = test.1.t\n}\n"
	saved := `test.1 = {
	immediate = {
		save_scope_as = duel_target
		exists = scope:duel_target
		save_scope_value_as = { name = gold_amt }
	}
	title = test.1.t
}
`
	t.Run("event loc and fire", func(t *testing.T) {
		ex := extractCK3("events/x.txt", eventSrc, "modA")
		if !findDef(ex.Defs, "event", "test.1") || ex.Defs[0].Origin != "modA" {
			t.Fatalf("defs=%v", ex.Defs)
		}
		if !hasRef(ex.Refs, "loc", "test.1.t") || !hasRef(ex.Refs, "event", "test.2") {
			t.Fatalf("refs=%v", ex.Refs)
		}
		if hasRef(ex.Refs, "event", "test.1.t") {
			t.Fatalf("title loc harvested as event: %v", ex.Refs)
		}
		if !hasEdge(ex.Edges, "test.1", "test.2", "trigger_event") {
			t.Fatalf("edges=%v", ex.Edges)
		}
		if !hasEdge(ex.Edges, "test.1", "test.3", "option") ||
			!hasEdge(ex.Edges, "test.1", "test.4", "events") {
			t.Fatalf("container edges=%v", ex.Edges)
		}
	})
	t.Run("TITLE arg is not loc", func(t *testing.T) {
		refs := extractCK3("events/x.txt", titleArg, "modA").Refs
		if hasRef(refs, "loc", "primary_title") {
			t.Fatalf("TITLE = primary_title should not be loc: %v", refs)
		}
		if !hasRef(refs, "loc", "test.1.t") {
			t.Fatalf("missing loc ref: %v", refs)
		}
	})
	t.Run("saved scope indexed", func(t *testing.T) {
		ex := extractCK3("events/x.txt", saved, "modA")
		if !findDef(ex.Defs, "saved_scope", "duel_target") {
			t.Fatalf("missing save_scope_as def: %v", ex.Defs)
		}
		if !findDef(ex.Defs, "saved_scope", "gold_amt") {
			t.Fatalf("missing save_scope_value_as def: %v", ex.Defs)
		}
		if !hasRef(ex.Refs, "saved_scope", "duel_target") {
			t.Fatalf("missing scope: ref: %v", ex.Refs)
		}
		if !hasRef(ex.Refs, "loc", "test.1.t") {
			t.Fatalf("missing loc ref: %v", ex.Refs)
		}
	})
	t.Run("variable and flag prefix", func(t *testing.T) {
		src := `test.1 = {
	immediate = {
		add_character_flag = used_life
		set_variable = { name = foo value = 3 }
		has_variable = foo
		exists = var:foo
		exists = flag:used_life
	}
}
`
		ex := extractCK3("events/x.txt", src, "modA")
		if findDef(ex.Defs, "character_flag", "used_life") ||
			findDef(ex.Defs, "flag", "used_life") {
			t.Fatalf("add_character_flag must not harvest a def: %v", ex.Defs)
		}
		if !hasRef(ex.Refs, "flag", "used_life") {
			t.Fatalf("missing flag: ref: %v", ex.Refs)
		}
		if !findDef(ex.Defs, "var", "foo") {
			t.Fatalf("missing var def: %v", ex.Defs)
		}
		for _, d := range ex.Defs {
			if d.Kind == "var" && d.Key == "foo" && d.Value != "3" {
				t.Fatalf("var Value=%q want 3", d.Value)
			}
		}
		if !hasRef(ex.Refs, "var", "foo") {
			t.Fatalf("missing var ref: %v", ex.Refs)
		}
	})
	t.Run("add_character_flag is not ScriptName", func(t *testing.T) {
		src := "e = { immediate = { add_character_flag = x set_variable = y } }\n"
		ex := ExtractFile("vic3", "events/x.txt", "events/x.txt", "m", src, false)
		for _, d := range ex.Defs {
			if d.Kind == "character_flag" || d.Kind == "flag" {
				t.Fatalf("must not harvest flag from add_character_flag: %+v", d)
			}
		}
		if !findDef(ex.Defs, "var", "y") {
			t.Fatalf("must still harvest var: %v", ex.Defs)
		}
	})
	t.Run("coa and flag_definition kinds", func(t *testing.T) {
		coa := ExtractFile("ck3", "common/coat_of_arms/coat_of_arms/x.txt",
			"common/coat_of_arms/coat_of_arms/x.txt", "m",
			"b_appleby = { pattern = \"pattern_solid.dds\" }\n", false)
		if !findDef(coa.Defs, "coat_of_arms", "b_appleby") {
			t.Fatalf("coa defs=%v", coa.Defs)
		}
		fd := ExtractFile("vic3", "common/flag_definitions/00.txt",
			"common/flag_definitions/00.txt", "m",
			"ENG = { flag_definition = { } }\n", false)
		if !findDef(fd.Defs, "flag_definition", "ENG") &&
			!findDef(fd.Defs, "flag_definitions", "ENG") {
			t.Fatalf("flag_definitions defs=%v", fd.Defs)
		}
	})
	t.Run("event namespace defs and refs", func(t *testing.T) {
		src := `namespace = a
a.1 = { type = character_event }
namespace = b
b.1 = { type = character_event }
`
		ex := extractCK3("events/x.txt", src, "modA")
		if !findDef(ex.Defs, "namespace", "a") || !findDef(ex.Defs, "namespace", "b") {
			t.Fatalf("ns defs=%v", ex.Defs)
		}
		if !findDef(ex.Defs, "event", "a.1") || !findDef(ex.Defs, "event", "b.1") {
			t.Fatalf("event defs=%v", ex.Defs)
		}
		if !hasRef(ex.Refs, "namespace", "a") || !hasRef(ex.Refs, "namespace", "b") {
			t.Fatalf("ns refs=%v", ex.Refs)
		}
	})
	t.Run("on_action file skips namespace key", func(t *testing.T) {
		ex := extractCK3("common/on_actions/x.txt",
			"namespace = foo\nfoo = { events = { bar.1 } }\n", "modA")
		if findDef(ex.Defs, "namespace", "foo") {
			t.Fatalf("on_action harvested namespace: %v", ex.Defs)
		}
		if findDef(ex.Defs, "on_action", "namespace") {
			t.Fatalf("on_action named namespace: %v", ex.Defs)
		}
		if !findDef(ex.Defs, "on_action", "foo") {
			t.Fatalf("want on_action foo: %v", ex.Defs)
		}
	})
	t.Run("on_action container", func(t *testing.T) {
		oa := extractCK3("common/on_action/x.txt", "on_birth = {\n\tevents = { test.5 }\n}\n", "modA")
		if !hasEdge(oa.Edges, "on_birth", "test.5", "events") {
			t.Fatalf("edges=%v", oa.Edges)
		}
	})
	t.Run("call edges only for effects", func(t *testing.T) {
		content := `test.1 = {
	immediate = {
		real_effect = yes
		not_an_effect = yes
	}
}
real_effect = {
	trigger_event = test.2
}
`
		rel := "common/scripted_effects/e.txt"
		ex := extractCK3(rel, content, "modA")
		edges := append(append([]Edge{}, ex.Edges...), ApplyCallEdges(ex.Cands, macroDefKeys(ex.Defs, nil))...)
		var call, fake, fire bool
		for _, e := range edges {
			call = call || (e.Kind == EdgeKindCall && e.From == "test.1" && e.To == "real_effect")
			fake = fake || (e.Kind == EdgeKindCall && e.To == "not_an_effect")
			fire = fire || (e.From == "real_effect" && e.To == "test.2")
		}
		if !call || fake || !fire {
			t.Fatalf("call=%v fake=%v fire=%v edges=%v", call, fake, fire, edges)
		}
		refs := ApplyCallRefs(ex.Cands, macroDefKinds(ex.Defs, nil))
		if !hasRef(refs, "scripted_effects", "real_effect") {
			t.Fatalf("call refs=%v want real_effect", refs)
		}
		if hasRef(refs, "scripted_effects", "not_an_effect") {
			t.Fatalf("engine-like key must not be a call ref: %v", refs)
		}
		for _, e := range ex.Edges {
			if e.Kind == EdgeKindCall {
				t.Fatalf("nil effectSet should omit calls: %v", ex.Edges)
			}
		}
	})
	t.Run("message title is loc not convention id", func(t *testing.T) {
		ex := extractCK3("common/messages/x.txt",
			"quieter_events_neutral = {\n\ttitle = event_message_title\n}\n", "mod")
		if hasRef(ex.Refs, "loc", "quieter_events_neutral") ||
			hasRef(ex.Refs, "loc-convention", "quieter_events_neutral") {
			t.Fatalf("invented message loc: %v", ex.Refs)
		}
		if !hasRef(ex.Refs, "loc", "event_message_title") {
			t.Fatalf("missing title loc: %v", ex.Refs)
		}
	})
	t.Run("typed prefix and noisy RHS", func(t *testing.T) {
		src := `e.1 = {
	immediate = {
		culture = english
		culture = culture:english
		culture = capital_province.culture
		coat_of_arms = $COA$
		exists = culture:danish
	}
}
`
		ex := extractCK3Overlay("events/x.txt", src, "mod", &VanillaCache{
			FieldValueKinds: map[string]string{"culture": "culture"},
		})
		if !hasRef(ex.Refs, "culture", "english") {
			t.Fatalf("bare culture = english: %v", ex.Refs)
		}
		if hasRef(ex.Refs, "culture", "culture:english") {
			t.Fatalf("typed cite stored raw: %v", ex.Refs)
		}
		var english int
		for _, r := range ex.Refs {
			if r.Kind == "culture" && r.Key == "english" {
				english++
			}
		}
		if english != 2 {
			t.Fatalf("want two english culture refs, got %d: %v", english, ex.Refs)
		}
		if hasRef(ex.Refs, "culture", "capital_province.culture") {
			t.Fatalf("dotted link harvested: %v", ex.Refs)
		}
		if hasRef(ex.Refs, "coat_of_arms", "$COA$") {
			t.Fatalf("$COA$ harvested: %v", ex.Refs)
		}
		if !hasRef(ex.Refs, "culture", "danish") {
			t.Fatalf("exists = culture:danish: %v", ex.Refs)
		}
	})
	t.Run("nested faiths and target_titles", func(t *testing.T) {
		rel := extractCK3Typed("common/religion/religion_types/r.txt",
			"christianity_religion = {\n\tfaiths = {\n\t\tcatholic = { color = { 1 1 1 } exists = faith:catholic }\n\t}\n}\n",
			"mod")
		if !findDef(rel.Defs, "religion_types", "christianity_religion") ||
			!findDef(rel.Defs, "faith", "catholic") {
			t.Fatalf("religion tree: %v", rel.Defs)
		}
		if findDef(rel.Defs, "faith", "color") {
			t.Fatal("color must not be a faith")
		}
		oldRel := extractCK3Typed("common/religion/religions/r.txt",
			"christianity_religion = {\n\tfaiths = {\n\t\torthodox = { color = { 1 1 1 } exists = faith:orthodox }\n\t}\n}\n",
			"mod")
		if !findDef(oldRel.Defs, "faith", "orthodox") {
			t.Fatalf("legacy religions folder: %v", oldRel.Defs)
		}
		ex := extractCK3Overlay("events/x.txt",
			"e.1 = { immediate = { faith = catholic faith = faith:orthodox } }\n",
			"mod", &VanillaCache{FieldValueKinds: map[string]string{"faith": "faith"}})
		if !hasRef(ex.Refs, "faith", "catholic") || !hasRef(ex.Refs, "faith", "orthodox") {
			t.Fatalf("faith cites: %v", ex.Refs)
		}
		if hasRef(ex.Refs, "faith", "faith:orthodox") {
			t.Fatalf("typed faith stored raw: %v", ex.Refs)
		}
		cb := extractCK3("common/casus_belli_types/c.txt",
			"my_cb = { titles = target_titles titles = titles:e_hre "+
				"war_name = \"HRE_WAR\" cb_name = \"HRE_CB\" }\n",
			"mod")
		if hasRef(cb.Refs, "title", "target_titles") || hasRef(cb.Refs, "titles", "target_titles") {
			t.Fatalf("target_titles harvested: %v", cb.Refs)
		}
		if !hasRef(cb.Refs, "titles", "e_hre") {
			t.Fatalf("missing title cite: %v", cb.Refs)
		}
		if !hasRef(cb.Refs, "loc", "HRE_WAR") {
			t.Fatalf("war_name not loc: %v", cb.Refs)
		}
		if !hasRef(cb.Refs, "loc", "HRE_CB") {
			t.Fatalf("cb_name not loc: %v", cb.Refs)
		}
	})
	t.Run("titles prefix", func(t *testing.T) {
		ex := extractCK3("events/x.txt",
			"e.1 = { immediate = { exists = titles:e_x } }\n", "mod")
		if hasRef(ex.Refs, "title", "titles:e_x") {
			t.Fatalf("typed title cite stored raw: %v", ex.Refs)
		}
		if !hasRef(ex.Refs, "titles", "e_x") {
			t.Fatalf("want titles:e_x as titles id: %v", ex.Refs)
		}
		tree := extractCK3Typed("common/landed_titles/t.txt",
			"e_x = {\n\tcolor = { 1 2 3 }\n\texists = title:k_x\n\tk_x = {\n\t\tc_x = { exists = title:c_x }\n\t}\n}\n",
			"mod")
		if !findDef(tree.Defs, "landed_titles", "e_x") ||
			!findDef(tree.Defs, "title", "k_x") ||
			!findDef(tree.Defs, "title", "c_x") {
			t.Fatalf("nested titles: %v", tree.Defs)
		}
		if findDef(tree.Defs, "title", "color") {
			t.Fatal("color must not be a title def")
		}
	})
	t.Run("script_value skips formula RHS", func(t *testing.T) {
		ex := extractCK3("events/x.txt",
			"e.1 = { immediate = {\n"+
				"\tset_variable = { name = x value = scope:foo }\n"+
				"\tvalue = script_value:my_val\n"+
				"\tvalue = current_military_strength\n"+
				"\tvalue = tier\n"+
				"} }\n",
			"mod")
		if hasRef(ex.Refs, "script_value", "scope:foo") ||
			hasRef(ex.Refs, "script_value", "x") ||
			hasRef(ex.Refs, "script_value", "tier") ||
			hasRef(ex.Refs, "script_value", "current_military_strength") {
			t.Fatalf("noisy script_value refs: %v", ex.Refs)
		}
		if !hasRef(ex.Refs, "script_value", "my_val") {
			t.Fatalf("script_value:my_val: %v", ex.Refs)
		}
	})
	// `game_rule_setting:` parses as a typed cite only because the lexer accepts
	// any `word:word`; CK3 declares no such scope type or link. Harvesting a
	// nested database on that basis is what minted 342,138 phantom defs across
	// kinds like `SKILL` and `$SKILL$`, so an undeclared child kind is not
	// harvested — and on the real CK3 install this shape produced zero defs
	// anyway, so nothing real is lost.
	t.Run("undeclared child kind is not a nested database", func(t *testing.T) {
		ex := extractCK3Typed("common/game_rules/x.txt",
			"my_rule = {\n\tdefault = a\n\tsuf_quieter = { }\n\texists = game_rule_setting:suf_quieter\n}\n", "mod")
		if findDef(ex.Defs, "game_rule_setting", "suf_quieter") {
			t.Fatalf("undeclared child kind harvested: %v", ex.Defs)
		}
		if !findDef(ex.Defs, "game_rules", "my_rule") {
			t.Fatalf("the rule itself must still be a def: %v", ex.Defs)
		}
	})
	t.Run("decision convention keys", func(t *testing.T) {
		id := "ai_mogyer_adopt_christianity"
		ex := extractCK3Overlay("common/decisions/x.txt",
			id+" = {\n\tselection_tooltip = "+id+"_tooltip\n}\n",
			"mod", &VanillaCache{LocAffixes: map[string][]LocAffix{
				"decisions": {{Defs: 10, Of: 10}, {Suf: "_desc", Defs: 10, Of: 10}},
			}})
		if !hasRef(ex.Refs, "loc-convention", id) ||
			!hasRef(ex.Refs, "loc-convention", id+"_desc") {
			t.Fatalf("missing decision convention loc: %v", ex.Refs)
		}
		if hasRef(ex.Refs, "loc-convention", id+"_confirm") {
			t.Fatalf("encyclopedia extra convention: %v", ex.Refs)
		}
		if !hasRef(ex.Refs, "loc", "ai_mogyer_adopt_christianity_tooltip") {
			t.Fatalf("selection_tooltip not harvested: %v", ex.Refs)
		}
	})
	t.Run("game_rules convention keys", func(t *testing.T) {
		ex := extractCK3Overlay("common/game_rules/x.txt",
			"my_rule = {\n\tdefault = a\n\ta = { }\n}\n", "mod",
			&VanillaCache{LocAffixes: map[string][]LocAffix{
				"game_rules": {{Pre: "game_rule_", Defs: 10, Of: 10}},
			}})
		if hasRef(ex.Refs, "loc", "my_rule") || hasRef(ex.Refs, "loc-convention", "my_rule") {
			t.Fatalf("bare rule id: %v", ex.Refs)
		}
		if !hasRef(ex.Refs, "loc-convention", "game_rules_my_rule") &&
			!hasRef(ex.Refs, "loc-convention", "game_rule_my_rule") {
			t.Fatalf("missing kind_id convention: %v", ex.Refs)
		}
		if hasRef(ex.Refs, "loc-convention", "setting_a") {
			t.Fatalf("encyclopedia setting_ key: %v", ex.Refs)
		}
	})
	t.Run("trigger localization slots are loc refs", func(t *testing.T) {
		ex := extractCK3("common/trigger_localization/x.txt",
			"is_adult = {\n\tglobal = IS_ADULT_TRIGGER\n\tfirst = I_AM_ADULT_TRIGGER\n"+
				"\tthird = THEY_ARE_ADULT_TRIGGER\n\tnone = yes\n}\n",
			"mod")
		if !hasRef(ex.Refs, "loc", "IS_ADULT_TRIGGER") ||
			!hasRef(ex.Refs, "loc", "I_AM_ADULT_TRIGGER") ||
			!hasRef(ex.Refs, "loc", "THEY_ARE_ADULT_TRIGGER") {
			t.Fatalf("missing trigger loc refs: %v", ex.Refs)
		}
		if hasRef(ex.Refs, "loc", "yes") {
			t.Fatalf("none = yes harvested: %v", ex.Refs)
		}
	})
	t.Run("effect localization slots are loc refs", func(t *testing.T) {
		ex := extractCK3("common/effect_localization/x.txt",
			"add_gold = {\n\tfirst = I_GAIN_GOLD_EFFECT\n\tfirst_past = I_GAINED_GOLD_EFFECT\n}\n",
			"mod")
		if !hasRef(ex.Refs, "loc", "I_GAIN_GOLD_EFFECT") ||
			!hasRef(ex.Refs, "loc", "I_GAINED_GOLD_EFFECT") {
			t.Fatalf("missing effect loc refs: %v", ex.Refs)
		}
	})
	// Still harvested, so the key counts as used and hovers with its text —
	// but broad, not strict. Customizable localization completes the stem at
	// runtime (`localization_key = CustomLoc_BR_male_`), and demanding those
	// produced 46,481 false "missing localization" findings on EU5 vanilla
	// alone. See TestLocalizationKeyIsBroadNotStrict.
	t.Run("customizable localization_key is a broad loc ref", func(t *testing.T) {
		ex := extractCK3("common/customizable_localization/x.txt",
			"LifestyleFocus = {\n\ttext = {\n\t\tlocalization_key = LifestyleFocus_martial\n\t}\n}\n",
			"mod")
		if !hasRef(ex.Refs, "loc-broad", "LifestyleFocus_martial") {
			t.Fatalf("missing localization_key ref: %v", ex.Refs)
		}
		if hasRef(ex.Refs, "loc", "LifestyleFocus_martial") {
			t.Fatalf("localization_key must not demand a key: %v", ex.Refs)
		}
	})
	t.Run("loc file interpolations", func(t *testing.T) {
		bom := "\ufeffl_english:\n used:0 \"Hi\"\n other:0 \"see $used$ and $used|U$\"\n" +
			" war:0 \"$ORDER$ $INDEPENDENCE_WAR_NAME$\"\n"
		ex := extractCK3("localization/english/a_l_english.yml", bom, "mod")
		if !findDef(ex.Defs, "loc_key", "used") || !findDef(ex.Defs, "loc_key", "other") {
			t.Fatalf("defs=%v", ex.Defs)
		}
		if !hasRef(ex.Refs, "loc", "used") {
			t.Fatalf("missing $used$ ref: %v", ex.Refs)
		}
		if hasRef(ex.Refs, "loc", "ORDER") {
			t.Fatalf("$ORDER$ harvested: %v", ex.Refs)
		}
		if !hasRef(ex.Refs, "loc", "INDEPENDENCE_WAR_NAME") {
			t.Fatalf("missing $INDEPENDENCE_WAR_NAME$ ref: %v", ex.Refs)
		}
	})
}

func TestBuildIndex(t *testing.T) {
	parent, child := t.TempDir(), t.TempDir()
	writeMod(t, parent, "common/traits/00.txt", "brave = { category = personality }\n")
	writeMod(t, child, "common/traits/01.txt", "craven = { category = personality }\n")
	cache := &VanillaCache{Defs: []Def{{Kind: "trait", Key: "vanilla_trait", Path: "v.txt"}}}
	idx, err := BuildIndex(context.Background(), "ck3", []ModInput{
		{Origin: "parent", Root: parent, Order: 0},
		{Origin: "child", Root: child, Order: 1},
	}, cache)
	if err != nil {
		t.Fatal(err)
	}
	var braveOrigin string
	for _, d := range idx.Defs {
		if d.Key == "brave" {
			braveOrigin = d.Origin
		}
	}
	if braveOrigin != "parent" {
		t.Errorf("brave origin = %q, want parent", braveOrigin)
	}
	if !findDef(cache.Defs, "trait", "vanilla_trait") {
		t.Error("missing vanilla_trait in cache defs")
	}

	root := t.TempDir()
	writeMod(t, root, "localization/english/events.yml", "l_english:\n test.1.t:0 \"Hello\"\n")
	locIdx, err := BuildIndex(context.Background(), "ck3", []ModInput{{Origin: "mod", Root: root, Order: 0}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := locIdx.Loc["english"]["test.1.t"].Value; got != "Hello" {
		t.Fatalf("Loc from header = %v", locIdx.Loc)
	}
}

func TestCK3InstallReligionTypesFaiths(t *testing.T) {
	const rel = "common/religion/religion_types/00_christianity.txt"
	p := filepath.Join(`C:\Program Files (x86)\Steam\steamapps\common\Crusader Kings III`,
		"game", filepath.FromSlash(rel))
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Skip(err)
	}
	text, _ := jomini.Decode(raw)
	ex := ExtractParsed("ck3", p, rel, "", jomini.Parse(text), false, &VanillaCache{
		NestedShapes: []NestedShape{{
			ParentKind: "religion_types", ChildKind: "faith", GroupKey: "faiths",
		}},
	})
	if !findDef(ex.Defs, "faith", "catholic") {
		t.Fatalf("catholic missing from religion_types (%d defs)", len(ex.Defs))
	}
}

// A nested database is only harvested when the game declares its child kind as
// a type. `faith` is a CK3 scope type, so religions yield faiths; `SKILL` is a
// script-parameter name that happens to repeat inside event blocks, and
// harvesting it minted 51,674 defs of a kind that does not exist.
func TestDeriveNestedShapesNeedsDeclaredChild(t *testing.T) {
	tree := jomini.Parse(jomini.Normalize(
		"christianity_religion = {\n\tfaiths = {\n\t\tcatholic = { color = { 1 1 1 } }\n\t}\n}\n"))
	cite := jomini.Parse(jomini.Normalize(
		"e.1 = { immediate = { exists = faith:catholic } }\n"))
	files := []parsedScript{
		{f: fileRef{rel: "common/religion/religion_types/r.txt"}, res: tree},
		{f: fileRef{rel: "events/x.txt"}, res: cite},
	}
	defs := []Def{{Kind: "religion_types", Key: "christianity_religion"}}
	schema := &Schema{Scopes: map[string]ScopeType{"faith": {}}}

	got := deriveNestedShapes("ck3", corpus{gameID: "ck3", inline: files}, defs, defKeySet(defs), nil, schema, citesOf(files), nil)
	if len(got) != 1 || got[0].ParentKind != "religion_types" ||
		got[0].ChildKind != "faith" || got[0].GroupKey != "faiths" {
		t.Fatalf("declared child kind should be harvested; shapes=%+v", got)
	}
	if got := deriveNestedShapes("ck3", corpus{gameID: "ck3", inline: files}, defs, defKeySet(defs), nil, &Schema{}, citesOf(files), nil); len(got) != 0 {
		t.Errorf("undeclared child kind harvested anyway: %+v", got)
	}
	if got := deriveNestedShapes("ck3", corpus{gameID: "ck3", inline: files}, defs, defKeySet(defs), nil, nil, citesOf(files), nil); len(got) != 0 {
		t.Errorf("no schema must yield no nested databases: %+v", got)
	}
}

// citesOf collects the typed cites the shared derive walk would gather.
func citesOf(files []parsedScript) []typedCite {
	var out []typedCite
	for _, f := range files {
		out = append(out, collectTypedCites(f.res.Root)...)
	}
	return out
}

// A fire key needs coverage too, but only once there is enough evidence to read
// it. For fire keys a low hit rate is often the defect worth reporting — a mod
// whose trigger_event targets mostly do not exist — so coverage may veto a key
// only when it carries at least minFireNames names. Without the floor, Vic3 keys
// like `alert` became on_action fire sites and produced 4,033 false dangling
// rows on one real mod.
func TestResolveFireKindsCoverage(t *testing.T) {
	events := map[string]bool{"ns.1": true}
	// Many names, one hit: not a fire key.
	noisy := &fireCount{kinds: map[string]int{"event": 1}, named: 40}
	if got := resolveFireKinds(map[string]*fireCount{"alert": noisy}); got["alert"] != "" {
		t.Errorf("1 of 40 names typed a fire key: %v", got)
	}
	// Few names: coverage is noise, so unanimity still decides. This is what
	// keeps a small mod's broken trigger_event targets reportable.
	thin := &fireCount{kinds: map[string]int{"event": 1}, named: 3}
	if got := resolveFireKinds(map[string]*fireCount{"trigger_event": thin}); got["trigger_event"] != "event" {
		t.Errorf("small sample must fall back to unanimity: %v", got)
	}
	// Real fire key on real evidence.
	solid := &fireCount{kinds: map[string]int{"event": 30}, named: 32}
	if got := resolveFireKinds(map[string]*fireCount{"trigger_event": solid}); got["trigger_event"] != "event" {
		t.Errorf("30 of 32 must type: %v", got)
	}
	_ = events
}

// A list member is a value, not an assignment, so the field-RHS harvest never
// saw one — and localization keys hide there in quantity. CK3 writes a culture's
// given names and its cadet dynasty names as lists, both are keys
// (`Abbas:0 "Abbas"`, `dynn_Rasulid:0 "Rasulid"`), and nothing cited them: 6,978
// of A Game of Thrones' 20,196 orphaned English keys.
func TestListMembersAreLocRefs(t *testing.T) {
	const body = "name_list_x = {\n" +
		"\tmale_names = { Abbas Ali Ibrahim }\n" +
		"\tcadet_dynasty_names = { \"dynn_Rasulid\" \"dynn_Umarid\" }\n" +
		"\tsome_other_list = { not_a_key also_not }\n}\n"
	cache := &VanillaCache{LocListFields: []string{
		"male_names", "cadet_dynasty_names",
	}}
	ex := extractCK3Overlay("common/culture/name_lists/x.txt", body, "mod", cache)
	for _, want := range []string{"Abbas", "Ali", "Ibrahim", "dynn_Rasulid", "dynn_Umarid"} {
		if !hasRef(ex.Refs, "loc-broad", want) {
			t.Errorf("missing list loc ref %q: %v", want, ex.Refs)
		}
	}
	// Quoting says nothing either way — vanilla quotes cadet dynasty names and
	// not given names — but a list no evidence elected contributes nothing.
	for _, no := range []string{"not_a_key", "also_not"} {
		if hasRef(ex.Refs, "loc-broad", no) {
			t.Errorf("undeclared list contributed %q: %v", no, ex.Refs)
		}
	}
	// Broad, never strict: a name the author has not localized is not a defect.
	for _, no := range []string{"Abbas", "dynn_Rasulid"} {
		if hasRef(ex.Refs, "loc", no) {
			t.Errorf("%q must not demand a key: %v", no, ex.Refs)
		}
	}
}

// covers Winner (LIOS/FIOS) and Contests overlay vs conflict.
func TestWinner(t *testing.T) {
	tests := []struct {
		name   string
		gameID string
		defs   []Def
		order  map[string]int
		origin string
	}{
		{
			"LIOS last mod", "",
			[]Def{
				{Kind: "trait", Key: "brave", Origin: "a"},
				{Kind: "trait", Key: "brave", Origin: "b"},
				{Kind: "trait", Key: "brave", Origin: ""},
			},
			map[string]int{"a": 0, "b": 1}, "b",
		},
		{
			"FIOS gui", "ck3",
			[]Def{
				{Kind: "gui_type", Key: "widget", Origin: "b"},
				{Kind: "gui_type", Key: "widget", Origin: "a"},
			},
			map[string]int{"a": 0, "b": 1}, "a",
		},
		{
			"vanilla only", "",
			[]Def{{Kind: "trait", Key: "x", Origin: ""}},
			map[string]int{}, "",
		},
		{
			"vic3 event FIOS", "vic3",
			[]Def{
				{Kind: "event", Key: "e.1", Origin: "b"},
				{Kind: "event", Key: "e.1", Origin: "a"},
			},
			map[string]int{"a": 0, "b": 1}, "a",
		},
		{
			"eu5 gui_type LIOS", "eu5",
			[]Def{
				{Kind: "gui_type", Key: "widget", Origin: "a"},
				{Kind: "gui_type", Key: "widget", Origin: "b"},
			},
			map[string]int{"a": 0, "b": 1}, "b",
		},
		{
			"ck3 event LIOS", "ck3",
			[]Def{
				{Kind: "event", Key: "e.1", Origin: "a"},
				{Kind: "event", Key: "e.1", Origin: "b"},
			},
			map[string]int{"a": 0, "b": 1}, "b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := Winner(tt.gameID, tt.defs, tt.order)
			if w == nil || w.Origin != tt.origin {
				t.Errorf("winner = %v, want origin %q", w, tt.origin)
			}
		})
	}
}

// Session.Resolve buckets by key alone, so a bucket can span kinds. FIOS/LIOS is
// a per-kind rule, and Winner used to read it from mods[0] — so the same three
// defs in a different slice order gave a different winner. Ordering inside the
// bucket comes from map iteration and index build order, so that was a live
// nondeterminism, not a hypothetical one.
func TestWinnerMixedKindBucketIsOrderIndependent(t *testing.T) {
	// vic3: event is FIOS, decision is LIOS.
	defs := []Def{
		{Kind: "decision", Key: "x", Origin: "a"},
		{Kind: "event", Key: "x", Origin: "b"},
		{Kind: "event", Key: "x", Origin: "c"},
	}
	order := map[string]int{"a": 0, "b": 1, "c": 2}
	// event reduces under FIOS -> b, not c. decision has only a. Across kinds
	// there is no engine rule, so the later-loaded of the two wins: b over a.
	const wantKind, wantOrigin = "event", "b"

	perms := [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	for _, p := range perms {
		in := []Def{defs[p[0]], defs[p[1]], defs[p[2]]}
		w := Winner("vic3", in, order)
		if w == nil {
			t.Fatalf("%v: no winner", p)
		}
		if w.Kind != wantKind || w.Origin != wantOrigin {
			t.Errorf("%v: winner = %s/%s, want %s/%s",
				p, w.Kind, w.Origin, wantKind, wantOrigin)
		}
	}
}

func TestContests(t *testing.T) {
	vanilla := []Def{{Kind: "trait", Key: "brave", Origin: "", Path: "vanilla.txt", Line: 9}}
	modPair := []Def{
		{Kind: "trait", Key: "brave", Origin: "a", Path: "a.txt", Line: 1},
		{Kind: "trait", Key: "brave", Origin: "b", Path: "b.txt", Line: 2},
	}
	tests := []struct {
		name    string
		gameID  string
		defs    []Def
		cache   []Def
		order   []string
		n       int
		overlay bool
		winner  string
		mods    int
	}{
		{"mod vs mod LIOS", "", modPair, nil, []string{"a", "b"}, 1, false, "b", 2},
		{"vanilla shadow conflict", "", modPair, vanilla, []string{"a", "b"}, 1, false, "b", 2},
		{"vanilla overlay only", "",
			[]Def{{Kind: "trait", Key: "brave", Origin: "a", Path: "a.txt", Line: 1}},
			vanilla, []string{"a"}, 1, true, "", 1},
		{
			"skip loc and isolated kinds", "",
			[]Def{
				{Kind: "loc_key", Key: "brave", Origin: "a", Path: "a.yml"},
				{Kind: "loc_key", Key: "brave", Origin: "b", Path: "b.yml"},
				{Kind: "mod_descriptor", Key: "name", Origin: "a", Path: "a.mod"},
				{Kind: "mod_descriptor", Key: "name", Origin: "b", Path: "b.mod"},
				{Kind: "trait", Key: "same", Origin: "a", Path: "t.txt"},
				{Kind: "event", Key: "same", Origin: "b", Path: "e.txt"},
			},
			nil, []string{"a", "b"}, 0, false, "", 0,
		},
		{
			"skip shared namespace", "",
			[]Def{
				{Kind: "namespace", Key: "ns", Origin: "a", Path: "a.txt"},
				{Kind: "namespace", Key: "ns", Origin: "b", Path: "b.txt"},
			},
			[]Def{{Kind: "namespace", Key: "ns", Origin: "", Path: "v.txt"}},
			[]string{"a", "b"}, 0, false, "", 0,
		},
		{
			"vic3 event FIOS", "vic3",
			[]Def{
				{Kind: "event", Key: "e.1", Origin: "a", Path: "a.txt", Line: 1},
				{Kind: "event", Key: "e.1", Origin: "b", Path: "b.txt", Line: 2},
			},
			nil, []string{"a", "b"}, 1, false, "a", 2,
		},
		{
			"eu5 gui_type LIOS", "eu5",
			[]Def{
				{Kind: "gui_type", Key: "w", Origin: "a", Path: "a.gui", Line: 1},
				{Kind: "gui_type", Key: "w", Origin: "b", Path: "b.gui", Line: 2},
			},
			nil, []string{"a", "b"}, 1, false, "b", 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := Contests(tt.gameID, tt.defs, tt.cache, tt.order)
			if len(rows) != tt.n {
				t.Fatalf("rows = %d, want %d (%+v)", len(rows), tt.n, rows)
			}
			if tt.n == 0 {
				return
			}
			r := rows[0]
			if r.Overlay != tt.overlay || r.ModCount != tt.mods {
				t.Errorf("overlay=%v mods=%d", r.Overlay, r.ModCount)
			}
			if tt.winner != "" && r.Winner != tt.winner {
				t.Errorf("winner = %q, want %s", r.Winner, tt.winner)
			}
			if tt.name == "vic3 event FIOS" && r.Rule != "FIOS" {
				t.Errorf("rule = %q, want FIOS", r.Rule)
			}
			if tt.name == "eu5 gui_type LIOS" && r.Rule != "LIOS" {
				t.Errorf("rule = %q, want LIOS", r.Rule)
			}
		})
	}
}

func internFixture() *VanillaCache {
	const a = `C:\Games\CK3\game\common\traits\00_traits.txt`
	const b = `C:\Games\CK3\game\events\yearly_events.txt`
	return &VanillaCache{
		FormatVersion: CacheFormatVersion,
		InstallID:     "i", GameID: "ck3", GameVersion: "1.0",
		Defs: []Def{
			{Kind: "traits", Key: "brave", Path: a, Line: 1},
			{Kind: "traits", Key: "craven", Path: a, Line: 9},
			{Kind: "event", Key: "y.1", Path: b, Line: 3},
		},
		LocRefs:  []Ref{{Key: "brave", Kind: "loc", Path: a, Line: 2}},
		CallRefs: []Ref{{Key: "my_effect", Kind: "scripted_effects", Path: b, Line: 4}},
		Edges:    []Edge{{From: "y.1", To: "y.2", Via: "trigger_event", Path: b, Line: 5}},
	}
}

// Paths must survive the save/load round trip exactly: they are what go-to
// definition navigates to.
func TestInternPathsRoundTrip(t *testing.T) {
	src := internFixture()
	dir := t.TempDir()
	p := filepath.Join(dir, "vanilla-test.json")
	if err := saveCacheFile(p, src); err != nil {
		t.Fatal(err)
	}
	got, err := loadCacheFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range src.Defs {
		if got.Defs[i].Path != d.Path {
			t.Errorf("def %d path = %q, want %q", i, got.Defs[i].Path, d.Path)
		}
		if got.Defs[i].Key != d.Key || got.Defs[i].Line != d.Line {
			t.Errorf("def %d = %+v, want %+v", i, got.Defs[i], d)
		}
	}
	if got.LocRefs[0].Path != src.LocRefs[0].Path {
		t.Errorf("locRef path = %q", got.LocRefs[0].Path)
	}
	if got.CallRefs[0].Path != src.CallRefs[0].Path {
		t.Errorf("callRef path = %q", got.CallRefs[0].Path)
	}
	if got.Edges[0].Path != src.Edges[0].Path {
		t.Errorf("edge path = %q", got.Edges[0].Path)
	}
	if len(got.Paths) != 0 {
		t.Errorf("Paths table leaked into the loaded cache: %v", got.Paths)
	}
}

// Saving must not disturb the in-memory cache: a live session may hold it while
// the scan writes.
func TestInternPathsDoesNotMutateSource(t *testing.T) {
	src := internFixture()
	want := src.Defs[0].Path
	dir := t.TempDir()
	if err := saveCacheFile(filepath.Join(dir, "v.json"), src); err != nil {
		t.Fatal(err)
	}
	if src.Defs[0].Path != want {
		t.Errorf("source mutated: %q, want %q", src.Defs[0].Path, want)
	}
	if len(src.Paths) != 0 {
		t.Errorf("source gained a Paths table: %v", src.Paths)
	}
}

// The whole point: the install path appears once, not once per record.
func TestInternPathsShrinksFile(t *testing.T) {
	src := internFixture()
	dir := t.TempDir()
	p := filepath.Join(dir, "v.json")
	if err := saveCacheFile(p, src); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	const a = `common\\traits\\00_traits.txt`
	if n := strings.Count(string(raw), a); n != 1 {
		t.Errorf("traits path written %d times, want 1:\n%s", n, raw)
	}
}

// TestFireEdgeFromIsTheEnclosingObject pins that an event's fire edges are
// attributed to the event, not to whatever ephemeral name was declared just
// above them.
//
// defContainers was the only one of six "what counts as a definition" predicates
// that did not exclude ephemeral kinds, so a `saved_scope` def — created at the
// line of the save_scope_as RHS, INSIDE the event body — became a container.
// containerAt binary-searches for the LAST container at or before a line, so the
// saved scope won over the event enclosing it.
//
// Measured on the real installs before the fix: 5,604 of CK3's 17,073 non-call
// edges (32.8%) and 1,501 of Vic3's 3,847 (39.0%) had a From that was not a
// definition at all — `bloc_leaving_reason -> festival.2000`,
// `spouse_seeker -> wedding.2`. That is the Event Graph's source node.
//
// The two fixtures that would have caught it were split across the bug:
// eventSrc asserts the edge and has no save_scope_as; `saved` has a
// save_scope_as and no trigger_event.
func TestFireEdgeFromIsTheEnclosingObject(t *testing.T) {
	const src = `namespace = t
t.1 = {
	type = character_event
	immediate = {
		save_scope_as = tgt
		trigger_event = t.2
	}
}
`
	ex := ExtractFile("ck3", `C:\mod\events\t.txt`, "events/t.txt", "mod", src, true)
	if len(ex.Edges) == 0 {
		t.Fatal("no edges harvested — the fixture stopped exercising the path")
	}
	if !hasEdge(ex.Edges, "t.1", "t.2", "") {
		var got []string
		for _, e := range ex.Edges {
			got = append(got, e.From+" -> "+e.To)
		}
		t.Fatalf("fire edge is not attributed to the event; got %v", got)
	}
	for _, e := range ex.Edges {
		if e.From == "tgt" {
			t.Errorf("saved scope %q became an edge source", e.From)
		}
	}
}

// A saved scope and the reference to it must name the same thing, whatever
// alphabet it is written in.
//
// ParsePrefixed stopped its name scan at the first byte above ASCII and returned
// ok=true anyway, so `scope:Linnéa` harvested a reference to `Linn` while
// `save_scope_as = Linnéa` harvested a definition of `Linnéa`. Two objects where
// the script has one: hover reads a truncated name, go-to-definition looks for
// something nothing defines, and the real definition looks unreferenced.
//
// No check reported it, and none could: saved scopes are ephemeral, so
// skipCompatRef keeps them out of dangling. The vanilla gate is structurally
// blind to this class, which is why the finding was recorded as harmless.
func TestNonASCIIScopeDefAndRefAgree(t *testing.T) {
	const body = `ns.1 = {
	type = character_event
	immediate = {
		save_scope_as = Linnéa
		add_gold = scope:Linnéa
	}
}
`
	ex := extractForms("ck3", "C:/m/events/x.txt", "events/x.txt", "mod",
		jomini.Parse(body), true)

	var defKey, refKey string
	for _, d := range ex.Defs {
		if d.Kind == "saved_scope" {
			defKey = d.Key
		}
	}
	for _, r := range ex.Refs {
		if r.Kind == "saved_scope" {
			refKey = r.Key
		}
	}
	if defKey != "Linnéa" {
		t.Errorf("def key = %q, want Linnéa", defKey)
	}
	if refKey != defKey {
		t.Errorf("ref names %q but the def is %q; the reference was truncated",
			refKey, defKey)
	}
}
