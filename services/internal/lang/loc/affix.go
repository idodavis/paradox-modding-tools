// affix.go derives how a game names its localization keys, and reads those conventions back.

package loc

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// Affix is one derived localization naming convention for a kind: the key
// for a definition of that kind is Pre + <def id> + Suf. Both halves may be
// empty, which is the convention that a definition's key is simply its id.
type Affix struct {
	Pre  string `json:"pre,omitempty"`
	Suf  string `json:"suf,omitempty"`
	Defs int    `json:"defs"` // definitions of the kind that carry this key
	Of   int    `json:"of"`   // definitions of the kind in the install
}

// Coverage is the share of the kind's definitions carrying this key, percent.
//
// Consumers pick their own floor, because the two questions asked of a
// convention are not equally demanding: deciding a key is not orphaned only
// needs the convention to be plausible, while telling a modder a key is missing
// asserts the game requires it.
func (a Affix) Coverage() int {
	if a.Of == 0 {
		return 0
	}
	return a.Defs * 100 / a.Of
}

// Key applies the convention to a definition id.
func (a Affix) Key(id string) string { return a.Pre + id + a.Suf }

// The affix floors are EXPORTED, unlike every other tuning constant here.
const (
	// MinAffixDefs is how many definitions must share a shape before it is a
	// convention rather than a coincidence. Same rule as minFieldHits and
	// minOptionRefs: a ratio with no floor lets one accident decide.
	MinAffixDefs = 8
	// MinAffixCoverage is the share of a kind that must carry it.
	MinAffixCoverage   = 20
	MinAffixDefsStrong = 50
	// MaxAffixesPerKind bounds what one kind contributes. State regions produce
	// a convention per hub type and accessories one per portrait group, so the
	// cap has to clear a real fan-out before it starts discarding evidence.
	MaxAffixesPerKind = 16
	// affixKeyTokens caps span enumeration per key. Spans are quadratic in
	// tokens and a twenty-token key is a sentence, not a naming convention.
	affixKeyTokens = 10

	// ConventionUsed is the floor for "the engine may consume this key" —
	// what suppressing a false "orphaned" row needs.
	ConventionUsed     = MinAffixCoverage
	ConventionStored   = 90
	ConventionRequired = 100
)

