// Package graph is the service of the Event Graph page. It builds the graph
// of what fires what from one root, and the detail of one node.
package graph

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Event Graph.
type Service struct {
	Workspace *workspace.Service
}

// Node is one entry of the event graph, or a fold that stands for the rest
// of a node's firings.
type Node struct {
	// ID is database:name. A fold's is more:<id> or more-in:<id>.
	ID string `json:"id"`

	Database string `json:"database"`
	Name     string `json:"name"`

	// What is what its database calls a definition, such as "scripted
	// effect".
	What string `json:"what"`

	// Title is its localized title, "" if it has none.
	Title string `json:"title"`

	// Display is window, hidden or message for an event or a message, else "".
	Display string `json:"display,omitempty"`

	// Background is the path of an event's picture, "" if none is found.
	Background string `json:"background,omitempty"`

	// Trigger is its own trigger in words. It is set only in a graph and a
	// detail.
	Trigger []Condition `json:"trigger,omitempty"`

	// Comment is the comment lines just above its definition.
	Comment string `json:"comment,omitempty"`

	// Hardcoded is set for an on_action the game fires from its own code.
	Hardcoded bool `json:"hardcoded,omitempty"`

	// CopyOrigin is the origin of the copy its definition hides, "" for none.
	CopyOrigin string `json:"copyOrigin,omitempty"`

	Site lsp.Site `json:"site"`

	// Step is its distance from the root, negative for a node that fires
	// the root.
	Step int `json:"step"`

	// Parent is the drawn node one step nearer the root, "" for the root.
	Parent string `json:"parent"`

	// Count is how many firings a fold stands for, 0 for a definition.
	Count int `json:"count"`

	// Deep is set where the depth stopped at a node that leads further.
	Deep bool `json:"deep"`

	// Looped is set for a firer of the root that the backward walk found
	// through a node the root itself fires.
	Looped bool `json:"looped,omitempty"`

	// Fires is every firing it makes, in the order written. It is set only
	// in a detail.
	Fires []Edge `json:"fires,omitempty"`
}

// Edge is one firing, from the node that fires to the node it fires.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`

	// Delay is the delay in words, such as "10–30 days", "" for none.
	Delay string `json:"delay,omitempty"`

	// Steps are the blocks the firing is written inside, from the top of
	// its definition.
	Steps []Step `json:"steps,omitempty"`

	// Effect is the key of the statement that writes the firing, such as
	// trigger_event, and Title the title that statement gives it.
	Effect string `json:"effect,omitempty"`
	Title  string `json:"title,omitempty"`

	// Via are the scripted effects the firing passes through that are not
	// drawn as nodes.
	Via []string `json:"via,omitempty"`

	// Site is where the firing is written.
	Site lsp.Site `json:"site"`
}

// Filter says what the graph shows and how far it goes from its root.
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

// Graph is the event graph from one root. It has no coordinates: the page
// lays it out.
type Graph struct {
	Root  string `json:"root"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`

	// Truncated is set when the limit cut the graph. Total is how many nodes
	// were found before it did.
	Truncated bool `json:"truncated"`
	Total     int  `json:"total"`
}

// The ways the player sees an event or a message.
const (
	displayWindow  = "window"
	displayHidden  = "hidden"
	displayMessage = "message"
)

// reach is one graph while it is built from its root.
type reach struct {
	ws     *workspace.Session
	mods   *index.Index
	filter Filter
	nodes  []Node

	// ids holds the id of every node added.
	ids map[string]bool

	edges []Edge

	// count is how many nodes count toward the limit; hidden ones do not.
	// cut holds the ids of the nodes the limit kept out.
	count int
	cut   map[string]bool
}

// BuildGraph builds the graph of what the root fires and what fires it. It
// returns an error if the game keeps no definition of the root.
func (s *Service) BuildGraph(root string, filter Filter) (Graph, error) {
	ws, err := s.Workspace.Session()
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
		Root: root, Nodes: reach.nodes, Edges: dropRepeats(reach.edges, toKey),
		Truncated: len(reach.cut) > 0, Total: reach.count + len(reach.cut),
	}, nil
}

