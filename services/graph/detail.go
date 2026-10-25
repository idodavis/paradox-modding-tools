package graph

import (
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Detail is what the page shows about one node: its text, its keys, its firings, its references and its pictures.
type Detail struct {
	Node       Node            `json:"node"`
	Text       string          `json:"text"`     // the event's localized description
	Source     string          `json:"source"`   // the definition as written
	Sections   []Section       `json:"sections"` // the keys at the top of the definition, except its options
	Options    []Section       `json:"options"`  // the definition's option blocks
	Fires      []Edge          `json:"fires"`
	FiredBy    []Edge          `json:"firedBy"`
	Linked     map[string]Node `json:"linked"` // the node at the other end of each of those firings, by its id
	References []Node          `json:"references"`
	Portraits  []string        `json:"portraits"`  // the characters the event's portraits show
	Background string          `json:"background"` // the path of the picture found; "" if there is none
}

// A Section is one key at the top of a definition, as the page shows it.
type Section struct {
	Key    string    `json:"key"`
	Value  string    `json:"value"`            // a scalar as written, or a block on one line with inner blocks as { … }
	Line   int       `json:"line"`             // counted from one
	When   string    `json:"when,omitempty"`   // the block's own trigger on one line
	Random bool      `json:"random,omitempty"` // one of the texts shows at random, else the first whose trigger holds
	Texts  []Version `json:"texts,omitempty"`
	Flags  []string  `json:"flags,omitempty"` // an option's keys written as yes
}

// A Version is one text a key can show, with the trigger that decides it.
type Version struct {
	Key  string `json:"key"` // its localization key
	Text string `json:"text"`
	When string `json:"when,omitempty"` // its trigger on one line; "" if it always can
	Line int    `json:"line"`           // counted from one
}

// The keys of an event that its sections read for their texts, their trigger and their split into options.
const (
	descriptionKey = "desc"
	nameKey        = "name"
	labelKey       = "text"
	optionKey      = "option"
	triggerKey     = "trigger"
)

// BuildDetail returns the detail of the node with the given id, from its kept definition.
func (gph *Service) BuildDetail(id string) (Detail, error) {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return Detail{}, err
	}
	mods := ws.Mods.Load()
	site, err := resolveNode(ws, mods, id)
	if err != nil {
		return Detail{}, err
	}
	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return Detail{}, err
	}
	node := makeNode(ws, mods, site, 0)
	text, _, _ := ws.LookupText(mods, findDescription(tree, statement))
	sections := lo.Map(slices.Collect(tree.Children(statement)), func(child int32, _ int) Section {
		return makeSection(ws, mods, tree, child)
	})
	sections, options := lo.FilterReject(sections, func(section Section, _ int) bool { return section.Key != optionKey })
	fires, firedBy := listFires(ws, mods, site, true), mods.ListFiredBy(ws.Schema, site, true)
	return Detail{
		Node:       node,
		Text:       text,
		Source:     tree.Text(tree.Statements[statement].Span),
		Sections:   sections,
		Options:    options,
		Fires:      makeEdges(ws, mods, fires),
		FiredBy:    makeEdges(ws, mods, firedBy),
		Linked:     listLinked(ws, mods, fires, firedBy),
		References: listReferences(ws, mods, site),
		Portraits:  listPortraits(tree, statement),
		Background: findBackground(ws, mods, tree, statement),
	}, nil
}

// findDescription returns the localization key of a definition's description. If the description is a block, it is the
// first desc inside it that has a value. It returns "" if there is none.
func findDescription(tree *jomini.Tree, statement int32) string {
	child, ok := tree.FindChild(statement, descriptionKey)
	if !ok {
		return ""
	}
	for position := child; position < tree.Statements[child].Last; position++ {
		if description := tree.Statements[position]; tree.Text(description.Key) == descriptionKey && !description.Value.IsEmpty() {
			return tree.Unquoted(description.Value)
		}
	}
	return ""
}

// makeSection returns the section of one statement at the top of a definition. A block also gets its own trigger, the
// texts it can show and, for an option, the keys it writes as yes.
func makeSection(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) Section {
	current := tree.Statements[statement]
	section := Section{Key: tree.Text(current.Key), Value: tree.Unquoted(current.Value), Line: toSourceLine(tree, current.Key)}
	if !current.IsBlock() {
		if version, ok := makeVersion(ws, mods, tree, statement); ok && version.Text != "" {
			section.Texts = []Version{version}
		}
		return section
	}
	section.Value = makeValue(tree, statement)
	section.When = findTrigger(tree, statement)
	section.Texts, section.Random = listVersions(ws, mods, tree, statement)
	if section.Key == optionKey {
		section.Flags = lo.FilterMap(slices.Collect(tree.Children(statement)), func(child int32, _ int) (string, bool) {
			flag := tree.Statements[child]
			return tree.Text(flag.Key), tree.Text(flag.Value) == game.Bools[0]
		})
	}
	return section
}

