// Package graph is the service of the Graph page. It builds the view of one
// definition and the detail of one node: what an event, an on_action, a
// decision, a message or a scripted effect fires, the chain of a good, the
// line of a building and the tree of a technology. The page lays a view out;
// every amount is worded here.
//
// The rules of the games it applies:
//
//   - The game keeps one copy of a name, and a node is drawn at that copy.
//     Where no copy is kept for every file, it is the first loaded copy a
//     file keeps for itself. A name with no kept definition is no node.
//   - A node fires what every copy of its name that the game runs fires,
//     through the scripted effects it calls. A node the filter hides is
//     spliced out and its edges joined.
//   - The links between goods, buildings, production methods and
//     technologies are rows that cite the game's file and line. The first
//     row that fits a link reads it.
//   - A building is a line of levels: CK3's next_building and
//     previous_building, EU5's obsolete, one level on Vic3. An amount over a
//     line is the range over every level and method that writes it.
//   - A need for a category of goods is a need for each good in it.
//   - A definition is listed under the group the game gives it. CK3 has a
//     tree for each group of innovations, Vic3 for each category, EU5 for
//     each age, with the eras in the order the files define them.
//   - An EU5 advance with no potential is for every country. Otherwise it is
//     for the countries, religions and cultures its potential names, or for
//     whoever meets its conditions. A name under NOT says who cannot have it.
//   - A modifier is named as the game names it, or by its key where no text
//     does. A plain word in a modifier block is a condition of the block,
//     and a key that starts with not_ holds when its value does not. A
//     parameter is met by any definition that grants it.
//   - A CK3 innovation's custom = <key> is a line of what it gives, in the
//     game's own words.
//   - The places a good is found are counted.
//
// # Robustness
//
// A view is refused if no workspace is open, the game keeps no definition of
// the subject, or the subject is not of the view's type. A detail is refused
// if the definition's file cannot be read.
//
// Left out: a definition whose file changed since the scan, a link to a name
// with no definition, and an amount that is no number. A firing in a file
// that cannot be walked keeps its ends and has no blocks. A missing picture
// or text is "". An event graph past the filter's limit is cut and says so.
package graph

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"weak"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Graph.
type Service struct {
	Workspace *workspace.Service
}

// Node is one definition of the game in a graph, or a fold that stands for
// the rest of a node's links.
type Node struct {
	// ID is database:name. A fold's is more:<id> or more-in:<id>, and the
	// places a good is found are their database with no name.
	ID string `json:"id"`

	Database string `json:"database"`
	Name     string `json:"name"`

	// Type is what the definition is, as good or technology. It is "" for a
	// node of the event graph.
	Type lang.Type `json:"type,omitempty"`

	// What is what its database calls a definition, as "scripted effect".
	What string `json:"what"`

	// Title is its localized title, "" if it has none.
	Title string `json:"title"`

	// Display is window, hidden or message for an event or a message, else "".
	Display string `json:"display,omitempty"`

	// Picture is the path of its picture, "" if none is found. Silhouette is
	// set where the game tints it in the text's color, so only its shape
	// counts.
	Picture    string `json:"picture,omitempty"`
	Silhouette bool   `json:"silhouette,omitempty"`

	// Painting is the path of the painting behind its head, "" if none is
	// found.
	Painting string `json:"painting,omitempty"`

	// Group is the id of the group it is listed under, "" for none.
	Group string `json:"group,omitempty"`

	// Era is the id of a technology's era or age, "" for none.
	Era string `json:"era,omitempty"`

	// Year is the year an era starts in, "" where the game writes none.
	Year string `json:"year,omitempty"`

	// Trigger is its own trigger in words, set only in a graph and a detail.
	Trigger []Condition `json:"trigger,omitempty"`

	// Comment is the comment lines just above its definition.
	Comment string `json:"comment,omitempty"`

	// Hardcoded is set for an on_action the game fires from its own code.
	Hardcoded bool `json:"hardcoded,omitempty"`

	// CopyOrigin is the origin of the copy its definition hides, "" for none.
	CopyOrigin string `json:"copyOrigin,omitempty"`

	Site lsp.Site `json:"site"`

	// Distance is how many links it is from the subject, negative for one
	// that leads to it.
	Distance int `json:"distance"`

	// Parent is the drawn node one link nearer the root, "" for the root.
	Parent string `json:"parent"`

	// Count is how many links a fold stands for, or how many definitions a
	// group lists. It is 0 for any other node.
	Count int `json:"count"`

	// Deep is set where the depth stopped at a node that leads further.
	Deep bool `json:"deep"`

	// Looped is set for a firer of the root found through a node the root
	// itself fires.
	Looped bool `json:"looped,omitempty"`

	// Fires is every firing it makes, in the order written, set only in a
	// detail.
	Fires []Edge `json:"fires,omitempty"`
}

