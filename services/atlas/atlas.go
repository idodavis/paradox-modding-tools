// Package atlas answers the Atlas page: what a definition of the game is
// tied to, as the nodes and edges of one canvas, with the detail of a node
// and the lists the page offers. It keeps nothing between calls. Each answer
// reads the open workspace's index and the files it needs.
//
// A subject is drawn by what it is:
//
//   - An event, on_action, decision, message or scripted effect: what it
//     fires and what fires it, step by step, as the index lists firings.
//   - A good or a category of goods: the building lines that make it with
//     what they take, and the lines and needs that take it.
//   - A building, or a production method of one: its line of levels as a
//     table, its production methods, the goods it takes and makes, and what
//     unlocks it.
//   - A technology, innovation or advance: the tree it is in.
//
// The rules every answer keeps:
//
//   - A node stands at the copy of a name the game keeps. A name with no
//     kept copy is no node.
//   - A node is of every origin that writes a copy the game reads: the kept
//     one, or one merged into it. Its dot shows the last of them in load
//     order, and an origin filter draws a node any drawn origin writes.
//   - A name has one node on a canvas, whichever way leads to it.
//   - A link to a name the game keeps no definition of is left out.
//   - A text is said as the one line a player reads, and a number as the
//     game shows it.
//
// # Robustness
//
// With no workspace open, a canvas and a detail are an error and a list is
// empty. A subject the game keeps no definition of, or of a type Atlas does
// not draw, is an error that says so. A definition whose file changed since
// the scan is drawn bare: its name and place, with no picture, comment or
// trigger. A file that cannot be walked gives a firing its two ends and no
// blocks. An amount that is no number is left out.
package atlas

import (
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Atlas page.
type Service struct {
	Workspace *workspace.Service
}

// Node is one definition of the game on a canvas or in a list, or a node
// that stands for several: a fold, a group or a loop's marker.
type Node struct {
	// ID is database:name. A fold's is more:<id> or more-in:<id>, a loop's
	// fold loop:<id>, a marker back:<from>:<to>, a group's in:<group> or
	// out:<group>, a block's block:<heading>, and the places a good is found
	// are their database and a colon.
	ID string `json:"id"`

	Database string `json:"database"`
	Name     string `json:"name"`

	// Type is what the definition is, as good or technology. It is "" for a
	// definition of no type Atlas draws.
	Type lang.Type `json:"type,omitempty"`

	// What is what its database calls a definition, as "scripted effect".
	What string `json:"what"`

	// Title is its title as one line, "" if it has none.
	Title string `json:"title"`

	// Display is how the node is drawn: window or hidden for an event,
	// message, on_action, decision or effect. It is "" for any other.
	Display string `json:"display,omitempty"`

	// Picture is the file of its picture, "" if none is found. Silhouette is
	// set where the game tints it in the color of the text. Painting is the
	// file of the painting behind its head.
	Picture    string `json:"picture,omitempty"`
	Silhouette bool   `json:"silhouette,omitempty"`
	Painting   string `json:"painting,omitempty"`

	// Group is the id of the group it is listed under, and Era the id of a
	// technology's era or age. Each is "" for none.
	Group string `json:"group,omitempty"`
	Era   string `json:"era,omitempty"`

	// Year is the year an era starts in, "" where the game writes none.
	Year string `json:"year,omitempty"`

	// Summary is the quiet line under its name on a card, "" for none. Facts
	// are the facts a card shows as chips, at most three.
	Summary string `json:"summary,omitempty"`
	Facts   []Fact `json:"facts,omitempty"`

	// Trigger is its own trigger in words, set on a canvas and in a detail.
	Trigger []Condition `json:"trigger,omitempty"`

	// Comment is the comment lines just above its definition.
	Comment string `json:"comment,omitempty"`

	// Hardcoded is set for an on_action the game fires from its own code.
	Hardcoded bool `json:"hardcoded,omitempty"`

	// Origin is the origin its dot shows: the last in load order that writes
	// a copy the game reads, the kept one or one merged into it. CopyOrigin
	// is the origin of the latest copy its definition hides, "" for none.
	Origin     string `json:"origin"`
	CopyOrigin string `json:"copyOrigin,omitempty"`

	Site lsp.Site `json:"site"`

	// Distance is how many steps it stands from the subject, below zero for
	// what leads to the subject. Parent is the drawn node one step nearer,
	// "" for the subject.
	Distance int    `json:"distance"`
	Parent   string `json:"parent"`

	// Count is how many a fold or a group stands for, how many advances a
	// tree's root leads, or how many technologies of the tree an era holds.
	// It is 0 for any other node.
	Count int `json:"count"`

	// Deep is set where the depth stopped at a node that leads further.
	// Looped is set for a firer that reaches the subject only round a loop.
	Deep   bool `json:"deep"`
	Looped bool `json:"looped,omitempty"`

	// Fires is every firing it makes, in the order written, set only in a
	// detail.
	Fires []Edge `json:"fires,omitempty"`

	// Members are the definitions a group stands for, the one that takes the
	// most first. A category of goods that is the subject has its goods.
	Members []Node `json:"members,omitempty"`

	// Unlocks is what a technology unlocks. Root is the id of the advance an
	// EU5 advance's own tree starts from, Audience the id of whom it is for,
	// and Limit what the game limits it to in words. Each is empty for none.
	Unlocks  []Node `json:"unlocks,omitempty"`
	Root     string `json:"root,omitempty"`
	Audience string `json:"audience,omitempty"`
	Limit    string `json:"limit,omitempty"`

	// Line is a building's line as a table, set on a building that is the
	// subject.
	Line *Line `json:"line,omitempty"`
}

// Edge is one firing, from the node that fires, or one link between two
// definitions.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`

	// Delay is a firing's delay in words, as "10–30 days", "" for none.
	Delay string `json:"delay,omitempty"`

	// Steps are the blocks a firing is written inside, outermost first.
	Steps []Step `json:"steps,omitempty"`

	// Effect is the key that writes a firing, as trigger_event, and Title
	// the title that statement gives it.
	Effect string `json:"effect,omitempty"`
	Title  string `json:"title,omitempty"`

	// Via are the names of what the edge passes that is no node: the
	// scripted effects of a firing, the category a need asks for.
	Via []string `json:"via,omitempty"`

	// Relation is what a link says of its ends, as input or unlock, "" for a
	// firing. Amount is how much of a good it takes or makes in words, ""
	// for none, and Share is set where that is a share of what is produced.
	Relation index.Relation `json:"relation,omitempty"`
	Amount   string         `json:"amount,omitempty"`
	Share    bool           `json:"share,omitempty"`

	// Site is where the firing or the link is written.
	Site lsp.Site `json:"site"`
}