// ConventionKeys expands a kind's derived conventions for one definition,
// keeping those that hold for at least minCoverage percent of the kind.
func ConventionKeys(id string, affixes []Affix, minCoverage int) []string {
	if id == "" || len(affixes) == 0 {
		return nil
	}
	out := make([]string, 0, len(affixes))
	for _, a := range affixes {
		if a.Coverage() >= minCoverage {
			out = append(out, a.Key(id))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ConventionOwner reports the definition a convention key names: the key
// decomposes as <pre><id><suf> where pre/suf is a convention of a kind that
// defines id. eachDefKind visits the kinds that define a given id.
//
// This is the reverse of ConventionKeys and it is a lookup rather than stored
// references on purpose. Expanding forwards would mean storing one reference
// per definition per convention — hundreds of thousands on a large install — to
// answer a question that is only ever asked of the few keys that look orphaned.
// Cheap discriminator first: decide before you materialise.
//
// eachDefKind is a visitor rather than a `[]string` return because it is called
// for every span of every candidate key. Handing back a slice would allocate
// once per definition found, on a path that runs tens of thousands of times
// during one Workspace Health pass.
func ConventionOwner(
	affixes map[string][]Affix, key string,
	eachDefKind func(id string, visit func(kind string) bool),
) (id, kind string, ok bool) {
	if key == "" || len(affixes) == 0 {
		return "", "", false
	}
	// A key past the token cap is rejected anyway, so the offsets of one that is
	// not fit in a fixed buffer and cost nothing.
	var buf [affixKeyTokens + 2]int
	offs := tokenOffsets(key, buf[:0])
	if len(offs) > affixKeyTokens+1 {
		return "", "", false
	}
	// One closure for the whole call, reading pre/suf/hit rather than capturing
	// them fresh. Built inside the span loop it was allocated once per span —
	// twenty-odd allocations to answer one key, on a path that runs for every
	// key that survived every other orphan filter.
	var pre, suf, hit string
	visit := func(k string) bool {
		for _, a := range affixes[jomini.CanonicalKind(k)] {
			if a.Pre == pre && a.Suf == suf {
				hit = k
				return false
			}
		}
		return true
	}
	for n := len(offs) - 1; n > 0; n-- {
		for i := 0; i+n < len(offs); i++ {
			j := i + n
			span := key[offs[i] : offs[j]-1]
			pre, suf, hit = key[:offs[i]], key[offs[j]-1:], ""
			eachDefKind(span, visit)
			if hit != "" {
				return span, hit, true
			}
		}
	}
	return "", "", false
}

// AffixesFrom is the derivation itself: given names that belong to a kind, and
// how many names each kind has, find the prefix/suffix shapes that turn enough
// of them into real localization keys.
// KeyAffix is a loc key shape derived from another loc key rather than from
// a definition. EU5 writes NOT_<key> for the negated form of a trigger tooltip,
// so NOT_POP_LITERACY_TRIGGER is consumed by the engine even though nothing
// cites it and no definition is named POP_LITERACY_TRIGGER — the stem is
// itself a key. Vanilla EU5 has 69,565 keys reading as orphaned and this family
// is its largest cluster; see GAME-SYNTAX §12.
type KeyAffix struct {
	Pre string `json:"pre,omitempty"`
	Suf string `json:"suf,omitempty"`
	// Keys carrying this shape whose stem is itself a loc key, out of Of, every
	// key carrying it. The ratio is the evidence: a real derivation resolves
	// nearly always, a coincidence resolves sometimes.
	Keys int `json:"keys"`
	Of   int `json:"of"`
}

// MaxAffixStrip bounds how many whole tokens may be taken off each end when
// looking for a stem. Two covers OTHER_PERFORMS_<key> and <key>_option_desc;
// going wider costs a multiple of the key set per token for shapes no game was
// observed to use.
const MaxAffixStrip = 2

// DeriveKeyAffixes finds the shapes that build one loc key out of another.
//
// Linear in the key set rather than quadratic like AffixesFrom: the stem of a
// derived key is the key minus whole tokens off each end, so a bounded strip
// finds it directly and there is no need to enumerate every span. Both counts
// are collected here and the floor is applied in acceptKeyAffix, so the
// evidence and the judgement stay separable.
func DeriveKeyAffixes(locKeys map[string]bool) []KeyAffix {
	if len(locKeys) == 0 {
		return nil
	}
	type shape struct{ pre, suf string }
	hits := map[shape]int{}
	total := map[shape]int{}
	var offs []int
	for key := range locKeys {
		offs = tokenOffsets(key, offs[:0])
		n := len(offs) - 1 // tokens in key
		if n < 2 || len(offs) > affixKeyTokens+1 {
			continue
		}
		for pre := 0; pre <= MaxAffixStrip && pre < n; pre++ {
			for suf := 0; suf+pre <= MaxAffixStrip && pre+suf < n; suf++ {
				if pre == 0 && suf == 0 {
					continue
				}
				lo, hi := offs[pre], offs[n-suf]
				sh := shape{key[:lo], key[hi-1:]}
				total[sh]++
				if locKeys[key[lo:hi-1]] {
					hits[sh]++
				}
			}
		}
	}
	out := make([]KeyAffix, 0, 16)
	for sh, of := range total {
		a := KeyAffix{Pre: sh.pre, Suf: sh.suf, Keys: hits[sh], Of: of}
		if acceptKeyAffix(a) {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b KeyAffix) int {
		if c := cmp.Compare(b.Keys, a.Keys); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Pre, b.Pre); c != 0 {
			return c
		}
		return cmp.Compare(a.Suf, b.Suf)
	})
	return out
}

const (
	// MinKeyAffixKeys is how many keys must resolve through a shape before it
	// counts as a convention. Higher than MinAffixDefs because the denominator
	// here is the whole key set rather than one kind's definitions, so
	// coincidences are correspondingly easier to find: CK3 has 287,896 english
	// keys, and 29,647 distinct prefix shapes among the unexplained ones alone.
	MinKeyAffixKeys = 20
	// MinKeyAffixCoverage is the share of keys carrying a shape whose stem is
	// itself a key. This is the evidence that does not scale with volume: a real
	// derivation resolves nearly always — every NOT_<key> has its <key> — while
	// a coincidental shape like _desc resolves only where the stem happens to
	// exist as well. Set high on purpose. Under-reporting an orphan costs a
	// modder some dead lines; a shape accepted here explains keys away, and the
	// cheapest way to lose a real finding is to accept a shape that half works.
	MinKeyAffixCoverage = 80
)

// acceptKeyAffix decides whether one derived shape is real rather than a
// coincidence. Kept separate from the counting in DeriveKeyAffixes so the
// evidence and the judgement can be recalibrated independently — the ratio is
// the discriminator, and the count floor stops one lucky pair clearing it the
// way body_part cleared 50% coverage on 1 of 2 values.
func acceptKeyAffix(a KeyAffix) bool {
	if a.Keys < MinKeyAffixKeys || a.Of == 0 {
		return false
	}
	return a.Keys*100 >= a.Of*MinKeyAffixCoverage
}

// KeyAffixOwner reports the loc key that key is derived from, if any. isKey is
// asked last because it is the only part that touches the session's own keys.
func KeyAffixOwner(
	affixes []KeyAffix, key string, isKey func(string) bool,
) (stem string, ok bool) {
	for _, a := range affixes {
		if len(key) <= len(a.Pre)+len(a.Suf) {
			continue
		}
		if !strings.HasPrefix(key, a.Pre) || !strings.HasSuffix(key, a.Suf) {
			continue
		}
		s := key[len(a.Pre) : len(key)-len(a.Suf)]
		if isKey(s) {
			return s, true
		}
	}
	return "", false
}

func AffixesFrom(
	kindsOf map[string][]string, total map[string]int, locKeys map[string]bool,
) map[string][]Affix {
	if len(kindsOf) == 0 {
		return nil
	}
	// Cheap discriminator first: a span can only be one of these names if its
	// first token starts one. Without this, every key pays for every span of
	// itself.
	firstTok := make(map[string]bool, len(kindsOf))
	for id := range kindsOf {
		if i := strings.IndexByte(id, '_'); i > 0 {
			firstTok[id[:i]] = true
		} else {
			firstTok[id] = true
		}
	}

	// One count, not a set of ids: pre+id+suf reconstructs the key, so a given
	// key can contribute to one convention at most once.
	type candidate struct{ kind, pre, suf string }
	hits := map[candidate]int{}
	var offs []int
	for key := range locKeys {
		offs = tokenOffsets(key, offs[:0])
		if len(offs) > affixKeyTokens+1 {
			continue
		}
		for i := 0; i+1 < len(offs); i++ {
			if !firstTok[key[offs[i]:offs[i+1]-1]] {
				continue
			}
			for j := len(offs) - 1; j > i; j-- {
				span := key[offs[i] : offs[j]-1]
				kinds := kindsOf[span]
				if len(kinds) == 0 {
					continue
				}
				pre, suf := key[:offs[i]], key[offs[j]-1:]
				for _, k := range kinds {
					hits[candidate{k, pre, suf}]++
				}
			}
		}
	}

	out := map[string][]Affix{}
	for c, n := range hits {
		of := total[c.kind]
		if n < MinAffixDefs || of == 0 {
			continue
		}
		if n*100 < of*MinAffixCoverage && n < MinAffixDefsStrong {
			continue
		}
		out[c.kind] = append(out[c.kind], Affix{Pre: c.pre, Suf: c.suf, Defs: n, Of: of})
	}
	for kind, list := range out {
		slices.SortFunc(list, func(a, b Affix) int {
			if c := cmp.Compare(b.Defs, a.Defs); c != 0 {
				return c // best evidence first
			}
			// Then the plainest shape, so a hover reading the first hit gets
			// the definition's name rather than one of its decorations.
			if c := cmp.Compare(len(a.Pre)+len(a.Suf), len(b.Pre)+len(b.Suf)); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Pre, b.Pre); c != 0 {
				return c
			}
			return cmp.Compare(a.Suf, b.Suf)
		})
		if len(list) > MaxAffixesPerKind {
			list = list[:MaxAffixesPerKind]
		}
		out[kind] = list
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DeriveMemberAffixes is deriveLocAffixes for names that are block members of
// a definition rather than definitions themselves.
//
// CK3 game rules are the case that forces it. A rule is a definition and gets
// `rule_<id>`, which the definition-keyed pass already covers — 77 of vanilla's
// 78 rules. But a rule's *options* are nested one level inside it and are never
// harvested as definitions, and vanilla writes 590 `setting_<option>` /
// `setting_<option>_desc` keys for its 380 options. Nothing owned any of them,
// so every one read as an orphaned key, in vanilla and in every mod that adds a
// rule.
//
// Kept in its own map rather than merged into LocAffixes, because the two must
// not cross-apply. Member names include ordinary field names — `default`,
// `name`, `trigger` — and letting a definition-keyed convention like
// `<id>_desc` match one of those would explain away real orphans.
func DeriveMemberAffixes(
	structures map[string][]string, locKeys map[string]bool,
) map[string][]Affix {
	if len(locKeys) == 0 || len(structures) == 0 {
		return nil
	}
	names := make(map[string][]string, len(structures))
	total := make(map[string]int, len(structures))
	for kind, keys := range structures {
		k := jomini.CanonicalKind(kind)
		if k == "" {
			continue
		}
		total[k] += len(keys)
		for _, key := range keys {
			if key == "" {
				continue
			}
			if !slices.Contains(names[key], k) {
				names[key] = append(names[key], k)
			}
		}
	}
	return AffixesFrom(names, total, locKeys)
}

// tokenOffsets records where each underscore-delimited token of key starts,
// plus a sentinel one past the end, so a span of tokens i..j-1 is
// key[offs[i]:offs[j]-1] with no allocation. Reusing the caller's slice matters:
// this runs over every localization key in the install.
func tokenOffsets(key string, offs []int) []int {
	offs = append(offs, 0)
	for i := 0; i < len(key); i++ {
		if key[i] == '_' {
			offs = append(offs, i+1)
		}
	}
	return append(offs, len(key)+1)
}

// MemberConventionOwner is ConventionOwner for names that are block members of a
// definition rather than definitions themselves — `setting_normal_difficulty`
// naming an option inside a CK3 game rule.
//
// It does not enumerate spans. A member convention is always a whole-key match:
// strip the prefix and suffix, and ask whether what is left is a member of that
// kind. That is a handful of string comparisons per convention, against a set of
// conventions that is small because few kinds have one at all.
func MemberConventionOwner(
	affixes map[string][]Affix, key string, isMember func(kind, name string) bool,
) (name, kind string, ok bool) {
	if key == "" || len(affixes) == 0 {
		return "", "", false
	}
	for k, list := range affixes {
		for _, a := range list {
			if len(key) <= len(a.Pre)+len(a.Suf) ||
				!strings.HasPrefix(key, a.Pre) || !strings.HasSuffix(key, a.Suf) {
				continue
			}
			stem := key[len(a.Pre) : len(key)-len(a.Suf)]
			if isMember(k, stem) {
				return stem, k, true
			}
		}
	}
	return "", "", false
}