// makeValue returns a block's statements on one line. A statement that is a block is its head and { … }, and the
// block's own trigger is left out.
func makeValue(tree *jomini.Tree, statement int32) string {
	parts := []string{"{"}
	for child := range tree.Children(statement) {
		current := tree.Statements[child]
		switch {
		case !current.IsBlock():
			parts = append(parts, toOneLine(tree.Text(current.Span)))
		case tree.Text(current.Key) != triggerKey:
			parts = append(parts, toOneLine(tree.Text(jomini.Span{Start: current.Span.Start, End: current.Block.Start}))+" { … }")
		}
	}
	return strings.Join(append(parts, "}"), " ")
}

// findTrigger returns the trigger block a statement holds, on one line. It returns "" if there is none.
func findTrigger(tree *jomini.Tree, statement int32) string {
	trigger, ok := tree.FindChild(statement, triggerKey)
	if !ok || !tree.Statements[trigger].IsBlock() {
		return ""
	}
	return toOneLine(strings.TrimSuffix(strings.TrimPrefix(tree.Text(tree.Statements[trigger].Block), "{"), "}"))
}

// listVersions returns the texts a block can show in the order it writes them, and whether they show at random. A text
// is a desc or a name, or a block with its own trigger that holds one. A first_valid or random_valid block holds more.
func listVersions(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) (versions []Version, random bool) {
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		row, _ := game.LookupKeyword(game.ShapeDescription, "", key)
		switch {
		case row.Shows == "leaf" || key == descriptionKey || key == nameKey:
			if version, ok := makeVersion(ws, mods, tree, child); ok {
				version.When = findTrigger(tree, child)
				versions = append(versions, version)
			}
		case row.Shows != "":
			inner, innerRandom := listVersions(ws, mods, tree, child)
			versions = append(versions, inner...)
			random = random || innerRandom || row.Shows == "random"
		}
	}
	return versions, random
}

// makeVersion returns the text a statement shows. The statement is a key whose value is a localization key, or a block
// that writes one as its desc or text. It returns false if the statement writes none.
func makeVersion(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) (Version, bool) {
	label := statement
	if tree.Statements[statement].IsBlock() {
		// 0 is the file, which has no value
		label, _ = tree.FindChild(statement, descriptionKey)
		if label == 0 {
			label, _ = tree.FindChild(statement, labelKey)
		}
	}
	value := tree.Statements[label].Value
	if value.IsEmpty() {
		return Version{}, false
	}
	key := tree.Unquoted(value)
	text, _, _ := ws.LookupText(mods, key)
	return Version{Key: key, Text: text, Line: toSourceLine(tree, tree.Statements[label].Key)}, true
}

// toSourceLine returns the line of a span in a tree, counted from one.
func toSourceLine(tree *jomini.Tree, span jomini.Span) int {
	return tree.Lines.ToPosition(span.Start).Line + 1
}

// toOneLine returns a text with every run of white space made one space.
func toOneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// makeEdge returns the edge of one firing, with where its reference is written.
func makeEdge(ws *workspace.Session, mods *index.Index, fire index.Fire) Edge {
	return Edge{
		From: toID(mods, fire.Source), To: toID(mods, fire.Target), Days: findDelay(ws, mods, fire.Use), Via: listVia(mods, fire),
		Place: ws.Locate(mods, mods.File(fire.Use).Path), Line: toLine(mods, fire.Use),
	}
}

// makeEdges returns the edges of some firings, each edge once.
func makeEdges(ws *workspace.Session, mods *index.Index, fires []index.Fire) []Edge {
	return lo.UniqBy(lo.Map(fires, func(fire index.Fire, _ int) Edge { return makeEdge(ws, mods, fire) }), toKey)
}

// listLinked returns the nodes at the other end of a node's firings, by id: the targets of what it fires and the
// sources of what fires it.
func listLinked(ws *workspace.Session, mods *index.Index, fires, firedBy []index.Fire) map[string]Node {
	linked := map[string]Node{}
	for _, fire := range fires {
		node := makeNode(ws, mods, fire.Target, 0)
		linked[node.ID] = node
	}
	for _, fire := range firedBy {
		node := makeNode(ws, mods, fire.Source, 0)
		linked[node.ID] = node
	}
	return linked
}

