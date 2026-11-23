// audiences.go reads whom an advance of an EU5 age is for: every country, a
// religion, a culture or a country its potential names, or whoever meets its
// other conditions.

package graph

import (
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// Audience is one row of the list of whom an age's advances are for: a group,
// or one country, religion or culture in it.
type Audience struct {
	// ID is the filter's Potential that draws the row: a group's ID, or the id
	// of a definition.
	ID string `json:"id"`

	// Group is the ID of the group the row is in. A group's own row has it as
	// its ID.
	Group string `json:"group"`

	Title string `json:"title"`

	// Count is how many advances the row draws.
	Count int `json:"count"`
}

// audienceGroup is a group of rows: the definitions of its databases that a
// potential names.
type audienceGroup struct {
	ID, Title string
	Databases []string
}

// The groups no database makes up: the advances with no potential, and those
// whose potential names nobody, as one that asks for a region or a reform.
const (
	sharedAdvances = "shared"
	otherAdvances  = "other"
)

var (
	sharedGroup = audienceGroup{ID: sharedAdvances, Title: "Every country"}
	otherGroup  = audienceGroup{ID: otherAdvances, Title: "Other conditions"}
)

// audienceGroups holds the groups of each game whose advances have a potential,
// widest first. An advance is in the narrowest group its potential names: one
// for a Catholic country is in the countries.
var audienceGroups = map[string][]audienceGroup{
	game.EU5: {
		{ID: "religions", Title: "Religions", Databases: []string{"religions", "religion_groups"}},
		{ID: "cultures", Title: "Cultures", Databases: []string{"cultures", "culture_groups", "languages"}},
		{ID: "countries", Title: "Countries", Databases: []string{lang.SetupCountries}},
	},
}

// potentialKey is the block of an advance that says whom it is for. It names
// them as the triggers of in_game/common/advances/0_age_of_traditions.txt:188
// do: has_or_had_tag = BYZ.
const potentialKey = "potential"

// scriptedTriggers is the database of the triggers a potential can call, as
// is_protestant = yes.
const scriptedTriggers = "scripted_triggers"

// potentialParents holds what a definition a potential can name belongs to, as
// the keys that write it and the database each names: a country's culture and
// religion, in in_game/setup/countries/balkans.txt:62, a culture's language and
// groups, in in_game/common/cultures/greek.txt, and a religion's group, in
// in_game/common/religions/christian.txt:194.
var potentialParents = map[string]map[string]string{
	lang.SetupCountries: {"culture_definition": "cultures", "religion_definition": "religions"},
	"cultures":          {"language": "languages", "culture_groups": "culture_groups"},
	"religions":         {"group": "religion_groups"},
}

// listAudiences returns the rows of a tree's advances and the advances each row
// draws: a group what is in it, and a country, religion or culture what is
// written for it and for what it belongs to. A group with no advances has no
// row. It returns none for a game whose technologies have no potential.
func (d *draft) listAudiences(tree []string) ([]Audience, map[string][]string) {
	rows, members := []Audience{}, map[string][]string{}
	if audienceGroups[d.ws.Game.ID] == nil {
		return rows, members
	}

	advances, named := d.listWritten(tree)
	groups := slices.Concat([]audienceGroup{sharedGroup}, audienceGroups[d.ws.Game.ID], []audienceGroup{otherGroup})
	for _, group := range groups {
		ids := advances[group.ID]
		if len(ids) == 0 {
			continue
		}

		members[group.ID] = ids
		rows = append(rows, Audience{ID: group.ID, Group: group.ID, Title: group.Title, Count: len(ids)})
		for _, name := range named[group.ID] {
			members[name] = d.listWrittenFor(name, advances)
		}

		rows = append(rows, d.makeNameRows(group.ID, named[group.ID], members)...)
	}

	return rows, members
}

// listWritten returns the advances of a tree written for each group and each
// name, and the names of each group.
func (d *draft) listWritten(tree []string) (map[string][]string, map[string][]string) {
	advances, named := map[string][]string{}, map[string][]string{}
	for _, id := range tree {
		group, names := d.findAudience(id)
		advances[group] = append(advances[group], id)
		for _, name := range names {
			advances[name] = append(advances[name], id)
			named[group] = appendNew(named[group], name)
		}
	}

	return advances, named
}

// listWrittenFor returns the advances written for a definition and for what it
// belongs to.
func (d *draft) listWrittenFor(id string, advances map[string][]string) []string {
	var ids []string
	for _, owner := range d.listBelonging(id) {
		ids = append(ids, advances[owner]...)
	}

	return dropRepeats(ids, func(advance string) string { return advance })
}

// listBelonging returns a definition and all it belongs to: a country's culture
// and religion, and their language and groups.
func (d *draft) listBelonging(id string) []string {
	belonging := []string{id}
	for position := 0; position < len(belonging); position++ {
		owner := belonging[position]
		tree, statement, isRead := d.findStatement(owner)
		if !isRead {
			continue
		}

		for key, database := range potentialParents[toDatabase(owner)] {
			for _, value := range listValues(tree, statement, key) {
				belonging = appendNew(belonging, lang.Reference{Database: database, Name: value}.ToID())
			}
		}
	}

	return belonging
}

// makeNameRows returns the rows of the given names of a group, by title.
func (d *draft) makeNameRows(group string, names []string, members map[string][]string) []Audience {
	rows := make([]Audience, len(names))
	for position, name := range names {
		rows[position] = Audience{ID: name, Group: group, Title: labelNode(d.lookupNode(name)), Count: len(members[name])}
	}

	slices.SortFunc(rows, func(first, second Audience) int {
		return strings.Compare(strings.ToLower(first.Title), strings.ToLower(second.Title))
	})

	return rows
}

// findAudience returns the group an advance is in, and whom of that group its
// potential names.
func (d *draft) findAudience(id string) (string, []string) {
	ids, isWritten := d.listPotential(id)
	if !isWritten {
		return sharedAdvances, nil
	}

	for _, group := range slices.Backward(audienceGroups[d.ws.Game.ID]) {
		named := slices.DeleteFunc(slices.Clone(ids), func(name string) bool {
			return !slices.Contains(group.Databases, toDatabase(name))
		})
		if len(named) > 0 {
			return group.ID, named
		}
	}

	return otherAdvances, nil
}

// listPotential returns the ids of whom an advance's potential is for: the
// countries, cultures, religions, groups and languages it names, itself or
// through the scripted triggers it calls. It returns false if the advance
// writes no potential.
func (d *draft) listPotential(id string) ([]string, bool) {
	site, err := resolveNode(d.ws, d.mods, id)
	if err != nil {
		return nil, false
	}

	tree, statement, err := d.ws.FindStatement(d.mods, site)
	if err != nil {
		return nil, false
	}

	block, isWritten := tree.FindChild(statement, potentialKey)
	if !isWritten {
		return nil, false
	}

	var ids []string

	references := d.listNamed(site, tree, block)
	called := map[string]bool{}
	for position := 0; position < len(references); position++ {
		reference := references[position]
		switch {
		case d.isAudience(reference.Database):
			ids = appendNew(ids, reference.ToID())

		case reference.Database == scriptedTriggers && !called[reference.Name]:
			called[reference.Name] = true
			references = append(references, d.listCalled(reference)...)
		}
	}

	return ids, true
}

func (d *draft) isAudience(database string) bool {
	return slices.ContainsFunc(audienceGroups[d.ws.Game.ID], func(group audienceGroup) bool {
		return slices.Contains(group.Databases, database)
	})
}

// listCalled returns what a scripted trigger names. It returns none for a
// trigger the game keeps no definition of.
func (d *draft) listCalled(trigger lang.Reference) []lang.Reference {
	site, err := resolveNode(d.ws, d.mods, trigger.ToID())
	if err != nil {
		return nil
	}

	tree, statement, err := d.ws.FindStatement(d.mods, site)
	if err != nil {
		return nil
	}

	return d.listNamed(site, tree, statement)
}

// listNamed returns the references the index holds inside a block of a
// definition, in the order written. A reference inside a negating block is
// left out: it says whom the definition is not for.
func (d *draft) listNamed(site index.Site, tree *jomini.Tree, block int32) []lang.Reference {
	area := tree.Statements[block].Span

	var references []lang.Reference
	for _, use := range d.mods.ListReferences(site) {
		span := d.mods.Occurrence(use).Span
		if span.Start >= area.Start && span.End <= area.End && !isNegated(tree, block, span) {
			references = append(references, d.mods.ToReference(use))
		}
	}

	return references
}

// isNegated reports whether a span lies inside a negating block, as NOT, below
// a block.
func isNegated(tree *jomini.Tree, block int32, span jomini.Span) bool {
	for child := range tree.Children(block) {
		area := tree.Statements[child].Span
		if span.Start < area.Start || span.End > area.End {
			continue
		}

		wording, _ := findPhrase(tree.Text(tree.Statements[child].Key))

		return wording.Negates || isNegated(tree, child, span)
	}

	return false
}
