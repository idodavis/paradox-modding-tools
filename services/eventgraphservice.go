// eventgraphservice.go is the event graph page: the walk out of one definition over what it fires,
// and one event's detail -- where it is, what its block holds at the top, its options, and the
// fires in and out of it. Every number the walk obeys comes from the page.

package services

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
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
	Scripted bool     `json:"scripted"` // each scripted effect on a chain drawn as a node between the two it joins
	Follow   bool     `json:"follow"`   // fires reached through a scripted effect are drawn; else only direct ones
}

// Node is one definition drawn, or a stub standing for what is hidden.
type Node struct {
	ID       string `json:"id"`
	Database string `json:"database"`
	Kind     string `json:"kind"` // what one of its database is called (game.KindName)
	Title    string `json:"title,omitempty"`
	Origin   string `json:"origin,omitempty"`
	Root     bool   `json:"root,omitempty"`
	Stub     string `json:"stub,omitempty"` // "more": fires hidden out of the node in its ID; "fired-by": callers hidden
}

// Edge is one fire drawn; Via names the scripted effects between, in the order they are entered.
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

// Roots is a root search's hits, grouped by database, and how many of each database the query
// holds.
type Roots struct {
	Hits      []Node          `json:"hits"`
	Databases []DatabaseCount `json:"databases"`
}

// DatabaseCount is one database and how many roots of it a search holds.
type DatabaseCount struct {
	Database string `json:"database"`
	Count    int    `json:"count"`
}

// SearchRoots is the fired events and on_actions, and what fires one but a scripted entry, whose id
// holds the query: ids that start with it first, 50 of one database or 8 of each.
func (s *EventGraphService) SearchRoots(workspaceID, query string) (*Roots, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	query = strings.ToLower(query)
	sch, indexes := sess.Install.Schema, sess.Indexes()

	// in the files the game loads: what is fired, and what fires it, a scripted entry by what fires
	// through it (Session.FiredBy)
	fired, firing := map[string]string{}, map[index.Def][]session.Fire{}
	for _, idx := range indexes {
		for _, file := range idx.Files {
			records := idx.Records[file.Path]
			if _, loaded := index.Loaded(indexes, file.Path); records == nil || !loaded {
				continue
			}
			for _, ref := range records.Refs {
				if !ref.Fires(sch) {
					continue
				}
				fired[ref.Name] = ref.Database
				if ref.From < 0 {
					continue
				}
				caller := records.Defs[ref.From]
				if _, entered := firing[caller]; !entered && caller.Scripted(sch) {
					firing[caller] = sess.FiredBy(caller)
				} else if !entered {
					firing[caller] = []session.Fire{{Name: caller.Name, Database: caller.Database}}
				}
			}
		}
	}

	// the roots the query holds: what is fired and defined, and what fires one; a name of several
	// databases takes the first in sorted order, so no map order decides
	named := map[string]string{}
	for name, kind := range fired {
		if strings.Contains(strings.ToLower(name), query) && len(index.Lookup(indexes, kind, name)) > 0 {
			named[name] = kind
		}
	}
	for _, roots := range firing {
		for _, root := range roots {
			if current := named[root.Name]; strings.Contains(strings.ToLower(root.Name), query) && (current == "" || root.Database < current) {
				named[root.Name] = root.Database
			}
		}
	}

	// the query's hits by database, those that start with it first
	byKind := map[string][]string{}
	for name, kind := range named {
		byKind[kind] = append(byKind[kind], name)
	}
	out, limit := &Roots{}, 8
	builder := newGrapher(sess)
	for kind, names := range byKind {
		out.Databases = append(out.Databases, DatabaseCount{Database: kind, Count: len(names)})
		slices.SortFunc(names, func(first, second string) int {
			starts := func(name string) int { return strings.Index(strings.ToLower(name), query) }
			return cmp.Or(cmp.Compare(min(starts(first), 1), min(starts(second), 1)), strings.Compare(first, second))
		})
		for _, id := range names[:min(len(names), limit)] {
			builder.node(id, false)
		}
	}
	slices.SortFunc(out.Databases, func(first, second DatabaseCount) int {
		return cmp.Or(second.Count-first.Count, strings.Compare(first.Database, second.Database))
	})
	slices.SortStableFunc(builder.out.Nodes, func(first, second Node) int {
		return slices.IndexFunc(out.Databases, func(count DatabaseCount) bool { return count.Database == first.Database }) -
			slices.IndexFunc(out.Databases, func(count DatabaseCount) bool { return count.Database == second.Database })
	})
	out.Hits = builder.out.Nodes
	return out, nil
}

