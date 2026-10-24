package index

import (
	"slices"

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

// ListFires returns what a definition fires: the events, on_actions and decisions it names in a firing position.
// The schema says which databases those are, and which one holds the scripted effects. With the flag set, it also
// follows the scripted effects the definition calls.
func (index *Index) ListFires(schema *schema.Schema, site Site, follow bool) []Fire {
	targets, effects := schema.FireDatabases, schema.Effects
	source := index.ToReference(site).Database
	if !slices.Contains(targets, source) && source != effects {
		return nil
	}
	var fires []Fire
	// The scripted effects on the way from the definition are open, so that effects calling each other end the walk.
	open := map[Site]bool{}
	var walk func(current Site, via []Site)
	walk = func(current Site, via []Site) {
		open[current] = true
		defer delete(open, current)
		for _, use := range index.listReferences(current) {
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

// ListFiredBy returns what fires a definition, following scripted effects when the flag is set. The schema says
// which databases fire, as for ListFires.
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
			if !inside || open[parent] {
				continue
			}
			fired := use
			if current == site {
				fired = referrer
			}
			holder := index.ToReference(parent)
			switch {
			case slices.Contains(targets, holder.Database):
				// A firer the game drops fires nothing, and a merged one stands as the definition the game keeps.
				if winner, ok := index.FindKeptLocal(schema, holder, parent.File); ok && winner.IsKept(parent) {
					fires = append(fires, Fire{Source: winner.Site, Target: site, Use: fired, Via: via})
				}
			case holder.Database == effects && follow:
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
