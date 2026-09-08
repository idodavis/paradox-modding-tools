// complete_test.go covers completion: which items each slot offers, their
// ordering, and the range they replace.

package lsp

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
)

func TestCompleteActionsSymbols(t *testing.T) {
	body := "test.1 = {\n  type = character_event\n}\n"
	s, f := ck3Sess(t, body)
	if edits := FormatDocument(s, f); len(edits) == 0 {
		t.Fatal("want indent edits")
	}
	if folds := FoldingRanges(s, f); len(folds) == 0 {
		t.Fatal("want fold range")
	}
	syms := DocumentSymbols(s, f)
	if len(syms) == 0 || syms[0].Name != "test.1" {
		t.Fatalf("doc symbols=%v", syms)
	}
	if ws := WorkspaceSymbols(s, "test"); len(ws) == 0 {
		t.Fatal("workspace symbols empty")
	}

	s, f = ck3Sess(t, "test.1 = {\n	title = brand_new_key\n}\n")
	var edit *WorkspaceEdit
	for _, a := range CodeActions(s, f) {
		if strings.Contains(a.Title, "brand_new_key") {
			edit = a.Edit
		}
	}
	if edit == nil || len(edit.Create) == 0 {
		t.Fatal("expected create-loc")
	}
	for _, edits := range edit.Changes {
		if len(edits) > 0 && !strings.HasPrefix(edits[0].NewText, "\uFEFF") {
			t.Fatal("loc create must include BOM")
		}
	}

	s, root := buildSession(t, "ck3", map[string]string{"descriptor.mod": "name = \"t\"\n"}, nil, nil)
	f = filepath.Join(root, "descriptor.mod")
	found := false
	for _, it := range Complete(s, f, 0, 0) {
		found = found || it.Label == "dependencies"
	}
	if !found {
		t.Fatal("missing dependencies completion")
	}

	imm := "test.1 = {\n\timm"
	s, root = buildSession(t, "ck3", map[string]string{"events/x.txt": imm}, &catalog.VanillaCache{
		Structures: map[string][]string{"event": {"immediate", "option"}},
		Schema: &catalog.Schema{Effects: map[string]catalog.EngineToken{
			"immortal": {}, "immune": {},
		}},
		Vocabulary: []string{"immune_to", "immortal"},
	}, nil)
	f = filepath.Join(root, "events", "x.txt")
	line, col := lineCol(imm, "imm")
	if items := Complete(s, f, line, col+len("imm")); len(items) == 0 || items[0].Label != "immediate" {
		t.Fatalf("complete=%v", items)
	}
	vocab := make([]string, 5000)
	for i := range vocab {
		vocab[i] = "tok_" + string(rune('a'+i%26))
	}
	s, root = buildSession(t, "ck3", map[string]string{"events/x.txt": "test.1 = {\n\t"}, &catalog.VanillaCache{
		Structures: map[string][]string{"event": {"immediate", "option"}},
		Vocabulary: vocab,
	}, nil)
	f = filepath.Join(root, "events", "x.txt")
	items := Complete(s, f, 1, 1)
	if len(items) > 80 || len(items) == 0 || items[0].Label != "immediate" {
		t.Fatalf("complete len=%d first=%q", len(items), items[0].Label)
	}
}

func hasComplete(items []CompletionItem, label string) bool {
	for _, it := range items {
		if it.Label == label {
			return true
		}
	}
	return false
}