// Edge is one firing, from the node that fires, or one link between two
// goods, buildings or technologies.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`

	// Delay is the delay in words, as "10–30 days", "" for none.
	Delay string `json:"delay,omitempty"`

	// Steps are the blocks the firing is written inside, outermost first.
	Steps []Step `json:"steps,omitempty"`

	// Effect is the key that writes the firing, as trigger_event, and Title
	// the title that statement gives it.
	Effect string `json:"effect,omitempty"`
	Title  string `json:"title,omitempty"`

	// Via are the definitions between the two ends that are no nodes: the
	// scripted effects a firing passes through, a line's production methods.
	Via []string `json:"via,omitempty"`

	// Relation is what a link says of its ends, as input or unlock, "" for a
	// firing. Amount is how much of a good it takes or makes, or how many
	// links it stands for, "" for none.
	Relation string `json:"relation,omitempty"`
	Amount   string `json:"amount,omitempty"`

	// Share marks an amount that is a share of what is produced.
	Share bool `json:"share,omitempty"`

	// Site is where the firing or the link is written.
	Site lsp.Site `json:"site"`

	// block is the statement of the block a link is written in, 0 at the top
	// of its definition. need is the id of the good or category of the need
	// it is written inside, "" for none.
	block int32
	need  string
}

// Filter says what a graph shows and how far it goes from its root.
type Filter struct {
	Databases []string          `json:"databases" jsonschema:"the databases of the nodes drawn, by the part of a node's id before its colon, such as events"`
	Origins   []string          `json:"origins" jsonschema:"the mods drawn, by id, and vanilla for the game's own files"`
	Depth     int               `json:"depth" jsonschema:"how many steps from the root are drawn"`
	Limit     int               `json:"limit" jsonschema:"the most nodes drawn"`
	Fanout    int               `json:"fanout" jsonschema:"the links of one node drawn before the rest fold; 0 folds none"`
	Expand    []string          `json:"expand" jsonschema:"the folds opened, and the nodes the depth stopped at that are opened"`
	Follow    bool              `json:"follow" jsonschema:"whether a scripted effect is walked into for the events it fires"`
	Labels    map[string]string `json:"labels,omitempty" jsonschema:"what each database's nodes are called, for the page"`
	Era       string            `json:"era,omitempty" jsonschema:"an era or age's id: only what is available by it, unlocked by a technology of it or an earlier one"`
	Changed   bool              `json:"changed,omitempty" jsonschema:"whether only what the workspace's mods add or change is drawn"`
	Potential string            `json:"potential,omitempty" jsonschema:"EU5 only: which of an age's advances to draw beside those of every country: religions, cultures, countries, other, or the id of one country, culture or religion; empty draws the subject's own"`
}

// Graph is the nodes and links around a subject. It has no coordinates: the
// page lays it out.
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

// BuildGraph returns what a subject fires and what fires it. It returns an
// error if no workspace is open, the game keeps no definition of the subject,
// or the subject is no event, on_action, decision, message or scripted effect.
func (s *Service) BuildGraph(subject string, filter Filter) (Graph, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Graph{}, err
	}

	mods := ws.Mods.Load()
	site, err := resolveNode(ws, mods, subject)
	if err != nil {
		return Graph{}, err
	}

	if !isFiringDatabase(ws.Schema, mods.ToReference(site).Database) {
		return Graph{}, fmt.Errorf("%s fires nothing and nothing fires it", subject)
	}

	return buildFires(ws, mods, site, filter), nil
}

// findNodeType returns the type a node of a database says. It is "" for a
// database of the event graph and for one with no type.
func findNodeType(schema *lang.Schema, database string) lang.Type {
	if isFiringDatabase(schema, database) {
		return ""
	}

	return schema.LookupType(database)
}

