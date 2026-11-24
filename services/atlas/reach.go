// reach.go walks from a subject to what it is tied to under a filter: the
// firings of an event's graph, step by step, and the makers and users of a
// good.

package atlas

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// The starts of the ids of the nodes that stand for several: the firings
// past a node's fan-out, forward and backward, the firers that come round a
// loop, a firing back into its own chain, the groups of a good's canvas, for
// what leads to the good and for what it leads to, and a block of the
// advances that no requirement ties to a tree.
const (
	foldPrefix       = "more:"
	foldBackPrefix   = "more-in:"
	loopPrefix       = "loop:"
	backMarkerPrefix = "back:"
	groupInPrefix    = "in:"
	groupOutPrefix   = "out:"
	blockPrefix      = "block:"
)

// The most lines one side of a good draws before they stand under their
// groups, and the most members an opened group draws.
const (
	maxSide    = 12
	maxMembers = 8
)

// reach is one walk from a subject: the canvas found so far, and where each
// of its nodes is defined.
type reach struct {
	ws     *workspace.Session
	mods   *index.Index
	filter Filter
	graph  Graph

	// sites holds the definition of each drawn node, and positions where
	// each stands among the graph's nodes, by id.
	sites     map[string]index.Site
	positions map[string]int

	// callers holds what calls each scripted effect drawn on the way back
	// from the subject, as the firings found so far name them.
	callers map[index.Site][]index.Site

	// edges holds the firings added so far, by their ends, delay and
	// passage. walked holds the nodes expanded in the direction being walked.
	edges  map[string]bool
	walked map[string]bool

	// cut is how many nodes the limit left out.
	cut int
}

// hop is one step from a node to the next drawn one: the definition it leads
// to, the reference that writes it, and the names of what it passes that is
// not drawn.
type hop struct {
	end index.Site
	use index.Site
	via []string
}

// noSite is the reference of a hop that no statement writes, as a scripted
// effect whose call is not found.
var noSite = index.Site{File: -1, Occurrence: -1}

// startReach returns a walk with its subject drawn as the root.
func startReach(ws *workspace.Session, mods *index.Index, subject index.Site, filter Filter) *reach {
	r := &reach{
		ws: ws, mods: mods, filter: filter, sites: map[string]index.Site{}, positions: map[string]int{},
		callers: map[index.Site][]index.Site{}, edges: map[string]bool{},
	}
	r.graph.Root = r.add(subject, "", 0)

	return r
}

// reachFirings returns the event graph of a subject: what it fires, then
// what fires it, hop by hop to the filter's depth.
func reachFirings(ws *workspace.Session, mods *index.Index, subject index.Site, filter Filter) Graph {
	r := startReach(ws, mods, subject, filter)
	for _, isForward := range []bool{true, false} {
		r.walked = map[string]bool{}
		frontier := []string{r.graph.Root}
		for depth := 1; len(frontier) > 0; depth++ {
			var next []string
			for _, id := range frontier {
				next = append(next, r.expand(id, depth, isForward)...)
			}

			frontier = next
		}
	}

	r.graph.Total = len(r.graph.Nodes) + r.cut

	return r.graph
}

// add draws the node of a definition one step from its parent and returns its
// id. A definition drawn already keeps its place.
func (r *reach) add(definition index.Site, parent string, distance int) string {
	node := makeNode(r.ws, r.mods, definition)
	if _, drawn := r.positions[node.ID]; drawn {
		return node.ID
	}

	describeNode(r.ws, r.mods, &node, definition)
	node.Parent, node.Distance = parent, distance
	r.put(node, definition)

	return node.ID
}

// put adds a node to the graph, with the definition it stands at.
func (r *reach) put(node Node, definition index.Site) {
	r.sites[node.ID], r.positions[node.ID] = definition, len(r.graph.Nodes)
	r.graph.Nodes = append(r.graph.Nodes, node)
}