// Filter says what a canvas shows and how far it goes from its subject.
type Filter struct {
	Databases []string          `json:"databases" jsonschema:"the databases of the nodes an event's graph draws, by the part of a node's id before its colon, such as events"`
	Origins   []string          `json:"origins" jsonschema:"the mods drawn, by id, and vanilla for the game's own files; none draws every origin"`
	Depth     int               `json:"depth" jsonschema:"how many steps from the subject an event's graph draws"`
	Limit     int               `json:"limit" jsonschema:"the most nodes an event's graph draws"`
	Fanout    int               `json:"fanout" jsonschema:"the firings of one node drawn before the rest fold; 0 folds none"`
	Expand    []string          `json:"expand" jsonschema:"the ids of the folds and groups opened, and of the nodes the depth stopped at that are opened"`
	Follow    bool              `json:"follow" jsonschema:"whether a scripted effect is walked into for the events it fires"`
	Labels    map[string]string `json:"labels,omitempty" jsonschema:"what each database's nodes are called, for the page"`
	Era       string            `json:"era,omitempty" jsonschema:"an era or age's id: only what is available by it, unlocked by a technology of it or an earlier one"`
	Potential string            `json:"potential,omitempty" jsonschema:"EU5 only: which of an age's advances to draw beside those of every country: the id of one country, culture or religion; empty draws the subject's own"`
	Picked    []string          `json:"picked,omitempty" jsonschema:"ids of production methods: each one's group of a building runs it; a group none names runs its first"`
}