// resolveNode returns the definition the game keeps of a node's id. Where
// no copy is kept for every file, it takes the first loaded copy that a file
// keeps for itself, such as a scripted effect an events file defines.
func resolveNode(ws *workspace.Session, mods *index.Index, id string) (index.Site, error) {
	database, name, _ := strings.Cut(id, ":")
	reference := lang.Reference{Database: database, Name: name}
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

// hiddenKey is the key whose yes makes an event show no window.
const hiddenKey = "hidden"

// makeNode returns the node of a definition at a step from the root,
// without the picture and trigger that only a drawn node needs.
func makeNode(ws *workspace.Session, mods *index.Index, site index.Site, step int) Node {
	reference := mods.ToReference(site)
	node := Node{
		ID:       toID(mods, site),
		Database: reference.Database,
		Name:     reference.Name,
		What:     lang.DescribeDefinition(reference.Database, false),
		Title:    toTitle(ws, mods, ws.LookupDefinitionText(mods, site, lang.TitleKey)),
		Site:     lsp.MakeSite(ws, mods, site),
		Step:     step,
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
		node.Display = displayWindow
		if hidden, _ := tree.FindChildValue(statement, hiddenKey); hidden == lang.Yes {
			node.Display = displayHidden
		}
	}

	return node
}

// drawNode returns the node of a definition as the page draws it, with an
// event's picture and the definition's trigger.
func drawNode(ws *workspace.Session, mods *index.Index, site index.Site, step int) Node {
	node := makeNode(ws, mods, site, step)
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return node
	}

	if node.Display == displayWindow || node.Display == displayHidden {
		node.Background = findPicture(ws, mods, tree, statement, lang.LookupDialect(ws.Game.ID).Events.Picture)
	}

	node.Trigger = findCondition(ws, mods, tree, statement)

	return node
}

// findCopy returns the copy of a definition's name, the latest in load
// order, that the kept copy hides. It returns false if it hides none.
func findCopy(ws *workspace.Session, mods *index.Index, site index.Site) (index.Site, bool) {
	winner, _ := mods.FindKeptLocal(ws.Schema, mods.ToReference(site), site.File)
	if len(winner.Beaten) == 0 {
		return index.Site{}, false
	}

	return winner.Beaten[len(winner.Beaten)-1], true
}

func toID(mods *index.Index, site index.Site) string { return mods.ToReference(site).ToID() }

// expand adds the nodes forward or backward from the root, step by step.
// Past the filter's depth it goes on only from the nodes the filter opens,
// and marks the rest as leading further.
func (r *reach) expand(root index.Site, forward bool) {
	frontier := []index.Site{root}
	for distance := 1; len(frontier) > 0; distance++ {
		if distance > r.filter.Depth {
			parents := toParents(r.listHidden())
			var opened, rest []index.Site
			for _, site := range frontier {
				if slices.Contains(r.filter.Expand, findDrawn(parents, toID(r.mods, site))) {
					opened = append(opened, site)
				} else {
					rest = append(rest, site)
				}
			}

			frontier = opened
			r.markDeep(rest, forward)
		}

		step := distance
		if !forward {
			step = -distance
		}

		var next []index.Site
		for _, site := range frontier {
			for _, fire := range r.fold(site, r.listLinks(site, forward), forward, step) {
				if end, isNew := r.addFire(fire, forward, step); isNew {
					next = append(next, end)
				}
			}
		}

		frontier = next
	}
}

// listHidden returns the nodes the filter hides. The root and the folds are
// never hidden.
func (r *reach) listHidden() []Node {
	var hidden []Node
	for _, node := range r.nodes {
		if node.Step != 0 && node.Count == 0 && r.isHidden(node.Database, node.Site.Place.Origin) {
			hidden = append(hidden, node)
		}
	}

	return hidden
}

func toParents(nodes []Node) map[string]string {
	parents := map[string]string{}
	for _, node := range nodes {
		parents[node.ID] = node.Parent
	}

	return parents
}

// findDrawn returns the nearest drawn node at or above a node, climbing
// through the given parents of the hidden nodes.
func findDrawn(parents map[string]string, id string) string {
	for next, ok := parents[id]; ok; next, ok = parents[id] {
		id = next
	}

	return id
}

// markDeep marks each of the nodes that leads further in the given
// direction.
func (r *reach) markDeep(sites []index.Site, forward bool) {
	for _, site := range sites {
		id := toID(r.mods, site)
		position := slices.IndexFunc(r.nodes, func(node Node) bool { return node.Count == 0 && node.ID == id })
		if position >= 0 && len(r.listLinks(site, forward)) > 0 {
			r.nodes[position].Deep = true
		}
	}
}

// listLinks returns what a node fires going forward, or what fires it going
// backward.
func (r *reach) listLinks(site index.Site, forward bool) []index.Fire {
	if forward {
		return listFires(r.ws, r.mods, site, r.filter.Follow)
	}

	return r.mods.ListFiredBy(r.ws.Schema, site, r.filter.Follow)
}

