package graph

import (
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Detail is everything the detail panel shows for one node: its text, what it fires and what fires it, what it
// refers to, its portraits, its background picture, and the Form view's inputs for its database.
type Detail struct {
	Node       Node           `json:"node"`
	Text       string         `json:"text"` // the event's localized description
	Fires      []Edge         `json:"fires"`
	FiredBy    []Edge         `json:"firedBy"`
	References []Node         `json:"references"`
	Portraits  []string       `json:"portraits"`  // the characters the event's portraits show
	Background string         `json:"background"` // the path of the picture found; "" if there is none
	Inputs     []schema.Input `json:"inputs"`
}

// descriptionKey is the key an event writes its description with.
const descriptionKey = "desc"

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
	return Detail{
		Node:       node,
		Text:       text,
		Fires:      makeEdges(ws, mods, listFires(ws, mods, site, true)),
		FiredBy:    makeEdges(ws, mods, mods.ListFiredBy(ws.Schema, site, true)),
		References: listReferences(ws, mods, site),
		Portraits:  listPortraits(tree, statement),
		Background: findBackground(ws, mods, tree, statement),
		Inputs:     ws.Schema.ListInputs(node.Database),
	}, nil
}

// findDescription returns the localization key of a definition's description: the value of its desc key, or for a
// description that is a block, the first desc inside it that has a value. It returns "" if there is none.
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

// makeEdges returns the edges of some firings, each edge once.
func makeEdges(ws *workspace.Session, mods *index.Index, fires []index.Fire) []Edge {
	return lo.UniqBy(lo.Map(fires, func(fire index.Fire, _ int) Edge {
		return Edge{From: toID(mods, fire.Source), To: toID(mods, fire.Target), Days: findDelay(ws, mods, fire.Use), Via: fire.Via}
	}), toKey)
}

// listReferences returns the nodes of the definitions a node's definitions refer to, each name once. Every definition
// of the node counts where the database merges them. A name that is undefined, or that the game keeps no definition of,
// is left out.
func listReferences(ws *workspace.Session, mods *index.Index, site index.Site) []Node {
	references := []Node{}
	for _, part := range listMerged(ws, mods, site) {
		occurrences := mods.Occurrences[part.File]
		for position := part.Occurrence + 1; int(position) < len(occurrences) && isInside(occurrences, position, part.Occurrence); position++ {
			if occurrences[position].Role != index.RoleRefers {
				continue
			}
			reference := mods.ToReference(index.Site{File: part.File, Occurrence: position})
			if winner, ok := mods.FindKeptLocal(ws.Schema, reference, part.File); ok {
				references = append(references, makeNode(ws, mods, winner.Site, 0))
			}
		}
	}
	return lo.UniqBy(references, func(node Node) string { return node.ID })
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

// listPortraits returns the characters an event's portraits show. A portrait written as a block gives the character it
// names; one written as a value gives that value. It returns an empty list if the event shows none.
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

// findBackground returns the path of the picture an event leads to: the file it writes outright, else the file of the
// first of the game's chains that ends in one the game reads. It returns "" if there is none.
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

// resolveChain returns the path of the picture a chain of definitions ends in, from the values its first hop starts
// with. A file that the game does not read is replaced by the file of the definition the chain names as its
// fallback. It returns "" if a hop finds no definition or no file is read.
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

// findHop returns the definition a hop leads to from the values its key was written with. Without a match key it is
// the kept definition that the first value names. With one it is the first kept definition in the hop's database that
// holds every value at that key. It returns false if there is none.
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

// listValues returns the values written at a key path in a definition: the value of a scalar, or the values inside a
// block. The path is keys joined by slashes. It returns nil if the path is empty or the key is not written.
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
