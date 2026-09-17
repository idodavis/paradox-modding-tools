// inspect_test.go covers the hover card: what it names, and where it sources
// each line from.

package lsp

import (
	"paradox-modding-tools/services/internal/lang/loc"

	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/session"
)

func TestHover(t *testing.T) {
	vfile := write(t, t.TempDir(), "localization/english/inventory_l_english.yml",
		"l_english:\n type:0 \"Type\"\n")
	body := `# event should not hover
test.1 = {
	type = character_event
	title = test.1.t
	title = type
	immediate = {
		add_gold = 1
		my_trig = yes
		save_scope_as = duel_target
		exists = scope:duel_target
	}
}
t.1 = { title = k.t }
t.2 = { title = k.t }
`
	s, root := buildSession(t, "ck3", map[string]string{
		"events/x.txt":                    body,
		"localization/english/events.yml": "l_english:\n test.1.t:0 \"Hello\"\n",
		"common/scripted_triggers/t.txt":  "my_trig = { always = yes }\n",
		"common/traits/00.txt":            "brave = { category = personality }\n",
	}, &vanillaCache{
		FieldInfo: map[string]string{
			"type": "Invite", "add_gold": "Gives gold.",
		},
		FieldInfoByKind: map[string]map[string]string{
			"event": {"type": "presentation", "title": "Dynamic"},
		},
		Structures: map[string][]string{"event": {"immediate"}},
		Schema: &declared.Schema{
			Effects: map[string]declared.EngineToken{"add_gold": {}},
		},
		Defs: []derived.Def{{Kind: "loc_key", Key: "type", Path: vfile, Line: 1}},
	}, &vanillaLoc{
		Sites: map[string]derived.LocEntry{"type": {Path: vfile, Line: 1, Value: "Type"}},
	})
	f := filepath.Join(root, "events", "x.txt")
	col := strings.Index(body, "event")
	if Hover(s, f, 0, col) != nil || len(Definition(s, f, 0, col)) != 0 {
		t.Fatal("comment token must not hover or define")
	}
	line, col := lineCol(body, "type = character_event")
	wantHover(t, s, f, line, col, "presentation")
	line, col = lineCol(body, "title = test.1.t")
	wantHover(t, s, f, line, col, "Dynamic")
	line, col = lineCol(body, "test.1.t")
	wantHover(t, s, f, line, col, "loc key", "Hello")
	line, col = lineCol(body, "title = type")
	valCol := col + len("title = ")
	wantHover(t, s, f, line, valCol, "loc key")
	if locs := Definition(s, f, line, valCol); len(locs) != 1 || locs[0].URI != vfile {
		t.Fatalf("F12=%v want %s", locs, vfile)
	}
	line, col = lineCol(body, "immediate")
	wantHover(t, s, f, line, col, "event key", "immediate")
	if h := Hover(s, f, line, col); h == nil || h.OriginName != "Crusader Kings III" {
		t.Fatalf("immediate originName=%#v", h)
	}
	line, col = lineCol(body, "add_gold")
	wantHover(t, s, f, line, col, "effect", "add_gold", "Gives gold")
	line, col = lineCol(body, "my_trig")
	wantHover(t, s, f, line, col, "scripted trigger", "my_trig")
	line, col = lineCol(body, "scope:duel_target")
	wantHover(t, s, f, line, col+len("scope:"), "saved scope")
	line, col = lineCol(body, "save_scope_as = duel_target")
	wantHover(t, s, f, line, col+len("save_scope_as = "), "saved scope")
	line, col = lineCol(body, "scope:duel_target")
	if locs := Definition(s, f, line, col+len("scope:")); len(locs) != 1 {
		t.Fatalf("saved scope F12=%v", locs)
	}
	if refs := References(s, f, line, col+len("scope:")); len(refs) < 2 {
		t.Fatalf("saved scope refs=%v want def+use", refs)
	}
	line, col = lineCol(body, "test.1.t")
	if h := Hover(s, f, line, col); h == nil || h.Body != "Hello" {
		t.Fatalf("loc Body=%#v", h)
	}
	line, col = lineCol(body, "k.t")
	if locs := References(s, f, line, col); len(locs) != 2 {
		t.Fatalf("refs=%v", locs)
	}
	line, col = lineCol(body, "\nt.1 =")
	if Rename(s, f, line, col+1, "t.9") == nil {
		t.Fatal("rename nil")
	}
	tf := filepath.Join(root, "common", "traits", "00.txt")
	if locs := Definition(s, tf, 0, 1); len(locs) != 1 || !derived.SamePath(locs[0].URI, tf) {
		t.Fatalf("mod F12=%v want %s", locs, tf)
	}
}

func TestHoverDescriptorOrigin(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	descA := "name = \"Alpha\"\npicture = \"thumb.png\"\n"
	descB := "name = \"Beta\"\npicture = \"other.png\"\n"
	fA := write(t, rootA, "descriptor.mod", descA)
	fB := write(t, rootB, "descriptor.mod", descB)
	s := session.New("ws", "ck3", "english", toIndex(&vanillaCache{
		FieldInfoByKind: map[string]map[string]string{
			"mod_descriptor": {"picture": "A picture for the mod."},
		},
	}, nil), []derived.ModInput{
		{Origin: "alpha", Root: rootA, Order: 0, Name: "Alpha Mod"},
		{Origin: "beta", Root: rootB, Order: 1, Name: "Beta Mod"},
	})
	s.DidOpen(fA, descA)
	s.DidOpen(fB, descB)
	line, col := lineCol(descA, "picture")
	h := Hover(s, fA, line, col)
	if h == nil {
		t.Fatal("hover nil")
	}
	if h.Origin != "alpha" || h.OriginName != "Alpha Mod" {
		t.Fatalf("origin=%q originName=%q", h.Origin, h.OriginName)
	}
	if !strings.Contains(h.Rel, "descriptor.mod") {
		t.Fatalf("rel=%q", h.Rel)
	}
	locs := Definition(s, fA, line, col)
	if len(locs) != 1 || locs[0].URI != fA {
		t.Fatalf("definition=%v want %s", locs, fA)
	}
	if w := s.Resolve("picture"); w != nil {
		t.Fatalf("descriptor scalar must not be a workspace object: %v", w)
	}
	if h.VanillaPath != "" {
		t.Fatalf("descriptor must not report a vanilla overlay: %#v", h)
	}
}

