package lsp

import (
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
	"slices"
)

// wordResolver is one named source that may vouch for a word.
type wordResolver struct {
	// name identifies this step in the fixed order tests pin below.
	name string
	// resolve reports the identity this source can vouch for. Returning false
	// means "I have nothing to say", never "nothing is there" — the next step
	// gets its turn. Only unresolved, last, answers for everything.
	resolve func(s *session.Session, path string, at probe) (identity, bool)
}

var wordResolvers = []wordResolver{
	{"loc-file", resolveLocFile},
	{"scope-ref", resolveScopeRef},
	{"save-site", resolveSaveSite},
	{"param-span", resolveParamSpan},
	{"param-of-macro", resolveParamOfMacro},
	{"macro-call", macroCallIdentity},
	{"assign-saved-scope", resolveAssignSavedScope},
	{"local-def", resolveLocalDef},
	{"weight", resolveWeight},
	{"dotted-chain", resolveDottedChain},
	{"declared-key", resolveDeclaredKey},
	{"field-key", resolveFieldKey},
	{"no-word", resolveNoWord},
	{"literal", resolveLiteral},
	{"language-prefix", resolveLanguagePrefix},
	{"typed-cite", resolveTypedCite},
	{"slot-declared", resolveSlotDeclared},
	{"slot-derived", resolveSlotDerived},
	{"definition", resolveDefinition},
	{"vocabulary", resolveVocabulary},
	{"localization", resolveLocalization},
	{"data-function", resolveDataFunction},
	{"macro-kind", resolveMacroKind},
	{"field-doc", resolveFieldDoc},
	{"unresolved", resolveUnresolved},
}

// resolveLocFile answers everything in a localization file: the key, an
// interpolated key inside a value, or an engine value like `$GOLD|=+$`.
func resolveLocFile(s *session.Session, path string, at probe) (identity, bool) {
	if s.KindFor(path) != "loc" {
		return identity{}, false
	}
	hit, ok := locHitAt(at.src, at.off)
	if !ok {
		return identity{path: path}, true
	}
	if hit.engine {
		return identity{
			path: path, name: hit.key, kind: "loc_value", locFilter: hit.filter,
		}, true
	}
	id := identity{
		path: path, name: hit.key, kind: "loc_key",
		spanStart: hit.start, spanEnd: hit.end,
	}
	if file, ln, _, origin, ok := s.LocSite(hit.key); ok {
		id.def = &derived.Def{
			Kind: "loc_key", Key: hit.key, Path: file, Line: ln, Origin: origin,
		}
	}
	return id, true
}

// resolveScopeRef claims `scope:name` written out in the text.
func resolveScopeRef(s *session.Session, path string, at probe) (identity, bool) {
	name, ok := scopeRefAt(at)
	if !ok {
		return identity{}, false
	}
	return ephemeralID(s, path, name, "saved_scope"), true
}

// resolveSaveSite claims the name at a `save_scope_as = x` site.
func resolveSaveSite(s *session.Session, path string, at probe) (identity, bool) {
	if !at.saveOK {
		return identity{}, false
	}
	return ephemeralID(s, path, at.saveName, "saved_scope"), true
}

// resolveParamSpan claims `$PARAM$`; a macro's own parameter has no definition
// outside its body, so one is synthesised at the span when the index has none.
func resolveParamSpan(s *session.Session, path string, at probe) (identity, bool) {
	if !at.param {
		return identity{}, false
	}
	d := s.ResolveOfKindOwner(at.word, "script_param", at.paramOwner)
	if d == nil {
		origin, _, _ := s.Locate(path)
		d = &derived.Def{
			Kind: "script_param", Key: at.word, Path: path, Origin: origin,
			Start: at.start, End: at.end, OwnerKey: at.paramOwner,
			Line: at.res.Lines().PositionAt(at.start).Line,
		}
	}
	return identity{
		path: path, name: at.word, kind: "script_param", def: d, owner: at.paramOwner,
	}, true
}