// expand draws what one node leads to in a direction, at the given distance
// from the subject, and returns the nodes to go on from. Past the filter's
// depth it goes on only from a node the filter opens, and marks a node it
// stops at that leads further. Past the fan-out the rest fold into one node,
// which the filter opens. A firer found through a node the subject itself
// leads to comes round a loop: it folds into that node's loop, drawn only
// where the filter opens it.
func (r *reach) expand(id string, depth int, isForward bool) []string {
	if r.walked[id] {
		return nil
	}

	r.walked[id] = true
	node, definition := &r.graph.Nodes[r.positions[id]], r.sites[id]
	hops := r.listHops(definition, isForward, noSite, nil, map[index.Site]bool{definition: true})
	if depth > r.filter.Depth && !slices.Contains(r.filter.Expand, id) {
		node.Deep = node.Deep || len(hops) > 0

		return nil
	}

	isLooped := !isForward && (node.Distance > 0 || node.Looped)
	fold, room := r.makeFold(id, depth, isForward, isLooped, len(hops))

	var next, folded []string
	for _, step := range hops {
		end := r.mods.ToReference(step.end).ToID()
		position, isDrawn := r.positions[end]
		isFull := r.filter.Limit > 0 && len(r.graph.Nodes) >= r.filter.Limit
		switch {
		case isForward && end != id && r.isAbove(end, id):
			r.addMarker(id, end, depth, step)
		case isDrawn && !isForward && end != id && r.graph.Nodes[position].Distance >= 0:
			next = append(next, end)
		case isDrawn:
			r.link(id, end, isForward, step)
		case slices.Contains(folded, end):
		case room == 0 || isFull:
			folded = append(folded, end)
			fold.What = foldWhat(fold.What, len(folded), r.mods.ToReference(step.end).Database)
			if room > 0 {
				r.cut, r.graph.Truncated = r.cut+1, true
			}

		default:
			room--
			next = append(next, r.add(step.end, id, fold.Distance))
			r.graph.Nodes[r.positions[end]].Looped = isLooped
			r.link(id, end, isForward, step)
		}
	}

	if fold.Count = len(folded); fold.Count > 0 && room == 0 {
		r.put(fold, noSite)
	}

	return next
}

// makeFold returns the node the firings of one node fold into, and how many
// of the given count are drawn before the rest fold: all of them where the
// filter opens the fold, none of a closed loop, else the filter's fan-out.
func (r *reach) makeFold(id string, depth int, isForward, isLooped bool, count int) (Node, int) {
	fold := Node{ID: foldPrefix + id, Parent: id, Distance: depth}
	switch {
	case isLooped:
		fold.ID, fold.Distance = loopPrefix+id, -depth
	case !isForward:
		fold.ID, fold.Distance = foldBackPrefix+id, -depth
	}

	isOpen := slices.Contains(r.filter.Expand, fold.ID)
	switch {
	case isLooped && !isOpen:
		return fold, 0
	case r.filter.Fanout > 0 && !isOpen:
		return fold, r.filter.Fanout
	}

	return fold, count
}

// foldWhat returns what a fold's ends are called, given what the ends before
// this one were: the database's word for several, or "" once two differ.
func foldWhat(what string, count int, database string) string {
	plural := lang.DescribeDefinition(database, true)
	if count == 1 || what == plural {
		return plural
	}

	return ""
}

// isAbove reports whether one drawn node stands on another's path to the
// subject, itself included.
func (r *reach) isAbove(above, id string) bool {
	for current := id; current != ""; current = r.graph.Nodes[r.positions[current]].Parent {
		if current == above {
			return true
		}
	}

	return false
}

// addMarker draws a firing back into the walk's own chain as a marker under
// the node that fires. The marker carries what the node it stands for
// carries, under an id of its own.
func (r *reach) addMarker(id, end string, depth int, step hop) {
	marker := r.graph.Nodes[r.positions[end]]
	marker.ID, marker.Parent, marker.Distance = backMarkerPrefix+id+":"+end, id, depth
	marker.Deep, marker.Looped = false, false
	if _, drawn := r.positions[marker.ID]; !drawn {
		r.put(marker, r.sites[end])
	}

	r.link(id, marker.ID, true, step)
}

