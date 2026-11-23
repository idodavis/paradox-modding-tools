// roots.go lists the definitions the Graph can be asked about.

package graph

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
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
	// the game's files reach them, with the wider group each is in. A group's
	// Count is how many definitions it and the groups in it list.
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

	return matchDefinitions(ws, mods, databases, query, limit, func(site index.Site) (index.Site, bool) {
		return site, isKept(ws, mods, site)
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
// A line counts once, a category only where a need can ask for it, and eras
// stand in the game's order.
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
			if d.model.listLine(id)[0] != id || !d.isMarked(id) {
				continue
			}

			catalog.Count++
			for _, group := range d.listCatalogChain(id) {
				groups = appendNew(groups, group)
				counts[group]++
			}
		}
	}

	slices.SortStableFunc(groups, func(first, second string) int {
		return cmp.Compare(d.rankGroup(first), d.rankGroup(second))
	})

	for _, group := range groups {
		if node, isGroup := d.makeGroup(group, counts[group]); isGroup {
			catalog.Groups = append(catalog.Groups, node)
		}
	}

	return catalog
}

// ListCatalog returns the definitions of a catalog's types whose name holds
// the query, best match first, up to a limit, each with its group. A
// building's line is listed once, by its first level, whichever of its levels
// the query names. It returns nil if no workspace is open.
func (s *Service) ListCatalog(types []lang.Type, query string, limit int) []Node {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil
	}

	mods := ws.Mods.Load()
	draft := newDraft(ws, mods)
	list := func(site index.Site) (index.Site, bool) {
		winner, isDefined := mods.FindKept(ws.Schema, mods.ToReference(site))
		if !isDefined || winner.Site != site {
			return site, false
		}

		head, err := resolveNode(ws, mods, draft.model.listLine(toID(mods, site))[0])

		return head, err == nil && draft.isMarked(toID(mods, head))
	}

	rows := matchDefinitions(ws, mods, listCatalogDatabases(ws, types), query, limit, list)
	for position := range rows {
		rows[position].Group = draft.listCatalogChain(rows[position].ID)[0]
	}

	return rows
}

func listCatalogDatabases(ws *workspace.Session, types []lang.Type) []string {
	return listTypedDatabases(ws, func(drawn lang.Type) bool { return slices.Contains(types, drawn) })
}

// audiencePrefix starts the id of the heading an age lists the advances of
// one audience under, before the audience and the age.
const audiencePrefix = "audience:"

// listCatalogChain returns the groups a catalog lists a definition under,
// nearest first, "" alone for none. An advance that is not for every country
// stands under a heading for its audience inside its age.
func (d *draft) listCatalogChain(id string) []string {
	chain := d.listGroupChain(id)
	if len(chain) == 0 {
		return []string{""}
	}

	audience, _ := d.findAudience(id)
	if audienceGroups[d.ws.Game.ID] == nil || d.typeOf(id) != lang.TypeTechnology || audience == sharedAdvances {
		return chain
	}

	return slices.Insert(chain, 0, audiencePrefix+audience+writtenGroupSeparator+chain[0])
}

// matchDefinitions returns the definitions whose name, or else the plain
// words of whose title, hold the query, each once with its picture, the best
// match first, up to a limit. The given function says which definition a
// match lists: itself, the first level of its line, or none.
func matchDefinitions(
	ws *workspace.Session, mods *index.Index, databases []string, query string, limit int,
	list func(index.Site) (index.Site, bool),
) []Node {
	query = strings.ToLower(query)
	type match struct {
		site      index.Site
		id, label string
		rank      int
	}

	matches := map[string]match{}
	for _, database := range databases {
		for _, site := range mods.ListDefinitions(database) {
			label := strings.ToLower(mods.ToReference(site).Name)
			if !strings.Contains(label, query) {
				title := ws.LookupDefinitionText(mods, site, lang.TitleKey)
				label = strings.ToLower(loc.ToPlain(ws.ExpandText(mods, title)))
			}

			listed, isListed := list(site)
			if !strings.Contains(label, query) || !isListed {
				continue
			}

			id, rank := toID(mods, listed), files.ClassifyMatch(label, query)
			if best, isMatched := matches[id]; !isMatched || rank < best.rank {
				matches[id] = match{listed, id, label, rank}
			}
		}
	}

	ranked := slices.SortedFunc(maps.Values(matches), func(first, second match) int {
		return cmp.Or(cmp.Compare(first.rank, second.rank), strings.Compare(first.label, second.label), strings.Compare(first.id, second.id))
	})

	roots := []Node{}
	for _, best := range ranked[:min(limit, len(ranked))] {
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
