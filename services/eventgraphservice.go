// eventgraphservice.go is the event graph page: the walk out of one definition over the index's
// edges, and one event's detail -- where it is, what its block holds at the top, its options, and
// the fires in and out of it. Every number the walk obeys comes from the page; this file has none.
package services

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/session"
)

// Params is what the page asks for. Every number is the page's.
type Params struct {
	Root     string   `json:"root"`     // the definition to draw around
	Depth    int      `json:"depth"`    // hops out of the root
	Fanout   int      `json:"fanout"`   // edges drawn out of one node before the rest are a stub
	MaxNodes int      `json:"maxNodes"` // nodes drawn before the graph is marked truncated
	Expand   []string `json:"expand"`   // stubs the page opened: more:<id>, more-in:<id>
}

// Node is one definition drawn, or a stub standing for what is hidden.
type Node struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Title     string `json:"title,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Rel       string `json:"rel,omitempty"`
	Line      int    `json:"line"`
	Root      bool   `json:"root,omitempty"`
	Stub      string `json:"stub,omitempty"` // "more": hidden fires out of Hidden's node; "fired-by": hidden callers
	Hidden    int    `json:"hidden,omitempty"`
}

// Edge is one fire drawn; Via names the tokens and macros between.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Via  string `json:"via"`
}

// EventGraph is the page's graph; Reason says why it is empty.
type EventGraph struct {
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	Truncated bool   `json:"truncated"`
	Reason    string `json:"reason,omitempty"`
}

// fire is one edge out of a definition, with the macros it went through.
type fire struct {
	to, via string
}

// EventGraphService draws the event graph page.
type EventGraphService struct {
	Session *SessionService
}

// GetEventGraph draws the definitions around a root.
func (v *EventGraphService) GetEventGraph(workspaceID string, params Params) (*EventGraph, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	drawn := graph(sess, params)
	return &drawn, nil
}

// GetEventDetail opens one definition by name: the one the game keeps.
func (v *EventGraphService) GetEventDetail(workspaceID, id string) (*Detail, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return eventDetail(sess, id), nil
}

func graph(sess *session.Session, params Params) EventGraph {
	sess.RLock()
	defer sess.RUnlock()
	if params.Root == "" {
		return EventGraph{Reason: "Pick a root event."}
	}
	found := sess.Defs(params.Root)
	if len(found) == 0 {
		return EventGraph{Reason: fmt.Sprintf("Nothing defines %s.", params.Root)}
	}
	builder := &grapher{sess: sess, params: params, out: &EventGraph{}, drawn: map[string]bool{}}
	builder.node(params.Root, true)

	// out of the root, hop by hop, each node's fan-out cut by the page's number
	frontier := []string{params.Root}
	for hop := 0; hop < params.Depth && len(frontier) > 0 && !builder.out.Truncated; hop++ {
		var next []string
		for _, from := range frontier {
			fires := builder.fires(from, map[string]bool{})
			opened := slices.Contains(params.Expand, "more:"+from)
			for i, fire := range fires {
				if !opened && params.Fanout > 0 && i >= params.Fanout {
					builder.out.Nodes = append(builder.out.Nodes, Node{ID: "more:" + from, Kind: "more", Stub: "more", Hidden: len(fires) - i, Title: fmt.Sprintf("+%d more", len(fires)-i)})
					builder.out.Edges = append(builder.out.Edges, Edge{From: from, To: "more:" + from})
					break
				}
				if !builder.drawn[fire.to] {
					if params.MaxNodes > 0 && len(builder.out.Nodes) >= params.MaxNodes {
						builder.out.Truncated = true
						break
					}
					builder.node(fire.to, false)
					next = append(next, fire.to)
				}
				builder.out.Edges = append(builder.out.Edges, Edge{From: from, To: fire.to, Via: fire.via})
			}
		}
		frontier = next
	}

	// into the root, one hop: a stub until the page opens it
	callers := builder.firedBy(params.Root)
	if len(callers) > 0 {
		if slices.Contains(params.Expand, "more-in:"+params.Root) {
			for _, caller := range callers {
				if !builder.drawn[caller.to] {
					builder.node(caller.to, false)
				}
				builder.out.Edges = append(builder.out.Edges, Edge{From: caller.to, To: params.Root, Via: caller.via})
			}
		} else {
			builder.out.Nodes = append(builder.out.Nodes, Node{ID: "more-in:" + params.Root, Kind: "more", Stub: "fired-by", Hidden: len(callers), Title: fmt.Sprintf("+%d fired by", len(callers))})
			builder.out.Edges = append(builder.out.Edges, Edge{From: "more-in:" + params.Root, To: params.Root})
			builder.out.Nodes[0].Hidden = len(callers)
		}
	}

	// every fire between two drawn definitions is drawn, whatever hop found them
	for id := range builder.drawn {
		for _, fired := range builder.fires(id, map[string]bool{}) {
			if builder.drawn[fired.to] && !slices.ContainsFunc(builder.out.Edges, func(edge Edge) bool { return edge.From == id && edge.To == fired.to }) {
				builder.out.Edges = append(builder.out.Edges, Edge{From: id, To: fired.to, Via: fired.via})
			}
		}
	}
	return *builder.out
}

// grapher holds one graph as it is drawn.
type grapher struct {
	sess   *session.Session
	params Params
	out    *EventGraph
	drawn  map[string]bool
}

// node draws a definition: the one the game keeps, with its title.
func (grapher *grapher) node(id string, root bool) {
	kept, _ := grapher.sess.Winner(grapher.sess.Defs(id))
	grapher.drawn[id] = true
	grapher.out.Nodes = append(grapher.out.Nodes, Node{ID: id, Kind: kept.Kind, Namespace: namespace(id), Title: title(grapher.sess, kept), Origin: kept.At.Origin, Rel: kept.At.Rel, Line: int(kept.Line), Root: root})
}

// fires is what a definition fires, in name order; a fire through a macro is followed to what the
// macro fires, and a macro already met on the way is not entered again.
func (grapher *grapher) fires(from string, seen map[string]bool) []fire {
	var out []fire
	for _, idx := range grapher.sess.All() {
		for _, fired := range idx.Fires(from) {
			if fired.File < 0 {
				continue
			}
			if kind := grapher.sess.Schema.Kinds[fired.ToKind]; kind != nil && kind.Macro != 0 {
				if !seen[fired.To] {
					seen[fired.To] = true
					for _, through := range grapher.fires(fired.To, seen) {
						out = append(out, fire{to: through.to, via: fired.To + " → " + through.via})
					}
				}
				continue
			}
			out = append(out, fire{to: fired.To, via: fired.Via})
		}
	}
	slices.SortStableFunc(out, func(first, second fire) int { return strings.Compare(first.to, second.to) })
	return slices.CompactFunc(out, func(first, second fire) bool { return first.to == second.to })
}

// firedBy is what fires a definition, in name order; a fire inside a macro counts for its callers.
func (grapher *grapher) firedBy(to string) []fire {
	var out []fire
	for _, idx := range grapher.sess.All() {
		for _, incoming := range idx.FiredBy(to) {
			if incoming.File < 0 {
				continue
			}
			if kind := grapher.sess.Schema.Kinds[incoming.FromKind]; kind != nil && kind.Macro != 0 && incoming.From != to {
				for _, caller := range grapher.firedBy(incoming.From) {
					out = append(out, fire{to: caller.to, via: caller.via + " → " + incoming.From})
				}
				continue
			}
			out = append(out, fire{to: incoming.From, via: incoming.Via})
		}
	}
	slices.SortStableFunc(out, func(first, second fire) int { return strings.Compare(first.to, second.to) })
	return slices.CompactFunc(out, func(first, second fire) bool { return first.to == second.to })
}

// namespace is the part of an event id before its last dot; "" for any other name.
func namespace(id string) string {
	if at := strings.LastIndexByte(id, '.'); at > 0 {
		return id[:at]
	}
	return ""
}

// title is a definition's display name: its label, else the text of its first templated loc key.
func title(sess *session.Session, found index.Found) string {
	if found.Label != "" {
		return found.Label
	}
	if kind := sess.Schema.Kinds[found.Kind]; kind != nil {
		for _, key := range loc.Expand(found.Name, kind.LocTemplates) {
			if said, ok := sess.Loc(key, ""); ok {
				return said.Value
			}
		}
	}
	return ""
}

// Detail is one definition, opened.
type Detail struct {
	index.Found
	Namespace string         `json:"namespace,omitempty"`
	Title     string         `json:"title,omitempty"`
	Sections  []Section      `json:"sections"`
	Options   []Option       `json:"options"`
	Fires     []index.Firing `json:"fires"`
	FiredBy   []index.Firing `json:"firedBy"`
	Sites     []index.Found  `json:"sites"` // every definition of the name, in load order
	Reason    string         `json:"reason,omitempty"`
}

// Section is one key at the top of the definition's block, with its value where that is one word.
type Section struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
	Line  int    `json:"line"`
}

// Option is one option block of an event: its name key and the text that key has.
type Option struct {
	Name string `json:"name"`
	Text string `json:"text,omitempty"`
	Line int    `json:"line"`
}

func eventDetail(sess *session.Session, id string) *Detail {
	sess.RLock()
	defer sess.RUnlock()
	found := sess.Defs(id)
	if len(found) == 0 {
		return &Detail{Reason: "Nothing defines " + id + "."}
	}
	kept, _ := sess.Winner(found)
	detail := &Detail{Found: kept, Namespace: namespace(id), Title: title(sess, kept), Sites: found}
	for _, idx := range sess.All() {
		detail.Fires, detail.FiredBy = append(detail.Fires, idx.Fires(id)...), append(detail.FiredBy, idx.FiredBy(id)...)
	}

	// the block, read from the file: the statement at the definition's line
	parsed, _, err := sess.Parsed(filepath.Clean(kept.At.Path))
	if err != nil {
		return detail
	}
	lines := parsed.Lines()
	for _, st := range parsed.Root.Statements {
		assignment, ok := st.(*jomini.Assignment)
		if !ok || lines.PositionAt(assignment.Key.Range.Start).Line != int(kept.Line) {
			continue
		}
		block := jomini.BlockOf(assignment.Value)
		if block == nil {
			break
		}
		for _, inner := range block.Statements {
			assignment, ok := inner.(*jomini.Assignment)
			if !ok {
				continue
			}
			section := Section{Key: assignment.Key.Text, Line: lines.PositionAt(assignment.Key.Range.Start).Line}
			if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
				section.Value = scalar.Text
			}
			detail.Sections = append(detail.Sections, section)
			if assignment.Key.Text == "option" {
				if block := jomini.BlockOf(assignment.Value); block != nil {
					detail.Options = append(detail.Options, option(sess, block, section.Line))
				}
			}
		}
		break
	}
	return detail
}

// option reads an option block's name and the text it has.
func option(sess *session.Session, block *jomini.Block, line int) Option {
	other := Option{Line: line}
	for _, st := range block.Statements {
		if assignment, ok := st.(*jomini.Assignment); ok && assignment.Key.Text == "name" {
			if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
				other.Name = scalar.Text
				if said, ok := sess.Loc(scalar.Text, ""); ok {
					other.Text = said.Value
				}
			}
		}
	}
	return other
}
