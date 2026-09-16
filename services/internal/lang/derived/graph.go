// graph.go builds the fire graph: which keys fire events and which call sites
// invoke a scripted macro. The indirect hops between the two are not stored:
// EdgesTo/EdgesFrom walk the call index per query.

package derived

import (
	"strconv"

	"paradox-modding-tools/services/internal/lang/jomini"
)

func locKindRefs(refs []Ref) []Ref {
	var out []Ref
	for _, r := range refs {
		switch r.Kind {
		case "loc", "loc-broad":
			out = append(out, r)
		}
	}
	return out
}

// macroCallRefs keeps the call sites worth persisting: invocations of a scripted_* macro, which the Event Graph walks. Everything else the old call-kind vote elected — `subtract`, `trigger_localization`, `namespace`, the GUI value `right|top` — was not an object reference at all, and together they made up most of a 279 MB slice of the model.
func macroCallRefs(refs []Ref) []Ref {
	var out []Ref
	for _, r := range refs {
		if jomini.IsMacroKind(r.Kind) {
			out = append(out, r)
		}
	}
	return out
}

func fireTargetOK(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if c < 'A' || (c > 'Z' && c < 'a') || c > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c = s[i]
		if c != '_' && c != '.' && c != '-' &&
			(c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

func isDigitKey(s string) bool {
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// macroDefKinds maps scripted-macro def keys to their CanonicalKind. Only harvested defs (never script_docs vocab).
func macroDefKinds(defs []Def, cache *VanillaCache) map[string]string {
	out := map[string]string{}
	add := func(ds []Def) {
		for _, d := range ds {
			k := jomini.CanonicalKind(d.Kind)
			// A call is the invocation of a scripted_* macro, which the folder name states outright. Deriving this from observed usage was both too loose — CK3 elected `subtract` and the GUI value `right|top`, generating a call edge per occurrence — and too tight, since a macro that is defined but never called was not considered callable at all.
			if jomini.IsMacroKind(k) {
				out[d.Key] = k
			}
		}
	}
	add(defs)
	if cache != nil {
		add(cache.Defs)
	}
	return out
}

// ApplyCallRefs turns candidates whose To is a scripted-macro def into refs.
func ApplyCallRefs(cands []CallCandidate, kinds map[string]string) []Ref {
	if len(cands) == 0 || len(kinds) == 0 {
		return nil
	}
	var out []Ref
	for _, c := range cands {
		kind, ok := kinds[c.To]
		if !ok || c.From == c.To || c.Start >= c.End {
			continue
		}
		out = append(out, Ref{
			Key: c.To, Kind: kind, Path: c.Path, Line: c.Line,
			Start: c.Start, End: c.End,
		})
	}
	return out
}

// ApplyCallEdges turns candidates into call edges using a complete macroKeys.
func ApplyCallEdges(cands []CallCandidate, macroKeys map[string]bool) []Edge {
	if len(cands) == 0 || len(macroKeys) == 0 {
		return nil
	}
	var out []Edge
	for _, c := range cands {
		if macroKeys[c.To] && c.From != c.To {
			out = append(out, Edge{
				From: c.From, To: c.To, Path: c.Path, Line: c.Line, Kind: EdgeKindCall,
			})
		}
	}
	return out
}
