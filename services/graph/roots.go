// roots.go lists the definitions the Graph can be asked about: a search by
// name, the catalogs the page browses, the ones the workspace's mods define,
// those of one type, and an example for each question.

package graph

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// Catalog is the definitions of one type that the page lists together.
type Catalog struct {
	// Types are the types of its definitions.
	Types []lang.Type `json:"types"`

	// Label is what the game calls its definitions.
	Label string `json:"label"`

	// Count is how many it lists. A building's line counts once.
	Count int `json:"count"`

	// Groups are the groups its definitions are listed under, in the order
	// the game's files reach them: a district, a building group, a category
	// of goods, and the wider group each of those is in. A group's Count is
	// how many definitions it and the groups in it list. They are none where
	// the game groups nothing.
	Groups []Node `json:"groups"`
}

// catalogTypes holds the types of each catalog the page offers, in the order
// it offers them. A Vic3 production method has no catalog: it is reached
// through its building.
var catalogTypes = [][]lang.Type{
	{lang.TypeGood},
	{lang.TypeGoodCategory},
	{lang.TypeBuilding},
	{lang.TypeTechnology},
	{lang.TypeEvent},
}

// listGraphDatabases returns the databases of every type the Graph has a view
// of, beside what fires.
func listGraphDatabases(ws *workspace.Session) []string {
	graphTypes := lang.LookupDialect(ws.Game.ID).GraphTypes

	return listTypedDatabases(ws, func(drawn lang.Type) bool { return slices.Contains(graphTypes, drawn) })
}

// FindRoots returns the kept definitions the Graph asks about whose name
// holds the query, the best match first, up to a limit. It returns nil if no
// workspace is open.
func (s *Service) FindRoots(query string, limit int) []Node {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil
	}

	mods := ws.Mods.Load()
	databases := slices.Concat(ws.Schema.FireDatabases, listGraphDatabases(ws))

	return matchDefinitions(ws, mods, databases, query, limit, func(site index.Site) bool {
		return isKept(ws, mods, site)
	})
}

// ListCatalogs returns the catalogs of what the game's Graph shows, each with
// how many it lists and the groups it lists them under. It returns nil if no
// workspace is open.
func (s *Service) ListCatalogs() []Catalog {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil
	}

	draft := newDraft(ws, ws.Mods.Load())
	graphTypes := lang.LookupDialect(ws.Game.ID).GraphTypes
	catalogs := []Catalog{}
	for _, types := range catalogTypes {
		if types[0] == lang.TypeEvent || slices.Contains(graphTypes, types[0]) {
			catalogs = append(catalogs, draft.makeCatalog(types))
		}
	}

	return catalogs
}

// makeCatalog returns the catalog of the kept definitions of the given types.
func (d *draft) makeCatalog(types []lang.Type) Catalog {
	catalog := Catalog{Types: types, Groups: []Node{}}
	counts := map[string]int{}

	var groups []string
	for _, database := range listCatalogDatabases(d.ws, types) {
		if catalog.Label == "" {
			catalog.Label = lang.DescribeDefinition(database, true)
		}

		for _, winner := range d.mods.ListKept(d.ws.Schema, database) {
			id := toID(d.mods, winner.Site)
			if !isLineHead(d.model, id) {
				continue
			}

			catalog.Count++
			for _, group := range d.listGroupChain(id) {
				groups = appendNew(groups, group)
				counts[group]++
			}
		}
	}

	for _, group := range groups {
		if node, isGroup := d.makeGroup(group, counts[group]); isGroup {
			catalog.Groups = append(catalog.Groups, node)
		}
	}

	return catalog
}

// ListCatalog returns the definitions of a catalog's types whose name holds
// the query, the best match first, up to a limit, each with the group it is
// listed under. A building's line is listed once, by its first level. It
// returns nil if no workspace is open.
func (s *Service) ListCatalog(types []lang.Type, query string, limit int) []Node {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil
	}

	mods := ws.Mods.Load()
	draft := newDraft(ws, mods)
	rows := matchDefinitions(ws, mods, listCatalogDatabases(ws, types), query, limit, func(site index.Site) bool {
		winner, isDefined := mods.FindKept(ws.Schema, mods.ToReference(site))

		return isDefined && winner.Site == site && isLineHead(draft.model, toID(mods, site))
	})
	for position := range rows {
		rows[position].Group = draft.findGroupID(rows[position].ID)
	}

	return rows
}

func listCatalogDatabases(ws *workspace.Session, types []lang.Type) []string {
	return listTypedDatabases(ws, func(drawn lang.Type) bool { return slices.Contains(types, drawn) })
}

// isLineHead reports whether a definition is the first level of its building's
// line. Any other definition is a line of its own.
func isLineHead(model *model, id string) bool { return model.listLine(id)[0] == id }

// matchDefinitions returns the definitions of the databases that the test
// lists and whose name holds the query, with their pictures, the best match
// first, up to a limit.
func matchDefinitions(
	ws *workspace.Session, mods *index.Index, databases []string, query string, limit int, isListed func(index.Site) bool,
) []Node {
	query = strings.ToLower(query)
	type match struct {
		site  index.Site
		label string
		rank  int
	}

	var matches []match
	for _, database := range databases {
		for _, site := range mods.ListDefinitions(database) {
			label := strings.ToLower(mods.ToReference(site).Name)
			if strings.Contains(label, query) && isListed(site) {
				matches = append(matches, match{site, label, files.ClassifyMatch(label, query)})
			}
		}
	}

	slices.SortFunc(matches, func(first, second match) int {
		return cmp.Or(cmp.Compare(first.rank, second.rank), strings.Compare(first.label, second.label))
	})

	roots := []Node{}
	for _, best := range matches[:min(limit, len(matches))] {
		node := makeNode(ws, mods, best.site, 0)
		node.Picture, node.Silhouette = findPicture(ws, mods, best.site)
		roots = append(roots, node)
	}

	return roots
}

// isKept reports whether the game runs a definition: it is the kept copy,
// one merged into it, or, where no source says which copy is kept, any
// loaded copy.
func isKept(ws *workspace.Session, mods *index.Index, site index.Site) bool {
	return slices.Contains(listActing(ws, mods, site), site)
}

// ListOwnRoots returns the events and the definitions of the types the Graph
// asks about that the workspace's mods define and the game keeps. It returns
// nil if no workspace is open.
func (s *Service) ListOwnRoots() []Node {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil
	}

	mods := ws.Mods.Load()
	roots := []Node{}
	for _, database := range slices.Concat([]string{ws.Schema.Events}, listGraphDatabases(ws)) {
		for _, site := range mods.ListDefinitions(database) {
			if mods.File(site).Origin != files.OriginVanilla && isKept(ws, mods, site) {
				roots = append(roots, makeNode(ws, mods, site, 0))
			}
		}
	}

	return roots
}

// ListDefinitions returns the kept definitions of one type, database by
// database, each in the order the game's files define them. It returns nil if
// no workspace is open or the game has no such type.
func (s *Service) ListDefinitions(definitionType string) []Node {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil
	}

	draft := newDraft(ws, ws.Mods.Load())
	var nodes []Node
	for _, id := range draft.listDefinitions(lang.Type(definitionType)) {
		if node, isDefined := draft.build(id); isDefined {
			nodes = append(nodes, node)
		}
	}

	return nodes
}