// Column is the heading over the nodes one distance from the subject on a
// good's or a building's canvas.
type Column struct {
	Distance int `json:"distance"`

	// Title is the heading, as "Made by", and Summary the quiet line under
	// it, as "9 building lines".
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

// Graph is the nodes and edges around a subject. It has no coordinates: the
// page lays it out.
type Graph struct {
	Root  string `json:"root"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`

	// Truncated is set when the limit cut the graph. Total is how many nodes
	// were found before it did.
	Truncated bool `json:"truncated"`
	Total     int  `json:"total"`

	// Columns are the headings of a good's or a building's canvas.
	Columns []Column `json:"columns,omitempty"`

	// Eras are the game's eras in its own order. A good's and a building's
	// canvas list them all, and a tree's those of its technologies.
	Eras []Node `json:"eras,omitempty"`

	// Trees are the game's technology trees, and Audiences whom an EU5 age's
	// advances are for. Unlocks is what CK3's innovations unlock, by
	// database, each with how many innovations unlock it. Only a tree has
	// them.
	Trees     []Tree     `json:"trees,omitempty"`
	Audiences []Audience `json:"audiences,omitempty"`
	Unlocks   []Node     `json:"unlocks,omitempty"`

	// Needs is how many technologies the subject requires at any distance,
	// Leads how many require it, and SharedLeads how many of those every
	// country can have.
	Needs       int `json:"needs,omitempty"`
	Leads       int `json:"leads,omitempty"`
	SharedLeads int `json:"sharedLeads,omitempty"`
}

// The ways a node of an event's graph is drawn.
const (
	displayWindow   = "window"
	displayHidden   = "hidden"
	displayMessage  = "message"
	displayOnAction = "on_action"
	displayDecision = "decision"
	displayEffect   = "effect"
)

// hiddenKey is the key of an event the player never sees: hidden = yes.
const hiddenKey = "hidden"

// The filter an event's graph starts with.
const (
	defaultDepth  = 3
	defaultLimit  = 200
	defaultFanout = 6
)

// Defaults returns the filter a canvas starts with: every database that
// fires and the scripted effects, every origin, three steps, 200 nodes, six
// firings a node, through scripted effects, with every fold closed. It
// returns the zero filter if no workspace is open.
func (s *Service) Defaults() Filter {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Filter{}
	}

	filter := Filter{
		Databases: slices.Concat(ws.Schema.FireDatabases, []string{ws.Schema.Effects}),
		Origins:   []string{files.OriginVanilla},
		Depth:     defaultDepth, Limit: defaultLimit, Fanout: defaultFanout, Follow: true,
		Expand: []string{}, Labels: map[string]string{},
	}
	for _, mod := range *ws.Order.Load() {
		filter.Origins = append(filter.Origins, mod.ID)
	}

	for _, database := range filter.Databases {
		filter.Labels[database] = lang.DescribeDefinition(database, true)
	}

	return filter
}

// BuildGraph returns the canvas of a subject under a filter. It returns an
// error if no workspace is open, the game keeps no definition of the
// subject, or Atlas draws nothing of its type.
func (s *Service) BuildGraph(subject string, filter Filter) (Graph, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Graph{}, err
	}

	mods := ws.Mods.Load()
	definition, err := findSubject(ws, mods, subject)
	if err != nil {
		return Graph{}, err
	}

	database := mods.ToReference(definition).Database
	isFired := slices.Contains(ws.Schema.FireDatabases, database) || database == ws.Schema.Effects
	switch nodeType := ws.Schema.LookupType(database); {
	case isFired:
		return reachFirings(ws, mods, definition, filter), nil
	case nodeType == lang.TypeGood || nodeType == lang.TypeGoodCategory:
		return buildGood(ws, mods, definition, filter), nil
	case nodeType == lang.TypeBuilding || nodeType == lang.TypeProductionMethod:
		return buildBuilding(ws, mods, definition, filter), nil
	case nodeType == lang.TypeTechnology:
		return buildTree(ws, mods, definition, filter), nil
	}

	return Graph{}, fmt.Errorf("nothing is drawn for a %s such as %s", lang.DescribeDefinition(database, false), subject)
}

