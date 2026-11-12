// Package graph is the Event Graph page's service. It builds the graph of what fires what from one root, and the
// detail of one node.
package graph

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Event Graph.
type Service struct {
	Workspace *workspace.Service
}

// A Node is one entry of the event graph, or a fold standing for the rest of a node's firings.
type Node struct {
	ID         string      `json:"id"` // database:name; more:<id> or more-in:<id> for a fold
	Database   string      `json:"database"`
	Name       string      `json:"name"`
	What       string      `json:"what"`                 // what a definition of its database is called, such as "scripted effect"
	Title      string      `json:"title"`                // its localized title, each data call in words; "" if it has none
	Display    string      `json:"display,omitempty"`    // how the player sees an event or a message: window, hidden or message; "" for any other entry
	Background string      `json:"background,omitempty"` // the path of an event's picture; "" if none is found
	Trigger    []Condition `json:"trigger,omitempty"`    // its own trigger in words; nil for none, for a fold, and outside a graph and a detail
	Comment    string      `json:"comment,omitempty"`    // the comment lines just above its definition, joined by newlines
	Hardcoded  bool        `json:"hardcoded,omitempty"`  // an on_action the game calls from its own code
	CopyOrigin string      `json:"copyOrigin,omitempty"` // the origin of the latest-loaded copy of its name that its definition hides; "" for none
	Definition index.Site  `json:"-"`                    // the definition the node stands for, which only Go reads
	Site       lsp.Site    `json:"site"`
	Step       int         `json:"step"`             // its distance from the root, negative for a firer
	Parent     string      `json:"parent"`           // the drawn node one step nearer the root; "" for the root
	Count      int         `json:"count"`            // the firings a fold stands for; 0 for a definition
	Deep       bool        `json:"deep"`             // the depth stopped at it, and it links further to a next step
	Looped     bool        `json:"looped,omitempty"` // a firer the backward reach found through a node the root itself fires: it fires the root only round a loop of the root's own chain
	Fires      []Edge      `json:"fires,omitempty"`  // every firing it makes, in the order written; nil outside a detail
}

// An Edge is one firing, from a firer to what it fires.
type Edge struct {
	From   string   `json:"from"`
	To     string   `json:"to"`
	Delay  string   `json:"delay,omitempty"`  // the delay in words, such as 7 days or 10–30 days; "" for none
	Steps  []Step   `json:"steps,omitempty"`  // the blocks it is written inside, from the top of its definition
	Effect string   `json:"effect,omitempty"` // the key of the statement that writes it, such as trigger_event
	Title  string   `json:"title,omitempty"`  // the title that statement gives it, each data call in words
	Via    []string `json:"via,omitempty"`    // the scripted effects passed through, when they are not drawn as nodes
	Site   lsp.Site `json:"site"`             // where the firing is written
}

// A Filter says what the graph shows and how far it reaches from its root.
type Filter struct {
	Databases []string          `json:"databases" jsonschema:"the kinds of node drawn, by the part of a node's id before its colon, such as events"`
	Origins   []string          `json:"origins" jsonschema:"the mods drawn, by id, and vanilla for the game's own files"`
	Depth     int               `json:"depth" jsonschema:"how many steps from the root are drawn"`
	Limit     int               `json:"limit" jsonschema:"the most nodes drawn"`
	Fanout    int               `json:"fanout" jsonschema:"the firings of one node drawn before the rest fold; 0 folds none"`
	Expand    []string          `json:"expand" jsonschema:"the folds opened, and the nodes the depth stopped at that are opened"`
	Follow    bool              `json:"follow" jsonschema:"whether a scripted effect is walked into for the events it fires"`
	Labels    map[string]string `json:"labels,omitempty" jsonschema:"what each kind's nodes are called, for the page"`
}