func TestHoverVanillaOverlay(t *testing.T) {
	vbody := "overlay.1 = {\n\ttype = character_event\n}\n"
	vroot := t.TempDir()
	vpath := write(t, vroot, "events/vanilla.txt", vbody)
	body := "overlay.1 = {\n\ttype = character_event\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body},
		&vanillaCache{
			Defs: []derived.Def{{
				Kind: "event", Key: "overlay.1", Path: vpath, Line: 0, Start: 0, End: 9,
			}},
		}, nil)
	f := filepath.Join(root, "events", "x.txt")
	line, col := lineCol(body, "overlay.1")
	h := Hover(s, f, line, col)
	if h == nil {
		t.Fatal("hover nil")
	}
	if h.Origin != "mod" {
		t.Fatalf("origin=%q", h.Origin)
	}
	if h.VanillaPath == "" {
		t.Fatalf("want vanilla overlay path, got %#v", h)
	}
	if !derived.SamePath(h.VanillaPath, vpath) {
		t.Fatalf("vanillaPath=%q want %s", h.VanillaPath, vpath)
	}
	if h.VanillaOriginName != "Crusader Kings III" {
		t.Fatalf("vanillaOriginName=%q", h.VanillaOriginName)
	}
	locs := Definition(s, f, line, col)
	if !hasLocURI(locs, f) || !hasLocURI(locs, vpath) {
		t.Fatalf("definition=%v want mod %s and vanilla %s", locs, f, vpath)
	}
	refs := References(s, f, line, col)
	if !hasLocURI(refs, vpath) {
		t.Fatalf("references=%v want vanilla %s", refs, vpath)
	}
}

func hasHoverValue(h *HoverResult, text string) bool {
	if h == nil {
		return false
	}
	for _, v := range h.Values {
		if v.Text == text {
			return true
		}
	}
	return false
}

func hasLocURI(locs []Location, path string) bool {
	for _, l := range locs {
		if derived.SamePath(l.URI, path) {
			return true
		}
	}
	return false
}

func TestHoverMacroAndPortrait(t *testing.T) {
	trig := "scripted_trigger my_name = { always = yes }\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"common/scripted_triggers/x.txt": trig,
		"events/p.txt":                   "ns.1 = {\n\tright_portrait = none\n}\n",
	}, &vanillaCache{
		FieldInfo: map[string]string{"left_portrait": "Left side portrait."},
	}, nil)
	tf := filepath.Join(root, "common", "scripted_triggers", "x.txt")
	line, col := lineCol(trig, "scripted_trigger")
	wantHover(t, s, tf, line, col, "scripted trigger", "my_name")
	ev := filepath.Join(root, "events", "p.txt")
	src := "ns.1 = {\n\tright_portrait = none\n}\n"
	line, col = lineCol(src, "right_portrait")
	wantHover(t, s, ev, line, col, "Left side portrait.")
}

func TestKindMetaAndGameInfoOverride(t *testing.T) {
	body := "test.1 = {\n\ttype = character_event\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"events/x.txt": body,
	}, &vanillaCache{
		FieldInfoByKind: map[string]map[string]string{
			"event": {"type": "presentation from info"},
		},
	}, nil)
	f := filepath.Join(root, "events", "x.txt")
	line, col := lineCol(body, "type =")
	h := Hover(s, f, line, col)
	if h == nil || h.Docs != "presentation from info" {
		t.Fatalf("docs=%#v", h)
	}
	if h.Hint != "" {
		t.Fatalf("game info should hide kind hint, got %q", h.Hint)
	}

	tfBody := "brave = { category = personality }\n"
	s, root = buildSession(t, "ck3", map[string]string{
		"common/traits/00.txt": tfBody,
	}, nil, nil)
	tf := filepath.Join(root, "common", "traits", "00.txt")
	h = Hover(s, tf, 0, 1)
	if h == nil || h.Kind != "traits" {
		t.Fatalf("trait hover=%#v", h)
	}
}

func TestLocHoverUnescape(t *testing.T) {
	locFile := "l_english:\n k.t:0 \"\\n\\nHello\"\n"
	body := "test.1 = { title = k.t }\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"localization/english/a_l_english.yml": locFile,
		"events/x.txt":                         body,
	}, nil, nil)
	f := filepath.Join(root, "events", "x.txt")
	line, col := lineCol(body, "k.t")
	h := Hover(s, f, line, col)
	if h == nil || h.Body != "Hello" {
		t.Fatalf("Body=%#v", h)
	}
	if h.Hint != "" {
		t.Fatalf("loc hint should be empty, got %q", h.Hint)
	}
}

func TestNamespaceHover(t *testing.T) {
	body := "namespace = court_events\ncourt_events.1 = { type = character_event }\n"
	s, f := ck3Sess(t, body)
	line, col := lineCol(body, "court_events")
	wantHover(t, s, f, line, col, "namespace", "court_events")
}