// link adds the edge of one hop between two drawn nodes, from the one that
// fires. Two edges that join the same nodes with the same delay through the
// same effects are one.
func (r *reach) link(id, end string, isForward bool, step hop) {
	from, to := id, end
	if !isForward {
		from, to = end, id
	}

	edge := Edge{From: from, To: to}
	if step.use != noSite {
		edge = makeEdge(r.ws, r.mods, from, to, step.use)
	}

	edge.Via = step.via
	key := strings.Join(append([]string{from, to, edge.Delay}, edge.Via...), "\x00")
	if !r.edges[key] {
		r.edges[key] = true
		r.graph.Edges = append(r.graph.Edges, edge)
	}
}

// listHops returns the steps from a definition to the next drawn ones in a
// direction. A node the filter does not draw is passed through: each hop
// through it keeps the first firing and names the node in what it passes.
func (r *reach) listHops(definition index.Site, isForward bool, first index.Site, passed []string, seen map[index.Site]bool) []hop {
	var hops []hop
	for _, step := range r.listSteps(definition, isForward) {
		if first != noSite {
			step.use = first
		}

		step.via = slices.Concat(passed, step.via)
		database := r.mods.ToReference(step.end).Database
		isShown := slices.Contains(r.filter.Databases, database) && isDrawn(r.ws, r.mods, r.filter, step.end)
		switch {
		case isShown:
			hops = append(hops, step)
		case !seen[step.end]:
			seen[step.end] = true
			through := slices.Concat(step.via, []string{r.mods.ToReference(step.end).Name})
			hops = append(hops, r.listHops(step.end, isForward, step.use, through, seen)...)
		}
	}

	return hops
}

// listSteps returns the firings from or to one definition as single steps.
// Where the filter draws scripted effects, a firing through effects steps to
// the first of them, or back to the last, and what calls each effect on the
// way back is kept for its own turn. Otherwise the step is the firing's other
// end, and the effects are what it passes.
func (r *reach) listSteps(definition index.Site, isForward bool) []hop {
	schema := r.ws.Schema
	drawsEffects := slices.Contains(r.filter.Databases, schema.Effects)

	var fires []index.Fire
	var steps []hop
	if isForward {
		for _, written := range r.mods.ListActing(schema, r.mods.ToReference(definition), definition.File) {
			fires = append(fires, r.mods.ListFires(schema, written, r.filter.Follow)...)
		}
	} else {
		fires = r.mods.ListFiredBy(schema, definition, r.filter.Follow)
		for _, caller := range r.callers[definition] {
			steps = append(steps, hop{end: caller, use: r.findCall(caller, definition)})
		}
	}

	for _, fire := range fires {
		step := hop{end: fire.Target, use: fire.Use}
		if !isForward {
			step.end = fire.Source
		}

		switch {
		case len(fire.Via) == 0:
		case !drawsEffects:
			for _, effect := range fire.Via {
				step.via = append(step.via, r.mods.ToReference(effect).Name)
			}

		case isForward:
			step = hop{end: fire.Via[0], use: r.findCall(definition, fire.Via[0])}
		default:
			step.end = fire.Via[len(fire.Via)-1]
			for position, effect := range fire.Via {
				caller := fire.Source
				if position > 0 {
					caller = fire.Via[position-1]
				}

				if !slices.Contains(r.callers[effect], caller) {
					r.callers[effect] = append(r.callers[effect], caller)
				}
			}
		}

		steps = append(steps, step)
	}

	return steps
}

// findCall returns the reference by which one definition calls a scripted
// effect, or noSite where it writes none.
func (r *reach) findCall(caller, effect index.Site) index.Site {
	name := r.mods.ToReference(effect).Name
	for _, use := range r.mods.ListReferences(caller) {
		reference := r.mods.ToReference(use)
		if reference.Name == name && (reference.Database == r.ws.Schema.Effects || reference.Database == lang.AnyCall) {
			return use
		}
	}

	return noSite
}

// makerRelations are the ways a line makes a good, in the order a card says
// them. relationWords is what a card calls each relation.
var (
	makerRelations = []index.Relation{index.RelationOutput, index.RelationBoost, index.RelationGives}
	relationWords  = map[index.Relation]string{
		index.RelationOutput: "produces", index.RelationBoost: "boosts", index.RelationGives: "gives",
		index.RelationInput: "takes",
	}
)

