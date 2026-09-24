// eventgraphservice.go is the event graph page: the walk out of one definition over the index's
// edges, and one event's detail -- where it is, what its block holds at the top, its options, and
// the fires in and out of it. Every number the walk obeys comes from the page; this file has none.
package services

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/session"
)

// Params is what the page asks for. Every number is the page's.
type Params struct {
	Root     string   `json:"root"`     // the definition to draw around
	Depth    int      `json:"depth"`    // hops out of the root
	Fanout   int      `json:"fanout"`   // edges drawn out of one node before the rest are a stub
	MaxNodes int      `json:"maxNodes"` // nodes drawn before the graph is marked truncated
	Expand   []string `json:"expand"`   // stubs the page opened: more:<id>, more-in:<id>
	Origins  []string `json:"origins"`  // the origins drawn, with the root and its stubs; empty for all
	Macros   bool     `json:"macros"`   // each macro on a chain drawn as a node between the two it joins
}

// Node is one definition drawn, or a stub standing for what is hidden.
type Node struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title,omitempty"`
	Origin string `json:"origin,omitempty"`
	Rel    string `json:"rel,omitempty"`
	Line   int    `json:"line"`
	Root   bool   `json:"root,omitempty"`
	Stub   string `json:"stub,omitempty"` // "more": hidden fires out of Hidden's node; "fired-by": hidden callers
	Hidden int    `json:"hidden,omitempty"`
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
	Reached   int    `json:"reached,omitempty"` // truncated: the hop the node cap cut
	Reason    string `json:"reason,omitempty"`
}

// EventGraphService draws the event graph page.
type EventGraphService struct {
	Session *SessionService
}

// Roots is a root search's hits, grouped by kind, and how many of each kind the query holds.
type Roots struct {
	Hits  []Node      `json:"hits"`
	Kinds []KindCount `json:"kinds"`
}

// KindCount is one kind and how many roots of it a search holds.
type KindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// SearchRoots is what the graph can be drawn from: the events and on_actions the install and the mods
// fire, and every definition but a macro that fires one, whose id holds the query. Ids that start with
// it come first; of one kind at most 50, of every kind at most 8 each.
func (v *EventGraphService) SearchRoots(workspaceID, query, kind string) (*Roots, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	query = strings.ToLower(query)
	kinds, checked := map[string]string{}, map[string]bool{}
	for _, idx := range sess.All() {
		for _, edge := range idx.Edges {
			if edge.File < 0 {
				continue
			}
			if sess.Fired(edge.ToKind) {
				kinds[edge.To] = edge.ToKind
			}
			if caller := sess.Schema.Kinds[edge.FromKind]; !checked[edge.From] && (caller == nil || caller.Macro == 0) {
				checked[edge.From] = true
				if len(sess.Fires(edge.From, map[string]bool{})) > 0 {
					kinds[edge.From] = edge.FromKind
				}
			}
		}
	}

	// the query's hits by kind, those that start with it first
	byKind := map[string][]string{}
	for name, named := range kinds {
		if strings.Contains(strings.ToLower(name), query) {
			byKind[named] = append(byKind[named], name)
		}
	}
	out, limit := &Roots{}, 8
	if kind != "" {
		limit = 50
	}
	builder := &grapher{sess: sess, out: &EventGraph{}, drawn: map[string]bool{}, parsed: map[string]*jomini.Result{}}
	for named, names := range byKind {
		out.Kinds = append(out.Kinds, KindCount{Kind: named, Count: len(names)})
		if kind != "" && kind != named {
			continue
		}
		slices.SortFunc(names, func(first, second string) int {
			starts := func(name string) int { return strings.Index(strings.ToLower(name), query) }
			return cmp.Or(cmp.Compare(min(starts(first), 1), min(starts(second), 1)), strings.Compare(first, second))
		})
		for _, id := range names[:min(len(names), limit)] {
			builder.node(id, false)
		}
	}
	slices.SortFunc(out.Kinds, func(first, second KindCount) int {
		return cmp.Or(second.Count-first.Count, strings.Compare(first.Kind, second.Kind))
	})
	slices.SortStableFunc(builder.out.Nodes, func(first, second Node) int {
		return slices.IndexFunc(out.Kinds, func(count KindCount) bool { return count.Kind == first.Kind }) -
			slices.IndexFunc(out.Kinds, func(count KindCount) bool { return count.Kind == second.Kind })
	})
	out.Hits = builder.out.Nodes
	return out, nil
}