func TestLocEngineValueHover(t *testing.T) {
	body := "l_english:\n r:0 \"Cost: $VALUE|=+0$\"\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"localization/english/a_l_english.yml": body,
	}, nil, nil)
	f := filepath.Join(root, "localization", "english", "a_l_english.yml")
	line, col := lineCol(body, "VALUE")
	h := Hover(s, f, line, col)
	if h == nil || h.Kind != "loc value" {
		t.Fatalf("hover=%#v", h)
	}
	if locs := Definition(s, f, line, col); len(locs) != 0 {
		t.Fatalf("engine VALUE F12=%v", locs)
	}
}

// This is the specification for Phase 11b, kept rather than deleted because the
// need is real: modders hover and F12 game-rule options constantly.
//
// It passed only because the fixture invents `exists = game_rule_setting:suf_quieter`.
// Nothing in vanilla CK3 or in any real mod writes that prefix — options are
// referenced as `has_game_rule = suf_quieter`, a field RHS. The nested-database
// pass is driven by `prefix:id` citations, so it is structurally blind to this
// reference form, and on a real mod PMT harvests 7 rules and 0 options.
//
// Making it pass needs three things to line up: harvest options as defs, resolve
// `has_game_rule` through field-value kinds, and teach LocConventions the
// prefixed `setting_<id>` / `setting_<id>_desc` form CK3 actually uses.
func TestGameRuleSettingHover(t *testing.T) {
	t.Skip("Phase 11b: game-rule options are not indexed; see GAME-SYNTAX.md §5")
	rules := "secret_unbeliever_found_rule = {\n\tdefault = a\n\tsuf_quieter = { }\n" +
		"\texists = game_rule_setting:suf_quieter\n}\n"
	locYml := "l_english:\n game_rule_setting_suf_quieter:0 \"Quieter\"\n"
	use := "ev.1 = { limit = { has_game_rule = suf_quieter } }\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"common/game_rules/x.txt":              rules,
		"localization/english/a_l_english.yml": locYml,
		"events/e.txt":                         use,
	}, &vanillaCache{
		LocConventions: map[string][]loc.Convention{
			"game_rule_setting": {{Template: "game_rule_setting_{id}", Coverage: 1}},
		},
	}, &vanillaLoc{
		Sites: map[string]derived.LocEntry{
			"game_rule_setting_suf_quieter": {Path: "loc", Line: 1, Value: "Quieter"},
		},
	})
	ef := filepath.Join(root, "events", "e.txt")
	line, col := lineCol(use, "suf_quieter")
	h := Hover(s, ef, line, col)
	if h == nil || h.Body != "Quieter" {
		t.Fatalf("hover body=%#v", h)
	}
	if locs := Definition(s, ef, line, col); len(locs) != 1 {
		t.Fatalf("F12=%v", locs)
	}
}

func ck3VanillaSess(t *testing.T, install string, cache *vanillaCache) *session.Session {
	t.Helper()
	if cache == nil {
		cache = &vanillaCache{}
	}
	cache.InstallPath = install
	if cache.GameID == "" {
		cache.GameID = "ck3"
	}
	mod := t.TempDir()
	write(t, mod, "descriptor.mod", "name = \"t\"\n")
	return session.New("ws", "ck3", "english", toIndex(cache, nil), []derived.ModInput{
		{Origin: "mod", Root: mod, Order: 0},
	})
}

func TestVanillaScriptParamIgnoresFieldDoc(t *testing.T) {
	trig := "can_fight = {\n\tsubject = $ARMY_OWNER$\n}\n"
	call := "ev.1 = {\n\tcan_fight = { ARMY_OWNER = scope:potential }\n}\n"
	trigRel := "game/common/scripted_triggers/00_religious_triggers.txt"
	callRel := "game/events/dlc/mpo/mpo_events_ariana.txt"
	install := t.TempDir()
	tf := write(t, install, trigRel, trig)
	ef := write(t, install, callRel, call)
	s := ck3VanillaSess(t, install, &vanillaCache{
		Defs: []derived.Def{{
			Kind: "scripted_trigger", Key: "can_fight", Path: tf,
			Start: 0, End: len("can_fight"),
		}},
		FieldInfo: map[string]string{
			"ARMY_OWNER": "Get owner of scoped army",
		},
	})
	s.DidOpen(tf, trig)
	s.DidOpen(ef, call)
	line, col := lineCol(trig, "$ARMY_OWNER$")
	h := Hover(s, tf, line, col+1)
	if h == nil || h.Kind != "script param" ||
		!hasHoverValue(h, "scope:potential") {
		t.Fatalf("hover=%#v", h)
	}
}

// A bare number or boolean on the right of an assignment is a literal, so it
// must not be resolved against any database. Vanilla localization defines a key
// spelled `yes`, and before this the card for `is_ai = yes` showed that key's
// player-facing string as though it were the meaning of the line.
func TestHoverLiteralValueIsNotAName(t *testing.T) {
	body := "test.1 = {\n\ttrigger = {\n\t\tis_ai = yes\n\t\tgold = 100\n\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"events/x.txt": body,
		// Both spellings exist as real objects, so a hover that resolves the
		// literal has something to latch onto and the test can tell.
		"localization/english/ui.yml": "l_english:\n yes:0 \"Yes\"\n no:0 \"No\"\n",
		"common/traits/00.txt":        "brave = { category = personality }\n",
	}, &vanillaCache{
		Schema: &declared.Schema{
			Triggers: map[string]declared.EngineToken{
				"is_ai": {In: []string{"character"}, Doc: "Is the character AI-controlled?"},
			},
		},
	}, nil)
	f := filepath.Join(root, "events", "x.txt")
	for _, word := range []string{"yes", "100"} {
		line, col := lineCol(body, word)
		if h := Hover(s, f, line, col); h != nil {
			t.Errorf("hover on literal %q = kind=%q key=%q docs=%q, want no card",
				word, h.Kind, h.Key, h.Docs)
		}
		if locs := Definition(s, f, line, col); len(locs) > 0 {
			t.Errorf("F12 on literal %q = %v, want none", word, locs)
		}
	}
	// The key on the same line still hovers; only the value is suppressed.
	line, col := lineCol(body, "is_ai")
	if h := Hover(s, f, line, col); h == nil {
		t.Error("hover on the key is_ai: no card")
	}
}