// amount is one number a link writes: what the link does, and whether the
// number is a share of what is produced. A plain amount and a share are
// never said as one range.
type amount struct {
	relation index.Relation
	isShare  bool
	number   float64

	// group is the production method group of the method that writes the
	// number, noSite for a writer in no group.
	group index.Site
}

// tie is what one building line has to do with a good.
type tie struct {
	// line is the line's first level, or a definition that stands for
	// itself, as a pop need. use is the first reference that writes the tie.
	line index.Site
	use  index.Site

	// relation is the first thing the line does to the good, in the order of
	// makerRelations, and amounts every number its levels and methods write.
	relation index.Relation
	amounts  []amount

	// levels are the places in the line of the levels that write the tie,
	// counted from one, and count how many levels the line has.
	levels []int
	count  int

	// via is the title of the category a need asks for, "" where the need
	// names the good.
	via string

	// takes are the links to the goods the line takes to make the good.
	takes []index.Link
}

// buildGood returns the canvas of a good or of a category of goods: the lines
// that make it on the left with what they take, and the lines and needs that
// take it on the right. A category's makers and users are those of its goods,
// which are its members.
func buildGood(ws *workspace.Session, mods *index.Index, subject index.Site, filter Filter) Graph {
	r := startReach(ws, mods, subject, filter)
	r.graph.Nodes[0].Summary = describeFound(ws, mods, subject)
	r.graph.Eras = listEras(ws, mods)
	for _, link := range pickLinks(ws, mods, subject, index.RelationPart, false) {
		r.graph.Nodes[0].Members = append(r.graph.Nodes[0].Members, makeNode(ws, mods, link.Source))
	}

	makers, users := r.listTies(subject)
	made, used := Column{Title: "Made by", Summary: "No building makes it"}, Column{Title: "Used by", Summary: "Nothing takes it"}
	if len(makers) > 0 {
		made.Summary = countOf(len(makers), "building line")
	}

	if len(users) > 0 {
		used.Summary = describeNumber(float64(len(users))) + " buildings and needs"
	}

	r.drawSide(makers, -1, made)
	r.drawSide(users, 1, used)
	r.graph.Total = len(r.graph.Nodes)

	return r.graph
}

// listTies returns the lines that make a good, in the order produces, boosts,
// gives, and those that take it, the one that takes the most first. A need
// for a category is a need for each good in it, so a good's categories count
// too, with the category's title as what the tie goes through.
func (r *reach) listTies(subject index.Site) (makers, users []tie) {
	targets, vias := []index.Site{subject}, []string{""}
	for _, link := range r.mods.ListLinks(r.ws.Schema, subject, index.RelationPart) {
		if link.Source == subject {
			targets, vias = append(targets, link.Target), append(vias, r.ws.LookupTitle(r.mods, link.Target))
		} else {
			targets, vias = append(targets, link.Source), append(vias, "")
		}
	}

	relations := append(slices.Clone(makerRelations), index.RelationInput)
	for position, target := range targets {
		for _, link := range r.mods.ListLinks(r.ws.Schema, target, relations...) {
			side := &users
			if link.Relation != index.RelationInput {
				side = &makers
			}

			if link.Target != target {
				continue
			}

			for _, level := range listRunners(r.ws, r.mods, link.Source) {
				r.addTie(side, level, link, vias[position])
			}
		}
	}

	slices.SortStableFunc(makers, func(first, second tie) int {
		return slices.Index(makerRelations, first.relation) - slices.Index(makerRelations, second.relation)
	})
	slices.SortStableFunc(users, func(first, second tie) int { return cmp.Compare(second.largest(), first.largest()) })

	return makers, users
}

// largest returns the largest number a tie writes, 0 for one with none.
func (t tie) largest() float64 {
	largest := 0.0
	for _, written := range t.amounts {
		largest = max(largest, written.number)
	}

	return largest
}