// resolveParamOfMacro claims a bare word that is a parameter of the macro the
// cursor sits inside.
func resolveParamOfMacro(s *session.Session, path string, at probe) (identity, bool) {
	if at.paramOwner == "" {
		return identity{}, false
	}
	d := s.ResolveOfKindOwner(at.word, "script_param", at.paramOwner)
	if d == nil {
		return identity{}, false
	}
	return identity{
		path: path, name: at.word, kind: "script_param", def: d, owner: at.paramOwner,
	}, true
}

// resolveAssignSavedScope claims `scope:x = { … }` written as a key.
func resolveAssignSavedScope(s *session.Session, path string, at probe) (identity, bool) {
	if at.assign == nil {
		return identity{}, false
	}
	p, ok := jomini.ParsePrefixed(at.assign.Key.Text)
	if !ok || !jomini.IsSavedScopePrefix(p) {
		return identity{}, false
	}
	return ephemeralID(s, path, p.Name, "saved_scope"), true
}

// resolveLocalDef claims a key in a file whose own rows are the definitions.
func resolveLocalDef(s *session.Session, path string, at probe) (identity, bool) {
	a := at.assign
	if a == nil || !s.IsLocalDefFile(path, at.kind) {
		return identity{}, false
	}
	return identity{
		path: path, name: a.Key.Text, kind: at.kind, local: true,
		extractKind: at.kind,
		assignStart: a.Key.Range.Start, assignEnd: a.Key.Range.End,
		assignLine: at.res.Lines().PositionAt(a.Key.Range.Start).Line,
	}, true
}

// resolveWeight claims a bare number on the left of an `=`: a branch of a distribution, not a name — CK3 keys map positions by province id, so `95` also resolves to a map_data row and the card offered "Amiens" for a probability.
// Numeric but not a distribution — Vic3's `20 = empty` is an index — stops rather than answering, which is what the wrong answer was.
func resolveWeight(_ *session.Session, path string, at probe) (identity, bool) {
	if at.assign == nil || !isWeightKey(at.assign.Key.Text) {
		return identity{}, false
	}
	key := at.assign.Key.Text
	if share := weightShare(at.slotAssign, at.slotKey, key); share != "" {
		return identity{path: path, name: key, kind: "weight", docs: share}, true
	}
	return identity{path: path}, true
}

// resolveDottedChain names the segment under the cursor of a chain key: `holder.culture = { … }` is two hops, and the whole string resolves to nothing.
func resolveDottedChain(s *session.Session, path string, at probe) (identity, bool) {
	a := at.assign
	if a == nil {
		return identity{}, false
	}
	seg, ok := dottedSegmentAt(a.Key.Text, at.off-a.Key.Range.Start)
	if !ok {
		return identity{}, false
	}
	return segmentIdentity(s, path, seg)
}

// resolveDeclaredKey claims a key the type system names — a declared effect or
// trigger keeps its own card rather than becoming a field of the block.
func resolveDeclaredKey(s *session.Session, path string, at probe) (identity, bool) {
	if at.assign == nil || !s.Install.Declared.HasName(at.assign.Key.Text) {
		return identity{}, false
	}
	ck := s.VocabRole(at.assign.Key.Text)
	if ck == "" {
		return identity{}, false
	}
	return identity{path: path, name: at.assign.Key.Text, kind: ck}, true
}