// script_docs documents the engine API, not script: of CK3's 11,708 scripted
// macros exactly 4 appear in it. The definition is the only source of a macro's
// signature, and PMT already harvests the $PARAM$ names inside its body.
func TestHoverScriptedMacroShowsItsParameters(t *testing.T) {
	macro := "scripted_effect give_gift_effect = {\n" +
		"\tadd_gold = $AMOUNT$\n" +
		"\tsave_scope_as = $RECIPIENT$\n}\n"
	call := "ns.1 = {\n\timmediate = {\n\t\tgive_gift_effect = { AMOUNT = 10 RECIPIENT = x }\n\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"common/scripted_effects/x.txt": macro,
		"events/e.txt":                  call,
	}, nil, nil)
	f := filepath.Join(root, "common", "scripted_effects", "x.txt")
	line, col := lineCol(macro, "give_gift_effect")
	h := Hover(s, f, line, col)
	if h == nil {
		t.Fatal("no card for the macro")
	}
	for _, want := range []string{"AMOUNT", "RECIPIENT", "give_gift_effect = {"} {
		if !strings.Contains(h.Usage, want) {
			t.Errorf("usage %q missing %q", h.Usage, want)
		}
	}
	// The old fixed string said nothing about this macro in particular.
	if strings.Contains(h.Hint, "{ PARAM }") {
		t.Errorf("hint still the placeholder: %q", h.Hint)
	}
}

// EU5 declares 2,436 modifiers with a category and prose for none of them, so
// the category is the whole of what the game says about one. The three games
// phrase the list differently and EU5 appends "all" to every entry.
func TestHoverModifierShowsDeclaredArea(t *testing.T) {
	body := "aga_tr = {\n\tlocal_trades_per_burgher = 0.25\n}\n"
	s, root := buildSession(t, "eu5", map[string]string{
		"in_game/common/town_rights/x.txt": body,
	}, &vanillaCache{
		Schema: &declared.Schema{
			Modifiers: map[string]declared.Modifier{
				"local_trades_per_burgher": {Area: "location, all"},
				"monthly_income":           {Area: "character and province"},
			},
		},
	}, nil)
	f := filepath.Join(root, "in_game", "common", "town_rights", "x.txt")
	line, col := lineCol(body, "local_trades_per_burgher")
	h := Hover(s, f, line, col)
	if h == nil {
		t.Fatal("no card for the modifier")
	}
	if !strings.Contains(strings.ToLower(h.Docs), "location") {
		t.Errorf("docs = %q, want the declared area", h.Docs)
	}
	if strings.Contains(strings.ToLower(h.Docs), "all") {
		t.Errorf("docs = %q, must drop the all-modifiers filler", h.Docs)
	}
}

// Paradox localizes a great many words that are also script tokens, and every
// localization entry is a `loc_key` definition in the index. Two places let one
// win where it should not: an unrecognised assignment key fell through to the
// localization lookup, and a value that named an object matched both a
// `loc_key` and the object, with load order rather than kind deciding.
func TestLocKeyNeverWinsOverScript(t *testing.T) {
	const body = "ns.1 = {\n\thas_realm_law = crown_authority_1\n\tculture = french\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"events/x.txt":          body,
		"common/cultures/c.txt": "french = { }\n",
		"common/laws/l.txt":     "crown_authority_1 = { }\n",
		"localization/english/a_l_english.yml": "l_english:\n has_realm_law:0 \"Realm Law\"\n" +
			" french:0 \"French\"\n crown_authority_1:0 \"Crown Authority\"\n",
	}, nil, nil)
	f := filepath.Join(root, "events", "x.txt")

	// The check macro. Nothing declares it here, which is the point: not
	// knowing is fine, calling it localization is not.
	line, col := lineCol(body, "has_realm_law")
	if h := Hover(s, f, line, col); h != nil && h.Kind == "loc key" {
		t.Errorf("assignment key came back as a localization key: %#v", h)
	}
	// The value names a culture, and a culture always has a key named after it.
	line, col = lineCol(body, "culture = french")
	h := Hover(s, f, line, col+len("culture = "))
	if h == nil || h.Kind == "loc key" {
		t.Errorf("culture value came back as a localization key: %#v", h)
	}
}

// A bare number on the left of an `=` is a weight. CK3 also keys map positions
// by province id, so `95 = { … }` inside a random_list resolved to a map_data
// definition and the card offered a French town as the meaning of a probability.
func TestRandomListWeightIsNotAMapPosition(t *testing.T) {
	const body = "ns.1 = {\n\timmediate = {\n\t\trandom_list = {\n" +
		"\t\t\t95 = { trigger_event = birth.1001 }\n" +
		"\t\t\t5 = { trigger_event = birth.1002 }\n\t\t}\n\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body},
		&vanillaCache{
			Defs: []derived.Def{{Kind: "map_data", Key: "95", Path: "positions.txt"}},
		}, nil)
	f := filepath.Join(root, "events", "x.txt")
	line, col := lineCol(body, "95 = { trigger_event")
	h := Hover(s, f, line, col)
	if h == nil {
		t.Fatal("no hover on a weight")
	}
	if h.Kind == "map data" || strings.Contains(h.Docs, "positions") {
		t.Fatalf("weight resolved to a map position: %#v", h)
	}
	// The denominator is the sum of the siblings, not an assumed 100.
	if h.Kind != "weight" || !strings.Contains(h.Docs, "95%") ||
		!strings.Contains(h.Docs, "random_list") {
		t.Fatalf("weight card = %#v", h)
	}
	// And peek must not offer to jump into positions.txt either.
	if locs := Definition(s, f, line, col); len(locs) != 0 {
		t.Fatalf("peek on a weight offered %v", locs)
	}
}