// fold returns a node's firings up to the filter's fan-out, and adds one
// fold node for the rest.
func (r *reach) fold(site index.Site, fires []index.Fire, forward bool, step int) []index.Fire {
	parent := toID(r.mods, site)
	id, edge := "more-in:"+parent, Edge{From: "more-in:" + parent, To: parent}
	if forward {
		id, edge = "more:"+parent, Edge{From: parent, To: "more:" + parent}
	}

	if !r.isFolded(len(fires), id) {
		return fires
	}

	rest := fires[r.filter.Fanout:]
	database := func(fire index.Fire) string {
		if forward {
			return r.mods.ToReference(fire.Target).Database
		}

		return r.mods.ToReference(fire.Source).Database
	}

	// A fold of one kind says what its firings are.
	node := Node{ID: id, Step: step, Parent: parent, Count: len(rest)}
	if kinds := dropRepeats(rest, database); len(kinds) == 1 {
		node.What = lang.DescribeDefinition(database(kinds[0]), len(rest) > 1)
	}

	r.nodes = append(r.nodes, node)
	r.edges = append(r.edges, edge)

	return fires[:r.filter.Fanout]
}

// isFolded reports whether a node's firings pass the fan-out while their
// fold is closed.
func (r *reach) isFolded(count int, id string) bool {
	return r.filter.Fanout > 0 && count > r.filter.Fanout && !slices.Contains(r.filter.Expand, id)
}

// listFires returns what a node fires: the firings of every copy of its
// name the game runs. With follow, it adds what the scripted effects it
// calls fire.
func listFires(ws *workspace.Session, mods *index.Index, site index.Site, follow bool) []index.Fire {
	var fires []index.Fire
	for _, part := range listActing(ws, mods, site) {
		fires = append(fires, mods.ListFires(ws.Schema, part, follow)...)
	}

	return fires
}

// listActing returns the copies of a node's name that the game runs, as
// seen from the given copy's file.
func listActing(ws *workspace.Session, mods *index.Index, site index.Site) []index.Site {
	return mods.ListActing(ws.Schema, mods.ToReference(site), site.File)
}