// listReferences returns the nodes that a node's definitions refer to, each name once. A name the game keeps no
// definition of is left out.
func listReferences(ws *workspace.Session, mods *index.Index, site index.Site) []Node {
	references := []Node{}
	seen := map[schema.Reference]bool{}
	for _, part := range listMerged(ws, mods, site) {
		occurrences := mods.Occurrences[part.File]
		for position := part.Occurrence + 1; int(position) < len(occurrences) && isInside(occurrences, position, part.Occurrence); position++ {
			if occurrences[position].Role != index.RoleRefers {
				continue
			}
			for _, reference := range index.ListCallees(mods.ToReference(index.Site{File: part.File, Occurrence: position})) {
				if winner, ok := mods.FindKeptLocal(ws.Schema, reference, part.File); ok && !seen[reference] {
					seen[reference] = true
					references = append(references, makeNode(ws, mods, winner.Site, 0))
				}
			}
		}
	}
	return references
}

// isInside reports whether an occurrence is inside a definition.
func isInside(occurrences []index.Occurrence, position, definition int32) bool {
	for current := occurrences[position]; current.Nested; current = occurrences[current.Parent] {
		if current.Parent == definition {
			return true
		}
	}
	return false
}

// listPortraits returns the characters an event's portraits show. It returns an empty list if the event shows none.
func listPortraits(tree *jomini.Tree, statement int32) []string {
	portraits := []string{}
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		if !lo.SomeBy(game.EventPortraits.Suffixes, func(suffix string) bool { return strings.HasSuffix(key, suffix) }) {
			continue
		}
		portrait, ok := child, true
		if tree.Statements[child].Value.IsEmpty() {
			portrait, ok = tree.FindChild(child, game.EventPortraits.Character)
		}
		if ok && !tree.Statements[portrait].Value.IsEmpty() {
			portraits = append(portraits, tree.Unquoted(tree.Statements[portrait].Value))
		}
	}
	return portraits
}

// findBackground returns the path of the picture an event shows as its background. It returns "" if there is none.
func findBackground(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) string {
	picture := ws.Game.Events.Picture
	if path, ok := mods.ResolveTexture(lo.FirstOr(listValues(tree, statement, picture.FileKey), "")); ok {
		return path
	}
	for _, chain := range picture.Chains {
		values := listValues(tree, statement, lo.FirstOr(chain.Steps, game.PictureStep{}).Key)
		if path := resolveChain(ws, mods, chain, values); path != "" {
			return path
		}
	}
	return ""
}

// resolveChain returns the path of the picture a chain of definitions ends in, starting from the given values. It uses
// the chain's fallback if the game does not read the final file. It returns "" if a hop finds no definition or no
// file is read.
func resolveChain(ws *workspace.Session, mods *index.Index, chain game.PictureChain, values []string) string {
	var tree *jomini.Tree
	var statement int32
	for position, step := range chain.Steps {
		if position > 0 {
			values = listValues(tree, statement, step.Key)
		}
		site, ok := findHop(ws, mods, step, values)
		if !ok {
			return ""
		}
		var err error
		if tree, statement, err = ws.FindStatement(mods, site); err != nil {
			return ""
		}
	}
	if path, ok := mods.ResolveTexture(lo.FirstOr(listValues(tree, statement, chain.FileKey), "")); ok || chain.Fallback == "" {
		return path
	}
	last := game.PictureStep{Database: lo.LastOr(chain.Steps, game.PictureStep{}).Database}
	fallback, ok := findHop(ws, mods, last, listValues(tree, statement, chain.Fallback))
	if !ok {
		return ""
	}
	tree, statement, err := ws.FindStatement(mods, fallback)
	if err != nil {
		return ""
	}
	path, _ := mods.ResolveTexture(lo.FirstOr(listValues(tree, statement, chain.FileKey), ""))
	return path
}

// findHop returns the definition a hop leads to from the given values. It returns false if there is none.
func findHop(ws *workspace.Session, mods *index.Index, step game.PictureStep, values []string) (index.Site, bool) {
	if len(values) == 0 {
		return index.Site{}, false
	}
	if step.Match == "" {
		winner, ok := mods.FindKept(ws.Schema, schema.Reference{Database: step.Database, Name: values[0]})
		return winner.Site, ok
	}
	for _, winner := range mods.ListKept(ws.Schema, step.Database) {
		tree, statement, err := ws.FindStatement(mods, winner.Site)
		if err == nil && lo.Every(listValues(tree, statement, step.Match), values) {
			return winner.Site, true
		}
	}
	return index.Site{}, false
}

// listValues returns the values written at a key path in a definition. The path is keys joined by slashes. It returns
// nil if the path is empty or the key is not written.
func listValues(tree *jomini.Tree, statement int32, path string) []string {
	if path == "" {
		return nil
	}
	for _, key := range strings.Split(path, "/") {
		child, ok := tree.FindChild(statement, key)
		if !ok {
			return nil
		}
		statement = child
	}
	if value := tree.Statements[statement].Value; !value.IsEmpty() {
		return []string{tree.Unquoted(value)}
	}
	var values []string
	for child := range tree.Children(statement) {
		if value := tree.Statements[child].Value; !value.IsEmpty() {
			values = append(values, tree.Unquoted(value))
		}
	}
	return values
}