// The same numeric key is not always a probability. A Victoria 3 gene block
// writes `20 = empty`, an index into a lookup table, and its entries are
// scalars rather than bodies — so there is no distribution to report and the
// card must say nothing rather than invent a percentage.
func TestNumericLookupKeyIsNotAWeight(t *testing.T) {
	const body = "portrait = {\n\tgenes = {\n\t\thair = {\n" +
		"\t\t\t20 = empty\n\t\t\t80 = empty\n\t\t}\n\t}\n}\n"
	s, root := buildSession(t, "vic3",
		map[string]string{"common/genes/g.txt": body}, nil, nil)
	f := filepath.Join(root, "common", "genes", "g.txt")
	line, col := lineCol(body, "20 = empty")
	if h := Hover(s, f, line, col); h != nil && h.Kind == "weight" {
		t.Fatalf("lookup index reported as a probability: %#v", h)
	}
}

// A namespace declares an id prefix for events; nothing points at one. It is a
// definition like any other in the index though, so load order decided ties
// against real objects — a mod's `namespace = conqueror` outranked vanilla's
// `conqueror` trait, and `add_trait = conqueror` hovered as the namespace.
func TestNamespaceDoesNotOutrankAnObject(t *testing.T) {
	const ev = "namespace = conqueror\nconqueror.1 = {\n" +
		"\timmediate = { add_trait = conqueror }\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/conq.txt": ev},
		&vanillaCache{
			Defs: []derived.Def{{
				Kind: "traits", Key: "conqueror", Path: "traits.txt", Line: 3,
			}},
		}, nil)
	f := filepath.Join(root, "events", "conq.txt")
	line, col := lineCol(ev, "add_trait = conqueror")
	h := Hover(s, f, line, col+len("add_trait = "))
	if h == nil || h.Kind != "traits" {
		t.Fatalf("add_trait value = %#v, want the trait", h)
	}
	if locs := Definition(s, f, line, col+len("add_trait = ")); len(locs) != 1 ||
		!strings.Contains(locs[0].URI, "traits.txt") {
		t.Fatalf("F12 went to the namespace, not the trait: %v", locs)
	}
}

// A namespace nothing shadows still resolves, so hovering one still works.
func TestNamespaceStillResolvesWhenAlone(t *testing.T) {
	const ev = "namespace = solitary\nsolitary.1 = { }\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"events/s.txt": ev}, nil, nil)
	f := filepath.Join(root, "events", "s.txt")
	line, col := lineCol(ev, "namespace = solitary")
	if h := Hover(s, f, line, col+len("namespace = ")); h == nil {
		t.Fatal("a namespace with no rival must still hover")
	}
}

// A key can be a declared token as well. `add_to_list` is an effect the game
// documents, and it is also a structure key of the kind being edited, so hover
// identifies it as a key — and the signature the game states for it was being
// dropped, on 58 of CK3's 1,201 sampled hovers that had one.
// TestDeclaredTokenOutranksFieldKey pins the precedence for a key that is both
// a structure key of its kind and a token the game declares.
func TestDeclaredTokenOutranksFieldKey(t *testing.T) {
	const body = "act = {\n\tadd_to_list = my_list\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"common/activity_types/a.txt": body,
	}, &vanillaCache{
		// Both a structure key of the kind and a declared effect.
		Structures: map[string][]string{"activity_types": {"add_to_list"}},
		Schema: &declared.Schema{Effects: map[string]declared.EngineToken{
			"add_to_list": {Usage: "add_to_list = <list name>"},
		}},
	}, nil)
	f := filepath.Join(root, "common", "activity_types", "a.txt")
	line, col := lineCol(body, "add_to_list")
	h := Hover(s, f, line, col)
	if h == nil {
		t.Fatal("no hover")
	}
	if h.Kind != "effect" {
		t.Fatalf("declared effect reported as %q: %#v", h.Kind, h)
	}
	// The signature is what this test was written to protect, and it survives
	// the change: a declared card carries it too.
	if h.Usage != "add_to_list = <list name>" {
		t.Fatalf("declared signature dropped: %#v", h)
	}
}

// A date is not a name. The games key their history files by one, so `1178.1.1`
// was harvested as a `struggles` object and a `cultures` object at once:
// hovering `game_start_date < 1178.1.1` produced a struggle card citing
// `struggle:id`, and go-to-definition jumped into a culture's history file.
func TestDateLiteralIsNotADefinition(t *testing.T) {
	const body = "d = {\n\tis_shown = { game_start_date < 1178.1.1 }\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"common/decisions/d.txt": body,
		// Exactly what vanilla ships: history keyed by date, under two kinds.
		"history/struggles/s.txt": "1178.1.1 = { effect = { } }\n",
		"history/cultures/c.txt":  "1178.1.1 = { discover_innovation = x }\n",
	}, nil, nil)
	f := filepath.Join(root, "common", "decisions", "d.txt")
	line, col := lineCol(body, "1178.1.1")

	if h := Hover(s, f, line, col); h != nil {
		t.Errorf("a date must not hover as an object: %#v", h)
	}
	if locs := Definition(s, f, line, col); len(locs) != 0 {
		t.Errorf("a date must not go to a definition: %v", locs)
	}
	// And the date never became a definition in the first place.
	if d := s.Resolve("1178.1.1"); d != nil {
		t.Errorf("date harvested as a %s definition: %+v", d.Kind, d)
	}
}