// addFire puts a firing and its nodes in the graph. Where the filter draws
// scripted effects, each effect the firing passes through is a node on the
// way. It returns the far end and whether that node is new; nothing past the
// node limit is added.
func (r *reach) addFire(fire index.Fire, forward bool, step int) (index.Site, bool) {
	path := []index.Site{fire.Source, fire.Target}
	if slices.Contains(r.filter.Databases, r.ws.Schema.Effects) {
		path = slices.Concat([]index.Site{fire.Source}, fire.Via, []index.Site{fire.Target})
	}

	if !forward {
		slices.Reverse(path)
	}

	end := path[len(path)-1]
	isNew := !r.ids[toID(r.mods, end)]
	whole := makeEdge(r.ws, r.mods, fire)
	for position := 1; position < len(path); position++ {
		if !r.add(path[position], step, path[position-1]) {
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

		edge.From, edge.To = toID(r.mods, source), toID(r.mods, target)
		if len(path) > 2 {
			edge.Via = nil
		}

		r.edges = append(r.edges, edge)
	}

	return end, isNew
}

// add puts a definition's node in the graph, reached from the given parent.
// A name has one node, whichever copy leads to it. It returns false if the
// node limit keeps it out.
func (r *reach) add(site index.Site, step int, parent index.Site) bool {
	id := toID(r.mods, site)
	if r.ids[id] {
		return true
	}

	if !r.isHidden(r.mods.ToReference(site).Database, r.mods.File(site).Origin) {
		if r.count >= r.filter.Limit {
			r.cut[id] = true

			return false
		}

		r.count++
	}

	r.ids[id] = true
	node := drawNode(r.ws, r.mods, site, step)
	node.Parent = toID(r.mods, parent)
	node.Looped = step < 0 && r.isLooping(node.Parent)
	r.nodes = append(r.nodes, node)

	return true
}

// isLooping reports whether a firer found through another node comes round
// a loop: that node is one the root fires, or was found round a loop itself.
func (r *reach) isLooping(id string) bool {
	return slices.ContainsFunc(r.nodes, func(node Node) bool { return node.ID == id && (node.Step > 0 || node.Looped) })
}

// isHidden reports whether the filter hides a node of a database and an
// origin. Only the kinds listKinds gives are hidden by kind.
func (r *reach) isHidden(database, origin string) bool {
	isHiddenKind := !slices.Contains(r.filter.Databases, database) && slices.Contains(listKinds(r.ws.Schema), database)

	return isHiddenKind || !slices.Contains(r.filter.Origins, origin)
}

// listKinds returns the databases the filter picks among: those whose
// definitions fire one another, and the scripted effects.
func listKinds(schema *lang.Schema) []string {
	kinds := slices.Clone(schema.FireDatabases)
	if schema.Effects != "" {
		kinds = append(kinds, schema.Effects)
	}

	return kinds
}

// splice takes the hidden nodes out and joins the edges through each. A
// joined edge keeps the first firing's delay and steps.
func (r *reach) splice() {
	hidden := r.listHidden()
	for _, node := range hidden {
		var rest, incoming, outgoing []Edge
		for _, edge := range r.edges {
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

		r.edges = rest
	}

	parents := toParents(hidden)
	r.nodes = slices.DeleteFunc(r.nodes, func(node Node) bool {
		_, isHidden := parents[node.ID]

		return isHidden
	})
	r.passDeep(hidden, parents)
	for position := range r.nodes {
		r.nodes[position].Parent = findDrawn(parents, r.nodes[position].Parent)
	}
}

// passDeep moves a hidden node's deep mark to the nearest drawn node above
// it, so that opening that node goes on past the hidden one.
func (r *reach) passDeep(hidden []Node, parents map[string]string) {
	for _, node := range hidden {
		if !node.Deep {
			continue
		}

		drawn := findDrawn(parents, node.Parent)
		if position := slices.IndexFunc(r.nodes, func(other Node) bool { return other.ID == drawn }); position >= 0 {
			r.nodes[position].Deep = true
		}
	}
}

// toKey returns a key that two edges share when they join the same nodes
// the same way.
func toKey(edge Edge) string {
	return strings.Join(slices.Concat([]string{edge.From, edge.To, edge.Delay}, edge.Via), "\x00")
}

// dropRepeats returns the items whose key no earlier item has, in order.
func dropRepeats[Item any, Key comparable](items []Item, key func(Item) Key) []Item {
	kept, seen := []Item{}, map[Key]bool{}
	for _, item := range items {
		if itemKey := key(item); !seen[itemKey] {
			seen[itemKey] = true
			kept = append(kept, item)
		}
	}

	return kept
}

// FindRoots returns the kept nodes whose name holds the query, the best
// match first, up to a limit. It returns nil if no workspace is open.
func (s *Service) FindRoots(query string, limit int) []Node {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil
	}

	mods := ws.Mods.Load()
	query = strings.ToLower(query)
	type match struct {
		site  index.Site
		label string
		rank  int
	}

	var matches []match
	for _, database := range ws.Schema.FireDatabases {
		for _, site := range mods.ListDefinitions(database) {
			label := strings.ToLower(mods.ToReference(site).Name)
			if strings.Contains(label, query) && isKept(ws, mods, site) {
				matches = append(matches, match{site, label, files.ClassifyMatch(label, query)})
			}
		}
	}

	slices.SortFunc(matches, func(first, second match) int {
		return cmp.Or(cmp.Compare(first.rank, second.rank), strings.Compare(first.label, second.label))
	})

	roots := []Node{}
	for _, one := range matches[:min(limit, len(matches))] {
		roots = append(roots, makeNode(ws, mods, one.site, 0))
	}

	return roots
}

// isKept reports whether the game runs a definition: it is the kept copy,
// one merged into it, or, where no source says which copy is kept, any
// loaded copy.
func isKept(ws *workspace.Session, mods *index.Index, site index.Site) bool {
	return slices.Contains(listActing(ws, mods, site), site)
}

// ListOwnRoots returns the events the workspace's mods define that the game
// keeps. It returns nil if no workspace is open.
func (s *Service) ListOwnRoots() []Node {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil
	}

	mods := ws.Mods.Load()
	roots := []Node{}
	for _, site := range mods.ListDefinitions(ws.Schema.Events) {
		if mods.File(site).Origin != files.OriginVanilla && isKept(ws, mods, site) {
			roots = append(roots, makeNode(ws, mods, site, 0))
		}
	}

	return roots
}

// Defaults returns the filter for a new graph: every kind and origin drawn,
// every fold closed. It returns an empty filter if no workspace is open.
func (s *Service) Defaults() Filter {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Filter{}
	}

	databases := listKinds(ws.Schema)
	origins := []string{files.OriginVanilla}
	for _, mod := range *ws.Order.Load() {
		origins = append(origins, mod.ID)
	}

	labels := map[string]string{}
	for _, database := range databases {
		labels[database] = lang.DescribeDefinition(database, true)
	}

	return Filter{Databases: databases, Origins: origins, Depth: 3, Limit: 200, Fanout: 6, Follow: true, Labels: labels}
}
