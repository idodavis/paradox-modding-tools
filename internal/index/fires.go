package index

import (
	"slices"

	"paradox-modding-tools/internal/schema"
)

// A Fire is one firing: one definition fires another, with the delay written on it and the scripted effects the
// firing passes through.
type Fire struct {
	Source Site     `json:"source"`
	Target Site     `json:"target"`
	Days   string   `json:"days,omitempty"` // as written: a number or a range
	Via    []string `json:"via,omitempty"`  // the scripted effects entered between them, in order
}

// ListFires returns what a definition fires: the events, on_actions and decisions it names in a firing position.
// The schema says which databases those are, and which one holds the scripted effects. With the flag set, it also
// follows the scripted effects the definition calls.
func (index *Index) ListFires(schema *schema.Schema, site Site, follow bool) []Fire {
	targets, effects := schema.FireDatabases, schema.Effects
	source := index.Databases.Texts[index.Occurrence(site).Database]
	if !slices.Contains(targets, source) && source != effects {
		return nil
	}
	var fires []Fire
	// The scripted effects on the way from the definition are open, so that effects calling each other end the walk.
	open := map[Site]bool{}
	var walk func(current Site, via []string)
	walk = func(current Site, via []string) {
		open[current] = true
		defer delete(open, current)
		for _, use := range index.listReferences(current) {
			occurrence := index.Occurrence(use)
			database, name := index.Databases.Texts[occurrence.Database], index.Names.Texts[occurrence.Name]
			winner, ok := index.findKept(schema, use)
			switch {
			case !ok:
			case slices.Contains(targets, database):
				fires = append(fires, Fire{Source: site, Target: winner.Site, Via: via})
			case database == effects && follow && !open[winner.Site]:
				walk(winner.Site, append(slices.Clone(via), name))
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
	if !slices.Contains(targets, index.Databases.Texts[index.Occurrence(site).Database]) {
		return nil
	}
	var fires []Fire
	// The scripted effects on the way from the definition are open, so that effects calling each other end the walk.
	open := map[Site]bool{}
	var walk func(current Site, via []string)
	walk = func(current Site, via []string) {
		open[current] = true
		defer delete(open, current)
		// Only the definition the game keeps is called by name.
		winner, ok := index.findKept(schema, current)
		if !ok || winner.Site != current && !slices.Contains(winner.Merged, current) {
			return
		}
		occurrence := index.Occurrence(current)
		for _, use := range index.ListUses(index.Databases.Texts[occurrence.Database], index.Names.Texts[occurrence.Name]) {
			parent, inside := index.FindDefinition(use)
			if !inside || open[parent] {
				continue
			}
			holder := index.Occurrence(parent)
			switch holderDatabase := index.Databases.Texts[holder.Database]; {
			case slices.Contains(targets, holderDatabase):
				fires = append(fires, Fire{Source: parent, Target: site, Via: via})
			case holderDatabase == effects && follow:
				walk(parent, slices.Insert(slices.Clone(via), 0, index.Names.Texts[holder.Name]))
			}
		}
	}
	walk(site, nil)
	return fires
}

// findKept returns the definition the game keeps of the name that an occurrence is written with, in the database
// the occurrence is written in. It returns false if the schema has no such database, the name is undefined, or
// the game keeps none of its definitions.
func (index *Index) findKept(schema *schema.Schema, site Site) (Winner, bool) {
	occurrence := index.Occurrence(site)
	database, ok := schema.Databases[index.Databases.Texts[occurrence.Database]]
	if !ok {
		return Winner{}, false
	}
	sites := index.listDefining(database.Name, index.Names.Texts[occurrence.Name], site.File)
	if len(sites) == 0 {
		return Winner{}, false
	}
	return index.FindWinner(sites, database.Precedence)
}

// listReferences returns the references that sit directly inside a definition.
func (index *Index) listReferences(definition Site) []Site {
	var sites []Site
	occurrences := index.Occurrences[definition.File]
	for position := definition.Occurrence + 1; int(position) < len(occurrences); position++ {
		occurrence := occurrences[position]
		if occurrence.Role == RoleRefers && occurrence.Parent == definition.Occurrence {
			sites = append(sites, Site{File: definition.File, Occurrence: position})
		}
	}
	return sites
}
