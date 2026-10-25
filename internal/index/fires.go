package index

import (
	"slices"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
)

// A Fire is one firing: one definition fires another, through the reference that names it and the scripted
// effects the firing passes through.
type Fire struct {
	Source Site   `json:"source"`
	Target Site   `json:"target"`
	Use    Site   `json:"use"`           // the reference that names the target, beside which a delay is written
	Via    []Site `json:"via,omitempty"` // the scripted effects entered between them, in order
}

// ListFires returns what a definition of any database fires: the events, on_actions and decisions it names in a
// reference that fires, the databases the schema lists in FireDatabases. A reference fires where the scan marks it
// Firing: in effect script and in an on_action's own lists. With the flag set, it also follows the scripted effects
// the definition calls, so what they fire counts too. It returns nil if the definition fires nothing.
func (index *Index) ListFires(schema *schema.Schema, site Site, follow bool) []Fire {
	targets, effects := schema.FireDatabases, schema.Effects
	var fires []Fire
	// The scripted effects on the way from the definition are open, so that effects calling each other end the walk.
	open := map[Site]bool{}
	var walk func(current Site, via []Site)
	walk = func(current Site, via []Site) {
		open[current] = true
		defer delete(open, current)
		for _, use := range index.listReferences(current) {
			if !index.Occurrence(use).Firing {
				continue
			}
			for _, reference := range ListCallees(index.ToReference(use)) {
				targeted := slices.Contains(targets, reference.Database)
				if !targeted && (!follow || reference.Database != effects) {
					continue
				}
				winner, ok := index.FindKeptLocal(schema, reference, use.File)
				switch {
				case !ok:
				case targeted:
					fires = append(fires, Fire{Source: site, Target: winner.Site, Use: use, Via: via})
				case !open[winner.Site]:
					walk(winner.Site, append(slices.Clone(via), winner.Site))
				}
			}
		}
	}
	walk(site, nil)
	return fires
}

// ListFiredBy returns what fires a definition: each definition of any database that names it in a reference that
// fires, as for ListFires. A definition that fires itself is one of them. Only an event, an on_action or a decision is
// fired, the databases the schema lists in FireDatabases, so it returns nil for any other. With the flag set, a
// scripted effect that fires it is followed to the definitions that call the effect, which fire it in the effect's
// place. It returns nil for a definition the game does not keep.
func (index *Index) ListFiredBy(schema *schema.Schema, site Site, follow bool) []Fire {
	targets, effects := schema.FireDatabases, schema.Effects
	if !slices.Contains(targets, index.ToReference(site).Database) {
		return nil
	}
	var fires []Fire
	// The scripted effects on the way from the definition are open, so that effects calling each other end the walk.
	open := map[Site]bool{}
	// The use passed down is the reference that names the fired definition, found at the first step.
	var walk func(current Site, via []Site, use Site)
	walk = func(current Site, via []Site, use Site) {
		open[current] = true
		defer delete(open, current)
		// Only the definition the game keeps is called by name.
		reference := index.ToReference(current)
		if winner, ok := index.FindKeptLocal(schema, reference, current.File); !ok || !winner.IsKept(current) {
			return
		}
		for _, referrer := range index.ListUses(reference.Database, reference.Name) {
			parent, inside := index.FindDefinition(referrer)
			if !inside || !index.Occurrence(referrer).Firing {
				continue
			}
			fired := use
			if current == site {
				fired = referrer
			}
			holder := index.ToReference(parent)
			switch {
			case holder.Database != effects || !follow:
				// A firer the game drops fires nothing, and a merged one stands as the definition the game keeps.
				if winner, ok := index.FindKeptLocal(schema, holder, parent.File); ok && winner.IsKept(parent) {
					fires = append(fires, Fire{Source: winner.Site, Target: site, Use: fired, Via: via})
				}
			case !open[parent]:
				walk(parent, slices.Insert(slices.Clone(via), 0, parent), fired)
			}
		}
	}
	walk(site, nil, Site{})
	return fires
}

// listReferences returns the references that sit directly inside a definition, in source order.
func (index *Index) listReferences(definition Site) []Site {
	var sites []Site
	// A definition's references follow it without a break, so the list ends at the first occurrence that is not
	// inside it.
	occurrences := index.Occurrences[definition.File]
	for position := definition.Occurrence + 1; int(position) < len(occurrences); position++ {
		occurrence := occurrences[position]
		if !occurrence.Nested || occurrence.Parent < definition.Occurrence {
			break
		}
		if occurrence.Role == RoleRefers && occurrence.Parent == definition.Occurrence {
			sites = append(sites, Site{File: definition.File, Occurrence: position})
		}
	}
	return sites
}

// isFiring reports whether a reference fires the event, on_action or decision it names. It fires in effect script,
// which is an effect or the arguments of one, but it fires a decision only through an effect the game's row says runs
// it. It also fires in an on_action's own lists, and in the fields the game's row lists. It never fires inside a
// trigger block, in a preview, a block that is shown in the tooltip and never run, or as a scope link. Anywhere else,
// such as in a GUI file or in a definition's other fields, it does not fire.
func (collector *collector) isFiring(context *schema.Context, reference schema.Reference) bool {
	if context.Conditional || context.Preview || context.Category == schema.CategoryLink {
		return false
	}
	row := game.LookupGame(collector.schema.Game)
	effect := context
	if !isEffect(effect, collector.schema.Effects) {
		effect = context.Parent
	}
	if effect != nil && isEffect(effect, collector.schema.Effects) {
		target := collector.schema.Databases[reference.Database]
		return target == nil || target.Row.Houses != "decisions" || slices.Contains(row.DecisionEffects, effect.Label)
	}
	if holder := collector.schema.Databases[context.Database]; holder != nil && holder.Row.Houses == "on_actions" {
		return true
	}
	keys := context.Path
	if context.Label != "" {
		keys = append(keys[:len(keys):len(keys)], context.Label)
	}
	return slices.ContainsFunc(row.FiringFields, func(field game.Reference) bool {
		return field.Database == context.Database && game.MatchKeyPath(field.KeyPath, keys)
	})
}

// isEffect reports whether a statement is an effect: a command the dumps declare as an effect where it is written,
// or a call of a scripted effect. The scripted effects are the definitions of the given database. A call that may
// name any callable database counts too, as outside a trigger block it calls a scripted effect.
func isEffect(context *schema.Context, effects string) bool {
	return context.Declaration == game.DeclaredEffect ||
		slices.ContainsFunc(context.References, func(reference schema.Reference) bool {
			return reference.Database == effects || reference.Database == game.AnyCall
		})
}
