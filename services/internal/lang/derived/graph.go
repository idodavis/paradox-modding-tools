// graph.go builds the fire graph: which keys fire events, which call sites
// invoke a scripted macro, and the indirect hops between them that let the Event
// Graph draw an event reached only through a chain of macros.

package derived

import (
	"slices"
	"strconv"
	"strings"

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

// macroCallRefs keeps the call sites worth persisting: invocations of a
// scripted_* macro, which the Event Graph walks. Everything else the old
// call-kind vote elected — `subtract`, `trigger_localization`, `namespace`,
// the GUI value `right|top` — was not an object reference at all, and together
// they made up most of a 279 MB slice of the model.
func macroCallRefs(refs []Ref) []Ref {
	var out []Ref
	for _, r := range refs {
		if jomini.IsMacroKind(r.Kind) {
			out = append(out, r)
		}
	}
	return out
}

func fireSiteKind(stack []edgeFrame, fireKey string) (kind, nameKey string) {
	for _, s := range slices.Backward(stack) {
		k := strings.ToLower(s.key)
		if k == "option" {
			return "option", s.nameKey
		}
		if slot := jomini.ScriptSlot(s.key); slot != "" {
			return slot, ""
		}
	}
	if slot := jomini.ScriptSlot(fireKey); slot != "" {
		return slot, ""
	}
	return "effect", ""
}

// fireSiteRescopes reports whether anything between the enclosing definition and
// the fire site changed scope. stack[0] is the definition's own key, which is
// where its declared scope applies, so the walk starts below it.
func fireSiteRescopes(stack []edgeFrame) bool {
	for _, fr := range stack[min(1, len(stack)):] {
		if !jomini.IsScopeNeutral(fr.key) {
			return true
		}
	}
	return false
}

func optionNameKey(v jomini.Value) string {
	b := jomini.BlockOf(v)
	if b == nil {
		return ""
	}
	for _, st := range b.Statements {
		a, ok := st.(*jomini.Assignment)
		if ok && strings.EqualFold(a.Key.Text, "name") {
			if sc, ok := a.Value.(*jomini.Scalar); ok {
				return sc.Text
			}
		}
	}
	return ""
}

// walkFireTargets visits every scalar under a fire assignment that could name an
// event or on_action: the bare RHS, list values, and `id = ` / numbered inners.
// derived supplies the fire kinds harvested from the install; nil means only the
// structural keys (`id`, digits) count.
func walkFireTargets(
	v jomini.Value, derived *Derived, fn func(name string, start, end int),
) {
	switch t := v.(type) {
	case *jomini.Scalar:
		if !t.Quoted && fireTargetOK(t.Text) {
			fn(t.Text, t.Range.Start, t.Range.End)
		}
	case *jomini.Block, *jomini.TaggedBlock:
		b := jomini.BlockOf(v)
		if b == nil {
			return
		}
		for _, st := range b.Statements {
			switch n := st.(type) {
			case *jomini.ValueStmt:
				if sc, ok := n.Value.(*jomini.Scalar); ok && !sc.Quoted && fireTargetOK(sc.Text) {
					fn(sc.Text, sc.Range.Start, sc.Range.End)
				} else if jomini.BlockOf(n.Value) != nil {
					walkFireTargets(n.Value, derived, fn)
				}
			case *jomini.Assignment:
				key := strings.ToLower(n.Key.Text)
				own := derived.fireKind(n.Key.Text)
				if sc, ok := n.Value.(*jomini.Scalar); ok && !sc.Quoted && fireTargetOK(sc.Text) &&
					(key == "id" || isDigitKey(key) || own != "") {
					fn(sc.Text, sc.Range.Start, sc.Range.End)
					continue
				}
				if own != "" || key == "id" || isDigitKey(key) {
					walkFireTargets(n.Value, derived, fn)
				}
			}
		}
	}
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

// macroDefKinds maps scripted-macro def keys to their CanonicalKind.
// Only harvested defs (never script_docs vocab).
func macroDefKinds(defs []Def, cache *VanillaCache) map[string]string {
	out := map[string]string{}
	add := func(ds []Def) {
		for _, d := range ds {
			k := jomini.CanonicalKind(d.Kind)
			// A call is the invocation of a scripted_* macro, which the folder
			// name states outright. Deriving this from observed usage was both
			// too loose — CK3 elected `subtract` and the GUI value `right|top`,
			// generating a call edge per occurrence — and too tight, since a
			// macro that is defined but never called was not considered
			// callable at all.
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

// macroDefKeys is every scripted-macro def key in defs plus cache (via-hop nodes).
func macroDefKeys(defs []Def, cache *VanillaCache) map[string]bool {
	out := map[string]bool{}
	for key := range macroDefKinds(defs, cache) {
		out[key] = true
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

const maxViaHops = 3

// DeriveVia extends stored edges with indirect hops through scripted-macro nodes.
func DeriveVia(stored []Edge, cache *VanillaCache, macroKeys map[string]bool) []Edge {
	if cache != nil {
		stored = append(stored, cache.Edges...)
	}
	calls := map[string]map[string]bool{}
	fires := map[string][]Edge{}
	direct := map[string]bool{}
	for _, e := range stored {
		switch {
		case e.Kind == EdgeKindCall:
			if calls[e.From] == nil {
				calls[e.From] = map[string]bool{}
			}
			calls[e.From][e.To] = true
		case macroKeys[e.From]:
			fires[e.From] = append(fires[e.From], e)
		default:
			direct[e.From+"→"+e.To] = true
		}
	}
	var via []Edge
	for from, hop := range calls {
		if macroKeys[from] {
			continue
		}
		visited := map[string]bool{}
		type step struct {
			eff   string
			chain []string
		}
		var q []step
		for eff := range hop {
			visited[eff] = true
			q = append(q, step{eff, []string{eff}})
		}
		for n := 0; n < maxViaHops && len(q) > 0; n++ {
			var next []step
			for _, st := range q {
				for _, fired := range fires[st.eff] {
					if fired.To == from || direct[from+"→"+fired.To] {
						continue
					}
					direct[from+"→"+fired.To] = true
					via = append(via, Edge{
						From: from, To: fired.To, Via: fired.Via,
						Path: fired.Path, Line: fired.Line, Kind: "via",
						NameKey: strings.Join(st.chain, " → "),
					})
				}
				for deeper := range calls[st.eff] {
					if visited[deeper] {
						continue
					}
					visited[deeper] = true
					next = append(next, step{deeper, append(append([]string{}, st.chain...), deeper)})
				}
			}
			q = next
		}
	}
	return via
}