func TestCompleteSlots(t *testing.T) {
	cache := &catalog.VanillaCache{
		Structures: map[string][]string{"event": {"immediate", "option", "title"}},
		Schema: &catalog.Schema{
			Effects:  map[string]catalog.EngineToken{"add_gold": {}},
			Triggers: map[string]catalog.EngineToken{"is_adult": {}},
		},
		FieldInfo: map[string]string{"immediate": "runs first"},
		FireKeys:  map[string]string{"trigger_event": "event"},
	}
	vloc := &catalog.VanillaLoc{Sites: map[string]catalog.LocEntry{
		"test.1.t": {Value: "Hi"},
		"other":    {},
	}}

	imm := "test.1 = {\n\timmediate = {\n\t\t\n\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": imm}, cache, nil)
	f := filepath.Join(root, "events", "x.txt")
	line, _ := lineCol(imm, "immediate = {")
	items := Complete(s, f, line+1, 2)
	if !hasComplete(items, "add_gold") {
		t.Fatalf("immediate slot missing add_gold: %v", labels(items))
	}
	if hasComplete(items, "option") || hasComplete(items, "title") {
		t.Fatalf("immediate slot leaked structures: %v", labels(items))
	}

	trig := "test.1 = {\n\ttrigger = {\n\t\t\n\t}\n}\n"
	s, root = buildSession(t, "ck3", map[string]string{"events/x.txt": trig}, cache, nil)
	f = filepath.Join(root, "events", "x.txt")
	line, _ = lineCol(trig, "trigger = {")
	items = Complete(s, f, line+1, 2)
	if !hasComplete(items, "is_adult") {
		t.Fatalf("trigger slot missing is_adult: %v", labels(items))
	}
	if hasComplete(items, "add_gold") {
		t.Fatalf("trigger slot leaked effect: %v", labels(items))
	}

	title := "test.1 = {\n\ttitle = te\n}\n"
	s, root = buildSession(t, "ck3", map[string]string{"events/x.txt": title}, cache, vloc)
	f = filepath.Join(root, "events", "x.txt")
	line, col := lineCol(title, "title = te")
	items = Complete(s, f, line, col+len("title = te"))
	if !hasComplete(items, "test.1.t") || hasComplete(items, "other") {
		t.Fatalf("title value loc: %v", labels(items))
	}
	if hasComplete(items, "add_gold") {
		t.Fatalf("title value leaked effect: %v", labels(items))
	}

	fire := "test.1 = {\n\timmediate = { trigger_event = te }\n}\ntest.2 = { type = character_event }\n"
	s, root = buildSession(t, "ck3", map[string]string{"events/x.txt": fire}, cache, nil)
	f = filepath.Join(root, "events", "x.txt")
	line, col = lineCol(fire, "trigger_event = te")
	items = Complete(s, f, line, col+len("trigger_event = te"))
	if !hasComplete(items, "test.1") && !hasComplete(items, "test.2") {
		t.Fatalf("trigger_event value: %v", labels(items))
	}

	body := "test.1 = {\n\timm"
	s, root = buildSession(t, "ck3", map[string]string{"events/x.txt": body}, cache, nil)
	f = filepath.Join(root, "events", "x.txt")
	line, col = lineCol(body, "imm")
	items = Complete(s, f, line, col+len("imm"))
	if len(items) == 0 || items[0].Label != "immediate" {
		t.Fatalf("structure key: %v", labels(items))
	}
	if items[0].Documentation != "runs first" {
		t.Fatalf("FieldDoc: %#v", items[0].Documentation)
	}
	if items[0].InsertText != "immediate = " {
		t.Fatalf("scalar insert=%q", items[0].InsertText)
	}
	if items[0].Range.Start.Line != line {
		t.Fatalf("range=%v want line %d", items[0].Range, line)
	}

	theme := "test.1 = {\n\ttheme = s\n}\n"
	s, root = buildSession(t, "ck3", map[string]string{
		"events/x.txt":               theme,
		"common/event_themes/00.txt": "seduction = { }\n",
	}, &catalog.VanillaCache{
		FieldValueKinds: map[string]string{"theme": "event_themes"},
		Defs: []catalog.Def{
			{Kind: "event_themes", Key: "seduction", Path: "t.txt", Line: 0},
		},
	}, nil)
	f = filepath.Join(root, "events", "x.txt")
	line, col = lineCol(theme, "theme = s")
	items = Complete(s, f, line, col+len("theme = s"))
	if !hasComplete(items, "seduction") {
		t.Fatalf("theme value: %v", labels(items))
	}
	if hasComplete(items, "add_gold") {
		t.Fatalf("theme leaked effect: %v", labels(items))
	}
}