// resolveFieldKey claims a key of the object being edited: documented, a member of the cell, or a localization property.
// A key naming a scripted macro is an invocation of it, never a field, whatever the cell at that position holds.
func resolveFieldKey(s *session.Session, path string, at probe) (identity, bool) {
	a := at.assign
	if a == nil || s.Work.Schema.MacroDefs[a.Key.Text] != "" {
		return identity{}, false
	}
	block := nestedBlock(at)
	docs := s.KindFieldDoc(a.Key.Text, at.kind, blockChain(block))
	// The structure's members, not this one position's: a key the game's own
	// readme lists for `select_trigger` is a member of it wherever it is
	// written, whether or not vanilla ever wrote one there.
	if docs == "" && !s.IsStructureMember(at.fileKind, at.path, a.Key.Text) &&
		loc.Classify(a.Key.Text) == loc.PropNone {
		return identity{}, false
	}
	return identity{
		path: path, name: a.Key.Text, kind: at.kind, fieldKey: true,
		extractKind: at.kind, block: block, cell: at.path,
	}, true
}

// resolveNoWord stops once the key resolvers have passed and there is no word.
func resolveNoWord(_ *session.Session, path string, at probe) (identity, bool) {
	if at.word != "" {
		return identity{}, false
	}
	return identity{path: path}, true
}

// resolveLiteral stops on a number or a quoted string, with one exception where a person declared it: a total conversion offsets its years so the engine's arithmetic works, and 8209.2.1 is read by a human who would otherwise subtract 7,782 in their head.
func resolveLiteral(s *session.Session, path string, at probe) (identity, bool) {
	if !jomini.IsLiteralValue(at.word) {
		return identity{}, false
	}
	if era := derived.EraDate(at.word, s.CalendarFor(path)); era != "" {
		return identity{path: path, name: at.word, kind: "date", docs: era}, true
	}
	return identity{path: path}, true
}

// resolveLanguagePrefix claims `scope:x` / `var:x` — a value that lives only for
// the duration of a script run, never a row in a database.
func resolveLanguagePrefix(s *session.Session, path string, at probe) (identity, bool) {
	p, ok := jomini.ParsePrefixed(at.word)
	if !ok {
		return identity{}, false
	}
	kind := jomini.PrefixKind(p.Prefix)
	if kind == "" {
		kind = p.Prefix
	}
	return ephemeralID(s, path, p.Name, kind), true
}

// resolveTypedCite claims `culture:english` — the game naming its own database
// in the text, which is as declared as a reference gets.
func resolveTypedCite(s *session.Session, path string, at probe) (identity, bool) {
	sp, ok := jomini.TypedSpanAt(at.word, at.off-at.start)
	if !ok {
		return identity{}, false
	}
	kind, id, ok := game.ParseTyped(s.GameID, sp.Prefix+":"+sp.ID)
	if !ok {
		kind, id = sp.Prefix, sp.ID
	}
	if pk := s.PrefixKind(sp.Prefix); pk != "" {
		kind = pk
	} else if kind == "" {
		kind = sp.Prefix
	}
	d := s.ResolveOfKind(id, kind)
	if d == nil {
		d = s.ResolveNonLoc(id)
	}
	return identity{path: path, name: id, kind: kind, def: d}, true
}

// resolveSlotDeclared types a value from what script_docs say the slot holds.
func resolveSlotDeclared(s *session.Session, path string, at probe) (identity, bool) {
	if at.slotKey == "" {
		return identity{}, false
	}
	r, ok := jomini.ScriptName(at.slotKey)
	if !ok {
		return identity{}, false
	}
	if jomini.IsEphemeral(r.Kind) {
		return ephemeralID(s, path, at.word, r.Kind), true
	}
	if d := s.ResolveOfKind(at.word, r.Kind); d != nil {
		return identity{path: path, name: at.word, kind: r.Kind, def: d}, true
	}
	return identity{}, false
}