// findSubject returns the definition a node's id names: the copy the game
// keeps, else the first copy a file keeps for itself, as a scripted effect
// an events file defines. It returns an error if the game keeps none.
func findSubject(ws *workspace.Session, mods *index.Index, id string) (index.Site, error) {
	database, name, _ := strings.Cut(id, ":")
	reference := lang.Reference{Database: database, Name: name}
	if winner, ok := mods.FindKept(ws.Schema, reference); ok {
		return winner.Site, nil
	}

	for _, local := range mods.ListDefinitions(database) {
		if mods.ToReference(local).Name != mods.ToCaseless(database, name) {
			continue
		}

		if winner, ok := mods.FindKeptLocal(ws.Schema, reference, local.File); ok {
			return winner.Site, nil
		}
	}

	return index.Site{}, fmt.Errorf("the game keeps no definition %q", id)
}

// makeNode returns the node of a definition as a list shows it: its names,
// its title and picture, its place, its origin and the copy it hides.
func makeNode(ws *workspace.Session, mods *index.Index, definition index.Site) Node {
	reference := mods.ToReference(definition)
	winner, _ := mods.FindKeptLocal(ws.Schema, reference, definition.File)
	origins := listOrigins(mods, winner, definition)
	node := Node{
		ID: reference.ToID(), Database: reference.Database, Name: reference.Name,
		Type: ws.Schema.LookupType(reference.Database), What: lang.DescribeDefinition(reference.Database, false),
		Title: ws.LookupTitle(mods, definition), Display: findDisplay(ws.Schema, reference.Database),
		Origin: origins[len(origins)-1], Site: lsp.MakeSite(ws, mods, definition),
	}
	node.Picture, node.Silhouette = ws.FindPicture(mods, definition)
	if declared := ws.Schema.LookupDefinition(reference.Database, reference.Name); declared != nil {
		node.Hardcoded = declared.Hardcoded
	}

	if len(winner.Beaten) > 0 {
		node.CopyOrigin = mods.File(winner.Beaten[len(winner.Beaten)-1]).Origin
	}

	return node
}

// describeNode adds what a canvas and a detail show of a node from its
// file: the comment above it, whether an event is hidden, and its trigger
// in words. A definition whose file changed since the scan stays bare, with
// no picture and no display.
func describeNode(ws *workspace.Session, mods *index.Index, node *Node, definition index.Site) {
	tree, statement, err := ws.FindStatement(mods, definition)
	if err != nil {
		node.Picture, node.Display = "", ""

		return
	}

	lines := strings.Split(tree.FindComment(statement), "\n")
	for position, line := range lines {
		lines[position] = strings.TrimSpace(strings.TrimLeft(line, "#"))
	}

	node.Comment = strings.Join(lines, "\n")
	if hidden, _ := tree.FindChildValue(statement, hiddenKey); hidden == lang.Yes && node.Display == displayWindow {
		node.Display = displayHidden
	}

	node.Trigger = describeTrigger(ws, mods, definition)
}

// findDisplay returns how the nodes of a database are drawn in an event's
// graph, "" for a database with no look of its own.
func findDisplay(schema *lang.Schema, database string) string {
	switch nodeType := schema.LookupType(database); {
	case database == schema.Events:
		return displayWindow
	case database == schema.Messages:
		return displayMessage
	case database == schema.Effects:
		return displayEffect
	case nodeType == lang.TypeOnAction:
		return displayOnAction
	case nodeType == lang.TypeDecision:
		return displayDecision
	}

	return ""
}

// listOrigins returns the origins that write a copy of a definition the game
// reads, in load order: the kept copy's and those of the copies merged into
// it.
func listOrigins(mods *index.Index, winner index.Winner, definition index.Site) []string {
	var origins []string
	for _, written := range slices.Concat(winner.Merged, []index.Site{definition}) {
		if origin := mods.File(written).Origin; !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}

	return origins
}

// isDrawn reports whether an origin filter draws a definition: one of the
// filter's origins writes a copy of it the game reads. A filter that names no
// origin draws every definition.
func isDrawn(ws *workspace.Session, mods *index.Index, filter Filter, definition index.Site) bool {
	if len(filter.Origins) == 0 {
		return true
	}

	winner, _ := mods.FindKeptLocal(ws.Schema, mods.ToReference(definition), definition.File)

	return slices.ContainsFunc(listOrigins(mods, winner, definition), func(origin string) bool {
		return slices.Contains(filter.Origins, origin)
	})
}