func TestCompleteEmptyPrefixAndFallback(t *testing.T) {
	cache := &catalog.VanillaCache{
		Structures: map[string][]string{"event": {"immediate", "option"}},
		Defs: []catalog.Def{
			{Kind: "trait", Key: "ambitious", Path: "t.txt", Line: 0},
		},
	}
	vloc := &catalog.VanillaLoc{Sites: map[string]catalog.LocEntry{
		"test.1.t": {Value: "Hi"},
	}}

	title := "test.1 = {\n\ttitle = \n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": title}, cache, vloc)
	f := filepath.Join(root, "events", "x.txt")
	line, col := lineCol(title, "title = ")
	items := Complete(s, f, line, col+len("title = "))
	if !hasComplete(items, "test.1.t") {
		t.Fatalf("empty title loc: %v", labels(items))
	}

	body := "test.1 = { \n}\n"
	s, root = buildSession(t, "ck3", map[string]string{"events/x.txt": body}, cache, nil)
	f = filepath.Join(root, "events", "x.txt")
	line, col = lineCol(body, "{ ")
	items = Complete(s, f, line, col+len("{ "))
	if !hasComplete(items, "immediate") {
		t.Fatalf("structure key after space: %v", labels(items))
	}

	unk := "test.1 = {\n\tunknown_field = \n}\n"
	s, root = buildSession(t, "ck3", map[string]string{"events/x.txt": unk}, cache, nil)
	f = filepath.Join(root, "events", "x.txt")
	line, col = lineCol(unk, "unknown_field = ")
	items = Complete(s, f, line, col+len("unknown_field = "))
	if len(items) == 0 || !hasComplete(items, "yes") {
		t.Fatalf("unknown field value: %v", labels(items))
	}
}

func TestCompleteRangeAndInsert(t *testing.T) {
	cache := &catalog.VanillaCache{
		Structures: map[string][]string{
			"event": {"title", "immediate", "rare_flag"},
		},
		StructureBlocks: map[string][]string{"event": {"immediate"}},
	}
	body := "test.1 = {\n\t\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, cache, nil)
	f := filepath.Join(root, "events", "x.txt")
	items := Complete(s, f, 1, 1)
	if len(items) < 3 || items[0].Label != "title" || items[2].Label != "rare_flag" {
		t.Fatalf("freq order: %v", labels(items))
	}
	var imm, title CompletionItem
	for _, it := range items {
		switch it.Label {
		case "immediate":
			imm = it
		case "title":
			title = it
		}
	}
	if imm.InsertText != "immediate = { $0 }" {
		t.Fatalf("block insert=%q", imm.InsertText)
	}
	if title.InsertText != "title = " {
		t.Fatalf("scalar insert=%q", title.InsertText)
	}
}

func TestCompleteDottedReplace(t *testing.T) {
	src := "namespace = hostile_scheme_discovery\n" +
		"hostile_scheme_discovery.3002 = { type = character_event }\n" +
		"hostile_scheme_discovery.1 = {\n" +
		"	trigger_event = hostile_scheme_discovery.3\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": src}, nil, nil)
	f := filepath.Join(root, "events", "x.txt")
	line, col := lineCol(src, "trigger_event = hostile_scheme_discovery.3")
	col += len("trigger_event = ")
	items := Complete(s, f, line, col+len("hostile_scheme_discovery.3"))
	var hit CompletionItem
	for _, it := range items {
		if it.Label == "hostile_scheme_discovery.3002" {
			hit = it
			break
		}
	}
	if hit.Label == "" {
		t.Fatalf("missing 3002: %v", labels(items))
	}
	if hit.Range.Start.Line != line || hit.Range.Start.Character != col {
		t.Fatalf("range start=%v want %d:%d", hit.Range.Start, line, col)
	}
	end := col + len("hostile_scheme_discovery.3")
	if hit.Range.End.Line != line || hit.Range.End.Character != end {
		t.Fatalf("range end=%v want %d:%d", hit.Range.End, line, end)
	}
}