// composeHint describes the KIND — its scope type, how it is cited, how its
// localization keys are named. A field-key card is not about the kind, so
// attaching that hint reported a coat of arms' localization convention as
// though it described `color2`.
func TestFieldKeyCardDoesNotDescribeItsKind(t *testing.T) {
	const body = "k_hungary = {\n\tcolor2 = \"white\"\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"common/coat_of_arms/coat_of_arms/c.txt": body,
	}, &vanillaCache{
		Structures: map[string][]string{"coat_of_arms": {"color2"}},
		// The kind has a localization convention; the field does not.
		LocConventions: map[string][]loc.Convention{
			"coat_of_arms": {{Template: "{id}", Coverage: 1}},
		},
	}, nil)
	f := filepath.Join(root, "common", "coat_of_arms", "coat_of_arms", "c.txt")
	line, col := lineCol(body, "color2")
	h := Hover(s, f, line, col)
	if h == nil {
		t.Fatal("no hover")
	}
	if !strings.Contains(h.Kind, "key") {
		t.Fatalf("expected the field-key card: %#v", h)
	}
	if h.Hint != "" {
		t.Fatalf("field-key card described its kind: hint=%q", h.Hint)
	}
}

// The kind hint never mentions localization conventions. Two renderings were
// tried — the label "loc kind_id" and the template "loc `<id>_adj`" — and
// neither says anything a reader can act on. Which keys an object should have,
// and which are missing, is a list with per-key state: Inspector material, not a
// subtitle.
func TestKindHintHasNoLocConvention(t *testing.T) {
	const body = "my_trait = {\n\tcategory = personality\n}\n"
	cache := &vanillaCache{
		LocConventions: map[string][]loc.Convention{
			"traits": {{Template: "{id}", Coverage: 1}, {Template: "{id}_desc", Coverage: 1}},
		},
	}
	s, root := buildSession(t, "ck3",
		map[string]string{"common/traits/t.txt": body}, cache, nil)
	f := filepath.Join(root, "common", "traits", "t.txt")
	line, col := lineCol(body, "my_trait")
	h := Hover(s, f, line, col)
	if h == nil {
		t.Fatal("no hover")
	}
	if strings.Contains(h.Hint, "loc ") || strings.Contains(h.Hint, "<id>") {
		t.Fatalf("hint still carries a loc convention: hint=%q", h.Hint)
	}
}

// covers the scope context on the hover card: which scope
// the cursor sits in, and where a scope link moves.
// TestHoverReportsScopeAtCursor pins that the card answers "what scope am I
// in?". The engine already computed this for completion filtering and simply
// never showed it, which is the single most useful thing to tell a new modder.
func TestHoverReportsScopeAtCursor(t *testing.T) {
	body := "on_birth = {\n\teffect = {\n\t\tadd_gold = 10\n\t}\n}\n"
	s, path := scopedSession(t, body)
	// Line 2, on `add_gold`.
	h := Hover(s, path, 2, 3)
	if h == nil {
		t.Fatal("no hover on add_gold")
	}
	if h.Scope != "character" {
		t.Errorf("Scope = %q, want %q", h.Scope, "character")
	}
	if h.ScopeOut != "" {
		t.Errorf("ScopeOut = %q, want empty for a plain effect", h.ScopeOut)
	}
}

// TestHoverReportsScopeTransition pins the other half: a scope link's whole
// purpose is to move somewhere else, so the card names where it lands.
func TestHoverReportsScopeTransition(t *testing.T) {
	body := "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
	s, path := scopedSession(t, body)
	h := Hover(s, path, 2, 3) // on `capital_county`
	if h == nil {
		t.Fatal("no hover on capital_county")
	}
	if h.ScopeOut != "landed_title" {
		t.Errorf("ScopeOut = %q, want %q", h.ScopeOut, "landed_title")
	}
	// Sitting on the link itself asks what scope the link is used in, which is
	// the scope outside its block — not the one it moves to.
	if h.Scope != "character" {
		t.Errorf("Scope = %q, want %q", h.Scope, "character")
	}
}

// TestHoverScopeInsideLinkBlock pins that the scope follows the cursor into the
// block, which is the case a file cannot answer: the enclosing link may be
// hundreds of lines up.
func TestHoverScopeInsideLinkBlock(t *testing.T) {
	body := "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
	s, path := scopedSession(t, body)
	h := Hover(s, path, 3, 4) // on `add_title_law`, inside the link block
	if h == nil {
		t.Fatal("no hover on add_title_law")
	}
	if h.Scope != "landed_title" {
		t.Errorf("Scope = %q, want %q", h.Scope, "landed_title")
	}
}

// TestHoverScopeAbsentWithoutSchema pins the floor: with no declared type
// system there is no scope to report, and an empty result means "do not say",
// never "no scope is valid".
func TestHoverScopeAbsentWithoutSchema(t *testing.T) {
	s, root := buildSession(t, "ck3",
		map[string]string{"common/traits/x.txt": "brave = {\n\tdiplomacy = 1\n}\n"}, nil, nil)
	h := Hover(s, root+"/common/traits/x.txt", 1, 2)
	if h != nil && strings.TrimSpace(h.Scope) != "" {
		t.Errorf("Scope = %q, want empty without a schema", h.Scope)
	}
}