// resolveSlotDerived types a value from the field type PMT inferred from the
// install. Below resolveSlotDeclared because it is evidence, not a declaration:
// it carries coverage floors and is silent wherever they are not met.
func resolveSlotDerived(s *session.Session, path string, at probe) (identity, bool) {
	if at.slotKey == "" {
		return identity{}, false
	}
	k := s.FieldValueKind(at.fileKind, at.path, at.slotKey)
	if k == "" {
		return identity{}, false
	}
	if jomini.IsEphemeral(k) {
		return ephemeralID(s, path, at.word, k), true
	}
	// A localization slot holds a key, not an object of the field's type.
	if loc.Classify(at.slotKey) != loc.PropNone {
		return identity{}, false
	}
	if d := s.ResolveOfKind(at.word, k); d != nil {
		return identity{path: path, name: at.word, kind: k, def: d}, true
	}
	return identity{}, false
}

// resolveDefinition names a word that is a definition somewhere in the model.
//
// ResolveNonLoc, not Resolve: every localization entry is a `loc_key` definition
// in the index and Resolve's default filter drops only ephemerals. Every object
// in these games has a key named after it, so a value naming an object matched
// both, and load order decided which came back.
func resolveDefinition(s *session.Session, path string, at probe) (identity, bool) {
	d := s.ResolveNonLoc(at.word)
	if d == nil {
		return identity{}, false
	}
	return identity{path: path, name: d.Key, kind: d.Kind, def: d}, true
}

// resolveVocabulary names a word the type system declares — an effect, a
// trigger, a scope link. Above localization because Paradox localizes a great
// many ordinary words: with loc first, hovering a declared effect showed the
// player-facing string in quotes instead of the token's own documentation.
func resolveVocabulary(s *session.Session, _ string, at probe) (identity, bool) {
	ck := s.VocabRole(at.word)
	if ck == "" {
		return identity{}, false
	}
	return identity{name: at.word, kind: ck}, true
}

// resolveLocalization names a word that is a loc key.
//
// Never on the left of an `=`. A key is a property name — a field, an effect, a
// check macro — and the games localize a great many of those words, so an
// unrecognised key came back as a localization entry and the card showed the
// player-facing string. Not knowing what a key is is the honest answer;
// claiming it is loc is not. Localization files go through identifyLoc.
func resolveLocalization(s *session.Session, path string, at probe) (identity, bool) {
	if at.inKey || !s.LocDefined(at.word) {
		return identity{}, false
	}
	file, ln, _, origin, ok := s.LocSite(at.word)
	if !ok {
		return identity{}, false
	}
	d := &derived.Def{
		Kind: "loc_key", Key: at.word, Path: file, Line: ln, Origin: origin,
	}
	return identity{path: path, name: at.word, kind: "loc_key", def: d}, true
}

func resolveDataFunction(s *session.Session, path string, at probe) (identity, bool) {
	if slices.Contains(s.Vocab("datafunction"), at.word) {
		return identity{path: path, name: at.word, kind: "data_function"}, true
	}
	return identity{}, false
}

func resolveMacroKind(s *session.Session, path string, at probe) (identity, bool) {
	ck := jomini.CanonicalKind(at.word)
	if !jomini.IsMacroKind(ck) {
		return identity{}, false
	}
	return identity{path: path, name: at.word, kind: ck}, true
}

// resolveFieldDoc is prose and nothing else: the word is documented as a field
// somewhere, which is enough to say "field" and show the documentation, but not
// enough to say what it points at.
func resolveFieldDoc(s *session.Session, path string, at probe) (identity, bool) {
	if s.FieldDoc(at.word, "", nil) == "" {
		return identity{}, false
	}
	return identity{path: path, name: at.word, kind: "field"}, true
}

// resolveUnresolved is the floor: the word is named but nothing is claimed
// about it. Answering here is what stops a card inventing a kind — the key
// holding it is carried so the card can say what the value is FOR without
// pretending to know what it is.
func resolveUnresolved(_ *session.Session, path string, at probe) (identity, bool) {
	if at.word == "" {
		return identity{}, false
	}
	id := identity{path: path, name: at.word}
	if !at.inKey && at.slotKey != "" {
		id.slot, id.extractKind, id.cell, id.block = at.slotKey, at.kind, at.path, nestedBlock(at)
	}
	return id, true
}