func TestCompleteRootAndEnums(t *testing.T) {
	src := "script"
	s, root := buildSession(t, "ck3", map[string]string{
		"common/scripted_triggers/x.txt": src,
	}, &catalog.VanillaCache{}, nil)
	f := filepath.Join(root, "common", "scripted_triggers", "x.txt")
	items := Complete(s, f, 0, len("script"))
	if !hasComplete(items, "scripted_triggers") {
		t.Fatalf("root scripted_triggers: %v", labels(items))
	}

	cache := &catalog.VanillaCache{
		Structures: map[string][]string{"event": {"type", "immediate"}},
		FieldEnumsByKind: map[string]map[string][]string{
			"event": {"type": {"character_event", "letter_event"}},
		},
	}
	body := "test.1 = {\n\ttype = c\n}\n"
	s, root = buildSession(t, "ck3", map[string]string{"events/x.txt": body}, cache, nil)
	f = filepath.Join(root, "events", "x.txt")
	line, col := lineCol(body, "type = c")
	items = Complete(s, f, line, col+len("type = c"))
	if !hasComplete(items, "character_event") {
		t.Fatalf("type enum: %v", labels(items))
	}

	unk := "test.1 = {\n\tunknown_field = \n}\n"
	s, root = buildSession(t, "ck3", map[string]string{
		"events/x.txt": unk,
		"events/y.txt": "other.1 = { type = character_event }\n",
	}, cache, nil)
	f = filepath.Join(root, "events", "x.txt")
	line, col = lineCol(unk, "unknown_field = ")
	items = Complete(s, f, line, col+len("unknown_field = "))
	if !hasComplete(items, "yes") {
		t.Fatalf("unknown_field yes: %v", labels(items))
	}
	for _, it := range items {
		if it.Label != "yes" && it.Label != "no" {
			t.Fatalf("unknown_field dumped %q: %v", it.Label, labels(items))
		}
	}
}

// The game declares what a token takes: EU5 states
// "Supported Targets: culture_group" for has_culture_group. Completion must use
// that, and in the form the schema says the value is written — as `prefix:id`
// when the target type is a global link requiring data, bare otherwise.
func TestCompleteUsesDeclaredTarget(t *testing.T) {
	cache := &catalog.VanillaCache{
		Schema: &catalog.Schema{
			Triggers: map[string]catalog.EngineToken{
				"has_culture_group": {In: []string{"culture"}, Target: "culture_group"},
				"has_or_had_tag":    {In: []string{"country"}},
			},
			Links: map[string]catalog.ScopeLink{
				"culture_group": {Out: "culture_group", Global: true, Data: true},
			},
		},
		KindScope: map[string]string{"culture_groups": "culture_group"},
	}
	body := "potential = {\n\thas_culture_group = \n}\n"
	s, root := buildSession(t, "eu5", map[string]string{
		"in_game/common/scripts/x.txt": body,
		"in_game/common/culture_groups/00.txt": "turkic_group = { }\n" +
			"iranian_group = { }\n",
	}, cache, nil)
	f := filepath.Join(root, "in_game", "common", "scripts", "x.txt")

	labels := func(line, col int) []string {
		var out []string
		for _, it := range Complete(s, f, line, col) {
			out = append(out, it.Label)
		}
		return out
	}
	has := func(got []string, want string) bool {
		for _, g := range got {
			if g == want {
				return true
			}
		}
		return false
	}

	// Right after `= `, the whole citation is offered so one accept is valid.
	got := labels(1, len("\thas_culture_group = "))
	if !has(got, "culture_group:turkic_group") {
		t.Errorf("after `=` got %v, want culture_group:turkic_group", got)
	}
	if has(got, "yes") {
		t.Errorf("declared target must beat the yes/no fallback: %v", got)
	}
}