// A Graph is the event graph from one root. It has no coordinates.
type Graph struct {
	Root      string `json:"root"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	Truncated bool   `json:"truncated"` // the limit cut it
	Total     int    `json:"total"`     // how many nodes the reach found before the limit cut it
}

// The ways the player sees an event or a message.
const (
	displayWindow  = "window"  // an event that shows its window
	displayHidden  = "hidden"  // an event that shows nothing
	displayMessage = "message" // a message sent to the player
)

// A reach is one graph while it is built from its root.
type reach struct {
	ws     *workspace.Session
	mods   *index.Index
	filter Filter
	nodes  []Node
	ids    map[string]bool // the ids of nodes, one for each name
	edges  []Edge
	count  int             // how many of the nodes the filter draws
	cut    map[string]bool // the ids of the nodes the limit kept out
}

// BuildGraph builds the graph of what the root fires and what fires it, under the given filter. It returns an error if
// the game keeps no definition of the root.
func (gph *Service) BuildGraph(root string, filter Filter) (Graph, error) {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return Graph{}, err
	}
	mods := ws.Mods.Load()
	site, err := resolveNode(ws, mods, root)
	if err != nil {
		return Graph{}, err
	}
	reach := &reach{ws: ws, mods: mods, filter: filter, ids: map[string]bool{toID(mods, site): true}, count: 1, cut: map[string]bool{}}
	reach.nodes = []Node{drawNode(ws, mods, site, 0)}
	reach.expand(site, true)
	reach.expand(site, false)
	reach.splice()
	return Graph{
		Root: root, Nodes: reach.nodes, Edges: lo.UniqBy(reach.edges, toKey),
		Truncated: len(reach.cut) > 0, Total: reach.count + len(reach.cut),
	}, nil
}

// resolveNode returns the definition the game keeps of the node with the given id, `database:name`. It returns an
// error if the game keeps none.
func resolveNode(ws *workspace.Session, mods *index.Index, id string) (index.Site, error) {
	database, name, _ := strings.Cut(id, ":")
	reference := schema.Reference{Database: database, Name: name}
	if winner, ok := mods.FindKept(ws.Schema, reference); ok {
		return winner.Site, nil
	}
	for _, site := range mods.ListDefinitions(database) {
		if mods.ToReference(site).Name != name || !mods.IsLoaded(site.File) {
			continue
		}
		if winner, ok := mods.FindKeptLocal(ws.Schema, reference, site.File); ok {
			return winner.Site, nil
		}
	}
	return index.Site{}, fmt.Errorf("the game keeps no definition %q", id)
}

// makeNode returns the node of a definition, at the given step from the root, without its picture.
func makeNode(ws *workspace.Session, mods *index.Index, site index.Site, step int) Node {
	reference := mods.ToReference(site)
	node := Node{
		ID:         toID(mods, site),
		Database:   reference.Database,
		Name:       reference.Name,
		What:       game.DescribeDefinition(reference.Database, false),
		Title:      toTitle(ws, mods, ws.LookupDefinitionText(mods, site, game.TitleKey)),
		Definition: site,
		Site:       lsp.MakeSite(ws, mods, site),
		Step:       step,
	}
	if hidden, ok := findCopy(ws, mods, site); ok {
		node.CopyOrigin = mods.File(hidden).Origin
	}
	if declared := ws.Schema.LookupDefinition(reference.Database, reference.Name); declared != nil {
		node.Hardcoded = declared.Hardcoded
	}
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return node
	}
	node.Comment = tree.FindComment(statement)
	switch reference.Database {
	case ws.Schema.Messages:
		node.Display = displayMessage
	case ws.Schema.Events:
		hidden, _ := tree.FindChildValue(statement, game.HiddenKey)
		node.Display = lo.Ternary(hidden == game.Bools[0], displayHidden, displayWindow)
	}
	return node
}

// drawNode returns the node of a definition as the page draws it, at the given step from the root: makeNode's, with an
// event's picture and trigger.
func drawNode(ws *workspace.Session, mods *index.Index, site index.Site, step int) Node {
	node := makeNode(ws, mods, site, step)
	if node.Display == displayWindow || node.Display == displayHidden {
		node.Background = findPicture(ws, mods, site, ws.Game.Events.Picture)
	}
	if tree, statement, err := ws.FindStatement(mods, site); err == nil {
		node.Trigger = findCondition(ws, mods, tree, statement)
	}
	return node
}

// findCopy returns the last copy in load order that the kept copy of a
// definition's name beats. It returns false if it beats none.
func findCopy(ws *workspace.Session, mods *index.Index, site index.Site) (index.Site, bool) {
	winner, _ := mods.FindKeptLocal(ws.Schema, mods.ToReference(site), site.File)

	return lo.Last(winner.Beaten)
}

// toID returns the id of the node of a definition: its database and its name.
func toID(mods *index.Index, site index.Site) string { return mods.ToReference(site).ToID() }

// expand adds the nodes further from the root, forward or backward, up to the filter's depth.
func (reach *reach) expand(root index.Site, forward bool) {
	frontier := []index.Site{root}
	for distance := 1; len(frontier) > 0; distance++ {
		if distance > reach.filter.Depth {
			parents := toParents(reach.listHidden())
			var rest []index.Site
			frontier, rest = lo.FilterReject(frontier, func(site index.Site, _ int) bool {
				return slices.Contains(reach.filter.Expand, findDrawn(parents, toID(reach.mods, site)))
			})
			reach.markDeep(rest, forward)
		}
		step := lo.Ternary(forward, distance, -distance)
		var next []index.Site
		for _, site := range frontier {
			for _, fire := range reach.fold(site, reach.listLinks(site, forward), forward, step) {
				if end, isNew := reach.addFire(fire, forward, step); isNew {
					next = append(next, end)
				}
			}
		}
		frontier = next
	}
}

// listHidden returns the nodes the filter hides. The root and the folds are never hidden.
func (reach *reach) listHidden() []Node {
	return lo.Filter(reach.nodes, func(node Node, _ int) bool {
		return node.Step != 0 && node.Count == 0 && reach.isHidden(node.Definition)
	})
}

// toParents returns the parent of each of the given nodes, by the node's id.
func toParents(nodes []Node) map[string]string {
	return lo.SliceToMap(nodes, func(node Node) (string, string) { return node.ID, node.Parent })
}

// findDrawn returns the id of the nearest drawn node at or before the node with the given id, going up through the
// given parents of the hidden nodes.
func findDrawn(parents map[string]string, id string) string {
	for next, ok := parents[id]; ok; next, ok = parents[id] {
		id = next
	}
	return id
}

// markDeep marks as deep each of the given nodes that links further in the given direction.
func (reach *reach) markDeep(sites []index.Site, forward bool) {
	for _, site := range sites {
		id := toID(reach.mods, site)
		position := slices.IndexFunc(reach.nodes, func(node Node) bool { return node.Count == 0 && node.ID == id })
		if position >= 0 && len(reach.listLinks(site, forward)) > 0 {
			reach.nodes[position].Deep = true
		}
	}
}

// listLinks returns what a node fires going forward, or what fires it going backward.
func (reach *reach) listLinks(site index.Site, forward bool) []index.Fire {
	if forward {
		return listFires(reach.ws, reach.mods, site, reach.filter.Follow)
	}
	return reach.mods.ListFiredBy(reach.ws.Schema, site, reach.filter.Follow)
}

// fold returns a node's firings up to the filter's fan-out and adds the rest to the graph as one fold node, at the
// given step from the root.
func (reach *reach) fold(site index.Site, fires []index.Fire, forward bool, step int) []index.Fire {
	parent := toID(reach.mods, site)
	id := lo.Ternary(forward, "more:", "more-in:") + parent
	if !reach.isFolded(len(fires), id) {
		return fires
	}
	rest := fires[reach.filter.Fanout:]
	databases := lo.Uniq(lo.Map(rest, func(fire index.Fire, _ int) string {
		return reach.mods.ToReference(lo.Ternary(forward, fire.Target, fire.Source)).Database
	}))
	node := Node{ID: id, Step: step, Parent: parent, Count: len(rest)}
	if len(databases) == 1 {
		node.What = game.DescribeDefinition(databases[0], len(rest) > 1)
	}
	reach.nodes = append(reach.nodes, node)
	reach.edges = append(reach.edges, lo.Ternary(forward, Edge{From: parent, To: id}, Edge{From: id, To: parent}))
	return fires[:reach.filter.Fanout]
}

// isFolded reports whether the filter folds the given count of a node's firings or rows into the given fold.
func (reach *reach) isFolded(count int, id string) bool {
	return reach.filter.Fanout > 0 && count > reach.filter.Fanout && !slices.Contains(reach.filter.Expand, id)
}

// listFires returns what a node fires. When following, it includes what the scripted effects the node calls fire.
func listFires(ws *workspace.Session, mods *index.Index, site index.Site, follow bool) []index.Fire {
	return lo.FlatMap(listActing(ws, mods, site), func(part index.Site, _ int) []index.Fire {
		return mods.ListFires(ws.Schema, part, follow)
	})
}

// listActing returns the copies of a node's name that the game runs, as seen from the file of the given copy.
func listActing(ws *workspace.Session, mods *index.Index, site index.Site) []index.Site {
	return mods.ListActing(ws.Schema, mods.ToReference(site), site.File)
}

// addFire puts a firing in the graph, with its nodes, at the given step from the root. It returns the node of the
// firing that is farther from the root, and whether the graph is new to it. Nothing past the node limit is added.
func (reach *reach) addFire(fire index.Fire, forward bool, step int) (index.Site, bool) {
	path := []index.Site{fire.Source, fire.Target}
	if slices.Contains(reach.filter.Databases, reach.ws.Schema.Effects) {
		path = slices.Concat([]index.Site{fire.Source}, fire.Via, []index.Site{fire.Target})
	}
	if !forward {
		slices.Reverse(path)
	}
	end := path[len(path)-1]
	isNew := !reach.ids[toID(reach.mods, end)]
	whole := makeEdge(reach.ws, reach.mods, fire)
	for position := 1; position < len(path); position++ {
		if !reach.add(path[position], step, path[position-1]) {
			return end, false
		}
		source, target := path[position-1], path[position]
		if !forward {
			source, target = target, source
		}
		edge := Edge{Site: whole.Site}
		if target == fire.Target {
			edge = whole
		}
		edge.From, edge.To = toID(reach.mods, source), toID(reach.mods, target)
		if len(path) > 2 {
			edge.Via = nil
		}
		reach.edges = append(reach.edges, edge)
	}
	return end, isNew
}

// add puts the node of a definition in the graph, at the given step from the root, reached from the given parent. A
// name has one node, whichever copy reaches it. It returns false if the node limit keeps it out.
func (reach *reach) add(site index.Site, step int, parent index.Site) bool {
	id := toID(reach.mods, site)
	if reach.ids[id] {
		return true
	}
	if !reach.isHidden(site) {
		if reach.count >= reach.filter.Limit {
			reach.cut[id] = true
			return false
		}
		reach.count++
	}
	reach.ids[id] = true
	node := drawNode(reach.ws, reach.mods, site, step)
	node.Parent = toID(reach.mods, parent)
	node.Looped = step < 0 && reach.isLooping(node.Parent)
	reach.nodes = append(reach.nodes, node)
	return true
}

// isLooping reports whether a firer reached backward from the node with the given id comes round a loop: the node is
// one the root fires, or was itself reached round one.
func (reach *reach) isLooping(id string) bool {
	return slices.ContainsFunc(reach.nodes, func(node Node) bool { return node.ID == id && (node.Step > 0 || node.Looped) })
}

// isHidden reports whether the filter hides a definition's node by its origin or its kind. Only the kinds Defaults
// lists can be hidden.
func (reach *reach) isHidden(site index.Site) bool {
	database := reach.mods.ToReference(site).Database
	hiddenKind := !slices.Contains(reach.filter.Databases, database) && slices.Contains(listKinds(reach.ws.Schema), database)
	return hiddenKind || !slices.Contains(reach.filter.Origins, reach.mods.File(site).Origin)
}

// listKinds returns the databases whose nodes the filter picks among: the fired databases and the scripted effects.
func listKinds(schema *schema.Schema) []string {
	kinds := slices.Clone(schema.FireDatabases)
	if schema.Effects != "" {
		kinds = append(kinds, schema.Effects)
	}
	return kinds
}

// splice removes the hidden nodes, joining the edges through each. A joined edge keeps the first firing's delay and
// blocks.
func (reach *reach) splice() {
	hidden := reach.listHidden()
	for _, node := range hidden {
		var rest, incoming, outgoing []Edge
		for _, edge := range reach.edges {
			switch {
			case edge.From == node.ID && edge.To == node.ID: // a loop on the node goes with it
			case edge.To == node.ID:
				incoming = append(incoming, edge)
			case edge.From == node.ID:
				outgoing = append(outgoing, edge)
			default:
				rest = append(rest, edge)
			}
		}
		for _, first := range incoming {
			for _, second := range outgoing {
				joined := first
				joined.To, joined.Via = second.To, slices.Concat(first.Via, []string{node.Name}, second.Via)
				rest = append(rest, joined)
			}
		}
		reach.edges = rest
	}
	parents := toParents(hidden)
	reach.nodes = lo.Reject(reach.nodes, func(node Node, _ int) bool { return lo.HasKey(parents, node.ID) })
	reach.passDeep(hidden, parents)
	for position := range reach.nodes {
		reach.nodes[position].Parent = findDrawn(parents, reach.nodes[position].Parent)
	}
}

// passDeep gives the deep mark of each hidden node to the nearest drawn node before it, so that opening that node goes
// on past the hidden one.
func (reach *reach) passDeep(hidden []Node, parents map[string]string) {
	for _, node := range hidden {
		if !node.Deep {
			continue
		}
		drawn := findDrawn(parents, node.Parent)
		if position := slices.IndexFunc(reach.nodes, func(other Node) bool { return other.ID == drawn }); position >= 0 {
			reach.nodes[position].Deep = true
		}
	}
}

// toKey returns a text that is the same for two edges exactly when they join the same nodes the same way.
func toKey(edge Edge) string {
	return strings.Join(slices.Concat([]string{edge.From, edge.To, edge.Delay}, edge.Via), "\x00")
}

// FindRoots returns up to the given limit of nodes whose name holds the query, best first. It returns nil if no
// workspace is open.
func (gph *Service) FindRoots(query string, limit int) []Node {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return nil
	}
	mods := ws.Mods.Load()
	query = strings.ToLower(query)
	type match struct {
		site index.Site
		name string
		rank int
	}
	var matches []match
	for _, database := range ws.Schema.FireDatabases {
		for _, site := range mods.ListDefinitions(database) {
			name := strings.ToLower(mods.ToReference(site).Name)
			if strings.Contains(name, query) && isKept(ws, mods, site) {
				matches = append(matches, match{site, name, files.ClassifyMatch(name, query)})
			}
		}
	}
	slices.SortFunc(matches, func(first, second match) int {
		return cmp.Or(cmp.Compare(first.rank, second.rank), strings.Compare(first.name, second.name))
	})
	return lo.Map(matches[:min(limit, len(matches))], func(one match, _ int) Node {
		return makeNode(ws, mods, one.site, 0)
	})
}

// isKept reports whether a definition takes part in what the game runs, as
// the index says: the copy it keeps, one merged into it, or, where no source
// says which copy it keeps, any copy in a file it reads.
func isKept(ws *workspace.Session, mods *index.Index, site index.Site) bool {
	return slices.Contains(listActing(ws, mods, site), site)
}

// ListOwnRoots returns the events the workspace's mods define that the game keeps. It returns nil if no workspace is
// open.
func (gph *Service) ListOwnRoots() []Node {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return nil
	}
	mods := ws.Mods.Load()
	own := lo.Filter(mods.ListDefinitions(ws.Schema.Events), func(site index.Site, _ int) bool {
		return mods.File(site).Origin != files.OriginVanilla && isKept(ws, mods, site)
	})
	return lo.Map(own, func(site index.Site, _ int) Node { return makeNode(ws, mods, site, 0) })
}

// Defaults returns the filter for a new graph. It returns an empty filter if no workspace is open.
func (gph *Service) Defaults() Filter {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return Filter{}
	}
	databases := listKinds(ws.Schema)
	origins := []string{files.OriginVanilla}
	for _, mod := range *ws.Order.Load() {
		origins = append(origins, mod.ID)
	}
	labels := lo.SliceToMap(databases, func(database string) (string, string) {
		return database, game.DescribeDefinition(database, true)
	})
	return Filter{Databases: databases, Origins: origins, Depth: 3, Limit: 200, Fanout: 6, Follow: true, Labels: labels}
}
