package index

import (
	"slices"

	"github.com/samber/lo"

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

// ListFires returns the events, on_actions and decisions a definition fires, through its scripted effects if asked. It
// returns nil if it fires nothing.
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
				for _, target := range index.ListActing(schema, reference, use.File) {
					switch {
					case targeted:
						fires = append(fires, Fire{Source: site, Target: target, Use: use, Via: via})
					case !open[target]:
						walk(target, append(slices.Clone(via), target))
					}
				}
			}
		}
	}
	walk(site, nil)
	return fires
}

// ListFiredBy returns the definitions that fire a definition, through scripted effects if asked. It returns nil if
// nothing fires it.
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
		// Only a definition that acts is called by name.
		reference := index.ToReference(current)
		if winner, ok := index.FindKeptLocal(schema, reference, current.File); !ok || !index.isActing(winner, current) {
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
				if winner, ok := index.FindKeptLocal(schema, holder, parent.File); ok && index.isActing(winner, parent) {
					source := lo.Ternary(winner.Reason == Unsettled, parent, winner.Site)
					fires = append(fires, Fire{Source: source, Target: site, Use: fired, Via: via})
				}
			case !open[parent]:
				walk(parent, slices.Insert(slices.Clone(via), 0, parent), fired)
			}
		}
	}
	walk(site, nil, Site{})
	return fires
}

// ListActing returns the copies of a name that a reference in a file reaches. It returns none if the game keeps none.
func (index *Index) ListActing(schema *schema.Schema, reference schema.Reference, file int32) []Site {
	winner, ok := index.FindKeptLocal(schema, reference, file)
	if !ok {
		return nil
	}
	return lo.Filter(index.ResolveLocal(reference, index.Files[file].Path), func(site Site, _ int) bool {
		return index.isActing(winner, site)
	})
}

// isActing reports whether a definition of the name a winner won takes part in what the game runs: the one it keeps,
// one merged into it, or, where no source says which it keeps, any copy in a file it reads.
func (index *Index) isActing(winner Winner, site Site) bool {
	kept, reason := index.ResolveSite(winner, site)
	return kept || reason == Unsettled
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

// isFiring reports whether a reference fires the event, on_action or decision it names.
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

// isEffect reports whether a statement is an effect or a call of a scripted effect.
func isEffect(context *schema.Context, effects string) bool {
	return context.Declaration == game.DeclaredEffect ||
		slices.ContainsFunc(context.References, func(reference schema.Reference) bool {
			return reference.Database == effects || reference.Database == game.AnyCall
		})
}