// Accepting a completion must replace what was typed, not append to it. The
// word scan stops at `:` while the lexer does not, so with `culture_group:tur`
// typed the replace range covered only `tur` and accepting the citation
// produced `culture_group:culture_group:turkic_group`.
func TestCompleteCitationReplacesWholeCite(t *testing.T) {
	cache := &catalog.VanillaCache{
		Schema: &catalog.Schema{
			Triggers: map[string]catalog.EngineToken{
				"has_culture_group": {In: []string{"culture"}, Target: "culture_group"},
			},
			Links: map[string]catalog.ScopeLink{
				"culture_group": {Out: "culture_group", Global: true, Data: true},
			},
		},
		KindScope: map[string]string{"culture_groups": "culture_group"},
	}
	const typed = "\thas_culture_group = culture_group:tur"
	body := "potential = {\n" + typed + "\n}\n"
	s, root := buildSession(t, "eu5", map[string]string{
		"in_game/common/scripts/x.txt":         body,
		"in_game/common/culture_groups/00.txt": "turkic_group = { }\n",
	}, cache, nil)
	f := filepath.Join(root, "in_game", "common", "scripts", "x.txt")

	items := Complete(s, f, 1, len(typed))
	var cite *CompletionItem
	for i, it := range items {
		if it.Label == "culture_group:turkic_group" {
			cite = &items[i]
		}
	}
	if cite == nil {
		t.Fatalf("no citation item; got %v", items)
	}
	// The replaced span must be the whole `culture_group:tur`, so applying the
	// label leaves exactly the label behind.
	startCol := strings.Index(typed, "culture_group:tur")
	if cite.Range.Start.Character != startCol || cite.Range.End.Character != len(typed) {
		t.Fatalf("range = %d..%d, want %d..%d (over %q)",
			cite.Range.Start.Character, cite.Range.End.Character, startCol, len(typed),
			"culture_group:tur")
	}
}

// Filtering after the limit returns whichever definitions the index reached
// first, which on a real workspace is almost never the kind asked for. Both
// value paths had it: completion fell through to `yes`/`no` even where the type
// was perfectly well known — 203 of Victoria 3's 496 unanswered value positions
// knew the field kind (`interest_groups`, `cultures`, `religions`) and got
// nothing back, and EU5's `has_culture_group = ` declared its target, bound it
// to `culture_groups`, and offered nothing.
//
// The fixture is the shape that breaks it: more definitions of an unrelated
// kind than the completion limit, with the wanted ones behind them.
func TestCompletionFiltersKindBeforeTheLimit(t *testing.T) {
	// The noise is in the workspace and the traits are in the install, because
	// eachDef walks every mod file before it reaches the cache. That ordering is
	// the one part of the iteration that is specified, so a search which takes
	// maxComplete definitions of any kind and only then keeps the matching ones
	// fills its budget on decisions and never sees a trait — deterministically,
	// rather than depending on Go's map order.
	var noise strings.Builder
	for i := range maxComplete * 4 {
		fmt.Fprintf(&noise, "aaa_decision_%03d = { }\n", i)
	}
	const ev = "ns.1 = {\n\timmediate = { add_trait = \n} }\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"common/decisions/00.txt": noise.String(),
		"events/x.txt":            ev,
	}, &catalog.VanillaCache{
		FieldValueKinds: map[string]string{"add_trait": "traits"},
		Defs: []catalog.Def{
			{Kind: "traits", Key: "zzz_brave", Path: "t.txt"},
			{Kind: "traits", Key: "zzz_craven", Path: "t.txt"},
		},
	}, nil)
	f := filepath.Join(root, "events", "x.txt")

	line, col := lineCol(ev, "add_trait = ")
	got := labels(Complete(s, f, line, col+len("add_trait = ")))
	if len(got) == 0 || got[0] == "yes" {
		t.Fatalf("add_trait fell through to the boolean fallback: %v", got)
	}
	for _, l := range got {
		if !strings.HasPrefix(l, "zzz_") {
			t.Fatalf("offered a non-trait %q: %v", l, got)
		}
	}
}

