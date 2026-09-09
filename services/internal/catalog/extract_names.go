// extract_names.go harvests ephemeral names, script params, and convention
// loc refs — kept out of extract.go.

package catalog

import (
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
)

// extractScriptNames walks assignments for ScriptName defs/refs and scope:/var: refs.
func extractScriptNames(
	gameID string, root *jomini.Root, li *jomini.LineIndex, path, origin string,
) (defs []Def, refs []Ref) {
	if root == nil {
		return nil, nil
	}
	var walk func(stmts []jomini.Statement)
	walk = func(stmts []jomini.Statement) {
		for _, st := range stmts {
			a, ok := st.(*jomini.Assignment)
			if !ok {
				if vs, ok := st.(*jomini.ValueStmt); ok {
					if b := jomini.BlockOf(vs.Value); b != nil {
						walk(b.Statements)
					}
				}
				continue
			}
			if !a.Key.Quoted {
				if r, ok := jomini.ScriptName(a.Key.Text); ok {
					name, start, end, found := scriptNameSpan(a, r.InnerKey)
					if found && name != "" {
						if r.IsDef {
							// Ephemeral and script-name definitions live inside
							// another definition, never at a file's top level.
							d := makeDef(r.Kind, name, path, origin,
								jomini.Range{Start: start, End: end}, li)
							d.Value = scriptNameValue(a, r)
							d.Nested = true
							defs = append(defs, d)
						} else {
							refs = append(refs, Ref{
								Key: name, Kind: r.Kind, Path: path,
								Line:  li.PositionAt(start).Line,
								Start: start, End: end,
							})
						}
					}
				}
				if sc, ok := a.Value.(*jomini.Scalar); ok && !sc.Quoted {
					if p, ok := jomini.ParsePrefixed(sc.Text); ok {
						kind := jomini.PrefixKind(p.Prefix)
						if kind == "" {
							kind = p.Prefix
						}
						refs = append(refs, Ref{
							Key: p.Name, Kind: kind, Path: path,
							Line:  li.PositionAt(sc.Range.Start).Line,
							Start: sc.Range.Start + p.NameOff,
							End:   sc.Range.Start + p.NameOff + len(p.Name),
						})
					} else if kind, id, ok := game.ParseTyped(gameID, sc.Text); ok {
						refs = append(refs, Ref{
							Key: id, Kind: kind, Path: path,
							Line:  li.PositionAt(sc.Range.Start).Line,
							Start: sc.Range.Start, End: sc.Range.End,
						})
					}
				}
			}
			if b := jomini.BlockOf(a.Value); b != nil {
				walk(b.Statements)
			}
		}
	}
	walk(root.Statements)
	return defs, refs
}

// extractScriptParams harvests $NAME$ macro defs/refs and call-site TARGET = refs.
func extractScriptParams(
	root *jomini.Root, li *jomini.LineIndex, path, origin string,
	existing []Def,
) (defs []Def, refs []Ref) {
	if root == nil {
		return nil, nil
	}
	owners := scriptedMacroKeys(existing)
	ownerParams := map[string]map[string]bool{}
	for _, st := range root.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted || !owners[a.Key.Text] {
			continue
		}
		b := jomini.BlockOf(a.Value)
		if b == nil {
			continue
		}
		pdefs, prefs, names := harvestDollarParams(b, li, path, origin, a.Key.Text)
		defs = append(defs, pdefs...)
		refs = append(refs, prefs...)
		ownerParams[a.Key.Text] = names
	}
	var walk func(stmts []jomini.Statement, callKey string)
	walk = func(stmts []jomini.Statement, callKey string) {
		for _, st := range stmts {
			a, ok := st.(*jomini.Assignment)
			if !ok {
				if vs, ok := st.(*jomini.ValueStmt); ok {
					if b := jomini.BlockOf(vs.Value); b != nil {
						walk(b.Statements, callKey)
					}
				}
				continue
			}
			if !a.Key.Quoted {
				if callKey != "" && ownerParams[callKey][a.Key.Text] {
					refs = append(refs, Ref{
						Key: a.Key.Text, Kind: "script_param", Path: path,
						Line:  li.PositionAt(a.Key.Range.Start).Line,
						Start: a.Key.Range.Start, End: a.Key.Range.End,
						OwnerKey: callKey,
					})
				}
				nextCall := callKey
				if owners[a.Key.Text] {
					nextCall = a.Key.Text
				}
				if b := jomini.BlockOf(a.Value); b != nil {
					walk(b.Statements, nextCall)
					continue
				}
			}
			if b := jomini.BlockOf(a.Value); b != nil {
				walk(b.Statements, callKey)
			}
		}
	}
	walk(root.Statements, "")
	return defs, refs
}

// scriptedMacroKeys is IsCitableDef plus two exclusions of its own, and they are
// deliberate: a `$PARAM$` site resolves against names a macro body could call,
// and neither an event namespace nor a GUI template is callable from script.
func scriptedMacroKeys(defs []Def) map[string]bool {
	out := map[string]bool{}
	for _, d := range defs {
		k := jomini.CanonicalKind(d.Kind)
		if !IsCitableDef(d) || k == jomini.KindNamespace || k == jomini.KindGUIType {
			continue
		}
		out[d.Key] = true
	}
	return out
}