// TestHoverScopeWithoutDeclaredRoot pins the case that covers most of what
// anyone actually hovers. Only on_action files declare a starting scope, so the
// walk used to give up immediately everywhere else — every event, decision,
// scripted effect and modifier body reported nothing. A declared scope link
// states what it produces regardless of what preceded it.
func TestHoverScopeWithoutDeclaredRoot(t *testing.T) {
	// common/scripted_effects has no declared root scope of its own.
	s, root := buildSession(t, "ck3", map[string]string{
		"common/scripted_effects/e.txt": "do_it = {\n\tcapital_county = {\n\t\tadd_title_law = x\n\t}\n}\n",
	}, scopedCache(), nil)
	path := root + "/common/scripted_effects/e.txt"

	h := Hover(s, path, 2, 3) // inside capital_county's block
	if h == nil {
		t.Fatal("no hover")
	}
	if h.Scope != "landed_title" {
		t.Errorf("Scope = %q, want %q — a declared link establishes the scope",
			h.Scope, "landed_title")
	}
}

// TestHoverScopeStaysSilentWhenUndeclared pins the floor the change must not
// cross: with nothing declared to establish a scope, the card says nothing
// rather than guessing. Reporting a wrong scope is worse than reporting none.
func TestHoverScopeStaysSilentWhenUndeclared(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/scripted_effects/e.txt": "do_it = {\n\tadd_gold = 10\n}\n",
	}, scopedCache(), nil)
	h := Hover(s, root+"/common/scripted_effects/e.txt", 1, 3)
	if h != nil && h.Scope != "" {
		t.Errorf("Scope = %q, want empty — nothing declares the root here", h.Scope)
	}
}

// covers the accepted-value line: what a key's value may
// be, which is often knowable even where nothing documents what the key means.
// acceptsCache declares a token whose argument type the game states, alongside
// a field PMT could only have inferred, so the precedence is observable.
func acceptsCache() *vanillaCache {
	c := &vanillaCache{
		Schema: &declared.Schema{
			Scopes: map[string]declared.ScopeType{
				"character":     {ChangeScopes: true, ExecuteEffects: true},
				"culture_group": {},
			},
			Triggers: map[string]declared.EngineToken{
				"has_culture_group": {In: []string{"character"}, Target: "culture_group"},
			},
			OnActions: map[string]string{"on_birth": "character"},
		},
		// Derived, and deliberately disagreeing with the declared target above
		// so a card that reads the wrong one is visible.
		FieldValueKinds: map[string]string{
			"has_culture_group": "traits",
			"some_undeclared":   "traits",
		},
		// Documented, so the key is identified at all and reaches a card. A
		// token nothing recognises produces no hover, which is correct.
		FieldInfo: map[string]string{"some_undeclared": "An undeclared field."},
	}
	return c
}

func TestHoverAcceptsPrefersDeclaredTarget(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/on_action/x.txt": "on_birth = {\n\ttrigger = {\n\t\thas_culture_group = x\n\t}\n}\n",
	}, acceptsCache(), nil)
	h := Hover(s, root+"/common/on_action/x.txt", 2, 3)
	if h == nil {
		t.Fatal("no hover")
	}
	if !strings.Contains(h.Accepts, "culture group") {
		t.Errorf("Accepts = %q, want the declared target type", h.Accepts)
	}
	if strings.Contains(h.Accepts, "trait") {
		t.Errorf("Accepts = %q used the derived field type over the declared one",
			h.Accepts)
	}
}

func TestHoverAcceptsFallsBackToDerived(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\tsome_undeclared = x\n\t}\n}\n",
	}, acceptsCache(), nil)
	h := Hover(s, root+"/common/on_action/x.txt", 2, 3)
	if h == nil {
		t.Fatal("no hover")
	}
	if !strings.Contains(h.Accepts, "trait") {
		t.Errorf("Accepts = %q, want the derived field type when nothing declares one",
			h.Accepts)
	}
}

// TestHoverAcceptsSilentWhenUnknown pins the floor: saying nothing is correct
// when no source types the slot. Inventing a type would be the failure this
// whole line exists to avoid.
func TestHoverAcceptsSilentWhenUnknown(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\tnothing_knows_this = x\n\t}\n}\n",
	}, acceptsCache(), nil)
	h := Hover(s, root+"/common/on_action/x.txt", 2, 3)
	if h != nil && h.Accepts != "" {
		t.Errorf("Accepts = %q, want empty", h.Accepts)
	}
}

// TestHoverDateInModEra covers the calendar reading aid end to end: a date in a
// mod that declares an era hovers as the year its players see.
func TestHoverDateInModEra(t *testing.T) {
	withCalendar(t, derived.ModCalendar{Era: "AC", EpochOffset: 7782})
	s, root := buildSession(t, "ck3", map[string]string{
		"common/bookmarks/bookmarks/00.txt": "bm = {\n\tstart_date = 8082.1.1\n}\n",
	}, nil, nil)
	h := Hover(s, root+"/common/bookmarks/bookmarks/00.txt", 1, 15)
	if h == nil {
		t.Fatal("no hover on a date in a mod with a declared era")
	}
	if !strings.Contains(h.Docs, "300.1.1 AC") {
		t.Errorf("Docs = %q, want the era form", h.Docs)
	}
}

// TestHoverDateSilentWithoutCalendar is the guardrail for the whole feature: no
// calendar, no card. A date is a number, and saying anything about it is the bug
// that made 1178.1.1 hover as a struggle.
func TestHoverDateSilentWithoutCalendar(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/bookmarks/bookmarks/00.txt": "bm = {\n\tstart_date = 8082.1.1\n}\n",
	}, nil, nil)
	if h := Hover(s, root+"/common/bookmarks/bookmarks/00.txt", 1, 15); h != nil {
		t.Errorf("date hovered with no calendar declared: %+v", h)
	}
}