// TestValueSourceOrder pins completion's resolution order. It is the second
// place the declared-beats-derived invariant lives — hover's is in
// session/resolve.go — and the two drifting apart would make a slot complete
// differently from the way it hovers.
func TestValueSourceOrder(t *testing.T) {
	want := []string{
		// First: the text names the database outright, which beats any
		// inference about the field it sits in.
		"cited-prefix",
		"fire-key",
		"localization",
		"slot-declared",
		"target-declared",
		// The game stating a closed set in its own documentation. Declared, so
		// above the derived slot and the harvested enum; below the script_docs
		// sources, which name a database rather than a type word.
		"slot-declared-doc",
		"slot-derived",
		"macro-params",
		"macro-call-params",
		"field-enums",
	}
	if len(valueSources) != len(want) {
		t.Fatalf("source count = %d, want %d — add it to the list here too",
			len(valueSources), len(want))
	}
	for i, w := range want {
		if got := valueSources[i].name; got != w {
			t.Errorf("source %d = %q, want %q", i, got, w)
		}
	}
}

// TestValueDeclaredOutranksDerived is the invariant itself: both declared
// sources must be consulted before the install-derived field type. When
// target-declared sat below slot-derived, `has_culture_group = ` fell through
// to a derived type with too little evidence and offered yes/no.
func TestValueDeclaredOutranksDerived(t *testing.T) {
	at := func(name string) int {
		for i, s := range valueSources {
			if s.name == name {
				return i
			}
		}
		t.Fatalf("source %q missing", name)
		return -1
	}
	derived := at("slot-derived")
	for _, declared := range []string{"slot-declared", "target-declared"} {
		if i := at(declared); i > derived {
			t.Errorf("%s at %d ranks below slot-derived at %d", declared, i, derived)
		}
	}
}

// TestSnippetOffered covers the derived skeleton reaching completion: it is
// offered at the top level of a file, carries snippet placeholders, and ranks
// behind the names — someone part-way through a word wants the token, not a
// twelve-line block dropped into their file.
func TestSnippetOffered(t *testing.T) {
	c := &catalog.VanillaCache{
		Snippets: map[string][]catalog.SnippetMember{
			"traits": {
				{Key: "diplomacy", Hits: 10, Of: 10},
				{Key: "icon", Block: true, Hits: 9, Of: 10},
			},
		},
	}
	catalog.PrepareCache(c)
	s, root := buildSession(t, "ck3",
		map[string]string{"common/traits/00_traits.txt": "\n"}, c, nil)
	items := Complete(s, root+"/common/traits/00_traits.txt", 0, 0)

	var snip *CompletionItem
	for i := range items {
		if items[i].Kind == "snippet" {
			snip = &items[i]
		}
	}
	if snip == nil {
		t.Fatalf("no skeleton offered: %v", labels(items))
	}
	for _, want := range []string{"diplomacy = ${", "icon = {", "${1:", "$0"} {
		if !strings.Contains(snip.InsertText, want) {
			t.Errorf("insert text missing %q:\n%s", want, snip.InsertText)
		}
	}
}

// TestNoSnippetWithoutEvidence pins the floor reaching the user: a kind the
// install did not evidence a shape for offers nothing rather than a guess.
func TestNoSnippetWithoutEvidence(t *testing.T) {
	c := &catalog.VanillaCache{}
	catalog.PrepareCache(c)
	s, root := buildSession(t, "ck3",
		map[string]string{"common/traits/00_traits.txt": "\n"}, c, nil)
	for _, it := range Complete(s, root+"/common/traits/00_traits.txt", 0, 0) {
		if it.Kind == "snippet" {
			t.Errorf("skeleton offered with no derived shape: %q", it.Label)
		}
	}
}