func harvestDollarParams(
	b *jomini.Block, li *jomini.LineIndex, path, origin, ownerKey string,
) (defs []Def, refs []Ref, names map[string]bool) {
	names = map[string]bool{}
	seen := map[string]bool{}
	var record func(name string, start, end int)
	record = func(name string, start, end int) {
		names[name] = true
		if !seen[name] {
			seen[name] = true
			defs = append(defs, Def{
				Kind: "script_param", Key: name, Path: path, Origin: origin,
				Line: li.PositionAt(start).Line, Start: start, End: end,
				OwnerKey: ownerKey,
			})
			return
		}
		refs = append(refs, Ref{
			Key: name, Kind: "script_param", Path: path,
			Line: li.PositionAt(start).Line, Start: start, End: end,
			OwnerKey: ownerKey,
		})
	}
	var walk func(stmts []jomini.Statement)
	walk = func(stmts []jomini.Statement) {
		for _, st := range stmts {
			a, ok := st.(*jomini.Assignment)
			if !ok {
				if vs, ok := st.(*jomini.ValueStmt); ok {
					if inner := jomini.BlockOf(vs.Value); inner != nil {
						walk(inner.Statements)
					}
				}
				continue
			}
			if !a.Key.Quoted {
				for _, span := range dollarParamsIn(a.Key.Text, a.Key.Range.Start) {
					record(span.name, span.start, span.end)
				}
				if sc, ok := a.Value.(*jomini.Scalar); ok && !sc.Quoted {
					for _, span := range dollarParamsIn(sc.Text, sc.Range.Start) {
						record(span.name, span.start, span.end)
					}
				}
			}
			if inner := jomini.BlockOf(a.Value); inner != nil {
				walk(inner.Statements)
			}
		}
	}
	walk(b.Statements)
	return defs, refs, names
}

type dollarSpan struct {
	name       string
	start, end int
}

func dollarParamsIn(text string, base int) []dollarSpan {
	var out []dollarSpan
	for i := 0; i < len(text); {
		if text[i] != '$' {
			i++
			continue
		}
		j := i + 1
		nameStart := j
		for j < len(text) && isParamByte(text[j], j == nameStart) {
			j++
		}
		if j == nameStart || j >= len(text) || text[j] != '$' {
			i++
			continue
		}
		out = append(out, dollarSpan{
			name:  text[nameStart:j],
			start: base + i,
			end:   base + j + 1,
		})
		i = j + 1
	}
	return out
}

func isParamByte(c byte, first bool) bool {
	if first {
		return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9')
}

// scriptNameSpan returns the name token span for a ScriptName assignment.
// Scalar RHS uses the scalar; a block uses InnerKey (flag/name) when set.
func scriptNameSpan(a *jomini.Assignment, innerKey string) (name string, start, end int, ok bool) {
	if sc, is := a.Value.(*jomini.Scalar); is && !sc.Quoted && sc.Text != "" {
		return sc.Text, sc.Range.Start, sc.Range.End, true
	}
	if innerKey == "" {
		return "", 0, 0, false
	}
	b := jomini.BlockOf(a.Value)
	if b == nil {
		return "", 0, 0, false
	}
	for _, st := range b.Statements {
		ia, is := st.(*jomini.Assignment)
		if !is || ia.Key.Quoted || ia.Key.Text != innerKey {
			continue
		}
		sc, isS := ia.Value.(*jomini.Scalar)
		if !isS || sc.Quoted || sc.Text == "" {
			return "", 0, 0, false
		}
		return sc.Text, sc.Range.Start, sc.Range.End, true
	}
	return "", 0, 0, false
}

// scriptNameValue is the hover RHS for a ScriptName def. Scalar saves
// (save_scope_as) have no value; their hover body is the ancestor scope expr.
func scriptNameValue(a *jomini.Assignment, r jomini.ScriptNameRule) string {
	if r.Kind == "saved_scope" && r.InnerKey == "" {
		return ""
	}
	if sc, ok := a.Value.(*jomini.Scalar); ok && !sc.Quoted {
		return "yes"
	}
	b := jomini.BlockOf(a.Value)
	if b == nil {
		return "yes"
	}
	for _, st := range b.Statements {
		ia, ok := st.(*jomini.Assignment)
		if !ok || ia.Key.Quoted || ia.Key.Text != "value" {
			continue
		}
		if sc, ok := ia.Value.(*jomini.Scalar); ok && !sc.Quoted && sc.Text != "" {
			return sc.Text
		}
	}
	return "yes"
}

func conventionLocRefs(
	li *jomini.LineIndex, path string, defs []Def, derived *Derived,
) []Ref {
	if derived == nil || li == nil {
		return nil
	}
	var out []Ref
	for _, d := range defs {
		// Only the strongest conventions are stored as references. The full set
		// is applied in reverse by ConventionOwner, which costs nothing until a
		// key actually looks orphaned; expanding all of them forwards would add
		// a reference per definition per convention to every install cache.
		//
		// LocConventionStored, not LocConventionRequired: what is worth a
		// navigable reference and what the game demands are different questions,
		// and sharing a constant coupled them. Raising the required floor to 100
		// shrank this set and pushed EU5's orphan count up 143 as a side effect.
		affixes := derived.locAffixes(d.Kind)
		for _, key := range ConventionKeys(d.Key, affixes, LocConventionStored) {
			out = append(out, Ref{
				Key: key, Kind: "loc-convention", Path: path,
				Line: li.PositionAt(d.Start).Line, Start: d.Start, End: d.End,
			})
		}
	}
	return out
}

// dropEphemeralDefs drops flags/variables/scopes from the install cache.
// script_param stays so $NAME$ in vanilla scripted_* files can resolve.
func dropEphemeralDefs(defs []Def) []Def {
	out := make([]Def, 0, len(defs))
	for _, d := range defs {
		k := jomini.CanonicalKind(d.Kind)
		if jomini.IsEphemeral(k) && k != "script_param" {
			continue
		}
		out = append(out, d)
	}
	return out
}