// TestHoverAcceptsLocFieldIsNotEnumerated pins the fix for a card that listed
// forty-one unrelated event descriptions under `desc`. A field holding a
// localization key takes ANY key, not one of a set.
func TestHoverAcceptsLocFieldIsNotEnumerated(t *testing.T) {
	c := &vanillaCache{
		FieldInfo: map[string]string{"desc": "Dynamic Description syntax."},
	}
	s, root := buildSession(t, "ck3", map[string]string{
		"events/x.txt": "namespace = ns\nns.1 = {\n\ttype = character_event\n\tdesc = ns.1.desc\n}\n",
	}, c, nil)
	h := Hover(s, root+"/events/x.txt", 3, 2)
	if h == nil {
		t.Fatal("no hover on desc")
	}
	if !strings.Contains(h.Accepts, "localization key") {
		t.Errorf("Accepts = %q, want it to name what the field takes", h.Accepts)
	}
}

// TestHoverNamesRealContainer pins that a card names what a key belongs to, not
// whatever block happens to enclose it. Found in the running app: real CK3 cards
// read "OR key" and "scope:secondary recipient key", which name a construct
// rather than a container.
func TestHoverNamesRealContainer(t *testing.T) {
	c := &vanillaCache{
		Structures: map[string][]string{"event": {"trigger", "type"}},
		NestedMembers: map[string][]string{
			"trigger": {"or", "has_trait", "exists", "opinion"},
		},
		// FieldInfoByKind, not FieldInfo: identify reaches the field-key
		// branch through kindFieldDoc, which consults the per-kind table only.
		// A fixture setting the global one alone produces a bare "field" card
		// and proves nothing.
		FieldInfoByKind: map[string]map[string]string{
			"event": {"has_trait": "Checks a trait."},
		},
	}
	body := "namespace = ns\n" +
		"ns.1 = {\n" +
		"\ttrigger = {\n" +
		"\t\tOR = {\n" +
		"\t\t\tscope:target = {\n" +
		"\t\t\t\thas_trait = brave\n" +
		"\t\t\t}\n" +
		"\t\t}\n" +
		"\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, c, nil)
	h := Hover(s, root+"/events/x.txt", 5, 6) // has_trait, two grouping blocks deep
	if h == nil {
		t.Fatal("no card")
	}
	// OR groups and scope: redirects; neither changes which keys are legal.
	for _, wrong := range []string{"OR", "scope"} {
		if strings.Contains(h.Kind, wrong) {
			t.Errorf("Kind = %q, want the governing block, not %q", h.Kind, wrong)
		}
	}
	if !strings.Contains(h.Kind, "trigger") {
		t.Errorf("Kind = %q, want it to name the trigger block", h.Kind)
	}
}

func TestHoverNamesNestedMember(t *testing.T) {
	c := &vanillaCache{
		Structures: map[string][]string{"coat_of_arms": {"pattern", "colored_emblem"}},
		NestedMembers: map[string][]string{
			"colored_emblem": {"texture", "color1", "instance"},
			"instance":       {"position", "scale"},
		},
	}
	body := "k_test = {\n" +
		"\tpattern = \"p.dds\"\n" +
		"\tcolored_emblem = {\n" +
		"\t\ttexture = \"e.dds\"\n" +
		"\t\tinstance = {\n" +
		"\t\t\tposition = { 0.5 0.5 }\n" +
		"\t\t}\n" +
		"\t}\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"common/coat_of_arms/coat_of_arms/x.txt": body}, c, nil)
	p := root + "/common/coat_of_arms/coat_of_arms/x.txt"

	for _, tc := range []struct {
		line, col int
		want      string
	}{
		{3, 4, "colored emblem"}, // texture, two levels down
		{4, 4, "colored emblem"}, // instance
		{5, 5, "instance"},       // position, three levels down
	} {
		h := Hover(s, p, tc.line, tc.col)
		if h == nil {
			t.Errorf("line %d: no card", tc.line)
			continue
		}
		if !strings.Contains(h.Kind, tc.want) {
			t.Errorf("line %d: Kind = %q, want it to name %q", tc.line, h.Kind, tc.want)
		}
	}
}

// TestHoverLabelsEffectArguments pins that a block which is itself an effect
// holds arguments, not keys: "set variable key" reads as though set_variable
// were a container.
func TestHoverLabelsEffectArguments(t *testing.T) {
	c := &vanillaCache{
		Structures:    map[string][]string{"event": {"immediate"}},
		NestedMembers: map[string][]string{"set_variable": {"name", "value"}},
		Schema: &declared.Schema{Effects: map[string]declared.EngineToken{
			"set_variable": {Doc: "Sets a variable."},
		}},
	}
	body := "namespace = ns\nns.1 = {\n\timmediate = {\n\t\tset_variable = {\n" +
		"\t\t\tname = x\n\t\t}\n\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, c, nil)
	h := Hover(s, root+"/events/x.txt", 4, 4)
	if h == nil {
		t.Fatal("no card on an effect's argument")
	}
	if !strings.Contains(h.Kind, "argument") {
		t.Errorf("Kind = %q, want it to read as an argument", h.Kind)
	}
}

func TestLocHoverCarriesColumn(t *testing.T) {
	// Two keys, so the one under test is not on the first line — a column of 0
	// on line 1 would be indistinguishable from the bug.
	locBody := "l_english:\n first_key:0 \"one\"\n wanted_key:0 \"two\"\n"
	script := "ns.1 = {\n\ttitle = wanted_key\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"events/x.txt":                         script,
		"localization/english/a_l_english.yml": locBody,
	}, nil, nil)

	line, col := lineCol(script, "wanted_key")
	h := Hover(s, filepath.Join(root, "events", "x.txt"), line, col)
	if h == nil {
		t.Fatal("no hover card for a localization key")
	}
	if h.Line == 0 {
		t.Fatalf("card carries no line: %+v", h)
	}
	if h.Col == 0 {
		t.Fatalf("card carries line %d but no column — the editor opens the file "+
			"at line 1 because languageClient gates the selection on Col", h.Line)
	}
}