// nestedCache declares what the install would have evidenced about two blocks.
func nestedCache() *catalog.VanillaCache {
	c := &catalog.VanillaCache{
		Structures: map[string][]string{
			"event": {"type", "title", "desc", "theme", "option", "trigger"},
		},
		NestedMembers: map[string][]string{
			"desc":           {"first_valid", "triggered_desc", "random_valid"},
			"first_valid":    {"triggered_desc", "desc"},
			"triggered_desc": {"trigger", "desc"},
		},
		AmbiguousFields: map[string]bool{"trigger": true, "desc": true},
		FieldInfo: map[string]string{
			"trigger": "controls visibility for this portrait",
			"type":    "Optional, defaults to character_event",
		},
	}
	catalog.PrepareCache(c)
	return c
}

const nestedEvent = "namespace = ns\n" +
	"ns.1 = {\n" +
	"\ttype = character_event\n" +
	"\tdesc = {\n" +
	"\t\tfirst_valid = {\n" +
	"\t\t\ttriggered_desc = {\n" +
	"\t\t\t\ttrigger = { is_ai = yes }\n" +
	"\t\t\t}\n" +
	"\t\t}\n" +
	"\t}\n" +
	"}\n"

// TestCompleteFollowsEnclosingBlock pins the fix for completion that offered an
// event's top-level keys at every depth. Verified against the running app: the
// three positions returned byte-identical lists before this.
func TestCompleteFollowsEnclosingBlock(t *testing.T) {
	s, root := buildSession(t, "ck3",
		map[string]string{"events/x.txt": nestedEvent}, nestedCache(), nil)
	p := root + "/events/x.txt"
	at := func(line, col int) []string {
		return labels(Complete(s, p, line, col))
	}
	cases := []struct {
		line, col     int
		want, notWant string
	}{
		{2, 1, "type", "first_valid"},      // event top level
		{4, 2, "first_valid", "option"},    // inside desc
		{5, 3, "triggered_desc", "option"}, // inside first_valid
		{6, 4, "trigger", "option"},        // inside triggered_desc
	}
	for _, c := range cases {
		got := at(c.line, c.col)
		if !has(Complete(s, p, c.line, c.col), c.want) {
			t.Errorf("line %d: missing %q, got %v", c.line, c.want, got)
		}
		if has(Complete(s, p, c.line, c.col), c.notWant) {
			t.Errorf("line %d: %q cannot go here, got %v", c.line, c.notWant, got)
		}
	}
}

// TestFieldDocWithheldWhenAmbiguousAndNested pins the other half. The install
// documents a field once, by name; 819 CK3 names are used in more than one kind
// of block, so showing that description below the definition tells a reader
// something false. At the definition's own level it is exactly right.
func TestFieldDocWithheldWhenAmbiguousAndNested(t *testing.T) {
	s, root := buildSession(t, "ck3",
		map[string]string{"events/x.txt": nestedEvent}, nestedCache(), nil)
	p := root + "/events/x.txt"

	deep := Hover(s, p, 6, 5) // trigger inside triggered_desc
	if deep != nil && strings.Contains(deep.Docs, "portrait") {
		t.Errorf("portrait description shown for a nested trigger: %q", deep.Docs)
	}
	top := Hover(s, p, 2, 2) // type at the event's top level
	if top == nil || !strings.Contains(top.Docs, "character_event") {
		t.Errorf("top-level field lost its documentation: %+v", top)
	}
}