// listNumbers returns the numbers a tie writes of one relation, plain or as
// shares. A Vic3 production method's amount below zero takes away from what
// the other groups give, as a method that automates laborers away: it lowers
// the least by the most its group takes away, and stands as written where
// nothing else is given.
func (t tie) listNumbers(relation index.Relation, isShare bool) []float64 {
	var numbers, given []float64
	lowered := map[index.Site]float64{}
	for _, written := range t.amounts {
		switch {
		case written.relation != relation || written.isShare != isShare:
		case written.number < 0 && written.group != noSite:
			numbers = append(numbers, written.number)
			lowered[written.group] = min(lowered[written.group], written.number)
		default:
			numbers, given = append(numbers, written.number), append(given, written.number)
		}
	}

	if len(given) == 0 || len(lowered) == 0 {
		return numbers
	}

	least := slices.Min(given)
	for _, number := range lowered {
		least += number
	}

	return []float64{least, slices.Max(given)}
}

// addTie adds one link of a building's level to the tie of its line on one
// side, making the tie on its first link. The filter keeps a line a drawn
// origin writes and, with an era, one available by it.
func (r *reach) addTie(side *[]tie, level index.Site, link index.Link, via string) {
	levels := listLevels(r.ws, r.mods, level)
	line := levels[0]
	if !isDrawn(r.ws, r.mods, r.filter, line) || !isAvailable(r.ws, r.mods, line, r.filter.Era, r.graph.Eras) {
		return
	}

	position := slices.IndexFunc(*side, func(member tie) bool { return member.line == line })
	if position < 0 {
		position = len(*side)
		*side = append(*side, tie{line: line, use: link.Use, relation: link.Relation, count: len(levels), via: via})
	}

	member := &(*side)[position]
	if slices.Index(makerRelations, link.Relation) < slices.Index(makerRelations, member.relation) {
		member.relation = link.Relation
	}

	if place := slices.Index(levels, level) + 1; !slices.Contains(member.levels, place) {
		member.levels = append(member.levels, place)
	}

	if number, ok := readAmount(r.ws, r.mods, link); ok {
		member.amounts = append(member.amounts, amount{link.Relation, link.Share, number, findMethodGroup(r.ws, r.mods, link.Source)})
	}

	if link.Relation != index.RelationInput {
		member.takes = append(member.takes, listTakes(r.ws, r.mods, link)...)
	}
}

// listRunners returns the buildings whose line a link's writer belongs to:
// the building itself, or each building that runs a production method, by its
// group where the game has groups. What is part of no building, as a pop
// need, stands for itself.
func listRunners(ws *workspace.Session, mods *index.Index, writer index.Site) []index.Site {
	if ws.Schema.LookupType(mods.ToReference(writer).Database) != lang.TypeProductionMethod {
		return []index.Site{writer}
	}

	var buildings []index.Site
	for _, link := range pickLinks(ws, mods, writer, index.RelationPart, true) {
		if ws.Schema.LookupType(mods.ToReference(link.Target).Database) == lang.TypeBuilding {
			buildings = append(buildings, link.Target)

			continue
		}

		for _, held := range pickLinks(ws, mods, link.Target, index.RelationPart, true) {
			buildings = append(buildings, held.Target)
		}
	}

	if len(buildings) == 0 {
		return []index.Site{writer}
	}

	return buildings
}

// findMethodGroup returns the production method group a link's writer is in,
// or noSite for a writer that is no production method or is in no group.
func findMethodGroup(ws *workspace.Session, mods *index.Index, writer index.Site) index.Site {
	if ws.Schema.LookupType(mods.ToReference(writer).Database) != lang.TypeProductionMethod {
		return noSite
	}

	for _, link := range pickLinks(ws, mods, writer, index.RelationPart, true) {
		if ws.Schema.LookupType(mods.ToReference(link.Target).Database) != lang.TypeBuilding {
			return link.Target
		}
	}

	return noSite
}