// GetEventGraph draws the definitions around a root.
func (v *EventGraphService) GetEventGraph(workspaceID string, params Params) (*EventGraph, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	if params.Root == "" {
		return &EventGraph{Reason: "Pick a root event."}, nil
	}
	found := sess.Defs(params.Root)
	if len(found) == 0 {
		return &EventGraph{Reason: fmt.Sprintf("Nothing defines %s.", params.Root)}, nil
	}
	builder := &grapher{sess: sess, params: params, out: &EventGraph{}, drawn: map[string]bool{}, parsed: map[string]*jomini.Result{}}
	builder.node(params.Root, true)

	// out of the root, hop by hop, each node's fan-out cut by the page's number
	frontier := []string{params.Root}
	for hop := 0; hop < params.Depth && len(frontier) > 0 && !builder.out.Truncated; hop++ {
		var next []string
		for _, from := range frontier {
			fires := sess.Fires(from, map[string]bool{})
			opened := slices.Contains(params.Expand, "more:"+from)
			for i, fire := range fires {
				if !opened && params.Fanout > 0 && i >= params.Fanout {
					builder.out.Nodes = append(builder.out.Nodes, Node{ID: "more:" + from, Kind: "more", Stub: "more", Hidden: len(fires) - i, Title: fmt.Sprintf("+%d more", len(fires)-i)})
					builder.out.Edges = append(builder.out.Edges, Edge{From: from, To: "more:" + from})
					break
				}
				if !builder.drawn[fire.Name] {
					if params.MaxNodes > 0 && len(builder.out.Nodes) >= params.MaxNodes {
						builder.out.Truncated, builder.out.Reached = true, hop+1
						break
					}
					builder.node(fire.Name, false)
					next = append(next, fire.Name)
				}
				builder.out.Edges = append(builder.out.Edges, Edge{From: from, To: fire.Name, Via: fire.Via})
			}
		}
		frontier = next
	}

	// into a root the game fires, one hop: a stub until the page opens it
	var callers []session.Fire
	if kept, _ := sess.Winner(found); sess.Fired(kept.Kind) {
		callers = firedBy(sess, params.Root)
	}
	if len(callers) > 0 {
		if slices.Contains(params.Expand, "more-in:"+params.Root) {
			for _, caller := range callers {
				if !builder.drawn[caller.Name] {
					builder.node(caller.Name, false)
				}
				builder.out.Edges = append(builder.out.Edges, Edge{From: caller.Name, To: params.Root, Via: caller.Via})
			}
		} else {
			builder.out.Nodes = append(builder.out.Nodes, Node{ID: "more-in:" + params.Root, Kind: "more", Stub: "fired-by", Hidden: len(callers), Title: fmt.Sprintf("+%d fired by", len(callers))})
			builder.out.Edges = append(builder.out.Edges, Edge{From: "more-in:" + params.Root, To: params.Root})
			builder.out.Nodes[0].Hidden = len(callers)
		}
	}

	// every fire between two drawn definitions is drawn, whatever hop found them
	for id := range builder.drawn {
		for _, fired := range sess.Fires(id, map[string]bool{}) {
			if builder.drawn[fired.Name] && !slices.ContainsFunc(builder.out.Edges, func(edge Edge) bool { return edge.From == id && edge.To == fired.Name }) {
				builder.out.Edges = append(builder.out.Edges, Edge{From: id, To: fired.Name, Via: fired.Via})
			}
		}
	}

	// only the page's origins: the root, their nodes, a stub whose node stays, the edges among them
	if len(params.Origins) > 0 {
		kept := map[string]bool{}
		builder.out.Nodes = slices.DeleteFunc(builder.out.Nodes, func(node Node) bool {
			_, stubbed, _ := strings.Cut(node.ID, ":")
			kept[node.ID] = node.Root || slices.Contains(params.Origins, node.Origin) || (node.Stub != "" && kept[stubbed])
			return !kept[node.ID]
		})
		builder.out.Edges = slices.DeleteFunc(builder.out.Edges, func(edge Edge) bool { return !kept[edge.From] || !kept[edge.To] })
	}

	// the page's macros shown: each chain's macros drawn in turn between the two it joins, the
	// last hop labelled by the token that fires; a hop two chains share is drawn once
	if params.Macros {
		var edges []Edge
		for _, edge := range builder.out.Edges {
			hops := strings.Split(edge.Via, " → ")
			from := edge.From
			for _, macro := range hops[:len(hops)-1] {
				if !builder.drawn[macro] {
					builder.node(macro, false)
				}
				edges = append(edges, Edge{From: from, To: macro})
				from = macro
			}
			edges = append(edges, Edge{From: from, To: edge.To, Via: hops[len(hops)-1]})
		}
		slices.SortStableFunc(edges, func(first, second Edge) int { return strings.Compare(first.From+first.To, second.From+second.To) })
		builder.out.Edges = slices.CompactFunc(edges, func(first, second Edge) bool { return first.From == second.From && first.To == second.To })
	}
	return builder.out, nil
}