// GetEventGraph draws the definitions around a root.
func (s *EventGraphService) GetEventGraph(workspaceID string, params Params) (*EventGraph, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	if params.Root == "" {
		return &EventGraph{Reason: "Pick a root event."}, nil
	}
	builder := newGrapher(sess)
	root := builder.def(params.Root)
	if root.Name == "" {
		return &EventGraph{Reason: fmt.Sprintf("Nothing defines %s.", params.Root)}, nil
	}
	builder.node(params.Root, true)

	// the fires drawn: through a scripted effect only when the page follows them
	followed := func(fires []session.Fire) []session.Fire {
		if params.Follow {
			return fires
		}
		return slices.DeleteFunc(fires, func(fire session.Fire) bool { return fire.Via != "" })
	}

	// out of the root, hop by hop, each node's fan-out cut by the page's number
	frontier := []string{params.Root}
	for hop := 0; hop < params.Depth && len(frontier) > 0 && !builder.out.Truncated; hop++ {
		var next []string
		for _, from := range frontier {
			fires := followed(sess.Fires(builder.def(from)))
			opened := slices.Contains(params.Expand, "more:"+from)
			for i, fire := range fires {
				if !opened && params.Fanout > 0 && i >= params.Fanout {
					builder.out.Nodes = append(builder.out.Nodes, Node{ID: "more:" + from, Database: "more", Stub: "more", Title: stubTitle(fires[i:])})
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
	if (index.Ref{Database: root.Database}).Fires(sess.Install.Schema) {
		callers = followed(sess.FiredBy(root))
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
			builder.out.Nodes = append(builder.out.Nodes, Node{ID: "more-in:" + params.Root, Database: "more", Stub: "fired-by", Title: fmt.Sprintf("+%d fired by", len(callers))})
			builder.out.Edges = append(builder.out.Edges, Edge{From: "more-in:" + params.Root, To: params.Root})
		}
	}

	// every fire between two drawn definitions is drawn, whatever hop found them
	for _, id := range slices.Sorted(maps.Keys(builder.drawn)) {
		for _, fired := range followed(sess.Fires(builder.def(id))) {
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

	// the page's scripted effects shown: each chain's scripted effects drawn in turn between the
	// two it joins; a hop two chains share is drawn once
	if params.Scripted {
		var edges []Edge
		for _, edge := range builder.out.Edges {
			from := edge.From
			if edge.Via != "" {
				for _, scripted := range strings.Split(edge.Via, " → ") {
					if !builder.drawn[scripted] {
						builder.node(scripted, false)
					}
					edges = append(edges, Edge{From: from, To: scripted})
					from = scripted
				}
			}
			edges = append(edges, Edge{From: from, To: edge.To})
		}
		slices.SortStableFunc(edges, func(first, second Edge) int { return strings.Compare(first.From+first.To, second.From+second.To) })
		builder.out.Edges = slices.CompactFunc(edges, func(first, second Edge) bool { return first.From == second.From && first.To == second.To })
	}
	return builder.out, nil
}

// stubTitle is a fan-out stub's title: "+7 more events" where every hidden fire is of one database,
// else "+7 more".
func stubTitle(hidden []session.Fire) string {
	title := fmt.Sprintf("+%d more", len(hidden))
	if slices.ContainsFunc(hidden, func(fire session.Fire) bool { return fire.Database != hidden[0].Database }) {
		return title
	}
	title += " " + game.KindName(hidden[0].Database)
	if len(hidden) > 1 {
		title += "s"
	}
	return title
}

// grapher holds one graph as it is drawn, and each definition found, once.
type grapher struct {
	sess  *session.Session
	out   *EventGraph
	drawn map[string]bool
	found map[string]index.Def
}

// newGrapher is an empty graph over a session.
func newGrapher(sess *session.Session) *grapher {
	return &grapher{sess: sess, out: &EventGraph{}, drawn: map[string]bool{}, found: map[string]index.Def{}}
}

// def is the definition the game keeps of an id, in whichever database defines it: one a fire names
// (an event, an on_action) before the others; the zero Def where nothing does.
func (grapher *grapher) def(id string) index.Def {
	found, known := grapher.found[id]
	if known {
		return found
	}
	sch := grapher.sess.Install.Schema
	for _, database := range slices.Sorted(maps.Keys(sch.Databases)) {
		if defs := index.Lookup(grapher.sess.Indexes(), database, id); len(defs) > 0 && (found.Name == "" || (index.Ref{Database: database}).Fires(sch)) {
			found, _, _, _ = grapher.sess.Winner(defs)
		}
	}
	grapher.found[id] = found
	return found
}

// node draws a definition: the one the game keeps, with its title.
func (grapher *grapher) node(id string, root bool) {
	def := grapher.def(id)
	file, _, _ := grapher.sess.Located(def.Path)
	grapher.drawn[id] = true
	grapher.out.Nodes = append(grapher.out.Nodes, Node{ID: id, Database: def.Database, Kind: game.KindName(def.Database), Title: grapher.sess.Title(def), Origin: file.Origin, Root: root})
}

// Detail is one definition, opened.
type Detail struct {
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	Line     int            `json:"line"`
	At       source.File    `json:"at"`
	Lines    []string       `json:"lines"` // the definition as its file writes it
	Sections []Section      `json:"sections"`
	Options  []Option       `json:"options"`
	Fires    []session.Fire `json:"fires"`   // as the graph draws them: events and on_actions, through scripted effects
	FiredBy  []session.Fire `json:"firedBy"` // the same, into it
	Refs     []RefGroup     `json:"refs"`    // what the block names, by database in the order first written
	Reason   string         `json:"reason,omitempty"`
}

// Section is one key at the top of the definition's block: its value as written (a block as its
// statements on one line, each inner block as { … }), and the text a one-word value has as a loc key.
type Section struct {
	Key      string     `json:"key"`
	Value    string     `json:"value,omitempty"`
	Line     int        `json:"line"`
	When     string     `json:"when,omitempty"`     // a block's own trigger, on one line: it applies only then
	Parts    []TextPart `json:"parts,omitempty"`    // a text's parts in order: a loc key, or a dynamic description's
	Portrait string     `json:"portrait,omitempty"` // a portrait's character
}

// TextPart is one part of a text, read in order: one of its versions shows, the first whose trigger
// holds, or one of them at random.
type TextPart struct {
	Random   bool      `json:"random,omitempty"`
	Versions []Version `json:"versions"`
}

// Version is one text a part can show: its trigger on one line (none: it always can), its loc key and
// that key's text, at its line.
type Version struct {
	When string `json:"when,omitempty"`
	Key  string `json:"key"`
	Text string `json:"text,omitempty"`
	Line int    `json:"line"`
}

// Option is one option block of an event: its trigger on one line, every name it is written with (the
// first whose trigger holds shows) and its flags.
type Option struct {
	Line  int       `json:"line"`
	When  string    `json:"when,omitempty"`
	Names []Version `json:"names,omitempty"`
	Flags []string  `json:"flags,omitempty"`
}

// RefGroup is the references of one database written in a block; loc keys are database
// localization, files file.
type RefGroup struct {
	Database string  `json:"database"`
	Kind     string  `json:"kind"` // what one of its database is called (game.KindName)
	Refs     []Cited `json:"refs"`
}

// Cited is one name a block writes, and where.
type Cited struct {
	Name string      `json:"name"`
	Line int         `json:"line"`
	Col  int         `json:"col"`
	At   source.File `json:"at"`
}

// GetEventDetail opens one definition by name: the one the game keeps.
func (s *EventGraphService) GetEventDetail(workspaceID, id string) (*Detail, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	builder := newGrapher(sess)
	def := builder.def(id)
	if def.Name == "" {
		return &Detail{Reason: "Nothing defines " + id + "."}, nil
	}
	file, _, _ := sess.Located(def.Path)
	detail := &Detail{
		Kind: game.KindName(def.Database), Name: def.Name, Line: int(def.Line), At: file,
		Fires: sess.Fires(def), FiredBy: sess.FiredBy(def),
	}

	// the block, read from the file: the statement whose key the definition was found at
	parsed, defined := sess.Entry(def)
	block := jomini.ChildBlock(defined)
	if block == nil {
		return detail, nil
	}
	detail.Lines = spanLines(parsed, defined.NodeRange(), 0)
	read := eventText{sess, parsed}
	for _, inner := range block.Statements {
		assignment, ok := inner.(*jomini.Assignment)
		if !ok {
			continue
		}
		section := Section{Key: assignment.Key.Text, Line: parsed.Lines().PositionAt(assignment.Key.Range.Start).Line}
		portrait := slices.ContainsFunc(game.EventPortraits.Suffixes, func(suffix string) bool { return strings.HasSuffix(section.Key, suffix) })
		if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
			section.Value = scalar.Text
			if _, ok := sess.Loc(scalar.Text, ""); ok {
				section.Parts = []TextPart{{Versions: []Version{read.version(scalar, "")}}}
			}
			if portrait {
				section.Portrait = scalar.Text
			}
		} else if written := jomini.BlockOf(assignment.Value); written != nil {
			when, trigger := read.trigger(written)
			section.When = when
			section.Value = oneLine(parsed.Src, slices.DeleteFunc(slices.Clone(written.Statements), func(statement jomini.Statement) bool { return statement == trigger }))
			section.Parts = read.parts(written)
			if character := read.scalar(written, game.EventPortraits.Character); portrait && character != nil {
				section.Portrait = character.Text
			}
			if section.Key == "option" {
				detail.Options = append(detail.Options, read.option(written, section.Line))
			}
		}
		detail.Sections = append(detail.Sections, section)
	}

	// the references the walk read inside the entry, grouped by database in the order first written;
	// a call by the database that defines what it calls, and one nothing defines is left out
	indexes := sess.Indexes()
	for _, ref := range index.Within(indexes, def) {
		database := ref.Database
		if database == index.Parameter {
			continue
		}
		if ref.Called != "" {
			called, known := index.Resolve(sess.Install.Schema, indexes, ref)
			if !known {
				continue
			}
			database = called[0].Database
		}
		at := slices.IndexFunc(detail.Refs, func(group RefGroup) bool { return group.Database == database })
		if at < 0 {
			at = len(detail.Refs)
			detail.Refs = append(detail.Refs, RefGroup{Database: database, Kind: game.KindName(database)})
		}
		detail.Refs[at].Refs = append(detail.Refs[at].Refs, Cited{Name: ref.Name, Line: int(ref.Line), Col: int(ref.Col), At: file})
	}
	return detail, nil
}

// oneLine is a block's statements on one line: each as written, an inner block as { … }. Comments are
// outside the tree, so none is kept.
func oneLine(src string, statements []jomini.Statement) string {
	parts := []string{"{"}
	for _, statement := range statements {
		span := statement.NodeRange()
		if inner := jomini.ChildBlock(statement); inner != nil {
			parts = append(parts, strings.Join(strings.Fields(src[span.Start:inner.OpenBrace]), " ")+" { … }")
		} else {
			parts = append(parts, strings.Join(strings.Fields(src[span.Start:span.End]), " "))
		}
	}
	return strings.Join(append(parts, "}"), " ")
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

// eventText reads an event's texts, triggers and options from its file's parse, as written.
type eventText struct {
	sess   *session.Session
	parsed *jomini.Result
}

// flat is the source between two offsets on one line, its comments left out.
func (read eventText) flat(start, end int) string {
	text := []byte(read.parsed.Src[start:end])
	for _, comment := range read.parsed.Comments {
		for at := max(comment.Range.Start, start); at < min(comment.Range.End, end); at++ {
			text[at-start] = ' '
		}
	}
	return strings.Join(strings.Fields(string(text)), " ")
}

// trigger is a block's own trigger, its inside on one line, and the statement it is written in.
func (read eventText) trigger(block *jomini.Block) (string, jomini.Statement) {
	for _, statement := range block.Statements {
		if assignment, ok := statement.(*jomini.Assignment); ok && assignment.Key.Text == game.DynamicDescription.Trigger {
			if inner := jomini.BlockOf(assignment.Value); inner != nil {
				return read.flat(inner.OpenBrace+1, inner.CloseBrace), statement
			}
		}
	}
	return "", nil
}

// scalar is the one-word value a block writes for a key; nil where it writes none.
func (read eventText) scalar(block *jomini.Block, key string) *jomini.Scalar {
	for _, statement := range block.Statements {
		if assignment, ok := statement.(*jomini.Assignment); ok && assignment.Key.Text == key {
			if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
				return scalar
			}
		}
	}
	return nil
}

// version is a loc key as one version of a text, shown when its trigger holds.
func (read eventText) version(scalar *jomini.Scalar, when string) Version {
	version := Version{When: when, Key: scalar.Text, Line: read.parsed.Lines().PositionAt(scalar.Range.Start).Line}
	if said, ok := read.sess.Loc(scalar.Text, ""); ok {
		version.Text = said.Text
	}
	return version
}

// parts reads a dynamic description as the game does: each statement a part, in order. A leaf shows its
// key; a triggered leaf shows it only while its trigger holds; another marker shows one of its own.
func (read eventText) parts(block *jomini.Block) []TextPart {
	shape := game.DynamicDescription
	var out []TextPart
	for _, statement := range block.Statements {
		assignment, ok := statement.(*jomini.Assignment)
		switch {
		case !ok:
		case assignment.Key.Text == shape.Leaf && jomini.BlockOf(assignment.Value) != nil:
			out = append(out, read.parts(jomini.BlockOf(assignment.Value))...)
		case assignment.Key.Text == shape.Leaf || assignment.Key.Text == shape.Triggered:
			out = append(out, TextPart{Versions: read.versions(statement)})
		case slices.Contains(shape.Markers, assignment.Key.Text):
			part := TextPart{Random: assignment.Key.Text == shape.Random}
			for _, inner := range jomini.ChildBlock(statement).Statements {
				part.Versions = append(part.Versions, read.versions(inner)...)
			}
			out = append(out, part)
		}
	}
	return out
}

// versions is what one statement of a dynamic description shows: a leaf's key, a triggered leaf's with
// its trigger.
func (read eventText) versions(statement jomini.Statement) []Version {
	assignment, ok := statement.(*jomini.Assignment)
	if !ok {
		return nil
	}
	shape := game.DynamicDescription
	if scalar, ok := assignment.Value.(*jomini.Scalar); ok && assignment.Key.Text == shape.Leaf {
		return []Version{read.version(scalar, "")}
	}
	if block := jomini.BlockOf(assignment.Value); block != nil && assignment.Key.Text == shape.Triggered {
		if leaf := read.scalar(block, shape.Leaf); leaf != nil {
			when, _ := read.trigger(block)
			return []Version{read.version(leaf, when)}
		}
	}
	return nil
}

// option reads an option block at its line: its trigger, each name (a block one per condition, its text
// key the name) and its flags.
func (read eventText) option(block *jomini.Block, line int) Option {
	option := Option{Line: line}
	option.When, _ = read.trigger(block)
	for _, statement := range block.Statements {
		assignment, ok := statement.(*jomini.Assignment)
		if !ok {
			continue
		}
		scalar, _ := assignment.Value.(*jomini.Scalar)
		named := jomini.BlockOf(assignment.Value)
		switch key := assignment.Key.Text; {
		case key == "name" && scalar != nil:
			option.Names = append(option.Names, read.version(scalar, ""))
		case key == "name" && named != nil:
			if text := read.scalar(named, "text"); text != nil {
				when, _ := read.trigger(named)
				option.Names = append(option.Names, read.version(text, when))
			}
		case slices.Contains(game.OptionFlags, key) && scalar != nil && scalar.Text == game.Bools[0]:
			option.Flags = append(option.Flags, key)
		}
	}
	return option
}