// resolveNode returns the definition the game keeps of a node's id. Where no
// copy is kept for every file, it is the first loaded copy a file keeps for
// itself, as a scripted effect an events file defines.
func resolveNode(ws *workspace.Session, mods *index.Index, id string) (index.Site, error) {
	reference := toReference(id)
	if winner, ok := mods.FindKept(ws.Schema, reference); ok {
		return winner.Site, nil
	}

	for _, site := range mods.ListDefinitions(reference.Database) {
		if mods.ToReference(site).Name != reference.Name || !mods.IsLoaded(site.File) {
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

// makeNode returns the node of a definition, without the picture and the
// trigger that only a drawn node needs.
func makeNode(ws *workspace.Session, mods *index.Index, site index.Site, distance int) Node {
	reference := mods.ToReference(site)
	node := Node{
		ID:       toID(mods, site),
		Database: reference.Database,
		Name:     reference.Name,
		Type:     findNodeType(ws.Schema, reference.Database),
		What:     lang.DescribeDefinition(reference.Database, false),
		Title:    describeLabel(ws, mods, ws.LookupDefinitionText(mods, site, lang.TitleKey)),
		Site:     lsp.MakeSite(ws, mods, site),
		Distance: distance,
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

// drawNode returns the node of a definition with its picture and trigger.
func drawNode(ws *workspace.Session, mods *index.Index, site index.Site, distance int) Node {
	node := makeNode(ws, mods, site, distance)
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return node
	}

	node.Picture, node.Silhouette = findPicture(ws, mods, site)
	node.Trigger = findCondition(ws, mods, tree, statement)

	return node
}

// findCopy returns the latest copy the kept one hides, and false for none.
func findCopy(ws *workspace.Session, mods *index.Index, site index.Site) (index.Site, bool) {
	winner, _ := mods.FindKeptLocal(ws.Schema, mods.ToReference(site), site.File)
	if len(winner.Beaten) == 0 {
		return index.Site{}, false
	}

	return winner.Beaten[len(winner.Beaten)-1], true
}

func toID(mods *index.Index, site index.Site) string { return mods.ToReference(site).ToID() }

// makeFold returns the fold that stands for the links a parent's fan-out
// leaves out, one for each of the given databases. A fold of one database
// says what its links are.
func makeFold(id, parent string, distance int, databases []string) Node {
	node := Node{ID: id, Distance: distance, Parent: parent, Count: len(databases)}
	if distinct := slices.Compact(slices.Sorted(slices.Values(databases))); len(distinct) == 1 {
		node.What = lang.DescribeDefinition(distinct[0], len(databases) > 1)
	}

	return node
}

// toKey returns a key two edges share when they join the same nodes the same
// way.
func toKey(edge Edge) string {
	return strings.Join(slices.Concat([]string{edge.From, edge.To, edge.Delay}, edge.Via), "\x00")
}

// dropRepeats returns the elements whose key no earlier element has, in order.
func dropRepeats[Element any, Key comparable](elements []Element, key func(Element) Key) []Element {
	kept, seen := []Element{}, map[Key]bool{}
	for _, element := range elements {
		if elementKey := key(element); !seen[elementKey] {
			seen[elementKey] = true
			kept = append(kept, element)
		}
	}

	return kept
}

func appendNew[Element comparable](elements []Element, element Element) []Element {
	if slices.Contains(elements, element) {
		return elements
	}

	return append(elements, element)
}

// latest keeps what was worked out from the latest index, by key. The index
// is held weakly, so a closed workspace's is not kept alive.
type latest[Key comparable, Result any] struct {
	sync.Mutex
	mods    weak.Pointer[index.Index]
	results map[Key]Result
}

// load returns the result of a key for an index, built the first time.
func (l *latest[Key, Result]) load(mods *index.Index, key Key, build func() Result) Result {
	pointer := weak.Make(mods)
	l.Lock()
	result, isKept := l.results[key]
	isKept = isKept && l.mods == pointer
	l.Unlock()

	if isKept {
		return result
	}

	result = build()

	l.Lock()
	defer l.Unlock()

	if l.mods != pointer {
		l.mods, l.results = pointer, map[Key]Result{}
	}

	l.results[key] = result

	return result
}

// Defaults returns the filter of a new event graph: every origin and every
// database that fires, three steps deep, every fold closed. It is empty if
// no workspace is open.
func (s *Service) Defaults() Filter {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Filter{}
	}

	filter := Filter{
		Databases: listFiringDatabases(ws.Schema), Origins: []string{files.OriginVanilla},
		Depth: 3, Limit: 200, Fanout: 6, Follow: true, Labels: map[string]string{},
	}
	for _, mod := range *ws.Order.Load() {
		filter.Origins = append(filter.Origins, mod.ID)
	}

	for _, database := range filter.Databases {
		filter.Labels[database] = lang.DescribeDefinition(database, true)
	}

	return filter
}

// listTypedDatabases returns the game's databases of a type the test
// accepts, sorted by name.
func listTypedDatabases(ws *workspace.Session, accepts func(lang.Type) bool) []string {
	var databases []string
	for label, database := range ws.Schema.Databases {
		if accepts(database.Declaration.Type) {
			databases = append(databases, label)
		}
	}

	slices.Sort(databases)

	return databases
}