// grapher holds one graph as it is drawn, and each file parsed for a title, once.
type grapher struct {
	sess   *session.Session
	params Params
	out    *EventGraph
	drawn  map[string]bool
	parsed map[string]*jomini.Result
}

// node draws a definition: the one the game keeps, with its title.
func (grapher *grapher) node(id string, root bool) {
	kept, _ := grapher.sess.Winner(grapher.sess.Defs(id))
	parsed, known := grapher.parsed[kept.At.Path]
	if !known {
		parsed, _, _ = grapher.sess.Parsed(kept.At.Path)
		grapher.parsed[kept.At.Path] = parsed
	}
	grapher.drawn[id] = true
	grapher.out.Nodes = append(grapher.out.Nodes, Node{ID: id, Kind: kept.Kind, Title: title(grapher.sess, kept, parsed), Origin: kept.At.Origin, Rel: kept.At.Rel, Line: int(kept.Line), Root: root})
}

// firedBy is what fires a definition, in name order; a fire inside a macro counts for its callers,
// its chain the macros from the caller's own call to the token at this site, as Fires writes one.
func firedBy(sess *session.Session, to string) []session.Fire {
	var out []session.Fire
	for _, idx := range sess.All() {
		for _, incoming := range idx.FiredBy(to) {
			if incoming.File < 0 {
				continue
			}
			if kind := sess.Schema.Kinds[incoming.FromKind]; kind != nil && kind.Macro != 0 && incoming.From != to {
				for _, caller := range firedBy(sess, incoming.From) {
					out = append(out, session.Fire{Name: caller.Name, Via: caller.Via + " → " + incoming.Via})
				}
				continue
			}
			out = append(out, session.Fire{Name: incoming.From, Via: incoming.Via})
		}
	}
	slices.SortStableFunc(out, func(first, second session.Fire) int { return strings.Compare(first.Name, second.Name) })
	return slices.CompactFunc(out, func(first, second session.Fire) bool { return first.Name == second.Name })
}

// title is a definition's display name: its label, else the text of its first templated loc key,
// else, in its parsed file, the text of the key its title member names, as events write theirs.
func title(sess *session.Session, found index.Found, parsed *jomini.Result) string {
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
	if parsed == nil {
		return ""
	}
	if defined := keyedAt(parsed, int(found.Line), int(found.Col)); defined != nil && jomini.BlockOf(defined.Value) != nil {
		for _, st := range jomini.BlockOf(defined.Value).Statements {
			if assignment, ok := st.(*jomini.Assignment); ok && assignment.Key.Text == "title" {
				if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
					said, _ := sess.Loc(scalar.Text, "")
					return said.Value
				}
			}
		}
	}
	return ""
}

// Detail is one definition, opened.
type Detail struct {
	index.Found
	Title    string         `json:"title,omitempty"`
	Lines    []string       `json:"lines"` // the definition as its file writes it
	Sections []Section      `json:"sections"`
	Options  []Option       `json:"options"`
	Fires    []session.Fire `json:"fires"`   // as the graph draws them: events and on_actions, through macros
	FiredBy  []session.Fire `json:"firedBy"` // the same, into it
	Refs     []RefGroup     `json:"refs"`    // what the block names, by kind in the order first written
	Reason   string         `json:"reason,omitempty"`
}

// Section is one key at the top of the definition's block: its value where that is one word, and
// the text that word has as a loc key.
type Section struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
	Text  string `json:"text,omitempty"`
	Line  int    `json:"line"`
}

// Option is one option block of an event: its name key and the text that key has.
type Option struct {
	Name string `json:"name"`
	Text string `json:"text,omitempty"`
	Line int    `json:"line"`
}

// RefGroup is the references of one kind written in a block; loc keys are kind loc, files file.
type RefGroup struct {
	Kind string        `json:"kind"`
	Refs []index.Cited `json:"refs"`
}