// listTakes returns the links to what a maker takes to make a good: the good
// of the need whose meeting gives it, or the inputs of the production method
// that makes it. A CK3 building that makes a good outright shows no need.
func listTakes(ws *workspace.Session, mods *index.Index, made index.Link) []index.Link {
	writer := made.Source
	isMethod := ws.Schema.LookupType(mods.ToReference(writer).Database) == lang.TypeProductionMethod
	tree, statement, err := ws.FindStatement(mods, made.Use)
	if err != nil || !isMethod && made.Relation != index.RelationGives {
		return nil
	}

	// A need is a block at the top of its building, which holds both the
	// good it asks for and what meeting it gives.
	need := statement
	for need != 0 && tree.Statements[need].Parent != mods.Occurrence(writer).Statement {
		need = tree.Statements[need].Parent
	}

	var takes []index.Link
	for _, link := range pickLinks(ws, mods, writer, index.RelationInput, true) {
		isOwn := link.Use.File == made.Use.File
		if isOwn && (isMethod || tree.Statements[mods.Occurrence(link.Use).Statement].Parent == need) {
			takes = append(takes, link)
		}
	}

	return takes
}

// isAvailable reports whether a line is available by an era: no technology
// unlocks its first level, or one of that era or an earlier one does. With no
// era every line is.
func isAvailable(ws *workspace.Session, mods *index.Index, line index.Site, era string, eras []Node) bool {
	limit := slices.IndexFunc(eras, func(node Node) bool { return node.ID == era })
	if limit < 0 {
		return true
	}

	isUnlocked := false
	for _, link := range pickLinks(ws, mods, line, index.RelationUnlock, false) {
		isUnlocked = true
		unlocked := findEra(ws, mods, link.Source)
		if slices.IndexFunc(eras, func(node Node) bool { return node.ID == unlocked }) <= limit {
			return true
		}
	}

	return !isUnlocked
}

// drawSide draws one side of a good under its column's heading: each tie as
// its own node where they are twelve or fewer, else a node for each widest
// group with its count, a group of one as its member.
func (r *reach) drawSide(ties []tie, direction int, column Column) {
	column.Distance = direction
	if len(ties) <= maxSide {
		for _, member := range ties {
			r.drawMember(r.describeTie(member, direction), member, direction)
		}

		r.graph.Columns = append(r.graph.Columns, column)

		return
	}

	var groups []Node
	var single []tie
	for _, member := range ties {
		group := r.findWidest(member.line)
		place := slices.IndexFunc(groups, func(other Node) bool { return other.ID == group.ID })
		if place < 0 {
			place, groups, single = len(groups), append(groups, group), append(single, member)
		}

		node := r.describeTie(member, direction)
		groups[place].Members = append(groups[place].Members, node)
		groups[place].Picture = cmp.Or(groups[place].Picture, node.Picture)
	}

	column.Summary += " in " + countOf(len(groups), "group")
	r.graph.Columns = append(r.graph.Columns, column)
	for position, group := range groups {
		if len(group.Members) == 1 {
			r.drawMember(group.Members[0], single[position], direction)

			continue
		}

		r.drawGroup(group, direction)
	}
}

// drawGroup draws a group of one side of a good. Where the filter opens it,
// its first eight members stand one step further out, then a fold for the
// rest.
func (r *reach) drawGroup(group Node, direction int) {
	root := r.graph.Root
	prefix := groupOutPrefix
	if direction < 0 {
		prefix = groupInPrefix
	}

	group.ID, group.Parent, group.Distance, group.Count = prefix+group.ID, root, direction, len(group.Members)
	r.put(group, noSite)
	r.join(root, group.ID, direction, Edge{})
	if !slices.Contains(r.filter.Expand, group.ID) {
		return
	}

	shown := group.Members[:min(len(group.Members), maxMembers)]
	for _, member := range shown {
		member.Parent, member.Distance = group.ID, direction*2
		r.put(member, noSite)
		r.join(group.ID, member.ID, direction, Edge{})
	}

	if rest := len(group.Members) - len(shown); rest > 0 {
		r.put(Node{ID: foldPrefix + group.ID, Parent: group.ID, Distance: direction * 2, Count: rest}, noSite)
	}

	r.graph.Columns = append(r.graph.Columns, Column{Distance: direction * 2, Title: group.Title, Summary: "opened"})
}

