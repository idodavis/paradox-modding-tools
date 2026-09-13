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
	// name appears on the identity, so a wrong card says which step named it.
	name string
	// resolve reports the identity this source can vouch for. Returning false
	// means "I have nothing to say", never "nothing is there" — the next step
	// gets its turn. Only unresolved, last, answers for everything.
	resolve func(s *session.Session, path string, at probe) (identity, bool)
}

var wordResolvers = []wordResolver{
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

// resolveWord runs the order and stamps the winner's name onto the identity.
func resolveWord(s *session.Session, path string, at probe) (identity, bool) {
	for _, r := range wordResolvers {
		if id, ok := r.resolve(s, path, at); ok {
			id.via = r.name
			return id, true
		}
	}
	return identity{}, false
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
	k := s.FieldValueKind(at.slotKey)
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
	if s.FieldDoc(at.word, "") == "" {
		return identity{}, false
	}
	return identity{path: path, name: at.word, kind: "field"}, true
}

// resolveUnresolved is the floor: the word is named but nothing is claimed
// about it. Answering here is what stops a card inventing a kind.
func resolveUnresolved(_ *session.Session, path string, at probe) (identity, bool) {
	if at.word == "" {
		return identity{}, false
	}
	return identity{path: path, name: at.word}, true
}