// GetEventDetail opens one definition by name: the one the game keeps.
func (v *EventGraphService) GetEventDetail(workspaceID, id string) (*Detail, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	found := sess.Defs(id)
	if len(found) == 0 {
		return &Detail{Reason: "Nothing defines " + id + "."}, nil
	}
	kept, _ := sess.Winner(found)
	detail := &Detail{Found: kept, Fires: sess.Fires(id, map[string]bool{}), FiredBy: firedBy(sess, id)}

	// the block, read from the file: the statement whose key the definition was found at
	parsed, _, err := sess.Parsed(filepath.Clean(kept.At.Path))
	detail.Title = title(sess, kept, parsed)
	if err != nil {
		return detail, nil
	}
	defined := keyedAt(parsed, int(kept.Line), int(kept.Col))
	if defined == nil || jomini.BlockOf(defined.Value) == nil {
		return detail, nil
	}
	detail.Lines = spanLines(parsed, defined.Range, 0)
	lines := parsed.Lines()
	for _, inner := range jomini.BlockOf(defined.Value).Statements {
		assignment, ok := inner.(*jomini.Assignment)
		if !ok {
			continue
		}
		section := Section{Key: assignment.Key.Text, Line: lines.PositionAt(assignment.Key.Range.Start).Line}
		if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
			section.Value = scalar.Text
			if said, ok := sess.Loc(scalar.Text, ""); ok {
				section.Text = said.Value
			}
		}
		if block := jomini.BlockOf(assignment.Value); block != nil && assignment.Key.Text == "option" {
			detail.Options = append(detail.Options, option(sess, block, section.Line))
		}
		detail.Sections = append(detail.Sections, section)
	}

	// the references written inside the block, grouped by kind in the order first written
	for _, ref := range index.Visit(kept.At, parsed, sess.Reader(), nil).Refs {
		if offset := lines.OffsetAt(int(ref.Line), int(ref.Col)); offset < defined.Range.Start || offset >= defined.Range.End {
			continue
		}
		kind := ref.Kind
		if ref.Type != schema.TypeRef {
			kind = ref.Type.String()
		}
		at := slices.IndexFunc(detail.Refs, func(group RefGroup) bool { return group.Kind == kind })
		if at < 0 {
			at = len(detail.Refs)
			detail.Refs = append(detail.Refs, RefGroup{Kind: kind})
		}
		detail.Refs[at].Refs = append(detail.Refs[at].Refs, index.Cited{Ref: ref, At: kept.At})
	}
	return detail, nil
}

// keyedAt is the assignment whose key starts at a line and byte column: where a definition sits.
func keyedAt(parsed *jomini.Result, line, col int) *jomini.Assignment {
	offset := parsed.Lines().OffsetAt(line, col)
	var keyed *jomini.Assignment
	jomini.Walk(parsed.Root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		if assignment, ok := st.(*jomini.Assignment); ok && assignment.Key.Range.Start == offset {
			keyed = assignment
		}
		return keyed == nil && st.NodeRange().Start <= offset && offset < st.NodeRange().End
	})
	return keyed
}

// spanLines is the source lines a span is written on; past limit lines, when limit is not 0, the
// rest is one "…".
func spanLines(parsed *jomini.Result, span jomini.Range, limit int) []string {
	lines := parsed.Lines()
	var out []string
	for line := lines.PositionAt(span.Start).Line; line <= lines.PositionAt(max(span.End-1, span.Start)).Line; line++ {
		if limit > 0 && len(out) == limit {
			return append(out, "…")
		}
		out = append(out, strings.TrimRight(parsed.Src[lines.OffsetAt(line, 0):lines.OffsetAt(line+1, 0)], "\r\n"))
	}
	return out
}

// option reads an option block's name and the text it has, at its line. A name written as a
// block, one per condition, is its text member's key; the first name is kept.
func option(sess *session.Session, block *jomini.Block, line int) Option {
	other := Option{Line: line}
	for _, st := range block.Statements {
		assignment, ok := st.(*jomini.Assignment)
		if !ok || assignment.Key.Text != "name" || other.Name != "" {
			continue
		}
		value := assignment.Value
		if named := jomini.BlockOf(value); named != nil {
			for _, inner := range named.Statements {
				if text, ok := inner.(*jomini.Assignment); ok && text.Key.Text == "text" {
					value = text.Value
				}
			}
		}
		if scalar, ok := value.(*jomini.Scalar); ok {
			other.Name = scalar.Text
			if said, ok := sess.Loc(scalar.Text, ""); ok {
				other.Text = said.Value
			}
		}
	}
	return other
}