// drawMember draws one tie as its own node beside the subject, and for a
// maker the goods it takes one step further out. A line drawn already, as
// one that both makes and takes the good, keeps its node.
func (r *reach) drawMember(node Node, member tie, direction int) {
	root := r.graph.Root
	if _, drawn := r.positions[node.ID]; !drawn {
		node.Parent, node.Distance = root, direction
		r.put(node, member.line)
	}

	edge := Edge{Relation: member.relation, Site: lsp.MakeSite(r.ws, r.mods, member.use)}
	if len(node.Facts) > 0 {
		edge.Amount = node.Facts[0].Text
	}

	r.join(root, node.ID, direction, edge)
	r.drawTakes(node.ID, member.takes, direction)
}

// drawTakes draws the goods a maker takes one step further out than its
// node, each good once with the range of what the maker's methods take.
func (r *reach) drawTakes(maker string, takes []index.Link, direction int) {
	for position, taken := range takes {
		isSame := func(other index.Link) bool { return other.Target == taken.Target }
		if slices.ContainsFunc(takes[:position], isSame) {
			continue
		}

		var numbers []float64
		for _, other := range takes[position:] {
			if number, ok := readAmount(r.ws, r.mods, other); ok && isSame(other) {
				numbers = append(numbers, number)
			}
		}

		good := makeNode(r.ws, r.mods, taken.Target)
		good.Parent, good.Distance = maker, direction*2
		if _, drawn := r.positions[good.ID]; !drawn {
			r.put(good, taken.Target)
		}

		r.join(maker, good.ID, direction, Edge{
			Relation: index.RelationInput, Share: taken.Share, Amount: describeRange(numbers, taken.Share),
			Site: lsp.MakeSite(r.ws, r.mods, taken.Use),
		})
	}

	isHeaded := slices.ContainsFunc(r.graph.Columns, func(column Column) bool { return column.Distance == direction*2 })
	if len(takes) > 0 && !isHeaded {
		r.graph.Columns = append(r.graph.Columns, Column{Distance: direction * 2, Title: "What they take"})
	}
}

// join adds the edge between a node and the one a step further from the
// subject, from left to right: toward the subject on its left side, and away
// from it on its right.
func (r *reach) join(inner, outer string, direction int, edge Edge) {
	edge.From, edge.To = inner, outer
	if direction < 0 {
		edge.From, edge.To = outer, inner
	}

	r.graph.Edges = append(r.graph.Edges, edge)
}

// describeTie returns the node of one tie: the line's first level with what
// the line does to the good as facts, and as its quiet line its group, its
// levels and the category its need asks for.
func (r *reach) describeTie(member tie, direction int) Node {
	node := makeNode(r.ws, r.mods, member.line)
	for _, relation := range append(slices.Clone(makerRelations), index.RelationInput) {
		for _, isShare := range []bool{false, true} {
			numbers := member.listNumbers(relation, isShare)
			fact := Fact{Text: describeRange(numbers, isShare), Label: relationWords[relation]}
			if relation == index.RelationGives {
				fact.When = "when its need is met"
			}

			if len(numbers) > 0 {
				node.Facts = append(node.Facts, fact)
			}
		}
	}

	var parts []string
	if groups := listGroups(r.ws, r.mods, member.line); len(groups) > 0 && direction < 0 {
		parts = append(parts, groups[0].Title)
	}

	slices.Sort(member.levels)
	switch first, last := member.levels[0], member.levels[len(member.levels)-1]; {
	case direction < 0 && member.count > 1:
		parts = append(parts, countOf(member.count, "level"))
	case direction < 0 || len(member.levels) == member.count:
	case first == last:
		parts = append(parts, "level "+toRoman(first))
	default:
		parts = append(parts, "levels "+toRoman(first)+" to "+toRoman(last))
	}

	if member.via != "" {
		parts = append(parts, "as "+member.via)
	}

	node.Summary = strings.Join(parts, " · ")

	return node
}

// findWidest returns the widest group a line is listed under. A line in no
// group stands under what its database calls its definitions, with the
// database and a colon as the group's id.
func (r *reach) findWidest(line index.Site) Node {
	if groups := listGroups(r.ws, r.mods, line); len(groups) > 0 {
		return groups[len(groups)-1]
	}

	database := r.mods.ToReference(line).Database
	words := lang.DescribeDefinition(database, true)

	return Node{ID: database + ":", Database: database, Title: strings.ToUpper(words[:1]) + words[1:]}
}