// TestCompleteWidensRatherThanEmpties pins the three properties that have to
// hold together, and that took three attempts to get simultaneously:
//
//  1. a well-evidenced block offers only its own members;
//  2. a prefix matching none of them still offers the vocabulary;
//  3. widening never leaks into case 1.
//
// The middle attempt fell back to the KIND's keys when the block's members did
// not match, which answered with a near-miss from the wrong set — offering
// `current_description` to someone four characters into `current_year` — and
// stopped the caller ever reaching the vocabulary.
func TestCompleteWidensRatherThanEmpties(t *testing.T) {
	c := &catalog.VanillaCache{
		Structures: map[string][]string{
			"decisions": {"is_shown", "effect", "current_description"},
		},
		NestedMembers: map[string][]string{"is_shown": {"always", "exists", "not"}},
		Vocabulary:    []string{"current_year", "current_description", "is_adult"},
	}
	catalog.PrepareCache(c)
	body := "d = {\n\tis_shown = {\n\t\talways = yes\n\t}\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"common/decisions/d.txt": body}, c, nil)
	p := root + "/common/decisions/d.txt"

	// Browsing inside the block: its own members, and nothing from the kind.
	browse := labels(Complete(s, p, 2, 2))
	if !has(Complete(s, p, 2, 2), "always") {
		t.Errorf("block members missing: %v", browse)
	}
	for _, wrong := range []string{"is_shown", "current_description"} {
		if has(Complete(s, p, 2, 2), wrong) {
			t.Errorf("%q is a key of the kind, not of this block: %v", wrong, browse)
		}
	}
}

// TestCompleteCitedPrefix pins the source that reads the database out of the
// text. `this = title:k_england` says which rows it wants whatever the field's
// declared type is or is not, and every other source offers bare ids that a
// typed `title:` can never match — which left half of all value positions in a
// real mod completing to nothing.
func TestCompleteCitedPrefix(t *testing.T) {
	c := &catalog.VanillaCache{
		PrefixKinds: map[string]string{"title": "titles"},
		Defs: []catalog.Def{
			{Kind: "titles", Key: "k_england"}, {Kind: "titles", Key: "k_essex"},
			{Kind: "titles", Key: "d_wessex"},
		},
	}
	catalog.PrepareCache(c)
	body := "a = {\n\tthis = title:k_e\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"common/scripted_effects/e.txt": body}, c, nil)
	// Column 16 is the last character of the value; 17 is past the token and
	// there is no word to complete.
	items := Complete(s, root+"/common/scripted_effects/e.txt", 1, 16)
	got := labels(items)
	if !has(items, "title:k_england") || !has(items, "title:k_essex") {
		t.Errorf("cited titles not offered: %v", got)
	}
	// Offered in the form the reader is typing, so one accept is valid script.
	for _, l := range got {
		if !strings.HasPrefix(l, "title:") {
			t.Errorf("bare id offered where a citation was typed: %v", got)
			break
		}
	}
	// A prefix the model knows nothing about must not invent rows.
	body2 := "a = {\n\tthis = nosuch:k_e\n}\n"
	s2, root2 := buildSession(t, "ck3",
		map[string]string{"common/scripted_effects/e.txt": body2}, c, nil)
	for _, l := range labels(Complete(s2, root2+"/common/scripted_effects/e.txt", 1, 17)) {
		if strings.HasPrefix(l, "nosuch:") {
			t.Errorf("unbound prefix produced rows: %q", l)
		}
	}
}

// TestParameterlessMacroDoesNotSwallowSlot pins the line between a source that
// may claim a slot on its condition and one that may not.
//
// "The game says this slot holds kind X and nothing of kind X matches" is a real
// answer and must stop the search. "This macro declares no parameters" is not an
// answer to what value belongs here — and a scripted trigger invoked as
// `x = yes` has none, so claiming the slot swallowed the boolean it takes. That
// was 17 of 24 empty value positions on a real Victoria 3 mod.
func TestParameterlessMacroDoesNotSwallowSlot(t *testing.T) {
	s, root := buildSession(t, "vic3", map[string]string{
		"common/scripted_triggers/t.txt": "has_cholera_treatment_trigger = {\n\talways = yes\n}\n",
		"common/decisions/d.txt":         "d = {\n\tis_shown = {\n\t\thas_cholera_treatment_trigger = y\n\t}\n}\n",
	}, nil, nil)
	items := Complete(s, root+"/common/decisions/d.txt", 2, 33)
	if !has(items, "yes") {
		t.Errorf("a parameterless macro swallowed the slot: %v", labels(items))
	}
}
